package mocktv

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	core "github.com/open-anywhere/open-anywhere/internal/casting"
	"github.com/open-anywhere/open-anywhere/internal/casting/dlna"
	api "github.com/open-anywhere/open-anywhere/pkg/casting"
)

// The casting service drives a real DLNA device through internal/casting/dlna.
// This test uses that same client so mock-tv cannot drift from the protocol the
// control plane actually speaks.
func TestAVTransportMatchesCastingClient(t *testing.T) {
	display := NewDisplay(Config{Name: "Mock TV", UDN: "uuid:test", Logf: t.Logf})
	server := httptest.NewServer(display.Handler())
	defer server.Close()

	device := core.Device{ControlURL: server.URL + "/control/avtransport"}
	transport := &dlna.Transport{}
	ctx := context.Background()
	source := api.StreamSource{
		Type:       "stream",
		URL:        "http://127.0.0.1:8091/streams/instance/live.ts",
		Format:     "mpegts",
		VideoCodec: "h264",
	}
	if err := transport.Start(ctx, device, source); err != nil {
		t.Fatalf("Start: %v", err)
	}
	state, err := transport.Status(ctx, device)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if state != api.SessionPlaying {
		t.Fatalf("state = %q, want %q", state, api.SessionPlaying)
	}
	if got := display.State(); got.State != "playing" || got.URL != source.URL {
		t.Fatalf("display state = %+v", got)
	}

	if err := transport.Stop(ctx, device); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	state, err = transport.Status(ctx, device)
	if err != nil {
		t.Fatalf("Status after stop: %v", err)
	}
	if state != api.SessionStopped {
		t.Fatalf("state after stop = %q, want %q", state, api.SessionStopped)
	}
	if got := display.State(); got.State != "idle" {
		t.Fatalf("display state after stop = %+v", got)
	}
}

func TestCurrentURIDecodesXML(t *testing.T) {
	body := `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body>` +
		`<u:SetAVTransportURI xmlns:u="urn:schemas-upnp-org:service:AVTransport:1">` +
		`<InstanceID>0</InstanceID><CurrentURI>http://127.0.0.1:8091/live.ts?x=1&amp;y=2</CurrentURI>` +
		`</u:SetAVTransportURI></s:Body></s:Envelope>`
	if got, want := currentURI([]byte(body)), "http://127.0.0.1:8091/live.ts?x=1&y=2"; got != want {
		t.Fatalf("currentURI = %q, want %q", got, want)
	}
}

func TestUnsupportedSOAPActionReturnsFault(t *testing.T) {
	display := NewDisplay(Config{Logf: t.Logf})
	request := httptest.NewRequest(http.MethodPost, "/control/avtransport", strings.NewReader(
		`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:GetVolume xmlns:u="urn:schemas-upnp-org:service:AVTransport:1"/></s:Body></s:Envelope>`))
	request.Header.Set("SOAPAction", `"urn:schemas-upnp-org:service:AVTransport:1#GetVolume"`)
	recorder := httptest.NewRecorder()
	display.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "Invalid Action") {
		t.Fatalf("fault body = %s", recorder.Body.String())
	}
}
