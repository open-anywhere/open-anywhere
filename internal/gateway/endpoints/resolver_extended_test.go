package endpoints

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/http"
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

func newResolverEnvAtPath(t *testing.T, path string) *resolverEnv {
	t.Helper()
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	box, err := secret.New(bytes.Repeat([]byte{3}, 32))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := screen.LoadCatalog("")
	if err != nil {
		t.Fatal(err)
	}
	provider := settings.New(db, box)
	return &resolverEnv{
		db:       db,
		box:      box,
		provider: provider,
		resolver: NewResolver(db, provider, box, netguard.New(false), catalog),
		catalog:  catalog,
	}
}

func seedBinding(t *testing.T, env *resolverEnv, binding store.PortalServiceBinding) {
	t.Helper()
	now := time.Now().UTC()
	if binding.CreatedAt.IsZero() {
		binding.CreatedAt = now
	}
	if binding.UpdatedAt.IsZero() {
		binding.UpdatedAt = now
	}
	if err := env.db.UpsertServiceBinding(context.Background(), binding); err != nil {
		t.Fatal(err)
	}
}

func (e *resolverEnv) encrypt(t *testing.T, userID, field, value string) string {
	t.Helper()
	encrypted, err := e.box.Encrypt(value, secret.BindingAAD(userID, field))
	if err != nil {
		t.Fatal(err)
	}
	return encrypted
}

func TestResolverReportsDatabaseFailures(t *testing.T) {
	env := newResolverEnv(t)
	env.createUser(t, "usr_1", "alice", "mac-1", "agent-1", store.PortalDeviceRegistered)
	if err := env.db.Close(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if _, err := env.resolver.BindingFor(ctx, app.Identity{DeviceID: "mac-1", AgentID: "agent-1"}); err == nil || !strings.Contains(err.Error(), "load device") {
		t.Fatalf("BindingFor err = %v", err)
	}
	if _, err := env.resolver.BindingForUser(ctx, "usr_1"); err == nil || !strings.Contains(err.Error(), "load service binding") {
		t.Fatalf("BindingForUser err = %v", err)
	}
	if _, err := env.resolver.PreviewBinding(ctx, "usr_1", BindingOverride{}); err == nil || !strings.Contains(err.Error(), "load service binding") {
		t.Fatalf("PreviewBinding err = %v", err)
	}
	if _, err := env.resolver.SaveBinding(ctx, "usr_1", BindingOverride{}); err == nil || !strings.Contains(err.Error(), "load service binding") {
		t.Fatalf("SaveBinding err = %v", err)
	}
	if err := env.resolver.ValidateSecrets(ctx); err == nil {
		t.Fatal("ValidateSecrets accepted a closed database")
	}
	if err := env.resolver.ValidateOwner(ctx, app.Identity{DeviceID: "mac-1", AgentID: "agent-1"}); err == nil || !strings.Contains(err.Error(), "load device") {
		t.Fatalf("ValidateOwner err = %v", err)
	}
	if _, err := env.resolver.managed(ctx); err == nil || !strings.Contains(err.Error(), "read platform services") {
		t.Fatalf("managed err = %v", err)
	}
}

func TestResolverRejectsMissingUserIDs(t *testing.T) {
	env := newResolverEnv(t)
	ctx := context.Background()
	if _, err := env.resolver.BindingForUser(ctx, ""); toolCode(t, err) != "OWNER_NOT_FOUND" {
		t.Fatalf("BindingForUser err = %v", err)
	}
	if _, err := env.resolver.PreviewBinding(ctx, "", BindingOverride{}); toolCode(t, err) != "OWNER_NOT_FOUND" {
		t.Fatalf("PreviewBinding err = %v", err)
	}
	if _, err := env.resolver.SaveBinding(ctx, "", BindingOverride{}); toolCode(t, err) != "OWNER_NOT_FOUND" {
		t.Fatalf("SaveBinding err = %v", err)
	}
}

func TestBindingForCustomReportsUnreadableTokens(t *testing.T) {
	env := newResolverEnv(t)
	user := env.createUser(t, "usr_1", "alice", "mac-1", "agent-1", store.PortalDeviceRegistered)
	// The casting token is sealed for the render field, so decrypting the
	// casting column must fail.
	seedBinding(t, env, store.PortalServiceBinding{
		UserID: user.ID, Mode: store.ServiceModeCustom,
		RenderURL: "https://my-render.example", CastingURL: "https://my-casting.example",
		RenderTokenEnc:  env.encrypt(t, user.ID, "render", "render-secret"),
		CastingTokenEnc: env.encrypt(t, user.ID, "render", "casting-secret"),
	})
	_, err := env.resolver.BindingFor(context.Background(), app.Identity{DeviceID: "mac-1", AgentID: "agent-1"})
	if err == nil || !strings.Contains(err.Error(), "decrypt service token") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "casting-secret") {
		t.Fatalf("the error leaked token material: %v", err)
	}
}

func TestPreviewBindingReusesSavedCustomBinding(t *testing.T) {
	env := newResolverEnv(t)
	user := env.createUser(t, "usr_1", "alice", "mac-1", "agent-1", store.PortalDeviceRegistered)
	seedBinding(t, env, store.PortalServiceBinding{
		UserID: user.ID, Mode: store.ServiceModeCustom,
		RenderURL: "https://my-render.example", CastingURL: "https://my-casting.example",
		RenderTokenEnc:  env.encrypt(t, user.ID, "render", "render-secret"),
		CastingTokenEnc: env.encrypt(t, user.ID, "casting", "casting-secret"),
	})

	binding, err := env.resolver.PreviewBinding(context.Background(), user.ID, BindingOverride{})
	if err != nil {
		t.Fatal(err)
	}
	if binding.Managed {
		t.Fatal("a saved custom binding was previewed as managed")
	}
	if binding.RenderURL != "https://my-render.example" || binding.CastingURL != "https://my-casting.example" {
		t.Fatalf("urls = %q %q", binding.RenderURL, binding.CastingURL)
	}
	if binding.RenderToken != "render-secret" || binding.CastingToken != "casting-secret" {
		t.Fatalf("tokens = %q %q", binding.RenderToken, binding.CastingToken)
	}
}

func TestPreviewBindingReportsUndecryptableTokens(t *testing.T) {
	env := newResolverEnv(t)
	user := env.createUser(t, "usr_1", "alice", "mac-1", "agent-1", store.PortalDeviceRegistered)
	validCasting := env.encrypt(t, user.ID, "casting", "casting-secret")
	validRender := env.encrypt(t, user.ID, "render", "render-secret")
	ctx := context.Background()

	seedBinding(t, env, store.PortalServiceBinding{
		UserID: user.ID, Mode: store.ServiceModeCustom,
		RenderURL: "https://my-render.example", CastingURL: "https://my-casting.example",
		RenderTokenEnc:  "enc:v1:broken",
		CastingTokenEnc: validCasting,
	})
	if _, err := env.resolver.PreviewBinding(ctx, user.ID, BindingOverride{}); err == nil || !strings.Contains(err.Error(), "decrypt service token") {
		t.Fatalf("render error = %v", err)
	}

	seedBinding(t, env, store.PortalServiceBinding{
		UserID: user.ID, Mode: store.ServiceModeCustom,
		RenderURL: "https://my-render.example", CastingURL: "https://my-casting.example",
		RenderTokenEnc:  validRender,
		CastingTokenEnc: "enc:v1:broken",
	})
	if _, err := env.resolver.PreviewBinding(ctx, user.ID, BindingOverride{}); err == nil || !strings.Contains(err.Error(), "decrypt service token") {
		t.Fatalf("casting error = %v", err)
	}
}

func TestPreviewBindingClearsTokensOnRequest(t *testing.T) {
	env := newResolverEnv(t)
	user := env.createUser(t, "usr_1", "alice", "mac-1", "agent-1", store.PortalDeviceRegistered)
	seedBinding(t, env, store.PortalServiceBinding{
		UserID: user.ID, Mode: store.ServiceModeCustom,
		RenderURL: "https://my-render.example", CastingURL: "https://my-casting.example",
		RenderTokenEnc:  env.encrypt(t, user.ID, "render", "render-secret"),
		CastingTokenEnc: env.encrypt(t, user.ID, "casting", "casting-secret"),
	})
	binding, err := env.resolver.PreviewBinding(context.Background(), user.ID, BindingOverride{
		ClearRenderToken:  true,
		ClearCastingToken: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if binding.RenderToken != "" || binding.CastingToken != "" {
		t.Fatalf("tokens = %q %q", binding.RenderToken, binding.CastingToken)
	}
}

func TestSaveBindingRejectsInvalidInput(t *testing.T) {
	env := newResolverEnv(t)
	env.createUser(t, "usr_1", "alice", "mac-1", "agent-1", store.PortalDeviceRegistered)
	ctx := context.Background()

	if _, err := env.resolver.SaveBinding(ctx, "usr_1", BindingOverride{Mode: "bogus"}); toolCode(t, err) != "INVALID_ARGUMENT" {
		t.Fatalf("invalid mode err = %v", err)
	}
	if _, err := env.resolver.SaveBinding(ctx, "usr_1", BindingOverride{Mode: store.ServiceModeCustom}); toolCode(t, err) != "SERVICE_NOT_CONFIGURED" {
		t.Fatalf("missing addresses err = %v", err)
	}
}

func TestSaveBindingAppliesTheOutboundPolicy(t *testing.T) {
	env := newResolverEnv(t)
	env.createUser(t, "usr_1", "alice", "mac-1", "agent-1", store.PortalDeviceRegistered)
	resolver := NewResolver(env.db, env.provider, env.box, netguard.New(false), env.catalog)
	ctx := context.Background()

	_, err := resolver.SaveBinding(ctx, "usr_1", BindingOverride{
		Mode:       store.ServiceModeCustom,
		RenderURL:  "http://127.0.0.1:8080",
		CastingURL: "http://127.0.0.1:8081",
	})
	if netguard.CodeOf(err) != netguard.CodePrivateTarget {
		t.Fatalf("render policy err = %v", err)
	}

	_, err = resolver.SaveBinding(ctx, "usr_1", BindingOverride{
		Mode:       store.ServiceModeCustom,
		RenderURL:  "http://8.8.8.8:8080",
		CastingURL: "http://127.0.0.1:8081",
	})
	if netguard.CodeOf(err) != netguard.CodePrivateTarget {
		t.Fatalf("casting policy err = %v", err)
	}
}

func TestSaveBindingRequiresASecretBoxForTokens(t *testing.T) {
	env := newResolverEnv(t)
	env.createUser(t, "usr_1", "alice", "mac-1", "agent-1", store.PortalDeviceRegistered)
	resolver := NewResolver(env.db, env.provider, nil, netguard.New(true), env.catalog)
	ctx := context.Background()

	_, err := resolver.SaveBinding(ctx, "usr_1", BindingOverride{
		Mode:        store.ServiceModeCustom,
		RenderURL:   "https://my-render.example",
		CastingURL:  "https://my-casting.example",
		RenderToken: "render-secret",
	})
	if err == nil || !strings.Contains(err.Error(), "no secret box") {
		t.Fatalf("render token err = %v", err)
	}

	_, err = resolver.SaveBinding(ctx, "usr_1", BindingOverride{
		Mode:         store.ServiceModeCustom,
		RenderURL:    "https://my-render.example",
		CastingURL:   "https://my-casting.example",
		CastingToken: "casting-secret",
	})
	if err == nil || !strings.Contains(err.Error(), "no secret box") {
		t.Fatalf("casting token err = %v", err)
	}
}

func TestSaveBindingClearsTokensInCustomMode(t *testing.T) {
	env := newResolverEnv(t)
	user := env.createUser(t, "usr_1", "alice", "mac-1", "agent-1", store.PortalDeviceRegistered)
	seedBinding(t, env, store.PortalServiceBinding{
		UserID: user.ID, Mode: store.ServiceModeCustom,
		RenderURL: "https://my-render.example", CastingURL: "https://my-casting.example",
		RenderTokenEnc:  env.encrypt(t, user.ID, "render", "render-secret"),
		CastingTokenEnc: env.encrypt(t, user.ID, "casting", "casting-secret"),
	})
	resolver := NewResolver(env.db, env.provider, env.box, netguard.New(true), env.catalog)
	stored, err := resolver.SaveBinding(context.Background(), user.ID, BindingOverride{
		Mode:              store.ServiceModeCustom,
		RenderURL:         "https://my-render.example",
		CastingURL:        "https://my-casting.example",
		ClearRenderToken:  true,
		ClearCastingToken: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stored.RenderTokenEnc != "" || stored.CastingTokenEnc != "" {
		t.Fatalf("tokens were not cleared: %+v", stored)
	}
}

func TestSaveBindingKeepsStoredValuesWhenFieldsAreEmpty(t *testing.T) {
	env := newResolverEnv(t)
	user := env.createUser(t, "usr_1", "alice", "mac-1", "agent-1", store.PortalDeviceRegistered)
	renderEnc := env.encrypt(t, user.ID, "render", "render-secret")
	castingEnc := env.encrypt(t, user.ID, "casting", "casting-secret")
	seedBinding(t, env, store.PortalServiceBinding{
		UserID: user.ID, Mode: store.ServiceModeCustom,
		RenderURL: "https://my-render.example", CastingURL: "https://my-casting.example",
		RenderTokenEnc: renderEnc, CastingTokenEnc: castingEnc,
	})
	resolver := NewResolver(env.db, env.provider, env.box, netguard.New(true), env.catalog)
	// An empty override keeps the saved mode, addresses and tokens.
	stored, err := resolver.SaveBinding(context.Background(), user.ID, BindingOverride{})
	if err != nil {
		t.Fatal(err)
	}
	if stored.Mode != store.ServiceModeCustom {
		t.Fatalf("mode = %q", stored.Mode)
	}
	if stored.RenderURL != "https://my-render.example" || stored.CastingURL != "https://my-casting.example" {
		t.Fatalf("urls = %q %q", stored.RenderURL, stored.CastingURL)
	}
	if stored.RenderTokenEnc != renderEnc || stored.CastingTokenEnc != castingEnc {
		t.Fatal("stored tokens were replaced by empty values")
	}
}

func TestSaveBindingClearsTokensWhenRestoringManagedMode(t *testing.T) {
	env := newResolverEnv(t)
	user := env.createUser(t, "usr_1", "alice", "mac-1", "agent-1", store.PortalDeviceRegistered)
	seedBinding(t, env, store.PortalServiceBinding{
		UserID: user.ID, Mode: store.ServiceModeCustom,
		RenderURL: "https://my-render.example", CastingURL: "https://my-casting.example",
		RenderTokenEnc:  env.encrypt(t, user.ID, "render", "render-secret"),
		CastingTokenEnc: env.encrypt(t, user.ID, "casting", "casting-secret"),
	})
	resolver := NewResolver(env.db, env.provider, env.box, netguard.New(true), env.catalog)
	stored, err := resolver.SaveBinding(context.Background(), user.ID, BindingOverride{
		Mode:              store.ServiceModeManaged,
		ClearRenderToken:  true,
		ClearCastingToken: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stored.Mode != store.ServiceModeManaged {
		t.Fatalf("mode = %q", stored.Mode)
	}
	if stored.RenderTokenEnc != "" || stored.CastingTokenEnc != "" {
		t.Fatalf("tokens were not cleared: %+v", stored)
	}
	if stored.RenderURL != "https://my-render.example" || stored.CastingURL != "https://my-casting.example" {
		t.Fatalf("custom addresses should be kept for a later switch back: %+v", stored)
	}
}

func TestSaveBindingReportsPersistenceFailures(t *testing.T) {
	env := newResolverEnv(t)
	resolver := NewResolver(env.db, env.provider, env.box, netguard.New(true), env.catalog)
	_, err := resolver.SaveBinding(context.Background(), "usr_missing", BindingOverride{
		Mode:       store.ServiceModeCustom,
		RenderURL:  "https://my-render.example",
		CastingURL: "https://my-casting.example",
	})
	if err == nil || !strings.Contains(err.Error(), "save service binding") {
		t.Fatalf("err = %v", err)
	}
}

func TestResolverGuardFallbacks(t *testing.T) {
	env := newResolverEnv(t)
	resolver := NewResolver(env.db, env.provider, env.box, nil, nil)
	if err := resolver.CheckURL(context.Background(), "http://127.0.0.1:8080"); err != nil {
		t.Fatalf("a resolver without a guard rejected a private address: %v", err)
	}
	plain := resolver.HTTPClient(3 * time.Second)
	if plain.Timeout != 3*time.Second || plain.Transport != nil {
		t.Fatalf("plain client = %+v", plain)
	}

	guarded := env.resolver.HTTPClient(4 * time.Second)
	if guarded.Timeout != 4*time.Second {
		t.Fatalf("guarded timeout = %v", guarded.Timeout)
	}
	transport, ok := guarded.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil || transport.DialContext == nil {
		t.Fatalf("guarded transport = %#v", guarded.Transport)
	}
}

func TestDecryptRequiresABoxForStoredValues(t *testing.T) {
	env := newResolverEnv(t)
	resolver := NewResolver(env.db, env.provider, nil, env.resolver.guard, env.catalog)
	plain, err := resolver.decrypt("", "")
	if err != nil || plain != "" {
		t.Fatalf("decrypt(empty) = %q %v", plain, err)
	}
	if _, err := resolver.decrypt("enc:v1:value", "aad"); err == nil || !strings.Contains(err.Error(), "no secret box") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateSecretsReportsCastingTokenFailures(t *testing.T) {
	env := newResolverEnv(t)
	user := env.createUser(t, "usr_1", "alice", "mac-1", "agent-1", store.PortalDeviceRegistered)
	seedBinding(t, env, store.PortalServiceBinding{
		UserID: user.ID, Mode: store.ServiceModeCustom,
		RenderURL: "https://my-render.example", CastingURL: "https://my-casting.example",
		RenderTokenEnc:  env.encrypt(t, user.ID, "render", "render-secret"),
		CastingTokenEnc: "enc:v1:broken",
	})
	err := env.resolver.ValidateSecrets(context.Background())
	if err == nil || !strings.Contains(err.Error(), "casting token") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "render-secret") {
		t.Fatalf("the error leaked token material: %v", err)
	}
}

func TestValidateOwnerRequiresAnExistingUser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "portal.db")
	env := newResolverEnvAtPath(t, path)
	env.createUser(t, "usr_1", "alice", "mac-1", "agent-1", store.PortalDeviceRegistered)
	ctx := context.Background()

	// The portal never deletes an account that still owns a device, but a
	// database restored from a backup can contain the pair. Deleting the user
	// from a connection without foreign keys reproduces that state.
	raw, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(0)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.ExecContext(ctx, `DELETE FROM portal_users WHERE id = ?`, "usr_1"); err != nil {
		t.Fatal(err)
	}

	err = env.resolver.ValidateOwner(ctx, app.Identity{DeviceID: "mac-1", AgentID: "agent-1"})
	if code := toolCode(t, err); code != "OWNER_NOT_FOUND" {
		t.Fatalf("code = %q err = %v", code, err)
	}
}

func TestValidateOwnerReportsAnUnreadableUser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "portal.db")
	env := newResolverEnvAtPath(t, path)
	env.createUser(t, "usr_1", "alice", "mac-1", "agent-1", store.PortalDeviceRegistered)
	ctx := context.Background()

	raw, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.ExecContext(ctx, `UPDATE portal_users SET created_at = 'not-a-time' WHERE id = ?`, "usr_1"); err != nil {
		t.Fatal(err)
	}

	err = env.resolver.ValidateOwner(ctx, app.Identity{DeviceID: "mac-1", AgentID: "agent-1"})
	if err == nil || !strings.Contains(err.Error(), "load user") {
		t.Fatalf("err = %v", err)
	}
	var toolErr *service.ToolError
	if errors.As(err, &toolErr) {
		t.Fatalf("an unreadable user became a tool error: %v", err)
	}
}
