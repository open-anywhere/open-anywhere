package controlapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/coder/websocket"
	"github.com/open-anywhere/open-anywhere/internal/voiceentry/register"
	"github.com/open-anywhere/open-anywhere/internal/voiceentry/voice"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeTransport struct{}

func (fakeTransport) ListenStart(string) error { return nil }
func (fakeTransport) ListenStop() error        { return nil }
func (fakeTransport) SendText(string) error    { return nil }
func (fakeTransport) Announce(string) error    { return nil }
func (fakeTransport) Abort(string) error       { return nil }
func TestAnnounceEndpoint(t *testing.T) {
	c := voice.NewController(fakeTransport{}, voice.Realtime)
	_ = c.Start()
	s := New(c, func() Status { return Status{State: c.State(), Mode: c.Mode()} })
	r := httptest.NewRequest("POST", "/announce", strings.NewReader(`{"text":"开始"}`))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 202 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestConsoleEndpoint(t *testing.T) {
	c := voice.NewController(fakeTransport{}, voice.Realtime)
	s := New(c, func() Status { return Status{} })
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "灵矽本地控制台") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAudioEndpointForwardsPCM(t *testing.T) {
	got := make(chan []byte, 1)
	c := voice.NewController(fakeTransport{}, voice.Realtime)
	s := New(c, func() Status { return Status{} })
	s.SetPCMHandler(func(p []byte) error { got <- p; return nil })
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/audio", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
	want := []byte{1, 2, 3, 4}
	if err := conn.Write(ctx, websocket.MessageBinary, want); err != nil {
		t.Fatal(err)
	}
	select {
	case p := <-got:
		if string(p) != string(want) {
			t.Fatalf("pcm=%v", p)
		}
	case <-time.After(time.Second):
		t.Fatal("PCM was not forwarded")
	}
}

func TestEventJSONUsesStableFields(t *testing.T) {
	b, err := json.Marshal(voice.Event{Type: "tts", State: "stop", Text: "完成"})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"type":"tts","state":"stop","text":"完成"}` {
		t.Fatalf("json=%s", b)
	}
}

func TestAudioEndpointForwardsOpusPackets(t *testing.T) {
	got := make(chan []byte, 1)
	s := New(voice.NewController(fakeTransport{}, voice.Realtime), func() Status { return Status{} })
	s.SetOpusHandler(func(p []byte) error { got <- p; return nil })
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/audio?codec=opus", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
	if err := conn.Write(ctx, websocket.MessageBinary, []byte{9, 8}); err != nil {
		t.Fatal(err)
	}
	select {
	case p := <-got:
		if string(p) != string([]byte{9, 8}) {
			t.Fatalf("opus=%v", p)
		}
	case <-time.After(time.Second):
		t.Fatal("Opus packet was not forwarded")
	}
}

func TestPlaybackStreamsDownlinkAudio(t *testing.T) {
	s := New(voice.NewController(fakeTransport{}, voice.Realtime), func() Status { return Status{} })
	s.SetPlaybackSampleRate(24000)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/playback", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
	_, hello, err := conn.Read(ctx)
	if err != nil || string(hello) != `{"sample_rate":24000}` {
		t.Fatalf("hello=%q err=%v", hello, err)
	}
	s.PublishAudio([]byte{7, 7})
	typ, data, err := conn.Read(ctx)
	if err != nil || typ != websocket.MessageBinary || string(data) != string([]byte{7, 7}) {
		t.Fatalf("typ=%v data=%v err=%v", typ, data, err)
	}
}

func TestConnectionControlEndpoints(t *testing.T) {
	var calls []string
	s := New(voice.NewController(fakeTransport{}, voice.Realtime), func() Status { return Status{} })
	s.SetConnectionControl(
		func() error { calls = append(calls, "disconnect"); return nil },
		func() error { calls = append(calls, "reconnect"); return errors.New("dial failed") },
	)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	post := func(path string) int {
		resp, err := http.Post(ts.URL+path, "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if got := post("/disconnect"); got != http.StatusAccepted {
		t.Fatalf("disconnect=%d", got)
	}
	if got := post("/reconnect"); got != http.StatusBadGateway {
		t.Fatalf("reconnect=%d, want 502 on dial failure", got)
	}
	if resp, _ := http.Get(ts.URL + "/disconnect"); resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET disconnect=%d", resp.StatusCode)
	}
	if len(calls) != 2 {
		t.Fatalf("calls=%v", calls)
	}
}

func TestRegisterEndpointRequiresJSONAndReturnsResult(t *testing.T) {
	s := New(voice.NewController(fakeTransport{}, voice.Realtime), func() Status { return Status{} })
	s.SetRegistration(func(_ context.Context, in register.Request) (register.Result, error) {
		if in.APIKey == "bad" {
			return register.Result{Status: "pending", Code: "1"}, errors.New("bind failed")
		}
		return register.Result{Status: "pending", Code: "123456"}, nil
	})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	post := func(ctype, body string) (*http.Response, string) {
		resp, err := http.Post(ts.URL+"/device/register", ctype, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp, string(b)
	}
	if resp, _ := post("text/plain", "{}"); resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("text/plain=%d", resp.StatusCode)
	}
	if resp, body := post("application/json", "{}"); resp.StatusCode != http.StatusOK || !strings.Contains(body, `"code":"123456"`) {
		t.Fatalf("ok=%d %s", resp.StatusCode, body)
	}
	if resp, body := post("application/json", `{"api_key":"bad"}`); resp.StatusCode != http.StatusBadGateway || !strings.Contains(body, "bind failed") || strings.Contains(body, "bad") {
		t.Fatalf("err=%d %s", resp.StatusCode, body)
	}
}
