package portal

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
	"github.com/open-anywhere/open-anywhere/internal/voiceentry"
)

func TestVoiceManagerStartGuards(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager := NewVoiceManager(ctx)

	if err := manager.Start(store.PortalDevice{ID: "hw", Kind: "hardware"}); err != nil {
		t.Fatalf("a non-virtual device was rejected: %v", err)
	}
	registered := store.PortalDevice{ID: "dev_none", Kind: store.PortalDeviceVirtual, State: store.PortalDeviceRegistered}
	if err := manager.Start(registered); err == nil {
		t.Fatal("a device without a credential path started")
	}
	registered.CredentialPath = filepath.Join(t.TempDir(), "missing.json")
	if err := manager.Start(registered); err == nil {
		t.Fatal("a device with a missing credential file started")
	}

	broken := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(broken, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	registered.CredentialPath = broken
	if err := manager.Start(registered); err == nil {
		t.Fatal("a device with an unreadable credential file started")
	}

	// A healthy runtime is left alone when Start is called again.
	manager.apps[registered.ID] = &voiceRuntime{cancel: func() {}}
	if err := manager.Start(registered); err != nil {
		t.Fatalf("restarting a healthy runtime: %v", err)
	}
}

func TestVoiceManagerStatusReportsFailuresAndConnectingRuntimes(t *testing.T) {
	manager := NewVoiceManager(context.Background())
	if state := manager.Status("missing"); state.State != "offline" {
		t.Fatalf("offline state=%+v", state)
	}
	manager.apps["failed"] = &voiceRuntime{err: "boom"}
	if state := manager.Status("failed"); state.State != "error" || state.Error != "boom" {
		t.Fatalf("error state=%+v", state)
	}

	// A runtime without a control API cannot be mounted.
	manager.apps["headless"] = &voiceRuntime{app: &voiceentry.App{}}
	if _, ok := manager.Handler("headless"); ok {
		t.Fatal("a runtime without a handler was mounted")
	}
	if _, ok := manager.Handler("missing"); ok {
		t.Fatal("an unknown device was mounted")
	}
}

func TestVoiceManagerWaitForConnectionUsesADefaultInterval(t *testing.T) {
	manager := NewVoiceManager(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if manager.WaitForConnection(ctx, "missing", 0) {
		t.Fatal("a missing device connected")
	}
	started := time.Now()
	if manager.WaitForConnection(ctx, "missing", -time.Second) {
		t.Fatal("a missing device connected")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("the default interval kept the call waiting %v", elapsed)
	}
}
