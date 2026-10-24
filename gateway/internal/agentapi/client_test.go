package agentapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/gateway/internal/application"
	"github.com/open-anywhere/open-anywhere/gateway/internal/artifact"
	"github.com/open-anywhere/open-anywhere/gateway/internal/testfixture"
)

func TestBuildSessionSSEAndFiles(t *testing.T) {
	archive := testfixture.Artifact(nil)
	submitted := make(chan struct{})
	var once sync.Once
	var submits, streams, archives atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "test-key" {
			t.Error("API key missing")
		}
		switch r.URL.Path {
		case "/v1/sessions":
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in["metadata"].(map[string]any)["app_id"] != "app-test" {
				t.Error(in)
			}
			fmt.Fprint(w, `{"id":"build-1"}`)
		case "/v1/sessions/build-1/events/stream":
			stream := streams.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: session.status_idle\ndata: {\"id\":\"old\",\"type\":\"session.status_idle\",\"stop_reason\":{\"type\":\"end_turn\"}}\n\n")
			w.(http.Flusher).Flush()
			select {
			case <-submitted:
			case <-r.Context().Done():
				return
			}
			fmt.Fprint(w, "data: {\"id\":\"running\",\"type\":\"session.status_running\"}\n\n")
			if stream == 1 {
				return
			}
			fmt.Fprint(w, "data: {\"id\":\"msg\",\"type\":\"agent.message\",\"content\":[{\"type\":\"text\",\"text\":\" build   done \"}]}\n\n")
			fmt.Fprint(w, "data: {\"id\":\"idle\",\"type\":\"session.status_idle\",\"stop_reason\":{\"type\":\"end_turn\"}}\n\n")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		case "/v1/sessions/build-1/events":
			submits.Add(1)
			b, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(b), "user_name") || !strings.Contains(string(b), "韩鲁鹏") {
				t.Error("prompt lacks player contract")
			}
			once.Do(func() { close(submitted) })
			fmt.Fprint(w, `{}`)
		case "/v1/files":
			if r.URL.Query().Get("scope_id") != "build-1" {
				t.Error("wrong file scope")
			}
			if r.URL.Query().Get("page") == "" {
				fmt.Fprint(w, `{"data":[],"has_more":true,"next_page":"page2"}`)
			} else {
				fmt.Fprintf(w, `{"data":[{"id":"file1","filename":"artifact.tar.gz","size_bytes":%d,"downloadable":true,"created_at":"2026-10-24T01:00:00Z"}],"has_more":false}`, len(archive))
			}
		case "/v1/files/file1/content":
			w.Write(archive)
		case "/v1/sessions/build-1/archive":
			archives.Add(1)
			fmt.Fprint(w, `{}`)
		default:
			t.Errorf("unexpected %s", r.URL)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	client, err := New(Config{BaseURL: srv.URL, AgentID: "agent", AgentVersion: 1, EnvironmentID: "env"}, "test-key", artifact.Store{Dir: t.TempDir(), PublicURL: "http://gateway.test"})
	if err != nil {
		t.Fatal(err)
	}
	client.FileDelays = []time.Duration{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req := application.BuildRequest{AppID: "app-test", Prompt: "quiz", Players: []string{"韩鲁鹏"}}
	id, err := client.Create(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	updates := map[string]int{}
	var message string
	out, err := client.Run(ctx, id, req, false, application.BuildCursor{}, func(submitted bool, e *application.BuildEvent) error {
		if e != nil {
			updates[e.ID]++
			if e.Message != "" {
				message = e.Message
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Name != "test-app" || len(out.Document) == 0 || message != "build done" {
		t.Fatal(out, message)
	}
	if submits.Load() != 1 || streams.Load() != 2 || archives.Load() != 1 || updates["running"] != 1 {
		t.Fatalf("submit=%d stream=%d archive=%d events=%v", submits.Load(), streams.Load(), archives.Load(), updates)
	}
}
func TestStopReasons(t *testing.T) {
	for _, reason := range []string{"requires_action", "interrupted", "budget_reached", "retries_exhausted", ""} {
		t.Run(reason, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/stream") {
					fmt.Fprintf(w, "data: {\"id\":\"x\",\"type\":\"session.status_running\"}\n\ndata: {\"id\":\"i\",\"type\":\"session.status_idle\",\"stop_reason\":{\"type\":%q}}\n\n", reason)
				} else {
					fmt.Fprint(w, `{}`)
				}
			}))
			defer srv.Close()
			c, _ := New(Config{BaseURL: srv.URL, AgentID: "a", AgentVersion: 1, EnvironmentID: "e"}, "key", artifact.Store{Dir: t.TempDir()})
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if _, err := c.Run(ctx, "s", application.BuildRequest{}, false, application.BuildCursor{}, func(bool, *application.BuildEvent) error { return nil }); err == nil {
				t.Fatal("non end_turn accepted")
			}
		})
	}
}

func TestPromptIncludesExactLaunchContract(t *testing.T) {
	text := prompt(application.BuildRequest{Prompt: "counter"})
	for _, field := range []string{`"runtimeExecutable":"node"`, `"runtimeArgs":["app/app.js"]`, `"healthPath":"/health"`, `"readyTimeoutMs":10000`, "禁止用 command/args/cwd"} {
		if !strings.Contains(text, field) {
			t.Fatalf("missing launch constraint %s", field)
		}
	}
}
