// Command render runs Node.js application artifacts and streams their browser UI.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	core "github.com/open-anywhere/open-anywhere/internal/render"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stderr); err != nil && !errors.Is(err, flag.ErrHelp) {
		log.Print(err)
		os.Exit(1)
	}
}
func run(ctx context.Context, args []string, output io.Writer) error {
	cfg, err := loadConfig(args, os.Getenv, output)
	if err != nil {
		return err
	}
	if cfg.PublicURL == "" {
		return errors.New("public_url is required (render.yaml or -public-url), e.g. http://192.168.1.20:8090")
	}
	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}
	defer listener.Close()
	_, p, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(p)
	cfg.ServicePorts = []int{port}
	s, err := core.New(cfg.Config)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: core.NewHandler(s), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	errCh := make(chan error, 1)
	go func() { errCh <- server.Serve(listener) }()
	auth := "disabled"
	if cfg.Token != "" {
		auth = "enabled"
	}
	log.Printf("render listening on %s; public origin %s; /v1 bearer auth %s", listener.Addr(), cfg.PublicURL, auth)
	select {
	case <-ctx.Done():
	case err = <-errCh:
	}
	return finish(server, err, s.Close)
}

// finish stops the HTTP server and cleans up runtime instances, preferring a
// cleanup failure over the shutdown outcome.
func finish(server *http.Server, runErr error, cleanup func(context.Context) error) error {
	shutdown, stop := context.WithTimeout(context.Background(), 20*time.Second)
	defer stop()
	cleanupErr := cleanup(shutdown)
	if e := server.Shutdown(shutdown); e != nil {
		_ = server.Close()
		if runErr == nil {
			runErr = e
		}
	}
	if errors.Is(runErr, http.ErrServerClosed) {
		runErr = nil
	}
	if cleanupErr != nil {
		return fmt.Errorf("cleanup: %w", cleanupErr)
	}
	return runErr
}
