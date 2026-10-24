package register

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/voiceentry/linx"
)

func TestRegisterPendingThenBoundSavesDevice(t *testing.T) {
	var bound atomic.Bool
	var mu sync.Mutex
	macs := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/ota/":
			mu.Lock()
			macs[r.Header.Get("Device-Id")] = true
			mu.Unlock()
			if bound.Load() {
				_, _ = w.Write([]byte(`{"websocket":{"url":"ws://x","token":"tok"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"activation":{"code":"123456"},"websocket":{"url":"ws://x"}}`))
		case "/xiaozhi/device/bind/agent/123456":
			bound.Store(true)
			_, _ = w.Write([]byte(`{"code":0}`))
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "device.json")
	applied := 0
	r := New(Config{Path: path, OTA: linx.OTAClient{BaseURL: srv.URL + "/v1/ota", APIBase: srv.URL}, Device: &atomic.Pointer[linx.Device]{}, Poll: time.Millisecond, Apply: func() error { applied++; return nil }})

	out, err := r.Register(context.Background(), Request{})
	if err != nil || out.Status != "pending" || out.Code != "123456" || r.Registered() || r.ActivationCode() != "123456" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	if _, err := r.Register(context.Background(), Request{AgentID: "agent"}); err == nil {
		t.Fatal("agent id without api key accepted")
	}
	out, err = r.Register(context.Background(), Request{AgentID: "agent", APIKey: "key"})
	if err != nil || out.Status != "registered" || !r.Registered() || applied != 1 {
		t.Fatalf("out=%+v err=%v applied=%d", out, err, applied)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(macs) != 1 {
		t.Fatalf("identity changed between attempts: %v", macs)
	}
	b, err := os.ReadFile(path)
	var saved linx.Device
	if err != nil || json.Unmarshal(b, &saved) != nil || saved.Token != "tok" || saved.MAC != out.MAC || saved.WSURL != "ws://x" {
		t.Fatalf("saved=%s err=%v", b, err)
	}
	if again, _ := r.Register(context.Background(), Request{}); again.Status != "registered" || applied != 1 {
		t.Fatalf("second register=%+v applied=%d", again, applied)
	}
}
