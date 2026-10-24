// Package store persists control-plane applications and artifact metadata in
// SQLite so state survives restarts.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/artifact"
)

const schemaVersion = 1

var memorySequence atomic.Uint64

var schema = []string{
	`CREATE TABLE IF NOT EXISTS apps (
		id TEXT PRIMARY KEY,
		owner_device_id TEXT NOT NULL,
		owner_session_id TEXT NOT NULL,
		state TEXT NOT NULL,
		revision INTEGER NOT NULL,
		data TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS apps_owner_state ON apps(owner_device_id, owner_session_id, state)`,
	`CREATE TABLE IF NOT EXISTS artifacts (
		app_id TEXT PRIMARY KEY,
		path TEXT NOT NULL,
		size INTEGER NOT NULL,
		sha256 TEXT NOT NULL,
		token TEXT NOT NULL,
		download_url TEXT NOT NULL,
		stored_at TEXT NOT NULL
	)`,
}

// DB is the SQLite-backed control-plane store.
type DB struct {
	sql *sql.DB
}

// Open creates or opens the database and applies the schema.
func Open(path string) (*DB, error) {
	if path == "" {
		return nil, fmt.Errorf("store: database path is required")
	}
	if path != ":memory:" {
		dir := filepath.Dir(path)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("store: create database directory: %w", err)
			}
		}
	}
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	if path == ":memory:" {
		dsn = fmt.Sprintf("file:gateway-memory-%d?mode=memory&cache=shared&_pragma=busy_timeout(5000)", memorySequence.Add(1))
	}
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open database: %w", err)
	}
	database.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := database.PingContext(ctx); err != nil {
		database.Close()
		return nil, fmt.Errorf("store: ping database: %w", err)
	}
	var version int
	if err := database.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		database.Close()
		return nil, fmt.Errorf("store: read schema version: %w", err)
	}
	if version > schemaVersion {
		database.Close()
		return nil, fmt.Errorf("store: database schema version %d is newer than %d", version, schemaVersion)
	}
	for _, statement := range schema {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			database.Close()
			return nil, fmt.Errorf("store: apply schema: %w", err)
		}
	}
	if _, err := database.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		database.Close()
		return nil, fmt.Errorf("store: set schema version: %w", err)
	}
	return &DB{sql: database}, nil
}

// Close closes the database.
func (d *DB) Close() error { return d.sql.Close() }

// Ping checks database availability.
func (d *DB) Ping(ctx context.Context) error { return d.sql.PingContext(ctx) }

// SaveApp inserts or updates an application snapshot. Older revisions are
// ignored so concurrent writers cannot overwrite newer state.
func (d *DB) SaveApp(value app.App) error {
	if value.ID == "" {
		return fmt.Errorf("store: app id is required")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("store: encode app %s: %w", value.ID, err)
	}
	_, err = d.sql.Exec(`INSERT INTO apps
		(id, owner_device_id, owner_session_id, state, revision, data, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			owner_device_id = excluded.owner_device_id,
			owner_session_id = excluded.owner_session_id,
			state = excluded.state,
			revision = excluded.revision,
			data = excluded.data,
			updated_at = excluded.updated_at
		WHERE excluded.revision >= apps.revision`,
		value.ID, value.Owner.DeviceID, value.Owner.SessionID, string(value.State), value.Revision,
		string(data), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("store: save app %s: %w", value.ID, err)
	}
	return nil
}

// DeleteApp removes an application snapshot.
func (d *DB) DeleteApp(id string) error {
	if _, err := d.sql.Exec(`DELETE FROM apps WHERE id = ?`, id); err != nil {
		return fmt.Errorf("store: delete app %s: %w", id, err)
	}
	return nil
}

// ListApps returns every persisted application ordered by last update.
func (d *DB) ListApps() ([]app.App, error) {
	rows, err := d.sql.Query(`SELECT data FROM apps ORDER BY updated_at, id`)
	if err != nil {
		return nil, fmt.Errorf("store: list apps: %w", err)
	}
	defer rows.Close()
	var values []app.App
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("store: scan app: %w", err)
		}
		var value app.App
		if err := json.Unmarshal([]byte(data), &value); err != nil {
			return nil, fmt.Errorf("store: decode app: %w", err)
		}
		if value.ID == "" {
			continue
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list apps: %w", err)
	}
	return values, nil
}

// SaveArtifact inserts or updates artifact metadata.
func (d *DB) SaveArtifact(item artifact.Item) error {
	if item.AppID == "" {
		return fmt.Errorf("store: artifact app id is required")
	}
	_, err := d.sql.Exec(`INSERT INTO artifacts
		(app_id, path, size, sha256, token, download_url, stored_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(app_id) DO UPDATE SET
			path = excluded.path,
			size = excluded.size,
			sha256 = excluded.sha256,
			token = excluded.token,
			download_url = excluded.download_url,
			stored_at = excluded.stored_at`,
		item.AppID, item.Path, item.Size, item.SHA256, item.Token, item.DownloadURL,
		item.StoredAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("store: save artifact %s: %w", item.AppID, err)
	}
	return nil
}

// DeleteArtifact removes the metadata of one application's package.
func (d *DB) DeleteArtifact(appID string) error {
	if _, err := d.sql.Exec(`DELETE FROM artifacts WHERE app_id = ?`, appID); err != nil {
		return fmt.Errorf("store: delete artifact %s: %w", appID, err)
	}
	return nil
}

// ListArtifacts returns every persisted artifact.
func (d *DB) ListArtifacts() ([]artifact.Item, error) {
	rows, err := d.sql.Query(`SELECT app_id, path, size, sha256, token, download_url, stored_at FROM artifacts ORDER BY stored_at, app_id`)
	if err != nil {
		return nil, fmt.Errorf("store: list artifacts: %w", err)
	}
	defer rows.Close()
	var items []artifact.Item
	for rows.Next() {
		var (
			item     artifact.Item
			storedAt string
		)
		if err := rows.Scan(&item.AppID, &item.Path, &item.Size, &item.SHA256, &item.Token, &item.DownloadURL, &storedAt); err != nil {
			return nil, fmt.Errorf("store: scan artifact: %w", err)
		}
		parsed, err := time.Parse(time.RFC3339Nano, storedAt)
		if err != nil {
			return nil, fmt.Errorf("store: decode artifact %s time: %w", item.AppID, err)
		}
		item.StoredAt = parsed
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list artifacts: %w", err)
	}
	return items, nil
}
