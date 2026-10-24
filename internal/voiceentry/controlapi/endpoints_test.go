package controlapi

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/open-anywhere/open-anywhere/internal/voiceentry/register"
	"github.com/open-anywhere/open-anywhere/internal/voiceentry/voice"
)

func newStartedServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	c := voice.NewController(fakeTransport{}, voice.Realtime)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	s := New(c, func() Status { return Status{State: c.State(), Mode: c.Mode(), Registered: true, MAC: "m"} })
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, ts
}

func TestControlEndpointsAcceptRequests(t *testing.T) {
	_, ts := newStartedServer(t)
	post := func(path, body string) int {
		resp, err := http.Post(ts.URL+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	for path, body := range map[string]string{"/say": `{"text":"嗨"}`, "/announce": `{"text":"播报"}`, "/pause": ``, "/resume": ``, "/abort": ``} {
		if got := post(path, body); got != http.StatusAccepted {
			t.Errorf("%s=%d", path, got)
		}
	}
	for _, path := range []string{"/say", "/announce"} {
		if got := post(path, `{"text":""}`); got != http.StatusBadRequest {
			t.Errorf("%s empty=%d", path, got)
		}
	}
	resp, err := http.Get(ts.URL + "/status")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("status err=%v resp=%v", err, resp)
	}
	resp.Body.Close()
	if resp, _ := http.Get(ts.URL + "/nope"); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown path=%d", resp.StatusCode)
	}
}

func TestEventsStreamsPublishedEvents(t *testing.T) {
	s, ts := newStartedServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	go func() {
		for ctx.Err() == nil {
			s.Publish(voice.Event{Type: "stt", Text: "你好"})
			time.Sleep(20 * time.Millisecond)
		}
	}()
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil || !strings.Contains(line, `"text":"你好"`) {
		t.Fatalf("line=%q err=%v", line, err)
	}
}

func TestAudioEndpointWithoutHandlerAndConsolePath(t *testing.T) {
	_, ts := newStartedServer(t)
	resp, err := http.Get(ts.URL + "/audio")
	if err != nil || resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("audio=%v err=%v", resp, err)
	}
	resp.Body.Close()
	resp, err = http.Get(ts.URL + "/")
	if err != nil || resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("console=%v err=%v", resp, err)
	}
	resp.Body.Close()
}

func TestAudioHandlerErrorClosesSocket(t *testing.T) {
	s, ts := newStartedServer(t)
	s.SetPCMHandler(func([]byte) error { return http.ErrAbortHandler })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/audio", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Write(ctx, websocket.MessageText, []byte("ignored"))
	_ = conn.Write(ctx, websocket.MessageBinary, []byte{1})
	if _, _, err := conn.Read(ctx); err == nil {
		t.Fatal("socket stayed open after handler error")
	}
}

func TestRegistrationUnavailableAndBadBody(t *testing.T) {
	s, ts := newStartedServer(t)
	post := func(body string) int {
		resp, err := http.Post(ts.URL+"/device/register", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if got := post("{}"); got != http.StatusNotImplemented {
		t.Fatalf("no handler=%d", got)
	}
	s.SetRegistration(func(context.Context, register.Request) (register.Result, error) { return register.Result{}, nil })
	if got := post("not json"); got != http.StatusBadRequest {
		t.Fatalf("bad body=%d", got)
	}
	if resp, _ := http.Get(ts.URL + "/device/register"); resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET=%d", resp.StatusCode)
	}
	if got := post("{}"); got != http.StatusOK {
		t.Fatalf("ok=%d", got)
	}
}

func TestConnectionActionUnavailable(t *testing.T) {
	_, ts := newStartedServer(t)
	resp, err := http.Post(ts.URL+"/reconnect", "application/json", nil)
	if err != nil || resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("reconnect=%v err=%v", resp, err)
	}
	resp.Body.Close()
}

func TestCrossOriginPostsAreRefused(t *testing.T) {
	_, ts := newStartedServer(t)
	post := func(origin string) int {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/say", strings.NewReader(`{"text":"嗨"}`))
		req.Header.Set("Content-Type", "text/plain")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if got := post("https://evil.example"); got != http.StatusForbidden {
		t.Fatalf("cross-origin=%d", got)
	}
	if got := post("null"); got != http.StatusForbidden {
		t.Fatalf("null origin=%d", got)
	}
	if got := post(ts.URL); got != http.StatusAccepted {
		t.Fatalf("same-origin=%d", got)
	}
	if got := post(""); got != http.StatusAccepted {
		t.Fatalf("no origin=%d", got)
	}
}
