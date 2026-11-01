package voiceentry

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/voiceentry/linx"
	"github.com/open-anywhere/open-anywhere/internal/voiceentry/voice"
)

// errReconnecting is returned for any send while no Linx session is attached.
var errReconnecting = errors.New("Linx session is reconnecting")

// defaultReconnectDelays is the backoff used when the Linx connection drops by itself.
var defaultReconnectDelays = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 15 * time.Second}

// transport forwards voice.Controller calls to whichever Linx session is
// current, so the session can be replaced while the controller keeps running.
type transport struct {
	mu sync.RWMutex
	s  *linx.Session
}

func (t *transport) Set(s *linx.Session) { t.mu.Lock(); t.s = s; t.mu.Unlock() }

func (t *transport) Connected() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.s != nil
}

func (t *transport) SessionID() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.s == nil {
		return ""
	}
	return t.s.SessionID()
}

// Session returns the current Linx session, nil when disconnected.
func (t *transport) Session() *linx.Session {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.s
}

func (t *transport) current() (*linx.Session, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.s == nil {
		return nil, errReconnecting
	}
	return t.s, nil
}

func (t *transport) ListenStart(mode string) error {
	s, err := t.current()
	if err != nil {
		return err
	}
	return s.ListenStart(mode)
}

func (t *transport) ListenStop() error {
	s, err := t.current()
	if err != nil {
		return err
	}
	return s.ListenStop()
}

func (t *transport) SendText(text string) error {
	s, err := t.current()
	if err != nil {
		return err
	}
	return s.SendText(text)
}

func (t *transport) Announce(text string) error {
	s, err := t.current()
	if err != nil {
		return err
	}
	return s.Announce(text)
}

func (t *transport) Abort(reason string) error {
	s, err := t.current()
	if err != nil {
		return err
	}
	return s.Abort(reason)
}

func (t *transport) SendAudio(packet []byte) error {
	s, err := t.current()
	if err != nil {
		return err
	}
	return s.SendAudio(packet)
}

// reconnect closes *current and dials a new session, waiting delays[i] before
// attempt i. On success it restores listening and stores the session in
// *current; on failure the transport is left disconnected.
func reconnect(ctx context.Context, current **linx.Session, t *transport, c *voice.Controller, dev linx.Device, tools []linx.Tool, delays []time.Duration) error {
	if *current != nil {
		_ = (*current).Close()
	}
	t.Set(nil)
	for _, delay := range delays {
		log.Printf("Linx disconnected; reconnecting in %s", delay)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		next, err := linx.NewSession(linx.Config{Device: dev, Tools: tools})
		if err != nil {
			log.Printf("reconnect attempt failed: %v", err)
			continue
		}
		t.Set(next)
		if err := c.Reconnected(); err != nil {
			_ = next.Close()
			t.Set(nil)
			return err
		}
		*current = next
		log.Printf("Linx reconnected session=%s", next.SessionID())
		return nil
	}
	return errors.New("all reconnect attempts failed")
}
