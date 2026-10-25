package build

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/artifact"
	"github.com/open-anywhere/open-anywhere/pkg/agentapi"
)

type capturingBuilderStore struct {
	mu    sync.Mutex
	saved []app.App
}

func (c *capturingBuilderStore) SaveApp(a app.App) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.saved = append(c.saved, a)
	return nil
}
func (*capturingBuilderStore) DeleteApp(string) error { return nil }

func (c *capturingBuilderStore) last() (app.App, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.saved) == 0 {
		return app.App{}, false
	}
	return c.saved[len(c.saved)-1], true
}

type recorder struct {
	mu       sync.Mutex
	prompts  []string
	archived []string
	metadata map[string]string
}

func (r *recorder) addPrompt(text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prompts = append(r.prompts, text)
}

func (r *recorder) promptList() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.prompts...)
}

func (r *recorder) addArchive(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.archived = append(r.archived, id)
}

func (r *recorder) archiveList() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.archived...)
}

func (r *recorder) setMetadata(metadata map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metadata = metadata
}

func (r *recorder) metadataValue() map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.metadata
}

type fakeNotifier struct {
	mu    sync.Mutex
	notes []app.Notification
}

func (n *fakeNotifier) Notify(_ app.Identity, notification app.Notification) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.notes = append(n.notes, notification)
}

func (n *fakeNotifier) list() []app.Notification {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]app.Notification(nil), n.notes...)
}

func TestBuilderCompletesBuild(t *testing.T) {
	archive := testArtifact(t)
	rec := &recorder{}
	server := newAgentServer(t, "complete", archive, rec)
	defer server.Close()
	notifier := &fakeNotifier{}
	builder, registry, artifacts := newTestBuilder(t, server.URL, notifier)

	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := registry.Add(app.App{ID: "app_1", Owner: owner, Prompt: "做一个小游戏", PlayerNames: []string{"韩鲁鹏", "邓军辉"}, State: app.StateBuilding, BuildStartedAt: time.Now().UTC()})
	if err := builder.Start(context.Background(), entry.Snapshot()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, 5*time.Second, func() bool { return entry.Snapshot().State != app.StateBuilding })

	snapshot := entry.Snapshot()
	if snapshot.State != app.StateIdle {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if snapshot.Name != "quiz" || snapshot.OpenAPI == nil {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if _, ok := snapshot.OpenAPI.Lookup("rollDice"); !ok {
		t.Fatal("OpenAPI was not indexed")
	}
	if snapshot.Artifact.DownloadURL == "" || snapshot.Artifact.SHA256 == "" || snapshot.Artifact.Size != int64(len(archive)) {
		t.Fatalf("artifact = %+v", snapshot.Artifact)
	}
	if snapshot.BuildDuration < 0 || snapshot.LastAgentMessage != "" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if _, ok := artifacts.Get("app_1"); !ok {
		t.Fatal("artifact was not stored")
	}
	waitFor(t, 2*time.Second, func() bool { return len(notifier.list()) == 1 })
	time.Sleep(100 * time.Millisecond)
	if archived := rec.archiveList(); len(archived) != 0 {
		t.Fatalf("successful build must keep the session idle, archives = %+v", archived)
	}
	notes := notifier.list()
	if notes[0].State != app.StateIdle || notes[0].Message != "APP 创建完成" {
		t.Fatalf("notes = %+v", notes)
	}
	prompts := rec.promptList()
	if len(prompts) != 1 || !strings.Contains(prompts[0], "做一个小游戏") || !strings.Contains(prompts[0], "app-builder 技能") {
		t.Fatalf("prompts = %+v", prompts)
	}
	if !strings.Contains(prompts[0], `初始玩家（按加入顺序）：["韩鲁鹏","邓军辉"]`) {
		t.Fatalf("prompt does not carry player names: %q", prompts[0])
	}
	if metadata := rec.metadataValue(); metadata["app_id"] != "app_1" {
		t.Fatalf("metadata = %+v", metadata)
	}
}

func TestBuilderReportsAgentStop(t *testing.T) {
	rec := &recorder{}
	server := newAgentServer(t, "fail", nil, rec)
	defer server.Close()
	notifier := &fakeNotifier{}
	builder, registry, _ := newTestBuilder(t, server.URL, notifier)

	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := registry.Add(app.App{ID: "app_1", Owner: owner, Prompt: "做一个小游戏", State: app.StateBuilding, BuildStartedAt: time.Now().UTC()})
	if err := builder.Start(context.Background(), entry.Snapshot()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, 5*time.Second, func() bool { return entry.Snapshot().State != app.StateBuilding })

	snapshot := entry.Snapshot()
	if snapshot.State != app.StateError || snapshot.Error == nil {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if snapshot.Error.Code != "BUILD_FAILED" || snapshot.Error.Stage != app.StageBuilding || snapshot.Error.Message != "构建需要人工处理" {
		t.Fatalf("error = %+v", snapshot.Error)
	}
	if snapshot.Error.LastAgentMessage != "正在实现页面" {
		t.Fatalf("agent message = %q", snapshot.Error.LastAgentMessage)
	}
	waitFor(t, 2*time.Second, func() bool { return len(notifier.list()) == 1 })
	if notes := notifier.list(); notes[0].State != app.StateError || notes[0].Error == nil {
		t.Fatalf("notes = %+v", notes)
	}
	waitFor(t, 2*time.Second, func() bool { return len(rec.archiveList()) == 1 })
}

func TestBuilderReportsThinkingAsProgress(t *testing.T) {
	archive := testArtifact(t)
	rec := &recorder{}
	gate := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(gate) }) }
	server := newAgentServerWithGate(t, "thinking", archive, rec, gate)
	defer server.Close()
	defer release()
	notifier := &fakeNotifier{}
	builder, registry, _ := newTestBuilder(t, server.URL, notifier)

	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := registry.Add(app.App{ID: "app_1", Owner: owner, Prompt: "做一个小游戏", State: app.StateBuilding, BuildStartedAt: time.Now().UTC()})
	if err := builder.Start(context.Background(), entry.Snapshot()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	waitFor(t, 5*time.Second, func() bool {
		return entry.Snapshot().BuildingMessage() == "先读模板和接口约定。然后写 app.js，再自测。"
	})
	if got := entry.Snapshot().LastAgentMessage; got != "" {
		t.Fatalf("thinking must not become the agent message: %q", got)
	}

	release()
	waitFor(t, 5*time.Second, func() bool { return entry.Snapshot().State == app.StateIdle })
	if got := entry.Snapshot().LastProgress; got != "" {
		t.Fatalf("progress was not cleared after the build: %q", got)
	}
}

func TestRecordProgress(t *testing.T) {
	builder, registry, _ := newTestBuilder(t, "http://127.0.0.1:1", &fakeNotifier{})
	entry := registry.Add(app.App{ID: "app_1", State: app.StateBuilding})

	builder.recordThinking("app_1", "   ")
	if got := entry.Snapshot().LastProgress; got != "" {
		t.Fatalf("blank thinking changed progress: %q", got)
	}
	builder.recordThinking("app_1", "Now write app.js.")
	if got := entry.Snapshot().BuildingMessage(); got != "Now write app.js." {
		t.Fatalf("message = %q", got)
	}
	builder.recordMessage("app_1", "正在实现页面")
	snapshot := entry.Snapshot()
	if snapshot.LastAgentMessage != "正在实现页面" || snapshot.LastProgress != "正在实现页面" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	entry.Update(func(a *app.App) { a.State = app.StateIdle })
	builder.recordThinking("app_1", "late thinking")
	if got := entry.Snapshot().LastProgress; got != "正在实现页面" {
		t.Fatalf("thinking was recorded outside building: %q", got)
	}
}

func TestProgressText(t *testing.T) {
	short := "先读模板和接口约定。然后写 app.js。"
	if got := progressText(short); got != short {
		t.Fatalf("short text = %q", got)
	}
	long := "先读模板和接口约定。" + strings.Repeat("细节", 300)
	if got := progressText(long); got != "先读模板和接口约定。" {
		t.Fatalf("long text = %q", got)
	}
	withoutStop := strings.Repeat("长", 300)
	if got := progressText(withoutStop); utf8.RuneCountInString(got) != 200 {
		t.Fatalf("truncated runes = %d", utf8.RuneCountInString(got))
	}
}

func TestBuilderStartFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, `{"error":{"code":"BOOM","message":"no capacity"}}`)
	}))
	defer server.Close()
	builder, registry, _ := newTestBuilder(t, server.URL, &fakeNotifier{})

	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := registry.Add(app.App{ID: "app_1", Owner: owner, State: app.StateBuilding})
	err := builder.Start(context.Background(), entry.Snapshot())
	if err == nil || !strings.Contains(err.Error(), "create session") {
		t.Fatalf("error = %v", err)
	}
	if entry.Snapshot().State != app.StateBuilding {
		t.Fatal("app state changed after a failed start")
	}
}

func TestResumeWithoutSessionFails(t *testing.T) {
	builder, registry, _ := newTestBuilder(t, "http://127.0.0.1:1", &fakeNotifier{})
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := registry.Add(app.App{ID: "app_1", Owner: owner, State: app.StateBuilding})
	builder.Resume(entry.Snapshot())

	waitFor(t, time.Second, func() bool { return entry.Snapshot().State == app.StateError })
	snapshot := entry.Snapshot()
	if snapshot.Error == nil || snapshot.Error.Message != "无法恢复构建" || snapshot.Error.Code != "BUILD_FAILED" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestWatchIgnoresReplayedPriorTurnWhenCursorSet(t *testing.T) {
	// Continue opens a fresh watch against a session whose SSE replay includes
	// the previous turn's agent.message + end_turn. Without EventCursor, that
	// replay falsely completes the update in seconds with the old package.
	archive := testArtifact(t)
	priorDone := make(chan struct{})
	releaseNew := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/sessions/ses_1/events/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, _ := w.(http.Flusher)
			write := func(data string) {
				io.WriteString(w, "event: message\ndata: "+data+"\n\n")
				if flusher != nil {
					flusher.Flush()
				}
			}
			// Prior turn replay — must be ignored when EventCursor is e_prior_idle.
			write(`{"id":"e_prior_msg","type":"agent.message","content":[{"type":"text","text":"上一轮已完成"}]}`)
			write(`{"id":"e_prior_idle","type":"session.status_idle","stop_reason":{"type":"end_turn"}}`)
			close(priorDone)
			<-releaseNew
			write(`{"id":"e_new_run","type":"session.status_running"}`)
			write(`{"id":"e_new_msg","type":"agent.message","content":[{"type":"text","text":"正在改春节主题"}]}`)
			write(`{"id":"e_new_idle","type":"session.status_idle","stop_reason":{"type":"end_turn"}}`)
		case "/v1/sessions/ses_1":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"id":"ses_1","status":"idle"}`)
		case "/v1/files":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{
					"id": "file_new", "filename": "artifact.tar.gz", "downloadable": true,
					"size_bytes": len(archive), "created_at": "2026-10-25T07:30:00Z",
				}},
				"has_more": false,
			})
		case "/v1/files/file_new/content":
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	notifier := &fakeNotifier{}
	builder, registry, _ := newTestBuilder(t, server.URL, notifier)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := registry.Add(app.App{
		ID: "app_1", Owner: owner, Prompt: "改成春节主题", State: app.StateBuilding,
		SessionID: "ses_1", EventCursor: "e_prior_idle", BuildStartedAt: time.Now().UTC(),
		Name: "welcome-home-particles",
		Artifact: app.Artifact{SHA256: "old", Path: "old.tar.gz"},
	})

	go builder.watch("app_1", "ses_1")
	select {
	case <-priorDone:
	case <-time.After(2 * time.Second):
		t.Fatal("prior replay not delivered")
	}
	// Give the false-complete path time to fire if cursor skip is broken.
	time.Sleep(300 * time.Millisecond)
	if entry.Snapshot().State != app.StateBuilding {
		t.Fatalf("replayed prior end_turn completed the update early: %+v", entry.Snapshot())
	}
	close(releaseNew)
	waitFor(t, 5*time.Second, func() bool { return entry.Snapshot().State != app.StateBuilding })
	snapshot := entry.Snapshot()
	if snapshot.State != app.StateIdle || snapshot.Artifact.SHA256 == "old" || snapshot.EventCursor != "e_new_idle" {
		t.Fatalf("expected new-turn completion, got %+v", snapshot)
	}
	if notes := notifier.list(); len(notes) != 1 || notes[0].Message != "APP 更新完成" {
		t.Fatalf("notes = %+v", notes)
	}
}

func TestBuilderContinueUpdates(t *testing.T) {
	archive := testArtifact(t)
	rec := &recorder{}
	server := newAgentServer(t, "complete", archive, rec)
	defer server.Close()
	notifier := &fakeNotifier{}
	builder, registry, _ := newTestBuilder(t, server.URL, notifier)
	store := &capturingBuilderStore{}
	registry.SetPersister(store)

	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := registry.Add(app.App{
		ID: "app_1", Owner: owner, Prompt: "题目改成动物主题", State: app.StateBuilding,
		SessionID: "ses_1", BuildStartedAt: time.Now().UTC(),
		Name: "quiz", Artifact: app.Artifact{SHA256: "old", Path: "old.tar.gz", DownloadURL: "http://old"},
	})
	if err := builder.Continue(context.Background(), entry.Snapshot()); err != nil {
		t.Fatalf("Continue: %v", err)
	}
	waitFor(t, 5*time.Second, func() bool { return entry.Snapshot().State != app.StateBuilding })

	snapshot := entry.Snapshot()
	if snapshot.State != app.StateIdle || snapshot.Artifact.SHA256 == "old" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	saved, ok := store.last()
	if !ok || saved.State != app.StateIdle || saved.Artifact.SHA256 == "old" || saved.SessionID != "ses_1" {
		t.Fatalf("continue success was not persisted: %+v", saved)
	}
	waitFor(t, 2*time.Second, func() bool { return len(notifier.list()) == 1 })
	if notes := notifier.list(); notes[0].Message != "APP 更新完成" {
		t.Fatalf("notes = %+v", notes)
	}
	prompts := rec.promptList()
	if len(prompts) != 1 || !strings.Contains(prompts[0], "题目改成动物主题") || !strings.Contains(prompts[0], "重新交付完整的 artifact.tar.gz") {
		t.Fatalf("prompts = %+v", prompts)
	}
	if archived := rec.archiveList(); len(archived) != 0 {
		t.Fatalf("update must keep the session: %v", archived)
	}
}

func TestBuilderContinueRejectsBusyOrMissingSession(t *testing.T) {
	t.Run("busy", func(t *testing.T) {
		server := builderTestServer(t, func(w http.ResponseWriter, r *http.Request) {}, nil, "running", nil)
		defer server.Close()
		builder, _, _ := newTestBuilder(t, server.URL, &fakeNotifier{})
		err := builder.Continue(context.Background(), app.App{ID: "app_1", SessionID: "ses_1", Prompt: "x"})
		if err != ErrSessionBusy {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("terminated", func(t *testing.T) {
		server := builderTestServer(t, func(w http.ResponseWriter, r *http.Request) {}, nil, "terminated", nil)
		defer server.Close()
		builder, _, _ := newTestBuilder(t, server.URL, &fakeNotifier{})
		err := builder.Continue(context.Background(), app.App{ID: "app_1", SessionID: "ses_1", Prompt: "x"})
		if err != ErrSessionUnavailable {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("missing session id", func(t *testing.T) {
		builder, _, _ := newTestBuilder(t, "http://127.0.0.1:1", &fakeNotifier{})
		if err := builder.Continue(context.Background(), app.App{ID: "app_1", Prompt: "x"}); err != ErrSessionUnavailable {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestFailKeepsPreviousArtifact(t *testing.T) {
	builder, registry, _ := newTestBuilder(t, "http://127.0.0.1:1", &fakeNotifier{})
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	store := &capturingBuilderStore{}
	registry.SetPersister(store)
	entry := registry.Add(app.App{
		ID: "app_1", Owner: owner, State: app.StateBuilding,
		Artifact: app.Artifact{SHA256: "keep", Path: "keep.tar.gz"},
	})
	if kept := builder.fail("app_1", "构建需要人工处理", nil); !kept {
		t.Fatal("expected previous artifact to be kept")
	}
	snapshot := entry.Snapshot()
	if snapshot.State != app.StateIdle || snapshot.Artifact.SHA256 != "keep" || snapshot.Error != nil {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	saved, ok := store.last()
	if !ok || saved.State != app.StateIdle || saved.Artifact.SHA256 != "keep" {
		t.Fatalf("fail did not persist idle+previous artifact: %+v", saved)
	}
	notes := builder.notifier.(*fakeNotifier).list()
	if len(notes) != 1 || notes[0].State != app.StateIdle || !strings.Contains(notes[0].Message, "更新失败") {
		t.Fatalf("notes = %+v", notes)
	}
}

func TestFailDoesNotPublishWhenPersistFails(t *testing.T) {
	builder, registry, _ := newTestBuilder(t, "http://127.0.0.1:1", &fakeNotifier{})
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := registry.Add(app.App{
		ID: "app_1", Owner: owner, State: app.StateBuilding,
		Artifact: app.Artifact{SHA256: "keep", Path: "keep.tar.gz"},
	})
	registry.SetPersister(failingBuilderStore{func(app.App) bool { return true }})
	if kept := builder.fail("app_1", "构建需要人工处理", nil); kept {
		t.Fatal("fail reported kept despite persist failure")
	}
	if entry.Snapshot().State != app.StateBuilding {
		t.Fatalf("memory changed despite persist failure: %+v", entry.Snapshot())
	}
	if notes := builder.notifier.(*fakeNotifier).list(); len(notes) != 0 {
		t.Fatalf("notes = %+v", notes)
	}
}

func newTestBuilder(t *testing.T, serverURL string, notifier *fakeNotifier) (*Builder, *app.Registry, *artifact.Store) {
	t.Helper()
	registry := app.NewRegistry()
	artifacts, err := artifact.NewStore(filepath.Join(t.TempDir(), "artifacts"), "http://gateway:8090", nil)
	if err != nil {
		t.Fatal(err)
	}
	builder := New(agentapi.New(serverURL, "key", 2*time.Second), artifacts, registry, notifier, Config{
		AgentID:        "agent_1",
		AgentVersion:   3,
		EnvironmentID:  "env_1",
		BuildTimeout:   10 * time.Second,
		InterruptGrace: 10 * time.Millisecond,
		ArtifactLimit:  1 << 20,
	})
	return builder, registry, artifacts
}

func newAgentServer(t *testing.T, mode string, archive []byte, rec *recorder) *httptest.Server {
	t.Helper()
	return newAgentServerWithGate(t, mode, archive, rec, nil)
}

func newAgentServerWithGate(t *testing.T, mode string, archive []byte, rec *recorder, gate <-chan struct{}) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Agent struct {
				Type    string `json:"type"`
				ID      string `json:"id"`
				Version int    `json:"version"`
			} `json:"agent"`
			EnvironmentID string            `json:"environment_id"`
			Metadata      map[string]string `json:"metadata"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode session: %v", err)
			return
		}
		if body.Agent.ID != "agent_1" || body.Agent.Version != 3 || body.EnvironmentID != "env_1" {
			t.Errorf("session body = %+v", body)
		}
		rec.setMetadata(body.Metadata)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"ses_1","status":"idle"}`)
	})
	mux.HandleFunc("POST /v1/sessions/ses_1/events", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Events []struct {
				Type    string `json:"type"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"events"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode events: %v", err)
			return
		}
		for _, event := range body.Events {
			if event.Type == "user.message" && len(event.Content) > 0 {
				rec.addPrompt(event.Content[0].Text)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{}`)
	})
	mux.HandleFunc("GET /v1/sessions/ses_1/events/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		write := func(data string) {
			io.WriteString(w, "event: message\ndata: "+data+"\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		}
		if mode == "inactive-first" {
			write(`{"id":"e0","type":"session.status_idle","stop_reason":{"type":"end_turn"}}`)
		}
		write(`{"id":"e1","type":"session.status_running"}`)
		if mode == "thinking" {
			write(`{"id":"e2","type":"agent.thinking","content":[{"type":"text","text":"先读模板和接口约定。然后写 app.js，再自测。"}]}`)
			if gate != nil {
				<-gate
			}
			write(`{"id":"e3","type":"session.status_idle","stop_reason":{"type":"end_turn"}}`)
			return
		}
		write(`{"id":"e2","type":"agent.message","content":[{"type":"text","text":"正在实现页面"}]}`)
		if mode == "error-event" {
			write(`{"id":"e2b","type":"session.error","error":{"message":"重试中"}}`)
		}
		if mode == "terminated" {
			write(`{"id":"e3","type":"session.status_terminated"}`)
			return
		}
		if mode == "fail" {
			write(`{"id":"e3","type":"session.status_idle","stop_reason":{"type":"requires_action"}}`)
			return
		}
		write(`{"id":"e3","type":"session.status_idle","stop_reason":{"type":"end_turn"}}`)
	})
	mux.HandleFunc("GET /v1/sessions/ses_1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"ses_1","status":"idle"}`)
	})
	mux.HandleFunc("GET /v1/files", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if archive == nil {
			io.WriteString(w, `{"data":[],"has_more":false}`)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{
				"id":           "file_1",
				"filename":     "artifact.tar.gz",
				"downloadable": true,
				"size_bytes":   len(archive),
				"created_at":   "2026-01-02T03:04:05Z",
			}},
			"has_more": false,
		})
	})
	mux.HandleFunc("GET /v1/files/file_1/content", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(archive)
	})
	mux.HandleFunc("POST /v1/sessions/ses_1/archive", func(w http.ResponseWriter, r *http.Request) {
		rec.addArchive("ses_1")
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"ses_1","status":"terminated"}`)
	})
	return httptest.NewServer(mux)
}

func testArtifact(t *testing.T) []byte {
	t.Helper()
	entries := []struct {
		name string
		body string
		dir  bool
	}{
		{name: "app/", dir: true},
		{name: "app/app.js", body: "console.log('quiz')"},
		{name: "launch.json", body: `{"name":"quiz","runtimeExecutable":"node","runtimeArgs":["app/app.js"]}`},
		{name: "openapi.yaml", body: "openapi: 3.1.0\npaths:\n  /api/roll:\n    post:\n      operationId: rollDice\n      responses:\n        \"200\":\n          description: ok\n"},
	}
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

func TestBuildPrompt(t *testing.T) {
	without := BuildPrompt(" 做一个游戏 ", nil)
	if !strings.Contains(without, "需求：做一个游戏\n") {
		t.Fatalf("prompt = %q", without)
	}
	if strings.Contains(without, "初始玩家（按加入顺序）") {
		t.Fatalf("prompt has player names without input: %q", without)
	}
	if !strings.Contains(without, "请按系统提示词和 app-builder 技能完成构建。") {
		t.Fatalf("prompt does not reference the system prompt or skill: %q", without)
	}
	for _, want := range []string{"渲染画布：1280×720（16:9）", "不允许页面滚动", "距边缘至少 5%"} {
		if !strings.Contains(without, want) {
			t.Fatalf("prompt does not carry canvas constraint %q: %q", want, without)
		}
	}
	for _, constraint := range []string{"硬性要求", "artifact.tar.gz", "user_name", "launch.json", "不要提问"} {
		if strings.Contains(without, constraint) {
			t.Fatalf("prompt should leave %q to the system prompt: %q", constraint, without)
		}
	}
	with := BuildPrompt("做一个游戏", []string{"韩鲁鹏", "邓军辉"})
	if !strings.Contains(with, `初始玩家（按加入顺序）：["韩鲁鹏","邓军辉"]`) {
		t.Fatalf("prompt does not carry player names: %q", with)
	}
}

func TestUpdatePrompt(t *testing.T) {
	got := UpdatePrompt(" 题目改成动物主题 ")
	if !strings.Contains(got, "修改要求：\n题目改成动物主题\n") {
		t.Fatalf("prompt = %q", got)
	}
	if !strings.Contains(got, "重新交付完整的 artifact.tar.gz") {
		t.Fatalf("prompt = %q", got)
	}
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition was not met before the deadline")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
