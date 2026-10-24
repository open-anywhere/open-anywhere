package render

import (
	"context"
	"errors"
	"fmt"
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

	api "github.com/open-anywhere/open-anywhere/pkg/render"
)

func TestAdversarialExecuteSuccessAndStop(t *testing.T) {
	payload := standardArtifact(t, validLaunch(5000))
	files := artifactServer(t, map[string][]byte{"/artifact.tar.gz": payload})
	s := newFakeService(t, fakeServiceOptions{})
	port := availableTestPort(t)
	v, ferr := s.Create(context.Background(), api.CreateRequest{AppID: "adv-ok", ArtifactURL: files.URL + "/artifact.tar.gz", Port: port})
	if ferr != nil {
		t.Fatalf("create: %+v", ferr)
	}
	if v.State != "ready" || v.AppBaseURL == "" || v.Source == nil || v.Source.Format != "mpegts" {
		t.Fatalf("bad ready instance: %+v", v)
	}
	if v.AppBaseURL != fmt.Sprintf("http://127.0.0.1:%d", port) {
		t.Fatalf("unexpected app base url: %s", v.AppBaseURL)
	}
	// Let the one-second health ticker run at least once while healthy.
	time.Sleep(1200 * time.Millisecond)
	if got, ok := s.Get(v.InstanceID); !ok || got.State != "ready" {
		t.Fatalf("instance did not stay ready: %+v", got)
	}

	stopped, ferr := s.Stop(context.Background(), v.InstanceID)
	if ferr != nil {
		t.Fatalf("stop: %+v", ferr)
	}
	if stopped.State != "stopped" || stopped.Source != nil || stopped.AppBaseURL != "" || stopped.Error != nil {
		t.Fatalf("bad stopped instance: %+v", stopped)
	}
	p, perr := s.Port(port)
	if perr != nil || p.Occupied {
		t.Fatalf("port leaked: %+v %v", p, perr)
	}
	// Repeated stop is idempotent.
	again, ferr := s.Stop(context.Background(), v.InstanceID)
	if ferr != nil || again.State != "stopped" {
		t.Fatalf("second stop: %+v %+v", again, ferr)
	}
	entries, err := os.ReadDir(s.cfg.WorkDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("work dir leaked: %v %v", entries, err)
	}
}

func TestAdversarialStartupFailures(t *testing.T) {
	t.Run("startup deadline overrides download error", func(t *testing.T) {
		blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}))
		defer blocked.Close()
		s := newFakeService(t, fakeServiceOptions{startup: time.Second})
		_, ferr := s.Create(context.Background(), api.CreateRequest{AppID: "slow-download", ArtifactURL: blocked.URL, Port: availableTestPort(t)})
		if ferr == nil || ferr.Detail.Code != "START_TIMEOUT" || ferr.HTTP != 504 {
			t.Fatalf("expected startup timeout, got %+v", ferr)
		}
	})

	t.Run("health readiness timeout", func(t *testing.T) {
		payload := standardArtifact(t, validLaunch(300))
		files := artifactServer(t, map[string][]byte{"/a.tar.gz": payload})
		s := newFakeService(t, fakeServiceOptions{nodeMode: "nofinish"})
		v, ferr := s.Create(context.Background(), api.CreateRequest{AppID: "no-health", ArtifactURL: files.URL + "/a.tar.gz", Port: availableTestPort(t)})
		if ferr == nil || ferr.Detail.Code != "START_TIMEOUT" || ferr.HTTP != 504 || v.State != "error" {
			t.Fatalf("expected health timeout, got %+v %+v", v, ferr)
		}
	})

	t.Run("application exits before healthy", func(t *testing.T) {
		payload := standardArtifact(t, validLaunch(5000))
		files := artifactServer(t, map[string][]byte{"/a.tar.gz": payload})
		s := newFakeService(t, fakeServiceOptions{nodeMode: "exit"})
		_, ferr := s.Create(context.Background(), api.CreateRequest{AppID: "exits", ArtifactURL: files.URL + "/a.tar.gz", Port: availableTestPort(t)})
		if ferr == nil || ferr.Detail.Code != "APP_HEALTH_FAILED" || ferr.HTTP != 502 {
			t.Fatalf("expected health failure, got %+v", ferr)
		}
	})

	t.Run("page content type rejected", func(t *testing.T) {
		payload := standardArtifact(t, validLaunch(5000))
		files := artifactServer(t, map[string][]byte{"/a.tar.gz": payload})
		s := newFakeService(t, fakeServiceOptions{nodeMode: "badpage"})
		_, ferr := s.Create(context.Background(), api.CreateRequest{AppID: "bad-page", ArtifactURL: files.URL + "/a.tar.gz", Port: availableTestPort(t)})
		if ferr == nil || ferr.Detail.Code != "PAGE_LOAD_FAILED" || ferr.HTTP != 502 {
			t.Fatalf("expected page failure, got %+v", ferr)
		}
	})

	t.Run("encoder cannot start", func(t *testing.T) {
		payload := standardArtifact(t, validLaunch(5000))
		files := artifactServer(t, map[string][]byte{"/a.tar.gz": payload})
		s := newFakeService(t, fakeServiceOptions{ffmpegPath: "/no-such-ffmpeg-adversarial"})
		_, ferr := s.Create(context.Background(), api.CreateRequest{AppID: "no-encoder", ArtifactURL: files.URL + "/a.tar.gz", Port: availableTestPort(t)})
		if ferr == nil || ferr.Detail.Code != "STREAM_START_FAILED" || ferr.HTTP != 502 {
			t.Fatalf("expected encoder failure, got %+v", ferr)
		}
	})

	t.Run("invalid artifact", func(t *testing.T) {
		files := artifactServer(t, map[string][]byte{"/a.tar.gz": tarBytes(t, map[string]string{"readme": "no app"})})
		s := newFakeService(t, fakeServiceOptions{})
		_, ferr := s.Create(context.Background(), api.CreateRequest{AppID: "bad-artifact", ArtifactURL: files.URL + "/a.tar.gz", Port: availableTestPort(t)})
		if ferr == nil || ferr.Detail.Code != "INVALID_ARTIFACT" || ferr.HTTP != 422 {
			t.Fatalf("expected invalid artifact, got %+v", ferr)
		}
	})

	t.Run("work directory vanished", func(t *testing.T) {
		work := t.TempDir()
		s := newFakeService(t, fakeServiceOptions{workDir: work})
		if err := os.RemoveAll(work); err != nil {
			t.Fatal(err)
		}
		_, ferr := s.Create(context.Background(), api.CreateRequest{AppID: "no-workdir", ArtifactURL: "http://127.0.0.1:1/a.tar.gz", Port: availableTestPort(t)})
		if ferr == nil || ferr.Detail.Code != "APP_START_FAILED" || ferr.HTTP != 500 {
			t.Fatalf("expected mkdir failure, got %+v", ferr)
		}
	})

	t.Run("port squatted after reservation", func(t *testing.T) {
		payload := standardArtifact(t, validLaunch(5000))
		entered := make(chan struct{})
		release := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-entered:
			default:
				close(entered)
			}
			<-release
			_, _ = w.Write(payload)
		}))
		defer server.Close()
		s := newFakeService(t, fakeServiceOptions{nodeMode: "healthy"})
		port := availableTestPort(t)
		result := createAsync(s, api.CreateRequest{AppID: "squat", ArtifactURL: server.URL + "/a.tar.gz", Port: port})
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("artifact download did not start")
		}
		ln, err := net.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", port))
		if err != nil {
			close(release)
			t.Fatalf("squat port: %v", err)
		}
		close(release)
		r := waitCreate(t, result, 10*time.Second)
		_ = ln.Close()
		if r.failure == nil || r.failure.Detail.Code != "PORT_IN_USE" || r.failure.HTTP != 409 {
			t.Fatalf("expected PORT_IN_USE from health probe, got %+v", r.failure)
		}
	})
}

func TestAdversarialRuntimeFailures(t *testing.T) {
	t.Run("application exits while running", func(t *testing.T) {
		payload := standardArtifact(t, validLaunch(5000))
		files := artifactServer(t, map[string][]byte{"/a.tar.gz": payload})
		s := newFakeService(t, fakeServiceOptions{nodeMode: "exitlater"})
		v, ferr := s.Create(context.Background(), api.CreateRequest{AppID: "exits-later", ArtifactURL: files.URL + "/a.tar.gz", Port: availableTestPort(t)})
		if ferr != nil {
			t.Fatalf("create: %+v", ferr)
		}
		got := waitInstanceState(t, s, v.InstanceID, "error", 10*time.Second)
		if got.Error == nil || got.Error.Code != "APP_EXITED" || got.Error.Stage != "running" {
			t.Fatalf("unexpected runtime error: code=%q stage=%q msg=%q", got.Error.Code, got.Error.Stage, got.Error.Message)
		}
	})

	t.Run("encoder fails while running", func(t *testing.T) {
		payload := standardArtifact(t, validLaunch(5000))
		files := artifactServer(t, map[string][]byte{"/a.tar.gz": payload})
		var captures atomic.Int32
		hooks := fakeBrowserHooks()
		hooks.capture = func(context.Context) ([]byte, error) {
			if captures.Add(1) <= 3 {
				return []byte("frame"), nil
			}
			return nil, errors.New("synthetic capture failure")
		}
		s := newFakeService(t, fakeServiceOptions{ffmpegMode: ffmpegStream, browser: hooks})
		v, ferr := s.Create(context.Background(), api.CreateRequest{AppID: "encoder-exits", ArtifactURL: files.URL + "/a.tar.gz", Port: availableTestPort(t)})
		if ferr != nil {
			t.Fatalf("create: %+v", ferr)
		}
		got := waitInstanceState(t, s, v.InstanceID, "error", 10*time.Second)
		if got.Error == nil || got.Error.Code != "STREAM_FAILED" || got.Error.Stage != "running" {
			t.Fatalf("unexpected stream error: code=%q stage=%q msg=%q", got.Error.Code, got.Error.Stage, got.Error.Message)
		}
	})

	t.Run("stalled encoder output", func(t *testing.T) {
		payload := standardArtifact(t, validLaunch(5000))
		files := artifactServer(t, map[string][]byte{"/a.tar.gz": payload})
		s := newFakeService(t, fakeServiceOptions{ffmpegMode: ffmpegStream})
		v, ferr := s.Create(context.Background(), api.CreateRequest{AppID: "stalled", ArtifactURL: files.URL + "/a.tar.gz", Port: availableTestPort(t)})
		if ferr != nil {
			t.Fatalf("create: %+v", ferr)
		}
		s.mu.Lock()
		r := s.instances[v.InstanceID]
		s.mu.Unlock()
		r.hub.mu.Lock()
		r.hub.last = time.Now().Add(-10 * time.Second)
		r.hub.mu.Unlock()
		got := waitInstanceState(t, s, v.InstanceID, "error", 10*time.Second)
		if got.Error == nil || got.Error.Code != "STREAM_FAILED" || !strings.Contains(got.Error.Message, "stalled") {
			t.Fatalf("unexpected stall error: %+v", got)
		}
	})

	t.Run("three failed health checks", func(t *testing.T) {
		payload := standardArtifact(t, validLaunch(5000))
		files := artifactServer(t, map[string][]byte{"/a.tar.gz": payload})
		s := newFakeService(t, fakeServiceOptions{nodeMode: "degrade"})
		v, ferr := s.Create(context.Background(), api.CreateRequest{AppID: "unhealthy", ArtifactURL: files.URL + "/a.tar.gz", Port: availableTestPort(t)})
		if ferr != nil {
			t.Fatalf("create: %+v", ferr)
		}
		got := waitInstanceState(t, s, v.InstanceID, "error", 15*time.Second)
		if got.Error == nil || got.Error.Code != "APP_UNHEALTHY" {
			t.Fatalf("unexpected health error: %+v", got)
		}
	})
}

func TestAdversarialCleanupFailureCanBeRetried(t *testing.T) {
	payload := standardArtifact(t, validLaunch(5000))
	files := artifactServer(t, map[string][]byte{"/a.tar.gz": payload})
	work := t.TempDir()
	s := newFakeService(t, fakeServiceOptions{workDir: work})
	t.Cleanup(func() { _ = os.Chmod(work, 0700) })

	v, ferr := s.Create(context.Background(), api.CreateRequest{AppID: "cleanup-fails", ArtifactURL: files.URL + "/a.tar.gz", Port: availableTestPort(t)})
	if ferr != nil {
		t.Fatalf("create: %+v", ferr)
	}
	mustChmod(t, work, 0500)
	if _, ferr := s.Stop(context.Background(), v.InstanceID); ferr == nil || ferr.Detail.Code != "STOP_FAILED" || ferr.HTTP != 500 {
		t.Fatalf("expected cleanup failure, got %+v", ferr)
	}
	got, ok := s.Get(v.InstanceID)
	if !ok || got.State != "error" || got.Error == nil || got.Error.Code != "STOP_FAILED" {
		t.Fatalf("instance should retain error state: %+v", got)
	}
	mustChmod(t, work, 0700)
	stopped, ferr := s.Stop(context.Background(), v.InstanceID)
	if ferr != nil || stopped.State != "stopped" || stopped.Error != nil {
		t.Fatalf("cleanup retry failed: %+v %+v", stopped, ferr)
	}
	if entries, err := os.ReadDir(work); err != nil || len(entries) != 0 {
		t.Fatalf("work dir leaked: %v %v", entries, err)
	}
}

func TestAdversarialCreateInspectionAndEntropyFailures(t *testing.T) {
	origPath := os.Getenv("PATH")
	s := newFakeService(t, fakeServiceOptions{})
	fakeLsof(t, "exit 2")
	_, ferr := s.Create(context.Background(), api.CreateRequest{AppID: "inspect-fails", ArtifactURL: "http://127.0.0.1:1/a.tar.gz", Port: availableTestPort(t)})
	if ferr == nil || ferr.Detail.Code != "PORT_INSPECTION_FAILED" || ferr.HTTP != 503 {
		t.Fatalf("expected port inspection failure, got %+v", ferr)
	}

	// Restore the real lsof so the free-port probe below succeeds.
	t.Setenv("PATH", origPath)
	useEntropyFailure(s)
	t.Cleanup(func() { restoreEntropy(s) })
	_, ferr = s.Create(context.Background(), api.CreateRequest{AppID: "entropy-fails", ArtifactURL: "http://127.0.0.1:1/a.tar.gz", Port: availableTestPort(t)})
	if ferr == nil || ferr.Detail.Code != "APP_START_FAILED" || ferr.HTTP != 500 {
		t.Fatalf("expected entropy failure, got %+v", ferr)
	}
}

func TestAdversarialCloseCollectsStopFailures(t *testing.T) {
	s := newFakeService(t, fakeServiceOptions{})
	stuck := &run{info: api.Instance{AppID: "stuck", InstanceID: "stuck-id", Port: 40200}, done: make(chan struct{}), cancel: func() {}}
	s.mu.Lock()
	s.instances[stuck.info.InstanceID] = stuck
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if err := s.Close(ctx); err == nil {
		t.Fatal("Close should report the stuck instance")
	}
	s.mu.Lock()
	delete(s.instances, stuck.info.InstanceID)
	s.mu.Unlock()
}

func TestAdversarialPostMediaStartupExit(t *testing.T) {
	payload := standardArtifact(t, validLaunch(5000))
	files := artifactServer(t, map[string][]byte{"/a.tar.gz": payload})
	// The app disappears before the encoder produces its first frame, so the
	// post-media select observes processDone rather than a timeout.
	s := newFakeService(t, fakeServiceOptions{nodeMode: "exitlater", ffmpegMode: ffmpegSlow})
	_, ferr := s.Create(context.Background(), api.CreateRequest{AppID: "late-exit", ArtifactURL: files.URL + "/a.tar.gz", Port: availableTestPort(t)})
	if ferr == nil || ferr.Detail.Code != "APP_EXITED" || ferr.HTTP != 502 {
		t.Fatalf("expected startup exit, got %+v", ferr)
	}
}

func TestAdversarialCleanupStopErrors(t *testing.T) {
	payload := standardArtifact(t, validLaunch(5000))
	files := artifactServer(t, map[string][]byte{"/a.tar.gz": payload})
	// Kill the process first, then report failure so no process is leaked.
	stop := func(cmd *exec.Cmd, done <-chan struct{}) error {
		_ = stopProcess(cmd, done)
		return errors.New("synthetic stop failure")
	}
	s := newFakeService(t, fakeServiceOptions{stopGroup: stop})
	v, ferr := s.Create(context.Background(), api.CreateRequest{AppID: "stop-errors", ArtifactURL: files.URL + "/a.tar.gz", Port: availableTestPort(t)})
	if ferr != nil {
		t.Fatalf("create: %+v", ferr)
	}
	if _, ferr := s.Stop(context.Background(), v.InstanceID); ferr == nil || ferr.Detail.Code != "STOP_FAILED" {
		t.Fatalf("expected stop failure, got %+v", ferr)
	}
}

func TestAdversarialNewFilesystemErrors(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "file")
	writeFile(t, parent, "x", 0600)
	if _, err := New(Config{WorkDir: filepath.Join(parent, "child"), PublicURL: "http://127.0.0.1:8090"}); err == nil {
		t.Fatal("work directory below a regular file was accepted")
	}

}

func TestAdversarialWaitHealthSeesExitAfterHealthyProbe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()
	port := server.Listener.Addr().(*net.TCPAddr).Port
	done := make(chan struct{})
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(done)
	}()
	err := waitHealth(context.Background(), server.URL, os.Getpid(), port, done)
	if err == nil || !strings.Contains(err.Error(), "application exited") {
		t.Fatalf("expected exit after healthy probe, got %v", err)
	}
}

func TestAdversarialCheckPageRedirectIsNotFollowed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/next", http.StatusFound)
	}))
	defer server.Close()
	if err := checkPage(context.Background(), server.URL); err == nil {
		t.Fatal("redirecting page was accepted")
	}
}

func TestAdversarialFailureErrorAndSnapshot(t *testing.T) {
	e := &Failure{Detail: api.Error{Message: "boom"}}
	if e.Error() != "boom" {
		t.Fatalf("unexpected error text: %s", e.Error())
	}
	r := &run{info: api.Instance{AppID: "a", InstanceID: "i"}}
	copy := snapshot(r)
	copy.AppID = "changed"
	if r.info.AppID != "a" {
		t.Fatal("snapshot mutated the run")
	}
}

func TestAdversarialAfterDownloadAppDirConflict(t *testing.T) {
	payload := standardArtifact(t, validLaunch(5000))
	files := artifactServer(t, map[string][]byte{"/artifact.tar.gz": payload})
	var ran atomic.Bool
	s := newFakeService(t, fakeServiceOptions{
		afterDownload: func(dir string) {
			ran.Store(true)
			// A regular file where the unpacked app directory belongs makes
			// the following Mkdir fail with EEXIST.
			if err := os.WriteFile(filepath.Join(dir, "artifact"), []byte("occupied"), 0600); err != nil {
				t.Errorf("seed artifact file: %v", err)
			}
		},
	})
	_, ferr := s.Create(context.Background(), api.CreateRequest{
		AppID:       "adv-download-conflict",
		ArtifactURL: files.URL + "/artifact.tar.gz",
		Port:        availableTestPort(t),
	})
	if !ran.Load() {
		t.Fatal("afterDownload hook did not run")
	}
	if ferr == nil || ferr.HTTP != 500 || ferr.Detail.Code != "APP_START_FAILED" {
		t.Fatalf("expected app dir creation failure, got %+v", ferr)
	}
	if !strings.Contains(ferr.Detail.Message, "artifact") {
		t.Fatalf("unexpected failure message: %s", ferr.Detail.Message)
	}
}

func TestAdversarialAfterUnpackLogPathConflict(t *testing.T) {
	payload := standardArtifact(t, validLaunch(5000))
	files := artifactServer(t, map[string][]byte{"/artifact.tar.gz": payload})
	var ran atomic.Bool
	s := newFakeService(t, fakeServiceOptions{
		afterUnpack: func(dir, appDir string) {
			ran.Store(true)
			// A directory where app.log belongs makes os.Create fail with EISDIR.
			if err := os.Mkdir(filepath.Join(dir, "app.log"), 0700); err != nil {
				t.Errorf("seed app.log directory: %v", err)
			}
		},
	})
	_, ferr := s.Create(context.Background(), api.CreateRequest{
		AppID:       "adv-log-conflict",
		ArtifactURL: files.URL + "/artifact.tar.gz",
		Port:        availableTestPort(t),
	})
	if !ran.Load() {
		t.Fatal("afterUnpack hook did not run")
	}
	if ferr == nil || ferr.HTTP != 500 || ferr.Detail.Code != "APP_START_FAILED" {
		t.Fatalf("expected log creation failure, got %+v", ferr)
	}
	if !strings.Contains(ferr.Detail.Message, "app.log") {
		t.Fatalf("unexpected failure message: %s", ferr.Detail.Message)
	}
}

func TestAdversarialAfterMediaStopReturnsDuringStartup(t *testing.T) {
	payload := standardArtifact(t, validLaunch(5000))
	files := artifactServer(t, map[string][]byte{"/artifact.tar.gz": payload})
	var ran atomic.Bool
	stopDone := make(chan struct{})
	s := newFakeService(t, fakeServiceOptions{
		afterMedia: func(svc *Service, r *run) {
			ran.Store(true)
			go func() {
				defer close(stopDone)
				_, _ = svc.Stop(context.Background(), r.info.InstanceID)
			}()
			if !waitStopRequested(svc, r, 10*time.Second) {
				t.Errorf("Stop never recorded a stop request")
			}
		},
	})
	res := waitCreate(t, createAsync(s, api.CreateRequest{
		AppID:       "adv-stop-after-media",
		ArtifactURL: files.URL + "/artifact.tar.gz",
		Port:        availableTestPort(t),
	}), 20*time.Second)
	if !ran.Load() {
		t.Fatal("afterMedia hook did not run")
	}
	if res.failure == nil || res.failure.Detail.Code != "APP_START_FAILED" || res.failure.Detail.Message != "instance stopped during startup" {
		t.Fatalf("expected stopped-during-startup failure, got %+v", res.failure)
	}
	if res.instance.State != "stopped" || res.instance.Error != nil {
		t.Fatalf("unexpected instance snapshot: %+v", res.instance)
	}
	select {
	case <-stopDone:
	case <-time.After(10 * time.Second):
		t.Fatal("Stop did not return")
	}
}

func TestAdversarialBeforeReadyStopReturnsDuringStartup(t *testing.T) {
	payload := standardArtifact(t, validLaunch(5000))
	files := artifactServer(t, map[string][]byte{"/artifact.tar.gz": payload})
	var ran atomic.Bool
	stopDone := make(chan struct{})
	s := newFakeService(t, fakeServiceOptions{
		beforeReady: func(svc *Service, r *run) {
			ran.Store(true)
			go func() {
				defer close(stopDone)
				_, _ = svc.Stop(context.Background(), r.info.InstanceID)
			}()
			if !waitStopRequested(svc, r, 10*time.Second) {
				t.Errorf("Stop never recorded a stop request")
			}
		},
	})
	res := waitCreate(t, createAsync(s, api.CreateRequest{
		AppID:       "adv-stop-before-ready",
		ArtifactURL: files.URL + "/artifact.tar.gz",
		Port:        availableTestPort(t),
	}), 20*time.Second)
	if !ran.Load() {
		t.Fatal("beforeReady hook did not run")
	}
	if res.failure == nil || res.failure.Detail.Code != "APP_START_FAILED" || res.failure.Detail.Message != "instance stopped during startup" {
		t.Fatalf("expected stopped-during-startup failure, got %+v", res.failure)
	}
	if res.instance.State != "stopped" || res.instance.Error != nil {
		t.Fatalf("unexpected instance snapshot: %+v", res.instance)
	}
	select {
	case <-stopDone:
	case <-time.After(10 * time.Second):
		t.Fatal("Stop did not return")
	}
}

func TestAdversarialWaitHealthContextEndsBetweenProbes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := waitHealth(ctx, srv.URL, 4242, 1234, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
	if elapsed := time.Since(started); elapsed < 40*time.Millisecond {
		t.Fatalf("health probe returned before the context deadline: %v", elapsed)
	}
}
