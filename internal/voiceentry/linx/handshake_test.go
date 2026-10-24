package linx

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// scriptedServer accepts one WebSocket connection, waits for the device hello
// and then runs script, so tests can exercise odd server behaviour.
func scriptedServer(t *testing.T, script func(ctx context.Context, c *websocket.Conn)) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		ctx := context.Background()
		if _, _, err := c.Read(ctx); err != nil {
			return
		}
		script(ctx, c)
	}))
	t.Cleanup(srv.Close)
	return "ws" + srv.URL[len("http"):]
}

func text(ctx context.Context, c *websocket.Conn, s string) {
	_ = c.Write(ctx, websocket.MessageText, []byte(s))
}

func newDevice(url string) Device {
	return Device{MAC: "02:00:00:00:00:01", ClientID: "c", Token: "t", WSURL: url}
}

func TestNewSessionRejectsMissingURLAndDialFailure(t *testing.T) {
	if _, err := NewSession(Config{}); err == nil {
		t.Fatal("missing URL accepted")
	}
	if _, err := NewSession(Config{Device: newDevice("ws://127.0.0.1:1/"), HelloTimeoutSeconds: 1}); err == nil || !strings.Contains(err.Error(), "websocket dial") {
		t.Fatalf("err=%v", err)
	}
}

func TestNewSessionFailsWhenServerNeverGreets(t *testing.T) {
	url := scriptedServer(t, func(ctx context.Context, c *websocket.Conn) { c.Close(websocket.StatusNormalClosure, "bye") })
	if _, err := NewSession(Config{Device: newDevice(url), HelloTimeoutSeconds: 2}); err == nil || !strings.Contains(err.Error(), "read hello") {
		t.Fatalf("err=%v", err)
	}
}

func TestNewSessionRejectsInvalidHello(t *testing.T) {
	url := scriptedServer(t, func(ctx context.Context, c *websocket.Conn) {
		text(ctx, c, "not json")
		time.Sleep(200 * time.Millisecond)
	})
	if _, err := NewSession(Config{Device: newDevice(url), HelloTimeoutSeconds: 2}); err == nil || !strings.Contains(err.Error(), "invalid hello") {
		t.Fatalf("err=%v", err)
	}
}

func TestNewSessionToleratesNoiseBeforeHelloAndDefaultsAudioParams(t *testing.T) {
	url := scriptedServer(t, func(ctx context.Context, c *websocket.Conn) {
		_ = c.Write(ctx, websocket.MessageBinary, []byte{1, 2})
		text(ctx, c, `{"type":"stt","text":"早到的消息"}`)
		text(ctx, c, `{"type":"hello","session_id":"z"}`)
		time.Sleep(300 * time.Millisecond)
	})
	s, err := NewSession(Config{Device: newDevice(url), HelloTimeoutSeconds: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.SessionID() != "z" || s.AudioParams().SampleRate != DefaultSampleRate || s.AudioParams().FrameDuration != DefaultFrameMS {
		t.Fatalf("session=%s params=%+v", s.SessionID(), s.AudioParams())
	}
	select {
	case e := <-s.Events():
		if e.Type != "stt" || e.Text != "早到的消息" {
			t.Fatalf("event=%+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("early stt was dropped")
	}
}

func TestSessionEmitsEveryEventKind(t *testing.T) {
	url := scriptedServer(t, func(ctx context.Context, c *websocket.Conn) {
		text(ctx, c, `{"type":"hello","session_id":"k","audio_params":{"sample_rate":16000}}`)
		_ = c.Write(ctx, websocket.MessageBinary, []byte{9, 9, 9})
		text(ctx, c, "plain text, not json")
		text(ctx, c, `{"type":"llm","emotion":"happy","text":"😊"}`)
		text(ctx, c, `{"type":"tts","state":"sentence_start","text":"你好"}`)
		text(ctx, c, `{"type":"mcp","payload":{"jsonrpc":"2.0","method":"notifications/initialized"}}`)
		time.Sleep(500 * time.Millisecond)
	})
	s, err := NewSession(Config{Device: newDevice(url), HelloTimeoutSeconds: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got := map[string]Event{}
	deadline := time.After(2 * time.Second)
	for len(got) < 5 {
		select {
		case e := <-s.Events():
			got[e.Type] = e
		case <-deadline:
			t.Fatalf("got only %v", got)
		}
	}
	if string(got["audio"].Audio) != "\x09\x09\x09" {
		t.Fatalf("audio=%v", got["audio"].Audio)
	}
	if string(got["message"].Raw) != "plain text, not json" {
		t.Fatalf("message=%q", got["message"].Raw)
	}
	if got["llm"].Text != "😊" || got["tts"].Text != "你好" || got["mcp"].Text != "notifications/initialized" {
		t.Fatalf("events=%+v", got)
	}
}

func TestWriteFailsOnUnmarshalableMessage(t *testing.T) {
	s := &Session{}
	if err := s.write(map[string]any{"bad": make(chan int)}); err == nil {
		t.Fatal("unmarshalable message accepted")
	}
}

func TestRedactTokensHandlesArraysAndInvalidJSON(t *testing.T) {
	if got := redactTokens([]byte("{broken")); got != nil {
		t.Fatalf("invalid JSON redacted to %q", got)
	}
	out := string(redactTokens([]byte(`{"list":[{"token":"a"},"plain",1,[{"token":"b"}]]}`)))
	if strings.Contains(out, `"a"`) || strings.Contains(out, `"b"`) || !strings.Contains(out, "plain") {
		t.Fatalf("out=%s", out)
	}
}

func TestMax(t *testing.T) {
	if max(1, 2) != 2 || max(3, 2) != 3 {
		t.Fatal("max is wrong")
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func reply(status int, body string) roundTrip {
	return func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	}
}

func TestReportUsesProductionURLAndSurfacesFailures(t *testing.T) {
	var seen string
	ok := roundTrip(func(r *http.Request) (*http.Response, error) {
		seen = r.URL.String()
		return reply(200, `{"websocket":{"url":"ws://x","token":"t"}}`)(r)
	})
	res, err := OTAClient{HTTPClient: &http.Client{Transport: ok}}.Report(context.Background(), Device{MAC: "m", ClientID: "c"})
	if err != nil || seen != OTAURL || res.WebSocket == nil {
		t.Fatalf("url=%q res=%+v err=%v", seen, res, err)
	}
	d := Device{MAC: "m", ClientID: "c"}
	ctx := context.Background()
	if _, err := (OTAClient{BaseURL: "http://[bad"}).Report(ctx, d); err == nil {
		t.Fatal("invalid base URL accepted")
	}
	failing := &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) { return nil, errors.New("network down") })}
	if _, err := (OTAClient{HTTPClient: failing}).Report(ctx, d); err == nil || !strings.Contains(err.Error(), "network down") {
		t.Fatalf("err=%v", err)
	}
	if _, err := (OTAClient{HTTPClient: &http.Client{Transport: reply(200, "<html>")}}).Report(ctx, d); err == nil {
		t.Fatal("non-JSON response accepted")
	}
	if _, err := (OTAClient{HTTPClient: &http.Client{Transport: reply(500, `{"error":"boom"}`)}}).Report(ctx, d); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err=%v", err)
	}
}

func TestBindUsesProductionURLAndSurfacesFailures(t *testing.T) {
	var seen string
	ok := roundTrip(func(r *http.Request) (*http.Response, error) {
		seen = r.URL.String()
		return reply(200, `{"code":0}`)(r)
	})
	if err := (OTAClient{HTTPClient: &http.Client{Transport: ok}}).Bind(context.Background(), "k", "agent", "123456"); err != nil {
		t.Fatal(err)
	}
	if want := APIURL + "/xiaozhi/device/bind/agent/123456"; seen != want {
		t.Fatalf("url=%q want %q", seen, want)
	}
	if err := (OTAClient{APIBase: "http://[bad"}).Bind(context.Background(), "k", "a", "c"); err == nil {
		t.Fatal("invalid base URL accepted")
	}
	failing := &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) { return nil, errors.New("network down") })}
	if err := (OTAClient{HTTPClient: failing}).Bind(context.Background(), "k", "a", "c"); err == nil || !strings.Contains(err.Error(), "bind request") {
		t.Fatalf("err=%v", err)
	}
}

func TestNewIdentityReportsEntropyFailure(t *testing.T) {
	orig := randRead
	defer func() { randRead = orig }()
	calls := 0
	randRead = func(b []byte) (int, error) {
		calls++
		if calls == 2 {
			return 0, errors.New("no entropy")
		}
		return orig(b)
	}
	if _, err := NewIdentity(); err == nil {
		t.Fatal("second entropy failure ignored")
	}
	calls = 0
	randRead = func([]byte) (int, error) { return 0, errors.New("no entropy") }
	if _, err := NewIdentity(); err == nil {
		t.Fatal("first entropy failure ignored")
	}
}

func TestSaveDeviceReportsWriteFailure(t *testing.T) {
	if err := SaveDevice(filepath.Join(t.TempDir(), "missing-dir", "d.json"), Device{}); err == nil {
		t.Fatal("write into a missing directory succeeded")
	}
}
