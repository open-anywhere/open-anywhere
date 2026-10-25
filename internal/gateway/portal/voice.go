package portal

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
	"github.com/open-anywhere/open-anywhere/internal/voiceentry"
	"github.com/open-anywhere/open-anywhere/internal/voiceentry/voice"
)

// VoiceState is the portal-facing view of one device's voice runtime. It never
// contains the device token or the credential path.
type VoiceState struct {
	State     string `json:"state"`
	Connected bool   `json:"connected"`
	SessionID string `json:"session_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

// VoiceManager runs one voiceentry.App per registered virtual device. It is
// the portal's only owner of those apps: a device gets at most one runtime.
type VoiceManager struct {
	ctx  context.Context
	mu   sync.Mutex
	apps map[string]*voiceRuntime
	logf func(string, ...any)
}

type voiceRuntime struct {
	app    *voiceentry.App
	cancel context.CancelFunc
	err    string
	// done is closed once app.Run has returned, so a restart can wait for the
	// old connection to close before opening the next one.
	done chan struct{}
}

// voiceStopWait bounds how long Restart waits for the old runtime to exit.
const voiceStopWait = 5 * time.Second

// NewVoiceManager returns a manager whose runtimes stop when ctx is canceled.
func NewVoiceManager(ctx context.Context) *VoiceManager {
	return &VoiceManager{ctx: ctx, apps: make(map[string]*voiceRuntime), logf: log.Printf}
}

// Start launches the runtime for a registered device. Repeated calls return
// the existing runtime; a runtime that has exited is replaced.
func (m *VoiceManager) Start(device store.PortalDevice) error {
	if device.Kind != store.PortalDeviceVirtual {
		return nil
	}
	if err := checkStartable(device); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if running, ok := m.apps[device.ID]; ok && running.err == "" {
		return nil
	}
	app, err := voiceentry.New(voiceentry.Config{
		DevicePath: device.CredentialPath,
		Mode:       voice.Realtime,
		Serve:      true,
		NoListen:   true,
		Out:        io.Discard,
	})
	if err != nil {
		return fmt.Errorf("portal: create voice runtime: %w", err)
	}
	ctx, cancel := context.WithCancel(m.ctx)
	runtime := &voiceRuntime{app: app, cancel: cancel, done: make(chan struct{})}
	m.apps[device.ID] = runtime
	go func() {
		defer close(runtime.done)
		err := app.Run(ctx)
		m.mu.Lock()
		if err != nil && ctx.Err() == nil {
			runtime.err = err.Error()
			m.logf("portal: voice runtime for %s stopped: %v", device.ID, err)
		}
		m.mu.Unlock()
	}()
	return nil
}

// checkStartable reports why a device cannot run a voice runtime.
func checkStartable(device store.PortalDevice) error {
	if device.State != store.PortalDeviceRegistered {
		return fmt.Errorf("portal: device %s is not registered", device.ID)
	}
	if device.CredentialPath == "" {
		return fmt.Errorf("portal: device %s has no credential file", device.ID)
	}
	if _, err := os.Stat(device.CredentialPath); err != nil {
		return fmt.Errorf("portal: device credential: %w", err)
	}
	return nil
}

// Restart drops the device's current connection and opens a new one, even when
// the old runtime is healthy. The device is checked first so a device that
// cannot start again is not disconnected for nothing.
func (m *VoiceManager) Restart(device store.PortalDevice) error {
	if device.Kind != store.PortalDeviceVirtual {
		return nil
	}
	if err := checkStartable(device); err != nil {
		return err
	}
	if done := m.stop(device.ID); done != nil {
		select {
		case <-done:
		case <-time.After(voiceStopWait):
			m.logf("portal: voice runtime for %s did not exit within %s; starting anyway", device.ID, voiceStopWait)
		}
	}
	return m.Start(device)
}

// Stop cancels a device's runtime. Logging out or closing a tab does not call
// this: the runtime is meant to stay online like real hardware. The user's
// explicit disconnect does, and the device stays offline until reconnected.
func (m *VoiceManager) Stop(deviceID string) {
	m.stop(deviceID)
}

// stop cancels a device's runtime and returns a channel that closes once it
// has exited, or nil when the device had no runtime.
func (m *VoiceManager) stop(deviceID string) <-chan struct{} {
	m.mu.Lock()
	runtime, ok := m.apps[deviceID]
	delete(m.apps, deviceID)
	m.mu.Unlock()
	if !ok {
		return nil
	}
	runtime.cancel()
	return runtime.done
}

// StopAll cancels every runtime.
func (m *VoiceManager) StopAll() {
	m.mu.Lock()
	apps := m.apps
	m.apps = make(map[string]*voiceRuntime)
	m.mu.Unlock()
	for _, runtime := range apps {
		runtime.cancel()
	}
}

// Status reports a device's runtime, or an offline state when none exists.
func (m *VoiceManager) Status(deviceID string) VoiceState {
	m.mu.Lock()
	runtime, ok := m.apps[deviceID]
	var failure string
	if ok {
		failure = runtime.err
	}
	m.mu.Unlock()
	if !ok {
		return VoiceState{State: "offline"}
	}
	if failure != "" {
		return VoiceState{State: "error", Error: failure}
	}
	status := runtime.app.Status()
	state := string(status.State)
	if state == "" {
		state = "connecting"
	}
	return VoiceState{State: state, Connected: status.Connected, SessionID: status.SessionID}
}

// Handler returns the mounted control API for a device runtime.
func (m *VoiceManager) Handler(deviceID string) (httpHandler, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime, ok := m.apps[deviceID]
	if !ok {
		return nil, false
	}
	handler := runtime.app.Handler()
	if handler == nil {
		return nil, false
	}
	return handler, true
}

// httpHandler mirrors http.Handler without importing net/http in this file's
// public surface; the portal router only needs ServeHTTP.
type httpHandler interface {
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

// WaitForConnection polls until the device's runtime reports a connected
// session or the deadline passes. It exists for tests and diagnostics.
func (m *VoiceManager) WaitForConnection(ctx context.Context, deviceID string, interval time.Duration) bool {
	if interval <= 0 {
		interval = 200 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if m.Status(deviceID).Connected {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}
