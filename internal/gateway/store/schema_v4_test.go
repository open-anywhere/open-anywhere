package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestSchemaV4AllowsSeveralDevicesAndSharedAgents(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "gateway.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	now := time.Now().UTC()
	if err := db.CreatePortalUser(ctx, PortalUser{ID: "usr_1", Username: "ann", PasswordHash: "x", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	add := func(id, kind, mac, agent, name string) error {
		return db.CreatePortalDevice(ctx, PortalDevice{
			ID: id, UserID: "usr_1", Kind: kind, MAC: mac, LinxAgentID: agent, Name: name,
			State: PortalDeviceRegistered, CreatedAt: now, UpdatedAt: now,
		})
	}
	if err := add("dev_1", PortalDeviceVirtual, "02:00:00:00:00:01", "agent_1", "客厅"); err != nil {
		t.Fatalf("first virtual device: %v", err)
	}
	if err := add("dev_2", PortalDeviceVirtual, "02:00:00:00:00:02", "agent_2", "卧室"); err != nil {
		t.Fatalf("second virtual device: %v", err)
	}
	if err := add("dev_3", PortalDevicePhysical, "aa:00:00:00:00:03", "agent_1", "音箱"); err != nil {
		t.Fatalf("hardware sharing an agent: %v", err)
	}
	if err := add("dev_4", PortalDevicePhysical, "aa:00:00:00:00:03", "agent_1", "重复"); err == nil {
		t.Fatal("the same MAC was accepted twice")
	}
	got, err := db.PortalDeviceByIdentity(ctx, "aa:00:00:00:00:03", "agent_1")
	if err != nil || got.ID != "dev_3" || got.Name != "音箱" {
		t.Fatalf("identity lookup = %+v err=%v", got, err)
	}
	if err := db.DeletePortalDevice(ctx, "dev_3"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.PortalDeviceByID(ctx, "dev_3"); err != sql.ErrNoRows {
		t.Fatalf("deleted device lookup err=%v", err)
	}
}
