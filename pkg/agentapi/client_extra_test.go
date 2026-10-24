package agentapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewDefaults(t *testing.T) {
	client := New("http://agent", "key", 0)
	if client.timeout != 30*time.Second {
		t.Fatalf("timeout = %v", client.timeout)
	}
	empty := &Client{}
	if len(empty.options()) != 0 {
		t.Fatalf("options = %+v", empty.options())
	}
}

func TestEventHelpers(t *testing.T) {
	event := Event{Content: []Content{{Type: "text", Text: "a"}, {Type: "tool_use", Text: "b"}, {Type: "text", Text: "c"}}}
	if event.Text() != "ac" {
		t.Fatalf("text = %q", event.Text())
	}
	if event.MessageText() != "" {
		t.Fatalf("message = %q", event.MessageText())
	}
	if got := (Event{Error: &EventError{Message: "details"}}).MessageText(); got != "details" {
		t.Fatalf("message = %q", got)
	}
	if got := (Event{Message: "top"}).MessageText(); got != "top" {
		t.Fatalf("message = %q", got)
	}
	if session := convertSession(nil); session.ID != "" || session.Status != "" {
		t.Fatalf("session = %+v", session)
	}
}

func TestListAndDownloadErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, `{"type":"error","error":{"type":"api_error","message":"boom"}}`)
	}))
	defer server.Close()
	client := New(server.URL, "key", time.Second)
	if _, err := client.ListFiles(context.Background(), "ses_1"); err == nil {
		t.Fatal("list should fail")
	}
	if _, err := client.DownloadFile(context.Background(), "file_1", io.Discard, 100); err == nil {
		t.Fatal("download should fail")
	}
}

func TestStreamErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"bad"}}`)
	}))
	defer server.Close()
	stream := New(server.URL, "key", time.Second).OpenEventStream(context.Background(), "ses_1")
	defer stream.Close()
	if _, err := stream.Next(); err == nil {
		t.Fatal("stream should fail")
	}
}
