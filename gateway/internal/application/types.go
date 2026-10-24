// Package application owns application state and coordinates build, runtime and display services.
package application

import (
	"context"
	"encoding/json"
	"github.com/open-anywhere/open-anywhere/pkg/casting"
	"time"
)

type Owner struct {
	Device  string `json:"device"`
	Agent   string `json:"agent"`
	Session string `json:"session"`
}

func (o Owner) Valid() bool { return o.Device != "" && o.Agent != "" && o.Session != "" }

type Failure struct {
	Stage   string `json:"stage,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}

func (e *Failure) Error() string  { return e.Code + ": " + e.Message }
func fail(code, msg string) error { return &Failure{Code: code, Message: msg} }

type App struct {
	ID                string          `json:"app_id"`
	Owner             Owner           `json:"owner"`
	Prompt            string          `json:"prompt"`
	Players           []string        `json:"player_names,omitempty"`
	Name              string          `json:"name"`
	State             string          `json:"state"`
	Error             *Failure        `json:"error,omitempty"`
	Created           time.Time       `json:"created_at"`
	Started           time.Time       `json:"started_at,omitempty"`
	BuildSeconds      int64           `json:"build_duration_seconds"`
	SessionID         string          `json:"build_session_id,omitempty"`
	Submitted         bool            `json:"submitted"`
	Seen              map[string]bool `json:"seen_events,omitempty"`
	TurnSeen          bool            `json:"turn_seen"`
	Completed         bool            `json:"build_completed"`
	Message           string          `json:"last_agent_message,omitempty"`
	ArtifactURL       string          `json:"artifact_url,omitempty"`
	ArtifactPath      string          `json:"artifact_path,omitempty"`
	Digest            string          `json:"artifact_sha256,omitempty"`
	Document          json.RawMessage `json:"openapi,omitempty"`
	ScreenID          string          `json:"screen_id,omitempty"`
	InstanceID        string          `json:"instance_id,omitempty"`
	ProjectionID      string          `json:"projection_id,omitempty"`
	Port              int             `json:"port,omitempty"`
	BaseURL           string          `json:"app_base_url,omitempty"`
	Busy              bool            `json:"busy,omitempty"`
	UnknownRuntime    bool            `json:"unknown_runtime,omitempty"`
	UnknownProjection bool            `json:"unknown_projection,omitempty"`
}

// Public exposes only the stable external contract, never internal endpoint or artifact credentials.
func (a App) Public() map[string]any {
	r := map[string]any{"app_id": a.ID, "state": a.State}
	switch a.State {
	case "building":
		r["elapsed_seconds"] = int64(time.Since(a.Created).Seconds())
		r["message"] = a.Message
	case "idle":
		r["name"] = a.Name
		r["build_duration_seconds"] = a.BuildSeconds
	case "running":
		r["name"] = a.Name
		r["screen_id"] = a.ScreenID
		r["started_at"] = a.Started
		r["uptime_seconds"] = int64(time.Since(a.Started).Seconds())
	case "error":
		if a.Error != nil {
			failure := map[string]any{"code": a.Error.Code, "stage": a.Error.Stage, "message": a.Error.Message}
			if a.Message != "" {
				failure["last_agent_message"] = a.Message
			}
			r["error"] = failure
		}
		if a.ScreenID != "" {
			r["screen_id"] = a.ScreenID
		}
	}
	return r
}

type BuildRequest struct {
	AppID, Prompt string
	Players       []string
}
type BuildCursor struct {
	Seen      map[string]bool
	TurnSeen  bool
	Completed bool
}
type BuildEvent struct{ ID, Type, Message, StopReason string }
type BuildProduct struct {
	Name, URL, Path, Digest string
	Document                json.RawMessage
}
type Builder interface {
	Create(context.Context, BuildRequest) (string, error)
	Run(context.Context, string, BuildRequest, bool, BuildCursor, func(bool, *BuildEvent) error) (BuildProduct, error)
}
type Instance struct {
	AppID   string               `json:"app_id"`
	ID      string               `json:"instance_id"`
	State   string               `json:"state"`
	Port    int                  `json:"port"`
	BaseURL string               `json:"app_base_url"`
	Source  casting.StreamSource `json:"source"`
	Error   *Failure             `json:"error,omitempty"`
}
type Runtime interface {
	PortFree(context.Context, int) (bool, error)
	Start(context.Context, string, string, int) (Instance, error)
	Get(context.Context, string) (Instance, error)
	Stop(context.Context, string) (Instance, error)
}
type Display interface {
	Targets(context.Context) ([]casting.Target, error)
	Start(context.Context, casting.CreateSessionRequest) (casting.Session, error)
	Get(context.Context, string) (casting.Session, error)
	Stop(context.Context, string) (casting.Session, error)
}
type Screen struct {
	ID         string `json:"screen_id"`
	Name       string `json:"name"`
	TargetID   string `json:"target_id"`
	EndpointID string `json:"endpoint_id"`
}
type Options struct {
	StateFile                      string
	Screens                        []Screen
	PortMin, PortMax               int
	BuildTimeout, OperationTimeout time.Duration
	Builder                        Builder
	Runtime                        Runtime
	Display                        Display
}

// Rejected denotes a documented validation/conflict response before any resource was created.
type Rejected struct {
	Code    string
	Message string
}

func (e *Rejected) Error() string { return e.Code + ": " + e.Message }
