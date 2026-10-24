package service

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/render"
	"github.com/open-anywhere/open-anywhere/pkg/casting"
)

func persistRuntime(record *app.Record, fn func(*app.App)) error {
	if err := record.Do(func(a *app.App) error { fn(a); return nil }); err != nil {
		return &ToolError{Code: "PERSISTENCE_FAILED", Message: "无法保存运行状态，停止后续操作"}
	}
	return nil
}

func (s *Service) updateRuntime(id string, fn func(*app.App)) error {
	if r, ok := s.apps.Get(id); ok {
		return persistRuntime(r, fn)
	}
	return &ToolError{Code: "APP_NOT_FOUND", Message: "应用不存在"}
}

// reconcile identifies unknown outcomes using read-only downstream records before
// cleanup. It never repeats a POST and never deletes a foreign app/target session.
func (s *Service) reconcile(parent context.Context, record *app.Record) error {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	a := record.Snapshot()
	if a.PendingRuntime {
		ports, err := s.render.ListPorts(ctx)
		if err != nil {
			return err
		}
		found := ""
		for _, p := range ports {
			if p.AppID == a.ID && p.InstanceID != "" {
				if found != "" && found != p.InstanceID {
					return fmt.Errorf("multiple Render instances require operator review")
				}
				found = p.InstanceID
			}
		}
		if found == "" {
			// ListPorts succeeded and knows no instance for this app, so there is
			// no runtime to adopt or clean up. Keeping the reservation would
			// leave the application and its screen occupied forever.
			if err := persistRuntime(record, func(v *app.App) { v.PendingRuntime = false }); err != nil {
				return err
			}
			a = record.Snapshot()
		} else {
			instance, err := s.render.Get(ctx, found)
			if err != nil {
				return err
			}
			if instance.AppID != a.ID {
				return fmt.Errorf("foreign Render instance")
			}
			if err := persistRuntime(record, func(v *app.App) {
				v.InstanceID = found
				v.PendingRuntime = false
				v.Source = instance.Source
				v.AppBaseURL = instance.AppBaseURL
			}); err != nil {
				return err
			}
			a = record.Snapshot()
		}
	}
	if a.PendingProjection {
		target, err := s.casting.GetTarget(ctx, a.TargetID)
		if err != nil {
			return err
		}
		if target.ActiveSessionID == "" {
			// The target reports no active session, so the unknown projection
			// outcome is resolved: either nothing was created or what was
			// created is already gone. Keeping the reservation would leave the
			// screen occupied by an application that is not projecting.
			if err := persistRuntime(record, func(v *app.App) { v.PendingProjection = false }); err != nil {
				return err
			}
			a = record.Snapshot()
		} else {
			p, err := s.casting.GetSession(ctx, target.ActiveSessionID)
			if err != nil {
				return err
			}
			if p.TargetID != a.TargetID || p.EndpointID != a.EndpointID || p.Source.URL != a.Source.URL {
				return fmt.Errorf("foreign projection session; no cleanup")
			}
			if err := persistRuntime(record, func(v *app.App) { v.ProjectionSessionID = p.ID; v.PendingProjection = false }); err != nil {
				return err
			}
			a = record.Snapshot()
		}
	}
	if a.ProjectionSessionID != "" {
		p, err := s.casting.StopSession(ctx, a.ProjectionSessionID)
		if err != nil && !casting.IsStatus(err, http.StatusNotFound) {
			return err
		}
		if err == nil && p.State != casting.SessionStopped {
			return fmt.Errorf("projection cleanup not complete")
		}
		if err := persistRuntime(record, func(v *app.App) { v.ProjectionSessionID = "" }); err != nil {
			return err
		}
		a = record.Snapshot()
	}
	if a.InstanceID != "" {
		r, err := s.render.Delete(ctx, a.InstanceID)
		if err != nil && !render.IsStatus(err, http.StatusNotFound) {
			return err
		}
		if err == nil && r.State != render.StateStopped {
			return fmt.Errorf("runtime cleanup not complete")
		}
		if err := persistRuntime(record, func(v *app.App) { v.InstanceID = ""; v.Port = 0; v.AppBaseURL = "" }); err != nil {
			return err
		}
	}
	return persistRuntime(record, func(v *app.App) {
		v.CleanupPending = false
		v.ScreenID = ""
		v.TargetID = ""
		v.EndpointID = ""
		v.Source = casting.StreamSource{}
	})
}

// RecoverRuntime marks interrupted starts for read-only reconciliation, never restarting them.
func (s *Service) RecoverRuntime() error {
	for _, a := range s.apps.List() {
		if a.PendingRuntime || a.PendingProjection || (a.State == app.StateIdle && (a.CleanupPending || a.InstanceID != "" || a.ProjectionSessionID != "")) {
			if err := s.updateRuntime(a.ID, func(v *app.App) {
				v.State = app.StateError
				v.CleanupPending = true
				v.Error = &app.Error{Stage: app.StageStarting, Code: "RECOVERY_PENDING", Message: "正在核对中断的启动"}
			}); err != nil {
				return err
			}
		}
	}
	return nil
}
