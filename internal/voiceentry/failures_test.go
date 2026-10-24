package voiceentry

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/voiceentry/linx"
	"github.com/open-anywhere/open-anywhere/internal/voiceentry/voice"
)

// refusingTransport is a voice.Transport whose every call fails.
type refusingTransport struct{}

var errRefused = errors.New("transport refused")

func (refusingTransport) ListenStart(string) error { return errRefused }
func (refusingTransport) ListenStop() error        { return errRefused }
func (refusingTransport) SendText(string) error    { return errRefused }
func (refusingTransport) Announce(string) error    { return errRefused }
func (refusingTransport) Abort(string) error       { return errRefused }

func TestRunClosesTheSessionWhenListeningCannotStart(t *testing.T) {
	f := newFakeLinx(t, false)
	a, err := New(Config{DevicePath: writeDevice(t, f.wsURL()), Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	a.controller = voice.NewController(refusingTransport{}, voice.Realtime)
	if err := a.Run(context.Background()); !errors.Is(err, errRefused) {
		t.Fatalf("err=%v", err)
	}
}

func TestReconnectClosesTheNewSessionWhenListeningCannotResume(t *testing.T) {
	srv, _ := helloServer(t)
	dev := linx.Device{MAC: "m", ClientID: "c", Token: "t", WSURL: "ws" + srv.URL[len("http"):]}
	tr := &transport{}
	c := voice.NewController(refusingTransport{}, voice.Realtime)
	var s *linx.Session
	if err := reconnect(context.Background(), &s, tr, c, dev, nil, []time.Duration{0}); !errors.Is(err, errRefused) {
		t.Fatalf("err=%v", err)
	}
	if s != nil || tr.Connected() {
		t.Fatalf("session kept after a failed resume: session=%v connected=%v", s, tr.Connected())
	}
}

func TestReconnectStopsWaitingWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var s *linx.Session
	c := voice.NewController(&transport{}, voice.Realtime)
	if err := reconnect(ctx, &s, &transport{}, c, linx.Device{}, nil, []time.Duration{time.Hour}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestResidentRunSurvivesListeningFailingToStart(t *testing.T) {
	f := newFakeLinx(t, false)
	a, err := New(Config{DevicePath: writeDevice(t, f.wsURL()), Serve: true, Listener: mustListen(t), ReconnectDelays: []time.Duration{5 * time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	a.controller = voice.NewController(refusingTransport{}, voice.Realtime)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	deadline := time.Now().Add(3 * time.Second)
	for f.sessions.Load() < 2 { // the first session was closed and replaced
		if time.Now().After(deadline) {
			t.Fatalf("no reconnect attempt, sessions=%d", f.sessions.Load())
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case err := <-done:
		t.Fatalf("Run returned early: %v", err)
	default:
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run=%v", err)
	}
}

func mustListen(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return ln
}
