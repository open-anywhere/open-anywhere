// Package voiceentry runs the on-site voice entry for Linx.
//
// It owns the Linx session, connects it to the voice controller and the web
// control API, and keeps the connection alive. Two modes exist: one-shot
// (send a text, announcement or audio file and wait for the reply) and
// resident (-serve), which stays up, reconnects and serves the web console.
package voiceentry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/voiceentry/audio"
	"github.com/open-anywhere/open-anywhere/internal/voiceentry/casting"
	"github.com/open-anywhere/open-anywhere/internal/voiceentry/controlapi"
	"github.com/open-anywhere/open-anywhere/internal/voiceentry/linx"
	"github.com/open-anywhere/open-anywhere/internal/voiceentry/register"
	"github.com/open-anywhere/open-anywhere/internal/voiceentry/tools"
	"github.com/open-anywhere/open-anywhere/internal/voiceentry/voice"
)

// Config selects what the client does. The zero value is not runnable:
// Serve or one of Text, Announce, File and Mic must be set.
type Config struct {
	// DevicePath is the credential file. In resident mode it may be missing,
	// and the device can then be registered from the web console.
	DevicePath string
	Mode       voice.Mode

	// Serve keeps the client running and exposes the web control API.
	Serve bool
	// Listen is the control API address. Listener, when set, is used instead.
	Listen   string
	Listener net.Listener
	// NoListen keeps the control API off the network. The embedding process
	// mounts Handler at its own route instead.
	NoListen bool

	// One-shot actions; the first non-empty one wins.
	Text     string
	Announce string
	File     string
	Mic      string
	NoPlay   bool

	// OTA is the platform client used for registration; the zero value uses production.
	OTA linx.OTAClient
	// Out receives the printed transcript. Defaults to standard output.
	Out io.Writer
	// Timeout bounds how long a one-shot run waits for the reply. Defaults to 120s.
	Timeout time.Duration
	// ReconnectDelays is the backoff after an unexpected disconnect.
	ReconnectDelays []time.Duration

	// CastingURL is the local casting service the device is paired with, for
	// example http://192.168.1.20:8091. When set, the device reports the
	// self.cast.* tools: Linx runs the application in the cloud, gets a stream
	// address back, and uses these tools to put it on a screen on site.
	CastingURL string
}

func (c *Config) applyDefaults() {
	if c.Mode == "" {
		c.Mode = voice.Realtime
	}
	if c.Listen == "" {
		c.Listen = "127.0.0.1:8788"
	}
	if c.Out == nil {
		c.Out = os.Stdout
	}
	if c.Timeout <= 0 {
		c.Timeout = 120 * time.Second
	}
	if c.ReconnectDelays == nil {
		c.ReconnectDelays = defaultReconnectDelays
	}
}

// connCmd is a web request to drop or re-establish the Linx connection. The
// run loop owns the session, so it executes the command and answers on done.
type connCmd struct {
	reconnect bool
	done      chan error
}

// App is one client instance. Create it with New and run it once with Run.
type App struct {
	cfg        Config
	dev        atomic.Pointer[linx.Device]
	tools      *tools.Set
	casting    *casting.Tools
	transport  *transport
	controller *voice.Controller
	api        *controlapi.Server
	reg        *register.Registrar
	conns      chan connCmd
	runCtx     context.Context

	media      media
	cmdTimeout time.Duration

	// The fields below belong to the run loop.
	session *linx.Session
	down    bool
	codec   codec
	player  pcmPlayer
}

// New loads the device credentials and wires the components together without
// touching the network.
func New(cfg Config) (*App, error) {
	cfg.applyDefaults()
	a := &App{cfg: cfg, transport: &transport{}, conns: make(chan connCmd), media: systemMedia(), cmdTimeout: 45 * time.Second}
	d, err := linx.LoadDevice(cfg.DevicePath)
	switch {
	case err == nil:
		a.dev.Store(&d)
	case cfg.Serve && errors.Is(err, fs.ErrNotExist):
		log.Printf("未找到 %s，请在网页控制台注册设备", cfg.DevicePath)
	default:
		return nil, err
	}
	a.tools = tools.New(func(e voice.Event) { a.api.Publish(e) })
	a.casting = casting.NewTools(casting.New(casting.Config{URL: cfg.CastingURL}), func(text string) {
		a.api.Publish(voice.Event{Type: "cast", Text: text})
	})
	a.controller = voice.NewController(a.transport, cfg.Mode)
	a.reg = register.New(register.Config{
		Path:   cfg.DevicePath,
		OTA:    cfg.OTA,
		Device: &a.dev,
		Apply:  func() error { return a.command(true) },
	})
	a.api = controlapi.New(a.controller, a.status)
	return a, nil
}

// deviceTools is the device-side tool list for the current configuration.
func (a *App) deviceTools() []linx.Tool {
	list := a.tools.List()
	if a.cfg.CastingURL != "" {
		list = append(list, a.casting.List()...)
	}
	return list
}

func (a *App) status() controlapi.Status {
	st := controlapi.Status{
		State:          a.controller.State(),
		Mode:           a.controller.Mode(),
		SessionID:      a.transport.SessionID(),
		Connected:      a.transport.Connected(),
		Volume:         a.tools.Volume(),
		ActivationCode: a.reg.ActivationCode(),
	}
	if dev := a.dev.Load(); dev != nil {
		st.MAC, st.Registered = dev.MAC, true
	}
	return st
}

// Handler returns the web control API, or nil when the client is not serving.
// The caller is responsible for authentication and ownership checks before
// mounting it: sameOriginOnly only protects against other web origins.
func (a *App) Handler() http.Handler {
	if a.api == nil {
		return nil
	}
	return a.api.Handler()
}

// Register advances device registration in-process. It is the same operation
// as POST /device/register on the control API; Run must already be active.
func (a *App) Register(ctx context.Context, in register.Request) (register.Result, error) {
	return a.reg.Register(ctx, in)
}

// Status reports the current session state without exposing credentials.
func (a *App) Status() controlapi.Status { return a.status() }

// Run connects, serves and processes events until ctx is cancelled, the
// one-shot reply arrives, or an unrecoverable error occurs. A cancelled
// resident run returns nil.
func (a *App) Run(ctx context.Context) error {
	a.runCtx = ctx
	if dev := a.dev.Load(); dev != nil {
		s, err := linx.NewSession(linx.Config{Device: *dev, Tools: a.deviceTools()})
		if err != nil {
			return err
		}
		a.session = s
		a.transport.Set(s)
		if err := a.controller.Start(); err != nil {
			_ = s.Close()
			if !a.cfg.Serve {
				return err
			}
			// A resident run must survive Linx hanging up right after the
			// greeting: the closed session sends the loop into reconnecting.
			log.Printf("start listening: %v", err)
		}
	}
	defer func() {
		if a.session != nil {
			_ = a.session.Close()
		}
	}()
	a.down = a.session == nil

	if a.cfg.Serve {
		a.setupWebAudio()
		a.api.SetRegistration(a.reg.Register)
		a.api.SetConnectionControl(func() error { return a.command(false) }, func() error { return a.command(true) })
		if !a.cfg.NoListen {
			shutdown, err := a.serveHTTP()
			if err != nil {
				return err
			}
			defer shutdown()
		}
	}
	stop, err := a.startOneShot()
	if err != nil {
		return err
	}
	defer stop()
	return a.loop(ctx)
}

// setupWebAudio lets the browser send audio: Opus packets are forwarded as
// they are, raw PCM is encoded here when an Opus codec was built in.
func (a *App) setupWebAudio() {
	a.api.SetPlaybackSampleRate(linx.DefaultSampleRate)
	if a.session != nil {
		a.api.SetPlaybackSampleRate(a.session.AudioParams().SampleRate)
	}
	a.api.SetOpusHandler(a.transport.SendAudio)
	codec, err := a.media.newCodec()
	if err != nil {
		return
	}
	a.codec = codec
	var mu sync.Mutex
	buf := make([]byte, 0, audio.PCMFrameBytes*2)
	a.api.SetPCMHandler(func(pcm []byte) error {
		mu.Lock()
		defer mu.Unlock()
		buf = append(buf, pcm...)
		for len(buf) >= audio.PCMFrameBytes {
			packet, err := codec.Encode(buf[:audio.PCMFrameBytes])
			if err != nil {
				return err
			}
			if err := a.transport.SendAudio(packet); err != nil {
				return err
			}
			buf = buf[audio.PCMFrameBytes:]
		}
		return nil
	})
}

// serveHTTP starts the control API and returns a function that stops it.
func (a *App) serveHTTP() (shutdown func(), err error) {
	ln := a.cfg.Listener
	if ln == nil {
		if ln, err = net.Listen("tcp", a.cfg.Listen); err != nil {
			return nil, fmt.Errorf("listen %s: %w", a.cfg.Listen, err)
		}
	}
	srv := &http.Server{Handler: a.api.Handler(), ReadHeaderTimeout: 10 * time.Second}
	log.Printf("local control API listening on http://%s", ln.Addr())
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("control API: %v", err)
		}
	}()
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			_ = srv.Close() // long-lived SSE and WebSocket clients
		}
	}, nil
}

// startOneShot performs the requested one-shot action and returns a cleanup function.
func (a *App) startOneShot() (func(), error) {
	noop := func() {}
	switch {
	case a.cfg.Announce != "":
		return noop, a.controller.Announce(a.cfg.Announce)
	case a.cfg.Text != "":
		return noop, a.controller.SendText(a.cfg.Text)
	case a.cfg.File != "" || a.cfg.Mic != "":
		return a.startAudioSource()
	}
	return noop, nil
}

// startAudioSource streams an audio file or microphone to Linx.
func (a *App) startAudioSource() (func(), error) {
	if a.session == nil {
		return nil, errors.New("设备未注册")
	}
	codec, err := a.media.newCodec()
	if err != nil {
		return nil, err
	}
	a.codec = codec
	var source frameSource
	if a.cfg.File != "" {
		source, err = a.media.fileSource(a.cfg.File)
	} else {
		source, err = a.media.micSource(a.cfg.Mic)
	}
	if err != nil {
		return nil, err
	}
	if !a.cfg.NoPlay {
		a.player, _ = a.media.newPlayer(a.session.AudioParams().SampleRate)
	}
	go func() {
		for {
			pcm, err := source.ReadFrame()
			if err != nil {
				_ = a.transport.ListenStop()
				return
			}
			packet, err := codec.Encode(pcm)
			if err != nil {
				return
			}
			_ = a.transport.SendAudio(packet)
		}
	}()
	return func() { _ = source.Close() }, nil
}

// command asks the run loop to disconnect or reconnect and waits for the result.
func (a *App) command(reconnect bool) error {
	c := connCmd{reconnect: reconnect, done: make(chan error, 1)}
	select {
	case a.conns <- c:
	case <-time.After(a.cmdTimeout):
		return errors.New("voice entry is busy reconnecting")
	case <-a.runCtx.Done():
		return a.runCtx.Err()
	}
	return <-c.done
}

func (a *App) loop(ctx context.Context) error {
	var timeout <-chan time.Time
	if !a.cfg.Serve {
		timer := time.NewTimer(a.cfg.Timeout)
		defer timer.Stop()
		timeout = timer.C
	}
	for {
		var events <-chan linx.Event
		if !a.down {
			events = a.session.Events()
		}
		select {
		case <-ctx.Done():
			if a.cfg.Serve {
				return nil
			}
			return ctx.Err()
		case <-timeout:
			return errors.New("等待回复超时")
		case c := <-a.conns:
			a.handleCommand(ctx, c)
		case e, ok := <-events:
			finished, closed := false, false
			if ok {
				finished, closed = a.handleEvent(e)
			} else {
				_ = a.controller.Handle(voice.Event{Type: "close"})
				closed = true
			}
			if finished {
				return nil
			}
			if !closed {
				continue
			}
			if !a.cfg.Serve {
				return errors.New("灵矽连接已关闭")
			}
			a.autoReconnect(ctx)
		}
	}
}

// autoReconnect re-dials after an unexpected disconnect, with backoff.
func (a *App) autoReconnect(ctx context.Context) {
	err := reconnect(ctx, &a.session, a.transport, a.controller, *a.dev.Load(), a.deviceTools(), a.cfg.ReconnectDelays)
	if err != nil {
		log.Printf("reconnect: %v", err)
		return
	}
	a.api.SetPlaybackSampleRate(a.session.AudioParams().SampleRate)
}

func (a *App) handleCommand(ctx context.Context, c connCmd) {
	if !c.reconnect {
		if a.session != nil {
			_ = a.session.Close()
		}
		a.transport.Set(nil)
		a.controller.Disconnected()
		a.api.Publish(voice.Event{Type: "close", Text: "已手动断开"})
		a.down = true
		c.done <- nil
		return
	}
	dev := a.dev.Load()
	if dev == nil {
		c.done <- errors.New("设备未注册，请先在网页注册")
		return
	}
	err := reconnect(ctx, &a.session, a.transport, a.controller, *dev, a.deviceTools(), []time.Duration{0})
	a.down = err != nil
	if err != nil {
		a.controller.Disconnected()
	} else {
		a.api.SetPlaybackSampleRate(a.session.AudioParams().SampleRate)
		a.api.Publish(voice.Event{Type: "hello", Text: "已重新连接"})
	}
	c.done <- err
}

// handleEvent feeds one Linx event to the controller and the web console.
// finished is true when a one-shot run has received its complete reply;
// closed is true when the connection ended.
func (a *App) handleEvent(e linx.Event) (finished, closed bool) {
	ve := voice.Event{Type: e.Type, Text: e.Text}
	if e.Type != "audio" && json.Valid(e.Raw) {
		ve.Raw = e.Raw
	}
	state := ""
	if e.Type == "tts" {
		state, _ = e.Data["state"].(string)
		ve.State = state
		ve.Aborted, _ = e.Data["is_aborted"].(bool)
	}
	_ = a.controller.Handle(ve)
	// Audio frames have their own channel. Sending them to the event stream
	// fills its small buffer and pushes out the transcript events.
	if e.Type != "audio" {
		a.api.Publish(ve)
	}
	if e.Type == "audio" {
		a.api.PublishAudio(e.Audio)
		if a.player != nil && a.codec != nil {
			if pcm, err := a.codec.Decode(e.Audio); err == nil {
				_ = a.player.Write(pcm)
			}
		}
	}
	switch e.Type {
	case "stt":
		fmt.Fprintf(a.cfg.Out, "用户: %s\n", e.Text)
	case "tts":
		if e.Text != "" {
			fmt.Fprintf(a.cfg.Out, "灵矽: %s\n", e.Text)
		}
		if state == "stop" && !a.cfg.Serve {
			return true, false
		}
	case "close":
		return false, true
	}
	return false, false
}
