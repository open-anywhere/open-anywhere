package render

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// hub continuously drains the encoder even with no viewers. Slow viewers are
// disconnected rather than blocking the encoder or buffering unbounded video.
type hub struct {
	mu      sync.Mutex
	viewers map[chan []byte]struct{}
	closed  bool
	last    time.Time
	ready   chan struct{}
	once    sync.Once
}

func newHub() *hub { return &hub{viewers: make(map[chan []byte]struct{}), ready: make(chan struct{})} }
func (h *hub) publish(b []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.last = time.Now()
	h.once.Do(func() { close(h.ready) })
	for ch := range h.viewers {
		select {
		case ch <- b:
		default:
			delete(h.viewers, ch)
			close(ch)
		}
	}
}
func (h *hub) subscribe() (chan []byte, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan []byte, 128)
	if h.closed {
		close(ch)
	} else {
		h.viewers[ch] = struct{}{}
	}
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := h.viewers[ch]; ok {
			delete(h.viewers, ch)
			close(ch)
		}
	}
}
func (h *hub) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.closed {
		h.closed = true
		for ch := range h.viewers {
			close(ch)
			delete(h.viewers, ch)
		}
	}
}
func (h *hub) stalled() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return !h.last.IsZero() && time.Since(h.last) > 5*time.Second
}

type media struct {
	hub             *hub
	cancel          context.CancelFunc
	browserCancel   context.CancelFunc
	allocatorCancel context.CancelFunc
	cmd             *exec.Cmd
	processDone     chan struct{}
	workers         sync.WaitGroup
	failure         chan error
	log             *os.File
	stopProc        func(*exec.Cmd, <-chan struct{}) error
}

// browserHooks isolates the Chrome/DevTools machinery so tests can exercise the
// ffmpeg pipeline without launching a real browser.
type browserHooks struct {
	start   func(context.Context, Config) (context.Context, context.CancelFunc, context.CancelFunc)
	run     func(context.Context, ...chromedp.Action) error
	capture func(context.Context) ([]byte, error)
	layout  func(context.Context) (layoutMetrics, error)
}

// layoutMetrics describes the rendered page's scrollable content and viewport.
type layoutMetrics struct {
	Width          int `json:"width"`
	Height         int `json:"height"`
	ViewportWidth  int `json:"viewportWidth"`
	ViewportHeight int `json:"viewportHeight"`
}

func (c Config) browserHooks() browserHooks {
	h := browserHooks{
		start: func(ctx context.Context, cfg Config) (context.Context, context.CancelFunc, context.CancelFunc) {
			options := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
			options = append(options, chromedp.WindowSize(cfg.Width, cfg.Height), chromedp.Flag("hide-scrollbars", true), chromedp.Flag("disable-background-timer-throttling", true))
			if cfg.audioEnabled() {
				// Headless Chromium commonly starts with audio muted and blocks
				// media autoplay. Render has no user gesture, so explicitly enable
				// both when an audio capture device is configured.
				options = append(options,
					chromedp.Flag("mute-audio", false),
					chromedp.Flag("autoplay-policy", "no-user-gesture-required"),
				)
			}
			if cfg.Chrome != "" {
				options = append(options, chromedp.ExecPath(cfg.Chrome))
			}
			alloc, allocCancel := chromedp.NewExecAllocator(ctx, options...)
			browser, browserCancel := chromedp.NewContext(alloc)
			return browser, browserCancel, allocCancel
		},
		run:     chromedp.Run,
		capture: captureFrame,
		layout:  measureLayout,
	}
	if c.browser.start != nil {
		h.start = c.browser.start
	}
	if c.browser.run != nil {
		h.run = c.browser.run
	}
	if c.browser.capture != nil {
		h.capture = c.browser.capture
	}
	if c.browser.layout != nil {
		h.layout = c.browser.layout
	}
	return h
}

// captureFrame grabs one JPEG screenshot from the page attached to ctx.
func captureFrame(ctx context.Context) ([]byte, error) {
	var frame []byte
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(c context.Context) error {
		return captureScreenshot(c, &frame)
	}))
	return frame, err
}

// captureScreenshot performs the DevTools Page.captureScreenshot call used by
// captureFrame.
func captureScreenshot(ctx context.Context, frame *[]byte) error {
	var err error
	*frame, err = page.CaptureScreenshot().WithFormat(page.CaptureScreenshotFormatJpeg).WithQuality(85).Do(ctx)
	return err
}

// measureLayout reads the page's scrollable content size and viewport size.
// It is diagnostic only: a failure to measure must not stop the stream.
func measureLayout(ctx context.Context) (layoutMetrics, error) {
	var m layoutMetrics
	err := chromedp.Run(ctx, chromedp.Evaluate(`({
		width: Math.max(document.documentElement.scrollWidth, document.body ? document.body.scrollWidth : 0),
		height: Math.max(document.documentElement.scrollHeight, document.body ? document.body.scrollHeight : 0),
		viewportWidth: window.innerWidth,
		viewportHeight: window.innerHeight
	})`, &m))
	return m, err
}

const (
	audioSampleRate = 48000
	audioChannels   = 2
)

// encoderArgs builds the FFmpeg command used by one Render instance. The
// screenshot pipe is always input 0. If an audio device is configured, input
// 1 is a live PCM stream captured by FFmpeg and encoded as AAC in the same
// MPEG-TS output. Keeping both inputs in one process gives the muxer one clock
// and avoids a second process introducing drift or an unbounded PCM buffer.
func encoderArgs(cfg Config, fps string) []string {
	args := []string{
		"-hide_banner", "-loglevel", "warning",
		"-f", "image2pipe", "-vcodec", "mjpeg", "-framerate", fps, "-i", "pipe:0",
	}
	if cfg.audioEnabled() {
		args = append(args,
			"-thread_queue_size", "512",
			"-f", cfg.AudioFormat, "-i", cfg.AudioInput,
			"-map", "0:v:0", "-map", "1:a:0",
		)
	} else {
		args = append(args, "-an")
	}
	args = append(args,
		"-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
		"-pix_fmt", "yuv420p", "-threads", "2",
		"-g", fps, "-keyint_min", fps, "-sc_threshold", "0",
	)
	if cfg.audioEnabled() {
		args = append(args,
			"-c:a", "aac", "-profile:a", "aac_low", "-b:a", "128k",
			"-ar", strconv.Itoa(audioSampleRate), "-ac", strconv.Itoa(audioChannels),
			// Keep audio timestamps continuous when a capture device starts a
			// little before or after the first screenshot.
			"-af", "aresample=async=1:first_pts=0",
		)
	}
	return append(args,
		"-max_muxing_queue_size", "1024",
		"-mpegts_flags", "+resend_headers", "-muxdelay", "0", "-muxpreload", "0",
		"-f", "mpegts", "pipe:1",
	)
}

func startMedia(life, startup context.Context, cfg Config, url, logPath string, h *hub) (*media, error) {
	hooks := cfg.browserHooks()
	ctx, cancel := context.WithCancel(life)
	stopProc := cfg.stopGroup
	if stopProc == nil {
		stopProc = stopProcess
	}
	m := &media{hub: h, cancel: cancel, processDone: make(chan struct{}), failure: make(chan error, 1), stopProc: stopProc}
	browser, browserCancel, allocCancel := hooks.start(ctx, cfg)
	m.allocatorCancel = allocCancel
	m.browserCancel = browserCancel
	// Cancel startup operations if the independent startup deadline expires.
	stopDeadline := context.AfterFunc(startup, cancel)
	defer stopDeadline()
	if err := hooks.run(browser, chromedp.EmulateViewport(int64(cfg.Width), int64(cfg.Height)), chromedp.Navigate(url), chromedp.WaitReady("body", chromedp.ByQuery)); err != nil {
		return m, fmt.Errorf("load browser page: %w", err)
	}
	// Overflow is a build-contract defect, not a render failure: report it and
	// keep streaming the visible viewport. Re-check periodically because page
	// content can grow after load.
	overflow := false
	var lastOverflowLog time.Time
	checkLayout := func() {
		if hooks.layout == nil || cfg.Logger == nil {
			return
		}
		metrics, err := hooks.layout(browser)
		if err != nil {
			return
		}
		if metrics.Width <= metrics.ViewportWidth && metrics.Height <= metrics.ViewportHeight {
			overflow = false
			return
		}
		if overflow && time.Since(lastOverflowLog) < 10*time.Second {
			return
		}
		cfg.Logger.Printf("layout_overflow content=%dx%d viewport=%dx%d canvas=%dx%d", metrics.Width, metrics.Height, metrics.ViewportWidth, metrics.ViewportHeight, cfg.Width, cfg.Height)
		overflow = true
		lastOverflowLog = time.Now()
	}
	var err error
	m.log, err = os.Create(logPath)
	if err != nil {
		return m, err
	}
	fps := strconv.Itoa(cfg.FPS)
	m.cmd = exec.Command(cfg.FFmpeg, encoderArgs(cfg, fps)...)
	ownProcessGroup(m.cmd)
	m.cmd.Stderr = m.log
	stdin, err := m.cmd.StdinPipe()
	if err != nil {
		return m, err
	}
	stdout, err := m.cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return m, err
	}
	if err = m.cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		m.cmd = nil
		return m, err
	}
	m.workers.Go(func() {
		defer stdin.Close()
		ticker := time.NewTicker(time.Second / time.Duration(cfg.FPS))
		defer ticker.Stop()
		frames := 0
		for {
			if frames%cfg.FPS == 0 {
				checkLayout()
			}
			frames++
			frame, err := hooks.capture(browser)
			if err != nil {
				m.fail(fmt.Errorf("browser capture: %w", err))
				return
			}
			if _, err = stdin.Write(frame); err != nil {
				m.fail(err)
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	})
	outputDone := make(chan struct{})
	m.workers.Go(func() {
		defer close(outputDone)
		// Keep packet boundaries intact for independent HTTP subscribers.
		buf := make([]byte, 188*7)
		for {
			n, e := io.ReadFull(stdout, buf)
			if n > 0 {
				packet := append([]byte(nil), buf[:n]...)
				h.publish(packet)
			}
			if e != nil {
				m.fail(e)
				return
			}
		}
	})
	go func() {
		<-outputDone // Finish reading StdoutPipe before Wait closes it.
		err := m.cmd.Wait()
		if err == nil {
			err = errors.New("encoder exited")
		}
		m.fail(err)
		close(m.processDone)
	}()
	select {
	case <-h.ready:
		return m, nil
	case err := <-m.failure:
		return m, err
	case <-startup.Done():
		return m, startup.Err()
	case <-ctx.Done():
		return m, ctx.Err()
	}
}
func (m *media) fail(err error) {
	select {
	case m.failure <- err:
	default:
	}
}
func (m *media) close() error {
	m.hub.close()
	m.cancel()
	stop := m.stopProc
	if stop == nil {
		stop = stopProcess
	}
	err := stop(m.cmd, m.processDone)
	if m.browserCancel != nil {
		m.browserCancel()
	}
	if m.allocatorCancel != nil {
		m.allocatorCancel()
	}
	m.workers.Wait()
	if m.log != nil {
		_ = m.log.Close()
	}
	return err
}
