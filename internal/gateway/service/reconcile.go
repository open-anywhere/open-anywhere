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

func (s *Service) updateRuntime(id string, fn func(*app.App)) {
	if r, ok := s.apps.Get(id); ok {
		r.Update(fn)
	}
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
			return &ToolError{Code: "RECONCILIATION_PENDING", Message: "Render 结果未确认，保留占用"}
		}
		instance, err := s.render.Get(ctx, found)
		if err != nil {
			return err
		}
		if instance.AppID != a.ID {
			return fmt.Errorf("foreign Render instance")
		}
		record.Update(func(v *app.App) {
			v.InstanceID = found
			v.PendingRuntime = false
			v.Source = instance.Source
			v.AppBaseURL = instance.AppBaseURL
		})
		a = record.Snapshot()
	}
	if a.PendingProjection {
		target, err := s.casting.GetTarget(ctx, a.TargetID)
		if err != nil {
			return err
		}
		if target.ActiveSessionID == "" {
			return &ToolError{Code: "RECONCILIATION_PENDING", Message: "投屏结果未确认，保留占用"}
		}
		p, err := s.casting.GetSession(ctx, target.ActiveSessionID)
		if err != nil {
			return err
		}
		if p.TargetID != a.TargetID || p.EndpointID != a.EndpointID || p.Source.URL != a.Source.URL {
			return fmt.Errorf("foreign projection session; no cleanup")
		}
		record.Update(func(v *app.App) { v.ProjectionSessionID = p.ID; v.PendingProjection = false })
		a = record.Snapshot()
	}
	if a.ProjectionSessionID != "" {
		p, err := s.casting.StopSession(ctx, a.ProjectionSessionID)
		if err != nil && !casting.IsStatus(err, http.StatusNotFound) {
			return err
		}
		if err == nil && p.State != casting.SessionStopped {
			return fmt.Errorf("projection cleanup not complete")
		}
		record.Update(func(v *app.App) { v.ProjectionSessionID = "" })
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
		record.Update(func(v *app.App) { v.InstanceID = ""; v.Port = 0; v.AppBaseURL = "" })
	}
	record.Update(func(v *app.App) {
		v.CleanupPending = false
		v.ScreenID = ""
		v.TargetID = ""
		v.EndpointID = ""
		v.Source = casting.StreamSource{}
	})
	return nil
}

// RecoverRuntime marks interrupted starts for read-only reconciliation, never restarting them.
func (s *Service) RecoverRuntime() {
	for _, a := range s.apps.List() {
		if a.PendingRuntime || a.PendingProjection || (a.State == app.StateIdle && (a.InstanceID != "" || a.ProjectionSessionID != "")) {
			s.updateRuntime(a.ID, func(v *app.App) {
				v.State = app.StateError
				v.CleanupPending = true
				v.Error = &app.Error{Stage: app.StageStarting, Code: "RECOVERY_PENDING", Message: "正在核对中断的启动"}
			})
		}
	}
}
