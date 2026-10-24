package render

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func mediaTestConfig(t *testing.T, ffmpegMode string, hooks browserHooks) Config {
	t.Helper()
	return Config{
		Width:   1280,
		Height:  720,
		FPS:     10,
		FFmpeg:  fakeFFmpeg(t, ffmpegMode),
		browser: hooks,
	}
}

func TestAdversarialDefaultBrowserHooksBuildAllocator(t *testing.T) {
	cfg := Config{Width: 640, Height: 480, Chrome: "/custom/chrome"}
	hooks := cfg.browserHooks()
	if hooks.start == nil || hooks.run == nil || hooks.capture == nil {
		t.Fatal("default hooks are incomplete")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// NewExecAllocator/NewContext are lazy: no Chrome process is started here.
	_, browserCancel, allocatorCancel := hooks.start(ctx, cfg)
	browserCancel()
	allocatorCancel()
}

func TestAdversarialStartMediaRequiresBrowser(t *testing.T) {
	hooks := fakeBrowserHooks()
	hooks.run = func(context.Context, ...chromedp.Action) error { return errors.New("browser unavailable") }
	cfg := mediaTestConfig(t, ffmpegStream, hooks)
	m, err := startMedia(context.Background(), context.Background(), cfg, "http://127.0.0.1:1/", filepath.Join(t.TempDir(), "encoder.log"), newHub())
	if err == nil || !strings.Contains(err.Error(), "load browser page") {
		t.Fatalf("expected browser failure, got %v", err)
	}
	if err := m.close(); err != nil {
		t.Fatalf("close after browser failure: %v", err)
	}
}

func TestAdversarialStartMediaLogPathFailure(t *testing.T) {
	cfg := mediaTestConfig(t, ffmpegStream, fakeBrowserHooks())
	h := newHub()
	m, err := startMedia(context.Background(), context.Background(), cfg, "http://127.0.0.1:1/", filepath.Join(t.TempDir(), "missing", "encoder.log"), h)
	if err == nil {
		t.Fatal("expected log creation failure")
	}
	if err := m.close(); err != nil {
		t.Fatalf("close after log failure: %v", err)
	}
}

func TestAdversarialStartMediaEncoderFailures(t *testing.T) {
	t.Run("missing executable", func(t *testing.T) {
		cfg := mediaTestConfig(t, ffmpegStream, fakeBrowserHooks())
		cfg.FFmpeg = "/no-such-ffmpeg-adversarial"
		m, err := startMedia(context.Background(), context.Background(), cfg, "http://127.0.0.1:1/", filepath.Join(t.TempDir(), "encoder.log"), newHub())
		if err == nil {
			t.Fatal("expected start failure")
		}
		if err := m.close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	})

	t.Run("encoder exits immediately", func(t *testing.T) {
		cfg := mediaTestConfig(t, ffmpegFail, fakeBrowserHooks())
		m, err := startMedia(context.Background(), context.Background(), cfg, "http://127.0.0.1:1/", filepath.Join(t.TempDir(), "encoder.log"), newHub())
		if err == nil {
			t.Fatal("expected encoder failure")
		}
		if err := m.close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	})

	t.Run("capture failure", func(t *testing.T) {
		hooks := fakeBrowserHooks()
		hooks.capture = func(context.Context) ([]byte, error) { return nil, errors.New("capture failed") }
		cfg := mediaTestConfig(t, ffmpegSilent, hooks)
		m, err := startMedia(context.Background(), context.Background(), cfg, "http://127.0.0.1:1/", filepath.Join(t.TempDir(), "encoder.log"), newHub())
		if err == nil || !strings.Contains(err.Error(), "capture") {
			t.Fatalf("expected capture failure, got %v", err)
		}
		if err := m.close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	})

	t.Run("encoder stdin closed", func(t *testing.T) {
		cfg := mediaTestConfig(t, ffmpegNoStdin, fakeBrowserHooks())
		m, err := startMedia(context.Background(), context.Background(), cfg, "http://127.0.0.1:1/", filepath.Join(t.TempDir(), "encoder.log"), newHub())
		if err == nil {
			t.Fatal("expected stdin write failure")
		}
		if err := m.close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	})

	t.Run("partial packet then EOF", func(t *testing.T) {
		cfg := mediaTestConfig(t, ffmpegPartial, fakeBrowserHooks())
		h := newHub()
		m, _ := startMedia(context.Background(), context.Background(), cfg, "http://127.0.0.1:1/", filepath.Join(t.TempDir(), "encoder.log"), h)
		// The partial packet may win the race with the failure report; either way
		// the encoder must be torn down cleanly.
		if m == nil {
			t.Fatal("media was not returned")
		}
		if err := m.close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	})
}

func TestAdversarialStartMediaDeadlines(t *testing.T) {
	t.Run("startup deadline", func(t *testing.T) {
		cfg := mediaTestConfig(t, ffmpegSilent, fakeBrowserHooks())
		startup, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		defer cancel()
		m, err := startMedia(context.Background(), startup, cfg, "http://127.0.0.1:1/", filepath.Join(t.TempDir(), "encoder.log"), newHub())
		if err == nil {
			t.Fatal("expected startup deadline")
		}
		if err := m.close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	})

	t.Run("life cancellation", func(t *testing.T) {
		cfg := mediaTestConfig(t, ffmpegSilent, fakeBrowserHooks())
		life, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(150 * time.Millisecond)
			cancel()
		}()
		m, err := startMedia(life, context.Background(), cfg, "http://127.0.0.1:1/", filepath.Join(t.TempDir(), "encoder.log"), newHub())
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancellation, got %v", err)
		}
		cancel()
		if err := m.close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	})
}

func TestAdversarialMediaSuccessAndClose(t *testing.T) {
	cfg := mediaTestConfig(t, ffmpegStream, fakeBrowserHooks())
	h := newHub()
	logPath := filepath.Join(t.TempDir(), "encoder.log")
	m, err := startMedia(context.Background(), context.Background(), cfg, "http://127.0.0.1:1/", logPath, h)
	if err != nil {
		t.Fatalf("startMedia: %v", err)
	}
	select {
	case <-h.ready:
	default:
		t.Fatal("hub never became ready")
	}
	if err := m.close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("encoder log missing: %v", err)
	}
}

func TestAdversarialMediaCloseWithoutProcess(t *testing.T) {
	m := &media{hub: newHub(), cancel: func() {}}
	if err := m.close(); err != nil {
		t.Fatalf("close without process: %v", err)
	}
}

func TestAdversarialMediaFailIsNonBlocking(t *testing.T) {
	m := &media{failure: make(chan error, 1)}
	first := errors.New("first")
	m.fail(first)
	m.fail(errors.New("second")) // dropped, must not block
	select {
	case err := <-m.failure:
		if !errors.Is(err, first) {
			t.Fatalf("unexpected failure: %v", err)
		}
	default:
		t.Fatal("failure was not recorded")
	}
}

func TestAdversarialMediaCloseReportsStopError(t *testing.T) {
	m := &media{
		hub:         newHub(),
		cancel:      func() {},
		processDone: make(chan struct{}),
		failure:     make(chan error, 1),
		stopProc: func(*exec.Cmd, <-chan struct{}) error {
			return errors.New("kill failed")
		},
	}
	if err := m.close(); err == nil {
		t.Fatal("expected stop error")
	}
}
