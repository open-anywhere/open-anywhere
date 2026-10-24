package store

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/artifact"
)

func TestOpenReportsAnUnusableDatabase(t *testing.T) {
	// A directory is a valid path for sql.Open but cannot be opened as a
	// SQLite database, so the failure must surface from Ping.
	_, err := Open(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "ping database") {
		t.Fatalf("err = %v", err)
	}
}

func TestOpenReportsASchemaThatCannotBeApplied(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`DROP TABLE apps`); err != nil {
		t.Fatal(err)
	}
	// An apps table without the indexed columns makes the schema migration fail.
	if _, err := raw.Exec(`CREATE TABLE apps (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err == nil || !strings.Contains(err.Error(), "apply schema") {
		if reopened != nil {
			reopened.Close()
		}
		t.Fatalf("err = %v", err)
	}
}

func TestListAppsReportsANullColumn(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.sql.Exec(`DROP TABLE apps`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.Exec(`CREATE TABLE apps (id TEXT PRIMARY KEY, updated_at TEXT NOT NULL, data TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.Exec(`INSERT INTO apps (id, updated_at, data) VALUES ('a', 't', NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ListApps(); err == nil || !strings.Contains(err.Error(), "scan app") {
		t.Fatalf("err = %v", err)
	}
}

func TestListArtifactsReportsANullColumn(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.sql.Exec(`DROP TABLE artifacts`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.Exec(`CREATE TABLE artifacts (
		app_id TEXT PRIMARY KEY, path TEXT, size INTEGER, sha256 TEXT,
		token TEXT, download_url TEXT, stored_at TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.Exec(`INSERT INTO artifacts VALUES ('a', 'p', 1, 's', 't', 'u', NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ListArtifacts(); err == nil || !strings.Contains(err.Error(), "scan artifact") {
		t.Fatalf("err = %v", err)
	}
}

func TestDeleteArtifact(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	item := artifact.Item{
		AppID:       "app_1",
		Path:        "/tmp/app_1.tar.gz",
		Size:        12,
		SHA256:      "abc",
		Token:       "token",
		DownloadURL: "http://gateway/artifacts/app_1/artifact.tar.gz?token=token",
		StoredAt:    time.Now().UTC(),
	}
	if err := db.SaveArtifact(item); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteArtifact("app_1"); err != nil {
		t.Fatalf("DeleteArtifact: %v", err)
	}
	items, err := db.ListArtifacts()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("items = %+v", items)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteArtifact("app_1"); err == nil {
		t.Fatal("DeleteArtifact succeeded on a closed database")
	}
}
