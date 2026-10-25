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

// schemaVersion is the newest schema this binary understands. A database that
// reports a newer version is refused so an older binary cannot corrupt it.
const schemaVersion = 3

var memorySequence atomic.Uint64

// schemaV1 introduced the application and artifact tables.
var schemaV1 = []string{
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

// schemaV2 introduced the portal account, session, device and setting tables.
var schemaV2 = []string{
	`CREATE TABLE IF NOT EXISTS portal_users (
		id TEXT PRIMARY KEY,
		username TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS portal_sessions (
		token_hash TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		created_at TEXT NOT NULL,
		expires_at TEXT NOT NULL,
		last_seen_at TEXT NOT NULL,
		user_agent TEXT NOT NULL DEFAULT '',
		FOREIGN KEY (user_id) REFERENCES portal_users(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS portal_sessions_user ON portal_sessions(user_id)`,
	`CREATE TABLE IF NOT EXISTS portal_devices (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		kind TEXT NOT NULL,
		mac TEXT NOT NULL UNIQUE,
		client_id TEXT NOT NULL,
		linx_agent_id TEXT NOT NULL DEFAULT '',
		agent_name TEXT NOT NULL DEFAULT '',
		state TEXT NOT NULL,
		step TEXT NOT NULL DEFAULT '',
		error TEXT NOT NULL DEFAULT '',
		credential_path TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		registered_at TEXT,
		FOREIGN KEY (user_id) REFERENCES portal_users(id) ON DELETE CASCADE
	)`,
	`CREATE INDEX IF NOT EXISTS portal_devices_user ON portal_devices(user_id, created_at)`,
	`CREATE TABLE IF NOT EXISTS portal_settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`,
}

// schemaV3 adds roles and account status, per-user service bindings and
// platform-level settings. It deliberately does not touch the apps table: app
// ownership stays the (owner_device_id, owner_session_id) pair plus the agent
// ID inside the JSON payload.
var schemaV3 = []string{
	`ALTER TABLE portal_users ADD COLUMN role TEXT NOT NULL DEFAULT 'user'
		CHECK (role IN ('admin','user'))`,
	`ALTER TABLE portal_users ADD COLUMN status TEXT NOT NULL DEFAULT 'active'
		CHECK (status IN ('active','disabled'))`,
	`ALTER TABLE portal_users ADD COLUMN last_login_at TEXT`,
	`CREATE INDEX IF NOT EXISTS portal_users_role ON portal_users(role, status)`,
	`CREATE INDEX IF NOT EXISTS portal_sessions_expires ON portal_sessions(expires_at)`,
	// One virtual agent per account. A legacy database that somehow holds two
	// is rejected by prepareSchemaV3 instead of being silently cleaned up.
	`CREATE UNIQUE INDEX IF NOT EXISTS portal_devices_one_virtual
		ON portal_devices(user_id) WHERE kind = 'virtual'`,
	`CREATE UNIQUE INDEX IF NOT EXISTS portal_devices_agent
		ON portal_devices(linx_agent_id) WHERE linx_agent_id <> ''`,
	`CREATE INDEX IF NOT EXISTS portal_devices_mac_agent ON portal_devices(mac, linx_agent_id)`,
	`CREATE TABLE IF NOT EXISTS portal_service_bindings (
		user_id           TEXT PRIMARY KEY,
		mode              TEXT NOT NULL DEFAULT 'managed'
			CHECK (mode IN ('managed','custom')),
		render_url        TEXT NOT NULL DEFAULT '',
		casting_url       TEXT NOT NULL DEFAULT '',
		render_token_enc  TEXT NOT NULL DEFAULT '',
		casting_token_enc TEXT NOT NULL DEFAULT '',
		created_at        TEXT NOT NULL,
		updated_at        TEXT NOT NULL,
		FOREIGN KEY (user_id) REFERENCES portal_users(id) ON DELETE CASCADE
	)`,
	`CREATE TABLE IF NOT EXISTS platform_settings (
		key        TEXT PRIMARY KEY,
		value      TEXT NOT NULL,
		is_secret  INTEGER NOT NULL DEFAULT 0,
		updated_at TEXT NOT NULL,
		updated_by TEXT NOT NULL DEFAULT ''
	)`,
}

type migration struct {
	version    int
	statements []string
	// prepare runs before the migration statements and may reject a database
	// whose data cannot satisfy the new constraints.
	prepare func(ctx context.Context, tx *sql.Tx) error
}

var migrations = []migration{
	{version: 1, statements: schemaV1},
	{version: 2, statements: schemaV2},
	{version: 3, statements: schemaV3, prepare: prepareSchemaV3},
}

// prepareSchemaV3 refuses duplicate virtual devices and duplicate Linx agent
// IDs. Deleting either would destroy a user's records, so the operator has to
// resolve them by hand.
func prepareSchemaV3(ctx context.Context, tx *sql.Tx) error {
	var userID string
	err := tx.QueryRowContext(ctx, `SELECT user_id FROM portal_devices
		WHERE kind = 'virtual' GROUP BY user_id HAVING COUNT(*) > 1 LIMIT 1`).Scan(&userID)
	if err == nil {
		return fmt.Errorf("portal user %q owns more than one virtual device; resolve the duplicates before upgrading", userID)
	}
	if err != sql.ErrNoRows {
		return err
	}
	var agentID string
	err = tx.QueryRowContext(ctx, `SELECT linx_agent_id FROM portal_devices
		WHERE linx_agent_id <> '' GROUP BY linx_agent_id HAVING COUNT(*) > 1 LIMIT 1`).Scan(&agentID)
	if err == nil {
		return fmt.Errorf("Linx agent %q is bound to more than one device; resolve the duplicates before upgrading", agentID)
	}
	if err != sql.ErrNoRows {
		return err
	}
	return nil
}

// DB is the SQLite-backed control-plane store.
type DB struct {
	sql *sql.DB
}

// Open creates or opens the database and applies outstanding migrations.
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
	db := &DB{sql: database}
	if err := db.migrate(ctx); err != nil {
		database.Close()
		return nil, err
	}
	return db, nil
}

func (d *DB) migrate(ctx context.Context) error {
	var version int
	if err := d.sql.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("store: read schema version: %w", err)
	}
	if version > schemaVersion {
		return fmt.Errorf("store: database schema version %d is newer than %d", version, schemaVersion)
	}
	for _, step := range migrations {
		if step.version <= version {
			continue
		}
		if err := d.applyMigration(ctx, step); err != nil {
			return err
		}
		version = step.version
	}
	return nil
}

// applyMigration runs one version in a transaction. PRAGMA user_version is
// written inside it, so a failed migration leaves both the schema and the
// recorded version untouched.
func (d *DB) applyMigration(ctx context.Context, step migration) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin schema v%d: %w", step.version, err)
	}
	defer func() { _ = tx.Rollback() }()
	if step.prepare != nil {
		if err := step.prepare(ctx, tx); err != nil {
			return fmt.Errorf("store: schema v%d precheck: %w", step.version, err)
		}
	}
	for _, statement := range step.statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("store: apply schema v%d: %w", step.version, err)
		}
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", step.version)); err != nil {
		return fmt.Errorf("store: set schema version %d: %w", step.version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit schema v%d: %w", step.version, err)
	}
	return nil
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
