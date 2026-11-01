package voiceentry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/open-anywhere/open-anywhere/internal/voiceentry/linx"
)

// fakeLinx is a minimal Linx WebSocket server: it greets, records every text
// message, and answers a user text with stt plus a three-part tts reply.
type fakeLinx struct {
	srv      *httptest.Server
	msgs     chan map[string]any
	sessions atomic.Int32
	binary   chan int // sizes of binary (audio) frames received
	// dropFirst closes the first connection right after the greeting.
	dropFirst bool
	// silent never answers a user text; closeOnDetect hangs up when one arrives;
	// audioReply adds a binary frame to every reply.
	silent, closeOnDetect, audioReply bool
	// refuseFrom hangs up before the greeting from this session number on (0 = never).
	refuseFrom int32
}

func newFakeLinx(t *testing.T, dropFirst bool) *fakeLinx {
	t.Helper()
	return newFakeLinxWith(t, &fakeLinx{dropFirst: dropFirst})
}

func newFakeLinxWith(t *testing.T, f *fakeLinx) *fakeLinx {
	t.Helper()
	f.msgs = make(chan map[string]any, 64)
	f.binary = make(chan int, 64)
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")
		ctx := context.Background()
		if _, _, err := c.Read(ctx); err != nil {
			return
		}
		n := f.sessions.Add(1)
		if f.refuseFrom > 0 && n >= f.refuseFrom {
			return
		}
		send := func(m map[string]any) {
			b, _ := json.Marshal(m)
			_ = c.Write(ctx, websocket.MessageText, b)
		}
		send(map[string]any{"type": "hello", "session_id": fmt.Sprintf("s%d", n), "audio_params": map[string]any{"sample_rate": 16000}})
		if f.dropFirst && n == 1 {
			return
		}
		for {
			typ, b, err := c.Read(ctx)
			if err != nil {
				return
			}
			if typ == websocket.MessageBinary {
				f.binary <- len(b)
				continue
			}
			if typ != websocket.MessageText {
				continue
			}
			var m map[string]any
			_ = json.Unmarshal(b, &m)
			f.msgs <- m
			if m["type"] == "listen" && m["state"] == "detect" {
				if f.closeOnDetect {
					return
				}
				if f.silent {
					continue
				}
				reply := "播报"
				if text, ok := m["text"].(string); ok && m["text_response"] == nil {
					send(map[string]any{"type": "stt", "text": text})
					reply = "收到:" + text
				}
				send(map[string]any{"type": "tts", "state": "start"})
				if f.audioReply {
					_ = c.Write(ctx, websocket.MessageBinary, []byte{7, 7, 7})
				}
				send(map[string]any{"type": "tts", "state": "sentence_start", "text": reply})
				send(map[string]any{"type": "tts", "state": "stop", "is_aborted": false})
			}
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeLinx) wsURL() string { return "ws" + f.srv.URL[len("http"):] }

func (f *fakeLinx) waitFor(t *testing.T, match func(map[string]any) bool) map[string]any {
	t.Helper()
	timeout := time.After(3 * time.Second)
	for {
		select {
		case m := <-f.msgs:
			if match(m) {
				return m
			}
		case <-timeout:
			t.Fatal("timed out waiting for a Linx message")
		}
	}
}

func writeDevice(t *testing.T, wsURL string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "device.json")
	if err := linx.SaveDevice(path, linx.Device{MAC: "02:00:00:00:00:01", ClientID: "c", Token: "t", WSURL: wsURL}); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOneShotTextPrintsReply(t *testing.T) {
	f := newFakeLinx(t, false)
	var out bytes.Buffer
	a, err := New(Config{DevicePath: writeDevice(t, f.wsURL()), Text: "你好", Out: &out, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "用户: 你好") || !strings.Contains(got, "灵矽: 收到:你好") {
		t.Fatalf("output=%q", got)
	}
}

func TestOneShotAnnounceAndMissingDevice(t *testing.T) {
	f := newFakeLinx(t, false)
	var out bytes.Buffer
	a, err := New(Config{DevicePath: writeDevice(t, f.wsURL()), Announce: "游戏开始", Out: &out, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Run(context.Background()); err != nil || !strings.Contains(out.String(), "灵矽: 播报") {
		t.Fatalf("err=%v output=%q", err, out.String())
	}
	if _, err := New(Config{DevicePath: filepath.Join(t.TempDir(), "none.json"), Text: "x"}); err == nil {
		t.Fatal("one-shot mode started without a device file")
	}
}

// startResident runs a resident client on a loopback listener and returns its base URL.
// startResident runs a resident app; tweak, if given, adjusts the App before it runs.
func startResident(t *testing.T, cfg Config, tweak ...func(*App)) (base string, stop func() error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Serve, cfg.Listener = true, ln
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range tweak {
		f(a)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	stopped := false
	stop = func() error {
		stopped = true
		cancel()
		select {
		case err := <-done:
			return err
		case <-time.After(5 * time.Second):
			return fmt.Errorf("Run did not return after cancel")
		}
	}
	t.Cleanup(func() {
		if !stopped {
			_ = stop()
		}
	})
	return "http://" + ln.Addr().String(), stop
}

type statusBody struct {
	Connected  bool   `json:"connected"`
	Registered bool   `json:"registered"`
	SessionID  string `json:"session_id"`
	State      string `json:"state"`
}

func getStatus(t *testing.T, base string) statusBody {
	t.Helper()
	resp, err := http.Get(base + "/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var st statusBody
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	return st
}

func waitStatus(t *testing.T, base string, ok func(statusBody) bool) statusBody {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		st := getStatus(t, base)
		if ok(st) {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("status never matched, last=%+v", st)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func post(t *testing.T, url, body string) int {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestResidentWebFlowAndGracefulStop(t *testing.T) {
	f := newFakeLinx(t, false)
	base, stop := startResident(t, Config{DevicePath: writeDevice(t, f.wsURL())})
	st := waitStatus(t, base, func(s statusBody) bool { return s.Connected })
	if !st.Registered || st.SessionID != "s1" {
		t.Fatalf("status=%+v", st)
	}
	if got := post(t, base+"/say", `{"text":"嗨"}`); got != http.StatusAccepted {
		t.Fatalf("say=%d", got)
	}
	f.waitFor(t, func(m map[string]any) bool { return m["state"] == "detect" && m["text"] == "嗨" })

	if got := post(t, base+"/disconnect", `{}`); got != http.StatusAccepted {
		t.Fatalf("disconnect=%d", got)
	}
	waitStatus(t, base, func(s statusBody) bool { return !s.Connected && s.State == "disconnected" })
	if got := post(t, base+"/say", `{"text":"嗨"}`); got != http.StatusConflict {
		t.Fatalf("say while disconnected=%d", got)
	}
	if got := post(t, base+"/reconnect", `{}`); got != http.StatusAccepted {
		t.Fatalf("reconnect=%d", got)
	}
	st = waitStatus(t, base, func(s statusBody) bool { return s.Connected })
	if st.SessionID != "s2" {
		t.Fatalf("session after reconnect=%q", st.SessionID)
	}
	if err := stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func TestResidentReconnectsAfterServerDrop(t *testing.T) {
	f := newFakeLinx(t, true)
	base, _ := startResident(t, Config{DevicePath: writeDevice(t, f.wsURL()), ReconnectDelays: []time.Duration{10 * time.Millisecond}})
	st := waitStatus(t, base, func(s statusBody) bool { return s.Connected && s.SessionID == "s2" })
	if st.State != "listening" {
		t.Fatalf("status=%+v", st)
	}
}

func TestResidentFailsFastWhenPortIsTaken(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	a, err := New(Config{DevicePath: filepath.Join(t.TempDir(), "none.json"), Serve: true, Listen: ln.Addr().String()})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "listen") {
		t.Fatalf("err=%v", err)
	}
}
