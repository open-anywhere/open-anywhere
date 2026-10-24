package casting

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/open-anywhere/open-anywhere/pkg/casting"
)

type fakeAdapter struct {
	startErr, statusErr, stopErr error
	state                        api.SessionState
	starts, stops                atomic.Int32
	entered                      chan struct{}
	resume                       chan struct{}
}

func (a *fakeAdapter) Start(ctx context.Context, _ Device, _ api.StreamSource) error {
	a.starts.Add(1)
	if a.entered != nil {
		close(a.entered)
		select {
		case <-a.resume:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return a.startErr
}
func (a *fakeAdapter) Status(context.Context, Device) (api.SessionState, error) {
	return a.state, a.statusErr
}
func (a *fakeAdapter) Stop(context.Context, Device) error { a.stops.Add(1); return a.stopErr }
func testDevice() Device {
	return Device{Target: api.Target{ID: "tv", Name: "Test TV", State: api.TargetOnline, Endpoints: []api.Endpoint{{ID: "dlna-renderer", Protocol: "dlna", Capabilities: api.Capabilities{Formats: []string{"mpegts"}, VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Streaming: true}}}}, ControlURL: "http://device.invalid/control"}
}
func testRequest() api.CreateSessionRequest {
	return api.CreateSessionRequest{TargetID: "tv", EndpointID: "dlna-renderer", Source: api.StreamSource{Type: "stream", URL: "http://media.invalid/live.ts", Format: "mpegts", VideoCodec: "h264", AudioCodec: "aac"}}
}
func setup(a *fakeAdapter) (*Service, *Registry) {
	r := NewRegistry()
	r.Upsert(testDevice())
	return NewService(r, map[string]Adapter{"dlna": a}), r
}

func TestSessionLifecycle(t *testing.T) {
	ctx := context.Background()
	a := &fakeAdapter{state: api.SessionStarting}
	s, _ := setup(a)
	first, err := s.CreateSession(ctx, testRequest())
	if err != nil {
		t.Fatal(err)
	}
	if first.State != api.SessionStarting || first.ID == "" || first.CreatedAt.IsZero() {
		t.Fatalf("invalid session: %+v", first)
	}
	if _, err = s.CreateSession(ctx, testRequest()); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected busy target: %v", err)
	}
	a.state = api.SessionPlaying
	playing, err := s.GetSession(ctx, first.ID)
	if err != nil || playing.State != api.SessionPlaying {
		t.Fatalf("playing: %+v %v", playing, err)
	}
	a.state = api.SessionPaused
	paused, err := s.GetSession(ctx, first.ID)
	if err != nil || paused.State != api.SessionPaused {
		t.Fatalf("paused: %+v %v", paused, err)
	}
	if _, err = s.StopSession(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	next, err := s.CreateSession(ctx, testRequest())
	if err != nil {
		t.Fatal(err)
	}
	if next.ID == first.ID {
		t.Fatal("session IDs reused")
	}
	// Deleting an old session must not issue Stop against the new playback.
	stopped, err := s.StopSession(ctx, first.ID)
	if err != nil || stopped.State != api.SessionStopped || a.stops.Load() != 1 {
		t.Fatalf("idempotent stop: %+v %v, stops=%d", stopped, err, a.stops.Load())
	}
	if _, err = s.GetSession(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
}

func TestCreateValidation(t *testing.T) {
	tests := []struct {
		name   string
		change func(*api.CreateSessionRequest, *Device)
		want   error
	}{
		{"target required", func(r *api.CreateSessionRequest, _ *Device) { r.TargetID = "" }, ErrInvalid},
		{"endpoint required", func(r *api.CreateSessionRequest, _ *Device) { r.EndpointID = "" }, ErrInvalid},
		{"source type", func(r *api.CreateSessionRequest, _ *Device) { r.Source.Type = "screen" }, ErrInvalid},
		{"relative URL", func(r *api.CreateSessionRequest, _ *Device) { r.Source.URL = "/live.ts" }, ErrInvalid},
		{"bad URL", func(r *api.CreateSessionRequest, _ *Device) { r.Source.URL = "http://[" }, ErrInvalid},
		{"URL scheme", func(r *api.CreateSessionRequest, _ *Device) { r.Source.URL = "file:///live.ts" }, ErrInvalid},
		{"URL credentials", func(r *api.CreateSessionRequest, _ *Device) { r.Source.URL = "http://user:pass@media.invalid/a" }, ErrInvalid},
		{"URL fragment", func(r *api.CreateSessionRequest, _ *Device) { r.Source.URL += "#part" }, ErrInvalid},
		{"missing target", func(r *api.CreateSessionRequest, _ *Device) { r.TargetID = "missing" }, ErrNotFound},
		{"missing endpoint", func(r *api.CreateSessionRequest, _ *Device) { r.EndpointID = "other" }, ErrNotFound},
		{"offline", func(_ *api.CreateSessionRequest, d *Device) { d.Target.State = api.TargetOffline }, ErrOffline},
		{"unsupported format", func(r *api.CreateSessionRequest, _ *Device) { r.Source.Format = "mp4" }, ErrUnsupported},
		{"unsupported video", func(r *api.CreateSessionRequest, _ *Device) { r.Source.VideoCodec = "vp9" }, ErrUnsupported},
		{"unsupported audio", func(r *api.CreateSessionRequest, _ *Device) { r.Source.AudioCodec = "opus" }, ErrUnsupported},
		{"streaming disabled", func(_ *api.CreateSessionRequest, d *Device) { d.Target.Endpoints[0].Capabilities.Streaming = false }, ErrUnsupported},
		{"adapter missing", func(_ *api.CreateSessionRequest, d *Device) { d.Target.Endpoints[0].Protocol = "unknown" }, ErrUnsupported},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &fakeAdapter{}
			s, r := setup(a)
			req, d := testRequest(), testDevice()
			tt.change(&req, &d)
			r.Upsert(d)
			if _, err := s.CreateSession(context.Background(), req); !errors.Is(err, tt.want) {
				t.Fatalf("want %v, got %v", tt.want, err)
			}
			if a.starts.Load() != 0 {
				t.Fatal("invalid request reached device")
			}
		})
	}
	t.Run("silent video allowed", func(t *testing.T) {
		s, _ := setup(&fakeAdapter{state: api.SessionPlaying})
		req := testRequest()
		req.Source.AudioCodec = ""
		if _, err := s.CreateSession(context.Background(), req); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("canceled before start", func(t *testing.T) {
		a := &fakeAdapter{}
		s, _ := setup(a)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := s.CreateSession(ctx, testRequest()); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if a.starts.Load() != 0 {
			t.Fatal("called device")
		}
	})
}

func TestDeviceFailuresAndRecovery(t *testing.T) {
	ctx := context.Background()
	t.Run("failed start cleanup", func(t *testing.T) {
		a := &fakeAdapter{startErr: errors.New("Play rejected")}
		s, _ := setup(a)
		failed, err := s.CreateSession(ctx, testRequest())
		if !errors.Is(err, ErrUpstream) || failed.State != api.SessionFailed || failed.Error == nil {
			t.Fatalf("%+v %v", failed, err)
		}
		saved, err := s.GetSession(ctx, failed.ID)
		if err != nil || saved.State != api.SessionFailed {
			t.Fatalf("%+v %v", saved, err)
		}
		a.startErr = nil
		a.state = api.SessionPlaying
		if _, err := s.CreateSession(ctx, testRequest()); err != nil {
			t.Fatalf("cleanup did not free target: %v", err)
		}
	})
	t.Run("failed cleanup keeps reservation", func(t *testing.T) {
		a := &fakeAdapter{startErr: errors.New("start timeout"), stopErr: errors.New("stop timeout")}
		s, _ := setup(a)
		failed, _ := s.CreateSession(ctx, testRequest())
		if _, err := s.CreateSession(ctx, testRequest()); !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
		if _, err := s.StopSession(ctx, failed.ID); !errors.Is(err, ErrUpstream) {
			t.Fatal(err)
		}
		a.stopErr = nil
		if _, err := s.StopSession(ctx, failed.ID); err != nil {
			t.Fatal(err)
		}
		a.startErr = nil
		if _, err := s.CreateSession(ctx, testRequest()); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("status error is not playback confirmation", func(t *testing.T) {
		a := &fakeAdapter{statusErr: errors.New("timeout")}
		s, _ := setup(a)
		pending, err := s.CreateSession(ctx, testRequest())
		if err != nil || pending.State != api.SessionStarting {
			t.Fatalf("%+v %v", pending, err)
		}
		if _, err := s.GetSession(ctx, pending.ID); !errors.Is(err, ErrUpstream) {
			t.Fatal(err)
		}
		a.statusErr = nil
		a.state = api.SessionStopped
		if _, err := s.GetSession(ctx, pending.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateSession(ctx, testRequest()); err != nil {
			t.Fatal(err)
		}
	})
	for _, op := range []func(context.Context, string) (api.Session, error){sEmpty().GetSession, sEmpty().StopSession} {
		if _, err := op(ctx, "missing"); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
	}
}
func sEmpty() *Service { return NewService(NewRegistry(), nil) }

func TestConcurrentCreateReservesTarget(t *testing.T) {
	a := &fakeAdapter{state: api.SessionPlaying, entered: make(chan struct{}), resume: make(chan struct{})}
	s, _ := setup(a)
	done := make(chan error, 1)
	go func() { _, err := s.CreateSession(context.Background(), testRequest()); done <- err }()
	<-a.entered
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if _, err := s.CreateSession(context.Background(), testRequest()); !errors.Is(err, ErrConflict) {
				t.Errorf("expected conflict: %v", err)
			}
		})
	}
	wg.Wait()
	close(a.resume)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if a.starts.Load() != 1 {
		t.Fatal("multiple playback starts")
	}
}

func TestRegistryIsolationExpiryAndConcurrentAccess(t *testing.T) {
	r := NewRegistry()
	d := testDevice()
	r.Upsert(d)
	d.Target.Endpoints[0].Capabilities.Formats[0] = "corrupted"
	got, ok := r.Get("tv")
	if !ok || got.Target.Endpoints[0].Capabilities.Formats[0] != "mpegts" {
		t.Fatal("registry retained mutable input")
	}
	got.Target.Endpoints[0].Capabilities.Formats[0] = "also corrupted"
	list := r.List()
	if list[0].Endpoints[0].Capabilities.Formats[0] != "mpegts" {
		t.Fatal("registry leaked mutable output")
	}
	expired := testDevice()
	expired.Target.ID = "aaa"
	expired.ExpiresAt = time.Now().Add(-time.Second)
	r.Upsert(expired)
	if r.List()[0].ID != "aaa" || r.List()[0].State != api.TargetOffline {
		t.Fatal("expiry or ordering incorrect")
	}
	if d, _ := r.Get("aaa"); d.Target.State != api.TargetOffline {
		t.Fatal("expired target online")
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { r.Upsert(testDevice()); r.Get("tv"); r.List() })
	}
	wg.Wait()
	if _, err := NewService(r, nil).GetTarget("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if target, err := NewService(r, nil).GetTarget("tv"); err != nil || target.ID != "tv" {
		t.Fatalf("%+v %v", target, err)
	}
}

func TestStoppingFailedOldSessionDoesNotStopNewSession(t *testing.T) {
	a := &fakeAdapter{startErr: errors.New("Play failed")}
	s, _ := setup(a)
	old, _ := s.CreateSession(context.Background(), testRequest())
	a.startErr = nil
	a.state = api.SessionPlaying
	next, err := s.CreateSession(context.Background(), testRequest())
	if err != nil {
		t.Fatal(err)
	}
	before := a.stops.Load()
	if _, err := s.StopSession(context.Background(), old.ID); err != nil {
		t.Fatal(err)
	}
	if a.stops.Load() != before {
		t.Fatal("old failed session stopped new playback")
	}
	if got, err := s.GetSession(context.Background(), next.ID); err != nil || got.State != api.SessionPlaying {
		t.Fatalf("%+v %v", got, err)
	}
}

type cancellationAdapter struct {
	entered    chan struct{}
	cleanupErr chan error
}

func (a *cancellationAdapter) Start(ctx context.Context, _ Device, _ api.StreamSource) error {
	close(a.entered)
	<-ctx.Done()
	return ctx.Err()
}
func (a *cancellationAdapter) Status(context.Context, Device) (api.SessionState, error) {
	return api.SessionStarting, nil
}
func (a *cancellationAdapter) Stop(ctx context.Context, _ Device) error {
	a.cleanupErr <- ctx.Err()
	return nil
}
func TestCanceledStartStillCleansUpDevice(t *testing.T) {
	a := &cancellationAdapter{entered: make(chan struct{}), cleanupErr: make(chan error, 1)}
	registry := NewRegistry()
	registry.Upsert(testDevice())
	s := NewService(registry, map[string]Adapter{"dlna": a})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := s.CreateSession(ctx, testRequest()); done <- err }()
	<-a.entered
	cancel()
	if err := <-done; !errors.Is(err, ErrUpstream) {
		t.Fatal(err)
	}
	if err := <-a.cleanupErr; err != nil {
		t.Fatalf("cleanup inherited canceled request: %v", err)
	}
}

func assertTargetSession(t *testing.T, s *Service, want string) {
	t.Helper()
	target, err := s.GetTarget("tv")
	if err != nil || target.ActiveSessionID != want {
		t.Fatalf("target session: want %q, got %+v %v", want, target, err)
	}
	targets := s.ListTargets()
	if len(targets) != 1 || targets[0].ActiveSessionID != want {
		t.Fatalf("target list: want session %q, got %+v", want, targets)
	}
}

func TestTargetReportsActiveSessionLifecycle(t *testing.T) {
	a := &fakeAdapter{state: api.SessionPlaying}
	s, r := setup(a)
	ctx := context.Background()
	assertTargetSession(t, s, "")
	first, err := s.CreateSession(ctx, testRequest())
	if err != nil {
		t.Fatal(err)
	}
	assertTargetSession(t, s, first.ID)
	// Rediscovery updates device metadata, not its service-owned reservation.
	r.Upsert(testDevice())
	assertTargetSession(t, s, first.ID)
	a.stopErr = errors.New("Stop failed")
	if _, err := s.StopSession(ctx, first.ID); !errors.Is(err, ErrUpstream) {
		t.Fatal(err)
	}
	assertTargetSession(t, s, first.ID)
	a.stopErr = nil
	if _, err := s.StopSession(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	assertTargetSession(t, s, "")
	second, err := s.CreateSession(ctx, testRequest())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StopSession(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	assertTargetSession(t, s, second.ID)
	a.state = api.SessionStopped
	if _, err := s.GetSession(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	assertTargetSession(t, s, "")
}

func TestTargetReportsReservationWhileStarting(t *testing.T) {
	a := &fakeAdapter{state: api.SessionPlaying, entered: make(chan struct{}), resume: make(chan struct{})}
	s, _ := setup(a)
	done := make(chan api.Session, 1)
	go func() {
		session, err := s.CreateSession(context.Background(), testRequest())
		if err != nil {
			t.Error(err)
		}
		done <- session
	}()
	<-a.entered
	target, err := s.GetTarget("tv")
	if err != nil || target.ActiveSessionID == "" {
		t.Errorf("starting session not visible: %+v %v", target, err)
	}
	assertTargetSession(t, s, target.ActiveSessionID)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { s.ListTargets(); _, _ = s.GetTarget("tv") })
	}
	wg.Wait()
	close(a.resume)
	session := <-done
	if session.ID != target.ActiveSessionID {
		t.Fatalf("reservation changed: %s / %s", target.ActiveSessionID, session.ID)
	}
}

func TestTargetReportsFailedCleanupReservation(t *testing.T) {
	a := &fakeAdapter{startErr: errors.New("Play failed"), stopErr: errors.New("Stop failed")}
	s, _ := setup(a)
	failed, err := s.CreateSession(context.Background(), testRequest())
	if !errors.Is(err, ErrUpstream) {
		t.Fatal(err)
	}
	assertTargetSession(t, s, failed.ID)
	a.stopErr = nil
	if _, err := s.StopSession(context.Background(), failed.ID); err != nil {
		t.Fatal(err)
	}
	assertTargetSession(t, s, "")
	a.startErr = errors.New("still failing")
	if _, err := s.CreateSession(context.Background(), testRequest()); !errors.Is(err, ErrUpstream) {
		t.Fatal(err)
	}
	assertTargetSession(t, s, "")
}
