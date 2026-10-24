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
}

func startMedia(life, startup context.Context, cfg Config, url, logPath string, h *hub) (*media, error) {
	ctx, cancel := context.WithCancel(life)
	m := &media{hub: h, cancel: cancel, processDone: make(chan struct{}), failure: make(chan error, 1)}
	options := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	options = append(options, chromedp.WindowSize(cfg.Width, cfg.Height), chromedp.Flag("hide-scrollbars", true), chromedp.Flag("disable-background-timer-throttling", true))
	if cfg.Chrome != "" {
		options = append(options, chromedp.ExecPath(cfg.Chrome))
	}
	alloc, allocCancel := chromedp.NewExecAllocator(ctx, options...)
	m.allocatorCancel = allocCancel
	browser, browserCancel := chromedp.NewContext(alloc)
	m.browserCancel = browserCancel
	// Cancel startup operations if the independent startup deadline expires.
	stopDeadline := context.AfterFunc(startup, cancel)
	defer stopDeadline()
	if err := chromedp.Run(browser, chromedp.EmulateViewport(int64(cfg.Width), int64(cfg.Height)), chromedp.Navigate(url), chromedp.WaitReady("body", chromedp.ByQuery)); err != nil {
		return m, fmt.Errorf("load browser page: %w", err)
	}
	var err error
	m.log, err = os.Create(logPath)
	if err != nil {
		return m, err
	}
	fps := strconv.Itoa(cfg.FPS)
	m.cmd = exec.Command(cfg.FFmpeg, "-hide_banner", "-loglevel", "warning", "-f", "image2pipe", "-vcodec", "mjpeg", "-framerate", fps, "-i", "pipe:0", "-an", "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency", "-pix_fmt", "yuv420p", "-threads", "2", "-g", fps, "-keyint_min", fps, "-sc_threshold", "0", "-mpegts_flags", "+resend_headers", "-muxdelay", "0", "-muxpreload", "0", "-f", "mpegts", "pipe:1")
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
		for {
			var frame []byte
			err := chromedp.Run(browser, chromedp.ActionFunc(func(c context.Context) error {
				var e error
				frame, e = page.CaptureScreenshot().WithFormat(page.CaptureScreenshotFormatJpeg).WithQuality(85).Do(c)
				return e
			}))
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
	err := stopProcess(m.cmd, m.processDone)
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
