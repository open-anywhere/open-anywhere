package mocktv

import (
	"bufio"
	"bytes"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestMSearchResponse(t *testing.T) {
	cfg := SSDPConfig{
		Location: "http://127.0.0.1:8095/device.xml",
		UDN:      "uuid:open-anywhere-mock-tv",
		MaxAge:   30 * time.Minute,
	}
	query := "M-SEARCH * HTTP/1.1\r\n" +
		"HOST: 239.255.255.250:1900\r\n" +
		"MAN: \"ssdp:discover\"\r\n" +
		"MX: 1\r\n" +
		"ST: urn:schemas-upnp-org:device:MediaRenderer:1\r\n\r\n"
	raw, ok := mSearchResponse([]byte(query), cfg)
	if !ok {
		t.Fatal("expected a response")
	}
	response, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(raw)), nil)
	if err != nil {
		t.Fatalf("ReadResponse: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	if got := response.Header.Get("LOCATION"); got != cfg.Location {
		t.Fatalf("LOCATION = %q, want %q", got, cfg.Location)
	}
	if got := response.Header.Get("ST"); got != mediaRenderer {
		t.Fatalf("ST = %q, want %q", got, mediaRenderer)
	}
	if got := response.Header.Get("USN"); got != cfg.UDN+"::"+mediaRenderer {
		t.Fatalf("USN = %q", got)
	}
	if got := response.Header.Get("CACHE-CONTROL"); !strings.Contains(got, "max-age=1800") {
		t.Fatalf("CACHE-CONTROL = %q", got)
	}
}

func TestMSearchIgnoresOtherPackets(t *testing.T) {
	cfg := SSDPConfig{Location: "http://127.0.0.1:8095/device.xml", UDN: "uuid:test"}
	cases := []string{
		"NOTIFY * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nNT: upnp:rootdevice\r\n\r\n",
		"M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"ssdp:discover\"\r\nST: urn:schemas-upnp-org:device:MediaServer:1\r\n\r\n",
		"not a request",
	}
	for _, packet := range cases {
		if _, ok := mSearchResponse([]byte(packet), cfg); ok {
			t.Fatalf("unexpected response for %q", packet)
		}
	}
}

func TestMSearchRootDevice(t *testing.T) {
	cfg := SSDPConfig{Location: "http://127.0.0.1:8095/device.xml", UDN: "uuid:test"}
	query := "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"ssdp:discover\"\r\nST: upnp:rootdevice\r\n\r\n"
	raw, ok := mSearchResponse([]byte(query), cfg)
	if !ok {
		t.Fatal("expected a response")
	}
	response, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(raw)), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := response.Header.Get("ST"); got != "upnp:rootdevice" {
		t.Fatalf("ST = %q", got)
	}
}
