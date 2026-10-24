package mocktv

import (
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeviceDescription(t *testing.T) {
	const name = `客厅<电视> & "A"`
	display := NewDisplay(Config{Name: name, UDN: "uuid:test-device", Logf: t.Logf})
	server := httptest.NewServer(display.Handler())
	defer server.Close()

	response, err := http.Get(server.URL + "/device.xml")
	if err != nil {
		t.Fatalf("GET /device.xml: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	var root struct {
		Device struct {
			DeviceType   string `xml:"deviceType"`
			FriendlyName string `xml:"friendlyName"`
			UDN          string `xml:"UDN"`
			Services     []struct {
				ServiceType string `xml:"serviceType"`
				ControlURL  string `xml:"controlURL"`
			} `xml:"serviceList>service"`
		} `xml:"device"`
	}
	if err := xml.Unmarshal(body, &root); err != nil {
		t.Fatalf("device description is not valid XML: %v\n%s", err, body)
	}
	if root.Device.DeviceType != mediaRenderer {
		t.Fatalf("deviceType = %q", root.Device.DeviceType)
	}
	if root.Device.FriendlyName != name {
		t.Fatalf("friendlyName = %q, want %q", root.Device.FriendlyName, name)
	}
	if root.Device.UDN != "uuid:test-device" {
		t.Fatalf("UDN = %q", root.Device.UDN)
	}
	if len(root.Device.Services) != 1 {
		t.Fatalf("services = %+v", root.Device.Services)
	}
	service := root.Device.Services[0]
	if service.ServiceType != avTransport {
		t.Fatalf("serviceType = %q", service.ServiceType)
	}
	if service.ControlURL != "/control/avtransport" {
		t.Fatalf("controlURL = %q", service.ControlURL)
	}
}

func TestIndexAndPlayerScript(t *testing.T) {
	display := NewDisplay(Config{Logf: t.Logf})
	server := httptest.NewServer(display.Handler())
	defer server.Close()

	page, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer page.Body.Close()
	html, err := io.ReadAll(page.Body)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/mpegts.js", "EventSource('/events')", "/stream?url="} {
		if !strings.Contains(string(html), want) {
			t.Fatalf("page does not contain %q", want)
		}
	}

	script, err := http.Get(server.URL + "/mpegts.js")
	if err != nil {
		t.Fatalf("GET /mpegts.js: %v", err)
	}
	defer script.Body.Close()
	js, err := io.ReadAll(script.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(js) < 100*1024 {
		t.Fatalf("mpegts.js is unexpectedly small: %d bytes", len(js))
	}
	if got := script.Header.Get("Content-Type"); !strings.Contains(got, "javascript") {
		t.Fatalf("Content-Type = %q", got)
	}
}
