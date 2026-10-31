// Package service implements the application lifecycle behind the MCP tools.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/build"
	"github.com/open-anywhere/open-anywhere/internal/gateway/httpapi"
	"github.com/open-anywhere/open-anywhere/internal/gateway/netguard"
	"github.com/open-anywhere/open-anywhere/internal/gateway/openapi"
	"github.com/open-anywhere/open-anywhere/internal/gateway/render"
	"github.com/open-anywhere/open-anywhere/pkg/casting"
)

// BuildStarter starts an asynchronous build for a new application.
type BuildStarter interface {
	Start(ctx context.Context, value app.App) error
}

// BuildContinuer submits a follow-up build turn on an existing AgentAPI session.
type BuildContinuer interface {
	Continue(ctx context.Context, value app.App) error
}

// RenderClient is the subset of the Render API used by the control plane.
type RenderClient interface {
	Create(ctx context.Context, request render.CreateRequest) (render.App, error)
	Get(ctx context.Context, instanceID string) (render.App, error)
	Delete(ctx context.Context, instanceID string) (render.App, error)
	ListPorts(ctx context.Context) ([]render.Port, error)
	GetPort(ctx context.Context, port int) (render.Port, error)
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
	// StreamURL is the application's media stream. The device casts it to a
	// screen on site; the control plane does not project it itself.
	StreamURL     string     `json:"stream_url,omitempty"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	UptimeSeconds int64      `json:"uptime_seconds,omitempty"`
	StoppedAt     *time.Time `json:"stopped_at,omitempty"`
	Error         *app.Error `json:"error,omitempty"`
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

// Binding resolves the Render service for one owner. A user may use the
// platform default (Managed) or their own public service.
type Binding struct {
	RenderURL   string
	RenderToken string
	Managed     bool
}

// BindingSource resolves an owner's service binding.
type BindingSource interface {
	BindingFor(ctx context.Context, owner app.Identity) (Binding, error)
}

// Clients builds clients for a binding. Implementations may cache by address
// and token; tokens never appear in logs.
type Clients interface {
	Render(binding Binding) RenderClient
}

// StaticBindings returns the same binding for every owner. It keeps
// single-tenant deployments and tests working unchanged.
type StaticBindings struct {
	Binding Binding
}

// BindingFor implements BindingSource.
func (s StaticBindings) BindingFor(context.Context, app.Identity) (Binding, error) {
	return s.Binding, nil
}

// legacyClients resolves the fixed single-tenant clients on every access so
// replacing them (as tests do) takes effect for owner-aware paths too.
type legacyClients struct {
	service *Service
}

func (c legacyClients) Render(Binding) RenderClient { return c.service.render }

// runtime is the resolved downstream configuration for one owner.
type runtime struct {
	binding Binding
	render  RenderClient
}

// keyedMutex serializes lifecycle work per owner instead of globally, so one
// user's slow Render or Casting service cannot block another user.
type keyedMutex struct {
	mu    sync.Mutex
	locks map[string]*keyedLock
}

type keyedLock struct {
	key  string
	mu   sync.Mutex
	refs int
}

func newKeyedMutex() *keyedMutex { return &keyedMutex{locks: make(map[string]*keyedLock)} }

func (k *keyedMutex) ref(key string) *keyedLock {
	k.mu.Lock()
	defer k.mu.Unlock()
	lock := k.locks[key]
	if lock == nil {
		lock = &keyedLock{key: key}
		k.locks[key] = lock
	}
	lock.refs++
	return lock
}

func (k *keyedMutex) drop(lock *keyedLock) {
	k.mu.Lock()
	defer k.mu.Unlock()
	lock.refs--
	if lock.refs == 0 {
		delete(k.locks, lock.key)
	}
}

// Lock blocks until the owner's lock is held and returns its release.
func (k *keyedMutex) Lock(key string) func() {
	lock := k.ref(key)
	lock.mu.Lock()
	return func() {
		lock.mu.Unlock()
		k.drop(lock)
	}
}

// TryLock acquires the owner's lock without blocking. It returns false when
// another operation for the same owner is in flight.
func (k *keyedMutex) TryLock(key string) (func(), bool) {
	lock := k.ref(key)
	if !lock.mu.TryLock() {
		k.drop(lock)
		return nil, false
	}
	return func() {
		lock.mu.Unlock()
		k.drop(lock)
	}, true
}

func ownerKey(owner app.Identity) string {
	return owner.DeviceID + "\x00" + owner.AgentID
}

// ownedByAny reports whether owner is one of owners.
func ownedByAny(owner app.Identity, owners []app.Identity) bool {
	for _, candidate := range owners {
		if owner.SameOwner(candidate) {
			return true
		}
	}
	return false
}

// Service coordinates applications, builds and Render.
type Service struct {
	lifecycle *keyedMutex
	createMu  *keyedMutex
	pruneMu   sync.Mutex
	apps      *app.Registry
	builder   BuildStarter
	bindings  BindingSource
	clients   Clients
	// render serves the single-tenant constructor and the compatibility
	// helpers below; the owner-aware paths use runtimeFor.
	render    RenderClient
	notifier  app.Notifier
	artifacts ArtifactStore
	cfg       Config

	http        *http.Client
	validateURL func(context.Context, string) error
	now         func() time.Time
	logf        func(string, ...any)
}

// New creates the application service for a single-tenant deployment.
func New(registry *app.Registry, builder BuildStarter, renderClient RenderClient, notifier app.Notifier, cfg Config) *Service {
	service := NewWithBindings(registry, builder, StaticBindings{Binding: Binding{Managed: true}}, nil, notifier, cfg)
	service.render = renderClient
	service.clients = legacyClients{service: service}
	return service
}

// NewWithBindings creates the application service for a multi-tenant
// deployment. Every downstream access resolves the owner's binding first.
func NewWithBindings(registry *app.Registry, builder BuildStarter, bindings BindingSource, clients Clients, notifier app.Notifier, cfg Config) *Service {
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
		lifecycle: newKeyedMutex(),
		createMu:  newKeyedMutex(),
		apps:      registry,
		builder:   builder,
		bindings:  bindings,
		clients:   clients,
		notifier:  notifier,
		cfg:       cfg,
		http:      &http.Client{Timeout: cfg.OperationsTimeout},
		now:       func() time.Time { return time.Now().UTC() },
		logf:      log.Printf,
	}
}

// SetURLValidator installs the outbound address policy used before forwarding
// a request to an application.
func (s *Service) SetURLValidator(validate func(context.Context, string) error) {
	s.validateURL = validate
}

// SetHTTPClient replaces the HTTP client used for application requests.
func (s *Service) SetHTTPClient(client *http.Client) {
	if client != nil {
		s.http = client
	}
}

// runtimeFor resolves the owner's binding and the client that talks to it.
func (s *Service) runtimeFor(ctx context.Context, owner app.Identity) (runtime, error) {
	if s.bindings == nil || s.clients == nil {
		return runtime{}, &ToolError{Code: "SERVICE_NOT_CONFIGURED", Message: "尚未配置渲染服务"}
	}
	binding, err := s.bindings.BindingFor(ctx, owner)
	if err != nil {
		return runtime{}, err
	}
	return runtime{
		binding: binding,
		render:  s.clients.Render(binding),
	}, nil
}

// CreateApp registers an application and starts its build.
func (s *Service) CreateApp(ctx context.Context, owner app.Identity, prompt string, playerNames []string) (CreateResult, error) {
	unlock := s.createMu.Lock(ownerKey(owner))
	defer unlock()
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return CreateResult{}, &ToolError{Code: "INVALID_ARGUMENT", Message: "prompt 不能为空"}
	}
	normalizedNames, err := normalizePlayerNames(playerNames)
	if err != nil {
		return CreateResult{}, err
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
	if err := persistRuntime(entry, func(*app.App) {}); err != nil {
		s.apps.Remove(id)
		return CreateResult{}, err
	}
	if err := s.builder.Start(ctx, entry.Snapshot()); err != nil {
		s.apps.Remove(id)
		s.logf("create-app %s: %v", id, err)
		if errors.Is(err, app.ErrPlatformNotConfigured) {
			return CreateResult{}, &ToolError{Code: "PLATFORM_NOT_CONFIGURED", Message: "平台尚未完成配置，请联系管理员"}
		}
		return CreateResult{}, &ToolError{Code: "BUILD_START_FAILED", Message: "无法启动构建"}
	}
	s.PruneHistory()
	return CreateResult{AppID: id, State: app.StateBuilding}, nil
}

// UpdateApp submits a revision turn on the application's bound AgentAPI session.
func (s *Service) UpdateApp(ctx context.Context, owner app.Identity, appID, prompt string) (CreateResult, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return CreateResult{}, &ToolError{Code: "INVALID_ARGUMENT", Message: "prompt 不能为空"}
	}
	record, err := s.entry(owner, appID)
	if err != nil {
		return CreateResult{}, err
	}
	before := record.Snapshot()
	switch before.State {
	case app.StateBuilding:
		return CreateResult{}, &ToolError{Code: "APP_BUILDING", Message: "应用仍在构建或修订中"}
	case app.StateRunning:
		return CreateResult{}, &ToolError{Code: "APP_RUNNING", Message: "应用仍在运行，请先 stop-app"}
	case app.StateIdle:
		// ok
	case app.StateError:
		if !hasStartableArtifact(before) {
			return CreateResult{}, &ToolError{Code: "APP_IN_ERROR", Message: "应用处于错误状态且无法修订"}
		}
	default:
		return CreateResult{}, &ToolError{Code: "APP_IN_ERROR", Message: "应用当前状态无法修订"}
	}
	if before.SessionID == "" {
		return CreateResult{}, &ToolError{Code: "SESSION_UNAVAILABLE", Message: "构建会话不可用，请重新 create-app"}
	}
	continuer, ok := s.builder.(BuildContinuer)
	if !ok {
		return CreateResult{}, &ToolError{Code: "UPDATE_START_FAILED", Message: "无法启动修订"}
	}
	started := s.now()
	if err := persistRuntime(record, func(a *app.App) {
		a.State = app.StateBuilding
		a.Prompt = prompt
		a.BuildStartedAt = started
		a.BuildFinishedAt = time.Time{}
		a.BuildDuration = 0
		a.Error = nil
		a.LastAgentMessage = ""
		a.LastProgress = ""
		a.LastStatusMessage = ""
	}); err != nil {
		return CreateResult{}, err
	}
	if err := continuer.Continue(ctx, record.Snapshot()); err != nil {
		_ = persistRuntime(record, func(a *app.App) {
			a.State = before.State
			a.Prompt = before.Prompt
			a.BuildStartedAt = before.BuildStartedAt
			a.BuildFinishedAt = before.BuildFinishedAt
			a.BuildDuration = before.BuildDuration
			a.Error = before.Error
			a.LastAgentMessage = before.LastAgentMessage
			a.LastProgress = before.LastProgress
			a.LastStatusMessage = before.LastStatusMessage
		})
		return CreateResult{}, mapContinueError(err)
	}
	return CreateResult{AppID: appID, State: app.StateBuilding}, nil
}

func hasStartableArtifact(value app.App) bool {
	return value.Artifact.SHA256 != "" && (value.Artifact.Path != "" || value.Artifact.DownloadURL != "")
}

func mapContinueError(err error) error {
	switch {
	case errors.Is(err, build.ErrSessionUnavailable):
		return &ToolError{Code: "SESSION_UNAVAILABLE", Message: "构建会话不可用，请重新 create-app"}
	case errors.Is(err, build.ErrSessionBusy):
		return &ToolError{Code: "UPDATE_START_FAILED", Message: "构建会话正忙，稍后再试"}
	default:
		return &ToolError{Code: "UPDATE_START_FAILED", Message: "无法启动修订"}
	}
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
		if value.OpenAPI == nil {
			return OperationsView{}, &ToolError{Code: "APP_IN_ERROR", Message: "应用处于错误状态"}
		}
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

// StartApp starts the application's Render instance and returns its stream
// address. It never contacts a casting service: which screen shows the stream
// is decided on site by the device's own casting tools, so a running
// application may hold no screen at all.
func (s *Service) StartApp(ctx context.Context, owner app.Identity, appID string) (StatusView, error) {
	unlock := s.lifecycle.Lock(ownerKey(owner))
	defer unlock()
	entry, err := s.entry(owner, appID)
	if err != nil {
		return StatusView{}, err
	}
	entry.Begin()
	defer entry.End()
	value := entry.Snapshot()
	if !value.Owner.SameOwner(owner) {
		return StatusView{}, &ToolError{Code: "APP_NOT_FOUND", Message: "应用归属已改变"}
	}
	switch value.State {
	case app.StateRunning:
		// The runtime is already up. A binding that is temporarily unavailable
		// must not turn a repeated start into an error.
		rt, err := s.runtimeFor(ctx, value.Owner)
		if err != nil {
			s.logf("service: verify running app %s without binding: %v", value.ID, err)
			return s.statusView(value), nil
		}
		if err := s.verifyRunningWith(ctx, rt, value); err != nil {
			return StatusView{}, err
		}
		return s.statusView(value), nil
	case app.StateBuilding:
		return StatusView{}, &ToolError{Code: "APP_BUILDING", Message: "应用仍在构建"}
	case app.StateError:
		if !retryable(value) {
			return StatusView{}, &ToolError{Code: "APP_IN_ERROR", Message: "应用处于错误状态"}
		}
		// Built fine, failed later: release whatever the failed start left behind
		// and start again from idle.
		if err := s.releaseFailedStart(ctx, entry); err != nil {
			return StatusView{}, err
		}
		value = entry.Snapshot()
	case app.StateIdle:
	default:
		return StatusView{}, &ToolError{Code: "INVALID_STATE", Message: "应用状态不允许启动"}
	}
	if s.artifacts != nil && !s.artifacts.Has(value.ID) {
		return StatusView{}, &ToolError{Code: "ARTIFACT_MISSING", Message: "应用包已丢失，请重新创建应用"}
	}
	rt, err := s.runtimeFor(ctx, owner)
	if err != nil {
		return StatusView{}, err
	}
	// failStart records a start that never got as far as a running application.
	// The application is marked before reconciliation so a crash between the
	// two never leaves it looking idle with reservations behind.
	failStart := func(failure *startFailure) (StatusView, error) {
		if failure.Code == "APP_ALREADY_RUNNING" {
			if err := persistRuntime(entry, func(a *app.App) { a.PendingRuntime = false; a.CleanupPending = false }); err != nil {
				return StatusView{}, err
			}
			return StatusView{}, failure.toolError()
		}
		entry.Update(func(a *app.App) {
			if a.State != app.StateIdle {
				return
			}
			a.State = app.StateError
			a.Error = &app.Error{Stage: app.StageStarting, Code: failure.Code, Message: failure.Message}
			a.CleanupPending = true
		})
		_ = s.reconcileWith(ctx, rt, entry)
		s.notifyError(entry.Snapshot())
		return StatusView{}, failure.toolError()
	}
	started, failure := s.startRender(ctx, rt, value)
	if failure != nil {
		return failStart(failure)
	}
	if err := persistRuntime(entry, func(a *app.App) {
		if a.State != app.StateIdle {
			return
		}
		a.State = app.StateRunning
		a.PendingRuntime = false
		a.CleanupPending = false
		a.InstanceID = started.instance.InstanceID
		a.Port = started.port
		a.AppBaseURL = started.instance.AppBaseURL
		a.Source = started.source
		a.StartedAt = s.now()
		a.StoppedAt = time.Time{}
		a.Error = nil
	}); err != nil {
		// The downstream IDs were durably stored before this final transition.
		// Retain them and let the monitor retry cleanup after storage recovers.
		entry.Update(func(a *app.App) { a.State = app.StateError; a.CleanupPending = true })
		return StatusView{}, err
	}
	return s.statusView(entry.Snapshot()), nil
}

// StopApp stops the Render instance that runs the application.
func (s *Service) StopApp(ctx context.Context, owner app.Identity, appID string) (StatusView, error) {
	entry, err := s.entry(owner, appID)
	if err != nil {
		return StatusView{}, err
	}
	entry.Begin()
	defer entry.End()
	value := entry.Snapshot()
	if !value.Owner.SameOwner(owner) {
		return StatusView{}, &ToolError{Code: "APP_NOT_FOUND", Message: "应用归属已改变"}
	}
	switch value.State {
	case app.StateIdle:
		return s.statusView(value), nil
	case app.StateError:
		if retryable(value) {
			// Built fine, failed later: release what the failure left behind and
			// make the application startable again.
			if err := s.releaseFailedStart(ctx, entry); err != nil {
				return StatusView{}, err
			}
			return s.statusView(entry.Snapshot()), nil
		}
		if err := s.reconcile(ctx, entry); err != nil {
			return StatusView{}, err
		}
		return s.statusView(entry.Snapshot()), nil
	case app.StateBuilding:
		return StatusView{}, &ToolError{Code: "APP_BUILDING", Message: "应用仍在构建"}
	}
	rt, err := s.runtimeFor(ctx, owner)
	if err != nil {
		return StatusView{}, err
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if value.InstanceID != "" {
		if stopped, err := rt.render.Delete(cleanupCtx, value.InstanceID); (err != nil && !render.IsStatus(err, http.StatusNotFound)) || (err == nil && stopped.State != render.StateStopped) {
			return StatusView{}, &ToolError{Code: "STOP_FAILED", Message: "停止应用失败"}
		}
	}
	if err := persistRuntime(entry, func(a *app.App) {
		if a.State != app.StateRunning {
			return
		}
		a.State = app.StateIdle
		a.StoppedAt = s.now()
		a.InstanceID = ""
		a.Port = 0
		a.AppBaseURL = ""
		a.Source = casting.StreamSource{}
		a.Error = nil
	}); err != nil {
		return StatusView{}, err
	}
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
	if !value.Owner.SameOwner(owner) {
		return OperationResult{}, &ToolError{Code: "APP_NOT_FOUND", Message: "应用归属已改变"}
	}
	rt, err := s.runtimeFor(ctx, owner)
	if err != nil {
		return OperationResult{}, err
	}
	if err = s.verifyRunningWith(ctx, rt, value); err != nil {
		return OperationResult{}, err
	}
	if value.State != app.StateRunning || value.AppBaseURL == "" {
		return OperationResult{}, &ToolError{Code: "APP_NOT_RUNNING", Message: "应用未在运行"}
	}
	if _, ok := value.OpenAPI.Lookup(operationID); !ok {
		return OperationResult{}, &ToolError{Code: "OPERATION_NOT_FOUND", Message: "应用没有该操作"}
	}
	// AppBaseURL comes from the owner's Render service. It must not become a
	// way to reach the deployment network even when Render reports a private
	// address.
	if s.validateURL != nil {
		if err := s.validateURL(ctx, value.AppBaseURL); err != nil {
			code := netguard.CodeOf(err)
			if code == "" {
				code = "INVALID_TARGET_URL"
			}
			return OperationResult{}, &ToolError{Code: code, Message: "应用地址不可访问"}
		}
	}
	out, err := value.OpenAPI.Execute(ctx, value.AppBaseURL, operationID, parameters, body, s.http)
	if err != nil {
		// The executor deliberately drops transport details for callers. Map
		// the stable failure classes to the public error codes without echoing
		// dial errors or internal addresses.
		code := netguard.CodeOf(err)
		message := "应用请求失败"
		if code == "" {
			var unsupported *json.UnsupportedTypeError
			var unsupportedValue *json.UnsupportedValueError
			var apiErr *httpapi.Error
			switch {
			case errors.As(err, &unsupported), errors.As(err, &unsupportedValue):
				code, message = "INVALID_ARGUMENT", "应用请求参数无效"
			case errors.As(err, &apiErr):
				switch apiErr.Code {
				case "TIMEOUT", "UPSTREAM_ERROR", "INVALID_RESPONSE", "RESPONSE_TOO_LARGE", "OPENAPI_UNAVAILABLE":
					code = "TARGET_UNREACHABLE"
				case "TARGET_NOT_ALLOWED":
					code = "INVALID_TARGET_URL"
				default:
					code, message = "INVALID_ARGUMENT", "应用请求参数无效"
				}
			default:
				code = "TARGET_UNREACHABLE"
			}
		}
		return OperationResult{}, &ToolError{Code: code, Message: message}
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
		if value.State == app.StateError && (value.CleanupPending || value.PendingRuntime || value.InstanceID != "") {
			rt, err := s.runtimeFor(ctx, value.Owner)
			if err != nil {
				s.logf("monitor %s: resolve services: %v", value.ID, err)
				continue
			}
			if record, ok := s.apps.Get(value.ID); ok {
				record.Begin()
				_ = s.reconcileWith(ctx, rt, record)
				record.End()
			}
			continue
		}
		if value.State != app.StateRunning {
			continue
		}
		rt, err := s.runtimeFor(ctx, value.Owner)
		if err != nil {
			s.logf("monitor %s: resolve services: %v", value.ID, err)
			continue
		}
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		if value.InstanceID != "" {
			instance, err := rt.render.Get(checkCtx, value.InstanceID)
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
		AppID:   snapshot.ID,
		State:   app.StateError,
		Message: message,
		Error:   snapshot.Error,
	})
}

type startFailure struct {
	Code    string
	Message string
	Cause   error
}

func (f *startFailure) toolError() error {
	return &ToolError{Code: f.Code, Message: f.Message}
}

// renderStart is a started runtime, before any screen is involved.
type renderStart struct {
	instance render.App
	port     int
	source   casting.StreamSource
}

// startRender starts the application's Render instance and returns the stream
// it serves. It touches no casting service: projection is a separate step that
// only the frozen on-screen path performs.
func (s *Service) startRender(ctx context.Context, rt runtime, value app.App) (renderStart, *startFailure) {
	persistFailure := func(err error) (renderStart, *startFailure) {
		return renderStart{}, &startFailure{Code: "PERSISTENCE_FAILED", Message: "无法保存运行状态，停止后续操作", Cause: err}
	}
	if value.Artifact.DownloadURL == "" {
		return renderStart{}, &startFailure{Code: "ARTIFACT_INVALID", Message: "应用产物不可用，请修订后重新构建"}
	}
	startCtx, cancel := context.WithTimeout(ctx, s.cfg.StartTimeout)
	defer cancel()
	excluded := make(map[int]bool)
	var instance render.App
	var port int
	var createErr error
	for attempt := 0; attempt < 3; attempt++ {
		port, createErr = s.choosePortWith(startCtx, rt, excluded)
		if createErr != nil {
			break
		}
		if err := s.updateRuntime(value.ID, func(a *app.App) { a.Port = port; a.PendingRuntime = true }); err != nil {
			return persistFailure(err)
		}
		instance, createErr = rt.render.Create(startCtx, render.CreateRequest{
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
			if err := s.updateRuntime(value.ID, func(a *app.App) {
				a.InstanceID = instance.InstanceID
				a.PendingRuntime = false
				a.AppBaseURL = instance.AppBaseURL
				a.Source = instance.Source
			}); err != nil {
				return persistFailure(err)
			}
			break
		}
		if render.IsCode(createErr, "PORT_IN_USE") || render.IsCode(createErr, "INVALID_REQUEST") || render.IsCode(createErr, "APP_ALREADY_RUNNING") || render.IsInvalidArtifact(createErr) {
			if err := s.updateRuntime(value.ID, func(a *app.App) { a.PendingRuntime = false }); err != nil {
				return persistFailure(err)
			}
		}
		if render.IsCode(createErr, "PORT_IN_USE") {
			excluded[port] = true
			continue
		}
		break
	}
	if createErr != nil {
		if render.IsCode(createErr, "APP_ALREADY_RUNNING") {
			return renderStart{}, &startFailure{Code: "APP_ALREADY_RUNNING", Message: "应用已有运行实例", Cause: createErr}
		}
		if render.IsInvalidArtifact(createErr) {
			return renderStart{}, &startFailure{Code: "ARTIFACT_INVALID", Message: "应用产物无效，请修订后重新构建", Cause: createErr}
		}
		return renderStart{}, &startFailure{Code: "START_FAILED", Message: "应用启动失败", Cause: createErr}
	}
	if instance.State != render.StateReady {
		if instance.Error != nil && instance.Error.Code == render.CodeInvalidArtifact {
			return renderStart{}, &startFailure{Code: "ARTIFACT_INVALID", Message: "应用产物无效，请修订后重新构建"}
		}
		// Cleanup uses the persisted application record.
		return renderStart{}, &startFailure{Code: "START_FAILED", Message: "应用未就绪"}
	}
	source := instance.Source
	if source.Type == "" || source.URL == "" {
		// Cleanup uses the persisted application record.
		return renderStart{}, &startFailure{Code: "STREAM_START_FAILED", Message: "应用媒体流未就绪"}
	}
	return renderStart{instance: instance, port: port, source: source}, nil
}

func (s *Service) choosePort(ctx context.Context, excluded map[int]bool) (int, error) {
	if s.render == nil {
		return 0, &ToolError{Code: "SERVICE_NOT_CONFIGURED", Message: "尚未配置渲染服务"}
	}
	return s.choosePortWith(ctx, runtime{render: s.render}, excluded)
}

func (s *Service) choosePortWith(ctx context.Context, rt runtime, excluded map[int]bool) (int, error) {
	ports, err := rt.render.ListPorts(ctx)
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
		info, err := rt.render.GetPort(ctx, port)
		if err != nil {
			return 0, err
		}
		if !info.Occupied {
			return port, nil
		}
	}
	return 0, errors.New("no free port")
}

func (s *Service) entry(owner app.Identity, appID string) (*app.Record, error) {
	entry, ok := s.apps.Get(appID)
	if !ok {
		return nil, &ToolError{Code: "APP_NOT_FOUND", Message: "应用不存在"}
	}
	if !entry.Snapshot().Owner.SameOwner(owner) {
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
		if !value.StoppedAt.IsZero() {
			stopped := value.StoppedAt.UTC().Truncate(time.Second)
			view.StoppedAt = &stopped
		}
	case app.StateRunning:
		view.Name = value.Name
		view.StreamURL = value.Source.URL
		if !value.StartedAt.IsZero() {
			started := value.StartedAt.UTC().Truncate(time.Second)
			view.StartedAt = &started
			view.UptimeSeconds = int64(s.now().Sub(value.StartedAt).Seconds())
			if view.UptimeSeconds < 0 {
				view.UptimeSeconds = 0
			}
		}
	case app.StateError:
		view.Error = value.Error
		if view.Error == nil {
			view.Error = &app.Error{Stage: app.StageRunning, Code: "APP_EXITED"}
		}
	}
	return view
}

func (s *Service) notifyError(value app.App) {
	s.notifier.Notify(value.Owner, app.Notification{
		AppID: value.ID,
		State: app.StateError,
		Error: value.Error,
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
	if a.OpenAPI == nil {
		return nil, &ToolError{Code: "APP_IN_ERROR", Message: "应用没有可用的 OpenAPI 文档"}
	}
	return a.OpenAPI.Document()
}

func (s *Service) verifyRunning(ctx context.Context, a app.App) error {
	rt, err := s.runtimeFor(ctx, a.Owner)
	if err != nil {
		return err
	}
	return s.verifyRunningWith(ctx, rt, a)
}

func (s *Service) verifyRunningWith(ctx context.Context, rt runtime, a app.App) error {
	if a.State != app.StateRunning {
		return nil
	}
	if a.InstanceID != "" {
		r, err := rt.render.Get(ctx, a.InstanceID)
		if err != nil {
			return &ToolError{Code: "RUNTIME_UNAVAILABLE", Message: "无法确认应用状态"}
		}
		if r.State != render.StateReady {
			return &ToolError{Code: "APP_NOT_RUNNING", Message: "应用未就绪"}
		}
	}
	return nil
}
