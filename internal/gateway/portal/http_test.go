package portal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/service"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

const (
	testPortalUsername = "alice"
	testPortalPassword = "password123"
)

type portalHTTPEnv struct {
	server  *httptest.Server
	client  *http.Client
	db      *store.DB
	api     *API
	dataDir string
	dbPath  string
}

func newPortalHTTPEnv(t *testing.T) *portalHTTPEnv {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "portal.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	auth := NewAuth(db, AuthConfig{SessionTTL: time.Hour})
	if _, err := auth.EnsureAdmin(context.Background(), Credentials{Username: testPortalUsername, Password: testPortalPassword}); err != nil {
		t.Fatalf("EnsureAdmin: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	voice := NewVoiceManager(ctx)
	t.Cleanup(voice.StopAll)
	dataDir := t.TempDir()
	provision := NewProvisioner(ctx, db, nil, voice, ProvisionConfig{DataDir: dataDir})
	api := NewAPI(db, auth, nil, voice, provision, filepath.Join(t.TempDir(), "web"))
	mux := http.NewServeMux()
	mux.Handle("/", api.Handler())
	mux.Handle("/api/admin/", api.AdminHandler())
	mux.Handle("/voice/", api.VoiceHandler())
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &portalHTTPEnv{server: server, client: &http.Client{Jar: jar}, db: db, api: api, dataDir: dataDir, dbPath: dbPath}
}

func (e *portalHTTPEnv) do(t *testing.T, method, path string, body string, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	request, err := http.NewRequest(method, e.server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := e.client.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response, payload
}

func (e *portalHTTPEnv) login(t *testing.T) {
	t.Helper()
	response, payload := e.do(t, http.MethodPost, "/api/portal/login",
		`{"username":"`+testPortalUsername+`","password":"`+testPortalPassword+`"}`,
		map[string]string{"Origin": e.server.URL})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("login status=%d body=%s", response.StatusCode, payload)
	}
}

func errorCode(t *testing.T, payload []byte) string {
	t.Helper()
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("error body %q: %v", payload, err)
	}
	return envelope.Error.Code
}

func TestHTTPLoginMeAndLogout(t *testing.T) {
	env := newPortalHTTPEnv(t)
	origin := map[string]string{"Origin": env.server.URL}

	// New deployments keep registration closed until an administrator enables
	// it in platform settings.
	response, payload := env.do(t, http.MethodPost, "/api/portal/register",
		`{"username":"Alice","password":"password123"}`, origin)
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("register status=%d body=%s", response.StatusCode, payload)
	}
	if code := errorCode(t, payload); code != "REGISTRATION_CLOSED" {
		t.Fatalf("register code=%s body=%s", code, payload)
	}

	response, payload = env.do(t, http.MethodPost, "/api/portal/login",
		`{"username":"Alice","password":"password123"}`, origin)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("login status=%d body=%s", response.StatusCode, payload)
	}
	if len(response.Cookies()) != 1 || response.Cookies()[0].Name != SessionCookie {
		t.Fatalf("cookies=%v", response.Cookies())
	}

	response, payload = env.do(t, http.MethodGet, "/api/portal/me", "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("me status=%d body=%s", response.StatusCode, payload)
	}
	var me struct {
		User struct {
			Username string `json:"username"`
		} `json:"user"`
		Devices []DeviceView `json:"devices"`
	}
	if err := json.Unmarshal(payload, &me); err != nil {
		t.Fatal(err)
	}
	if me.User.Username != "alice" || len(me.Devices) != 0 {
		t.Fatalf("me=%s", payload)
	}

	response, payload = env.do(t, http.MethodPost, "/api/portal/logout", `{}`, origin)
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status=%d body=%s", response.StatusCode, payload)
	}
	response, payload = env.do(t, http.MethodGet, "/api/portal/me", "", nil)
	if response.StatusCode != http.StatusUnauthorized || errorCode(t, payload) != "UNAUTHORIZED" {
		t.Fatalf("me after logout status=%d body=%s", response.StatusCode, payload)
	}
}

func TestHTTPRejectsCrossSiteAndNonJSONWrites(t *testing.T) {
	env := newPortalHTTPEnv(t)
	response, payload := env.do(t, http.MethodPost, "/api/portal/login",
		`{"username":"alice","password":"password123"}`, map[string]string{"Origin": "https://evil.example"})
	if response.StatusCode != http.StatusForbidden || errorCode(t, payload) != "FORBIDDEN" {
		t.Fatalf("cross-site status=%d body=%s", response.StatusCode, payload)
	}

	request, err := http.NewRequest(http.MethodPost, env.server.URL+"/api/portal/login", bytes.NewBufferString("username=alice"))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err = env.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	payload, _ = io.ReadAll(response.Body)
	if response.StatusCode != http.StatusUnsupportedMediaType || errorCode(t, payload) != "INVALID_CONTENT_TYPE" {
		t.Fatalf("form status=%d body=%s", response.StatusCode, payload)
	}

	response, payload = env.do(t, http.MethodPost, "/api/portal/login", `{"username":"alice","password":"wrong-password"}`, nil)
	if response.StatusCode != http.StatusUnauthorized || errorCode(t, payload) != "INVALID_CREDENTIALS" {
		t.Fatalf("wrong password status=%d body=%s", response.StatusCode, payload)
	}
}

func TestHTTPRequiresASessionForEveryOwnedResource(t *testing.T) {
	env := newPortalHTTPEnv(t)
	for _, path := range []string{
		"/api/portal/me",
		"/api/portal/devices/dev_missing",
		"/api/portal/apps",
		"/api/portal/apps/app_missing",
	} {
		response, payload := env.do(t, http.MethodGet, path, "", nil)
		if response.StatusCode != http.StatusUnauthorized || errorCode(t, payload) != "UNAUTHORIZED" {
			t.Errorf("%s status=%d body=%s", path, response.StatusCode, payload)
		}
	}
	response, payload := env.do(t, http.MethodGet, "/voice/dev_missing/", "", nil)
	if response.StatusCode != http.StatusUnauthorized || errorCode(t, payload) != "UNAUTHORIZED" {
		t.Fatalf("voice status=%d body=%s", response.StatusCode, payload)
	}
}

func TestHTTPDoesNotRevealAnotherUsersDevice(t *testing.T) {
	env := newPortalHTTPEnv(t)
	env.login(t)
	response, payload := env.do(t, http.MethodGet, "/api/portal/devices/dev_missing", "", nil)
	if response.StatusCode != http.StatusNotFound || errorCode(t, payload) != "NOT_FOUND" {
		t.Fatalf("unknown device status=%d body=%s", response.StatusCode, payload)
	}
	response, payload = env.do(t, http.MethodGet, "/voice/dev_missing/", "", nil)
	if response.StatusCode != http.StatusNotFound || errorCode(t, payload) != "NOT_FOUND" {
		t.Fatalf("unknown voice device status=%d body=%s", response.StatusCode, payload)
	}
}

func TestHTTPRetryRestartsARegisteredDevicesVoiceRuntime(t *testing.T) {
	env := newPortalHTTPEnv(t)
	origin := map[string]string{"Origin": env.server.URL}
	env.login(t)
	user, err := env.db.PortalUserByUsername(context.Background(), testPortalUsername)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	device := store.PortalDevice{
		ID:        "dev_voice",
		UserID:    user.ID,
		Kind:      store.PortalDeviceVirtual,
		MAC:       "AA:BB:CC:DD:EE:01",
		ClientID:  "client-voice",
		State:     store.PortalDeviceRegistered,
		Step:      stepDone,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := env.db.CreatePortalDevice(context.Background(), device); err != nil {
		t.Fatal(err)
	}

	// Without a credential file the runtime cannot start; the API says so
	// instead of pretending the retry succeeded.
	response, payload := env.do(t, http.MethodPost, "/api/portal/devices/dev_voice/retry", `{}`, origin)
	if response.StatusCode != http.StatusServiceUnavailable || errorCode(t, payload) != "VOICE_UNAVAILABLE" {
		t.Fatalf("missing credential status=%d body=%s", response.StatusCode, payload)
	}

	credential := filepath.Join(env.dataDir, "devices", "dev_voice.json")
	if err := os.MkdirAll(filepath.Dir(credential), 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{"mac":"AA:BB:CC:DD:EE:01","clientId":"client-voice","token":"secret","wsUrl":"ws://127.0.0.1:1/"}`
	if err := os.WriteFile(credential, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	response, payload = env.do(t, http.MethodPost, "/api/portal/devices/dev_voice/retry", `{}`, origin)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("retry status=%d body=%s", response.StatusCode, payload)
	}
	var out struct {
		Device DeviceView `json:"device"`
	}
	if err := json.Unmarshal(payload, &out); err != nil {
		t.Fatal(err)
	}
	if out.Device.Voice.State == "offline" {
		t.Fatalf("voice runtime was not started: %s", payload)
	}
}

func TestHTTPDisconnectAndReconnectControlTheVoiceRuntime(t *testing.T) {
	env := newPortalHTTPEnv(t)
	origin := map[string]string{"Origin": env.server.URL}
	env.login(t)
	user, err := env.db.PortalUserByUsername(context.Background(), testPortalUsername)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	registered := store.PortalDevice{
		ID: "dev_voice", UserID: user.ID, Kind: store.PortalDeviceVirtual,
		MAC: "AA:BB:CC:DD:EE:01", ClientID: "client-voice",
		State: store.PortalDeviceRegistered, Step: stepDone, CreatedAt: now, UpdatedAt: now,
	}
	if err := env.db.CreatePortalDevice(context.Background(), registered); err != nil {
		t.Fatal(err)
	}
	credential := filepath.Join(env.dataDir, "devices", "dev_voice.json")
	if err := os.MkdirAll(filepath.Dir(credential), 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{"mac":"AA:BB:CC:DD:EE:01","clientId":"client-voice","token":"secret","wsUrl":"ws://127.0.0.1:1/"}`
	if err := os.WriteFile(credential, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	voiceState := func(path string) string {
		t.Helper()
		response, payload := env.do(t, http.MethodPost, path, `{}`, origin)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, response.StatusCode, payload)
		}
		var out struct {
			Device DeviceView `json:"device"`
		}
		if err := json.Unmarshal(payload, &out); err != nil {
			t.Fatal(err)
		}
		return out.Device.Voice.State
	}

	if state := voiceState("/api/portal/devices/dev_voice/reconnect"); state == "offline" {
		t.Fatal("reconnect did not start the voice runtime")
	}
	if state := voiceState("/api/portal/devices/dev_voice/disconnect"); state != "offline" {
		t.Fatalf("voice state after disconnect = %q", state)
	}
	// Disconnecting twice is not an error; the device is simply offline.
	if state := voiceState("/api/portal/devices/dev_voice/disconnect"); state != "offline" {
		t.Fatalf("voice state after second disconnect = %q", state)
	}
	if state := voiceState("/api/portal/devices/dev_voice/reconnect"); state == "offline" {
		t.Fatal("reconnect did not bring the device back")
	}
	// Reconnecting a healthy device replaces its connection.
	if state := voiceState("/api/portal/devices/dev_voice/reconnect"); state == "offline" {
		t.Fatal("reconnecting a connected device left it offline")
	}

	response, payload := env.do(t, http.MethodPost, "/api/portal/devices/dev_missing/disconnect", `{}`, origin)
	if response.StatusCode != http.StatusNotFound || errorCode(t, payload) != "NOT_FOUND" {
		t.Fatalf("unknown device status=%d body=%s", response.StatusCode, payload)
	}

	// A user has one virtual device, so the same device is moved back to
	// provisioning to cover the unregistered case.
	registered.State, registered.Step = store.PortalDeviceProvisioning, "create_agent"
	if err := env.db.UpdatePortalDevice(context.Background(), registered); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"disconnect", "reconnect"} {
		response, payload = env.do(t, http.MethodPost, "/api/portal/devices/dev_voice/"+action, `{}`, origin)
		if response.StatusCode != http.StatusConflict || errorCode(t, payload) != "DEVICE_NOT_READY" {
			t.Fatalf("%s on an unregistered device status=%d body=%s", action, response.StatusCode, payload)
		}
	}
}

func TestHTTPDisconnectAndReconnectRequireLogin(t *testing.T) {
	env := newPortalHTTPEnv(t)
	origin := map[string]string{"Origin": env.server.URL}
	for _, action := range []string{"disconnect", "reconnect"} {
		response, payload := env.do(t, http.MethodPost, "/api/portal/devices/dev_voice/"+action, `{}`, origin)
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s without a session status=%d body=%s", action, response.StatusCode, payload)
		}
	}
}

func TestVoiceHandlerOnlyExposesCallControlEndpoints(t *testing.T) {
	env := newPortalHTTPEnv(t)
	env.login(t)
	user, err := env.db.PortalUserByUsername(context.Background(), testPortalUsername)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	device := store.PortalDevice{
		ID:        "dev_voice",
		UserID:    user.ID,
		Kind:      store.PortalDeviceVirtual,
		MAC:       "AA:BB:CC:DD:EE:01",
		ClientID:  "client-voice",
		State:     store.PortalDeviceRegistered,
		Step:      stepDone,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := env.db.CreatePortalDevice(context.Background(), device); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		"/voice/dev_voice/",
		"/voice/dev_voice/device/register",
		"/voice/dev_voice/disconnect",
		"/voice/dev_voice/reconnect",
	} {
		response, payload := env.do(t, http.MethodGet, path, "", nil)
		if response.StatusCode != http.StatusNotFound {
			t.Errorf("%s status=%d body=%s", path, response.StatusCode, payload)
		}
	}

	response, payload := env.do(t, http.MethodGet, "/voice/dev_voice/status", "", nil)
	if response.StatusCode != http.StatusServiceUnavailable || errorCode(t, payload) != "VOICE_UNAVAILABLE" {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}
}

func TestMapServiceErrorMapsKnownAndUnknownFailures(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{
			name:   "cleanup pending",
			err:    &service.ToolError{Code: "CLEANUP_PENDING", Message: "internal cleanup detail"},
			status: http.StatusConflict,
			code:   "CLEANUP_PENDING",
		},
		{
			name:   "app not found",
			err:    &service.ToolError{Code: "APP_NOT_FOUND"},
			status: http.StatusNotFound,
			code:   "APP_NOT_FOUND",
		},
		{
			name:   "unknown tool error",
			err:    &service.ToolError{Code: "INTERNAL_STACK_TRACE", Message: "database password=secret"},
			status: http.StatusInternalServerError,
			code:   "INTERNAL",
		},
		{
			name:   "plain error",
			err:    errors.New("boom: internal"),
			status: http.StatusInternalServerError,
			code:   "INTERNAL",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			writeError(recorder, mapServiceError(testCase.err))
			if recorder.Code != testCase.status {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, testCase.status, recorder.Body.String())
			}
			if code := errorCode(t, recorder.Body.Bytes()); code != testCase.code {
				t.Fatalf("code = %q, want %q; body=%s", code, testCase.code, recorder.Body.String())
			}
			if testCase.code == "INTERNAL" {
				body := recorder.Body.String()
				for _, leaked := range []string{
					"INTERNAL_STACK_TRACE", "database password=secret", "boom: internal", "internal cleanup detail",
				} {
					if strings.Contains(body, leaked) {
						t.Errorf("the response leaked %q: %s", leaked, body)
					}
				}
			}
		})
	}
}

func TestDeviceViewMapsPlatformNotConfigured(t *testing.T) {
	env := newPortalHTTPEnv(t)
	now := time.Now().UTC()
	view := env.api.deviceView(store.PortalDevice{
		ID:        "dev_platform",
		State:     store.PortalDeviceError,
		Error:     deviceErrorCodePlatformNotConfigured,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if view.ErrorCode != deviceErrorCodePlatformNotConfigured {
		t.Fatalf("error_code = %q", view.ErrorCode)
	}
	if view.Error != "平台尚未配置，请联系维护者" {
		t.Fatalf("error = %q", view.Error)
	}

	other := env.api.deviceView(store.PortalDevice{
		ID:    "dev_other",
		State: store.PortalDeviceError,
		Error: "LINX_CREATE_FAILED",
	})
	if other.ErrorCode != "" || other.Error != "LINX_CREATE_FAILED" {
		t.Fatalf("an unrelated error was rewritten: %+v", other)
	}
}

func TestStaticServesTheShellAndExplainsAMissingBuild(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html><div id=root>shell</div>"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler := NewSPA(dir)
	for _, path := range []string{"/portal/", "/portal/console", "/portal/console/apps/app_1"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "shell") {
			t.Errorf("%s status=%d body=%q", path, recorder.Code, recorder.Body.String())
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/portal", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusMovedPermanently || recorder.Header().Get("Location") != "/portal/" {
		t.Fatalf("redirect status=%d location=%q", recorder.Code, recorder.Header().Get("Location"))
	}

	missing := NewSPA(filepath.Join(t.TempDir(), "nope"))
	request = httptest.NewRequest(http.MethodGet, "/portal/", nil)
	recorder = httptest.NewRecorder()
	missing.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), "pnpm build") {
		t.Fatalf("missing build status=%d body=%q", recorder.Code, recorder.Body.String())
	}
}
