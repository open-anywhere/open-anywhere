package linx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// toolServer greets, sends tools/list plus two tools/call requests, then
// reports every message it receives (binary frames as {"binary":n}).
func toolServer(t *testing.T) (*httptest.Server, <-chan map[string]any) {
	t.Helper()
	msgs := make(chan map[string]any, 32)
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
		for _, m := range []string{
			`{"type":"hello","session_id":"s9","audio_params":{"format":"opus","sample_rate":24000,"channels":1,"frame_duration":60}}`,
			`{"type":"mcp","payload":{"jsonrpc":"2.0","id":2,"method":"tools/list"}}`,
			`{"type":"mcp","payload":{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"self.echo","arguments":{"x":1}}}}`,
			`{"type":"mcp","payload":{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"self.nope"}}}`,
			`{"type":"mcp","payload":{"jsonrpc":"2.0","id":5,"method":"initialize"}}`,
		} {
			_ = c.Write(ctx, websocket.MessageText, []byte(m))
		}
		for {
			typ, b, err := c.Read(ctx)
			if err != nil {
				return
			}
			if typ == websocket.MessageBinary {
				msgs <- map[string]any{"binary": float64(len(b))}
				continue
			}
			var m map[string]any
			_ = json.Unmarshal(b, &m)
			msgs <- m
		}
	}))
	t.Cleanup(srv.Close)
	return srv, msgs
}

func next(t *testing.T, ch <-chan map[string]any) map[string]any {
	t.Helper()
	select {
	case m := <-ch:
		return m
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for message")
		return nil
	}
}

func TestSessionToolsAndControlMessages(t *testing.T) {
	srv, msgs := toolServer(t)
	echo := Tool{Name: "self.echo", Description: "echo", Call: func(a map[string]any) (string, error) { return "ok", nil }}
	s, err := NewSession(Config{Device: Device{MAC: "m", ClientID: "c", Token: "t", WSURL: "ws" + srv.URL[len("http"):]}, Tools: []Tool{echo}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.SessionID() != "s9" || s.AudioParams().SampleRate != 24000 {
		t.Fatalf("session=%s params=%+v", s.SessionID(), s.AudioParams())
	}
	byID := map[float64]map[string]any{}
	for i := 0; i < 4; i++ {
		p := next(t, msgs)["payload"].(map[string]any)
		byID[p["id"].(float64)] = p
	}
	tools := byID[2]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["name"] != "self.echo" {
		t.Fatalf("tools=%v", tools)
	}
	if byID[3]["result"].(map[string]any)["isError"] != false {
		t.Fatalf("call=%v", byID[3])
	}
	if byID[4]["error"].(map[string]any)["code"] != float64(-32601) {
		t.Fatalf("unknown tool=%v", byID[4])
	}
	if byID[5]["result"].(map[string]any)["serverInfo"] == nil {
		t.Fatalf("initialize=%v", byID[5])
	}
	var mcpEvents []string
	for len(mcpEvents) < 4 {
		select {
		case e := <-s.Events():
			if e.Type == "mcp" {
				mcpEvents = append(mcpEvents, e.Text)
			}
		case <-time.After(time.Second):
			t.Fatalf("mcp events=%v", mcpEvents)
		}
	}
	if mcpEvents[1] != "tools/call self.echo" {
		t.Fatalf("mcp events=%v", mcpEvents)
	}

	_ = s.ListenStart("")
	_ = s.ListenStop()
	_ = s.Announce("播报")
	_ = s.Abort("")
	if err := s.SendAudio([]byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if m := next(t, msgs); m["type"] != "listen" || m["state"] != "start" || m["mode"] != "realtime" {
		t.Fatalf("listen start=%v", m)
	}
	if m := next(t, msgs); m["state"] != "stop" {
		t.Fatalf("listen stop=%v", m)
	}
	if m := next(t, msgs); m["text_response"] != "播报" {
		t.Fatalf("announce=%v", m)
	}
	if m := next(t, msgs); m["type"] != "abort" || m["reason"] != "user_interrupt" {
		t.Fatalf("abort=%v", m)
	}
	if m := next(t, msgs); m["binary"] != float64(3) {
		t.Fatalf("audio=%v", m)
	}
}

func TestLoadAndSaveDevice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.json")
	if _, err := LoadDevice(path); !os.IsNotExist(err) {
		t.Fatalf("missing file err=%v", err)
	}
	want := Device{MAC: "02:00:00:00:00:01", ClientID: "c", Token: "t", WSURL: "ws://x"}
	if err := SaveDevice(path, want); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadDevice(path); err != nil || got != want {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	_ = os.WriteFile(path, []byte(`{"mac":"m"}`), 0o600)
	if _, err := LoadDevice(path); err == nil {
		t.Fatal("incomplete device accepted")
	}
	_ = os.WriteFile(path, []byte(`not json`), 0o600)
	if _, err := LoadDevice(path); err == nil {
		t.Fatal("invalid JSON accepted")
	}
}
