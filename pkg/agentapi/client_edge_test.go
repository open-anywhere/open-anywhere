package agentapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCreateSessionPropagatesAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"api_error","message":"boom"}}`)
	}))
	defer server.Close()

	client := New(server.URL, "key", time.Second)
	if _, err := client.CreateSession(context.Background(), "agent_1", 0, "env_1", "title", nil); err == nil {
		t.Fatal("CreateSession should surface the API error")
	}
}

type failingDst struct{}

func (failingDst) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestDownloadFilePropagatesWriterError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "some bytes")
	}))
	defer server.Close()

	client := New(server.URL, "key", time.Second)
	written, err := client.DownloadFile(context.Background(), "file_1", failingDst{}, 100)
	if err == nil || written != 0 {
		t.Fatalf("written = %d, err = %v", written, err)
	}
}

func TestDownloadFileZeroLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "x")
	}))
	defer server.Close()

	client := New(server.URL, "key", time.Second)
	if _, err := client.DownloadFile(context.Background(), "file_1", io.Discard, 0); err == nil {
		t.Fatal("a single byte exceeds a zero limit")
	}
}
