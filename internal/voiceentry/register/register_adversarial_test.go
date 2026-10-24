package register

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/voiceentry/linx"
)

func TestRegisterReportsAFailedTokenPollAfterBinding(t *testing.T) {
	var reports atomic.Int32
	ota := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if reports.Add(1) == 1 {
			io.WriteString(w, `{"activation":{"code":"1234"}}`)
			return
		}
		http.Error(w, "platform unavailable", http.StatusInternalServerError)
	}))
	defer ota.Close()
	bind := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		io.WriteString(w, `{}`)
	}))
	defer bind.Close()

	var device atomic.Pointer[linx.Device]
	registrar := New(Config{
		Path: filepath.Join(t.TempDir(), "device.json"),
		OTA: linx.OTAClient{
			BaseURL:    ota.URL,
			APIBase:    bind.URL,
			HTTPClient: ota.Client(),
		},
		Device: &device,
		Poll:   time.Millisecond,
	})

	result, err := registrar.Register(context.Background(), Request{AgentID: "agent", APIKey: "key"})
	if err == nil {
		t.Fatalf("result = %+v", result)
	}
	if device.Load() != nil {
		t.Fatal("credentials were stored after a failed token poll")
	}
	if code := registrar.ActivationCode(); code != "1234" {
		t.Fatalf("activation code = %q", code)
	}
}
