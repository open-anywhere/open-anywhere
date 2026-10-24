package store

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
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

func TestOpenReportsAReadOnlyDatabase(t *testing.T) {
	if os.Geteuid() == 0 || runtime.GOOS == "windows" {
		t.Skip("file permission semantics differ for root and on Windows")
	}
	path := filepath.Join(t.TempDir(), "gateway.db")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	// The database already has the current schema, so every CREATE statement
	// is a no-op and only the schema version bump needs to write.
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	second, err := Open(path)
	if second != nil {
		second.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "set schema version") {
		t.Fatalf("err = %v", err)
	}
}

// corruptLastTableLeaf breaks the last table b-tree leaf page of a SQLite
// file, leaving the schema and the first rows readable. A later row read then
// fails while the cursor walks the table, which is how a truncated or damaged
// package database looks after a crash.
func corruptLastTableLeaf(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pageSize := int(binary.BigEndian.Uint16(data[16:18]))
	if pageSize == 1 {
		pageSize = 65536
	}
	last := -1
	for offset := pageSize; offset+pageSize <= len(data); offset += pageSize {
		// 0x0d marks a table b-tree leaf page.
		if data[offset] == 0x0d {
			last = offset
		}
	}
	if last < 0 {
		t.Fatal("no table leaf page found")
	}
	for i := 0; i < 32 && last+i < len(data); i++ {
		data[last+i] = 0
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestListAppsReportsACorruptPage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// The ordering index lets SQLite stream rows instead of materializing the
	// whole result on the first step, so damage to a later page surfaces while
	// the cursor is already iterating.
	if _, err := db.sql.Exec(`CREATE INDEX apps_order ON apps(updated_at, id)`); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 120; index++ {
		value := app.App{ID: fmt.Sprintf("app-%03d", index), Prompt: strings.Repeat("x", 3000), Revision: 1}
		if err := db.SaveApp(value); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	corruptLastTableLeaf(t, path)
	damaged, err := Open(path)
	if err != nil {
		t.Fatalf("Open damaged database: %v", err)
	}
	defer damaged.Close()
	probe, err := damaged.sql.Query(`SELECT data FROM apps ORDER BY updated_at, id`)
	if err != nil {
		t.Fatalf("query should plan an index scan, failed before iterating: %v", err)
	}
	if !probe.Next() {
		probe.Close()
		t.Fatal("expected at least one readable row before the damaged page")
	}
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := damaged.ListApps(); err == nil || !strings.Contains(err.Error(), "list apps") {
		t.Fatalf("err = %v", err)
	}
}

func TestListArtifactsReportsACorruptPage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.Exec(`CREATE INDEX artifacts_order ON artifacts(stored_at, app_id)`); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 120; index++ {
		item := artifact.Item{
			AppID:       fmt.Sprintf("app-%03d", index),
			Path:        "/tmp/artifact.tar.gz",
			Size:        10,
			SHA256:      "abc",
			Token:       "token",
			DownloadURL: "http://gateway/artifacts/app.tar.gz?token=" + strings.Repeat("x", 3000),
			StoredAt:    time.Unix(int64(index), 0).UTC(),
		}
		if err := db.SaveArtifact(item); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	corruptLastTableLeaf(t, path)
	damaged, err := Open(path)
	if err != nil {
		t.Fatalf("Open damaged database: %v", err)
	}
	defer damaged.Close()
	probe, err := damaged.sql.Query(`SELECT app_id FROM artifacts ORDER BY stored_at, app_id`)
	if err != nil {
		t.Fatalf("query should plan an index scan, failed before iterating: %v", err)
	}
	if !probe.Next() {
		probe.Close()
		t.Fatal("expected at least one readable row before the damaged page")
	}
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := damaged.ListArtifacts(); err == nil || !strings.Contains(err.Error(), "list artifacts") {
		t.Fatalf("err = %v", err)
	}
}

func TestSaveAppReportsAnUnencodableRecord(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// A timestamp outside the range encoding/json can render makes the
	// snapshot unencodable; SaveApp must report that instead of writing a
	// partial row.
	value := app.App{
		ID:        "app_1",
		Revision:  1,
		StartedAt: time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := db.SaveApp(value); err == nil || !strings.Contains(err.Error(), "encode app app_1") {
		t.Fatalf("err = %v", err)
	}
	var count int
	if err := db.sql.QueryRow(`SELECT COUNT(*) FROM apps`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("apps = %d, want 0", count)
	}
}
