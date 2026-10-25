package portal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/endpoints"
	"github.com/open-anywhere/open-anywhere/internal/gateway/netguard"
	"github.com/open-anywhere/open-anywhere/internal/gateway/secret"
	"github.com/open-anywhere/open-anywhere/internal/gateway/settings"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

// newPlatformHTTPEnv wires the settings provider, resolver and client pool the
// service-binding endpoints need. The guard allows loopback so tests can point
// bindings at httptest servers.
func newPlatformHTTPEnv(t *testing.T) *portalHTTPEnv {
	t.Helper()
	env := newPortalHTTPEnv(t)
	box, err := secret.New(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatalf("secret.New: %v", err)
	}
	provider := settings.New(env.db, box)
	guard := netguard.New(true)
	env.api.SetPlatform(provider, endpoints.NewPool(guard, 5*time.Second, 5*time.Second),
		endpoints.NewResolver(env.db, provider, box, guard, nil))
	return env
}

func loadPortalTestUser(t *testing.T, env *portalHTTPEnv, username string) store.PortalUser {
	t.Helper()
	user, err := env.db.PortalUserByUsername(context.Background(), username)
	if err != nil {
		t.Fatalf("PortalUserByUsername(%s): %v", username, err)
	}
	return user
}

func TestPortalServicesReportsIncompleteCustomBindingAsNotReady(t *testing.T) {
	env := newPlatformHTTPEnv(t)
	env.login(t)
	user := loadPortalTestUser(t, env, testPortalUsername)
	now := time.Now().UTC()
	if err := env.db.UpsertServiceBinding(context.Background(), store.PortalServiceBinding{
		UserID:    user.ID,
		Mode:      store.ServiceModeCustom,
		RenderURL: "https://render.example.com",
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("UpsertServiceBinding: %v", err)
	}

	response, payload := env.do(t, http.MethodGet, "/api/portal/services", "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}
	var out struct {
		Mode  string `json:"mode"`
		Ready bool   `json:"ready"`
	}
	if err := json.Unmarshal(payload, &out); err != nil {
		t.Fatalf("body %s: %v", payload, err)
	}
	if out.Mode != store.ServiceModeCustom || out.Ready {
		t.Fatalf("incomplete binding reported ready: %s", payload)
	}
}

func TestPortalServicesReturns500ForAnUndecryptableToken(t *testing.T) {
	env := newPlatformHTTPEnv(t)
	env.login(t)
	user := loadPortalTestUser(t, env, testPortalUsername)
	now := time.Now().UTC()
	if err := env.db.UpsertServiceBinding(context.Background(), store.PortalServiceBinding{
		UserID:          user.ID,
		Mode:            store.ServiceModeCustom,
		RenderURL:       "https://render.example.com",
		CastingURL:      "https://casting.example.com",
		RenderTokenEnc:  "enc:v1:bm90LXJlYWw=",
		CastingTokenEnc: "enc:v1:bm90LXJlYWw=",
		CreatedAt:       now,
		UpdatedAt:       now,
	}); err != nil {
		t.Fatalf("UpsertServiceBinding: %v", err)
	}

	response, payload := env.do(t, http.MethodGet, "/api/portal/services", "", nil)
	if response.StatusCode != http.StatusInternalServerError || errorCode(t, payload) != "INTERNAL" {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}
	// An undecryptable token is a server-side problem, not an empty binding, so
	// it must not masquerade as ready=false.
	if bytes.Contains(payload, []byte(`"ready":false`)) || bytes.Contains(payload, []byte("decrypt")) {
		t.Fatalf("the response leaks binding state or internals: %s", payload)
	}
}

func TestPortalServiceTestRejectsAnInvalidMode(t *testing.T) {
	env := newPlatformHTTPEnv(t)
	env.login(t)
	response, payload := env.do(t, http.MethodPost, "/api/portal/services/test",
		`{"mode":"sideways"}`, map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusBadRequest || errorCode(t, payload) != "INVALID_ARGUMENT" {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}
}

func TestPortalServiceTestProbesFakeRenderAndCasting(t *testing.T) {
	env := newPlatformHTTPEnv(t)
	env.login(t)

	renderServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer renderServer.Close()
	castingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
		case "/v1/targets":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"targets":[{"id":"tv_1","name":"客厅","state":"online"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer castingServer.Close()

	body := fmt.Sprintf(`{"mode":"custom","render_url":%q,"casting_url":%q}`,
		renderServer.URL, castingServer.URL)
	response, payload := env.do(t, http.MethodPost, "/api/portal/services/test",
		body, map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}
	var out struct {
		Render  serviceCheck `json:"render"`
		Casting serviceCheck `json:"casting"`
		Screens int          `json:"screens"`
	}
	if err := json.Unmarshal(payload, &out); err != nil {
		t.Fatalf("body %s: %v", payload, err)
	}
	if !out.Render.OK || !out.Casting.OK || out.Screens != 1 {
		t.Fatalf("out=%+v body=%s", out, payload)
	}
}

func TestPortalDevicesListsOnlyTheCallersDevices(t *testing.T) {
	env := newPortalHTTPEnv(t)
	env.login(t)
	admin := loadPortalTestUser(t, env, testPortalUsername)
	own := createPortalTestDevice(t, env, "dev_own", admin.ID, "AA:BB:CC:00:00:41", "agent_own", store.PortalDeviceRegistered)
	other := createPortalTestUser(t, env, "usr_dave", "dave", "password123", store.PortalRoleUser)
	createPortalTestDevice(t, env, "dev_dave", other.ID, "AA:BB:CC:00:00:42", "agent_dave", store.PortalDeviceRegistered)

	response, payload := env.do(t, http.MethodGet, "/api/portal/devices", "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}
	var out struct {
		Devices []DeviceView `json:"devices"`
	}
	if err := json.Unmarshal(payload, &out); err != nil {
		t.Fatalf("body %s: %v", payload, err)
	}
	if len(out.Devices) != 1 || out.Devices[0].ID != own.ID || out.Devices[0].AgentID != own.LinxAgentID {
		t.Fatalf("devices=%+v body=%s", out.Devices, payload)
	}
	if strings.Contains(string(payload), "dev_dave") || strings.Contains(string(payload), "agent_dave") {
		t.Fatalf("another user's device leaked: %s", payload)
	}
}
