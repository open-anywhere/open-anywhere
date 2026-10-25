package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sqlite "modernc.org/sqlite"
)

func TestCreatePortalUserValidationAndFailures(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()

	t.Run("id and username required", func(t *testing.T) {
		db := openPortalDB(t)
		for _, user := range []PortalUser{{}, {ID: "usr_1"}, {Username: "alice"}} {
			if err := db.CreatePortalUser(ctx, user); err == nil ||
				!strings.Contains(err.Error(), "id and username are required") {
				t.Fatalf("user=%+v err=%v", user, err)
			}
		}
	})

	t.Run("transaction cannot begin", func(t *testing.T) {
		db := openPortalDB(t)
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		err := db.CreatePortalUserWithDevice(ctx,
			PortalUser{ID: "usr_1", Username: "alice", CreatedAt: now, UpdatedAt: now},
			PortalDevice{ID: "dev_1", UserID: "usr_1", MAC: "02:00:00:00:00:01", CreatedAt: now, UpdatedAt: now})
		if err == nil || !strings.Contains(err.Error(), "begin create portal user") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("invalid account rolls back", func(t *testing.T) {
		db := openPortalDB(t)
		device := PortalDevice{ID: "dev_1", UserID: "usr_1", MAC: "02:00:00:00:00:01", CreatedAt: now, UpdatedAt: now}
		if err := db.CreatePortalUserWithDevice(ctx, PortalUser{ID: "usr_1"}, device); err == nil {
			t.Fatal("an account without a username was accepted")
		}
		var devices int
		if err := db.sql.QueryRow(`SELECT COUNT(*) FROM portal_devices`).Scan(&devices); err != nil {
			t.Fatal(err)
		}
		if devices != 0 {
			t.Fatalf("an invalid registration left %d devices", devices)
		}
	})

	t.Run("commit failure rolls back", func(t *testing.T) {
		db := openPortalDB(t)
		mustExec(t, db.sql,
			`DROP TABLE portal_devices`,
			`CREATE TABLE actors (id TEXT PRIMARY KEY)`,
			`CREATE TABLE portal_devices (
				id TEXT PRIMARY KEY,
				user_id TEXT NOT NULL,
				kind TEXT NOT NULL,
				mac TEXT NOT NULL UNIQUE,
				client_id TEXT NOT NULL,
				linx_agent_id TEXT NOT NULL DEFAULT '',
				agent_name TEXT NOT NULL DEFAULT '',
				name TEXT NOT NULL DEFAULT '',
				state TEXT NOT NULL,
				step TEXT NOT NULL DEFAULT '',
				error TEXT NOT NULL DEFAULT '',
				credential_path TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				registered_at TEXT,
				FOREIGN KEY (client_id) REFERENCES actors(id) DEFERRABLE INITIALLY DEFERRED)`)
		err := db.CreatePortalUserWithDevice(ctx,
			PortalUser{ID: "usr_1", Username: "alice", CreatedAt: now, UpdatedAt: now},
			PortalDevice{ID: "dev_1", UserID: "usr_1", MAC: "02:00:00:00:00:01", ClientID: "missing_actor", CreatedAt: now, UpdatedAt: now})
		if err == nil || !strings.Contains(err.Error(), "commit create portal user") {
			t.Fatalf("err = %v", err)
		}
		var users int
		if err := db.sql.QueryRow(`SELECT COUNT(*) FROM portal_users`).Scan(&users); err != nil {
			t.Fatal(err)
		}
		if users != 0 {
			t.Fatalf("a failed commit left %d accounts", users)
		}
	})
}

func insertRawPortalUser(t *testing.T, db *DB, id, created, updated string, lastLogin any) {
	t.Helper()
	if _, err := db.sql.Exec(`INSERT INTO portal_users
		(id, username, password_hash, role, status, created_at, updated_at, last_login_at)
		VALUES (?, ?, 'hash', 'user', 'active', ?, ?, ?)`, id, id, created, updated, lastLogin); err != nil {
		t.Fatalf("insert user %s: %v", id, err)
	}
}

func TestPortalUserScanErrors(t *testing.T) {
	ctx := context.Background()
	valid := time.Now().UTC().Format(time.RFC3339Nano)
	cases := []struct {
		name             string
		created, updated string
		lastLogin        any
		errorContains    string
	}{
		{name: "created at", created: "not-a-time", updated: valid, errorContains: "decode portal user time"},
		{name: "updated at", created: valid, updated: "not-a-time", errorContains: "decode portal user time"},
		{name: "last login", created: valid, updated: valid, lastLogin: "not-a-time", errorContains: "decode portal user login time"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			db := openPortalDB(t)
			insertRawPortalUser(t, db, "usr_1", testCase.created, testCase.updated, testCase.lastLogin)
			if _, err := db.PortalUserByID(ctx, "usr_1"); err == nil ||
				!strings.Contains(err.Error(), testCase.errorContains) {
				t.Fatalf("err = %v", err)
			}
			if _, err := db.PortalUserByUsername(ctx, "usr_1"); err == nil {
				t.Fatal("PortalUserByUsername accepted a malformed record")
			}
		})
	}
}

func TestUpdatePortalUser(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	login := now.Add(30 * time.Minute)

	t.Run("missing id", func(t *testing.T) {
		db := openPortalDB(t)
		if err := db.UpdatePortalUser(ctx, PortalUser{Username: "alice"}); err == nil ||
			!strings.Contains(err.Error(), "portal user id is required") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("defaults and fields", func(t *testing.T) {
		db := openPortalDB(t)
		if err := db.CreatePortalUser(ctx, PortalUser{ID: "usr_1", Username: "alice", PasswordHash: "old", CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		if err := db.UpdatePortalUser(ctx, PortalUser{
			ID: "usr_1", Username: "alice2", PasswordHash: "new",
			UpdatedAt: now.Add(time.Minute), LastLoginAt: &login,
		}); err != nil {
			t.Fatalf("UpdatePortalUser: %v", err)
		}
		stored, err := db.PortalUserByID(ctx, "usr_1")
		if err != nil {
			t.Fatal(err)
		}
		if stored.Username != "alice2" || stored.PasswordHash != "new" {
			t.Fatalf("stored = %+v", stored)
		}
		if stored.Role != PortalRoleUser || stored.Status != PortalUserActive {
			t.Fatalf("empty role and status did not fall back: %+v", stored)
		}
		if stored.LastLoginAt == nil || !stored.LastLoginAt.Equal(login) {
			t.Fatalf("LastLoginAt = %v", stored.LastLoginAt)
		}

		if err := db.UpdatePortalUser(ctx, PortalUser{
			ID: "usr_1", Username: "alice2", PasswordHash: "new",
			Role: PortalRoleAdmin, Status: PortalUserDisabled, UpdatedAt: now.Add(2 * time.Minute),
		}); err != nil {
			t.Fatal(err)
		}
		stored, err = db.PortalUserByID(ctx, "usr_1")
		if err != nil || stored.Role != PortalRoleAdmin || stored.Status != PortalUserDisabled || stored.LastLoginAt != nil {
			t.Fatalf("stored=%+v err=%v", stored, err)
		}
	})

	t.Run("closed database", func(t *testing.T) {
		db := openPortalDB(t)
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		if err := db.UpdatePortalUser(ctx, PortalUser{ID: "usr_1", Username: "alice"}); err == nil ||
			!strings.Contains(err.Error(), "update portal user") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestPortalUserStatusErrorPaths(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()

	t.Run("validation", func(t *testing.T) {
		db := openPortalDB(t)
		if err := db.SetPortalUserStatus(ctx, "", PortalUserDisabled, now); err == nil {
			t.Fatal("an empty user id was accepted")
		}
		if err := db.SetPortalUserStatus(ctx, "usr_1", "pending", now); err == nil ||
			!strings.Contains(err.Error(), "invalid portal user status") {
			t.Fatalf("err = %v", err)
		}
		if _, err := db.SetPortalUserStatusGuarded(ctx, "", PortalUserDisabled, now); err == nil {
			t.Fatal("an empty user id was accepted by the guarded call")
		}
		if _, err := db.SetPortalUserStatusGuarded(ctx, "usr_1", "pending", now); err == nil {
			t.Fatal("an invalid status was accepted by the guarded call")
		}
	})

	t.Run("closed database", func(t *testing.T) {
		db := openPortalDB(t)
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		if err := db.SetPortalUserStatus(ctx, "usr_1", PortalUserDisabled, now); err == nil {
			t.Fatal("SetPortalUserStatus succeeded on a closed database")
		}
		if _, err := db.SetPortalUserStatusGuarded(ctx, "usr_1", PortalUserDisabled, now); err == nil {
			t.Fatal("SetPortalUserStatusGuarded succeeded on a closed database")
		}
		if err := db.TouchPortalUserLogin(ctx, "usr_1", now); err == nil {
			t.Fatal("TouchPortalUserLogin succeeded on a closed database")
		}
	})
}

var (
	existenceProbeOnce sync.Once
	existenceProbeErr  error
	existenceProbeCall atomic.Int64
)

// registerExistenceProbe installs a SQL function that succeeds once and then
// fails. A view built on it lets a test fail the existence check that follows
// the guarded UPDATE, a query the package cannot otherwise make fail.
func registerExistenceProbe(t *testing.T) {
	t.Helper()
	existenceProbeOnce.Do(func() {
		existenceProbeErr = sqlite.RegisterScalarFunction("oa_test_existence_probe", 1,
			func(_ *sqlite.FunctionContext, _ []driver.Value) (driver.Value, error) {
				// The UPDATE scans the view once; the existence check that
				// follows is the second call, which must fail.
				if existenceProbeCall.Add(1) == 1 {
					return nil, nil
				}
				return nil, errors.New("probe failure")
			})
	})
	if existenceProbeErr != nil {
		t.Fatalf("register probe: %v", existenceProbeErr)
	}
	existenceProbeCall.Store(0)
}

func TestSetPortalUserStatusGuardedReportsExistenceCheckFailure(t *testing.T) {
	registerExistenceProbe(t)
	db, err := Open(filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	mustExec(t, db.sql,
		`DROP TABLE portal_devices`,
		`DROP TABLE portal_sessions`,
		`DROP TABLE portal_users`,
		`CREATE TABLE base_users (id TEXT PRIMARY KEY, username TEXT, password_hash TEXT,
			role TEXT, status TEXT, created_at TEXT, updated_at TEXT, last_login_at TEXT)`,
		`INSERT INTO base_users VALUES ('usr_1', 'alice', 'hash', 'user', 'active',
			'2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', NULL)`,
		`CREATE VIEW portal_users AS SELECT id, username, password_hash, role, status,
			created_at, updated_at, last_login_at FROM base_users
			WHERE oa_test_existence_probe(id) IS NOT NULL`,
		`CREATE TRIGGER portal_users_noop INSTEAD OF UPDATE ON portal_users BEGIN SELECT 1; END`)

	applied, err := db.SetPortalUserStatusGuarded(ctx, "usr_1", PortalUserDisabled, now)
	if err == nil || !strings.Contains(err.Error(), "check portal user") {
		t.Fatalf("applied=%v err=%v", applied, err)
	}
	if applied {
		t.Fatal("a failed existence check reported the change as applied")
	}
}

func TestPortalUserListingErrorPaths(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()

	t.Run("limit and offset fallbacks", func(t *testing.T) {
		db := openPortalDB(t)
		if err := db.CreatePortalUser(ctx, PortalUser{ID: "usr_1", Username: "alice", CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		for _, testCase := range []struct{ limit, offset int }{{0, 0}, {-1, 0}, {1000, 0}, {10, -5}} {
			users, err := db.PortalUsers(ctx, "", "", testCase.limit, testCase.offset)
			if err != nil || len(users) != 1 {
				t.Fatalf("limit=%d offset=%d users=%+v err=%v", testCase.limit, testCase.offset, users, err)
			}
		}
	})

	t.Run("closed database", func(t *testing.T) {
		db := openPortalDB(t)
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := db.PortalUsers(ctx, "", "", 10, 0); err == nil {
			t.Fatal("PortalUsers succeeded on a closed database")
		}
	})

	t.Run("scan failure", func(t *testing.T) {
		db := openPortalDB(t)
		mustExec(t, db.sql,
			`DROP TABLE portal_devices`,
			`DROP TABLE portal_sessions`,
			`DROP TABLE portal_users`,
			`CREATE TABLE portal_users (id TEXT, username TEXT, password_hash TEXT, role TEXT,
				status TEXT, created_at TEXT, updated_at TEXT, last_login_at TEXT)`,
			`INSERT INTO portal_users VALUES ('usr_1', 'alice', 'hash', 'user', 'active', NULL, NULL, NULL)`)
		if _, err := db.PortalUsers(ctx, "", "", 10, 0); err == nil ||
			!strings.Contains(err.Error(), "scan portal user") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("failing view", func(t *testing.T) {
		db := openPortalDB(t)
		mustExec(t, db.sql,
			`DROP TABLE portal_devices`,
			`DROP TABLE portal_sessions`,
			`DROP TABLE portal_users`,
			`CREATE TABLE base_users (id TEXT, username TEXT, password_hash TEXT, role TEXT,
				status TEXT, created_at TEXT, updated_at TEXT, last_login_at TEXT)`,
			`INSERT INTO base_users VALUES ('usr_1', 'alice', 'hash', 'user', 'active',
				'2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', NULL)`,
			`CREATE VIEW portal_users AS
				SELECT id, username, password_hash, role, status, created_at, updated_at, last_login_at FROM base_users
				UNION ALL
				SELECT 'usr_2', 'bob', 'hash', 'user', 'active', '2026-01-01T00:00:00Z',
					abs(-9223372036854775808), NULL`)
		if _, err := db.PortalUsers(ctx, "", "", 10, 0); err == nil ||
			!strings.Contains(err.Error(), "list portal users") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestPortalUserCountErrors(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()

	t.Run("counts", func(t *testing.T) {
		db := openPortalDB(t)
		for _, user := range []PortalUser{
			{ID: "usr_1", Username: "alice", Role: PortalRoleAdmin, CreatedAt: now, UpdatedAt: now},
			{ID: "usr_2", Username: "alicia", Status: PortalUserDisabled, CreatedAt: now, UpdatedAt: now},
		} {
			if err := db.CreatePortalUser(ctx, user); err != nil {
				t.Fatal(err)
			}
		}
		total, err := db.CountPortalUsers(ctx, "", "")
		if err != nil || total != 2 {
			t.Fatalf("total=%d err=%v", total, err)
		}
		matched, err := db.CountPortalUsers(ctx, "ali", "")
		if err != nil || matched != 2 {
			t.Fatalf("matched=%d err=%v", matched, err)
		}
		disabled, err := db.CountPortalUsers(ctx, "", PortalUserDisabled)
		if err != nil || disabled != 1 {
			t.Fatalf("disabled=%d err=%v", disabled, err)
		}
	})

	t.Run("query failure", func(t *testing.T) {
		db := openPortalDB(t)
		mustExec(t, db.sql, `DROP TABLE portal_users`)
		if _, err := db.CountPortalUsers(ctx, "", ""); err == nil {
			t.Fatal("CountPortalUsers succeeded without a table")
		}
		if _, err := db.CountPortalUsersByStatus(ctx); err == nil {
			t.Fatal("CountPortalUsersByStatus succeeded without a table")
		}
		if _, err := db.CountActiveAdmins(ctx); err == nil {
			t.Fatal("CountActiveAdmins succeeded without a table")
		}
	})

	t.Run("scan failure", func(t *testing.T) {
		db := openPortalDB(t)
		mustExec(t, db.sql,
			`DROP TABLE portal_devices`,
			`DROP TABLE portal_sessions`,
			`DROP TABLE portal_users`,
			`CREATE TABLE portal_users (id TEXT, username TEXT, password_hash TEXT, role TEXT,
				status TEXT, created_at TEXT, updated_at TEXT, last_login_at TEXT)`,
			`INSERT INTO portal_users VALUES ('usr_1', 'alice', 'hash', 'user', NULL, '', '', NULL)`)
		if _, err := db.CountPortalUsersByStatus(ctx); err == nil ||
			!strings.Contains(err.Error(), "scan portal user count") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("failing view", func(t *testing.T) {
		db := openPortalDB(t)
		mustExec(t, db.sql,
			`DROP TABLE portal_devices`,
			`DROP TABLE portal_sessions`,
			`DROP TABLE portal_users`,
			`CREATE TABLE base_users (id TEXT, username TEXT, password_hash TEXT, role TEXT,
				status TEXT, created_at TEXT, updated_at TEXT, last_login_at TEXT)`,
			`INSERT INTO base_users VALUES ('usr_1', 'alice', 'hash', 'user', 'active', '', '', NULL)`,
			`CREATE VIEW portal_users AS
				SELECT id, username, password_hash, role, status, created_at, updated_at, last_login_at FROM base_users
				UNION ALL
				SELECT 'usr_2', 'bob', 'hash', 'user', abs(-9223372036854775808), '', '', NULL`)
		if _, err := db.CountPortalUsersByStatus(ctx); err == nil ||
			!strings.Contains(err.Error(), "count portal users") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestDeletePortalSessionsByUser(t *testing.T) {
	db := openPortalDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for _, user := range []PortalUser{
		{ID: "usr_1", Username: "alice", CreatedAt: now, UpdatedAt: now},
		{ID: "usr_2", Username: "bob", CreatedAt: now, UpdatedAt: now},
	} {
		if err := db.CreatePortalUser(ctx, user); err != nil {
			t.Fatal(err)
		}
	}
	for _, session := range []PortalSession{
		{TokenHash: "alice_1", UserID: "usr_1", CreatedAt: now, ExpiresAt: now.Add(time.Hour), LastSeenAt: now},
		{TokenHash: "alice_2", UserID: "usr_1", CreatedAt: now, ExpiresAt: now.Add(time.Hour), LastSeenAt: now},
		{TokenHash: "bob_1", UserID: "usr_2", CreatedAt: now, ExpiresAt: now.Add(time.Hour), LastSeenAt: now},
	} {
		if err := db.CreatePortalSession(ctx, session); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.DeletePortalSessionsByUser(ctx, "usr_1"); err != nil {
		t.Fatalf("DeletePortalSessionsByUser: %v", err)
	}
	for _, hash := range []string{"alice_1", "alice_2"} {
		if _, err := db.PortalSessionByTokenHash(ctx, hash); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("%s survived: %v", hash, err)
		}
	}
	if _, err := db.PortalSessionByTokenHash(ctx, "bob_1"); err != nil {
		t.Fatalf("another user's session was revoked: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.DeletePortalSessionsByUser(ctx, "usr_2"); err == nil {
		t.Fatal("DeletePortalSessionsByUser succeeded on a closed database")
	}
}

func TestPortalSessionErrorPaths(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()

	t.Run("validation", func(t *testing.T) {
		db := openPortalDB(t)
		for _, session := range []PortalSession{{}, {TokenHash: "hash"}, {UserID: "usr_1"}} {
			if err := db.CreatePortalSession(ctx, session); err == nil ||
				!strings.Contains(err.Error(), "token and user are required") {
				t.Fatalf("session=%+v err=%v", session, err)
			}
		}
	})

	t.Run("duplicate token", func(t *testing.T) {
		db := openPortalDB(t)
		if err := db.CreatePortalUser(ctx, PortalUser{ID: "usr_1", Username: "alice", CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		session := PortalSession{TokenHash: "hash", UserID: "usr_1", CreatedAt: now, ExpiresAt: now.Add(time.Hour), LastSeenAt: now}
		if err := db.CreatePortalSession(ctx, session); err != nil {
			t.Fatal(err)
		}
		if err := db.CreatePortalSession(ctx, session); err == nil ||
			!strings.Contains(err.Error(), "create portal session") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("undecodable timestamps", func(t *testing.T) {
		for _, column := range []string{"created_at", "expires_at", "last_seen_at"} {
			t.Run(column, func(t *testing.T) {
				db := openPortalDB(t)
				if err := db.CreatePortalUser(ctx, PortalUser{ID: "usr_1", Username: "alice", CreatedAt: now, UpdatedAt: now}); err != nil {
					t.Fatal(err)
				}
				created, expires, seen := now.Format(time.RFC3339Nano), now.Add(time.Hour).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)
				switch column {
				case "created_at":
					created = "not-a-time"
				case "expires_at":
					expires = "not-a-time"
				case "last_seen_at":
					seen = "not-a-time"
				}
				if _, err := db.sql.Exec(`INSERT INTO portal_sessions
					(token_hash, user_id, created_at, expires_at, last_seen_at, user_agent)
					VALUES ('hash', 'usr_1', ?, ?, ?, '')`, created, expires, seen); err != nil {
					t.Fatal(err)
				}
				if _, err := db.PortalSessionByTokenHash(ctx, "hash"); err == nil ||
					!strings.Contains(err.Error(), "decode portal session time") {
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
		if err := db.CreatePortalSession(ctx, PortalSession{TokenHash: "hash", UserID: "usr_1"}); err == nil {
			t.Fatal("CreatePortalSession succeeded on a closed database")
		}
		if err := db.TouchPortalSession(ctx, "hash", now); err == nil {
			t.Fatal("TouchPortalSession succeeded on a closed database")
		}
		if err := db.DeletePortalSession(ctx, "hash"); err == nil {
			t.Fatal("DeletePortalSession succeeded on a closed database")
		}
		if _, err := db.DeleteExpiredPortalSessions(ctx, now); err == nil {
			t.Fatal("DeleteExpiredPortalSessions succeeded on a closed database")
		}
	})
}

func TestPortalDeviceValidationAndScanErrors(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()

	t.Run("validation", func(t *testing.T) {
		db := openPortalDB(t)
		for _, device := range []PortalDevice{
			{},
			{ID: "dev_1"},
			{ID: "dev_1", UserID: "usr_1"},
			{ID: "dev_1", MAC: "02:00:00:00:00:01"},
		} {
			if err := db.CreatePortalDevice(ctx, device); err == nil ||
				!strings.Contains(err.Error(), "id, user and mac are required") {
				t.Fatalf("device=%+v err=%v", device, err)
			}
		}
	})

	t.Run("undecodable timestamps", func(t *testing.T) {
		cases := []struct {
			name                     string
			created, updated, regged any
		}{
			{name: "created at", created: "not-a-time", updated: now.Format(time.RFC3339Nano), regged: nil},
			{name: "updated at", created: now.Format(time.RFC3339Nano), updated: "not-a-time", regged: nil},
			{name: "registered at", created: now.Format(time.RFC3339Nano), updated: now.Format(time.RFC3339Nano), regged: "not-a-time"},
		}
		for _, testCase := range cases {
			t.Run(testCase.name, func(t *testing.T) {
				db := openPortalDB(t)
				if err := db.CreatePortalUser(ctx, PortalUser{ID: "usr_1", Username: "alice", CreatedAt: now, UpdatedAt: now}); err != nil {
					t.Fatal(err)
				}
				if _, err := db.sql.Exec(`INSERT INTO portal_devices
					(id, user_id, kind, mac, client_id, linx_agent_id, agent_name, state, step, error,
					 credential_path, created_at, updated_at, registered_at)
					VALUES ('dev_1', 'usr_1', 'virtual', 'mac', 'client', '', '', 'provisioning', '', '', '',
					 ?, ?, ?)`, testCase.created, testCase.updated, testCase.regged); err != nil {
					t.Fatal(err)
				}
				if _, err := db.PortalDeviceByID(ctx, "dev_1"); err == nil ||
					!strings.Contains(err.Error(), "decode portal device") {
					t.Fatalf("err = %v", err)
				}
			})
		}
	})
}

func TestPortalDeviceListingErrorPaths(t *testing.T) {
	ctx := context.Background()

	dropDevices := []string{`DROP TABLE portal_devices`}
	nullDeviceView := []string{
		`DROP TABLE portal_devices`,
		`CREATE TABLE base_devices (id TEXT, user_id TEXT, kind TEXT, mac TEXT, client_id TEXT,
			linx_agent_id TEXT, agent_name TEXT, state TEXT, step TEXT, error TEXT,
			credential_path TEXT, created_at TEXT, updated_at TEXT, registered_at TEXT)`,
		`INSERT INTO base_devices VALUES ('dev_1', 'usr_1', 'virtual', 'mac', 'client', '', '',
			'provisioning', '', '', '', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', NULL)`,
		`CREATE VIEW portal_devices AS
			SELECT id, user_id, kind, mac, client_id, linx_agent_id, agent_name, state, step, error,
				credential_path, created_at, updated_at, registered_at FROM base_devices
			UNION ALL
			SELECT 'dev_2', 'usr_1', 'virtual', 'mac2', 'client', '', '', 'provisioning', '', '', NULL,
				'2026-01-01T00:00:00Z', abs(-9223372036854775808), NULL`,
	}
	deviceListings := []struct {
		name string
		call func(*DB) error
	}{
		{name: "by user", call: func(db *DB) error { _, err := db.PortalDevicesByUser(ctx, "usr_1"); return err }},
		{name: "by state", call: func(db *DB) error { _, err := db.PortalDevicesByState(ctx, "provisioning"); return err }},
		{name: "all", call: func(db *DB) error { _, err := db.PortalDevicesAll(ctx, "", "", 10); return err }},
	}
	for _, listing := range deviceListings {
		t.Run(listing.name+" query failure", func(t *testing.T) {
			db := openPortalDB(t)
			mustExec(t, db.sql, dropDevices...)
			if err := listing.call(db); err == nil || !strings.Contains(err.Error(), "list portal devices") {
				t.Fatalf("err = %v", err)
			}
		})
		t.Run(listing.name+" scan failure", func(t *testing.T) {
			db := openPortalDB(t)
			mustExec(t, db.sql, `DROP TABLE portal_devices`,
				`CREATE TABLE portal_devices (id TEXT, user_id TEXT, kind TEXT, mac TEXT, client_id TEXT,
					linx_agent_id TEXT, agent_name TEXT, name TEXT, state TEXT, step TEXT, error TEXT,
					credential_path TEXT, created_at TEXT, updated_at TEXT, registered_at TEXT)`,
				`INSERT INTO portal_devices VALUES ('dev_1', 'usr_1', 'virtual', 'mac', 'client', '', '', '',
					'provisioning', '', '', NULL, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', NULL)`)
			if err := listing.call(db); err == nil || !strings.Contains(err.Error(), "scan portal device") {
				t.Fatalf("err = %v", err)
			}
		})
		t.Run(listing.name+" row iteration failure", func(t *testing.T) {
			db := openPortalDB(t)
			mustExec(t, db.sql, nullDeviceView...)
			if err := listing.call(db); err == nil || !strings.Contains(err.Error(), "list portal devices") {
				t.Fatalf("err = %v", err)
			}
		})
	}

	t.Run("closed database", func(t *testing.T) {
		db := openPortalDB(t)
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		for _, listing := range deviceListings {
			if err := listing.call(db); err == nil {
				t.Fatalf("%s succeeded on a closed database", listing.name)
			}
		}
	})
}

func TestPortalDevicesAllFilters(t *testing.T) {
	db := openPortalDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 25, 16, 0, 0, 0, time.UTC)
	for _, user := range []PortalUser{
		{ID: "usr_1", Username: "alice", CreatedAt: now, UpdatedAt: now},
		{ID: "usr_2", Username: "bob", CreatedAt: now, UpdatedAt: now},
	} {
		if err := db.CreatePortalUser(ctx, user); err != nil {
			t.Fatal(err)
		}
	}
	devices := []PortalDevice{
		{ID: "dev_1", UserID: "usr_1", Kind: PortalDeviceVirtual, MAC: "02:00:00:00:00:01",
			LinxAgentID: "agent_1", AgentName: "Alice TV", State: PortalDeviceRegistered,
			CreatedAt: now, UpdatedAt: now},
		{ID: "dev_2", UserID: "usr_2", Kind: PortalDevicePhysical, MAC: "02:00:00:00:00:02",
			LinxAgentID: "agent_2", AgentName: "Bob speaker", State: PortalDeviceProvisioning,
			CreatedAt: now.Add(time.Minute), UpdatedAt: now.Add(time.Minute)},
	}
	for _, device := range devices {
		if err := db.CreatePortalDevice(ctx, device); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name        string
		state       string
		query       string
		limit       int
		wantDevices []string
	}{
		{name: "all with default limit", wantDevices: []string{"dev_2", "dev_1"}},
		{name: "limit above maximum falls back", limit: 1000, wantDevices: []string{"dev_2", "dev_1"}},
		{name: "negative limit falls back", limit: -1, wantDevices: []string{"dev_2", "dev_1"}},
		{name: "state filter", state: PortalDeviceRegistered, wantDevices: []string{"dev_1"}},
		{name: "mac filter", query: "00:02", wantDevices: []string{"dev_2"}},
		{name: "agent name filter", query: "Alice", wantDevices: []string{"dev_1"}},
		{name: "agent id filter", query: "agent_2", wantDevices: []string{"dev_2"}},
		{name: "user filter", query: "usr_1", wantDevices: []string{"dev_1"}},
		{name: "limit", limit: 1, wantDevices: []string{"dev_2"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			found, err := db.PortalDevicesAll(ctx, testCase.state, testCase.query, testCase.limit)
			if err != nil {
				t.Fatalf("PortalDevicesAll: %v", err)
			}
			var ids []string
			for _, device := range found {
				ids = append(ids, device.ID)
			}
			if fmt.Sprint(ids) != fmt.Sprint(testCase.wantDevices) {
				t.Fatalf("devices = %v, want %v", ids, testCase.wantDevices)
			}
		})
	}
}

func TestPortalDeviceIdentityAndCounts(t *testing.T) {
	db := openPortalDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := db.CreatePortalUser(ctx, PortalUser{ID: "usr_1", Username: "alice", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	device := PortalDevice{
		ID: "dev_1", UserID: "usr_1", Kind: PortalDeviceVirtual, MAC: "02:00:00:00:00:01",
		ClientID: "client", LinxAgentID: "agent_1", State: PortalDeviceProvisioning,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.CreatePortalDevice(ctx, device); err != nil {
		t.Fatal(err)
	}
	loaded, err := db.PortalDeviceByIdentity(ctx, device.MAC, "agent_1")
	if err != nil || loaded.ID != device.ID {
		t.Fatalf("device=%+v err=%v", loaded, err)
	}
	if _, err := db.PortalDeviceByIdentity(ctx, device.MAC, "agent_other"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("a wrong agent id resolved a device: %v", err)
	}

	counts, err := db.CountPortalDevicesByState(ctx)
	if err != nil || counts[PortalDeviceProvisioning] != 1 {
		t.Fatalf("counts=%+v err=%v", counts, err)
	}

	t.Run("query failure", func(t *testing.T) {
		mustExec(t, db.sql, `DROP TABLE portal_devices`)
		if _, err := db.CountPortalDevicesByState(ctx); err == nil ||
			!strings.Contains(err.Error(), "count portal devices") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestCountPortalDevicesByStateErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("scan failure", func(t *testing.T) {
		db := openPortalDB(t)
		mustExec(t, db.sql,
			`DROP TABLE portal_devices`,
			`CREATE TABLE portal_devices (id TEXT, user_id TEXT, kind TEXT, mac TEXT, client_id TEXT,
				linx_agent_id TEXT, agent_name TEXT, name TEXT, state TEXT, step TEXT, error TEXT,
				credential_path TEXT, created_at TEXT, updated_at TEXT, registered_at TEXT)`,
			`INSERT INTO portal_devices VALUES ('dev_1', 'usr_1', 'virtual', 'mac', 'client', '', '', '',
				NULL, '', '', '', '', '', NULL)`)
		if _, err := db.CountPortalDevicesByState(ctx); err == nil ||
			!strings.Contains(err.Error(), "scan portal device count") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("failing view", func(t *testing.T) {
		db := openPortalDB(t)
		mustExec(t, db.sql,
			`DROP TABLE portal_devices`,
			`CREATE TABLE base_devices (id TEXT, user_id TEXT, kind TEXT, mac TEXT, client_id TEXT,
				linx_agent_id TEXT, agent_name TEXT, state TEXT, step TEXT, error TEXT,
				credential_path TEXT, created_at TEXT, updated_at TEXT, registered_at TEXT)`,
			`INSERT INTO base_devices VALUES ('dev_1', 'usr_1', 'virtual', 'mac', 'client', '', '',
				'provisioning', '', '', '', '', '', NULL)`,
			`CREATE VIEW portal_devices AS
				SELECT id, user_id, kind, mac, client_id, linx_agent_id, agent_name, state, step, error,
					credential_path, created_at, updated_at, registered_at FROM base_devices
				UNION ALL
				SELECT 'dev_2', 'usr_1', 'virtual', 'mac2', 'client', '', '',
					abs(-9223372036854775808), '', '', '', '', '', NULL`)
		if _, err := db.CountPortalDevicesByState(ctx); err == nil ||
			!strings.Contains(err.Error(), "count portal devices") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestPortalDeviceUpdatesAndSettings(t *testing.T) {
	ctx := context.Background()

	t.Run("update validation", func(t *testing.T) {
		db := openPortalDB(t)
		if err := db.UpdatePortalDevice(ctx, PortalDevice{}); err == nil ||
			!strings.Contains(err.Error(), "portal device id is required") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("closed database", func(t *testing.T) {
		db := openPortalDB(t)
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		if err := db.UpdatePortalDevice(ctx, PortalDevice{ID: "dev_1"}); err == nil {
			t.Fatal("UpdatePortalDevice succeeded on a closed database")
		}
		if err := db.SetPortalSetting(ctx, "key", "value"); err == nil {
			t.Fatal("SetPortalSetting succeeded on a closed database")
		}
	})
}

func TestTimeHelpers(t *testing.T) {
	if got := formatTime(time.Time{}); got != "" {
		t.Fatalf("formatTime(zero) = %q", got)
	}
	moment := time.Date(2026, 10, 25, 17, 30, 0, 123456789, time.UTC)
	if got, want := formatTime(moment), "2026-10-25T17:30:00.123456789Z"; got != want {
		t.Fatalf("formatTime = %q, want %q", got, want)
	}
	if parsed, err := parseTime(""); err != nil || !parsed.IsZero() {
		t.Fatalf("parseTime(empty) = %v, %v", parsed, err)
	}
	if parsed, err := parseTime(formatTime(moment)); err != nil || !parsed.Equal(moment) {
		t.Fatalf("parseTime(round trip) = %v, %v", parsed, err)
	}
	if _, err := parseTime("not-a-time"); err == nil {
		t.Fatal("parseTime accepted an invalid timestamp")
	}
}
