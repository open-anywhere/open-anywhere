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
	statuses                     atomic.Int32
	lastDevice                   atomic.Pointer[Device]
	entered                      chan struct{}
	resume                       chan struct{}
}

func (a *fakeAdapter) record(d Device) { a.lastDevice.Store(&d) }

func (a *fakeAdapter) Start(ctx context.Context, d Device, _ api.StreamSource) error {
	a.record(d)
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
func (a *fakeAdapter) Status(_ context.Context, d Device) (api.SessionState, error) {
	a.record(d)
	a.statuses.Add(1)
	return a.state, a.statusErr
}
func (a *fakeAdapter) Stop(_ context.Context, d Device) error {
	a.record(d)
	a.stops.Add(1)
	return a.stopErr
}
func testDevice() Device {
	return Device{Target: api.Target{ID: "tv", Name: "Test TV", State: api.TargetOnline, Endpoints: []api.Endpoint{{ID: "dlna-renderer", Protocol: "dlna", Capabilities: api.Capabilities{Formats: []string{"mpegts"}, VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Streaming: true}}}}, ControlURL: "http://device.invalid/control"}
}
func testRequest() api.CreateSessionRequest {
	return api.CreateSessionRequest{TargetID: "tv", EndpointID: "dlna-renderer", Source: api.StreamSource{Type: "stream", URL: "http://media.invalid/live.ts", Format: "mpegts", VideoCodec: "h264", AudioCodec: "aac"}}
}
func setup(a *fakeAdapter) (*Service, *Registry) {
	return setupWithConfig(a, Config{})
}

func setupWithConfig(a *fakeAdapter, cfg Config) (*Service, *Registry) {
	r := NewRegistry()
	r.Upsert(testDevice())
	return NewService(r, map[string]Adapter{"dlna": a}, cfg), r
}

func quietConfig(grace time.Duration, now func() time.Time) Config {
	return Config{UnreachableGrace: grace, Now: now, Logf: func(string, ...any) {}}
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

func testClock() (func() time.Time, func(time.Duration)) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return func() time.Time { return now }, func(d time.Duration) { now = now.Add(d) }
}

func TestPlayingSessionGetsDoubledGrace(t *testing.T) {
	now, advance := testClock()
	a := &fakeAdapter{state: api.SessionPlaying}
	s, _ := setupWithConfig(a, quietConfig(10*time.Second, now))
	session, err := s.CreateSession(context.Background(), testRequest())
	if err != nil || session.State != api.SessionPlaying {
		t.Fatalf("%+v %v", session, err)
	}
	a.statusErr = errors.New("unreachable")
	if _, err := s.GetSession(context.Background(), session.ID); !errors.Is(err, ErrUpstream) {
		t.Fatalf("first failure changed the session: %v", err)
	}
	advance(19 * time.Second)
	if _, err := s.GetSession(context.Background(), session.ID); !errors.Is(err, ErrUpstream) {
		t.Fatalf("released before the doubled grace: %v", err)
	}
	assertTargetSession(t, s, session.ID)
	advance(time.Second)
	failed, err := s.GetSession(context.Background(), session.ID)
	if err != nil || failed.State != api.SessionFailed || failed.Error == nil || failed.Error.Code != api.ErrorDeviceUnreachable {
		t.Fatalf("%+v %v", failed, err)
	}
	assertTargetSession(t, s, "")
	a.statusErr = nil
	if _, err := s.CreateSession(context.Background(), testRequest()); err != nil {
		t.Fatalf("released target still busy: %v", err)
	}
}

func TestStartingSessionUsesBaseGrace(t *testing.T) {
	now, advance := testClock()
	a := &fakeAdapter{state: api.SessionStarting}
	s, _ := setupWithConfig(a, quietConfig(10*time.Second, now))
	session, err := s.CreateSession(context.Background(), testRequest())
	if err != nil || session.State != api.SessionStarting {
		t.Fatalf("%+v %v", session, err)
	}
	a.statusErr = errors.New("unreachable")
	if _, err := s.GetSession(context.Background(), session.ID); !errors.Is(err, ErrUpstream) {
		t.Fatalf("first failure changed the session: %v", err)
	}
	advance(9 * time.Second)
	if _, err := s.GetSession(context.Background(), session.ID); !errors.Is(err, ErrUpstream) {
		t.Fatalf("released before the grace: %v", err)
	}
	advance(time.Second)
	failed, err := s.GetSession(context.Background(), session.ID)
	if err != nil || failed.State != api.SessionFailed {
		t.Fatalf("%+v %v", failed, err)
	}
	assertTargetSession(t, s, "")
}

func TestFailureGapStartsNewStreak(t *testing.T) {
	now, advance := testClock()
	a := &fakeAdapter{state: api.SessionStarting}
	s, _ := setupWithConfig(a, quietConfig(10*time.Second, now))
	session, err := s.CreateSession(context.Background(), testRequest())
	if err != nil {
		t.Fatal(err)
	}
	a.statusErr = errors.New("unreachable")
	advance(time.Second)
	if _, err := s.GetSession(context.Background(), session.ID); !errors.Is(err, ErrUpstream) {
		t.Fatal(err)
	}
	// A gap longer than 2x grace means the old failure is not evidence.
	advance(25 * time.Second)
	if _, err := s.GetSession(context.Background(), session.ID); !errors.Is(err, ErrUpstream) {
		t.Fatalf("gap did not reset the streak: %v", err)
	}
	advance(9 * time.Second)
	if _, err := s.GetSession(context.Background(), session.ID); !errors.Is(err, ErrUpstream) {
		t.Fatalf("new streak expired early: %v", err)
	}
	advance(time.Second)
	failed, err := s.GetSession(context.Background(), session.ID)
	if err != nil || failed.State != api.SessionFailed {
		t.Fatalf("%+v %v", failed, err)
	}
}

func TestRecoveryResetsFailureStreak(t *testing.T) {
	now, advance := testClock()
	a := &fakeAdapter{state: api.SessionPaused}
	s, _ := setupWithConfig(a, quietConfig(10*time.Second, now))
	session, err := s.CreateSession(context.Background(), testRequest())
	if err != nil {
		t.Fatal(err)
	}
	a.statusErr = errors.New("unreachable")
	advance(time.Second)
	if _, err := s.GetSession(context.Background(), session.ID); !errors.Is(err, ErrUpstream) {
		t.Fatal(err)
	}
	advance(4 * time.Second)
	a.statusErr = nil
	if got, err := s.GetSession(context.Background(), session.ID); err != nil || got.State != api.SessionPaused {
		t.Fatalf("%+v %v", got, err)
	}
	a.statusErr = errors.New("unreachable")
	if _, err := s.GetSession(context.Background(), session.ID); !errors.Is(err, ErrUpstream) {
		t.Fatal(err)
	}
	advance(9 * time.Second)
	if _, err := s.GetSession(context.Background(), session.ID); !errors.Is(err, ErrUpstream) {
		t.Fatalf("failure before recovery counted: %v", err)
	}
	advance(time.Second)
	failed, err := s.GetSession(context.Background(), session.ID)
	if err != nil || failed.State != api.SessionFailed {
		t.Fatalf("%+v %v", failed, err)
	}
}

func TestStopUnconfirmedReleasesReservation(t *testing.T) {
	now, advance := testClock()
	a := &fakeAdapter{state: api.SessionPlaying}
	s, _ := setupWithConfig(a, quietConfig(10*time.Second, now))
	session, err := s.CreateSession(context.Background(), testRequest())
	if err != nil {
		t.Fatal(err)
	}
	a.stopErr = errors.New("unreachable")
	if _, err := s.StopSession(context.Background(), session.ID); !errors.Is(err, ErrUpstream) {
		t.Fatalf("first failed stop changed the session: %v", err)
	}
	advance(19 * time.Second)
	if _, err := s.StopSession(context.Background(), session.ID); !errors.Is(err, ErrUpstream) {
		t.Fatalf("released before the doubled grace: %v", err)
	}
	assertTargetSession(t, s, session.ID)
	advance(time.Second)
	stopped, err := s.StopSession(context.Background(), session.ID)
	if err != nil || stopped.State != api.SessionStopped || stopped.Error == nil || stopped.Error.Code != api.ErrorStopUnconfirmed {
		t.Fatalf("%+v %v", stopped, err)
	}
	assertTargetSession(t, s, "")
	// The idempotent stop still reports the unconfirmed result.
	again, err := s.StopSession(context.Background(), session.ID)
	if err != nil || again.Error == nil {
		t.Fatalf("%+v %v", again, err)
	}
	a.stopErr = nil
	if _, err := s.CreateSession(context.Background(), testRequest()); err != nil {
		t.Fatalf("released target still busy: %v", err)
	}
}

func TestSweepRefreshesUnqueriedSession(t *testing.T) {
	now, advance := testClock()
	a := &fakeAdapter{state: api.SessionPlaying}
	s, _ := setupWithConfig(a, quietConfig(10*time.Second, now))
	session, err := s.CreateSession(context.Background(), testRequest())
	if err != nil {
		t.Fatal(err)
	}
	a.statusErr = errors.New("unreachable")
	s.Sweep(context.Background())
	assertTargetSession(t, s, session.ID)
	advance(20 * time.Second)
	s.Sweep(context.Background())
	got, err := s.GetSession(context.Background(), session.ID)
	if err != nil || got.State != api.SessionFailed || got.Error == nil || got.Error.Code != api.ErrorDeviceUnreachable {
		t.Fatalf("%+v %v", got, err)
	}
	assertTargetSession(t, s, "")
}

func TestSweepReleasesWhenTargetReportsStopped(t *testing.T) {
	a := &fakeAdapter{state: api.SessionStarting}
	s, _ := setup(a)
	session, err := s.CreateSession(context.Background(), testRequest())
	if err != nil {
		t.Fatal(err)
	}
	a.state = api.SessionStopped
	s.Sweep(context.Background())
	got, err := s.GetSession(context.Background(), session.ID)
	if err != nil || got.State != api.SessionStopped {
		t.Fatalf("%+v %v", got, err)
	}
	assertTargetSession(t, s, "")
}

func TestSweepRetriesFailedCleanup(t *testing.T) {
	t.Run("releases on success", func(t *testing.T) {
		now, advance := testClock()
		a := &fakeAdapter{startErr: errors.New("Play failed"), stopErr: errors.New("Stop failed")}
		s, _ := setupWithConfig(a, quietConfig(10*time.Second, now))
		failed, _ := s.CreateSession(context.Background(), testRequest())
		assertTargetSession(t, s, failed.ID)
		advance(9 * time.Second)
		s.Sweep(context.Background())
		assertTargetSession(t, s, failed.ID)
		a.stopErr = nil
		advance(time.Second)
		s.Sweep(context.Background())
		assertTargetSession(t, s, "")
		got, err := s.GetSession(context.Background(), failed.ID)
		if err != nil || got.State != api.SessionStopped || got.Error != nil {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("releases unconfirmed after grace", func(t *testing.T) {
		now, advance := testClock()
		a := &fakeAdapter{startErr: errors.New("Play failed"), stopErr: errors.New("Stop failed")}
		s, _ := setupWithConfig(a, quietConfig(10*time.Second, now))
		failed, _ := s.CreateSession(context.Background(), testRequest())
		assertTargetSession(t, s, failed.ID)
		advance(10 * time.Second)
		s.Sweep(context.Background())
		assertTargetSession(t, s, "")
		got, err := s.GetSession(context.Background(), failed.ID)
		if err != nil || got.State != api.SessionStopped || got.Error == nil || got.Error.Code != api.ErrorStopUnconfirmed {
			t.Fatalf("%+v %v", got, err)
		}
	})
}

func TestSweepSkipsBusySession(t *testing.T) {
	a := &fakeAdapter{state: api.SessionPlaying, entered: make(chan struct{}), resume: make(chan struct{})}
	s, _ := setup(a)
	done := make(chan error, 1)
	go func() {
		_, err := s.CreateSession(context.Background(), testRequest())
		done <- err
	}()
	<-a.entered
	s.Sweep(context.Background())
	if a.statuses.Load() != 0 {
		t.Fatal("sweep queried a session that was busy with a request")
	}
	close(a.resume)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestSweepStopsWhenContextCanceled(t *testing.T) {
	a := &fakeAdapter{state: api.SessionPlaying}
	s, _ := setup(a)
	if _, err := s.CreateSession(context.Background(), testRequest()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.Sweep(ctx)
	if a.statuses.Load() != 1 {
		t.Fatalf("sweep ignored cancellation: %d status calls", a.statuses.Load())
	}
}

func TestSweepUsesLatestDeviceAddress(t *testing.T) {
	a := &fakeAdapter{state: api.SessionPlaying}
	s, r := setup(a)
	if _, err := s.CreateSession(context.Background(), testRequest()); err != nil {
		t.Fatal(err)
	}
	updated := testDevice()
	updated.ControlURL = "http://new.invalid/control"
	r.Upsert(updated)
	s.Sweep(context.Background())
	got := a.lastDevice.Load()
	if got == nil || got.ControlURL != updated.ControlURL {
		t.Fatalf("stale device used: %+v", got)
	}
}

func TestDisabledAutomaticRelease(t *testing.T) {
	now, advance := testClock()
	a := &fakeAdapter{state: api.SessionPlaying}
	s, _ := setupWithConfig(a, quietConfig(-time.Second, now))
	session, err := s.CreateSession(context.Background(), testRequest())
	if err != nil {
		t.Fatal(err)
	}
	a.statusErr = errors.New("unreachable")
	advance(time.Hour)
	if _, err := s.GetSession(context.Background(), session.ID); !errors.Is(err, ErrUpstream) {
		t.Fatalf("automatic release disabled but session changed: %v", err)
	}
	assertTargetSession(t, s, session.ID)
}

func TestCanceledProbeIsNotDeviceFailure(t *testing.T) {
	a := &fakeAdapter{state: api.SessionStarting}
	s, _ := setup(a)
	session, err := s.CreateSession(context.Background(), testRequest())
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.lookup(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if s.noteFailure(r, ctx, errors.New("shutdown")) {
		t.Fatal("canceled probe counted as a device failure")
	}
	if !r.failingSince.IsZero() || r.failures != 0 {
		t.Fatalf("canceled probe changed state: %+v", r)
	}
}

func TestCallerCancellationDoesNotAbortProbe(t *testing.T) {
	a := &fakeAdapter{state: api.SessionStarting}
	s, _ := setup(a)
	session, err := s.CreateSession(context.Background(), testRequest())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.GetSession(ctx, session.ID); err != nil {
		t.Fatalf("probe inherited caller cancellation: %v", err)
	}
	if a.statuses.Load() != 2 {
		t.Fatalf("device not queried: %d status calls", a.statuses.Load())
	}
}
