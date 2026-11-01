package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustExec(t *testing.T, conn *sql.DB, statements ...string) {
	t.Helper()
	for _, statement := range statements {
		if _, err := conn.Exec(statement); err != nil {
			t.Fatalf("exec %q: %v", statement, err)
		}
	}
}

func TestHasEncryptedSecrets(t *testing.T) {
	ctx := context.Background()

	t.Run("empty database", func(t *testing.T) {
		db := openPortalDB(t)
		found, err := db.HasEncryptedSecrets(ctx)
		if err != nil || found {
			t.Fatalf("HasEncryptedSecrets=%v err=%v", found, err)
		}
	})

	t.Run("encrypted platform setting", func(t *testing.T) {
		db := openPortalDB(t)
		if err := db.PutPlatformSettings(ctx, []PlatformSetting{
			{Key: "agentapi.api_key", Value: "enc:v1:ciphertext", IsSecret: true},
		}); err != nil {
			t.Fatal(err)
		}
		found, err := db.HasEncryptedSecrets(ctx)
		if err != nil || !found {
			t.Fatalf("HasEncryptedSecrets=%v err=%v", found, err)
		}
	})

	t.Run("plaintext secret is not encrypted", func(t *testing.T) {
		db := openPortalDB(t)
		if err := db.PutPlatformSettings(ctx, []PlatformSetting{
			{Key: "agentapi.api_key", Value: "plaintext", IsSecret: true},
		}); err != nil {
			t.Fatal(err)
		}
		found, err := db.HasEncryptedSecrets(ctx)
		if err != nil || found {
			t.Fatalf("HasEncryptedSecrets=%v err=%v", found, err)
		}
	})

	t.Run("encrypted binding token", func(t *testing.T) {
		db := openPortalDB(t)
		if err := db.CreatePortalUser(ctx, PortalUser{ID: "usr_1", Username: "alice", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
		binding := PortalServiceBinding{UserID: "usr_1", Mode: ServiceModeCustom, RenderTokenEnc: "enc:v1:render"}
		if err := db.UpsertServiceBinding(ctx, binding); err != nil {
			t.Fatal(err)
		}
		found, err := db.HasEncryptedSecrets(ctx)
		if err != nil || !found {
			t.Fatalf("HasEncryptedSecrets=%v err=%v", found, err)
		}
	})

	t.Run("closed database", func(t *testing.T) {
		db := openPortalDB(t)
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := db.HasEncryptedSecrets(ctx); err == nil {
			t.Fatal("HasEncryptedSecrets succeeded on a closed database")
		}
	})
}

func TestServiceBindingErrorPaths(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

	t.Run("user id is required", func(t *testing.T) {
		db := openPortalDB(t)
		if _, err := db.ServiceBindingByUser(ctx, ""); err == nil {
			t.Fatal("an empty user id was accepted")
		}
		if err := db.UpsertServiceBinding(ctx, PortalServiceBinding{}); err == nil {
			t.Fatal("an upsert without a user id was accepted")
		}
	})

	t.Run("invalid binding mode", func(t *testing.T) {
		db := openPortalDB(t)
		err := db.UpsertServiceBinding(ctx, PortalServiceBinding{UserID: "usr_1", Mode: "shared"})
		if err == nil || !strings.Contains(err.Error(), "invalid service binding mode") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("empty mode defaults to managed", func(t *testing.T) {
		db := openPortalDB(t)
		if err := db.CreatePortalUser(ctx, PortalUser{ID: "usr_1", Username: "alice", CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		if err := db.UpsertServiceBinding(ctx, PortalServiceBinding{UserID: "usr_1", CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		loaded, err := db.ServiceBindingByUser(ctx, "usr_1")
		if err != nil || loaded.Mode != ServiceModeManaged {
			t.Fatalf("mode=%q err=%v", loaded.Mode, err)
		}
	})

	t.Run("undecodable timestamps", func(t *testing.T) {
		for _, testCase := range []struct{ name, created, updated string }{
			{name: "created", created: "not-a-time", updated: now.Format(time.RFC3339Nano)},
			{name: "updated", created: now.Format(time.RFC3339Nano), updated: "not-a-time"},
		} {
			t.Run(testCase.name, func(t *testing.T) {
				db := openPortalDB(t)
				if err := db.CreatePortalUser(ctx, PortalUser{ID: "usr_1", Username: "alice", CreatedAt: now, UpdatedAt: now}); err != nil {
					t.Fatal(err)
				}
				mustExec(t, db.sql, `INSERT INTO portal_service_bindings
					(user_id, mode, render_url, render_token_enc, created_at, updated_at)
					VALUES ('usr_1', 'managed', '', '', '`+testCase.created+`', '`+testCase.updated+`')`)
				if _, err := db.ServiceBindingByUser(ctx, "usr_1"); err == nil ||
					!strings.Contains(err.Error(), "decode service binding time") {
					t.Fatalf("err = %v", err)
				}
			})
		}
	})

	t.Run("closed database", func(t *testing.T) {
		db := openPortalDB(t)
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ServiceBindingByUser(ctx, "usr_1"); err == nil {
			t.Fatal("ServiceBindingByUser succeeded on a closed database")
		}
		if err := db.UpsertServiceBinding(ctx, PortalServiceBinding{UserID: "usr_1", Mode: ServiceModeManaged}); err == nil {
			t.Fatal("UpsertServiceBinding succeeded on a closed database")
		}
		if err := db.DeleteServiceBinding(ctx, "usr_1"); err == nil {
			t.Fatal("DeleteServiceBinding succeeded on a closed database")
		}
		if _, err := db.ServiceBindingSecrets(ctx); err == nil {
			t.Fatal("ServiceBindingSecrets succeeded on a closed database")
		}
	})
}

func TestServiceBindingSecretsScanErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("null user id", func(t *testing.T) {
		db := openPortalDB(t)
		mustExec(t, db.sql,
			`DROP TABLE portal_service_bindings`,
			`CREATE TABLE portal_service_bindings (user_id TEXT, render_token_enc TEXT, casting_token_enc TEXT)`,
			`INSERT INTO portal_service_bindings VALUES (NULL, 'enc:v1:a', '')`)
		if _, err := db.ServiceBindingSecrets(ctx); err == nil ||
			!strings.Contains(err.Error(), "scan service binding secret") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("failing view", func(t *testing.T) {
		db := openPortalDB(t)
		mustExec(t, db.sql,
			`DROP TABLE portal_service_bindings`,
			`CREATE TABLE base_bindings (user_id TEXT, render_token_enc TEXT, casting_token_enc TEXT)`,
			`INSERT INTO base_bindings VALUES ('usr_1', 'enc:v1:a', '')`,
			`CREATE VIEW portal_service_bindings AS
				SELECT user_id, render_token_enc, casting_token_enc FROM base_bindings
				UNION ALL
				SELECT abs(-9223372036854775808), 'enc:v1:b', ''`)
		if _, err := db.ServiceBindingSecrets(ctx); err == nil ||
			!strings.Contains(err.Error(), "list service binding secrets") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestPlatformSettingErrorPaths(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 25, 13, 0, 0, 0, time.UTC)

	t.Run("closed database", func(t *testing.T) {
		db := openPortalDB(t)
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := db.PlatformSettings(ctx); err == nil {
			t.Fatal("PlatformSettings succeeded on a closed database")
		}
		if err := db.PutPlatformSettings(ctx, []PlatformSetting{{Key: "k", Value: "v"}}); err == nil {
			t.Fatal("PutPlatformSettings succeeded on a closed database")
		}
		if err := db.DeletePlatformSettings(ctx, "k"); err == nil {
			t.Fatal("DeletePlatformSettings succeeded on a closed database")
		}
		if err := db.DeletePortalSettings(ctx, "k"); err == nil {
			t.Fatal("DeletePortalSettings succeeded on a closed database")
		}
	})

	t.Run("cancelled batch is not started", func(t *testing.T) {
		db := openPortalDB(t)
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		err := db.PutPlatformSettings(cancelled, []PlatformSetting{{Key: "k", Value: "v"}})
		if err == nil || !strings.Contains(err.Error(), "begin platform settings") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("insert failure", func(t *testing.T) {
		db := openPortalDB(t)
		mustExec(t, db.sql, `CREATE TRIGGER refuse_insert BEFORE INSERT ON platform_settings
			BEGIN SELECT RAISE(ABORT, 'blocked'); END`)
		err := db.PutPlatformSettings(ctx, []PlatformSetting{{Key: "k", Value: "v", UpdatedAt: now}})
		if err == nil || !strings.Contains(err.Error(), "put platform setting k") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("commit failure leaves no row", func(t *testing.T) {
		db := openPortalDB(t)
		mustExec(t, db.sql,
			`DROP TABLE platform_settings`,
			`CREATE TABLE actors (id TEXT PRIMARY KEY)`,
			`CREATE TABLE platform_settings (
				key TEXT PRIMARY KEY,
				value TEXT NOT NULL,
				is_secret INTEGER NOT NULL DEFAULT 0,
				updated_at TEXT NOT NULL,
				updated_by TEXT NOT NULL DEFAULT '',
				FOREIGN KEY (updated_by) REFERENCES actors(id) DEFERRABLE INITIALLY DEFERRED)`)
		err := db.PutPlatformSettings(ctx, []PlatformSetting{{Key: "k", Value: "v", UpdatedBy: "missing_actor"}})
		if err == nil || !strings.Contains(err.Error(), "commit platform settings") {
			t.Fatalf("err = %v", err)
		}
		var count int
		if err := db.sql.QueryRow(`SELECT COUNT(*) FROM platform_settings`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("a failed commit persisted %d settings", count)
		}
	})

	t.Run("delete failure", func(t *testing.T) {
		db := openPortalDB(t)
		if err := db.PutPlatformSettings(ctx, []PlatformSetting{{Key: "k", Value: "v"}}); err != nil {
			t.Fatal(err)
		}
		mustExec(t, db.sql, `CREATE TRIGGER refuse_delete BEFORE DELETE ON platform_settings
			BEGIN SELECT RAISE(ABORT, 'blocked'); END`)
		err := db.DeletePlatformSettings(ctx, "k")
		if err == nil || !strings.Contains(err.Error(), "delete platform setting k") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestPlatformSettingsReadErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("null value", func(t *testing.T) {
		db := openPortalDB(t)
		mustExec(t, db.sql,
			`DROP TABLE platform_settings`,
			`CREATE TABLE platform_settings (key TEXT, value TEXT, is_secret INTEGER, updated_at TEXT, updated_by TEXT)`,
			`INSERT INTO platform_settings VALUES ('k', NULL, 0, '2026-01-01T00:00:00Z', '')`)
		if _, err := db.PlatformSettings(ctx); err == nil ||
			!strings.Contains(err.Error(), "scan platform setting") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("undecodable timestamp", func(t *testing.T) {
		db := openPortalDB(t)
		mustExec(t, db.sql,
			`DROP TABLE platform_settings`,
			`CREATE TABLE platform_settings (key TEXT, value TEXT, is_secret INTEGER, updated_at TEXT, updated_by TEXT)`,
			`INSERT INTO platform_settings VALUES ('k', 'v', 0, 'not-a-time', '')`)
		if _, err := db.PlatformSettings(ctx); err == nil ||
			!strings.Contains(err.Error(), "decode platform setting time") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("failing view", func(t *testing.T) {
		db := openPortalDB(t)
		mustExec(t, db.sql,
			`DROP TABLE platform_settings`,
			`CREATE TABLE base_settings (key TEXT, value TEXT, is_secret INTEGER, updated_at TEXT, updated_by TEXT)`,
			`INSERT INTO base_settings VALUES ('k', 'v', 0, '2026-01-01T00:00:00Z', '')`,
			`CREATE VIEW platform_settings AS
				SELECT key, value, is_secret, updated_at, updated_by FROM base_settings
				UNION ALL
				SELECT abs(-9223372036854775808), 'v', 0, '2026-01-01T00:00:00Z', ''`)
		if _, err := db.PlatformSettings(ctx); err == nil ||
			!strings.Contains(err.Error(), "list platform settings") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestDeletePortalSettings(t *testing.T) {
	db := openPortalDB(t)
	ctx := context.Background()
	for _, key := range []string{"linx.mcp_server_id", "linx.mcp_instance_id", "linx.other"} {
		if err := db.SetPortalSetting(ctx, key, "value"); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.DeletePortalSettings(ctx, "linx.mcp_server_id", "linx.mcp_instance_id"); err != nil {
		t.Fatalf("DeletePortalSettings: %v", err)
	}
	for _, key := range []string{"linx.mcp_server_id", "linx.mcp_instance_id"} {
		if _, err := db.PortalSetting(ctx, key); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("%s survived delete: %v", key, err)
		}
	}
	if value, err := db.PortalSetting(ctx, "linx.other"); err != nil || value != "value" {
		t.Fatalf("unrelated setting changed: %q %v", value, err)
	}
}

func TestOpenReportsMigrationPrecheckFailures(t *testing.T) {
	t.Run("missing device table", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "gateway.db")
		v2 := createV2Database(t, path)
		mustExec(t, v2, `DROP TABLE portal_devices`)
		if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "precheck") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("device table without agent column", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "gateway.db")
		v2 := createV2Database(t, path)
		mustExec(t, v2,
			`DROP TABLE portal_devices`,
			`CREATE TABLE portal_devices (user_id TEXT, kind TEXT)`)
		if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "precheck") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestMigrationErrorPaths(t *testing.T) {
	ctx := context.Background()

	t.Run("cancelled version read", func(t *testing.T) {
		db := openPortalDB(t)
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		err := db.migrate(cancelled)
		if err == nil || !strings.Contains(err.Error(), "read schema version") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("cancelled transaction", func(t *testing.T) {
		db := openPortalDB(t)
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		err := db.applyMigration(cancelled, migration{version: 4})
		if err == nil || !strings.Contains(err.Error(), "begin schema v4") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("deferred constraint blocks the commit", func(t *testing.T) {
		db := openPortalDB(t)
		mustExec(t, db.sql,
			`CREATE TABLE parent (id INTEGER PRIMARY KEY)`,
			`CREATE TABLE child (pid INTEGER REFERENCES parent(id) DEFERRABLE INITIALLY DEFERRED)`)
		err := db.applyMigration(ctx, migration{
			version:    42,
			statements: []string{`INSERT INTO child (pid) VALUES (1)`},
		})
		if err == nil || !strings.Contains(err.Error(), "commit schema v42") {
			t.Fatalf("err = %v", err)
		}
		var count int
		if err := db.sql.QueryRow(`SELECT COUNT(*) FROM child`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("a failed commit persisted %d rows", count)
		}
		var version int
		if err := db.sql.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
			t.Fatal(err)
		}
		if version != schemaVersion {
			t.Fatalf("a failed commit changed the schema version to %d", version)
		}
	})
}
