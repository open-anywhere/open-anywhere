package service

import (
	"context"
	"strings"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
)

// ListApps finds an authenticated device's applications across its Linx sessions.
// Agent must be present: a missing agent must never widen ownership.
func (s *Service) ListApps(ctx context.Context, owner app.Identity) ([]StatusView, error) {
	if !validIdentity(owner) {
		return nil, &ToolError{Code: "MISSING_CONTEXT", Message: "device, agent and session required"}
	}
	out := []StatusView{}
	for _, a := range s.apps.List() {
		if sameDevice(a.Owner, owner) {
			out = append(out, s.statusView(a))
		}
	}
	return out, nil
}
func validIdentity(o app.Identity) bool {
	return strings.TrimSpace(o.DeviceID) != "" && strings.TrimSpace(o.AgentID) != "" && strings.TrimSpace(o.SessionID) != ""
}
func sameDevice(a, b app.Identity) bool {
	return a.DeviceID == b.DeviceID && a.AgentID != "" && a.AgentID == b.AgentID
}

// ClaimApp explicitly transfers an application to a reconnected session of the same device and agent.
func (s *Service) ClaimApp(ctx context.Context, owner app.Identity, id string) (StatusView, error) {
	if !validIdentity(owner) {
		return StatusView{}, &ToolError{Code: "MISSING_CONTEXT", Message: "device, agent and session required"}
	}
	record, ok := s.apps.Get(id)
	if !ok {
		return StatusView{}, &ToolError{Code: "APP_NOT_FOUND", Message: "应用不存在"}
	}
	record.Begin()
	defer record.End()
	err := record.Do(func(a *app.App) error {
		if !sameDevice(a.Owner, owner) {
			return &ToolError{Code: "APP_NOT_FOUND", Message: "应用不存在"}
		}
		a.Owner = owner
		return nil
	})
	if err != nil {
		return StatusView{}, err
	}
	value := record.Snapshot()
	s.notifier.Notify(owner, app.Notification{AppID: id, State: value.State, Message: "已接管应用，请查询最新状态"})
	return s.statusView(value), nil
}
