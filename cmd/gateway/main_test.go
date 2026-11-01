package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/config"
)

func testConfig(t *testing.T) config.Config {
	t.Helper()
	return config.Config{
		Addr:            "127.0.0.1:0",
		AgentAPIURL:     "http://127.0.0.1:1",
		AgentAPIKey:     "key",
		AgentID:         "agent",
		AgentVersion:    1,
		EnvironmentID:   "env",
		RenderURL:       "http://127.0.0.1:1",
		ArtifactsDir:    t.TempDir(),
		ArtifactBaseURL: "http://127.0.0.1:8090",
		DBPath:          filepath.Join(t.TempDir(), "gateway.db"),
		PortMin:         39000,
		PortMax:         39999,
		BuildTimeout:    time.Minute,
		StartTimeout:    time.Second,
		MonitorInterval: time.Hour,
	}
}

func TestServeStopsOnCancel(t *testing.T) {
	cfg := testConfig(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := serve(ctx, cfg); err != nil {
		t.Fatalf("serve: %v", err)
	}
}

func TestServeErrors(t *testing.T) {
	t.Run("database", func(t *testing.T) {
		cfg := testConfig(t)
		file := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg.DBPath = filepath.Join(file, "nested", "gateway.db")
		if err := serve(context.Background(), cfg); err == nil {
			t.Fatal("bad database path should fail")
		}
	})
	t.Run("artifacts", func(t *testing.T) {
		cfg := testConfig(t)
		file := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg.ArtifactsDir = file
		if err := serve(context.Background(), cfg); err == nil {
			t.Fatal("bad artifact directory should fail")
		}
	})
	t.Run("listen", func(t *testing.T) {
		cfg := testConfig(t)
		cfg.Addr = "not-an-address"
		if err := serve(context.Background(), cfg); err == nil {
			t.Fatal("bad listen address should fail")
		}
	})
}

func TestRunErrors(t *testing.T) {
	if err := run([]string{"-unknown"}); err == nil {
		t.Fatal("unknown flag should fail")
	}
	path := filepath.Join(t.TempDir(), "gateway.yaml")
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-config", path}); err == nil {
		t.Fatal("incomplete config should fail")
	}
	cfg := testConfig(t)
	args := []string{
		"-config", path,
		"-addr", "not-an-address",
		"-agentapi-url", cfg.AgentAPIURL,
		"-agentapi-key", cfg.AgentAPIKey,
		"-agent-id", cfg.AgentID,
		"-environment-id", cfg.EnvironmentID,
		"-render-url", cfg.RenderURL,
		"-artifact-base-url", cfg.ArtifactBaseURL,
		"-artifacts-dir", cfg.ArtifactsDir,
		"-db", cfg.DBPath,
	}
	if err := run(args); err == nil {
		t.Fatal("bad listen address should fail")
	}
}
