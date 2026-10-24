package voice

import "testing"

func TestReconnectedKeepsPausedState(t *testing.T) {
	f := &fakeTransport{}
	c := NewController(f, Realtime)
	_ = c.Start()
	_ = c.Pause()
	if err := c.Reconnected(); err != nil {
		t.Fatal(err)
	}
	if c.State() != Idle {
		t.Fatalf("state=%s", c.State())
	}
	if len(f.calls) != 2 {
		t.Fatalf("calls=%v", f.calls)
	}
}
