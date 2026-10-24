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

func TestServeAlreadyCanceledDoesNotRefresh(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Serve(ctx, listener, NewHandler(sEmpty()), time.Second, func(context.Context) { t.Error("refresh ran after cancellation") }); err != nil {
		t.Fatal(err)
	}
}

func TestServeForcesCloseWhenGracefulShutdownTimesOut(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	handlerDone := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-r.Context().Done():
		case <-release:
		}
		close(handlerDone)
	})
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, listener, handler, time.Second, nil) }()
	clientDone := make(chan error, 1)
	go func() {
		resp, err := (&http.Client{Timeout: 10 * time.Second}).Get("http://" + listener.Addr().String())
		if err == nil {
			resp.Body.Close()
		}
		clientDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not start")
	}
	cancel()
	// The handler stays blocked until Server.Close cancels its request, so this
	// exercises the real five-second graceful-shutdown deadline and fallback.
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("forced shutdown hung")
	}
	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("forced close did not cancel in-flight request")
	}
	select {
	case err := <-clientDone:
		if err == nil {
			t.Fatal("blocked request unexpectedly completed normally")
		}
	case <-time.After(time.Second):
		t.Fatal("client connection was not closed")
	}
}
