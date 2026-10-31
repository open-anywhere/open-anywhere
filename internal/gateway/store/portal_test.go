package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func openPortalDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "portal.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestPortalUsersAndSessions(t *testing.T) {
	db := openPortalDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)

	user := PortalUser{
		ID:           "usr_1",
		Username:     "alice",
		PasswordHash: "pbkdf2-sha256$1$c2FsdA$aGFzaA",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := db.CreatePortalUser(ctx, user); err != nil {
		t.Fatalf("CreatePortalUser: %v", err)
	}
	if err := db.CreatePortalUser(ctx, PortalUser{ID: "usr_2", Username: "alice", CreatedAt: now, UpdatedAt: now}); err == nil {
		t.Fatal("a duplicate username was accepted")
	}
	loaded, err := db.PortalUserByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("PortalUserByUsername: %v", err)
	}
	if loaded.ID != user.ID || loaded.PasswordHash != user.PasswordHash || !loaded.CreatedAt.Equal(now) {
		t.Fatalf("user=%+v", loaded)
	}
	if _, err := db.PortalUserByID(ctx, user.ID); err != nil {
		t.Fatalf("PortalUserByID: %v", err)
	}

	session := PortalSession{
		TokenHash:  "hash_live",
		UserID:     user.ID,
		CreatedAt:  now,
		ExpiresAt:  now.Add(time.Hour),
		LastSeenAt: now,
		UserAgent:  "test",
	}
	if err := db.CreatePortalSession(ctx, session); err != nil {
		t.Fatalf("CreatePortalSession: %v", err)
	}
	expired := session
	expired.TokenHash = "hash_expired"
	expired.ExpiresAt = now.Add(-time.Minute)
	if err := db.CreatePortalSession(ctx, expired); err != nil {
		t.Fatalf("CreatePortalSession expired: %v", err)
	}
	loadedSession, err := db.PortalSessionByTokenHash(ctx, session.TokenHash)
	if err != nil {
		t.Fatalf("PortalSessionByTokenHash: %v", err)
	}
	if loadedSession.UserID != user.ID || loadedSession.UserAgent != "test" || !loadedSession.ExpiresAt.Equal(session.ExpiresAt) {
		t.Fatalf("session=%+v", loadedSession)
	}
	later := now.Add(5 * time.Minute)
	if err := db.TouchPortalSession(ctx, session.TokenHash, later); err != nil {
		t.Fatalf("TouchPortalSession: %v", err)
	}
	loadedSession, err = db.PortalSessionByTokenHash(ctx, session.TokenHash)
	if err != nil || !loadedSession.LastSeenAt.Equal(later) {
		t.Fatalf("last seen=%v err=%v", loadedSession.LastSeenAt, err)
	}

	removed, err := db.DeleteExpiredPortalSessions(ctx, now)
	if err != nil || removed != 1 {
		t.Fatalf("DeleteExpiredPortalSessions=%d err=%v", removed, err)
	}
	if _, err := db.PortalSessionByTokenHash(ctx, "hash_expired"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expired session err=%v", err)
	}
	if _, err := db.PortalSessionByTokenHash(ctx, session.TokenHash); err != nil {
		t.Fatalf("live session disappeared: %v", err)
	}
	if err := db.DeletePortalSession(ctx, session.TokenHash); err != nil {
		t.Fatalf("DeletePortalSession: %v", err)
	}
	if err := db.DeletePortalSession(ctx, session.TokenHash); err != nil {
		t.Fatalf("deleting twice: %v", err)
	}
}

func TestSetPortalUserStatusGuardedProtectsTheLastAdmin(t *testing.T) {
	db := openPortalDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 25, 11, 0, 0, 0, time.UTC)
	admin := PortalUser{ID: "usr_admin", Username: "admin", Role: PortalRoleAdmin, CreatedAt: now, UpdatedAt: now}
	member := PortalUser{ID: "usr_member", Username: "member", Role: PortalRoleUser, CreatedAt: now, UpdatedAt: now}
	secondAdmin := PortalUser{ID: "usr_admin2", Username: "admin2", Role: PortalRoleAdmin, CreatedAt: now, UpdatedAt: now}
	for _, user := range []PortalUser{admin, member} {
		if err := db.CreatePortalUser(ctx, user); err != nil {
			t.Fatal(err)
		}
	}

	// The only active administrator cannot be disabled.
	applied, err := db.SetPortalUserStatusGuarded(ctx, admin.ID, PortalUserDisabled, now.Add(time.Minute))
	if err != nil || applied {
		t.Fatalf("disabling the last admin: applied=%v err=%v", applied, err)
	}
	stored, err := db.PortalUserByID(ctx, admin.ID)
	if err != nil || stored.Status != PortalUserActive {
		t.Fatalf("the last admin changed status: %+v err=%v", stored, err)
	}

	// A normal account can always be disabled, even with a single admin.
	applied, err = db.SetPortalUserStatusGuarded(ctx, member.ID, PortalUserDisabled, now.Add(time.Minute))
	if err != nil || !applied {
		t.Fatalf("disabling a member: applied=%v err=%v", applied, err)
	}
	if stored, err = db.PortalUserByID(ctx, member.ID); err != nil || stored.Status != PortalUserDisabled {
		t.Fatalf("member=%+v err=%v", stored, err)
	}

	// With two administrators either one can be disabled, and enabling works.
	if err := db.CreatePortalUser(ctx, secondAdmin); err != nil {
		t.Fatal(err)
	}
	applied, err = db.SetPortalUserStatusGuarded(ctx, admin.ID, PortalUserDisabled, now.Add(2*time.Minute))
	if err != nil || !applied {
		t.Fatalf("disabling one of two admins: applied=%v err=%v", applied, err)
	}
	if stored, err = db.PortalUserByID(ctx, admin.ID); err != nil || stored.Status != PortalUserDisabled {
		t.Fatalf("admin=%+v err=%v", stored, err)
	}
	applied, err = db.SetPortalUserStatusGuarded(ctx, admin.ID, PortalUserActive, now.Add(3*time.Minute))
	if err != nil || !applied {
		t.Fatalf("enabling an admin: applied=%v err=%v", applied, err)
	}
	if stored, err = db.PortalUserByID(ctx, admin.ID); err != nil || stored.Status != PortalUserActive {
		t.Fatalf("admin=%+v err=%v", stored, err)
	}

	// A missing account is reported as sql.ErrNoRows, like the unguarded call.
	_, err = db.SetPortalUserStatusGuarded(ctx, "usr_missing", PortalUserDisabled, now)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing user err=%v", err)
	}
	if _, err := db.SetPortalUserStatusGuarded(ctx, admin.ID, "pending", now); err == nil {
		t.Fatal("an invalid status was accepted")
	}
}

func TestServiceBindingSecretsOnlyReturnsRowsWithTokens(t *testing.T) {
	db := openPortalDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	for _, user := range []PortalUser{
		{ID: "usr_1", Username: "alice", CreatedAt: now, UpdatedAt: now},
		{ID: "usr_2", Username: "bob", CreatedAt: now, UpdatedAt: now},
		{ID: "usr_3", Username: "carol", CreatedAt: now, UpdatedAt: now},
	} {
		if err := db.CreatePortalUser(ctx, user); err != nil {
			t.Fatal(err)
		}
	}
	bindings := []PortalServiceBinding{
		{
			UserID: "usr_1", Mode: ServiceModeCustom,
			RenderURL: "https://render.example",
			RenderTokenEnc: "enc:v1:render", CreatedAt: now, UpdatedAt: now,
		},
		{
			UserID: "usr_2", Mode: ServiceModeCustom,
			RenderURL: "https://render.example",
			CreatedAt: now, UpdatedAt: now,
		},
	}
	for _, binding := range bindings {
		if err := db.UpsertServiceBinding(ctx, binding); err != nil {
			t.Fatal(err)
		}
	}
	secrets, err := db.ServiceBindingSecrets(ctx)
	if err != nil {
		t.Fatalf("ServiceBindingSecrets: %v", err)
	}
	if len(secrets) != 1 {
		t.Fatalf("secrets=%+v", secrets)
	}
	if secrets[0].UserID != "usr_1" || secrets[0].RenderTokenEnc != "enc:v1:render" {
		t.Fatalf("binding=%+v", secrets[0])
	}
	for _, binding := range secrets {
		if binding.RenderURL != "" || binding.Mode != "" {
			t.Fatalf("addresses leaked from ServiceBindingSecrets: %+v", binding)
		}
	}
}

func TestPortalDevicesAndSettings(t *testing.T) {
	db := openPortalDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC)
	if err := db.CreatePortalUser(ctx, PortalUser{ID: "usr_1", Username: "alice", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}

	first := PortalDevice{
		ID:        "dev_1",
		UserID:    "usr_1",
		Kind:      PortalDeviceVirtual,
		MAC:       "02:00:00:00:00:01",
		ClientID:  "client-1",
		State:     PortalDeviceProvisioning,
		Step:      "create_agent",
		CreatedAt: now,
		UpdatedAt: now,
	}
	second := PortalDevice{
		ID:        "dev_2",
		UserID:    "usr_1",
		Kind:      PortalDevicePhysical,
		MAC:       "02:00:00:00:00:02",
		ClientID:  "client-2",
		State:     PortalDeviceProvisioning,
		Step:      "create_agent",
		CreatedAt: now.Add(time.Minute),
		UpdatedAt: now.Add(time.Minute),
	}
	if err := db.CreatePortalDevice(ctx, first); err != nil {
		t.Fatalf("CreatePortalDevice: %v", err)
	}
	if err := db.CreatePortalDevice(ctx, second); err != nil {
		t.Fatalf("CreatePortalDevice second: %v", err)
	}
	if err := db.CreatePortalDevice(ctx, PortalDevice{ID: "dev_3", UserID: "usr_1", MAC: first.MAC, CreatedAt: now, UpdatedAt: now}); err == nil {
		t.Fatal("a duplicate MAC was accepted")
	}

	devices, err := db.PortalDevicesByUser(ctx, "usr_1")
	if err != nil || len(devices) != 2 {
		t.Fatalf("devices=%+v err=%v", devices, err)
	}
	if devices[0].ID != "dev_2" || devices[1].ID != "dev_1" {
		t.Fatalf("devices are not newest first: %+v", devices)
	}

	registeredAt := now.Add(2 * time.Minute)
	first.State = PortalDeviceRegistered
	first.Step = "done"
	first.LinxAgentID = "agent_1"
	first.AgentName = "Open Anywhere · alice"
	first.CredentialPath = "devices/dev_1.json"
	first.RegisteredAt = &registeredAt
	first.UpdatedAt = registeredAt
	if err := db.UpdatePortalDevice(ctx, first); err != nil {
		t.Fatalf("UpdatePortalDevice: %v", err)
	}
	loaded, err := db.PortalDeviceByID(ctx, "dev_1")
	if err != nil {
		t.Fatalf("PortalDeviceByID: %v", err)
	}
	if loaded.State != PortalDeviceRegistered || loaded.LinxAgentID != "agent_1" ||
		loaded.CredentialPath != "devices/dev_1.json" || loaded.RegisteredAt == nil ||
		!loaded.RegisteredAt.Equal(registeredAt) {
		t.Fatalf("device=%+v", loaded)
	}
	registered, err := db.PortalDevicesByState(ctx, PortalDeviceRegistered)
	if err != nil || len(registered) != 1 || registered[0].ID != "dev_1" {
		t.Fatalf("registered=%+v err=%v", registered, err)
	}

	if err := db.SetPortalSetting(ctx, "linx.mcp_server_id", "server_1"); err != nil {
		t.Fatalf("SetPortalSetting: %v", err)
	}
	if err := db.SetPortalSetting(ctx, "linx.mcp_server_id", "server_2"); err != nil {
		t.Fatalf("SetPortalSetting replace: %v", err)
	}
	value, err := db.PortalSetting(ctx, "linx.mcp_server_id")
	if err != nil || value != "server_2" {
		t.Fatalf("PortalSetting=%q err=%v", value, err)
	}
	if _, err := db.PortalSetting(ctx, "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing setting err=%v", err)
	}
}
