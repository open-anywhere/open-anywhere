package portal

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/service"
	"github.com/open-anywhere/open-anywhere/internal/gateway/settings"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

// adminDirect calls a handler below withAdmin with a synthetic administrator,
// which lets a test force storage failures that also break authentication.
func adminDirect(handler authedHandler, method, target, body string, user store.PortalUser) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	parts := strings.Split(strings.SplitN(target, "?", 2)[0], "/")
	if len(parts) >= 5 && parts[1] == "api" && parts[2] == "admin" && (parts[3] == "users" || parts[3] == "agents") {
		request.SetPathValue("id", parts[4])
	}
	recorder := httptest.NewRecorder()
	handler(recorder, request, user)
	return recorder
}

func createAdminDevice(t *testing.T, env *portalHTTPEnv, id, userID, mac, agentID, state, errMessage string) store.PortalDevice {
	t.Helper()
	device := createPortalTestDevice(t, env, id, userID, mac, agentID, state)
	if errMessage != "" {
		device.Error = errMessage
		if err := env.db.UpdatePortalDevice(context.Background(), device); err != nil {
			t.Fatalf("UpdatePortalDevice(%s): %v", id, err)
		}
	}
	return device
}

func adminService(values ...app.App) *service.Service {
	registry := app.NewRegistry()
	for _, value := range values {
		registry.Add(value)
	}
	return service.NewWithBindings(registry, nil, nil, nil, nil, service.Config{})
}

type adminUserListResponse struct {
	Users []struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Agents   []struct {
			ID string `json:"id"`
		} `json:"agents"`
		AppCount int `json:"app_count"`
	} `json:"users"`
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

func TestAdminListUsersFiltersPagesAndCountsApps(t *testing.T) {
	env := newPortalHTTPEnv(t)
	env.login(t)
	admin := loadPortalTestUser(t, env, testPortalUsername)
	carol := createPortalTestUser(t, env, "usr_carol", "carol", "password123", store.PortalRoleUser)
	dave := createPortalTestUser(t, env, "usr_dave", "dave", "password123", store.PortalRoleUser)
	carolDevice := createPortalTestDevice(t, env, "dev_carol", carol.ID, "AA:BB:CC:00:01:01", "agent_carol", store.PortalDeviceRegistered)
	createPortalTestDevice(t, env, "dev_dave", dave.ID, "AA:BB:CC:00:01:03", "agent_dave", store.PortalDeviceRegistered)
	erin := createPortalTestUser(t, env, "usr_erin", "erin", "password123", store.PortalRoleUser)
	createPortalTestDevice(t, env, "dev_erin", erin.ID, "AA:BB:CC:00:01:02", "", store.PortalDeviceRegistered)
	if err := env.db.SetPortalUserStatus(context.Background(), dave.ID, store.PortalUserDisabled, time.Now().UTC()); err != nil {
		t.Fatalf("SetPortalUserStatus: %v", err)
	}
	owner := app.Identity{DeviceID: carolDevice.MAC, AgentID: carolDevice.LinxAgentID}
	env.api.apps = adminService(
		app.App{ID: "app_one", Owner: owner, State: app.StateRunning},
		app.App{ID: "app_two", Owner: owner, State: app.StateIdle},
		app.App{ID: "app_orphan", Owner: app.Identity{DeviceID: "dev_gone", AgentID: "agent_gone"}, State: app.StateIdle},
	)

	response, payload := env.do(t, http.MethodGet, "/api/admin/users", "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}
	var list adminUserListResponse
	if err := json.Unmarshal(payload, &list); err != nil {
		t.Fatalf("body %s: %v", payload, err)
	}
	if list.Total != 4 || len(list.Users) != 4 || list.Limit != 50 || list.Offset != 0 {
		t.Fatalf("list=%+v", list)
	}
	byID := map[string]int{}
	for index, user := range list.Users {
		byID[user.ID] = index
	}
	carolView := list.Users[byID[carol.ID]]
	if carolView.AppCount != 2 || len(carolView.Agents) != 1 {
		t.Fatalf("carol=%+v", carolView)
	}
	if adminView := list.Users[byID[admin.ID]]; adminView.Username != testPortalUsername {
		t.Fatalf("admin=%+v", adminView)
	}

	response, payload = env.do(t, http.MethodGet, "/api/admin/users?status=disabled", "", nil)
	if response.StatusCode != http.StatusOK || !strings.Contains(string(payload), `"username":"dave"`) || strings.Contains(string(payload), `"username":"carol"`) {
		t.Fatalf("status filter: %d %s", response.StatusCode, payload)
	}
	response, payload = env.do(t, http.MethodGet, "/api/admin/users?status=sideways", "", nil)
	if response.StatusCode != http.StatusBadRequest || errorCode(t, payload) != "INVALID_ARGUMENT" {
		t.Fatalf("invalid status: %d %s", response.StatusCode, payload)
	}
	response, payload = env.do(t, http.MethodGet, "/api/admin/users?query=car", "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("query: %d %s", response.StatusCode, payload)
	}
	if err := json.Unmarshal(payload, &list); err != nil {
		t.Fatal(err)
	}
	if list.Total != 1 || len(list.Users) != 1 || list.Users[0].ID != carol.ID {
		t.Fatalf("query list=%+v", list)
	}

	response, payload = env.do(t, http.MethodGet, "/api/admin/users?limit=1&offset=1", "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("paging: %d %s", response.StatusCode, payload)
	}
	if err := json.Unmarshal(payload, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Users) != 1 || list.Limit != 1 || list.Offset != 1 {
		t.Fatalf("paging list=%+v", list)
	}
	for _, target := range []string{"limit=abc", "limit=-2", "offset=-1", "offset=abc"} {
		response, payload = env.do(t, http.MethodGet, "/api/admin/users?"+target, "", nil)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s: %d %s", target, response.StatusCode, payload)
		}
		if err := json.Unmarshal(payload, &list); err != nil {
			t.Fatal(err)
		}
		if list.Limit != 50 || list.Offset != 0 || len(list.Users) != 4 {
			t.Fatalf("%s: %+v", target, list)
		}
	}
	response, payload = env.do(t, http.MethodGet, "/api/admin/users?limit=100000", "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("clamped limit: %d %s", response.StatusCode, payload)
	}
	if err := json.Unmarshal(payload, &list); err != nil {
		t.Fatal(err)
	}
	if list.Limit != 100 {
		t.Fatalf("limit=100000 was not clamped: %d", list.Limit)
	}
}

func TestAdminListUsersReportsStorageFailures(t *testing.T) {
	env := newPortalHTTPEnv(t)
	user := loadPortalTestUser(t, env, testPortalUsername)
	createPortalTestUser(t, env, "usr_carol", "carol", "password123", store.PortalRoleUser)
	createPortalTestDevice(t, env, "dev_carol", "usr_carol", "AA:BB:CC:00:01:11", "agent_carol", store.PortalDeviceRegistered)

	execRaw(t, env.dbPath, "ALTER TABLE portal_devices RENAME TO portal_devices_hidden")
	recorder := adminDirect(env.api.adminListUsers, http.MethodGet, "/api/admin/users", "", user)
	if recorder.Code != http.StatusInternalServerError || errorCode(t, recorder.Body.Bytes()) != "INTERNAL" {
		t.Fatalf("devices failure: %d %s", recorder.Code, recorder.Body.String())
	}

	other := newPortalHTTPEnv(t)
	otherUser := loadPortalTestUser(t, other, testPortalUsername)
	execRaw(t, other.dbPath, "ALTER TABLE portal_users RENAME TO portal_users_hidden")
	recorder = adminDirect(other.api.adminListUsers, http.MethodGet, "/api/admin/users", "", otherUser)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("users failure: %d %s", recorder.Code, recorder.Body.String())
	}
}

type adminUserDetailResponse struct {
	User struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		AppCount int    `json:"app_count"`
		Agents   []struct {
			ID string `json:"id"`
		} `json:"agents"`
	} `json:"user"`
	Apps []struct {
		AppID          string `json:"app_id"`
		UserID         string `json:"user_id"`
		Username       string `json:"username"`
		DeviceID       string `json:"device_id"`
		AgentID        string `json:"agent_id"`
		OperationCount int    `json:"operation_count"`
	} `json:"apps"`
}

func TestAdminGetUserListsAppsNewestFirst(t *testing.T) {
	env := newPortalHTTPEnv(t)
	env.login(t)
	carol := createPortalTestUser(t, env, "usr_carol", "carol", "password123", store.PortalRoleUser)
	device := createPortalTestDevice(t, env, "dev_carol", carol.ID, "AA:BB:CC:00:01:21", "agent_carol", store.PortalDeviceRegistered)
	owner := app.Identity{DeviceID: device.MAC, AgentID: device.LinxAgentID}
	old := time.Now().UTC().Add(-time.Hour)
	now := time.Now().UTC()
	env.api.apps = adminService(
		app.App{ID: "app_old", Owner: owner, Name: "old", State: app.StateIdle, BuildStartedAt: old},
		app.App{ID: "app_new", Owner: owner, Name: "new", State: app.StateRunning, BuildStartedAt: now, BuildDuration: 12 * time.Second},
		app.App{ID: "app_nil", Owner: owner, Name: "nil", State: app.StateIdle},
	)

	response, payload := env.do(t, http.MethodGet, "/api/admin/users/"+carol.ID, "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}
	var detail adminUserDetailResponse
	if err := json.Unmarshal(payload, &detail); err != nil {
		t.Fatalf("body %s: %v", payload, err)
	}
	if detail.User.AppCount != 3 || len(detail.User.Agents) != 1 || detail.User.Username != "carol" {
		t.Fatalf("user=%+v", detail.User)
	}
	order := make([]string, 0, len(detail.Apps))
	for _, item := range detail.Apps {
		order = append(order, item.AppID)
		if item.Username != "carol" || item.UserID != carol.ID || item.DeviceID != device.ID || item.AgentID != device.LinxAgentID {
			t.Fatalf("app owner fields=%+v", item)
		}
	}
	if strings.Join(order, ",") != "app_nil,app_new,app_old" {
		t.Fatalf("order=%v body=%s", order, payload)
	}

	response, payload = env.do(t, http.MethodGet, "/api/admin/users/usr_missing", "", nil)
	if response.StatusCode != http.StatusNotFound || errorCode(t, payload) != "USER_NOT_FOUND" {
		t.Fatalf("missing: %d %s", response.StatusCode, payload)
	}

	execRaw(t, env.dbPath, "ALTER TABLE portal_devices RENAME TO portal_devices_hidden")
	recorder := adminDirect(env.api.adminGetUser, http.MethodGet, "/api/admin/users/"+carol.ID, "", carol)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("devices failure: %d %s", recorder.Code, recorder.Body.String())
	}

	other := newPortalHTTPEnv(t)
	execRaw(t, other.dbPath, "ALTER TABLE portal_users RENAME TO portal_users_hidden")
	recorder = adminDirect(other.api.adminGetUser, http.MethodGet, "/api/admin/users/usr_any", "", store.PortalUser{})
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("users failure: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestAdminSetStatusDisablesAndEnablesAUser(t *testing.T) {
	env := newPortalHTTPEnv(t)
	env.login(t)
	admin := loadPortalTestUser(t, env, testPortalUsername)
	carol := createPortalTestUser(t, env, "usr_carol", "carol", "password123", store.PortalRoleUser)
	device := createPortalTestDevice(t, env, "dev_carol", carol.ID, "AA:BB:CC:00:01:31", "agent_carol", store.PortalDeviceRegistered)
	_, token, err := env.api.auth.Login(context.Background(), Credentials{Username: "carol", Password: "password123"}, "agent", "10.0.0.9")
	if err != nil {
		t.Fatal(err)
	}
	env.api.apps = adminService(app.App{
		ID: "app_running", Owner: app.Identity{DeviceID: device.MAC, AgentID: device.LinxAgentID},
		State: app.StateRunning,
	})
	var logs []string
	env.api.logf = func(format string, args ...any) {
		logs = append(logs, format)
	}

	response, payload := env.do(t, http.MethodPost, "/api/admin/users/"+carol.ID+"/status",
		`{"status":"disabled"}`, map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("disable: %d %s", response.StatusCode, payload)
	}
	stored, err := env.db.PortalUserByID(context.Background(), carol.ID)
	if err != nil || stored.Status != store.PortalUserDisabled {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
	if _, err := env.api.auth.Authenticate(context.Background(), token); err == nil {
		t.Fatal("disabling the account kept its session")
	}
	if !containsLog(logs, "admin: %s disabled user %s") {
		t.Fatalf("logs=%v", logs)
	}
	if !containsLog(logs, "admin: stop resources for %s: %v") {
		t.Fatalf("the running app failure was not logged: %v", logs)
	}

	response, payload = env.do(t, http.MethodPost, "/api/admin/users/"+carol.ID+"/status",
		`{"status":"active"}`, map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusOK || !containsLog(logs, "admin: %s enabled user %s") {
		t.Fatalf("enable: %d %s logs=%v", response.StatusCode, payload, logs)
	}
	stored, err = env.db.PortalUserByID(context.Background(), carol.ID)
	if err != nil || stored.Status != store.PortalUserActive {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
	_ = admin
}

func containsLog(logs []string, format string) bool {
	for _, entry := range logs {
		if entry == format {
			return true
		}
	}
	return false
}

func TestAdminSetStatusRejectsBadRequests(t *testing.T) {
	env := newPortalHTTPEnv(t)
	env.login(t)
	carol := createPortalTestUser(t, env, "usr_carol", "carol", "password123", store.PortalRoleUser)

	response, payload := env.do(t, http.MethodPost, "/api/admin/users/"+carol.ID+"/status",
		`{"status":`, map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusBadRequest || errorCode(t, payload) != "INVALID_ARGUMENT" {
		t.Fatalf("malformed: %d %s", response.StatusCode, payload)
	}
	response, payload = env.do(t, http.MethodPost, "/api/admin/users/"+carol.ID+"/status",
		`{"status":"sideways"}`, map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusBadRequest || errorCode(t, payload) != "INVALID_ARGUMENT" {
		t.Fatalf("invalid status: %d %s", response.StatusCode, payload)
	}
	response, payload = env.do(t, http.MethodPost, "/api/admin/users/usr_missing/status",
		`{"status":"disabled"}`, map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusNotFound || errorCode(t, payload) != "USER_NOT_FOUND" {
		t.Fatalf("missing: %d %s", response.StatusCode, payload)
	}
}

func TestAdminSetStatusLogsCleanupFailures(t *testing.T) {
	env := newPortalHTTPEnv(t)
	carol := createPortalTestUser(t, env, "usr_carol", "carol", "password123", store.PortalRoleUser)
	createPortalTestDevice(t, env, "dev_carol", carol.ID, "AA:BB:CC:00:01:41", "agent_carol", store.PortalDeviceRegistered)
	_, _, err := env.api.auth.Login(context.Background(), Credentials{Username: "carol", Password: "password123"}, "agent", "10.0.0.9")
	if err != nil {
		t.Fatal(err)
	}
	execRaw(t, env.dbPath, `CREATE TRIGGER block_session_delete BEFORE DELETE ON portal_sessions
		BEGIN SELECT RAISE(ABORT, 'sessions are read-only'); END`)
	var logs []string
	env.api.logf = func(format string, args ...any) {
		logs = append(logs, format)
	}
	recorder := adminDirect(env.api.adminSetUserStatus, http.MethodPost,
		"/api/admin/users/"+carol.ID+"/status", `{"status":"disabled"}`,
		store.PortalUser{ID: "usr_peer", Role: store.PortalRoleAdmin, Status: store.PortalUserActive})
	if recorder.Code != http.StatusOK {
		t.Fatalf("disable: %d %s", recorder.Code, recorder.Body.String())
	}
	if !containsLog(logs, "admin: revoke sessions for %s: %v") {
		t.Fatalf("the session failure was not logged: %v", logs)
	}

	other := newPortalHTTPEnv(t)
	dave := createPortalTestUser(t, other, "usr_dave", "dave", "password123", store.PortalRoleUser)
	createPortalTestDevice(t, other, "dev_dave", dave.ID, "AA:BB:CC:00:01:42", "agent_dave", store.PortalDeviceRegistered)
	execRaw(t, other.dbPath, "ALTER TABLE portal_devices RENAME TO portal_devices_hidden")
	logs = nil
	other.api.logf = func(format string, args ...any) {
		logs = append(logs, format)
	}
	recorder = adminDirect(other.api.adminSetUserStatus, http.MethodPost,
		"/api/admin/users/"+dave.ID+"/status", `{"status":"disabled"}`,
		store.PortalUser{ID: "usr_peer", Role: store.PortalRoleAdmin, Status: store.PortalUserActive})
	if recorder.Code != http.StatusOK {
		t.Fatalf("disable with hidden devices: %d %s", recorder.Code, recorder.Body.String())
	}
	if !containsLog(logs, "admin: load devices for %s: %v") {
		t.Fatalf("the device failure was not logged: %v", logs)
	}
}

func TestAdminSetStatusReportsStorageFailure(t *testing.T) {
	env := newPortalHTTPEnv(t)
	carol := createPortalTestUser(t, env, "usr_carol", "carol", "password123", store.PortalRoleUser)
	execRaw(t, env.dbPath, `CREATE TRIGGER block_user_update BEFORE UPDATE ON portal_users
		BEGIN SELECT RAISE(ABORT, 'users are read-only'); END`)
	recorder := adminDirect(env.api.adminSetUserStatus, http.MethodPost,
		"/api/admin/users/"+carol.ID+"/status", `{"status":"disabled"}`,
		store.PortalUser{ID: "usr_peer", Role: store.PortalRoleAdmin, Status: store.PortalUserActive})
	if recorder.Code != http.StatusInternalServerError || errorCode(t, recorder.Body.Bytes()) != "INTERNAL" {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

type adminAgentListResponse struct {
	Agents []struct {
		ID        string `json:"id"`
		Username  string `json:"username"`
		State     string `json:"state"`
		Error     string `json:"error"`
		ErrorCode string `json:"error_code"`
	} `json:"agents"`
}

func TestAdminListAgentsFiltersAndMapsErrors(t *testing.T) {
	env := newPortalHTTPEnv(t)
	env.login(t)
	carol := createPortalTestUser(t, env, "usr_carol", "carol", "password123", store.PortalRoleUser)
	dave := createPortalTestUser(t, env, "usr_dave", "dave", "password123", store.PortalRoleUser)
	createAdminDevice(t, env, "dev_carol", carol.ID, "AA:BB:CC:00:02:01", "agent_carol", store.PortalDeviceRegistered, "")
	createAdminDevice(t, env, "dev_dave", dave.ID, "AA:BB:CC:00:02:02", "agent_dave", store.PortalDeviceProvisioning, "")
	erin := createPortalTestUser(t, env, "usr_erin", "erin", "password123", store.PortalRoleUser)
	createAdminDevice(t, env, "dev_erin", erin.ID, "AA:BB:CC:00:02:03", "agent_erin", store.PortalDeviceError, deviceErrorCodePlatformNotConfigured)

	response, payload := env.do(t, http.MethodGet, "/api/admin/agents", "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}
	var list adminAgentListResponse
	if err := json.Unmarshal(payload, &list); err != nil {
		t.Fatalf("body %s: %v", payload, err)
	}
	if len(list.Agents) != 3 {
		t.Fatalf("agents=%+v", list.Agents)
	}
	byID := map[string]int{}
	for index, agent := range list.Agents {
		byID[agent.ID] = index
	}
	erinView := list.Agents[byID["dev_erin"]]
	if erinView.ErrorCode != deviceErrorCodePlatformNotConfigured || erinView.Error != "平台尚未配置，请联系维护者" || erinView.Username != "erin" {
		t.Fatalf("erin=%+v", erinView)
	}
	if byID["dev_carol"] == byID["dev_erin"] {
		t.Fatal("the second device of a user swallowed the first")
	}

	response, payload = env.do(t, http.MethodGet, "/api/admin/agents?state=error", "", nil)
	if response.StatusCode != http.StatusOK || !strings.Contains(string(payload), "dev_erin") || strings.Contains(string(payload), "dev_dave") {
		t.Fatalf("state filter: %d %s", response.StatusCode, payload)
	}
	response, payload = env.do(t, http.MethodGet, "/api/admin/agents?query=carol", "", nil)
	if response.StatusCode != http.StatusOK || !strings.Contains(string(payload), "dev_carol") || strings.Contains(string(payload), "dev_dave") {
		t.Fatalf("query filter: %d %s", response.StatusCode, payload)
	}

	recorder := adminDirect(env.api.adminListAgents, http.MethodGet, "/api/admin/agents", "", carol)
	if recorder.Code != http.StatusOK {
		t.Fatalf("direct: %d %s", recorder.Code, recorder.Body.String())
	}

	other := newPortalHTTPEnv(t)
	loadPortalTestUser(t, other, testPortalUsername)
	execRaw(t, other.dbPath, "ALTER TABLE portal_users RENAME TO portal_users_hidden")
	recorder = adminDirect(other.api.adminListAgents, http.MethodGet, "/api/admin/agents", "", store.PortalUser{})
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"username":""`) && !strings.Contains(recorder.Body.String(), `"agents":[]`) {
		t.Fatalf("missing owner lookup: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestAdminListAgentsReportsStorageFailure(t *testing.T) {
	env := newPortalHTTPEnv(t)
	carol := createPortalTestUser(t, env, "usr_carol", "carol", "password123", store.PortalRoleUser)
	createAdminDevice(t, env, "dev_carol", carol.ID, "AA:BB:CC:00:02:11", "agent_carol", store.PortalDeviceRegistered, "")
	execRaw(t, env.dbPath, "ALTER TABLE portal_devices RENAME TO portal_devices_hidden")
	recorder := adminDirect(env.api.adminListAgents, http.MethodGet, "/api/admin/agents", "", carol)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestAdminRetryAgentCoversEveryState(t *testing.T) {
	env := newPortalHTTPEnv(t)
	env.login(t)
	origin := map[string]string{"Origin": env.server.URL}
	carol := createPortalTestUser(t, env, "usr_carol", "carol", "password123", store.PortalRoleUser)
	createAdminDevice(t, env, "dev_nocred", carol.ID, "AA:BB:CC:00:02:21", "agent_nocred", store.PortalDeviceRegistered, "")

	response, payload := env.do(t, http.MethodPost, "/api/admin/agents/dev_missing/retry", `{}`, origin)
	if response.StatusCode != http.StatusNotFound || errorCode(t, payload) != "AGENT_NOT_FOUND" {
		t.Fatalf("missing: %d %s", response.StatusCode, payload)
	}
	response, payload = env.do(t, http.MethodPost, "/api/admin/agents/dev_nocred/retry", `{}`, origin)
	if response.StatusCode != http.StatusServiceUnavailable || errorCode(t, payload) != "VOICE_UNAVAILABLE" {
		t.Fatalf("voice: %d %s", response.StatusCode, payload)
	}

	dave := createPortalTestUser(t, env, "usr_dave", "dave", "password123", store.PortalRoleUser)
	createAdminDevice(t, env, "dev_ready", dave.ID, "AA:BB:CC:00:02:22", "agent_ready", store.PortalDeviceRegistered, "")
	writeCredential(t, env.dataDir, "dev_ready")
	response, payload = env.do(t, http.MethodPost, "/api/admin/agents/dev_ready/retry", `{}`, origin)
	if response.StatusCode != http.StatusAccepted || !strings.Contains(string(payload), `"agent_id":"agent_ready"`) {
		t.Fatalf("registered: %d %s", response.StatusCode, payload)
	}

	erin := createPortalTestUser(t, env, "usr_erin", "erin", "password123", store.PortalRoleUser)
	createAdminDevice(t, env, "dev_busy", erin.ID, "AA:BB:CC:00:02:23", "agent_busy", store.PortalDeviceProvisioning, "")
	env.api.provision.mu.Lock()
	env.api.provision.running["dev_busy"] = true
	env.api.provision.mu.Unlock()
	response, payload = env.do(t, http.MethodPost, "/api/admin/agents/dev_busy/retry", `{}`, origin)
	if response.StatusCode != http.StatusConflict || errorCode(t, payload) != "DEVICE_BUSY" {
		t.Fatalf("busy: %d %s", response.StatusCode, payload)
	}
	env.api.provision.mu.Lock()
	delete(env.api.provision.running, "dev_busy")
	env.api.provision.mu.Unlock()

	response, payload = env.do(t, http.MethodPost, "/api/admin/agents/dev_busy/retry", `{}`, origin)
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("restarted: %d %s", response.StatusCode, payload)
	}
	stored, err := env.db.PortalDeviceByID(context.Background(), "dev_busy")
	if err != nil || stored.State != store.PortalDeviceProvisioning {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
}

func TestAdminRetryAgentReportsProvisionerAndStorageFailures(t *testing.T) {
	env := newPortalHTTPEnv(t)
	env.login(t)
	origin := map[string]string{"Origin": env.server.URL}
	carol := createPortalTestUser(t, env, "usr_carol", "carol", "password123", store.PortalRoleUser)
	createAdminDevice(t, env, "dev_carol", carol.ID, "AA:BB:CC:00:02:31", "agent_carol", store.PortalDeviceProvisioning, "")

	provisioner := env.api.provision
	env.api.provision = nil
	response, payload := env.do(t, http.MethodPost, "/api/admin/agents/dev_carol/retry", `{}`, origin)
	if response.StatusCode != http.StatusServiceUnavailable || errorCode(t, payload) != "PROVISION_UNAVAILABLE" {
		t.Fatalf("nil provisioner: %d %s", response.StatusCode, payload)
	}
	env.api.provision = provisioner

	execRaw(t, env.dbPath, `CREATE TRIGGER block_device_update BEFORE UPDATE ON portal_devices
		BEGIN SELECT RAISE(ABORT, 'devices are read-only'); END`)
	recorder := adminDirect(env.api.adminRetryAgent, http.MethodPost, "/api/admin/agents/dev_carol/retry", `{}`, carol)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("update failure: %d %s", recorder.Code, recorder.Body.String())
	}

	other := newPortalHTTPEnv(t)
	dave := createPortalTestUser(t, other, "usr_dave", "dave", "password123", store.PortalRoleUser)
	createAdminDevice(t, other, "dev_dave", dave.ID, "AA:BB:CC:00:02:32", "agent_dave", store.PortalDeviceRegistered, "")
	execRaw(t, other.dbPath, "ALTER TABLE portal_users RENAME TO portal_users_hidden")
	recorder = adminDirect(other.api.adminRetryAgent, http.MethodPost, "/api/admin/agents/dev_dave/retry", `{}`, store.PortalUser{})
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("owner failure: %d %s", recorder.Code, recorder.Body.String())
	}

	third := newPortalHTTPEnv(t)
	erin := createPortalTestUser(t, third, "usr_erin", "erin", "password123", store.PortalRoleUser)
	createAdminDevice(t, third, "dev_erin", erin.ID, "AA:BB:CC:00:02:33", "agent_erin", store.PortalDeviceProvisioning, "")
	execRaw(t, third.dbPath, "ALTER TABLE portal_devices RENAME TO portal_devices_hidden")
	recorder = adminDirect(third.api.adminRetryAgent, http.MethodPost, "/api/admin/agents/dev_erin/retry", `{}`, erin)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("load failure: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestAdminRetryAgentAcceptsADeviceWithACanceledProvisioner(t *testing.T) {
	env := newPortalHTTPEnv(t)
	env.login(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	env.api.provision = NewProvisioner(ctx, env.db, nil, env.api.voice, ProvisionConfig{DataDir: env.dataDir})
	carol := createPortalTestUser(t, env, "usr_carol", "carol", "password123", store.PortalRoleUser)
	createAdminDevice(t, env, "dev_carol", carol.ID, "AA:BB:CC:00:02:41", "agent_carol", store.PortalDeviceError, "boom")

	response, payload := env.do(t, http.MethodPost, "/api/admin/agents/dev_carol/retry", `{}`,
		map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}
	stored, err := env.db.PortalDeviceByID(context.Background(), "dev_carol")
	if err != nil || stored.State != store.PortalDeviceProvisioning || stored.Error != "" {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
	deadline := time.Now().Add(time.Second)
	for env.api.provision.Running("dev_carol") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if env.api.provision.Running("dev_carol") {
		t.Fatal("the canceled provisioner left an iteration marked as running")
	}
}

func TestAdminListAppsFiltersLimitsAndReportsFailures(t *testing.T) {
	env := newPortalHTTPEnv(t)
	env.login(t)
	carol := createPortalTestUser(t, env, "usr_carol", "carol", "password123", store.PortalRoleUser)
	device := createPortalTestDevice(t, env, "dev_carol", carol.ID, "AA:BB:CC:00:02:51", "agent_carol", store.PortalDeviceRegistered)
	owner := app.Identity{DeviceID: device.MAC, AgentID: device.LinxAgentID}
	env.api.apps = adminService(
		app.App{ID: "app_running", Owner: owner, Name: "quiz", State: app.StateRunning},
		app.App{ID: "app_building", Owner: owner, Name: "puzzle", State: app.StateBuilding},
		app.App{ID: "app_orphan", Owner: app.Identity{DeviceID: "dev_gone", AgentID: "agent_gone"}, State: app.StateIdle},
	)

	response, payload := env.do(t, http.MethodGet, "/api/admin/apps", "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}
	var list struct {
		Apps  []map[string]any `json:"apps"`
		Limit int              `json:"limit"`
	}
	if err := json.Unmarshal(payload, &list); err != nil {
		t.Fatalf("body %s: %v", payload, err)
	}
	if len(list.Apps) != 3 || list.Limit != 100 {
		t.Fatalf("list=%+v", list)
	}
	if !strings.Contains(string(payload), `"username":"carol"`) || !strings.Contains(string(payload), `"agent_id":"agent_gone"`) {
		t.Fatalf("owner fields missing: %s", payload)
	}

	cases := []struct {
		query string
		want  []string
	}{
		{"state=running", []string{"app_running"}},
		{"query=quiz", []string{"app_running"}},
		{"query=carol", []string{"app_building", "app_running"}},
		{"query=usr_carol", []string{"app_building", "app_running"}},
		{"query=agent_carol", []string{"app_building", "app_running"}},
		{"query=dev_carol", []string{"app_building", "app_running"}},
		{"query=app_build", []string{"app_building"}},
		{"query=zzz", nil},
	}
	for _, test := range cases {
		response, payload = env.do(t, http.MethodGet, "/api/admin/apps?"+test.query, "", nil)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s: %d %s", test.query, response.StatusCode, payload)
		}
		ids := appIDsFromBody(t, payload)
		if strings.Join(ids, ",") != strings.Join(test.want, ",") {
			t.Fatalf("%s: ids=%v body=%s", test.query, ids, payload)
		}
	}

	response, payload = env.do(t, http.MethodGet, "/api/admin/apps?limit=1", "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("limit=1: %d %s", response.StatusCode, payload)
	}
	if ids := appIDsFromBody(t, payload); len(ids) != 1 {
		t.Fatalf("limit was not applied: %v", ids)
	}
	response, payload = env.do(t, http.MethodGet, "/api/admin/apps?limit=999", "", nil)
	if response.StatusCode != http.StatusOK || !strings.Contains(string(payload), `"limit":200`) {
		t.Fatalf("limit=999: %d %s", response.StatusCode, payload)
	}
	response, payload = env.do(t, http.MethodGet, "/api/admin/apps?limit=abc", "", nil)
	if response.StatusCode != http.StatusOK || !strings.Contains(string(payload), `"limit":100`) {
		t.Fatalf("limit=abc: %d %s", response.StatusCode, payload)
	}

	empty := newPortalHTTPEnv(t)
	loadPortalTestUser(t, empty, testPortalUsername)
	recorder := adminDirect(empty.api.adminListApps, http.MethodGet, "/api/admin/apps", "", store.PortalUser{})
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"apps":[]`) {
		t.Fatalf("nil apps: %d %s", recorder.Code, recorder.Body.String())
	}

	env.login(t)
	execRaw(t, env.dbPath, "ALTER TABLE portal_devices RENAME TO portal_devices_hidden")
	recorder = adminDirect(env.api.adminListApps, http.MethodGet, "/api/admin/apps", "", store.PortalUser{})
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("devices failure: %d %s", recorder.Code, recorder.Body.String())
	}
}

func appIDsFromBody(t *testing.T, payload []byte) []string {
	t.Helper()
	var out struct {
		Apps []struct {
			AppID string `json:"app_id"`
		} `json:"apps"`
	}
	if err := json.Unmarshal(payload, &out); err != nil {
		t.Fatalf("body %s: %v", payload, err)
	}
	ids := make([]string, 0, len(out.Apps))
	for _, item := range out.Apps {
		ids = append(ids, item.AppID)
	}
	return ids
}

func TestAdminSettingsReadUpdateAndFailures(t *testing.T) {
	env := newPlatformHTTPEnv(t)
	env.login(t)

	response, payload := env.do(t, http.MethodGet, "/api/admin/settings", "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("get: %d %s", response.StatusCode, payload)
	}
	var initial struct {
		Settings struct {
			Registration struct {
				Open bool `json:"Open"`
			} `json:"registration"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(payload, &initial); err != nil {
		t.Fatalf("body %s: %v", payload, err)
	}
	if initial.Settings.Registration.Open {
		t.Fatalf("registration started open: %s", payload)
	}
	response, payload = env.do(t, http.MethodPut, "/api/admin/settings", `{"registration":`, map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusBadRequest || errorCode(t, payload) != "INVALID_ARGUMENT" {
		t.Fatalf("malformed: %d %s", response.StatusCode, payload)
	}
	response, payload = env.do(t, http.MethodPut, "/api/admin/settings",
		`{"services":{"render_url":"not-a-url"}}`, map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("invalid url: %d %s", response.StatusCode, payload)
	}
	response, payload = env.do(t, http.MethodPut, "/api/admin/settings",
		`{"agentapi":{"agent_version":-1}}`, map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusBadRequest || errorCode(t, payload) != "INVALID_SETTINGS" {
		t.Fatalf("invalid version: %d %s", response.StatusCode, payload)
	}

	body := `{"agentapi":{"base_url":"https://agent.example.com","agent_id":"ag_1","environment_id":"env_1","api_key":{"value":"agent-secret"}},` +
		`"linx":{"api_base":"https://linx.example.com","api_key":{"value":"linx-secret"}},` +
		`"services":{"render_url":"https://render.example.com","casting_url":"https://casting.example.com"},` +
		`"registration":{"open":true}}`
	response, payload = env.do(t, http.MethodPut, "/api/admin/settings", body, map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("update: %d %s", response.StatusCode, payload)
	}
	var masked struct {
		Settings struct {
			Registration struct {
				Open bool `json:"Open"`
			} `json:"registration"`
			Ready struct {
				AgentAPI bool `json:"agentapi"`
				Linx     bool `json:"linx"`
				Services bool `json:"services"`
			} `json:"ready"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(payload, &masked); err != nil {
		t.Fatalf("body %s: %v", payload, err)
	}
	if !masked.Settings.Registration.Open || !masked.Settings.Ready.AgentAPI || !masked.Settings.Ready.Linx || !masked.Settings.Ready.Services {
		t.Fatalf("masked=%+v body=%s", masked, payload)
	}
	response, payload = env.do(t, http.MethodGet, "/api/admin/settings", "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("get after update: %d %s", response.StatusCode, payload)
	}
	if err := json.Unmarshal(payload, &masked); err != nil {
		t.Fatalf("body %s: %v", payload, err)
	}
	if !masked.Settings.Registration.Open {
		t.Fatalf("the registration switch was not saved: %s", payload)
	}

	noKey := newPlatformHTTPEnv(t)
	noKey.login(t)
	noKey.api.settings = settings.New(noKey.db, nil)
	response, payload = noKey.do(t, http.MethodPut, "/api/admin/settings",
		`{"services":{"render_token":{"value":"secret"}}}`, map[string]string{"Origin": noKey.server.URL})
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("secret without a key: %d %s", response.StatusCode, payload)
	}

	broken := newPlatformHTTPEnv(t)
	loadPortalTestUser(t, broken, testPortalUsername)
	execRaw(t, broken.dbPath, "ALTER TABLE platform_settings RENAME TO platform_settings_hidden")
	recorder := adminDirect(broken.api.adminGetSettings, http.MethodGet, "/api/admin/settings", "", store.PortalUser{})
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("settings failure: %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = adminDirect(broken.api.adminUpdateSettings, http.MethodPut, "/api/admin/settings",
		`{"registration":{"open":true}}`, store.PortalUser{})
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("update failure: %d %s", recorder.Code, recorder.Body.String())
	}

	plain := newPortalHTTPEnv(t)
	plain.login(t)
	response, payload = plain.do(t, http.MethodGet, "/api/admin/settings", "", nil)
	if response.StatusCode != http.StatusServiceUnavailable || errorCode(t, payload) != "SETTINGS_UNAVAILABLE" {
		t.Fatalf("unavailable get: %d %s", response.StatusCode, payload)
	}
	response, payload = plain.do(t, http.MethodPut, "/api/admin/settings", `{}`, map[string]string{"Origin": plain.server.URL})
	if response.StatusCode != http.StatusServiceUnavailable || errorCode(t, payload) != "SETTINGS_UNAVAILABLE" {
		t.Fatalf("unavailable update: %d %s", response.StatusCode, payload)
	}
}

func TestAdminUpdateSettingsRefusesWhileDefaultServicesRun(t *testing.T) {
	type scenario struct {
		name        string
		bindingMode string
		withDevice  bool
		appState    app.State
		services    string
		hideTable   string
		wantStatus  int
		wantCode    string
	}
	scenarios := []scenario{
		{name: "legacy app without a device", appState: app.StateRunning, services: `{"render_url":"https://render2.example.com"}`, wantStatus: http.StatusConflict, wantCode: "APPS_RUNNING"},
		{name: "device without a binding", withDevice: true, appState: app.StateRunning, services: `{"render_url":"https://render2.example.com"}`, wantStatus: http.StatusConflict, wantCode: "APPS_RUNNING"},
		{name: "managed binding", withDevice: true, bindingMode: store.ServiceModeManaged, appState: app.StateRunning, services: `{"render_url":"https://render2.example.com"}`, wantStatus: http.StatusConflict, wantCode: "APPS_RUNNING"},
		{name: "custom binding", withDevice: true, bindingMode: store.ServiceModeCustom, appState: app.StateRunning, services: `{"render_url":"https://render2.example.com"}`, wantStatus: http.StatusOK},
		{name: "idle app", withDevice: true, appState: app.StateIdle, services: `{"render_url":"https://render2.example.com"}`, wantStatus: http.StatusOK},
		{name: "unchanged services", withDevice: true, appState: app.StateRunning, services: `{"render_url":""}`, wantStatus: http.StatusOK},
		{name: "devices unavailable", withDevice: true, appState: app.StateRunning, services: `{"render_url":"https://render2.example.com"}`, hideTable: "portal_devices", wantStatus: http.StatusInternalServerError},
		{name: "bindings unavailable", withDevice: true, appState: app.StateRunning, services: `{"render_url":"https://render2.example.com"}`, hideTable: "portal_service_bindings", wantStatus: http.StatusInternalServerError},
	}
	for _, test := range scenarios {
		t.Run(test.name, func(t *testing.T) {
			env := newPlatformHTTPEnv(t)
			env.login(t)
			carol := createPortalTestUser(t, env, "usr_carol", "carol", "password123", store.PortalRoleUser)
			owner := app.Identity{DeviceID: "dev_gone", AgentID: "agent_gone"}
			if test.withDevice {
				device := createPortalTestDevice(t, env, "dev_carol", carol.ID, "AA:BB:CC:00:03:01", "agent_carol", store.PortalDeviceRegistered)
				owner = app.Identity{DeviceID: device.MAC, AgentID: device.LinxAgentID}
			}
			env.api.apps = adminService(
				app.App{ID: "app_one", Owner: owner, State: test.appState},
				app.App{ID: "app_two", Owner: owner, State: test.appState},
			)
			if test.bindingMode != "" {
				now := time.Now().UTC()
				if err := env.db.UpsertServiceBinding(context.Background(), store.PortalServiceBinding{
					UserID: carol.ID, Mode: test.bindingMode, CreatedAt: now, UpdatedAt: now,
				}); err != nil {
					t.Fatalf("UpsertServiceBinding: %v", err)
				}
			}
			if test.hideTable != "" {
				execRaw(t, env.dbPath, "ALTER TABLE "+test.hideTable+" RENAME TO "+test.hideTable+"_hidden")
				recorder := adminDirect(env.api.adminUpdateSettings, http.MethodPut, "/api/admin/settings",
					`{"services":`+test.services+`}`, store.PortalUser{ID: "usr_admin"})
				if recorder.Code != test.wantStatus {
					t.Fatalf("status=%d want=%d body=%s", recorder.Code, test.wantStatus, recorder.Body.String())
				}
				return
			}
			response, payload := env.do(t, http.MethodPut, "/api/admin/settings",
				`{"services":`+test.services+`}`, map[string]string{"Origin": env.server.URL})
			if response.StatusCode != test.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", response.StatusCode, test.wantStatus, payload)
			}
			if test.wantCode != "" && errorCode(t, payload) != test.wantCode {
				t.Fatalf("code=%s body=%s", errorCode(t, payload), payload)
			}
		})
	}
}

func TestAdminTestSettingsProbesEveryGroup(t *testing.T) {
	env := newPlatformHTTPEnv(t)
	env.login(t)
	origin := map[string]string{"Origin": env.server.URL}

	response, payload := env.do(t, http.MethodPost, "/api/admin/settings/test", `{"linx":`, origin)
	if response.StatusCode != http.StatusBadRequest || errorCode(t, payload) != "INVALID_ARGUMENT" {
		t.Fatalf("malformed: %d %s", response.StatusCode, payload)
	}
	response, payload = env.do(t, http.MethodPost, "/api/admin/settings/test",
		`{"services":{"casting_url":"not-a-url"}}`, origin)
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("invalid url: %d %s", response.StatusCode, payload)
	}
	response, payload = env.do(t, http.MethodPost, "/api/admin/settings/test",
		`{"agentapi":{"agent_version":-1}}`, origin)
	if response.StatusCode != http.StatusBadRequest || errorCode(t, payload) != "INVALID_SETTINGS" {
		t.Fatalf("invalid setting: %d %s", response.StatusCode, payload)
	}

	response, payload = env.do(t, http.MethodPost, "/api/admin/settings/test", `{}`, origin)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unconfigured: %d %s", response.StatusCode, payload)
	}
	var unconfigured struct {
		Linx    serviceCheck `json:"linx"`
		Render  serviceCheck `json:"render"`
		Casting serviceCheck `json:"casting"`
		Screens int          `json:"screens"`
	}
	if err := json.Unmarshal(payload, &unconfigured); err != nil {
		t.Fatalf("body %s: %v", payload, err)
	}
	if unconfigured.Linx.OK || unconfigured.Render.OK || unconfigured.Casting.OK || unconfigured.Screens != 0 ||
		unconfigured.Render.Message == "" || unconfigured.Casting.Message == "" {
		t.Fatalf("unconfigured=%+v", unconfigured)
	}

	linxServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/xiaozhi/agent/list" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":[]}`))
	}))
	defer linxServer.Close()
	renderServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer renderServer.Close()
	castingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
		case "/v1/targets":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"targets":[{"id":"tv_1","name":"客厅","state":"online"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer castingServer.Close()

	body := fmt.Sprintf(`{"linx":{"api_base":%q,"api_key":{"value":"key"}},`+
		`"services":{"render_url":%q,"casting_url":%q}}`,
		linxServer.URL, renderServer.URL, castingServer.URL)
	response, payload = env.do(t, http.MethodPost, "/api/admin/settings/test", body, origin)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("configured: %d %s", response.StatusCode, payload)
	}
	var configured struct {
		Linx    serviceCheck `json:"linx"`
		Render  serviceCheck `json:"render"`
		Casting serviceCheck `json:"casting"`
		Screens int          `json:"screens"`
	}
	if err := json.Unmarshal(payload, &configured); err != nil {
		t.Fatalf("body %s: %v", payload, err)
	}
	if !configured.Linx.OK || !configured.Render.OK || !configured.Casting.OK || configured.Screens != 1 {
		t.Fatalf("configured=%+v body=%s", configured, payload)
	}

	response, payload = env.do(t, http.MethodPost, "/api/admin/settings/test",
		`{"linx":{"api_base":"http://127.0.0.1:1","api_key":{"value":"key"}}}`, origin)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unreachable linx: %d %s", response.StatusCode, payload)
	}
	if err := json.Unmarshal(payload, &unconfigured); err != nil {
		t.Fatal(err)
	}
	if unconfigured.Linx.OK || unconfigured.Linx.Message == "" {
		t.Fatalf("unreachable linx=%+v", unconfigured.Linx)
	}

	response, payload = env.do(t, http.MethodPost, "/api/admin/settings/test",
		`{"services":{"render_url":"http://127.0.0.1:1","casting_url":"http://127.0.0.1:2"}}`, origin)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unreachable services: %d %s", response.StatusCode, payload)
	}
	if err := json.Unmarshal(payload, &unconfigured); err != nil {
		t.Fatal(err)
	}
	if unconfigured.Render.OK || unconfigured.Casting.OK || unconfigured.Screens != 0 {
		t.Fatalf("unreachable services=%+v", unconfigured)
	}

	env.api.adminTest = newWindowLimiter(env.api.now, time.Minute, 1)
	response, _ = env.do(t, http.MethodPost, "/api/admin/settings/test", `{}`, origin)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("first limited probe: %d", response.StatusCode)
	}
	response, payload = env.do(t, http.MethodPost, "/api/admin/settings/test", `{}`, origin)
	if response.StatusCode != http.StatusTooManyRequests || errorCode(t, payload) != "SETTINGS_TEST_RATE_LIMITED" {
		t.Fatalf("rate limit: %d %s", response.StatusCode, payload)
	}

	broken := newPlatformHTTPEnv(t)
	loadPortalTestUser(t, broken, testPortalUsername)
	execRaw(t, broken.dbPath, "ALTER TABLE platform_settings RENAME TO platform_settings_hidden")
	recorder := adminDirect(broken.api.adminTestSettings, http.MethodPost, "/api/admin/settings/test", `{}`, store.PortalUser{})
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("preview failure: %d %s", recorder.Code, recorder.Body.String())
	}

	plain := newPortalHTTPEnv(t)
	plain.login(t)
	response, payload = plain.do(t, http.MethodPost, "/api/admin/settings/test", `{}`, map[string]string{"Origin": plain.server.URL})
	if response.StatusCode != http.StatusServiceUnavailable || errorCode(t, payload) != "SETTINGS_UNAVAILABLE" {
		t.Fatalf("unavailable: %d %s", response.StatusCode, payload)
	}
}

func settingsString(value string) *string { return &value }
func settingsBool(value bool) *bool       { return &value }

func configureReadyPlatform(t *testing.T, env *portalHTTPEnv) {
	t.Helper()
	patch := settings.Patch{
		AgentAPI: &settings.AgentAPIPatch{
			BaseURL:       settingsString("https://agent.example.com"),
			AgentID:       settingsString("ag_1"),
			EnvironmentID: settingsString("env_1"),
			APIKey:        &settings.SecretValue{Value: "agent-secret"},
		},
		Linx: &settings.LinxPatch{
			APIBase: settingsString("https://linx.example.com"),
			APIKey:  &settings.SecretValue{Value: "linx-secret"},
		},
		Services: &settings.ServicesPatch{
			RenderURL:  settingsString("https://render.example.com"),
			CastingURL: settingsString("https://casting.example.com"),
		},
		Registration: &settings.RegistrationPatch{Open: settingsBool(true)},
	}
	if err := env.api.settings.Update(context.Background(), patch, "usr_admin"); err != nil {
		t.Fatalf("settings.Update: %v", err)
	}
}

func TestAdminOverviewCountsEverything(t *testing.T) {
	env := newPlatformHTTPEnv(t)
	env.login(t)
	configureReadyPlatform(t, env)
	carol := createPortalTestUser(t, env, "usr_carol", "carol", "password123", store.PortalRoleUser)
	dave := createPortalTestUser(t, env, "usr_dave", "dave", "password123", store.PortalRoleUser)
	erin := createPortalTestUser(t, env, "usr_erin", "erin", "password123", store.PortalRoleUser)
	if err := env.db.SetPortalUserStatus(context.Background(), dave.ID, store.PortalUserDisabled, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	carolDevice := createPortalTestDevice(t, env, "dev_carol", carol.ID, "AA:BB:CC:00:04:01", "agent_carol", store.PortalDeviceRegistered)
	createPortalTestDevice(t, env, "dev_dave", dave.ID, "AA:BB:CC:00:04:02", "agent_dave", store.PortalDeviceError)
	createPortalTestDevice(t, env, "dev_erin", erin.ID, "AA:BB:CC:00:04:03", "agent_erin", store.PortalDeviceProvisioning)
	owner := app.Identity{DeviceID: carolDevice.MAC, AgentID: carolDevice.LinxAgentID}
	env.api.apps = adminService(
		app.App{ID: "app_running", Owner: owner, State: app.StateRunning},
		app.App{ID: "app_building", Owner: owner, State: app.StateBuilding},
		app.App{ID: "app_error", Owner: owner, State: app.StateError},
		app.App{ID: "app_idle", Owner: owner, State: app.StateIdle},
	)

	response, payload := env.do(t, http.MethodGet, "/api/admin/overview", "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}
	var out struct {
		Users struct {
			Total    int `json:"total"`
			Active   int `json:"active"`
			Disabled int `json:"disabled"`
			Admins   int `json:"admins"`
		} `json:"users"`
		Agents struct {
			Total        int `json:"total"`
			Registered   int `json:"registered"`
			Provisioning int `json:"provisioning"`
			Error        int `json:"error"`
			Disabled     int `json:"disabled"`
		} `json:"agents"`
		Apps struct {
			Total    int `json:"total"`
			Running  int `json:"running"`
			Building int `json:"building"`
			Error    int `json:"error"`
			Idle     int `json:"idle"`
		} `json:"apps"`
		Platform struct {
			AgentAPI         bool `json:"agentapi"`
			Linx             bool `json:"linx"`
			Services         bool `json:"services"`
			RegistrationOpen bool `json:"registration_open"`
		} `json:"platform"`
	}
	if err := json.Unmarshal(payload, &out); err != nil {
		t.Fatalf("body %s: %v", payload, err)
	}
	if out.Users.Total != 4 || out.Users.Active != 3 || out.Users.Disabled != 1 || out.Users.Admins != 1 {
		t.Fatalf("users=%+v", out.Users)
	}
	if out.Agents.Total != 3 || out.Agents.Registered != 1 || out.Agents.Provisioning != 1 || out.Agents.Error != 1 || out.Agents.Disabled != 0 {
		t.Fatalf("agents=%+v", out.Agents)
	}
	if out.Apps.Total != 4 || out.Apps.Running != 1 || out.Apps.Building != 1 || out.Apps.Error != 1 || out.Apps.Idle != 1 {
		t.Fatalf("apps=%+v", out.Apps)
	}
	if !out.Platform.AgentAPI || !out.Platform.Linx || !out.Platform.Services || !out.Platform.RegistrationOpen {
		t.Fatalf("platform=%+v", out.Platform)
	}
}

func TestAdminOverviewReportsStorageFailures(t *testing.T) {
	for _, table := range []string{"portal_users", "portal_devices", "platform_settings"} {
		t.Run(table, func(t *testing.T) {
			env := newPlatformHTTPEnv(t)
			loadPortalTestUser(t, env, testPortalUsername)
			execRaw(t, env.dbPath, "ALTER TABLE "+table+" RENAME TO "+table+"_hidden")
			recorder := adminDirect(env.api.adminOverview, http.MethodGet, "/api/admin/overview", "", store.PortalUser{})
			if recorder.Code != http.StatusInternalServerError || errorCode(t, recorder.Body.Bytes()) != "INTERNAL" {
				t.Fatalf("%s: %d %s", table, recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestAdminRoutesRequireASessionAndUnknownPathsAre404(t *testing.T) {
	env := newPortalHTTPEnv(t)
	response, payload := env.do(t, http.MethodGet, "/api/admin/overview", "", nil)
	if response.StatusCode != http.StatusUnauthorized || errorCode(t, payload) != "UNAUTHORIZED" {
		t.Fatalf("anonymous: %d %s", response.StatusCode, payload)
	}
	env.login(t)
	response, payload = env.do(t, http.MethodGet, "/api/admin/does-not-exist", "", nil)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown: %d %s", response.StatusCode, payload)
	}
}
