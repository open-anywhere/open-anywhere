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
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/artifact"
	"github.com/open-anywhere/open-anywhere/internal/gateway/build"
	"github.com/open-anywhere/open-anywhere/internal/gateway/config"
	"github.com/open-anywhere/open-anywhere/internal/gateway/mcp"
	"github.com/open-anywhere/open-anywhere/internal/gateway/render"
	"github.com/open-anywhere/open-anywhere/internal/gateway/screen"
	"github.com/open-anywhere/open-anywhere/internal/gateway/service"
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

	agentapiClient := agentapi.New(cfg.AgentAPIURL, cfg.AgentAPIKey, cfg.HTTPTimeout)
	renderClient := render.New(cfg.RenderURL, cfg.HTTPTimeout)
	castingClient := casting.NewClient(cfg.CastingURL, cfg.HTTPTimeout)

	server := mcp.New(nil, cfg.CallTimeout)
	builder := build.New(agentapiClient, artifactStore, registry, server, build.Config{
		AgentID:       cfg.AgentID,
		AgentVersion:  cfg.AgentVersion,
		EnvironmentID: cfg.EnvironmentID,
		BuildTimeout:  cfg.BuildTimeout,
		ArtifactLimit: artifact.MaxSize,
	})
	apps := service.New(registry, builder, renderClient, castingClient, catalog, server, service.Config{
		StartTimeout:      cfg.StartTimeout,
		ProjectionTimeout: cfg.ProjectionTimeout,
		OperationsTimeout: cfg.HTTPTimeout,
		MonitorInterval:   cfg.MonitorInterval,
		PortMin:           cfg.PortMin,
		PortMax:           cfg.PortMax,
	})
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
	mux.Handle("/mcp", server.StreamableHTTP(cfg.PublicHost, cfg.SpeakerProbe))
	mux.HandleFunc("GET /sse", server.ServeSSE)
	mux.Handle("POST /mcp/message", http.MaxBytesHandler(http.HandlerFunc(server.ServeMessage), 1<<20))
	mux.HandleFunc("GET /artifacts/{app_id}/{name}", artifactStore.ServeDownload)
	mux.HandleFunc("GET /healthz", healthHandler(database))

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mcpAuth(cfg.MCPToken, mux),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	result := make(chan error, 1)
	go func() { result <- httpServer.ListenAndServe() }()
	log.Printf("gateway listening on %s", cfg.Addr)

	return awaitShutdown(ctx, httpServer, server.Close, result)
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
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mcp" || r.URL.Path == "/mcp/message" || r.URL.Path == "/sse" {
			if token != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
				http.Error(w, "Unauthorized", 401)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
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
