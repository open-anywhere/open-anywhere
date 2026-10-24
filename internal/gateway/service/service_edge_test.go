package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/openapi"
	"github.com/open-anywhere/open-anywhere/internal/gateway/render"
	"github.com/open-anywhere/open-anywhere/pkg/casting"
)

// hookCasting wraps the package fake to run a callback at a precise point of a
// lifecycle call, so a concurrent state change can be injected deterministically.
type hookCasting struct {
	*fakeCasting
	onCreate  func()
	onGet     func()
	onStop    func()
	stopState casting.SessionState
}

func (h *hookCasting) CreateSession(ctx context.Context, request casting.CreateSessionRequest) (casting.Session, error) {
	if h.onCreate != nil {
		h.onCreate()
	}
	return h.fakeCasting.CreateSession(ctx, request)
}

func (h *hookCasting) GetSession(ctx context.Context, id string) (casting.Session, error) {
	if h.onGet != nil {
		h.onGet()
	}
	return h.fakeCasting.GetSession(ctx, id)
}

func (h *hookCasting) StopSession(ctx context.Context, id string) (casting.Session, error) {
	if h.onStop != nil {
		h.onStop()
	}
	if h.stopState != "" {
		if h.fakeCasting.stopErr != nil {
			return casting.Session{}, h.fakeCasting.stopErr
		}
		return casting.Session{ID: id, State: h.stopState}, nil
	}
	return h.fakeCasting.StopSession(ctx, id)
}

// hookRender wraps the package fake to run a callback while the monitor probes
// the instance, and to return a caller-chosen state from Delete.
type hookRender struct {
	*fakeRender
	onGet       func()
	deleteState render.State
}

func (h *hookRender) Get(ctx context.Context, id string) (render.App, error) {
	if h.onGet != nil {
		h.onGet()
	}
	return h.fakeRender.Get(ctx, id)
}

func (h *hookRender) Delete(ctx context.Context, id string) (render.App, error) {
	if h.deleteState != "" {
		return render.App{InstanceID: id, State: h.deleteState}, nil
	}
	return h.fakeRender.Delete(ctx, id)
}

func TestToolErrorString(t *testing.T) {
	err := &ToolError{Code: "TARGET_BUSY", Message: "屏幕被占用"}
	if got := err.Error(); got != "TARGET_BUSY: 屏幕被占用" {
		t.Fatalf("Error() = %q", got)
	}
}

func TestStartAppRefusesUnknownState(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	env.registry.Add(app.App{ID: "app_1", Owner: owner, State: app.State("paused")})
	if _, err := env.svc.StartApp(context.Background(), owner, "app_1", "tv_1", false); toolCode(t, err) != "INVALID_STATE" {
		t.Fatalf("err = %v", err)
	}
}

func TestStartAppDetectsARunningRuntimeThatIsGone(t *testing.T) {
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	for name, tc := range map[string]struct {
		setup func(*testEnv)
		code  string
	}{
		"render probe error": {
			setup: func(env *testEnv) { env.render.getErr = errors.New("render down") },
			code:  "RUNTIME_UNAVAILABLE",
		},
		"render not ready": {
			setup: func(env *testEnv) {
				env.render.getInstance = render.App{InstanceID: "inst_1", State: render.StateError}
			},
			code: "APP_NOT_RUNNING",
		},
		"projection probe error": {
			setup: func(env *testEnv) { env.casting.getErr = errors.New("casting down") },
			code:  "PROJECTION_UNAVAILABLE",
		},
		"projection stopped": {
			setup: func(env *testEnv) {
				env.casting.session = casting.Session{ID: "ps_1", State: casting.SessionStopped}
			},
			code: "APP_NOT_RUNNING",
		},
	} {
		t.Run(name, func(t *testing.T) {
			env := newTestEnv(t, nil)
			env.registry.Add(runningApp(owner))
			tc.setup(env)
			if _, err := env.svc.StartApp(context.Background(), owner, "app_1", "tv_1", false); toolCode(t, err) != tc.code {
				t.Fatalf("err = %v, want %s", err, tc.code)
			}
		})
	}
}

func TestStartAppRefusesItsOwnStaleProjectionReservation(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	stale := idleApp(t, owner)
	stale.ScreenID = "tv_1"
	stale.TargetID = "tv_1"
	stale.EndpointID = "ep_1"
	stale.ProjectionSessionID = "ps_stale"
	env.registry.Add(stale)
	env.casting.target.ActiveSessionID = "ps_stale"

	if _, err := env.svc.StartApp(context.Background(), owner, "app_1", "tv_1", false); toolCode(t, err) != "CLEANUP_PENDING" {
		t.Fatalf("err = %v", err)
	}
	if len(env.render.requests()) != 0 {
		t.Fatal("a start was attempted while the stale reservation was still pending")
	}
}

func TestClaimScreenGivesUpAfterFourReservations(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	for i := 0; i < 5; i++ {
		other := runningApp(owner)
		other.ID = fmt.Sprintf("app_other_%d", i)
		other.InstanceID = fmt.Sprintf("inst_old_%d", i)
		other.ProjectionSessionID = fmt.Sprintf("ps_old_%d", i)
		env.registry.Add(other)
	}
	env.registry.Add(idleApp(t, owner))

	if _, err := env.svc.StartApp(context.Background(), owner, "app_1", "tv_1", true); toolCode(t, err) != "TARGET_BUSY" {
		t.Fatalf("err = %v", err)
	}
	if stopped := env.casting.stoppedIDs(); len(stopped) != 4 {
		t.Fatalf("stopped %d reservations, want exactly the four attempts: %v", len(stopped), stopped)
	}
	if _, busy := env.registry.RunningOnScreen("tv_1"); !busy {
		t.Fatal("the last reservation disappeared without being stopped")
	}
}

func TestStartAppResetAfterAlreadyRunningIsNotDurable(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	env.registry.Add(idleApp(t, owner))
	env.render.errs = []error{&render.HTTPError{Status: 409, Code: "APP_ALREADY_RUNNING", Message: "running"}}
	env.registry.SetPersister(failingRuntimeStore{func(a app.App) bool {
		return a.State == app.StateIdle && a.ScreenID == "" && a.TargetID == "" && !a.PendingRuntime && !a.CleanupPending
	}})
	_, err := env.svc.StartApp(context.Background(), owner, "app_1", "tv_1", false)
	if toolCode(t, err) != "PERSISTENCE_FAILED" {
		t.Fatalf("err = %v", err)
	}
}

func TestStartAppPersistsTheSessionFromACreationError(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := env.registry.Add(idleApp(t, owner))
	env.casting.createErr = &casting.ClientError{Status: 502, Code: "device_control_failed", Message: "start failed", SessionID: "ps_leaked"}
	// Cleanup also fails, so the session ID has to survive in the record.
	env.casting.stopErr = errors.New("device unreachable")

	if _, err := env.svc.StartApp(context.Background(), owner, "app_1", "tv_1", false); toolCode(t, err) != "PROJECTION_TIMEOUT" {
		t.Fatalf("err = %v", err)
	}
	if got := entry.Snapshot().ProjectionSessionID; got != "ps_leaked" {
		t.Fatalf("session from the creation error was lost: %q", got)
	}
	if stopped := env.casting.stoppedIDs(); len(stopped) != 1 || stopped[0] != "ps_leaked" {
		t.Fatalf("cleanup did not target the leaked session: %v", stopped)
	}
}

func TestStartAppKeepsAConcurrentRunningState(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := env.registry.Add(idleApp(t, owner))
	env.casting.createErr = errors.New("casting rejected the session")
	env.svc.casting = &hookCasting{fakeCasting: env.casting, onCreate: func() {
		entry.Update(func(a *app.App) { a.State = app.StateRunning })
	}}

	_, err := env.svc.StartApp(context.Background(), owner, "app_1", "tv_1", false)
	if toolCode(t, err) != "PROJECTION_TIMEOUT" {
		t.Fatalf("err = %v", err)
	}
	if got := entry.Snapshot(); got.State != app.StateRunning || got.Error != nil {
		t.Fatalf("the concurrent running state was overwritten: %+v", got)
	}
}

func TestStartAppDoesNotOverwriteAConcurrentRunningTransition(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := env.registry.Add(idleApp(t, owner))
	env.svc.casting = &hookCasting{fakeCasting: env.casting, onGet: func() {
		entry.Update(func(a *app.App) { a.State = app.StateRunning })
	}}

	view, err := env.svc.StartApp(context.Background(), owner, "app_1", "tv_1", false)
	if err != nil {
		t.Fatalf("StartApp: %v", err)
	}
	if view.State != app.StateRunning || entry.Snapshot().State != app.StateRunning {
		t.Fatalf("view=%+v snapshot=%+v", view, entry.Snapshot())
	}
}

func TestStartAppReportsAFailedFinalPersistence(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := env.registry.Add(idleApp(t, owner))
	env.registry.SetPersister(failingRuntimeStore{func(a app.App) bool {
		return a.State == app.StateRunning
	}})
	if _, err := env.svc.StartApp(context.Background(), owner, "app_1", "tv_1", false); toolCode(t, err) != "PERSISTENCE_FAILED" {
		t.Fatalf("err = %v", err)
	}
	if got := entry.Snapshot(); got.State != app.StateError || !got.CleanupPending {
		t.Fatalf("after an undurable ready transition the app must wait for cleanup: %+v", got)
	}
}

func TestStartAppRejectsAForeignRenderInstance(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	env.registry.Add(idleApp(t, owner))
	instance := readyApp()
	instance.AppID = "another_app"
	env.render.instance = instance
	if _, err := env.svc.StartApp(context.Background(), owner, "app_1", "tv_1", false); toolCode(t, err) != "START_FAILED" {
		t.Fatalf("err = %v", err)
	}
	if len(env.casting.requests()) != 0 {
		t.Fatal("a projection was created for a foreign Render instance")
	}
}

func TestStartAppRejectsARenderInstanceWithoutID(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	env.registry.Add(idleApp(t, owner))
	instance := readyApp()
	instance.InstanceID = ""
	env.render.instance = instance
	if _, err := env.svc.StartApp(context.Background(), owner, "app_1", "tv_1", false); toolCode(t, err) != "START_FAILED" {
		t.Fatalf("err = %v", err)
	}
}

func TestStartAppClearingAPortReservationIsDurable(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	env.registry.Add(idleApp(t, owner))
	env.render.errs = []error{&render.HTTPError{Status: 409, Code: "PORT_IN_USE", Message: "busy"}}
	env.registry.SetPersister(failingRuntimeStore{func(a app.App) bool {
		return a.Port != 0 && !a.PendingRuntime && a.InstanceID == "" && a.State == app.StateIdle
	}})
	if _, err := env.svc.StartApp(context.Background(), owner, "app_1", "tv_1", false); toolCode(t, err) != "PERSISTENCE_FAILED" {
		t.Fatalf("err = %v", err)
	}
}

func TestStartAppRejectsAProjectionWithoutSessionID(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := env.registry.Add(idleApp(t, owner))
	env.casting.session = casting.Session{State: casting.SessionPlaying}
	if _, err := env.svc.StartApp(context.Background(), owner, "app_1", "tv_1", false); toolCode(t, err) != "PROJECTION_TIMEOUT" {
		t.Fatalf("err = %v", err)
	}
	if got := entry.Snapshot(); got.State != app.StateError {
		t.Fatalf("snapshot = %+v", got)
	}
}

func TestStartAppProjectionBindingIsDurable(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	env.registry.Add(idleApp(t, owner))
	env.registry.SetPersister(failingRuntimeStore{func(a app.App) bool {
		return a.ProjectionSessionID != "" && !a.PendingProjection && a.State == app.StateIdle
	}})
	if _, err := env.svc.StartApp(context.Background(), owner, "app_1", "tv_1", false); toolCode(t, err) != "PERSISTENCE_FAILED" {
		t.Fatalf("err = %v", err)
	}
}

func TestChoosePortFailures(t *testing.T) {
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	t.Run("probe error", func(t *testing.T) {
		env := newTestEnv(t, nil)
		env.registry.Add(idleApp(t, owner))
		env.svc.render = &portFailRender{fakeRender: env.render}
		if _, err := env.svc.StartApp(context.Background(), owner, "app_1", "tv_1", false); toolCode(t, err) != "START_FAILED" {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("all ports occupied", func(t *testing.T) {
		env := newTestEnv(t, nil)
		env.registry.Add(idleApp(t, owner))
		var ports []render.Port
		for port := 39000; port <= 39002; port++ {
			ports = append(ports, render.Port{Port: port, Occupied: true})
		}
		env.render.ports = ports
		if _, err := env.svc.StartApp(context.Background(), owner, "app_1", "tv_1", false); toolCode(t, err) != "START_FAILED" {
			t.Fatalf("err = %v", err)
		}
		if len(env.render.requests()) != 0 {
			t.Fatal("Render was asked to create an instance without a free port")
		}
	})
}

// portFailRender fails the per-port probe while listing succeeds.
type portFailRender struct{ *fakeRender }

func (p *portFailRender) GetPort(context.Context, int) (render.Port, error) {
	return render.Port{}, errors.New("port probe failed")
}

func TestStopAppMissingAndTransferredOwnership(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	ctx := context.Background()
	if _, err := env.svc.StopApp(ctx, owner, "missing"); toolCode(t, err) != "APP_NOT_FOUND" {
		t.Fatalf("err = %v", err)
	}

	entry := env.registry.Add(runningApp(owner))
	entry.Begin()
	done := make(chan error, 1)
	go func() {
		_, err := env.svc.StopApp(ctx, owner, "app_1")
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)
	entry.Update(func(a *app.App) {
		a.Owner = app.Identity{DeviceID: "device", AgentID: "other", SessionID: "x"}
	})
	entry.End()
	if err := <-done; toolCode(t, err) != "APP_NOT_FOUND" {
		t.Fatalf("err = %v", err)
	}
	if entry.Snapshot().State != app.StateRunning {
		t.Fatal("a transferred app was stopped")
	}
}

func TestStopAppErrorStates(t *testing.T) {
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	t.Run("retryable cleanup persistence failure", func(t *testing.T) {
		env := newTestEnv(t, nil)
		value := builtApp(t, "app_1", 0, app.StateError)
		value.Owner = owner
		value.Error = &app.Error{Stage: app.StageStarting, Code: "PROJECTION_TIMEOUT"}
		env.registry.Add(value)
		env.registry.SetPersister(failingRuntimeStore{func(app.App) bool { return true }})
		if _, err := env.svc.StopApp(context.Background(), owner, "app_1"); toolCode(t, err) != "PERSISTENCE_FAILED" {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("non retryable cleanup failure", func(t *testing.T) {
		env := newTestEnv(t, nil)
		env.registry.Add(app.App{ID: "app_1", Owner: owner, State: app.StateError})
		env.registry.SetPersister(failingRuntimeStore{func(app.App) bool { return true }})
		if _, err := env.svc.StopApp(context.Background(), owner, "app_1"); toolCode(t, err) != "PERSISTENCE_FAILED" {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("non retryable cleanup succeeds", func(t *testing.T) {
		env := newTestEnv(t, nil)
		entry := env.registry.Add(app.App{ID: "app_1", Owner: owner, State: app.StateError})
		view, err := env.svc.StopApp(context.Background(), owner, "app_1")
		if err != nil {
			t.Fatalf("StopApp: %v", err)
		}
		if view.State != app.StateError || entry.Snapshot().CleanupPending {
			t.Fatalf("view=%+v snapshot=%+v", view, entry.Snapshot())
		}
	})
}

func TestStopAppRuntimeDeleteFailure(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := env.registry.Add(runningApp(owner))
	env.render.deleteErr = errors.New("render down")
	if _, err := env.svc.StopApp(context.Background(), owner, "app_1"); toolCode(t, err) != "STOP_FAILED" {
		t.Fatalf("err = %v", err)
	}
	if entry.Snapshot().State != app.StateRunning || len(env.render.deletedIDs()) != 1 {
		t.Fatalf("snapshot=%+v deleted=%v", entry.Snapshot(), env.render.deletedIDs())
	}
}

func TestStopAppFinalResetIsDurable(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := env.registry.Add(runningApp(owner))
	env.registry.SetPersister(failingRuntimeStore{func(a app.App) bool {
		return a.State == app.StateIdle && a.ScreenID == ""
	}})
	if _, err := env.svc.StopApp(context.Background(), owner, "app_1"); toolCode(t, err) != "PERSISTENCE_FAILED" {
		t.Fatalf("err = %v", err)
	}
	if entry.Snapshot().State != app.StateRunning {
		t.Fatal("an undurable stop was published as idle")
	}
}

func TestSendRequestTransferredOwnership(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := env.registry.Add(app.App{ID: "app_1", Owner: owner, State: app.StateIdle})
	entry.Begin()
	done := make(chan error, 1)
	go func() {
		_, err := env.svc.SendRequest(context.Background(), owner, "app_1", "updateItem", nil, nil)
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)
	entry.Update(func(a *app.App) {
		a.Owner = app.Identity{DeviceID: "device", AgentID: "other", SessionID: "x"}
	})
	entry.End()
	if err := <-done; toolCode(t, err) != "APP_NOT_FOUND" {
		t.Fatalf("err = %v", err)
	}
}

func TestSendRequestProbeFailures(t *testing.T) {
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	for name, tc := range map[string]struct {
		setup func(*testEnv)
		code  string
	}{
		"render probe":     {setup: func(env *testEnv) { env.render.getErr = errors.New("down") }, code: "RUNTIME_UNAVAILABLE"},
		"render not ready": {setup: func(env *testEnv) { env.render.getInstance = render.App{State: render.StateStarting} }, code: "APP_NOT_RUNNING"},
		"projection probe": {setup: func(env *testEnv) { env.casting.getErr = errors.New("down") }, code: "PROJECTION_UNAVAILABLE"},
		"projection idle":  {setup: func(env *testEnv) { env.casting.session = casting.Session{State: casting.SessionStarting} }, code: "APP_NOT_RUNNING"},
	} {
		t.Run(name, func(t *testing.T) {
			env := newTestEnv(t, nil)
			value := runningApp(owner)
			value.OpenAPI = mustSpec(t)
			env.registry.Add(value)
			tc.setup(env)
			if _, err := env.svc.SendRequest(context.Background(), owner, "app_1", "updateItem", nil, nil); toolCode(t, err) != tc.code {
				t.Fatalf("err = %v, want %s", err, tc.code)
			}
		})
	}
}

func TestCheckRunningProjectionMissing(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := env.registry.Add(runningApp(owner))
	env.render.getInstance = render.App{InstanceID: "inst_1", State: render.StateReady}
	env.casting.getErr = &casting.ClientError{Status: 404, Code: "NOT_FOUND"}
	env.svc.checkRunning(context.Background())
	snapshot := entry.Snapshot()
	if snapshot.State != app.StateError || snapshot.Error == nil || snapshot.Error.Code != "PROJECTION_DISCONNECTED" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestRuntimeFailureIgnoresAVanishedRecord(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	env.registry.Add(runningApp(owner))
	env.svc.render = &hookRender{fakeRender: env.render, onGet: func() {
		env.registry.Remove("app_1")
	}}
	env.render.getInstance = render.App{InstanceID: "inst_1", State: render.StateError}
	env.svc.checkRunning(context.Background())
	if notes := env.notifier.notifications(); len(notes) != 0 {
		t.Fatalf("notifications for a vanished app: %+v", notes)
	}
}

func TestRuntimeFailureIgnoresAStaleProbe(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := env.registry.Add(runningApp(owner))
	env.svc.render = &hookRender{fakeRender: env.render, onGet: func() {
		entry.Update(func(a *app.App) { a.InstanceID = "a_new_instance" })
	}}
	env.render.getInstance = render.App{InstanceID: "inst_1", State: render.StateError}
	env.svc.checkRunning(context.Background())
	if entry.Snapshot().State != app.StateRunning {
		t.Fatalf("a stale probe changed the app: %+v", entry.Snapshot())
	}
	if notes := env.notifier.notifications(); len(notes) != 0 {
		t.Fatalf("notifications for a stale probe: %+v", notes)
	}
}

func TestCleanupRuntime(t *testing.T) {
	t.Run("both", func(t *testing.T) {
		env := newTestEnv(t, nil)
		env.svc.cleanupRuntime("inst_1", "ps_1")
		if got := env.casting.stoppedIDs(); len(got) != 1 || got[0] != "ps_1" {
			t.Fatalf("stopped = %v", got)
		}
		if got := env.render.deletedIDs(); len(got) != 1 || got[0] != "inst_1" {
			t.Fatalf("deleted = %v", got)
		}
	})
	t.Run("neither", func(t *testing.T) {
		env := newTestEnv(t, nil)
		env.svc.cleanupRuntime("", "")
		if len(env.casting.stoppedIDs()) != 0 || len(env.render.deletedIDs()) != 0 {
			t.Fatal("cleanup touched downstream resources without ids")
		}
	})
	t.Run("only instance", func(t *testing.T) {
		env := newTestEnv(t, nil)
		env.svc.cleanupRuntime("inst_1", "")
		if len(env.casting.stoppedIDs()) != 0 || len(env.render.deletedIDs()) != 1 {
			t.Fatalf("stopped=%v deleted=%v", env.casting.stoppedIDs(), env.render.deletedIDs())
		}
	})
}

func TestTargetErrors(t *testing.T) {
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	for name, tc := range map[string]struct {
		err     error
		message string
	}{
		"not found":   {err: &casting.ClientError{Status: 404, Code: "NOT_FOUND"}, message: "目标屏幕不可达"},
		"unreachable": {err: errors.New("network down"), message: "无法访问投屏服务"},
	} {
		t.Run(name, func(t *testing.T) {
			env := newTestEnv(t, nil)
			env.registry.Add(idleApp(t, owner))
			env.casting.targetErr = tc.err
			_, err := env.svc.StartApp(context.Background(), owner, "app_1", "tv_1", false)
			var tool *ToolError
			if !errors.As(err, &tool) || tool.Code != "TARGET_UNREACHABLE" || tool.Message != tc.message {
				t.Fatalf("err = %#v", err)
			}
		})
	}
}

func TestStatusViewClampsFutureTimestamps(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	env.svc.now = func() time.Time { return now }
	env.registry.Add(app.App{ID: "build", Owner: owner, State: app.StateBuilding, BuildStartedAt: now.Add(time.Hour)})
	env.registry.Add(app.App{ID: "run", Owner: owner, State: app.StateRunning, StartedAt: now.Add(time.Hour)})

	building, err := env.svc.GetAppStatus(context.Background(), owner, "build")
	if err != nil || building.ElapsedSeconds != 0 {
		t.Fatalf("building = %+v err=%v", building, err)
	}
	running, err := env.svc.GetAppStatus(context.Background(), owner, "run")
	if err != nil || running.UptimeSeconds != 0 {
		t.Fatalf("running = %+v err=%v", running, err)
	}
}

func TestGetOpenAPIFailures(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	env.registry.Add(app.App{ID: "building", Owner: owner, State: app.StateBuilding})
	env.registry.Add(app.App{ID: "empty", Owner: owner, State: app.StateIdle})
	ctx := context.Background()
	if _, err := env.svc.GetOpenAPI(ctx, owner, "missing"); toolCode(t, err) != "APP_NOT_FOUND" {
		t.Fatalf("err = %v", err)
	}
	if _, err := env.svc.GetOpenAPI(ctx, owner, "building"); toolCode(t, err) != "APP_BUILDING" {
		t.Fatalf("err = %v", err)
	}
	if _, err := env.svc.GetOpenAPI(ctx, owner, "empty"); toolCode(t, err) != "APP_IN_ERROR" {
		t.Fatalf("err = %v", err)
	}
}

func TestStartAppVerifyRunningBeforeIdempotentStart(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	value := runningApp(owner)
	value.OpenAPI = mustSpec(t)
	env.registry.Add(value)
	env.render.getInstance = render.App{InstanceID: "inst_1", State: render.StateReady}
	env.casting.session = casting.Session{ID: "ps_1", State: casting.SessionPlaying}

	view, err := env.svc.StartApp(context.Background(), owner, "app_1", "tv_1", false)
	if err != nil || view.State != app.StateRunning {
		t.Fatalf("view=%+v err=%v", view, err)
	}
	if len(env.render.requests()) != 0 {
		t.Fatal("an idempotent start created a Render instance")
	}
}

// TestStartAppUnknownConfiguredScreen exercises the empty-catalog branch: when
// no catalog is configured, the screen id is used as the target id directly.
func TestTargetUsesScreenIDWhenCatalogIsEmpty(t *testing.T) {
	env := newTestEnv(t, nil)
	env.casting.target = casting.Target{ID: "raw_screen", State: casting.TargetOnline, Endpoints: testTarget().Endpoints}
	if _, err := env.svc.target(context.Background(), "raw_screen"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.target(context.Background(), "unknown"); err != nil {
		t.Fatalf("an empty catalog must fall back to the discovered target: %v", err)
	}
}

func TestPruneHistoryKeepsTheNewestOnly(t *testing.T) {
	env := newTestEnv(t, nil)
	for i := 0; i < HistoryLimit; i++ {
		env.registry.Add(builtApp(t, fmt.Sprintf("keep_%02d", i), i, app.StateIdle))
	}
	env.svc.PruneHistory()
	if got := len(env.registry.List()); got != HistoryLimit {
		t.Fatalf("%d apps left", got)
	}
}

func TestHistorySortTieBreaksByID(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	same := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, id := range []string{"app_b", "app_a"} {
		env.registry.Add(app.App{ID: id, Owner: owner, State: app.StateIdle, BuildStartedAt: same})
	}
	list, err := env.svc.ListApps(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].AppID != "app_a" || list[1].AppID != "app_b" {
		t.Fatalf("order = %+v", list)
	}
}

func TestSummarizeTitleFromOpenAPIInfo(t *testing.T) {
	const titled = `openapi: 3.1.0
info:
  title: 猜拳游戏
paths:
  /api/roll:
    post:
      operationId: rollDice
      responses:
        "200":
          description: ok
`
	spec, err := openapi.Parse([]byte(titled))
	if err != nil {
		t.Fatal(err)
	}
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	env.registry.Add(app.App{ID: "app_1", Owner: owner, State: app.StateIdle, OpenAPI: spec})
	list, err := env.svc.ListApps(context.Background(), owner)
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	if list[0].Title != "猜拳游戏" {
		t.Fatalf("title = %q", list[0].Title)
	}
}

func TestStartableRejectsAnUnknownState(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	env.registry.Add(app.App{ID: "app_1", Owner: owner, State: app.State("paused")})
	list, err := env.svc.ListApps(context.Background(), owner)
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	if list[0].Startable || !strings.Contains(list[0].NotStartableReason, "状态不允许启动") {
		t.Fatalf("summary = %+v", list[0])
	}
}

type failingArtifactStore struct{ err error }

func (f failingArtifactStore) Has(string) bool { return true }
func (f failingArtifactStore) Remove(string) error {
	return f.err
}

func TestPruneHistoryLogsPackageRemovalFailure(t *testing.T) {
	env := newTestEnv(t, nil)
	env.svc.SetArtifactStore(failingArtifactStore{err: errors.New("disk unavailable")})
	var logged []string
	env.svc.logf = func(format string, args ...any) {
		logged = append(logged, fmt.Sprintf(format, args...))
	}
	for i := 0; i < HistoryLimit+2; i++ {
		env.registry.Add(builtApp(t, fmt.Sprintf("app_%02d", i), i, app.StateIdle))
	}
	env.svc.PruneHistory()
	if got := len(env.registry.List()); got != HistoryLimit {
		t.Fatalf("%d apps left", got)
	}
	if len(logged) != 4 {
		t.Fatalf("logs = %v", logged)
	}
}

func TestReleaseFailedStartKeepsTheUnknownOutcomeWhenItCannotPersist(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	value := builtApp(t, "app_1", 0, app.StateError)
	value.Owner = owner
	value.Error = &app.Error{Stage: app.StageStarting, Code: "PROJECTION_TIMEOUT"}
	value.TargetID, value.PendingProjection, value.ScreenID = "tv_1", true, "tv_1"
	entry := env.registry.Add(value)
	env.registry.SetPersister(failingRuntimeStore{func(a app.App) bool {
		return !a.PendingProjection && a.TargetID != ""
	}})
	if _, err := env.svc.StartApp(context.Background(), owner, "app_1", "tv_1", false); toolCode(t, err) != "PERSISTENCE_FAILED" {
		t.Fatalf("err = %v", err)
	}
	if !entry.Snapshot().PendingProjection {
		t.Fatal("the unknown projection outcome was dropped without a durable record")
	}
}

func TestReleaseFailedStartBlocksRestartWhenRenderIsUnreachable(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	value := builtApp(t, "app_1", 0, app.StateError)
	value.Owner = owner
	value.Error = &app.Error{Stage: app.StageStarting, Code: "PROJECTION_TIMEOUT"}
	value.PendingRuntime = true
	value.CleanupPending = true
	entry := env.registry.Add(value)
	// Render cannot be queried, so the previous start stays unresolved and the
	// retry is refused as a tool error rather than leaking the reservation.
	env.render.listErr = errors.New("render unreachable")
	if _, err := env.svc.StartApp(context.Background(), owner, "app_1", "tv_1", false); toolCode(t, err) != "CLEANUP_PENDING" {
		t.Fatalf("err = %v", err)
	}
	if !entry.Snapshot().PendingRuntime {
		t.Fatal("the unresolved reservation was dropped without checking Render")
	}
}

func TestReleaseFailedStartDropsReservationWhenRenderHasNoInstance(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	value := builtApp(t, "app_1", 0, app.StateError)
	value.Owner = owner
	value.Error = &app.Error{Stage: app.StageStarting, Code: "PROJECTION_TIMEOUT"}
	value.PendingRuntime = true
	value.CleanupPending = true
	value.ScreenID = "tv_1"
	entry := env.registry.Add(value)
	// Render answers the port query and reports no instance for app_1: the
	// unknown start outcome is resolved and the retry must proceed.
	view, err := env.svc.StartApp(context.Background(), owner, "app_1", "tv_1", false)
	if err != nil {
		t.Fatalf("StartApp: %v", err)
	}
	if view.State != app.StateRunning || entry.Snapshot().PendingRuntime {
		t.Fatalf("view = %+v pending=%v", view, entry.Snapshot().PendingRuntime)
	}
}

func TestStartAppOwnershipChangedWhileWaitingForTheLock(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := env.registry.Add(idleApp(t, owner))
	entry.Begin()
	done := make(chan error, 1)
	go func() {
		_, err := env.svc.StartApp(context.Background(), owner, "app_1", "tv_1", false)
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)
	entry.Update(func(a *app.App) {
		a.Owner = app.Identity{DeviceID: "device", AgentID: "other", SessionID: "x"}
	})
	entry.End()
	if err := <-done; toolCode(t, err) != "APP_NOT_FOUND" {
		t.Fatalf("err = %v", err)
	}
}

func TestStopAppStateChangedDuringCleanup(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := env.registry.Add(runningApp(owner))
	env.svc.casting = &hookCasting{fakeCasting: env.casting, onStop: func() {
		entry.Update(func(a *app.App) { a.State = app.StateError })
	}}
	view, err := env.svc.StopApp(context.Background(), owner, "app_1")
	if err != nil {
		t.Fatalf("StopApp: %v", err)
	}
	if view.State != app.StateError {
		t.Fatalf("the concurrent error state was overwritten: %+v", view)
	}
}

func TestWaitPlayingStopsWhenTheStartContextIsCanceled(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	env.registry.Add(idleApp(t, owner))
	env.svc.cfg.ProjectionTimeout = time.Minute
	env.casting.session = casting.Session{ID: "ps_1", State: casting.SessionStarting}
	ctx, cancel := context.WithCancel(context.Background())
	env.svc.casting = &hookCasting{fakeCasting: env.casting, onGet: cancel}
	if _, err := env.svc.StartApp(ctx, owner, "app_1", "tv_1", false); toolCode(t, err) != "PROJECTION_TIMEOUT" {
		t.Fatalf("err = %v", err)
	}
}

func TestReconcileErrorPaths(t *testing.T) {
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	ctx := context.Background()
	record := func(env *testEnv, value app.App) *app.Record {
		return env.registry.Add(value)
	}

	t.Run("update runtime of a missing app", func(t *testing.T) {
		env := newTestEnv(t, nil)
		if err := env.svc.updateRuntime("missing", func(*app.App) {}); toolCode(t, err) != "APP_NOT_FOUND" {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("port listing fails during runtime reconciliation", func(t *testing.T) {
		env := newTestEnv(t, nil)
		entry := record(env, app.App{ID: "app_1", Owner: owner, PendingRuntime: true})
		env.render.listErr = errors.New("render down")
		if err := env.svc.reconcile(ctx, entry); err == nil {
			t.Fatal("reconcile ignored a port listing failure")
		}
	})
	t.Run("two instances require review", func(t *testing.T) {
		env := newTestEnv(t, nil)
		entry := record(env, app.App{ID: "app_1", Owner: owner, PendingRuntime: true})
		env.render.ports = []render.Port{{AppID: "app_1", InstanceID: "a"}, {AppID: "app_1", InstanceID: "b"}}
		err := env.svc.reconcile(ctx, entry)
		if err == nil || !strings.Contains(err.Error(), "multiple") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("instance probe fails", func(t *testing.T) {
		env := newTestEnv(t, nil)
		entry := record(env, app.App{ID: "app_1", Owner: owner, PendingRuntime: true})
		env.render.ports = []render.Port{{AppID: "app_1", InstanceID: "inst_1"}}
		env.render.getErr = errors.New("render down")
		if err := env.svc.reconcile(ctx, entry); err == nil {
			t.Fatal("reconcile ignored a failed instance probe")
		}
	})
	t.Run("foreign instance is never adopted", func(t *testing.T) {
		env := newTestEnv(t, nil)
		entry := record(env, app.App{ID: "app_1", Owner: owner, PendingRuntime: true})
		env.render.ports = []render.Port{{AppID: "app_1", InstanceID: "inst_1"}}
		env.render.getInstance = render.App{InstanceID: "inst_1", AppID: "other"}
		err := env.svc.reconcile(ctx, entry)
		if err == nil || !strings.Contains(err.Error(), "foreign") {
			t.Fatalf("err = %v", err)
		}
		if entry.Snapshot().InstanceID != "" {
			t.Fatal("a foreign instance was adopted")
		}
	})
	t.Run("adopting the instance is durable", func(t *testing.T) {
		env := newTestEnv(t, nil)
		entry := record(env, app.App{ID: "app_1", Owner: owner, PendingRuntime: true})
		env.render.ports = []render.Port{{AppID: "app_1", InstanceID: "inst_1"}}
		env.render.getInstance = render.App{InstanceID: "inst_1", AppID: "app_1"}
		env.registry.SetPersister(failingRuntimeStore{func(a app.App) bool {
			return !a.PendingRuntime && a.InstanceID == "inst_1"
		}})
		if err := env.svc.reconcile(ctx, entry); err == nil {
			t.Fatal("reconcile adopted an instance without a durable record")
		}
		if !entry.Snapshot().PendingRuntime {
			t.Fatal("the reservation was dropped without a durable record")
		}
	})
	t.Run("target probe fails during projection reconciliation", func(t *testing.T) {
		env := newTestEnv(t, nil)
		entry := record(env, app.App{ID: "app_1", Owner: owner, PendingProjection: true, TargetID: "tv_1"})
		env.casting.targetErr = errors.New("casting down")
		if err := env.svc.reconcile(ctx, entry); err == nil {
			t.Fatal("reconcile ignored a failed target probe")
		}
	})
	t.Run("session probe fails during projection reconciliation", func(t *testing.T) {
		env := newTestEnv(t, nil)
		entry := record(env, app.App{ID: "app_1", Owner: owner, PendingProjection: true, TargetID: "tv_1"})
		env.casting.target.ActiveSessionID = "ps_x"
		env.casting.getErr = errors.New("casting down")
		if err := env.svc.reconcile(ctx, entry); err == nil {
			t.Fatal("reconcile ignored a failed session probe")
		}
	})
	t.Run("adopting the projection is durable", func(t *testing.T) {
		env := newTestEnv(t, nil)
		source := casting.StreamSource{Type: "stream", URL: "http://127.0.0.1:39000/stream"}
		entry := record(env, app.App{ID: "app_1", Owner: owner, PendingProjection: true, TargetID: "tv_1", EndpointID: "ep_1", Source: source})
		env.casting.target.ActiveSessionID = "ps_x"
		env.casting.session = casting.Session{ID: "ps_x", State: casting.SessionPlaying, TargetID: "tv_1", EndpointID: "ep_1", Source: source}
		env.registry.SetPersister(failingRuntimeStore{func(a app.App) bool {
			return !a.PendingProjection && a.ProjectionSessionID == "ps_x"
		}})
		if err := env.svc.reconcile(ctx, entry); err == nil {
			t.Fatal("reconcile adopted a projection without a durable record")
		}
		if !entry.Snapshot().PendingProjection || entry.Snapshot().ProjectionSessionID != "" {
			t.Fatalf("snapshot = %+v", entry.Snapshot())
		}
	})
	t.Run("stopping the projection fails", func(t *testing.T) {
		env := newTestEnv(t, nil)
		entry := record(env, app.App{ID: "app_1", Owner: owner, ProjectionSessionID: "ps_1"})
		env.casting.stopErr = errors.New("casting down")
		if err := env.svc.reconcile(ctx, entry); err == nil {
			t.Fatal("reconcile ignored a failed projection stop")
		}
		if entry.Snapshot().ProjectionSessionID != "ps_1" {
			t.Fatal("the projection reservation was dropped after a failed stop")
		}
	})
	t.Run("projection cleanup is incomplete", func(t *testing.T) {
		env := newTestEnv(t, nil)
		entry := record(env, app.App{ID: "app_1", Owner: owner, ProjectionSessionID: "ps_1"})
		env.svc.casting = &hookCasting{fakeCasting: env.casting, stopState: casting.SessionPlaying}
		err := env.svc.reconcile(ctx, entry)
		if err == nil || !strings.Contains(err.Error(), "projection cleanup") {
			t.Fatalf("err = %v", err)
		}
		if entry.Snapshot().ProjectionSessionID != "ps_1" {
			t.Fatal("an unconfirmed stop released the projection")
		}
	})
	t.Run("runtime cleanup is incomplete", func(t *testing.T) {
		env := newTestEnv(t, nil)
		entry := record(env, app.App{ID: "app_1", Owner: owner, InstanceID: "inst_1"})
		env.svc.render = &hookRender{fakeRender: env.render, deleteState: render.StateError}
		err := env.svc.reconcile(ctx, entry)
		if err == nil || !strings.Contains(err.Error(), "runtime cleanup") {
			t.Fatalf("err = %v", err)
		}
		if entry.Snapshot().InstanceID != "inst_1" {
			t.Fatal("an unconfirmed delete released the instance")
		}
	})
	t.Run("clearing the instance is durable", func(t *testing.T) {
		env := newTestEnv(t, nil)
		entry := record(env, app.App{ID: "app_1", Owner: owner, InstanceID: "inst_1", Port: 39000})
		env.registry.SetPersister(failingRuntimeStore{func(a app.App) bool {
			return a.InstanceID == "" && a.Port == 0
		}})
		if err := env.svc.reconcile(ctx, entry); err == nil {
			t.Fatal("reconcile cleared an instance without a durable record")
		}
		if entry.Snapshot().InstanceID != "inst_1" {
			t.Fatal("the instance id was cleared without a durable record")
		}
	})
}

func TestRecoverRuntimeIsDurable(t *testing.T) {
	env := newTestEnv(t, nil)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := env.registry.Add(app.App{ID: "app_1", Owner: owner, PendingRuntime: true})
	env.registry.SetPersister(failingRuntimeStore{func(a app.App) bool {
		return a.Error != nil && a.Error.Code == "RECOVERY_PENDING"
	}})
	if err := env.svc.RecoverRuntime(); err == nil {
		t.Fatal("RecoverRuntime reported success after a persistence failure")
	}
	if entry.Snapshot().State == app.StateError {
		t.Fatal("an interrupted start was marked recovered without a durable record")
	}
}
