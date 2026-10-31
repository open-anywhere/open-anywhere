package service

import (
	"context"
	"testing"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/pkg/casting"
)

func TestStartAppRunsWithoutAScreen(t *testing.T) {
	env := newTestEnv(t)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := env.registry.Add(idleApp(t, owner))
	ctx := context.Background()

	view, err := env.svc.StartApp(ctx, owner, "app_1")
	if err != nil {
		t.Fatalf("StartApp: %v", err)
	}
	if view.State != app.StateRunning || view.Name != "quiz" {
		t.Fatalf("view = %+v", view)
	}
	if view.StreamURL != "http://127.0.0.1:39000/stream" {
		t.Fatalf("stream_url = %q", view.StreamURL)
	}
	if view.StartedAt == nil {
		t.Fatalf("started_at missing: %+v", view)
	}

	snapshot := entry.Snapshot()
	if snapshot.InstanceID != "inst_1" || snapshot.Source.URL == "" || snapshot.StartedAt.IsZero() {
		t.Fatalf("runtime = %+v", snapshot)
	}
	if created := env.render.requests(); len(created) != 1 {
		t.Fatalf("render requests = %+v", created)
	}

	// A repeated start converges on the running application.
	again, err := env.svc.StartApp(ctx, owner, "app_1")
	if err != nil {
		t.Fatalf("idempotent start: %v", err)
	}
	if again.State != app.StateRunning || again.StreamURL != view.StreamURL {
		t.Fatalf("idempotent view = %+v", again)
	}
	if len(env.render.requests()) != 1 {
		t.Fatal("idempotent start created another instance")
	}
}

func TestGetAppStatusReturnsStreamURL(t *testing.T) {
	env := newTestEnv(t)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	env.registry.Add(idleApp(t, owner))
	ctx := context.Background()

	if _, err := env.svc.StartApp(ctx, owner, "app_1"); err != nil {
		t.Fatalf("StartApp: %v", err)
	}
	view, err := env.svc.GetAppStatus(ctx, owner, "app_1")
	if err != nil {
		t.Fatalf("GetAppStatus: %v", err)
	}
	if view.StreamURL != "http://127.0.0.1:39000/stream" {
		t.Fatalf("view = %+v", view)
	}
}

func TestStopAppReleasesOnlyTheRuntime(t *testing.T) {
	env := newTestEnv(t)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	env.registry.Add(idleApp(t, owner))
	ctx := context.Background()

	if _, err := env.svc.StartApp(ctx, owner, "app_1"); err != nil {
		t.Fatalf("StartApp: %v", err)
	}
	view, err := env.svc.StopApp(ctx, owner, "app_1")
	if err != nil {
		t.Fatalf("StopApp: %v", err)
	}
	if view.State != app.StateIdle || view.StreamURL != "" {
		t.Fatalf("view = %+v", view)
	}
	if deleted := env.render.deletedIDs(); len(deleted) != 1 || deleted[0] != "inst_1" {
		t.Fatalf("render deleted = %+v", deleted)
	}
}

// The monitor only knows about the runtime now; a missing projection is not a
// failure because the application is not supposed to have one.
func TestMonitorKeepsRunningWithoutAScreen(t *testing.T) {
	env := newTestEnv(t)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := env.registry.Add(idleApp(t, owner))
	ctx := context.Background()

	if _, err := env.svc.StartApp(ctx, owner, "app_1"); err != nil {
		t.Fatalf("StartApp: %v", err)
	}
	env.svc.checkRunning(ctx)
	if got := entry.Snapshot().State; got != app.StateRunning {
		t.Fatalf("state after monitor = %s", got)
	}
	if notes := env.notifier.notifications(); len(notes) != 0 {
		t.Fatalf("monitor reported a failure: %+v", notes)
	}
}

func TestStartAppRequiresAStream(t *testing.T) {
	env := newTestEnv(t)
	owner := app.Identity{DeviceID: "device", SessionID: "session"}
	entry := env.registry.Add(idleApp(t, owner))
	env.render.instance = readyApp()
	env.render.instance.Source = casting.StreamSource{}
	ctx := context.Background()

	if _, err := env.svc.StartApp(ctx, owner, "app_1"); toolCode(t, err) != "STREAM_START_FAILED" {
		t.Fatalf("err = %v", err)
	}
	if got := entry.Snapshot().State; got != app.StateError {
		t.Fatalf("state = %s", got)
	}
}
