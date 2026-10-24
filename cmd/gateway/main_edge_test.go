package main

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/artifact"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

func newDatabase(t *testing.T) *store.DB {
	t.Helper()
	database, err := store.Open(filepath.Join(t.TempDir(), "edge.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func TestHealthHandlerReflectsDatabaseState(t *testing.T) {
	database := newDatabase(t)
	recorder := httptest.NewRecorder()
	healthHandler(database)(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"status":"ok"`) {
		t.Fatalf("body = %s", recorder.Body.String())
	}

	if err := database.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	recorder = httptest.NewRecorder()
	healthHandler(database)(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("closed database status = %d, want 503", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"status":"unavailable"`) {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestMCPAuth(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("reached"))
	})
	handler := mcpAuth("s3cret", next)

	cases := []struct {
		name   string
		path   string
		header string
		want   int
	}{
		{"mcp without token", "/mcp", "", http.StatusUnauthorized},
		{"mcp wrong token", "/mcp", "Bearer nope", http.StatusUnauthorized},
		{"mcp wrong scheme", "/mcp", "s3cret", http.StatusUnauthorized},
		{"mcp correct token", "/mcp", "Bearer s3cret", http.StatusOK},
		{"message endpoint without token", "/mcp/message", "", http.StatusUnauthorized},
		{"sse without token", "/sse", "", http.StatusUnauthorized},
		{"sse with token", "/sse", "Bearer s3cret", http.StatusOK},
		{"unprotected path", "/healthz", "", http.StatusOK},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, testCase.path, nil)
			if testCase.header != "" {
				request.Header.Set("Authorization", testCase.header)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != testCase.want {
				t.Fatalf("status = %d, want %d", recorder.Code, testCase.want)
			}
		})
	}

	// Without a configured token, MCP endpoints stay open.
	open := mcpAuth("", next)
	recorder := httptest.NewRecorder()
	open.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/mcp", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("open MCP status = %d", recorder.Code)
	}
}

func TestAwaitShutdownCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var closed atomic.Int32
	server := &http.Server{}
	if err := awaitShutdown(ctx, server, func() { closed.Add(1) }, make(chan error)); err != nil {
		t.Fatalf("awaitShutdown: %v", err)
	}
	if closed.Load() != 1 {
		t.Fatalf("closeMCP calls = %d", closed.Load())
	}
}

func TestAwaitShutdownServerResults(t *testing.T) {
	result := make(chan error, 1)
	result <- http.ErrServerClosed
	if err := awaitShutdown(context.Background(), &http.Server{}, func() {}, result); err != nil {
		t.Fatalf("ErrServerClosed: %v", err)
	}

	result = make(chan error, 1)
	result <- errors.New("listen failure")
	if err := awaitShutdown(context.Background(), &http.Server{}, func() {}, result); err == nil || err.Error() != "listen failure" {
		t.Fatalf("serve error = %v", err)
	}
}

// closeFailingListener reports a close error during Shutdown.
type closeFailingListener struct {
	net.Listener
	accepted chan struct{}
	once     sync.Once
}

func (l *closeFailingListener) Accept() (net.Conn, error) {
	l.once.Do(func() { close(l.accepted) })
	return l.Listener.Accept()
}

func (l *closeFailingListener) Close() error {
	_ = l.Listener.Close()
	return errors.New("listener close failure")
}

func TestAwaitShutdownHandlesShutdownFailure(t *testing.T) {
	base, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener := &closeFailingListener{Listener: base, accepted: make(chan struct{})}
	server := &http.Server{Handler: http.NotFoundHandler()}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = server.Serve(listener)
	}()
	select {
	case <-listener.accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("server never accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := awaitShutdown(ctx, server, func() {}, make(chan error)); err != nil {
		t.Fatalf("awaitShutdown: %v", err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop")
	}
}

func seedStore(t *testing.T, path string, values ...app.App) {
	t.Helper()
	database, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer database.Close()
	for _, value := range values {
		if err := database.SaveApp(value); err != nil {
			t.Fatalf("SaveApp(%s): %v", value.ID, err)
		}
	}
}

func rawDatabase(t *testing.T, path string) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func TestServeFailsOnCorruptPersistedApp(t *testing.T) {
	cfg := testConfig(t)
	seedStore(t, cfg.DBPath)
	raw := rawDatabase(t, cfg.DBPath)
	if _, err := raw.Exec(`INSERT INTO apps (id, owner_device_id, owner_session_id, state, revision, data, updated_at)
		VALUES ('broken', 'dev', 'sess', 'idle', 1, '{not-json', '2020-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("insert corrupt row: %v", err)
	}
	err := serve(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "decode app") {
		t.Fatalf("error = %v, want decode failure", err)
	}
}

func TestServeRestoresArtifactLocation(t *testing.T) {
	cfg := testConfig(t)
	seedStore(t, cfg.DBPath, app.App{
		ID:       "app1",
		Revision: 3,
		Owner:    app.Identity{DeviceID: "dev", SessionID: "sess"},
		State:    app.StateRunning,
		Artifact: app.Artifact{Path: "/stale/place.tar.gz", DownloadURL: "http://stale.example/artifact"},
	})
	database, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.SaveArtifact(artifact.Item{
		AppID: "app1", Path: "/stale/place.tar.gz", Size: 10, SHA256: "abc",
		Token: "tok", DownloadURL: "http://stale.example/artifact", StoredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveArtifact: %v", err)
	}
	_ = database.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serve(ctx, cfg) }()

	wantPath := filepath.Join(cfg.ArtifactsDir, "app1.tar.gz")
	deadline := time.Now().Add(10 * time.Second)
	for {
		current := readApp(t, cfg.DBPath, "app1")
		if current != nil && current.Revision == 4 {
			if current.Artifact.Path != wantPath {
				t.Fatalf("artifact path = %q, want %q", current.Artifact.Path, wantPath)
			}
			if !strings.Contains(current.Artifact.DownloadURL, "/artifacts/app1/artifact.tar.gz?token=tok") {
				t.Fatalf("download URL = %q", current.Artifact.DownloadURL)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("artifact location was not restored: %+v", current)
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not stop")
	}
}

func TestServeFailsWhenRestoreCannotPersist(t *testing.T) {
	cfg := testConfig(t)
	seedStore(t, cfg.DBPath, app.App{
		ID: "app1", Revision: 3, State: app.StateRunning,
		Artifact: app.Artifact{Path: "/stale/place.tar.gz"},
	})
	database, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.SaveArtifact(artifact.Item{
		AppID: "app1", Path: "/stale/place.tar.gz", Token: "tok", StoredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	_ = database.Close()
	raw := rawDatabase(t, cfg.DBPath)
	if _, err := raw.Exec(`CREATE TRIGGER block_app_update BEFORE UPDATE ON apps
		WHEN NEW.id = 'app1' BEGIN SELECT RAISE(ABORT, 'blocked by trigger'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	err = serve(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "restore app artifact location") {
		t.Fatalf("error = %v", err)
	}
}

func TestServeFailsWhenRecoveryCannotPersist(t *testing.T) {
	cfg := testConfig(t)
	seedStore(t, cfg.DBPath, app.App{
		ID: "rec1", Revision: 2, State: app.StateIdle, InstanceID: "instance-1",
	})
	raw := rawDatabase(t, cfg.DBPath)
	if _, err := raw.Exec(`CREATE TRIGGER block_recovery BEFORE UPDATE ON apps
		WHEN NEW.id = 'rec1' BEGIN SELECT RAISE(ABORT, 'blocked by trigger'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	err := serve(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "PERSISTENCE_FAILED") {
		t.Fatalf("error = %v", err)
	}
}

func TestServeResumesInFlightBuild(t *testing.T) {
	cfg := testConfig(t)
	cfg.BuildTimeout = time.Minute
	seedStore(t, cfg.DBPath, app.App{
		ID: "build1", Revision: 2, State: app.StateBuilding, SessionID: "",
		Owner: app.Identity{DeviceID: "dev", SessionID: "sess"},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serve(ctx, cfg) }()

	deadline := time.Now().Add(10 * time.Second)
	for {
		current := readApp(t, cfg.DBPath, "build1")
		if current != nil && current.State == app.StateError {
			if current.Error == nil || current.Error.Code != "BUILD_FAILED" {
				t.Fatalf("resume error = %+v", current.Error)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("build was not resumed: %+v", current)
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not stop")
	}
}

func readApp(t *testing.T, path, id string) *app.App {
	t.Helper()
	database, err := store.Open(path)
	if err != nil {
		return nil
	}
	defer database.Close()
	values, err := database.ListApps()
	if err != nil {
		return nil
	}
	for i := range values {
		if values[i].ID == id {
			return &values[i]
		}
	}
	return nil
}
