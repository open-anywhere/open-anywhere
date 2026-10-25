//go:build linux || darwin

package portal

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
	devicelinx "github.com/open-anywhere/open-anywhere/internal/voiceentry/linx"
)

func TestStartVoiceStopsARuntimeWhenTheOwnerIsDisabledWhileStarting(t *testing.T) {
	env := newProvisionTestEnv(t, &fakePlatform{}, &fakeOTA{immediate: true})
	device := env.device
	device.State = store.PortalDeviceRegistered
	fifo := filepath.Join(t.TempDir(), "device.json")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	device.CredentialPath = fifo

	done := make(chan error, 1)
	go func() { done <- env.provisioner.startVoice(context.Background(), device) }()

	// Opening the pipe for writing blocks until the voice runtime opens the
	// credential for reading, which pins the start at a known point.
	writerReady := make(chan *os.File, 1)
	go func() {
		writer, err := os.OpenFile(fifo, os.O_WRONLY, 0)
		if err != nil {
			writerReady <- nil
			return
		}
		writerReady <- writer
	}()
	var writer *os.File
	select {
	case writer = <-writerReady:
		if writer == nil {
			t.Fatal("open the credential pipe")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("startVoice never opened the credential pipe")
	}

	// The owner is disabled while the runtime is still starting.
	execRaw(t, env.dbPath, `UPDATE portal_users SET status = 'disabled' WHERE id = 'usr_1'`)
	encoded, err := json.Marshal(devicelinx.Device{
		MAC: device.MAC, ClientID: device.ClientID, Token: "device-token", WSURL: "ws://127.0.0.1:1/ws",
	})
	if err != nil {
		t.Fatalf("marshal credential: %v", err)
	}
	if _, err := writer.Write(append(encoded, '\n')); err != nil {
		t.Fatalf("write credential: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close credential: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("startVoice: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("startVoice did not finish")
	}
	if state := env.voice.Status(device.ID); state.State != "offline" {
		t.Fatalf("voice state = %+v", state)
	}
}
