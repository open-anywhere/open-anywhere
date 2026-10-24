package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// closeFailingListener makes http.Server.Shutdown return an error.
type closeFailingListener struct {
	net.Listener
	accepted chan struct{}
	once     sync.Once
}

func (l *closeFailingListener) Accept() (net.Conn, error) {
	l.once.Do(func() { close(l.accepted) })
	return l.Listener.Accept()
}

func (l *closeFailingListener) Close() error {
	_ = l.Listener.Close()
	return errors.New("listener close failure")
}

func TestFinishNormalizesServerClosed(t *testing.T) {
	if err := finish(&http.Server{}, http.ErrServerClosed, func(context.Context) error { return nil }); err != nil {
		t.Fatalf("ErrServerClosed should be normalized: %v", err)
	}
}

func TestFinishReturnsServeError(t *testing.T) {
	want := errors.New("serve exploded")
	if err := finish(&http.Server{}, want, func(context.Context) error { return nil }); !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

func TestFinishReportsCleanupFailure(t *testing.T) {
	cleanupErr := errors.New("node would not stop")
	err := finish(&http.Server{}, http.ErrServerClosed, func(context.Context) error { return cleanupErr })
	if err == nil || !strings.Contains(err.Error(), "cleanup:") || !errors.Is(err, cleanupErr) {
		t.Fatalf("error = %v", err)
	}

	// A cleanup failure wins over the serve outcome.
	err = finish(&http.Server{}, errors.New("serve exploded"), func(context.Context) error { return cleanupErr })
	if err == nil || !errors.Is(err, cleanupErr) {
		t.Fatalf("error = %v", err)
	}
}

func TestFinishSurvivesShutdownFailure(t *testing.T) {
	base, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener := &closeFailingListener{Listener: base, accepted: make(chan struct{})}
	server := &http.Server{Handler: http.NotFoundHandler()}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = server.Serve(listener)
	}()
	select {
	case <-listener.accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("server never accepted")
	}
	err = finish(server, nil, func(context.Context) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "listener close failure") {
		t.Fatalf("error = %v", err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop")
	}
}

func TestRunRejectsRequiredFlagsAndPositionalArgs(t *testing.T) {
	cases := [][]string{
		{"-public-url", "http://127.0.0.1:8090", "positional"},
		{"-public-url", ""},
		{"-public-url", "http://127.0.0.1:8090", "-addr", "not-an-address"},
		{"-public-url", "http://127.0.0.1:8090", "-work-dir", ".", "-width", "321"},
		{"-public-url", "http://127.0.0.1:8090", "-fps", "1000"},
		{"-public-url", "ftp://127.0.0.1", "-work-dir", "."},
	}
	for _, args := range cases {
		if err := run(context.Background(), args, discard{}); err == nil {
			t.Fatalf("invalid arguments accepted: %v", args)
		}
	}
}

// discard avoids importing io just for a sink.
type discard struct{}

func (discard) Write(data []byte) (int, error) { return len(data), nil }

func TestRunAlreadyCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	address := freeLoopbackAddr(t)
	if err := run(ctx, []string{
		"-addr", address,
		"-public-url", "http://" + address,
		"-work-dir", t.TempDir(),
	}, discard{}); err != nil {
		t.Fatalf("run: %v", err)
	}
	// The listener must have been released.
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		t.Fatalf("listener retained: %v", err)
	}
	_ = listener.Close()
}

func freeLoopbackAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	return address
}
