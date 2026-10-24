package mcp

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/service"
)

func TestServerDefaultsAndSetTools(t *testing.T) {
	server := New(nil, 0)
	if server.callTimeout != defaultTimeout {
		t.Fatalf("timeout = %v", server.callTimeout)
	}
	if server.currentTools() != nil {
		t.Fatal("tools should start nil")
	}
	fake := &fakeApps{screens: []service.ScreenView{{ScreenID: "tv_1"}}}
	server.SetTools(fake)
	if server.currentTools() != fake {
		t.Fatal("SetTools did not install the service")
	}
	server.Close()
}

func TestServeSSEValidation(t *testing.T) {
	server := New(nil, 0)
	defer server.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /mcp", server.ServeSSE)
	httpServer := httptest.NewServer(mux)
	defer httpServer.Close()

	request, err := http.NewRequest(http.MethodGet, httpServer.URL+"/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Linx-Device-Id", "device")
	request.Header.Set("X-Linx-Session-Id", "session")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("nil tools status = %d", response.StatusCode)
	}

	server.SetTools(&fakeApps{})
	request, err = http.NewRequest(http.MethodGet, httpServer.URL+"/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing headers status = %d", response.StatusCode)
	}
}

type plainWriter struct {
	header http.Header
}

func (w *plainWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}
func (w *plainWriter) Write(data []byte) (int, error) { return len(data), nil }
func (w *plainWriter) WriteHeader(int)                {}

func TestServeSSERequiresStreaming(t *testing.T) {
	server := New(&fakeApps{}, 0)
	request := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	request.Header.Set("X-Linx-Device-Id", "device")
	request.Header.Set("X-Linx-Session-Id", "session")
	recorder := &plainWriter{}
	server.ServeSSE(recorder, request)
	if recorder.header.Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("content type = %q", recorder.header.Get("Content-Type"))
	}
}

// endpointProbeWriter captures the SSE stream and runs probe as soon as the
// endpoint event is flushed, which happens inside server.Connect while
// ServeSSE is still executing.
type endpointProbeWriter struct {
	header http.Header
	body   bytes.Buffer
	probe  func(endpoint string)
	status int
}

func (w *endpointProbeWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *endpointProbeWriter) Write(data []byte) (int, error) { return w.body.Write(data) }
func (w *endpointProbeWriter) WriteHeader(int)                {}

func (w *endpointProbeWriter) Flush() {
	probe := w.probe
	if probe == nil {
		return
	}
	endpoint := ""
	for line := range strings.SplitSeq(w.body.String(), "\n") {
		if value, ok := strings.CutPrefix(line, "data: "); ok {
			endpoint = strings.TrimSpace(value)
			break
		}
	}
	if endpoint == "" {
		return
	}
	w.probe = nil
	probe(endpoint)
}

// TestServeSSERegistersSessionBeforeEndpoint guards against serving a 404 to a
// client that POSTs immediately after receiving the endpoint event.
func TestServeSSERegistersSessionBeforeEndpoint(t *testing.T) {
	server := New(&fakeApps{}, 0)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	probe := &endpointProbeWriter{}
	probe.probe = func(endpoint string) {
		payload := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, endpoint, strings.NewReader(payload))
		server.ServeMessage(recorder, request)
		probe.status = recorder.Code
		cancel()
	}

	request := httptest.NewRequest(http.MethodGet, "/mcp", nil).WithContext(ctx)
	request.Header.Set("X-Linx-Device-Id", "device")
	request.Header.Set("X-Linx-Session-Id", "session")
	server.ServeSSE(probe, request)

	if probe.status != http.StatusAccepted {
		t.Fatalf("POST sent on endpoint event: status = %d", probe.status)
	}
}

func TestConnectionTransportProtocolSupport(t *testing.T) {
	transport := &connectionTransport{inner: &sdk.SSEServerTransport{}}
	if !transport.SupportsProtocolVersion("2025-06-18") {
		t.Fatal("SSE transport should support 2025-06-18")
	}
	plain := &connectionTransport{inner: plainTransport{}}
	if !plain.SupportsProtocolVersion("anything") {
		t.Fatal("unknown transports should accept the version")
	}
}

type plainTransport struct{}

func (plainTransport) Connect(context.Context) (sdk.Connection, error) { return nil, nil }

func TestNotifyWithoutSessions(t *testing.T) {
	server := New(&fakeApps{}, 0)
	server.Notify(app.Identity{DeviceID: "device", SessionID: "session"}, app.Notification{AppID: "app_1"})
}
