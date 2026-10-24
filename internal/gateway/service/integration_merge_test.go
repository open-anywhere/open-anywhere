package service

import (
	"context"
	"errors"
	"testing"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/render"
	"github.com/open-anywhere/open-anywhere/pkg/casting"
)

func TestReconcileLostRenderResponse(t *testing.T) {
	env := newTestEnv(t, nil)
	a := app.App{ID: "app_1", State: app.StateError, ScreenID: "screen", TargetID: "tv_1", PendingRuntime: true, CleanupPending: true}
	record := env.registry.Add(a)
	env.render.ports = []render.Port{{AppID: "app_1", InstanceID: "inst_1", Port: 39000, Occupied: true}}
	env.render.getInstance = render.App{AppID: "app_1", InstanceID: "inst_1", State: render.StateReady}
	if err := env.svc.reconcile(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	got := record.Snapshot()
	if got.PendingRuntime || got.CleanupPending || got.ScreenID != "" || got.InstanceID != "" {
		t.Fatal(got)
	}
	if len(env.render.deleted) != 1 || env.render.deleted[0] != "inst_1" {
		t.Fatal(env.render.deleted)
	}
}
func TestReconcileResolvesMissingInstanceAndRetainsFailedCleanup(t *testing.T) {
	env := newTestEnv(t, nil)
	record := env.registry.Add(app.App{ID: "a", State: app.StateError, ScreenID: "screen", PendingRuntime: true})
	// Render answers and lists no instance for this app, so there is nothing
	// left to protect: the unknown outcome is resolved instead of held open.
	if err := env.svc.reconcile(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if snapshot := record.Snapshot(); snapshot.PendingRuntime || snapshot.ScreenID != "" {
		t.Fatalf("reservation kept without a Render instance: %+v", snapshot)
	}
	record.Update(func(a *app.App) { a.PendingRuntime = false; a.InstanceID = "instance"; a.CleanupPending = true })
	env.render.deleteErr = errors.New("network down")
	if err := env.svc.reconcile(context.Background(), record); err == nil {
		t.Fatal("failed cleanup accepted")
	}
	if record.Snapshot().InstanceID == "" {
		t.Fatal("instance lost")
	}
	env.render.deleteErr = nil
	if err := env.svc.reconcile(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if record.Snapshot().CleanupPending {
		t.Fatal("cleanup still pending")
	}
}
func TestReconcileForeignProjectionNeverStopped(t *testing.T) {
	env := newTestEnv(t, nil)
	record := env.registry.Add(app.App{ID: "a", State: app.StateError, ScreenID: "screen", TargetID: "tv_1", EndpointID: "dlna", Source: casting.StreamSource{URL: "http://mine"}, PendingProjection: true})
	env.casting.target.ActiveSessionID = "foreign"
	env.casting.session = casting.Session{ID: "foreign", TargetID: "tv_1", EndpointID: "dlna", Source: casting.StreamSource{URL: "http://other"}}
	if err := env.svc.reconcile(context.Background(), record); err == nil {
		t.Fatal("foreign adopted")
	}
	if !record.Snapshot().PendingProjection {
		t.Fatal("uncertainty cleared")
	}
}

func TestReconcileLostProjectionResponse(t *testing.T) {
	env := newTestEnv(t, nil)
	source := casting.StreamSource{URL: "http://mine", Format: "mpegts"}
	record := env.registry.Add(app.App{ID: "app_1", State: app.StateError, ScreenID: "screen", TargetID: "tv_1", EndpointID: "dlna", Source: source, PendingProjection: true, CleanupPending: true, InstanceID: "inst_1"})
	env.casting.target.ActiveSessionID = "ps_1"
	env.casting.session = casting.Session{ID: "ps_1", TargetID: "tv_1", EndpointID: "dlna", Source: source, State: casting.SessionPlaying}
	if err := env.svc.reconcile(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	got := record.Snapshot()
	if got.PendingProjection || got.CleanupPending || got.ScreenID != "" {
		t.Fatal(got)
	}
}

func TestReconcileReleasesProjectionWhenTheTargetHasNoSession(t *testing.T) {
	env := newTestEnv(t, nil)
	record := env.registry.Add(app.App{ID: "app_1", State: app.StateError, ScreenID: "screen", TargetID: "tv_1", PendingProjection: true, CleanupPending: true})
	// The target reports no active session: whatever the timed-out create did,
	// nothing is projecting, so the reservation must not outlive it.
	if err := env.svc.reconcile(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	got := record.Snapshot()
	if got.PendingProjection || got.CleanupPending || got.ScreenID != "" || got.TargetID != "" {
		t.Fatalf("reservation kept without any active session: %+v", got)
	}
}

func TestCrashAfterRenderSuccessIsReconciled(t *testing.T) {
	env := newTestEnv(t, nil)
	r := env.registry.Add(app.App{ID: "app_1", State: app.StateIdle, ScreenID: "screen", InstanceID: "inst_1"})
	env.svc.RecoverRuntime()
	if !r.Snapshot().CleanupPending || r.Snapshot().State != app.StateError {
		t.Fatal("interrupted runtime not detected")
	}
	env.svc.checkRunning(context.Background())
	if r.Snapshot().InstanceID != "" || r.Snapshot().ScreenID != "" {
		t.Fatal("cleanup not complete")
	}
}
