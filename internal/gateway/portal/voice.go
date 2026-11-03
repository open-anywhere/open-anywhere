package portal

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
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
	// presence tracks the browser requests currently attached to each runtime.
	// A virtual device is page-bound: once no page has touched it for
	// idleGrace, the watchdog stops the runtime instead of keeping a Linx
	// session open forever.
	presence  map[string]*voicePresence
	idleGrace time.Duration
	// casting is the LAN address on-site casting starts from. Nil keeps the
	// self.cast.* tools off, matching a deployment with no screens nearby.
	casting net.IP
	// castingFor limits on-site casting to selected accounts. The address is
	// the host's own LAN, so in a multi-tenant deployment every other tenant
	// would otherwise see — and cast to — the operator's screens. Nil allows
	// every virtual device, which is only correct for a single-tenant portal.
	castingFor func(userID string) bool
}

type voicePresence struct {
	clients  int
	lastSeen time.Time
}

// VoiceOption adjusts a manager at construction time.
type VoiceOption func(*VoiceManager)

// WithOnSiteCasting makes virtual devices report the self.cast.* tools, backed
// by DLNA on the given LAN address instead of a casting service.
func WithOnSiteCasting(localIP net.IP) VoiceOption {
	return func(m *VoiceManager) { m.casting = localIP }
}

// WithCastingFor limits on-site casting to the accounts the predicate accepts.
// The predicate runs when a runtime starts, so a role change applies on the
// next (re)start. Without it every virtual device gets the tools.
func WithCastingFor(predicate func(userID string) bool) VoiceOption {
	return func(m *VoiceManager) { m.castingFor = predicate }
}

// WithVoiceIdleGrace changes how long a virtual device may have no attached
// browser before its Linx runtime is stopped. The default is 30 seconds.
func WithVoiceIdleGrace(grace time.Duration) VoiceOption {
	return func(m *VoiceManager) {
		if grace > 0 {
			m.idleGrace = grace
		}
	}
}

// SetOnSiteCasting updates the LAN address used by future (and restarted)
// virtual devices. A nil IP turns the self.cast.* tools off.
func (m *VoiceManager) SetOnSiteCasting(localIP net.IP) {
	m.mu.Lock()
	m.casting = localIP
	m.mu.Unlock()
}

// castingIPFor returns the LAN address this device may cast from, or nil when
// casting is off for the deployment or this device's account is not allowed to
// act as the mock hardware entry.
func (m *VoiceManager) castingIPFor(device store.PortalDevice) net.IP {
	m.mu.Lock()
	ip, allowed := m.casting, m.castingFor
	m.mu.Unlock()
	if ip == nil {
		return nil
	}
	if allowed != nil && !allowed(device.UserID) {
		return nil
	}
	return ip
}

// CastingIP returns the address currently used for on-site casting, or nil.
func (m *VoiceManager) CastingIP() net.IP {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.casting == nil {
		return nil
	}
	return append(net.IP(nil), m.casting...)
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
func NewVoiceManager(ctx context.Context, options ...VoiceOption) *VoiceManager {
	manager := &VoiceManager{
		ctx:       ctx,
		apps:      make(map[string]*voiceRuntime),
		presence:  make(map[string]*voicePresence),
		idleGrace: 30 * time.Second,
		logf:      log.Printf,
	}
	for _, option := range options {
		option(manager)
	}
	go manager.watch()
	return manager
}

// watch stops virtual-device runtimes whose page has been gone for idleGrace.
func (m *VoiceManager) watch() {
	interval := m.idleGrace / 2
	if interval < 50*time.Millisecond {
		interval = 50 * time.Millisecond
	}
	if interval > 5*time.Second {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case now := <-ticker.C:
			m.mu.Lock()
			var idle []string
			for deviceID, presence := range m.presence {
				if presence.clients == 0 && now.Sub(presence.lastSeen) >= m.idleGrace {
					idle = append(idle, deviceID)
				}
			}
			m.mu.Unlock()
			for _, deviceID := range idle {
				m.logf("portal: voice runtime for %s has no attached page; stopping", deviceID)
				m.Stop(deviceID)
			}
		}
	}
}

// Acquire marks a browser request as attached to the device's runtime. The
// returned release function must run when the request ends; an SSE or
// WebSocket request therefore keeps the runtime alive until the page closes.
func (m *VoiceManager) Acquire(deviceID string) func() {
	m.mu.Lock()
	presence := m.presence[deviceID]
	if presence == nil {
		presence = &voicePresence{}
		m.presence[deviceID] = presence
	}
	presence.clients++
	presence.lastSeen = time.Now()
	m.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			m.mu.Lock()
			if current := m.presence[deviceID]; current != nil {
				if current.clients > 0 {
					current.clients--
				}
				current.lastSeen = time.Now()
			}
			m.mu.Unlock()
		})
	}
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
	// Resolve the account policy before taking the lock: the predicate reads
	// the account table, and Start holds m.mu for the whole runtime setup.
	casting := m.castingIPFor(device)
	m.mu.Lock()
	defer m.mu.Unlock()
	if running, ok := m.apps[device.ID]; ok && running.err == "" {
		return nil
	}
	config := voiceentry.Config{
		DevicePath: device.CredentialPath,
		Mode:       voice.Realtime,
		Serve:      true,
		NoListen:   true,
		Out:        io.Discard,
		// A page-bound virtual device must not dial back on its own after the
		// Linx session ends (for example, after the agent says goodbye).
		// The call page reconnects explicitly when the user starts again.
		NoAutoReconnect: true,
	}
	if casting != nil {
		config.Casting = true
		config.CastingLocalIP = casting
	}
	app, err := voiceentry.New(config)
	if err != nil {
		return fmt.Errorf("portal: create voice runtime: %w", err)
	}
	ctx, cancel := context.WithCancel(m.ctx)
	runtime := &voiceRuntime{app: app, cancel: cancel, done: make(chan struct{})}
	m.apps[device.ID] = runtime
	m.presence[device.ID] = &voicePresence{lastSeen: time.Now()}
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

// Stop cancels a device's runtime. An explicit disconnect stops it immediately;
// the idle watchdog calls the same path after the page has been gone for
// idleGrace, so a virtual device is page-bound rather than always online.
func (m *VoiceManager) Stop(deviceID string) {
	m.stop(deviceID)
}

// stop cancels a device's runtime and returns a channel that closes once it
// has exited, or nil when the device had no runtime.
func (m *VoiceManager) stop(deviceID string) <-chan struct{} {
	m.mu.Lock()
	runtime, ok := m.apps[deviceID]
	delete(m.apps, deviceID)
	delete(m.presence, deviceID)
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
	m.presence = make(map[string]*voicePresence)
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
