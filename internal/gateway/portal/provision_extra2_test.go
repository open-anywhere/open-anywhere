package portal

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
	devicelinx "github.com/open-anywhere/open-anywhere/internal/voiceentry/linx"
)

func TestProvisionRunReportsASaveFailureAtEachStep(t *testing.T) {
	cases := []struct {
		name    string
		trigger string
		prepare func(t *testing.T, env *provisionTestEnv)
		ota     *fakeOTA
	}{
		{
			name: "agent",
			trigger: `CREATE TRIGGER gate_block_agent_save BEFORE UPDATE ON portal_devices
				WHEN NEW.step = 'create_agent' AND OLD.step <> 'create_agent'
				BEGIN SELECT RAISE(ABORT, 'blocked'); END`,
			prepare: func(t *testing.T, env *provisionTestEnv) {
				device := env.device
				device.Step = stepAttachMCP
				if err := env.db.UpdatePortalDevice(context.Background(), device); err != nil {
					t.Fatal(err)
				}
			},
			ota: &fakeOTA{immediate: true},
		},
		{
			name: "ota",
			trigger: `CREATE TRIGGER gate_block_ota_save BEFORE UPDATE ON portal_devices
				WHEN NEW.step = 'ota' AND OLD.step = 'ota'
				BEGIN SELECT RAISE(ABORT, 'blocked'); END`,
			ota: &fakeOTA{immediate: true},
		},
		{
			name: "bind",
			trigger: `CREATE TRIGGER gate_block_bind_save BEFORE UPDATE ON portal_devices
				WHEN NEW.step = 'bind'
				BEGIN SELECT RAISE(ABORT, 'blocked'); END`,
			ota: &fakeOTA{},
		},
		{
			name: "token",
			trigger: `CREATE TRIGGER gate_block_token_save BEFORE UPDATE ON portal_devices
				WHEN NEW.step = 'token'
				BEGIN SELECT RAISE(ABORT, 'blocked'); END`,
			ota: &fakeOTA{afterBind: true},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			platform := &fakePlatform{}
			testCase.ota.platform = platform
			env := newProvisionTestEnv(t, platform, testCase.ota)
			if testCase.prepare != nil {
				testCase.prepare(t, env)
			}
			execRaw(t, env.dbPath, testCase.trigger)
			err := env.provisioner.Run(context.Background(), env.device.ID)
			if err == nil || !strings.Contains(err.Error(), "save device") {
				t.Fatalf("Run error = %v", err)
			}
			if device := loadProvisionDevice(t, env, env.device.ID); device.State == store.PortalDeviceRegistered {
				t.Fatalf("device=%+v", device)
			}
		})
	}
}

func TestProvisionRunReportsAMalformedMCPInstanceSetting(t *testing.T) {
	env := newProvisionTestEnv(t, &fakePlatform{}, &fakeOTA{immediate: true})
	execRaw(t, env.dbPath, `ALTER TABLE portal_settings RENAME TO portal_settings_base`)
	execRaw(t, env.dbPath, `INSERT INTO portal_settings_base (key, value, updated_at)
		VALUES ('linx.mcp_instance_id', 'mcp_1', '2026-01-01T00:00:00Z')`)
	// The view succeeds for the server key and raises for the instance key, so
	// the second lookup in mcpIDs is the one that fails.
	execRaw(t, env.dbPath, `CREATE VIEW portal_settings AS
		SELECT key,
			CASE WHEN key = 'linx.mcp_instance_id' THEN json_extract('poison', '$') ELSE value END AS value,
			updated_at
		FROM portal_settings_base`)
	err := env.provisioner.Run(context.Background(), env.device.ID)
	if err == nil {
		t.Fatal("Run ignored the malformed MCP instance setting")
	}
	if !strings.Contains(err.Error(), "malformed JSON") {
		t.Fatalf("Run error = %v", err)
	}
}

func TestProvisionRunLogsAVoiceStartFailureAfterRegistration(t *testing.T) {
	platform := &fakePlatform{}
	env := newProvisionTestEnv(t, platform, &fakeOTA{immediate: true})
	prepareDeviceThroughMCP(t, env)
	execRaw(t, env.dbPath, "ALTER TABLE portal_users RENAME TO portal_users_hidden")
	if err := env.provisioner.Run(context.Background(), env.device.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	device := loadProvisionDevice(t, env, env.device.ID)
	if device.State != store.PortalDeviceRegistered || device.Step != stepDone {
		t.Fatalf("device=%+v", device)
	}
}

func TestProvisionRecoverLogsVoiceFailuresFromTheOwnerLookup(t *testing.T) {
	env := newProvisionTestEnv(t, &fakePlatform{}, &fakeOTA{immediate: true})
	device := env.device
	device.State = store.PortalDeviceRegistered
	device.Step = stepDone
	device.CredentialPath = filepath.Join("devices", device.ID+".json")
	if err := saveCredential(filepath.Join(env.dataDir, device.CredentialPath), devicelinx.Device{
		MAC: device.MAC, ClientID: device.ClientID, Token: "device-token", WSURL: "ws://127.0.0.1:1/ws",
	}); err != nil {
		t.Fatal(err)
	}
	if err := env.db.UpdatePortalDevice(context.Background(), device); err != nil {
		t.Fatal(err)
	}
	execRaw(t, env.dbPath, "ALTER TABLE portal_users RENAME TO portal_users_hidden")
	if err := env.provisioner.Recover(context.Background()); err != nil {
		t.Fatalf("Recover: %v", err)
	}
}

func TestProvisionRecoverReportsAPromotionSaveFailure(t *testing.T) {
	env := newProvisionTestEnv(t, &fakePlatform{}, &fakeOTA{immediate: true})
	if err := saveCredential(filepath.Join(env.dataDir, "devices", env.device.ID+".json"), devicelinx.Device{
		MAC: env.device.MAC, ClientID: env.device.ClientID, Token: "device-token", WSURL: "ws://127.0.0.1:1/ws",
	}); err != nil {
		t.Fatal(err)
	}
	execRaw(t, env.dbPath, `CREATE TRIGGER gate_block_promotion BEFORE UPDATE ON portal_devices
		WHEN NEW.state = 'registered' AND OLD.state <> 'registered'
		BEGIN SELECT RAISE(ABORT, 'blocked'); END`)
	if err := env.provisioner.Recover(context.Background()); err == nil {
		t.Fatal("Recover ignored the promotion save failure")
	}
	if device := loadProvisionDevice(t, env, env.device.ID); device.State != store.PortalDeviceProvisioning {
		t.Fatalf("device=%+v", device)
	}
}

func TestProvisionRecoverLogsAVoiceFailureAfterPromotion(t *testing.T) {
	env := newProvisionTestEnv(t, &fakePlatform{}, &fakeOTA{immediate: true})
	if err := saveCredential(filepath.Join(env.dataDir, "devices", env.device.ID+".json"), devicelinx.Device{
		MAC: env.device.MAC, ClientID: env.device.ClientID, Token: "device-token", WSURL: "ws://127.0.0.1:1/ws",
	}); err != nil {
		t.Fatal(err)
	}
	execRaw(t, env.dbPath, "ALTER TABLE portal_users RENAME TO portal_users_hidden")
	if err := env.provisioner.Recover(context.Background()); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if device := loadProvisionDevice(t, env, env.device.ID); device.State != store.PortalDeviceRegistered {
		t.Fatalf("device=%+v", device)
	}
}

func TestProvisionRecoverSkipsADeviceThatIsAlreadyProvisioning(t *testing.T) {
	env := newProvisionTestEnv(t, &fakePlatform{}, &fakeOTA{immediate: true})
	env.provisioner.mu.Lock()
	env.provisioner.running[env.device.ID] = true
	env.provisioner.mu.Unlock()
	if err := env.provisioner.Recover(context.Background()); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if device := loadProvisionDevice(t, env, env.device.ID); device.State != store.PortalDeviceProvisioning {
		t.Fatalf("device=%+v", device)
	}
}

func TestProvisionStopsPollingWhenItsContextIsCanceled(t *testing.T) {
	env := newProvisionTestEnv(t, &fakePlatform{}, &fakeOTA{})
	env.provisioner.cfg.TokenPollInterval = 500 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- env.provisioner.Run(ctx, env.device.ID) }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if loadProvisionDevice(t, env, env.device.ID).Step == stepToken {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the provisioner never reached the token poll")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run reported success after its context was canceled")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not observe the canceled poll")
	}
}
