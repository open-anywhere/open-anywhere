package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/artifact"
	"github.com/open-anywhere/open-anywhere/internal/gateway/openapi"
)

const testSpec = `openapi: 3.1.0
paths:
  /api/roll:
    post:
      operationId: rollDice
      responses:
        "200":
          description: ok
`

func TestAppPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "gateway.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	spec, err := openapi.Parse([]byte(testSpec))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	started := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	value := app.App{
		ID:               "app_1",
		Revision:         3,
		Owner:            app.Identity{DeviceID: "device", SessionID: "session"},
		Prompt:           "做一个小游戏",
		PlayerNames:      []string{"韩鲁鹏", "邓军辉"},
		Name:             "quiz",
		State:            app.StateIdle,
		SessionID:        "ses_1",
		BuildStartedAt:   started,
		LastAgentMessage: "done",
		OpenAPI:          spec,
	}
	if err := db.SaveApp(value); err != nil {
		t.Fatalf("SaveApp: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	values, err := reopened.ListApps()
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	if len(values) != 1 {
		t.Fatalf("apps = %+v", values)
	}
	got := values[0]
	if got.ID != "app_1" || got.Name != "quiz" || got.State != app.StateIdle || got.Revision != 3 {
		t.Fatalf("app = %+v", got)
	}
	if got.Owner.DeviceID != "device" || got.Owner.SessionID != "session" || got.SessionID != "ses_1" {
		t.Fatalf("app = %+v", got)
	}
	if !got.BuildStartedAt.Equal(started) {
		t.Fatalf("BuildStartedAt = %v", got.BuildStartedAt)
	}
	if len(got.PlayerNames) != 2 || got.PlayerNames[0] != "韩鲁鹏" || got.PlayerNames[1] != "邓军辉" {
		t.Fatalf("PlayerNames = %+v", got.PlayerNames)
	}
	if operation, ok := got.OpenAPI.Lookup("rollDice"); !ok || operation.Method != "POST" {
		t.Fatalf("OpenAPI was not restored: %+v", got.OpenAPI)
	}
	if err := reopened.DeleteApp("app_1"); err != nil {
		t.Fatalf("DeleteApp: %v", err)
	}
	if values, err := reopened.ListApps(); err != nil || len(values) != 0 {
		t.Fatalf("apps = %+v, %v", values, err)
	}
}

func TestSaveAppIgnoresOlderRevision(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "gateway.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.SaveApp(app.App{ID: "app_1", Name: "new", Revision: 5}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveApp(app.App{ID: "app_1", Name: "old", Revision: 4}); err != nil {
		t.Fatal(err)
	}
	values, err := db.ListApps()
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0].Name != "new" || values[0].Revision != 5 {
		t.Fatalf("apps = %+v", values)
	}
	if err := db.SaveApp(app.App{}); err == nil {
		t.Fatal("an app without an ID should be rejected")
	}
}

func TestArtifactPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	storedAt := time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC)
	item := artifact.Item{
		AppID:       "app_1",
		Path:        "/var/lib/gateway/app_1.tar.gz",
		Size:        42,
		SHA256:      "abc",
		Token:       "token",
		DownloadURL: "http://gateway/artifacts/app_1/artifact.tar.gz?token=token",
		StoredAt:    storedAt,
	}
	if err := db.SaveArtifact(item); err != nil {
		t.Fatalf("SaveArtifact: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	items, err := reopened.ListArtifacts()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %+v", items)
	}
	got := items[0]
	if got.AppID != item.AppID || got.Path != item.Path || got.Size != item.Size || got.Token != item.Token {
		t.Fatalf("item = %+v", got)
	}
	if !got.StoredAt.Equal(storedAt) {
		t.Fatalf("StoredAt = %v", got.StoredAt)
	}
	if err := reopened.SaveArtifact(artifact.Item{}); err == nil {
		t.Fatal("an artifact without an app ID should be rejected")
	}
}

func TestOpenRejectsInvalidState(t *testing.T) {
	if _, err := Open(""); err == nil {
		t.Fatal("an empty path should be rejected")
	}
	path := filepath.Join(t.TempDir(), "gateway.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if _, err := db.sql.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("err = %v", err)
	}
}
