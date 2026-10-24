package integration

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// This opt-in test mutates the registered demo and restores its initial state.
func TestLiveQuiz(t *testing.T) {
	endpoint := os.Getenv("GATEWAY_SMOKE_URL")
	if endpoint == "" {
		t.Skip("set GATEWAY_SMOKE_URL to test a running quiz via MCP")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := mcp.NewClient(&mcp.Implementation{Name: "go-quiz-smoke", Version: "1"}, nil)
	session, err := c.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint, DisableStandaloneSSE: true}, nil)
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
	if !names["get_openapi"] || !names["send_request"] {
		t.Fatal(names)
	}
	invoke := func(name string, args map[string]any) map[string]any {
		t.Helper()
		r, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(r.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		var out map[string]any
		if err = json.Unmarshal(b, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	doc := invoke("get_openapi", map[string]any{"service_id": "quiz-app"})
	if doc["openapi"] != "3.0.3" {
		t.Fatal(doc)
	}
	send := func(op string, params map[string]any, body any) map[string]any {
		t.Helper()
		p := map[string]any{"session_id": "quiz-session-001"}
		for k, v := range params {
			p[k] = v
		}
		a := map[string]any{"service_id": "quiz-app", "operationId": op, "parameters": p}
		if body != nil {
			a["requestBody"] = body
		}
		out := invoke("send_request", a)
		t.Log(op, out["status"], out["ok"])
		return out
	}
	success := func(out map[string]any) map[string]any {
		t.Helper()
		if out["ok"] != true || out["status"] != float64(200) {
			t.Fatal(out)
		}
		return out["data"].(map[string]any)
	}
	success(send("resetGame", nil, nil))
	defer send("resetGame", nil, nil)
	if out := send("startGame", nil, nil); out["status"] != float64(409) || out["ok"] != false {
		t.Fatal(out)
	}
	for _, id := range []string{"p1", "p2", "p3"} {
		success(send("playerReady", map[string]any{"player_id": id}, nil))
	}
	success(send("startGame", nil, nil))
	answer := success(send("submitAnswer", nil, map[string]any{"user_id": "u2", "transcript": "火车进站"}))
	evaluated := success(send("evaluateAnswer", map[string]any{"answer_id": answer["answer_id"]}, map[string]any{"answer_right": true, "reason": "Go migration smoke test"}))
	state := evaluated["state"].(map[string]any)
	if state["current_question_number"] != float64(2) {
		t.Fatal(state)
	}
	found := false
	for _, v := range state["players"].([]any) {
		p := v.(map[string]any)
		if p["user_id"] == "u2" {
			found = true
			if p["score"] != float64(1) {
				t.Fatal(p)
			}
		}
	}
	if !found {
		t.Fatal("player missing")
	}
	for _, op := range []string{"pauseGame", "resumeGame", "getGameState"} {
		success(send(op, nil, nil))
	}
}

// TestManagedDiscovery performs read-only checks against a running managed Gateway.
func TestManagedDiscovery(t *testing.T) {
	endpoint := os.Getenv("GATEWAY_MANAGED_URL")
	if endpoint == "" {
		t.Skip("set GATEWAY_MANAGED_URL for managed deployment discovery")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "managed-discovery", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint, DisableStandaloneSSE: true}, nil)
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
		t.Log(tool.Name)
	}
	for _, name := range []string{"create-app", "get-app-status", "get-screens", "start-app", "stop-app", "get_openapi", "send_request"} {
		if !names[name] {
			t.Fatal("missing tool", name)
		}
	}
	r, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_openapi", Arguments: map[string]any{"app_id": "nonexistent"}})
	if err != nil {
		t.Fatal(err)
	}
	if !r.IsError {
		t.Fatal("missing caller context accepted")
	}
}
