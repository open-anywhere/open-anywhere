package portal

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestAdminSeesPlatformMCPServicesWithoutTheirSecrets(t *testing.T) {
	env, _ := newAgentEnv(t)
	if err := env.db.SetPortalSetting(context.Background(), settingMCPInstance, "mcp_ours"); err != nil {
		t.Fatal(err)
	}
	response, payload := env.do(t, http.MethodGet, "/api/admin/mcp", "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}
	body := string(payload)
	if strings.Contains(body, "secret-token") || strings.Contains(body, "Authorization") {
		t.Fatalf("connection details leaked: %s", body)
	}
	if !strings.Contains(body, `"id":"mcp_ours"`) || !strings.Contains(body, `"required":true`) ||
		!strings.Contains(body, `"provisioned":true`) {
		t.Fatalf("body = %s", body)
	}
	// The gateway's own service is listed first.
	if strings.Index(body, "mcp_ours") > strings.Index(body, "mcp_weather") {
		t.Fatalf("required service is not first: %s", body)
	}
}

func TestAdminChoosesWhichMCPServicesUsersMayAdd(t *testing.T) {
	env, _ := newAgentEnv(t)
	origin := map[string]string{"Origin": env.server.URL}
	if err := env.db.SetPortalSetting(context.Background(), settingMCPInstance, "mcp_ours"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, body string }{
		{"unknown service", `{"ids":["mcp_nope"]}`},
		{"the gateway's own service", `{"ids":["mcp_ours"]}`},
	} {
		response, payload := env.do(t, http.MethodPut, "/api/admin/mcp/shared", tc.body, origin)
		if response.StatusCode != http.StatusBadRequest || errorCode(t, payload) != "INVALID_ARGUMENT" {
			t.Fatalf("%s: status=%d body=%s", tc.name, response.StatusCode, payload)
		}
	}
	response, payload := env.do(t, http.MethodPut, "/api/admin/mcp/shared", `{"ids":["mcp_weather","mcp_weather"]}`, origin)
	if response.StatusCode != http.StatusOK || strings.Count(string(payload), "mcp_weather") != 1 {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}
	response, payload = env.do(t, http.MethodGet, "/api/admin/mcp", "", nil)
	if !strings.Contains(string(payload), `"id":"mcp_weather","name":"天气","desc":"查天气","private":false,"required":false,"shared":true`) {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}
}

func TestUserAddsOnlyOfferedMCPServicesAndKeepsTheRequiredOne(t *testing.T) {
	env, platform := newAgentEnv(t)
	origin := map[string]string{"Origin": env.server.URL}
	ctx := context.Background()
	if err := env.db.SetPortalSetting(ctx, settingMCPInstance, "mcp_ours"); err != nil {
		t.Fatal(err)
	}
	platform.mu.Lock()
	platform.mcpBound = []string{"mcp_ours"}
	platform.mu.Unlock()
	if response, payload := env.do(t, http.MethodPut, "/api/admin/mcp/shared", `{"ids":["mcp_weather"]}`, origin); response.StatusCode != http.StatusOK {
		t.Fatalf("share status=%d body=%s", response.StatusCode, payload)
	}

	// The user sees the required service and only the offered one.
	response, payload := env.do(t, http.MethodGet, "/api/portal/devices/dev_base/mcps", "", nil)
	body := string(payload)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, `"required":{"id":"mcp_ours"`) ||
		!strings.Contains(body, "mcp_weather") || strings.Contains(body, "mcp_music") {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}

	// A service that is not offered cannot be added.
	response, payload = env.do(t, http.MethodPut, "/api/portal/devices/dev_base/mcps", `{"ids":["mcp_music"]}`, origin)
	if response.StatusCode != http.StatusBadRequest || errorCode(t, payload) != "INVALID_ARGUMENT" {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}

	// Choosing nothing still leaves the gateway's own service in place.
	for _, tc := range []struct {
		body string
		want []string
	}{
		{`{"ids":["mcp_weather"]}`, []string{"mcp_ours", "mcp_weather"}},
		{`{"ids":[]}`, []string{"mcp_ours"}},
	} {
		response, payload = env.do(t, http.MethodPut, "/api/portal/devices/dev_base/mcps", tc.body, origin)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("status=%d body=%s", response.StatusCode, payload)
		}
		platform.mu.Lock()
		got := platform.mcpPuts[len(platform.mcpPuts)-1]
		platform.mu.Unlock()
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Fatalf("body %s bound %v, want %v", tc.body, got, tc.want)
		}
	}
}

func TestUserCannotChangeMCPsBeforeTheGatewayServiceExists(t *testing.T) {
	env, platform := newAgentEnv(t)
	origin := map[string]string{"Origin": env.server.URL}
	// Nothing records the gateway's own service yet, so a replacement would drop it.
	response, payload := env.do(t, http.MethodPut, "/api/portal/devices/dev_base/mcps", `{"ids":[]}`, origin)
	if response.StatusCode != http.StatusConflict || errorCode(t, payload) != "MCP_NOT_READY" {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}
	platform.mu.Lock()
	defer platform.mu.Unlock()
	if len(platform.mcpPuts) != 0 {
		t.Fatalf("platform was called: %v", platform.mcpPuts)
	}
}
