package casting

import (
	"context"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestServeLifecycle(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	refreshed := make(chan struct{}, 4)
	var calls atomic.Int32
	refresh := func(ctx context.Context) {
		calls.Add(1)
		select {
		case refreshed <- struct{}{}:
		default:
		}
	}
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, listener, NewHandler(sEmpty()), 5*time.Millisecond, refresh) }()
	for range 2 {
		select {
		case <-refreshed:
		case <-time.After(time.Second):
			t.Fatal("refresh did not run immediately and periodically")
		}
	}
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Get("http://" + listener.Addr().String() + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown hung")
	}
	after := calls.Load()
	if _, err := client.Get("http://" + listener.Addr().String() + "/healthz"); err == nil {
		t.Fatal("listener still accepting after shutdown")
	}
	if calls.Load() != after {
		t.Fatal("refresh continued after shutdown")
	}
}
func TestServeCancelsRefreshOnListenerFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener.Close()
	err = Serve(context.Background(), listener, NewHandler(sEmpty()), time.Second, func(ctx context.Context) { <-ctx.Done() })
	if err == nil {
		t.Fatal("closed listener succeeded")
	}
}
func TestServeRejectsInvalidInterval(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := Serve(context.Background(), listener, NewHandler(sEmpty()), 0, nil); err != ErrInvalid {
		t.Fatal(err)
	}
}
