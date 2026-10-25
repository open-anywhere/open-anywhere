package portal

import (
	"runtime"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

func openPortalTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "portal.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSystemPromptPrefersTheEmbeddedCopy(t *testing.T) {
	prompt, err := SystemPrompt("  ")
	if err != nil || prompt == "" || prompt != defaultSystemPrompt {
		t.Fatalf("default prompt len=%d err=%v", len(prompt), err)
	}
	path := filepath.Join(t.TempDir(), "prompt.txt")
	if err := os.WriteFile(path, []byte("自定义提示词"), 0o600); err != nil {
		t.Fatal(err)
	}
	prompt, err = SystemPrompt(path)
	if err != nil || prompt != "自定义提示词" {
		t.Fatalf("prompt=%q err=%v", prompt, err)
	}
	if _, err := SystemPrompt(filepath.Join(t.TempDir(), "missing.txt")); err == nil {
		t.Fatal("a missing prompt file was accepted")
	}
}

func TestNewServiceBuildsThePortal(t *testing.T) {
	db := openPortalTestDB(t)
	dataDir := filepath.Join(t.TempDir(), "data")
	webDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<!doctype html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service, err := NewService(ctx, BuildConfig{
		DataDir:      dataDir,
		WebDir:       webDir,
		Username:     "alice",
		Password:     "password123",
		SessionTTL:   time.Hour,
		SystemPrompt: "",
	}, db, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if service.Auth == nil || service.Provision == nil || service.Voice == nil || service.API == nil {
		t.Fatalf("service=%+v", service)
	}
	if service.Settings != nil || service.Resolver != nil {
		t.Fatalf("nil platform was reported as configured: %+v", service)
	}
	info, err := os.Stat(dataDir)
	// Windows has no Unix permission bits.
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o700) {
		t.Fatalf("data dir mode=%v err=%v", info, err)
	}
	if _, _, err := service.Auth.Login(ctx, Credentials{Username: "alice", Password: "password123"}, "agent", "10.0.0.1"); err != nil {
		t.Fatalf("the configured account cannot log in: %v", err)
	}

	recorder := httptest.NewRecorder()
	service.API.WebHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "<!doctype html>") {
		t.Fatalf("web handler: %d %s", recorder.Code, recorder.Body.String())
	}

	// Without web_dir the SPA explains that the frontend is not built.
	bare, err := NewService(ctx, BuildConfig{
		DataDir:    filepath.Join(t.TempDir(), "data"),
		Username:   "bob",
		Password:   "password123",
		SessionTTL: time.Hour,
	}, db, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewService without web: %v", err)
	}
	recorder = httptest.NewRecorder()
	bare.API.WebHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), "门户前端尚未构建") {
		t.Fatalf("missing build: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestNewServiceReportsConfigurationFailures(t *testing.T) {
	db := openPortalTestDB(t)
	ctx := context.Background()

	occupied := filepath.Join(t.TempDir(), "data")
	if err := os.WriteFile(occupied, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(ctx, BuildConfig{DataDir: occupied, Username: "alice", Password: "password123"}, db, nil, nil, nil, nil); err == nil {
		t.Fatal("a file in place of the data directory was accepted")
	}

	missingPrompt := filepath.Join(t.TempDir(), "missing.txt")
	if _, err := NewService(ctx, BuildConfig{
		DataDir: filepath.Join(t.TempDir(), "data"), WebDir: t.TempDir(),
		Username: "alice", Password: "password123", SystemPrompt: missingPrompt,
	}, db, nil, nil, nil, nil); err == nil {
		t.Fatal("a missing system prompt was accepted")
	}

	if _, err := NewService(ctx, BuildConfig{
		DataDir: filepath.Join(t.TempDir(), "data"), WebDir: t.TempDir(),
		Username: "a", Password: "password123",
	}, db, nil, nil, nil, nil); err == nil {
		t.Fatal("an invalid bootstrap username was accepted")
	}

	if !EmbeddedWebAvailable() {
		if _, err := NewService(ctx, BuildConfig{
			DataDir: filepath.Join(t.TempDir(), "data"), WebEmbedded: true,
			Username: "alice", Password: "password123",
		}, db, nil, nil, nil, nil); err == nil || !strings.Contains(err.Error(), "not embedded") {
			t.Fatalf("embedded build without the tag: %v", err)
		}
	}
}

func TestResolveWebFSAndLogMissingWebDir(t *testing.T) {
	dir := t.TempDir()
	if fs := resolveWebFS(dir); fs == nil {
		t.Fatal("a configured web directory produced no filesystem")
	}
	_ = resolveWebFS("")

	logMissingWebDir("")
	logMissingWebDir(filepath.Join(t.TempDir(), "missing"))
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	logMissingWebDir(dir)
}

func TestSPAAndRootHandlerBranches(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("shell"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("code"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := NewRootSPA(dir)
	scoped := NewSPA(dir)

	cases := []struct {
		name       string
		handler    http.Handler
		method     string
		target     string
		wantStatus int
	}{
		{name: "scoped bare redirect", handler: scoped, method: http.MethodGet, target: "/portal", wantStatus: http.StatusMovedPermanently},
		{name: "scoped outside prefix", handler: scoped, method: http.MethodGet, target: "/other", wantStatus: http.StatusNotFound},
		{name: "root reserved", handler: root, method: http.MethodGet, target: "/api/portal/me", wantStatus: http.StatusNotFound},
		{name: "root method", handler: root, method: http.MethodPost, target: "/portal", wantStatus: http.StatusMethodNotAllowed},
		{name: "root index", handler: root, method: http.MethodGet, target: "/", wantStatus: http.StatusOK},
		{name: "root asset", handler: root, method: http.MethodGet, target: "/assets/app.js", wantStatus: http.StatusOK},
		{name: "root client route", handler: root, method: http.MethodGet, target: "/portal/room/1", wantStatus: http.StatusOK},
		{name: "root stale asset", handler: root, method: http.MethodGet, target: "/assets/missing.js", wantStatus: http.StatusNotFound},
	}
	for _, test := range cases {
		recorder := httptest.NewRecorder()
		test.handler.ServeHTTP(recorder, httptest.NewRequest(test.method, test.target, nil))
		if recorder.Code != test.wantStatus {
			t.Errorf("%s: status=%d want=%d body=%s", test.name, recorder.Code, test.wantStatus, recorder.Body.String())
		}
		if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s: nosniff=%q", test.name, got)
		}
	}

	missing := NewRootSPA(filepath.Join(t.TempDir(), "missing"))
	recorder := httptest.NewRecorder()
	missing.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing index: %d %s", recorder.Code, recorder.Body.String())
	}
}
