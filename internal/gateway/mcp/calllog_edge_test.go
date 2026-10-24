package mcp

import (
	"errors"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestDescribeCallInitializeWithClientInfo(t *testing.T) {
	request := &sdk.InitializeRequest{Params: &sdk.InitializeParams{ClientInfo: &sdk.Implementation{Name: "linx", Version: "1.2.3"}}}
	if got := describeCall("initialize", request, nil, nil); got != "initialize client=linx/1.2.3" {
		t.Fatalf("describeCall = %q", got)
	}
	if got := describeCall("initialize", nil, nil, nil); got != "initialize" {
		t.Fatalf("fallback = %q", got)
	}
}

func TestDescribeCallListToolsFallback(t *testing.T) {
	// A non-ListTools result for tools/list still yields the method name.
	if got := describeCall("tools/list", nil, nil, nil); got != "tools/list" {
		t.Fatalf("fallback = %q", got)
	}
	result := &sdk.ListToolsResult{Tools: []*sdk.Tool{{Name: "a"}, {Name: "b"}}}
	if got := describeCall("tools/list", nil, result, nil); got != "tools/list -> 2 tools [a b]" {
		t.Fatalf("describeCall = %q", got)
	}
}

func TestDescribeToolCallMalformedInputs(t *testing.T) {
	if got := describeCall("tools/call", nil, nil, nil); got != "tools/call" {
		t.Fatalf("non call request = %q", got)
	}
	request := &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Name: "send-request"}}
	if got := describeCall("tools/call", request, nil, nil); !strings.HasSuffix(got, "-> no result") {
		t.Fatalf("missing result = %q", got)
	}
	// Invalid JSON arguments are ignored, not fatal.
	request.Params.Arguments = []byte(`"not-an-object"`)
	if got := describeCall("tools/call", request, &sdk.CallToolResult{}, nil); !strings.Contains(got, "tools/call send-request") {
		t.Fatalf("string arguments = %q", got)
	}
	if got := describeCall("tools/call", request, &sdk.CallToolResult{}, errors.New("protocol down")); !strings.Contains(got, "protocol error: protocol down") {
		t.Fatalf("protocol error = %q", got)
	}
}

func TestResultSummaryReplacedApp(t *testing.T) {
	if got := resultSummary("start-app", `{"state":"running","replaced_app_id":"old_1"}`); got != " state=running replaced=old_1" {
		t.Fatalf("summary = %q", got)
	}
	if got := resultSummary("get-app-status", `not json`); got != "" {
		t.Fatalf("invalid json summary = %q", got)
	}
	if got := resultSummary("send-request", `{"status":0}`); got != "" {
		t.Fatalf("zero status summary = %q", got)
	}
	if got := resultSummary("other", `{"status":200}`); got != "" {
		t.Fatalf("unknown tool summary = %q", got)
	}
}

func TestResultTextAndCompact(t *testing.T) {
	if got := resultText(&sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "first"}}}); got != "first" {
		t.Fatalf("text = %q", got)
	}
	structured := &sdk.CallToolResult{StructuredContent: map[string]any{"a": 1}}
	if got := resultText(structured); got != `{"a":1}` {
		t.Fatalf("structured = %q", got)
	}
	if got := resultText(&sdk.CallToolResult{}); got != "" {
		t.Fatalf("empty = %q", got)
	}
	if got := compact(make(chan int)); got != "" {
		t.Fatalf("unmarshalable compact = %q", got)
	}
	if got := compact(map[string]any{"b": 2, "a": 1}); got != `{"a":1,"b":2}` {
		t.Fatalf("compact = %q", got)
	}
	if got := shortSession("abcdefgh"); got != "abcdef…" {
		t.Fatalf("shortSession = %q", got)
	}
	if got := truncate("short", 10); got != "short" {
		t.Fatalf("truncate = %q", got)
	}
}

func TestFirstHelpers(t *testing.T) {
	args := map[string]any{"operationId": "fallback", "body": nil, "requestBody": map[string]any{"x": 1}, "screen_id": 5}
	if got := firstString(args, "operation_id", "operationId"); got != "fallback" {
		t.Fatalf("firstString = %q", got)
	}
	if got := firstString(args, "missing"); got != "" {
		t.Fatalf("firstString missing = %q", got)
	}
	if got := firstValue(args, "body", "requestBody"); got == nil {
		t.Fatal("firstValue should skip nil values")
	}
	if got := firstValue(args, "screen_id", "missing"); got == nil {
		t.Fatal("firstValue should return non-nil values of any type")
	}
	if got := firstValue(args, "missing"); got != nil {
		t.Fatalf("firstValue missing = %v", got)
	}
	if str(5) != "" || str("x") != "x" {
		t.Fatal("str conversion")
	}
}
