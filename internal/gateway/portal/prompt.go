package portal

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/endpoints"
	"github.com/open-anywhere/open-anywhere/internal/gateway/linx"
	"github.com/open-anywhere/open-anywhere/internal/gateway/service"
	"github.com/open-anywhere/open-anywhere/internal/gateway/settings"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

// agent-prompt.txt is a copy of docs/agent-system-prompts/open-anywhere-host.txt.
// It is embedded so deployment does not depend on the repository checkout.
//
//go:embed agent-prompt.txt
var defaultSystemPrompt string

// SystemPrompt returns the agent prompt, preferring an operator-provided file.
func SystemPrompt(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return defaultSystemPrompt, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("portal: read system prompt: %w", err)
	}
	return string(content), nil
}

// BuildService creates the portal's auth, provisioning and voice layers and
// the HTTP handler. It is exported so cmd/gateway only wires configuration.
type BuildConfig struct {
	DataDir           string
	WebDir            string
	Username          string
	Password          string
	SessionTTL        time.Duration
	CookieSecure      bool
	LinxPublicMCPURL  string
	LinxMCPName       string
	LinxAssistantName string
	LinxLLMModelID    string
	LinxMemModelID    string
	LinxIntentModelID string
	SystemPrompt      string
	MCPToken          string
	ProvisionTimeout  time.Duration
	TokenPollInterval time.Duration
	TokenPollAttempts int
}

// Service is the complete portal.
type Service struct {
	Auth      *Auth
	Provision *Provisioner
	Voice     *VoiceManager
	API       *API
	Settings  *settings.Provider
	Resolver  *endpoints.Resolver
}

// NewService builds the portal from validated configuration. Platform
// AgentAPI and Linx credentials come from provider; only the gateway's own MCP
// address, token and agent template remain deployment configuration.
func NewService(ctx context.Context, cfg BuildConfig, db *store.DB, apps *service.Service, provider *settings.Provider, resolver *endpoints.Resolver, pool *endpoints.Pool) (*Service, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return nil, fmt.Errorf("portal: create data directory: %w", err)
	}
	if err := os.Chmod(cfg.DataDir, 0o700); err != nil {
		return nil, fmt.Errorf("portal: secure data directory: %w", err)
	}
	prompt, err := SystemPrompt(cfg.SystemPrompt)
	if err != nil {
		return nil, err
	}
	auth := NewAuth(db, AuthConfig{SessionTTL: cfg.SessionTTL, CookieSecure: cfg.CookieSecure})
	if _, err := auth.EnsureAdmin(ctx, Credentials{Username: cfg.Username, Password: cfg.Password}); err != nil {
		return nil, err
	}
	voice := NewVoiceManager(ctx)
	linxProvider := func(ctx context.Context) (*linx.Client, error) {
		if provider == nil {
			return nil, app.ErrPlatformNotConfigured
		}
		snapshot, err := provider.Snapshot(ctx)
		if err != nil {
			return nil, err
		}
		if !snapshot.Linx.Ready() {
			return nil, app.ErrPlatformNotConfigured
		}
		return linx.New(linx.Config{
			BaseURL:      snapshot.Linx.APIBase,
			APIKey:       snapshot.Linx.APIKey,
			OTAURL:       snapshot.Linx.OTAURL,
			PublicMCPURL: cfg.LinxPublicMCPURL,
			MCPName:      cfg.LinxMCPName,
			MCPToken:     cfg.MCPToken,
		}), nil
	}
	provision := NewProvisionerWithProvider(ctx, db, linxProvider, voice, ProvisionConfig{
		DataDir:           cfg.DataDir,
		MCPName:           cfg.LinxMCPName,
		MCPDescription:    "Open Anywhere 应用管理 MCP",
		PublicMCPURL:      cfg.LinxPublicMCPURL,
		MCPToken:          cfg.MCPToken,
		AssistantName:     cfg.LinxAssistantName,
		LLMModelID:        cfg.LinxLLMModelID,
		MemModelID:        cfg.LinxMemModelID,
		IntentModelID:     cfg.LinxIntentModelID,
		SystemPrompt:      prompt,
		ProvisionTimeout:  cfg.ProvisionTimeout,
		TokenPollInterval: cfg.TokenPollInterval,
		TokenPollAttempts: cfg.TokenPollAttempts,
	})
	api := NewAPI(db, auth, apps, voice, provision, cfg.WebDir)
	api.SetPlatform(provider, pool, resolver)
	logMissingWebDir(cfg.WebDir)
	return &Service{Auth: auth, Provision: provision, Voice: voice, API: api, Settings: provider, Resolver: resolver}, nil
}
