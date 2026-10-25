package service

import (
	"context"
	"errors"
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
	AppID   string    `json:"app_id"`
	Name    string    `json:"name,omitempty"`
	Title   string    `json:"title,omitempty"`
	Summary string    `json:"summary,omitempty"`
	State   app.State `json:"state"`
	// Progress is the latest build progress text while State is building.
	Progress             string     `json:"progress,omitempty"`
	Startable            bool       `json:"startable"`
	NotStartableReason   string     `json:"not_startable_reason,omitempty"`
	BuildStartedAt       *time.Time `json:"build_started_at,omitempty"`
	BuiltAt              *time.Time `json:"built_at,omitempty"`
	BuildDurationSeconds int64      `json:"build_duration_seconds,omitempty"`
	OperationCount       int        `json:"operation_count"`
	Operations           []string   `json:"operations,omitempty"`
	ScreenID             string     `json:"screen_id,omitempty"`
	StartedAt            *time.Time `json:"started_at,omitempty"`
	StoppedAt            *time.Time `json:"stopped_at,omitempty"`
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

// OwnedAppSummary is an application summary with its owner, for the admin
// console. Ownership is included so callers can map it to a portal user.
type OwnedAppSummary struct {
	AppSummary
	Owner app.Identity `json:"owner"`
}

// ListAllApps returns every application on the control plane, newest first.
// The admin handler reduces it to a view that intentionally omits screen and
// prompt fields.
func (s *Service) ListAllApps(_ context.Context) []OwnedAppSummary {
	values := s.apps.List()
	sortNewestFirst(values)
	out := make([]OwnedAppSummary, 0, len(values))
	for _, value := range values {
		out = append(out, OwnedAppSummary{AppSummary: s.summarize(value), Owner: value.Owner})
	}
	return out
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
	if !a.BuildStartedAt.IsZero() {
		started := a.BuildStartedAt
		sum.BuildStartedAt = &started
	}
	if !a.BuildFinishedAt.IsZero() {
		built := a.BuildFinishedAt
		sum.BuiltAt = &built
		sum.BuildDurationSeconds = int64(a.BuildDuration.Seconds())
	}
	if !a.StartedAt.IsZero() {
		started := a.StartedAt
		sum.StartedAt = &started
	}
	if !a.StoppedAt.IsZero() {
		stopped := a.StoppedAt
		sum.StoppedAt = &stopped
	}
	if a.State == app.StateBuilding {
		sum.Progress = a.BuildingMessage()
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

// PruneHistory removes the oldest applications beyond HistoryLimit in each
// (device, agent) group. Applications that are building, running or still
// holding downstream resources are never removed. A group with a start or stop
// in progress is skipped.
func (s *Service) PruneHistory() {
	s.pruneMu.Lock()
	defer s.pruneMu.Unlock()
	groups := make(map[string][]app.App)
	for _, a := range s.apps.List() {
		key := ownerKey(a.Owner)
		groups[key] = append(groups[key], a)
	}
	for key, all := range groups {
		unlock, ok := s.lifecycle.TryLock(key)
		if !ok {
			continue
		}
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
			s.logf("prune: removed %s (%s) beyond the newest %d applications for %s/%s",
				a.ID, a.State, HistoryLimit, a.Owner.DeviceID, a.Owner.AgentID)
		}
		unlock()
	}
}

// DeleteApp removes an idle or failed application and its package. Building,
// running and applications still holding downstream resources are refused.
func (s *Service) DeleteApp(ctx context.Context, owner app.Identity, appID string) error {
	unlock := s.lifecycle.Lock(ownerKey(owner))
	defer unlock()
	entry, err := s.entry(owner, appID)
	if err != nil {
		return err
	}
	entry.Begin()
	defer entry.End()
	value := entry.Snapshot()
	if !value.Owner.SameOwner(owner) {
		return &ToolError{Code: "APP_NOT_FOUND", Message: "应用不存在"}
	}
	if !removable(value) {
		return &ToolError{Code: "APP_BUSY", Message: "应用正在构建或屏幕上运行，暂时不能删除"}
	}
	s.apps.Remove(value.ID)
	if s.artifacts != nil {
		if err := s.artifacts.Remove(value.ID); err != nil {
			// The record is already gone; a leftover package is harmless and
			// must not resurrect the application.
			s.logf("delete %s: remove package: %v", value.ID, err)
		}
	}
	s.logf("delete: removed %s (%s)", value.ID, value.State)
	return nil
}

func removable(a app.App) bool {
	if a.State != app.StateIdle && a.State != app.StateError {
		return false
	}
	return !a.CleanupPending && !a.PendingRuntime && !a.PendingProjection && a.InstanceID == "" && a.ProjectionSessionID == ""
}

// releaseFailedStart frees what a failed start left behind and returns the
// application to idle so it can be started again.
//
// A projection request that timed out has an unknown outcome, which reconcile
// keeps as an occupation. When Casting reports no active session on the target
// there is nothing to stop and nothing that is not ours to touch, so the
// unknown outcome is dropped; a session that does exist is left to reconcile,
// which only ever stops one it can prove is ours.
func (s *Service) releaseFailedStart(ctx context.Context, entry *app.Record) error {
	rt, err := s.runtimeFor(ctx, entry.Snapshot().Owner)
	if err != nil {
		return err
	}
	return s.releaseFailedStartWith(ctx, rt, entry)
}

func (s *Service) releaseFailedStartWith(ctx context.Context, rt runtime, entry *app.Record) error {
	value := entry.Snapshot()
	if value.PendingProjection && value.TargetID != "" {
		probe, cancel := context.WithTimeout(ctx, 10*time.Second)
		target, err := rt.casting.GetTarget(probe, value.TargetID)
		cancel()
		if err == nil && target.ActiveSessionID == "" {
			if err := persistRuntime(entry, func(a *app.App) { a.PendingProjection = false }); err != nil {
				return err
			}
		}
	}
	if err := s.reconcileWith(ctx, rt, entry); err != nil {
		var toolErr *ToolError
		if errors.As(err, &toolErr) {
			return err
		}
		return &ToolError{Code: "CLEANUP_PENDING", Message: "上次启动遗留的资源还没清理干净，请稍后再试"}
	}
	return persistRuntime(entry, func(a *app.App) {
		if a.State == app.StateError {
			a.State = app.StateIdle
			a.Error = nil
		}
	})
}

// hasActiveResources reports whether an application still owns downstream
// resources or is about to. Changing a service binding while this is true
// would leave those resources at an address nobody remembers.
func hasActiveResources(value app.App) bool {
	return value.State == app.StateRunning || value.OccupiesScreen() ||
		value.PendingRuntime || value.PendingProjection || value.CleanupPending ||
		value.InstanceID != "" || value.ProjectionSessionID != ""
}

// HasActiveRuntime reports whether any of owners has an application that still
// holds a runtime or projection. Portal handlers use it to refuse a service
// binding change before the old endpoints are lost.
func (s *Service) HasActiveRuntime(owners []app.Identity) bool {
	for _, value := range s.apps.List() {
		if ownedByAny(value.Owner, owners) && hasActiveResources(value) {
			return true
		}
	}
	return false
}

// HasAnyActiveResources reports whether any application on the control plane
// still holds a runtime or projection. It protects a change of the platform
// default services, which managed users rely on for cleanup.
func (s *Service) HasAnyActiveResources() bool {
	for _, value := range s.apps.List() {
		if hasActiveResources(value) {
			return true
		}
	}
	return false
}

// StopAllForOwner stops every application of owners that still holds
// resources. It is best effort: errors are collected so disabling a user never
// blocks on an unreachable downstream service.
func (s *Service) StopAllForOwner(ctx context.Context, owners []app.Identity) []error {
	var failures []error
	for _, value := range s.apps.List() {
		if !ownedByAny(value.Owner, owners) || !hasActiveResources(value) {
			continue
		}
		if _, err := s.StopApp(ctx, value.Owner, value.ID); err != nil {
			failures = append(failures, err)
		}
	}
	return failures
}
