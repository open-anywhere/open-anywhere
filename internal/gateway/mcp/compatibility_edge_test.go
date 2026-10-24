package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/service"
)

type failingExtended struct{ fakeApps }

func (f *failingExtended) GetOpenAPI(context.Context, app.Identity, string) (map[string]any, error) {
	return nil, errors.New("openapi unavailable")
}
func (f *failingExtended) ListApps(context.Context, app.Identity) ([]service.AppSummary, error) {
	return nil, errors.New("list unavailable")
}

func mustCall(t *testing.T, session *sdk.ClientSession, ctx context.Context, params sdk.CallToolParams) *sdk.CallToolResult {
	t.Helper()
	result, err := session.CallTool(ctx, &params)
	if err != nil {
		t.Fatalf("CallTool(%s): %v", params.Name, err)
	}
	return result
}

func TestCompatibilityRejectsInvalidArguments(t *testing.T) {
	session, _, ctx := connectLogged(t, &extendedFake{})

	result := mustCall(t, session, ctx, sdk.CallToolParams{Name: "send_request", Arguments: map[string]any{"app_id": "app_1"}})
	if !result.IsError {
		t.Fatalf("missing operationId should fail: %+v", result)
	}
	if text := toolTextJSON(t, result); !strings.Contains(text, "INVALID_ARGUMENT") {
		t.Fatalf("text = %q", text)
	}

	// Wrong types are rejected by the JSON schema as well.
	result = mustCall(t, session, ctx, sdk.CallToolParams{Name: "get_openapi", Arguments: map[string]any{"app_id": 5}})
	if !result.IsError {
		t.Fatalf("non-string app_id should fail: %+v", result)
	}
}

func TestCompatibilityToolFailuresAreReported(t *testing.T) {
	fake := &failingExtended{}
	session, _, ctx := connectLogged(t, fake)

	result := mustCall(t, session, ctx, sdk.CallToolParams{Name: "get_openapi", Arguments: map[string]any{"app_id": "app_1"}})
	if !result.IsError || !strings.Contains(toolTextJSON(t, result), "openapi unavailable") {
		t.Fatalf("get_openapi result = %+v", result)
	}

	result = mustCall(t, session, ctx, sdk.CallToolParams{Name: "list-apps", Arguments: map[string]any{}})
	if !result.IsError || !strings.Contains(toolTextJSON(t, result), "list unavailable") {
		t.Fatalf("list-apps result = %+v", result)
	}

	// send_request propagates service errors and marks non-2xx as failures.
	fake.fakeApps.err = errors.New("send exploded")
	result = mustCall(t, session, ctx, sdk.CallToolParams{Name: "send_request", Arguments: map[string]any{"app_id": "app_1", "operationId": "op"}})
	if !result.IsError || !strings.Contains(toolTextJSON(t, result), "send exploded") {
		t.Fatalf("send_request error result = %+v", result)
	}

	fake.fakeApps.err = nil
	fake.fakeApps.result = service.OperationResult{Status: http.StatusInternalServerError, Body: map[string]any{"message": "boom"}}
	result = mustCall(t, session, ctx, sdk.CallToolParams{Name: "send_request", Arguments: map[string]any{"app_id": "app_1", "operationId": "op"}})
	if !result.IsError || !strings.Contains(toolTextJSON(t, result), `"ok":false`) {
		t.Fatalf("send_request non-2xx result = %+v", result)
	}
}

// toolTextJSON extracts the raw text content of a compatibility tool result.
func toolTextJSON(t *testing.T, result *sdk.CallToolResult) string {
	t.Helper()
	for _, content := range result.Content {
		if text, ok := content.(*sdk.TextContent); ok {
			return text.Text
		}
	}
	t.Fatalf("no text content: %+v", result)
	return ""
}

func TestStreamableHTTPRejectsMissingIdentityOrTools(t *testing.T) {
	payload := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"probe","version":"1"}}}`

	withHeaders := func(request *http.Request) {
		request.Header.Set("X-Linx-Device-Id", "device")
		request.Header.Set("X-Linx-Session-Id", "session")
	}
	post := func() *http.Request {
		request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(payload))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		return request
	}

	// No identity headers -> the factory returns nil and the SDK answers 400.
	server := New(&fakeApps{}, 0)
	recorder := httptest.NewRecorder()
	server.StreamableHTTP("", false).ServeHTTP(recorder, post())
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("missing identity status = %d", recorder.Code)
	}

	// Identity present but no tools installed -> 400 as well.
	noTools := New(nil, 0)
	request := post()
	withHeaders(request)
	recorder = httptest.NewRecorder()
	noTools.StreamableHTTP("", false).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("nil tools status = %d", recorder.Code)
	}

	// A configured public host rejects requests addressed to another host.
	restricted := New(&fakeApps{}, 0)
	request = post()
	withHeaders(request)
	request.Host = "attacker.example"
	recorder = httptest.NewRecorder()
	restricted.StreamableHTTP("public.example", false).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("host mismatch status = %d", recorder.Code)
	}
}
