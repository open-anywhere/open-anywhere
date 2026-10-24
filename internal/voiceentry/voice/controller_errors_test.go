package voice

import (
	"errors"
	"testing"
)

var errSend = errors.New("send failed")

// failingTransport fails the operations named in fail and records the rest.
type failingTransport struct {
	fail  map[string]bool
	calls []string
}

func (f *failingTransport) do(op string) error {
	if f.fail[op] {
		return errSend
	}
	f.calls = append(f.calls, op)
	return nil
}

func (f *failingTransport) ListenStart(string) error { return f.do("start") }
func (f *failingTransport) ListenStop() error        { return f.do("stop") }
func (f *failingTransport) SendText(string) error    { return f.do("text") }
func (f *failingTransport) Announce(string) error    { return f.do("announce") }
func (f *failingTransport) Abort(string) error       { return f.do("abort") }

func TestTransportFailuresAreReturnedAndKeepState(t *testing.T) {
	f := &failingTransport{fail: map[string]bool{"start": true, "stop": true, "text": true, "announce": true, "abort": true}}
	c := NewController(f, Realtime)
	for name, err := range map[string]error{
		"Start":       c.Start(),
		"SendText":    c.SendText("x"),
		"Announce":    c.Announce("x"),
		"Abort":       c.Abort(),
		"Pause":       c.Pause(),
		"Resume":      c.Resume(),
		"Reconnected": c.Reconnected(),
	} {
		if !errors.Is(err, errSend) {
			t.Errorf("%s err=%v", name, err)
		}
	}
	if c.State() != Connecting {
		t.Fatalf("state changed to %s after failures", c.State())
	}
}

func TestHandleSimpleEvents(t *testing.T) {
	c := NewController(&failingTransport{}, Realtime)
	for event, want := range map[string]State{"stt": Thinking, "close": Reconnecting, "idle": Idle} {
		if err := c.Handle(Event{Type: event}); err != nil || c.State() != want {
			t.Errorf("%s: state=%s err=%v", event, c.State(), err)
		}
	}
}

func TestAbortedReplyClearsAnnouncementAndResumesListening(t *testing.T) {
	f := &failingTransport{}
	c := NewController(f, Realtime)
	_ = c.Announce("播报")
	if err := c.Handle(Event{Type: "tts", State: "stop", Aborted: true}); err != nil || c.State() != Listening {
		t.Fatalf("state=%s err=%v", c.State(), err)
	}
	// the announcement flag is cleared, so the next announcement is sent at once
	if err := c.Announce("下一条"); err != nil || c.QueueLength() != 0 {
		t.Fatalf("queue=%d err=%v", c.QueueLength(), err)
	}
}

func TestReplyEndFailuresAreReturned(t *testing.T) {
	f := &failingTransport{fail: map[string]bool{"announce": true}}
	c := NewController(f, Realtime)
	c.queue = []string{"排队的"}
	if err := c.Handle(Event{Type: "tts", State: "stop"}); !errors.Is(err, errSend) {
		t.Fatalf("queued announce err=%v", err)
	}
	f = &failingTransport{fail: map[string]bool{"start": true}}
	c = NewController(f, Realtime)
	if err := c.Handle(Event{Type: "tts", State: "stop"}); !errors.Is(err, errSend) {
		t.Fatalf("resume listening err=%v", err)
	}
}
