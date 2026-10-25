package portal

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/linx"
	"github.com/open-anywhere/open-anywhere/internal/gateway/service"
	"github.com/open-anywhere/open-anywhere/internal/gateway/settings"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

// AdminHandler returns the administrator API. Every route is authenticated and
// requires role=admin; responses never include screen, target, projection or
// prompt fields.
func (a *API) AdminHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/admin/overview", a.withAdmin(a.adminOverview))
	mux.HandleFunc("GET /api/admin/users", a.withAdmin(a.adminListUsers))
	mux.HandleFunc("GET /api/admin/users/{id}", a.withAdmin(a.adminGetUser))
	mux.HandleFunc("POST /api/admin/users/{id}/status", a.withAdmin(a.adminSetUserStatus))
	mux.HandleFunc("GET /api/admin/agents", a.withAdmin(a.adminListAgents))
	mux.HandleFunc("POST /api/admin/agents/{id}/retry", a.withAdmin(a.adminRetryAgent))
	mux.HandleFunc("GET /api/admin/apps", a.withAdmin(a.adminListApps))
	mux.HandleFunc("GET /api/admin/settings", a.withAdmin(a.adminGetSettings))
	mux.HandleFunc("PUT /api/admin/settings", a.withAdmin(a.adminUpdateSettings))
	mux.HandleFunc("POST /api/admin/settings/test", a.withAdmin(a.adminTestSettings))
	mux.HandleFunc("GET /api/admin/mcp", a.withAdmin(a.adminGetMCP))
	mux.HandleFunc("PUT /api/admin/mcp/shared", a.withAdmin(a.adminSetSharedMCP))
	mux.HandleFunc("POST /api/admin/mcp/sync", a.withAdmin(a.adminSyncMCP))
	mux.Handle("/api/admin/", http.NotFoundHandler())
	return a.securityHeaders(a.guard(mux))
}

func (a *API) withAdmin(next authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := a.auth.Authenticate(r.Context(), CookieToken(r))
		if err != nil {
			writeError(w, err)
			return
		}
		if user.Role != store.PortalRoleAdmin {
			writeError(w, apiError(http.StatusForbidden, "ADMIN_REQUIRED", "需要管理员权限"))
			return
		}
		next(w, r, user)
	}
}

type adminAgentView struct {
	ID           string     `json:"id"`
	UserID       string     `json:"user_id"`
	Username     string     `json:"username,omitempty"`
	Kind         string     `json:"kind"`
	MAC          string     `json:"mac"`
	AgentID      string     `json:"agent_id,omitempty"`
	AgentName    string     `json:"agent_name,omitempty"`
	State        string     `json:"state"`
	Step         string     `json:"step,omitempty"`
	Error        string     `json:"error,omitempty"`
	ErrorCode    string     `json:"error_code,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	RegisteredAt *time.Time `json:"registered_at,omitempty"`
}

func adminAgentViewFrom(device store.PortalDevice, username string) adminAgentView {
	view := adminAgentView{
		ID:           device.ID,
		UserID:       device.UserID,
		Username:     username,
		Kind:         device.Kind,
		MAC:          device.MAC,
		AgentID:      device.LinxAgentID,
		AgentName:    device.AgentName,
		State:        device.State,
		Step:         device.Step,
		Error:        device.Error,
		CreatedAt:    device.CreatedAt,
		UpdatedAt:    device.UpdatedAt,
		RegisteredAt: device.RegisteredAt,
	}
	if device.Error == deviceErrorCodePlatformNotConfigured {
		view.ErrorCode = deviceErrorCodePlatformNotConfigured
		view.Error = "平台尚未配置，请联系维护者"
	}
	return view
}

// adminAppView is deliberately smaller than AppView: it has no screen ID and
// no prompt-derived summary.
type adminAppView struct {
	AppID                string     `json:"app_id"`
	Name                 string     `json:"name,omitempty"`
	Title                string     `json:"title,omitempty"`
	State                app.State  `json:"state"`
	BuildStartedAt       *time.Time `json:"build_started_at,omitempty"`
	BuiltAt              *time.Time `json:"built_at,omitempty"`
	BuildDurationSeconds int64      `json:"build_duration_seconds,omitempty"`
	OperationCount       int        `json:"operation_count"`
	UserID               string     `json:"user_id,omitempty"`
	Username             string     `json:"username,omitempty"`
	DeviceID             string     `json:"device_id,omitempty"`
	AgentID              string     `json:"agent_id,omitempty"`
}

func adminAppViewFrom(summary service.AppSummary, device store.PortalDevice, owner app.Identity, username string) adminAppView {
	agentID := device.LinxAgentID
	if agentID == "" {
		agentID = owner.AgentID
	}
	userID := device.UserID
	return adminAppView{
		AppID:                summary.AppID,
		Name:                 summary.Name,
		Title:                summary.Title,
		State:                summary.State,
		BuildStartedAt:       summary.BuildStartedAt,
		BuiltAt:              summary.BuiltAt,
		BuildDurationSeconds: summary.BuildDurationSeconds,
		OperationCount:       summary.OperationCount,
		UserID:               userID,
		Username:             username,
		DeviceID:             device.ID,
		AgentID:              agentID,
	}
}

type adminUserView struct {
	ID          string           `json:"id"`
	Username    string           `json:"username"`
	Role        string           `json:"role"`
	Status      string           `json:"status"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
	LastLoginAt *time.Time       `json:"last_login_at,omitempty"`
	Agents      []adminAgentView `json:"agents"`
	AppCount    int              `json:"app_count"`
}

func adminUserViewFrom(user store.PortalUser, devices []store.PortalDevice) adminUserView {
	agents := make([]adminAgentView, 0, len(devices))
	for _, device := range devices {
		agents = append(agents, adminAgentViewFrom(device, user.Username))
	}
	return adminUserView{
		ID:          user.ID,
		Username:    user.Username,
		Role:        user.Role,
		Status:      user.Status,
		CreatedAt:   user.CreatedAt,
		UpdatedAt:   user.UpdatedAt,
		LastLoginAt: user.LastLoginAt,
		Agents:      agents,
	}
}

func (a *API) adminOverview(w http.ResponseWriter, r *http.Request, _ store.PortalUser) {
	ctx := r.Context()
	users, err := a.db.CountPortalUsersByStatus(ctx)
	if err != nil {
		writeError(w, err)
		return
	}
	devices, err := a.db.CountPortalDevicesByState(ctx)
	if err != nil {
		writeError(w, err)
		return
	}
	admins, err := a.db.CountActiveAdmins(ctx)
	if err != nil {
		writeError(w, err)
		return
	}
	appCounts := map[string]int{}
	if a.apps != nil {
		for _, item := range a.apps.ListAllApps(ctx) {
			appCounts[string(item.State)]++
		}
	}
	registration := false
	ready := settings.ReadyFlags{}
	if a.settings != nil {
		masked, err := a.settings.Masked(ctx)
		if err != nil {
			writeError(w, err)
			return
		}
		registration = masked.Registration.Open
		ready = masked.Ready
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"users": map[string]any{
			"total":    sumCounts(users),
			"active":   users[store.PortalUserActive],
			"disabled": users[store.PortalUserDisabled],
			"admins":   admins,
		},
		"agents": map[string]any{
			"total":        sumCounts(devices),
			"registered":   devices[store.PortalDeviceRegistered],
			"provisioning": devices[store.PortalDeviceProvisioning],
			"error":        devices[store.PortalDeviceError],
			"disabled":     devices[store.PortalDeviceDisabled],
		},
		"apps": map[string]any{
			"total":    sumCounts(appCounts),
			"running":  appCounts[string(app.StateRunning)],
			"building": appCounts[string(app.StateBuilding)],
			"error":    appCounts[string(app.StateError)],
			"idle":     appCounts[string(app.StateIdle)],
		},
		"platform": map[string]any{
			"agentapi":          ready.AgentAPI,
			"linx":              ready.Linx,
			"mcp":               ready.MCP,
			"services":          ready.Services,
			"registration_open": registration,
		},
	})
}

func (a *API) adminListUsers(w http.ResponseWriter, r *http.Request, _ store.PortalUser) {
	ctx := r.Context()
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status != "" && status != store.PortalUserActive && status != store.PortalUserDisabled {
		writeError(w, apiError(http.StatusBadRequest, "INVALID_ARGUMENT", "用户状态筛选无效"))
		return
	}
	limit := queryInt(r, "limit", 50, 100)
	offset := queryInt(r, "offset", 0, 1_000_000)
	users, err := a.db.PortalUsers(ctx, query, status, limit, offset)
	if err != nil {
		writeError(w, err)
		return
	}
	total, err := a.db.CountPortalUsers(ctx, query, status)
	if err != nil {
		writeError(w, err)
		return
	}
	apps := []service.OwnedAppSummary{}
	if a.apps != nil {
		apps = a.apps.ListAllApps(ctx)
	}
	deviceOwner := map[string]string{}
	views := make([]adminUserView, 0, len(users))
	for _, user := range users {
		devices, err := a.db.PortalDevicesByUser(ctx, user.ID)
		if err != nil {
			writeError(w, err)
			return
		}
		for _, device := range devices {
			if key := identityKey(device.MAC, device.LinxAgentID); key != "" {
				deviceOwner[key] = user.ID
			}
		}
		views = append(views, adminUserViewFrom(user, devices))
	}
	appCounts := map[string]int{}
	for _, item := range apps {
		if userID, ok := deviceOwner[identityKey(item.Owner.DeviceID, item.Owner.AgentID)]; ok {
			appCounts[userID]++
		}
	}
	for index := range views {
		views[index].AppCount = appCounts[views[index].ID]
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"users":  views,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func (a *API) adminGetUser(w http.ResponseWriter, r *http.Request, _ store.PortalUser) {
	ctx := r.Context()
	user, err := a.db.PortalUserByID(ctx, r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, apiError(http.StatusNotFound, "USER_NOT_FOUND", "用户不存在"))
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	devices, err := a.db.PortalDevicesByUser(ctx, user.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	apps := make([]adminAppView, 0)
	usernames := map[string]string{user.ID: user.Username}
	for _, device := range devices {
		if device.MAC == "" || device.LinxAgentID == "" || a.apps == nil {
			continue
		}
		owner := app.Identity{DeviceID: device.MAC, AgentID: device.LinxAgentID}
		summaries, err := a.apps.ListApps(ctx, owner)
		if err != nil {
			writeError(w, err)
			return
		}
		for _, summary := range summaries {
			apps = append(apps, adminAppViewFrom(summary, device, owner, a.username(ctx, usernames, user.ID)))
		}
	}
	sort.SliceStable(apps, func(i, j int) bool {
		left, right := apps[i].BuildStartedAt, apps[j].BuildStartedAt
		if left == nil || right == nil {
			return right != nil
		}
		return left.After(*right)
	})
	view := adminUserViewFrom(user, devices)
	view.AppCount = len(apps)
	writeJSON(w, http.StatusOK, map[string]any{"user": view, "apps": apps})
}

func (a *API) adminSetUserStatus(w http.ResponseWriter, r *http.Request, actor store.PortalUser) {
	var in struct {
		Status string `json:"status"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, err)
		return
	}
	if in.Status != store.PortalUserActive && in.Status != store.PortalUserDisabled {
		writeError(w, apiError(http.StatusBadRequest, "INVALID_ARGUMENT", "用户状态无效"))
		return
	}
	ctx := r.Context()
	target, err := a.db.PortalUserByID(ctx, r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, apiError(http.StatusNotFound, "USER_NOT_FOUND", "用户不存在"))
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	if in.Status == store.PortalUserDisabled && target.ID == actor.ID {
		writeError(w, apiError(http.StatusConflict, "CANNOT_DISABLE_SELF", "不能停用自己的账号"))
		return
	}
	applied, err := a.db.SetPortalUserStatusGuarded(ctx, target.ID, in.Status, a.now())
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, apiError(http.StatusNotFound, "USER_NOT_FOUND", "用户不存在"))
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	if !applied {
		// The count and the update happen in one statement, so two concurrent
		// requests cannot both disable the remaining administrators.
		writeError(w, apiError(http.StatusConflict, "LAST_ADMIN", "不能停用最后一个管理员"))
		return
	}
	target.Status = in.Status
	if in.Status == store.PortalUserDisabled {
		a.stopUserResources(ctx, target)
		a.logf("admin: %s disabled user %s", actor.ID, target.ID)
	} else {
		a.logf("admin: %s enabled user %s", actor.ID, target.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": userView(target)})
}

// stopUserResources revokes sessions and stops everything the user still owns.
// Failures are logged, never returned: disabling an account must not be
// blocked by an unreachable downstream service.
func (a *API) stopUserResources(ctx context.Context, user store.PortalUser) {
	if err := a.db.DeletePortalSessionsByUser(ctx, user.ID); err != nil {
		a.logf("admin: revoke sessions for %s: %v", user.ID, err)
	}
	devices, err := a.db.PortalDevicesByUser(ctx, user.ID)
	if err != nil {
		a.logf("admin: load devices for %s: %v", user.ID, err)
		return
	}
	owners := make([]app.Identity, 0, len(devices))
	for _, device := range devices {
		if a.voice != nil {
			a.voice.Stop(device.ID)
		}
		if device.MAC != "" && device.LinxAgentID != "" {
			owners = append(owners, app.Identity{DeviceID: device.MAC, AgentID: device.LinxAgentID})
		}
	}
	if a.apps == nil || len(owners) == 0 {
		return
	}
	for _, failure := range a.apps.StopAllForOwner(ctx, owners) {
		a.logf("admin: stop resources for %s: %v", user.ID, failure)
	}
}

func (a *API) adminListAgents(w http.ResponseWriter, r *http.Request, _ store.PortalUser) {
	ctx := r.Context()
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	devices, err := a.db.PortalDevicesAll(ctx, state, query, 500)
	if err != nil {
		writeError(w, err)
		return
	}
	usernames := map[string]string{}
	views := make([]adminAgentView, 0, len(devices))
	for _, device := range devices {
		views = append(views, adminAgentViewFrom(device, a.username(ctx, usernames, device.UserID)))
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": views})
}

func (a *API) adminRetryAgent(w http.ResponseWriter, r *http.Request, actor store.PortalUser) {
	ctx := r.Context()
	device, err := a.db.PortalDeviceByID(ctx, r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, apiError(http.StatusNotFound, "AGENT_NOT_FOUND", "智能体不存在"))
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	if a.provision == nil {
		writeError(w, apiError(http.StatusServiceUnavailable, "PROVISION_UNAVAILABLE", "开通服务暂不可用"))
		return
	}
	if device.State == store.PortalDeviceRegistered {
		user, err := a.db.PortalUserByID(ctx, device.UserID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			writeError(w, err)
			return
		}
		if err == nil && user.Status != store.PortalUserActive {
			writeError(w, apiError(http.StatusConflict, "USER_DISABLED", "用户已停用，启用后才能重启语音"))
			return
		}
		if a.voice != nil {
			if err := a.provision.startVoice(ctx, device); err != nil {
				a.logf("admin: restart voice for %s: %v", device.ID, err)
				writeError(w, apiError(http.StatusServiceUnavailable, "VOICE_UNAVAILABLE", "语音运行时启动失败"))
				return
			}
		}
	} else {
		if a.provision.Running(device.ID) {
			writeError(w, apiError(http.StatusConflict, "DEVICE_BUSY", "智能体正在开通中"))
			return
		}
		device.State = store.PortalDeviceProvisioning
		device.Error = ""
		device.UpdatedAt = a.now()
		if err := a.db.UpdatePortalDevice(ctx, device); err != nil {
			writeError(w, err)
			return
		}
		a.provision.Start(device.ID)
	}
	usernames := map[string]string{}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"agent": adminAgentViewFrom(device, a.username(ctx, usernames, device.UserID)),
	})
	a.logf("admin: %s retried agent %s", actor.ID, device.ID)
}

func (a *API) adminListApps(w http.ResponseWriter, r *http.Request, _ store.PortalUser) {
	ctx := r.Context()
	if a.apps == nil {
		writeJSON(w, http.StatusOK, map[string]any{"apps": []adminAppView{}})
		return
	}
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("query")))
	limit := queryInt(r, "limit", 100, 200)
	devices, err := a.db.PortalDevicesAll(ctx, "", "", 500)
	if err != nil {
		writeError(w, err)
		return
	}
	deviceByOwner := map[string]store.PortalDevice{}
	for _, device := range devices {
		if key := identityKey(device.MAC, device.LinxAgentID); key != "" {
			deviceByOwner[key] = device
		}
	}
	usernames := map[string]string{}
	out := make([]adminAppView, 0, limit)
	for _, item := range a.apps.ListAllApps(ctx) {
		if state != "" && string(item.State) != state {
			continue
		}
		device := deviceByOwner[identityKey(item.Owner.DeviceID, item.Owner.AgentID)]
		username := ""
		if device.ID != "" {
			username = a.username(ctx, usernames, device.UserID)
		}
		view := adminAppViewFrom(item.AppSummary, device, item.Owner, username)
		if query != "" && !adminAppMatches(view, query) {
			continue
		}
		out = append(out, view)
		if len(out) >= limit {
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"apps": out, "limit": limit})
}

// managedActiveResources reports whether an application that relies on the
// platform's default services still holds a runtime or projection. Applications
// owned by custom-binding users do not depend on the defaults, so they must not
// block an administrator from changing them.
func (a *API) managedActiveResources(ctx context.Context) (bool, error) {
	owners := make(map[string]app.Identity)
	for _, item := range a.apps.ListAllApps(ctx) {
		owners[ownerKey(item.Owner)] = item.Owner
	}
	for _, owner := range owners {
		if !a.apps.HasActiveRuntime([]app.Identity{owner}) {
			continue
		}
		device, err := a.db.PortalDeviceByIdentity(ctx, owner.DeviceID, owner.AgentID)
		if errors.Is(err, sql.ErrNoRows) {
			// Legacy applications predate per-user bindings and still rely on
			// the platform defaults for cleanup.
			return true, nil
		}
		if err != nil {
			return false, err
		}
		binding, err := a.db.ServiceBindingByUser(ctx, device.UserID)
		if errors.Is(err, sql.ErrNoRows) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		if binding.Mode != store.ServiceModeCustom {
			return true, nil
		}
	}
	return false, nil
}

func ownerKey(owner app.Identity) string {
	return owner.DeviceID + "\x00" + owner.AgentID
}

func adminAppMatches(view adminAppView, query string) bool {
	for _, value := range []string{view.AppID, view.Name, view.Title, view.Username, view.UserID, view.AgentID, view.DeviceID} {
		if value != "" && strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}

func (a *API) adminGetSettings(w http.ResponseWriter, r *http.Request, _ store.PortalUser) {
	if a.settings == nil {
		writeError(w, apiError(http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "平台设置暂不可用"))
		return
	}
	masked, err := a.settings.Masked(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": masked})
}

func (a *API) adminUpdateSettings(w http.ResponseWriter, r *http.Request, actor store.PortalUser) {
	if a.settings == nil {
		writeError(w, apiError(http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "平台设置暂不可用"))
		return
	}
	var patch settings.Patch
	if err := decodeJSON(w, r, &patch); err != nil {
		writeError(w, err)
		return
	}
	if patch.Services != nil && a.apps != nil {
		snapshot, err := a.settings.Snapshot(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		if settings.ServicesChanged(snapshot, patch.Services) {
			active, err := a.managedActiveResources(r.Context())
			if err != nil {
				writeError(w, err)
				return
			}
			if active {
				writeError(w, apiError(http.StatusConflict, "APPS_RUNNING",
					"仍有应用占用着默认服务，请先停止后再修改"))
				return
			}
		}
	}
	if err := a.settings.Update(r.Context(), patch, actor.ID); err != nil {
		switch {
		case errors.Is(err, settings.ErrInvalidURL):
			writeError(w, mapURLPolicyError(err))
		case errors.Is(err, settings.ErrInvalidSetting):
			writeError(w, apiError(http.StatusBadRequest, "INVALID_SETTINGS", "平台设置无效"))
		default:
			writeError(w, err)
		}
		return
	}
	masked, err := a.settings.Masked(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	a.logf("admin: %s updated platform settings", actor.ID)
	response := map[string]any{"settings": masked}
	// A rotated token must also reach the MCP instance Linx already holds,
	// otherwise Linx keeps calling the gateway with the old one.
	if patch.Linx != nil && patch.Linx.RotateMCPToken && a.provision != nil {
		syncCtx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		syncErr := a.provision.SyncMCP(syncCtx)
		cancel()
		if syncErr != nil {
			a.logf("admin: sync rotated MCP token to Linx: %v", syncErr)
			response["warning"] = "新的 MCP Token 已保存，但同步到灵矽失败，请检查 Linx 配置后重新生成"
		}
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *API) adminTestSettings(w http.ResponseWriter, r *http.Request, actor store.PortalUser) {
	if wait, allowed := a.adminTest.allow(actor.ID); !allowed {
		writeError(w, apiError(http.StatusTooManyRequests, "SETTINGS_TEST_RATE_LIMITED",
			"测试过于频繁，请在 "+secondsText(wait)+" 秒后重试"))
		return
	}
	if a.settings == nil || a.pool == nil {
		writeError(w, apiError(http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "平台设置暂不可用"))
		return
	}
	var patch settings.Patch
	if err := decodeJSON(w, r, &patch); err != nil {
		writeError(w, err)
		return
	}
	snapshot, err := a.settings.Preview(r.Context(), patch)
	if err != nil {
		switch {
		case errors.Is(err, settings.ErrInvalidURL):
			writeError(w, mapURLPolicyError(err))
		case errors.Is(err, settings.ErrInvalidSetting):
			writeError(w, apiError(http.StatusBadRequest, "INVALID_SETTINGS", "平台设置无效"))
		default:
			writeError(w, err)
		}
		return
	}
	linxCheck := a.checkLinx(r.Context(), snapshot.Linx)
	binding := service.Binding{
		RenderURL:    snapshot.Services.RenderURL,
		RenderToken:  snapshot.Services.RenderToken,
		CastingURL:   snapshot.Services.CastingURL,
		CastingToken: snapshot.Services.CastingToken,
		Managed:      true,
	}
	render := serviceCheck{Message: "尚未配置默认渲染服务"}
	if binding.RenderURL != "" {
		render = a.checkRender(r.Context(), binding)
	}
	casting := serviceCheck{Message: "尚未配置默认投屏服务"}
	screens := 0
	if binding.CastingURL != "" {
		casting, screens = a.checkCasting(r.Context(), binding)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"linx":    linxCheck,
		"render":  render,
		"casting": casting,
		"screens": screens,
	})
}

func (a *API) checkLinx(ctx context.Context, cfg settings.Linx) serviceCheck {
	if !cfg.Ready() {
		return serviceCheck{Message: "尚未配置 Linx 地址或 API Key"}
	}
	clientCfg := linx.Config{BaseURL: cfg.APIBase, APIKey: cfg.APIKey, OTAURL: cfg.OTAURL}
	if a.resolver != nil {
		clientCfg.HTTPClient = a.resolver.HTTPClient(10 * time.Second)
	}
	client := linx.New(clientCfg)
	probe, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := client.Check(probe); err != nil {
		return serviceCheck{Message: "无法连接 Linx 平台或 API Key 无效"}
	}
	return serviceCheck{OK: true}
}

func (a *API) username(ctx context.Context, cache map[string]string, userID string) string {
	if userID == "" {
		return ""
	}
	if name, ok := cache[userID]; ok {
		return name
	}
	name := ""
	if user, err := a.db.PortalUserByID(ctx, userID); err == nil {
		name = user.Username
	}
	cache[userID] = name
	return name
}

func identityKey(deviceID, agentID string) string {
	if deviceID == "" || agentID == "" {
		return ""
	}
	return deviceID + "\x00" + agentID
}

func sumCounts(values map[string]int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}

func queryInt(r *http.Request, name string, fallback, maximum int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return fallback
	}
	if value > maximum {
		return maximum
	}
	return value
}
