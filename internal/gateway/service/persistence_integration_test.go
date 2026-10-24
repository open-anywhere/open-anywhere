package service

import (
	"context"
	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
	"path/filepath"
	"testing"
)

func TestClaimAndReconcileSurviveSQLiteRestart(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "gateway.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	env := newTestEnv(t, nil)
	env.registry.SetPersister(db)
	old := app.Identity{DeviceID: "d", AgentID: "a", SessionID: "old"}
	fresh := old
	fresh.SessionID = "new"
	env.registry.Add(app.App{ID: "app_1", Owner: old, State: app.StateError, InstanceID: "inst_1", ScreenID: "screen", CleanupPending: true})
	if _, err := env.svc.ClaimApp(context.Background(), fresh, "app_1"); err != nil {
		t.Fatal(err)
	}
	values, err := db.ListApps()
	if err != nil {
		t.Fatal(err)
	}
	recovered := newTestEnv(t, nil)
	recovered.registry.Load(values)
	recovered.registry.SetPersister(db)
	record, _ := recovered.registry.Get("app_1")
	if record.Snapshot().Owner.SessionID != "new" {
		t.Fatal("claim not persistent")
	}
	if err := recovered.svc.reconcile(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	values, err = db.ListApps()
	if err != nil || len(values) != 1 || values[0].CleanupPending || values[0].InstanceID != "" {
		t.Fatal(values, err)
	}
}
