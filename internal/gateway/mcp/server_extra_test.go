package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
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
