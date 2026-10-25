package portal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/service"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

func createPortalTestUser(t *testing.T, env *portalHTTPEnv, id, username, password, role string) store.PortalUser {
	t.Helper()
	hash, err := hashPassword(password)
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	now := time.Now().UTC()
	user := store.PortalUser{
		ID:           id,
		Username:     username,
		PasswordHash: hash,
		Role:         role,
		Status:       store.PortalUserActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := env.db.CreatePortalUser(context.Background(), user); err != nil {
		t.Fatalf("CreatePortalUser(%s): %v", username, err)
	}
	return user
}

func createPortalTestDevice(t *testing.T, env *portalHTTPEnv, id, userID, mac, agentID, state string) store.PortalDevice {
	t.Helper()
	now := time.Now().UTC()
	device := store.PortalDevice{
		ID:          id,
		UserID:      userID,
		Kind:        store.PortalDeviceVirtual,
		MAC:         mac,
		ClientID:    "client-" + id,
		LinxAgentID: agentID,
		AgentName:   "agent-" + id,
		State:       state,
		Step:        stepDone,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := env.db.CreatePortalDevice(context.Background(), device); err != nil {
		t.Fatalf("CreatePortalDevice(%s): %v", id, err)
	}
	return device
}

func loginAs(t *testing.T, env *portalHTTPEnv, username, password string) {
	t.Helper()
	response, payload := env.do(t, http.MethodPost, "/api/portal/login",
		`{"username":"`+username+`","password":"`+password+`"}`,
		map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("login %s status=%d body=%s", username, response.StatusCode, payload)
	}
}

func TestAdminOverviewRequiresAnAdmin(t *testing.T) {
	env := newPortalHTTPEnv(t)
	createPortalTestUser(t, env, "usr_carol", "carol", "password123", store.PortalRoleUser)
	loginAs(t, env, "carol", "password123")

	response, payload := env.do(t, http.MethodGet, "/api/admin/overview", "", nil)
	if response.StatusCode != http.StatusForbidden || errorCode(t, payload) != "ADMIN_REQUIRED" {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}
}

func TestAdminOverviewReportsPlatformCounts(t *testing.T) {
	env := newPortalHTTPEnv(t)
	env.login(t)
	carol := createPortalTestUser(t, env, "usr_carol", "carol", "password123", store.PortalRoleUser)
	createPortalTestDevice(t, env, "dev_carol", carol.ID, "AA:BB:CC:00:00:11", "", store.PortalDeviceProvisioning)

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
			Provisioning int `json:"provisioning"`
		} `json:"agents"`
		Platform struct {
			RegistrationOpen bool `json:"registration_open"`
			AgentAPI         bool `json:"agentapi"`
		} `json:"platform"`
	}
	if err := json.Unmarshal(payload, &out); err != nil {
		t.Fatalf("overview %s: %v", payload, err)
	}
	if out.Users.Total != 2 || out.Users.Active != 2 || out.Users.Disabled != 0 || out.Users.Admins != 1 {
		t.Fatalf("users=%+v", out.Users)
	}
	if out.Agents.Total != 1 || out.Agents.Provisioning != 1 {
		t.Fatalf("agents=%+v", out.Agents)
	}
	if out.Platform.RegistrationOpen || out.Platform.AgentAPI {
		t.Fatalf("unconfigured platform reported ready: %+v", out.Platform)
	}
}

func TestAdminAppsOmitScreenAndPromptFields(t *testing.T) {
	env := newPortalHTTPEnv(t)
	env.login(t)
	admin, err := env.db.PortalUserByUsername(context.Background(), testPortalUsername)
	if err != nil {
		t.Fatal(err)
	}
	device := createPortalTestDevice(t, env, "dev_admin", admin.ID, "AA:BB:CC:00:00:21", "agent_admin", store.PortalDeviceRegistered)
	registry := app.NewRegistry()
	registry.Add(app.App{
		ID:                  "app_leak",
		Owner:               app.Identity{DeviceID: device.MAC, AgentID: device.LinxAgentID},
		Prompt:              "隐藏的提示词摘要",
		Name:                "quiz",
		State:               app.StateRunning,
		ScreenID:            "screen_secret",
		TargetID:            "target_secret",
		ProjectionSessionID: "projection_secret",
		BuildStartedAt:      time.Now().UTC(),
	})
	env.api.apps = service.NewWithBindings(registry, nil, nil, nil, nil, service.Config{})

	response, payload := env.do(t, http.MethodGet, "/api/admin/apps", "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}
	text := string(payload)
	for _, forbidden := range []string{
		"screen_id", "target_id", "projection_session_id", "summary",
		"screen_secret", "target_secret", "projection_secret", "隐藏的提示词摘要",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("admin apps leak %q: %s", forbidden, payload)
		}
	}
	if !strings.Contains(text, `"app_id":"app_leak"`) || !strings.Contains(text, `"username":"alice"`) {
		t.Fatalf("the app or its owner is missing: %s", payload)
	}
}

func TestAdminSetStatusRefusesTheLastAdmin(t *testing.T) {
	env := newPortalHTTPEnv(t)
	admin, err := env.db.PortalUserByUsername(context.Background(), testPortalUsername)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/admin/users/"+admin.ID+"/status",
		strings.NewReader(`{"status":"disabled"}`))
	request.Header.Set("Content-Type", "application/json")
	request.SetPathValue("id", admin.ID)
	recorder := httptest.NewRecorder()
	// The authenticated route can only reach this state concurrently: a
	// different active actor would already be a second administrator, and an
	// actor who is the target hits CANNOT_DISABLE_SELF first. Call the handler
	// with a synthetic peer administrator so the guarded update is exercised
	// deterministically.
	env.api.adminSetUserStatus(recorder, request, store.PortalUser{
		ID: "usr_peer_admin", Role: store.PortalRoleAdmin, Status: store.PortalUserActive,
	})
	if recorder.Code != http.StatusConflict || errorCode(t, recorder.Body.Bytes()) != "LAST_ADMIN" {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	stored, err := env.db.PortalUserByID(context.Background(), admin.ID)
	if err != nil || stored.Status != store.PortalUserActive {
		t.Fatalf("the last admin was disabled: %+v err=%v", stored, err)
	}
}

func TestAdminCannotDisableOwnAccount(t *testing.T) {
	env := newPortalHTTPEnv(t)
	env.login(t)
	admin, err := env.db.PortalUserByUsername(context.Background(), testPortalUsername)
	if err != nil {
		t.Fatal(err)
	}
	response, payload := env.do(t, http.MethodPost, "/api/admin/users/"+admin.ID+"/status",
		`{"status":"disabled"}`, map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusConflict || errorCode(t, payload) != "CANNOT_DISABLE_SELF" {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}
}

func TestAdminRetryRejectsAnAgentOfADisabledUser(t *testing.T) {
	env := newPortalHTTPEnv(t)
	env.login(t)
	carol := createPortalTestUser(t, env, "usr_carol", "carol", "password123", store.PortalRoleUser)
	device := createPortalTestDevice(t, env, "dev_carol", carol.ID, "AA:BB:CC:00:00:31", "agent_carol", store.PortalDeviceRegistered)

	response, payload := env.do(t, http.MethodPost, "/api/admin/users/"+carol.ID+"/status",
		`{"status":"disabled"}`, map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("disable status=%d body=%s", response.StatusCode, payload)
	}

	response, payload = env.do(t, http.MethodPost, "/api/admin/agents/"+device.ID+"/retry",
		`{}`, map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusConflict || errorCode(t, payload) != "USER_DISABLED" {
		t.Fatalf("retry status=%d body=%s", response.StatusCode, payload)
	}
}
