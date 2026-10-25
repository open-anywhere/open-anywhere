package portal

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	devicelinx "github.com/open-anywhere/open-anywhere/internal/voiceentry/linx"
)

// portalDeviceView keeps the device rows readable while one expression makes a
// single query fail. Callers rename the table first and then create the view.
const portalDeviceView = `SELECT id, user_id, kind, mac, client_id, linx_agent_id, agent_name, state, step,
	%s AS error, credential_path, created_at, updated_at, registered_at
	FROM portal_devices_base`

func createPortalDeviceView(t *testing.T, env *provisionTestEnv, errorExpression string) {
	t.Helper()
	execRaw(t, env.dbPath, "ALTER TABLE portal_devices RENAME TO portal_devices_base")
	execRaw(t, env.dbPath, "CREATE VIEW portal_devices AS "+fmt.Sprintf(portalDeviceView, errorExpression))
}

func writeProvisionCredential(t *testing.T, env *provisionTestEnv) {
	t.Helper()
	if err := saveCredential(filepath.Join(env.dataDir, "devices", env.device.ID+".json"), devicelinx.Device{
		MAC: env.device.MAC, ClientID: env.device.ClientID, Token: "device-token", WSURL: "ws://127.0.0.1:1/ws",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestProvisionRecoverReportsADeviceStateQueryFailure(t *testing.T) {
	env := newProvisionTestEnv(t, &fakePlatform{}, &fakeOTA{immediate: true})
	// The registered lookup returns no rows and reads nothing; the repair loop
	// then fails while reading the provisioning row.
	createPortalDeviceView(t, env, `CASE WHEN state = 'provisioning' THEN json_extract('poison', '$') ELSE error END`)
	if err := env.provisioner.Recover(context.Background()); err == nil {
		t.Fatal("Recover ignored the failing provisioning query")
	}
}

func TestProvisionRecoverReportsAFinalProvisioningQueryFailure(t *testing.T) {
	env := newProvisionTestEnv(t, &fakePlatform{}, &fakeOTA{immediate: true})
	writeProvisionCredential(t, env)
	execRaw(t, env.dbPath, "CREATE TABLE gate_poison (value TEXT NOT NULL)")
	execRaw(t, env.dbPath, "INSERT INTO gate_poison (value) VALUES ('0')")
	createPortalDeviceView(t, env, `CASE WHEN state = 'provisioning' AND (SELECT value FROM gate_poison) = '1'
		THEN json_extract('poison', '$') ELSE error END`)
	// The repair loop promotes the device and records that the write happened,
	// but leaves the row provisioning so the final query reads it again.
	execRaw(t, env.dbPath, `CREATE TRIGGER portal_devices_record_update INSTEAD OF UPDATE ON portal_devices
		BEGIN
			UPDATE gate_poison SET value = '1';
		END`)
	if err := env.provisioner.Recover(context.Background()); err == nil {
		t.Fatal("Recover ignored the failing final provisioning query")
	}
}

func TestProvisionRecoverSkipsResumingADeviceThatAlreadyHasCredentials(t *testing.T) {
	env := newProvisionTestEnv(t, &fakePlatform{}, &fakeOTA{immediate: true})
	writeProvisionCredential(t, env)
	createPortalDeviceView(t, env, "error")
	// The repair loop promotes the device and swallows the write, so the last
	// query still finds it provisioning but with valid credentials.
	execRaw(t, env.dbPath, `CREATE TRIGGER portal_devices_swallow_update INSTEAD OF UPDATE ON portal_devices
		BEGIN
			SELECT 1;
		END`)
	if err := env.provisioner.Recover(context.Background()); err != nil {
		t.Fatalf("Recover: %v", err)
	}
}
