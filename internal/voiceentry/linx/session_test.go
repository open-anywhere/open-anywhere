package linx

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestSessionHandshakeMCPAndText(t *testing.T) {
	got := make(chan map[string]any, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for key, want := range map[string]string{"Authorization": "Bearer t", "Device-Id": "02:00:00:00:00:01", "Client-Id": "c", "Protocol-Version": "1"} {
			if got := r.Header.Get(key); got != want {
				t.Errorf("header %s=%q, want %q", key, got, want)
			}
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "done")
		ctx := context.Background()
		_, hello, err := c.Read(ctx)
		if err != nil {
			t.Error(err)
			return
		}
		var h map[string]any
		_ = json.Unmarshal(hello, &h)
		if h["type"] != "hello" {
			t.Errorf("hello = %#v", h)
		}
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"hello","session_id":"s1","audio_params":{"format":"opus","sample_rate":16000,"channels":1,"frame_duration":60,"play_buffer_duration":1000}}`))
		_, b, _ := c.Read(ctx)
		var req map[string]any
		_ = json.Unmarshal(b, &req)
		got <- req
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"mcp","payload":{"jsonrpc":"2.0","id":1,"method":"tools/list"}}`))
		_, b, _ = c.Read(ctx)
		var mcp map[string]any
		_ = json.Unmarshal(b, &mcp)
		got <- mcp
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"tts","state":"sentence_start","text":"你好"}`))
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"tts","state":"stop","is_aborted":false}`))
		select {
		case <-time.After(time.Second):
		case <-make(chan struct{}):
		}
	}))
	defer srv.Close()
	url := "ws" + srv.URL[len("http"):]
	s, err := NewSession(Config{Device: Device{MAC: "02:00:00:00:00:01", ClientID: "c", Token: "t", WSURL: url}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.SendText("测试"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		select {
		case e := <-s.Events():
			if e.Type == "tts" && e.Data["state"] == "stop" {
				return
			}
		case <-time.After(time.Second):
			t.Fatal("event timeout")
		}
	}
	_ = got
}

func TestRedactTokensMasksNestedTokens(t *testing.T) {
	out := string(redactTokens([]byte(`{"payload":{"params":{"capabilities":{"vision":{"token":"secret","url":"u"}}}}}`)))
	if strings.Contains(out, "secret") || !strings.Contains(out, `"token":"***"`) || !strings.Contains(out, `"url":"u"`) {
		t.Fatalf("redacted=%s", out)
	}
}

func TestCallToolRunsRegisteredToolAndRejectsUnknown(t *testing.T) {
	s := &Session{tools: []Tool{{Name: "self.t", Call: func(a map[string]any) (string, error) {
		if a["x"] == nil {
			return "", errors.New("缺少 x")
		}
		return "ok", nil
	}}}}
	if r := s.callTool("self.t", map[string]any{"x": 1.0}); r["isError"] != false {
		t.Fatalf("result=%v", r)
	}
	if r := s.callTool("self.t", nil); r["isError"] != true {
		t.Fatalf("result=%v", r)
	}
	if r := s.callTool("nope", nil); r != nil {
		t.Fatalf("unknown tool result=%v", r)
	}
}

func TestDispatchRedactsTokensInRawMessages(t *testing.T) {
	s := &Session{events: make(chan Event, 1)}
	raw := []byte(`{"type":"custom","token":"secret","nested":{"token":"secret2"}}`)
	var msg map[string]any
	_ = json.Unmarshal(raw, &msg)
	s.dispatch(msg, raw)
	e := <-s.events
	if strings.Contains(string(e.Raw), "secret") || !strings.Contains(string(e.Raw), `"token":"***"`) {
		t.Fatalf("raw=%s", e.Raw)
	}
}
