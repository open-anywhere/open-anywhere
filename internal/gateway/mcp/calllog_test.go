package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/open-anywhere/open-anywhere/internal/gateway/service"
)

type logSink struct {
	mu    sync.Mutex
	lines []string
}

func (l *logSink) logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func (l *logSink) all() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

func connectLogged(t *testing.T, fake Apps) (*sdk.ClientSession, *logSink, context.Context) {
	t.Helper()
	sink := &logSink{}
	server := New(fake, time.Second)
	server.logf = sink.logf
	httpServer := httptest.NewServer(server.StreamableHTTP("", false))
	t.Cleanup(httpServer.Close)
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	client := sdk.NewClient(&sdk.Implementation{Name: "probe", Version: "9"}, nil)
	session, err := client.Connect(ctx, &sdk.StreamableClientTransport{
		Endpoint: httpServer.URL, DisableStandaloneSSE: true,
		HTTPClient: &http.Client{Transport: contextHeaders{http.DefaultTransport}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session, sink, ctx
}

func TestCallLogShowsDiscoveryOperationsAndCalls(t *testing.T) {
	session, sink, ctx := connectLogged(t, &extendedFake{})
	if _, err := session.ListTools(ctx, nil); err != nil {
		t.Fatal(err)
	}
	for _, call := range []sdk.CallToolParams{
		{Name: "get-app-operations", Arguments: map[string]any{"app_id": "app_1"}},
		{Name: "send-request", Arguments: map[string]any{"app_id": "app_1", "operation_id": "rollDice", "body": map[string]any{"count": 2}}},
		{Name: "start-app", Arguments: map[string]any{"app_id": "app_1", "screen_id": "tv_1", "force": true}},
	} {
		if _, err := session.CallTool(ctx, &call); err != nil {
			t.Fatal(err)
		}
	}
	log := sink.all()
	for _, want := range []string{
		// the stateless transport answers initialize itself, so only tools/list shows discovery
		"mcp: tools/list -> ",
		"get-app-operations",
		"tools/call get-app-operations app=app_1 -> ok operations=1 [rollDice]",
		`tools/call send-request app=app_1 op=rollDice body={"count":2} -> ok status=200`,
		"tools/call start-app app=app_1 screen=tv_1 force=true -> ok",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("log is missing %q:\n%s", want, log)
		}
	}
}

func TestCallLogRecordsFailedCallsWithTheirCode(t *testing.T) {
	fake := &extendedFake{}
	fake.err = &service.ToolError{Code: "APP_NOT_FOUND", Message: "应用不存在"}
	session, sink, ctx := connectLogged(t, fake)
	result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "send-request", Arguments: map[string]any{"app_id": "gone", "operation_id": "attack"}})
	if err != nil || !result.IsError {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if log := sink.all(); !strings.Contains(log, "tools/call send-request app=gone op=attack -> error APP_NOT_FOUND") {
		t.Fatalf("log=\n%s", log)
	}
}

func TestCallLogHelpers(t *testing.T) {
	if got := truncate("你好世界你好世界", 4); got != "你好世界…" {
		t.Fatalf("truncate=%q", got)
	}
	if got := shortSession("abc"); got != "abc" {
		t.Fatalf("shortSession=%q", got)
	}
	if describeCall("resources/list", nil, nil, nil) != "" {
		t.Fatal("unrelated methods must not be logged")
	}
	if got := resultSummary("get_openapi", `paths: {a: {get: {operationId: getA}}, b: {post: {operationId: postB}}}`); !strings.Contains(got, "operations=2 [getA postB]") {
		t.Fatalf("yaml spec summary=%q", got)
	}
}
