package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/open-anywhere/open-anywhere/gateway/internal/openapi"
)

func TestMCPHTTP(t *testing.T) {
	for _, probe := range []bool{false, true} {
		t.Run(fmt.Sprint(probe), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/spec" {
					fmt.Fprint(w, `{"openapi":"3.1.0","paths":{"/state":{"get":{"operationId":"read","responses":{"200":{"description":"ok"}}}}}}`)
				} else {
					fmt.Fprint(w, `{"message":"works"}`)
				}
			}))
			defer upstream.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			controller, err := openapi.New(ctx, openapi.Config{Services: []openapi.Service{{ID: "test", OpenAPIURL: upstream.URL + "/spec", BaseURL: upstream.URL}}})
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(Handler(controller, Options{SpeakerProbe: probe, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}))
			defer server.Close()
			client := mcp.NewClient(&mcp.Implementation{Name: "go-test", Version: "1"}, nil)
			session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", DisableStandaloneSSE: true}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			tools, err := session.ListTools(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			want := 2
			if probe {
				want = 3
			}
			if len(tools.Tools) != want {
				t.Fatal(len(tools.Tools))
			}
			call := func(name string, args any) *mcp.CallToolResult {
				t.Helper()
				r, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
				if err != nil {
					t.Fatal(err)
				}
				return r
			}
			doc := call("get_openapi", map[string]any{"service_id": "test"})
			if doc.IsError {
				t.Fatal(doc)
			}
			good := map[string]any{"service_id": "test", "operationId": "read"}
			if call("send_request", good).IsError {
				t.Fatal("operation failed")
			}
			good["url"] = "http://evil.test"
			if !call("send_request", good).IsError {
				t.Fatal("unknown field accepted")
			}
			if probe {
				r := call("probe_speaker", map[string]any{"speaker_id": nil, "speaker_name": nil, "context_source": "unavailable"})
				if r.IsError {
					t.Fatal(r)
				}
				b, _ := json.Marshal(r.StructuredContent)
				var value map[string]any
				_ = json.Unmarshal(b, &value)
				if value["verified"] != false {
					t.Fatal("false authentication claim")
				}
				if !call("probe_speaker", map[string]any{"speaker_name": "name"}).IsError {
					t.Fatal("missing field accepted")
				}
			}
			proxied := Handler(controller, Options{PublicHost: "gateway.example:6004"})
			for _, tc := range []struct {
				host, origin string
				forbidden    bool
			}{
				{"evil.example", "", true}, {"gateway.example:6004", "http://evil.example", true},
				{"gateway.example:6004", "", false}, {"localhost:8787", "http://localhost:8787", false},
			} {
				req := httptest.NewRequest("GET", "/mcp", nil)
				req.Host = tc.host
				if tc.origin != "" {
					req.Header.Set("Origin", tc.origin)
				}
				w := httptest.NewRecorder()
				proxied.ServeHTTP(w, req)
				if (w.Code == 403) != tc.forbidden {
					t.Fatalf("host/origin check: %+v status=%d", tc, w.Code)
				}
			}
			// Bearer protection is checked before protocol processing.
			protected := Handler(controller, Options{Token: "test-secret"})
			req := httptest.NewRequest("POST", "/mcp", nil)
			w := httptest.NewRecorder()
			protected.ServeHTTP(w, req)
			if w.Code != 401 {
				t.Fatal(w.Code)
			}
		})
	}
}
