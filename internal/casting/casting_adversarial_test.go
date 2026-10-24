package casting

import (
	"context"
	"errors"
	"testing"
	"time"

	api "github.com/open-anywhere/open-anywhere/pkg/casting"
)

// inspectingAdapter lets a test look at the freshly reserved record while the
// device start is in flight, then fails both start and stop.
type inspectingAdapter struct {
	fakeAdapter
	onStart func()
}

func (a *inspectingAdapter) Start(context.Context, Device, api.StreamSource) error {
	if a.onStart != nil {
		a.onStart()
	}
	a.starts.Add(1)
	return a.startErr
}

func TestCreateSessionReleasesWhenTheFailedStopIsAlsoOld(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	a := &inspectingAdapter{}
	a.startErr = errors.New("start failed")
	a.stopErr = errors.New("stop failed")
	registry := NewRegistry()
	registry.Upsert(testDevice())
	s := NewService(registry, map[string]Adapter{"dlna": a}, quietConfig(time.Minute, func() time.Time { return now }))
	a.onStart = func() {
		for _, r := range s.sessions {
			// The device has been failing for longer than the grace period.
			r.failingSince = now.Add(-time.Hour)
		}
	}

	session, err := s.CreateSession(context.Background(), testRequest())
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("err = %v", err)
	}
	if session.State != api.SessionFailed {
		t.Fatalf("session = %+v", session)
	}
	s.mu.Lock()
	active := s.active["tv"]
	s.mu.Unlock()
	if active != "" {
		t.Fatalf("the reservation was not released: %q", active)
	}
}

func TestSweepReleasesAStoppedSessionThatWasNeverReleased(t *testing.T) {
	ctx := context.Background()
	a := &fakeAdapter{state: api.SessionStarting}
	s, _ := setup(a)
	session, err := s.CreateSession(ctx, testRequest())
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.lookup(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	r.session.State = api.SessionStopped
	r.mu.Unlock()

	s.Sweep(ctx)

	if !r.released {
		t.Fatal("a stopped session kept its reservation")
	}
	s.mu.Lock()
	active := s.active["tv"]
	s.mu.Unlock()
	if active != "" {
		t.Fatalf("active reservation = %q", active)
	}
}

func TestSweepDoesNotStopAnAlreadyReleasedFailedSession(t *testing.T) {
	ctx := context.Background()
	a := &fakeAdapter{state: api.SessionStarting}
	s, _ := setup(a)
	session, err := s.CreateSession(ctx, testRequest())
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.lookup(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	r.session.State = api.SessionFailed
	s.release(r)
	r.mu.Unlock()
	stops := a.stops.Load()

	s.Sweep(ctx)

	if got := a.stops.Load(); got != stops {
		t.Fatalf("Stop was retried %d extra times", got-stops)
	}
}
