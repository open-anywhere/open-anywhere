package render

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	api "github.com/open-anywhere/open-anywhere/pkg/render"
)

const (
	helperEnabled = "RENDER_ADVERSARIAL_HELPER"
	helperMode    = "RENDER_ADVERSARIAL_HELPER_MODE"
)

// TestRenderAdversarialHelperProcess is re-executed as a stand-in for the Node
// application. The outer test run returns immediately; fake node scripts set
// the helper environment variables and select this test by name.
func TestRenderAdversarialHelperProcess(t *testing.T) {
	if os.Getenv(helperEnabled) != "1" {
		return
	}
	mode := os.Getenv(helperMode)
	if mode == "exit" {
		leaveChildInGroup()
		os.Exit(1)
	}
	if mode == "nofinish" {
		// A long sleep keeps the runtime deadlock detector quiet while the
		// process deliberately never binds or answers.
		time.Sleep(24 * time.Hour)
	}
	host := os.Getenv("HOST")
	if host == "" {
		host = "0.0.0.0"
	}
	port := os.Getenv("PORT")
	ln, err := net.Listen("tcp4", host+":"+port)
	if err != nil {
		os.Exit(2)
	}
	var degraded atomic.Bool
	var pageOnce atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if mode == "badhealth" || degraded.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"status":"bad"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if mode == "badpage" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{}`)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "<html><body>ok</body></html>")
		if !pageOnce.CompareAndSwap(false, true) {
			return
		}
		switch mode {
		case "degrade":
			time.AfterFunc(300*time.Millisecond, func() { degraded.Store(true) })
		case "exitlater":
			time.AfterFunc(500*time.Millisecond, func() {
				leaveChildInGroup()
				os.Exit(0)
			})
		}
	})
	if err := http.Serve(ln, mux); err != nil {
		os.Exit(3)
	}
}

// leaveChildInGroup keeps the process group alive after the main helper exits
// so shutdown exercises the "parent exited before its children" path instead of
// signalling a vanished group. The child ignores SIGTERM so neither signal sent
// by stopProcess can race with the group disappearing.
func leaveChildInGroup() {
	cmd := exec.Command("/bin/sh", "-c", "trap '' TERM; exec sleep 3")
	_ = cmd.Start()
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func writeExecutable(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	return p
}

func fakeNode(t *testing.T, mode string) string {
	t.Helper()
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("#!/bin/sh\n%s=1 %s=%s exec %s -test.run '^TestRenderAdversarialHelperProcess$'\n",
		helperEnabled, helperMode, shellQuote(mode), shellQuote(bin))
	return writeExecutable(t, "node-"+mode, body)
}

const (
	ffmpegStream  = "stream"
	ffmpegShort   = "short"
	ffmpegSilent  = "silent"
	ffmpegNoStdin = "nostdin"
	ffmpegPartial = "partial"
	ffmpegFail    = "fail"
	ffmpegSlow    = "slow"
)

func fakeFFmpeg(t *testing.T, mode string) string {
	t.Helper()
	var body string
	switch mode {
	case ffmpegStream:
		body = "#!/bin/sh\nhead -c 1316 /dev/zero\nsleep 30\n"
	case ffmpegShort:
		body = "#!/bin/sh\nhead -c 1316 /dev/zero\nsleep 0.2\nexit 0\n"
	case ffmpegSilent:
		body = "#!/bin/sh\nsleep 30\n"
	case ffmpegNoStdin:
		body = "#!/bin/sh\nsleep 0.3\nexec 0<&-\nsleep 30\n"
	case ffmpegPartial:
		body = "#!/bin/sh\nprintf abc\nexit 0\n"
	case ffmpegFail:
		body = "#!/bin/sh\nexit 1\n"
	case ffmpegSlow:
		body = "#!/bin/sh\nsleep 2\nhead -c 1316 /dev/zero\nsleep 30\n"
	default:
		body = "#!/bin/sh\nexit 1\n"
	}
	return writeExecutable(t, "ffmpeg-"+mode, body)
}

func fakeBrowserHooks() browserHooks {
	return browserHooks{
		start: func(ctx context.Context, _ Config) (context.Context, context.CancelFunc, context.CancelFunc) {
			return ctx, func() {}, func() {}
		},
		run:     func(context.Context, ...chromedp.Action) error { return nil },
		capture: func(context.Context) ([]byte, error) { return []byte("frame"), nil },
		layout: func(context.Context) (layoutMetrics, error) {
			return layoutMetrics{Width: 1280, Height: 720, ViewportWidth: 1280, ViewportHeight: 720}, nil
		},
	}
}

const validOpenAPI = `openapi: 3.1.0
info: {title: adversarial, version: '1'}
paths:
  /api/state:
    get: {operationId: state, description: state, responses: {'200': {description: ok}}}
`

func validLaunch(readyMS int) string {
	if readyMS == 0 {
		readyMS = 5000
	}
	return fmt.Sprintf(`{"name":"adversarial","runtimeExecutable":"node","runtimeArgs":["app/app.js"],"readyTimeoutMs":%d}`, readyMS)
}

// standardArtifact returns a gzip tar that passes validateArtifact.
func standardArtifact(t *testing.T, launchJSON string) []byte {
	t.Helper()
	if launchJSON == "" {
		launchJSON = validLaunch(5000)
	}
	files := map[string]string{
		"launch.json":           launchJSON,
		"openapi.yaml":          validOpenAPI,
		"app/app.js":            "// fake application",
		"app/package.json":      "{}",
		"app/public/index.html": "<html></html>",
	}
	return tarBytes(t, files)
}

func tarBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		h := &tar.Header{Name: name, Mode: 0600, Size: int64(len(content)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func artifactServer(t *testing.T, payloads map[string][]byte) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		b, ok := payloads[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	})
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

type fakeServiceOptions struct {
	nodeMode      string
	ffmpegMode    string
	ffmpegPath    string
	startup       time.Duration
	workDir       string
	ports         []int
	browser       browserHooks
	stopGroup     func(*exec.Cmd, <-chan struct{}) error
	afterDownload func(dir string)
	afterUnpack   func(dir, appDir string)
	afterMedia    func(*Service, *run)
	beforeReady   func(*Service, *run)
}

func newFakeService(t *testing.T, opts fakeServiceOptions) *Service {
	t.Helper()
	if opts.nodeMode == "" {
		opts.nodeMode = "healthy"
	}
	if opts.ffmpegPath == "" {
		if opts.ffmpegMode == "" {
			opts.ffmpegMode = ffmpegStream
		}
		opts.ffmpegPath = fakeFFmpeg(t, opts.ffmpegMode)
	}
	if opts.startup == 0 {
		opts.startup = 15 * time.Second
	}
	if opts.workDir == "" {
		opts.workDir = t.TempDir()
	}
	if opts.browser.start == nil {
		opts.browser = fakeBrowserHooks()
	}
	s, err := New(Config{
		WorkDir:        opts.workDir,
		PublicURL:      "http://127.0.0.1:8090",
		Node:           fakeNode(t, opts.nodeMode),
		FFmpeg:         opts.ffmpegPath,
		StartupTimeout: opts.startup,
		ServicePorts:   opts.ports,
		Logger:         log.New(io.Discard, "", 0),
		browser:        opts.browser,
		stopGroup:      opts.stopGroup,
		afterDownload:  opts.afterDownload,
		afterUnpack:    opts.afterUnpack,
		afterMedia:     opts.afterMedia,
		beforeReady:    opts.beforeReady,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = s.Close(ctx)
	})
	return s
}

// useEntropyFailure makes Create's random instance id generation fail.
func useEntropyFailure(s *Service) {
	s.randRead = func([]byte) (int, error) { return 0, fmt.Errorf("entropy source failed") }
}

func restoreEntropy(s *Service) {
	s.randRead = rand.Read
}

func waitInstanceState(t *testing.T, s *Service, id, state string, timeout time.Duration) api.Instance {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last api.Instance
	for {
		v, ok := s.Get(id)
		if ok {
			last = v
			if v.State == state {
				return v
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("instance %s never reached %s: %+v", id, state, last)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitStopRequested polls until Stop has flipped the run's stop flag (and
// therefore canceled its context), or the deadline expires.
func waitStopRequested(s *Service, r *run, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		requested := r.stopRequested
		s.mu.Unlock()
		if requested {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return false
}

type createResult struct {
	instance api.Instance
	failure  *Failure
}

func createAsync(s *Service, req api.CreateRequest) <-chan createResult {
	out := make(chan createResult, 1)
	go func() {
		v, e := s.Create(context.Background(), req)
		out <- createResult{v, e}
	}()
	return out
}

func waitCreate(t *testing.T, ch <-chan createResult, timeout time.Duration) createResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(timeout):
		t.Fatal("Create did not return")
		return createResult{}
	}
}

func fakeLsof(t *testing.T, body string) {
	t.Helper()
	p := writeExecutable(t, "lsof", "#!/bin/sh\n"+body+"\n")
	t.Setenv("PATH", filepath.Dir(p)+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func mustChmod(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}
