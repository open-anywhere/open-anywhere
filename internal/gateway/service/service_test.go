package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/openapi"
	"github.com/open-anywhere/open-anywhere/internal/gateway/render"
	"github.com/open-anywhere/open-anywhere/internal/gateway/screen"
	"github.com/open-anywhere/open-anywhere/pkg/casting"
)

const specYAML = `openapi: 3.1.0
paths:
  /api/items/{id}:
    post:
      operationId: updateItem
      responses:
        "200":
          description: ok
`

type fakeBuilder struct {
	mu      sync.Mutex
	started []app.App
	err     error
}

func (f *fakeBuilder) Start(_ context.Context, value app.App) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = append(f.started, value)
	return f.err
}

func (f *fakeBuilder) setError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeBuilder) requests() []app.App {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]app.App(nil), f.started...)
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

type fakeCasting struct {
	mu        sync.Mutex
	targets   []casting.Target
	listErr   error
	target    casting.Target
	targetErr error
	session   casting.Session
	states    []casting.SessionState
	createErr error
	getErr    error
	stopErr   error
	created   []casting.CreateSessionRequest
	stopped   []string
}

func (f *fakeCasting) ListTargets(_ context.Context) ([]casting.Target, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.targets, f.listErr
}

func (f *fakeCasting) GetTarget(_ context.Context, _ string) (casting.Target, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.target, f.targetErr
}

func (f *fakeCasting) CreateSession(_ context.Context, request casting.CreateSessionRequest) (casting.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, request)
	if f.createErr != nil {
		return casting.Session{}, f.createErr
	}
	return f.session, nil
}

func (f *fakeCasting) GetSession(_ context.Context, id string) (casting.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return casting.Session{}, f.getErr
	}
	state := f.session.State
	if len(f.states) > 0 {
		state = f.states[0]
		f.states = f.states[1:]
	}
	return casting.Session{ID: id, State: state}, nil
}

func (f *fakeCasting) StopSession(_ context.Context, id string) (casting.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = append(f.stopped, id)
	if f.stopErr != nil {
		return casting.Session{}, f.stopErr
	}
	return casting.Session{ID: id, State: casting.SessionStopped}, nil
}

func (f *fakeCasting) requests() []casting.CreateSessionRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]casting.CreateSessionRequest(nil), f.created...)
}

func (f *fakeCasting) stoppedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.stopped...)
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
	casting  *fakeCasting
	notifier *fakeNotifier
}

func newTestEnv(t *testing.T, catalog *screen.Catalog) *testEnv {
	t.Helper()
	if catalog == nil {
		var err error
		catalog, err = screen.LoadCatalog("")
		if err != nil {
			t.Fatal(err)
		}
	}
	env := &testEnv{
		registry: app.NewRegistry(),
		builder:  &fakeBuilder{},
		render: &fakeRender{
			instance: readyApp(),
		},
		casting: &fakeCasting{
			target:  testTarget(),
			session: casting.Session{ID: "ps_1", State: casting.SessionPlaying},
		},
		notifier: &fakeNotifier{},
	}
	env.svc = New(env.registry, env.builder, env.render, env.casting, catalog, env.notifier, Config{
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

func testTarget() casting.Target {
	return casting.Target{
		ID:    "tv_1",
		Name:  "客厅",
		State: casting.TargetOnline,
		Endpoints: []casting.Endpoint{{
			ID:       "ep_1",
			Protocol: "dlna",
			Capabilities: casting.Capabilities{
				Formats:     []string{"mpegts"},
				VideoCodecs: []string{"h264"},
				AudioCodecs: []string{"aac"},
				Streaming:   true,
			},
		}},
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
		ScreenID:            "tv_1",
		InstanceID:          "inst_1",
		Port:                39000,
		AppBaseURL:          "http://127.0.0.1:39000",
		TargetID:            "tv_1",
		EndpointID:          "ep_1",
		ProjectionSessionID: "ps_1",
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
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	ctx := context.Background()

	if _, err := env.svc.CreateApp(ctx, owner, "   "); toolCode(t, err) != "INVALID_ARGUMENT" {
		t.Fatal("empty prompt should be rejected")
	}
	result, err := env.svc.CreateApp(ctx, owner, "做一个小游戏")
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

	again, err := env.svc.CreateApp(ctx, owner, "另一个需求")
	if err != nil || again.AppID != result.AppID {
		t.Fatalf("idempotent create = %+v, %v", again, err)
	}
	if len(env.builder.requests()) != 1 {
		t.Fatal("duplicate build started for the same owner")
	}

	other, err := env.svc.CreateApp(ctx, app.Identity{DeviceID: "device", SessionID: "other"}, "其他")
	if err != nil || other.AppID == result.AppID {
		t.Fatalf("other owner = %+v, %v", other, err)
	}

	env.builder.setError(errors.New("agent down"))
	if _, err := env.svc.CreateApp(ctx, app.Identity{DeviceID: "device", SessionID: "third"}, "失败"); toolCode(t, err) != "BUILD_START_FAILED" {
		t.Fatal("builder failure should be reported")
	}
	if len(env.registry.List()) != 2 {
		t.Fatalf("failed app was not removed: %+v", env.registry.List())
	}
}

func TestGetAppStatusStates(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	env.svc.now = func() time.Time { return now }
	ctx := context.Background()

	env.registry.Add(app.App{ID: "build", Owner: owner, State: app.StateBuilding, BuildStartedAt: now.Add(-90 * time.Second), LastAgentMessage: "正在实现页面"})
	env.registry.Add(app.App{ID: "idle", Owner: owner, State: app.StateIdle, Name: "quiz", BuildDuration: 2 * time.Minute})
	env.registry.Add(app.App{ID: "running", Owner: owner, State: app.StateRunning, Name: "quiz", ScreenID: "tv_1", StartedAt: now.Add(-30 * time.Second)})
	env.registry.Add(app.App{ID: "error", Owner: owner, State: app.StateError, ScreenID: "tv_1"})

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
	if running.State != app.StateRunning || running.ScreenID != "tv_1" || running.UptimeSeconds != 30 || running.StartedAt == nil {
		t.Fatalf("running = %+v", running)
	}
	failed, err := env.svc.GetAppStatus(ctx, owner, "error")
	if err != nil {
		t.Fatal(err)
	}
	if failed.Error == nil || failed.Error.Code != "APP_EXITED" || failed.Error.Stage != app.StageRunning || failed.ScreenID != "tv_1" {
		t.Fatalf("error = %+v", failed)
	}
	if _, err := env.svc.GetAppStatus(ctx, app.Identity{DeviceID: "device", SessionID: "other"}, "idle"); toolCode(t, err) != "APP_NOT_FOUND" {
		t.Fatal("owner isolation failed")
	}
	if _, err := env.svc.GetAppStatus(ctx, owner, "missing"); toolCode(t, err) != "APP_NOT_FOUND" {
		t.Fatal("missing app should not be found")
	}
}

func TestStartAppSuccess(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := env.registry.Add(idleApp(t, owner))
	ctx := context.Background()

	view, err := env.svc.StartApp(ctx, owner, "app_1", "tv_1")
	if err != nil {
		t.Fatalf("StartApp: %v", err)
	}
	if view.State != app.StateRunning || view.ScreenID != "tv_1" || view.Name != "quiz" {
		t.Fatalf("view = %+v", view)
	}
	snapshot := entry.Snapshot()
	if snapshot.State != app.StateRunning || snapshot.ScreenID != "tv_1" || snapshot.InstanceID != "inst_1" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if snapshot.Port != 39000 || snapshot.AppBaseURL != "http://127.0.0.1:39000" {
		t.Fatalf("runtime = %+v", snapshot)
	}
	if snapshot.TargetID != "tv_1" || snapshot.EndpointID != "ep_1" || snapshot.ProjectionSessionID != "ps_1" {
		t.Fatalf("projection = %+v", snapshot)
	}
	if snapshot.Source.Format != "mpegts" || snapshot.StartedAt.IsZero() {
		t.Fatalf("source = %+v, started = %v", snapshot.Source, snapshot.StartedAt)
	}
	created := env.render.requests()
	if len(created) != 1 || created[0].AppID != "app_1" || created[0].Port != 39000 {
		t.Fatalf("render requests = %+v", created)
	}
	if created[0].ArtifactURL != snapshot.Artifact.DownloadURL {
		t.Fatalf("artifact url = %q", created[0].ArtifactURL)
	}
	sessions := env.casting.requests()
	if len(sessions) != 1 || sessions[0].TargetID != "tv_1" || sessions[0].EndpointID != "ep_1" || sessions[0].Source.VideoCodec != "h264" {
		t.Fatalf("casting requests = %+v", sessions)
	}

	if _, err := env.svc.StartApp(ctx, owner, "app_1", "tv_1"); err != nil {
		t.Fatalf("idempotent start: %v", err)
	}
	if len(env.render.requests()) != 1 {
		t.Fatal("idempotent start created another instance")
	}
	if _, err := env.svc.StartApp(ctx, owner, "app_1", "tv_2"); toolCode(t, err) != "APP_ALREADY_RUNNING" {
		t.Fatal("running on another screen should be rejected")
	}
}

func TestStartAppStateFailures(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	env.registry.Add(app.App{ID: "building", Owner: owner, State: app.StateBuilding})
	env.registry.Add(app.App{ID: "error", Owner: owner, State: app.StateError})
	ctx := context.Background()

	if _, err := env.svc.StartApp(ctx, owner, "missing", "tv_1"); toolCode(t, err) != "APP_NOT_FOUND" {
		t.Fatal("missing app should not start")
	}
	if _, err := env.svc.StartApp(ctx, owner, "building", "tv_1"); toolCode(t, err) != "APP_BUILDING" {
		t.Fatal("building app should not start")
	}
	if _, err := env.svc.StartApp(ctx, owner, "error", "tv_1"); toolCode(t, err) != "APP_IN_ERROR" {
		t.Fatal("error app should not start")
	}
	if _, err := env.svc.StartApp(ctx, owner, "missing", ""); toolCode(t, err) != "INVALID_ARGUMENT" {
		t.Fatal("empty screen should be rejected")
	}
}

func TestStartAppTargetFailures(t *testing.T) {
	ctx := context.Background()
	owner := app.Identity{DeviceID: "device", SessionID: "session"}

	t.Run("offline target", func(t *testing.T) {
		env := newTestEnv(t, nil)
		env.casting.target = testTarget()
		env.casting.target.State = casting.TargetOffline
		entry := env.registry.Add(idleApp(t, owner))
		if _, err := env.svc.StartApp(ctx, owner, "app_1", "tv_1"); toolCode(t, err) != "TARGET_UNREACHABLE" {
			t.Fatal("offline target should fail")
		}
		snapshot := entry.Snapshot()
		if snapshot.State != app.StateError || snapshot.Error == nil || snapshot.Error.Stage != app.StageStarting || snapshot.Error.Code != "TARGET_UNREACHABLE" {
			t.Fatalf("snapshot = %+v", snapshot)
		}
		if snapshot.ScreenID != "tv_1" {
			t.Fatalf("screen = %q", snapshot.ScreenID)
		}
	})

	t.Run("screen busy", func(t *testing.T) {
		env := newTestEnv(t, nil)
		other := runningApp(app.Identity{DeviceID: "device", SessionID: "other"})
		other.ID = "app_other"
		env.registry.Add(other)
		entry := env.registry.Add(idleApp(t, owner))
		if _, err := env.svc.StartApp(ctx, owner, "app_1", "tv_1"); toolCode(t, err) != "TARGET_BUSY" {
			t.Fatal("busy screen should fail")
		}
		if entry.Snapshot().State != app.StateIdle {
			t.Fatal("app state changed on TARGET_BUSY")
		}
	})

	t.Run("unknown configured screen", func(t *testing.T) {
		catalog := writeCatalog(t, `[{"screen_id":"living","name":"客厅","target_id":"tv_1"}]`)
		env := newTestEnv(t, catalog)
		entry := env.registry.Add(idleApp(t, owner))
		if _, err := env.svc.StartApp(ctx, owner, "app_1", "kitchen"); toolCode(t, err) != "TARGET_UNREACHABLE" {
			t.Fatal("unknown screen should fail")
		}
		if entry.Snapshot().State != app.StateIdle {
			t.Fatal("app state changed on unknown screen")
		}
	})

	t.Run("projection timeout", func(t *testing.T) {
		env := newTestEnv(t, nil)
		env.casting.session = casting.Session{ID: "ps_1", State: casting.SessionStarting}
		entry := env.registry.Add(idleApp(t, owner))
		if _, err := env.svc.StartApp(ctx, owner, "app_1", "tv_1"); toolCode(t, err) != "PROJECTION_TIMEOUT" {
			t.Fatal("projection timeout should fail")
		}
		snapshot := entry.Snapshot()
		if snapshot.State != app.StateError || snapshot.Error == nil || snapshot.Error.Stage != app.StageStarting || snapshot.Error.Code != "PROJECTION_TIMEOUT" {
			t.Fatalf("snapshot = %+v", snapshot)
		}
		deleted := env.render.deletedIDs()
		if len(deleted) != 1 || deleted[0] != "inst_1" {
			t.Fatalf("deleted = %+v", deleted)
		}
		stopped := env.casting.stoppedIDs()
		if len(stopped) != 1 || stopped[0] != "ps_1" {
			t.Fatalf("stopped = %+v", stopped)
		}
		if notes := env.notifier.notifications(); len(notes) != 1 || notes[0].State != app.StateError {
			t.Fatalf("notifications = %+v", notes)
		}
	})

	t.Run("port in use retries", func(t *testing.T) {
		env := newTestEnv(t, nil)
		env.render.ports = []render.Port{{Port: 39000, Occupied: true}}
		env.render.errs = []error{&render.HTTPError{Status: http.StatusConflict, Code: "PORT_IN_USE", Message: "busy"}}
		entry := env.registry.Add(idleApp(t, owner))
		if _, err := env.svc.StartApp(ctx, owner, "app_1", "tv_1"); err != nil {
			t.Fatalf("StartApp: %v", err)
		}
		created := env.render.requests()
		if len(created) != 2 || created[0].Port != 39001 || created[1].Port != 39002 {
			t.Fatalf("created = %+v", created)
		}
		if entry.Snapshot().Port != 39002 {
			t.Fatalf("port = %d", entry.Snapshot().Port)
		}
	})

	t.Run("render already running", func(t *testing.T) {
		env := newTestEnv(t, nil)
		env.render.errs = []error{&render.HTTPError{Status: http.StatusConflict, Code: "APP_ALREADY_RUNNING", Message: "running"}}
		entry := env.registry.Add(idleApp(t, owner))
		if _, err := env.svc.StartApp(ctx, owner, "app_1", "tv_1"); toolCode(t, err) != "APP_ALREADY_RUNNING" {
			t.Fatal("render conflict should be reported")
		}
		if entry.Snapshot().State != app.StateIdle {
			t.Fatal("app state changed on render conflict")
		}
	})
}

func TestStopApp(t *testing.T) {
	ctx := context.Background()
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	env := newTestEnv(t, nil)
	entry := env.registry.Add(runningApp(owner))

	view, err := env.svc.StopApp(ctx, owner, "app_1")
	if err != nil {
		t.Fatalf("StopApp: %v", err)
	}
	if view.State != app.StateIdle {
		t.Fatalf("view = %+v", view)
	}
	snapshot := entry.Snapshot()
	if snapshot.State != app.StateIdle || snapshot.ScreenID != "" || snapshot.InstanceID != "" || snapshot.ProjectionSessionID != "" || snapshot.StoppedAt.IsZero() {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if stopped := env.casting.stoppedIDs(); len(stopped) != 1 || stopped[0] != "ps_1" {
		t.Fatalf("stopped = %+v", stopped)
	}
	if deleted := env.render.deletedIDs(); len(deleted) != 1 || deleted[0] != "inst_1" {
		t.Fatalf("deleted = %+v", deleted)
	}

	t.Run("stop failure", func(t *testing.T) {
		env := newTestEnv(t, nil)
		entry := env.registry.Add(runningApp(owner))
		env.casting.stopErr = errors.New("casting down")
		if _, err := env.svc.StopApp(ctx, owner, "app_1"); toolCode(t, err) != "STOP_FAILED" {
			t.Fatal("stop failure should be reported")
		}
		if entry.Snapshot().State != app.StateRunning {
			t.Fatal("running app changed state on stop failure")
		}
		if len(env.render.deletedIDs()) != 0 {
			t.Fatal("render instance deleted after projection failure")
		}
	})

	t.Run("idempotent and building", func(t *testing.T) {
		env := newTestEnv(t, nil)
		env.registry.Add(app.App{ID: "idle", Owner: owner, State: app.StateIdle})
		env.registry.Add(app.App{ID: "building", Owner: owner, State: app.StateBuilding})
		if _, err := env.svc.StopApp(ctx, owner, "idle"); err != nil {
			t.Fatal("idle app stop should be a no-op")
		}
		if _, err := env.svc.StopApp(ctx, owner, "building"); toolCode(t, err) != "APP_BUILDING" {
			t.Fatal("building app should not stop")
		}
		if len(env.casting.stoppedIDs()) != 0 || len(env.render.deletedIDs()) != 0 {
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
	env := newTestEnv(t, nil)
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
	if _, err := env.svc.SendRequest(ctx, app.Identity{DeviceID: "device", SessionID: "other"}, "app_1", "updateItem", nil, nil); toolCode(t, err) != "APP_NOT_FOUND" {
		t.Fatal("owner isolation failed")
	}
}

func TestGetScreens(t *testing.T) {
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	ctx := context.Background()

	t.Run("configured catalog", func(t *testing.T) {
		catalog := writeCatalog(t, `[{"screen_id":"living","name":"客厅","target_id":"tv_1"}]`)
		env := newTestEnv(t, catalog)
		env.casting.targets = []casting.Target{testTarget()}
		env.registry.Add(app.App{ID: "app_1", Owner: owner, Name: "quiz", State: app.StateRunning, ScreenID: "living"})
		views, err := env.svc.GetScreens(ctx, owner)
		if err != nil {
			t.Fatalf("GetScreens: %v", err)
		}
		if len(views) != 1 {
			t.Fatalf("views = %+v", views)
		}
		if views[0].ScreenID != "living" || views[0].Name != "客厅" || !views[0].Available {
			t.Fatalf("view = %+v", views[0])
		}
		if views[0].CurrentApp == nil || views[0].CurrentApp.AppID != "app_1" || views[0].CurrentApp.Name != "quiz" {
			t.Fatalf("current = %+v", views[0].CurrentApp)
		}
	})

	t.Run("casting discovery fallback", func(t *testing.T) {
		env := newTestEnv(t, nil)
		offline := testTarget()
		offline.ID = "tv_2"
		offline.State = casting.TargetOffline
		incompatible := testTarget()
		incompatible.ID = "tv_3"
		incompatible.Endpoints[0].Capabilities.Formats = []string{"mp4"}
		env.casting.targets = []casting.Target{testTarget(), offline, incompatible}
		views, err := env.svc.GetScreens(ctx, owner)
		if err != nil {
			t.Fatalf("GetScreens: %v", err)
		}
		if len(views) != 3 {
			t.Fatalf("views = %+v", views)
		}
		if !views[0].Available || views[1].Available || views[2].Available {
			t.Fatalf("views = %+v", views)
		}
	})

	t.Run("discovery failure", func(t *testing.T) {
		env := newTestEnv(t, nil)
		env.casting.listErr = errors.New("casting down")
		if _, err := env.svc.GetScreens(ctx, owner); toolCode(t, err) != "SCREEN_DISCOVERY_FAILED" {
			t.Fatal("discovery failure should be reported")
		}
	})
}

func TestCheckRunning(t *testing.T) {
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	ctx := context.Background()

	t.Run("healthy", func(t *testing.T) {
		env := newTestEnv(t, nil)
		entry := env.registry.Add(runningApp(owner))
		env.render.getInstance = render.App{InstanceID: "inst_1", State: render.StateReady}
		env.casting.states = []casting.SessionState{casting.SessionPlaying}
		env.svc.checkRunning(ctx)
		if entry.Snapshot().State != app.StateRunning {
			t.Fatal("healthy app changed state")
		}
		if len(env.render.deletedIDs()) != 0 || len(env.casting.stoppedIDs()) != 0 || len(env.notifier.notifications()) != 0 {
			t.Fatal("healthy app triggered cleanup")
		}
	})

	t.Run("render error", func(t *testing.T) {
		env := newTestEnv(t, nil)
		entry := env.registry.Add(runningApp(owner))
		env.render.getInstance = render.App{InstanceID: "inst_1", State: render.StateError, Error: &render.Error{Code: "APP_CRASHED", Message: "boom"}}
		env.svc.checkRunning(ctx)
		snapshot := entry.Snapshot()
		if snapshot.State != app.StateError || snapshot.Error == nil || snapshot.Error.Stage != app.StageRunning || snapshot.Error.Code != "APP_CRASHED" || snapshot.Error.Message != "boom" {
			t.Fatalf("snapshot = %+v", snapshot)
		}
		if deleted := env.render.deletedIDs(); len(deleted) != 1 || deleted[0] != "inst_1" {
			t.Fatalf("deleted = %+v", deleted)
		}
		if stopped := env.casting.stoppedIDs(); len(stopped) != 1 || stopped[0] != "ps_1" {
			t.Fatalf("stopped = %+v", stopped)
		}
		notes := env.notifier.notifications()
		if len(notes) != 1 || notes[0].AppID != "app_1" || notes[0].State != app.StateError || notes[0].Error == nil {
			t.Fatalf("notes = %+v", notes)
		}
	})

	t.Run("render missing", func(t *testing.T) {
		env := newTestEnv(t, nil)
		entry := env.registry.Add(runningApp(owner))
		env.render.getErr = &render.HTTPError{Status: http.StatusNotFound, Code: "NOT_FOUND"}
		env.svc.checkRunning(ctx)
		snapshot := entry.Snapshot()
		if snapshot.State != app.StateError || snapshot.Error == nil || snapshot.Error.Code != "APP_EXITED" {
			t.Fatalf("snapshot = %+v", snapshot)
		}
	})

	t.Run("projection failed", func(t *testing.T) {
		env := newTestEnv(t, nil)
		entry := env.registry.Add(runningApp(owner))
		env.render.getInstance = render.App{InstanceID: "inst_1", State: render.StateReady}
		env.casting.states = []casting.SessionState{casting.SessionFailed}
		env.svc.checkRunning(ctx)
		snapshot := entry.Snapshot()
		if snapshot.State != app.StateError || snapshot.Error == nil || snapshot.Error.Code != "PROJECTION_DISCONNECTED" {
			t.Fatalf("snapshot = %+v", snapshot)
		}
	})
}

func TestStartAppMapsConfiguredScreen(t *testing.T) {
	catalog := writeCatalog(t, `[{"screen_id":"living","name":"客厅","target_id":"tv_1"}]`)
	env := newTestEnv(t, catalog)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := env.registry.Add(idleApp(t, owner))
	if _, err := env.svc.StartApp(context.Background(), owner, "app_1", "living"); err != nil {
		t.Fatalf("StartApp: %v", err)
	}
	snapshot := entry.Snapshot()
	if snapshot.ScreenID != "living" || snapshot.TargetID != "tv_1" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if sessions := env.casting.requests(); len(sessions) != 1 || sessions[0].TargetID != "tv_1" {
		t.Fatalf("sessions = %+v", sessions)
	}
}

func writeCatalog(t *testing.T, data string) *screen.Catalog {
	t.Helper()
	path := filepath.Join(t.TempDir(), "screens.json")
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog, err := screen.LoadCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}
