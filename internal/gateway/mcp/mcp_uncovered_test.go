package mcp

import (
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/service"
)

func TestUpdateAppToolReturnsTheRevision(t *testing.T) {
	fake := &fakeApps{create: service.CreateResult{AppID: "app_1", State: app.StateBuilding}}
	session, _, ctx := connectLogged(t, fake)

	result, err := session.CallTool(ctx, &sdk.CallToolParams{
		Name:      "update-app",
		Arguments: map[string]any{"app_id": "app_1", "prompt": "改成春节主题"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("update-app failed: %+v", result)
	}
	fake.mu.Lock()
	prompt := fake.prompt
	fake.mu.Unlock()
	if prompt != "改成春节主题" {
		t.Fatalf("prompt = %q", prompt)
	}
	if owner := fake.currentIdentity(); owner.DeviceID != "device" || owner.SessionID != "session" {
		t.Fatalf("identity = %+v", owner)
	}
}
