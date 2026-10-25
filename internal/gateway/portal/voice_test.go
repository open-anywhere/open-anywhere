package portal

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

func writeCredential(t *testing.T, dir, id string) string {
	t.Helper()
	path := filepath.Join(dir, "devices", id+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]string{
		"mac":      "02:00:00:00:00:01",
		"clientId": "client-1",
		"token":    "device-token",
		"wsUrl":    "ws://127.0.0.1:1/ws",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestVoiceManagerOnlyRunsRegisteredDevicesWithCredentials(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager := NewVoiceManager(ctx)
	defer manager.StopAll()

	unregistered := store.PortalDevice{ID: "dev_1", Kind: store.PortalDeviceVirtual, State: store.PortalDeviceProvisioning}
	if err := manager.Start(unregistered); err == nil {
		t.Fatal("an unregistered device was started")
	}
	missing := store.PortalDevice{
		ID: "dev_2", Kind: store.PortalDeviceVirtual, State: store.PortalDeviceRegistered,
		CredentialPath: filepath.Join(t.TempDir(), "nope.json"),
	}
	if err := manager.Start(missing); err == nil {
		t.Fatal("a device without a credential file was started")
	}
	if status := manager.Status("dev_2"); status.State != "offline" || status.Connected {
		t.Fatalf("status=%+v", status)
	}
	if _, ok := manager.Handler("dev_2"); ok {
		t.Fatal("a handler was mounted without a runtime")
	}
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer waitCancel()
	if manager.WaitForConnection(waitCtx, "dev_2", 5*time.Millisecond) {
		t.Fatal("an offline device reported a connection")
	}
}

func TestVoiceManagerStartsAndStopsARegisteredDevice(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager := NewVoiceManager(ctx)
	path := writeCredential(t, t.TempDir(), "dev_1")
	device := store.PortalDevice{
		ID: "dev_1", Kind: store.PortalDeviceVirtual, State: store.PortalDeviceRegistered,
		CredentialPath: path,
	}
	if err := manager.Start(device); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, ok := manager.Handler("dev_1"); !ok {
		t.Fatal("the control API was not mounted")
	}
	if status := manager.Status("dev_1"); status.State == "offline" {
		t.Fatalf("status=%+v", status)
	}
	manager.Stop("dev_1")
	if status := manager.Status("dev_1"); status.State != "offline" {
		t.Fatalf("status after stop=%+v", status)
	}
}
