package portal

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

const poisonedAdminID = "usr_poison_admin"

func TestAdminOverviewAndStatusReportPoisonedUserRows(t *testing.T) {
	env := newPortalHTTPEnv(t)
	env.login(t)
	createPortalTestUser(t, env, poisonedAdminID, "poison", "password123", store.PortalRoleAdmin)
	execRaw(t, env.dbPath, "ALTER TABLE portal_users RENAME TO portal_users_base")
	// Reading this administrator's role raises; every other row reads normally.
	execRaw(t, env.dbPath, `CREATE VIEW portal_users AS
		SELECT id, username, password_hash,
			CASE WHEN id = '`+poisonedAdminID+`' THEN json_extract('poison', '$') ELSE role END AS role,
			status, created_at, updated_at, last_login_at
		FROM portal_users_base`)

	response, payload := env.do(t, http.MethodGet, "/api/admin/overview", "", nil)
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("overview status=%d body=%s", response.StatusCode, payload)
	}
	response, payload = env.do(t, http.MethodPost, "/api/admin/users/"+poisonedAdminID+"/status",
		`{"status":"disabled"}`, map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status change status=%d body=%s", response.StatusCode, payload)
	}
}

func TestAdminSetUserStatusReportsAGuardedUpdateFailure(t *testing.T) {
	env := newPortalHTTPEnv(t)
	env.login(t)
	bob := createPortalTestUser(t, env, "usr_update", "updater", "password123", store.PortalRoleUser)
	execRaw(t, env.dbPath, `CREATE TRIGGER gate_block_status_update BEFORE UPDATE ON portal_users
		BEGIN SELECT RAISE(ABORT, 'blocked'); END`)
	response, payload := env.do(t, http.MethodPost, "/api/admin/users/"+bob.ID+"/status",
		`{"status":"disabled"}`, map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status change status=%d body=%s", response.StatusCode, payload)
	}
}

func TestAdminGetUserReportsADeviceLookupFailure(t *testing.T) {
	env := newPortalHTTPEnv(t)
	env.login(t)
	admin := loadPortalTestUser(t, env, testPortalUsername)
	execRaw(t, env.dbPath, "ALTER TABLE portal_devices RENAME TO portal_devices_hidden")
	response, payload := env.do(t, http.MethodGet, "/api/admin/users/"+admin.ID, "", nil)
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("user detail status=%d body=%s", response.StatusCode, payload)
	}
}

func TestAdminUpdateSettingsReportsAPlatformSnapshotFailure(t *testing.T) {
	env := newPlatformHTTPEnv(t)
	env.login(t)
	env.api.apps = portalTestService(t)
	execRaw(t, env.dbPath, "ALTER TABLE platform_settings RENAME TO platform_settings_hidden")
	response, payload := env.do(t, http.MethodPut, "/api/admin/settings", `{"services":{}}`,
		map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("settings update status=%d body=%s", response.StatusCode, payload)
	}
}

func TestAdminUsernameLookup(t *testing.T) {
	env := newPortalHTTPEnv(t)
	admin := loadPortalTestUser(t, env, testPortalUsername)
	if name := env.api.username(context.Background(), map[string]string{admin.ID: "cached"}, admin.ID); name != "cached" {
		t.Fatalf("cache hit = %q", name)
	}
	if name := env.api.username(context.Background(), map[string]string{}, ""); name != "" {
		t.Fatalf("empty user = %q", name)
	}
	if name := env.api.username(context.Background(), map[string]string{}, "usr_missing"); name != "" {
		t.Fatalf("missing user = %q", name)
	}
}

func TestAdminGetUserSkipsDevicesWithoutAnAgentAndUnconfiguredApps(t *testing.T) {
	env := newPortalHTTPEnv(t)
	env.login(t)
	admin := loadPortalTestUser(t, env, testPortalUsername)
	createPortalTestDevice(t, env, "dev_no_agent", admin.ID, "AA:BB:CC:00:03:01", "", store.PortalDeviceProvisioning)
	response, payload := env.do(t, http.MethodGet, "/api/admin/users/"+admin.ID, "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("user detail status=%d body=%s", response.StatusCode, payload)
	}
}

func TestAdminSetUserStatusReportsAUserThatVanishedDuringTheUpdate(t *testing.T) {
	env := newPortalHTTPEnv(t)
	env.login(t)
	bob := createPortalTestUser(t, env, "usr_vanished", "vanished", "password123", store.PortalRoleUser)
	execRaw(t, env.dbPath, "ALTER TABLE portal_users RENAME TO portal_users_base")
	execRaw(t, env.dbPath, "CREATE VIEW portal_users AS SELECT * FROM portal_users_base")
	// The guarded update succeeds against the view but removes the row instead of
	// changing it, so the follow-up existence check finds nothing.
	execRaw(t, env.dbPath, `CREATE TRIGGER portal_users_vanish INSTEAD OF UPDATE ON portal_users
		BEGIN
			DELETE FROM portal_users_base WHERE id = NEW.id;
		END`)
	response, payload := env.do(t, http.MethodPost, "/api/admin/users/"+bob.ID+"/status",
		`{"status":"disabled"}`, map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusNotFound || errorCode(t, payload) != "USER_NOT_FOUND" {
		t.Fatalf("vanished user status=%d body=%s", response.StatusCode, payload)
	}
}

func TestAdminListUsersReportsACountFailureAfterThePageLoaded(t *testing.T) {
	env := newPortalHTTPEnv(t)
	env.login(t)
	createPortalTestUser(t, env, "usr_page", "needle", "password123", store.PortalRoleUser)
	old := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	execRaw(t, env.dbPath, "ALTER TABLE portal_users RENAME TO portal_users_base")
	// An older account whose username raises while it is evaluated.
	execRaw(t, env.dbPath, `INSERT INTO portal_users_base
		(id, username, password_hash, role, status, created_at, updated_at, last_login_at)
		VALUES ('usr_poison_count', 'poison', 'x', 'user', 'active', '`+old+`', '`+old+`', NULL)`)
	execRaw(t, env.dbPath, `CREATE VIEW portal_users AS
		SELECT id,
			CASE WHEN id = 'usr_poison_count' THEN json_extract('poison', '$') ELSE username END AS username,
			password_hash, role, status, created_at, updated_at, last_login_at
		FROM portal_users_base`)
	// The index lets the page query stop after its first match, before it
	// reaches the row that raises, while the count still evaluates every match.
	execRaw(t, env.dbPath, "CREATE INDEX gate_users_page ON portal_users_base(created_at DESC, id)")
	response, payload := env.do(t, http.MethodGet, "/api/admin/users?query=needle&status=active&limit=1", "", nil)
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("user list status=%d body=%s", response.StatusCode, payload)
	}
}

func TestAdminUpdateSettingsReportsAMaskedSnapshotFailure(t *testing.T) {
	env := newPlatformHTTPEnv(t)
	env.login(t)
	// The settings write succeeds, then the row the trigger added makes the
	// masked snapshot fail.
	execRaw(t, env.dbPath, `CREATE TRIGGER gate_poison_settings AFTER INSERT ON platform_settings
		WHEN NEW.key <> 'agentapi.agent_version'
		BEGIN
			INSERT INTO platform_settings (key, value, is_secret, updated_at, updated_by)
			VALUES ('agentapi.agent_version', 'not-a-number', 0, '2026-01-01T00:00:00Z', '');
		END`)
	response, payload := env.do(t, http.MethodPut, "/api/admin/settings", `{"registration":{"open":true}}`,
		map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("settings update status=%d body=%s", response.StatusCode, payload)
	}
}
