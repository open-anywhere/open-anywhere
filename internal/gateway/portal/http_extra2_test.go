package portal

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

func TestNewAPIWiresTheApplicationService(t *testing.T) {
	api := NewAPI(openPortalTestDB(t), nil, portalTestService(t), nil, nil, "")
	if api.apps == nil {
		t.Fatal("a configured application service was not installed")
	}
	if api.web == nil || api.now == nil || api.serviceTest == nil || api.adminTest == nil {
		t.Fatalf("api=%+v", api)
	}
}

func TestHTTPRegisterStartsProvisioningAndRejectsAClosedPlatform(t *testing.T) {
	env := newPortalHTTPEnv(t)
	enablePortalRegistration(t, env)
	response, payload := env.do(t, http.MethodPost, "/api/portal/register",
		`{"username":"starter","password":"password123"}`,
		map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("register status=%d body=%s", response.StatusCode, payload)
	}
	var registered struct {
		Device DeviceView `json:"device"`
	}
	if err := json.Unmarshal(payload, &registered); err != nil {
		t.Fatalf("body %s: %v", payload, err)
	}
	if registered.Device.ID == "" {
		t.Fatalf("registration returned no device: %s", payload)
	}
	// Provisioning starts asynchronously; the unconfigured platform fails it and
	// the failure is recorded on the device.
	deadline := time.Now().Add(3 * time.Second)
	for {
		device, err := env.db.PortalDeviceByID(context.Background(), registered.Device.ID)
		if err != nil {
			t.Fatalf("load device: %v", err)
		}
		if device.State == store.PortalDeviceError {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("device was not provisioned: %+v", device)
		}
		time.Sleep(5 * time.Millisecond)
	}

	closed := newPlatformHTTPEnv(t)
	response, payload = closed.do(t, http.MethodPost, "/api/portal/register",
		`{"username":"late","password":"password123"}`,
		map[string]string{"Origin": closed.server.URL})
	if response.StatusCode != http.StatusForbidden || errorCode(t, payload) != "REGISTRATION_CLOSED" {
		t.Fatalf("closed platform status=%d body=%s", response.StatusCode, payload)
	}
}

func TestHTTPLogoutToleratesASessionStorageFailure(t *testing.T) {
	env := newPortalHTTPEnv(t)
	env.login(t)
	execRaw(t, env.dbPath, "ALTER TABLE portal_sessions RENAME TO portal_sessions_hidden")
	response, payload := env.do(t, http.MethodPost, "/api/portal/logout", `{}`,
		map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status=%d body=%s", response.StatusCode, payload)
	}
}

func TestHTTPDeviceAndAppHandlersReportStorageFailuresWithoutLosingSessions(t *testing.T) {
	env := newPortalHTTPEnv(t)
	env.login(t)
	execRaw(t, env.dbPath, "ALTER TABLE portal_devices RENAME TO portal_devices_hidden")
	origin := map[string]string{"Origin": env.server.URL}

	cases := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/portal/me", ""},
		{http.MethodGet, "/api/portal/devices", ""},
		{http.MethodPost, "/api/portal/devices", `{}`},
		{http.MethodGet, "/api/portal/apps", ""},
		{http.MethodGet, "/api/portal/apps/app_x", ""},
	}
	for _, testCase := range cases {
		response, payload := env.do(t, testCase.method, testCase.path, testCase.body, origin)
		if response.StatusCode != http.StatusInternalServerError {
			t.Errorf("%s %s status=%d body=%s", testCase.method, testCase.path, response.StatusCode, payload)
		}
	}
}

func TestHTTPCreateDeviceReportsAnInsertFailure(t *testing.T) {
	env := newPortalHTTPEnv(t)
	env.login(t)
	execRaw(t, env.dbPath,
		"CREATE TRIGGER gate_block_device_insert BEFORE INSERT ON portal_devices BEGIN SELECT RAISE(ABORT, 'blocked'); END")
	response, payload := env.do(t, http.MethodPost, "/api/portal/devices", `{}`,
		map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("insert failure status=%d body=%s", response.StatusCode, payload)
	}
}

func TestHTTPRetryDeviceReportsOwnershipAndUpdateFailures(t *testing.T) {
	env := newPortalHTTPEnv(t)
	env.login(t)
	user := loadPortalTestUser(t, env, testPortalUsername)
	origin := map[string]string{"Origin": env.server.URL}
	response, payload := env.do(t, http.MethodPost, "/api/portal/devices/dev_missing/retry", `{}`, origin)
	if response.StatusCode != http.StatusNotFound || errorCode(t, payload) != "NOT_FOUND" {
		t.Fatalf("missing retry status=%d body=%s", response.StatusCode, payload)
	}

	broken := createPortalTestDevice(t, env, "dev_retry", user.ID, "AA:BB:CC:00:02:01", "agent_retry", store.PortalDeviceError)
	execRaw(t, env.dbPath,
		"CREATE TRIGGER gate_block_device_update BEFORE UPDATE ON portal_devices BEGIN SELECT RAISE(ABORT, 'blocked'); END")
	response, payload = env.do(t, http.MethodPost, "/api/portal/devices/"+broken.ID+"/retry", `{}`, origin)
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("update failure status=%d body=%s", response.StatusCode, payload)
	}
}

func TestHTTPDeviceAccessRequiresOwnership(t *testing.T) {
	env := newPortalHTTPEnv(t)
	env.login(t)
	bob := createPortalTestUser(t, env, "usr_other", "other", "password123", store.PortalRoleUser)
	device := createPortalTestDevice(t, env, "dev_other", bob.ID, "AA:BB:CC:00:02:11", "agent_other", store.PortalDeviceRegistered)
	response, payload := env.do(t, http.MethodGet, "/api/portal/devices/"+device.ID, "", nil)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("another user's device status=%d body=%s", response.StatusCode, payload)
	}
}

func TestHTTPAppDetailReportsIdleStopsAndEmptyStates(t *testing.T) {
	env := newPortalHTTPEnv(t)
	env.login(t)
	user := loadPortalTestUser(t, env, testPortalUsername)
	device := createPortalTestDevice(t, env, "dev_detail", user.ID, "AA:BB:CC:00:02:21", "agent_detail", store.PortalDeviceRegistered)
	owner := app.Identity{DeviceID: device.MAC, AgentID: device.LinxAgentID}
	stoppedAt := time.Now().UTC().Add(-time.Minute)
	env.api.apps = portalTestService(t,
		app.App{ID: "app_idle", Owner: owner, State: app.StateIdle, StoppedAt: stoppedAt},
		app.App{ID: "app_unset", Owner: owner},
	)

	response, payload := env.do(t, http.MethodGet, "/api/portal/apps/app_idle", "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("idle detail status=%d body=%s", response.StatusCode, payload)
	}
	var detail struct {
		App struct {
			StoppedAt *time.Time `json:"stopped_at"`
		} `json:"app"`
	}
	if err := json.Unmarshal(payload, &detail); err != nil {
		t.Fatalf("body %s: %v", payload, err)
	}
	if detail.App.StoppedAt == nil {
		t.Fatalf("idle detail lost the stopped time: %s", payload)
	}

	response, payload = env.do(t, http.MethodGet, "/api/portal/apps/app_unset", "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unset detail status=%d body=%s", response.StatusCode, payload)
	}

	response, payload = env.do(t, http.MethodGet, "/api/portal/apps/app_missing/openapi.yaml", "", nil)
	if response.StatusCode != http.StatusNotFound || errorCode(t, payload) != "APP_NOT_FOUND" {
		t.Fatalf("missing openapi status=%d body=%s", response.StatusCode, payload)
	}
	response, payload = env.do(t, http.MethodDelete, "/api/portal/apps/app_missing", `{}`,
		map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusNotFound || errorCode(t, payload) != "APP_NOT_FOUND" {
		t.Fatalf("missing delete status=%d body=%s", response.StatusCode, payload)
	}
}

func TestHTTPAppListReportsAnOwnerLookupFailure(t *testing.T) {
	env := newPortalHTTPEnv(t)
	env.login(t)
	user := loadPortalTestUser(t, env, testPortalUsername)
	// The store rejects an empty MAC, but a legacy row can still carry one. It
	// cannot be mapped to an application owner.
	execRaw(t, env.dbPath, `INSERT INTO portal_devices
		(id, user_id, kind, mac, client_id, linx_agent_id, agent_name, state, step, error,
		 credential_path, created_at, updated_at, registered_at)
		VALUES ('dev_no_mac', '`+user.ID+`', 'virtual', '', 'client-no-mac', 'agent_no_mac',
		 'agent', 'registered', 'done', '', '', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', NULL)`)
	env.api.apps = portalTestService(t)
	response, payload := env.do(t, http.MethodGet, "/api/portal/apps", "", nil)
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("owner lookup status=%d body=%s", response.StatusCode, payload)
	}
}
