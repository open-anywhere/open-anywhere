package portal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/endpoints"
	"github.com/open-anywhere/open-anywhere/internal/gateway/netguard"
	"github.com/open-anywhere/open-anywhere/internal/gateway/secret"
	"github.com/open-anywhere/open-anywhere/internal/gateway/service"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

func useStrictResolver(t *testing.T, env *portalHTTPEnv) {
	t.Helper()
	box, err := secret.New(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatalf("secret.New: %v", err)
	}
	env.api.SetPlatform(env.api.settings, env.api.pool,
		endpoints.NewResolver(env.db, env.api.settings, box, netguard.New(false)))
}

func TestPortalServicesSaveCustomAndManagedBindings(t *testing.T) {
	env := newPlatformHTTPEnv(t)
	env.login(t)
	user := loadPortalTestUser(t, env, testPortalUsername)
	renderServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer renderServer.Close()
	body := fmt.Sprintf(`{"mode":"custom","render_url":%q,"render_token":"render-key"}`,
		renderServer.URL)
	response, payload := env.do(t, http.MethodPut, "/api/portal/services", body,
		map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("save custom: %d %s", response.StatusCode, payload)
	}
	var view struct {
		Mode   string `json:"mode"`
		Ready  bool   `json:"ready"`
		Custom struct {
			RenderURL             string `json:"render_url"`
			RenderTokenConfigured bool   `json:"render_token_configured"`
		} `json:"custom"`
	}
	if err := json.Unmarshal(payload, &view); err != nil {
		t.Fatalf("body %s: %v", payload, err)
	}
	if view.Mode != store.ServiceModeCustom || !view.Ready || view.Custom.RenderURL != renderServer.URL ||
		!view.Custom.RenderTokenConfigured {
		t.Fatalf("view=%+v body=%s", view, payload)
	}

	response, payload = env.do(t, http.MethodPut, "/api/portal/services",
		`{"mode":"managed","clear_render_token":true}`,
		map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("save managed: %d %s", response.StatusCode, payload)
	}
	if err := json.Unmarshal(payload, &view); err != nil {
		t.Fatal(err)
	}
	if view.Mode != store.ServiceModeManaged || view.Ready ||
		view.Custom.RenderTokenConfigured {
		t.Fatalf("managed view=%+v body=%s", view, payload)
	}
	saved, err := env.db.ServiceBindingByUser(context.Background(), user.ID)
	if err != nil || saved.Mode != store.ServiceModeManaged || saved.RenderTokenEnc != "" {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
}

func TestPortalServicesSaveRejectsBadInput(t *testing.T) {
	env := newPlatformHTTPEnv(t)
	env.login(t)
	origin := map[string]string{"Origin": env.server.URL}

	response, payload := env.do(t, http.MethodPut, "/api/portal/services", `{"mode":`,
		origin)
	if response.StatusCode != http.StatusBadRequest || errorCode(t, payload) != "INVALID_ARGUMENT" {
		t.Fatalf("malformed: %d %s", response.StatusCode, payload)
	}
	response, payload = env.do(t, http.MethodPut, "/api/portal/services", `{"mode":"sideways"}`, origin)
	if response.StatusCode != http.StatusBadRequest || errorCode(t, payload) != "INVALID_ARGUMENT" {
		t.Fatalf("invalid mode: %d %s", response.StatusCode, payload)
	}
	response, payload = env.do(t, http.MethodPut, "/api/portal/services", `{"mode":"custom"}`, origin)
	if response.StatusCode != http.StatusConflict || errorCode(t, payload) != "SERVICE_NOT_CONFIGURED" {
		t.Fatalf("incomplete: %d %s", response.StatusCode, payload)
	}
	response, payload = env.do(t, http.MethodPut, "/api/portal/services",
		`{"mode":"custom","render_url":"ftp://render.example.com"}`, origin)
	if response.StatusCode != http.StatusBadRequest || errorCode(t, payload) != netguard.CodeInvalidURL {
		t.Fatalf("invalid url: %d %s", response.StatusCode, payload)
	}

	useStrictResolver(t, env)
	response, payload = env.do(t, http.MethodPut, "/api/portal/services",
		`{"mode":"custom","render_url":"http://127.0.0.1:9000"}`, origin)
	if response.StatusCode != http.StatusBadRequest || errorCode(t, payload) != netguard.CodePrivateTarget {
		t.Fatalf("private target: %d %s", response.StatusCode, payload)
	}

	plain := newPlatformHTTPEnv(t)
	plain.login(t)
	plain.api.resolver = nil
	response, payload = plain.do(t, http.MethodPut, "/api/portal/services", `{"mode":"managed"}`,
		map[string]string{"Origin": plain.server.URL})
	if response.StatusCode != http.StatusServiceUnavailable || errorCode(t, payload) != "SERVICE_UNAVAILABLE" {
		t.Fatalf("no resolver: %d %s", response.StatusCode, payload)
	}
}

func TestPortalServicesSaveReportsStorageAndRuntimeConflicts(t *testing.T) {
	env := newPlatformHTTPEnv(t)
	env.login(t)
	user := loadPortalTestUser(t, env, testPortalUsername)
	device := createPortalTestDevice(t, env, "dev_admin", user.ID, "AA:BB:CC:00:05:01", "agent_admin", store.PortalDeviceRegistered)
	env.api.apps = adminService(app.App{
		ID: "app_live", Owner: app.Identity{DeviceID: device.MAC, AgentID: device.LinxAgentID},
		State: app.StateRunning,
	})
	response, payload := env.do(t, http.MethodPut, "/api/portal/services",
		`{"mode":"managed"}`, map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusConflict || errorCode(t, payload) != "APPS_RUNNING" {
		t.Fatalf("active runtime: %d %s", response.StatusCode, payload)
	}

	broken := newPlatformHTTPEnv(t)
	broken.login(t)
	execRaw(t, broken.dbPath, "ALTER TABLE portal_devices RENAME TO portal_devices_hidden")
	response, payload = broken.do(t, http.MethodPut, "/api/portal/services",
		`{"mode":"managed"}`, map[string]string{"Origin": broken.server.URL})
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("device failure: %d %s", response.StatusCode, payload)
	}

	storage := newPlatformHTTPEnv(t)
	storage.login(t)
	execRaw(t, storage.dbPath, "ALTER TABLE portal_service_bindings RENAME TO portal_service_bindings_hidden")
	response, payload = storage.do(t, http.MethodPut, "/api/portal/services",
		`{"mode":"managed"}`, map[string]string{"Origin": storage.server.URL})
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("binding failure: %d %s", response.StatusCode, payload)
	}
}

func TestPortalServicesViewReportsEveryFailure(t *testing.T) {
	env := newPlatformHTTPEnv(t)
	env.login(t)
	response, payload := env.do(t, http.MethodGet, "/api/portal/services", "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("managed default: %d %s", response.StatusCode, payload)
	}
	var view struct {
		Mode    string `json:"mode"`
		Ready   bool   `json:"ready"`
		Managed struct {
			Available bool `json:"available"`
		} `json:"managed"`
	}
	if err := json.Unmarshal(payload, &view); err != nil {
		t.Fatalf("body %s: %v", payload, err)
	}
	if view.Mode != store.ServiceModeManaged || view.Ready || view.Managed.Available {
		t.Fatalf("default view=%+v", view)
	}

	broken := newPlatformHTTPEnv(t)
	broken.login(t)
	execRaw(t, broken.dbPath, "ALTER TABLE portal_service_bindings RENAME TO portal_service_bindings_hidden")
	response, payload = broken.do(t, http.MethodGet, "/api/portal/services", "", nil)
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("binding failure: %d %s", response.StatusCode, payload)
	}

	settingsBroken := newPlatformHTTPEnv(t)
	settingsBroken.login(t)
	execRaw(t, settingsBroken.dbPath, "ALTER TABLE platform_settings RENAME TO platform_settings_hidden")
	response, payload = settingsBroken.do(t, http.MethodGet, "/api/portal/services", "", nil)
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("settings failure: %d %s", response.StatusCode, payload)
	}
}

func TestPortalServiceTestsCoverPolicyAndRateLimits(t *testing.T) {
	env := newPlatformHTTPEnv(t)
	env.login(t)
	origin := map[string]string{"Origin": env.server.URL}

	response, payload := env.do(t, http.MethodPost, "/api/portal/services/test", `{"mode":`, origin)
	if response.StatusCode != http.StatusBadRequest || errorCode(t, payload) != "INVALID_ARGUMENT" {
		t.Fatalf("malformed: %d %s", response.StatusCode, payload)
	}
	response, payload = env.do(t, http.MethodPost, "/api/portal/services/test", `{"mode":"custom"}`, origin)
	if response.StatusCode != http.StatusConflict || errorCode(t, payload) != "SERVICE_NOT_CONFIGURED" {
		t.Fatalf("incomplete: %d %s", response.StatusCode, payload)
	}
	response, payload = env.do(t, http.MethodPost, "/api/portal/services/test",
		`{"mode":"custom","render_url":"ftp://render.example.com"}`, origin)
	if response.StatusCode != http.StatusBadRequest || errorCode(t, payload) != netguard.CodeInvalidURL {
		t.Fatalf("invalid render: %d %s", response.StatusCode, payload)
	}

	useStrictResolver(t, env)
	response, payload = env.do(t, http.MethodPost, "/api/portal/services/test",
		`{"mode":"custom","render_url":"http://10.0.0.5"}`, origin)
	if response.StatusCode != http.StatusBadRequest || errorCode(t, payload) != netguard.CodePrivateTarget {
		t.Fatalf("private target: %d %s", response.StatusCode, payload)
	}

	env.api.serviceTest = newWindowLimiter(env.api.now, time.Minute, 1)
	response, _ = env.do(t, http.MethodPost, "/api/portal/services/test", `{"mode":"managed"}`, origin)
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusConflict && response.StatusCode != http.StatusBadGateway {
		t.Fatalf("first limited probe: %d", response.StatusCode)
	}
	response, payload = env.do(t, http.MethodPost, "/api/portal/services/test", `{"mode":"managed"}`, origin)
	if response.StatusCode != http.StatusTooManyRequests || errorCode(t, payload) != "SERVICE_TEST_RATE_LIMITED" {
		t.Fatalf("rate limit: %d %s", response.StatusCode, payload)
	}

	plain := newPortalHTTPEnv(t)
	plain.login(t)
	response, payload = plain.do(t, http.MethodPost, "/api/portal/services/test", `{}`,
		map[string]string{"Origin": plain.server.URL})
	if response.StatusCode != http.StatusServiceUnavailable || errorCode(t, payload) != "SERVICE_UNAVAILABLE" {
		t.Fatalf("no platform: %d %s", response.StatusCode, payload)
	}
}

func TestServiceErrorMappingHelpers(t *testing.T) {
	cases := []struct {
		err        error
		wantStatus int
		wantCode   string
	}{
		{&netguard.Error{Code: netguard.CodePrivateTarget}, http.StatusBadRequest, netguard.CodePrivateTarget},
		{&netguard.Error{Code: netguard.CodeInvalidURL}, http.StatusBadRequest, netguard.CodeInvalidURL},
		{&netguard.Error{Code: netguard.CodeTargetUnreach}, http.StatusBadGateway, netguard.CodeTargetUnreach},
	}
	for _, test := range cases {
		var apiErr *APIError
		if err := mapURLPolicyError(test.err); !errors.As(err, &apiErr) || apiErr.Status != test.wantStatus || apiErr.Code != test.wantCode {
			t.Fatalf("%v -> %#v", test.err, err)
		}
	}
	plain := errors.New("boom")
	if got := mapURLPolicyError(plain); got != plain {
		t.Fatalf("plain error = %v", got)
	}

	bindingCases := []struct {
		err        error
		wantStatus int
		wantCode   string
	}{
		{&service.ToolError{Code: "SERVICE_NOT_CONFIGURED"}, http.StatusConflict, "SERVICE_NOT_CONFIGURED"},
		{&service.ToolError{Code: "INVALID_ARGUMENT"}, http.StatusBadRequest, "INVALID_ARGUMENT"},
		{&service.ToolError{Code: "OWNER_NOT_FOUND"}, http.StatusNotFound, "OWNER_NOT_FOUND"},
		{&netguard.Error{Code: netguard.CodePrivateTarget}, http.StatusBadRequest, netguard.CodePrivateTarget},
	}
	for _, test := range bindingCases {
		var apiErr *APIError
		if err := mapBindingError(test.err); !errors.As(err, &apiErr) || apiErr.Status != test.wantStatus || apiErr.Code != test.wantCode {
			t.Fatalf("%v -> %#v", test.err, err)
		}
	}
	if got := mapBindingError(plain); got != plain {
		t.Fatalf("plain binding error = %v", got)
	}
	for _, test := range []struct {
		wait time.Duration
		want string
	}{{0, "1"}, {200 * time.Millisecond, "1"}, {1400 * time.Millisecond, "1"}, {1500 * time.Millisecond, "2"}, {10 * time.Second, "10"}} {
		if got := secondsText(test.wait); got != test.want {
			t.Fatalf("secondsText(%v) = %q want %q", test.wait, got, test.want)
		}
	}
}
