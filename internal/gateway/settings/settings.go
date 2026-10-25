// Package settings stores platform-level upstream configuration (AgentAPI,
// Linx, default Render/Casting services and the registration switch) in the
// database so an administrator can change it without editing gateway.yaml.
package settings

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/secret"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

// Platform setting keys.
const (
	KeyAgentAPIBaseURL       = "agentapi.base_url"
	KeyAgentAPIKey           = "agentapi.api_key"
	KeyAgentAPIAgentID       = "agentapi.agent_id"
	KeyAgentAPIAgentVersion  = "agentapi.agent_version"
	KeyAgentAPIEnvironmentID = "agentapi.environment_id"

	KeyLinxAPIBase = "linx.api_base"
	KeyLinxOTAURL  = "linx.ota_url"
	KeyLinxAPIKey  = "linx.api_key"

	KeyLinxPublicMCPURL  = "linx.public_mcp_url"
	KeyLinxMCPName       = "linx.mcp_name"
	KeyLinxMCPToken      = "linx.mcp_token"
	KeyLinxAssistantName = "linx.assistant_name"
	KeyLinxLLMModelID    = "linx.llm_model_id"
	KeyLinxMemModelID    = "linx.mem_model_id"
	KeyLinxIntentModelID = "linx.intent_model_id"

	KeyDefaultRenderURL    = "services.default_render_url"
	KeyDefaultRenderToken  = "services.default_render_token"
	KeyDefaultCastingURL   = "services.default_casting_url"
	KeyDefaultCastingToken = "services.default_casting_token"

	KeyRegistrationOpen = "registration.open"
)

// secretKeys marks settings whose value is encrypted at rest.
var secretKeys = map[string]bool{
	KeyAgentAPIKey:         true,
	KeyLinxAPIKey:          true,
	KeyLinxMCPToken:        true,
	KeyDefaultRenderToken:  true,
	KeyDefaultCastingToken: true,
}

// AgentAPI identifies the AgentAPI deployment used for builds.
type AgentAPI struct {
	BaseURL       string
	APIKey        string
	AgentID       string
	AgentVersion  int
	EnvironmentID string
}

// Ready reports whether a build can be started. AgentAPI requires an
// environment because every build creates its session inside one.
func (a AgentAPI) Ready() bool {
	return a.BaseURL != "" && a.APIKey != "" && a.AgentID != "" && a.EnvironmentID != ""
}

// Linx holds the platform's Linx credentials. Every user's agent is created
// with this key.
type Linx struct {
	APIBase string
	OTAURL  string
	APIKey  string

	// PublicMCPURL is the address Linx cloud uses to reach the gateway MCP.
	PublicMCPURL string
	MCPName      string
	// MCPToken is the bearer token Linx presents to the gateway MCP.
	MCPToken      string
	AssistantName string
	LLMModelID    string
	MemModelID    string
	IntentModelID string
}

// Defaults for agent creation parameters an administrator has not set.
const (
	DefaultLinxMCPName       = "Open Anywhere"
	DefaultLinxAssistantName = "小灵"
	DefaultLinxLLMModelID    = "f3626c105383d71654a57f2bb8a973f3"
	DefaultLinxMemModelID    = "Memory_nomem"
	DefaultLinxIntentModelID = "Intent_function_call"
)

// withDefaults fills agent-creation parameters that were never configured.
func (l Linx) withDefaults() Linx {
	fill := func(target *string, value string) {
		if *target == "" {
			*target = value
		}
	}
	fill(&l.MCPName, DefaultLinxMCPName)
	fill(&l.AssistantName, DefaultLinxAssistantName)
	fill(&l.LLMModelID, DefaultLinxLLMModelID)
	fill(&l.MemModelID, DefaultLinxMemModelID)
	fill(&l.IntentModelID, DefaultLinxIntentModelID)
	return l
}

// MCPReady reports whether provisioning can register the gateway MCP with Linx.
func (l Linx) MCPReady() bool { return l.PublicMCPURL != "" && l.MCPToken != "" }

// Ready reports whether provisioning can talk to Linx.
func (l Linx) Ready() bool { return l.APIBase != "" && l.APIKey != "" }

// ServiceDefaults are the platform-managed Render and Casting services.
type ServiceDefaults struct {
	RenderURL    string
	RenderToken  string
	CastingURL   string
	CastingToken string
}

// Ready reports whether both managed services are configured.
func (s ServiceDefaults) Ready() bool { return s.RenderURL != "" && s.CastingURL != "" }

// Registration controls whether the public sign-up endpoint accepts requests.
type Registration struct {
	Open bool
}

// Snapshot is the complete platform configuration.
type Snapshot struct {
	AgentAPI     AgentAPI
	Linx         Linx
	Services     ServiceDefaults
	Registration Registration
}

// SecretState describes a secret without revealing it.
type SecretState struct {
	Configured bool       `json:"configured"`
	UpdatedAt  *time.Time `json:"updated_at,omitempty"`
}

// AgentAPIMasked is the admin-visible AgentAPI configuration.
type AgentAPIMasked struct {
	BaseURL       string      `json:"base_url"`
	AgentID       string      `json:"agent_id"`
	AgentVersion  int         `json:"agent_version"`
	EnvironmentID string      `json:"environment_id"`
	APIKey        SecretState `json:"api_key"`
}

// LinxMasked is the admin-visible Linx configuration.
type LinxMasked struct {
	APIBase string      `json:"api_base"`
	OTAURL  string      `json:"ota_url"`
	APIKey  SecretState `json:"api_key"`

	PublicMCPURL  string      `json:"public_mcp_url"`
	MCPName       string      `json:"mcp_name"`
	MCPToken      SecretState `json:"mcp_token"`
	AssistantName string      `json:"assistant_name"`
	LLMModelID    string      `json:"llm_model_id"`
	MemModelID    string      `json:"mem_model_id"`
	IntentModelID string      `json:"intent_model_id"`
}

// ServicesMasked is the admin-visible default service configuration.
type ServicesMasked struct {
	RenderURL    string      `json:"render_url"`
	CastingURL   string      `json:"casting_url"`
	RenderToken  SecretState `json:"render_token"`
	CastingToken SecretState `json:"casting_token"`
}

// ReadyFlags summarizes which upstream groups are usable.
type ReadyFlags struct {
	AgentAPI bool `json:"agentapi"`
	Linx     bool `json:"linx"`
	Services bool `json:"services"`
}

// Masked is the admin settings representation. Secret values appear only as
// configured flags.
type Masked struct {
	AgentAPI     AgentAPIMasked `json:"agentapi"`
	Linx         LinxMasked     `json:"linx"`
	Services     ServicesMasked `json:"services"`
	Registration Registration   `json:"registration"`
	Ready        ReadyFlags     `json:"ready"`
}

// Provider reads and writes platform settings.
type Provider struct {
	db                 *store.DB
	box                *secret.Box
	now                func() time.Time
	validateURL        func(string) error
	validateURLContext func(context.Context, string) error
}

// New returns a provider. box may be nil only in tests that never touch a
// secret key.
func New(db *store.DB, box *secret.Box) *Provider {
	return &Provider{db: db, box: box, now: func() time.Time { return time.Now().UTC() }}
}

// SetURLValidator installs the outbound URL policy used when addresses are
// saved. The netguard-backed validator lives in the endpoints package.
func (p *Provider) SetURLValidator(validate func(string) error) {
	p.validateURL = validate
}

// SetURLValidatorContext installs a policy that can resolve the host before an
// address is saved, so a private target is rejected at the source.
func (p *Provider) SetURLValidatorContext(validate func(context.Context, string) error) {
	p.validateURLContext = validate
}

// Seed writes default values for keys that do not exist yet. Existing values
// are never overwritten: once an administrator edits a setting, the database
// is authoritative even if gateway.yaml changes.
func (p *Provider) Seed(ctx context.Context, seeds []Seed) error {
	existing, err := p.db.PlatformSettings(ctx)
	if err != nil {
		return err
	}
	var entries []store.PlatformSetting
	for _, seed := range seeds {
		if seed.Key == "" || seed.Value == "" {
			continue
		}
		if _, ok := existing[seed.Key]; ok {
			continue
		}
		value := seed.Value
		if secretKeys[seed.Key] {
			if p.box == nil {
				return fmt.Errorf("settings: cannot seed secret %s without a master key", seed.Key)
			}
			value, err = p.box.Encrypt(seed.Value, secret.PlatformAAD(seed.Key))
			if err != nil {
				return err
			}
		}
		entries = append(entries, store.PlatformSetting{
			Key:       seed.Key,
			Value:     value,
			IsSecret:  secretKeys[seed.Key],
			UpdatedAt: p.now(),
			UpdatedBy: "bootstrap",
		})
	}
	return p.db.PutPlatformSettings(ctx, entries)
}

// Seed is one bootstrap value from gateway.yaml.
type Seed struct {
	Key   string
	Value string
}

// Snapshot returns the effective platform configuration. A stored secret that
// cannot be decrypted is an error: the process must not fall back to a fresh
// key and silently lose the administrator's credentials.
func (p *Provider) Snapshot(ctx context.Context) (Snapshot, error) {
	rows, err := p.db.PlatformSettings(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	read := func(key string) (string, error) {
		row, ok := rows[key]
		if !ok || row.Value == "" {
			return "", nil
		}
		if secretKeys[key] {
			if p.box == nil {
				return "", fmt.Errorf("settings: %s is encrypted but no master key is available", key)
			}
			return p.box.Decrypt(row.Value, secret.PlatformAAD(key))
		}
		return row.Value, nil
	}
	var snapshot Snapshot
	for _, item := range []struct {
		key    string
		target *string
	}{
		{KeyAgentAPIBaseURL, &snapshot.AgentAPI.BaseURL},
		{KeyAgentAPIKey, &snapshot.AgentAPI.APIKey},
		{KeyAgentAPIAgentID, &snapshot.AgentAPI.AgentID},
		{KeyAgentAPIEnvironmentID, &snapshot.AgentAPI.EnvironmentID},
		{KeyLinxAPIBase, &snapshot.Linx.APIBase},
		{KeyLinxOTAURL, &snapshot.Linx.OTAURL},
		{KeyLinxAPIKey, &snapshot.Linx.APIKey},
		{KeyLinxPublicMCPURL, &snapshot.Linx.PublicMCPURL},
		{KeyLinxMCPName, &snapshot.Linx.MCPName},
		{KeyLinxMCPToken, &snapshot.Linx.MCPToken},
		{KeyLinxAssistantName, &snapshot.Linx.AssistantName},
		{KeyLinxLLMModelID, &snapshot.Linx.LLMModelID},
		{KeyLinxMemModelID, &snapshot.Linx.MemModelID},
		{KeyLinxIntentModelID, &snapshot.Linx.IntentModelID},
		{KeyDefaultRenderURL, &snapshot.Services.RenderURL},
		{KeyDefaultRenderToken, &snapshot.Services.RenderToken},
		{KeyDefaultCastingURL, &snapshot.Services.CastingURL},
		{KeyDefaultCastingToken, &snapshot.Services.CastingToken},
	} {
		value, err := read(item.key)
		if err != nil {
			return Snapshot{}, err
		}
		*item.target = value
	}
	snapshot.Linx = snapshot.Linx.withDefaults()
	version, err := read(KeyAgentAPIAgentVersion)
	if err != nil {
		return Snapshot{}, err
	}
	if version != "" {
		parsed, err := strconv.Atoi(version)
		if err != nil || parsed < 0 {
			return Snapshot{}, fmt.Errorf("settings: invalid %s value %q", KeyAgentAPIAgentVersion, version)
		}
		snapshot.AgentAPI.AgentVersion = parsed
	}
	open, err := read(KeyRegistrationOpen)
	if err != nil {
		return Snapshot{}, err
	}
	switch strings.ToLower(strings.TrimSpace(open)) {
	case "", "false", "0":
		snapshot.Registration.Open = false
	case "true", "1":
		snapshot.Registration.Open = true
	default:
		return Snapshot{}, fmt.Errorf("settings: invalid %s value %q", KeyRegistrationOpen, open)
	}
	return snapshot, nil
}

// RegistrationOpen reports the switch alone, for handlers that only need that.
func (p *Provider) RegistrationOpen(ctx context.Context) (bool, error) {
	rows, err := p.db.PlatformSettings(ctx)
	if err != nil {
		return false, err
	}
	row, ok := rows[KeyRegistrationOpen]
	if !ok || row.Value == "" {
		return false, nil
	}
	value, err := strconv.ParseBool(strings.TrimSpace(row.Value))
	if err != nil {
		return false, fmt.Errorf("settings: invalid %s value", KeyRegistrationOpen)
	}
	return value, nil
}

// Masked returns the admin representation with secrets reduced to flags.
func (p *Provider) Masked(ctx context.Context) (Masked, error) {
	rows, err := p.db.PlatformSettings(ctx)
	if err != nil {
		return Masked{}, err
	}
	state := func(key string) SecretState {
		row, ok := rows[key]
		if !ok || row.Value == "" {
			return SecretState{}
		}
		updated := row.UpdatedAt
		return SecretState{Configured: true, UpdatedAt: &updated}
	}
	plain := func(key string) string {
		row, ok := rows[key]
		if !ok || secretKeys[key] {
			return ""
		}
		return row.Value
	}
	snapshot, err := p.Snapshot(ctx)
	if err != nil {
		return Masked{}, err
	}
	return Masked{
		AgentAPI: AgentAPIMasked{
			BaseURL:       plain(KeyAgentAPIBaseURL),
			AgentID:       plain(KeyAgentAPIAgentID),
			AgentVersion:  snapshot.AgentAPI.AgentVersion,
			EnvironmentID: plain(KeyAgentAPIEnvironmentID),
			APIKey:        state(KeyAgentAPIKey),
		},
		Linx: LinxMasked{
			APIBase: plain(KeyLinxAPIBase),
			OTAURL:  plain(KeyLinxOTAURL),
			APIKey:  state(KeyLinxAPIKey),

			PublicMCPURL:  plain(KeyLinxPublicMCPURL),
			MCPName:       snapshot.Linx.MCPName,
			MCPToken:      state(KeyLinxMCPToken),
			AssistantName: snapshot.Linx.AssistantName,
			LLMModelID:    snapshot.Linx.LLMModelID,
			MemModelID:    snapshot.Linx.MemModelID,
			IntentModelID: snapshot.Linx.IntentModelID,
		},
		Services: ServicesMasked{
			RenderURL:    plain(KeyDefaultRenderURL),
			CastingURL:   plain(KeyDefaultCastingURL),
			RenderToken:  state(KeyDefaultRenderToken),
			CastingToken: state(KeyDefaultCastingToken),
		},
		Registration: snapshot.Registration,
		Ready: ReadyFlags{
			AgentAPI: snapshot.AgentAPI.Ready(),
			Linx:     snapshot.Linx.Ready(),
			Services: snapshot.Services.Ready(),
		},
	}, nil
}

// SecretValue updates a secret. An empty Value with Clear false keeps the
// existing value; Clear removes it.
type SecretValue struct {
	Value string `json:"value"`
	Clear bool   `json:"clear"`
}

// AgentAPIPatch updates the AgentAPI group. Nil fields are unchanged.
type AgentAPIPatch struct {
	BaseURL       *string      `json:"base_url"`
	APIKey        *SecretValue `json:"api_key"`
	AgentID       *string      `json:"agent_id"`
	AgentVersion  *int         `json:"agent_version"`
	EnvironmentID *string      `json:"environment_id"`
}

// LinxPatch updates the Linx group.
type LinxPatch struct {
	APIBase *string      `json:"api_base"`
	OTAURL  *string      `json:"ota_url"`
	APIKey  *SecretValue `json:"api_key"`

	PublicMCPURL  *string `json:"public_mcp_url"`
	MCPName       *string `json:"mcp_name"`
	AssistantName *string `json:"assistant_name"`
	LLMModelID    *string `json:"llm_model_id"`
	MemModelID    *string `json:"mem_model_id"`
	IntentModelID *string `json:"intent_model_id"`
	// RotateMCPToken replaces the MCP bearer token with a new random value.
	RotateMCPToken bool `json:"rotate_mcp_token"`
}

// ServicesPatch updates the platform default services.
type ServicesPatch struct {
	RenderURL    *string      `json:"render_url"`
	CastingURL   *string      `json:"casting_url"`
	RenderToken  *SecretValue `json:"render_token"`
	CastingToken *SecretValue `json:"casting_token"`
}

// RegistrationPatch updates the registration switch.
type RegistrationPatch struct {
	Open *bool `json:"open"`
}

// Patch is a partial settings update.
type Patch struct {
	AgentAPI     *AgentAPIPatch     `json:"agentapi"`
	Linx         *LinxPatch         `json:"linx"`
	Services     *ServicesPatch     `json:"services"`
	Registration *RegistrationPatch `json:"registration"`
}

// ErrInvalidURL is returned for an address that fails policy.
var ErrInvalidURL = errors.New("settings: invalid service address")

// ErrInvalidSetting is returned for a value outside the accepted range.
var ErrInvalidSetting = errors.New("settings: invalid setting value")

// ServicesChanged reports whether a patch would change the saved default
// Render/Casting configuration. An empty secret means "keep", so it only
// counts as a change when it differs from the stored value or clears one.
func ServicesChanged(current Snapshot, patch *ServicesPatch) bool {
	if patch == nil {
		return false
	}
	if patch.RenderURL != nil && strings.TrimSpace(*patch.RenderURL) != current.Services.RenderURL {
		return true
	}
	if patch.CastingURL != nil && strings.TrimSpace(*patch.CastingURL) != current.Services.CastingURL {
		return true
	}
	return secretChanged(current.Services.RenderToken, patch.RenderToken) ||
		secretChanged(current.Services.CastingToken, patch.CastingToken)
}

// linxCredentialsChanged reports whether a patch changes the Linx account
// used to create agents and MCP resources. OTA-only edits keep the cached
// MCP IDs because they still belong to the same account.
func linxCredentialsChanged(current Linx, patch *LinxPatch) bool {
	if patch == nil {
		return false
	}
	if patch.APIBase != nil && strings.TrimSpace(*patch.APIBase) != current.APIBase {
		return true
	}
	// The cached server and instance describe one address and one name.
	if patch.PublicMCPURL != nil && strings.TrimSpace(*patch.PublicMCPURL) != current.PublicMCPURL {
		return true
	}
	if patch.MCPName != nil && strings.TrimSpace(*patch.MCPName) != current.MCPName {
		return true
	}
	return secretChanged(current.APIKey, patch.APIKey)
}

func secretChanged(current string, value *SecretValue) bool {
	if value == nil {
		return false
	}
	if value.Clear {
		return current != ""
	}
	trimmed := strings.TrimSpace(value.Value)
	return trimmed != "" && trimmed != current
}

func (p *Provider) checkURL(ctx context.Context, key, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if p.validateURLContext != nil {
		if err := p.validateURLContext(ctx, value); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidURL, err)
		}
		return nil
	}
	if p.validateURL != nil {
		if err := p.validateURL(value); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidURL, err)
		}
		return nil
	}
	if parsed, err := url.Parse(value); err != nil || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%w: %s must be an absolute HTTP(S) URL", ErrInvalidURL, key)
	}
	return nil
}

// Preview returns the configuration a patch would produce without writing it.
// An empty secret keeps the stored value; Clear removes it. Addresses in the
// patch are validated with the same policy as Update.
func (p *Provider) Preview(ctx context.Context, patch Patch) (Snapshot, error) {
	snapshot, err := p.Snapshot(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	urlValue := func(key string, value *string, target *string) error {
		if value == nil {
			return nil
		}
		trimmed := strings.TrimSpace(*value)
		if err := p.checkURL(ctx, key, trimmed); err != nil {
			return err
		}
		*target = trimmed
		return nil
	}
	secretValue := func(value *SecretValue, target *string) {
		if value == nil {
			return
		}
		if value.Clear {
			*target = ""
			return
		}
		if strings.TrimSpace(value.Value) == "" {
			return
		}
		*target = value.Value
	}
	if group := patch.AgentAPI; group != nil {
		if err := urlValue(KeyAgentAPIBaseURL, group.BaseURL, &snapshot.AgentAPI.BaseURL); err != nil {
			return Snapshot{}, err
		}
		if group.AgentID != nil {
			snapshot.AgentAPI.AgentID = strings.TrimSpace(*group.AgentID)
		}
		if group.AgentVersion != nil {
			if *group.AgentVersion < 0 {
				return Snapshot{}, fmt.Errorf("%w: agent version must not be negative", ErrInvalidSetting)
			}
			snapshot.AgentAPI.AgentVersion = *group.AgentVersion
		}
		if group.EnvironmentID != nil {
			snapshot.AgentAPI.EnvironmentID = strings.TrimSpace(*group.EnvironmentID)
		}
		secretValue(group.APIKey, &snapshot.AgentAPI.APIKey)
	}
	if group := patch.Linx; group != nil {
		if err := urlValue(KeyLinxAPIBase, group.APIBase, &snapshot.Linx.APIBase); err != nil {
			return Snapshot{}, err
		}
		if err := urlValue(KeyLinxOTAURL, group.OTAURL, &snapshot.Linx.OTAURL); err != nil {
			return Snapshot{}, err
		}
		secretValue(group.APIKey, &snapshot.Linx.APIKey)
		if err := urlValue(KeyLinxPublicMCPURL, group.PublicMCPURL, &snapshot.Linx.PublicMCPURL); err != nil {
			return Snapshot{}, err
		}
		for _, field := range []struct {
			value  *string
			target *string
		}{
			{group.MCPName, &snapshot.Linx.MCPName},
			{group.AssistantName, &snapshot.Linx.AssistantName},
			{group.LLMModelID, &snapshot.Linx.LLMModelID},
			{group.MemModelID, &snapshot.Linx.MemModelID},
			{group.IntentModelID, &snapshot.Linx.IntentModelID},
		} {
			if field.value != nil {
				*field.target = strings.TrimSpace(*field.value)
			}
		}
		snapshot.Linx = snapshot.Linx.withDefaults()
	}
	if group := patch.Services; group != nil {
		if err := urlValue(KeyDefaultRenderURL, group.RenderURL, &snapshot.Services.RenderURL); err != nil {
			return Snapshot{}, err
		}
		if err := urlValue(KeyDefaultCastingURL, group.CastingURL, &snapshot.Services.CastingURL); err != nil {
			return Snapshot{}, err
		}
		secretValue(group.RenderToken, &snapshot.Services.RenderToken)
		secretValue(group.CastingToken, &snapshot.Services.CastingToken)
	}
	if group := patch.Registration; group != nil && group.Open != nil {
		snapshot.Registration.Open = *group.Open
	}
	return snapshot, nil
}

// Update applies a patch. It returns ErrInvalidURL for a rejected address.
func (p *Provider) Update(ctx context.Context, patch Patch, actorID string) error {
	current, err := p.Snapshot(ctx)
	if err != nil {
		return err
	}
	now := p.now()
	var (
		entries []store.PlatformSetting
		removes []string
	)
	put := func(key, value string) {
		entries = append(entries, store.PlatformSetting{
			Key: key, Value: value, IsSecret: secretKeys[key], UpdatedAt: now, UpdatedBy: actorID,
		})
	}
	putURL := func(key string, value string) error {
		value = strings.TrimSpace(value)
		if value != "" {
			if err := p.checkURL(ctx, key, value); err != nil {
				return err
			}
		}
		put(key, value)
		return nil
	}
	putSecret := func(key string, value *SecretValue) error {
		if value == nil {
			return nil
		}
		if value.Clear {
			removes = append(removes, key)
			return nil
		}
		if strings.TrimSpace(value.Value) == "" {
			return nil
		}
		if p.box == nil {
			return fmt.Errorf("settings: cannot store secret %s without a master key", key)
		}
		encrypted, err := p.box.Encrypt(value.Value, secret.PlatformAAD(key))
		if err != nil {
			return err
		}
		put(key, encrypted)
		return nil
	}
	if patch.AgentAPI != nil {
		group := patch.AgentAPI
		if group.BaseURL != nil {
			if err := putURL(KeyAgentAPIBaseURL, *group.BaseURL); err != nil {
				return err
			}
		}
		if group.AgentID != nil {
			put(KeyAgentAPIAgentID, strings.TrimSpace(*group.AgentID))
		}
		if group.AgentVersion != nil {
			if *group.AgentVersion < 0 {
				return fmt.Errorf("%w: agent version must not be negative", ErrInvalidSetting)
			}
			put(KeyAgentAPIAgentVersion, strconv.Itoa(*group.AgentVersion))
		}
		if group.EnvironmentID != nil {
			put(KeyAgentAPIEnvironmentID, strings.TrimSpace(*group.EnvironmentID))
		}
		if err := putSecret(KeyAgentAPIKey, group.APIKey); err != nil {
			return err
		}
	}
	if patch.Linx != nil {
		group := patch.Linx
		if group.APIBase != nil {
			if err := putURL(KeyLinxAPIBase, *group.APIBase); err != nil {
				return err
			}
		}
		if group.OTAURL != nil {
			if err := putURL(KeyLinxOTAURL, *group.OTAURL); err != nil {
				return err
			}
		}
		if err := putSecret(KeyLinxAPIKey, group.APIKey); err != nil {
			return err
		}
		if group.PublicMCPURL != nil {
			if err := putURL(KeyLinxPublicMCPURL, *group.PublicMCPURL); err != nil {
				return err
			}
		}
		for _, field := range []struct {
			key   string
			value *string
		}{
			{KeyLinxMCPName, group.MCPName},
			{KeyLinxAssistantName, group.AssistantName},
			{KeyLinxLLMModelID, group.LLMModelID},
			{KeyLinxMemModelID, group.MemModelID},
			{KeyLinxIntentModelID, group.IntentModelID},
		} {
			if field.value != nil {
				put(field.key, strings.TrimSpace(*field.value))
			}
		}
		if group.RotateMCPToken {
			token, err := GenerateMCPToken()
			if err != nil {
				return err
			}
			if err := putSecret(KeyLinxMCPToken, &SecretValue{Value: token}); err != nil {
				return err
			}
		}
	}
	if patch.Services != nil {
		group := patch.Services
		if group.RenderURL != nil {
			if err := putURL(KeyDefaultRenderURL, *group.RenderURL); err != nil {
				return err
			}
		}
		if group.CastingURL != nil {
			if err := putURL(KeyDefaultCastingURL, *group.CastingURL); err != nil {
				return err
			}
		}
		if err := putSecret(KeyDefaultRenderToken, group.RenderToken); err != nil {
			return err
		}
		if err := putSecret(KeyDefaultCastingToken, group.CastingToken); err != nil {
			return err
		}
	}
	if patch.Registration != nil && patch.Registration.Open != nil {
		put(KeyRegistrationOpen, strconv.FormatBool(*patch.Registration.Open))
	}
	if err := p.db.PutPlatformSettings(ctx, entries); err != nil {
		return err
	}
	if err := p.db.DeletePlatformSettings(ctx, removes...); err != nil {
		return err
	}
	// Cached MCP resources belong to the previous Linx account. Clearing them
	// makes the next provisioning run recreate the server and instance.
	if linxCredentialsChanged(current.Linx, patch.Linx) {
		if err := p.db.DeletePortalSettings(ctx, "linx.mcp_server_id", "linx.mcp_instance_id"); err != nil {
			return err
		}
	}
	return nil
}

// GenerateMCPToken returns a new random bearer token for the gateway MCP.
func GenerateMCPToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("settings: generate MCP token: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

// EnsureMCPToken makes sure a gateway MCP token exists. The first start uses
// bootstrap (a legacy gateway.yaml value) when given, otherwise a random one.
// A stored value is never overwritten here; rotate it from the admin page.
func (p *Provider) EnsureMCPToken(ctx context.Context, bootstrap string) error {
	rows, err := p.db.PlatformSettings(ctx)
	if err != nil {
		return err
	}
	if row, ok := rows[KeyLinxMCPToken]; ok && row.Value != "" {
		return nil
	}
	token := strings.TrimSpace(bootstrap)
	if token == "" {
		if token, err = GenerateMCPToken(); err != nil {
			return err
		}
	}
	if p.box == nil {
		return errors.New("settings: cannot store the MCP token without a master key")
	}
	encrypted, err := p.box.Encrypt(token, secret.PlatformAAD(KeyLinxMCPToken))
	if err != nil {
		return err
	}
	return p.db.PutPlatformSettings(ctx, []store.PlatformSetting{{
		Key: KeyLinxMCPToken, Value: encrypted, IsSecret: true, UpdatedAt: p.now(), UpdatedBy: "bootstrap",
	}})
}

// MCPToken returns the current gateway MCP token, or "" when none is stored.
func (p *Provider) MCPToken(ctx context.Context) (string, error) {
	rows, err := p.db.PlatformSettings(ctx)
	if err != nil {
		return "", err
	}
	row, ok := rows[KeyLinxMCPToken]
	if !ok || row.Value == "" {
		return "", nil
	}
	if p.box == nil {
		return "", fmt.Errorf("settings: %s is encrypted but no master key is available", KeyLinxMCPToken)
	}
	return p.box.Decrypt(row.Value, secret.PlatformAAD(KeyLinxMCPToken))
}
