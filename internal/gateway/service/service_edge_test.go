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
)

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

func TestStopAppMissingAndTransferredOwnership(t *testing.T) {
	env := newTestEnv(t)
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
		env := newTestEnv(t)
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
		env := newTestEnv(t)
		env.registry.Add(app.App{ID: "app_1", Owner: owner, State: app.StateError})
		env.registry.SetPersister(failingRuntimeStore{func(app.App) bool { return true }})
		if _, err := env.svc.StopApp(context.Background(), owner, "app_1"); toolCode(t, err) != "PERSISTENCE_FAILED" {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("non retryable cleanup succeeds", func(t *testing.T) {
		env := newTestEnv(t)
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
	env := newTestEnv(t)
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

func TestSendRequestTransferredOwnership(t *testing.T) {
	env := newTestEnv(t)
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

func TestRuntimeFailureIgnoresAVanishedRecord(t *testing.T) {
	env := newTestEnv(t)
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
	env := newTestEnv(t)
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

func TestStatusViewClampsFutureTimestamps(t *testing.T) {
	env := newTestEnv(t)
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
	env := newTestEnv(t)
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

func TestPruneHistoryKeepsTheNewestOnly(t *testing.T) {
	env := newTestEnv(t)
	for i := 0; i < HistoryLimit; i++ {
		env.registry.Add(builtApp(t, fmt.Sprintf("keep_%02d", i), i, app.StateIdle))
	}
	env.svc.PruneHistory()
	if got := len(env.registry.List()); got != HistoryLimit {
		t.Fatalf("%d apps left", got)
	}
}

func TestHistorySortTieBreaksByID(t *testing.T) {
	env := newTestEnv(t)
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
	env := newTestEnv(t)
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
	env := newTestEnv(t)
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
	env := newTestEnv(t)
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

func TestRecoverRuntimeIsDurable(t *testing.T) {
	env := newTestEnv(t)
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
