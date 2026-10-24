package voiceentry

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/open-anywhere/open-anywhere/internal/voiceentry/linx"
)

// fakeCodec stands in for libopus: an encoded packet is one byte long and a
// decoded one is the packet with a "pcm:" prefix.
type fakeCodec struct{ encodeErr error }

func (c fakeCodec) Encode(pcm []byte) ([]byte, error) {
	if c.encodeErr != nil {
		return nil, c.encodeErr
	}
	return []byte{byte(len(pcm) / 100)}, nil
}

func (fakeCodec) Decode(p []byte) ([]byte, error) { return append([]byte("pcm:"), p...), nil }

type fakePlayer struct {
	mu  sync.Mutex
	got [][]byte
}

func (p *fakePlayer) Write(pcm []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.got = append(p.got, append([]byte(nil), pcm...))
	return nil
}

// fakeSource yields frames 60 ms PCM frames and then ends.
type fakeSource struct {
	frames int
	closed atomic.Bool
}

func (s *fakeSource) ReadFrame() ([]byte, error) {
	if s.frames == 0 {
		return nil, io.EOF
	}
	s.frames--
	return make([]byte, 1920), nil
}

func (s *fakeSource) Close() error { s.closed.Store(true); return nil }

// fakeMedia returns a media set built from the given pieces.
func fakeMedia(c codec, src *fakeSource, player *fakePlayer, rate *atomic.Int32) media {
	return media{
		newCodec:   func() (codec, error) { return c, nil },
		fileSource: func(string) (frameSource, error) { return src, nil },
		micSource:  func(string) (frameSource, error) { return src, nil },
		newPlayer: func(r int) (pcmPlayer, error) {
			if rate != nil {
				rate.Store(int32(r))
			}
			return player, nil
		},
	}
}

func waitInt(t *testing.T, ch <-chan int) int {
	t.Helper()
	select {
	case n := <-ch:
		return n
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for a binary frame")
		return 0
	}
}

func dialWS(t *testing.T, base, path string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+base[len("http"):]+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	return c
}

func TestWebAudioEncodesBrowserPCMAndForwardsOpus(t *testing.T) {
	f := newFakeLinx(t, false)
	base, _ := startResident(t, Config{DevicePath: writeDevice(t, f.wsURL())}, func(a *App) {
		a.media = fakeMedia(fakeCodec{}, nil, nil, nil)
	})
	waitStatus(t, base, func(s statusBody) bool { return s.Connected })

	pcm := dialWS(t, base, "/audio")
	ctx := context.Background()
	// two whole 60 ms frames and a partial third one, in awkward chunks
	_ = pcm.Write(ctx, websocket.MessageBinary, make([]byte, 1000))
	_ = pcm.Write(ctx, websocket.MessageBinary, make([]byte, 2840))
	for i := 0; i < 2; i++ {
		if n := waitInt(t, f.binary); n != 1 {
			t.Fatalf("encoded frame has %d bytes", n)
		}
	}
	select {
	case n := <-f.binary:
		t.Fatalf("a partial frame was sent (%d bytes)", n)
	case <-time.After(150 * time.Millisecond):
	}

	opus := dialWS(t, base, "/audio?codec=opus")
	_ = opus.Write(ctx, websocket.MessageBinary, []byte{1, 2, 3, 4, 5})
	if n := waitInt(t, f.binary); n != 5 {
		t.Fatalf("opus packet forwarded as %d bytes", n)
	}
}

func TestWebAudioClosesWhenEncodingOrSendingFails(t *testing.T) {
	f := newFakeLinx(t, false)
	base, _ := startResident(t, Config{DevicePath: writeDevice(t, f.wsURL())}, func(a *App) {
		a.media = fakeMedia(fakeCodec{encodeErr: errors.New("encoder broke")}, nil, nil, nil)
	})
	waitStatus(t, base, func(s statusBody) bool { return s.Connected })
	c := dialWS(t, base, "/audio")
	_ = c.Write(context.Background(), websocket.MessageBinary, make([]byte, 1920))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, _, err := c.Read(ctx); err == nil {
		t.Fatal("socket stayed open after an encoding error")
	}

	// a healthy encoder still fails when Linx is not connected
	f2 := newFakeLinx(t, false)
	base2, _ := startResident(t, Config{DevicePath: writeDevice(t, f2.wsURL())}, func(a *App) {
		a.media = fakeMedia(fakeCodec{}, nil, nil, nil)
	})
	waitStatus(t, base2, func(s statusBody) bool { return s.Connected })
	post(t, base2+"/disconnect", `{}`)
	waitStatus(t, base2, func(s statusBody) bool { return !s.Connected })
	c2 := dialWS(t, base2, "/audio")
	_ = c2.Write(context.Background(), websocket.MessageBinary, make([]byte, 1920))
	if _, _, err := c2.Read(ctx); err == nil {
		t.Fatal("socket stayed open although nothing could be sent")
	}
}

func TestHandleEventPlaysDecodedSpeechLocally(t *testing.T) {
	f := newFakeLinx(t, false)
	a, err := New(Config{DevicePath: writeDevice(t, f.wsURL()), Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	player := &fakePlayer{}
	a.codec, a.player = fakeCodec{}, player
	if finished, closed := a.handleEvent(linx.Event{Type: "audio", Audio: []byte{4, 2}}); finished || closed {
		t.Fatalf("audio ended the run: finished=%v closed=%v", finished, closed)
	}
	if len(player.got) != 1 || string(player.got[0]) != "pcm:\x04\x02" {
		t.Fatalf("played=%q", player.got)
	}
	if _, closed := a.handleEvent(linx.Event{Type: "close"}); !closed {
		t.Fatal("close event did not report a closed connection")
	}
}

func TestToolsPublishVolumeChanges(t *testing.T) {
	f := newFakeLinx(t, false)
	a, err := New(Config{DevicePath: writeDevice(t, f.wsURL()), Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.tools.List()[0].Call(map[string]any{"volume": 20.0}); err != nil || a.tools.Volume() != 20 {
		t.Fatalf("volume=%d err=%v", a.tools.Volume(), err)
	}
}

func TestOneShotFileStreamsFramesAndPlaysSpeech(t *testing.T) {
	f := newFakeLinx(t, false)
	src, player := &fakeSource{frames: 3}, &fakePlayer{}
	var rate atomic.Int32
	a, err := New(Config{DevicePath: writeDevice(t, f.wsURL()), File: "clip.wav", Timeout: 400 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	a.media = fakeMedia(fakeCodec{}, src, player, &rate)
	if err := a.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "超时") {
		t.Fatalf("err=%v, want a reply timeout", err)
	}
	for i := 0; i < 3; i++ {
		waitInt(t, f.binary)
	}
	f.waitFor(t, func(m map[string]any) bool { return m["type"] == "listen" && m["state"] == "stop" })
	if !src.closed.Load() || rate.Load() != 16000 {
		t.Fatalf("source closed=%v player rate=%d", src.closed.Load(), rate.Load())
	}
}

func TestOneShotMicUsesTheMicrophoneAndHonoursNoPlay(t *testing.T) {
	f := newFakeLinx(t, false)
	var micName string
	var played atomic.Bool
	a, err := New(Config{DevicePath: writeDevice(t, f.wsURL()), Mic: "Microphone (USB)", NoPlay: true, Timeout: 300 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	src := &fakeSource{frames: 1}
	a.media = fakeMedia(fakeCodec{}, src, nil, nil)
	a.media.micSource = func(device string) (frameSource, error) { micName = device; return src, nil }
	a.media.newPlayer = func(int) (pcmPlayer, error) { played.Store(true); return nil, errors.New("must not be called") }
	_ = a.Run(context.Background())
	if micName != "Microphone (USB)" || played.Load() {
		t.Fatalf("mic=%q playerCreated=%v", micName, played.Load())
	}
}

func TestOneShotAudioSourceFailures(t *testing.T) {
	f := newFakeLinx(t, false)
	run := func(cfg Config, tweak func(*media)) error {
		cfg.DevicePath = writeDevice(t, f.wsURL())
		cfg.Timeout = 300 * time.Millisecond
		a, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		a.media = fakeMedia(fakeCodec{}, &fakeSource{}, &fakePlayer{}, nil)
		tweak(&a.media)
		return a.Run(context.Background())
	}
	boom := errors.New("boom")
	if err := run(Config{File: "x"}, func(m *media) { m.newCodec = func() (codec, error) { return nil, boom } }); !errors.Is(err, boom) {
		t.Fatalf("codec err=%v", err)
	}
	if err := run(Config{File: "x"}, func(m *media) { m.fileSource = func(string) (frameSource, error) { return nil, boom } }); !errors.Is(err, boom) {
		t.Fatalf("file err=%v", err)
	}
	if err := run(Config{Mic: "x"}, func(m *media) { m.micSource = func(string) (frameSource, error) { return nil, boom } }); !errors.Is(err, boom) {
		t.Fatalf("mic err=%v", err)
	}
	// an encoder that fails ends the streaming goroutine without sending anything
	if err := run(Config{File: "x"}, func(m *media) {
		m.newCodec = func() (codec, error) { return fakeCodec{encodeErr: boom}, nil }
		m.fileSource = func(string) (frameSource, error) { return &fakeSource{frames: 2}, nil }
	}); err == nil {
		t.Fatal("run succeeded without a reply")
	}
	select {
	case n := <-f.binary:
		t.Fatalf("a frame was sent although encoding failed (%d bytes)", n)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestAudioFileNeedsARegisteredDevice(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(Config{DevicePath: filepath.Join(t.TempDir(), "none.json"), Serve: true, Listener: ln, File: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "设备未注册") {
		t.Fatalf("err=%v", err)
	}
}

func TestRunFailsWhenLinxIsUnreachable(t *testing.T) {
	a, err := New(Config{DevicePath: writeDevice(t, "ws://127.0.0.1:1/"), Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Run(context.Background()); err == nil {
		t.Fatal("Run succeeded without a connection")
	}
}

func TestAnnouncementWithoutADeviceFailsFast(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(Config{DevicePath: filepath.Join(t.TempDir(), "none.json"), Serve: true, Listener: ln, Announce: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Run(context.Background()); err == nil {
		t.Fatal("announcement without a session succeeded")
	}
}

func TestOneShotEndsWhenCancelledOrDisconnected(t *testing.T) {
	f := newFakeLinxWith(t, &fakeLinx{silent: true})
	a, err := New(Config{DevicePath: writeDevice(t, f.wsURL()), Text: "你好", Timeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	f.waitFor(t, func(m map[string]any) bool { return m["state"] == "detect" })
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run ignored cancellation")
	}

	hangup := newFakeLinxWith(t, &fakeLinx{closeOnDetect: true})
	a, err = New(Config{DevicePath: writeDevice(t, hangup.wsURL()), Text: "你好", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "关闭") {
		t.Fatalf("err=%v, want a closed-connection error", err)
	}
}

func TestCommandGivesUpWhenNobodyIsListening(t *testing.T) {
	f := newFakeLinx(t, false)
	a, err := New(Config{DevicePath: writeDevice(t, f.wsURL()), Serve: true})
	if err != nil {
		t.Fatal(err)
	}
	a.runCtx, a.cmdTimeout = context.Background(), 20*time.Millisecond
	if err := a.command(true); err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("err=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a.runCtx, a.cmdTimeout = ctx, time.Minute
	if err := a.command(false); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestResidentKeepsRetryingWhileLinxRefusesConnections(t *testing.T) {
	f := newFakeLinxWith(t, &fakeLinx{dropFirst: true, refuseFrom: 2})
	base, stop := startResident(t, Config{DevicePath: writeDevice(t, f.wsURL()), ReconnectDelays: []time.Duration{5 * time.Millisecond}})
	deadline := time.Now().Add(3 * time.Second)
	for f.sessions.Load() < 4 {
		if time.Now().After(deadline) {
			t.Fatalf("only %d connection attempts", f.sessions.Load())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if st := getStatus(t, base); st.Connected {
		t.Fatalf("status=%+v", st)
	}
	if err := stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func TestResidentStopsWhileWaitingToReconnect(t *testing.T) {
	f := newFakeLinx(t, true)
	_, stop := startResident(t, Config{DevicePath: writeDevice(t, f.wsURL()), ReconnectDelays: []time.Duration{time.Hour}})
	deadline := time.Now().Add(3 * time.Second)
	for f.sessions.Load() < 1 {
		if time.Now().After(deadline) {
			t.Fatal("never connected")
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond) // the drop has been noticed and the long wait started
	if err := stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func TestManualReconnectFailureLeavesTheEntryDisconnected(t *testing.T) {
	f := newFakeLinxWith(t, &fakeLinx{refuseFrom: 2})
	base, _ := startResident(t, Config{DevicePath: writeDevice(t, f.wsURL())})
	waitStatus(t, base, func(s statusBody) bool { return s.Connected })
	if got := post(t, base+"/reconnect", `{}`); got != http.StatusBadGateway {
		t.Fatalf("reconnect=%d", got)
	}
	st := waitStatus(t, base, func(s statusBody) bool { return s.State == "disconnected" })
	if st.Connected {
		t.Fatalf("status=%+v", st)
	}
}

func TestShutdownForcesOpenStreamsClosed(t *testing.T) {
	f := newFakeLinx(t, false)
	base, stop := startResident(t, Config{DevicePath: writeDevice(t, f.wsURL())})
	waitStatus(t, base, func(s statusBody) bool { return s.Connected })
	resp, err := http.Get(base + "/events") // an SSE client that never goes away on its own
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	start := time.Now()
	if err := stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("shutdown took %s", time.Since(start))
	}
}
