package portal

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/endpoints"
	"github.com/open-anywhere/open-anywhere/internal/gateway/service"
	"github.com/open-anywhere/open-anywhere/internal/gateway/settings"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

// Request limits keep a hostile browser from allocating unbounded bodies.
const maxJSONBody = 1 << 20

// screenService is the owner-aware screen surface the portal needs. It is an
// interface so HTTP tests can substitute a fake without a casting service.
type screenService interface {
	GetScreensFor(ctx context.Context, owners []app.Identity) ([]service.ScreenView, error)
	StopAppOnScreen(ctx context.Context, owners []app.Identity, screenID string) (service.StatusView, error)
}

// API serves /api/portal. Every handler below the auth boundary receives the
// session user; ownership is checked again when a device or app is touched.
type API struct {
	auth      *Auth
	db        *store.DB
	apps      *service.Service
	screens   screenService
	voice     *VoiceManager
	provision *Provisioner
	web       http.Handler
	settings  *settings.Provider
	pool      *endpoints.Pool
	resolver  *endpoints.Resolver
	// Service and platform connection tests are rate limited per account so the
	// portal cannot be used as a network scanner.
	serviceTest *windowLimiter
	adminTest   *windowLimiter
	now         func() time.Time
	logf        func(string, ...any)
}

// NewAPI wires the portal HTTP surface.
func NewAPI(db *store.DB, auth *Auth, apps *service.Service, voice *VoiceManager, provision *Provisioner, webDir string) *API {
	api := &API{
		auth:      auth,
		db:        db,
		apps:      apps,
		voice:     voice,
		provision: provision,
		web:       NewRootSPA(webDir),
		now:       func() time.Time { return time.Now().UTC() },
		logf:      log.Printf,
	}
	if apps != nil {
		api.screens = apps
	}
	api.serviceTest = newWindowLimiter(api.now, time.Minute, 10)
	api.adminTest = newWindowLimiter(api.now, time.Minute, 20)
	return api
}

// SetPlatform installs the settings provider, client pool and binding resolver
// used by the service-binding, registration and admin endpoints.
func (a *API) SetPlatform(provider *settings.Provider, pool *endpoints.Pool, resolver *endpoints.Resolver) {
	a.settings = provider
	a.pool = pool
	a.resolver = resolver
}

// Handler returns the portal API. It is mounted at /api/portal/; the root
// site and /portal pages are served by WebHandler.
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/portal/register", a.register)
	mux.HandleFunc("POST /api/portal/login", a.login)
	mux.HandleFunc("POST /api/portal/logout", a.logout)
	mux.HandleFunc("GET /api/portal/me", a.withUser(a.me))
	mux.HandleFunc("GET /api/portal/devices", a.withUser(a.listDevices))
	mux.HandleFunc("POST /api/portal/devices", a.withUser(a.createDevice))
	mux.HandleFunc("GET /api/portal/devices/{id}", a.withUser(a.getDevice))
	mux.HandleFunc("DELETE /api/portal/devices/{id}", a.withUser(a.deleteDevice))
	mux.HandleFunc("GET /api/portal/devices/{id}/agent", a.withUser(a.getDeviceAgent))
	mux.HandleFunc("PUT /api/portal/devices/{id}/agent", a.withUser(a.updateDeviceAgent))
	mux.HandleFunc("GET /api/portal/devices/{id}/mcps", a.withUser(a.getDeviceMCPs))
	mux.HandleFunc("PUT /api/portal/devices/{id}/mcps", a.withUser(a.setDeviceMCPs))
	mux.HandleFunc("GET /api/portal/agent-options", a.withUser(a.listAgentOptions))
	mux.HandleFunc("POST /api/portal/devices/{id}/retry", a.withUser(a.retryDevice))
	mux.HandleFunc("POST /api/portal/devices/{id}/disconnect", a.withUser(a.disconnectDevice))
	mux.HandleFunc("POST /api/portal/devices/{id}/reconnect", a.withUser(a.reconnectDevice))
	mux.HandleFunc("GET /api/portal/screens", a.withUser(a.listScreens))
	mux.HandleFunc("POST /api/portal/screens/{screen_id}/stop", a.withUser(a.stopScreen))
	mux.HandleFunc("GET /api/portal/services", a.withUser(a.getServices))
	mux.HandleFunc("PUT /api/portal/services", a.withUser(a.updateServices))
	mux.HandleFunc("POST /api/portal/services/test", a.withUser(a.testServices))
	mux.HandleFunc("GET /api/portal/apps", a.withUser(a.listApps))
	mux.HandleFunc("GET /api/portal/apps/{id}", a.withUser(a.getApp))
	mux.HandleFunc("GET /api/portal/apps/{id}/openapi.yaml", a.withUser(a.getOpenAPI))
	mux.HandleFunc("DELETE /api/portal/apps/{id}", a.withUser(a.deleteApp))
	mux.Handle("/api/portal/", http.NotFoundHandler())
	return a.guard(mux)
}

// WebHandler returns the single-page application for /, /login and /portal.
func (a *API) WebHandler() http.Handler {
	return a.web
}

type authedHandler func(http.ResponseWriter, *http.Request, store.PortalUser)

func (a *API) withUser(next authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := a.auth.Authenticate(r.Context(), CookieToken(r))
		if err != nil {
			writeError(w, err)
			return
		}
		next(w, r, user)
	}
}

// guard rejects cross-site writes. Writes must be JSON: a form or <img> cannot
// send this content type without a CORS preflight, which the portal never
// answers.
func (a *API) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if origin := r.Header.Get("Origin"); origin != "" {
				if !sameOrigin(origin, r.Host) {
					writeError(w, errForbidden)
					return
				}
			}
			if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
				writeError(w, apiError(http.StatusUnsupportedMediaType, "INVALID_CONTENT_TYPE", "请求体必须是 application/json"))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func sameOrigin(origin, host string) bool {
	origin = strings.TrimRight(origin, "/")
	if origin == "http://"+host || origin == "https://"+host {
		return true
	}
	// Origins only carry scheme, host and port; a configured reverse proxy host
	// is compared as a plain string above.
	return false
}

func (a *API) register(w http.ResponseWriter, r *http.Request) {
	if a.settings == nil {
		writeError(w, apiError(http.StatusForbidden, "REGISTRATION_CLOSED", "注册暂未开放"))
		return
	}
	open, err := a.settings.RegistrationOpen(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	if !open {
		writeError(w, apiError(http.StatusForbidden, "REGISTRATION_CLOSED", "注册暂未开放"))
		return
	}
	var in Credentials
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, err)
		return
	}
	result, err := a.auth.Register(r.Context(), in, r.UserAgent(), ClientIP(r))
	if err != nil {
		writeError(w, err)
		return
	}
	a.auth.SetCookie(w, result.Token)
	if a.provision != nil {
		a.provision.Start(result.Device.ID)
	}
	a.logf("portal: registered user %s (device %s)", result.User.ID, result.Device.ID)
	writeJSON(w, http.StatusCreated, map[string]any{
		"user":   userView(result.User),
		"device": a.deviceView(result.Device),
	})
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var in Credentials
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, err)
		return
	}
	user, token, err := a.auth.Login(r.Context(), in, r.UserAgent(), ClientIP(r))
	if err != nil {
		writeError(w, err)
		return
	}
	a.auth.SetCookie(w, token)
	writeJSON(w, http.StatusOK, map[string]any{"user": userView(user)})
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	if token := CookieToken(r); token != "" {
		if err := a.auth.Logout(r.Context(), token); err != nil {
			a.logf("portal: logout: %v", err)
		}
	}
	a.auth.ClearCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) me(w http.ResponseWriter, r *http.Request, user store.PortalUser) {
	devices, err := a.deviceViews(r.Context(), user.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": userView(user), "devices": devices})
}

func (a *API) listDevices(w http.ResponseWriter, r *http.Request, user store.PortalUser) {
	devices, err := a.deviceViews(r.Context(), user.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": devices})
}

func (a *API) getDevice(w http.ResponseWriter, r *http.Request, user store.PortalUser) {
	device, err := a.ownedDevice(r, user, r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"device": a.deviceView(device)})
}

func (a *API) retryDevice(w http.ResponseWriter, r *http.Request, user store.PortalUser) {
	device, err := a.ownedDevice(r, user, r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if device.State == store.PortalDeviceRegistered {
		// The platform side is complete; retrying here restarts a voice runtime
		// that exited after registration. A healthy runtime is left untouched.
		if a.provision != nil {
			if err := a.voice.Start(a.provision.withCredential(device)); err != nil {
				a.logf("portal: restart voice for %s: %v", device.ID, err)
				writeError(w, apiError(http.StatusServiceUnavailable, "VOICE_UNAVAILABLE", "语音运行时启动失败，请稍后重试"))
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"device": a.deviceView(device)})
		return
	}
	if a.provision.Running(device.ID) {
		writeError(w, apiError(http.StatusConflict, "DEVICE_BUSY", "设备正在开通中"))
		return
	}
	device.State = store.PortalDeviceProvisioning
	device.Error = ""
	device.UpdatedAt = a.now()
	if err := a.db.UpdatePortalDevice(r.Context(), device); err != nil {
		writeError(w, err)
		return
	}
	a.provision.Start(device.ID)
	writeJSON(w, http.StatusAccepted, map[string]any{"device": a.deviceView(device)})
}

// voiceDevice loads a device the user owns and requires it to be a registered
// virtual device, the only kind that has a voice runtime to control.
func (a *API) voiceDevice(r *http.Request, user store.PortalUser) (store.PortalDevice, error) {
	device, err := a.ownedDevice(r, user, r.PathValue("id"))
	if err != nil {
		return store.PortalDevice{}, err
	}
	if device.Kind != store.PortalDeviceVirtual || device.State != store.PortalDeviceRegistered {
		return store.PortalDevice{}, apiError(http.StatusConflict, "DEVICE_NOT_READY", "设备尚未完成注册")
	}
	return device, nil
}

// disconnectDevice drops the device's connection to Linx. The device stays
// offline, with its registration and credentials intact, until the user
// reconnects it or the gateway restarts.
func (a *API) disconnectDevice(w http.ResponseWriter, r *http.Request, user store.PortalUser) {
	device, err := a.voiceDevice(r, user)
	if err != nil {
		writeError(w, err)
		return
	}
	if a.voice == nil {
		writeError(w, apiError(http.StatusServiceUnavailable, "VOICE_UNAVAILABLE", "语音运行时还未就绪，请稍后重试"))
		return
	}
	a.voice.Stop(device.ID)
	writeJSON(w, http.StatusOK, map[string]any{"device": a.deviceView(device)})
}

// reconnectDevice replaces the device's connection with a fresh one, whether or
// not the current connection is healthy.
func (a *API) reconnectDevice(w http.ResponseWriter, r *http.Request, user store.PortalUser) {
	device, err := a.voiceDevice(r, user)
	if err != nil {
		writeError(w, err)
		return
	}
	if a.voice == nil || a.provision == nil {
		writeError(w, apiError(http.StatusServiceUnavailable, "VOICE_UNAVAILABLE", "语音运行时还未就绪，请稍后重试"))
		return
	}
	if err := a.voice.Restart(a.provision.withCredential(device)); err != nil {
		a.logf("portal: reconnect voice for %s: %v", device.ID, err)
		writeError(w, apiError(http.StatusServiceUnavailable, "VOICE_UNAVAILABLE", "语音运行时启动失败，请稍后重试"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"device": a.deviceView(device)})
}

func (a *API) listScreens(w http.ResponseWriter, r *http.Request, user store.PortalUser) {
	if a.screens == nil {
		writeError(w, apiError(http.StatusServiceUnavailable, "SCREEN_DISCOVERY_FAILED", "屏幕服务不可用"))
		return
	}
	identities, err := a.ownedIdentities(r.Context(), user.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	views, err := a.screens.GetScreensFor(r.Context(), ownerIdentities(identities))
	if err != nil {
		writeError(w, mapServiceError(err))
		return
	}
	out := make([]ScreenView, 0, len(views))
	for _, view := range views {
		out = append(out, ScreenView{
			ScreenID:   view.ScreenID,
			Name:       view.Name,
			Online:     view.Online,
			Status:     view.Status,
			CurrentApp: view.CurrentApp,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"screens": out})
}

func (a *API) stopScreen(w http.ResponseWriter, r *http.Request, user store.PortalUser) {
	if a.screens == nil {
		writeError(w, apiError(http.StatusServiceUnavailable, "SCREEN_DISCOVERY_FAILED", "屏幕服务不可用"))
		return
	}
	identities, err := a.ownedIdentities(r.Context(), user.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	screenID := r.PathValue("screen_id")
	status, err := a.screens.StopAppOnScreen(r.Context(), ownerIdentities(identities), screenID)
	if err != nil {
		writeError(w, mapServiceError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"screen_id": screenID,
		"app":       map[string]any{"app_id": status.AppID, "state": status.State},
	})
}

func (a *API) listApps(w http.ResponseWriter, r *http.Request, user store.PortalUser) {
	items, err := a.appItems(r.Context(), user)
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]AppView, 0, len(items))
	for _, item := range items {
		out = append(out, item.AppView)
	}
	writeJSON(w, http.StatusOK, map[string]any{"apps": out})
}

func (a *API) getApp(w http.ResponseWriter, r *http.Request, user store.PortalUser) {
	item, err := a.ownedApp(r, user, r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	status, err := a.apps.GetAppStatus(r.Context(), item.Identity, item.AppID)
	if err != nil {
		writeError(w, mapServiceError(err))
		return
	}
	operations := []service.OperationView{}
	if view, err := a.apps.GetAppOperations(r.Context(), item.Identity, item.AppID); err == nil {
		operations = view.Operations
	} else if code := toolCode(err); code != "APP_BUILDING" && code != "APP_IN_ERROR" {
		writeError(w, mapServiceError(err))
		return
	}
	detail := AppDetailView{AppView: item.AppView, Operations: operations}
	detail.Message = status.Message
	if status.ScreenID != "" {
		detail.ScreenID = status.ScreenID
	}
	if status.StartedAt != nil {
		detail.StartedAt = status.StartedAt
	}
	if status.StoppedAt != nil {
		detail.StoppedAt = status.StoppedAt
	}
	detail.UptimeSeconds = status.UptimeSeconds
	if detail.State == "" {
		detail.State = status.State
	}
	if _, err := a.apps.GetOpenAPI(r.Context(), item.Identity, item.AppID); err == nil {
		detail.OpenAPIAvailable = true
	}
	writeJSON(w, http.StatusOK, map[string]any{"app": detail})
}

func (a *API) getOpenAPI(w http.ResponseWriter, r *http.Request, user store.PortalUser) {
	item, err := a.ownedApp(r, user, r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	document, err := a.apps.GetOpenAPI(r.Context(), item.Identity, item.AppID)
	if err != nil {
		writeError(w, mapServiceError(err))
		return
	}
	encoded, err := marshalYAML(document)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Query().Get("download") != "" {
		w.Header().Set("Content-Disposition", `attachment; filename="openapi.yaml"`)
	}
	_, _ = w.Write(encoded)
}

func (a *API) deleteApp(w http.ResponseWriter, r *http.Request, user store.PortalUser) {
	item, err := a.ownedApp(r, user, r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if err := a.apps.DeleteApp(r.Context(), item.Identity, item.AppID); err != nil {
		writeError(w, mapServiceError(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// VoiceHandler mounts the per-device voice runtime control API under
// /voice/{device_id}/. It authenticates the session cookie, checks device
// ownership, exposes only the endpoints the portal call page uses, and then
// strips the prefix. The control API's own same-origin check remains in place.
func (a *API) VoiceHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/voice/")
		deviceID, tail, found := strings.Cut(rest, "/")
		if !found || deviceID == "" {
			http.NotFound(w, r)
			return
		}
		user, err := a.auth.Authenticate(r.Context(), CookieToken(r))
		if err != nil {
			writeError(w, err)
			return
		}
		device, err := a.ownedDevice(r, user, deviceID)
		if err != nil {
			writeError(w, err)
			return
		}
		if device.Kind != store.PortalDeviceVirtual || device.State != store.PortalDeviceRegistered {
			writeError(w, apiError(http.StatusConflict, "DEVICE_NOT_READY", "设备尚未完成注册"))
			return
		}
		// The gateway exposes only the control endpoints the portal call page
		// needs. The debug console, device registration and connection controls
		// remain available when voiceentry runs standalone.
		switch tail {
		case "status", "events", "audio", "playback", "pause", "resume", "abort":
		default:
			http.NotFound(w, r)
			return
		}
		handler, ok := a.voice.Handler(deviceID)
		if !ok {
			writeError(w, apiError(http.StatusServiceUnavailable, "VOICE_UNAVAILABLE", "语音运行时还未就绪，请稍后重试"))
			return
		}
		http.StripPrefix("/voice/"+deviceID, handler).ServeHTTP(w, r)
	})
}

// ownedDevice loads a device and requires it to belong to user.
func (a *API) ownedDevice(r *http.Request, user store.PortalUser, id string) (store.PortalDevice, error) {
	device, err := a.db.PortalDeviceByID(r.Context(), id)
	if err != nil {
		return store.PortalDevice{}, errNotFound
	}
	if device.UserID != user.ID {
		return store.PortalDevice{}, errNotFound
	}
	return device, nil
}

type ownedAppItem struct {
	AppView
	Identity app.Identity
}

func (a *API) ownedApp(r *http.Request, user store.PortalUser, appID string) (ownedAppItem, error) {
	items, err := a.appItems(r.Context(), user)
	if err != nil {
		return ownedAppItem{}, err
	}
	for _, item := range items {
		if item.AppID == appID {
			return item, nil
		}
	}
	return ownedAppItem{}, apiError(http.StatusNotFound, "APP_NOT_FOUND", "应用不存在")
}

func (a *API) deviceViews(ctx context.Context, userID string) ([]DeviceView, error) {
	devices, err := a.db.PortalDevicesByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]DeviceView, 0, len(devices))
	for _, device := range devices {
		out = append(out, a.deviceView(device))
	}
	return out, nil
}

func (a *API) deviceView(device store.PortalDevice) DeviceView {
	view := DeviceView{
		ID:           device.ID,
		Kind:         device.Kind,
		MAC:          device.MAC,
		ClientID:     device.ClientID,
		AgentID:      device.LinxAgentID,
		AgentName:    device.AgentName,
		Name:         device.Name,
		State:        device.State,
		Step:         device.Step,
		Error:        device.Error,
		CreatedAt:    device.CreatedAt,
		UpdatedAt:    device.UpdatedAt,
		RegisteredAt: device.RegisteredAt,
		Voice:        a.voice.Status(device.ID),
	}
	// The provisioner stores a stable code when the platform is incomplete so
	// clients can react to it; the localized text is a presentation concern.
	if device.Error == deviceErrorCodePlatformNotConfigured {
		view.ErrorCode = deviceErrorCodePlatformNotConfigured
		view.Error = "平台尚未配置，请联系维护者"
	}
	return view
}

func decodeJSON(w http.ResponseWriter, r *http.Request, out any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody))
	if err := decoder.Decode(out); err != nil {
		return invalidArgument("请求体不是有效的 JSON")
	}
	return nil
}

func userView(user store.PortalUser) map[string]any {
	return map[string]any{
		"id":            user.ID,
		"username":      user.Username,
		"role":          user.Role,
		"status":        user.Status,
		"created_at":    user.CreatedAt,
		"last_login_at": user.LastLoginAt,
	}
}

func mapServiceError(err error) error {
	code := toolCode(err)
	switch code {
	case "APP_NOT_FOUND":
		return apiError(http.StatusNotFound, "APP_NOT_FOUND", "应用不存在")
	case "APP_BUILDING":
		return apiError(http.StatusConflict, "APP_BUILDING", "应用仍在构建")
	case "APP_ALREADY_RUNNING":
		return apiError(http.StatusConflict, "APP_ALREADY_RUNNING", "应用已在屏幕上运行")
	case "APP_NOT_RUNNING":
		return apiError(http.StatusConflict, "APP_NOT_RUNNING", "应用未在运行")
	case "CLEANUP_PENDING":
		return apiError(http.StatusConflict, "CLEANUP_PENDING", "上次启动的资源还在清理，请稍后重试")
	case "APP_BUSY":
		return apiError(http.StatusConflict, "APP_BUSY", "应用正在构建或屏幕上运行，暂时不能删除")
	case "APP_IN_ERROR":
		return apiError(http.StatusConflict, "APP_IN_ERROR", "应用处于错误状态")
	case "SCREEN_APP_NOT_FOUND":
		return apiError(http.StatusNotFound, "SCREEN_APP_NOT_FOUND", "屏幕上没有你的应用")
	case "SCREEN_DISCOVERY_FAILED":
		return apiError(http.StatusServiceUnavailable, "SCREEN_DISCOVERY_FAILED", "无法获取屏幕列表")
	case "SERVICE_NOT_CONFIGURED":
		return apiError(http.StatusConflict, "SERVICE_NOT_CONFIGURED", "还没有可用的屏幕服务，请先完成服务绑定")
	case "PLATFORM_NOT_CONFIGURED":
		return apiError(http.StatusConflict, "PLATFORM_NOT_CONFIGURED", "平台服务尚未配置，请联系管理员")
	case "TARGET_BUSY":
		return apiError(http.StatusConflict, "TARGET_BUSY", "屏幕上已经有应用在运行")
	case "INVALID_TARGET_URL":
		return apiError(http.StatusBadRequest, "INVALID_TARGET_URL", "应用地址无效")
	case "PRIVATE_TARGET_FORBIDDEN":
		return apiError(http.StatusBadRequest, "PRIVATE_TARGET_FORBIDDEN", "不允许访问内网地址")
	case "TARGET_UNREACHABLE":
		return apiError(http.StatusBadGateway, "TARGET_UNREACHABLE", "无法访问应用地址")
	case "STOP_FAILED":
		return apiError(http.StatusBadGateway, "STOP_FAILED", "停止投屏失败")
	case "OPERATION_NOT_FOUND":
		return apiError(http.StatusNotFound, "OPERATION_NOT_FOUND", "应用没有这个操作")
	case "INVALID_ARGUMENT":
		return apiError(http.StatusBadRequest, "INVALID_ARGUMENT", "请求参数无效")
	case "INVALID_STATE":
		return apiError(http.StatusConflict, "INVALID_STATE", "应用当前状态不允许该操作")
	case "PROJECTION_UNAVAILABLE", "PROJECTION_TIMEOUT":
		return apiError(http.StatusBadGateway, code, "投屏失败，请稍后重试")
	case "RUNTIME_UNAVAILABLE":
		return apiError(http.StatusBadGateway, "RUNTIME_UNAVAILABLE", "应用运行时不可用")
	case "START_FAILED", "BUILD_FAILED", "BUILD_START_FAILED":
		return apiError(http.StatusBadGateway, code, "应用启动失败")
	case "":
		return err
	default:
		// Unknown service errors may carry internal detail. Do not echo their
		// code or message to the client.
		return apiError(http.StatusInternalServerError, "INTERNAL", "服务暂时不可用")
	}
}

func toolCode(err error) string {
	var toolErr *service.ToolError
	if errors.As(err, &toolErr) {
		return toolErr.Code
	}
	return ""
}
