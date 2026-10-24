package mcp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
)

type errWriter struct {
	header http.Header
	status int
}

func (w *errWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}
func (w *errWriter) Write([]byte) (int, error) { return 0, errors.New("client went away") }
func (w *errWriter) WriteHeader(status int)    { w.status = status }
func (w *errWriter) Flush()                    {}

func TestSessionWriteWithoutConnectionDropsMessage(t *testing.T) {
	item := &session{}
	message := &jsonrpc.Request{Method: "notifications/app_status"}
	if err := item.write(context.Background(), message); err != nil {
		t.Fatalf("write = %v", err)
	}
	// Closing a session that never connected must also be safe.
	item.close()
}

type failingConn struct{}

func (failingConn) Read(context.Context) (jsonrpc.Message, error) { return nil, io.EOF }
func (failingConn) Write(context.Context, jsonrpc.Message) error  { return errors.New("write failed") }
func (failingConn) Close() error                                  { return nil }
func (failingConn) SessionID() string                             { return "conn" }

func TestNotifyLogsWriteFailures(t *testing.T) {
	server := New(&fakeApps{}, 0)
	sink := &logSink{}
	server.logf = sink.logf
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	server.sessions["s1"] = &session{identity: owner, connection: failingConn{}, cancel: func() {}}
	t.Cleanup(server.Close)

	server.Notify(owner, app.Notification{AppID: "app_1", State: app.StateError})
	log := sink.all()
	if !strings.Contains(log, "notify app_1") || !strings.Contains(log, "write failed") {
		t.Fatalf("log = %q", log)
	}
}

func TestServeMessageRejectsSessionOwnerMismatch(t *testing.T) {
	server := New(&fakeApps{}, 0)
	owner := app.Identity{DeviceID: "device", AgentID: "agent", SessionID: "session"}
	server.sessions["sid"] = &session{identity: owner}
	cases := []struct{ header, value string }{
		{"X-Linx-Device-Id", "other-device"},
		{"X-Linx-Agent-Id", "other-agent"},
		{"X-Linx-Session-Id", "other-session"},
	}
	for _, tc := range cases {
		t.Run(tc.header, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/mcp/message?sessionid=sid", strings.NewReader("{}"))
			request.Header.Set(tc.header, tc.value)
			recorder := httptest.NewRecorder()
			server.ServeMessage(recorder, request)
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("status = %d", recorder.Code)
			}
		})
	}
}

func TestServeSSEConnectFailure(t *testing.T) {
	server := New(&fakeApps{}, 0)
	request := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	request.Header.Set("X-Linx-Device-Id", "device")
	request.Header.Set("X-Linx-Session-Id", "session")
	writer := &errWriter{}
	server.ServeSSE(writer, request)
	if writer.status != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %q", writer.status, writer.header.Get("Content-Type"))
	}
	if len(server.sessions) != 0 {
		t.Fatalf("session leaked: %+v", server.sessions)
	}
}

func TestServeSSEWithCompatibilityTools(t *testing.T) {
	server := New(&fakeApps{}, 0)
	server.Compatibility = true
	server.SpeakerProbe = true

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	probe := &endpointProbeWriter{}
	probe.probe = func(endpoint string) {
		if !strings.HasPrefix(endpoint, "/mcp/message?sessionid=") {
			t.Errorf("endpoint = %q", endpoint)
		}
		cancel()
	}
	request := httptest.NewRequest(http.MethodGet, "/mcp", nil).WithContext(ctx)
	request.Header.Set("X-Linx-Device-Id", "device")
	request.Header.Set("X-Linx-Session-Id", "session")
	server.ServeSSE(probe, request)
	if !bytes.Contains(probe.body.Bytes(), []byte("event: endpoint")) {
		t.Fatalf("missing endpoint event: %q", probe.body.String())
	}
	if len(server.sessions) != 0 {
		t.Fatalf("session leaked: %+v", server.sessions)
	}
}

type failingTransport struct{}

func (failingTransport) Connect(context.Context) (sdk.Connection, error) {
	return nil, errors.New("dial failed")
}

func TestConnectionTransportConnectFailure(t *testing.T) {
	transport := &connectionTransport{inner: failingTransport{}}
	if _, err := transport.Connect(context.Background()); err == nil {
		t.Fatal("expected connect failure")
	}
}

func TestServeSSEWithoutSessionHeaders(t *testing.T) {
	server := New(&fakeApps{}, 0)
	request := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	request.Header.Set("X-Linx-Device-Id", "device")
	recorder := httptest.NewRecorder()
	server.ServeSSE(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", recorder.Code)
	}
}
