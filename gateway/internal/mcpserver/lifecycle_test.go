package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/open-anywhere/open-anywhere/gateway/internal/application"
	"github.com/open-anywhere/open-anywhere/gateway/internal/downstream"
)

type completedBuilder struct{ document json.RawMessage }

func (b completedBuilder) Create(context.Context, application.BuildRequest) (string, error) {
	return "session", nil
}
func (b completedBuilder) Run(ctx context.Context, id string, r application.BuildRequest, _ bool, _ application.BuildCursor, save func(bool, *application.BuildEvent) error) (application.BuildProduct, error) {
	if len(r.Players) != 2 || r.Players[0] != "韩鲁鹏" {
		return application.BuildProduct{}, fmt.Errorf("player contract mismatch")
	}
	if err := save(true, &application.BuildEvent{ID: "done", Type: "agent.message", Message: "built"}); err != nil {
		return application.BuildProduct{}, err
	}
	return application.BuildProduct{Name: "quiz", URL: "https://artifact.test/quiz.tar.gz", Document: b.document}, nil
}

type headersTransport struct {
	base  http.RoundTripper
	owner application.Owner
}

func (h headersTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("X-Linx-Device-Id", h.owner.Device)
	r.Header.Set("X-Linx-Agent-Id", h.owner.Agent)
	r.Header.Set("X-Linx-Session-Id", h.owner.Session)
	return h.base.RoundTrip(r)
}
func TestManagedMCPToRenderCastingAndBusinessHTTP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var mu sync.Mutex
	var calls []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["user_name"] != "韩鲁鹏" {
			t.Error("name not forwarded")
		}
		fmt.Fprint(w, `{"message":"回答已接收","user_name":"韩鲁鹏"}`)
	}))
	defer upstream.Close()
	render := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" render "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/ports/39100":
			fmt.Fprint(w, `{"occupied":false}`)
		case "POST /v1/apps":
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			fmt.Fprintf(w, `{"app_id":%q,"instance_id":"instance1","state":"ready","port":39100,"app_base_url":%q,"source":{"type":"stream","url":"http://media/live.ts","format":"mpegts","video_codec":"h264","audio_codec":""}}`, in["app_id"], upstream.URL)
		case "GET /v1/apps/instance1":
			fmt.Fprint(w, `{"state":"ready"}`)
		case "DELETE /v1/apps/instance1":
			fmt.Fprint(w, `{"state":"stopped"}`)
		default:
			t.Errorf("unexpected render request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer render.Close()
	display := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" casting "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/targets":
			fmt.Fprint(w, `{"targets":[{"id":"tv","name":"TV","state":"online","endpoints":[{"id":"dlna","capabilities":{"formats":["mpegts"],"video_codecs":["h264"],"streaming":true}}]}]}`)
		case "POST /v1/projection-sessions":
			fmt.Fprint(w, `{"session_id":"projection1","state":"starting"}`)
		case "GET /v1/projection-sessions/projection1":
			fmt.Fprint(w, `{"session_id":"projection1","state":"playing"}`)
		case "DELETE /v1/projection-sessions/projection1":
			fmt.Fprint(w, `{"session_id":"projection1","state":"stopped"}`)
		default:
			t.Errorf("unexpected casting request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer display.Close()
	rc, _ := downstream.New(render.URL, "")
	dc, _ := downstream.New(display.URL, "")
	doc := json.RawMessage(`{"openapi":"3.1.0","paths":{"/api/answer":{"post":{"operationId":"answer","requestBody":{"required":true,"content":{"application/json":{"schema":{"type":"object","required":["user_name"],"properties":{"user_name":{"type":"string"}}}}}},"responses":{"200":{"description":"ok"}}}}}}`)
	manager, err := application.New(ctx, application.Options{StateFile: filepath.Join(t.TempDir(), "apps.json"), Builder: completedBuilder{doc}, Runtime: downstream.Render{Client: rc}, Display: downstream.Casting{Client: dc}, Screens: []application.Screen{{ID: "living-room", TargetID: "tv", EndpointID: "dlna"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	server := httptest.NewServer(Handler(nil, Options{Applications: manager, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}))
	defer server.Close()
	connect := func(owner application.Owner) *mcp.ClientSession {
		t.Helper()
		client := mcp.NewClient(&mcp.Implementation{Name: "managed-test", Version: "1"}, nil)
		s, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", DisableStandaloneSSE: true, HTTPClient: &http.Client{Transport: headersTransport{http.DefaultTransport, owner}}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		return s
	}
	session := connect(application.Owner{Device: "d", Agent: "a", Session: "s"})
	tools, err := session.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 7 {
		t.Fatal(tools, err)
	}
	invoke := func(s *mcp.ClientSession, name string, args any) (map[string]any, bool) {
		t.Helper()
		r, err := s.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(r.StructuredContent)
		var out map[string]any
		_ = json.Unmarshal(b, &out)
		return out, r.IsError
	}
	out, failed := invoke(session, "create-app", map[string]any{"prompt": "quiz", "player_names": []string{"韩鲁鹏", "张之阳"}})
	if failed {
		t.Fatal(out)
	}
	id := out["app_id"].(string)
	for i := 0; i < 100; i++ {
		out, failed = invoke(session, "get-app-status", map[string]any{"app_id": id})
		if failed {
			t.Fatal(out)
		}
		if out["state"] == "idle" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if out["state"] != "idle" {
		t.Fatal(out)
	}
	other := connect(application.Owner{Device: "other", Agent: "a", Session: "s"})
	if _, failed = invoke(other, "get_openapi", map[string]any{"app_id": id}); !failed {
		t.Fatal("cross-owner read accepted")
	}
	if _, failed = invoke(session, "send_request", map[string]any{"app_id": id, "operationId": "answer"}); !failed {
		t.Fatal("idle dispatch accepted")
	}
	if _, failed = invoke(session, "get-screens", map[string]any{}); failed {
		t.Fatal("screens failed")
	}
	out, failed = invoke(session, "start-app", map[string]any{"app_id": id, "screen_id": "living-room"})
	if failed || out["state"] != "running" {
		t.Fatal(out)
	}
	if _, failed = invoke(session, "get_openapi", map[string]any{"app_id": id}); failed {
		t.Fatal("document unavailable")
	}
	out, failed = invoke(session, "send_request", map[string]any{"app_id": id, "operationId": "answer", "requestBody": map[string]any{"user_name": "韩鲁鹏"}})
	if failed || out["status"] != float64(200) {
		t.Fatal(out)
	}
	out, failed = invoke(session, "stop-app", map[string]any{"app_id": id})
	if failed || out["state"] != "idle" {
		t.Fatal(out)
	}
	mu.Lock()
	defer mu.Unlock()
	n := len(calls)
	if n < 2 || calls[n-2] != "DELETE casting /v1/projection-sessions/projection1" || calls[n-1] != "DELETE render /v1/apps/instance1" {
		t.Fatal(calls)
	}
}
