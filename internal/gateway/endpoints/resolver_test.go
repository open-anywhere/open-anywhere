package endpoints

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/netguard"
	"github.com/open-anywhere/open-anywhere/internal/gateway/screen"
	"github.com/open-anywhere/open-anywhere/internal/gateway/secret"
	"github.com/open-anywhere/open-anywhere/internal/gateway/service"
	"github.com/open-anywhere/open-anywhere/internal/gateway/settings"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

type resolverEnv struct {
	db       *store.DB
	box      *secret.Box
	provider *settings.Provider
	resolver *Resolver
	catalog  *screen.Catalog
}

func newResolverEnv(t *testing.T) *resolverEnv {
	t.Helper()
	return newResolverEnvAtPath(t, filepath.Join(t.TempDir(), "portal.db"))
}

func (e *resolverEnv) seedDefaults(t *testing.T) {
	t.Helper()
	err := e.provider.Seed(context.Background(), []settings.Seed{
		{Key: settings.KeyDefaultRenderURL, Value: "https://render.example"},
		{Key: settings.KeyDefaultRenderToken, Value: "platform-render-token"},
		{Key: settings.KeyDefaultCastingURL, Value: "https://casting.example"},
		{Key: settings.KeyDefaultCastingToken, Value: "platform-casting-token"},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (e *resolverEnv) createUser(t *testing.T, id, username, mac, agentID, state string) store.PortalUser {
	t.Helper()
	now := time.Now().UTC()
	user := store.PortalUser{
		ID: id, Username: username, PasswordHash: "hash", Role: store.PortalRoleUser,
		Status: store.PortalUserActive, CreatedAt: now, UpdatedAt: now,
	}
	device := store.PortalDevice{
		ID: "dev_" + id, UserID: id, Kind: store.PortalDeviceVirtual, MAC: mac,
		ClientID: "client_" + id, LinxAgentID: agentID, AgentName: "agent-" + id,
		State: state, Step: "done", CreatedAt: now, UpdatedAt: now,
	}
	if err := e.db.CreatePortalUserWithDevice(context.Background(), user, device); err != nil {
		t.Fatal(err)
	}
	return user
}

func toolCode(t *testing.T, err error) string {
	t.Helper()
	var toolErr *service.ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("error %v is not a ToolError", err)
	}
	return toolErr.Code
}

func TestBindingForManagedUsesPlatformDefaults(t *testing.T) {
	env := newResolverEnv(t)
	env.seedDefaults(t)
	user := env.createUser(t, "usr_1", "alice", "mac-1", "agent-1", store.PortalDeviceRegistered)

	binding, err := env.resolver.BindingFor(context.Background(), app.Identity{DeviceID: "mac-1", AgentID: "agent-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !binding.Managed || binding.RenderURL != "https://render.example" || binding.CastingURL != "https://casting.example" {
		t.Fatalf("binding = %+v", binding)
	}
	if binding.RenderToken != "platform-render-token" || binding.CastingToken != "platform-casting-token" {
		t.Fatalf("tokens = %q %q", binding.RenderToken, binding.CastingToken)
	}
	if binding.Screens != env.catalog {
		t.Fatalf("managed binding should carry the platform catalog")
	}
	_ = user
}

func TestBindingForCustomDecryptsTokens(t *testing.T) {
	env := newResolverEnv(t)
	env.seedDefaults(t)
	user := env.createUser(t, "usr_1", "alice", "mac-1", "agent-1", store.PortalDeviceRegistered)
	now := time.Now().UTC()
	renderToken, err := env.box.Encrypt("render-secret", secret.BindingAAD(user.ID, "render"))
	if err != nil {
		t.Fatal(err)
	}
	castingToken, err := env.box.Encrypt("casting-secret", secret.BindingAAD(user.ID, "casting"))
	if err != nil {
		t.Fatal(err)
	}
	err = env.db.UpsertServiceBinding(context.Background(), store.PortalServiceBinding{
		UserID: user.ID, Mode: store.ServiceModeCustom,
		RenderURL: "https://my-render.example", CastingURL: "https://my-casting.example",
		RenderTokenEnc: renderToken, CastingTokenEnc: castingToken,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	binding, err := env.resolver.BindingFor(context.Background(), app.Identity{DeviceID: "mac-1", AgentID: "agent-1"})
	if err != nil {
		t.Fatal(err)
	}
	if binding.Managed || binding.Screens != nil {
		t.Fatalf("binding = %+v", binding)
	}
	if binding.RenderURL != "https://my-render.example" || binding.CastingURL != "https://my-casting.example" {
		t.Fatalf("urls = %q %q", binding.RenderURL, binding.CastingURL)
	}
	if binding.RenderToken != "render-secret" || binding.CastingToken != "casting-secret" {
		t.Fatalf("tokens = %q %q", binding.RenderToken, binding.CastingToken)
	}
}

func TestBindingForManagedRowFallsBackToDefaults(t *testing.T) {
	env := newResolverEnv(t)
	env.seedDefaults(t)
	user := env.createUser(t, "usr_1", "alice", "mac-1", "agent-1", store.PortalDeviceRegistered)
	now := time.Now().UTC()
	err := env.db.UpsertServiceBinding(context.Background(), store.PortalServiceBinding{
		UserID: user.ID, Mode: store.ServiceModeManaged,
		RenderURL: "https://ignored.example", CastingURL: "https://ignored.example",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := env.resolver.BindingFor(context.Background(), app.Identity{DeviceID: "mac-1", AgentID: "agent-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !binding.Managed || binding.RenderURL != "https://render.example" {
		t.Fatalf("binding = %+v", binding)
	}
}

func TestBindingForRequiresCompleteCustomAddresses(t *testing.T) {
	env := newResolverEnv(t)
	env.seedDefaults(t)
	user := env.createUser(t, "usr_1", "alice", "mac-1", "agent-1", store.PortalDeviceRegistered)
	now := time.Now().UTC()
	err := env.db.UpsertServiceBinding(context.Background(), store.PortalServiceBinding{
		UserID: user.ID, Mode: store.ServiceModeCustom,
		RenderURL: "https://my-render.example",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = env.resolver.BindingFor(context.Background(), app.Identity{DeviceID: "mac-1", AgentID: "agent-1"})
	if code := toolCode(t, err); code != "SERVICE_NOT_CONFIGURED" {
		t.Fatalf("code = %q", code)
	}
}

func TestBindingForRequiresConfiguredDefaults(t *testing.T) {
	env := newResolverEnv(t)
	env.createUser(t, "usr_1", "alice", "mac-1", "agent-1", store.PortalDeviceRegistered)
	_, err := env.resolver.BindingFor(context.Background(), app.Identity{DeviceID: "mac-1", AgentID: "agent-1"})
	if code := toolCode(t, err); code != "SERVICE_NOT_CONFIGURED" {
		t.Fatalf("code = %q", code)
	}
}

func TestBindingForRequiresAKnownOwner(t *testing.T) {
	env := newResolverEnv(t)
	env.seedDefaults(t)
	env.createUser(t, "usr_1", "alice", "mac-1", "agent-1", store.PortalDeviceRegistered)
	for _, owner := range []app.Identity{
		{},
		{DeviceID: "mac-1"},
		{AgentID: "agent-1"},
		{DeviceID: "mac-1", AgentID: "agent-2"},
		{DeviceID: "mac-2", AgentID: "agent-1"},
	} {
		_, err := env.resolver.BindingFor(context.Background(), owner)
		if code := toolCode(t, err); code != "OWNER_NOT_FOUND" {
			t.Fatalf("owner %+v code = %q", owner, code)
		}
	}
}

func TestBindingForRejectsATokenFromAnotherField(t *testing.T) {
	env := newResolverEnv(t)
	env.seedDefaults(t)
	user := env.createUser(t, "usr_1", "alice", "mac-1", "agent-1", store.PortalDeviceRegistered)
	now := time.Now().UTC()
	// A casting token moved into the render column must not decrypt.
	moved, err := env.box.Encrypt("top-secret-token", secret.BindingAAD(user.ID, "casting"))
	if err != nil {
		t.Fatal(err)
	}
	err = env.db.UpsertServiceBinding(context.Background(), store.PortalServiceBinding{
		UserID: user.ID, Mode: store.ServiceModeCustom,
		RenderURL: "https://my-render.example", CastingURL: "https://my-casting.example",
		RenderTokenEnc: moved, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.resolver.BindingFor(context.Background(), app.Identity{DeviceID: "mac-1", AgentID: "agent-1"}); err == nil {
		t.Fatal("a token bound to another field was accepted")
	} else if strings.Contains(err.Error(), "top-secret-token") {
		t.Fatalf("error leaked token material: %v", err)
	}
}

func TestBindingForStillResolvesADisabledUser(t *testing.T) {
	env := newResolverEnv(t)
	env.seedDefaults(t)
	env.createUser(t, "usr_1", "alice", "mac-1", "agent-1", store.PortalDeviceRegistered)
	if err := env.db.SetPortalUserStatus(context.Background(), "usr_1", store.PortalUserDisabled, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := env.resolver.BindingFor(context.Background(), app.Identity{DeviceID: "mac-1", AgentID: "agent-1"}); err != nil {
		t.Fatalf("cleanup needs the endpoints of a disabled user: %v", err)
	}
}

func TestValidateOwner(t *testing.T) {
	env := newResolverEnv(t)
	env.createUser(t, "usr_ok", "active", "mac-ok", "agent-ok", store.PortalDeviceRegistered)
	env.createUser(t, "usr_pending", "pending", "mac-pending", "agent-pending", store.PortalDeviceProvisioning)
	env.createUser(t, "usr_disabled", "disabled", "mac-disabled", "agent-disabled", store.PortalDeviceRegistered)
	if err := env.db.SetPortalUserStatus(context.Background(), "usr_disabled", store.PortalUserDisabled, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	cases := []struct {
		owner app.Identity
		code  string
	}{
		{app.Identity{DeviceID: "mac-ok", AgentID: "agent-ok"}, ""},
		{app.Identity{DeviceID: "mac-pending", AgentID: "agent-pending"}, "IDENTITY_NOT_REGISTERED"},
		{app.Identity{DeviceID: "mac-disabled", AgentID: "agent-disabled"}, "USER_DISABLED"},
		{app.Identity{DeviceID: "mac-ok", AgentID: "wrong"}, "OWNER_NOT_FOUND"},
		{app.Identity{DeviceID: "unknown", AgentID: "agent-ok"}, "OWNER_NOT_FOUND"},
		{app.Identity{}, "OWNER_NOT_FOUND"},
	}
	for _, testCase := range cases {
		err := env.resolver.ValidateOwner(ctx, testCase.owner)
		if testCase.code == "" {
			if err != nil {
				t.Errorf("ValidateOwner(%+v) = %v", testCase.owner, err)
			}
			continue
		}
		if code := toolCode(t, err); code != testCase.code {
			t.Errorf("ValidateOwner(%+v) = %q, want %q", testCase.owner, code, testCase.code)
		}
	}
}

func TestPreviewBindingRejectsAnInvalidMode(t *testing.T) {
	env := newResolverEnv(t)
	env.createUser(t, "usr_1", "alice", "mac-1", "agent-1", store.PortalDeviceRegistered)
	_, err := env.resolver.PreviewBinding(context.Background(), "usr_1", BindingOverride{Mode: "bogus"})
	if code := toolCode(t, err); code != "INVALID_ARGUMENT" {
		t.Fatalf("code = %q, err = %v", code, err)
	}
}

func TestPreviewBindingTrimsCustomAddressesAndTokens(t *testing.T) {
	env := newResolverEnv(t)
	env.createUser(t, "usr_1", "alice", "mac-1", "agent-1", store.PortalDeviceRegistered)

	binding, err := env.resolver.PreviewBinding(context.Background(), "usr_1", BindingOverride{
		Mode:         store.ServiceModeCustom,
		RenderURL:    "  https://my-render.example  ",
		CastingURL:   "\t https://my-casting.example \n",
		RenderToken:  "  render-token  ",
		CastingToken: "\ncasting-token\t",
	})
	if err != nil {
		t.Fatal(err)
	}
	if binding.RenderURL != "https://my-render.example" || binding.CastingURL != "https://my-casting.example" {
		t.Fatalf("urls = %q %q", binding.RenderURL, binding.CastingURL)
	}
	if binding.RenderToken != "render-token" || binding.CastingToken != "casting-token" {
		t.Fatalf("tokens = %q %q", binding.RenderToken, binding.CastingToken)
	}
	if binding.Managed {
		t.Fatal("a custom preview was marked managed")
	}
}

func TestPreviewBindingRequiresServiceAddresses(t *testing.T) {
	env := newResolverEnv(t)
	env.createUser(t, "usr_1", "alice", "mac-1", "agent-1", store.PortalDeviceRegistered)
	ctx := context.Background()

	// Managed mode with no platform defaults configured.
	if _, err := env.resolver.PreviewBinding(ctx, "usr_1", BindingOverride{Mode: store.ServiceModeManaged}); toolCode(t, err) != "SERVICE_NOT_CONFIGURED" {
		t.Fatalf("managed err = %v", err)
	}
	// Custom mode still missing the casting address.
	_, err := env.resolver.PreviewBinding(ctx, "usr_1", BindingOverride{
		Mode:      store.ServiceModeCustom,
		RenderURL: "https://my-render.example",
	})
	if code := toolCode(t, err); code != "SERVICE_NOT_CONFIGURED" {
		t.Fatalf("custom err code = %q (%v)", code, err)
	}
}

func TestSaveBindingTrimsCustomAddressesAndTokens(t *testing.T) {
	env := newResolverEnv(t)
	env.createUser(t, "usr_1", "alice", "mac-1", "agent-1", store.PortalDeviceRegistered)
	// allowPrivate skips DNS resolution; trimming is what this test checks.
	resolver := NewResolver(env.db, env.provider, env.box, netguard.New(true), env.catalog)

	stored, err := resolver.SaveBinding(context.Background(), "usr_1", BindingOverride{
		Mode:         store.ServiceModeCustom,
		RenderURL:    "  http://127.0.0.1:8080  ",
		CastingURL:   "\thttp://127.0.0.1:8081\n",
		RenderToken:  "  render-token  ",
		CastingToken: "\ncasting-token\t",
	})
	if err != nil {
		t.Fatal(err)
	}
	if stored.RenderURL != "http://127.0.0.1:8080" || stored.CastingURL != "http://127.0.0.1:8081" {
		t.Fatalf("stored urls = %q %q", stored.RenderURL, stored.CastingURL)
	}

	binding, err := resolver.BindingFor(context.Background(), app.Identity{DeviceID: "mac-1", AgentID: "agent-1"})
	if err != nil {
		t.Fatal(err)
	}
	if binding.RenderURL != "http://127.0.0.1:8080" || binding.CastingURL != "http://127.0.0.1:8081" {
		t.Fatalf("resolved urls = %q %q", binding.RenderURL, binding.CastingURL)
	}
	if binding.RenderToken != "render-token" || binding.CastingToken != "casting-token" {
		t.Fatalf("resolved tokens = %q %q", binding.RenderToken, binding.CastingToken)
	}
}

func TestValidateSecrets(t *testing.T) {
	env := newResolverEnv(t)
	user := env.createUser(t, "usr_1", "alice", "mac-1", "agent-1", store.PortalDeviceRegistered)
	ctx := context.Background()
	now := time.Now().UTC()

	renderToken, err := env.box.Encrypt("render-secret", secret.BindingAAD(user.ID, "render"))
	if err != nil {
		t.Fatal(err)
	}
	castingToken, err := env.box.Encrypt("casting-secret", secret.BindingAAD(user.ID, "casting"))
	if err != nil {
		t.Fatal(err)
	}
	binding := store.PortalServiceBinding{
		UserID: user.ID, Mode: store.ServiceModeCustom,
		RenderURL: "https://my-render.example", CastingURL: "https://my-casting.example",
		RenderTokenEnc: renderToken, CastingTokenEnc: castingToken,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := env.db.UpsertServiceBinding(ctx, binding); err != nil {
		t.Fatal(err)
	}
	if err := env.resolver.ValidateSecrets(ctx); err != nil {
		t.Fatalf("valid secrets were rejected: %v", err)
	}

	// A binding sealed with another master key must fail startup, not silently
	// reset the token.
	foreignBox, err := secret.New(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	foreignToken, err := foreignBox.Encrypt("render-secret", secret.BindingAAD(user.ID, "render"))
	if err != nil {
		t.Fatal(err)
	}
	binding.RenderTokenEnc = foreignToken
	if err := env.db.UpsertServiceBinding(ctx, binding); err != nil {
		t.Fatal(err)
	}
	err = env.resolver.ValidateSecrets(ctx)
	if !errors.Is(err, secret.ErrDecrypt) {
		t.Fatalf("foreign key error = %v, want ErrDecrypt", err)
	}
	if strings.Contains(err.Error(), "render-secret") {
		t.Fatalf("error leaked token material: %v", err)
	}
}
