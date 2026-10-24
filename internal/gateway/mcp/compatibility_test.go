package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/service"
)

type extendedFake struct{ fakeApps }

func (f *extendedFake) GetOpenAPI(context.Context, app.Identity, string) (map[string]any, error) {
	return map[string]any{"openapi": "3.1.0", "paths": map[string]any{}}, nil
}
func (f *extendedFake) ListApps(context.Context, app.Identity) ([]service.StatusView, error) {
	return []service.StatusView{{AppID: "app", State: app.StateIdle}}, nil
}
func (f *extendedFake) ClaimApp(context.Context, app.Identity, string) (service.StatusView, error) {
	return service.StatusView{AppID: "app", State: app.StateIdle}, nil
}

type contextHeaders struct{ base http.RoundTripper }

func (t contextHeaders) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("X-Linx-Device-Id", "device")
	r.Header.Set("X-Linx-Agent-Id", "agent")
	r.Header.Set("X-Linx-Session-Id", "session")
	return t.base.RoundTrip(r)
}
func TestStreamableCompatibilityAndClaim(t *testing.T) {
	server := New(&extendedFake{}, time.Second)
	defer server.Close()
	httpServer := httptest.NewServer(server.StreamableHTTP("", true, true))
	defer httpServer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := sdk.NewClient(&sdk.Implementation{Name: "compat", Version: "1"}, nil)
	session, err := client.Connect(ctx, &sdk.StreamableClientTransport{Endpoint: httpServer.URL, DisableStandaloneSSE: true, HTTPClient: &http.Client{Transport: contextHeaders{http.DefaultTransport}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range tools.Tools {
		names[tool.Name] = true
	}
	for _, n := range []string{"create-app", "get-app-operations", "get_openapi", "send-request", "send_request", "claim-app", "list-apps", "probe_speaker"} {
		if !names[n] {
			t.Fatal(n)
		}
	}
	for _, call := range []sdk.CallToolParams{{Name: "get_openapi", Arguments: map[string]any{"app_id": "app"}}, {Name: "send_request", Arguments: map[string]any{"app_id": "app", "operationId": "read"}}, {Name: "claim-app", Arguments: map[string]any{"app_id": "app"}}, {Name: "probe_speaker", Arguments: map[string]any{"speaker_id": nil, "speaker_name": nil, "context_source": "unavailable"}}} {
		r, e := session.CallTool(ctx, &call)
		if e != nil || r.IsError {
			t.Fatal(r, e)
		}
	}
}
