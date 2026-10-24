// Package service implements the application lifecycle behind the MCP tools.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/openapi"
	"github.com/open-anywhere/open-anywhere/internal/gateway/render"
	"github.com/open-anywhere/open-anywhere/internal/gateway/screen"
	"github.com/open-anywhere/open-anywhere/pkg/casting"
)

// BuildStarter starts an asynchronous build for a new application.
type BuildStarter interface {
	Start(ctx context.Context, value app.App) error
}

// RenderClient is the subset of the Render API used by the control plane.
type RenderClient interface {
	Create(ctx context.Context, request render.CreateRequest) (render.App, error)
	Get(ctx context.Context, instanceID string) (render.App, error)
	Delete(ctx context.Context, instanceID string) (render.App, error)
	ListPorts(ctx context.Context) ([]render.Port, error)
	GetPort(ctx context.Context, port int) (render.Port, error)
}

// CastingClient is the subset of the casting API used by the control plane.
type CastingClient interface {
	ListTargets(ctx context.Context) ([]casting.Target, error)
	GetTarget(ctx context.Context, id string) (casting.Target, error)
	CreateSession(ctx context.Context, request casting.CreateSessionRequest) (casting.Session, error)
	GetSession(ctx context.Context, id string) (casting.Session, error)
	StopSession(ctx context.Context, id string) (casting.Session, error)
}

// Config contains service timeouts and port allocation bounds.
type Config struct {
	StartTimeout      time.Duration
	ProjectionTimeout time.Duration
	OperationsTimeout time.Duration
	MonitorInterval   time.Duration
	PortMin           int
	PortMax           int
}

// ToolError is a tool-level failure returned to Linx without changing the
// application state.
type ToolError struct {
	Code    string
	Message string
}

func (e *ToolError) Error() string { return e.Code + ": " + e.Message }

// ScreenView is one entry of get-screens.
type ScreenView struct {
	ScreenID   string  `json:"screen_id"`
	Name       string  `json:"name"`
	Available  bool    `json:"available"`
	CurrentApp *AppRef `json:"current_app"`
}

// AppRef identifies an application occupying a screen.
type AppRef struct {
	AppID string `json:"app_id"`
	Name  string `json:"name"`
}

// CreateResult is the create-app response.
type CreateResult struct {
	AppID string    `json:"app_id"`
	State app.State `json:"state"`
}

// StatusView is the state-dependent get-app-status response.
type StatusView struct {
	AppID                string     `json:"app_id"`
	State                app.State  `json:"state"`
	ElapsedSeconds       int64      `json:"elapsed_seconds,omitempty"`
	Message              string     `json:"message,omitempty"`
	Name                 string     `json:"name,omitempty"`
	BuildDurationSeconds int64      `json:"build_duration_seconds,omitempty"`
	ScreenID             string     `json:"screen_id,omitempty"`
	StartedAt            *time.Time `json:"started_at,omitempty"`
	UptimeSeconds        int64      `json:"uptime_seconds,omitempty"`
	StoppedAt            *time.Time `json:"stopped_at,omitempty"`
	Error                *app.Error `json:"error,omitempty"`
}

// OperationView describes one business operation for MCP clients.
type OperationView struct {
	OperationID string               `json:"operation_id"`
	Method      string               `json:"method"`
	Path        string               `json:"path"`
	Summary     string               `json:"summary,omitempty"`
	Description string               `json:"description,omitempty"`
	Parameters  []openapi.Parameter  `json:"parameters,omitempty"`
	RequestBody *openapi.RequestBody `json:"request_body,omitempty"`
}

// OperationsView lists the business operations an application exposes.
type OperationsView struct {
	AppID      string          `json:"app_id"`
	State      app.State       `json:"state"`
	Operations []OperationView `json:"operations"`
}

// OperationResult is the send-request response.
type OperationResult struct {
	Status int `json:"status"`
	Body   any `json:"body,omitempty"`
}

// Service coordinates applications, builds, Render and casting.
type Service struct {
	lifecycle sync.Mutex
	createMu  sync.Mutex
	apps      *app.Registry
	builder   BuildStarter
	render    RenderClient
	casting   CastingClient
	screens   *screen.Catalog
	notifier  app.Notifier
	cfg       Config

	http *http.Client
	now  func() time.Time
	logf func(string, ...any)
}

// New creates the application service.
func New(registry *app.Registry, builder BuildStarter, renderClient RenderClient, castingClient CastingClient, screens *screen.Catalog, notifier app.Notifier, cfg Config) *Service {
	if cfg.StartTimeout <= 0 {
		cfg.StartTimeout = 60 * time.Second
	}
	if cfg.ProjectionTimeout <= 0 {
		cfg.ProjectionTimeout = 30 * time.Second
	}
	if cfg.OperationsTimeout <= 0 {
		cfg.OperationsTimeout = 15 * time.Second
	}
	if cfg.MonitorInterval <= 0 {
		cfg.MonitorInterval = 5 * time.Second
	}
	if cfg.PortMin == 0 {
		cfg.PortMin = 39000
	}
	if cfg.PortMax == 0 {
		cfg.PortMax = 39999
	}
	return &Service{
		apps:     registry,
		builder:  builder,
		render:   renderClient,
		casting:  castingClient,
		screens:  screens,
		notifier: notifier,
		cfg:      cfg,
		http:     &http.Client{Timeout: cfg.OperationsTimeout},
		now:      func() time.Time { return time.Now().UTC() },
		logf:     log.Printf,
	}
}

// GetScreens aggregates configured screens, casting discovery and current
// application occupancy.
func (s *Service) GetScreens(ctx context.Context, _ app.Identity) ([]ScreenView, error) {
	targets, err := s.casting.ListTargets(ctx)
	if err != nil {
		return nil, &ToolError{Code: "SCREEN_DISCOVERY_FAILED", Message: "无法获取屏幕列表"}
	}
	byID := make(map[string]casting.Target, len(targets))
	for _, target := range targets {
		byID[target.ID] = target
	}
	configured := s.screens.List()
	views := make([]ScreenView, 0, len(targets))
	if len(configured) == 0 {
		for _, target := range targets {
			_, compatible := screen.DiscoverableEndpoint(target)
			views = append(views, ScreenView{
				ScreenID:  target.ID,
				Name:      target.Name,
				Available: target.State == casting.TargetOnline && compatible,
			})
		}
	} else {
		for _, item := range configured {
			target, found := byID[item.TargetID]
			available := false
			if found && target.State == casting.TargetOnline {
				_, available = screen.DiscoverableEndpoint(target)
			}
			views = append(views, ScreenView{ScreenID: item.ID, Name: item.Name, Available: available})
		}
	}
	running := make(map[string]app.App)
	for _, value := range s.apps.List() {
		if value.ScreenID != "" && (value.State == app.StateRunning || value.PendingRuntime || value.PendingProjection || value.CleanupPending || value.InstanceID != "" || value.ProjectionSessionID != "") {
			running[value.ScreenID] = value
		}
	}
	for index := range views {
		if value, ok := running[views[index].ScreenID]; ok {
			views[index].CurrentApp = &AppRef{AppID: value.ID, Name: value.Name}
		}
	}
	return views, nil
}

// CreateApp registers an application and starts its build.
func (s *Service) CreateApp(ctx context.Context, owner app.Identity, prompt string, playerNames []string) (CreateResult, error) {
	s.createMu.Lock()
	defer s.createMu.Unlock()
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return CreateResult{}, &ToolError{Code: "INVALID_ARGUMENT", Message: "prompt 不能为空"}
	}
	normalizedNames, err := normalizePlayerNames(playerNames)
	if err != nil {
		return CreateResult{}, err
	}
	if existing, ok := s.apps.FindBuilding(owner); ok {
		return CreateResult{AppID: existing.ID, State: existing.State}, nil
	}
	id := app.NewID()
	entry := s.apps.Add(app.App{
		ID:             id,
		Owner:          owner,
		Prompt:         prompt,
		PlayerNames:    normalizedNames,
		State:          app.StateBuilding,
		BuildStartedAt: s.now(),
	})
	if err := s.builder.Start(ctx, entry.Snapshot()); err != nil {
		s.apps.Remove(id)
		s.logf("create-app %s: %v", id, err)
		return CreateResult{}, &ToolError{Code: "BUILD_START_FAILED", Message: "无法启动构建"}
	}
	return CreateResult{AppID: id, State: app.StateBuilding}, nil
}

// normalizePlayerNames trims names, rejects blanks and duplicates, and keeps
// the caller's order. An empty list means the build has no initial players.
func normalizePlayerNames(names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	normalized := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, &ToolError{Code: "INVALID_PLAYER_NAMES", Message: "玩家名称不能为空白"}
		}
		if _, exists := seen[name]; exists {
			return nil, &ToolError{Code: "INVALID_PLAYER_NAMES", Message: "玩家名称不能重复：" + name}
		}
		seen[name] = struct{}{}
		normalized = append(normalized, name)
	}
	return normalized, nil
}

// GetAppStatus returns the state-dependent status of an application.
func (s *Service) GetAppStatus(ctx context.Context, owner app.Identity, appID string) (StatusView, error) {
	value, err := s.owned(owner, appID)
	if err != nil {
		return StatusView{}, err
	}
	return s.statusView(value), nil
}

// GetAppOperations returns the operations declared by an application's
// OpenAPI contract so the caller can discover valid operation IDs and
// parameter schemas.
func (s *Service) GetAppOperations(ctx context.Context, owner app.Identity, appID string) (OperationsView, error) {
	value, err := s.owned(owner, appID)
	if err != nil {
		return OperationsView{}, err
	}
	switch value.State {
	case app.StateBuilding:
		return OperationsView{}, &ToolError{Code: "APP_BUILDING", Message: "应用仍在构建"}
	case app.StateError:
		return OperationsView{}, &ToolError{Code: "APP_IN_ERROR", Message: "应用处于错误状态"}
	}
	operations := []OperationView{}
	if value.OpenAPI != nil {
		for _, operation := range value.OpenAPI.Operations() {
			operations = append(operations, OperationView{
				OperationID: operation.OperationID,
				Method:      operation.Method,
				Path:        operation.Path,
				Summary:     operation.Summary,
				Description: operation.Description,
				Parameters:  operation.Parameters,
				RequestBody: operation.RequestBody,
			})
		}
	}
	return OperationsView{AppID: value.ID, State: value.State, Operations: operations}, nil
}

// StartApp starts the application on the screen and waits for projection.
func (s *Service) StartApp(ctx context.Context, owner app.Identity, appID, screenID string) (StatusView, error) {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	if strings.TrimSpace(screenID) == "" {
		return StatusView{}, &ToolError{Code: "INVALID_ARGUMENT", Message: "screen_id 不能为空"}
	}
	entry, err := s.entry(owner, appID)
	if err != nil {
		return StatusView{}, err
	}
	entry.Begin()
	defer entry.End()
	value := entry.Snapshot()
	if !value.Owner.Equal(owner) {
		return StatusView{}, &ToolError{Code: "APP_NOT_FOUND", Message: "会话归属已改变"}
	}
	switch value.State {
	case app.StateRunning:
		if value.ScreenID == screenID {
			if err := s.verifyRunning(ctx, value); err != nil {
				return StatusView{}, err
			}
			return s.statusView(value), nil
		}
		return StatusView{}, &ToolError{Code: "APP_ALREADY_RUNNING", Message: "应用已在其他屏幕运行"}
	case app.StateBuilding:
		return StatusView{}, &ToolError{Code: "APP_BUILDING", Message: "应用仍在构建"}
	case app.StateError:
		return StatusView{}, &ToolError{Code: "APP_IN_ERROR", Message: "应用处于错误状态"}
	case app.StateIdle:
	default:
		return StatusView{}, &ToolError{Code: "INVALID_STATE", Message: "应用状态不允许启动"}
	}
	target, err := s.target(ctx, screenID)
	if err != nil {
		return StatusView{}, err
	}
	if other, ok := s.apps.RunningOnScreen(screenID); ok && other.ID != appID {
		return StatusView{}, &ToolError{Code: "TARGET_BUSY", Message: "目标屏幕已被占用"}
	}
	entry.Update(func(a *app.App) { a.ScreenID = screenID; a.TargetID = target.ID; a.PendingRuntime = true })
	value = entry.Snapshot()
	binding, failure := s.startRuntime(ctx, value, target)
	if failure != nil {
		if failure.Code == "APP_ALREADY_RUNNING" {
			entry.Update(func(a *app.App) { a.PendingRuntime = false; a.ScreenID = ""; a.TargetID = "" })
			return StatusView{}, failure.toolError()
		}
		entry.Update(func(a *app.App) {
			if a.State != app.StateIdle {
				return
			}
			a.State = app.StateError
			a.ScreenID = screenID
			a.Error = &app.Error{Stage: app.StageStarting, Code: failure.Code, Message: failure.Message}
			a.CleanupPending = true
		})
		_ = s.reconcile(ctx, entry)
		s.notifyError(entry.Snapshot(), screenID)
		return StatusView{}, failure.toolError()
	}
	entry.Update(func(a *app.App) {
		if a.State != app.StateIdle {
			return
		}
		a.State = app.StateRunning
		a.PendingRuntime = false
		a.PendingProjection = false
		a.CleanupPending = false
		a.ScreenID = screenID
		a.InstanceID = binding.instance.InstanceID
		a.Port = binding.port
		a.AppBaseURL = binding.instance.AppBaseURL
		a.Source = binding.source
		a.TargetID = target.ID
		a.EndpointID = binding.endpoint.ID
		a.ProjectionSessionID = binding.session.ID
		a.StartedAt = s.now()
		a.StoppedAt = time.Time{}
		a.Error = nil
	})
	return s.statusView(entry.Snapshot()), nil
}

// StopApp stops projection first and then the Render instance.
func (s *Service) StopApp(ctx context.Context, owner app.Identity, appID string) (StatusView, error) {
	entry, err := s.entry(owner, appID)
	if err != nil {
		return StatusView{}, err
	}
	entry.Begin()
	defer entry.End()
	value := entry.Snapshot()
	if !value.Owner.Equal(owner) {
		return StatusView{}, &ToolError{Code: "APP_NOT_FOUND", Message: "会话归属已改变"}
	}
	switch value.State {
	case app.StateIdle:
		return s.statusView(value), nil
	case app.StateError:
		if err := s.reconcile(ctx, entry); err != nil {
			return StatusView{}, err
		}
		return s.statusView(entry.Snapshot()), nil
	case app.StateBuilding:
		return StatusView{}, &ToolError{Code: "APP_BUILDING", Message: "应用仍在构建"}
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if value.ProjectionSessionID != "" {
		if stopped, err := s.casting.StopSession(cleanupCtx, value.ProjectionSessionID); (err != nil && !casting.IsStatus(err, http.StatusNotFound)) || (err == nil && stopped.State != casting.SessionStopped) {
			return StatusView{}, &ToolError{Code: "STOP_FAILED", Message: "停止投屏失败"}
		}
	}
	if value.InstanceID != "" {
		if stopped, err := s.render.Delete(cleanupCtx, value.InstanceID); (err != nil && !render.IsStatus(err, http.StatusNotFound)) || (err == nil && stopped.State != render.StateStopped) {
			return StatusView{}, &ToolError{Code: "STOP_FAILED", Message: "停止应用失败"}
		}
	}
	entry.Update(func(a *app.App) {
		if a.State != app.StateRunning {
			return
		}
		a.State = app.StateIdle
		a.StoppedAt = s.now()
		a.ScreenID = ""
		a.InstanceID = ""
		a.Port = 0
		a.AppBaseURL = ""
		a.Source = casting.StreamSource{}
		a.TargetID = ""
		a.EndpointID = ""
		a.ProjectionSessionID = ""
		a.Error = nil
	})
	return s.statusView(entry.Snapshot()), nil
}

// SendRequest forwards one OpenAPI operation to a running application.
func (s *Service) SendRequest(ctx context.Context, owner app.Identity, appID, operationID string, parameters map[string]any, body any) (OperationResult, error) {
	record, err := s.entry(owner, appID)
	if err != nil {
		return OperationResult{}, err
	}
	record.Begin()
	defer record.End()
	value := record.Snapshot()
	if !value.Owner.Equal(owner) {
		return OperationResult{}, &ToolError{Code: "APP_NOT_FOUND", Message: "会话归属已改变"}
	}
	if err = s.verifyRunning(ctx, value); err != nil {
		return OperationResult{}, err
	}
	if value.State != app.StateRunning || value.AppBaseURL == "" {
		return OperationResult{}, &ToolError{Code: "APP_NOT_RUNNING", Message: "应用未在运行"}
	}
	if _, ok := value.OpenAPI.Lookup(operationID); !ok {
		return OperationResult{}, &ToolError{Code: "OPERATION_NOT_FOUND", Message: "应用没有该操作"}
	}
	out, err := value.OpenAPI.Execute(ctx, value.AppBaseURL, operationID, parameters, body, s.http)
	if err != nil {
		return OperationResult{}, &ToolError{Code: "INVALID_ARGUMENT", Message: err.Error()}
	}
	return OperationResult{Status: out.Status, Body: out.Data}, nil
}

// RunMonitor polls running applications until ctx is canceled.
func (s *Service) RunMonitor(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.MonitorInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkRunning(ctx)
		}
	}
}

func (s *Service) checkRunning(ctx context.Context) {
	for _, value := range s.apps.List() {
		if value.State == app.StateError && (value.CleanupPending || value.PendingRuntime || value.PendingProjection || value.InstanceID != "" || value.ProjectionSessionID != "") {
			if record, ok := s.apps.Get(value.ID); ok {
				record.Begin()
				_ = s.reconcile(ctx, record)
				record.End()
			}
			continue
		}
		if value.State != app.StateRunning {
			continue
		}
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		if value.InstanceID != "" {
			instance, err := s.render.Get(checkCtx, value.InstanceID)
			switch {
			case err == nil && (instance.State == render.StateError || instance.State == render.StateStopped || instance.State == render.StateStopping):
				code, message := "APP_EXITED", "应用进程已退出"
				if instance.Error != nil {
					if instance.Error.Code != "" {
						code = instance.Error.Code
					}
					if instance.Error.Message != "" {
						message = instance.Error.Message
					}
				}
				cancel()
				s.runtimeFailure(value, code, message)
				continue
			case err != nil && render.IsStatus(err, http.StatusNotFound):
				cancel()
				s.runtimeFailure(value, "APP_EXITED", "应用进程已退出")
				continue
			}
		}
		if value.ProjectionSessionID != "" {
			session, err := s.casting.GetSession(checkCtx, value.ProjectionSessionID)
			if err == nil && (session.State == casting.SessionStopped || session.State == casting.SessionFailed) {
				cancel()
				s.runtimeFailure(value, "PROJECTION_DISCONNECTED", "投屏已断开")
				continue
			}
			if casting.IsStatus(err, http.StatusNotFound) {
				cancel()
				s.runtimeFailure(value, "PROJECTION_DISCONNECTED", "投屏已断开")
				continue
			}
		}
		cancel()
	}
}

func (s *Service) runtimeFailure(value app.App, code, message string) {
	entry, ok := s.apps.Get(value.ID)
	if !ok {
		return
	}
	var snapshot app.App
	applied := false
	entry.Update(func(a *app.App) {
		if a.State != app.StateRunning || a.InstanceID != value.InstanceID {
			return
		}
		applied = true
		a.State = app.StateError
		a.CleanupPending = true
		a.Error = &app.Error{Stage: app.StageRunning, Code: code, Message: message}
		snapshot = *a
	})
	if !applied {
		return
	}
	s.logf("app %s runtime failure: %s: %s", value.ID, code, message)
	entry.Begin()
	_ = s.reconcile(context.Background(), entry)
	entry.End()

	s.notifier.Notify(snapshot.Owner, app.Notification{
		AppID:    snapshot.ID,
		State:    app.StateError,
		ScreenID: snapshot.ScreenID,
		Message:  message,
		Error:    snapshot.Error,
	})
}

type startBinding struct {
	instance render.App
	port     int
	source   casting.StreamSource
	endpoint casting.Endpoint
	session  casting.Session
}

type startFailure struct {
	Code    string
	Message string
	Cause   error
}

func (f *startFailure) toolError() error {
	return &ToolError{Code: f.Code, Message: f.Message}
}

func (s *Service) startRuntime(ctx context.Context, value app.App, target casting.Target) (startBinding, *startFailure) {
	if target.State != casting.TargetOnline {
		return startBinding{}, &startFailure{Code: "TARGET_UNREACHABLE", Message: "目标屏幕不可达"}
	}
	if value.Artifact.DownloadURL == "" {
		return startBinding{}, &startFailure{Code: "START_FAILED", Message: "应用产物不可用"}
	}
	startCtx, cancel := context.WithTimeout(ctx, s.cfg.StartTimeout)
	defer cancel()
	excluded := make(map[int]bool)
	var instance render.App
	var port int
	var createErr error
	for attempt := 0; attempt < 3; attempt++ {
		port, createErr = s.choosePort(startCtx, excluded)
		if createErr != nil {
			break
		}
		s.updateRuntime(value.ID, func(a *app.App) { a.Port = port; a.PendingRuntime = true })
		instance, createErr = s.render.Create(startCtx, render.CreateRequest{
			AppID:       value.ID,
			ArtifactURL: value.Artifact.DownloadURL,
			Port:        port,
		})
		if createErr == nil {
			if instance.AppID != "" && instance.AppID != value.ID {
				createErr = errors.New("Render returned foreign app_id")
				break
			}
			if instance.InstanceID == "" {
				createErr = errors.New("Render returned no instance_id")
				break
			}
			s.updateRuntime(value.ID, func(a *app.App) {
				a.InstanceID = instance.InstanceID
				a.PendingRuntime = false
				a.AppBaseURL = instance.AppBaseURL
				a.Source = instance.Source
			})
			break
		}
		if render.IsCode(createErr, "PORT_IN_USE") || render.IsCode(createErr, "INVALID_REQUEST") || render.IsCode(createErr, "APP_ALREADY_RUNNING") {
			s.updateRuntime(value.ID, func(a *app.App) { a.PendingRuntime = false })
		}
		if render.IsCode(createErr, "PORT_IN_USE") {
			excluded[port] = true
			continue
		}
		break
	}
	if createErr != nil {
		if render.IsCode(createErr, "APP_ALREADY_RUNNING") {
			return startBinding{}, &startFailure{Code: "APP_ALREADY_RUNNING", Message: "应用已有运行实例", Cause: createErr}
		}
		return startBinding{}, &startFailure{Code: "START_FAILED", Message: "应用启动失败", Cause: createErr}
	}
	if instance.State != render.StateReady {
		// Cleanup uses the persisted application record.
		return startBinding{}, &startFailure{Code: "START_FAILED", Message: "应用未就绪"}
	}
	source := instance.Source
	if source.Type == "" || source.URL == "" {
		// Cleanup uses the persisted application record.
		return startBinding{}, &startFailure{Code: "STREAM_START_FAILED", Message: "应用媒体流未就绪"}
	}
	endpoint, ok := screen.SelectEndpoint(target, source)
	if !ok {
		// Cleanup uses the persisted application record.
		return startBinding{}, &startFailure{Code: "TARGET_UNREACHABLE", Message: "屏幕不支持该媒体格式"}
	}
	s.updateRuntime(value.ID, func(a *app.App) { a.EndpointID = endpoint.ID; a.Source = source; a.PendingProjection = true })
	session, err := s.casting.CreateSession(startCtx, casting.CreateSessionRequest{
		TargetID:   target.ID,
		EndpointID: endpoint.ID,
		Source:     source,
	})
	if err != nil {
		// Cleanup uses the persisted application record.
		return startBinding{}, &startFailure{Code: "PROJECTION_TIMEOUT", Message: "无法建立投屏", Cause: err}
	}
	s.updateRuntime(value.ID, func(a *app.App) { a.ProjectionSessionID = session.ID; a.PendingProjection = false })
	if err := s.waitPlaying(startCtx, session.ID); err != nil {
		// Cleanup uses the persisted application record.
		return startBinding{}, &startFailure{Code: "PROJECTION_TIMEOUT", Message: "投屏超时", Cause: err}
	}
	return startBinding{instance: instance, port: port, source: source, endpoint: endpoint, session: session}, nil
}

func (s *Service) waitPlaying(ctx context.Context, sessionID string) error {
	deadline := time.Now().Add(s.cfg.ProjectionTimeout)
	for {
		session, err := s.casting.GetSession(ctx, sessionID)
		if err != nil {
			return err
		}
		switch session.State {
		case casting.SessionPlaying:
			return nil
		case casting.SessionStopped, casting.SessionFailed:
			return fmt.Errorf("projection state %s", session.State)
		}
		if time.Now().After(deadline) {
			return context.DeadlineExceeded
		}
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *Service) choosePort(ctx context.Context, excluded map[int]bool) (int, error) {
	ports, err := s.render.ListPorts(ctx)
	if err != nil {
		return 0, err
	}
	used := make(map[int]bool, len(ports))
	for _, item := range ports {
		if item.Occupied {
			used[item.Port] = true
		}
	}
	for port := s.cfg.PortMin; port <= s.cfg.PortMax; port++ {
		if used[port] || excluded[port] {
			continue
		}
		info, err := s.render.GetPort(ctx, port)
		if err != nil {
			return 0, err
		}
		if !info.Occupied {
			return port, nil
		}
	}
	return 0, errors.New("no free port")
}

func (s *Service) cleanupRuntime(instanceID, sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if sessionID != "" {
		_, _ = s.casting.StopSession(ctx, sessionID)
	}
	if instanceID != "" {
		_, _ = s.render.Delete(ctx, instanceID)
	}
}

func (s *Service) target(ctx context.Context, screenID string) (casting.Target, error) {
	targetID := screenID
	if configured, ok := s.screens.Get(screenID); ok {
		targetID = configured.TargetID
	} else if !s.screens.Empty() {
		return casting.Target{}, &ToolError{Code: "TARGET_UNREACHABLE", Message: "未知的屏幕"}
	}
	target, err := s.casting.GetTarget(ctx, targetID)
	if err != nil {
		if casting.IsStatus(err, http.StatusNotFound) {
			return casting.Target{}, &ToolError{Code: "TARGET_UNREACHABLE", Message: "目标屏幕不可达"}
		}
		return casting.Target{}, &ToolError{Code: "TARGET_UNREACHABLE", Message: "无法访问投屏服务"}
	}
	return target, nil
}

func (s *Service) entry(owner app.Identity, appID string) (*app.Record, error) {
	entry, ok := s.apps.Get(appID)
	if !ok {
		return nil, &ToolError{Code: "APP_NOT_FOUND", Message: "应用不存在"}
	}
	if !entry.Snapshot().Owner.Equal(owner) || entry.Snapshot().Owner.AgentID != owner.AgentID {
		return nil, &ToolError{Code: "APP_NOT_FOUND", Message: "应用不存在"}
	}
	return entry, nil
}

func (s *Service) owned(owner app.Identity, appID string) (app.App, error) {
	entry, err := s.entry(owner, appID)
	if err != nil {
		return app.App{}, err
	}
	return entry.Snapshot(), nil
}

func (s *Service) statusView(value app.App) StatusView {
	view := StatusView{AppID: value.ID, State: value.State}
	switch value.State {
	case app.StateBuilding:
		if !value.BuildStartedAt.IsZero() {
			view.ElapsedSeconds = int64(s.now().Sub(value.BuildStartedAt).Seconds())
			if view.ElapsedSeconds < 0 {
				view.ElapsedSeconds = 0
			}
		}
		view.Message = value.BuildingMessage()
	case app.StateIdle:
		view.Name = value.Name
		view.BuildDurationSeconds = int64(value.BuildDuration.Seconds())
	case app.StateRunning:
		view.Name = value.Name
		view.ScreenID = value.ScreenID
		if !value.StartedAt.IsZero() {
			started := value.StartedAt.UTC().Truncate(time.Second)
			view.StartedAt = &started
			view.UptimeSeconds = int64(s.now().Sub(value.StartedAt).Seconds())
			if view.UptimeSeconds < 0 {
				view.UptimeSeconds = 0
			}
		}
	case app.StateError:
		view.ScreenID = value.ScreenID
		view.Error = value.Error
		if view.Error == nil {
			view.Error = &app.Error{Stage: app.StageRunning, Code: "APP_EXITED"}
		}
	}
	return view
}

func (s *Service) notifyError(value app.App, screenID string) {
	s.notifier.Notify(value.Owner, app.Notification{
		AppID:    value.ID,
		State:    app.StateError,
		ScreenID: screenID,
		Error:    value.Error,
	})
}

func scalarParameter(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, true
	case bool:
		return strconv.FormatBool(typed), true
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64), true
	case float32:
		return strconv.FormatFloat(float64(typed), 'f', -1, 32), true
	case int:
		return strconv.Itoa(typed), true
	case int64:
		return strconv.FormatInt(typed, 10), true
	case int32:
		return strconv.FormatInt(int64(typed), 10), true
	case json.Number:
		return typed.String(), true
	default:
		return "", false
	}
}

// GetOpenAPI exposes the full registered contract for clients that need more than operation summaries.
func (s *Service) GetOpenAPI(ctx context.Context, owner app.Identity, id string) (map[string]any, error) {
	a, err := s.owned(owner, id)
	if err != nil {
		return nil, err
	}
	if a.State == app.StateBuilding {
		return nil, &ToolError{Code: "APP_BUILDING", Message: "应用仍在构建"}
	}
	return a.OpenAPI.Document()
}

func (s *Service) verifyRunning(ctx context.Context, a app.App) error {
	if a.State != app.StateRunning {
		return nil
	}
	if a.InstanceID != "" {
		r, err := s.render.Get(ctx, a.InstanceID)
		if err != nil {
			return &ToolError{Code: "RUNTIME_UNAVAILABLE", Message: "无法确认应用状态"}
		}
		if r.State != render.StateReady {
			return &ToolError{Code: "APP_NOT_RUNNING", Message: "应用未就绪"}
		}
	}
	if a.ProjectionSessionID != "" {
		p, err := s.casting.GetSession(ctx, a.ProjectionSessionID)
		if err != nil {
			return &ToolError{Code: "PROJECTION_UNAVAILABLE", Message: "无法确认投屏状态"}
		}
		if p.State != casting.SessionPlaying {
			return &ToolError{Code: "APP_NOT_RUNNING", Message: "投屏未播放"}
		}
	}
	return nil
}
