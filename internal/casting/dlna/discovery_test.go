package dlna

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func rendererXML(base, control string) string {
	return `<root><URLBase>` + base + `</URLBase><device><deviceType>urn:schemas-upnp-org:device:MediaRenderer:1</deviceType><friendlyName>Living room</friendlyName><UDN>uuid:stable-id</UDN><serviceList><service><serviceType>` + avTransport + `</serviceType><controlURL>` + control + `</controlURL></service></serviceList></device></root>`
}
func TestSSDPAdvertisements(t *testing.T) {
	for _, tt := range []struct {
		name, response string
		want           time.Duration
		bad            bool
	}{
		{"valid", "HTTP/1.1 200 OK\r\nLOCATION: http://127.0.0.1/device.xml\r\nCACHE-CONTROL: max-age=1800\r\n\r\n", 30 * time.Minute, false},
		{"default TTL", "HTTP/1.1 200 OK\r\nlocation: http://127.0.0.1/device.xml\r\n\r\n", 30 * time.Minute, false},
		{"quoted TTL", "HTTP/1.1 200 OK\r\nlocation: http://127.0.0.1/device.xml\r\nCache-Control: public, max-age=\"30\"\r\n\r\n", 30 * time.Second, false},
		{"clamp TTL", "HTTP/1.1 200 OK\r\nlocation: http://127.0.0.1/device.xml\r\nCache-Control: max-age=999999999\r\n\r\n", 24 * time.Hour, false},
		{"bad packet", "garbage", 0, true},
		{"bad status", "HTTP/1.1 404 Nope\r\nlocation: http://127.0.0.1/device.xml\r\n\r\n", 0, true},
		{"no location", "HTTP/1.1 200 OK\r\n\r\n", 0, true},
		{"bad scheme", "HTTP/1.1 200 OK\r\nlocation: file:///tmp/device.xml\r\n\r\n", 0, true},
		{"bad age", "HTTP/1.1 200 OK\r\nlocation: http://127.0.0.1/device.xml\r\nCache-Control: max-age=-1\r\n\r\n", 0, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAdvertisement([]byte(tt.response))
			if (err != nil) != tt.bad {
				t.Fatal(err)
			}
			if !tt.bad && got.ttl != tt.want {
				t.Fatalf("%v", got.ttl)
			}
		})
	}
}
func TestDescriptionParsing(t *testing.T) {
	tests := []struct {
		name   string
		body   func(string) string
		status int
		path   string
		bad    bool
	}{
		{"relative", func(_ string) string { return rendererXML("", "control") }, 200, "/devices/control", false},
		{"absolute path", func(_ string) string { return rendererXML("", "/control") }, 200, "/control", false},
		{"URLBase", func(base string) string { return rendererXML(base+"/base/", "control") }, 200, "/base/control", false},
		{"absolute URL", func(base string) string { return rendererXML("", base+"/control") }, 200, "/control", false},
		{"embedded renderer", func(_ string) string {
			inner := rendererXML("", "/control")
			inner = strings.TrimSuffix(strings.Split(inner, "<device>")[1], "</root>")
			return "<root><device><deviceList><device>" + inner + "</deviceList></device></root>"
		}, 200, "/control", false},
		{"invalid XML", func(_ string) string { return "<" }, 200, "", true},
		{"HTTP error", func(_ string) string { return "nope" }, 500, "", true},
		{"missing identity", func(_ string) string { return strings.ReplaceAll(rendererXML("", "/control"), "uuid:stable-id", "") }, 200, "", true},
		{"wrong device type", func(_ string) string {
			return strings.ReplaceAll(rendererXML("", "/control"), "MediaRenderer", "MediaServer")
		}, 200, "", true},
		{"missing AVTransport", func(_ string) string { return strings.ReplaceAll(rendererXML("", "/control"), "AVTransport", "Other") }, 200, "", true},
		{"bad control URL", func(_ string) string { return rendererXML("", "file:///tmp/control") }, 200, "", true},
		{"bad base", func(_ string) string { return rendererXML("http://[", "control") }, 200, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var base string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tt.status); fmt.Fprint(w, tt.body(base)) }))
			defer server.Close()
			base = server.URL
			device, err := (&Discoverer{Client: server.Client()}).describe(context.Background(), advertisement{location: base + "/devices/description.xml", ttl: time.Minute})
			if (err != nil) != tt.bad {
				t.Fatalf("%+v %v", device, err)
			}
			if !tt.bad && (device.ControlURL != base+tt.path || device.Target.ID != "dlna-stable-id" || device.Target.Endpoints[0].ID != "dlna-renderer" || time.Until(device.ExpiresAt) < 50*time.Second) {
				t.Fatalf("bad description: %+v", device)
			}
		})
	}
}
func TestDiscovererLocalUDP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, rendererXML("", "/control")) }))
	defer server.Close()
	udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	received := make(chan string, 1)
	go func() {
		buf := make([]byte, 4096)
		n, peer, err := udp.ReadFromUDP(buf)
		if err != nil {
			return
		}
		received <- string(buf[:n])
		// Duplicate announcements and malformed neighbors must not duplicate targets.
		reply := []byte("HTTP/1.1 200 OK\r\nLOCATION: " + server.URL + "/description.xml\r\nCACHE-CONTROL: max-age=120\r\n\r\n")
		_, _ = udp.WriteToUDP([]byte("garbage"), peer)
		for range 2 {
			_, _ = udp.WriteToUDP(reply, peer)
		}
	}()
	d := &Discoverer{LocalIP: net.ParseIP("127.0.0.1"), Destination: udp.LocalAddr().String(), Window: 100 * time.Millisecond, Client: server.Client()}
	targets, err := d.Scan(context.Background())
	if err != nil || len(targets) != 1 {
		t.Fatalf("%+v %v", targets, err)
	}
	if query := <-received; !strings.Contains(query, "M-SEARCH") || !strings.Contains(query, "device:MediaRenderer:1") {
		t.Fatal(query)
	}
}
func TestDiscoveryCancellationAndEmptyNetwork(t *testing.T) {
	udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	d := &Discoverer{LocalIP: net.ParseIP("127.0.0.1"), Destination: udp.LocalAddr().String(), Window: 10 * time.Millisecond}
	got, err := d.Scan(context.Background())
	if err != nil || len(got) != 0 {
		t.Fatalf("%+v %v", got, err)
	}
	d.Window = time.Minute
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := d.Scan(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	d.Destination = "invalid address"
	if _, err := d.Scan(context.Background()); err == nil {
		t.Fatal("invalid destination succeeded")
	}
}

func TestDiscoveryPartialFailureAndCancellationWhileReading(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad.xml" {
			w.WriteHeader(503)
			return
		}
		fmt.Fprint(w, rendererXML("", "/control"))
	}))
	defer server.Close()
	udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	go func() {
		buf := make([]byte, 4096)
		_, peer, err := udp.ReadFromUDP(buf)
		if err != nil {
			return
		}
		for _, path := range []string{"/good.xml", "/bad.xml"} {
			_, _ = udp.WriteToUDP([]byte("HTTP/1.1 200 OK\r\nLOCATION: "+server.URL+path+"\r\n\r\n"), peer)
		}
	}()
	d := &Discoverer{LocalIP: net.ParseIP("127.0.0.1"), Destination: udp.LocalAddr().String(), Window: 100 * time.Millisecond}
	devices, err := d.Scan(context.Background())
	if err == nil || len(devices) != 1 {
		t.Fatalf("want one device and partial error, got %+v %v", devices, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	d.Window = time.Minute
	if _, err := d.Scan(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline exceeded, got %v", err)
	}
}

// scriptedSocket injects socket errors, while the existing UDP tests exercise
// the real network implementation against local temporary ports.
type scriptedSocket struct {
	deadline                 time.Time
	setDeadlineErr, writeErr error
	read                     func([]byte) (int, *net.UDPAddr, error)
	write                    func([]byte, *net.UDPAddr) (int, error)
	closed                   chan struct{}
	closeOnce                sync.Once
}

func (s *scriptedSocket) SetDeadline(deadline time.Time) error {
	s.deadline = deadline
	return s.setDeadlineErr
}
func (s *scriptedSocket) WriteToUDP(data []byte, addr *net.UDPAddr) (int, error) {
	if s.write != nil {
		return s.write(data, addr)
	}
	return len(data), s.writeErr
}
func (s *scriptedSocket) ReadFromUDP(buf []byte) (int, *net.UDPAddr, error) { return s.read(buf) }
func (s *scriptedSocket) Close() error {
	s.closeOnce.Do(func() {
		if s.closed != nil {
			close(s.closed)
		}
	})
	return nil
}

type socketTimeout struct{}

func (socketTimeout) Error() string   { return "socket deadline exceeded" }
func (socketTimeout) Timeout() bool   { return true }
func (socketTimeout) Temporary() bool { return true }

func TestDiscoverySocketFailures(t *testing.T) {
	cause := errors.New("socket failure")
	for _, tt := range []struct {
		name                                      string
		listenErr, deadlineErr, writeErr, readErr error
	}{
		{name: "listen", listenErr: cause},
		{name: "set deadline", deadlineErr: cause},
		{name: "send", writeErr: cause},
		{name: "receive", readErr: cause},
	} {
		t.Run(tt.name, func(t *testing.T) {
			socket := &scriptedSocket{setDeadlineErr: tt.deadlineErr, writeErr: tt.writeErr, closed: make(chan struct{}), read: func([]byte) (int, *net.UDPAddr, error) { return 0, nil, tt.readErr }}
			d := &Discoverer{LocalIP: net.IPv4(127, 0, 0, 1), Destination: "127.0.0.1:1900", Window: time.Second, listenUDP: func(network string, addr *net.UDPAddr) (discoverySocket, error) {
				if network != "udp4" || !addr.IP.Equal(net.IPv4(127, 0, 0, 1)) {
					t.Error("wrong bind configuration")
				}
				return socket, tt.listenErr
			}}
			if _, err := d.Scan(context.Background()); !errors.Is(err, cause) {
				t.Fatalf("want socket failure, got %v", err)
			}
			if tt.listenErr == nil {
				select {
				case <-socket.closed:
				default:
					t.Fatal("socket leaked after error")
				}
			}
		})
	}
}

func TestDiscoveryDefaultsWithoutLANMulticast(t *testing.T) {
	before := time.Now()
	socket := &scriptedSocket{closed: make(chan struct{}), read: func([]byte) (int, *net.UDPAddr, error) { return 0, nil, socketTimeout{} }}
	socket.write = func(data []byte, remote *net.UDPAddr) (int, error) {
		if remote.String() != "239.255.255.250:1900" || !strings.Contains(string(data), "MX:1\r\n") {
			t.Errorf("wrong default destination or discovery request: %s %s", remote, data)
		}
		return len(data), nil
	}
	d := &Discoverer{listenUDP: func(string, *net.UDPAddr) (discoverySocket, error) { return socket, nil }}
	got, err := d.Scan(context.Background())
	if err != nil || len(got) != 0 {
		t.Fatalf("%+v %v", got, err)
	}
	if delta := socket.deadline.Sub(before); delta < 3*time.Second || delta > 4*time.Second {
		t.Fatalf("wrong default scan deadline: %v", delta)
	}
}

func TestDiscoveryCancellationClosesBlockedSocket(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	socket := &scriptedSocket{closed: make(chan struct{})}
	socket.read = func([]byte) (int, *net.UDPAddr, error) { close(entered); <-socket.closed; return 0, nil, net.ErrClosed }
	d := &Discoverer{Destination: "127.0.0.1:1900", Window: time.Hour, listenUDP: func(string, *net.UDPAddr) (discoverySocket, error) { return socket, nil }}
	done := make(chan error, 1)
	go func() { _, err := d.Scan(ctx); done <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("read never started")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not unblock read")
	}
}

// Socket deadlines can fire before a context's cancellation callback publishes
// Err. This context reproduces that ordering without a scheduler-dependent race.
type pendingDeadlineContext struct{ context.Context }

func (pendingDeadlineContext) Deadline() (time.Time, bool) { return time.Now().Add(-time.Second), true }
func TestDiscoverySocketDeadlineBeforeContextErr(t *testing.T) {
	socket := &scriptedSocket{read: func([]byte) (int, *net.UDPAddr, error) { return 0, nil, socketTimeout{} }}
	d := &Discoverer{Destination: "127.0.0.1:1900", Window: time.Hour, listenUDP: func(string, *net.UDPAddr) (discoverySocket, error) { return socket, nil }}
	if _, err := d.Scan(pendingDeadlineContext{context.Background()}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline exceeded, got %v", err)
	}
}

func TestDescriptionRequestErrors(t *testing.T) {
	if _, err := (&Discoverer{}).describe(context.Background(), advertisement{location: "http://["}); err == nil {
		t.Fatal("malformed description URL accepted")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("canceled request reached server") }))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (&Discoverer{}).describe(ctx, advertisement{location: server.URL}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled description request: %v", err)
	}
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, rendererXML("", "http://[")) }))
	defer server2.Close()
	if _, err := (&Discoverer{}).describe(context.Background(), advertisement{location: server2.URL}); err == nil {
		t.Fatal("malformed control URL accepted")
	}
}
