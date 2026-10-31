package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-anywhere/open-anywhere/internal/gateway/secret"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

func newProvider(t *testing.T) (*Provider, *store.DB) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "gateway.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	box, err := secret.New(bytes.Repeat([]byte{3}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return New(db, box), db
}

func TestSeedAndSnapshot(t *testing.T) {
	provider, db := newProvider(t)
	ctx := context.Background()
	seeds := []Seed{
		{Key: KeyAgentAPIBaseURL, Value: "https://agent.example.com"},
		{Key: KeyAgentAPIKey, Value: "agent-secret"},
		{Key: KeyAgentAPIAgentID, Value: "agent-1"},
		{Key: KeyAgentAPIAgentVersion, Value: "3"},
		{Key: KeyAgentAPIEnvironmentID, Value: "env-1"},
		{Key: KeyLinxAPIBase, Value: "https://linx.example.com"},
		{Key: KeyLinxAPIKey, Value: "linx-secret"},
		{Key: KeyDefaultRenderURL, Value: "https://render.example.com"},
		{Key: KeyRegistrationOpen, Value: "true"},
	}
	if err := provider.Seed(ctx, seeds); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	snapshot, err := provider.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if !snapshot.AgentAPI.Ready() || !snapshot.Linx.Ready() || !snapshot.Services.Ready() || !snapshot.Registration.Open {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if snapshot.AgentAPI.APIKey != "agent-secret" || snapshot.AgentAPI.AgentVersion != 3 {
		t.Fatalf("agentapi = %+v", snapshot.AgentAPI)
	}

	// Secrets are encrypted at rest.
	rows, err := db.PlatformSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rows[KeyLinxAPIKey].Value, "enc:v1:") || strings.Contains(rows[KeyLinxAPIKey].Value, "linx-secret") {
		t.Fatalf("linx key at rest = %q", rows[KeyLinxAPIKey].Value)
	}

	// Seeding again must not overwrite administrator changes.
	if err := provider.Update(ctx, Patch{AgentAPI: &AgentAPIPatch{BaseURL: ptr("https://agent2.example.com")}}, "usr_admin"); err != nil {
		t.Fatal(err)
	}
	if err := provider.Seed(ctx, seeds); err != nil {
		t.Fatal(err)
	}
	snapshot, err = provider.Snapshot(ctx)
	if err != nil || snapshot.AgentAPI.BaseURL != "https://agent2.example.com" {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}

	masked, err := provider.Masked(ctx)
	if err != nil {
		t.Fatalf("Masked: %v", err)
	}
	if !masked.AgentAPI.APIKey.Configured || !masked.Linx.APIKey.Configured {
		t.Fatalf("masked = %+v", masked)
	}
	if masked.Services.RenderToken.Configured {
		t.Fatal("an unset token is reported as configured")
	}
	if !masked.Ready.AgentAPI || !masked.Ready.Linx || !masked.Ready.Services {
		t.Fatalf("ready = %+v", masked.Ready)
	}
}

func TestMaskedNeverReturnsSecretValues(t *testing.T) {
	provider, _ := newProvider(t)
	ctx := context.Background()
	if err := provider.Seed(ctx, []Seed{
		{Key: KeyAgentAPIKey, Value: "top-secret"},
		{Key: KeyLinxAPIKey, Value: "linx-secret"},
	}); err != nil {
		t.Fatal(err)
	}
	masked, err := provider.Masked(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if masked.AgentAPI.APIKey.Configured != true || masked.AgentAPI.APIKey.UpdatedAt == nil {
		t.Fatalf("api key state = %+v", masked.AgentAPI.APIKey)
	}
}

func TestUpdateKeepsSecretUnlessCleared(t *testing.T) {
	provider, _ := newProvider(t)
	ctx := context.Background()
	if err := provider.Update(ctx, Patch{Linx: &LinxPatch{APIKey: &SecretValue{Value: "first"}}}, "admin"); err != nil {
		t.Fatal(err)
	}
	// Editing an unrelated field with an empty secret keeps the existing key.
	if err := provider.Update(ctx, Patch{
		Linx: &LinxPatch{APIKey: &SecretValue{}, APIBase: ptr("https://linx.example.com")},
	}, "admin"); err != nil {
		t.Fatal(err)
	}
	snapshot, err := provider.Snapshot(ctx)
	if err != nil || snapshot.Linx.APIKey != "first" {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	if err := provider.Update(ctx, Patch{Linx: &LinxPatch{APIKey: &SecretValue{Clear: true}}}, "admin"); err != nil {
		t.Fatal(err)
	}
	snapshot, err = provider.Snapshot(ctx)
	if err != nil || snapshot.Linx.APIKey != "" {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
}

func TestUpdateValidatesURLsAndVersion(t *testing.T) {
	provider, _ := newProvider(t)
	provider.SetURLValidator(func(value string) error {
		parsed, err := url.Parse(value)
		if err != nil || parsed.Scheme != "https" {
			return errors.New("must be https")
		}
		return nil
	})
	ctx := context.Background()
	if err := provider.Update(ctx, Patch{Services: &ServicesPatch{RenderURL: ptr("http://render")}}, "admin"); !errors.Is(err, ErrInvalidURL) {
		t.Fatalf("err = %v", err)
	}
	negative := -1
	if err := provider.Update(ctx, Patch{AgentAPI: &AgentAPIPatch{AgentVersion: &negative}}, "admin"); err == nil {
		t.Fatal("a negative agent version was accepted")
	}
	if err := provider.Update(ctx, Patch{Services: &ServicesPatch{RenderURL: ptr("https://render")}}, "admin"); err != nil {
		t.Fatalf("valid URL rejected: %v", err)
	}
}

func TestLinxCredentialChangeClearsCachedMCPResources(t *testing.T) {
	provider, db := newProvider(t)
	ctx := context.Background()
	if err := db.SetPortalSetting(ctx, "linx.mcp_server_id", "server_1"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetPortalSetting(ctx, "linx.mcp_instance_id", "instance_1"); err != nil {
		t.Fatal(err)
	}
	if err := provider.Update(ctx, Patch{Linx: &LinxPatch{APIBase: ptr("https://linx.example.com")}}, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.PortalSetting(ctx, "linx.mcp_server_id"); err == nil {
		t.Fatal("the cached MCP server survived a Linx endpoint change")
	}
	if err := db.SetPortalSetting(ctx, "linx.mcp_server_id", "server_2"); err != nil {
		t.Fatal(err)
	}
	if err := provider.Update(ctx, Patch{Registration: &RegistrationPatch{Open: ptr(true)}}, "admin"); err != nil {
		t.Fatal(err)
	}
	if value, err := db.PortalSetting(ctx, "linx.mcp_server_id"); err != nil || value != "server_2" {
		t.Fatalf("unrelated updates cleared the MCP cache: %q %v", value, err)
	}
}

func TestSnapshotRejectsUndecryptableSecret(t *testing.T) {
	provider, db := newProvider(t)
	ctx := context.Background()
	if err := db.PutPlatformSettings(ctx, []store.PlatformSetting{
		{Key: KeyLinxAPIKey, Value: "enc:v1:bm90LXJlYWw=", IsSecret: true},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Snapshot(ctx); !errors.Is(err, secret.ErrDecrypt) {
		t.Fatalf("err = %v", err)
	}
}

func TestRegistrationOpenAndSnapshotDefaults(t *testing.T) {
	provider, _ := newProvider(t)
	ctx := context.Background()
	open, err := provider.RegistrationOpen(ctx)
	if err != nil || open {
		t.Fatalf("open=%v err=%v", open, err)
	}
	snapshot, err := provider.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.AgentAPI.Ready() || snapshot.Linx.Ready() || snapshot.Services.Ready() {
		t.Fatalf("empty snapshot is ready: %+v", snapshot)
	}
}

func TestServicesChanged(t *testing.T) {
	current := Snapshot{Services: ServiceDefaults{
		RenderURL:   "https://render.example.com",
		RenderToken: "render-secret",
	}}
	secret := func(value string) *SecretValue { return &SecretValue{Value: value} }
	cases := []struct {
		name  string
		patch *ServicesPatch
		want  bool
	}{
		{name: "no patch", patch: nil, want: false},
		{name: "empty patch", patch: &ServicesPatch{}, want: false},
		{name: "same urls", patch: &ServicesPatch{
			RenderURL: ptr("https://render.example.com"),
		}, want: false},
		{name: "same urls with surrounding space", patch: &ServicesPatch{
			RenderURL: ptr("  https://render.example.com  "),
		}, want: false},
		{name: "different render url", patch: &ServicesPatch{
			RenderURL: ptr("https://render2.example.com"),
		}, want: true},
		{name: "cleared render url", patch: &ServicesPatch{RenderURL: ptr("")}, want: true},
		{name: "same render token", patch: &ServicesPatch{RenderToken: secret("render-secret")}, want: false},
		{name: "empty token keeps the stored value", patch: &ServicesPatch{
			RenderToken: &SecretValue{},
		}, want: false},
		{name: "different render token", patch: &ServicesPatch{RenderToken: secret("other")}, want: true},
		{name: "clear an existing token", patch: &ServicesPatch{
			RenderToken: &SecretValue{Clear: true},
		}, want: true},
		{name: "clear an absent token", patch: &ServicesPatch{
			RenderToken: &SecretValue{Clear: true},
		}, want: false},
	}
	withoutRenderToken := current
	withoutRenderToken.Services.RenderToken = ""
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			snapshot := current
			if testCase.name == "clear an absent token" {
				snapshot = withoutRenderToken
			}
			if got := ServicesChanged(snapshot, testCase.patch); got != testCase.want {
				t.Fatalf("ServicesChanged() = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestUpdateClearsMCPCacheOnlyWhenLinxCredentialsChange(t *testing.T) {
	cases := []struct {
		name        string
		patch       Patch
		wantCleared bool
	}{
		{
			name:  "unchanged api base",
			patch: Patch{Linx: &LinxPatch{APIBase: ptr("https://linx.example.com")}},
		},
		{
			name:  "ota url only",
			patch: Patch{Linx: &LinxPatch{OTAURL: ptr("https://ota.example.com")}},
		},
		{
			name:  "agentapi only",
			patch: Patch{AgentAPI: &AgentAPIPatch{BaseURL: ptr("https://agent.example.com")}},
		},
		{
			name:  "registration switch only",
			patch: Patch{Registration: &RegistrationPatch{Open: ptr(true)}},
		},
		{
			name:  "same api key",
			patch: Patch{Linx: &LinxPatch{APIKey: &SecretValue{Value: "linx-key"}}},
		},
		{
			name:        "api key changed",
			patch:       Patch{Linx: &LinxPatch{APIKey: &SecretValue{Value: "linx-key-2"}}},
			wantCleared: true,
		},
		{
			name:        "api key cleared",
			patch:       Patch{Linx: &LinxPatch{APIKey: &SecretValue{Clear: true}}},
			wantCleared: true,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			provider, db := newProvider(t)
			ctx := context.Background()
			if err := provider.Update(ctx, Patch{Linx: &LinxPatch{
				APIBase: ptr("https://linx.example.com"), APIKey: &SecretValue{Value: "linx-key"},
			}}, "admin"); err != nil {
				t.Fatal(err)
			}
			setMCPCache(t, db)
			if err := provider.Update(ctx, testCase.patch, "admin"); err != nil {
				t.Fatalf("Update: %v", err)
			}
			assertMCPCache(t, db, !testCase.wantCleared)
		})
	}
}

func setMCPCache(t *testing.T, db *store.DB) {
	t.Helper()
	ctx := context.Background()
	if err := db.SetPortalSetting(ctx, "linx.mcp_server_id", "server_1"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetPortalSetting(ctx, "linx.mcp_instance_id", "instance_1"); err != nil {
		t.Fatal(err)
	}
}

func assertMCPCache(t *testing.T, db *store.DB, present bool) {
	t.Helper()
	ctx := context.Background()
	for _, key := range []string{"linx.mcp_server_id", "linx.mcp_instance_id"} {
		_, err := db.PortalSetting(ctx, key)
		if present && err != nil {
			t.Errorf("%s was cleared", key)
		}
		if !present && err == nil {
			t.Errorf("%s survived a Linx credential change", key)
		}
	}
}

func TestMaskedJSONOmitsActorAndSecrets(t *testing.T) {
	provider, _ := newProvider(t)
	ctx := context.Background()
	if err := provider.Update(ctx, Patch{
		AgentAPI: &AgentAPIPatch{
			BaseURL: ptr("https://agent.example.com"),
			APIKey:  &SecretValue{Value: "agentapi-plaintext-secret"},
		},
		Linx: &LinxPatch{
			APIBase: ptr("https://linx.example.com"),
			APIKey:  &SecretValue{Value: "linx-plaintext-secret"},
		},
		Services: &ServicesPatch{
			RenderURL:   ptr("https://render.example.com"),
			RenderToken: &SecretValue{Value: "render-plaintext-secret"},
		},
	}, "usr_admin"); err != nil {
		t.Fatalf("Update: %v", err)
	}
	masked, err := provider.Masked(ctx)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(masked)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, forbidden := range []string{
		"updated_by", "usr_admin",
		"agentapi-plaintext-secret", "linx-plaintext-secret",
		"render-plaintext-secret",
		"enc:v1:",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("masked settings leak %q: %s", forbidden, encoded)
		}
	}
	if !masked.AgentAPI.APIKey.Configured || !masked.Linx.APIKey.Configured ||
		!masked.Services.RenderToken.Configured {
		t.Fatalf("secrets are not reported as configured: %s", encoded)
	}
	if !strings.Contains(text, `"configured":true`) {
		t.Fatalf("masked settings do not expose the configured flags: %s", encoded)
	}
}

func ptr[T any](value T) *T { return &value }

func TestLinxAgentTemplateDefaultsAndUpdates(t *testing.T) {
	provider, _ := newProvider(t)
	ctx := context.Background()
	snapshot, err := provider.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Linx.MCPName != DefaultLinxMCPName || snapshot.Linx.LLMModelID != DefaultLinxLLMModelID ||
		snapshot.Linx.AssistantName != DefaultLinxAssistantName {
		t.Fatalf("defaults not applied: %+v", snapshot.Linx)
	}
	name, mcpURL := "阿灵", "https://gw.example.com/mcp"
	err = provider.Update(ctx, Patch{Linx: &LinxPatch{AssistantName: &name, PublicMCPURL: &mcpURL}}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _ = provider.Snapshot(ctx)
	if snapshot.Linx.AssistantName != "阿灵" || snapshot.Linx.PublicMCPURL != mcpURL {
		t.Fatalf("snapshot = %+v", snapshot.Linx)
	}
	masked, err := provider.Masked(ctx)
	if err != nil || masked.Linx.PublicMCPURL != mcpURL || masked.Linx.AssistantName != "阿灵" {
		t.Fatalf("masked = %+v err=%v", masked.Linx, err)
	}
}

func TestEnsureMCPTokenKeepsStoredValueAndRotates(t *testing.T) {
	provider, db := newProvider(t)
	ctx := context.Background()
	if err := provider.EnsureMCPToken(ctx, "legacy-token"); err != nil {
		t.Fatal(err)
	}
	token, err := provider.MCPToken(ctx)
	if err != nil || token != "legacy-token" {
		t.Fatalf("token = %q err=%v", token, err)
	}
	rows, _ := db.PlatformSettings(ctx)
	if strings.Contains(rows[KeyLinxMCPToken].Value, "legacy-token") || !rows[KeyLinxMCPToken].IsSecret {
		t.Fatalf("token stored in clear: %+v", rows[KeyLinxMCPToken])
	}
	if err := provider.EnsureMCPToken(ctx, "other"); err != nil {
		t.Fatal(err)
	}
	if again, _ := provider.MCPToken(ctx); again != "legacy-token" {
		t.Fatalf("EnsureMCPToken overwrote the stored token: %q", again)
	}
	if err := provider.Update(ctx, Patch{Linx: &LinxPatch{RotateMCPToken: true}}, "admin"); err != nil {
		t.Fatal(err)
	}
	rotated, _ := provider.MCPToken(ctx)
	if rotated == "" || rotated == "legacy-token" || len(rotated) != 64 {
		t.Fatalf("rotated token = %q", rotated)
	}
	masked, _ := provider.Masked(ctx)
	if !masked.Linx.MCPToken.Configured {
		t.Fatalf("masked token = %+v", masked.Linx.MCPToken)
	}
}

func TestEnsureMCPTokenGeneratesWhenNoLegacyValue(t *testing.T) {
	provider, _ := newProvider(t)
	ctx := context.Background()
	if err := provider.EnsureMCPToken(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if token, err := provider.MCPToken(ctx); err != nil || len(token) != 64 {
		t.Fatalf("token = %q err=%v", token, err)
	}
}

func TestMCPAddressChangeClearsCachedMCPResources(t *testing.T) {
	provider, db := newProvider(t)
	ctx := context.Background()
	if err := db.SetPortalSetting(ctx, "linx.mcp_server_id", "srv"); err != nil {
		t.Fatal(err)
	}
	mcpURL := "https://gw.example.com/mcp"
	if err := provider.Update(ctx, Patch{Linx: &LinxPatch{PublicMCPURL: &mcpURL}}, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.PortalSetting(ctx, "linx.mcp_server_id"); err == nil {
		t.Fatal("cached MCP server survived an address change")
	}
}
