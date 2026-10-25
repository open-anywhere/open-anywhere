// Package controlapi serves the local web console and its HTTP, SSE and
// WebSocket control API. It should only listen on a loopback address.
package controlapi

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/coder/websocket"
	"github.com/open-anywhere/open-anywhere/internal/voiceentry/register"
	"github.com/open-anywhere/open-anywhere/internal/voiceentry/voice"
)

//go:embed console.html
var consoleHTML []byte

type Status struct {
	State     voice.State `json:"state"`
	Mode      voice.Mode  `json:"mode"`
	SessionID string      `json:"session_id,omitempty"`
	MAC       string      `json:"mac,omitempty"`
	Connected bool        `json:"connected"`
	Volume    int         `json:"volume"`
	// Registered is false until the device has platform credentials.
	Registered     bool   `json:"registered"`
	ActivationCode string `json:"activation_code,omitempty"`
}
type Server struct {
	controller  *voice.Controller
	statusFn    func() Status
	mu          sync.Mutex
	subscribers map[chan voice.Event]struct{}
	pcmHandler  func([]byte) error
	opusHandler func([]byte) error
	audioSubs   map[chan []byte]struct{}
	sampleRate  int
	register    func(context.Context, register.Request) (register.Result, error)
	disconnect  func() error
	reconnect   func() error
}

func New(controller *voice.Controller, statusFn func() Status) *Server {
	return &Server{controller: controller, statusFn: statusFn, subscribers: make(map[chan voice.Event]struct{}), audioSubs: make(map[chan []byte]struct{}), sampleRate: 16000}
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.console)
	mux.HandleFunc("/status", s.status)
	mux.HandleFunc("/announce", s.announce)
	mux.HandleFunc("/say", s.say)
	mux.HandleFunc("/abort", s.abort)
	mux.HandleFunc("/pause", s.pause)
	mux.HandleFunc("/resume", s.resume)
	mux.HandleFunc("/events", s.events)
	mux.HandleFunc("/audio", s.audio)
	mux.HandleFunc("/playback", s.playback)
	mux.HandleFunc("/device/register", s.registerDevice)
	mux.HandleFunc("/disconnect", s.disconnectLinx)
	mux.HandleFunc("/reconnect", s.reconnectLinx)
	return sameOriginOnly(mux)
}

// sameOriginOnly refuses state-changing requests that a browser marks as
// coming from another origin. Without it any web page the user opens could
// POST to the local port (say, disconnect, ...). Requests without an Origin
// header (curl, scripts, same-origin navigation) are unaffected. WebSocket
// upgrades are checked by the websocket library itself.
func sameOriginOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || u.Host != r.Host {
					http.Error(w, "cross-origin request refused", http.StatusForbidden)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// SetPCMHandler attaches a raw 16-bit mono 16kHz PCM receiver for browser audio.
func (s *Server) SetPCMHandler(handler func([]byte) error) { s.pcmHandler = handler }

// SetRegistration attaches the handler behind POST /device/register.
func (s *Server) SetRegistration(fn func(context.Context, register.Request) (register.Result, error)) {
	s.register = fn
}

// requirePost turns away state-changing endpoints reached with any other
// method. sameOriginOnly cannot help here: a browser request made by an <img>
// tag or a link preview is a GET with no Origin header, and without this check
// it would still pause, abort or speak on the device.
func requirePost(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodPost {
		return true
	}
	w.Header().Set("Allow", http.MethodPost)
	http.Error(w, "POST required", http.StatusMethodNotAllowed)
	return false
}

func (s *Server) registerDevice(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	// Requiring JSON forces a CORS preflight, so other sites cannot trigger this blindly.
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return
	}
	if s.register == nil {
		http.Error(w, "registration is unavailable", http.StatusNotImplemented)
		return
	}
	var in register.Request
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	out, err := s.register(r.Context(), in)
	if err != nil {
		out.Error = err.Error()
		if out.Status == "" {
			out.Status = "error"
		}
		writeJSON(w, http.StatusBadGateway, out)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// SetConnectionControl attaches the handlers behind POST /disconnect and /reconnect.
func (s *Server) SetConnectionControl(disconnect, reconnect func() error) {
	s.disconnect, s.reconnect = disconnect, reconnect
}

func (s *Server) disconnectLinx(w http.ResponseWriter, r *http.Request) {
	s.connectionAction(w, r, s.disconnect, "disconnected")
}

func (s *Server) reconnectLinx(w http.ResponseWriter, r *http.Request) {
	s.connectionAction(w, r, s.reconnect, "connected")
}

func (s *Server) connectionAction(w http.ResponseWriter, r *http.Request, fn func() error, status string) {
	if !requirePost(w, r) {
		return
	}
	if fn == nil {
		http.Error(w, "connection control is unavailable", http.StatusNotImplemented)
		return
	}
	if err := fn(); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": status})
}

// SetOpusHandler attaches a receiver for ready-made Opus packets encoded by the browser.
func (s *Server) SetOpusHandler(handler func([]byte) error) { s.opusHandler = handler }

// SetPlaybackSampleRate sets the sample rate the browser should decode downlink Opus with.
func (s *Server) SetPlaybackSampleRate(rate int) {
	s.mu.Lock()
	s.sampleRate = rate
	s.mu.Unlock()
}

// PublishAudio fans a downlink Opus packet out to connected browser players.
func (s *Server) PublishAudio(packet []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.audioSubs {
		select {
		case ch <- packet:
		default:
		}
	}
}

// playbackConn is the part of a WebSocket connection used to stream downlink
// audio; it is an interface so the write-failure paths can be exercised.
type playbackConn interface {
	CloseRead(context.Context) context.Context
	Write(context.Context, websocket.MessageType, []byte) error
}

func (s *Server) playback(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.Close(websocket.StatusNormalClosure, "playback closed")
	ch := make(chan []byte, 64)
	s.mu.Lock()
	s.audioSubs[ch] = struct{}{}
	rate := s.sampleRate
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.audioSubs, ch); s.mu.Unlock() }()
	s.streamPlayback(r.Context(), c, ch, rate)
}

// streamPlayback sends the negotiated sample rate and then every downlink
// packet until the connection or the request is gone.
func (s *Server) streamPlayback(parent context.Context, c playbackConn, ch <-chan []byte, rate int) {
	ctx := c.CloseRead(parent)
	if err := c.Write(ctx, websocket.MessageText, fmt.Appendf(nil, `{"sample_rate":%d}`, rate)); err != nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case packet := <-ch:
			if err := c.Write(ctx, websocket.MessageBinary, packet); err != nil {
				return
			}
		}
	}
}

func (s *Server) audio(w http.ResponseWriter, r *http.Request) {
	handler := s.pcmHandler
	if r.URL.Query().Get("codec") == "opus" {
		handler = s.opusHandler
	}
	if handler == nil {
		http.Error(w, "audio input is unavailable; start with Opus support", http.StatusNotImplemented)
		return
	}
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.Close(websocket.StatusNormalClosure, "audio input closed")
	for {
		typ, data, err := c.Read(r.Context())
		if err != nil {
			return
		}
		if typ != websocket.MessageBinary {
			continue
		}
		if err := handler(data); err != nil {
			_ = c.Close(websocket.StatusUnsupportedData, err.Error())
			return
		}
	}
}

func (s *Server) console(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(consoleHTML)
}

func (s *Server) pause(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	if err := s.controller.Pause(); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "paused"})
}

func (s *Server) resume(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	if err := s.controller.Resume(); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "listening"})
}
func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.statusFn())
}
func (s *Server) announce(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	var in struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Text == "" {
		http.Error(w, "text is required", http.StatusBadRequest)
		return
	}
	if err := s.controller.Announce(in.Text); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}
func (s *Server) say(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	var in struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Text == "" {
		http.Error(w, "text is required", http.StatusBadRequest)
		return
	}
	if err := s.controller.SendText(in.Text); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "sent"})
}
func (s *Server) abort(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	if err := s.controller.Abort(); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "aborted"})
}
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	f, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", http.StatusInternalServerError)
		return
	}
	ch := make(chan voice.Event, 256)
	s.mu.Lock()
	s.subscribers[ch] = struct{}{}
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.subscribers, ch); close(ch); s.mu.Unlock() }()
	f.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case e := <-ch:
			b, _ := json.Marshal(e)
			fmt.Fprintf(w, "data: %s\n\n", b)
			f.Flush()
		}
	}
}
func (s *Server) Publish(e voice.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subscribers {
		select {
		case ch <- e:
		default:
		}
	}
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
