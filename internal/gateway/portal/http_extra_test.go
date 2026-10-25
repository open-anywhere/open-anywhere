package portal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/openapi"
	"github.com/open-anywhere/open-anywhere/internal/gateway/secret"
	"github.com/open-anywhere/open-anywhere/internal/gateway/service"
	"github.com/open-anywhere/open-anywhere/internal/gateway/settings"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

func portalTestSpec(t *testing.T) *openapi.Spec {
	t.Helper()
	spec, err := openapi.Parse([]byte(`{
		"openapi": "3.1.0",
		"info": {"title": "quiz", "version": "1.0.0"},
		"paths": {"/api/play": {"post": {"operationId": "play", "summary": "Play"}}}
	}`))
	if err != nil {
		t.Fatalf("openapi.Parse: %v", err)
	}
	return spec
}

func portalTestService(t *testing.T, values ...app.App) *service.Service {
	t.Helper()
	registry := app.NewRegistry()
	for _, value := range values {
		registry.Add(value)
	}
	return service.NewWithBindings(registry, nil, nil, nil, nil, service.Config{})
}

func enablePortalRegistration(t *testing.T, env *portalHTTPEnv) *settings.Provider {
	t.Helper()
	box, err := secret.New(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatalf("secret.New: %v", err)
	}
	provider := settings.New(env.db, box)
	open := true
	if err := provider.Update(context.Background(), settings.Patch{
		Registration: &settings.RegistrationPatch{Open: &open},
	}, "usr_admin"); err != nil {
		t.Fatalf("open registration: %v", err)
	}
	env.api.settings = provider
	return provider
}

func TestHTTPRegistrationAndVirtualDeviceCreation(t *testing.T) {
	env := newPortalHTTPEnv(t)
	enablePortalRegistration(t, env)
	env.api.provision = nil // registration must not depend on the provisioner

	response, payload := env.do(t, http.MethodPost, "/api/portal/register",
		`{"username":"Newcomer","password":"password123"}`,
		map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}
	var registered struct {
		User   map[string]any `json:"user"`
		Device DeviceView     `json:"device"`
	}
	if err := json.Unmarshal(payload, &registered); err != nil {
		t.Fatalf("body %s: %v", payload, err)
	}
	if registered.User["username"] != "newcomer" || registered.Device.ID == "" ||
		registered.Device.Kind != store.PortalDeviceVirtual || registered.Device.State != store.PortalDeviceProvisioning {
		t.Fatalf("registration=%s", payload)
	}
	if len(response.Cookies()) != 1 || response.Cookies()[0].Name != SessionCookie {
		t.Fatalf("cookies=%v", response.Cookies())
	}

	// A second registration with the same name is a conflict.
	response, payload = env.do(t, http.MethodPost, "/api/portal/register",
		`{"username":"newcomer","password":"password123"}`,
		map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusConflict || errorCode(t, payload) != "USERNAME_TAKEN" {
		t.Fatalf("duplicate status=%d body=%s", response.StatusCode, payload)
	}

	// Malformed JSON is a client error, not a crash.
	response, payload = env.do(t, http.MethodPost, "/api/portal/register", `{`,
		map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusBadRequest || errorCode(t, payload) != "INVALID_ARGUMENT" {
		t.Fatalf("malformed status=%d body=%s", response.StatusCode, payload)
	}
}

func TestHTTPCreateDeviceReusesTheVirtualDevice(t *testing.T) {
	env := newPortalHTTPEnv(t)
	user := createPortalTestUser(t, env, "usr_make", "maker", "password123", store.PortalRoleUser)
	loginAs(t, env, "maker", "password123")
	runCtx, cancelRun := context.WithCancel(context.Background())
	cancelRun()
	env.api.provision = NewProvisioner(runCtx, env.db, nil, env.api.voice, ProvisionConfig{DataDir: env.dataDir})
	origin := map[string]string{"Origin": env.server.URL}

	response, payload := env.do(t, http.MethodPost, "/api/portal/devices", `{}`, origin)
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("create status=%d body=%s", response.StatusCode, payload)
	}
	var created struct {
		Device DeviceView `json:"device"`
	}
	if err := json.Unmarshal(payload, &created); err != nil {
		t.Fatalf("body %s: %v", payload, err)
	}
	if created.Device.ID == "" || created.Device.MAC == "" || created.Device.ClientID == "" {
		t.Fatalf("device=%+v", created.Device)
	}
	stored, err := env.db.PortalDevicesByUser(context.Background(), user.ID)
	if err != nil || len(stored) != 1 || stored[0].ID != created.Device.ID {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}

	// Creating again returns the existing device instead of a second one.
	response, payload = env.do(t, http.MethodPost, "/api/portal/devices", `{}`, origin)
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("second create status=%d body=%s", response.StatusCode, payload)
	}
	var again struct {
		Device DeviceView `json:"device"`
	}
	if err := json.Unmarshal(payload, &again); err != nil || again.Device.ID != created.Device.ID {
		t.Fatalf("again=%s err=%v", payload, err)
	}
}

func TestHTTPLoginRejectsMalformedJSON(t *testing.T) {
	env := newPortalHTTPEnv(t)
	response, payload := env.do(t, http.MethodPost, "/api/portal/login", `{`,
		map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusBadRequest || errorCode(t, payload) != "INVALID_ARGUMENT" {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}
}

func TestHTTPGetDeviceAndRetryStates(t *testing.T) {
	env := newPortalHTTPEnv(t)
	env.login(t)
	user := loadPortalTestUser(t, env, testPortalUsername)
	device := createPortalTestDevice(t, env, "dev_retry", user.ID, "AA:BB:CC:00:00:51", "agent_retry", store.PortalDeviceProvisioning)
	origin := map[string]string{"Origin": env.server.URL}

	response, payload := env.do(t, http.MethodGet, "/api/portal/devices/"+device.ID, "", nil)
	if response.StatusCode != http.StatusOK || !strings.Contains(string(payload), device.ID) {
		t.Fatalf("get status=%d body=%s", response.StatusCode, payload)
	}

	// A running provisioning iteration rejects a concurrent retry.
	env.api.provision.mu.Lock()
	env.api.provision.running[device.ID] = true
	env.api.provision.mu.Unlock()
	response, payload = env.do(t, http.MethodPost, "/api/portal/devices/"+device.ID+"/retry", `{}`, origin)
	if response.StatusCode != http.StatusConflict || errorCode(t, payload) != "DEVICE_BUSY" {
		t.Fatalf("busy status=%d body=%s", response.StatusCode, payload)
	}
	env.api.provision.mu.Lock()
	delete(env.api.provision.running, device.ID)
	env.api.provision.mu.Unlock()
	// The background run would race with the assertions below; give the
	// provisioner an already-canceled context so Start records the device and
	// returns without touching the platform.
	runCtx, cancelRun := context.WithCancel(context.Background())
	cancelRun()
	env.api.provision = NewProvisioner(runCtx, env.db, nil, env.api.voice, ProvisionConfig{DataDir: env.dataDir})

	response, payload = env.do(t, http.MethodPost, "/api/portal/devices/"+device.ID+"/retry", `{}`, origin)
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("retry status=%d body=%s", response.StatusCode, payload)
	}
	stored, err := env.db.PortalDeviceByID(context.Background(), device.ID)
	if err != nil || stored.State != store.PortalDeviceProvisioning {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}

	// A registered device without a provisioner needs no voice restart.
	stored.State = store.PortalDeviceRegistered
	stored.Step = stepDone
	if err := env.db.UpdatePortalDevice(context.Background(), stored); err != nil {
		t.Fatal(err)
	}
	env.api.provision = nil
	response, payload = env.do(t, http.MethodPost, "/api/portal/devices/"+device.ID+"/retry", `{}`, origin)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("registered retry status=%d body=%s", response.StatusCode, payload)
	}
}

func TestHTTPAppListDetailOpenAPIAndDelete(t *testing.T) {
	env := newPortalHTTPEnv(t)
	env.login(t)
	user := loadPortalTestUser(t, env, testPortalUsername)
	device := createPortalTestDevice(t, env, "dev_apps", user.ID, "AA:BB:CC:00:00:61", "agent_apps", store.PortalDeviceRegistered)
	owner := app.Identity{DeviceID: device.MAC, AgentID: device.LinxAgentID}
	now := time.Now().UTC()
	spec := portalTestSpec(t)
	running := app.App{
		ID: "app_running", Owner: owner, Name: "quiz", State: app.StateRunning,
		ScreenID: "tv_1", StartedAt: now.Add(-time.Minute), BuildStartedAt: now.Add(-time.Hour),
		BuildFinishedAt: now.Add(-30 * time.Minute), BuildDuration: 30 * time.Minute, OpenAPI: spec,
	}
	building := app.App{
		ID: "app_building", Owner: owner, Name: "draw", State: app.StateBuilding,
		BuildStartedAt: now.Add(-time.Minute), OpenAPI: spec,
	}
	idle := app.App{
		ID: "app_idle", Owner: owner, Name: "trivia", State: app.StateIdle,
		BuildStartedAt: now.Add(-2 * time.Hour), StoppedAt: now.Add(-time.Minute),
	}
	broken := app.App{ID: "app_broken", Owner: owner, Name: "oops", State: app.StateError}
	env.api.apps = portalTestService(t, running, building, idle, broken)

	response, payload := env.do(t, http.MethodGet, "/api/portal/apps", "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d body=%s", response.StatusCode, payload)
	}
	for _, id := range []string{"app_running", "app_building", "app_idle", "app_broken"} {
		if !strings.Contains(string(payload), id) {
			t.Errorf("the list is missing %s: %s", id, payload)
		}
	}

	response, payload = env.do(t, http.MethodGet, "/api/portal/apps/app_running", "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", response.StatusCode, payload)
	}
	var detail struct {
		App struct {
			AppView
			Message          string                  `json:"message"`
			UptimeSeconds    int64                   `json:"uptime_seconds"`
			Operations       []service.OperationView `json:"operations"`
			OpenAPIAvailable bool                    `json:"openapi_available"`
		} `json:"app"`
	}
	if err := json.Unmarshal(payload, &detail); err != nil {
		t.Fatalf("body %s: %v", payload, err)
	}
	if detail.App.ScreenID != "tv_1" || detail.App.StartedAt == nil || detail.App.UptimeSeconds < 0 ||
		len(detail.App.Operations) != 1 || detail.App.Operations[0].OperationID != "play" || !detail.App.OpenAPIAvailable {
		t.Fatalf("detail=%+v", detail.App)
	}

	// A building app has no operations yet, but its detail still loads.
	response, payload = env.do(t, http.MethodGet, "/api/portal/apps/app_building", "", nil)
	if response.StatusCode != http.StatusOK || !strings.Contains(string(payload), `"state":"building"`) {
		t.Fatalf("building status=%d body=%s", response.StatusCode, payload)
	}

	response, payload = env.do(t, http.MethodGet, "/api/portal/apps/app_broken", "", nil)
	if response.StatusCode != http.StatusOK || !strings.Contains(string(payload), `"openapi_available":false`) {
		t.Fatalf("broken status=%d body=%s", response.StatusCode, payload)
	}

	response, payload = env.do(t, http.MethodGet, "/api/portal/apps/app_missing", "", nil)
	if response.StatusCode != http.StatusNotFound || errorCode(t, payload) != "APP_NOT_FOUND" {
		t.Fatalf("missing status=%d body=%s", response.StatusCode, payload)
	}

	response, payload = env.do(t, http.MethodGet, "/api/portal/apps/app_running/openapi.yaml", "", nil)
	if response.StatusCode != http.StatusOK || !strings.Contains(string(payload), "openapi") {
		t.Fatalf("openapi status=%d body=%s", response.StatusCode, payload)
	}
	if response.Header.Get("Content-Type") != "text/yaml; charset=utf-8" || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("openapi headers=%v", response.Header)
	}
	response, payload = env.do(t, http.MethodGet, "/api/portal/apps/app_running/openapi.yaml?download=1", "", nil)
	if response.StatusCode != http.StatusOK || !strings.Contains(response.Header.Get("Content-Disposition"), "openapi.yaml") {
		t.Fatalf("download status=%d headers=%v body=%s", response.StatusCode, response.Header, payload)
	}
	response, payload = env.do(t, http.MethodGet, "/api/portal/apps/app_building/openapi.yaml", "", nil)
	if response.StatusCode != http.StatusConflict || errorCode(t, payload) != "APP_BUILDING" {
		t.Fatalf("building openapi status=%d body=%s", response.StatusCode, payload)
	}
	response, payload = env.do(t, http.MethodGet, "/api/portal/apps/app_broken/openapi.yaml", "", nil)
	if response.StatusCode != http.StatusConflict || errorCode(t, payload) != "APP_IN_ERROR" {
		t.Fatalf("broken openapi status=%d body=%s", response.StatusCode, payload)
	}

	response, payload = env.do(t, http.MethodDelete, "/api/portal/apps/app_running", `{}`, map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusConflict || errorCode(t, payload) != "APP_BUSY" {
		t.Fatalf("running delete status=%d body=%s", response.StatusCode, payload)
	}
	response, payload = env.do(t, http.MethodDelete, "/api/portal/apps/app_idle", `{}`, map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("idle delete status=%d body=%s", response.StatusCode, payload)
	}
	response, payload = env.do(t, http.MethodGet, "/api/portal/apps/app_idle", "", nil)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("deleted app status=%d body=%s", response.StatusCode, payload)
	}
}

func TestHTTPHandlersReportStorageFailures(t *testing.T) {
	env := newPortalHTTPEnv(t)
	enablePortalRegistration(t, env)
	env.login(t)
	if err := env.db.Close(); err != nil {
		t.Fatal(err)
	}
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
		{http.MethodPost, "/api/portal/register", `{"username":"late","password":"password123"}`},
	}
	for _, testCase := range cases {
		response, payload := env.do(t, testCase.method, testCase.path, testCase.body, origin)
		if response.StatusCode != http.StatusInternalServerError {
			t.Errorf("%s %s status=%d body=%s", testCase.method, testCase.path, response.StatusCode, payload)
		}
	}
}

func TestHTTPRegistrationReportsASettingsFailure(t *testing.T) {
	env := newPortalHTTPEnv(t)
	enablePortalRegistration(t, env)
	if err := env.db.Close(); err != nil {
		t.Fatal(err)
	}
	response, payload := env.do(t, http.MethodPost, "/api/portal/register",
		`{"username":"late","password":"password123"}`, map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}
}

func TestHTTPScreenHandlersReportServiceFailures(t *testing.T) {
	env := newPortalHTTPEnv(t)
	env.login(t)
	env.api.screens = &fakeScreens{viewErr: errors.New("casting down")}
	response, payload := env.do(t, http.MethodGet, "/api/portal/screens", "", nil)
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("list status=%d body=%s", response.StatusCode, payload)
	}
	env.api.screens = &fakeScreens{stopErr: errors.New("casting down")}
	response, payload = env.do(t, http.MethodPost, "/api/portal/screens/tv_1/stop", `{}`, map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("stop status=%d body=%s", response.StatusCode, payload)
	}
	env.api.screens = nil
	response, payload = env.do(t, http.MethodGet, "/api/portal/screens", "", nil)
	if response.StatusCode != http.StatusServiceUnavailable || errorCode(t, payload) != "SCREEN_DISCOVERY_FAILED" {
		t.Fatalf("unavailable status=%d body=%s", response.StatusCode, payload)
	}
	response, payload = env.do(t, http.MethodPost, "/api/portal/screens/tv_1/stop", `{}`, map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unavailable stop status=%d body=%s", response.StatusCode, payload)
	}

	// Ownership lookup failures surface as storage errors.
	env.api.screens = &fakeScreens{}
	if err := env.db.Close(); err != nil {
		t.Fatal(err)
	}
	response, payload = env.do(t, http.MethodGet, "/api/portal/screens", "", nil)
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("closed list status=%d body=%s", response.StatusCode, payload)
	}
}

func TestHTTPVoiceHandlerDeviceStates(t *testing.T) {
	env := newPortalHTTPEnv(t)
	env.login(t)
	user := loadPortalTestUser(t, env, testPortalUsername)

	response, payload := env.do(t, http.MethodGet, "/voice/dev_missing", "", nil)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("no tail status=%d body=%s", response.StatusCode, payload)
	}

	device := createPortalTestDevice(t, env, "dev_wait", user.ID, "AA:BB:CC:00:00:71", "agent_wait", store.PortalDeviceProvisioning)
	response, payload = env.do(t, http.MethodGet, "/voice/"+device.ID+"/status", "", nil)
	if response.StatusCode != http.StatusConflict || errorCode(t, payload) != "DEVICE_NOT_READY" {
		t.Fatalf("not ready status=%d body=%s", response.StatusCode, payload)
	}

	// Once the same device registers and its runtime starts, the control API is
	// mounted behind the portal prefix.
	device.State = store.PortalDeviceRegistered
	device.Step = stepDone
	device.CredentialPath = writeCredential(t, env.dataDir, device.ID)
	if err := env.db.UpdatePortalDevice(context.Background(), device); err != nil {
		t.Fatal(err)
	}
	if err := env.api.voice.Start(device); err != nil {
		t.Fatalf("VoiceManager.Start: %v", err)
	}
	response, payload = env.do(t, http.MethodGet, "/voice/"+device.ID+"/status", "", nil)
	if response.StatusCode == http.StatusServiceUnavailable {
		t.Fatalf("a running runtime was reported missing: %d %s", response.StatusCode, payload)
	}
}

func TestMapServiceErrorCoversEveryCode(t *testing.T) {
	cases := []struct {
		code   string
		status int
		out    string
	}{
		{"APP_NOT_FOUND", http.StatusNotFound, "APP_NOT_FOUND"},
		{"APP_BUILDING", http.StatusConflict, "APP_BUILDING"},
		{"APP_ALREADY_RUNNING", http.StatusConflict, "APP_ALREADY_RUNNING"},
		{"APP_NOT_RUNNING", http.StatusConflict, "APP_NOT_RUNNING"},
		{"CLEANUP_PENDING", http.StatusConflict, "CLEANUP_PENDING"},
		{"APP_BUSY", http.StatusConflict, "APP_BUSY"},
		{"APP_IN_ERROR", http.StatusConflict, "APP_IN_ERROR"},
		{"SCREEN_APP_NOT_FOUND", http.StatusNotFound, "SCREEN_APP_NOT_FOUND"},
		{"SCREEN_DISCOVERY_FAILED", http.StatusServiceUnavailable, "SCREEN_DISCOVERY_FAILED"},
		{"SERVICE_NOT_CONFIGURED", http.StatusConflict, "SERVICE_NOT_CONFIGURED"},
		{"PLATFORM_NOT_CONFIGURED", http.StatusConflict, "PLATFORM_NOT_CONFIGURED"},
		{"TARGET_BUSY", http.StatusConflict, "TARGET_BUSY"},
		{"INVALID_TARGET_URL", http.StatusBadRequest, "INVALID_TARGET_URL"},
		{"PRIVATE_TARGET_FORBIDDEN", http.StatusBadRequest, "PRIVATE_TARGET_FORBIDDEN"},
		{"TARGET_UNREACHABLE", http.StatusBadGateway, "TARGET_UNREACHABLE"},
		{"STOP_FAILED", http.StatusBadGateway, "STOP_FAILED"},
		{"OPERATION_NOT_FOUND", http.StatusNotFound, "OPERATION_NOT_FOUND"},
		{"INVALID_ARGUMENT", http.StatusBadRequest, "INVALID_ARGUMENT"},
		{"INVALID_STATE", http.StatusConflict, "INVALID_STATE"},
		{"PROJECTION_UNAVAILABLE", http.StatusBadGateway, "PROJECTION_UNAVAILABLE"},
		{"PROJECTION_TIMEOUT", http.StatusBadGateway, "PROJECTION_TIMEOUT"},
		{"RUNTIME_UNAVAILABLE", http.StatusBadGateway, "RUNTIME_UNAVAILABLE"},
		{"START_FAILED", http.StatusBadGateway, "START_FAILED"},
		{"BUILD_FAILED", http.StatusBadGateway, "BUILD_FAILED"},
		{"BUILD_START_FAILED", http.StatusBadGateway, "BUILD_START_FAILED"},
	}
	for _, testCase := range cases {
		t.Run(testCase.code, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			writeError(recorder, mapServiceError(&service.ToolError{Code: testCase.code}))
			if recorder.Code != testCase.status || errorCode(t, recorder.Body.Bytes()) != testCase.out {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestViewHelpers(t *testing.T) {
	if randText() == "" {
		t.Fatal("randText returned an empty string")
	}
	if err := internalError("boom"); err.Status != http.StatusInternalServerError || err.Code != "INTERNAL" {
		t.Fatalf("internalError=%+v", err)
	}
	if err := formatHTTPError(http.StatusTeapot, "TEAPOT", "tea %d", 1); err.Status != http.StatusTeapot || err.Message != "tea 1" {
		t.Fatalf("formatHTTPError=%+v", err)
	}
	if _, err := marshalYAML(map[string]any{"openapi": "3.1.0"}); err != nil {
		t.Fatalf("marshalYAML: %v", err)
	}
}

func TestAppItemsSortsAndReturnsAnEmptySlice(t *testing.T) {
	env := newPortalHTTPEnv(t)
	user := loadPortalTestUser(t, env, testPortalUsername)
	if items, err := env.api.appItems(context.Background(), user); err != nil || items == nil || len(items) != 0 {
		t.Fatalf("items=%v err=%v", items, err)
	}
	createPortalTestDevice(t, env, "dev_sort", user.ID, "AA:BB:CC:00:00:81", "agent_sort", store.PortalDeviceRegistered)
	owner := app.Identity{DeviceID: "AA:BB:CC:00:00:81", AgentID: "agent_sort"}
	now := time.Now().UTC()
	later := now
	earlier := now.Add(-time.Hour)
	env.api.apps = portalTestService(t,
		app.App{ID: "app_b", Owner: owner, State: app.StateIdle, BuildFinishedAt: later},
		app.App{ID: "app_a", Owner: owner, State: app.StateIdle, BuildFinishedAt: later},
		app.App{ID: "app_c", Owner: owner, State: app.StateIdle, BuildFinishedAt: earlier},
		app.App{ID: "app_d", Owner: owner, State: app.StateIdle},
	)
	items, err := env.api.appItems(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	order := make([]string, 0, len(items))
	for _, item := range items {
		order = append(order, item.AppID)
	}
	if strings.Join(order, ",") != "app_d,app_a,app_b,app_c" {
		t.Fatalf("order=%v", order)
	}
}

func TestOwnedIdentitiesSkipsDevicesWithoutAnAgentAndReportsErrors(t *testing.T) {
	env := newPortalHTTPEnv(t)
	user := loadPortalTestUser(t, env, testPortalUsername)
	createPortalTestDevice(t, env, "dev_none", user.ID, "AA:BB:CC:00:00:91", "", store.PortalDeviceProvisioning)
	identities, err := env.api.ownedIdentities(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(identities) != 0 {
		t.Fatalf("identities=%+v", identities)
	}
	if owners := ownerIdentities(identities); len(owners) != 0 {
		t.Fatalf("owners=%+v", owners)
	}
	if err := env.db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := env.api.ownedIdentities(context.Background(), user.ID); err == nil {
		t.Fatal("ownedIdentities ignored a storage failure")
	}
}
