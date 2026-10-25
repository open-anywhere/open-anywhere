package build

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/artifact"
	"github.com/open-anywhere/open-anywhere/pkg/agentapi"
)

type failingBuilderStore struct{ reject func(app.App) bool }

func (s failingBuilderStore) SaveApp(value app.App) error {
	if s.reject(value) {
		return errors.New("disk unavailable")
	}
	return nil
}

func (failingBuilderStore) DeleteApp(string) error { return nil }

type archiveEntry struct {
	name string
	body string
	dir  bool
}

func archiveOf(t *testing.T, entries []archiveEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(gz)
	for _, item := range entries {
		header := &tar.Header{Name: item.name, Mode: 0o644, Size: int64(len(item.body))}
		if item.dir {
			header.Typeflag = tar.TypeDir
			header.Mode = 0o755
			header.Size = 0
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if !item.dir {
			if _, err := writer.Write([]byte(item.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// builderTestServer exposes the AgentAPI endpoints the builder needs, with a
// caller-supplied SSE handler.
func builderTestServer(t *testing.T, stream http.HandlerFunc, archive []byte, status string, rec *recorder) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"ses_1","status":"idle"}`)
	})
	mux.HandleFunc("POST /v1/sessions/ses_1/events", func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{}`)
	})

	mux.HandleFunc("GET /v1/sessions/ses_1/events/stream", stream)
	mux.HandleFunc("GET /v1/sessions/ses_1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"ses_1","status":"`+status+`"}`)
	})
	mux.HandleFunc("GET /v1/files", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if archive == nil {
			io.WriteString(w, `{"data":[],"has_more":false}`)
			return
		}
		io.WriteString(w, `{"data":[{"id":"file_1","filename":"artifact.tar.gz","downloadable":true,"size_bytes":`+
			strconv.Itoa(len(archive))+`,"created_at":"2026-01-02T03:04:05Z"}],"has_more":false}`)
	})
	mux.HandleFunc("GET /v1/files/file_1/content", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(archive)
	})
	mux.HandleFunc("POST /v1/sessions/ses_1/archive", func(w http.ResponseWriter, r *http.Request) {
		if rec != nil {
			rec.addArchive("ses_1")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"ses_1","status":"terminated"}`)
	})
	return httptest.NewServer(mux)
}

func sseWrite(w http.ResponseWriter, data string) {
	io.WriteString(w, "event: message\ndata: "+data+"\n\n")
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func TestNewAppliesDefaults(t *testing.T) {
	builder := New(nil, nil, nil, nil, Config{})
	if builder.cfg.BuildTimeout != 60*time.Minute {
		t.Fatalf("BuildTimeout = %v", builder.cfg.BuildTimeout)
	}
	if builder.cfg.InterruptGrace != 5*time.Minute {
		t.Fatalf("InterruptGrace = %v", builder.cfg.InterruptGrace)
	}
	if builder.cfg.ArtifactLimit != artifact.MaxSize {
		t.Fatalf("ArtifactLimit = %d", builder.cfg.ArtifactLimit)
	}
	if builder.now == nil || builder.logf == nil {
		t.Fatal("defaults were not installed")
	}
}

func TestStartRequiresAgentAndEnvironment(t *testing.T) {
	builder := New(nil, nil, nil, &fakeNotifier{}, Config{AgentID: "agent"})
	err := builder.Start(context.Background(), app.App{ID: "app_1"})
	if err == nil || !strings.Contains(err.Error(), "agent and environment are not configured") {
		t.Fatalf("err = %v", err)
	}
}

func TestStartArchivesWhenTheSessionCannotBePersisted(t *testing.T) {
	rec := &recorder{}
	server := newAgentServer(t, "complete", testArtifact(t), rec)
	defer server.Close()
	builder, registry, _ := newTestBuilder(t, server.URL, &fakeNotifier{})
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := registry.Add(app.App{ID: "app_1", Owner: owner, State: app.StateBuilding})
	registry.SetPersister(failingBuilderStore{func(value app.App) bool { return value.SessionID != "" }})

	err := builder.Start(context.Background(), entry.Snapshot())
	if err == nil || !strings.Contains(err.Error(), "persist build session") {
		t.Fatalf("err = %v", err)
	}
	if archived := rec.archiveList(); len(archived) != 1 {
		t.Fatalf("an unpersisted session must be archived: %v", archived)
	}
}

func TestStartArchivesWhenTheRecordDisappears(t *testing.T) {
	rec := &recorder{}
	server := newAgentServer(t, "complete", testArtifact(t), rec)
	defer server.Close()
	builder, _, _ := newTestBuilder(t, server.URL, &fakeNotifier{})

	err := builder.Start(context.Background(), app.App{ID: "app_missing", State: app.StateBuilding})
	if err == nil || !strings.Contains(err.Error(), "disappeared") {
		t.Fatalf("err = %v", err)
	}
	if archived := rec.archiveList(); len(archived) != 1 {
		t.Fatalf("the orphan session must be archived: %v", archived)
	}
}

func TestStartContinuesWhenSubmitFailsForARunningSession(t *testing.T) {
	rec := &recorder{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"ses_1","status":"idle"}`)
	})
	mux.HandleFunc("POST /v1/sessions/ses_1/events", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"busy"}}`, http.StatusInternalServerError)
	})
	mux.HandleFunc("GET /v1/sessions/ses_1/events/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		sseWrite(w, `{"id":"e1","type":"session.status_running"}`)
	})
	mux.HandleFunc("GET /v1/sessions/ses_1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"ses_1","status":"running"}`)
	})
	mux.HandleFunc("POST /v1/sessions/ses_1/archive", func(w http.ResponseWriter, r *http.Request) {
		rec.addArchive("ses_1")
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"ses_1","status":"terminated"}`)
	})
	mux.HandleFunc("GET /v1/files", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"data":[],"has_more":false}`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	notifier := &fakeNotifier{}
	builder, registry, _ := newTestBuilder(t, server.URL, notifier)
	builder.sleepFn = func(context.Context, time.Duration) bool { return true }
	var logs []string
	var mu sync.Mutex
	builder.logf = func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		logs = append(logs, format)
	}
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := registry.Add(app.App{ID: "app_1", Owner: owner, State: app.StateIdle})

	if err := builder.Start(context.Background(), entry.Snapshot()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if entry.Snapshot().SessionID != "ses_1" {
		t.Fatalf("snapshot = %+v", entry.Snapshot())
	}
	waitFor(t, 2*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, line := range logs {
			if strings.Contains(line, "submit returned") {
				return true
			}
		}
		return false
	})
}

func TestWatchTimesOutImmediatelyForAnExpiredBuild(t *testing.T) {
	rec := &recorder{}
	server := newAgentServer(t, "complete", testArtifact(t), rec)
	defer server.Close()
	notifier := &fakeNotifier{}
	builder, registry, _ := newTestBuilder(t, server.URL, notifier)
	builder.cfg.BuildTimeout = time.Minute
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := registry.Add(app.App{ID: "app_1", Owner: owner, State: app.StateBuilding, BuildStartedAt: time.Now().Add(-time.Hour)})

	builder.watch("app_1", "ses_1")

	snapshot := entry.Snapshot()
	if snapshot.State != app.StateError || snapshot.Error == nil || snapshot.Error.Message != "构建超时" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if len(rec.archiveList()) != 1 {
		t.Fatalf("archived = %v", rec.archiveList())
	}
}

func TestWatchStopsWithoutProcessingWhenTheAppLeftBuilding(t *testing.T) {
	rec := &recorder{}
	server := newAgentServer(t, "complete", testArtifact(t), rec)
	defer server.Close()
	builder, registry, _ := newTestBuilder(t, server.URL, &fakeNotifier{})
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := registry.Add(app.App{ID: "app_1", Owner: owner, State: app.StateIdle, BuildStartedAt: time.Now()})

	builder.watch("app_1", "ses_1")

	if entry.Snapshot().State != app.StateIdle {
		t.Fatalf("snapshot = %+v", entry.Snapshot())
	}
	if len(rec.archiveList()) != 0 {
		t.Fatalf("a completed build was archived: %v", rec.archiveList())
	}
}

func TestWatchReconnectsDedupesAndCompletes(t *testing.T) {
	archive := testArtifact(t)
	rec := &recorder{}
	var mu sync.Mutex
	connections := 0
	stream := func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		connections++
		current := connections
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if current == 1 {
			sseWrite(w, `{"id":"e1","type":"session.status_running"}`)
			// A duplicate id must be ignored even when its type is terminal.
			sseWrite(w, `{"id":"e1","type":"session.status_terminated"}`)
			// Events without an id cannot be deduplicated but are still handled.
			sseWrite(w, `{"type":"agent.message","content":[{"type":"text","text":"中间进度"}]}`)
			return
		}
		sseWrite(w, `{"id":"e2","type":"session.status_running"}`)
		sseWrite(w, `{"id":"e3","type":"session.status_idle","stop_reason":{"type":"end_turn"}}`)
	}
	server := builderTestServer(t, stream, archive, "idle", rec)
	defer server.Close()

	notifier := &fakeNotifier{}
	builder, registry, artifacts := newTestBuilder(t, server.URL, notifier)
	builder.sleepFn = func(context.Context, time.Duration) bool { return true }
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := registry.Add(app.App{ID: "app_1", Owner: owner, Prompt: "做一个游戏", State: app.StateBuilding, BuildStartedAt: time.Now()})

	builder.watch("app_1", "ses_1")

	snapshot := entry.Snapshot()
	if snapshot.State != app.StateIdle || snapshot.Error != nil {
		t.Fatalf("a duplicate terminal event ended the build: %+v", snapshot)
	}
	if _, ok := artifacts.Get("app_1"); !ok {
		t.Fatal("the reconnected stream did not deliver an artifact")
	}
	mu.Lock()
	defer mu.Unlock()
	if connections < 2 {
		t.Fatalf("connections = %d", connections)
	}
}

func TestWatchTimesOutWhenTheStreamStopsAtTheDeadline(t *testing.T) {
	rec := &recorder{}
	stream := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		sseWrite(w, `{"id":"e1","type":"session.status_running"}`)
		<-r.Context().Done()
	}
	server := builderTestServer(t, stream, nil, "idle", rec)
	defer server.Close()

	notifier := &fakeNotifier{}
	builder, registry, _ := newTestBuilder(t, server.URL, notifier)
	builder.cfg.BuildTimeout = 150 * time.Millisecond
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := registry.Add(app.App{ID: "app_1", Owner: owner, State: app.StateBuilding, BuildStartedAt: time.Now()})

	builder.watch("app_1", "ses_1")

	snapshot := entry.Snapshot()
	if snapshot.State != app.StateError || snapshot.Error == nil || snapshot.Error.Message != "构建超时" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if len(rec.archiveList()) != 1 {
		t.Fatalf("archived = %v", rec.archiveList())
	}
}

func TestWatchTimesOutAfterAStreamReadError(t *testing.T) {
	stream := func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"type":"api_error","message":"boom"}}`, http.StatusInternalServerError)
	}
	server := builderTestServer(t, stream, nil, "idle", nil)
	defer server.Close()

	builder, registry, _ := newTestBuilder(t, server.URL, &fakeNotifier{})
	builder.sleepFn = func(context.Context, time.Duration) bool { return false }
	var logs []string
	var mu sync.Mutex
	builder.logf = func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		logs = append(logs, format)
	}
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := registry.Add(app.App{ID: "app_1", Owner: owner, State: app.StateBuilding, BuildStartedAt: time.Now()})

	builder.watch("app_1", "ses_1")

	if entry.Snapshot().State != app.StateError {
		t.Fatalf("snapshot = %+v", entry.Snapshot())
	}
	mu.Lock()
	defer mu.Unlock()
	for _, line := range logs {
		if strings.Contains(line, "event stream read") {
			return
		}
	}
	t.Fatalf("a stream read error was not logged: %v", logs)
}

func TestWatchGivesUpOnEOFWhenSleepIsCanceled(t *testing.T) {
	rec := &recorder{}
	stream := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
	}
	server := builderTestServer(t, stream, nil, "idle", rec)
	defer server.Close()

	builder, registry, _ := newTestBuilder(t, server.URL, &fakeNotifier{})
	builder.sleepFn = func(context.Context, time.Duration) bool { return false }
	var logs []string
	var mu sync.Mutex
	builder.logf = func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		logs = append(logs, format)
	}
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := registry.Add(app.App{ID: "app_1", Owner: owner, State: app.StateBuilding, BuildStartedAt: time.Now()})

	builder.watch("app_1", "ses_1")

	if entry.Snapshot().State != app.StateError {
		t.Fatalf("snapshot = %+v", entry.Snapshot())
	}
	mu.Lock()
	defer mu.Unlock()
	for _, line := range logs {
		if strings.Contains(line, "event stream read") {
			t.Fatalf("EOF was reported as a read error: %v", logs)
		}
	}
}

func TestCompleteIgnoresAMissingRecord(t *testing.T) {
	builder, _, _ := newTestBuilder(t, "http://127.0.0.1:1", &fakeNotifier{})
	builder.complete("missing", "ses_1")
}

func TestCompleteArchivesAnAppThatLeftBuilding(t *testing.T) {
	rec := &recorder{}
	server := newAgentServer(t, "complete", testArtifact(t), rec)
	defer server.Close()
	builder, registry, _ := newTestBuilder(t, server.URL, &fakeNotifier{})
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	registry.Add(app.App{ID: "app_1", Owner: owner, State: app.StateIdle})

	builder.complete("app_1", "ses_1")

	if len(rec.archiveList()) != 1 {
		t.Fatalf("archived = %v", rec.archiveList())
	}
}

func TestCompleteReportsArtifactProblems(t *testing.T) {
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	t.Run("corrupt archive", func(t *testing.T) {
		rec := &recorder{}
		server := newAgentServer(t, "complete", []byte("not a gzip archive"), rec)
		defer server.Close()
		builder, registry, _ := newTestBuilder(t, server.URL, &fakeNotifier{})
		entry := registry.Add(app.App{ID: "app_1", Owner: owner, State: app.StateBuilding, BuildStartedAt: time.Now()})
		builder.complete("app_1", "ses_1")
		snapshot := entry.Snapshot()
		if snapshot.State != app.StateError || snapshot.Error == nil || snapshot.Error.Message != "构建产物损坏" {
			t.Fatalf("snapshot = %+v", snapshot)
		}
		if len(rec.archiveList()) != 1 {
			t.Fatalf("archived = %v", rec.archiveList())
		}
	})
	t.Run("missing output", func(t *testing.T) {
		rec := &recorder{}
		server := newAgentServer(t, "complete", nil, rec)
		defer server.Close()
		builder, registry, _ := newTestBuilder(t, server.URL, &fakeNotifier{})
		builder.sleepFn = func(context.Context, time.Duration) bool { return true }
		entry := registry.Add(app.App{ID: "app_1", Owner: owner, State: app.StateBuilding, BuildStartedAt: time.Now()})
		builder.complete("app_1", "ses_1")
		snapshot := entry.Snapshot()
		if snapshot.State != app.StateError || snapshot.Error == nil || snapshot.Error.Message != "构建产物缺失" {
			t.Fatalf("snapshot = %+v", snapshot)
		}
	})
	t.Run("invalid contract", func(t *testing.T) {
		rec := &recorder{}
		invalid := archiveOf(t, []archiveEntry{
			{name: "app/", dir: true},
			{name: "app/app.js", body: "console.log('x')"},
			{name: "launch.json", body: `{"name":"x","runtimeExecutable":"node","runtimeArgs":["app/app.js"]}`},
		})
		server := newAgentServer(t, "complete", invalid, rec)
		defer server.Close()
		builder, registry, _ := newTestBuilder(t, server.URL, &fakeNotifier{})
		entry := registry.Add(app.App{ID: "app_1", Owner: owner, State: app.StateBuilding, BuildStartedAt: time.Now()})
		builder.complete("app_1", "ses_1")
		snapshot := entry.Snapshot()
		if snapshot.State != app.StateError || snapshot.Error == nil || snapshot.Error.Message != "构建产物校验失败" {
			t.Fatalf("snapshot = %+v", snapshot)
		}
	})
	t.Run("storing fails", func(t *testing.T) {
		rec := &recorder{}
		archive := testArtifact(t)
		server := newAgentServer(t, "complete", archive, rec)
		defer server.Close()
		dir := filepath.Join(t.TempDir(), "artifacts")
		store, err := artifact.NewStore(dir, "http://gateway", nil)
		if err != nil {
			t.Fatal(err)
		}
		// A directory at the final path makes the atomic rename fail.
		if err := os.MkdirAll(filepath.Join(dir, "app_1.tar.gz"), 0o755); err != nil {
			t.Fatal(err)
		}
		registry := app.NewRegistry()
		notifier := &fakeNotifier{}
		builder := New(agentapi.New(server.URL, "key", 2*time.Second), store, registry, notifier, Config{
			AgentID: "agent_1", EnvironmentID: "env_1", BuildTimeout: 10 * time.Second, InterruptGrace: 10 * time.Millisecond, ArtifactLimit: 1 << 20,
		})
		entry := registry.Add(app.App{ID: "app_1", Owner: owner, State: app.StateBuilding, BuildStartedAt: time.Now()})
		builder.complete("app_1", "ses_1")
		snapshot := entry.Snapshot()
		if snapshot.State != app.StateError || snapshot.Error == nil || snapshot.Error.Message != "构建失败" {
			t.Fatalf("snapshot = %+v", snapshot)
		}
	})
}

func TestRegisterPaths(t *testing.T) {
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	t.Run("state left building before registration", func(t *testing.T) {
		builder, registry, _ := newTestBuilder(t, "http://127.0.0.1:1", &fakeNotifier{})
		entry := registry.Add(app.App{ID: "app_1", Owner: owner, State: app.StateIdle})
		builder.register(entry, artifact.Result{Launch: artifact.Launch{Name: "late"}}, artifact.Item{}, false)
		if entry.Snapshot().State != app.StateIdle || entry.Snapshot().Name != "" {
			t.Fatalf("snapshot = %+v", entry.Snapshot())
		}
		if notes := builder.notifier.(*fakeNotifier).list(); len(notes) != 0 {
			t.Fatalf("notes = %+v", notes)
		}
	})
	t.Run("ready transition is not durable", func(t *testing.T) {
		builder, registry, _ := newTestBuilder(t, "http://127.0.0.1:1", &fakeNotifier{})
		entry := registry.Add(app.App{ID: "app_1", Owner: owner, State: app.StateBuilding})
		registry.SetPersister(failingBuilderStore{func(app.App) bool { return true }})
		var logs []string
		builder.logf = func(format string, args ...any) { logs = append(logs, format) }
		builder.register(entry, artifact.Result{Launch: artifact.Launch{Name: "quiz"}}, artifact.Item{}, false)
		if entry.Snapshot().State != app.StateBuilding {
			t.Fatalf("an undurable ready state was published: %+v", entry.Snapshot())
		}
		if len(logs) != 1 || !strings.Contains(logs[0], "persist ready failed") {
			t.Fatalf("logs = %v", logs)
		}
		if notes := builder.notifier.(*fakeNotifier).list(); len(notes) != 0 {
			t.Fatalf("notes = %+v", notes)
		}
	})
}

func TestFailIgnoresMissingAndFinishedApps(t *testing.T) {
	builder, registry, _ := newTestBuilder(t, "http://127.0.0.1:1", &fakeNotifier{})
	builder.fail("missing", "构建失败", errors.New("x"))
	entry := registry.Add(app.App{ID: "app_1", State: app.StateIdle})
	builder.fail("app_1", "构建失败", errors.New("x"))
	if entry.Snapshot().State != app.StateIdle {
		t.Fatalf("snapshot = %+v", entry.Snapshot())
	}
	if notes := builder.notifier.(*fakeNotifier).list(); len(notes) != 0 {
		t.Fatalf("notes = %+v", notes)
	}
}

func TestTimeoutStopsPollingWhenSleepIsCanceled(t *testing.T) {
	rec := &recorder{}
	server := builderTestServer(t, func(w http.ResponseWriter, r *http.Request) {}, nil, "running", rec)
	defer server.Close()
	builder, registry, _ := newTestBuilder(t, server.URL, &fakeNotifier{})
	builder.sleepFn = func(context.Context, time.Duration) bool { return false }
	builder.cfg.InterruptGrace = time.Minute
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := registry.Add(app.App{ID: "app_1", Owner: owner, State: app.StateBuilding})

	start := time.Now()
	builder.timeout("app_1", "ses_1")
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("timeout polling did not stop: %v", elapsed)
	}
	if snapshot := entry.Snapshot(); snapshot.State != app.StateError || snapshot.Error == nil || snapshot.Error.Message != "构建超时" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if len(rec.archiveList()) != 1 {
		t.Fatalf("archived = %v", rec.archiveList())
	}
}

func TestArchiveLogsFailures(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/sessions/ses_1/archive", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusInternalServerError)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	builder, _, _ := newTestBuilder(t, server.URL, &fakeNotifier{})
	var logs []string
	builder.logf = func(format string, args ...any) { logs = append(logs, format) }
	builder.archive("ses_1")
	if len(logs) != 1 || !strings.Contains(logs[0], "archive session") {
		t.Fatalf("logs = %v", logs)
	}
}

func TestCollectRetriesAndGivesUpWhenSleepIsCanceled(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/files", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusInternalServerError)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	builder, _, _ := newTestBuilder(t, server.URL, &fakeNotifier{})
	builder.sleepFn = func(context.Context, time.Duration) bool { return false }
	path, size, digest, err := builder.collect(context.Background(), "ses_1")
	if path != "" || size != 0 || digest != "" || err != nil {
		t.Fatalf("collect = %q %d %q %v", path, size, digest, err)
	}
}

func TestCollectSkipsUnrelatedFiles(t *testing.T) {
	archive := testArtifact(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/files", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"data":[`+
			`{"id":"other","filename":"notes.txt","downloadable":true,"size_bytes":1,"created_at":"2026-01-02T03:04:05Z"},`+
			`{"id":"offline","filename":"artifact.tar.gz","downloadable":false,"size_bytes":3,"created_at":"2026-01-02T03:04:05Z"},`+
			`{"id":"file_1","filename":"artifact.tar.gz","downloadable":true,"size_bytes":`+strconv.Itoa(len(archive))+`,"created_at":"2026-01-02T04:04:05Z"}`+
			`],"has_more":false}`)
	})
	mux.HandleFunc("GET /v1/files/file_1/content", func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	builder, _, _ := newTestBuilder(t, server.URL, &fakeNotifier{})
	path, size, digest, err := builder.collect(context.Background(), "ses_1")
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	defer os.Remove(path)
	if size != int64(len(archive)) || digest == "" {
		t.Fatalf("collect = %q %d %q", path, size, digest)
	}
}

func TestCollectReportsWhenNoTempFileCanBeCreated(t *testing.T) {
	archive := testArtifact(t)
	server := newAgentServer(t, "complete", archive, &recorder{})
	defer server.Close()
	builder, _, _ := newTestBuilder(t, server.URL, &fakeNotifier{})
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing", "dir"))
	if _, _, _, err := builder.collect(context.Background(), "ses_1"); err == nil {
		t.Fatal("collect succeeded without a temp directory")
	}
}

func TestUserMessageMapsArtifactCodes(t *testing.T) {
	for _, tc := range []struct {
		err     error
		message string
	}{
		{&artifact.Error{Code: "ARTIFACT_TOO_LARGE"}, "构建产物超过大小限制"},
		{&artifact.Error{Code: "ARTIFACT_CORRUPT"}, "构建产物损坏"},
		{&artifact.Error{Code: "INVALID_ARTIFACT"}, "构建产物校验失败"},
		{errors.New("other"), "构建失败"},
	} {
		if got := userMessage(tc.err); got != tc.message {
			t.Fatalf("userMessage(%v) = %q, want %q", tc.err, got, tc.message)
		}
	}
}

func TestWatchBacksOffAfterConsecutiveStreamFailures(t *testing.T) {
	rec := &recorder{}
	stream := func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}
	server := builderTestServer(t, stream, nil, "running", rec)
	defer server.Close()

	builder, registry, _ := newTestBuilder(t, server.URL, &fakeNotifier{})
	builder.cfg.BuildTimeout = 30 * time.Millisecond
	var mu sync.Mutex
	var delays []time.Duration
	builder.sleepFn = func(_ context.Context, d time.Duration) bool {
		mu.Lock()
		delays = append(delays, d)
		mu.Unlock()
		return true
	}
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	registry.Add(app.App{ID: "app_1", Owner: owner, State: app.StateBuilding, BuildStartedAt: time.Now()})

	builder.watch("app_1", "ses_1")

	mu.Lock()
	got := append([]time.Duration(nil), delays...)
	mu.Unlock()
	if len(got) < 3 {
		t.Fatalf("reconnect attempts = %v", got)
	}
	if got[0] != time.Second || got[1] != 2*time.Second || got[2] != 4*time.Second {
		t.Fatalf("backoff did not grow across failed connections: %v", got[:3])
	}
}
