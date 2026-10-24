package mocktv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// scriptedWriter is an http.ResponseWriter that can fail after a number of
// successful writes. It implements http.Flusher unless flushable is false.
type scriptedWriter struct {
	mu        sync.Mutex
	header    http.Header
	buf       bytes.Buffer
	failAfter int // -1 means never fail
	writes    int
	flushes   int
	wrote     chan struct{}
}

func newScriptedWriter(failAfter int) *scriptedWriter {
	return &scriptedWriter{header: make(http.Header), failAfter: failAfter, wrote: make(chan struct{}, 64)}
}

func (w *scriptedWriter) Header() http.Header { return w.header }

func (w *scriptedWriter) WriteHeader(int) {}

func (w *scriptedWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failAfter >= 0 && w.writes >= w.failAfter {
		return 0, errors.New("simulated write failure")
	}
	w.writes++
	w.buf.Write(data)
	select {
	case w.wrote <- struct{}{}:
	default:
	}
	return len(data), nil
}

func (w *scriptedWriter) Flush() {
	w.mu.Lock()
	w.flushes++
	w.mu.Unlock()
}

func (w *scriptedWriter) snapshot() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func (w *scriptedWriter) writeCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writes
}

func (w *scriptedWriter) flushCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.flushes
}

// plainWriter has no Flush method, like a server without streaming support.
type plainWriter struct {
	header http.Header
	buf    bytes.Buffer
	status int
}

func (w *plainWriter) Header() http.Header            { return w.header }
func (w *plainWriter) WriteHeader(status int)         { w.status = status }
func (w *plainWriter) Write(data []byte) (int, error) { return w.buf.Write(data) }

func TestNewDisplayDefaultsAndBlankCue(t *testing.T) {
	display := NewDisplay(Config{})
	if got := display.State(); got.Name != "Mock TV" || got.State != "idle" {
		t.Fatalf("defaults = %+v", got)
	}
	// The default logger must be installed, not left nil.
	display.logf("mocktv test: default logger")
	display.cue("   ")
	display.cue("\t\n")
	if got := display.State(); got.URL != "" || got.State != "idle" {
		t.Fatalf("blank cue changed state: %+v", got)
	}
}

func TestHealthz(t *testing.T) {
	display := NewDisplay(Config{Logf: t.Logf})
	server := httptest.NewServer(display.Handler())
	defer server.Close()

	response, err := http.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" {
		t.Fatalf("body = %v", body)
	}
}

func TestUnknownPathIsNotFound(t *testing.T) {
	display := NewDisplay(Config{Logf: t.Logf})
	request := httptest.NewRequest(http.MethodGet, "/definitely/not/here", nil)
	recorder := httptest.NewRecorder()
	display.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}

func TestEventsRequiresFlusher(t *testing.T) {
	display := NewDisplay(Config{Logf: t.Logf})
	writer := &plainWriter{header: make(http.Header)}
	display.Handler().ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/events", nil))
	if writer.status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", writer.status)
	}
	if !strings.Contains(writer.buf.String(), "streaming unsupported") {
		t.Fatalf("body = %q", writer.buf.String())
	}
}

func TestEventsInitialWriteFailure(t *testing.T) {
	display := NewDisplay(Config{Logf: t.Logf})
	writer := newScriptedWriter(0)
	display.Handler().ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/events", nil))
	if writer.writeCount() != 0 {
		t.Fatalf("expected the initial state write to fail, got %d writes", writer.writeCount())
	}
	display.mu.Lock()
	clients := len(display.clients)
	display.mu.Unlock()
	if clients != 0 {
		t.Fatalf("failed event stream leaked %d clients", clients)
	}
}

func TestEventsKeepAlive(t *testing.T) {
	previous := eventKeepAliveInterval
	eventKeepAliveInterval = 5 * time.Millisecond
	t.Cleanup(func() { eventKeepAliveInterval = previous })

	display := NewDisplay(Config{Logf: t.Logf})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writer := newScriptedWriter(-1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		display.Handler().ServeHTTP(writer, httptest.NewRequestWithContext(ctx, http.MethodGet, "/events", nil))
	}()

	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(writer.snapshot(), ": keepalive") {
		if time.Now().After(deadline) {
			t.Fatalf("no keepalive; got %q", writer.snapshot())
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("event handler did not stop after cancellation")
	}
	if writer.flushCount() == 0 {
		t.Fatal("keepalive was not flushed")
	}
	display.mu.Lock()
	defer display.mu.Unlock()
	if len(display.clients) != 0 {
		t.Fatalf("event stream did not unregister: %d clients", len(display.clients))
	}
}

func TestEventsKeepAliveWriteFailureStopsStream(t *testing.T) {
	previous := eventKeepAliveInterval
	eventKeepAliveInterval = 5 * time.Millisecond
	t.Cleanup(func() { eventKeepAliveInterval = previous })

	display := NewDisplay(Config{Logf: t.Logf})
	writer := newScriptedWriter(1) // initial state succeeds, keepalive fails
	done := make(chan struct{})
	go func() {
		defer close(done)
		display.Handler().ServeHTTP(writer, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/events", nil))
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("event handler did not stop after keepalive write failure")
	}
}

func TestEventsStateWriteFailureStopsStream(t *testing.T) {
	display := NewDisplay(Config{Logf: t.Logf})
	writer := newScriptedWriter(1) // initial state succeeds, the update fails
	done := make(chan struct{})
	go func() {
		defer close(done)
		display.Handler().ServeHTTP(writer, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/events", nil))
	}()
	select {
	case <-writer.wrote:
	case <-time.After(3 * time.Second):
		t.Fatal("initial state was not written")
	}
	display.cue("http://127.0.0.1:9/live.ts")
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("event handler did not stop after state write failure")
	}
}

func TestBroadcastDropsWhenSubscriberIsFull(t *testing.T) {
	display := NewDisplay(Config{Logf: t.Logf})
	full := make(chan State, 1)
	full <- State{State: "stale"}
	display.mu.Lock()
	display.clients[full] = struct{}{}
	display.mu.Unlock()

	display.cue("http://127.0.0.1:9/live.ts")

	select {
	case state := <-full:
		if state.State != "stale" {
			t.Fatalf("full subscriber was overwritten: %+v", state)
		}
	default:
		t.Fatal("subscriber channel unexpectedly empty")
	}
	display.mu.Lock()
	delete(display.clients, full)
	display.mu.Unlock()
}

func TestStreamRedirectLimits(t *testing.T) {
	t.Run("too many redirects", func(t *testing.T) {
		var server *httptest.Server
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, server.URL+"/live.ts", http.StatusFound)
		}))
		defer server.Close()

		display := NewDisplay(Config{Logf: t.Logf})
		proxy := httptest.NewServer(display.Handler())
		defer proxy.Close()
		response, err := http.Get(proxy.URL + "/stream?url=" + url.QueryEscape(server.URL+"/live.ts"))
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502", response.StatusCode)
		}
	})

	t.Run("redirect leaves local network", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://8.8.8.8/live.ts", http.StatusFound)
		}))
		defer server.Close()

		display := NewDisplay(Config{Logf: t.Logf})
		proxy := httptest.NewServer(display.Handler())
		defer proxy.Close()
		response, err := http.Get(proxy.URL + "/stream?url=" + url.QueryEscape(server.URL+"/live.ts"))
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502", response.StatusCode)
		}
	})
}

func TestStreamUpstreamStatusAndWriteFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer upstream.Close()

	display := NewDisplay(Config{Logf: t.Logf})
	proxy := httptest.NewServer(display.Handler())
	defer proxy.Close()
	response, err := http.Get(proxy.URL + "/stream?url=" + url.QueryEscape(upstream.URL+"/live.ts"))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", response.StatusCode)
	}

	// A client that goes away mid-stream must terminate the handler instead of
	// panicking or spinning.
	payload := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 64*1024)))
	}))
	defer payload.Close()
	request := httptest.NewRequest(http.MethodGet, "/stream?url="+url.QueryEscape(payload.URL+"/live.ts"), nil)
	writer := newScriptedWriter(0)
	display.Handler().ServeHTTP(writer, request)
	if writer.writeCount() != 0 {
		t.Fatalf("expected writes to fail, got %d", writer.writeCount())
	}
}

func TestStreamURLValidation(t *testing.T) {
	original := lookupIP
	t.Cleanup(func() { lookupIP = original })

	display := NewDisplay(Config{Logf: t.Logf})
	proxy := httptest.NewServer(display.Handler())
	defer proxy.Close()
	directCheck := func(raw string, want int) {
		t.Helper()
		response, err := http.Get(proxy.URL + "/stream?url=" + url.QueryEscape(raw))
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != want {
			t.Fatalf("url %q: status = %d, want %d", raw, response.StatusCode, want)
		}
	}

	directCheck("http://", http.StatusBadRequest)
	directCheck("https://[::1", http.StatusBadRequest)
	directCheck("http://8.8.8.8/live.ts", http.StatusForbidden)

	lookupIP = func(string) ([]net.IP, error) { return nil, errors.New("dns failure") }
	directCheck("http://127.0.0.1:1/live.ts", http.StatusForbidden)

	lookupIP = func(string) ([]net.IP, error) { return nil, nil }
	directCheck("http://127.0.0.1:1/live.ts", http.StatusForbidden)

	lookupIP = func(string) ([]net.IP, error) { return []net.IP{net.ParseIP("8.8.8.8")}, nil }
	directCheck("http://127.0.0.1:1/live.ts", http.StatusForbidden)

	lookupIP = func(string) ([]net.IP, error) { return []net.IP{net.ParseIP("100.64.0.1")}, nil }
	directCheck("http://127.0.0.1:1/live.ts", http.StatusBadGateway) // resolves locally, then connection fails
}

func TestCheckStreamURLDirect(t *testing.T) {
	original := lookupIP
	t.Cleanup(func() { lookupIP = original })

	if err := checkStreamURL(nil); err == nil {
		t.Fatal("nil URL accepted")
	}
	if err := checkStreamURL(&url.URL{Scheme: "ftp", Host: "127.0.0.1"}); err == nil {
		t.Fatal("ftp URL accepted")
	}
	if err := checkStreamURL(&url.URL{Scheme: "http"}); err == nil {
		t.Fatal("URL without host accepted")
	}
	lookupIP = func(string) ([]net.IP, error) { return nil, errors.New("dns failure") }
	if err := checkStreamURL(&url.URL{Scheme: "http", Host: "example.test"}); err == nil {
		t.Fatal("resolver failure accepted")
	}
	lookupIP = func(string) ([]net.IP, error) { return nil, nil }
	if err := checkStreamURL(&url.URL{Scheme: "http", Host: "example.test"}); err == nil {
		t.Fatal("empty address list accepted")
	}
	lookupIP = func(string) ([]net.IP, error) { return []net.IP{net.ParseIP("192.168.1.5")}, nil }
	if err := checkStreamURL(&url.URL{Scheme: "https", Host: "example.test"}); err != nil {
		t.Fatalf("private address rejected: %v", err)
	}
}

func TestIsLocalIP(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{"127.0.0.1", true},
		{"192.168.1.1", true},
		{"10.1.2.3", true},
		{"172.16.0.1", true},
		{"169.254.1.1", true},
		{"100.64.0.1", true},
		{"100.127.255.255", true},
		{"100.63.255.255", false},
		{"100.128.0.1", false},
		{"8.8.8.8", false},
		{"::1", true},
		{"fe80::1", true},
		{"2606:4700::1111", false},
	}
	for _, testCase := range cases {
		if got := isLocalIP(net.ParseIP(testCase.raw)); got != testCase.want {
			t.Fatalf("isLocalIP(%s) = %v, want %v", testCase.raw, got, testCase.want)
		}
	}
}

func TestAPIPlayRejectsOversizedAndGarbageBodies(t *testing.T) {
	display := NewDisplay(Config{Logf: t.Logf})
	server := httptest.NewServer(display.Handler())
	defer server.Close()

	oversized := strings.NewReader(`{"url":"` + strings.Repeat("a", 2<<20) + `"}`)
	response, err := http.Post(server.URL+"/api/play", "application/json", oversized)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("oversized body status = %d, want 400", response.StatusCode)
	}

	truncated, err := http.Post(server.URL+"/api/play", "application/json", strings.NewReader(`{"url":`))
	if err != nil {
		t.Fatal(err)
	}
	_ = truncated.Body.Close()
	if truncated.StatusCode != http.StatusBadRequest {
		t.Fatalf("truncated body status = %d, want 400", truncated.StatusCode)
	}
}

func TestConcurrentRequests(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("stream"))
	}))
	defer upstream.Close()

	display := NewDisplay(Config{Logf: t.Logf})
	server := httptest.NewServer(display.Handler())
	defer server.Close()

	var group sync.WaitGroup
	errs := make(chan error, 64)
	for i := 0; i < 20; i++ {
		group.Add(3)
		go func() {
			defer group.Done()
			response, err := http.Get(server.URL + "/api/state")
			if err != nil {
				errs <- err
				return
			}
			_ = response.Body.Close()
			if response.StatusCode != http.StatusOK {
				errs <- fmt.Errorf("state status = %d", response.StatusCode)
			}
		}()
		go func() {
			defer group.Done()
			body := strings.NewReader(`{"url":"http://127.0.0.1:9/live.ts"}`)
			response, err := http.Post(server.URL+"/api/play", "application/json", body)
			if err != nil {
				errs <- err
				return
			}
			_ = response.Body.Close()
			if response.StatusCode != http.StatusOK {
				errs <- fmt.Errorf("play status = %d", response.StatusCode)
			}
		}()
		go func() {
			defer group.Done()
			response, err := http.Get(server.URL + "/stream?url=" + url.QueryEscape(upstream.URL+"/live.ts"))
			if err != nil {
				errs <- err
				return
			}
			data, _ := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if response.StatusCode != http.StatusOK || string(data) != "stream" {
				errs <- fmt.Errorf("stream status=%d body=%q", response.StatusCode, data)
			}
		}()
	}
	group.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent request failed: %v", err)
	}
	if state := display.State(); state.State != "playing" {
		t.Fatalf("final state = %+v", state)
	}
}
