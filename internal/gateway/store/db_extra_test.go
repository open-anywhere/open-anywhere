package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/artifact"
)

func TestOpenErrors(t *testing.T) {
	if _, err := Open(""); err == nil {
		t.Fatal("empty path should fail")
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(filepath.Join(file, "nested", "db")); err == nil {
		t.Fatal("unwritable directory should fail")
	}
}

func TestOpenRejectsNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway.db")
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.sql.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("error = %v", err)
	}
}

func TestPingAndClosedDatabase(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if err := database.SaveApp(app.App{ID: "app_1", State: app.StateIdle}); err != nil {
		t.Fatalf("SaveApp: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if err := database.Ping(context.Background()); err == nil {
		t.Fatal("ping after close should fail")
	}
	if err := database.SaveApp(app.App{ID: "app_1"}); err == nil {
		t.Fatal("save after close should fail")
	}
	if err := database.DeleteApp("app_1"); err == nil {
		t.Fatal("delete after close should fail")
	}
	if _, err := database.ListApps(); err == nil {
		t.Fatal("list after close should fail")
	}
	if _, err := database.ListArtifacts(); err == nil {
		t.Fatal("list artifacts after close should fail")
	}
	if err := database.SaveArtifact(artifact.Item{AppID: "app_1"}); err == nil {
		t.Fatal("save artifact after close should fail")
	}
}

func TestSaveValidation(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.SaveApp(app.App{}); err == nil {
		t.Fatal("empty app id should fail")
	}
	if err := database.SaveArtifact(artifact.Item{}); err == nil {
		t.Fatal("empty artifact app id should fail")
	}
}

func TestListAppsSkipsAndRejectsBadRows(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.sql.Exec(`INSERT INTO apps (id, owner_device_id, owner_session_id, state, revision, data, updated_at) VALUES ('empty', '', '', 'idle', 1, '{}', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	values, err := database.ListApps()
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 0 {
		t.Fatalf("values = %+v", values)
	}
	if _, err := database.sql.Exec(`INSERT INTO apps (id, owner_device_id, owner_session_id, state, revision, data, updated_at) VALUES ('bad', '', '', 'idle', 1, '{', '2026-01-01T00:00:01Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ListApps(); err == nil {
		t.Fatal("bad JSON should fail")
	}
}

func TestListArtifactsRejectsBadTime(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.sql.Exec(`INSERT INTO artifacts (app_id, path, size, sha256, token, download_url, stored_at) VALUES ('bad', 'p', 1, 's', 't', 'u', 'not-a-time')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ListArtifacts(); err == nil {
		t.Fatal("bad time should fail")
	}
}

func TestSaveArtifactUpdates(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	item := artifact.Item{AppID: "app_1", Path: "p1", Size: 1, SHA256: "s", Token: "t", DownloadURL: "u", StoredAt: time.Now()}
	if err := database.SaveArtifact(item); err != nil {
		t.Fatal(err)
	}
	item.Size = 2
	if err := database.SaveArtifact(item); err != nil {
		t.Fatal(err)
	}
	items, err := database.ListArtifacts()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Size != 2 {
		t.Fatalf("items = %+v", items)
	}
	if err := database.DeleteApp("missing"); err != nil {
		t.Fatalf("DeleteApp: %v", err)
	}
	if err := database.SaveApp(app.App{ID: "app_1", State: app.StateIdle}); err != nil {
		t.Fatalf("SaveApp: %v", err)
	}
	if err := database.DeleteApp("app_1"); err != nil {
		t.Fatalf("DeleteApp: %v", err)
	}
	values, err := database.ListApps()
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 0 {
		t.Fatalf("values = %+v", values)
	}
}
