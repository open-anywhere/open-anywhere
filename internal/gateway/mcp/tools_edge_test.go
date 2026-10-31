package mcp

import (
	"errors"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestEveryToolPropagatesServiceErrors(t *testing.T) {
	fake := &fakeApps{err: errors.New("service down")}
	session, _, ctx := connectLogged(t, fake)

	calls := []sdk.CallToolParams{
		{Name: "get-screens", Arguments: map[string]any{}},
		{Name: "create-app", Arguments: map[string]any{"prompt": "build it"}},
		{Name: "update-app", Arguments: map[string]any{"app_id": "app_1", "prompt": "改主题"}},
		{Name: "get-app-status", Arguments: map[string]any{"app_id": "app_1"}},
		{Name: "get-app-operations", Arguments: map[string]any{"app_id": "app_1"}},
		{Name: "start-app", Arguments: map[string]any{"app_id": "app_1"}},
		{Name: "stop-app", Arguments: map[string]any{"app_id": "app_1"}},
		{Name: "send-request", Arguments: map[string]any{"app_id": "app_1", "operation_id": "op_1"}},
	}
	for _, call := range calls {
		t.Run(call.Name, func(t *testing.T) {
			result, err := session.CallTool(ctx, &call)
			if err != nil {
				t.Fatalf("protocol error: %v", err)
			}
			if !result.IsError {
				t.Fatalf("tool error was not reported: %+v", result)
			}
			if !strings.Contains(toolTextJSON(t, result), "service down") {
				t.Fatalf("text = %q", toolTextJSON(t, result))
			}
		})
	}
}

func TestToolSchemasRejectMalformedArguments(t *testing.T) {
	session, _, ctx := connectLogged(t, &fakeApps{})

	cases := []sdk.CallToolParams{
		{Name: "create-app", Arguments: map[string]any{}},
		{Name: "update-app", Arguments: map[string]any{"app_id": "app_1"}},
		{Name: "get-app-status", Arguments: map[string]any{"app_id": 5}},
		{Name: "start-app", Arguments: map[string]any{"app_id": 5}},
		{Name: "send-request", Arguments: map[string]any{"app_id": "app_1", "operation_id": 5}},
	}
	for _, call := range cases {
		t.Run(call.Name, func(t *testing.T) {
			result, err := session.CallTool(ctx, &call)
			if err != nil {
				t.Fatalf("protocol error: %v", err)
			}
			if !result.IsError {
				t.Fatalf("invalid arguments accepted: %+v", result)
			}
		})
	}
}
