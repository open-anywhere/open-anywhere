package store

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// rootPage returns the root b-tree page of a table or index. Targeting the
// object's own tree keeps the damage independent of page allocation order in
// the rest of the file.
func rootPage(t *testing.T, db *DB, name string) uint32 {
	t.Helper()
	var root uint32
	if err := db.sql.QueryRow(`SELECT rootpage FROM sqlite_master WHERE name = ?`, name).Scan(&root); err != nil {
		t.Fatalf("rootpage of %s: %v", name, err)
	}
	return root
}

// corruptRightmostLeaf zeroes the header of the right-most leaf of one b-tree.
// An ordered scan walks that page last, so the query returns its first rows and
// then fails while iterating, which is the row iteration error path.
func corruptRightmostLeaf(t *testing.T, path string, root uint32) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pageSize := uint32(binary.BigEndian.Uint16(data[16:18]))
	if pageSize == 1 {
		pageSize = 65536
	}
	page := root
	for depth := 0; depth < 64; depth++ {
		offset := (page - 1) * pageSize
		if int(offset)+int(pageSize) > len(data) || page == 0 {
			t.Fatalf("b-tree page %d is outside the database file", page)
		}
		switch data[offset] {
		case 0x02, 0x05: // index or table interior page
			page = binary.BigEndian.Uint32(data[offset+8 : offset+12])
		case 0x0a, 0x0d: // index or table leaf page
			for i := uint32(0); i < 32 && int(offset+i) < len(data); i++ {
				data[offset+i] = 0
			}
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
			return
		default:
			t.Fatalf("page %d has type %#x", page, data[offset])
		}
	}
	t.Fatal("b-tree is deeper than expected")
}

func reopenDamaged(t *testing.T, path string) *DB {
	t.Helper()
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open damaged database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestListingsReportARowIterationFailure(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

	t.Run("portal users", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "gateway.db")
		db, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		mustExec(t, db.sql, `CREATE INDEX portal_users_order ON portal_users(created_at DESC, id)`)
		for i := 0; i < 40; i++ {
			// Times decrease with the ID, so the descending index scan ends on
			// the rows written to the file last.
			if _, err := db.sql.Exec(`INSERT INTO portal_users
				(id, username, password_hash, role, status, created_at, updated_at, last_login_at)
				VALUES (?, ?, ?, 'user', 'active', ?, '', NULL)`,
				fmt.Sprintf("usr-%03d", i), fmt.Sprintf("usr-%03d", i), strings.Repeat("h", 3000),
				now.Add(-time.Duration(i)*time.Minute).Format(time.RFC3339Nano)); err != nil {
				t.Fatal(err)
			}
		}
		// The ordered index drives the scan; its rows live in insertion order,
		// so the table's right-most leaf is read last.
		root := rootPage(t, db, "portal_users")
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		corruptRightmostLeaf(t, path, root)
		damaged := reopenDamaged(t, path)
		if _, err := damaged.PortalUsers(ctx, "", "", 100, 0); err == nil ||
			!strings.Contains(err.Error(), "list portal users") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("portal users by status", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "gateway.db")
		db, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		mustExec(t, db.sql, `CREATE INDEX portal_users_status ON portal_users(status, password_hash)`)
		insert := func(id, status string) {
			if _, err := db.sql.Exec(`INSERT INTO portal_users
				(id, username, password_hash, role, status, created_at, updated_at, last_login_at)
				VALUES (?, ?, ?, 'user', ?, '', '', NULL)`, id, id, strings.Repeat("h", 1500), status); err != nil {
				t.Fatal(err)
			}
		}
		for i := 0; i < 80; i++ {
			insert(fmt.Sprintf("act-%03d", i), PortalUserActive)
		}
		for i := 0; i < 80; i++ {
			insert(fmt.Sprintf("dis-%03d", i), PortalUserDisabled)
		}
		root := rootPage(t, db, "portal_users_status")
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		corruptRightmostLeaf(t, path, root)
		damaged := reopenDamaged(t, path)
		if _, err := damaged.CountPortalUsersByStatus(ctx); err == nil ||
			!strings.Contains(err.Error(), "count portal users") {
			t.Fatalf("err = %v", err)
		}
	})

	devices := []struct {
		name  string
		index string
		setup func(*testing.T, *DB)
		call  func(*DB) error
		want  string
	}{
		{
			name:  "portal devices by user",
			index: "portal_devices_by_user",
			setup: func(t *testing.T, db *DB) {
				mustExec(t, db.sql, `CREATE INDEX portal_devices_by_user ON portal_devices(user_id, created_at DESC, id)`)
				for i := 0; i < 120; i++ {
					device := PortalDevice{ID: fmt.Sprintf("dev-%03d", i), UserID: "usr_1", Kind: PortalDevicePhysical,
						MAC: fmt.Sprintf("mac-%03d", i), ClientID: strings.Repeat("c", 1200),
						State: PortalDeviceProvisioning, CreatedAt: now.Add(-time.Duration(i) * time.Minute), UpdatedAt: now}
					if err := db.CreatePortalDevice(ctx, device); err != nil {
						t.Fatal(err)
					}
				}
			},
			call: func(db *DB) error { _, err := db.PortalDevicesByUser(ctx, "usr_1"); return err },
			want: "list portal devices",
		},
		{
			name:  "portal devices by state",
			index: "portal_devices_by_state",
			setup: func(t *testing.T, db *DB) {
				mustExec(t, db.sql, `CREATE INDEX portal_devices_by_state ON portal_devices(state, created_at, id)`)
				for i := 0; i < 120; i++ {
					device := PortalDevice{ID: fmt.Sprintf("dev-%03d", i), UserID: "usr_1", Kind: PortalDevicePhysical,
						MAC: fmt.Sprintf("mac-%03d", i), ClientID: strings.Repeat("c", 1200),
						State: PortalDeviceProvisioning, CreatedAt: now.Add(time.Duration(i) * time.Minute), UpdatedAt: now}
					if err := db.CreatePortalDevice(ctx, device); err != nil {
						t.Fatal(err)
					}
				}
			},
			call: func(db *DB) error {
				_, err := db.PortalDevicesByState(ctx, PortalDeviceProvisioning)
				return err
			},
			want: "list portal devices",
		},
		{
			name:  "all portal devices",
			index: "portal_devices_order",
			setup: func(t *testing.T, db *DB) {
				mustExec(t, db.sql, `CREATE INDEX portal_devices_order ON portal_devices(created_at DESC, id)`)
				for i := 0; i < 120; i++ {
					device := PortalDevice{ID: fmt.Sprintf("dev-%03d", i), UserID: "usr_1", Kind: PortalDevicePhysical,
						MAC: fmt.Sprintf("mac-%03d", i), ClientID: strings.Repeat("c", 1200),
						State: PortalDeviceProvisioning, CreatedAt: now.Add(-time.Duration(i) * time.Minute), UpdatedAt: now}
					if err := db.CreatePortalDevice(ctx, device); err != nil {
						t.Fatal(err)
					}
				}
			},
			call: func(db *DB) error { _, err := db.PortalDevicesAll(ctx, "", "", 500); return err },
			want: "list portal devices",
		},
		{
			name:  "portal devices by state count",
			index: "portal_devices_state_agent",
			setup: func(t *testing.T, db *DB) {
				mustExec(t, db.sql, `CREATE INDEX portal_devices_state_agent ON portal_devices(state, agent_name)`)
				for index, state := range []string{PortalDeviceProvisioning, PortalDeviceRegistered} {
					for i := 0; i < 80; i++ {
						device := PortalDevice{ID: fmt.Sprintf("%s-%03d", state, i), UserID: "usr_1", Kind: PortalDevicePhysical,
							MAC: fmt.Sprintf("%s-mac-%03d", state, i), ClientID: "c",
							AgentName: strings.Repeat(string(rune('a'+index)), 1500), State: state,
							CreatedAt: now, UpdatedAt: now}
						if err := db.CreatePortalDevice(ctx, device); err != nil {
							t.Fatal(err)
						}
					}
				}
			},
			call: func(db *DB) error { _, err := db.CountPortalDevicesByState(ctx); return err },
			want: "count portal devices",
		},
	}
	for _, testCase := range devices {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "gateway.db")
			db, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.CreatePortalUser(ctx, PortalUser{ID: "usr_1", Username: "alice", CreatedAt: now, UpdatedAt: now}); err != nil {
				t.Fatal(err)
			}
			testCase.setup(t, db)
			root := rootPage(t, db, testCase.index)
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			corruptRightmostLeaf(t, path, root)
			damaged := reopenDamaged(t, path)
			if err := testCase.call(damaged); err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}
