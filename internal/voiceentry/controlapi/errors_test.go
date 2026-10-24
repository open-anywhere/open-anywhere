package controlapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/open-anywhere/open-anywhere/internal/voiceentry/register"
	"github.com/open-anywhere/open-anywhere/internal/voiceentry/voice"
)

// brokenTransport fails every call, as the transport does while Linx is down.
type brokenTransport struct{}

var errDown = errors.New("Linx session is reconnecting")

func (brokenTransport) ListenStart(string) error { return errDown }
func (brokenTransport) ListenStop() error        { return errDown }
func (brokenTransport) SendText(string) error    { return errDown }
func (brokenTransport) Announce(string) error    { return errDown }
func (brokenTransport) Abort(string) error       { return errDown }

func TestControlEndpointsReportTransportFailuresAsConflict(t *testing.T) {
	s := New(voice.NewController(brokenTransport{}, voice.Realtime), func() Status { return Status{} })
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	for path, body := range map[string]string{
		"/say": `{"text":"嗨"}`, "/announce": `{"text":"播报"}`, "/pause": `{}`, "/resume": `{}`, "/abort": `{}`,
	} {
		resp, err := http.Post(ts.URL+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusConflict {
			t.Errorf("%s=%d, want 409", path, resp.StatusCode)
		}
	}
}

func TestRegisterFailureWithoutStatusIsLabelledError(t *testing.T) {
	s := New(voice.NewController(fakeTransport{}, voice.Realtime), func() Status { return Status{} })
	s.SetRegistration(func(context.Context, register.Request) (register.Result, error) {
		return register.Result{}, errors.New("platform unreachable")
	})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	resp, err := http.Post(ts.URL+"/device/register", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadGateway || !strings.Contains(string(body), `"status":"error"`) {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
}

func TestWebSocketEndpointsRefusePlainHTTP(t *testing.T) {
	s := New(voice.NewController(fakeTransport{}, voice.Realtime), func() Status { return Status{} })
	s.SetOpusHandler(func([]byte) error { return nil })
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	for _, path := range []string{"/playback", "/audio?codec=opus"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode < 400 {
			t.Errorf("%s answered %d to a request without a WebSocket upgrade", path, resp.StatusCode)
		}
	}
}

func TestPlaybackSubscriptionEndsWhenTheBrowserDisconnects(t *testing.T) {
	s := New(voice.NewController(fakeTransport{}, voice.Realtime), func() Status { return Status{} })
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/playback", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatal(err)
	}
	subscribers := func() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.audioSubs) }
	if subscribers() != 1 {
		t.Fatalf("subscribers=%d", subscribers())
	}
	_ = conn.CloseNow()
	for subscribers() != 0 {
		s.PublishAudio([]byte{1})
		if ctx.Err() != nil {
			t.Fatal("subscription leaked after the browser disconnected")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// noFlush is a ResponseWriter that cannot stream.
type noFlush struct{ http.ResponseWriter }

func TestEventsNeedAStreamingResponseWriter(t *testing.T) {
	s := New(voice.NewController(fakeTransport{}, voice.Realtime), func() Status { return Status{} })
	rec := httptest.NewRecorder()
	s.events(noFlush{rec}, httptest.NewRequest(http.MethodGet, "/events", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d", rec.Code)
	}
}
