// Package voice is the conversation state machine: it tracks listening,
// thinking and speaking, queues proactive announcements while Linx is
// talking, and restores listening after interrupts and reconnects.
package voice

import (
	"encoding/json"
	"errors"
	"sync"
)

type State string

const (
	Connecting   State = "connecting"
	Listening    State = "listening"
	Thinking     State = "thinking"
	Speaking     State = "speaking"
	Idle         State = "idle"
	Reconnecting State = "reconnecting"
	Disconnected State = "disconnected"
)

type Mode string

const (
	Auto     Mode = "auto"
	Realtime Mode = "realtime"
	Manual   Mode = "manual"
)

type Event struct {
	Type    string `json:"type"`
	State   string `json:"state,omitempty"`
	Text    string `json:"text,omitempty"`
	Aborted bool   `json:"is_aborted,omitempty"`
	// Raw is the original Linx message, kept for the console; empty for audio.
	Raw json.RawMessage `json:"raw,omitempty"`
}

type Transport interface {
	ListenStart(mode string) error
	ListenStop() error
	SendText(text string) error
	Announce(text string) error
	Abort(reason string) error
}

type Controller struct {
	mu         sync.Mutex
	transport  Transport
	state      State
	mode       Mode
	queue      []string
	announcing bool
}

func NewController(t Transport, mode Mode) *Controller {
	if mode == "" {
		mode = Realtime
	}
	return &Controller{transport: t, state: Connecting, mode: mode}
}

func (c *Controller) State() State     { c.mu.Lock(); defer c.mu.Unlock(); return c.state }
func (c *Controller) Mode() Mode       { c.mu.Lock(); defer c.mu.Unlock(); return c.mode }
func (c *Controller) QueueLength() int { c.mu.Lock(); defer c.mu.Unlock(); return len(c.queue) }

func (c *Controller) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.transport == nil {
		return errors.New("voice transport is nil")
	}
	if err := c.transport.ListenStart(string(c.mode)); err != nil {
		return err
	}
	c.state = Listening
	return nil
}

func (c *Controller) SendText(text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.transport.SendText(text); err != nil {
		return err
	}
	c.state = Thinking
	return nil
}

func (c *Controller) Announce(text string) error {
	if text == "" {
		return errors.New("announcement is empty")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == Speaking || c.announcing {
		c.queue = append(c.queue, text)
		return nil
	}
	if err := c.transport.Announce(text); err != nil {
		return err
	}
	c.announcing = true
	c.state = Speaking
	return nil
}

func (c *Controller) Handle(e Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch e.Type {
	case "stt":
		if c.announcing {
			return nil
		}
		c.state = Thinking
	case "tts":
		switch e.State {
		case "start", "sentence_start":
			c.state = Speaking
		case "stop":
			if e.Aborted {
				c.announcing = false
				c.state = Listening
				return c.listenLocked()
			}
			c.announcing = false
			if len(c.queue) > 0 {
				next := c.queue[0]
				c.queue = c.queue[1:]
				if err := c.transport.Announce(next); err != nil {
					return err
				}
				c.announcing = true
				c.state = Speaking
				return nil
			}
			c.state = Listening
			return c.listenLocked()
		}
	case "close":
		c.state = Reconnecting
	case "idle":
		c.state = Idle
	}
	return nil
}

func (c *Controller) listenLocked() error { return c.transport.ListenStart(string(c.mode)) }

func (c *Controller) Abort() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.transport.Abort("user_interrupt"); err != nil {
		return err
	}
	c.queue = nil
	c.announcing = false
	c.state = Listening
	return nil
}

func (c *Controller) Pause() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.transport.ListenStop(); err != nil {
		return err
	}
	c.state = Idle
	return nil
}

func (c *Controller) Resume() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.transport.ListenStart(string(c.mode)); err != nil {
		return err
	}
	c.state = Listening
	return nil
}

// Disconnected records a user-requested disconnect: queued announcements are
// dropped and nothing is sent until Reconnected is called.
func (c *Controller) Disconnected() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queue = nil
	c.announcing = false
	c.state = Disconnected
}

// Reconnected restores listening after a resident session creates a new
// transport. A user-paused controller stays paused across reconnects.
func (c *Controller) Reconnected() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == Idle {
		return nil
	}
	if err := c.transport.ListenStart(string(c.mode)); err != nil {
		return err
	}
	c.state = Listening
	return nil
}
