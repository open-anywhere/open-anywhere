package agentapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

func TestClientSessionLifecycle(t *testing.T) {
	var (
		mu         sync.Mutex
		eventTypes []string
		archived   bool
	)
	checkKey := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("x-api-key") != "secret" {
			t.Errorf("x-api-key = %q", r.Header.Get("x-api-key"))
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return false
		}
		return true
	}
	writeJSON := func(w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, body)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		if !checkKey(w, r) {
			return
		}
		var body struct {
			Agent struct {
				Type    string `json:"type"`
				ID      string `json:"id"`
				Version int    `json:"version"`
			} `json:"agent"`
			EnvironmentID string            `json:"environment_id"`
			Title         string            `json:"title"`
			Metadata      map[string]string `json:"metadata"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode create session: %v", err)
			return
		}
		if body.Agent.Type != "agent" || body.Agent.ID != "agent_1" || body.Agent.Version != 7 {
			t.Errorf("agent = %+v", body.Agent)
		}
		if body.EnvironmentID != "env_1" || body.Title != "app_1" || body.Metadata["app_id"] != "app_1" {
			t.Errorf("body = %+v", body)
		}
		writeJSON(w, `{"id":"ses_1","status":"idle"}`)
	})
	mux.HandleFunc("POST /v1/sessions/ses_1/events", func(w http.ResponseWriter, r *http.Request) {
		if !checkKey(w, r) {
			return
		}
		var body struct {
			Events []struct {
				Type    string `json:"type"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"events"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode events: %v", err)
			return
		}
		if len(body.Events) != 1 {
			t.Errorf("events = %+v", body.Events)
			return
		}
		if body.Events[0].Type == "user.message" {
			if len(body.Events[0].Content) != 1 || body.Events[0].Content[0].Type != "text" || body.Events[0].Content[0].Text != "build it" {
				t.Errorf("content = %+v", body.Events[0].Content)
			}
		}
		mu.Lock()
		eventTypes = append(eventTypes, body.Events[0].Type)
		mu.Unlock()
		writeJSON(w, `{}`)
	})
	mux.HandleFunc("GET /v1/sessions/ses_1", func(w http.ResponseWriter, r *http.Request) {
		if !checkKey(w, r) {
			return
		}
		writeJSON(w, `{"id":"ses_1","status":"running"}`)
	})
	mux.HandleFunc("POST /v1/sessions/ses_1/archive", func(w http.ResponseWriter, r *http.Request) {
		if !checkKey(w, r) {
			return
		}
		mu.Lock()
		archived = true
		mu.Unlock()
		writeJSON(w, `{"id":"ses_1","status":"terminated"}`)
	})
	mux.HandleFunc("GET /v1/files", func(w http.ResponseWriter, r *http.Request) {
		if !checkKey(w, r) {
			return
		}
		query := r.URL.Query()
		if query.Get("scope_id") != "ses_1" || query.Get("limit") != "100" {
			t.Errorf("files query = %s", r.URL.RawQuery)
		}
		if query.Get("page") == "p2" {
			writeJSON(w, `{"data":[{"id":"f2","filename":"notes.txt","downloadable":true}],"next_page":null}`)
			return
		}
		writeJSON(w, `{"data":[{"id":"f1","filename":"artifact.tar.gz","downloadable":true}],"next_page":"p2"}`)
	})
	mux.HandleFunc("GET /v1/files/f1/content", func(w http.ResponseWriter, r *http.Request) {
		if !checkKey(w, r) {
			return
		}
		io.WriteString(w, "artifact-bytes")
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	ctx := context.Background()
	client := New(server.URL, "secret", 2*time.Second)

	session, err := client.CreateSession(ctx, "agent_1", 7, "env_1", "app_1", map[string]string{"app_id": "app_1"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session.ID != "ses_1" || session.Status != "idle" {
		t.Fatalf("session = %+v", session)
	}
	if err := client.SubmitMessage(ctx, "ses_1", "build it"); err != nil {
		t.Fatalf("SubmitMessage: %v", err)
	}
	if err := client.Interrupt(ctx, "ses_1"); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}
	snapshot, err := client.GetSession(ctx, "ses_1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if snapshot.Status != "running" {
		t.Fatalf("session = %+v", snapshot)
	}
	if err := client.ArchiveSession(ctx, "ses_1"); err != nil {
		t.Fatalf("ArchiveSession: %v", err)
	}
	files, err := client.ListFiles(ctx, "ses_1")
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(files) != 2 || files[0].ID != "f1" || files[1].ID != "f2" {
		t.Fatalf("files = %+v", files)
	}
	var downloaded bytes.Buffer
	size, err := client.DownloadFile(ctx, "f1", &downloaded, 100)
	if err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}
	if size != int64(len("artifact-bytes")) || downloaded.String() != "artifact-bytes" {
		t.Fatalf("download = %d %q", size, downloaded.String())
	}
	if _, err := client.DownloadFile(ctx, "f1", io.Discard, 3); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("limit error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(eventTypes) != 2 || eventTypes[0] != "user.message" || eventTypes[1] != "user.interrupt" {
		t.Fatalf("event types = %+v", eventTypes)
	}
	if !archived {
		t.Fatal("session was not archived")
	}
}

func TestCreateSessionWithoutVersionOmitsField(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Agent struct {
				Version int `json:"version"`
			} `json:"agent"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode create session: %v", err)
			return
		}
		if body.Agent.Version != 0 {
			t.Errorf("agent.version = %d, want field omitted", body.Agent.Version)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"ses_2","status":"idle"}`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := New(server.URL, "secret", 2*time.Second)
	session, err := client.CreateSession(context.Background(), "agent_1", 0, "env_1", "", nil)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session.ID != "ses_2" {
		t.Fatalf("session = %+v", session)
	}
}

func TestEventStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/sessions/ses_1/events/stream":
			if r.Header.Get("Accept") != "text/event-stream" {
				t.Errorf("accept = %q", r.Header.Get("Accept"))
			}
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, ": keepalive\n\n")
			io.WriteString(w, "event: message\ndata: {\"id\":\"evt_1\",\"type\":\"agent.message\",\"content\":[{\"type\":\"text\",\"text\":\"hel\"},{\"type\":\"tool_use\",\"text\":\"x\"},{\"type\":\"text\",\"text\":\"lo\"}]}\n\n")
			io.WriteString(w, "event: message\ndata: {\"id\":\"evt_2\",\"type\":\"session.status_idle\",\"stop_reason\":{\"type\":\"end_turn\"}}\n\n")
			io.WriteString(w, "event: message\ndata: {\"id\":\"evt_3\",\"type\":\"session.error\",\"error\":{\"message\":\"details\"}}\n\n")
		case "/v1/sessions/bad/events/stream":
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, `{"error":{"type":"permission_error","message":"no access"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := New(server.URL, "secret", time.Second)
	stream, err := client.OpenEventStream(context.Background(), "ses_1")
	if err != nil {
		t.Fatalf("OpenEventStream: %v", err)
	}
	defer stream.Close()

	first, err := stream.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if first.ID != "evt_1" || first.Type != "agent.message" || first.Text() != "hello" {
		t.Fatalf("event = %+v", first)
	}
	second, err := stream.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if second.StopReason == nil || second.StopReason.Type != "end_turn" {
		t.Fatalf("event = %+v", second)
	}
	third, err := stream.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if third.MessageText() != "details" {
		t.Fatalf("message = %q", third.MessageText())
	}
	if _, err := stream.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("final error = %v", err)
	}

	bad, err := client.OpenEventStream(context.Background(), "bad")
	if err != nil {
		t.Fatalf("OpenEventStream: %v", err)
	}
	defer bad.Close()
	_, err = bad.Next()
	var apiErr *anthropic.Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusForbidden {
		t.Fatalf("error = %#v", err)
	}
}

func TestErrorStatus(t *testing.T) {
	for name, status := range map[string]int{
		"unauthorized": http.StatusUnauthorized,
		"conflict":     http.StatusConflict,
		"server":       http.StatusInternalServerError,
		"missing":      http.StatusNotFound,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				io.WriteString(w, `{"type":"error","error":{"type":"api_error","message":"boom"}}`)
			}))
			defer server.Close()

			_, err := New(server.URL, "", time.Second).GetSession(context.Background(), "missing")
			var apiErr *anthropic.Error
			if !errors.As(err, &apiErr) {
				t.Fatalf("error = %#v", err)
			}
			if apiErr.StatusCode != status {
				t.Fatalf("status = %d, want %d", apiErr.StatusCode, status)
			}
		})
	}
}
