package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
)

// withCallLog records what Linx does on one MCP session: that it connected,
// which tools it was shown, which operations an application offered it, and
// every tool call with its outcome. Without it a Linx that never calls anything
// is indistinguishable from one whose calls fail.
//
// Request bodies are shortened and only the fields needed to follow a call are
// logged; the control-plane credentials never pass through here.
func withCallLog(logf func(string, ...any), identity app.Identity) sdk.Middleware {
	return func(next sdk.MethodHandler) sdk.MethodHandler {
		return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
			started := time.Now()
			result, err := next(ctx, method, req)
			if line := describeCall(method, req, result, err); line != "" {
				logf("mcp: %s session=%s (%s)", line, shortSession(identity.SessionID), time.Since(started).Round(time.Millisecond))
			}
			return result, err
		}
	}
}

func shortSession(id string) string {
	if len(id) > 6 {
		return id[:6] + "…"
	}
	return id
}

// describeCall renders one log line, or "" for methods that are not worth logging.
func describeCall(method string, req sdk.Request, result sdk.Result, err error) string {
	switch method {
	case "initialize":
		if r, ok := req.(*sdk.InitializeRequest); ok && r.Params != nil && r.Params.ClientInfo != nil {
			return fmt.Sprintf("initialize client=%s/%s", r.Params.ClientInfo.Name, r.Params.ClientInfo.Version)
		}
		return "initialize"
	case "tools/list":
		if r, ok := result.(*sdk.ListToolsResult); ok {
			names := make([]string, 0, len(r.Tools))
			for _, t := range r.Tools {
				names = append(names, t.Name)
			}
			return fmt.Sprintf("tools/list -> %d tools %v", len(names), names)
		}
		return "tools/list"
	case "tools/call":
		return describeToolCall(req, result, err)
	}
	return ""
}

func describeToolCall(req sdk.Request, result sdk.Result, err error) string {
	r, ok := req.(*sdk.CallToolRequest)
	if !ok || r.Params == nil {
		return "tools/call"
	}
	args := map[string]any{}
	_ = json.Unmarshal(r.Params.Arguments, &args)
	line := "tools/call " + r.Params.Name
	if v := str(args["app_id"]); v != "" {
		line += " app=" + v
	}
	if v := firstString(args, "operation_id", "operationId"); v != "" {
		line += " op=" + v
	}
	if v := str(args["screen_id"]); v != "" {
		line += " screen=" + v
	}
	if force, _ := args["force"].(bool); force {
		line += " force=true"
	}
	if body := firstValue(args, "body", "requestBody"); body != nil {
		line += " body=" + truncate(compact(body), 160)
	}
	if err != nil {
		return line + " -> protocol error: " + truncate(err.Error(), 120)
	}
	out, ok := result.(*sdk.CallToolResult)
	if !ok || out == nil {
		return line + " -> no result"
	}
	text := resultText(out)
	if out.IsError {
		code, _, _ := strings.Cut(text, ":")
		return line + " -> error " + truncate(strings.TrimSpace(code), 40) + " " + truncate(strings.TrimSpace(strings.TrimPrefix(text, code+":")), 100)
	}
	return line + " -> ok" + resultSummary(r.Params.Name, text)
}

var operationIDPattern = regexp.MustCompile(`"?operation_?[iI][dD]"?\s*[:=]\s*"?([A-Za-z0-9_.\-]+)`)

// resultSummary adds what matters about a successful result: the operations an
// application offers, or the HTTP status of a forwarded request.
func resultSummary(tool, text string) string {
	switch tool {
	case "get-app-operations", "get_openapi":
		ids := map[string]bool{}
		for _, m := range operationIDPattern.FindAllStringSubmatch(text, -1) {
			ids[m[1]] = true
		}
		list := make([]string, 0, len(ids))
		for id := range ids {
			list = append(list, id)
		}
		sort.Strings(list)
		return fmt.Sprintf(" operations=%d %v bytes=%d", len(list), list, len(text))
	case "send-request", "send_request":
		var parsed struct {
			Status int `json:"status"`
		}
		if json.Unmarshal([]byte(text), &parsed) == nil && parsed.Status != 0 {
			return fmt.Sprintf(" status=%d", parsed.Status)
		}
	case "start-app", "stop-app", "get-app-status":
		var parsed struct {
			State         string `json:"state"`
			ReplacedAppID string `json:"replaced_app_id"`
		}
		if json.Unmarshal([]byte(text), &parsed) == nil && parsed.State != "" {
			if parsed.ReplacedAppID != "" {
				return fmt.Sprintf(" state=%s replaced=%s", parsed.State, parsed.ReplacedAppID)
			}
			return " state=" + parsed.State
		}
	}
	return ""
}

func resultText(r *sdk.CallToolResult) string {
	for _, c := range r.Content {
		if t, ok := c.(*sdk.TextContent); ok {
			return t.Text
		}
	}
	if r.StructuredContent != nil {
		return compact(r.StructuredContent)
	}
	return ""
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := str(m[k]); s != "" {
			return s
		}
	}
	return ""
}

func firstValue(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			return v
		}
	}
	return nil
}

func compact(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
