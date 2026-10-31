package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// createV2Database builds a database exactly as schema version 2 left it, so
// migration tests exercise a real upgrade instead of a fresh install.
func createV2Database(t *testing.T, path string) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	for _, statement := range append(append([]string{}, schemaV1...), schemaV2...) {
		if _, err := database.Exec(statement); err != nil {
			t.Fatalf("apply v2 schema: %v", err)
		}
	}
	if _, err := database.Exec("PRAGMA user_version = 2"); err != nil {
		t.Fatalf("set version: %v", err)
	}
	return database
}

func TestMigrationUpgradesV2AndKeepsData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway.db")
	v2 := createV2Database(t, path)
	now := time.Date(2026, 10, 25, 8, 0, 0, 0, time.UTC)
	if _, err := v2.Exec(`INSERT INTO apps (id, owner_device_id, owner_session_id, state, revision, data, updated_at)
		VALUES ('app_1', 'dev_1', 'ses_1', 'idle', 1, '{"id":"app_1","name":"kept"}', ?)`, now.Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert app: %v", err)
	}
	if _, err := v2.Exec(`INSERT INTO portal_users (id, username, password_hash, created_at, updated_at)
		VALUES ('usr_1', 'owner', 'hash', ?, ?)`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := v2.Exec(`INSERT INTO portal_devices
		(id, user_id, kind, mac, client_id, linx_agent_id, agent_name, state, step, error, credential_path, created_at, updated_at)
		VALUES ('dev_1', 'usr_1', 'virtual', '02:00:00:00:00:01', 'client', 'agent_1', 'Kept', 'registered', 'done', '', 'devices/dev_1.json', ?, ?)`,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert device: %v", err)
	}

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	var version int
	if err := db.sql.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != schemaVersion {
		t.Fatalf("version = %d", version)
	}

	user, err := db.PortalUserByID(ctx, "usr_1")
	if err != nil {
		t.Fatalf("PortalUserByID: %v", err)
	}
	if user.Role != PortalRoleUser || user.Status != PortalUserActive || user.LastLoginAt != nil {
		t.Fatalf("migrated user = %+v", user)
	}
	device, err := db.PortalDeviceByID(ctx, "dev_1")
	if err != nil {
		t.Fatalf("PortalDeviceByID: %v", err)
	}
	if device.LinxAgentID != "agent_1" || device.State != PortalDeviceRegistered || device.CredentialPath != "devices/dev_1.json" {
		t.Fatalf("migrated device = %+v", device)
	}
	apps, err := db.ListApps()
	if err != nil || len(apps) != 1 || apps[0].ID != "app_1" {
		t.Fatalf("apps=%+v err=%v", apps, err)
	}
	if _, err := db.ServiceBindingByUser(ctx, "usr_1"); err != sql.ErrNoRows {
		t.Fatalf("a migrated user should have no binding yet: %v", err)
	}

	// Starting again must be idempotent.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer again.Close()
}

func TestMigrationRejectsDuplicateVirtualDevices(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway.db")
	v2 := createV2Database(t, path)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := v2.Exec(`INSERT INTO portal_users (id, username, password_hash, created_at, updated_at)
		VALUES ('usr_1', 'owner', 'hash', ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	for index, id := range []string{"dev_1", "dev_2"} {
		if _, err := v2.Exec(`INSERT INTO portal_devices
			(id, user_id, kind, mac, client_id, linx_agent_id, agent_name, state, step, error, credential_path, created_at, updated_at)
			VALUES (?, 'usr_1', 'virtual', ?, 'client', '', '', 'connected', 'done', '', '', ?, ?)`,
			id, "02:00:00:00:00:0"+string(rune('1'+index)), now, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "more than one virtual device") {
		t.Fatalf("duplicate virtual devices were accepted: %v", err)
	}
}

func TestMigrationRejectsDuplicateAgents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway.db")
	v2 := createV2Database(t, path)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := v2.Exec(`INSERT INTO portal_users (id, username, password_hash, created_at, updated_at)
		VALUES ('usr_1', 'owner', 'hash', ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	for index, id := range []string{"dev_1", "dev_2"} {
		if _, err := v2.Exec(`INSERT INTO portal_devices
			(id, user_id, kind, mac, client_id, linx_agent_id, agent_name, state, step, error, credential_path, created_at, updated_at)
			VALUES (?, 'usr_1', 'physical', ?, 'client', 'agent_1', '', 'registered', 'done', '', '', ?, ?)`,
			id, "02:00:00:00:00:1"+string(rune('1'+index)), now, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "more than one device") {
		t.Fatalf("duplicate agents were accepted: %v", err)
	}
}

func TestCreatePortalUserWithDeviceIsAtomic(t *testing.T) {
	db := openPortalDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	user := PortalUser{ID: "usr_1", Username: "alice", PasswordHash: "hash", Role: PortalRoleUser, CreatedAt: now, UpdatedAt: now}
	device := PortalDevice{ID: "dev_1", UserID: user.ID, Kind: PortalDeviceVirtual, MAC: "02:00:00:00:00:01",
		ClientID: "client", State: PortalDeviceProvisioning, Step: "create_agent", CreatedAt: now, UpdatedAt: now}
	if err := db.CreatePortalUserWithDevice(ctx, user, device); err != nil {
		t.Fatalf("CreatePortalUserWithDevice: %v", err)
	}
	loaded, err := db.PortalUserByUsername(ctx, "alice")
	if err != nil || loaded.Role != PortalRoleUser || loaded.Status != PortalUserActive {
		t.Fatalf("user=%+v err=%v", loaded, err)
	}
	if _, err := db.PortalDeviceByID(ctx, device.ID); err != nil {
		t.Fatalf("device: %v", err)
	}

	// A duplicate MAC fails the device insert, which must also roll back the
	// account row.
	second := user
	second.ID = "usr_2"
	second.Username = "bob"
	device.ID = "dev_2"
	if err := db.CreatePortalUserWithDevice(ctx, second, device); err == nil {
		t.Fatal("a duplicate device was accepted")
	}
	if _, err := db.PortalUserByUsername(ctx, "bob"); err != sql.ErrNoRows {
		t.Fatalf("the failed registration left an account behind: %v", err)
	}
}

func TestPortalUserRoleStatusAndListing(t *testing.T) {
	db := openPortalDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	for _, user := range []PortalUser{
		{ID: "usr_1", Username: "admin", Role: PortalRoleAdmin, CreatedAt: now, UpdatedAt: now},
		{ID: "usr_2", Username: "alice", Role: PortalRoleUser, CreatedAt: now.Add(time.Minute), UpdatedAt: now},
		{ID: "usr_3", Username: "bob", Role: PortalRoleUser, Status: PortalUserDisabled, CreatedAt: now.Add(2 * time.Minute), UpdatedAt: now},
	} {
		if err := db.CreatePortalUser(ctx, user); err != nil {
			t.Fatal(err)
		}
	}
	admins, err := db.CountActiveAdmins(ctx)
	if err != nil || admins != 1 {
		t.Fatalf("admins=%d err=%v", admins, err)
	}
	counts, err := db.CountPortalUsersByStatus(ctx)
	if err != nil || counts[PortalUserActive] != 2 || counts[PortalUserDisabled] != 1 {
		t.Fatalf("counts=%+v err=%v", counts, err)
	}
	users, err := db.PortalUsers(ctx, "ali", "", 10, 0)
	if err != nil || len(users) != 1 || users[0].ID != "usr_2" {
		t.Fatalf("users=%+v err=%v", users, err)
	}
	disabled, err := db.PortalUsers(ctx, "", PortalUserDisabled, 10, 0)
	if err != nil || len(disabled) != 1 || disabled[0].ID != "usr_3" {
		t.Fatalf("disabled=%+v err=%v", disabled, err)
	}
	if err := db.SetPortalUserStatus(ctx, "usr_2", PortalUserDisabled, now); err != nil {
		t.Fatal(err)
	}
	if err := db.SetPortalUserStatus(ctx, "missing", PortalUserDisabled, now); err != sql.ErrNoRows {
		t.Fatalf("missing user err=%v", err)
	}
	if err := db.TouchPortalUserLogin(ctx, "usr_1", now); err != nil {
		t.Fatal(err)
	}
	admin, err := db.PortalUserByID(ctx, "usr_1")
	if err != nil || admin.LastLoginAt == nil || !admin.LastLoginAt.Equal(now) {
		t.Fatalf("admin=%+v err=%v", admin, err)
	}
}

func TestServiceBindingPersistence(t *testing.T) {
	db := openPortalDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := db.CreatePortalUser(ctx, PortalUser{ID: "usr_1", Username: "alice", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	binding := PortalServiceBinding{
		UserID: "usr_1", Mode: ServiceModeCustom,
		RenderURL: "https://render.example.com",
		RenderTokenEnc: "enc:v1:r",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.UpsertServiceBinding(ctx, binding); err != nil {
		t.Fatalf("UpsertServiceBinding: %v", err)
	}
	loaded, err := db.ServiceBindingByUser(ctx, "usr_1")
	if err != nil {
		t.Fatalf("ServiceBindingByUser: %v", err)
	}
	if loaded.Mode != ServiceModeCustom || loaded.RenderURL != binding.RenderURL || loaded.RenderTokenEnc != "enc:v1:r" {
		t.Fatalf("binding = %+v", loaded)
	}
	binding.Mode = ServiceModeManaged
	binding.UpdatedAt = now.Add(time.Minute)
	if err := db.UpsertServiceBinding(ctx, binding); err != nil {
		t.Fatal(err)
	}
	loaded, err = db.ServiceBindingByUser(ctx, "usr_1")
	if err != nil || loaded.Mode != ServiceModeManaged || !loaded.CreatedAt.Equal(now) {
		t.Fatalf("binding = %+v err=%v", loaded, err)
	}
	if err := db.DeleteServiceBinding(ctx, "usr_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ServiceBindingByUser(ctx, "usr_1"); err != sql.ErrNoRows {
		t.Fatalf("binding survived delete: %v", err)
	}
}

func TestPlatformSettingsPersistence(t *testing.T) {
	db := openPortalDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	entries := []PlatformSetting{
		{Key: "agentapi.base_url", Value: "https://agent.example.com", UpdatedAt: now, UpdatedBy: "usr_admin"},
		{Key: "agentapi.api_key", Value: "enc:v1:secret", IsSecret: true, UpdatedAt: now, UpdatedBy: "usr_admin"},
	}
	if err := db.PutPlatformSettings(ctx, entries); err != nil {
		t.Fatalf("PutPlatformSettings: %v", err)
	}
	values, err := db.PlatformSettings(ctx)
	if err != nil || len(values) != 2 {
		t.Fatalf("values=%+v err=%v", values, err)
	}
	if !values["agentapi.api_key"].IsSecret || values["agentapi.api_key"].Value != "enc:v1:secret" {
		t.Fatalf("secret setting = %+v", values["agentapi.api_key"])
	}
	entries[0].Value = "https://agent2.example.com"
	if err := db.PutPlatformSettings(ctx, entries[:1]); err != nil {
		t.Fatal(err)
	}
	values, err = db.PlatformSettings(ctx)
	if err != nil || values["agentapi.base_url"].Value != "https://agent2.example.com" {
		t.Fatalf("values=%+v err=%v", values, err)
	}
	if err := db.DeletePlatformSettings(ctx, "agentapi.api_key"); err != nil {
		t.Fatal(err)
	}
	values, err = db.PlatformSettings(ctx)
	if err != nil || len(values) != 1 {
		t.Fatalf("values=%+v err=%v", values, err)
	}
}

// The apps table intentionally gained no ownership columns in v3.
func TestAppsSchemaUnchanged(t *testing.T) {
	db := openPortalDB(t)
	rows, err := db.sql.Query(`SELECT name FROM pragma_table_info('apps') ORDER BY cid`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var columns []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		columns = append(columns, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := "id,owner_device_id,owner_session_id,state,revision,data,updated_at"
	if got := strings.Join(columns, ","); got != want {
		t.Fatalf("apps columns = %s, want %s", got, want)
	}
}
