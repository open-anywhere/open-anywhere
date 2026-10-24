// Package mocktv implements a fake DLNA MediaRenderer for local development.
// The casting service discovers it like a television; when it receives a
// stream URL over AVTransport, a browser page plays that stream.
package mocktv

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// lookupIP is a variable so tests can simulate resolver failures without
// depending on external DNS.
var lookupIP = net.LookupIP

// eventKeepAliveInterval controls the SSE keepalive cadence; tests shorten it.
var eventKeepAliveInterval = 15 * time.Second

// Config configures a Display.
type Config struct {
	Name string
	UDN  string
	Logf func(string, ...any)
}

// State is the current playback state of the simulated screen.
type State struct {
	Name  string `json:"name"`
	URL   string `json:"url,omitempty"`
	State string `json:"state"` // idle, cued, playing
}

// Display owns the DLNA state and the browser page that mirrors it.
type Display struct {
	cfg  Config
	logf func(string, ...any)

	mu      sync.Mutex
	url     string
	playing bool
	clients map[chan State]struct{}

	client *http.Client
}

// NewDisplay creates a simulated screen.
func NewDisplay(cfg Config) *Display {
	if cfg.Name == "" {
		cfg.Name = "Mock TV"
	}
	if cfg.UDN == "" {
		cfg.UDN = "uuid:open-anywhere-mock-tv"
	}
	logf := cfg.Logf
	if logf == nil {
		logf = log.Printf
	}
	return &Display{
		cfg:     cfg,
		logf:    logf,
		clients: make(map[chan State]struct{}),
		client: &http.Client{
			Transport: &http.Transport{
				Proxy:                 nil,
				DisableCompression:    true,
				DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
				ResponseHeaderTimeout: 5 * time.Second,
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 3 {
					return errors.New("too many redirects")
				}
				return checkStreamURL(req.URL)
			},
		},
	}
}

// Handler returns the HTTP surface: the display page, device description,
// AVTransport control, SSE state and the stream proxy.
func (d *Display) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", d.handleIndex)
	mux.HandleFunc("GET /mpegts.js", d.handlePlayerScript)
	mux.HandleFunc("GET /device.xml", d.handleDevice)
	mux.HandleFunc("POST /control/avtransport", d.handleAVTransport)
	mux.HandleFunc("GET /events", d.handleEvents)
	mux.HandleFunc("GET /api/state", d.handleState)
	mux.HandleFunc("POST /api/play", d.handleAPIPlay)
	mux.HandleFunc("POST /api/stop", d.handleAPIStop)
	mux.HandleFunc("GET /stream", d.handleStream)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return mux
}

// State returns the current state.
func (d *Display) State() State {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.stateLocked()
}

func (d *Display) stateLocked() State {
	state := "idle"
	if d.playing {
		state = "playing"
	} else if d.url != "" {
		state = "cued"
	}
	return State{Name: d.cfg.Name, URL: d.url, State: state}
}

// cue records a stream URL without playing it yet.
func (d *Display) cue(raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	d.mu.Lock()
	d.url = raw
	d.playing = false
	state := d.stateLocked()
	d.mu.Unlock()
	d.logf("mock-tv: cued %s", raw)
	d.broadcast(state)
}

// play starts the cued stream.
func (d *Display) play() {
	d.mu.Lock()
	if d.url == "" {
		// Play before SetAVTransportURI has nothing to start.
		d.mu.Unlock()
		return
	}
	d.playing = true
	state := d.stateLocked()
	d.mu.Unlock()
	d.logf("mock-tv: playing %s", state.URL)
	d.broadcast(state)
}

// stop clears the screen.
func (d *Display) stop() {
	d.mu.Lock()
	d.playing = false
	d.url = ""
	state := d.stateLocked()
	d.mu.Unlock()
	d.logf("mock-tv: stopped")
	d.broadcast(state)
}

// transportState is the UPnP AVTransport state returned to the casting
// service.
func (d *Display) transportState() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.playing {
		return "PLAYING"
	}
	return "STOPPED"
}

func (d *Display) broadcast(state State) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for ch := range d.clients {
		select {
		case ch <- state:
		default:
		}
	}
}

func (d *Display) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(indexHTML)
}

func (d *Display) handlePlayerScript(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(playerJS)
}

func (d *Display) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch := make(chan State, 8)
	d.mu.Lock()
	current := d.stateLocked()
	d.clients[ch] = struct{}{}
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		delete(d.clients, ch)
		d.mu.Unlock()
	}()

	if !writeState(w, flusher, current) {
		return
	}
	ticker := time.NewTicker(eventKeepAliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case state := <-ch:
			if !writeState(w, flusher, state) {
				return
			}
		}
	}
}

func writeState(w io.Writer, flusher http.Flusher, state State) bool {
	data, err := json.Marshal(state)
	if err != nil {
		return false
	}
	if _, err := fmt.Fprintf(w, "event: state\ndata: %s\n\n", data); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

func (d *Display) handleState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, d.State())
}

func (d *Display) handleAPIPlay(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	target, err := url.Parse(strings.TrimSpace(body.URL))
	if err != nil || checkStreamURL(target) != nil {
		http.Error(w, "url must be a local HTTP(S) stream", http.StatusBadRequest)
		return
	}
	d.cue(target.String())
	d.play()
	writeJSON(w, http.StatusOK, d.State())
}

func (d *Display) handleAPIStop(w http.ResponseWriter, r *http.Request) {
	d.stop()
	writeJSON(w, http.StatusOK, d.State())
}

// handleStream proxies the MPEG-TS stream through mock-tv so the browser page
// can read it from the same origin. Only local/private hosts are allowed.
func (d *Display) handleStream(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.URL.Query().Get("url"))
	target, err := url.Parse(raw)
	if err != nil || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") {
		http.Error(w, "invalid stream url", http.StatusBadRequest)
		return
	}
	if err := checkStreamURL(target); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target.String(), nil)
	if err != nil {
		http.Error(w, "invalid stream url", http.StatusBadRequest)
		return
	}
	resp, err := d.client.Do(req)
	if err != nil {
		http.Error(w, "stream unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		http.Error(w, "stream unavailable", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 64*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}

// checkStreamURL keeps the local proxy from becoming an open internet proxy.
func checkStreamURL(target *url.URL) error {
	if target == nil {
		return errors.New("missing url")
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return errors.New("url must be http or https")
	}
	host := target.Hostname()
	if host == "" {
		return errors.New("url has no host")
	}
	addresses, err := lookupIP(host)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", host, err)
	}
	if len(addresses) == 0 {
		return fmt.Errorf("resolve %s: no addresses", host)
	}
	for _, ip := range addresses {
		if !isLocalIP(ip) {
			return fmt.Errorf("%s is not a local address", host)
		}
	}
	return nil
}

func isLocalIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
		return true
	}
	// 100.64.0.0/10 is shared address space, used by some VPN overlays.
	if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
		return true
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
