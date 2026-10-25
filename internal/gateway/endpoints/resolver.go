// Package endpoints resolves each tenant's Render and Casting services and
// builds the HTTP clients that talk to them.
package endpoints

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/netguard"
	"github.com/open-anywhere/open-anywhere/internal/gateway/screen"
	"github.com/open-anywhere/open-anywhere/internal/gateway/secret"
	"github.com/open-anywhere/open-anywhere/internal/gateway/service"
	"github.com/open-anywhere/open-anywhere/internal/gateway/settings"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

// Resolver implements service.BindingSource and the MCP owner validator from
// the portal device table and platform settings.
type Resolver struct {
	db              *store.DB
	settings        *settings.Provider
	box             *secret.Box
	guard           *netguard.Guard
	platformScreens *screen.Catalog
}

// NewResolver returns a resolver. platformScreens is the deployment's screen
// catalog, used as an alias for the platform-managed Casting service; it may
// be nil. box decrypts per-user service tokens and may be nil only when no
// custom token exists.
func NewResolver(db *store.DB, provider *settings.Provider, box *secret.Box, guard *netguard.Guard, platformScreens *screen.Catalog) *Resolver {
	return &Resolver{
		db:              db,
		settings:        provider,
		box:             box,
		guard:           guard,
		platformScreens: platformScreens,
	}
}

// BindingFor resolves the owner's services. It deliberately does not check the
// user's status: authentication and authorization happen in the portal, MCP
// validator and admin endpoints, while the monitor and cleanup still need the
// endpoints of a disabled user to release its resources.
func (r *Resolver) BindingFor(ctx context.Context, owner app.Identity) (service.Binding, error) {
	if owner.DeviceID == "" || owner.AgentID == "" {
		return service.Binding{}, &service.ToolError{Code: "OWNER_NOT_FOUND", Message: "无法识别智能体"}
	}
	device, err := r.db.PortalDeviceByIdentity(ctx, owner.DeviceID, owner.AgentID)
	if errors.Is(err, sql.ErrNoRows) {
		return service.Binding{}, &service.ToolError{Code: "OWNER_NOT_FOUND", Message: "无法识别智能体"}
	}
	if err != nil {
		return service.Binding{}, fmt.Errorf("endpoints: load device: %w", err)
	}
	return r.BindingForUser(ctx, device.UserID)
}

// BindingForUser resolves the services of one account. Portal endpoints know
// the user directly and do not need to go through a device identity.
func (r *Resolver) BindingForUser(ctx context.Context, userID string) (service.Binding, error) {
	if userID == "" {
		return service.Binding{}, &service.ToolError{Code: "OWNER_NOT_FOUND", Message: "无法识别智能体"}
	}
	binding, err := r.db.ServiceBindingByUser(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return r.managed(ctx)
	}
	if err != nil {
		return service.Binding{}, fmt.Errorf("endpoints: load service binding: %w", err)
	}
	if binding.Mode != store.ServiceModeCustom {
		return r.managed(ctx)
	}
	if binding.RenderURL == "" || binding.CastingURL == "" {
		return service.Binding{}, &service.ToolError{Code: "SERVICE_NOT_CONFIGURED", Message: "请先绑定渲染和投屏服务"}
	}
	renderToken, err := r.decrypt(binding.RenderTokenEnc, secret.BindingAAD(userID, "render"))
	if err != nil {
		return service.Binding{}, err
	}
	castingToken, err := r.decrypt(binding.CastingTokenEnc, secret.BindingAAD(userID, "casting"))
	if err != nil {
		return service.Binding{}, err
	}
	return service.Binding{
		RenderURL:    binding.RenderURL,
		RenderToken:  renderToken,
		CastingURL:   binding.CastingURL,
		CastingToken: castingToken,
		Managed:      false,
	}, nil
}

// BindingOverride carries values a user is about to save. Empty token fields
// reuse the stored token; a clear flag removes it.
type BindingOverride struct {
	Mode              string
	RenderURL         string
	CastingURL        string
	RenderToken       string
	CastingToken      string
	ClearRenderToken  bool
	ClearCastingToken bool
}

// PreviewBinding builds the binding a save or connection test would use.
// Tokens never leave this package except as part of the returned Binding.
func (r *Resolver) PreviewBinding(ctx context.Context, userID string, override BindingOverride) (service.Binding, error) {
	if userID == "" {
		return service.Binding{}, &service.ToolError{Code: "OWNER_NOT_FOUND", Message: "无法识别智能体"}
	}
	saved, err := r.db.ServiceBindingByUser(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		saved = store.PortalServiceBinding{UserID: userID, Mode: store.ServiceModeManaged}
	} else if err != nil {
		return service.Binding{}, fmt.Errorf("endpoints: load service binding: %w", err)
	}
	mode := override.Mode
	if mode == "" {
		mode = saved.Mode
	}
	if mode != store.ServiceModeManaged && mode != store.ServiceModeCustom {
		return service.Binding{}, &service.ToolError{Code: "INVALID_ARGUMENT", Message: "服务模式无效"}
	}
	if mode != store.ServiceModeCustom {
		return r.managed(ctx)
	}
	renderURL := strings.TrimSpace(override.RenderURL)
	if renderURL == "" {
		renderURL = saved.RenderURL
	}
	castingURL := strings.TrimSpace(override.CastingURL)
	if castingURL == "" {
		castingURL = saved.CastingURL
	}
	renderToken, err := r.overrideToken(userID, "render", override.RenderToken, override.ClearRenderToken, saved.RenderTokenEnc)
	if err != nil {
		return service.Binding{}, err
	}
	castingToken, err := r.overrideToken(userID, "casting", override.CastingToken, override.ClearCastingToken, saved.CastingTokenEnc)
	if err != nil {
		return service.Binding{}, err
	}
	if renderURL == "" || castingURL == "" {
		return service.Binding{}, &service.ToolError{Code: "SERVICE_NOT_CONFIGURED", Message: "请先绑定渲染和投屏服务"}
	}
	return service.Binding{
		RenderURL:    renderURL,
		RenderToken:  renderToken,
		CastingURL:   castingURL,
		CastingToken: castingToken,
		Managed:      false,
	}, nil
}

func (r *Resolver) overrideToken(userID, field, value string, clear bool, saved string) (string, error) {
	if value = strings.TrimSpace(value); value != "" {
		return value, nil
	}
	if clear {
		return "", nil
	}
	return r.decrypt(saved, secret.BindingAAD(userID, field))
}

// SaveBinding validates and persists a binding. Empty fields keep the stored
// value; a clear flag removes a token. Switching to managed keeps the custom
// addresses and tokens encrypted so switching back does not lose them.
func (r *Resolver) SaveBinding(ctx context.Context, userID string, override BindingOverride) (store.PortalServiceBinding, error) {
	if userID == "" {
		return store.PortalServiceBinding{}, &service.ToolError{Code: "OWNER_NOT_FOUND", Message: "无法识别智能体"}
	}
	saved, err := r.db.ServiceBindingByUser(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		saved = store.PortalServiceBinding{UserID: userID, Mode: store.ServiceModeManaged}
	} else if err != nil {
		return store.PortalServiceBinding{}, fmt.Errorf("endpoints: load service binding: %w", err)
	}
	mode := override.Mode
	if mode == "" {
		mode = saved.Mode
	}
	if mode != store.ServiceModeManaged && mode != store.ServiceModeCustom {
		return store.PortalServiceBinding{}, &service.ToolError{Code: "INVALID_ARGUMENT", Message: "服务模式无效"}
	}
	now := time.Now().UTC()
	binding := store.PortalServiceBinding{
		UserID:          userID,
		Mode:            mode,
		RenderURL:       saved.RenderURL,
		CastingURL:      saved.CastingURL,
		RenderTokenEnc:  saved.RenderTokenEnc,
		CastingTokenEnc: saved.CastingTokenEnc,
		CreatedAt:       saved.CreatedAt,
		UpdatedAt:       now,
	}
	if binding.CreatedAt.IsZero() {
		binding.CreatedAt = now
	}
	if mode == store.ServiceModeCustom {
		if value := strings.TrimSpace(override.RenderURL); value != "" {
			binding.RenderURL = value
		}
		if value := strings.TrimSpace(override.CastingURL); value != "" {
			binding.CastingURL = value
		}
		if binding.RenderURL == "" || binding.CastingURL == "" {
			return store.PortalServiceBinding{}, &service.ToolError{Code: "SERVICE_NOT_CONFIGURED", Message: "请先填写渲染和投屏服务地址"}
		}
		if err := r.CheckURL(ctx, binding.RenderURL); err != nil {
			return store.PortalServiceBinding{}, err
		}
		if err := r.CheckURL(ctx, binding.CastingURL); err != nil {
			return store.PortalServiceBinding{}, err
		}
		if binding.RenderTokenEnc, err = r.encryptToken(userID, "render", override.RenderToken, override.ClearRenderToken, binding.RenderTokenEnc); err != nil {
			return store.PortalServiceBinding{}, err
		}
		if binding.CastingTokenEnc, err = r.encryptToken(userID, "casting", override.CastingToken, override.ClearCastingToken, binding.CastingTokenEnc); err != nil {
			return store.PortalServiceBinding{}, err
		}
	} else {
		// Explicit clearing still applies when the user restores managed mode.
		if override.ClearRenderToken {
			binding.RenderTokenEnc = ""
		}
		if override.ClearCastingToken {
			binding.CastingTokenEnc = ""
		}
	}
	if err := r.db.UpsertServiceBinding(ctx, binding); err != nil {
		return store.PortalServiceBinding{}, err
	}
	return binding, nil
}

func (r *Resolver) encryptToken(userID, field, value string, clear bool, saved string) (string, error) {
	if clear {
		return "", nil
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return saved, nil
	}
	if r.box == nil {
		return "", fmt.Errorf("endpoints: no secret box is configured")
	}
	encrypted, err := r.box.Encrypt(value, secret.BindingAAD(userID, field))
	if err != nil {
		return "", fmt.Errorf("endpoints: encrypt service token: %w", err)
	}
	return encrypted, nil
}

// CheckURL applies the outbound address policy to a user-supplied address.
func (r *Resolver) CheckURL(ctx context.Context, raw string) error {
	if r.guard == nil {
		return nil
	}
	return r.guard.CheckURL(ctx, raw)
}

// HTTPClient returns a client that enforces the outbound address policy. Admin
// connection tests use it for upstreams that are not Render or Casting.
func (r *Resolver) HTTPClient(timeout time.Duration) *http.Client {
	if r.guard != nil {
		return r.guard.Client(timeout)
	}
	return &http.Client{Timeout: timeout}
}

func (r *Resolver) managed(ctx context.Context) (service.Binding, error) {
	snapshot, err := r.settings.Snapshot(ctx)
	if err != nil {
		return service.Binding{}, fmt.Errorf("endpoints: read platform services: %w", err)
	}
	if !snapshot.Services.Ready() {
		return service.Binding{}, &service.ToolError{Code: "SERVICE_NOT_CONFIGURED", Message: "平台还没有配置默认屏幕服务"}
	}
	return service.Binding{
		RenderURL:    snapshot.Services.RenderURL,
		RenderToken:  snapshot.Services.RenderToken,
		CastingURL:   snapshot.Services.CastingURL,
		CastingToken: snapshot.Services.CastingToken,
		Managed:      true,
		Screens:      r.platformScreens,
	}, nil
}

func (r *Resolver) decrypt(value, aad string) (string, error) {
	if value == "" {
		return "", nil
	}
	if r.box == nil {
		return "", fmt.Errorf("endpoints: no secret box is configured")
	}
	plain, err := r.box.Decrypt(value, aad)
	if err != nil {
		return "", fmt.Errorf("endpoints: decrypt service token: %w", err)
	}
	return plain, nil
}

// ValidateSecrets decrypts every stored service token. It runs at startup so a
// missing or mismatched master key fails the process instead of surfacing as a
// broken binding on the first request.
func (r *Resolver) ValidateSecrets(ctx context.Context) error {
	bindings, err := r.db.ServiceBindingSecrets(ctx)
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		if _, err := r.decrypt(binding.RenderTokenEnc, secret.BindingAAD(binding.UserID, "render")); err != nil {
			return fmt.Errorf("endpoints: service binding %s render token: %w", binding.UserID, err)
		}
		if _, err := r.decrypt(binding.CastingTokenEnc, secret.BindingAAD(binding.UserID, "casting")); err != nil {
			return fmt.Errorf("endpoints: service binding %s casting token: %w", binding.UserID, err)
		}
	}
	return nil
}

// ValidateOwner is the MCP identity check: the device must exist, have
// completed provisioning, and belong to an active user.
func (r *Resolver) ValidateOwner(ctx context.Context, owner app.Identity) error {
	if owner.DeviceID == "" || owner.AgentID == "" {
		return &service.ToolError{Code: "OWNER_NOT_FOUND", Message: "无法识别智能体"}
	}
	device, err := r.db.PortalDeviceByIdentity(ctx, owner.DeviceID, owner.AgentID)
	if errors.Is(err, sql.ErrNoRows) {
		return &service.ToolError{Code: "OWNER_NOT_FOUND", Message: "无法识别智能体"}
	}
	if err != nil {
		return fmt.Errorf("endpoints: load device: %w", err)
	}
	if device.State != store.PortalDeviceRegistered {
		return &service.ToolError{Code: "IDENTITY_NOT_REGISTERED", Message: "智能体尚未完成开通"}
	}
	user, err := r.db.PortalUserByID(ctx, device.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		return &service.ToolError{Code: "OWNER_NOT_FOUND", Message: "无法识别智能体"}
	}
	if err != nil {
		return fmt.Errorf("endpoints: load user: %w", err)
	}
	if user.Status != store.PortalUserActive {
		return &service.ToolError{Code: "USER_DISABLED", Message: "账号已被停用"}
	}
	return nil
}
