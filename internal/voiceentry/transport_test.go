package voiceentry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/open-anywhere/open-anywhere/internal/voiceentry/linx"
	"github.com/open-anywhere/open-anywhere/internal/voiceentry/voice"
)

func helloServer(t *testing.T) (*httptest.Server, <-chan map[string]any) {
	t.Helper()
	msgs := make(chan map[string]any, 16)
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
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"hello","session_id":"sess","audio_params":{"sample_rate":16000}}`))
		for {
			typ, b, err := c.Read(ctx)
			if err != nil {
				return
			}
			var m map[string]any
			if typ == websocket.MessageBinary {
				m = map[string]any{"binary": float64(len(b))}
			} else {
				_ = json.Unmarshal(b, &m)
			}
			msgs <- m
		}
	}))
	t.Cleanup(srv.Close)
	return srv, msgs
}

func TestSessionTransportWithoutSession(t *testing.T) {
	tr := &transport{}
	if tr.Connected() || tr.SessionID() != "" {
		t.Fatal("empty transport reports a session")
	}
	for name, err := range map[string]error{
		"start": tr.ListenStart("realtime"), "stop": tr.ListenStop(), "text": tr.SendText("x"),
		"announce": tr.Announce("x"), "abort": tr.Abort("x"), "audio": tr.SendAudio([]byte{1}),
	} {
		if err == nil || !strings.Contains(err.Error(), "reconnecting") {
			t.Errorf("%s err=%v", name, err)
		}
	}
}

func TestReconnectResidentAndTransportForwarding(t *testing.T) {
	srv, msgs := helloServer(t)
	dev := linx.Device{MAC: "m", ClientID: "c", Token: "t", WSURL: "ws" + srv.URL[len("http"):]}
	tr := &transport{}
	c := voice.NewController(tr, voice.Realtime)
	var s *linx.Session
	if err := reconnect(context.Background(), &s, tr, c, dev, nil, []time.Duration{0}); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if !tr.Connected() || tr.SessionID() != "sess" || c.State() != voice.Listening {
		t.Fatalf("connected=%v session=%q state=%s", tr.Connected(), tr.SessionID(), c.State())
	}
	if m := <-msgs; m["state"] != "start" {
		t.Fatalf("first message=%v", m)
	}
	_ = tr.ListenStop()
	_ = tr.SendText("嗨")
	_ = tr.Announce("播报")
	_ = tr.Abort("x")
	_ = tr.SendAudio([]byte{1, 2})
	for _, want := range []string{"stop", "detect", "detect", "", ""} {
		m := <-msgs
		if want != "" && m["state"] != want {
			t.Fatalf("message=%v want state %s", m, want)
		}
	}
	// a failing dial leaves the transport disconnected and reports the error
	bad := dev
	bad.WSURL = "ws://127.0.0.1:1/"
	if err := reconnect(context.Background(), &s, tr, c, bad, nil, []time.Duration{0}); err == nil || tr.Connected() {
		t.Fatalf("err=%v connected=%v", err, tr.Connected())
	}
}
