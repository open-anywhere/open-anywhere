package portal

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/linx"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
	devicelinx "github.com/open-anywhere/open-anywhere/internal/voiceentry/linx"
)

type provisionTestEnv struct {
	provisioner *Provisioner
	db          *store.DB
	device      store.PortalDevice
	voice       *VoiceManager
	dataDir     string
	dbPath      string
}

func newProvisionTestEnv(t *testing.T, platform *fakePlatform, ota *fakeOTA) *provisionTestEnv {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "portal.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	now := time.Now().UTC()
	if err := db.CreatePortalUser(context.Background(), store.PortalUser{
		ID: "usr_1", Username: "alice", PasswordHash: "x", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	device := store.PortalDevice{
		ID: "dev_1", UserID: "usr_1", Kind: store.PortalDeviceVirtual,
		MAC: "02:00:00:00:00:01", ClientID: "client-1",
		State: store.PortalDeviceProvisioning, Step: stepCreateAgent,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.CreatePortalDevice(context.Background(), device); err != nil {
		t.Fatal(err)
	}
	platformServer := httptest.NewServer(platform.handler(t))
	t.Cleanup(platformServer.Close)
	otaServer := httptest.NewServer(ota.handler(t))
	t.Cleanup(otaServer.Close)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	client := linx.New(linx.Config{
		BaseURL:      platformServer.URL,
		APIKey:       "secret-key",
		PublicMCPURL: "https://gateway.example/sse",
		MCPName:      "Open Anywhere",
		MCPToken:     "mcp-token",
		OTAClient:    devicelinx.OTAClient{BaseURL: otaServer.URL},
	})
	voice := NewVoiceManager(ctx)
	t.Cleanup(voice.StopAll)
	dataDir := t.TempDir()
	provisioner := NewProvisioner(ctx, db, client, voice, ProvisionConfig{
		DataDir:           dataDir,
		MCPName:           "Open Anywhere",
		PublicMCPURL:      "https://gateway.example/sse",
		MCPToken:          "mcp-token",
		AssistantName:     "小灵",
		SystemPrompt:      "prompt",
		ProvisionTimeout:  5 * time.Second,
		TokenPollInterval: 5 * time.Millisecond,
		TokenPollAttempts: 3,
	})
	return &provisionTestEnv{provisioner: provisioner, db: db, device: device, voice: voice, dataDir: dataDir, dbPath: dbPath}
}

func insertOrphanDevice(t *testing.T, path, id string) {
	t.Helper()
	raw, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(0)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := raw.Exec(`INSERT INTO portal_devices
		(id, user_id, kind, mac, client_id, linx_agent_id, agent_name, state, step, error, credential_path, created_at, updated_at)
		VALUES (?, 'usr_missing', 'virtual', ?, ?, '', '', 'provisioning', 'create_agent', '', '', ?, ?)`,
		id, "02:00:00:00:00:99", "client-orphan", now, now); err != nil {
		t.Fatalf("insert orphan device: %v", err)
	}
}

func TestProvisionerHelpers(t *testing.T) {
	env := newProvisionTestEnv(t, &fakePlatform{}, &fakeOTA{immediate: true})
	provisioner := env.provisioner

	if got := provisioner.CredentialPath(store.PortalDevice{ID: "dev_x"}); got != filepath.Join(env.dataDir, "devices", "dev_x.json") {
		t.Fatalf("default credential path = %q", got)
	}
	if got := provisioner.CredentialPath(store.PortalDevice{ID: "dev_x", CredentialPath: "custom/x.json"}); got != filepath.Join(env.dataDir, "custom", "x.json") {
		t.Fatalf("relative credential path = %q", got)
	}
	absolute := filepath.Join(t.TempDir(), "x.json")
	if got := provisioner.CredentialPath(store.PortalDevice{ID: "dev_x", CredentialPath: absolute}); got != absolute {
		t.Fatalf("absolute credential path = %q", got)
	}

	if got := provisioner.scrub("token=mcp-token end"); got != "token=*** end" {
		t.Fatalf("scrub = %q", got)
	}
	if got := provisioner.scrub(strings.Repeat("x", 400)); len(got) != 300 {
		t.Fatalf("scrub did not truncate: %d", len(got))
	}

	for _, test := range []struct {
		err  error
		want string
	}{
		{nil, "设备注册失败，请重试"},
		{app.ErrPlatformNotConfigured, deviceErrorCodePlatformNotConfigured},
		{&linx.Error{Status: http.StatusUnauthorized, Message: "nope"}, "平台鉴权失败，请联系维护者"},
		{&linx.Error{Status: http.StatusBadGateway, Message: "nope"}, "平台暂时不可用，请稍后重试"},
		{&linx.Error{Message: "agent mcp-token exploded"}, "agent *** exploded"},
		{errors.New("plain failure"), "plain failure"},
	} {
		if got := provisioner.describeError(test.err); got != test.want {
			t.Fatalf("describeError(%v) = %q want %q", test.err, got, test.want)
		}
	}

	provisioner.linx = nil
	if _, err := provisioner.client(context.Background()); !errors.Is(err, app.ErrPlatformNotConfigured) {
		t.Fatalf("nil provider err = %v", err)
	}
	provisioner.linx = func(context.Context) (*linx.Client, error) { return nil, nil }
	if _, err := provisioner.client(context.Background()); !errors.Is(err, app.ErrPlatformNotConfigured) {
		t.Fatalf("nil client err = %v", err)
	}
	boom := errors.New("provider down")
	provisioner.linx = func(context.Context) (*linx.Client, error) { return nil, boom }
	if _, err := provisioner.client(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("provider err = %v", err)
	}
	provisioner.linx = func(context.Context) (*linx.Client, error) { return linx.New(linx.Config{}), nil }
	if _, err := provisioner.client(context.Background()); err != nil {
		t.Fatalf("client err = %v", err)
	}

	provisioner.running["dev_1"] = true
	if provisioner.Start("dev_1") {
		t.Fatal("Start launched a second iteration")
	}
	delete(provisioner.running, "dev_1")

	// A full semaphore plus a canceled root context ends the goroutine instead
	// of running provisioning.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	provisioner.ctx = ctx
	provisioner.sem = make(chan struct{}, 1)
	provisioner.sem <- struct{}{}
	if !provisioner.Start("dev_1") {
		t.Fatal("Start refused a fresh device")
	}
	deadline := time.Now().Add(time.Second)
	for provisioner.Running("dev_1") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if provisioner.Running("dev_1") {
		t.Fatal("the canceled iteration is still marked running")
	}
}

func TestProvisionRunRejectsUnknownAndNonVirtualDevices(t *testing.T) {
	env := newProvisionTestEnv(t, &fakePlatform{}, &fakeOTA{immediate: true})
	if err := env.provisioner.Run(context.Background(), "dev_missing"); err == nil {
		t.Fatal("Run accepted a missing device")
	}
	device := env.device
	device.Kind = "hardware"
	if err := env.db.UpdatePortalDevice(context.Background(), device); err != nil {
		t.Fatal(err)
	}
	if err := env.provisioner.Run(context.Background(), device.ID); err != nil {
		t.Fatalf("Run on a non-virtual device: %v", err)
	}
}

func TestProvisionRunRegisteredDeviceStartsVoice(t *testing.T) {
	env := newProvisionTestEnv(t, &fakePlatform{}, &fakeOTA{immediate: true})
	device := env.device
	device.State = store.PortalDeviceRegistered
	device.Step = stepDone
	if err := env.db.UpdatePortalDevice(context.Background(), device); err != nil {
		t.Fatal(err)
	}
	if err := env.provisioner.Run(context.Background(), device.ID); err == nil {
		t.Fatal("Run started a registered device without credentials")
	}
	writeCredential(t, env.dataDir, device.ID)
	if err := env.provisioner.Run(context.Background(), device.ID); err != nil {
		t.Fatalf("Run with credentials: %v", err)
	}
}

func TestProvisionRunReusesPersistedAgentAndMCP(t *testing.T) {
	platform := &fakePlatform{}
	env := newProvisionTestEnv(t, platform, &fakeOTA{immediate: true})
	ctx := context.Background()
	device := env.device
	device.LinxAgentID = "agent_existing"
	if err := env.db.UpdatePortalDevice(ctx, device); err != nil {
		t.Fatal(err)
	}
	if err := env.db.SetPortalSetting(ctx, settingMCPServer, "server_1"); err != nil {
		t.Fatal(err)
	}
	if err := env.db.SetPortalSetting(ctx, settingMCPInstance, "mcp_1"); err != nil {
		t.Fatal(err)
	}
	if err := env.provisioner.Run(ctx, device.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if platform.agents != 0 || len(platform.servers) != 0 || len(platform.instances) != 0 || platform.joins != 1 {
		t.Fatalf("platform agents=%d servers=%d instances=%d joins=%d",
			platform.agents, len(platform.servers), len(platform.instances), platform.joins)
	}
}

func TestProvisionRunFailureBranches(t *testing.T) {
	t.Run("device save fails", func(t *testing.T) {
		env := newProvisionTestEnv(t, &fakePlatform{}, &fakeOTA{immediate: true})
		execRaw(t, env.dbPath, `CREATE TRIGGER block_device_update BEFORE UPDATE ON portal_devices
			BEGIN SELECT RAISE(ABORT, 'read-only'); END`)
		if err := env.provisioner.Run(context.Background(), env.device.ID); err == nil {
			t.Fatal("Run ignored a save failure")
		}
	})
	t.Run("owner is missing", func(t *testing.T) {
		env := newProvisionTestEnv(t, &fakePlatform{}, &fakeOTA{immediate: true})
		insertOrphanDevice(t, env.dbPath, "dev_orphan")
		var logs []string
		env.provisioner.logf = func(format string, args ...any) { logs = append(logs, format) }
		if err := env.provisioner.Run(context.Background(), "dev_orphan"); err == nil {
			t.Fatal("Run registered a device without an owner")
		}
		device, err := env.db.PortalDeviceByID(context.Background(), "dev_orphan")
		if err != nil || device.State != store.PortalDeviceError || !strings.Contains(device.Error, "无法读取用户信息") {
			t.Fatalf("device=%+v err=%v", device, err)
		}
	})
	t.Run("mcp save fails", func(t *testing.T) {
		env := newProvisionTestEnv(t, &fakePlatform{}, &fakeOTA{immediate: true})
		device := env.device
		device.LinxAgentID = "agent_existing"
		if err := env.db.UpdatePortalDevice(context.Background(), device); err != nil {
			t.Fatal(err)
		}
		execRaw(t, env.dbPath, `CREATE TRIGGER block_mcp_save BEFORE UPDATE ON portal_devices
			WHEN NEW.step = 'attach_mcp' BEGIN SELECT RAISE(ABORT, 'read-only'); END`)
		if err := env.provisioner.Run(context.Background(), device.ID); err == nil {
			t.Fatal("Run ignored the mcp save failure")
		}
	})
	t.Run("mcp settings unreadable", func(t *testing.T) {
		env := newProvisionTestEnv(t, &fakePlatform{}, &fakeOTA{immediate: true})
		device := env.device
		device.LinxAgentID = "agent_existing"
		if err := env.db.UpdatePortalDevice(context.Background(), device); err != nil {
			t.Fatal(err)
		}
		execRaw(t, env.dbPath, "ALTER TABLE portal_settings RENAME TO portal_settings_hidden")
		if err := env.provisioner.Run(context.Background(), device.ID); err == nil {
			t.Fatal("Run ignored an unreadable MCP setting")
		}
	})
	t.Run("mcp platform fails", func(t *testing.T) {
		env := newProvisionTestEnv(t, &fakePlatform{failMCP: true}, &fakeOTA{immediate: true})
		device := env.device
		device.LinxAgentID = "agent_existing"
		if err := env.db.UpdatePortalDevice(context.Background(), device); err != nil {
			t.Fatal(err)
		}
		if err := env.provisioner.Run(context.Background(), device.ID); err == nil {
			t.Fatal("Run ignored a failing MCP platform")
		}
	})
	t.Run("mcp setting save fails", func(t *testing.T) {
		env := newProvisionTestEnv(t, &fakePlatform{}, &fakeOTA{immediate: true})
		device := env.device
		device.LinxAgentID = "agent_existing"
		if err := env.db.UpdatePortalDevice(context.Background(), device); err != nil {
			t.Fatal(err)
		}
		execRaw(t, env.dbPath, `CREATE TRIGGER block_mcp_setting BEFORE INSERT ON portal_settings
			BEGIN SELECT RAISE(ABORT, 'read-only'); END`)
		if err := env.provisioner.Run(context.Background(), device.ID); err == nil {
			t.Fatal("Run ignored a failed MCP setting write")
		}
	})
	t.Run("attach fails", func(t *testing.T) {
		env := newProvisionTestEnv(t, &fakePlatform{failAttach: true}, &fakeOTA{immediate: true})
		device := env.device
		device.LinxAgentID = "agent_existing"
		if err := env.db.UpdatePortalDevice(context.Background(), device); err != nil {
			t.Fatal(err)
		}
		if err := env.provisioner.Run(context.Background(), device.ID); err == nil {
			t.Fatal("Run ignored an attach failure")
		}
	})
	t.Run("ota call fails", func(t *testing.T) {
		env := newProvisionTestEnv(t, &fakePlatform{}, &fakeOTA{failReport: true})
		prepareDeviceThroughMCP(t, env)
		if err := env.provisioner.Run(context.Background(), env.device.ID); err == nil {
			t.Fatal("Run ignored an OTA failure")
		}
	})
	t.Run("ota returns no activation", func(t *testing.T) {
		env := newProvisionTestEnv(t, &fakePlatform{}, &fakeOTA{noActivation: true})
		prepareDeviceThroughMCP(t, env)
		err := env.provisioner.Run(context.Background(), env.device.ID)
		if err == nil || !strings.Contains(err.Error(), "没有返回激活码") {
			t.Fatalf("Run err = %v", err)
		}
	})
	t.Run("bind is rejected", func(t *testing.T) {
		env := newProvisionTestEnv(t, &fakePlatform{failBind: true}, &fakeOTA{})
		prepareDeviceThroughMCP(t, env)
		err := env.provisioner.Run(context.Background(), env.device.ID)
		if err == nil || !strings.Contains(err.Error(), "激活码或智能体无效") {
			t.Fatalf("Run err = %v", err)
		}
	})
	t.Run("token poll fails", func(t *testing.T) {
		env := newProvisionTestEnv(t, &fakePlatform{}, &fakeOTA{failAfterOne: true})
		prepareDeviceThroughMCP(t, env)
		if err := env.provisioner.Run(context.Background(), env.device.ID); err == nil {
			t.Fatal("Run ignored a failed token poll")
		}
	})
	t.Run("credential save fails", func(t *testing.T) {
		occupied := filepath.Join(t.TempDir(), "occupied")
		if err := os.WriteFile(occupied, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		env := newProvisionTestEnv(t, &fakePlatform{}, &fakeOTA{immediate: true})
		env.provisioner.cfg.DataDir = filepath.Join(occupied, "data")
		prepareDeviceThroughMCP(t, env)
		err := env.provisioner.Run(context.Background(), env.device.ID)
		if err == nil || !strings.Contains(err.Error(), "保存设备凭证失败") {
			t.Fatalf("Run err = %v", err)
		}
	})
	t.Run("registration save fails", func(t *testing.T) {
		env := newProvisionTestEnv(t, &fakePlatform{}, &fakeOTA{immediate: true})
		prepareDeviceThroughMCP(t, env)
		execRaw(t, env.dbPath, `CREATE TRIGGER block_registered BEFORE UPDATE ON portal_devices
			WHEN NEW.state = 'registered' BEGIN SELECT RAISE(ABORT, 'read-only'); END`)
		if err := env.provisioner.Run(context.Background(), env.device.ID); err == nil {
			t.Fatal("Run ignored a failed registration save")
		}
		device := loadProvisionDevice(t, env, env.device.ID)
		if device.State == store.PortalDeviceRegistered {
			t.Fatalf("device=%+v", device)
		}
	})
	t.Run("provider fails", func(t *testing.T) {
		env := newProvisionTestEnv(t, &fakePlatform{}, &fakeOTA{immediate: true})
		env.provisioner.linx = func(context.Context) (*linx.Client, error) {
			return nil, errors.New("provider down")
		}
		err := env.provisioner.Run(context.Background(), env.device.ID)
		if err == nil || !strings.Contains(err.Error(), "provider down") {
			t.Fatalf("Run err = %v", err)
		}
	})
	t.Run("platform is not configured", func(t *testing.T) {
		env := newProvisionTestEnv(t, &fakePlatform{}, &fakeOTA{immediate: true})
		env.provisioner.linx = func(context.Context) (*linx.Client, error) { return nil, nil }
		err := env.provisioner.Run(context.Background(), env.device.ID)
		if err == nil || !strings.Contains(err.Error(), deviceErrorCodePlatformNotConfigured) {
			t.Fatalf("Run err = %v", err)
		}
	})
}

func prepareDeviceThroughMCP(t *testing.T, env *provisionTestEnv) {
	t.Helper()
	ctx := context.Background()
	device := env.device
	device.LinxAgentID = "agent_existing"
	if err := env.db.UpdatePortalDevice(ctx, device); err != nil {
		t.Fatal(err)
	}
	if err := env.db.SetPortalSetting(ctx, settingMCPServer, "server_1"); err != nil {
		t.Fatal(err)
	}
	if err := env.db.SetPortalSetting(ctx, settingMCPInstance, "mcp_1"); err != nil {
		t.Fatal(err)
	}
}

func loadProvisionDevice(t *testing.T, env *provisionTestEnv, id string) store.PortalDevice {
	t.Helper()
	device, err := env.db.PortalDeviceByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return device
}

func TestProvisionCancelsTheTokenPoll(t *testing.T) {
	ota := &fakeOTA{}
	env := newProvisionTestEnv(t, &fakePlatform{}, ota)
	prepareDeviceThroughMCP(t, env)
	env.provisioner.cfg.TokenPollInterval = 200 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- env.provisioner.Run(ctx, env.device.ID) }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ota.mu.Lock()
		reports := ota.activationN
		ota.mu.Unlock()
		if reports >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
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
	device := loadProvisionDevice(t, env, env.device.ID)
	if device.State == store.PortalDeviceRegistered {
		t.Fatalf("device=%+v", device)
	}
}

func TestProvisionRecoverBranches(t *testing.T) {
	t.Run("device list fails", func(t *testing.T) {
		env := newProvisionTestEnv(t, &fakePlatform{}, &fakeOTA{immediate: true})
		execRaw(t, env.dbPath, "ALTER TABLE portal_devices RENAME TO portal_devices_hidden")
		if err := env.provisioner.Recover(context.Background()); err == nil {
			t.Fatal("Recover ignored a device list failure")
		}
	})
	t.Run("missing credential marks the device as failed", func(t *testing.T) {
		env := newProvisionTestEnv(t, &fakePlatform{}, &fakeOTA{immediate: true})
		device := env.device
		device.State = store.PortalDeviceRegistered
		device.Step = stepDone
		if err := env.db.UpdatePortalDevice(context.Background(), device); err != nil {
			t.Fatal(err)
		}
		if err := env.provisioner.Recover(context.Background()); err != nil {
			t.Fatalf("Recover: %v", err)
		}
		loaded := loadProvisionDevice(t, env, device.ID)
		if loaded.State != store.PortalDeviceError || loaded.Step != stepToken || !strings.Contains(loaded.Error, "凭证") {
			t.Fatalf("device=%+v", loaded)
		}
	})
	t.Run("credential repair save fails", func(t *testing.T) {
		env := newProvisionTestEnv(t, &fakePlatform{}, &fakeOTA{immediate: true})
		device := env.device
		device.State = store.PortalDeviceRegistered
		if err := env.db.UpdatePortalDevice(context.Background(), device); err != nil {
			t.Fatal(err)
		}
		execRaw(t, env.dbPath, `CREATE TRIGGER block_error_state BEFORE UPDATE ON portal_devices
			WHEN NEW.state = 'error' BEGIN SELECT RAISE(ABORT, 'read-only'); END`)
		if err := env.provisioner.Recover(context.Background()); err == nil {
			t.Fatal("Recover ignored a failed repair save")
		}
	})
	t.Run("credential promotes a provisioning device", func(t *testing.T) {
		env := newProvisionTestEnv(t, &fakePlatform{}, &fakeOTA{immediate: true})
		writeCredential(t, env.dataDir, env.device.ID)
		if err := env.provisioner.Recover(context.Background()); err != nil {
			t.Fatalf("Recover: %v", err)
		}
		loaded := loadProvisionDevice(t, env, env.device.ID)
		if loaded.State != store.PortalDeviceRegistered || loaded.RegisteredAt == nil {
			t.Fatalf("device=%+v", loaded)
		}
	})
	t.Run("resume respects a canceled root context", func(t *testing.T) {
		env := newProvisionTestEnv(t, &fakePlatform{}, &fakeOTA{immediate: true})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		env.provisioner.ctx = ctx
		if err := env.provisioner.Recover(context.Background()); err != nil {
			t.Fatalf("Recover: %v", err)
		}
		deadline := time.Now().Add(time.Second)
		for env.provisioner.Running(env.device.ID) && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if env.provisioner.Running(env.device.ID) {
			t.Fatal("the canceled resume is still marked running")
		}
	})
}

func TestSaveCredentialFailureBranches(t *testing.T) {
	occupied := filepath.Join(t.TempDir(), "occupied")
	if err := os.WriteFile(occupied, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := saveCredential(filepath.Join(occupied, "dir", "device.json"), devicelinx.Device{MAC: "02:00:00:00:00:01"}); err == nil {
		t.Fatal("saveCredential created a directory under a regular file")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "device.json")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := saveCredential(target, devicelinx.Device{MAC: "02:00:00:00:00:01"}); err == nil {
		t.Fatal("saveCredential replaced a directory")
	}
}

func TestStartVoiceBranches(t *testing.T) {
	env := newProvisionTestEnv(t, &fakePlatform{}, &fakeOTA{immediate: true})
	ctx := context.Background()
	device := env.device
	device.State = store.PortalDeviceRegistered

	provisioner := env.provisioner
	provisioner.voice = nil
	if err := provisioner.startVoice(ctx, device); err != nil {
		t.Fatalf("nil voice manager returned %v", err)
	}
	provisioner.voice = env.voice

	if err := provisioner.startVoice(ctx, device); err == nil {
		t.Fatal("a registered device started without credentials")
	}

	other := newProvisionTestEnv(t, &fakePlatform{}, &fakeOTA{immediate: true})
	insertOrphanDevice(t, other.dbPath, "dev_orphan")
	if err := other.provisioner.startVoice(ctx, loadProvisionDevice(t, other, "dev_orphan")); err != nil {
		t.Fatalf("missing owner: %v", err)
	}

	disabled := newProvisionTestEnv(t, &fakePlatform{}, &fakeOTA{immediate: true})
	if err := disabled.db.SetPortalUserStatus(ctx, "usr_1", store.PortalUserDisabled, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := disabled.provisioner.startVoice(ctx, disabled.device); err != nil {
		t.Fatalf("disabled owner: %v", err)
	}
}
