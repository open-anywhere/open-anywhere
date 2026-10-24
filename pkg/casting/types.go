// Package casting defines the public JSON contract shared by casting clients
// and the standalone casting service.
package casting

import "time"

type TargetState string

const (
	TargetOnline  TargetState = "online"
	TargetOffline TargetState = "offline"
)

type SessionState string

const (
	SessionStarting SessionState = "starting"
	SessionPlaying  SessionState = "playing"
	SessionPaused   SessionState = "paused"
	SessionStopped  SessionState = "stopped"
	SessionFailed   SessionState = "failed"
)

type Target struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	State     TargetState `json:"state"`
	Endpoints []Endpoint  `json:"endpoints"`
	// ActiveSessionID identifies this service's current reservation, including
	// sessions that are starting or whose failure cleanup has not succeeded.
	ActiveSessionID string `json:"active_session_id,omitempty"`
}

type Endpoint struct {
	ID           string       `json:"id"`
	Protocol     string       `json:"protocol"`
	Capabilities Capabilities `json:"capabilities"`
}

// Capabilities describes the media profile exposed by an endpoint. Streaming
// means progressive live playback, without a latency guarantee.
type Capabilities struct {
	Formats     []string `json:"formats"`
	VideoCodecs []string `json:"video_codecs"`
	AudioCodecs []string `json:"audio_codecs"`
	Streaming   bool     `json:"streaming"`
}

// StreamSource is served by the caller and fetched directly by the target.
// AudioCodec may be empty for video-only streams.
type StreamSource struct {
	Type       string `json:"type"`
	URL        string `json:"url"`
	Format     string `json:"format"`
	VideoCodec string `json:"video_codec"`
	AudioCodec string `json:"audio_codec"`
}

type CreateSessionRequest struct {
	TargetID   string       `json:"target_id"`
	EndpointID string       `json:"endpoint_id"`
	Source     StreamSource `json:"source"`
}

type Session struct {
	ID         string        `json:"session_id"`
	State      SessionState  `json:"state"`
	TargetID   string        `json:"target_id"`
	EndpointID string        `json:"endpoint_id"`
	Source     StreamSource  `json:"source"`
	Error      *SessionError `json:"error,omitempty"`
	CreatedAt  time.Time     `json:"created_at"`
}

// SessionError describes an error reported while starting a session.
type SessionError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
