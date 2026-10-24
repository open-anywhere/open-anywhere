package mocktv

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestPlayWithoutCuedURLIsIgnored(t *testing.T) {
	display := NewDisplay(Config{Logf: t.Logf})
	display.play()
	if state := display.State(); state.State != "idle" || state.URL != "" {
		t.Fatalf("state = %+v, want idle", state)
	}
	if got := display.transportState(); got != "STOPPED" {
		t.Fatalf("transport state = %q, want STOPPED", got)
	}
}

func TestAPIPlayAndStop(t *testing.T) {
	display := NewDisplay(Config{Name: "客厅电视", Logf: t.Logf})
	server := httptest.NewServer(display.Handler())
	defer server.Close()

	initial, err := getState(server.URL + "/api/state")
	if err != nil {
		t.Fatal(err)
	}
	if initial.State != "idle" || initial.Name != "客厅电视" {
		t.Fatalf("initial state = %+v", initial)
	}

	const streamURL = "http://127.0.0.1:9/live.ts"
	response, err := http.Post(server.URL+"/api/play", "application/json", strings.NewReader(`{"url":"`+streamURL+`"}`))
	if err != nil {
		t.Fatalf("POST /api/play: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	playing, err := decodeState(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if playing.State != "playing" || playing.URL != streamURL {
		t.Fatalf("state after play = %+v", playing)
	}

	stopped, err := postWithoutBody(server.URL + "/api/stop")
	if err != nil {
		t.Fatal(err)
	}
	if stopped.State != "idle" || stopped.URL != "" {
		t.Fatalf("state after stop = %+v", stopped)
	}
}

func TestAPIPlayRejectsNonLocalURLs(t *testing.T) {
	display := NewDisplay(Config{Logf: t.Logf})
	server := httptest.NewServer(display.Handler())
	defer server.Close()

	for _, body := range []string{
		`{"url":"http://8.8.8.8/live.ts"}`,
		`{"url":"ftp://127.0.0.1/live.ts"}`,
		`{"url":""}`,
		`not json`,
	} {
		response, err := http.Post(server.URL+"/api/play", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST %s: %v", body, err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("body %s: status = %d, want 400", body, response.StatusCode)
		}
	}
	if state := display.State(); state.State != "idle" {
		t.Fatalf("rejected play changed state: %+v", state)
	}
}

func TestStreamProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp2t")
		_, _ = w.Write([]byte("mpeg-ts-bytes"))
	}))
	defer upstream.Close()

	display := NewDisplay(Config{Logf: t.Logf})
	server := httptest.NewServer(display.Handler())
	defer server.Close()

	response, err := http.Get(server.URL + "/stream?url=" + url.QueryEscape(upstream.URL+"/live.ts"))
	if err != nil {
		t.Fatalf("GET /stream: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	if got := response.Header.Get("Content-Type"); got != "video/mp2t" {
		t.Fatalf("Content-Type = %q", got)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "mpeg-ts-bytes" {
		t.Fatalf("body = %q", body)
	}
}

func TestStreamProxyRejectsOtherURLs(t *testing.T) {
	// Reserve a loopback port and close it, so connecting is guaranteed to fail.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedAddress := listener.Addr().String()
	_ = listener.Close()

	display := NewDisplay(Config{Logf: t.Logf})
	server := httptest.NewServer(display.Handler())
	defer server.Close()

	cases := []struct {
		raw  string
		want int
	}{
		{"http://8.8.8.8/live.ts", http.StatusForbidden},
		{"ftp://127.0.0.1/live.ts", http.StatusBadRequest},
		{"", http.StatusBadRequest},
		{"http://" + closedAddress + "/live.ts", http.StatusBadGateway},
	}
	for _, testCase := range cases {
		response, err := http.Get(server.URL + "/stream?url=" + url.QueryEscape(testCase.raw))
		if err != nil {
			t.Fatalf("GET %s: %v", testCase.raw, err)
		}
		_ = response.Body.Close()
		if response.StatusCode != testCase.want {
			t.Fatalf("url %q: status = %d, want %d", testCase.raw, response.StatusCode, testCase.want)
		}
	}
}

func TestEventsStreamState(t *testing.T) {
	display := NewDisplay(Config{Logf: t.Logf})
	server := httptest.NewServer(display.Handler())
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET /events: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}

	reader := bufio.NewReader(response.Body)
	readState := func() State {
		t.Helper()
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("read event: %v", err)
			}
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var state State
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &state); err != nil {
				t.Fatalf("event data: %v", err)
			}
			return state
		}
	}

	if state := readState(); state.State != "idle" {
		t.Fatalf("initial event = %+v", state)
	}

	const streamURL = "http://127.0.0.1:9/live.ts"
	display.cue(streamURL)
	if state := readState(); state.State != "cued" || state.URL != streamURL {
		t.Fatalf("cued event = %+v", state)
	}

	display.play()
	if state := readState(); state.State != "playing" || state.URL != streamURL {
		t.Fatalf("playing event = %+v", state)
	}

	display.stop()
	if state := readState(); state.State != "idle" || state.URL != "" {
		t.Fatalf("stopped event = %+v", state)
	}
}

func getState(endpoint string) (State, error) {
	response, err := http.Get(endpoint)
	if err != nil {
		return State{}, err
	}
	defer response.Body.Close()
	return decodeState(response.Body)
}

func postWithoutBody(endpoint string) (State, error) {
	response, err := http.Post(endpoint, "application/json", nil)
	if err != nil {
		return State{}, err
	}
	defer response.Body.Close()
	return decodeState(response.Body)
}

func decodeState(reader io.Reader) (State, error) {
	var state State
	if err := json.NewDecoder(reader).Decode(&state); err != nil {
		return State{}, err
	}
	return state, nil
}
