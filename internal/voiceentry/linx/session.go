// Package linx speaks the Linx device protocol: credentials, OTA
// activation, the WebSocket session and the device-side MCP server.
package linx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	DefaultSampleRate = 16000
	DefaultFrameMS    = 60
)

type AudioParams struct {
	Format             string `json:"format"`
	SampleRate         int    `json:"sample_rate"`
	Channels           int    `json:"channels"`
	FrameDuration      int    `json:"frame_duration"`
	PlayBufferDuration int    `json:"play_buffer_duration"`
}

type Event struct {
	Type  string
	Text  string
	Data  map[string]any
	Audio []byte
	Raw   []byte
}

// Tool is a device-side MCP tool the Linx platform may list and call.
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]any
	// Call runs the tool; its text is returned to Linx, an error becomes isError.
	Call func(args map[string]any) (string, error)
}

type Session struct {
	tools       []Tool
	conn        *websocket.Conn
	mu          sync.Mutex
	sessionID   string
	agentID     string
	audioParams AudioParams
	events      chan Event
	done        chan struct{}
	closeOnce   sync.Once
}

type Config struct {
	Device              Device
	HTTPClient          *http.Client
	HelloTimeoutSeconds int
	EventsBuffer        int
	Tools               []Tool
}

func NewSession(cfg Config) (*Session, error) {
	if cfg.Device.WSURL == "" {
		return nil, errors.New("missing websocket URL")
	}
	url := cfg.Device.WSURL
	ctx := context.Background()
	if cfg.HelloTimeoutSeconds <= 0 {
		cfg.HelloTimeoutSeconds = 10
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+cfg.Device.Token)
	header.Set("Device-Id", cfg.Device.MAC)
	header.Set("Client-Id", cfg.Device.ClientID)
	header.Set("Protocol-Version", "1")
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPClient: cfg.HTTPClient, HTTPHeader: header})
	if err != nil {
		return nil, fmt.Errorf("websocket dial: %w", err)
	}
	s := &Session{conn: conn, events: make(chan Event, max(16, cfg.EventsBuffer)), done: make(chan struct{}), tools: cfg.Tools}
	if err := s.write(map[string]any{"type": "hello", "version": 1, "transport": "websocket", "features": map[string]any{"mcp": true}, "audio_params": AudioParams{Format: "opus", SampleRate: DefaultSampleRate, Channels: 1, FrameDuration: DefaultFrameMS, PlayBufferDuration: 1000}}); err != nil {
		conn.Close(websocket.StatusInternalError, "hello failed")
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.HelloTimeoutSeconds)*time.Second)
	defer cancel()
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			conn.Close(websocket.StatusNormalClosure, "hello timeout")
			return nil, fmt.Errorf("read hello: %w", err)
		}
		if typ != websocket.MessageText {
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("invalid hello: %w", err)
		}
		if msg["type"] != "hello" {
			s.dispatch(msg, data)
			continue
		}
		s.sessionID, _ = msg["session_id"].(string)
		if raw, ok := msg["audio_params"].(map[string]any); ok {
			s.audioParams = parseAudioParams(raw)
		}
		if raw, ok := msg["agent_id"].(string); ok {
			s.agentID = raw
		}
		if s.audioParams.SampleRate == 0 {
			s.audioParams = AudioParams{Format: "opus", SampleRate: DefaultSampleRate, Channels: 1, FrameDuration: DefaultFrameMS, PlayBufferDuration: 1000}
		}
		go s.readLoop()
		return s, nil
	}
}

// AudioParams returns the negotiated server audio settings.
func (s *Session) AudioParams() AudioParams { return s.audioParams }
func (s *Session) SessionID() string        { return s.sessionID }

// AgentID is the agent the server reports for this session, empty when the
// server does not name one.
func (s *Session) AgentID() string { return s.agentID }

func (s *Session) Events() <-chan Event     { return s.events }

func (s *Session) readLoop() {
	defer close(s.done)
	defer close(s.events)
	for {
		typ, data, err := s.conn.Read(context.Background())
		if err != nil {
			s.emit(Event{Type: "close", Data: map[string]any{"error": err.Error()}})
			return
		}
		if typ == websocket.MessageBinary {
			s.emit(Event{Type: "audio", Audio: append([]byte(nil), data...)})
			continue
		}
		var msg map[string]any
		if json.Unmarshal(data, &msg) == nil {
			s.dispatch(msg, data)
		} else {
			s.emit(Event{Type: "message", Raw: append([]byte(nil), data...)})
		}
	}
}

func (s *Session) dispatch(msg map[string]any, raw []byte) {
	t, _ := msg["type"].(string)
	if t == "mcp" {
		s.answerMCP(msg, raw)
		return
	}
	e := Event{Type: t, Raw: redactTokens(raw), Data: msg}
	if t == "stt" || t == "llm" {
		e.Text, _ = msg["text"].(string)
	}
	if t == "tts" {
		e.Text, _ = msg["text"].(string)
	}
	s.emit(e)
}

func (s *Session) answerMCP(msg map[string]any, raw []byte) {
	p, _ := msg["payload"].(map[string]any)
	method, _ := p["method"].(string)
	id := p["id"]
	params, _ := p["params"].(map[string]any)
	if method != "" {
		text := method
		if name, _ := params["name"].(string); method == "tools/call" && name != "" {
			text += " " + name
		}
		s.emit(Event{Type: "mcp", Text: text, Data: p, Raw: redactTokens(raw)})
	}
	if method == "" || id == nil {
		return
	}
	var result any = map[string]any{}
	switch method {
	case "initialize":
		result = map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "open-anywhere-client", "version": "0.1.0"}}
	case "tools/list":
		tools := []any{}
		for _, t := range s.tools {
			schema := t.InputSchema
			if schema == nil {
				schema = map[string]any{"type": "object", "properties": map[string]any{}, "required": []any{}}
			}
			tools = append(tools, map[string]any{"name": t.Name, "description": t.Description, "type": 0, "inputSchema": schema})
		}
		result = map[string]any{"tools": tools, "nextCursor": nil}
	case "tools/call":
		name, _ := params["name"].(string)
		args, _ := params["arguments"].(map[string]any)
		called := s.callTool(name, args)
		if called == nil {
			_ = s.write(map[string]any{"type": "mcp", "payload": map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32601, "message": "未知工具: " + name}}})
			return
		}
		result = called
	}
	_ = s.write(map[string]any{"type": "mcp", "payload": map[string]any{"jsonrpc": "2.0", "id": id, "result": result}})
}

// callTool runs a registered tool; nil means the tool is unknown.
func (s *Session) callTool(name string, args map[string]any) map[string]any {
	for _, t := range s.tools {
		if t.Name != name {
			continue
		}
		text, err := t.Call(args)
		if err != nil {
			text = err.Error()
		}
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}, "isError": err != nil}
	}
	return nil
}

// redactTokens masks every "token" value in a raw message; MCP initialize
// carries service tokens that must not reach the web console.
func redactTokens(raw []byte) []byte {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	var walk func(any)
	walk = func(n any) {
		switch x := n.(type) {
		case map[string]any:
			for k, c := range x {
				if k == "token" {
					x[k] = "***"
				} else {
					walk(c)
				}
			}
		case []any:
			for _, c := range x {
				walk(c)
			}
		}
	}
	walk(v)
	b, _ := json.Marshal(v)
	return b
}

func (s *Session) emit(e Event) {
	select {
	case s.events <- e:
	default:
	}
}
func (s *Session) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn.Write(context.Background(), websocket.MessageText, b)
}
func (s *Session) SendAudio(packet []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn.Write(context.Background(), websocket.MessageBinary, packet)
}
func (s *Session) ListenStart(mode string) error {
	if mode == "" {
		mode = "realtime"
	}
	return s.write(map[string]any{"type": "listen", "state": "start", "mode": mode})
}
func (s *Session) ListenStop() error {
	return s.write(map[string]any{"type": "listen", "state": "stop"})
}
func (s *Session) SendText(text string) error {
	return s.write(map[string]any{"type": "listen", "state": "detect", "text": text})
}
func (s *Session) Announce(text string) error {
	return s.write(map[string]any{"type": "listen", "state": "detect", "text": "主动播报", "text_response": text})
}
func (s *Session) Abort(reason string) error {
	if reason == "" {
		reason = "user_interrupt"
	}
	return s.write(map[string]any{"type": "abort", "reason": reason})
}
func (s *Session) Close() error {
	s.closeOnce.Do(func() { _ = s.conn.Close(websocket.StatusNormalClosure, "client closed"); <-s.done })
	return nil
}

func parseAudioParams(m map[string]any) AudioParams {
	var p AudioParams
	b, _ := json.Marshal(m)
	_ = json.Unmarshal(b, &p)
	return p
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
