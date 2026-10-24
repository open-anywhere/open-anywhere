package casting

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"maps"
	"net/url"
	"slices"
	"sync"
	"time"

	api "github.com/open-anywhere/open-anywhere/pkg/casting"
)

var (
	ErrNotFound    = errors.New("not found")
	ErrInvalid     = errors.New("invalid request")
	ErrUnsupported = errors.New("unsupported media")
	ErrConflict    = errors.New("target already has an active session")
	ErrOffline     = errors.New("target is offline")
	ErrUpstream    = errors.New("device control failed")
)

const (
	defaultUnreachableGrace = 30 * time.Second
	defaultProbeTimeout     = 3 * time.Second
)

// Config controls how long a session may keep failing before its screen
// reservation is released. The zero value uses the defaults; a negative
// UnreachableGrace disables automatic release.
type Config struct {
	UnreachableGrace time.Duration
	ProbeTimeout     time.Duration
	FailureGap       time.Duration
	Now              func() time.Time
	Logf             func(string, ...any)
}

type record struct {
	mu      sync.Mutex
	session api.Session
	device  Device
	adapter Adapter

	released     bool
	failingSince time.Time
	lastFailure  time.Time
	failures     int
}

type Service struct {
	registry *Registry
	adapters map[string]Adapter
	mu       sync.Mutex
	sessions map[string]*record
	active   map[string]string

	grace        time.Duration
	probeTimeout time.Duration
	failureGap   time.Duration
	now          func() time.Time
	logf         func(string, ...any)
}

func NewService(registry *Registry, adapters map[string]Adapter, options ...Config) *Service {
	var cfg Config
	if len(options) > 0 {
		cfg = options[0]
	}
	if cfg.UnreachableGrace == 0 {
		cfg.UnreachableGrace = defaultUnreachableGrace
	}
	if cfg.ProbeTimeout <= 0 {
		cfg.ProbeTimeout = defaultProbeTimeout
	}
	if cfg.FailureGap <= 0 {
		cfg.FailureGap = 2 * cfg.UnreachableGrace
		if cfg.FailureGap <= 0 {
			cfg.FailureGap = 2 * defaultUnreachableGrace
		}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logf == nil {
		cfg.Logf = log.Printf
	}
	copied := make(map[string]Adapter, len(adapters))
	maps.Copy(copied, adapters)
	return &Service{
		registry:     registry,
		adapters:     copied,
		sessions:     make(map[string]*record),
		active:       make(map[string]string),
		grace:        cfg.UnreachableGrace,
		probeTimeout: cfg.ProbeTimeout,
		failureGap:   cfg.FailureGap,
		now:          cfg.Now,
		logf:         cfg.Logf,
	}
}

func (s *Service) ListTargets() []api.Target {
	targets := s.registry.List()
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range targets {
		targets[i].ActiveSessionID = s.active[targets[i].ID]
	}
	return targets
}
func (s *Service) GetTarget(id string) (api.Target, error) {
	d, ok := s.registry.Get(id)
	if !ok {
		return api.Target{}, ErrNotFound
	}
	s.mu.Lock()
	d.Target.ActiveSessionID = s.active[id]
	s.mu.Unlock()
	return d.Target, nil
}

func (s *Service) CreateSession(ctx context.Context, req api.CreateSessionRequest) (api.Session, error) {
	if err := ctx.Err(); err != nil {
		return api.Session{}, err
	}
	u, err := url.Parse(req.Source.URL)
	if req.TargetID == "" || req.EndpointID == "" || req.Source.Type != "stream" || err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" {
		return api.Session{}, fmt.Errorf("%w: target_id, endpoint_id and an HTTP(S) stream URL are required", ErrInvalid)
	}
	d, ok := s.registry.Get(req.TargetID)
	if !ok {
		return api.Session{}, ErrNotFound
	}
	if d.Target.State != api.TargetOnline {
		return api.Session{}, ErrOffline
	}
	var ep *api.Endpoint
	for i := range d.Target.Endpoints {
		if d.Target.Endpoints[i].ID == req.EndpointID {
			ep = &d.Target.Endpoints[i]
			break
		}
	}
	if ep == nil {
		return api.Session{}, fmt.Errorf("%w: endpoint", ErrNotFound)
	}
	cap := ep.Capabilities
	if !cap.Streaming || !slices.Contains(cap.Formats, req.Source.Format) || !slices.Contains(cap.VideoCodecs, req.Source.VideoCodec) || (req.Source.AudioCodec != "" && !slices.Contains(cap.AudioCodecs, req.Source.AudioCodec)) {
		return api.Session{}, ErrUnsupported
	}
	a := s.adapters[ep.Protocol]
	if a == nil {
		return api.Session{}, ErrUnsupported
	}
	r := &record{device: d, adapter: a, session: api.Session{ID: "projection-" + rand.Text(), TargetID: req.TargetID, EndpointID: req.EndpointID, Source: req.Source, CreatedAt: time.Now().UTC(), State: api.SessionStarting}}
	// Reserve before talking to the device so concurrent callers cannot steal playback.
	s.mu.Lock()
	if s.active[req.TargetID] != "" {
		s.mu.Unlock()
		return api.Session{}, ErrConflict
	}
	s.active[req.TargetID] = r.session.ID
	s.sessions[r.session.ID] = r
	r.mu.Lock()
	s.mu.Unlock()
	defer r.mu.Unlock()
	if err := a.Start(ctx, d, req.Source); err != nil {
		r.session.State = api.SessionFailed
		r.session.Error = &api.SessionError{Code: api.ErrorDeviceControlFailed, Message: err.Error()}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		if stopErr := a.Stop(cleanupCtx, d); stopErr != nil {
			if s.noteFailure(r, context.Background(), stopErr) {
				s.release(r)
			}
		} else {
			s.release(r)
		}
		return r.session, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	// A successful Play command only means acceptance, not visible playback.
	if state, err := a.Status(ctx, d); err == nil {
		r.session.State = state
	}
	if r.session.State == api.SessionStopped {
		s.release(r)
	}
	return r.session, nil
}

func (s *Service) lookup(id string) (*record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.sessions[id]
	if !ok {
		return nil, ErrNotFound
	}
	return r, nil
}
func (s *Service) release(r *record) {
	r.released = true
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active[r.session.TargetID] == r.session.ID {
		delete(s.active, r.session.TargetID)
	}
}

// resolveDevice prefers the latest discovery result so a target that changed
// address after the session was created is still reachable.
func (s *Service) resolveDevice(r *record) Device {
	if device, ok := s.registry.Get(r.session.TargetID); ok {
		r.device = device
	}
	return r.device
}

// noteFailure records a failed device call and reports whether the session has
// been failing for longer than its grace period. A gap between failures larger
// than failureGap starts a new streak, so an old failure is never used as
// evidence. A canceled probe (shutdown) is not a device failure.
func (s *Service) noteFailure(r *record, probeCtx context.Context, err error) bool {
	if probeCtx.Err() == context.Canceled {
		return false
	}
	now := s.now()
	if r.failingSince.IsZero() || (!r.lastFailure.IsZero() && now.Sub(r.lastFailure) > s.failureGap) {
		r.failingSince = now
		r.failures = 0
	}
	r.failures++
	r.lastFailure = now
	if r.failures == 1 {
		s.logf("casting session %s target %s: device probe failed: %v", r.session.ID, r.session.TargetID, err)
	}
	grace := s.grace
	if r.session.State == api.SessionPlaying {
		// A session that was last seen playing gets more time: a short network
		// hiccup must not tear down a stream the user is watching.
		grace *= 2
	}
	if grace <= 0 {
		return false
	}
	return !now.Before(r.failingSince.Add(grace))
}

func (s *Service) noteSuccess(r *record) {
	if r.failingSince.IsZero() {
		return
	}
	s.logf("casting session %s target %s: device recovered after %s", r.session.ID, r.session.TargetID, s.now().Sub(r.failingSince).Round(time.Second))
	r.failingSince = time.Time{}
	r.lastFailure = time.Time{}
	r.failures = 0
}

// refreshLocked asks the device for its transport state. Repeated failures are
// evidence that the device is gone: once they span the grace period the session
// is failed and its reservation released, so a stale screen cannot block the
// next application.
func (s *Service) refreshLocked(probeCtx context.Context, r *record) (api.Session, error) {
	state, err := r.adapter.Status(probeCtx, s.resolveDevice(r))
	if err != nil {
		if s.noteFailure(r, probeCtx, err) {
			s.failLocked(r)
			return r.session, nil
		}
		return r.session, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	s.noteSuccess(r)
	r.session.State = state
	if state == api.SessionStopped {
		s.release(r)
	}
	return r.session, nil
}

func (s *Service) failLocked(r *record) {
	elapsed := s.now().Sub(r.failingSince).Round(time.Second)
	r.session.State = api.SessionFailed
	r.session.Error = &api.SessionError{Code: api.ErrorDeviceUnreachable, Message: fmt.Sprintf("device unreachable for %s", elapsed)}
	s.release(r)
	s.logf("casting session %s target %s: released after %s without a device answer", r.session.ID, r.session.TargetID, elapsed)
}

// stopLocked stops playback. If the device cannot be reached for the whole
// grace period, the reservation is released anyway so the screen is usable
// again; the session carries stop_unconfirmed so callers can tell that the
// stop was never acknowledged by the device.
func (s *Service) stopLocked(probeCtx context.Context, r *record) error {
	if err := r.adapter.Stop(probeCtx, s.resolveDevice(r)); err != nil {
		if s.noteFailure(r, probeCtx, err) {
			elapsed := s.now().Sub(r.failingSince).Round(time.Second)
			r.session.State = api.SessionStopped
			r.session.Error = &api.SessionError{Code: api.ErrorStopUnconfirmed, Message: fmt.Sprintf("device unreachable for %s; reservation released without a confirmed stop", elapsed)}
			s.release(r)
			s.logf("casting session %s target %s: released after %s without a confirmed stop", r.session.ID, r.session.TargetID, elapsed)
			return nil
		}
		return err
	}
	s.noteSuccess(r)
	r.session.State = api.SessionStopped
	r.session.Error = nil
	s.release(r)
	return nil
}

func (s *Service) GetSession(ctx context.Context, id string) (api.Session, error) {
	r, err := s.lookup(id)
	if err != nil {
		return api.Session{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session.State == api.SessionStopped || r.session.State == api.SessionFailed {
		return r.session, nil
	}
	probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.probeTimeout)
	defer cancel()
	return s.refreshLocked(probeCtx, r)
}

func (s *Service) StopSession(ctx context.Context, id string) (api.Session, error) {
	r, err := s.lookup(id)
	if err != nil {
		return api.Session{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session.State == api.SessionStopped || r.released {
		r.session.State = api.SessionStopped
		// Keep stop_unconfirmed: it is the only record that the device never
		// acknowledged the stop. Other errors belonged to the state being left.
		if r.session.Error != nil && r.session.Error.Code != api.ErrorStopUnconfirmed {
			r.session.Error = nil
		}
		return r.session, nil
	}
	probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.probeTimeout)
	defer cancel()
	if err := s.stopLocked(probeCtx, r); err != nil {
		return r.session, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	return r.session, nil
}

// Sweep refreshes sessions nobody is querying and retries cleanup for sessions
// whose failed start left a reservation behind. It skips a session that is busy
// with an HTTP request and stops probing when ctx is canceled.
func (s *Service) Sweep(ctx context.Context) {
	s.mu.Lock()
	records := make([]*record, 0, len(s.sessions))
	for _, r := range s.sessions {
		records = append(records, r)
	}
	s.mu.Unlock()
	for _, r := range records {
		if ctx.Err() != nil {
			return
		}
		if !r.mu.TryLock() {
			continue
		}
		s.sweepLocked(ctx, r)
		r.mu.Unlock()
	}
}

func (s *Service) sweepLocked(ctx context.Context, r *record) {
	switch r.session.State {
	case api.SessionStopped:
		if !r.released {
			s.release(r)
		}
	case api.SessionFailed:
		if r.released {
			return
		}
		probeCtx, cancel := context.WithTimeout(ctx, s.probeTimeout)
		defer cancel()
		_ = s.stopLocked(probeCtx, r)
	default:
		probeCtx, cancel := context.WithTimeout(ctx, s.probeTimeout)
		defer cancel()
		_, _ = s.refreshLocked(probeCtx, r)
	}
}
