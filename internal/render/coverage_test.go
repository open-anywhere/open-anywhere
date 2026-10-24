package render

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/pkg/casting"
	api "github.com/open-anywhere/open-anywhere/pkg/render"
)

func TestHubStalledAndClosedSubscribers(t *testing.T) {
	h := newHub()
	if h.stalled() {
		t.Fatal("a hub with no frames must not be stalled")
	}
	ch, unsubscribe := h.subscribe()
	h.mu.Lock()
	h.last = time.Now().Add(-6 * time.Second)
	h.mu.Unlock()
	if !h.stalled() {
		t.Fatal("an old frame must be considered stalled")
	}
	h.mu.Lock()
	h.last = time.Now()
	h.mu.Unlock()
	if h.stalled() {
		t.Fatal("a recent frame must not be considered stalled")
	}
	unsubscribe()
	h.close()
	if _, ok := <-ch; ok {
		t.Fatal("unsubscribed channel should be closed")
	}
	closed, remove := h.subscribe()
	defer remove()
	if _, ok := <-closed; ok {
		t.Fatal("subscriber added after close should be closed")
	}
	h.publish([]byte("ignored"))
}

func TestStopProcessHandlesNilAndProcessGroup(t *testing.T) {
	if err := stopProcess(nil, nil); err != nil {
		t.Fatalf("nil process: %v", err)
	}
	cmd := exec.Command("sh", "-c", "sleep 30")
	ownProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	if err := stopProcess(cmd, done); err != nil {
		t.Fatalf("stop process group: %v", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("process did not exit")
	}

	finished := exec.Command("true")
	ownProcessGroup(finished)
	if err := finished.Run(); err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{})
	close(closed)
	if err := stopProcess(finished, closed); err != nil {
		t.Fatalf("already exited process: %v", err)
	}

	ignoreTerm := exec.Command("sh", "-c", "trap '' TERM; exec sleep 30")
	ownProcessGroup(ignoreTerm)
	if err := ignoreTerm.Start(); err != nil {
		t.Fatal(err)
	}
	ignoreDone := make(chan struct{})
	go func() {
		_ = ignoreTerm.Wait()
		close(ignoreDone)
	}()
	if err := stopProcess(ignoreTerm, ignoreDone); err != nil {
		t.Fatalf("force stop process group: %v", err)
	}
}

func TestHealthAndWaitHealthBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      int
		contentType string
		body        string
		want        bool
	}{
		{name: "ok", status: http.StatusOK, contentType: "application/json", body: `{"status":"ok"}`, want: true},
		{name: "wrong status", status: http.StatusBadGateway, contentType: "application/json", body: `{"status":"ok"}`},
		{name: "wrong status value", status: http.StatusOK, contentType: "application/json", body: `{"status":"bad"}`},
		{name: "invalid json", status: http.StatusOK, contentType: "application/json", body: `{`},
		{name: "redirect", status: http.StatusFound, contentType: "text/html", body: "redirect"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.name == "redirect" {
					w.Header().Set("Location", "/health")
				}
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			if got := healthy(context.Background(), server.URL); got != tc.want {
				t.Fatalf("healthy=%v want %v", got, tc.want)
			}
		})
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()
	port := server.Listener.Addr().(*net.TCPAddr).Port
	done := make(chan struct{})
	if err := waitHealth(context.Background(), server.URL, os.Getpid(), port, done); err != nil {
		t.Fatalf("waitHealth success: %v", err)
	}
	closed := make(chan struct{})
	close(closed)
	if err := waitHealth(context.Background(), server.URL, os.Getpid(), port, closed); err == nil || !strings.Contains(err.Error(), "exited") {
		t.Fatalf("waitHealth done: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitHealth(ctx, server.URL, os.Getpid(), port, make(chan struct{})); !errors.Is(err, context.Canceled) {
		t.Fatalf("waitHealth cancellation: %v", err)
	}
}

func TestStopLifecycleBranches(t *testing.T) {
	s := testService(t)
	cleaned := &run{info: api.Instance{AppID: "clean", InstanceID: "clean-id", Port: 40101}, cleaned: true, done: make(chan struct{})}
	s.instances[cleaned.info.InstanceID] = cleaned
	if got, err := s.Stop(context.Background(), cleaned.info.InstanceID); err != nil || got.State != "stopped" {
		t.Fatalf("cleaned stop: %+v %v", got, err)
	}

	var calls atomic.Int32
	retry := &run{info: api.Instance{AppID: "retry", InstanceID: "retry-id", Port: 40102}, done: make(chan struct{}), cancel: func() {}, cleanup: func() error {
		if calls.Add(1) == 1 {
			return errors.New("cleanup failed")
		}
		return nil
	}}
	close(retry.done)
	s.instances[retry.info.InstanceID] = retry
	s.apps[retry.info.AppID] = retry.info.InstanceID
	s.ports[retry.info.Port] = retry.info.InstanceID
	if _, err := s.Stop(context.Background(), retry.info.InstanceID); err == nil || err.Detail.Code != "STOP_FAILED" {
		t.Fatalf("expected cleanup failure, got %v", err)
	}
	if _, err := s.Stop(context.Background(), retry.info.InstanceID); err != nil {
		t.Fatalf("cleanup retry: %v", err)
	}
	if _, ok := s.apps[retry.info.AppID]; ok {
		t.Fatal("successful cleanup retained app reservation")
	}

	timed := &run{info: api.Instance{AppID: "timed", InstanceID: "timed-id", Port: 40103}, done: make(chan struct{}), cancel: func() {}}
	s.instances[timed.info.InstanceID] = timed
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Stop(ctx, timed.info.InstanceID); err == nil || err.Detail.Code != "STOP_FAILED" {
		t.Fatalf("expected stop timeout, got %v", err)
	}
	// Remove the intentionally blocked run before testService cleanup.
	delete(s.instances, timed.info.InstanceID)
}

func TestStreamHandlerAvailabilityAndCancellation(t *testing.T) {
	s := testService(t)
	h := NewHandler(s)
	missing := httptest.NewRecorder()
	h.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/streams/missing/live.ts", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing stream status %d", missing.Code)
	}

	run := &run{info: api.Instance{InstanceID: "stream-id", State: "ready"}, hub: newHub(), cancel: func() {}}
	s.instances[run.info.InstanceID] = run
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/streams/stream-id/live.ts", nil).WithContext(ctx)
	writer := &streamCapture{header: make(http.Header), writes: make(chan []byte, 1)}
	done := make(chan struct{})
	go func() {
		h.ServeHTTP(writer, req)
		close(done)
	}()
	received := false
	for i := 0; i < 100 && !received; i++ {
		run.hub.publish([]byte("mpegts-packet"))
		select {
		case <-writer.writes:
			received = true
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	if !received {
		t.Fatal("stream did not receive a packet")
	}
	cancel()
	run.hub.close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stream handler did not stop after request cancellation")
	}
	if got := writer.Header().Get("Content-Type"); got != "video/mp2t" {
		t.Fatalf("content type %q", got)
	}
	delete(s.instances, run.info.InstanceID)
}

type streamCapture struct {
	header http.Header
	writes chan []byte
}

func (w *streamCapture) Header() http.Header { return w.header }
func (w *streamCapture) Write(p []byte) (int, error) {
	w.writes <- append([]byte(nil), p...)
	return len(p), nil
}
func (w *streamCapture) WriteHeader(int) {}
func (w *streamCapture) Flush()          {}

func TestSnapshotCopiesNestedFieldsAndEnvironment(t *testing.T) {
	source := &casting.StreamSource{URL: "http://example/stream"}
	errorValue := &api.Error{Code: "E", Message: "failed"}
	r := &run{info: api.Instance{Source: source, Error: errorValue}}
	v := snapshot(r)
	v.Source.URL = "changed"
	v.Error.Message = "changed"
	if source.URL != "http://example/stream" || errorValue.Message != "failed" {
		t.Fatal("snapshot did not copy nested values")
	}
	env := appEnvironment(41234)
	joined, _ := json.Marshal(env)
	if !strings.Contains(string(joined), "PORT=41234") || !strings.Contains(string(joined), "NODE_ENV=production") {
		t.Fatalf("missing runtime environment: %s", joined)
	}
}
