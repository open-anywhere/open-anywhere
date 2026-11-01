package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/build"
	"github.com/open-anywhere/open-anywhere/internal/gateway/openapi"
	"github.com/open-anywhere/open-anywhere/internal/gateway/render"
	"github.com/open-anywhere/open-anywhere/pkg/casting"
)

const specYAML = `openapi: 3.1.0
paths:
  /api/items/{id}:
    post:
      operationId: updateItem
      parameters:
        - name: id
          in: path
          required: true
          schema: {type: string}
        - name: q
          in: query
          schema: {type: string}
      requestBody:
        content:
          application/json:
            schema: {type: object, properties: {n: {type: integer}}}
      responses:
        "200":
          description: ok
`

type fakeBuilder struct {
	mu        sync.Mutex
	started   []app.App
	continued []app.App
	err       error
	contErr   error
}

func (f *fakeBuilder) Start(_ context.Context, value app.App) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = append(f.started, value)
	return f.err
}

func (f *fakeBuilder) Continue(_ context.Context, value app.App) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.continued = append(f.continued, value)
	return f.contErr
}

func (f *fakeBuilder) setError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeBuilder) setContinueError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.contErr = err
}

func (f *fakeBuilder) requests() []app.App {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]app.App(nil), f.started...)
}

func (f *fakeBuilder) continueRequests() []app.App {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]app.App(nil), f.continued...)
}

type fakeRender struct {
	mu          sync.Mutex
	created     []render.CreateRequest
	ports       []render.Port
	listErr     error
	instance    render.App
	errs        []error
	deleted     []string
	deleteErr   error
	getInstance render.App
	getErr      error
}

func (f *fakeRender) Create(_ context.Context, request render.CreateRequest) (render.App, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, request)
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		if err != nil {
			return render.App{}, err
		}
	}
	return f.instance, nil
}

func (f *fakeRender) Get(_ context.Context, _ string) (render.App, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.getInstance, f.getErr
}

func (f *fakeRender) Delete(_ context.Context, instanceID string) (render.App, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, instanceID)
	if f.deleteErr != nil {
		return render.App{}, f.deleteErr
	}
	return render.App{InstanceID: instanceID, State: render.StateStopped}, nil
}

func (f *fakeRender) ListPorts(_ context.Context) ([]render.Port, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]render.Port(nil), f.ports...), f.listErr
}

func (f *fakeRender) GetPort(_ context.Context, port int) (render.Port, error) {
	return render.Port{Port: port}, nil
}

func (f *fakeRender) requests() []render.CreateRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]render.CreateRequest(nil), f.created...)
}

func (f *fakeRender) deletedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.deleted...)
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

func (n *fakeNotifier) notifications() []app.Notification {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]app.Notification(nil), n.notes...)
}

type testEnv struct {
	svc      *Service
	registry *app.Registry
	builder  *fakeBuilder
	render   *fakeRender
	notifier *fakeNotifier
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	env := &testEnv{
		registry: app.NewRegistry(),
		builder:  &fakeBuilder{},
		render: &fakeRender{
			instance:    readyApp(),
			getInstance: readyApp(),
		},
		notifier: &fakeNotifier{},
	}
	env.svc = New(env.registry, env.builder, env.render, env.notifier, Config{
		StartTimeout:      2 * time.Second,
		ProjectionTimeout: time.Nanosecond,
		OperationsTimeout: time.Second,
		MonitorInterval:   time.Hour,
		PortMin:           39000,
		PortMax:           39002,
	})
	return env
}

func readyApp() render.App {
	return render.App{
		AppID:      "app_1",
		InstanceID: "inst_1",
		State:      render.StateReady,
		Port:       39000,
		AppBaseURL: "http://127.0.0.1:39000",
		Source: casting.StreamSource{
			Type:       "stream",
			URL:        "http://127.0.0.1:39000/stream",
			Format:     "mpegts",
			VideoCodec: "h264",
			AudioCodec: "aac",
		},
	}
}

func mustSpec(t *testing.T) *openapi.Spec {
	t.Helper()
	spec, err := openapi.Parse([]byte(specYAML))
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func idleApp(t *testing.T, owner app.Identity) app.App {
	t.Helper()
	return app.App{
		ID:      "app_1",
		Owner:   owner,
		Name:    "quiz",
		State:   app.StateIdle,
		OpenAPI: mustSpec(t),
		Artifact: app.Artifact{
			DownloadURL: "http://gateway/artifacts/app_1/artifact.tar.gz?token=x",
		},
	}
}

func runningApp(owner app.Identity) app.App {
	return app.App{
		ID:                  "app_1",
		Owner:               owner,
		Name:                "quiz",
		State:               app.StateRunning,
		InstanceID:          "inst_1",
		Port:                39000,
		AppBaseURL:          "http://127.0.0.1:39000",
		StartedAt:           time.Now().Add(-time.Minute),
	}
}

func toolCode(t *testing.T, err error) string {
	t.Helper()
	var tool *ToolError
	if !errors.As(err, &tool) {
		t.Fatalf("error = %#v", err)
	}
	return tool.Code
}

func TestCreateApp(t *testing.T) {
	env := newTestEnv(t)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	ctx := context.Background()

	if _, err := env.svc.CreateApp(ctx, owner, "   ", nil); toolCode(t, err) != "INVALID_ARGUMENT" {
		t.Fatal("empty prompt should be rejected")
	}
	for _, names := range [][]string{{"韩鲁鹏", "  "}, {"韩鲁鹏", " 韩鲁鹏 "}} {
		if _, err := env.svc.CreateApp(ctx, owner, "做一个小游戏", names); toolCode(t, err) != "INVALID_PLAYER_NAMES" {
			t.Fatalf("player names %+v should be rejected", names)
		}
	}
	result, err := env.svc.CreateApp(ctx, owner, "做一个小游戏", []string{" 韩鲁鹏 ", "邓军辉"})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if result.AppID == "" || result.State != app.StateBuilding {
		t.Fatalf("result = %+v", result)
	}
	started := env.builder.requests()
	if len(started) != 1 || started[0].Prompt != "做一个小游戏" || started[0].State != app.StateBuilding {
		t.Fatalf("builder requests = %+v", started)
	}
	if len(started[0].PlayerNames) != 2 || started[0].PlayerNames[0] != "韩鲁鹏" || started[0].PlayerNames[1] != "邓军辉" {
		t.Fatalf("player names = %+v", started[0].PlayerNames)
	}

	again, err := env.svc.CreateApp(ctx, owner, "另一个需求", []string{"张之阳"})
	if err != nil || again.AppID == "" || again.AppID == result.AppID {
		t.Fatalf("concurrent create = %+v, %v", again, err)
	}
	if len(env.builder.requests()) != 2 {
		t.Fatalf("expected two builds, got %d", len(env.builder.requests()))
	}
	first, ok := env.registry.Get(result.AppID)
	if !ok {
		t.Fatal("first app was not registered")
	}
	if snap := first.Snapshot(); snap.Prompt != "做一个小游戏" || len(snap.PlayerNames) != 2 {
		t.Fatalf("first build input changed: %+v", snap)
	}
	second, ok := env.registry.Get(again.AppID)
	if !ok {
		t.Fatal("second app was not registered")
	}
	if snap := second.Snapshot(); snap.Prompt != "另一个需求" || len(snap.PlayerNames) != 1 || snap.PlayerNames[0] != "张之阳" {
		t.Fatalf("second build input = %+v", snap)
	}

	other, err := env.svc.CreateApp(ctx, app.Identity{DeviceID: "other-device", SessionID: "other"}, "其他", nil)
	if err != nil || other.AppID == result.AppID || other.AppID == again.AppID {
		t.Fatalf("other owner = %+v, %v", other, err)
	}

	env.builder.setError(errors.New("agent down"))
	if _, err := env.svc.CreateApp(ctx, app.Identity{DeviceID: "third-device", SessionID: "third"}, "失败", nil); toolCode(t, err) != "BUILD_START_FAILED" {
		t.Fatal("builder failure should be reported")
	}
	if len(env.registry.List()) != 3 {
		t.Fatalf("failed app was not removed: %+v", env.registry.List())
	}
}

func TestUpdateApp(t *testing.T) {
	env := newTestEnv(t)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	ctx := context.Background()

	if _, err := env.svc.UpdateApp(ctx, owner, "missing", "改主题"); toolCode(t, err) != "APP_NOT_FOUND" {
		t.Fatalf("missing app error = %v", err)
	}
	if _, err := env.svc.UpdateApp(ctx, owner, "app_1", "  "); toolCode(t, err) != "INVALID_ARGUMENT" {
		t.Fatal("empty prompt should be rejected")
	}

	env.registry.Add(app.App{
		ID: "app_idle", Owner: owner, State: app.StateIdle, SessionID: "ses_1", Prompt: "原需求",
		Artifact: app.Artifact{SHA256: "abc", Path: "artifacts/app_idle.tar.gz"},
	})
	result, err := env.svc.UpdateApp(ctx, owner, "app_idle", "题目改成动物主题")
	if err != nil || result.AppID != "app_idle" || result.State != app.StateBuilding {
		t.Fatalf("update idle = %+v, %v", result, err)
	}
	continued := env.builder.continueRequests()
	if len(continued) != 1 || continued[0].Prompt != "题目改成动物主题" || continued[0].SessionID != "ses_1" {
		t.Fatalf("continue requests = %+v", continued)
	}
	if snap := mustGet(t, env, "app_idle"); snap.State != app.StateBuilding || snap.Artifact.SHA256 != "abc" {
		t.Fatalf("idle update snapshot = %+v", snap)
	}

	env.registry.Add(app.App{ID: "app_building", Owner: owner, State: app.StateBuilding, SessionID: "ses_b"})
	if _, err := env.svc.UpdateApp(ctx, owner, "app_building", "再改"); toolCode(t, err) != "APP_BUILDING" {
		t.Fatal("building app should reject update")
	}

	env.registry.Add(app.App{ID: "app_running", Owner: owner, State: app.StateRunning, SessionID: "ses_r"})
	if _, err := env.svc.UpdateApp(ctx, owner, "app_running", "再改"); toolCode(t, err) != "APP_RUNNING" {
		t.Fatal("running app should reject update")
	}

	env.registry.Add(app.App{ID: "app_err_no_art", Owner: owner, State: app.StateError, SessionID: "ses_e",
		Error: &app.Error{Stage: app.StageBuilding, Code: "BUILD_FAILED"}})
	if _, err := env.svc.UpdateApp(ctx, owner, "app_err_no_art", "再改"); toolCode(t, err) != "APP_IN_ERROR" {
		t.Fatal("build error without artifact should reject update")
	}

	env.registry.Add(app.App{
		ID: "app_err_start", Owner: owner, State: app.StateError, SessionID: "ses_ok",
		Artifact: app.Artifact{SHA256: "def", DownloadURL: "http://example/a.tar.gz"},
		Error:    &app.Error{Stage: app.StageStarting, Code: "START_FAILED"},
	})
	if _, err := env.svc.UpdateApp(ctx, owner, "app_err_start", "修一下"); err != nil {
		t.Fatalf("start-failed app with artifact should allow update: %v", err)
	}

	env.registry.Add(app.App{ID: "app_no_session", Owner: owner, State: app.StateIdle,
		Artifact: app.Artifact{SHA256: "ghi", Path: "x.tar.gz"}})
	if _, err := env.svc.UpdateApp(ctx, owner, "app_no_session", "再改"); toolCode(t, err) != "SESSION_UNAVAILABLE" {
		t.Fatal("missing session should be unavailable")
	}

	env.builder.setContinueError(build.ErrSessionBusy)
	env.registry.Add(app.App{
		ID: "app_busy", Owner: owner, State: app.StateIdle, SessionID: "ses_busy", Prompt: "旧",
		Artifact: app.Artifact{SHA256: "zzz", Path: "z.tar.gz"},
	})
	if _, err := env.svc.UpdateApp(ctx, owner, "app_busy", "新需求"); toolCode(t, err) != "UPDATE_START_FAILED" {
		t.Fatalf("busy session error = %v", err)
	}
	if snap := mustGet(t, env, "app_busy"); snap.State != app.StateIdle || snap.Prompt != "旧" {
		t.Fatalf("failed update should roll back: %+v", snap)
	}
}

func mustGet(t *testing.T, env *testEnv, id string) app.App {
	t.Helper()
	entry, ok := env.registry.Get(id)
	if !ok {
		t.Fatalf("missing app %s", id)
	}
	return entry.Snapshot()
}

func TestGetAppStatusStates(t *testing.T) {
	env := newTestEnv(t)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	env.svc.now = func() time.Time { return now }
	ctx := context.Background()

	env.registry.Add(app.App{ID: "build", Owner: owner, State: app.StateBuilding, BuildStartedAt: now.Add(-90 * time.Second), LastAgentMessage: "正在实现页面"})
	env.registry.Add(app.App{ID: "idle", Owner: owner, State: app.StateIdle, Name: "quiz", BuildDuration: 2 * time.Minute})
	env.registry.Add(app.App{ID: "running", Owner: owner, State: app.StateRunning, Name: "quiz", StartedAt: now.Add(-30 * time.Second)})
	env.registry.Add(app.App{ID: "error", Owner: owner, State: app.StateError})

	building, err := env.svc.GetAppStatus(ctx, owner, "build")
	if err != nil {
		t.Fatal(err)
	}
	if building.State != app.StateBuilding || building.ElapsedSeconds != 90 || building.Message != "正在实现页面" {
		t.Fatalf("building = %+v", building)
	}
	idle, err := env.svc.GetAppStatus(ctx, owner, "idle")
	if err != nil {
		t.Fatal(err)
	}
	if idle.State != app.StateIdle || idle.Name != "quiz" || idle.BuildDurationSeconds != 120 {
		t.Fatalf("idle = %+v", idle)
	}
	running, err := env.svc.GetAppStatus(ctx, owner, "running")
	if err != nil {
		t.Fatal(err)
	}
	if running.State != app.StateRunning || running.UptimeSeconds != 30 || running.StartedAt == nil {
		t.Fatalf("running = %+v", running)
	}
	failed, err := env.svc.GetAppStatus(ctx, owner, "error")
	if err != nil {
		t.Fatal(err)
	}
	if failed.Error == nil || failed.Error.Code != "APP_EXITED" || failed.Error.Stage != app.StageRunning {
		t.Fatalf("error = %+v", failed)
	}
	if _, err := env.svc.GetAppStatus(ctx, app.Identity{DeviceID: "other-device", SessionID: "other"}, "idle"); toolCode(t, err) != "APP_NOT_FOUND" {
		t.Fatal("owner isolation failed")
	}
	if _, err := env.svc.GetAppStatus(ctx, owner, "missing"); toolCode(t, err) != "APP_NOT_FOUND" {
		t.Fatal("missing app should not be found")
	}
}

func TestStopApp(t *testing.T) {
	ctx := context.Background()
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	env := newTestEnv(t)
	entry := env.registry.Add(runningApp(owner))

	view, err := env.svc.StopApp(ctx, owner, "app_1")
	if err != nil {
		t.Fatalf("StopApp: %v", err)
	}
	if view.State != app.StateIdle {
		t.Fatalf("view = %+v", view)
	}
	if view.StoppedAt == nil || view.StoppedAt.IsZero() {
		t.Fatalf("stop response is missing stopped_at: %+v", view)
	}
	snapshot := entry.Snapshot()
	if snapshot.State != app.StateIdle || snapshot.InstanceID != "" || snapshot.StoppedAt.IsZero() {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if deleted := env.render.deletedIDs(); len(deleted) != 1 || deleted[0] != "inst_1" {
		t.Fatalf("deleted = %+v", deleted)
	}

	t.Run("stop failure", func(t *testing.T) {
		env := newTestEnv(t)
		entry := env.registry.Add(runningApp(owner))
		env.render.deleteErr = errors.New("render down")
		if _, err := env.svc.StopApp(ctx, owner, "app_1"); toolCode(t, err) != "STOP_FAILED" {
			t.Fatal("stop failure should be reported")
		}
		if entry.Snapshot().State != app.StateRunning {
			t.Fatal("running app changed state on stop failure")
		}
	})

	t.Run("idempotent and building", func(t *testing.T) {
		env := newTestEnv(t)
		env.registry.Add(app.App{ID: "idle", Owner: owner, State: app.StateIdle})
		env.registry.Add(app.App{ID: "building", Owner: owner, State: app.StateBuilding})
		if _, err := env.svc.StopApp(ctx, owner, "idle"); err != nil {
			t.Fatal("idle app stop should be a no-op")
		}
		if _, err := env.svc.StopApp(ctx, owner, "building"); toolCode(t, err) != "APP_BUILDING" {
			t.Fatal("building app should not stop")
		}
		if len(env.render.deletedIDs()) != 0 {
			t.Fatal("no-op stop touched runtime")
		}
	})
}

func TestSendRequest(t *testing.T) {
	type recordedRequest struct {
		method      string
		path        string
		query       string
		body        string
		contentType string
		accept      string
	}
	var (
		mu       sync.Mutex
		recorded recordedRequest
	)
	appServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		mu.Lock()
		recorded = recordedRequest{
			method:      r.Method,
			path:        r.URL.Path,
			query:       r.URL.RawQuery,
			body:        string(data),
			contentType: r.Header.Get("Content-Type"),
			accept:      r.Header.Get("Accept"),
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true}`)
	}))
	defer appServer.Close()

	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	env := newTestEnv(t)
	env.registry.Add(app.App{ID: "app_1", Owner: owner, State: app.StateRunning, AppBaseURL: appServer.URL, OpenAPI: mustSpec(t)})
	env.registry.Add(app.App{ID: "idle", Owner: owner, State: app.StateIdle, OpenAPI: mustSpec(t)})
	ctx := context.Background()

	result, err := env.svc.SendRequest(ctx, owner, "app_1", "updateItem", map[string]any{"id": "a b", "q": "x"}, map[string]any{"n": 1})
	if err != nil {
		t.Fatalf("SendRequest: %v", err)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("result = %+v", result)
	}
	body, _ := result.Body.(map[string]any)
	if body["ok"] != true {
		t.Fatalf("body = %+v", result.Body)
	}
	mu.Lock()
	got := recorded
	mu.Unlock()
	if got.method != http.MethodPost || got.path != "/api/items/a b" || got.query != "q=x" {
		t.Fatalf("request = %+v", got)
	}
	if got.body != `{"n":1}` || got.contentType != "application/json" || got.accept != "application/json" {
		t.Fatalf("request = %+v", got)
	}

	if _, err := env.svc.SendRequest(ctx, owner, "idle", "updateItem", nil, nil); toolCode(t, err) != "APP_NOT_RUNNING" {
		t.Fatal("idle app should not receive requests")
	}
	if _, err := env.svc.SendRequest(ctx, owner, "app_1", "missing", nil, nil); toolCode(t, err) != "OPERATION_NOT_FOUND" {
		t.Fatal("unknown operation should fail")
	}
	if _, err := env.svc.SendRequest(ctx, owner, "app_1", "updateItem", nil, func() {}); toolCode(t, err) != "INVALID_ARGUMENT" {
		t.Fatal("unserializable body should fail")
	}
	if _, err := env.svc.SendRequest(ctx, app.Identity{DeviceID: "other-device", SessionID: "other"}, "app_1", "updateItem", nil, nil); toolCode(t, err) != "APP_NOT_FOUND" {
		t.Fatal("owner isolation failed")
	}
}
