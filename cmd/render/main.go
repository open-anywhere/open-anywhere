// Command render runs Node.js application artifacts and streams their browser UI.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
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
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
func run() error {
	addr := flag.String("addr", ":8090", "API and media listen address")
	publicURL := flag.String("public-url", "", "HTTP origin reachable by gateway and TV (required)")
	appHost := flag.String("app-host", "", "application host reachable by gateway (defaults to public URL host)")
	work := flag.String("work-dir", ".render-data", "temporary instance directories")
	node := flag.String("node", "node", "Node.js executable")
	chrome := flag.String("chrome", "", "Chromium executable (auto-detected if empty)")
	ffmpeg := flag.String("ffmpeg", "ffmpeg", "FFmpeg executable with libx264")
	width := flag.Int("width", 1280, "video width")
	height := flag.Int("height", 720, "video height")
	fps := flag.Int("fps", 10, "capture frames per second")
	flag.Parse()
	if flag.NArg() != 0 {
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
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	server := &http.Server{Handler: core.NewHandler(s), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	errCh := make(chan error, 1)
	go func() { errCh <- server.Serve(listener) }()
	log.Printf("render listening on %s; public origin %s", listener.Addr(), *publicURL)
	select {
	case <-ctx.Done():
	case err = <-errCh:
	}
	shutdown, stop := context.WithTimeout(context.Background(), 20*time.Second)
	defer stop()
	cleanupErr := s.Close(shutdown)
	if e := server.Shutdown(shutdown); e != nil {
		_ = server.Close()
		if err == nil {
			err = e
		}
	}
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	if cleanupErr != nil {
		return fmt.Errorf("cleanup: %w", cleanupErr)
	}
	return err
}
