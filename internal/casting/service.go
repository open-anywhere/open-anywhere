package casting

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
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

type record struct {
	mu       sync.Mutex
	session  api.Session
	device   Device
	adapter  Adapter
	released bool
}

type Service struct {
	registry *Registry
	adapters map[string]Adapter
	mu       sync.Mutex
	sessions map[string]*record
	active   map[string]string
}

func NewService(registry *Registry, adapters map[string]Adapter) *Service {
	copied := make(map[string]Adapter, len(adapters))
	for k, v := range adapters {
		copied[k] = v
	}
	return &Service{registry: registry, adapters: copied, sessions: make(map[string]*record), active: make(map[string]string)}
}

func (s *Service) ListTargets() []api.Target { return s.registry.List() }
func (s *Service) GetTarget(id string) (api.Target, error) {
	d, ok := s.registry.Get(id)
	if !ok {
		return api.Target{}, ErrNotFound
	}
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
		r.session.Error = &api.SessionError{Code: "device_control_failed", Message: err.Error()}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		if a.Stop(cleanupCtx, d) == nil {
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
	state, err := r.adapter.Status(ctx, r.device)
	if err != nil {
		return r.session, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	r.session.State = state
	if state == api.SessionStopped {
		s.release(r)
	}
	return r.session, nil
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
		r.session.Error = nil
		return r.session, nil
	}
	if err := r.adapter.Stop(ctx, r.device); err != nil {
		return r.session, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	r.session.State = api.SessionStopped
	r.session.Error = nil
	s.release(r)
	return r.session, nil
}
