package service

import (
	"context"
	"errors"
	"sync"
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

func TestCreateDoesNotBuildWithoutDurableRecord(t *testing.T) {
	env := newTestEnv(t)
	env.registry.SetPersister(failingRuntimeStore{func(app.App) bool { return true }})
	_, err := env.svc.CreateApp(context.Background(), app.Identity{DeviceID: "d", SessionID: "s"}, "counter", nil)
	if err == nil || len(env.builder.requests()) != 0 {
		t.Fatal("cloud build submitted without a durable app record")
	}
}

type capturingStore struct {
	mu    sync.Mutex
	saved []app.App
}

func (c *capturingStore) SaveApp(a app.App) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.saved = append(c.saved, a)
	return nil
}
func (*capturingStore) DeleteApp(string) error { return nil }

func (c *capturingStore) last() (app.App, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.saved) == 0 {
		return app.App{}, false
	}
	return c.saved[len(c.saved)-1], true
}

func TestUpdateAppPersistsBuildingBeforeContinue(t *testing.T) {
	env := newTestEnv(t)
	owner := app.Identity{DeviceID: "d", SessionID: "s"}
	store := &capturingStore{}
	env.registry.SetPersister(store)
	env.registry.Add(app.App{
		ID: "app_1", Owner: owner, State: app.StateIdle, SessionID: "ses_1", Prompt: "旧需求",
		Artifact: app.Artifact{SHA256: "old", Path: "old.tar.gz"},
	})
	if _, err := env.svc.UpdateApp(context.Background(), owner, "app_1", "改成动物主题"); err != nil {
		t.Fatalf("UpdateApp: %v", err)
	}
	saved, ok := store.last()
	if !ok || saved.ID != "app_1" || saved.State != app.StateBuilding || saved.Prompt != "改成动物主题" {
		t.Fatalf("persisted after update-app = %+v", saved)
	}
	if saved.SessionID != "ses_1" || saved.Artifact.SHA256 != "old" {
		t.Fatalf("update must keep session and old artifact while building: %+v", saved)
	}
	status, err := env.svc.GetAppStatus(context.Background(), owner, "app_1")
	if err != nil || status.State != app.StateBuilding {
		t.Fatalf("get-app-status after update = %+v, %v", status, err)
	}
}

func TestUpdateAppDoesNotContinueWithoutDurableBuilding(t *testing.T) {
	env := newTestEnv(t)
	owner := app.Identity{DeviceID: "d", SessionID: "s"}
	env.registry.Add(app.App{
		ID: "app_1", Owner: owner, State: app.StateIdle, SessionID: "ses_1",
		Artifact: app.Artifact{SHA256: "old", Path: "old.tar.gz"},
	})
	env.registry.SetPersister(failingRuntimeStore{func(a app.App) bool { return a.State == app.StateBuilding }})
	_, err := env.svc.UpdateApp(context.Background(), owner, "app_1", "改主题")
	if toolCode(t, err) != "PERSISTENCE_FAILED" {
		t.Fatalf("error = %v", err)
	}
	if len(env.builder.continueRequests()) != 0 {
		t.Fatal("continue ran without a durable building record")
	}
	if got := mustGet(t, env, "app_1"); got.State != app.StateIdle {
		t.Fatalf("memory changed despite persist failure: %+v", got)
	}
}
