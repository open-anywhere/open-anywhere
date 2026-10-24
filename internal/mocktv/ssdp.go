package mocktv

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"
)

// SSDPConfig configures the SSDP responder that makes the simulated screen
// discoverable by the casting service.
type SSDPConfig struct {
	Interface *net.Interface
	Location  string
	UDN       string
	MaxAge    time.Duration
	Logf      func(string, ...any)
}

// Responder answers SSDP M-SEARCH requests for a MediaRenderer.
type Responder struct {
	conn *net.UDPConn
	cfg  SSDPConfig
	logf func(string, ...any)
	done chan struct{}

	closeOnce sync.Once
}

// listenMulticastUDP is a variable so tests can substitute a loopback socket
// instead of depending on a multicast route or the fixed SSDP port.
var listenMulticastUDP = net.ListenMulticastUDP

// udpReply is the unicast socket used to answer one search.
type udpReply interface {
	io.WriteCloser
	SetWriteDeadline(time.Time) error
}

// dialUDP is a variable so tests can simulate an unavailable reply socket.
var dialUDP = func(network string, laddr, raddr *net.UDPAddr) (udpReply, error) {
	return net.DialUDP(network, laddr, raddr)
}

// ListenSSDP joins the SSDP multicast group and answers searches until Close.
func ListenSSDP(cfg SSDPConfig) (*Responder, error) {
	if strings.TrimSpace(cfg.Location) == "" {
		return nil, errors.New("ssdp: location is required")
	}
	if strings.TrimSpace(cfg.UDN) == "" {
		return nil, errors.New("ssdp: UDN is required")
	}
	logf := cfg.Logf
	if logf == nil {
		logf = log.Printf
	}
	group := &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 1900}
	conn, err := listenMulticastUDP("udp4", cfg.Interface, group)
	if err != nil {
		return nil, fmt.Errorf("ssdp listen: %w", err)
	}
	responder := &Responder{conn: conn, cfg: cfg, logf: logf, done: make(chan struct{})}
	go responder.loop()
	return responder, nil
}

// Close stops the responder.
func (r *Responder) Close() error {
	r.closeOnce.Do(func() {
		close(r.done)
		_ = r.conn.Close()
	})
	return nil
}

func (r *Responder) loop() {
	buffer := make([]byte, 4096)
	for {
		n, sender, err := r.conn.ReadFromUDP(buffer)
		if err != nil {
			select {
			case <-r.done:
				return
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			r.logf("mock-tv: ssdp read: %v", err)
			continue
		}
		if sender == nil {
			continue
		}
		response, ok := mSearchResponse(buffer[:n], r.cfg)
		if !ok {
			continue
		}
		reply, err := dialUDP("udp4", nil, sender)
		if err != nil {
			r.logf("mock-tv: ssdp reply socket: %v", err)
			continue
		}
		_ = reply.SetWriteDeadline(time.Now().Add(time.Second))
		if _, err := reply.Write(response); err != nil {
			r.logf("mock-tv: ssdp reply: %v", err)
		}
		_ = reply.Close()
		r.logf("mock-tv: answered M-SEARCH from %s", sender)
	}
}

// mSearchResponse builds a unicast response for a MediaRenderer search. It
// returns false for packets that are not searches this device should answer.
func mSearchResponse(data []byte, cfg SSDPConfig) ([]byte, bool) {
	request, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(data)))
	if err != nil {
		return nil, false
	}
	defer request.Body.Close()
	if request.Method != "M-SEARCH" {
		return nil, false
	}
	if man := strings.TrimSpace(strings.Trim(request.Header.Get("MAN"), `"`)); man != "" && !strings.EqualFold(man, "ssdp:discover") {
		return nil, false
	}
	searchTarget := strings.TrimSpace(request.Header.Get("ST"))
	var replyST, usn string
	switch searchTarget {
	case mediaRenderer, "ssdp:all":
		replyST = mediaRenderer
		usn = cfg.UDN + "::" + mediaRenderer
	case "upnp:rootdevice":
		replyST = "upnp:rootdevice"
		usn = cfg.UDN + "::upnp:rootdevice"
	default:
		return nil, false
	}
	age := int(cfg.MaxAge / time.Second)
	if age <= 0 {
		age = 1800
	}
	var response strings.Builder
	response.WriteString("HTTP/1.1 200 OK\r\n")
	fmt.Fprintf(&response, "CACHE-CONTROL: max-age=%d\r\n", age)
	fmt.Fprintf(&response, "DATE: %s\r\n", time.Now().UTC().Format(http.TimeFormat))
	response.WriteString("EXT:\r\n")
	fmt.Fprintf(&response, "LOCATION: %s\r\n", cfg.Location)
	fmt.Fprintf(&response, "SERVER: %s UPnP/1.0 mock-tv/0.1\r\n", runtime.GOOS)
	fmt.Fprintf(&response, "ST: %s\r\n", replyST)
	fmt.Fprintf(&response, "USN: %s\r\n", usn)
	response.WriteString("Content-Length: 0\r\n\r\n")
	return []byte(response.String()), true
}
