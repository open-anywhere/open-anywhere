package service

import (
	"context"
	"testing"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/pkg/casting"
)

// The product start path runs the application in the cloud and hands back a
// stream address. It must never contact casting: which screen shows the stream
// is decided on site.
func TestStartAppRunsWithoutAScreen(t *testing.T) {
	env := newTestEnv(t, nil)
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
	if view.ScreenID != "" {
		t.Fatalf("start-app claimed a screen: %q", view.ScreenID)
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
	if snapshot.ScreenID != "" || snapshot.TargetID != "" || snapshot.EndpointID != "" || snapshot.ProjectionSessionID != "" {
		t.Fatalf("screen bindings leaked into the run: %+v", snapshot)
	}
	if snapshot.OccupiesScreen() {
		t.Fatal("a running application without a screen must not occupy one")
	}
	if created := env.render.requests(); len(created) != 1 {
		t.Fatalf("render requests = %+v", created)
	}
	if sessions := env.casting.requests(); len(sessions) != 0 {
		t.Fatalf("start-app called casting: %+v", sessions)
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

// get-app-status is how Linx picks the stream up, so the address must survive
// a later status query and not just the start response.
func TestGetAppStatusReturnsStreamURL(t *testing.T) {
	env := newTestEnv(t, nil)
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
	if view.StreamURL != "http://127.0.0.1:39000/stream" || view.ScreenID != "" {
		t.Fatalf("view = %+v", view)
	}
}

// Stop releases the runtime and leaves casting alone: there is nothing on a
// screen to stop on the site's behalf.
func TestStopAppReleasesOnlyTheRuntime(t *testing.T) {
	env := newTestEnv(t, nil)
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
	if stopped := env.casting.stoppedIDs(); len(stopped) != 0 {
		t.Fatalf("stop-app stopped casting sessions: %+v", stopped)
	}
}

// The monitor only knows about the runtime now; a missing projection is not a
// failure because the application is not supposed to have one.
func TestMonitorKeepsRunningWithoutAScreen(t *testing.T) {
	env := newTestEnv(t, nil)
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

// Render can be ready before it serves a stream; starting then would hand Linx
// an empty address, so the start fails and the application reports why.
func TestStartAppRequiresAStream(t *testing.T) {
	env := newTestEnv(t, nil)
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
	if sessions := env.casting.requests(); len(sessions) != 0 {
		t.Fatalf("casting was called: %+v", sessions)
	}
}
