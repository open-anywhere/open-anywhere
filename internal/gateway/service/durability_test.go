package service

import (
	"context"
	"errors"
	"testing"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
)

type failingRuntimeStore struct{ reject func(app.App) bool }

func (p failingRuntimeStore) SaveApp(a app.App) error {
	if p.reject(a) {
		return errors.New("disk unavailable")
	}
	return nil
}
func (failingRuntimeStore) DeleteApp(string) error { return nil }

func TestStartStopsAtUndurableBoundary(t *testing.T) {
	for _, tc := range []struct {
		name        string
		reject      func(app.App) bool
		renderCalls int
	}{
		{"screen reservation", func(a app.App) bool { return a.ScreenID != "" }, 0},
		{"before render", func(a app.App) bool { return a.PendingRuntime }, 0},
		{"render response", func(a app.App) bool { return a.InstanceID != "" }, 1},
		{"before projection", func(a app.App) bool { return a.PendingProjection }, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestEnv(t, nil)
			owner := app.Identity{DeviceID: "d", SessionID: "s"}
			env.registry.Add(idleApp(t, owner))
			env.registry.SetPersister(failingRuntimeStore{tc.reject})
			_, err := env.svc.StartApp(context.Background(), owner, "app_1", "tv_1")
			if toolCode(t, err) != "PERSISTENCE_FAILED" {
				t.Fatalf("expected persistence failure, got %v", err)
			}
			if len(env.render.requests()) != tc.renderCalls || len(env.casting.requests()) != 0 {
				t.Fatalf("downstream calls after undurable state: render=%d casting=%d", len(env.render.requests()), len(env.casting.requests()))
			}
		})
	}
}

func TestCleanupRetainsReservationWhenPersistenceFails(t *testing.T) {
	env := newTestEnv(t, nil)
	value := runningApp(app.Identity{DeviceID: "d", SessionID: "s"})
	value.State, value.CleanupPending = app.StateError, true
	record := env.registry.Add(value)
	env.registry.SetPersister(failingRuntimeStore{func(app.App) bool { return true }})
	if err := env.svc.reconcile(context.Background(), record); err == nil {
		t.Fatal("cleanup ignored disk failure")
	}
	if record.Snapshot().ProjectionSessionID != value.ProjectionSessionID || len(env.render.deletedIDs()) != 0 {
		t.Fatal("cleanup lost the durable reservation or continued after failure")
	}
	if _, occupied := env.registry.RunningOnScreen(value.ScreenID); !occupied {
		t.Fatal("screen released before durable cleanup")
	}
}

func TestCreateDoesNotBuildWithoutDurableRecord(t *testing.T) {
	env := newTestEnv(t, nil)
	env.registry.SetPersister(failingRuntimeStore{func(app.App) bool { return true }})
	_, err := env.svc.CreateApp(context.Background(), app.Identity{DeviceID: "d", SessionID: "s"}, "counter", nil)
	if err == nil || len(env.builder.requests()) != 0 {
		t.Fatal("cloud build submitted without a durable app record")
	}
}
