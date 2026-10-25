package portal

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPortalServicesViewReportsSnapshotFailureAfterSaving(t *testing.T) {
	env := newPlatformHTTPEnv(t)
	env.login(t)
	execRaw(t, env.dbPath, "ALTER TABLE platform_settings RENAME TO platform_settings_hidden")
	response, payload := env.do(t, http.MethodPut, "/api/portal/services", `{"mode":"managed"}`,
		map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("save status=%d body=%s", response.StatusCode, payload)
	}
}

func TestPortalServicesViewTreatsAnEmptyModeAsManaged(t *testing.T) {
	env := newPlatformHTTPEnv(t)
	env.login(t)
	user := loadPortalTestUser(t, env, testPortalUsername)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	execRaw(t, env.dbPath, "ALTER TABLE portal_service_bindings RENAME TO portal_service_bindings_base")
	execRaw(t, env.dbPath, `INSERT INTO portal_service_bindings_base
		(user_id, mode, render_url, casting_url, render_token_enc, casting_token_enc, created_at, updated_at)
		VALUES ('`+user.ID+`', 'managed', '', '', '', '', '`+now+`', '`+now+`')`)
	execRaw(t, env.dbPath, `CREATE VIEW portal_service_bindings AS
		SELECT user_id, '' AS mode, render_url, casting_url, render_token_enc, casting_token_enc,
			created_at, updated_at
		FROM portal_service_bindings_base`)
	// The resolver cannot read the empty mode as a binding; the view itself is
	// what this test exercises.
	env.api.resolver = nil
	response, payload := env.do(t, http.MethodGet, "/api/portal/services", "", nil)
	if response.StatusCode != http.StatusOK || !strings.Contains(string(payload), `"mode":"managed"`) {
		t.Fatalf("view status=%d body=%s", response.StatusCode, payload)
	}
}

func TestPortalServiceTestsReportACastingTargetListFailure(t *testing.T) {
	env := newPlatformHTTPEnv(t)
	env.login(t)
	renderServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer renderServer.Close()
	castingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer castingServer.Close()

	body := `{"mode":"custom","render_url":"` + renderServer.URL + `","casting_url":"` + castingServer.URL + `"}`
	response, payload := env.do(t, http.MethodPost, "/api/portal/services/test", body,
		map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("probe status=%d body=%s", response.StatusCode, payload)
	}
	if !strings.Contains(string(payload), `"render":{"ok":true}`) {
		t.Fatalf("render probe: %s", payload)
	}
	if !strings.Contains(string(payload), "无法获取投屏服务的屏幕列表") {
		t.Fatalf("casting probe: %s", payload)
	}
}
