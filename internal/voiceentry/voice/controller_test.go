package voice

import "testing"

type fakeTransport struct{ calls []string }

func (f *fakeTransport) ListenStart(m string) error {
	f.calls = append(f.calls, "listen:"+m)
	return nil
}
func (f *fakeTransport) ListenStop() error       { f.calls = append(f.calls, "stop"); return nil }
func (f *fakeTransport) SendText(s string) error { f.calls = append(f.calls, "text:"+s); return nil }
func (f *fakeTransport) Announce(s string) error {
	f.calls = append(f.calls, "announce:"+s)
	return nil
}
func (f *fakeTransport) Abort(s string) error { f.calls = append(f.calls, "abort:"+s); return nil }

func TestAnnouncementsQueueUntilTTSStops(t *testing.T) {
	f := &fakeTransport{}
	c := NewController(f, Auto)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	if err := c.Announce("一"); err != nil {
		t.Fatal(err)
	}
	if err := c.Announce("二"); err != nil {
		t.Fatal(err)
	}
	if c.QueueLength() != 1 || c.State() != Speaking {
		t.Fatalf("queue=%d state=%s", c.QueueLength(), c.State())
	}
	if err := c.Handle(Event{Type: "tts", State: "stop"}); err != nil {
		t.Fatal(err)
	}
	if c.QueueLength() != 0 || len(f.calls) != 3 || f.calls[2] != "announce:二" {
		t.Fatalf("calls=%v", f.calls)
	}
	if err := c.Handle(Event{Type: "tts", State: "stop"}); err != nil {
		t.Fatal(err)
	}
	if c.State() != Listening || f.calls[len(f.calls)-1] != "listen:auto" {
		t.Fatalf("calls=%v", f.calls)
	}
}

func TestAnnouncementSTTEchoIsIgnored(t *testing.T) {
	f := &fakeTransport{}
	c := NewController(f, Realtime)
	_ = c.Start()
	_ = c.Announce("开始")
	_ = c.Handle(Event{Type: "stt", Text: "开始"})
	if c.State() != Speaking {
		t.Fatalf("state=%s", c.State())
	}
}

func TestPauseAndResume(t *testing.T) {
	f := &fakeTransport{}
	c := NewController(f, Realtime)
	_ = c.Start()
	if err := c.Pause(); err != nil {
		t.Fatal(err)
	}
	if c.State() != Idle {
		t.Fatalf("state=%s", c.State())
	}
	if err := c.Resume(); err != nil {
		t.Fatal(err)
	}
	if c.State() != Listening {
		t.Fatalf("state=%s", c.State())
	}
	if len(f.calls) != 3 || f.calls[1] != "stop" || f.calls[2] != "listen:realtime" {
		t.Fatalf("calls=%v", f.calls)
	}
}

func TestDisconnectedDropsQueueAndReconnectedResumes(t *testing.T) {
	c := NewController(&fakeTransport{}, Realtime)
	_ = c.Start()
	_ = c.Handle(Event{Type: "tts", State: "start"})
	_ = c.Announce("排队的话")
	c.Disconnected()
	if c.State() != Disconnected || c.QueueLength() != 0 {
		t.Fatalf("state=%s queue=%d", c.State(), c.QueueLength())
	}
	if err := c.Reconnected(); err != nil || c.State() != Listening {
		t.Fatalf("state=%s err=%v", c.State(), err)
	}
}

func TestControllerSendTextAbortAndDefaults(t *testing.T) {
	f := &fakeTransport{}
	c := NewController(f, "")
	if c.Mode() != Realtime {
		t.Fatalf("default mode=%s", c.Mode())
	}
	if err := NewController(nil, Realtime).Start(); err == nil {
		t.Fatal("nil transport accepted")
	}
	_ = c.Start()
	if err := c.SendText("你好"); err != nil || c.State() != Thinking {
		t.Fatalf("state=%s err=%v", c.State(), err)
	}
	_ = c.Announce("排队")
	if err := c.Abort(); err != nil || c.State() != Listening || c.QueueLength() != 0 {
		t.Fatalf("state=%s queue=%d err=%v", c.State(), c.QueueLength(), err)
	}
	if err := c.Announce(""); err == nil {
		t.Fatal("empty announcement accepted")
	}
	want := []string{"listen:realtime", "text:你好", "announce:排队", "abort:user_interrupt"}
	if len(f.calls) != len(want) {
		t.Fatalf("calls=%v", f.calls)
	}
	for i := range want {
		if f.calls[i] != want[i] {
			t.Fatalf("calls=%v", f.calls)
		}
	}
}
