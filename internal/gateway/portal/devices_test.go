package portal

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/linx"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

type fakeBindPlatform struct {
	mu       sync.Mutex
	binds    []string
	unbinds  []string
	unbindOK bool
}

func (f *fakeBindPlatform) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/xiaozhi/device/bind/"):
			parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/xiaozhi/device/bind/"), "/")
			f.binds = append(f.binds, parts[0]+"/"+parts[1])
			switch parts[1] {
			case "BAD":
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"code":400,"msg":"invalid code"}`))
			case "TAKEN":
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"code":409,"msg":"bound"}`))
			default:
				_, _ = w.Write([]byte(`{"code":0,"msg":"success","data":{"macAddress":"aa:bb:cc:dd:ee:01"}}`))
			}
		case r.URL.Path == "/xiaozhi/device/unbind":
			var body struct {
				DeviceID string `json:"deviceId"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.unbinds = append(f.unbinds, body.DeviceID)
			if !f.unbindOK {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"code":500,"msg":"boom"}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":0,"message":"ok"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func (f *fakeBindPlatform) setUnbindOK(value bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unbindOK = value
}

func newMultiDeviceEnv(t *testing.T) (*portalHTTPEnv, *fakeBindPlatform, store.PortalUser) {
	t.Helper()
	env := newPortalHTTPEnv(t)
	platform := &fakeBindPlatform{unbindOK: true}
	server := httptest.NewServer(platform.handler())
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	client := linx.New(linx.Config{BaseURL: server.URL, APIKey: "key"})
	env.api.provision = NewProvisioner(ctx, env.db, client, env.api.voice, ProvisionConfig{DataDir: env.dataDir})
	env.login(t)
	user, err := env.db.PortalUserByUsername(context.Background(), testPortalUsername)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	base := store.PortalDevice{
		ID: "dev_base", UserID: user.ID, Kind: store.PortalDeviceVirtual, MAC: "02:00:00:00:00:01",
		ClientID: "client-1", LinxAgentID: "agent_1", AgentName: "Open Anywhere · admin",
		State: store.PortalDeviceRegistered, Step: stepDone, CreatedAt: now, UpdatedAt: now, RegisteredAt: &now,
	}
	if err := env.db.CreatePortalDevice(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	return env, platform, user
}

func TestBindPhysicalHardwareToTheUsersAgent(t *testing.T) {
	env, platform, _ := newMultiDeviceEnv(t)
	origin := map[string]string{"Origin": env.server.URL}

	response, payload := env.do(t, http.MethodPost, "/api/portal/devices",
		`{"kind":"physical","activation_code":"ABC123","name":"客厅音箱"}`, origin)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}
	var created struct {
		Device DeviceView `json:"device"`
	}
	if err := json.Unmarshal(payload, &created); err != nil {
		t.Fatal(err)
	}
	device := created.Device
	if device.Kind != store.PortalDevicePhysical || device.MAC != "aa:bb:cc:dd:ee:01" || device.AgentID != "agent_1" ||
		device.State != store.PortalDeviceRegistered || device.Name != "客厅音箱" {
		t.Fatalf("device = %+v", device)
	}
	platform.mu.Lock()
	binds := append([]string(nil), platform.binds...)
	platform.mu.Unlock()
	if len(binds) != 1 || binds[0] != "agent_1/ABC123" {
		t.Fatalf("binds = %v", binds)
	}
	// The hardware owns its own connection: no browser voice runtime exists.
	if device.Voice.State != "offline" {
		t.Fatalf("voice = %+v", device.Voice)
	}
	// MCP calls from the hardware resolve to this user through (MAC, agent).
	stored, err := env.db.PortalDeviceByIdentity(context.Background(), "aa:bb:cc:dd:ee:01", "agent_1")
	if err != nil || stored.ID != device.ID {
		t.Fatalf("identity lookup = %+v err=%v", stored, err)
	}

	// The same hardware cannot be added twice.
	response, payload = env.do(t, http.MethodPost, "/api/portal/devices", `{"kind":"physical","activation_code":"ABC123"}`, origin)
	if response.StatusCode != http.StatusConflict || errorCode(t, payload) != "DEVICE_EXISTS" {
		t.Fatalf("duplicate status=%d body=%s", response.StatusCode, payload)
	}
}

func TestBindPhysicalHardwareRejectsBadInput(t *testing.T) {
	env, _, user := newMultiDeviceEnv(t)
	origin := map[string]string{"Origin": env.server.URL}
	cases := []struct {
		name, body, code string
		status           int
	}{
		{"missing code", `{"kind":"physical"}`, "INVALID_ARGUMENT", http.StatusBadRequest},
		{"odd characters", `{"kind":"physical","activation_code":"12 34;"}`, "INVALID_ARGUMENT", http.StatusBadRequest},
		{"refused by platform", `{"kind":"physical","activation_code":"BAD"}`, "ACTIVATION_CODE_INVALID", http.StatusBadRequest},
		{"already bound", `{"kind":"physical","activation_code":"TAKEN"}`, "DEVICE_ALREADY_BOUND", http.StatusConflict},
		{"unknown kind", `{"kind":"toaster"}`, "INVALID_ARGUMENT", http.StatusBadRequest},
		{"unknown agent device", `{"kind":"physical","activation_code":"ABC123","agent_device_id":"dev_nope"}`, "NOT_FOUND", http.StatusNotFound},
		{"long name", `{"kind":"physical","activation_code":"ABC123","name":"` + strings.Repeat("名", 41) + `"}`, "INVALID_ARGUMENT", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response, payload := env.do(t, http.MethodPost, "/api/portal/devices", tc.body, origin)
			if response.StatusCode != tc.status || errorCode(t, payload) != tc.code {
				t.Fatalf("status=%d body=%s", response.StatusCode, payload)
			}
		})
	}
	devices, _ := env.db.PortalDevicesByUser(context.Background(), user.ID)
	if len(devices) != 1 {
		t.Fatalf("a rejected request created %d devices", len(devices))
	}
}

func TestAccountCanOwnSeveralVirtualDevices(t *testing.T) {
	env, _, user := newMultiDeviceEnv(t)
	origin := map[string]string{"Origin": env.server.URL}
	for _, name := range []string{"客厅", "卧室"} {
		response, payload := env.do(t, http.MethodPost, "/api/portal/devices", `{"name":"`+name+`"}`, origin)
		if response.StatusCode != http.StatusAccepted {
			t.Fatalf("create %s: status=%d body=%s", name, response.StatusCode, payload)
		}
	}
	devices, err := env.db.PortalDevicesByUser(context.Background(), user.ID)
	if err != nil || len(devices) != 3 {
		t.Fatalf("devices = %d err=%v", len(devices), err)
	}
	names := map[string]bool{}
	for _, device := range devices {
		names[device.Name] = true
	}
	if !names["客厅"] || !names["卧室"] {
		t.Fatalf("names = %v", names)
	}
}

func TestDeviceLimit(t *testing.T) {
	env, _, user := newMultiDeviceEnv(t)
	now := time.Now().UTC()
	for index := 0; index < maxDevicesPerUser-1; index++ {
		device := store.PortalDevice{
			ID: fmt.Sprintf("dev_fill%d", index), UserID: user.ID, Kind: store.PortalDevicePhysical,
			MAC: fmt.Sprintf("aa:00:00:00:00:%02d", index), LinxAgentID: "agent_1",
			State: store.PortalDeviceRegistered, CreatedAt: now, UpdatedAt: now,
		}
		if err := env.db.CreatePortalDevice(context.Background(), device); err != nil {
			t.Fatal(err)
		}
	}
	response, payload := env.do(t, http.MethodPost, "/api/portal/devices", `{}`, map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusConflict || errorCode(t, payload) != "DEVICE_LIMIT" {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}
}

func TestDeleteDevice(t *testing.T) {
	env, platform, _ := newMultiDeviceEnv(t)
	origin := map[string]string{"Origin": env.server.URL}
	_, payload := env.do(t, http.MethodPost, "/api/portal/devices", `{"kind":"physical","activation_code":"ABC123"}`, origin)
	var created struct {
		Device DeviceView `json:"device"`
	}
	if err := json.Unmarshal(payload, &created); err != nil {
		t.Fatal(err)
	}
	hardware := created.Device.ID

	// An agent cannot go while hardware still hangs off it.
	response, payload := env.do(t, http.MethodDelete, "/api/portal/devices/dev_base", "{}", origin)
	if response.StatusCode != http.StatusConflict || errorCode(t, payload) != "DEVICE_HAS_HARDWARE" {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}

	// A failed unbind keeps the record unless the caller forces the removal.
	platform.setUnbindOK(false)
	response, payload = env.do(t, http.MethodDelete, "/api/portal/devices/"+hardware, "{}", origin)
	if response.StatusCode != http.StatusBadGateway || errorCode(t, payload) != "UNBIND_FAILED" {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}
	if _, err := env.db.PortalDeviceByID(context.Background(), hardware); err != nil {
		t.Fatalf("device removed despite a failed unbind: %v", err)
	}
	platform.setUnbindOK(true)
	response, payload = env.do(t, http.MethodDelete, "/api/portal/devices/"+hardware, "{}", origin)
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}
	platform.mu.Lock()
	last := platform.unbinds[len(platform.unbinds)-1]
	platform.mu.Unlock()
	if last != "aa:bb:cc:dd:ee:01" {
		t.Fatalf("unbound %q", last)
	}
	if _, err := env.db.PortalDeviceByID(context.Background(), hardware); err == nil {
		t.Fatal("device still present")
	}

	// The last remaining device stays.
	response, payload = env.do(t, http.MethodDelete, "/api/portal/devices/dev_base", "{}", origin)
	if response.StatusCode != http.StatusConflict || errorCode(t, payload) != "LAST_DEVICE" {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}
}

func TestForceDeleteIgnoresAFailedUnbind(t *testing.T) {
	env, platform, _ := newMultiDeviceEnv(t)
	origin := map[string]string{"Origin": env.server.URL}
	_, payload := env.do(t, http.MethodPost, "/api/portal/devices", `{"kind":"physical","activation_code":"ABC123"}`, origin)
	var created struct {
		Device DeviceView `json:"device"`
	}
	if err := json.Unmarshal(payload, &created); err != nil {
		t.Fatal(err)
	}
	platform.setUnbindOK(false)
	response, payload := env.do(t, http.MethodDelete, "/api/portal/devices/"+created.Device.ID+"?force=true", "{}", origin)
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}
}

func TestDeleteDeviceBelongsToTheOwner(t *testing.T) {
	env, _, _ := newMultiDeviceEnv(t)
	origin := map[string]string{"Origin": env.server.URL}
	response, payload := env.do(t, http.MethodDelete, "/api/portal/devices/dev_nobody", "{}", origin)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}
}
