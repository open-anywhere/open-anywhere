package render

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
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

func TestAdversarialDefaultCaptureRejectsInvalidContext(t *testing.T) {
	hooks := Config{}.browserHooks()
	frame, err := hooks.capture(context.Background())
	if !errors.Is(err, chromedp.ErrInvalidContext) {
		t.Fatalf("expected invalid chromedp context, got %v", err)
	}
	if frame != nil {
		t.Fatalf("capture returned a frame for an invalid context: %d bytes", len(frame))
	}
}

// exhaustFileDescriptors opens descriptors until the process runs out, then
// releases exactly free of them. The returned function releases the rest.
func exhaustFileDescriptors(t *testing.T, free int) func() {
	t.Helper()
	const maxProbe = 200000
	var held []*os.File
	for len(held) < maxProbe {
		f, err := os.Open(os.DevNull)
		if err != nil {
			if !errors.Is(err, syscall.EMFILE) {
				for _, h := range held {
					_ = h.Close()
				}
				t.Fatalf("open /dev/null: %v", err)
			}
			break
		}
		held = append(held, f)
	}
	if len(held) == maxProbe {
		for _, h := range held {
			_ = h.Close()
		}
		t.Fatalf("could not exhaust descriptors within %d opens", maxProbe)
	}
	if len(held) < free {
		for _, h := range held {
			_ = h.Close()
		}
		t.Fatalf("only %d descriptors were available, need %d", len(held), free)
	}
	for i := 0; i < free; i++ {
		last := held[len(held)-1]
		held = held[:len(held)-1]
		_ = last.Close()
	}
	return func() {
		for _, h := range held {
			_ = h.Close()
		}
	}
}

func TestAdversarialStartMediaStdinPipeExhaustion(t *testing.T) {
	cfg := mediaTestConfig(t, ffmpegStream, fakeBrowserHooks())
	logPath := filepath.Join(t.TempDir(), "encoder.log")
	// Leave a single descriptor: log creation consumes it, then StdinPipe
	// cannot allocate the pipe pair.
	release := exhaustFileDescriptors(t, 1)
	defer release()
	m, err := startMedia(context.Background(), context.Background(), cfg, "http://127.0.0.1:1/", logPath, newHub())
	if err == nil {
		t.Fatal("expected stdin pipe creation to fail with exhausted descriptors")
	}
	if !errors.Is(err, syscall.EMFILE) {
		t.Fatalf("expected EMFILE from StdinPipe, got %v", err)
	}
	if m == nil || m.cmd == nil || m.cmd.Process != nil {
		t.Fatalf("media was not left in a pre-start state: %+v", m)
	}
	if cerr := m.close(); cerr != nil {
		t.Fatalf("close: %v", cerr)
	}
}

func TestAdversarialStartMediaStdoutPipeExhaustion(t *testing.T) {
	cfg := mediaTestConfig(t, ffmpegStream, fakeBrowserHooks())
	logPath := filepath.Join(t.TempDir(), "encoder.log")
	// Three descriptors cover log creation plus the stdin pipe pair, leaving
	// nothing for the stdout pipe.
	release := exhaustFileDescriptors(t, 3)
	defer release()
	m, err := startMedia(context.Background(), context.Background(), cfg, "http://127.0.0.1:1/", logPath, newHub())
	if err == nil {
		t.Fatal("expected stdout pipe creation to fail with exhausted descriptors")
	}
	if !errors.Is(err, syscall.EMFILE) {
		t.Fatalf("expected EMFILE from StdoutPipe, got %v", err)
	}
	if m == nil || m.cmd == nil || m.cmd.Process != nil {
		t.Fatalf("media was not left in a pre-start state: %+v", m)
	}
	if cerr := m.close(); cerr != nil {
		t.Fatalf("close: %v", cerr)
	}
}

type screenshotExecutor struct {
	command string
	params  *page.CaptureScreenshotParams
	data    []byte
	err     error
}

func (e *screenshotExecutor) Execute(_ context.Context, method string, params, res any) error {
	e.command = method
	if p, ok := params.(*page.CaptureScreenshotParams); ok {
		e.params = p
	}
	if e.err != nil {
		return e.err
	}
	if r, ok := res.(*page.CaptureScreenshotReturns); ok {
		r.Data = base64.StdEncoding.EncodeToString(e.data)
	}
	return nil
}

func TestAdversarialCaptureScreenshot(t *testing.T) {
	exec := &screenshotExecutor{data: []byte("jpeg-frame")}
	var frame []byte
	ctx := cdp.WithExecutor(context.Background(), exec)
	if err := captureScreenshot(ctx, &frame); err != nil {
		t.Fatalf("capture screenshot: %v", err)
	}
	if exec.command != page.CommandCaptureScreenshot {
		t.Fatalf("unexpected DevTools command: %q", exec.command)
	}
	if exec.params == nil || exec.params.Format != page.CaptureScreenshotFormatJpeg || exec.params.Quality != 85 {
		t.Fatalf("unexpected screenshot params: %+v", exec.params)
	}
	if string(frame) != "jpeg-frame" {
		t.Fatalf("unexpected frame: %q", frame)
	}
}

func TestAdversarialCaptureScreenshotDevToolsError(t *testing.T) {
	exec := &screenshotExecutor{err: errors.New("devtools unavailable")}
	var frame []byte
	err := captureScreenshot(cdp.WithExecutor(context.Background(), exec), &frame)
	if err == nil || !strings.Contains(err.Error(), "devtools unavailable") {
		t.Fatalf("expected DevTools error, got %v", err)
	}
	if frame != nil {
		t.Fatalf("frame was set despite the error: %q", frame)
	}
}

func TestLayoutOverflowIsLoggedWithoutStoppingStream(t *testing.T) {
	hooks := fakeBrowserHooks()
	hooks.layout = func(context.Context) (layoutMetrics, error) {
		return layoutMetrics{Width: 1440, Height: 900, ViewportWidth: 1280, ViewportHeight: 720}, nil
	}
	var logs bytes.Buffer
	cfg := mediaTestConfig(t, ffmpegStream, hooks)
	cfg.Logger = log.New(&logs, "", 0)
	m, err := startMedia(context.Background(), context.Background(), cfg, "http://127.0.0.1:1/", filepath.Join(t.TempDir(), "encoder.log"), newHub())
	if err != nil {
		t.Fatalf("start media: %v", err)
	}
	if err := m.close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	for _, want := range []string{"layout_overflow", "content=1440x900", "viewport=1280x720", "canvas=1280x720"} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("overflow log does not contain %q: %q", want, logs.String())
		}
	}
}

func TestLayoutWithinViewportIsNotLogged(t *testing.T) {
	hooks := fakeBrowserHooks()
	hooks.layout = func(context.Context) (layoutMetrics, error) {
		return layoutMetrics{Width: 1280, Height: 720, ViewportWidth: 1280, ViewportHeight: 720}, nil
	}
	var logs bytes.Buffer
	cfg := mediaTestConfig(t, ffmpegStream, hooks)
	cfg.Logger = log.New(&logs, "", 0)
	m, err := startMedia(context.Background(), context.Background(), cfg, "http://127.0.0.1:1/", filepath.Join(t.TempDir(), "encoder.log"), newHub())
	if err != nil {
		t.Fatalf("start media: %v", err)
	}
	if err := m.close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if strings.Contains(logs.String(), "layout_overflow") {
		t.Fatalf("unexpected overflow log: %q", logs.String())
	}
}
