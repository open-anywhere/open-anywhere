package portal

import (
	"runtime"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/linx"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
	devicelinx "github.com/open-anywhere/open-anywhere/internal/voiceentry/linx"
)

type fakePlatform struct {
	mu        sync.Mutex
	agents    int
	binds     int
	joins     int
	failAgent bool
	failBind  bool
	servers   []map[string]any
	instances []map[string]any
}

func (f *fakePlatform) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/xiaozhi/agent":
			if f.failAgent {
				http.Error(w, `{"code":500,"msg":"agent creation failed"}`, http.StatusInternalServerError)
				return
			}
			f.agents++
			_, _ = io.WriteString(w, `{"code":0,"data":"agent_1"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/mcp-store/servers":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"servers": f.servers}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/mcp-store/servers":
			f.servers = append(f.servers, map[string]any{
				"id": "server_1", "name": "Open Anywhere",
				"config_schema": map[string]any{"sse": map[string]any{"url": "https://gateway.example/sse"}},
			})
			_, _ = io.WriteString(w, `{"code":0,"data":{"id":"server_1"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/mcps":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": f.instances})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/mcps":
			f.instances = append(f.instances, map[string]any{"id": "mcp_1", "mcp_id": "server_1", "name": "Open Anywhere"})
			_, _ = io.WriteString(w, `{"code":0,"data":{"id":"mcp_1"}}`)
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/v1/agents/"):
			f.joins++
			_, _ = io.WriteString(w, `{"code":0}`)
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/xiaozhi/device/bind/"):
			if f.failBind {
				_, _ = io.WriteString(w, `{"code":400,"msg":"invalid activation code"}`)
				return
			}
			f.binds++
			_, _ = io.WriteString(w, `{"code":0}`)
		default:
			t.Errorf("unexpected platform request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	})
}

type fakeOTA struct {
	mu          sync.Mutex
	immediate   bool
	afterBind   bool
	platform    *fakePlatform
	activationN int
}

func (f *fakeOTA) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Method != http.MethodPost {
			t.Errorf("OTA request %s %s", r.Method, r.URL.Path)
		}
		f.activationN++
		bound := false
		if f.platform != nil {
			f.platform.mu.Lock()
			bound = f.platform.binds > 0
			f.platform.mu.Unlock()
		}
		switch {
		case f.immediate:
			_, _ = io.WriteString(w, `{"websocket":{"url":"ws://127.0.0.1:1/ws","token":"device-token"}}`)
		case f.afterBind && bound:
			_, _ = io.WriteString(w, `{"websocket":{"url":"ws://127.0.0.1:1/ws","token":"device-token"}}`)
		default:
			_, _ = io.WriteString(w, `{"activation":{"code":"123456"}}`)
		}
	})
}

func newProvisionEnv(t *testing.T, platform *fakePlatform, ota *fakeOTA) (*Provisioner, *store.DB, store.PortalDevice, *VoiceManager, string) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "portal.db"))
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
	return provisioner, db, device, voice, dataDir
}

func loadDevice(t *testing.T, db *store.DB) store.PortalDevice {
	t.Helper()
	device, err := db.PortalDeviceByID(context.Background(), "dev_1")
	if err != nil {
		t.Fatal(err)
	}
	return device
}

func TestProvisionRegistersAVirtualDeviceAndStoresACredential(t *testing.T) {
	platform := &fakePlatform{}
	ota := &fakeOTA{immediate: true}
	provisioner, db, device, _, dataDir := newProvisionEnv(t, platform, ota)

	if err := provisioner.Run(context.Background(), device.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	loaded := loadDevice(t, db)
	if loaded.State != store.PortalDeviceRegistered || loaded.Step != stepDone ||
		loaded.LinxAgentID != "agent_1" || loaded.AgentName != "Open Anywhere · alice" ||
		loaded.RegisteredAt == nil || loaded.Error != "" {
		t.Fatalf("device=%+v", loaded)
	}
	if platform.agents != 1 || platform.binds != 0 || platform.joins != 1 {
		t.Fatalf("platform calls agents=%d binds=%d joins=%d", platform.agents, platform.binds, platform.joins)
	}
	path := filepath.Join(dataDir, "devices", "dev_1.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("credential: %v", err)
	}
	// Windows has no Unix permission bits; Go always reports 0666 there.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("credential mode=%o", info.Mode().Perm())
	}
	credential, err := devicelinx.LoadDevice(path)
	if err != nil || credential.Token != "device-token" || credential.MAC != device.MAC {
		t.Fatalf("credential=%+v err=%v", credential, err)
	}
	if loaded.CredentialPath != filepath.Join("devices", "dev_1.json") {
		t.Fatalf("credential path=%q", loaded.CredentialPath)
	}
}

func TestProvisionBindsAnActivationCodeAndPollsForTheToken(t *testing.T) {
	platform := &fakePlatform{}
	ota := &fakeOTA{afterBind: true, platform: platform}
	provisioner, db, device, _, _ := newProvisionEnv(t, platform, ota)

	if err := provisioner.Run(context.Background(), device.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	loaded := loadDevice(t, db)
	if loaded.State != store.PortalDeviceRegistered || platform.binds != 1 {
		t.Fatalf("device=%+v binds=%d", loaded, platform.binds)
	}
	if ota.activationN < 2 {
		t.Fatalf("the provisioner did not poll for the token: reports=%d", ota.activationN)
	}
}

func TestProvisionRecordsFailuresAtTheStepThatFailed(t *testing.T) {
	platform := &fakePlatform{failAgent: true}
	ota := &fakeOTA{immediate: true}
	provisioner, db, device, _, _ := newProvisionEnv(t, platform, ota)

	if err := provisioner.Run(context.Background(), device.ID); err == nil {
		t.Fatal("Run succeeded although agent creation failed")
	}
	loaded := loadDevice(t, db)
	if loaded.State != store.PortalDeviceError || loaded.Step != stepCreateAgent || loaded.Error == "" {
		t.Fatalf("device=%+v", loaded)
	}
	if strings.Contains(loaded.Error, "secret-key") || strings.Contains(loaded.Error, "mcp-token") {
		t.Fatalf("a secret leaked into the device error: %q", loaded.Error)
	}
}

func TestProvisionTimesOutWhenTheTokenNeverArrives(t *testing.T) {
	platform := &fakePlatform{}
	ota := &fakeOTA{}
	provisioner, db, device, _, _ := newProvisionEnv(t, platform, ota)

	if err := provisioner.Run(context.Background(), device.ID); err == nil {
		t.Fatal("Run succeeded although the token never arrived")
	}
	loaded := loadDevice(t, db)
	if loaded.State != store.PortalDeviceError || loaded.Step != stepToken ||
		!strings.Contains(loaded.Error, "超时") {
		t.Fatalf("device=%+v", loaded)
	}
	if platform.binds != 1 {
		t.Fatalf("binds=%d", platform.binds)
	}
}

func TestRecoverPromotesADeviceWhoseCredentialWasAlreadyWritten(t *testing.T) {
	platform := &fakePlatform{}
	ota := &fakeOTA{immediate: true}
	provisioner, db, device, _, dataDir := newProvisionEnv(t, platform, ota)
	credential := devicelinx.Device{MAC: device.MAC, ClientID: device.ClientID, Token: "device-token", WSURL: "ws://127.0.0.1:1/ws"}
	if err := saveCredential(filepath.Join(dataDir, "devices", "dev_1.json"), credential); err != nil {
		t.Fatal(err)
	}

	if err := provisioner.Recover(context.Background()); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	loaded := loadDevice(t, db)
	if loaded.State != store.PortalDeviceRegistered || loaded.Step != stepDone || loaded.RegisteredAt == nil {
		t.Fatalf("device=%+v", loaded)
	}
}
