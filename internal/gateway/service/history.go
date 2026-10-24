package service

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
)

// HistoryLimit is how many applications the control plane keeps. Older ones
// that are not building or running are removed together with their packages.
const HistoryLimit = 10

// ArtifactStore is the part of the artifact store the service needs to judge
// whether a built application can still be started and to clean up old ones.
type ArtifactStore interface {
	Has(appID string) bool
	Remove(appID string) error
}

// SetArtifactStore tells the service where application packages live.
func (s *Service) SetArtifactStore(store ArtifactStore) { s.artifacts = store }

// AppSummary describes one application of a device and agent well enough for
// a caller to pick one from the history and start it again.
type AppSummary struct {
	AppID                string     `json:"app_id"`
	Name                 string     `json:"name,omitempty"`
	Title                string     `json:"title,omitempty"`
	Summary              string     `json:"summary,omitempty"`
	State                app.State  `json:"state"`
	Startable            bool       `json:"startable"`
	NotStartableReason   string     `json:"not_startable_reason,omitempty"`
	BuiltAt              *time.Time `json:"built_at,omitempty"`
	BuildDurationSeconds int64      `json:"build_duration_seconds,omitempty"`
	OperationCount       int        `json:"operation_count"`
	Operations           []string   `json:"operations,omitempty"`
	ScreenID             string     `json:"screen_id,omitempty"`
	Error                *app.Error `json:"error,omitempty"`
}

// ListApps returns the applications of the caller's device and agent, newest
// first, whichever Linx session built them.
func (s *Service) ListApps(_ context.Context, owner app.Identity) ([]AppSummary, error) {
	if strings.TrimSpace(owner.DeviceID) == "" {
		return nil, &ToolError{Code: "MISSING_CONTEXT", Message: "缺少设备标识"}
	}
	mine := []app.App{}
	for _, a := range s.apps.List() {
		if a.Owner.SameOwner(owner) {
			mine = append(mine, a)
		}
	}
	sortNewestFirst(mine)
	out := make([]AppSummary, 0, len(mine))
	for _, a := range mine {
		out = append(out, s.summarize(a))
	}
	return out, nil
}

func sortNewestFirst(apps []app.App) {
	sort.SliceStable(apps, func(i, j int) bool {
		if !apps[i].BuildStartedAt.Equal(apps[j].BuildStartedAt) {
			return apps[i].BuildStartedAt.After(apps[j].BuildStartedAt)
		}
		return apps[i].ID < apps[j].ID
	})
}

func (s *Service) summarize(a app.App) AppSummary {
	sum := AppSummary{
		AppID:    a.ID,
		Name:     a.Name,
		Summary:  truncateRunes(strings.Join(strings.Fields(a.Prompt), " "), 80),
		State:    a.State,
		ScreenID: a.ScreenID,
		Error:    a.Error,
	}
	if !a.BuildFinishedAt.IsZero() {
		built := a.BuildFinishedAt
		sum.BuiltAt = &built
		sum.BuildDurationSeconds = int64(a.BuildDuration.Seconds())
	}
	if a.OpenAPI != nil {
		for _, operation := range a.OpenAPI.Operations() {
			sum.Operations = append(sum.Operations, operation.OperationID)
		}
		sum.OperationCount = len(sum.Operations)
		if document, err := a.OpenAPI.Document(); err == nil {
			if info, ok := document["info"].(map[string]any); ok {
				sum.Title, _ = info["title"].(string)
			}
		}
	}
	sum.Startable, sum.NotStartableReason = s.startable(a)
	return sum
}

// startable says whether start-app would accept the application, and if not why.
func (s *Service) startable(a app.App) (bool, string) {
	switch a.State {
	case app.StateBuilding:
		return false, "仍在构建"
	case app.StateRunning:
		return false, "已在屏幕上运行"
	case app.StateError:
		if !retryable(a) {
			return false, "构建失败，需要重新创建"
		}
	case app.StateIdle:
	default:
		return false, "状态不允许启动"
	}
	if s.artifacts != nil && !s.artifacts.Has(a.ID) {
		return false, "应用包已丢失，需要重新创建"
	}
	return true, ""
}

// retryable reports whether an application in error was built successfully and
// failed later, so starting it again is sensible.
func retryable(a app.App) bool {
	return a.State == app.StateError && a.Error != nil && a.Error.Stage != app.StageBuilding && a.OpenAPI != nil && a.Artifact.Path != ""
}

func truncateRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}

// PruneHistory removes the oldest applications beyond HistoryLimit. Applications
// that are building, running or still holding downstream resources are never
// removed. It does nothing while a start or stop is in progress.
func (s *Service) PruneHistory() {
	if !s.lifecycle.TryLock() {
		return
	}
	defer s.lifecycle.Unlock()
	all := s.apps.List()
	sortNewestFirst(all)
	for index, a := range all {
		if index < HistoryLimit || !removable(a) {
			continue
		}
		s.apps.Remove(a.ID)
		if s.artifacts != nil {
			if err := s.artifacts.Remove(a.ID); err != nil {
				s.logf("prune %s: remove package: %v", a.ID, err)
			}
		}
		s.logf("prune: removed %s (%s) beyond the newest %d applications", a.ID, a.State, HistoryLimit)
	}
}

func removable(a app.App) bool {
	if a.State != app.StateIdle && a.State != app.StateError {
		return false
	}
	return !a.CleanupPending && !a.PendingRuntime && !a.PendingProjection && a.InstanceID == "" && a.ProjectionSessionID == ""
}
