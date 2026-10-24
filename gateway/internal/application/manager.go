package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/open-anywhere/open-anywhere/gateway/internal/openapi"
	"github.com/open-anywhere/open-anywhere/pkg/casting"
)

type Manager struct {
	mu         sync.Mutex
	operations sync.Mutex
	apps       map[string]App
	opts       Options
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

func New(parent context.Context, opts Options) (*Manager, error) {
	if opts.StateFile == "" {
		return nil, fmt.Errorf("state_file required")
	}
	if opts.PortMin == 0 {
		opts.PortMin = 39100
	}
	if opts.PortMax == 0 {
		opts.PortMax = 39199
	}
	if opts.PortMin < 1 || opts.PortMax > 65535 || opts.PortMax < opts.PortMin {
		return nil, fmt.Errorf("invalid port range")
	}
	if opts.BuildTimeout == 0 {
		opts.BuildTimeout = time.Hour
	}
	if opts.BuildTimeout < 0 || opts.OperationTimeout < 0 {
		return nil, fmt.Errorf("timeouts must be positive")
	}
	if opts.OperationTimeout == 0 {
		opts.OperationTimeout = 2 * time.Minute
	}
	ids := map[string]bool{}
	targets := map[string]bool{}
	for _, s := range opts.Screens {
		if s.ID == "" || s.TargetID == "" || s.EndpointID == "" || ids[s.ID] || targets[s.TargetID] {
			return nil, fmt.Errorf("screen ids and targets must be unique and nonempty")
		}
		ids[s.ID] = true
		targets[s.TargetID] = true
	}
	ctx, cancel := context.WithCancel(parent)
	m := &Manager{apps: map[string]App{}, opts: opts, ctx: ctx, cancel: cancel}
	b, err := os.ReadFile(opts.StateFile)
	if err == nil {
		err = json.Unmarshal(b, &m.apps)
	}
	if err != nil && !os.IsNotExist(err) {
		cancel()
		return nil, err
	}
	if m.apps == nil {
		cancel()
		return nil, fmt.Errorf("invalid state file")
	}
	for id, a := range m.apps {
		if a.ID != id || !a.Owner.Valid() {
			cancel()
			return nil, fmt.Errorf("invalid app record")
		}
	}
	return m, nil
}
func (m *Manager) Close() { m.cancel(); m.wg.Wait() }
func copyApp(a App) App {
	b, _ := json.Marshal(a)
	var out App
	_ = json.Unmarshal(b, &out)
	return out
}

// putLocked commits to disk before publishing state. The caller holds mu.
func (m *Manager) putLocked(a App) error {
	next := make(map[string]App, len(m.apps)+1)
	for k, v := range m.apps {
		next[k] = v
	}
	next[a.ID] = copyApp(a)
	b, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(m.opts.StateFile)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".registry-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	cerr := f.Close()
	if err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), m.opts.StateFile); err != nil {
		return err
	}
	m.apps = next
	return nil
}
func (m *Manager) save(a App) error { m.mu.Lock(); defer m.mu.Unlock(); return m.putLocked(a) }
func (m *Manager) find(owner Owner, id string) (App, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !owner.Valid() {
		return App{}, fail("MISSING_CONTEXT", "Linx device, agent and session required")
	}
	a, ok := m.apps[id]
	if !ok || a.Owner != owner {
		return App{}, fail("APP_NOT_FOUND", "Application unavailable in this session")
	}
	return copyApp(a), nil
}
func (m *Manager) Create(owner Owner, prompt string, players []string) (map[string]any, error) {
	if !owner.Valid() {
		return nil, fail("MISSING_CONTEXT", "Linx context required")
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" || len(prompt) > 32000 {
		return nil, fail("INVALID_PROMPT", "Prompt required, max 32000 bytes")
	}
	seen := map[string]bool{}
	names := []string{}
	for _, p := range players {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			return nil, fail("INVALID_PLAYER_NAMES", "Empty or duplicate name")
		}
		seen[p] = true
		names = append(names, p)
	}
	if m.opts.Builder == nil {
		return nil, fail("BUILD_UNAVAILABLE", "AgentAPI is not configured")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.apps {
		if a.Owner == owner && a.State == "building" {
			return map[string]any{"app_id": a.ID, "state": "building"}, nil
		}
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	a := App{ID: "app-" + hex.EncodeToString(random[:]), Owner: owner, Prompt: prompt, Players: names, State: "building", Created: time.Now().UTC(), Seen: map[string]bool{}}
	if err := m.putLocked(a); err != nil {
		return nil, err
	}
	m.launch(a)
	return map[string]any{"app_id": a.ID, "state": "building"}, nil
}
func (m *Manager) launch(a App) { m.wg.Add(1); go func() { defer m.wg.Done(); m.build(a) }() }

// Recover resumes recorded build sessions and marks uncertain runtime transitions for reconciliation.
func (m *Manager) Recover() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.apps {
		if a.State == "building" {
			if a.SessionID == "" || m.opts.Builder == nil {
				a.State = "error"
				a.Error = &Failure{"building", "BUILD_FAILED", "Build session unavailable after restart"}
				if err := m.putLocked(a); err != nil {
					return err
				}
			} else {
				m.launch(copyApp(a))
			}
		} else if a.Busy {
			a.Busy = false
			a.State = "error"
			a.Error = &Failure{"running", "RECOVERY_REQUIRED", "Interrupted operation; reconcile resources before retry"}
			if err := m.putLocked(a); err != nil {
				return err
			}
		}
	}
	return nil
}
func (m *Manager) build(a App) {
	if a.Seen == nil {
		a.Seen = map[string]bool{}
	}
	deadline := a.Created.Add(m.opts.BuildTimeout)
	ctx, cancel := context.WithDeadline(m.ctx, deadline)
	defer cancel()
	req := BuildRequest{AppID: a.ID, Prompt: a.Prompt, Players: a.Players}
	var err error
	if a.SessionID == "" {
		a.SessionID, err = m.opts.Builder.Create(ctx, req)
		if err == nil {
			err = m.save(a)
		}
	}
	var product BuildProduct
	if err == nil {
		product, err = m.opts.Builder.Run(ctx, a.SessionID, req, a.Submitted, BuildCursor{a.Seen, a.TurnSeen, a.Completed}, func(submitted bool, e *BuildEvent) error {
			a.Submitted = a.Submitted || submitted
			if e != nil {
				if e.ID != "" {
					a.Seen[e.ID] = true
				}
				switch e.Type {
				case "user.message", "session.status_running", "agent.message":
					a.TurnSeen = true
				}
				if e.Message != "" {
					a.Message = e.Message
				}
				if e.Type == "session.status_idle" && e.StopReason == "end_turn" && a.TurnSeen {
					a.Completed = true
				}
			}
			return m.save(a)
		})
	}
	if m.ctx.Err() != nil {
		return
	}
	if err != nil {
		a.State = "error"
		a.Error = &Failure{"building", "BUILD_FAILED", err.Error()}
	} else {
		a.State = "idle"
		a.Name = product.Name
		a.ArtifactURL = product.URL
		a.ArtifactPath = product.Path
		a.Digest = product.Digest
		a.Document = product.Document
		a.BuildSeconds = int64(time.Since(a.Created).Seconds())
	}
	if saveErr := m.save(a); saveErr != nil {
		slog.Error("registry_save_failed", "app_id", a.ID, "error", saveErr)
	}
}
func (m *Manager) Status(ctx context.Context, owner Owner, id string) (map[string]any, error) {
	m.operations.Lock()
	defer m.operations.Unlock()
	a, err := m.find(owner, id)
	if err != nil {
		return nil, err
	}
	a, err = m.refresh(ctx, a)
	if err != nil {
		return nil, err
	}
	return a.Public(), nil
}

// refresh runs while operations is held, preventing a concurrent stop/start.
func (m *Manager) refresh(ctx context.Context, a App) (App, error) {
	if a.State == "running" && a.InstanceID != "" {
		if m.opts.Runtime == nil || m.opts.Display == nil {
			return a, fail("DEPENDENCY_UNAVAILABLE", "Runtime or display unavailable")
		}
		bounded, cancel := context.WithTimeout(ctx, m.opts.OperationTimeout)
		defer cancel()
		instance, e := m.opts.Runtime.Get(bounded, a.InstanceID)
		if e != nil {
			return a, e
		}
		code := ""
		if instance.State != "ready" {
			code = "APP_EXITED"
		} else {
			p, e := m.opts.Display.Get(bounded, a.ProjectionID)
			if e != nil {
				return a, e
			}
			if p.State != casting.SessionPlaying {
				code = "PROJECTION_DISCONNECTED"
			}
		}
		if code != "" {
			a.State = "error"
			a.Error = &Failure{"running", code, "Application or projection stopped"}
			_ = m.cleanup(bounded, &a)
			if e := m.save(a); e != nil {
				return a, e
			}
		}
	}
	return a, nil
}
func (m *Manager) Screens(ctx context.Context, owner Owner) (map[string]any, error) {
	if !owner.Valid() {
		return nil, fail("MISSING_CONTEXT", "Linx context required")
	}
	if m.opts.Display == nil || m.opts.Runtime == nil {
		return nil, fail("DEPENDENCY_UNAVAILABLE", "Render/Casting not configured")
	}
	targets, err := m.opts.Display.Targets(ctx)
	if err != nil {
		return nil, err
	}
	// A cheap port query also verifies the configured Render is reachable.
	_, renderErr := m.opts.Runtime.PortFree(ctx, m.opts.PortMin)
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []any{}
	for _, s := range m.opts.Screens {
		available := false
		busy := false
		for _, target := range targets {
			if target.ID == s.TargetID {
				busy = target.ActiveSessionID != ""
				for _, e := range target.Endpoints {
					if e.ID == s.EndpointID && target.State == casting.TargetOnline {
						available = renderErr == nil
					}
				}
			}
		}
		var current any
		for _, a := range m.apps {
			if a.ScreenID == s.ID && (a.State == "running" || a.Busy || a.InstanceID != "" || a.ProjectionID != "" || a.UnknownRuntime || a.UnknownProjection) {
				if a.Owner == owner {
					current = map[string]string{"app_id": a.ID, "name": a.Name}
				} else {
					available = false
				}
				busy = false
			}
		}
		if busy {
			available = false
		}
		out = append(out, map[string]any{"screen_id": s.ID, "name": s.Name, "available": available, "current_app": current})
	}
	return map[string]any{"screens": out}, nil
}
func (m *Manager) Start(ctx context.Context, owner Owner, id, screenID string) (map[string]any, error) {
	m.operations.Lock()
	defer m.operations.Unlock()
	a, err := m.find(owner, id)
	if err != nil {
		return nil, err
	}
	a, err = m.refresh(ctx, a)
	if err != nil {
		return nil, err
	}
	if a.State == "running" {
		if a.ScreenID == screenID {
			return a.Public(), nil
		}
		return nil, fail("APP_ALREADY_RUNNING", "Stop the application before switching screens")
	}
	if a.State == "building" {
		return nil, fail("APP_BUILDING", "Build still in progress")
	}
	if a.State != "idle" {
		return nil, fail("APP_IN_ERROR", "Application is not idle")
	}
	if m.opts.Runtime == nil || m.opts.Display == nil {
		return nil, fail("DEPENDENCY_UNAVAILABLE", "Render/Casting not configured")
	}
	var screen *Screen
	for i := range m.opts.Screens {
		if m.opts.Screens[i].ID == screenID {
			screen = &m.opts.Screens[i]
			break
		}
	}
	if screen == nil {
		return nil, fail("SCREEN_NOT_FOUND", "Unknown screen")
	}
	m.mu.Lock()
	for _, other := range m.apps {
		if other.ID != id && other.ScreenID == screenID && (other.Busy || other.State == "running" || other.InstanceID != "" || other.ProjectionID != "" || other.UnknownRuntime || other.UnknownProjection) {
			m.mu.Unlock()
			return nil, fail("TARGET_BUSY", "Screen occupied")
		}
	}
	m.mu.Unlock()
	bounded, cancel := context.WithTimeout(ctx, m.opts.OperationTimeout)
	defer cancel()
	targets, err := m.opts.Display.Targets(bounded)
	if err != nil {
		return nil, err
	}
	var endpoint *casting.Endpoint
	for _, target := range targets {
		if target.ID == screen.TargetID && target.State == casting.TargetOnline {
			if target.ActiveSessionID != "" {
				return nil, fail("TARGET_BUSY", "Casting target occupied")
			}
			for _, e := range target.Endpoints {
				if e.ID == screen.EndpointID {
					copy := e
					endpoint = &copy
				}
			}
		}
	}
	if endpoint == nil {
		return nil, fail("TARGET_UNREACHABLE", "Screen endpoint unavailable")
	}
	port := 0
	for p := m.opts.PortMin; p <= m.opts.PortMax; p++ {
		free, e := m.opts.Runtime.PortFree(bounded, p)
		if e != nil {
			return nil, e
		}
		if free {
			port = p
			break
		}
	}
	if port == 0 {
		return nil, fail("PORT_IN_USE", "No free configured port")
	}
	a.Busy = true
	a.ScreenID = screenID
	a.Port = port
	a.UnknownRuntime = true
	if err = m.save(a); err != nil {
		return nil, err
	}
	inst, err := m.opts.Runtime.Start(bounded, a.ID, a.ArtifactURL, port)
	var rejected *Rejected
	if errors.As(err, &rejected) {
		a.UnknownRuntime = false
		inst.ID = ""
	}
	if inst.ID != "" && inst.AppID != a.ID {
		inst.ID = ""
		err = fail("INVALID_RUNTIME_RESPONSE", "Render instance belongs to a different application")
	}
	if inst.ID != "" {
		a.InstanceID = inst.ID
		a.UnknownRuntime = false
	}
	if err == nil && (inst.State != "ready" || inst.AppID != a.ID || inst.ID == "" || inst.BaseURL == "") {
		err = fail("START_FAILED", "Render did not return a ready instance")
	}
	if err == nil {
		_, err = openapi.FromDocument(a.ID, inst.BaseURL, a.Document)
	}
	if err == nil && !supports(*endpoint, inst.Source) {
		err = fail("UNSUPPORTED_MEDIA", "Screen cannot play Render stream")
	}
	if err == nil {
		a.BaseURL = inst.BaseURL
		if err = m.save(a); err == nil {
			a.UnknownProjection = true
			err = m.save(a)
			if err == nil {
				var projection casting.Session
				projection, err = m.opts.Display.Start(bounded, casting.CreateSessionRequest{TargetID: screen.TargetID, EndpointID: screen.EndpointID, Source: inst.Source})
				var rejected *Rejected
				if errors.As(err, &rejected) {
					a.UnknownProjection = false
					projection.ID = ""
				}
				if projection.ID != "" {
					a.ProjectionID = projection.ID
					a.UnknownProjection = false
				}
				if err == nil && projection.ID == "" {
					err = fail("START_FAILED", "Missing projection id")
				}
				if err == nil {
					err = m.save(a)
				}
				for err == nil && projection.State == casting.SessionStarting {
					select {
					case <-bounded.Done():
						err = bounded.Err()
					case <-time.After(200 * time.Millisecond):
						projection, err = m.opts.Display.Get(bounded, a.ProjectionID)
					}
				}
				if err == nil && projection.State != casting.SessionPlaying {
					err = fail("START_FAILED", "Projection not playing")
				}
			}
		}
	}
	if err != nil {
		return nil, m.startFailure(a, err)
	}
	a.Busy = false
	a.State = "running"
	a.Started = time.Now().UTC()
	a.Error = nil
	if err = m.save(a); err != nil {
		return nil, m.startFailure(a, err)
	}
	return a.Public(), nil
}
func supports(e casting.Endpoint, s casting.StreamSource) bool {
	has := func(values []string, v string) bool {
		for _, x := range values {
			if x == v {
				return true
			}
		}
		return false
	}
	return e.Capabilities.Streaming && has(e.Capabilities.Formats, s.Format) && has(e.Capabilities.VideoCodecs, s.VideoCodec) && (s.AudioCodec == "" || has(e.Capabilities.AudioCodecs, s.AudioCodec))
}
func (m *Manager) startFailure(a App, cause error) error {
	a.State = "error"
	a.Busy = false
	a.Error = &Failure{"starting", "START_FAILED", cause.Error()}
	ctx, cancel := context.WithTimeout(m.ctx, m.opts.OperationTimeout)
	defer cancel()
	_ = m.cleanup(ctx, &a)
	if err := m.save(a); err != nil {
		return err
	}
	return a.Error
}
func (m *Manager) cleanup(ctx context.Context, a *App) error {
	if a.UnknownProjection || a.UnknownRuntime {
		return fail("RECONCILIATION_REQUIRED", "Downstream result unknown; reservations retained")
	}
	if a.ProjectionID != "" {
		if m.opts.Display == nil {
			return fail("STOP_FAILED", "Casting unavailable")
		}
		p, err := m.opts.Display.Stop(ctx, a.ProjectionID)
		if err != nil {
			return err
		}
		if p.State != casting.SessionStopped {
			return fail("STOP_FAILED", "Projection not stopped")
		}
		a.ProjectionID = ""
		if err = m.save(*a); err != nil {
			return err
		}
	}
	if a.InstanceID != "" {
		if m.opts.Runtime == nil {
			return fail("STOP_FAILED", "Render unavailable")
		}
		r, err := m.opts.Runtime.Stop(ctx, a.InstanceID)
		if err != nil {
			return err
		}
		if r.State != "stopped" {
			return fail("STOP_FAILED", "Instance not stopped")
		}
		a.InstanceID = ""
		a.BaseURL = ""
		a.Port = 0
	}
	return nil
}
func (m *Manager) Stop(ctx context.Context, owner Owner, id string) (map[string]any, error) {
	m.operations.Lock()
	defer m.operations.Unlock()
	a, err := m.find(owner, id)
	if err != nil {
		return nil, err
	}
	if a.State == "building" {
		return nil, fail("APP_BUILDING", "Build still in progress")
	}
	if a.State == "idle" {
		return a.Public(), nil
	}
	bounded, cancel := context.WithTimeout(ctx, m.opts.OperationTimeout)
	defer cancel()
	if err = m.cleanup(bounded, &a); err != nil {
		a.State = "error"
		a.Error = &Failure{"running", "STOP_FAILED", err.Error()}
		if saveErr := m.save(a); saveErr != nil {
			slog.Error("registry_save_failed", "app_id", a.ID, "error", saveErr)
		}
		return nil, a.Error
	}
	if a.State != "error" {
		a.State = "idle"
	}
	a.ScreenID = ""
	a.Busy = false
	if err = m.save(a); err != nil {
		return nil, err
	}
	out := a.Public()
	if a.State == "idle" {
		out["stopped_at"] = time.Now().UTC()
	}
	return out, nil
}
func (m *Manager) Document(owner Owner, id string) (map[string]any, error) {
	a, err := m.find(owner, id)
	if err != nil {
		return nil, err
	}
	if len(a.Document) == 0 {
		return nil, fail("OPENAPI_UNAVAILABLE", "Build has no registered OpenAPI")
	}
	var doc map[string]any
	err = json.Unmarshal(a.Document, &doc)
	return doc, err
}
func (m *Manager) Send(ctx context.Context, owner Owner, id string, args openapi.Arguments) (openapi.Result, error) {
	m.operations.Lock()
	defer m.operations.Unlock()
	a, err := m.find(owner, id)
	if err != nil {
		return openapi.Result{}, err
	}
	a, err = m.refresh(ctx, a)
	if err != nil {
		return openapi.Result{}, err
	}
	if a.State != "running" || a.Busy {
		return openapi.Result{}, fail("APP_NOT_RUNNING", "Application is not running")
	}
	c, err := openapi.FromDocument(a.ID, a.BaseURL, a.Document)
	if err != nil {
		return openapi.Result{}, err
	}
	args.ServiceID = a.ID
	return c.Send(ctx, args)
}

// RegisterDemo is a development adapter, not an MCP tool or a caller-provided URL.
func (m *Manager) RegisterDemo(owner Owner, id, base string, doc map[string]any) error {
	if !owner.Valid() || id == "" {
		return errors.New("demo owner and id required")
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	if _, err = openapi.FromDocument(id, base, b); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.apps[id]; exists {
		return nil
	}
	return m.putLocked(App{ID: id, Name: id, Owner: owner, State: "running", Document: b, BaseURL: base, Started: time.Now().UTC(), Created: time.Now().UTC()})
}

// Watch checks live application and projection states until process shutdown.
func (m *Manager) Watch(interval time.Duration) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-m.ctx.Done():
				return
			case <-ticker.C:
				m.mu.Lock()
				list := []App{}
				for _, a := range m.apps {
					if a.State == "running" && a.InstanceID != "" {
						list = append(list, copyApp(a))
					}
				}
				m.mu.Unlock()
				for _, a := range list {
					_, _ = m.Status(m.ctx, a.Owner, a.ID)
				}
			}
		}
	}()
}
