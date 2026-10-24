package voiceentry

import (
	"context"
	"errors"
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
