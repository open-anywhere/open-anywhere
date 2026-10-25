// Command gateway runs the Open Anywhere control plane: an MCP server for
// Linx, an AgentAPI build orchestrator, and a Render/casting coordinator.
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/artifact"
	"github.com/open-anywhere/open-anywhere/internal/gateway/build"
	"github.com/open-anywhere/open-anywhere/internal/gateway/config"
	"github.com/open-anywhere/open-anywhere/internal/gateway/endpoints"
	"github.com/open-anywhere/open-anywhere/internal/gateway/mcp"
	"github.com/open-anywhere/open-anywhere/internal/gateway/netguard"
	"github.com/open-anywhere/open-anywhere/internal/gateway/portal"
	"github.com/open-anywhere/open-anywhere/internal/gateway/render"
	"github.com/open-anywhere/open-anywhere/internal/gateway/screen"
	"github.com/open-anywhere/open-anywhere/internal/gateway/secret"
	"github.com/open-anywhere/open-anywhere/internal/gateway/service"
	"github.com/open-anywhere/open-anywhere/internal/gateway/settings"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
	"github.com/open-anywhere/open-anywhere/pkg/agentapi"
	"github.com/open-anywhere/open-anywhere/pkg/casting"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := config.Parse(args, os.Getenv)
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return serve(ctx, cfg)
}

func serve(ctx context.Context, cfg config.Config) error {
	catalog, err := screen.LoadCatalog(cfg.ScreensPath)
	if err != nil {
		return err
	}
	database, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer database.Close()

	// Portal mode reads upstream credentials from platform settings so an
	// administrator can configure or rotate them without a restart.
	var (
		guard      *netguard.Guard
		platform   *settings.Provider
		resolver   *endpoints.Resolver
		clientPool *endpoints.Pool
	)
	if cfg.PortalEnabled {
		// Refuse to generate a fresh key when the database already holds
		// ciphertext: the new key could never open it and would replace the
		// operator's only pointer to the original.
		if !secret.Available(cfg.PortalDataDir, os.Getenv) {
			encrypted, err := database.HasEncryptedSecrets(ctx)
			if err != nil {
				return err
			}
			if encrypted {
				return fmt.Errorf("stored credentials are encrypted but no master key is configured; "+
					"restore %s/%s or set %s to the original key",
					cfg.PortalDataDir, secret.KeyFile, secret.EnvKey)
			}
		}
		box, err := secret.Load(cfg.PortalDataDir, os.Getenv)
		if err != nil {
			return err
		}
		guard = netguard.New(cfg.AllowPrivateTargets)
		platform = settings.New(database, box)
		platform.SetURLValidatorContext(guard.CheckURL)
		if err := seedPlatformSettings(ctx, platform, cfg); err != nil {
			return err
		}
		// The MCP bearer token lives in the database: generated on first start,
		// or adopted once from a legacy mcp_token in gateway.yaml.
		if err := platform.EnsureMCPToken(ctx, cfg.MCPToken); err != nil {
			return err
		}
		// Fail at startup, not on the first request, when stored ciphertext
		// cannot be opened with the configured master key. Silently generating
		// a new key would make the administrator's credentials unusable.
		if _, err := platform.Snapshot(ctx); err != nil {
			return credentialError(cfg, "platform settings", err)
		}
		resolver = endpoints.NewResolver(database, platform, box, guard, catalog)
		if err := resolver.ValidateSecrets(ctx); err != nil {
			return credentialError(cfg, "service bindings", err)
		}
		clientPool = endpoints.NewPool(guard, cfg.HTTPTimeout, cfg.StartTimeout+cfg.HTTPTimeout)
	}

	registry := app.NewRegistry()
	registry.SetPersister(database)
	artifactStore, err := artifact.NewStore(cfg.ArtifactsDir, cfg.ArtifactBaseURL, database)
	if err != nil {
		return err
	}
	persisted, err := database.ListApps()
	if err != nil {
		return err
	}
	// Artifact locations follow deployment configuration, including when the
	// public origin or storage directory changes after a restart.
	for i := range persisted {
		value := &persisted[i]
		if item, ok := artifactStore.Get(value.ID); ok {
			value.Artifact.Path = item.Path
			value.Artifact.DownloadURL = item.DownloadURL
			value.Revision++
			if err := database.SaveApp(*value); err != nil {
				return fmt.Errorf("restore app artifact location: %w", err)
			}
		}
	}
	registry.Load(persisted)

	server := mcp.New(nil, cfg.CallTimeout)
	var builder *build.Builder
	if cfg.PortalEnabled {
		builder = build.NewWithProvider(func(ctx context.Context) (build.Platform, error) {
			snapshot, err := platform.Snapshot(ctx)
			if err != nil {
				return build.Platform{}, err
			}
			if !snapshot.AgentAPI.Ready() {
				return build.Platform{}, app.ErrPlatformNotConfigured
			}
			return build.Platform{
				Client:        agentapi.New(snapshot.AgentAPI.BaseURL, snapshot.AgentAPI.APIKey, cfg.HTTPTimeout),
				AgentID:       snapshot.AgentAPI.AgentID,
				AgentVersion:  snapshot.AgentAPI.AgentVersion,
				EnvironmentID: snapshot.AgentAPI.EnvironmentID,
			}, nil
		}, artifactStore, registry, server, build.Config{
			BuildTimeout:  cfg.BuildTimeout,
			ArtifactLimit: artifact.MaxSize,
		})
	} else {
		agentapiClient := agentapi.New(cfg.AgentAPIURL, cfg.AgentAPIKey, cfg.HTTPTimeout)
		builder = build.New(agentapiClient, artifactStore, registry, server, build.Config{
			AgentID:       cfg.AgentID,
			AgentVersion:  cfg.AgentVersion,
			EnvironmentID: cfg.EnvironmentID,
			BuildTimeout:  cfg.BuildTimeout,
			ArtifactLimit: artifact.MaxSize,
		})
	}
	serviceConfig := service.Config{
		StartTimeout:      cfg.StartTimeout,
		ProjectionTimeout: cfg.ProjectionTimeout,
		OperationsTimeout: cfg.HTTPTimeout,
		MonitorInterval:   cfg.MonitorInterval,
		PortMin:           cfg.PortMin,
		PortMax:           cfg.PortMax,
	}
	var apps *service.Service
	if cfg.PortalEnabled {
		apps = service.NewWithBindings(registry, builder, resolver, clientPool, server, serviceConfig)
		apps.SetURLValidator(guard.CheckURL)
		apps.SetHTTPClient(guard.Client(cfg.HTTPTimeout))
		server.SetOwnerValidator(resolver)
	} else {
		// A start request may legitimately use the whole startup budget, so the
		// client's per-request deadline must not cut it short at the ordinary
		// HTTP timeout (30s by default) while Render is still starting the app.
		renderClient := render.New(cfg.RenderURL, cfg.StartTimeout+cfg.HTTPTimeout)
		castingClient := casting.NewClient(cfg.CastingURL, cfg.HTTPTimeout)
		apps = service.New(registry, builder, renderClient, castingClient, catalog, server, serviceConfig)
	}
	apps.SetArtifactStore(artifactStore)
	if err := apps.RecoverRuntime(); err != nil {
		return err
	}
	apps.PruneHistory()
	server.SetTools(apps)
	server.Compatibility = true
	server.SpeakerProbe = cfg.SpeakerProbe

	for _, value := range persisted {
		if value.State != app.StateBuilding {
			continue
		}
		builder.Resume(value)
		log.Printf("resuming build %s (session %s)", value.ID, value.SessionID)
	}

	go apps.RunMonitor(ctx)

	mux := http.NewServeMux()
	mux.Handle("/mcp", server.StreamableHTTPHosts(mcpAllowedHosts(cfg, platform), cfg.SpeakerProbe))
	mux.HandleFunc("GET /sse", server.ServeSSE)
	mux.Handle("POST /mcp/message", http.MaxBytesHandler(http.HandlerFunc(server.ServeMessage), 1<<20))
	mux.HandleFunc("GET /artifacts/{app_id}/{name}", artifactStore.ServeDownload)
	mux.HandleFunc("GET /healthz", healthHandler(database))

	if cfg.PortalEnabled {
		portalService, err := portal.NewService(ctx, portal.BuildConfig{
			DataDir:           cfg.PortalDataDir,
			WebDir:            cfg.PortalWebDir,
			WebEmbedded:       cfg.PortalWebEmbedded,
			Username:          cfg.PortalUsername,
			Password:          cfg.PortalPassword,
			SessionTTL:        cfg.PortalSessionTTL,
			CookieSecure:      cfg.PortalCookieSecure,
			SystemPrompt:      cfg.LinxSystemPromptFile,
			ProvisionTimeout:  cfg.LinxProvisionTimeout,
			TokenPollInterval: cfg.LinxTokenPollInterval,
			TokenPollAttempts: cfg.LinxTokenPollAttempts,
		}, database, apps, platform, resolver, clientPool)
		if err != nil {
			return err
		}
		defer portalService.Voice.StopAll()
		if err := portalService.Provision.Recover(ctx); err != nil {
			return err
		}
		portalService.Auth.CleanupExpiredSessions(ctx)
		mux.Handle("/api/portal/", portalService.API.Handler())
		mux.Handle("/api/admin/", portalService.API.AdminHandler())
		mux.Handle("/", portalService.API.WebHandler())
		mux.Handle("/voice/", portalService.API.VoiceHandler())
		if snapshot, err := platform.Snapshot(ctx); err == nil &&
			strings.HasSuffix(strings.TrimRight(snapshot.Linx.PublicMCPURL, "/"), "/sse") {
			log.Printf("warning: linx.public_mcp_url ends with /sse; new Linx MCP servers use Streamable HTTP, configure .../mcp")
		}
		log.Printf("portal enabled at /portal")
	}

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mcpAuthFunc(mcpTokenSource(cfg, platform), mux),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	result := make(chan error, 1)
	go func() { result <- httpServer.ListenAndServe() }()
	log.Printf("gateway listening on %s", cfg.Addr)
	if cfg.PortalEnabled {
		base := browseURL(cfg.Addr)
		log.Printf("portal: %s/ (admin: %s/admin)", base, base)
	}

	return awaitShutdown(ctx, httpServer, server.Close, result)
}

// credentialError turns a master-key mismatch into an actionable startup
// failure. Other storage errors are reported as they are.
func credentialError(cfg config.Config, area string, err error) error {
	if errors.Is(err, secret.ErrDecrypt) {
		return fmt.Errorf("%s cannot be decrypted with the configured master key; "+
			"restore %s/%s or set %s to the original key: %w",
			area, cfg.PortalDataDir, secret.KeyFile, secret.EnvKey, err)
	}
	return fmt.Errorf("%s: %w", area, err)
}

// seedPlatformSettings copies deployment values into platform settings that an
// administrator has not filled in yet. Existing values are never overwritten,
// so edits made in the admin UI survive restarts and config changes.
func seedPlatformSettings(ctx context.Context, provider *settings.Provider, cfg config.Config) error {
	seeds := []settings.Seed{
		{Key: settings.KeyAgentAPIBaseURL, Value: cfg.AgentAPIURL},
		{Key: settings.KeyAgentAPIKey, Value: cfg.AgentAPIKey},
		{Key: settings.KeyAgentAPIAgentID, Value: cfg.AgentID},
		{Key: settings.KeyAgentAPIEnvironmentID, Value: cfg.EnvironmentID},
		{Key: settings.KeyLinxAPIBase, Value: cfg.LinxAPIBase},
		{Key: settings.KeyLinxOTAURL, Value: cfg.LinxOTAURL},
		{Key: settings.KeyLinxAPIKey, Value: cfg.LinxAPIKey},
		{Key: settings.KeyDefaultRenderURL, Value: cfg.RenderURL},
		{Key: settings.KeyDefaultCastingURL, Value: cfg.CastingURL},
		// Agent template and MCP address are managed in the admin page; these
		// legacy gateway.yaml values only seed a database that has none yet.
		{Key: settings.KeyLinxPublicMCPURL, Value: cfg.LinxPublicMCPURL},
		{Key: settings.KeyLinxMCPName, Value: cfg.LinxMCPName},
		{Key: settings.KeyLinxAssistantName, Value: cfg.LinxAssistantName},
		{Key: settings.KeyLinxLLMModelID, Value: cfg.LinxLLMModelID},
		{Key: settings.KeyLinxMemModelID, Value: cfg.LinxMemModelID},
		{Key: settings.KeyLinxIntentModelID, Value: cfg.LinxIntentModelID},
	}
	if cfg.AgentVersion > 0 {
		seeds = append(seeds, settings.Seed{Key: settings.KeyAgentAPIAgentVersion, Value: strconv.Itoa(cfg.AgentVersion)})
	}
	if cfg.PortalAllowRegistration {
		seeds = append(seeds, settings.Seed{Key: settings.KeyRegistrationOpen, Value: "true"})
	}
	return provider.Seed(ctx, seeds)
}

// healthHandler reports readiness, including database availability.
func healthHandler(database *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := "ok"
		code := http.StatusOK
		pingCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := database.Ping(pingCtx); err != nil {
			status = "unavailable"
			code = http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": status})
	}
}

// mcpAuth requires the bearer token on MCP endpoints when one is configured.
func mcpAuth(token string, next http.Handler) http.Handler {
	return mcpAuthFunc(func(context.Context) (string, error) { return token, nil }, next)
}

// mcpAuthFunc reads the token on every MCP request, so a token rotated in the
// admin page applies immediately. A failed lookup denies the request.
func mcpAuthFunc(token func(context.Context) (string, error), next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mcp" || r.URL.Path == "/mcp/message" || r.URL.Path == "/sse" {
			want, err := token(r.Context())
			if err != nil {
				http.Error(w, "Unauthorized", 401)
				return
			}
			if want != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+want)) != 1 {
				http.Error(w, "Unauthorized", 401)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// mcpAllowedHosts lists the extra Host values /mcp accepts: public_host from
// the config file and, in portal mode, the host of the public MCP address in
// the admin settings. The settings are read at most every few seconds, so an
// address saved in the admin page applies without a restart.
func mcpAllowedHosts(cfg config.Config, platform *settings.Provider) func(context.Context) []string {
	var (
		mu      sync.Mutex
		hosts   []string
		expires time.Time
	)
	return func(ctx context.Context) []string {
		base := []string{}
		if cfg.PublicHost != "" {
			base = append(base, cfg.PublicHost)
		}
		if !cfg.PortalEnabled || platform == nil {
			return base
		}
		mu.Lock()
		defer mu.Unlock()
		if time.Now().Before(expires) {
			return append(base, hosts...)
		}
		// A failed read keeps the previous answer; it is retried on the next request.
		if snapshot, err := platform.Snapshot(ctx); err == nil {
			hosts = mcp.HostsFromURL(snapshot.Linx.PublicMCPURL)
			expires = time.Now().Add(5 * time.Second)
		}
		return append(base, hosts...)
	}
}

// mcpTokenSource is the database token in portal mode and the configured one
// otherwise.
func mcpTokenSource(cfg config.Config, platform *settings.Provider) func(context.Context) (string, error) {
	if cfg.PortalEnabled && platform != nil {
		return platform.MCPToken
	}
	return func(context.Context) (string, error) { return cfg.MCPToken, nil }
}

// awaitShutdown waits for cancellation or a serve failure and drains cleanly.
func awaitShutdown(ctx context.Context, httpServer *http.Server, closeMCP func(), result <-chan error) error {
	select {
	case <-ctx.Done():
		closeMCP()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			_ = httpServer.Close()
		}
		return nil
	case err := <-result:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// browseURL turns a listen address into a URL a browser on this machine can
// open. A wildcard or empty host is not browsable, so it becomes localhost.
func browseURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, port)
}
