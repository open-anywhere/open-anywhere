// Gateway is the Go control-plane service for registered OpenAPI applications.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/open-anywhere/open-anywhere/gateway/internal/mcpserver"
)

func run(ctx context.Context, configPath, addr string, probe bool, publicHost ...string) error {
	deps, err := loadDependencies(ctx, configPath)
	if err != nil {
		return err
	}
	if deps.apps != nil {
		defer deps.apps.Close()
	}

	host := ""
	if len(publicHost) > 0 {
		host = publicHost[0]
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	handler := mcpserver.Handler(deps.controller, mcpserver.Options{Applications: deps.apps, PublicHost: host, SpeakerProbe: probe, Token: os.Getenv("MCP_TOKEN"), Logger: logger})
	mux := http.NewServeMux()
	mux.Handle("/", handler)
	if deps.artifacts != nil {
		mux.Handle("/artifacts/", deps.artifacts)
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	logger.Info("gateway_started", "addr", listener.Addr().String(), "runtime", "go", "speaker_probe", probe)
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
			return err
		}
		return nil
	}
}
func main() {
	cfg := flag.String("config", "gateway/config/gateway.local.json", "Local Gateway configuration path")
	addr := flag.String("listen", "127.0.0.1:8787", "HTTP listen address")
	probe := flag.Bool("speaker-probe", false, "Enable speaker parameter diagnostic tool")
	publicHost := flag.String("public-host", "", "Explicit public Host allowed through reverse proxy (host:port)")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, *cfg, *addr, *probe, *publicHost); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
