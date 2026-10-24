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
	flags := flag.NewFlagSet("render", flag.ContinueOnError)
	flags.SetOutput(output)
	addr := flags.String("addr", ":8090", "API and media listen address")
	publicURL := flags.String("public-url", "", "HTTP origin reachable by gateway and TV (required)")
	appHost := flags.String("app-host", "", "application host reachable by gateway (defaults to public URL host)")
	work := flags.String("work-dir", ".render-data", "temporary instance directories")
	node := flags.String("node", "node", "Node.js executable")
	chrome := flags.String("chrome", "", "Chromium executable (auto-detected if empty)")
	ffmpeg := flags.String("ffmpeg", "ffmpeg", "FFmpeg executable with libx264")
	width := flags.Int("width", 1280, "video width")
	height := flags.Int("height", 720, "video height")
	fps := flags.Int("fps", 10, "capture frames per second")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("render does not accept positional arguments")
	}
	if *publicURL == "" {
		return errors.New("-public-url is required, e.g. http://192.168.1.20:8090")
	}
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	defer listener.Close()
	_, p, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(p)
	s, err := core.New(core.Config{WorkDir: *work, PublicURL: *publicURL, AppHost: *appHost, Node: *node, Chrome: *chrome, FFmpeg: *ffmpeg, Width: *width, Height: *height, FPS: *fps, ServicePorts: []int{port}})
	if err != nil {
		return err
	}
	server := &http.Server{Handler: core.NewHandler(s), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	errCh := make(chan error, 1)
	go func() { errCh <- server.Serve(listener) }()
	log.Printf("render listening on %s; public origin %s", listener.Addr(), *publicURL)
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
