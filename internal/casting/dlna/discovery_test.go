package dlna

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
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
