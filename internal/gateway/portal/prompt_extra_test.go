package portal

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/secret"
	"github.com/open-anywhere/open-anywhere/internal/gateway/settings"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

func newPromptService(t *testing.T, dbPath string, provider *settings.Provider) *Service {
	t.Helper()
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	service, err := NewService(context.Background(), BuildConfig{
		DataDir:    t.TempDir(),
		Username:   "alice",
		Password:   "password123",
		SessionTTL: 0,
	}, db, nil, provider, nil, nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return service
}

func newPromptProvider(t *testing.T, dbPath string) (*store.DB, *settings.Provider) {
	t.Helper()
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	box, err := secret.New(bytes.Repeat([]byte{5}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return db, settings.New(db, box)
}

func TestNewServiceLinxProviderBranches(t *testing.T) {
	ctx := context.Background()

	// No platform provider at all.
	dbPath := filepath.Join(t.TempDir(), "portal.db")
	service := newPromptService(t, dbPath, nil)
	if _, err := service.Provision.client(ctx); !errors.Is(err, app.ErrPlatformNotConfigured) {
		t.Fatalf("nil provider error = %v", err)
	}

	// A provider whose settings cannot be read.
	dbPath = filepath.Join(t.TempDir(), "portal.db")
	_, provider := newPromptProvider(t, dbPath)
	service = newPromptService(t, dbPath, provider)
	execRaw(t, dbPath, "ALTER TABLE platform_settings RENAME TO platform_settings_hidden")
	if _, err := service.Provision.client(ctx); err == nil || !strings.Contains(err.Error(), "no such table") {
		t.Fatalf("snapshot failure error = %v", err)
	}

	// A readable provider without Linx credentials.
	dbPath = filepath.Join(t.TempDir(), "portal.db")
	_, provider = newPromptProvider(t, dbPath)
	service = newPromptService(t, dbPath, provider)
	if _, err := service.Provision.client(ctx); !errors.Is(err, app.ErrPlatformNotConfigured) {
		t.Fatalf("unconfigured Linx error = %v", err)
	}

	// A ready provider returns a client.
	dbPath = filepath.Join(t.TempDir(), "portal.db")
	_, provider = newPromptProvider(t, dbPath)
	base := "https://linx.example.com"
	key := settings.SecretValue{Value: "linx-key"}
	if err := provider.Update(ctx, settings.Patch{Linx: &settings.LinxPatch{
		APIBase: &base,
		APIKey:  &key,
	}}, "usr_admin"); err != nil {
		t.Fatalf("Update: %v", err)
	}
	service = newPromptService(t, dbPath, provider)
	client, err := service.Provision.client(ctx)
	if err != nil || client == nil {
		t.Fatalf("client=%v err=%v", client, err)
	}
}
