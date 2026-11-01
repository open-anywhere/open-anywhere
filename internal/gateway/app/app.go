// Package app holds control-plane application records and their state machine.
package app

import (
	"crypto/rand"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/openapi"
	"github.com/open-anywhere/open-anywhere/pkg/casting"
)

// ErrPlatformNotConfigured reports that the platform AgentAPI or Linx
// credentials are missing. The portal surfaces it as a configuration problem
// instead of a build failure.
var ErrPlatformNotConfigured = errors.New("platform is not configured")

// State is the application state exposed to Linx through MCP.
type State string

const (
	StateBuilding State = "building"
	StateIdle     State = "idle"
	StateRunning  State = "running"
	StateError    State = "error"
)

// Stage identifies the phase in which an application failed.
type Stage string

const (
	StageBuilding Stage = "building"
	StageStarting Stage = "starting"
	StageRunning  Stage = "running"
)

// Error is the structured application error returned by get-app-status and
// notifications/app_status.
type Error struct {
	Stage            Stage  `json:"stage"`
	Code             string `json:"code"`
	Message          string `json:"message,omitempty"`
	LastAgentMessage string `json:"last_agent_message,omitempty"`
}

// Identity is the Linx device/session that owns an application. The platform
// supplies it in the X-Linx-Device-Id and X-Linx-Session-Id headers.
type Identity struct {
	DeviceID  string `json:"device_id"`
	AgentID   string `json:"agent_id,omitempty"`
	SessionID string `json:"session_id"`
}

// SameOwner reports whether two identities belong to the same device and agent.
// The Linx session is deliberately not part of ownership: it changes on every
// reconnect, while the applications built for a device and agent stay theirs.
func (i Identity) SameOwner(other Identity) bool {
	return i.DeviceID != "" && i.DeviceID == other.DeviceID && i.AgentID == other.AgentID
}

// Artifact describes the stored application package.
type Artifact struct {
	Path        string
	SHA256      string
	Size        int64
	DownloadURL string
	StoredAt    time.Time
}

// App is the control plane's record of one application.
type App struct {
	ID               string
	Revision         uint64
	Owner            Identity
	Prompt           string
	PlayerNames      []string
	Name             string
	State            State
	Error            *Error
	LastAgentMessage string
	// LastProgress is the most recent build progress signal: an agent message,
	// an agent thinking event, or a session error, whichever arrived last.
	LastProgress string

	// Build progress, kept for recovery and status queries.
	SessionID         string
	EventCursor       string
	BuildStartedAt    time.Time
	BuildFinishedAt   time.Time
	BuildDuration     time.Duration
	LastStatusMessage string

	// Runtime bindings, internal to the control plane.
	Artifact            Artifact
	OpenAPI             *openapi.Spec
	InstanceID          string
	Port                int
	AppBaseURL          string
	Source              casting.StreamSource
	PendingRuntime      bool
	CleanupPending      bool

	StartedAt time.Time
	StoppedAt time.Time
}

// BuildingMessage returns the text shown for a building application.
func (a App) BuildingMessage() string {
	if a.LastProgress != "" {
		return a.LastProgress
	}
	if a.LastAgentMessage != "" {
		return a.LastAgentMessage
	}
	if a.LastStatusMessage != "" {
		return a.LastStatusMessage
	}
	return "正在构建"
}

// Record wraps an application with locks. Begin/End serialize lifecycle
// operations; Snapshot and Do guard individual state reads and writes.
type Record struct {
	op       sync.Mutex
	mu       sync.RWMutex
	app      App
	registry *Registry
}

// Begin acquires the lifecycle lock, held across network calls.
func (r *Record) Begin() { r.op.Lock() }

// End releases the lifecycle lock.
func (r *Record) End() { r.op.Unlock() }

// Snapshot returns a shallow copy safe for reading outside the lock.
func (r *Record) Snapshot() App {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.app
}

// Update runs fn under the record's write lock and persists the result.
func (r *Record) Update(fn func(*App)) {
	r.mu.Lock()
	fn(&r.app)
	r.app.Revision++
	snapshot := r.app
	r.mu.Unlock()
	if r.registry != nil {
		r.registry.persist(snapshot)
	}
}

// Do runs fn under the record's write lock and persists the result.
func (r *Record) Do(fn func(*App) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	next := r.app
	if err := fn(&next); err != nil {
		return err
	}
	next.Revision++
	if r.registry != nil {
		if err := r.registry.persist(next); err != nil {
			return err
		}
	}
	r.app = next
	return nil
}

// Persister saves application snapshots across restarts.
type Persister interface {
	SaveApp(App) error
	DeleteApp(string) error
}

// Registry keeps applications by ID and persists every mutation.
type Registry struct {
	mu        sync.RWMutex
	apps      map[string]*Record
	persister Persister
	persistMu sync.Mutex
	logf      func(string, ...any)
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		apps: make(map[string]*Record),
		logf: log.Printf,
	}
}

// SetPersister installs the durable store after the registry was created.
func (r *Registry) SetPersister(persister Persister) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.persister = persister
}

// Add registers app unless its ID already exists, and returns the record.
func (r *Registry) Add(value App) *Record {
	r.mu.Lock()
	if existing, ok := r.apps[value.ID]; ok {
		r.mu.Unlock()
		return existing
	}
	if value.Revision == 0 {
		value.Revision = 1
	}
	record := &Record{app: value, registry: r}
	r.apps[value.ID] = record
	r.mu.Unlock()
	r.persist(value)
	return record
}

// Load inserts persisted applications without writing them back.
func (r *Registry) Load(values []App) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, value := range values {
		if value.ID == "" {
			continue
		}
		if _, exists := r.apps[value.ID]; exists {
			continue
		}
		if value.Revision == 0 {
			value.Revision = 1
		}
		r.apps[value.ID] = &Record{app: value, registry: r}
	}
}

// Get returns the record for id.
func (r *Registry) Get(id string) (*Record, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	record, ok := r.apps[id]
	return record, ok
}

// Remove deletes an application record from memory and the store.
func (r *Registry) Remove(id string) {
	r.mu.Lock()
	_, exists := r.apps[id]
	delete(r.apps, id)
	r.mu.Unlock()
	if !exists {
		return
	}
	r.mu.RLock()
	persister := r.persister
	logf := r.logf
	r.mu.RUnlock()
	if persister == nil {
		return
	}
	r.persistMu.Lock()
	defer r.persistMu.Unlock()
	if err := persister.DeleteApp(id); err != nil {
		logf("registry: delete app %s: %v", id, err)
	}
}

func (r *Registry) persist(value App) error {
	r.mu.RLock()
	persister := r.persister
	logf := r.logf
	r.mu.RUnlock()
	if persister == nil {
		return nil
	}
	r.persistMu.Lock()
	defer r.persistMu.Unlock()
	if err := persister.SaveApp(value); err != nil {
		logf("registry: save app %s: %v", value.ID, err)
		return err
	}
	return nil
}

// List returns snapshots of all applications.
func (r *Registry) List() []App {
	r.mu.RLock()
	records := make([]*Record, 0, len(r.apps))
	for _, record := range r.apps {
		records = append(records, record)
	}
	r.mu.RUnlock()
	apps := make([]App, 0, len(records))
	for _, record := range records {
		apps = append(apps, record.Snapshot())
	}
	return apps
}

// NewID returns a new application ID.
func NewID() string { return "app_" + rand.Text() }

// Notifier delivers best-effort application notifications to Linx.
type Notifier interface {
	Notify(owner Identity, notification Notification)
}

// Notification is the params object of notifications/app_status.
type Notification struct {
	AppID   string `json:"app_id"`
	State   State  `json:"state"`
	Message string `json:"message,omitempty"`
	Error   *Error `json:"error,omitempty"`
}
