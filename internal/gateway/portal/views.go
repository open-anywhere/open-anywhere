package portal

import (
	"context"
	"crypto/rand"
	"sort"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/service"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

// DeviceView is the portal-facing device JSON. It never contains a token or
// the credential file path.
type DeviceView struct {
	ID           string     `json:"id"`
	Kind         string     `json:"kind"`
	MAC          string     `json:"mac"`
	ClientID     string     `json:"client_id"`
	AgentID      string     `json:"agent_id"`
	AgentName    string     `json:"agent_name"`
	Name         string     `json:"name"`
	State        string     `json:"state"`
	Step         string     `json:"step"`
	Error        string     `json:"error,omitempty"`
	ErrorCode    string     `json:"error_code,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	RegisteredAt *time.Time `json:"registered_at,omitempty"`
	Voice        VoiceState `json:"voice"`
}

// AppView is one row of the portal's application list.
type AppView struct {
	AppID                string     `json:"app_id"`
	DeviceID             string     `json:"device_id"`
	Name                 string     `json:"name,omitempty"`
	Title                string     `json:"title,omitempty"`
	Summary              string     `json:"summary,omitempty"`
	State                app.State  `json:"state"`
	Progress             string     `json:"progress,omitempty"`
	BuildStartedAt       *time.Time `json:"build_started_at,omitempty"`
	BuiltAt              *time.Time `json:"built_at,omitempty"`
	BuildDurationSeconds int64      `json:"build_duration_seconds,omitempty"`
	OperationCount       int        `json:"operation_count"`
	StartedAt            *time.Time `json:"started_at,omitempty"`
	StoppedAt            *time.Time `json:"stopped_at,omitempty"`
	Error                *app.Error `json:"error,omitempty"`
}

// AppDetailView adds status, operations and OpenAPI availability.
type AppDetailView struct {
	AppView
	Message          string                  `json:"message,omitempty"`
	UptimeSeconds    int64                   `json:"uptime_seconds,omitempty"`
	Operations       []service.OperationView `json:"operations"`
	OpenAPIAvailable bool                    `json:"openapi_available"`
}

type ownedIdentity struct {
	DeviceID string
	Identity app.Identity
}

// ownedIdentities returns the (MAC, agent) pairs whose applications the user
// may see. Devices that have not finished Linx provisioning are skipped.
func (a *API) ownedIdentities(ctx context.Context, userID string) ([]ownedIdentity, error) {
	devices, err := a.db.PortalDevicesByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	identities := make([]ownedIdentity, 0, len(devices))
	for _, device := range devices {
		if device.LinxAgentID == "" {
			continue
		}
		identities = append(identities, ownedIdentity{
			DeviceID: device.ID,
			Identity: app.Identity{DeviceID: device.MAC, AgentID: device.LinxAgentID},
		})
	}
	return identities, nil
}

func ownerIdentities(items []ownedIdentity) []app.Identity {
	owners := make([]app.Identity, len(items))
	for index, item := range items {
		owners[index] = item.Identity
	}
	return owners
}

// appItems merges every application owned by the user's devices and agents.
// Ownership uses (MAC, agent id), exactly like the Linx MCP side, so the
// portal cannot show an application Linx would not.
func (a *API) appItems(ctx context.Context, user store.PortalUser) ([]ownedAppItem, error) {
	identities, err := a.ownedIdentities(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	var items []ownedAppItem
	for _, owned := range identities {
		summaries, err := a.apps.ListApps(ctx, owned.Identity)
		if err != nil {
			return nil, err
		}
		for _, summary := range summaries {
			items = append(items, ownedAppItem{
				AppView: AppView{
					AppID:                summary.AppID,
					DeviceID:             owned.DeviceID,
					Name:                 summary.Name,
					Title:                summary.Title,
					Summary:              summary.Summary,
					State:                summary.State,
					Progress:             summary.Progress,
					BuildStartedAt:       summary.BuildStartedAt,
					BuiltAt:              summary.BuiltAt,
					BuildDurationSeconds: summary.BuildDurationSeconds,
					OperationCount:       summary.OperationCount,
					StartedAt:            summary.StartedAt,
					StoppedAt:            summary.StoppedAt,
					Error:                summary.Error,
				},
				Identity: owned.Identity,
			})
		}
	}
	now := time.Now()
	sort.SliceStable(items, func(i, j int) bool {
		left, right := now, now
		if items[i].BuiltAt != nil {
			left = *items[i].BuiltAt
		}
		if items[j].BuiltAt != nil {
			right = *items[j].BuiltAt
		}
		if !left.Equal(right) {
			return left.After(right)
		}
		return items[i].AppID < items[j].AppID
	})
	if items == nil {
		items = []ownedAppItem{}
	}
	return items, nil
}

func marshalYAML(value map[string]any) ([]byte, error) {
	return yaml.Marshal(value)
}

func randText() string { return rand.Text() }

func internalError(message string) *APIError {
	return apiError(500, "INTERNAL", message)
}
