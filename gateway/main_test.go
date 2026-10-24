package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/gateway/internal/openapi"
)

func TestStartupAndCancellation(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"openapi":"3.1.0","paths":{}}`)) }))
	defer upstream.Close()
	config := openapi.Config{Services: []openapi.Service{{ID: "test", OpenAPIURL: upstream.URL, BaseURL: upstream.URL}}}
	path := filepath.Join(t.TempDir(), "services.json")
	b, _ := json.Marshal(config)
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- run(ctx, path, "127.0.0.1:0", false) }()
	// Cancellation must exit even if it arrives during OpenAPI loading.
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown hung")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := run(context.Background(), path, listener.Addr().String(), false); err == nil {
		t.Fatal("occupied port accepted")
	}
}
