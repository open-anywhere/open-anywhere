package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLoadConfigPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "render.yaml")
	body := "" +
		"addr: \":18090\"\n" +
		"public_url: \"http://192.168.1.20:8090\"\n" +
		"app_host: \"10.0.0.8\"\n" +
		"work_dir: \"from-file\"\n" +
		"width: 1280\n" +
		"height: 720\n" +
		"fps: 8\n" +
		"token: \"from-file\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig([]string{"-config", path, "-fps", "15", "-public-url", "http://10.1.1.1:8090"}, func(key string) string {
		switch key {
		case "RENDER_TOKEN":
			return "from-env"
		case "RENDER_FPS":
			return "12"
		case "RENDER_APP_HOST":
			return "127.0.0.1"
		default:
			return ""
		}
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":18090" || cfg.WorkDir != "from-file" || cfg.Width != 1280 || cfg.Height != 720 {
		t.Fatalf("file values = %+v", cfg)
	}
	if cfg.PublicURL != "http://10.1.1.1:8090" || cfg.FPS != 15 {
		t.Fatalf("flag did not win: public %q fps %d", cfg.PublicURL, cfg.FPS)
	}
	if cfg.AppHost != "127.0.0.1" || cfg.Token != "from-env" {
		t.Fatalf("env did not override file: app_host %q token %q", cfg.AppHost, cfg.Token)
	}
}

func TestLoadConfigRejectsBadFiles(t *testing.T) {
	if _, err := loadConfig([]string{"-config"}, nil, io.Discard); err == nil {
		t.Fatal("missing config path accepted")
	}
	if _, err := loadConfig([]string{"-config", filepath.Join(t.TempDir(), "missing.yaml")}, nil, io.Discard); err == nil {
		t.Fatal("missing config file accepted")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "render.yaml")
	if err := os.WriteFile(path, []byte("unknown: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig([]string{"-config", path}, nil, io.Discard); err == nil {
		t.Fatal("unknown field accepted")
	}
	if _, err := loadConfig(nil, func(key string) string {
		if key == "RENDER_FPS" {
			return "fast"
		}
		return ""
	}, io.Discard); err == nil {
		t.Fatal("invalid RENDER_FPS accepted")
	}
}

func TestRunEnforcesTokenFromConfig(t *testing.T) {
	t.Setenv("RENDER_TOKEN", "")
	t.Setenv("RENDER_PUBLIC_URL", "")
	t.Setenv("RENDER_ADDR", "")
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	address := "127.0.0.1:" + strconv.Itoa(port)
	dir := t.TempDir()
	path := filepath.Join(dir, "render.yaml")
	secret := "config-secret"
	body := "addr: " + strconv.Quote(address) + "\n" +
		"public_url: " + strconv.Quote("http://"+address) + "\n" +
		"work_dir: " + strconv.Quote(dir) + "\n" +
		"token: " + strconv.Quote(secret) + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"-config", path}, io.Discard)
	}()
	client := http.Client{Timeout: time.Second}
	deadline := time.Now().Add(5 * time.Second)
	var health *http.Response
	for {
		health, err = client.Get("http://" + address + "/healthz")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("CLI not ready: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	drain(t, health)
	if health.StatusCode != http.StatusOK {
		t.Fatalf("health status %d", health.StatusCode)
	}
	open, err := client.Get("http://" + address + "/v1/ports")
	if err != nil {
		t.Fatal(err)
	}
	drain(t, open)
	if open.StatusCode != http.StatusUnauthorized {
		t.Fatalf("open control status %d", open.StatusCode)
	}
	req, err := http.NewRequest(http.MethodGet, "http://"+address+"/v1/ports", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	authed, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	drain(t, authed)
	if authed.StatusCode == http.StatusUnauthorized {
		t.Fatal("configured bearer was rejected")
	}
	stream, err := client.Get("http://" + address + "/streams/missing/live.ts")
	if err != nil {
		t.Fatal(err)
	}
	drain(t, stream)
	if stream.StatusCode != http.StatusNotFound {
		t.Fatalf("stream status %d", stream.StatusCode)
	}
	cancel()
	// http.Server.Shutdown only treats a never-used connection as idle once
	// Go's five-second grace expires, so a leftover connection would stretch
	// shutdown to ~5s. drain above keeps connections reusable; the deadline
	// stays well under the 20s graceful budget while tolerating that grace.
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("CLI shutdown timed out")
	}
	if strings.Contains(secret, "\n") {
		t.Fatal("secret fixture must stay a single line")
	}
}

// drain reads a response body to EOF before closing it. Closing an unread body
// makes the client abandon its connection, which leaves the server holding a
// connection that never completes a request and postpones Shutdown until Go's
// five-second grace for never-used connections expires.
func drain(t *testing.T, res *http.Response) {
	t.Helper()
	path := ""
	if res.Request != nil && res.Request.URL != nil {
		path = res.Request.URL.Path
	}
	if _, err := io.Copy(io.Discard, res.Body); err != nil {
		t.Errorf("read %s body: %v", path, err)
	}
	if err := res.Body.Close(); err != nil {
		t.Errorf("close %s body: %v", path, err)
	}
}
