package main

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/config"
	"github.com/open-anywhere/open-anywhere/internal/gateway/secret"
	"github.com/open-anywhere/open-anywhere/internal/gateway/settings"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

func portalTestConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := testConfig(t)
	cfg.PortalEnabled = true
	cfg.PortalDataDir = filepath.Join(t.TempDir(), "portal")
	cfg.PortalUsername = "admin"
	cfg.PortalPassword = "password123"
	cfg.PortalSessionTTL = time.Hour
	cfg.PortalAllowRegistration = true
	cfg.MCPToken = "mcp-token"
	cfg.LinxPublicMCPURL = "http://gateway.example/sse"
	return cfg
}

func freeAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

// startGateway runs serve in the background and returns an idempotent stop
// function that cancels the context and reports the serve result.
func startGateway(t *testing.T, cfg config.Config) func() error {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, cfg) }()
	var once sync.Once
	var result error
	stop := func() error {
		once.Do(func() {
			cancel()
			select {
			case result = <-done:
			case <-time.After(10 * time.Second):
				result = errors.New("serve did not stop")
			}
		})
		return result
	}
	t.Cleanup(func() { _ = stop() })
	return stop
}

func waitHealthy(t *testing.T, address string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		response, err := http.Get("http://" + address + "/healthz")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("gateway at %s did not become healthy", address)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitApp(t *testing.T, path, id string, match func(app.App) bool) app.App {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if value := readApp(t, path, id); value != nil && match(*value) {
			return *value
		}
		if time.Now().After(deadline) {
			t.Fatalf("application %s did not reach the expected state: %+v", id, readApp(t, path, id))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// migratedRaw opens the database once to apply migrations, then returns a raw
// connection for fault injection.
func migratedRaw(t *testing.T, path string) *sql.DB {
	t.Helper()
	database, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return rawDatabase(t, path)
}

func TestPortalStartsAndSeedsSettings(t *testing.T) {
	cfg := portalTestConfig(t)
	cfg.Addr = freeAddress(t)
	stop := startGateway(t, cfg)
	waitHealthy(t, cfg.Addr)

	response, err := http.Get("http://" + cfg.Addr + "/healthz")
	if err != nil {
		t.Fatalf("healthz: %v", err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), `"status":"ok"`) {
		t.Fatalf("healthz = %d %s", response.StatusCode, body)
	}

	response, err = http.Get("http://" + cfg.Addr + "/api/portal/me")
	if err != nil {
		t.Fatalf("portal me: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("portal me status = %d, want 401", response.StatusCode)
	}

	response, err = http.Get("http://" + cfg.Addr + "/mcp")
	if err != nil {
		t.Fatalf("mcp: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("mcp without token status = %d, want 401", response.StatusCode)
	}

	if _, err := os.Stat(filepath.Join(cfg.PortalDataDir, secret.KeyFile)); err != nil {
		t.Fatalf("master key was not created: %v", err)
	}

	raw := rawDatabase(t, cfg.DBPath)
	rows, err := raw.Query(`SELECT key, value, is_secret FROM platform_settings`)
	if err != nil {
		t.Fatalf("query settings: %v", err)
	}
	type setting struct {
		value  string
		secret bool
	}
	values := map[string]setting{}
	for rows.Next() {
		var key, value string
		var isSecret int
		if err := rows.Scan(&key, &value, &isSecret); err != nil {
			t.Fatalf("scan setting: %v", err)
		}
		values[key] = setting{value: value, secret: isSecret == 1}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("settings rows: %v", err)
	}
	_ = rows.Close()
	if got := values[settings.KeyAgentAPIBaseURL]; got.value != cfg.AgentAPIURL || got.secret {
		t.Fatalf("agentapi base = %+v", got)
	}
	if got := values[settings.KeyAgentAPIKey]; !got.secret || !strings.HasPrefix(got.value, "enc:v1:") {
		t.Fatalf("agentapi key = %+v", got)
	}
	if got := values[settings.KeyAgentAPIAgentVersion]; got.value != "1" {
		t.Fatalf("agent version = %+v", got)
	}
	if got := values[settings.KeyRegistrationOpen]; got.value != "true" {
		t.Fatalf("registration = %+v", got)
	}

	var role, status string
	if err := raw.QueryRow(`SELECT role, status FROM portal_users WHERE username = 'admin'`).Scan(&role, &status); err != nil {
		t.Fatalf("admin user: %v", err)
	}
	if role != store.PortalRoleAdmin || status != store.PortalUserActive {
		t.Fatalf("admin role=%q status=%q", role, status)
	}

	if err := stop(); err != nil {
		t.Fatalf("serve: %v", err)
	}
}

func serveError(t *testing.T, cfg config.Config) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return serve(ctx, cfg)
}

func TestPortalStartupFailures(t *testing.T) {
	t.Run("encrypted secrets need a key", func(t *testing.T) {
		t.Setenv(secret.EnvKey, "")
		cfg := portalTestConfig(t)
		raw := migratedRaw(t, cfg.DBPath)
		if _, err := raw.Exec(`INSERT INTO platform_settings (key, value, is_secret, updated_at, updated_by)
			VALUES (?, 'enc:v1:payload', 1, '2026-01-01T00:00:00Z', '')`, settings.KeyAgentAPIKey); err != nil {
			t.Fatalf("insert secret: %v", err)
		}
		err := serveError(t, cfg)
		if err == nil || !strings.Contains(err.Error(), "encrypted but no master key") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("encrypted secret check fails", func(t *testing.T) {
		t.Setenv(secret.EnvKey, "")
		cfg := portalTestConfig(t)
		raw := migratedRaw(t, cfg.DBPath)
		if _, err := raw.Exec(`DROP TABLE platform_settings`); err != nil {
			t.Fatalf("drop settings: %v", err)
		}
		if _, err := raw.Exec(`CREATE TABLE platform_settings (key TEXT)`); err != nil {
			t.Fatalf("create broken settings: %v", err)
		}
		err := serveError(t, cfg)
		if err == nil || !strings.Contains(err.Error(), "check encrypted secrets") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("master key is malformed", func(t *testing.T) {
		t.Setenv(secret.EnvKey, "")
		cfg := portalTestConfig(t)
		if err := os.MkdirAll(cfg.PortalDataDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cfg.PortalDataDir, secret.KeyFile), []byte("!!!"), 0o600); err != nil {
			t.Fatal(err)
		}
		err := serveError(t, cfg)
		if err == nil || !strings.Contains(err.Error(), "not valid base64") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("seed is rejected by the database", func(t *testing.T) {
		t.Setenv(secret.EnvKey, "")
		cfg := portalTestConfig(t)
		raw := migratedRaw(t, cfg.DBPath)
		if _, err := raw.Exec(`CREATE TRIGGER refuse_seed BEFORE INSERT ON platform_settings
			BEGIN SELECT RAISE(ABORT, 'seed blocked'); END`); err != nil {
			t.Fatalf("create trigger: %v", err)
		}
		err := serveError(t, cfg)
		if err == nil || !strings.Contains(err.Error(), "seed blocked") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("stored agent version is invalid", func(t *testing.T) {
		t.Setenv(secret.EnvKey, "")
		cfg := portalTestConfig(t)
		raw := migratedRaw(t, cfg.DBPath)
		if _, err := raw.Exec(`INSERT INTO platform_settings (key, value, is_secret, updated_at, updated_by)
			VALUES (?, 'not-a-number', 0, '2026-01-01T00:00:00Z', '')`, settings.KeyAgentAPIAgentVersion); err != nil {
			t.Fatalf("insert version: %v", err)
		}
		err := serveError(t, cfg)
		if err == nil || !strings.Contains(err.Error(), "invalid "+settings.KeyAgentAPIAgentVersion) {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("platform secret cannot be decrypted", func(t *testing.T) {
		t.Setenv(secret.EnvKey, "")
		cfg := portalTestConfig(t)
		if _, err := secret.Load(cfg.PortalDataDir, func(string) string { return "" }); err != nil {
			t.Fatalf("generate key: %v", err)
		}
		foreignDir := t.TempDir()
		foreign, err := secret.Load(foreignDir, func(string) string { return "" })
		if err != nil {
			t.Fatalf("foreign key: %v", err)
		}
		ciphertext, err := foreign.Encrypt("api-key", secret.PlatformAAD(settings.KeyAgentAPIKey))
		if err != nil {
			t.Fatalf("encrypt: %v", err)
		}
		raw := migratedRaw(t, cfg.DBPath)
		if _, err := raw.Exec(`INSERT INTO platform_settings (key, value, is_secret, updated_at, updated_by)
			VALUES (?, ?, 1, '2026-01-01T00:00:00Z', '')`, settings.KeyAgentAPIKey, ciphertext); err != nil {
			t.Fatalf("insert secret: %v", err)
		}
		err = serveError(t, cfg)
		if err == nil || !errors.Is(err, secret.ErrDecrypt) || !strings.Contains(err.Error(), "cannot be decrypted") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("service binding secret cannot be decrypted", func(t *testing.T) {
		t.Setenv(secret.EnvKey, "")
		cfg := portalTestConfig(t)
		if _, err := secret.Load(cfg.PortalDataDir, func(string) string { return "" }); err != nil {
			t.Fatalf("generate key: %v", err)
		}
		foreign, err := secret.Load(t.TempDir(), func(string) string { return "" })
		if err != nil {
			t.Fatalf("foreign key: %v", err)
		}
		ciphertext, err := foreign.Encrypt("render-token", secret.BindingAAD("usr_1", "render"))
		if err != nil {
			t.Fatalf("encrypt: %v", err)
		}
		database, err := store.Open(cfg.DBPath)
		if err != nil {
			t.Fatalf("store.Open: %v", err)
		}
		ctx := context.Background()
		now := time.Now().UTC()
		if err := database.CreatePortalUser(ctx, store.PortalUser{
			ID: "usr_1", Username: "alice", PasswordHash: "x", CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("create user: %v", err)
		}
		if err := database.UpsertServiceBinding(ctx, store.PortalServiceBinding{
			UserID: "usr_1", RenderTokenEnc: ciphertext, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("upsert binding: %v", err)
		}
		_ = database.Close()
		err = serveError(t, cfg)
		if err == nil || !errors.Is(err, secret.ErrDecrypt) || !strings.Contains(err.Error(), "service bindings cannot be decrypted") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("portal configuration is invalid", func(t *testing.T) {
		cfg := portalTestConfig(t)
		cfg.PortalUsername = "BAD!"
		err := serveError(t, cfg)
		if err == nil || !strings.Contains(err.Error(), "configured username") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("device recovery cannot scan", func(t *testing.T) {
		cfg := portalTestConfig(t)
		raw := migratedRaw(t, cfg.DBPath)
		if _, err := raw.Exec(`INSERT INTO portal_devices
			(id, user_id, kind, mac, client_id, linx_agent_id, agent_name, state, step, error, credential_path, created_at, updated_at, registered_at)
			VALUES ('dev_1', 'usr_1', 'hardware', 'mac-1', 'client-1', '', '', 'registered', '', '', '', '2026-01-01T00:00:00Z', 'not-a-time', NULL)`); err != nil {
			t.Fatalf("insert device: %v", err)
		}
		err := serveError(t, cfg)
		if err == nil || !strings.Contains(err.Error(), "scan portal device") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestPortalResumesBuildWithUnreadyPlatform(t *testing.T) {
	cfg := portalTestConfig(t)
	cfg.Addr = freeAddress(t)
	cfg.AgentAPIURL = ""
	cfg.AgentAPIKey = ""
	cfg.AgentID = ""
	cfg.AgentVersion = 0
	cfg.EnvironmentID = ""
	seedStore(t, cfg.DBPath, app.App{
		ID: "build1", State: app.StateBuilding, SessionID: "ses_1",
		Owner: app.Identity{DeviceID: "dev", SessionID: "sess"},
	})

	stop := startGateway(t, cfg)
	waitHealthy(t, cfg.Addr)
	resumed := waitApp(t, cfg.DBPath, "build1", func(value app.App) bool {
		return value.State == app.StateError
	})
	if resumed.Error == nil || resumed.Error.Message != "无法恢复构建" {
		t.Fatalf("resume error = %+v", resumed.Error)
	}
	if err := stop(); err != nil {
		t.Fatalf("serve: %v", err)
	}
}

func TestPortalResumesBuildWithReadyPlatform(t *testing.T) {
	var archives atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/sessions/ses_1/events/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: message\ndata: {\"id\":\"e1\",\"type\":\"session.status_terminated\"}\n\n")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	})
	mux.HandleFunc("GET /v1/sessions/ses_1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"ses_1","status":"terminated"}`)
	})
	mux.HandleFunc("POST /v1/sessions/ses_1/archive", func(w http.ResponseWriter, r *http.Request) {
		archives.Add(1)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"ses_1","status":"terminated"}`)
	})
	agentAPI := httptest.NewServer(mux)
	defer agentAPI.Close()

	cfg := portalTestConfig(t)
	cfg.Addr = freeAddress(t)
	cfg.AgentAPIURL = agentAPI.URL
	cfg.AgentAPIKey = "agent-key"
	cfg.AgentID = "agent_1"
	cfg.AgentVersion = 3
	cfg.EnvironmentID = "env_1"
	seedStore(t, cfg.DBPath, app.App{
		ID: "build1", State: app.StateBuilding, SessionID: "ses_1",
		Owner: app.Identity{DeviceID: "dev", SessionID: "sess"},
	})

	stop := startGateway(t, cfg)
	waitHealthy(t, cfg.Addr)
	resumed := waitApp(t, cfg.DBPath, "build1", func(value app.App) bool {
		return value.State == app.StateError
	})
	if resumed.Error == nil || resumed.Error.Message != "构建会话已终止" {
		t.Fatalf("resume error = %+v", resumed.Error)
	}
	deadline := time.Now().Add(5 * time.Second)
	for archives.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if archives.Load() == 0 {
		t.Fatal("the terminated session was not archived")
	}
	if err := stop(); err != nil {
		t.Fatalf("serve: %v", err)
	}
}

// gatewayMCPHeaders authenticates the MCP client and carries Linx identity.
type gatewayMCPHeaders struct {
	base  http.RoundTripper
	token string
}

func (h gatewayMCPHeaders) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header.Set("Authorization", "Bearer "+h.token)
	request.Header.Set("X-Linx-Device-Id", "device")
	request.Header.Set("X-Linx-Agent-Id", "agent")
	request.Header.Set("X-Linx-Session-Id", "session")
	return h.base.RoundTrip(request)
}

func TestPortalCreateAppReportsPlatformSnapshotFailure(t *testing.T) {
	var agentRequests atomic.Int32
	agentAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agentRequests.Add(1)
		http.Error(w, "unexpected call", http.StatusInternalServerError)
	}))
	defer agentAPI.Close()

	cfg := portalTestConfig(t)
	cfg.Addr = freeAddress(t)
	cfg.AgentAPIURL = agentAPI.URL
	cfg.AgentAPIKey = "agent-key"
	cfg.AgentID = "agent_1"
	cfg.AgentVersion = 3
	cfg.EnvironmentID = "env_1"
	stop := startGateway(t, cfg)
	waitHealthy(t, cfg.Addr)

	raw := rawDatabase(t, cfg.DBPath)
	var adminID string
	if err := raw.QueryRow(`SELECT id FROM portal_users WHERE username = 'admin'`).Scan(&adminID); err != nil {
		t.Fatalf("admin id: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO portal_devices
		(id, user_id, kind, mac, client_id, linx_agent_id, agent_name, state, step, error, credential_path, created_at, updated_at, registered_at)
		VALUES ('dev_1', ?, 'hardware', 'device', 'client-1', 'agent', '客厅', ?, '', '', '', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', NULL)`,
		adminID, store.PortalDeviceRegistered); err != nil {
		t.Fatalf("insert device: %v", err)
	}
	if _, err := raw.Exec(`UPDATE platform_settings SET value = 'not-a-number' WHERE key = ?`,
		settings.KeyAgentAPIAgentVersion); err != nil {
		t.Fatalf("break settings: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := sdk.NewClient(&sdk.Implementation{Name: "gateway-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &sdk.StreamableClientTransport{
		Endpoint: "http://" + cfg.Addr + "/mcp",
		HTTPClient: &http.Client{Transport: gatewayMCPHeaders{
			base: http.DefaultTransport, token: cfg.MCPToken,
		}},
	}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	result, err := session.CallTool(ctx, &sdk.CallToolParams{
		Name:      "create-app",
		Arguments: map[string]any{"prompt": "做一个小游戏"},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !result.IsError {
		t.Fatalf("create-app unexpectedly succeeded: %+v", result)
	}
	text := ""
	for _, content := range result.Content {
		if block, ok := content.(*sdk.TextContent); ok {
			text += block.Text
		}
	}
	if !strings.Contains(text, "无法启动构建") {
		t.Fatalf("tool error = %q", text)
	}
	if agentRequests.Load() != 0 {
		t.Fatalf("agent API was called %d times without valid platform settings", agentRequests.Load())
	}
	var apps int
	if err := raw.QueryRow(`SELECT COUNT(*) FROM apps`).Scan(&apps); err != nil {
		t.Fatalf("count apps: %v", err)
	}
	if apps != 0 {
		t.Fatalf("failed creation left %d applications", apps)
	}
	if err := stop(); err != nil {
		t.Fatalf("serve: %v", err)
	}
}
