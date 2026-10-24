package register

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/voiceentry/linx"
)

// platform is a fake Linx platform. otaBody is returned for every OTA report,
// bindBody for every bind request; token switches the OTA answer to bound.
type platform struct {
	srv       *httptest.Server
	otaBody   atomic.Value // string
	bindBody  atomic.Value // string
	token     atomic.Bool
	otaStatus atomic.Int32
	reports   atomic.Int32
}

func newPlatform(t *testing.T) *platform {
	t.Helper()
	p := &platform{}
	p.otaBody.Store(`{"activation":{"code":"123456"},"websocket":{"url":"ws://x"}}`)
	p.bindBody.Store(`{"code":0}`)
	p.otaStatus.Store(200)
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/ota") {
			p.reports.Add(1)
			w.WriteHeader(int(p.otaStatus.Load()))
			if p.token.Load() {
				_, _ = w.Write([]byte(`{"websocket":{"url":"ws://x","token":"tok"}}`))
				return
			}
			_, _ = w.Write([]byte(p.otaBody.Load().(string)))
			return
		}
		_, _ = w.Write([]byte(p.bindBody.Load().(string)))
	}))
	t.Cleanup(p.srv.Close)
	return p
}

func (p *platform) registrar(t *testing.T, apply func() error) *Registrar {
	t.Helper()
	return New(Config{
		Path:   filepath.Join(t.TempDir(), "device.json"),
		OTA:    linx.OTAClient{BaseURL: p.srv.URL + "/v1/ota", APIBase: p.srv.URL},
		Device: &atomic.Pointer[linx.Device]{},
		Poll:   time.Millisecond,
		Apply:  apply,
	})
}

func TestNewDefaultsThePollInterval(t *testing.T) {
	if got := New(Config{Device: &atomic.Pointer[linx.Device]{}}).cfg.Poll; got != 2*time.Second {
		t.Fatalf("poll=%s", got)
	}
}

func TestRegisterReportsOTAFailures(t *testing.T) {
	p := newPlatform(t)
	p.otaStatus.Store(500)
	r := p.registrar(t, nil)
	if _, err := r.Register(context.Background(), Request{}); err == nil {
		t.Fatal("OTA failure ignored")
	}
	if r.Registered() {
		t.Fatal("registered after an OTA failure")
	}
}

func TestRegisterFailsWhenPlatformGivesNeitherCodeNorToken(t *testing.T) {
	p := newPlatform(t)
	p.otaBody.Store(`{}`)
	if _, err := p.registrar(t, nil).Register(context.Background(), Request{}); err == nil || !strings.Contains(err.Error(), "激活码") {
		t.Fatalf("err=%v", err)
	}
}

func TestRegisterKeepsTheCodeWhenBindingFails(t *testing.T) {
	p := newPlatform(t)
	p.bindBody.Store(`{"code":409,"msg":"already bound"}`)
	r := p.registrar(t, nil)
	res, err := r.Register(context.Background(), Request{AgentID: "a", APIKey: "k"})
	if err == nil || res.Status != "pending" || res.Code != "123456" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if r.ActivationCode() != "123456" {
		t.Fatalf("code=%q", r.ActivationCode())
	}
}

func TestRegisterStaysPendingWhenTheTokenNeverArrives(t *testing.T) {
	p := newPlatform(t)
	r := p.registrar(t, nil)
	res, err := r.Register(context.Background(), Request{AgentID: "a", APIKey: "k"})
	if err != nil || res.Status != "pending" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if got := p.reports.Load(); got != 6 { // the first report plus five polls
		t.Fatalf("reports=%d", got)
	}
}

func TestWaitForTokenStopsOnCancelAndOnOTAErrors(t *testing.T) {
	p := newPlatform(t)
	r := p.registrar(t, nil)
	r.cfg.Poll = time.Hour
	r.pending = &linx.Device{MAC: "m", ClientID: "c"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.waitForToken(ctx, linx.OTAResponse{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	r.cfg.Poll = time.Millisecond
	p.otaStatus.Store(500)
	if _, err := r.waitForToken(context.Background(), linx.OTAResponse{}); err == nil {
		t.Fatal("OTA error during polling ignored")
	}
}

func TestRegisterReportsStorageAndConnectFailures(t *testing.T) {
	p := newPlatform(t)
	p.token.Store(true)
	r := p.registrar(t, nil)
	r.cfg.Path = filepath.Join(t.TempDir(), "missing-dir", "device.json")
	if _, err := r.Register(context.Background(), Request{}); err == nil || !strings.Contains(err.Error(), "保存凭证失败") {
		t.Fatalf("err=%v", err)
	}
	if r.Registered() {
		t.Fatal("registered although the credentials were not saved")
	}

	r = p.registrar(t, func() error { return errors.New("dial failed") })
	res, err := r.Register(context.Background(), Request{})
	if err == nil || res.Status != "registered" || !r.Registered() {
		t.Fatalf("res=%+v err=%v registered=%v", res, err, r.Registered())
	}
}
