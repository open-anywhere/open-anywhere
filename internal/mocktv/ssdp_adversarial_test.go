package mocktv

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func loopbackUDP(t *testing.T) *net.UDPConn {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen loopback UDP: %v", err)
	}
	return conn
}

func TestListenSSDPValidation(t *testing.T) {
	if _, err := ListenSSDP(SSDPConfig{UDN: "uuid:test"}); err == nil {
		t.Fatal("empty location accepted")
	}
	if _, err := ListenSSDP(SSDPConfig{Location: "http://127.0.0.1/device.xml", UDN: "   "}); err == nil {
		t.Fatal("blank UDN accepted")
	}
}

func TestListenSSDPInjectedSocket(t *testing.T) {
	original := listenMulticastUDP
	t.Cleanup(func() { listenMulticastUDP = original })

	conn := loopbackUDP(t)
	var (
		gotNetwork string
		gotIface   *net.Interface
		gotGroup   *net.UDPAddr
	)
	listenMulticastUDP = func(network string, iface *net.Interface, group *net.UDPAddr) (*net.UDPConn, error) {
		gotNetwork, gotIface, gotGroup = network, iface, group
		return conn, nil
	}
	responder, err := ListenSSDP(SSDPConfig{
		Interface: &net.Interface{Name: "lo0"},
		Location:  "http://127.0.0.1:8095/device.xml",
		UDN:       "uuid:test",
		MaxAge:    time.Minute,
	})
	if err != nil {
		t.Fatalf("ListenSSDP: %v", err)
	}
	if responder == nil {
		t.Fatal("nil responder")
	}
	if gotNetwork != "udp4" {
		t.Fatalf("network = %q", gotNetwork)
	}
	if gotIface == nil || gotIface.Name != "lo0" {
		t.Fatalf("interface = %+v", gotIface)
	}
	if gotGroup == nil || !gotGroup.IP.Equal(net.IPv4(239, 255, 255, 250)) || gotGroup.Port != 1900 {
		t.Fatalf("group = %+v", gotGroup)
	}
	if err := responder.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := responder.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestListenSSDPListenFailure(t *testing.T) {
	original := listenMulticastUDP
	t.Cleanup(func() { listenMulticastUDP = original })
	listenMulticastUDP = func(string, *net.Interface, *net.UDPAddr) (*net.UDPConn, error) {
		return nil, errors.New("multicast unavailable")
	}
	_, err := ListenSSDP(SSDPConfig{Location: "http://127.0.0.1/device.xml", UDN: "uuid:test"})
	if err == nil || !strings.Contains(err.Error(), "ssdp listen") {
		t.Fatalf("error = %v", err)
	}
}

func newTestResponder(conn *net.UDPConn, logs chan string) *Responder {
	logf := func(format string, args ...any) {
		select {
		case logs <- fmt.Sprintf(format, args...):
		default:
		}
	}
	return &Responder{
		conn: conn,
		cfg: SSDPConfig{
			Location: "http://127.0.0.1:8095/device.xml",
			UDN:      "uuid:adversarial",
			MaxAge:   time.Minute,
		},
		logf: logf,
		done: make(chan struct{}),
	}
}

const validSearch = "M-SEARCH * HTTP/1.1\r\n" +
	"HOST: 239.255.255.250:1900\r\n" +
	"MAN: \"ssdp:discover\"\r\n" +
	"MX: 1\r\n" +
	"ST: urn:schemas-upnp-org:device:MediaRenderer:1\r\n\r\n"

func TestResponderLoopAnswersAndIgnoresJunk(t *testing.T) {
	conn := loopbackUDP(t)
	logs := make(chan string, 64)
	responder := newTestResponder(conn, logs)
	loopDone := make(chan struct{})
	go func() {
		responder.loop()
		close(loopDone)
	}()
	t.Cleanup(func() { responder.Close() })

	// Use an unconnected socket: connected UDP clients drop replies that come
	// from the responder's separate reply socket.
	client := loopbackUDP(t)
	defer client.Close()

	if _, err := client.WriteToUDP([]byte("garbage that is not an HTTP request\r\n\r\n"), conn.LocalAddr().(*net.UDPAddr)); err != nil {
		t.Fatal(err)
	}
	_ = client.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	if _, _, err := client.ReadFromUDP(make([]byte, 512)); err == nil {
		t.Fatal("junk packet received a response")
	}

	if _, err := client.WriteToUDP([]byte(validSearch), conn.LocalAddr().(*net.UDPAddr)); err != nil {
		t.Fatal(err)
	}
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	buffer := make([]byte, 2048)
	n, _, err := client.ReadFromUDP(buffer)
	if err != nil {
		t.Fatalf("read M-SEARCH reply: %v", err)
	}
	response, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(buffer[:n])), nil)
	if err != nil {
		t.Fatalf("reply is not an HTTP response: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	if got := response.Header.Get("USN"); !strings.Contains(got, "uuid:adversarial") {
		t.Fatalf("USN = %q", got)
	}

	select {
	case line := <-logs:
		if !strings.Contains(line, "answered M-SEARCH") {
			t.Fatalf("log = %q", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no reply log")
	}

	// Closing the socket directly (without Close) must also end the loop.
	_ = conn.Close()
	select {
	case <-loopDone:
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not exit after socket close")
	}
}

func TestResponderLoopRecoversFromReadTimeout(t *testing.T) {
	conn := loopbackUDP(t)
	logs := make(chan string, 64)
	responder := newTestResponder(conn, logs)
	loopDone := make(chan struct{})
	go func() {
		responder.loop()
		close(loopDone)
	}()
	t.Cleanup(func() { responder.Close() })

	// An expired deadline produces a non-ErrClosed read error: the loop must
	// log it and keep serving rather than exit.
	_ = conn.SetReadDeadline(time.Now().Add(-time.Second))
	select {
	case line := <-logs:
		if !strings.Contains(line, "ssdp read") {
			t.Fatalf("log = %q", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("read timeout was not logged")
	}
	_ = conn.SetReadDeadline(time.Time{})

	client := loopbackUDP(t)
	defer client.Close()
	if _, err := client.WriteToUDP([]byte(validSearch), conn.LocalAddr().(*net.UDPAddr)); err != nil {
		t.Fatal(err)
	}
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := client.ReadFromUDP(make([]byte, 2048)); err != nil {
		t.Fatalf("loop stopped serving after timeout: %v", err)
	}

	responder.Close()
	select {
	case <-loopDone:
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not exit after Close")
	}
}

func TestResponderConcurrentSearches(t *testing.T) {
	conn := loopbackUDP(t)
	logs := make(chan string, 256)
	responder := newTestResponder(conn, logs)
	loopDone := make(chan struct{})
	go func() {
		responder.loop()
		close(loopDone)
	}()
	defer responder.Close()

	var group sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 10; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			client, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if err != nil {
				errs <- err
				return
			}
			defer client.Close()
			if _, err := client.WriteToUDP([]byte(validSearch), conn.LocalAddr().(*net.UDPAddr)); err != nil {
				errs <- err
				return
			}
			_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
			if _, _, err := client.ReadFromUDP(make([]byte, 2048)); err != nil {
				errs <- err
			}
		}()
	}
	group.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent search failed: %v", err)
	}
	responder.Close()
	select {
	case <-loopDone:
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not exit")
	}
}

type failingReply struct {
	writes int
	closed int
}

func (r *failingReply) Write([]byte) (int, error) { r.writes++; return 0, errors.New("write failed") }
func (r *failingReply) Close() error              { r.closed++; return nil }
func (r *failingReply) SetWriteDeadline(time.Time) error {
	return nil
}

func startSearchResponder(t *testing.T, logs chan string) (*Responder, *net.UDPConn, chan struct{}) {
	t.Helper()
	conn := loopbackUDP(t)
	responder := newTestResponder(conn, logs)
	done := make(chan struct{})
	go func() {
		responder.loop()
		close(done)
	}()
	t.Cleanup(func() { responder.Close() })
	return responder, conn, done
}

func sendSearch(t *testing.T, conn *net.UDPConn) {
	t.Helper()
	client := loopbackUDP(t)
	defer client.Close()
	if _, err := client.WriteToUDP([]byte(validSearch), conn.LocalAddr().(*net.UDPAddr)); err != nil {
		t.Fatal(err)
	}
	_ = client.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	_, _, _ = client.ReadFromUDP(make([]byte, 512))
}

func TestResponderReplySocketFailure(t *testing.T) {
	original := dialUDP
	t.Cleanup(func() { dialUDP = original })
	logs := make(chan string, 64)
	dialUDP = func(string, *net.UDPAddr, *net.UDPAddr) (udpReply, error) {
		return nil, errors.New("no reply socket")
	}
	_, conn, _ := startSearchResponder(t, logs)
	sendSearch(t, conn)
	deadline := time.After(2 * time.Second)
	for {
		select {
		case line := <-logs:
			if strings.Contains(line, "ssdp reply socket") {
				return
			}
		case <-deadline:
			t.Fatal("dial failure was not logged")
		}
	}
}

func TestResponderReplyWriteFailure(t *testing.T) {
	original := dialUDP
	t.Cleanup(func() { dialUDP = original })
	reply := &failingReply{}
	dialUDP = func(string, *net.UDPAddr, *net.UDPAddr) (udpReply, error) {
		return reply, nil
	}
	logs := make(chan string, 64)
	responder, conn, done := startSearchResponder(t, logs)
	sendSearch(t, conn)

	deadline := time.After(2 * time.Second)
	for {
		select {
		case line := <-logs:
			if strings.Contains(line, "ssdp reply:") {
				if reply.writes == 0 {
					t.Fatal("write was never attempted")
				}
				responder.Close()
				select {
				case <-done:
				case <-time.After(2 * time.Second):
					t.Fatal("loop did not exit")
				}
				if reply.closed != 1 {
					t.Fatalf("reply socket close calls = %d", reply.closed)
				}
				return
			}
		case <-deadline:
			t.Fatal("write failure was not logged")
		}
	}
}

func TestMSearchResponseAdversarial(t *testing.T) {
	cfg := SSDPConfig{Location: "http://127.0.0.1:8095/device.xml", UDN: "uuid:test"}
	cases := []struct {
		name   string
		packet string
		ok     bool
	}{
		{"wrong MAN", "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"not-ssdp\"\r\nST: ssdp:all\r\n\r\n", false},
		{"unquoted MAN", "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: ssdp:discover\r\nST: ssdp:all\r\n\r\n", true},
		{"uppercase MAN", "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"SSDP:DISCOVER\"\r\nST: ssdp:all\r\n\r\n", true},
		{"padded ST", "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nST:   ssdp:all   \r\n\r\n", true},
		{"uppercase ST", "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nST: SSDP:ALL\r\n\r\n", false},
		{"truncated packet", "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nST: ssdp:all\r\n", false},
		{"huge unknown ST", "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nST: " + strings.Repeat("a", 5000) + "\r\n\r\n", false},
		{"GET method", "GET / HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nST: ssdp:all\r\n\r\n", false},
		{"content length body", "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nST: ssdp:all\r\nContent-Length: 4\r\n\r\njunk", true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, ok := mSearchResponse([]byte(testCase.packet), cfg); ok != testCase.ok {
				t.Fatalf("mSearchResponse ok = %v, want %v", ok, testCase.ok)
			}
		})
	}
}

func TestMSearchResponseMaxAgeFallbacks(t *testing.T) {
	for _, maxAge := range []time.Duration{0, -time.Minute, time.Second} {
		cfg := SSDPConfig{Location: "http://127.0.0.1/device.xml", UDN: "uuid:test", MaxAge: maxAge}
		raw, ok := mSearchResponse([]byte(validSearch), cfg)
		if !ok {
			t.Fatalf("max-age %v: no response", maxAge)
		}
		want := fmt.Sprintf("max-age=%d", int(maxAge/time.Second))
		if maxAge <= 0 {
			want = "max-age=1800"
		}
		if !strings.Contains(string(raw), want) {
			t.Fatalf("max-age %v: response = %s", maxAge, raw)
		}
	}
}
