package build

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
)

// newBrokenPlatformBuilder returns a builder whose platform can never be resolved.
func newBrokenPlatformBuilder(t *testing.T) (*Builder, *app.Registry, *fakeNotifier) {
	t.Helper()
	registry := app.NewRegistry()
	notifier := &fakeNotifier{}
	builder := NewWithProvider(func(context.Context) (Platform, error) {
		return Platform{}, errors.New("platform unavailable")
	}, nil, registry, notifier, Config{
		BuildTimeout:   time.Second,
		InterruptGrace: 10 * time.Millisecond,
	})
	return builder, registry, notifier
}

// newContinueServer serves the AgentAPI calls Continue makes. statuses are
// returned by successive session GETs (the last one repeats); a non-zero
// getHTTP or submitHTTP status makes that endpoint fail.
func newContinueServer(t *testing.T, statuses []string, getHTTP, submitHTTP int, rec *recorder) *httptest.Server {
	t.Helper()
	var gets atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/sessions/ses_1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if getHTTP != 0 {
			w.WriteHeader(getHTTP)
			io.WriteString(w, `{"type":"error","error":{"type":"api_error","message":"get failed"}}`)
			return
		}
		status := ""
		if len(statuses) > 0 {
			index := int(gets.Add(1)) - 1
			if index >= len(statuses) {
				index = len(statuses) - 1
			}
			status = statuses[index]
		}
		io.WriteString(w, `{"id":"ses_1","status":"`+status+`"}`)
	})
	mux.HandleFunc("POST /v1/sessions/ses_1/events", func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		if submitHTTP != 0 {
			w.WriteHeader(submitHTTP)
			io.WriteString(w, `{"type":"error","error":{"type":"api_error","message":"submit failed"}}`)
			return
		}
		io.WriteString(w, `{}`)
	})
	mux.HandleFunc("GET /v1/sessions/ses_1/events/stream", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
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

func TestPlatformForMissingOrFailingProvider(t *testing.T) {
	missing := NewWithProvider(nil, nil, app.NewRegistry(), &fakeNotifier{}, Config{})
	if _, err := missing.platformFor(context.Background()); !errors.Is(err, app.ErrPlatformNotConfigured) {
		t.Fatalf("nil provider err = %v", err)
	}

	incomplete := NewWithProvider(func(context.Context) (Platform, error) {
		return Platform{AgentID: "agent_1"}, nil
	}, nil, app.NewRegistry(), &fakeNotifier{}, Config{})
	if _, err := incomplete.platformFor(context.Background()); !errors.Is(err, app.ErrPlatformNotConfigured) {
		t.Fatalf("incomplete provider err = %v", err)
	}

	broken, _, _ := newBrokenPlatformBuilder(t)
	if _, err := broken.platformFor(context.Background()); err == nil || !strings.Contains(err.Error(), "platform unavailable") {
		t.Fatalf("provider err = %v", err)
	}
}

func TestContinueReportsUnavailableSessions(t *testing.T) {
	ctx := context.Background()
	value := app.App{ID: "app_1", State: app.StateBuilding, SessionID: "ses_1", Prompt: "改一下"}

	t.Run("platform error", func(t *testing.T) {
		builder, registry, _ := newBrokenPlatformBuilder(t)
		registry.Add(value)
		if err := builder.Continue(ctx, value); err == nil || !strings.Contains(err.Error(), "platform unavailable") {
			t.Fatalf("err = %v", err)
		}
	})

	for name, tc := range map[string]struct {
		statuses []string
		get      int
		submit   int
		want     error
		text     string
	}{
		"session missing":     {get: http.StatusNotFound, want: ErrSessionUnavailable},
		"session query fails": {get: http.StatusInternalServerError, want: ErrSessionUnavailable},
		"terminated":          {statuses: []string{"terminated"}, want: ErrSessionUnavailable},
		"unknown status":      {statuses: []string{"paused"}, want: ErrSessionUnavailable},
		"running":             {statuses: []string{"running"}, want: ErrSessionBusy},
		"rescheduling":        {statuses: []string{"rescheduling"}, want: ErrSessionBusy},
		"submit conflict":     {statuses: []string{"idle"}, submit: http.StatusConflict, want: ErrSessionBusy},
		"submit lost session": {statuses: []string{"idle", "terminated"}, submit: http.StatusInternalServerError, want: ErrSessionUnavailable},
		"submit empty status": {statuses: []string{"idle", ""}, submit: http.StatusInternalServerError, want: ErrSessionUnavailable},
		"submit other status": {statuses: []string{"idle", "paused"}, submit: http.StatusInternalServerError, text: "submit update task"},
	} {
		t.Run(name, func(t *testing.T) {
			server := newContinueServer(t, tc.statuses, tc.get, tc.submit, nil)
			defer server.Close()
			builder, registry, _ := newTestBuilder(t, server.URL, &fakeNotifier{})
			entry := registry.Add(value)
			err := builder.Continue(ctx, value)
			if tc.want != nil {
				if !errors.Is(err, tc.want) {
					t.Fatalf("err = %v, want %v", err, tc.want)
				}
				if entry.Snapshot().State != app.StateBuilding {
					t.Fatalf("snapshot = %+v", entry.Snapshot())
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.text) {
				t.Fatalf("err = %v, want text %q", err, tc.text)
			}
		})
	}
}

func TestContinueKeepsWatchingWhenSubmitFailsButSessionRuns(t *testing.T) {
	rec := &recorder{}
	server := newContinueServer(t, []string{"idle", "running"}, 0, http.StatusInternalServerError, rec)
	defer server.Close()
	builder, registry, _ := newTestBuilder(t, server.URL, &fakeNotifier{})
	builder.sleepFn = func(context.Context, time.Duration) bool { return false }
	entry := registry.Add(app.App{ID: "app_1", State: app.StateBuilding, SessionID: "ses_1", Prompt: "改一下"})

	if err := builder.Continue(context.Background(), entry.Snapshot()); err != nil {
		t.Fatalf("Continue: %v", err)
	}
	waitFor(t, 3*time.Second, func() bool {
		snapshot := entry.Snapshot()
		return snapshot.State == app.StateError && snapshot.Error != nil && snapshot.Error.Message == "构建超时"
	})
	waitFor(t, 3*time.Second, func() bool { return len(rec.archiveList()) == 1 })
}

func TestCompatibilityHelpersReportMissingPlatform(t *testing.T) {
	builder, registry, notifier := newBrokenPlatformBuilder(t)
	entry := registry.Add(app.App{ID: "app_1", State: app.StateBuilding, SessionID: "ses_1", BuildStartedAt: time.Now()})

	builder.Resume(entry.Snapshot())
	if snapshot := entry.Snapshot(); snapshot.State != app.StateError || snapshot.Error == nil || snapshot.Error.Message != "无法恢复构建" {
		t.Fatalf("Resume snapshot = %+v", snapshot)
	}

	entry.Update(func(a *app.App) {
		a.State = app.StateBuilding
		a.Error = nil
	})
	builder.watch("app_1", "ses_1")
	if snapshot := entry.Snapshot(); snapshot.State != app.StateError || snapshot.Error == nil || snapshot.Error.Message != "无法恢复构建" {
		t.Fatalf("watch snapshot = %+v", snapshot)
	}

	if status := builder.sessionStatus("ses_1"); status != "" {
		t.Fatalf("sessionStatus = %q", status)
	}

	entry.Update(func(a *app.App) {
		a.State = app.StateBuilding
		a.Error = nil
	})
	builder.complete("app_1", "ses_1")
	if snapshot := entry.Snapshot(); snapshot.State != app.StateBuilding {
		t.Fatalf("complete changed state: %+v", snapshot)
	}
	builder.timeout("app_1", "ses_1")
	if snapshot := entry.Snapshot(); snapshot.State != app.StateBuilding {
		t.Fatalf("timeout changed state: %+v", snapshot)
	}
	builder.archive("ses_1")
	if notes := notifier.list(); len(notes) != 2 {
		t.Fatalf("notes = %+v", notes)
	}

	if _, _, _, err := builder.collect(context.Background(), "ses_1", collectOpts{}); err == nil || !strings.Contains(err.Error(), "platform unavailable") {
		t.Fatalf("collect err = %v", err)
	}
}

func TestWatchFailsWhenCursorReachesTerminatedSession(t *testing.T) {
	rec := &recorder{}
	stream := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		sseWrite(w, `{"id":"e1","type":"session.status_running"}`)
	}
	server := builderTestServer(t, stream, nil, "terminated", rec)
	defer server.Close()
	notifier := &fakeNotifier{}
	builder, registry, _ := newTestBuilder(t, server.URL, notifier)
	entry := registry.Add(app.App{
		ID: "app_1", State: app.StateBuilding, SessionID: "ses_1",
		BuildStartedAt: time.Now().UTC(), EventCursor: "e1",
	})

	builder.Resume(entry.Snapshot())
	waitFor(t, 3*time.Second, func() bool { return entry.Snapshot().State == app.StateError })
	snapshot := entry.Snapshot()
	if snapshot.Error == nil || snapshot.Error.Message != "构建会话已终止" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	waitFor(t, 3*time.Second, func() bool { return len(rec.archiveList()) == 1 })
	if notes := notifier.list(); len(notes) != 1 || notes[0].State != app.StateError {
		t.Fatalf("notes = %+v", notes)
	}
}

func TestCompleteResumesWatchForStaleArtifactWhileSessionRuns(t *testing.T) {
	rec := &recorder{}
	const staleFiles = `{"data":[{"id":"file_old","filename":"artifact.tar.gz","downloadable":true,"size_bytes":5,"created_at":"2020-01-01T00:00:00Z"}],"has_more":false}`
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/sessions/ses_1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"ses_1","status":"running"}`)
	})
	mux.HandleFunc("GET /v1/files", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, staleFiles)
	})
	mux.HandleFunc("POST /v1/sessions/ses_1/events", func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{}`)
	})
	mux.HandleFunc("POST /v1/sessions/ses_1/archive", func(w http.ResponseWriter, r *http.Request) {
		rec.addArchive("ses_1")
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"ses_1","status":"terminated"}`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	notifier := &fakeNotifier{}
	builder, registry, _ := newTestBuilder(t, server.URL, notifier)
	var sleeps atomic.Int32
	// Three sleeps let collect exhaust its retries and settle on the stale
	// candidate; the resumed watch then stops on the next sleep.
	builder.sleepFn = func(context.Context, time.Duration) bool { return sleeps.Add(1) <= 3 }
	entry := registry.Add(app.App{
		ID: "app_1", State: app.StateBuilding, SessionID: "ses_1",
		BuildStartedAt: time.Now().UTC(),
	})

	builder.complete("app_1", "ses_1")
	waitFor(t, 3*time.Second, func() bool {
		snapshot := entry.Snapshot()
		return snapshot.State == app.StateError && snapshot.Error != nil && snapshot.Error.Message == "构建超时"
	})
	waitFor(t, 3*time.Second, func() bool { return len(rec.archiveList()) == 1 })
	if notes := notifier.list(); len(notes) != 1 || notes[0].State != app.StateError {
		t.Fatalf("notes = %+v", notes)
	}
}
