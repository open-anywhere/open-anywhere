package main

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/open-anywhere/open-anywhere/internal/gateway/config"
	"github.com/open-anywhere/open-anywhere/internal/gateway/secret"
	"github.com/open-anywhere/open-anywhere/internal/gateway/settings"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

func TestMCPAllowedHostsComeFromTheConfigAndTheAdminSettings(t *testing.T) {
	ctx := context.Background()

	// Standalone mode only has the configured public_host.
	standalone := mcpAllowedHosts(config.Config{PublicHost: "proxy.example"}, nil)
	if got := standalone(ctx); !reflect.DeepEqual(got, []string{"proxy.example"}) {
		t.Fatalf("standalone hosts = %v", got)
	}
	if got := mcpAllowedHosts(config.Config{}, nil)(ctx); len(got) != 0 {
		t.Fatalf("no configuration should allow nothing extra, got %v", got)
	}

	database, err := store.Open(filepath.Join(t.TempDir(), "hosts.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	box, err := secret.New(make([]byte, 32))
	if err != nil {
		t.Fatalf("secret.New: %v", err)
	}
	platform := settings.New(database, box)
	cfg := config.Config{PortalEnabled: true, PublicHost: "proxy.example"}
	hosts := mcpAllowedHosts(cfg, platform)

	// Nothing saved yet: only the configured host.
	if got := hosts(ctx); !reflect.DeepEqual(got, []string{"proxy.example"}) {
		t.Fatalf("before the address is saved = %v", got)
	}

	// The cache is a few seconds long, so a fresh closure sees the saved address.
	address := "http://203.0.113.10:6004/mcp"
	if err := platform.Update(ctx, settings.Patch{Linx: &settings.LinxPatch{PublicMCPURL: &address}}, "admin"); err != nil {
		t.Fatalf("Update: %v", err)
	}
	hosts = mcpAllowedHosts(cfg, platform)
	if got := hosts(ctx); !reflect.DeepEqual(got, []string{"proxy.example", "203.0.113.10:6004"}) {
		t.Fatalf("after the address is saved = %v", got)
	}
}
