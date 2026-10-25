package build

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
)

func TestBuildHelpers(t *testing.T) {
	reasons := map[string]string{
		"requires_action":   "构建需要人工处理",
		"interrupted":       "构建被中断",
		"budget_reached":    "构建超出执行预算",
		"retries_exhausted": "构建重试耗尽",
		"other":             "构建未正常结束",
	}
	for reason, want := range reasons {
		if got := stopReasonMessage(reason); got != want {
			t.Fatalf("stopReasonMessage(%q) = %q", reason, got)
		}
	}
	if reconnectDelay(0) != time.Second || reconnectDelay(5) != 30*time.Second || reconnectDelay(99) != 30*time.Second {
		t.Fatal("reconnectDelay is not monotonic")
	}
	if got := compact("  a \n b  "); got != "a b" {
		t.Fatalf("compact = %q", got)
	}
	long := strings.Repeat("好", 250)
	if got := compact(long); utf8.RuneCountInString(got) != 200 {
		t.Fatalf("compact length = %d", utf8.RuneCountInString(got))
	}
	if err := (&stageError{Code: "X"}); err.Error() != "X" {
		t.Fatalf("stageError = %q", err.Error())
	}
	if err := (&stageError{Code: "X", Cause: io.EOF}); err.Error() != "X: EOF" {
		t.Fatalf("stageError = %q", err.Error())
	}
	if got := userMessage(&stageError{Code: "X", User: "用户可读"}); got != "用户可读" {
		t.Fatalf("userMessage = %q", got)
	}
	if got := userMessage(io.EOF); got != "构建失败" {
		t.Fatalf("userMessage = %q", got)
	}

	env := newTestEnv(t)
	builder := &Builder{registry: env.registry}
	if !builder.sleep(context.Background(), 0) {
		t.Fatal("sleep should complete")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if builder.sleep(canceled, time.Hour) {
		t.Fatal("sleep should stop on cancel")
	}

	env.registry.Add(app.App{ID: "building", State: app.StateBuilding})
	env.registry.Add(app.App{ID: "idle", State: app.StateIdle})
	builder.recordStatus("building", "  重试中 ")
	builder.recordStatus("building", "   ")
	builder.recordStatus("idle", "ignored")
	builder.recordStatus("missing", "ignored")
	if value, _ := env.registry.Get("building"); value.Snapshot().LastStatusMessage != "重试中" {
		t.Fatalf("status = %q", value.Snapshot().LastStatusMessage)
	}
	builder.recordMessage("building", "  ")
}

// newTestEnv is a small builder environment without an HTTP server.
func newTestEnv(t *testing.T) *struct {
	registry *app.Registry
} {
	t.Helper()
	return &struct{ registry *app.Registry }{registry: app.NewRegistry()}
}

func TestSessionStatus(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/sessions/ses_1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"ses_1","status":"running"}`)
	})
	mux.HandleFunc("GET /v1/sessions/missing", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, `{"type":"error","error":{"type":"api_error","message":"gone"}}`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	builder, _, _ := newTestBuilder(t, server.URL, &fakeNotifier{})
	if status := builder.sessionStatus("ses_1"); status != "running" {
		t.Fatalf("status = %q", status)
	}
	if status := builder.sessionStatus("missing"); status != "" {
		t.Fatalf("status = %q", status)
	}
}

func TestTimeoutInterruptsAndArchives(t *testing.T) {
	rec := &recorder{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/sessions/ses_1/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{}`)
	})
	mux.HandleFunc("GET /v1/sessions/ses_1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"ses_1","status":"terminated"}`)
	})
	mux.HandleFunc("POST /v1/sessions/ses_1/archive", func(w http.ResponseWriter, r *http.Request) {
		rec.addArchive("ses_1")
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"ses_1","status":"terminated"}`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	builder, registry, _ := newTestBuilder(t, server.URL, &fakeNotifier{})
	builder.cfg.InterruptGrace = time.Millisecond
	builder.sleepFn = func(context.Context, time.Duration) bool { return true }
	entry := registry.Add(app.App{ID: "app_1", State: app.StateBuilding})
	builder.timeout("app_1", "ses_1")
	if entry.Snapshot().State != app.StateError || entry.Snapshot().Error == nil || entry.Snapshot().Error.Message != "构建超时" {
		t.Fatalf("snapshot = %+v", entry.Snapshot())
	}
	if len(rec.archiveList()) != 1 {
		t.Fatalf("archives = %+v", rec.archiveList())
	}
}

func TestCollectScenarios(t *testing.T) {
	const content = "artifact"
	tests := map[string]struct {
		mode  string
		limit int64
		want  string
	}{
		"success":        {},
		"empty":          {mode: "empty", want: "OUTPUT_MISSING"},
		"list error":     {mode: "list-error", want: "OUTPUT_DOWNLOAD_FAILED"},
		"download error": {mode: "download-error", want: "OUTPUT_DOWNLOAD_FAILED"},
		"too large":      {mode: "too-large", limit: 4, want: "ARTIFACT_TOO_LARGE"},
		"corrupt":        {mode: "corrupt", want: "ARTIFACT_CORRUPT"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			server := newCollectServer(t, test.mode, content)
			defer server.Close()
			builder, _, _ := newTestBuilder(t, server.URL, &fakeNotifier{})
			builder.sleepFn = func(context.Context, time.Duration) bool { return true }
			if test.limit > 0 {
				builder.cfg.ArtifactLimit = test.limit
			}
			path, size, digest, err := builder.collect(context.Background(), "ses_1", collectOpts{})
			if test.want == "" {
				if err != nil {
					t.Fatalf("collect: %v", err)
				}
				if size != int64(len(content)) || digest == "" {
					t.Fatalf("size = %d digest = %q", size, digest)
				}
				os.Remove(path)
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %s", err, test.want)
			}
		})
	}
}

func newCollectServer(t *testing.T, mode, content string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/files", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if mode == "list-error" {
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, `{"type":"error","error":{"type":"api_error","message":"boom"}}`)
			return
		}
		if mode == "empty" {
			io.WriteString(w, `{"data":[],"next_page":null}`)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{
				"id":           "file_1",
				"filename":     "artifact.tar.gz",
				"downloadable": true,
				"size_bytes":   len(content),
				"created_at":   "2026-01-02T03:04:05Z",
			}},
			"next_page": nil,
		})
	})
	mux.HandleFunc("GET /v1/files/file_1/content", func(w http.ResponseWriter, r *http.Request) {
		switch mode {
		case "download-error":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, `{"type":"error","error":{"type":"api_error","message":"boom"}}`)
		case "too-large":
			io.WriteString(w, "0123456789")
		case "corrupt":
			io.WriteString(w, "short")
		default:
			io.WriteString(w, content)
		}
	})
	return httptest.NewServer(mux)
}

func TestStartWhenSubmitFailsButSessionRuns(t *testing.T) {
	rec := &recorder{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"ses_1","status":"idle"}`)
	})
	mux.HandleFunc("POST /v1/sessions/ses_1/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, `{"type":"error","error":{"type":"api_error","message":"busy"}}`)
	})
	mux.HandleFunc("GET /v1/sessions/ses_1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"ses_1","status":"running"}`)
	})
	mux.HandleFunc("GET /v1/sessions/ses_1/events/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: message\ndata: {\"id\":\"e1\",\"type\":\"session.status_terminated\"}\n\n")
	})
	mux.HandleFunc("POST /v1/sessions/ses_1/archive", func(w http.ResponseWriter, r *http.Request) {
		rec.addArchive("ses_1")
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"ses_1","status":"terminated"}`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	builder, registry, _ := newTestBuilder(t, server.URL, &fakeNotifier{})
	entry := registry.Add(app.App{ID: "app_1", State: app.StateBuilding})
	if err := builder.Start(context.Background(), entry.Snapshot()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, 2*time.Second, func() bool { return entry.Snapshot().State == app.StateError })
	waitFor(t, 2*time.Second, func() bool { return len(rec.archiveList()) == 1 })
}

func TestStartSubmitFailureArchives(t *testing.T) {
	rec := &recorder{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"ses_1","status":"idle"}`)
	})
	mux.HandleFunc("POST /v1/sessions/ses_1/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, `{"type":"error","error":{"type":"api_error","message":"busy"}}`)
	})
	mux.HandleFunc("GET /v1/sessions/ses_1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, `{"type":"error","error":{"type":"api_error","message":"gone"}}`)
	})
	mux.HandleFunc("POST /v1/sessions/ses_1/archive", func(w http.ResponseWriter, r *http.Request) {
		rec.addArchive("ses_1")
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"ses_1","status":"terminated"}`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	builder, registry, _ := newTestBuilder(t, server.URL, &fakeNotifier{})
	entry := registry.Add(app.App{ID: "app_1", State: app.StateBuilding})
	err := builder.Start(context.Background(), entry.Snapshot())
	if err == nil || !strings.Contains(err.Error(), "submit build task") {
		t.Fatalf("error = %v", err)
	}
	if len(rec.archiveList()) != 1 {
		t.Fatalf("archives = %+v", rec.archiveList())
	}
}

func TestResumeCompletes(t *testing.T) {
	archive := testArtifact(t)
	server := newAgentServer(t, "complete", archive, &recorder{})
	defer server.Close()
	builder, registry, _ := newTestBuilder(t, server.URL, &fakeNotifier{})
	entry := registry.Add(app.App{ID: "app_1", State: app.StateBuilding, SessionID: "ses_1", BuildStartedAt: time.Now()})
	builder.Resume(entry.Snapshot())
	waitFor(t, 5*time.Second, func() bool { return entry.Snapshot().State == app.StateIdle })
}

func TestWatchModes(t *testing.T) {
	tests := map[string]struct {
		mode  string
		state app.State
	}{
		"inactive idle first": {mode: "inactive-first", state: app.StateIdle},
		"session error event": {mode: "error-event", state: app.StateIdle},
		"terminated":          {mode: "terminated", state: app.StateError},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			archive := testArtifact(t)
			server := newAgentServer(t, test.mode, archive, &recorder{})
			defer server.Close()
			builder, registry, _ := newTestBuilder(t, server.URL, &fakeNotifier{})
			entry := registry.Add(app.App{ID: "app_1", State: app.StateBuilding, BuildStartedAt: time.Now()})
			if err := builder.Start(context.Background(), entry.Snapshot()); err != nil {
				t.Fatalf("Start: %v", err)
			}
			waitFor(t, 5*time.Second, func() bool { return entry.Snapshot().State == test.state })
		})
	}
}
