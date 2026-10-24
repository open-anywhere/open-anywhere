package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/service"
)

type fakeApps struct {
	mu          sync.Mutex
	identity    app.Identity
	prompt      string
	playerNames []string
	screens     []service.ScreenView
	create      service.CreateResult
	status      service.StatusView
	operations  service.OperationsView
	result      service.OperationResult
	operationID string
	parameters  map[string]any
	body        any
	force       bool
	err         error
}

func (f *fakeApps) record(owner app.Identity) {
	f.mu.Lock()
	f.identity = owner
	f.mu.Unlock()
}

func (f *fakeApps) currentIdentity() app.Identity {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.identity
}

func (f *fakeApps) GetScreens(_ context.Context, owner app.Identity) ([]service.ScreenView, error) {
	f.record(owner)
	return f.screens, f.err
}

func (f *fakeApps) CreateApp(_ context.Context, owner app.Identity, prompt string, playerNames []string) (service.CreateResult, error) {
	f.record(owner)
	f.mu.Lock()
	f.prompt = prompt
	f.playerNames = append([]string(nil), playerNames...)
	f.mu.Unlock()
	return f.create, f.err
}

func (f *fakeApps) GetAppStatus(_ context.Context, owner app.Identity, _ string) (service.StatusView, error) {
	f.record(owner)
	return f.status, f.err
}

func (f *fakeApps) GetAppOperations(_ context.Context, owner app.Identity, _ string) (service.OperationsView, error) {
	f.record(owner)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.operations.AppID != "" {
		return f.operations, f.err
	}
	return service.OperationsView{AppID: "app_1", State: app.StateIdle, Operations: []service.OperationView{{
		OperationID: "rollDice",
		Method:      http.MethodPost,
		Path:        "/api/roll",
		Description: "掷骰子",
	}}}, f.err
}

func (f *fakeApps) StartApp(_ context.Context, owner app.Identity, _, _ string, force bool) (service.StatusView, error) {
	f.record(owner)
	f.mu.Lock()
	f.force = force
	f.mu.Unlock()
	return f.status, f.err
}

func (f *fakeApps) StopApp(_ context.Context, owner app.Identity, _ string) (service.StatusView, error) {
	f.record(owner)
	return f.status, f.err
}

func (f *fakeApps) SendRequest(_ context.Context, owner app.Identity, _, operationID string, parameters map[string]any, body any) (service.OperationResult, error) {
	f.record(owner)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.operationID = operationID
	f.parameters = parameters
	f.body = body
	if f.result.Status != 0 {
		return f.result, f.err
	}
	return service.OperationResult{Status: http.StatusOK, Body: map[string]any{"ok": true}}, f.err
}

func TestServerSessionFlow(t *testing.T) {
	fake := &fakeApps{
		screens: []service.ScreenView{{ScreenID: "tv_1", Name: "客厅", Status: service.ScreenFree}},
		create:  service.CreateResult{AppID: "app_1", State: app.StateBuilding},
		status:  service.StatusView{AppID: "app_1", State: app.StateBuilding, Message: "正在构建"},
	}
	server := New(fake, 5*time.Second)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /mcp", server.ServeSSE)
	mux.HandleFunc("POST /mcp/message", server.ServeMessage)
	httpServer := httptest.NewServer(mux)
	defer httpServer.Close()
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, httpServer.URL+"/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Linx-Device-Id", "device")
	request.Header.Set("X-Linx-Agent-Id", "agent")
	request.Header.Set("X-Linx-Session-Id", "session")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET /mcp: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	if contentType := response.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/event-stream") {
		t.Fatalf("content type = %q", contentType)
	}
	reader := bufio.NewReader(response.Body)
	event, endpoint, err := readFrame(reader)
	if err != nil {
		t.Fatalf("endpoint frame: %v", err)
	}
	if event != "endpoint" || !strings.HasPrefix(endpoint, "/mcp/message?sessionid=") {
		t.Fatalf("endpoint = %q %q", event, endpoint)
	}
	messageURL := httpServer.URL + endpoint

	send := func(payload any) {
		t.Helper()
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		post, err := http.Post(messageURL, "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /mcp/message: %v", err)
		}
		defer post.Body.Close()
		if post.StatusCode != http.StatusAccepted {
			t.Fatalf("POST status = %d", post.StatusCode)
		}
	}
	call := func(id int, method string, params any) {
		t.Helper()
		payload := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
		if params != nil {
			payload["params"] = params
		}
		send(payload)
	}
	readResponse := func() map[string]any {
		t.Helper()
		event, data, err := readFrame(reader)
		if err != nil {
			t.Fatalf("read response: %v", err)
		}
		if event != "message" {
			t.Fatalf("event = %q", event)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			t.Fatalf("decode response %q: %v", data, err)
		}
		return payload
	}

	call(1, "initialize", map[string]any{"protocolVersion": "2024-11-05"})
	result := resultOf(t, readResponse())
	if result["protocolVersion"] != "2024-11-05" {
		t.Fatalf("protocol = %v", result["protocolVersion"])
	}
	serverInfo, _ := result["serverInfo"].(map[string]any)
	if serverInfo["name"] != "open-anywhere-gateway" {
		t.Fatalf("server info = %+v", serverInfo)
	}

	send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})

	call(2, "ping", nil)
	if result := resultOf(t, readResponse()); len(result) != 0 {
		t.Fatalf("ping result = %+v", result)
	}

	call(3, "tools/list", nil)
	tools, _ := resultOf(t, readResponse())["tools"].([]any)
	if len(tools) != 7 {
		t.Fatalf("tools = %d", len(tools))
	}
	names := make(map[string]bool, len(tools))
	var createTool map[string]any
	for _, raw := range tools {
		tool := raw.(map[string]any)
		names[tool["name"].(string)] = true
		if tool["name"] == "create-app" {
			createTool = tool
		}
	}
	for _, want := range []string{"get-screens", "create-app", "get-app-status", "get-app-operations", "start-app", "stop-app", "send-request"} {
		if !names[want] {
			t.Fatalf("missing tool %q in %+v", want, names)
		}
	}
	inputSchema, _ := createTool["inputSchema"].(map[string]any)
	properties, _ := inputSchema["properties"].(map[string]any)
	playerNamesSchema, _ := properties["player_names"].(map[string]any)
	if !schemaAllowsType(playerNamesSchema, "array") {
		t.Fatalf("player_names schema = %+v", playerNamesSchema)
	}
	items, _ := playerNamesSchema["items"].(map[string]any)
	if items["type"] != "string" {
		t.Fatalf("player_names items = %+v", items)
	}
	if description, _ := createTool["description"].(string); !strings.Contains(description, "player_names") || !strings.Contains(description, "不代表已准备好") {
		t.Fatalf("create-app description = %q", description)
	}

	call(4, "tools/call", map[string]any{"name": "get-screens", "arguments": map[string]any{}})
	result = resultOf(t, readResponse())
	if isError, _ := result["isError"].(bool); isError {
		t.Fatalf("result = %+v", result)
	}
	if text := toolText(t, result); !strings.Contains(text, "客厅") {
		t.Fatalf("text = %q", text)
	}
	if identity := fake.currentIdentity(); identity != (app.Identity{DeviceID: "device", AgentID: "agent", SessionID: "session"}) {
		t.Fatalf("identity = %+v", identity)
	}

	call(5, "tools/call", map[string]any{"name": "create-app", "arguments": map[string]any{"prompt": "做一个小游戏", "player_names": []string{"韩鲁鹏", "邓军辉"}}})
	result = resultOf(t, readResponse())
	if text := toolText(t, result); !strings.Contains(text, "app_1") {
		t.Fatalf("text = %q", text)
	}
	fake.mu.Lock()
	prompt := fake.prompt
	playerNames := append([]string(nil), fake.playerNames...)
	fake.mu.Unlock()
	if prompt != "做一个小游戏" {
		t.Fatalf("prompt = %q", prompt)
	}
	if len(playerNames) != 2 || playerNames[0] != "韩鲁鹏" || playerNames[1] != "邓军辉" {
		t.Fatalf("player_names = %+v", playerNames)
	}

	call(6, "tools/call", map[string]any{"name": "get-app-status", "arguments": map[string]any{"app_id": "app_1"}})
	if text := toolText(t, resultOf(t, readResponse())); !strings.Contains(text, "正在构建") {
		t.Fatalf("text = %q", text)
	}

	call(10, "tools/call", map[string]any{"name": "get-app-operations", "arguments": map[string]any{"app_id": "app_1"}})
	if text := toolText(t, resultOf(t, readResponse())); !strings.Contains(text, "rollDice") {
		t.Fatalf("text = %q", text)
	}

	call(11, "tools/call", map[string]any{"name": "start-app", "arguments": map[string]any{"app_id": "app_1", "screen_id": "tv_1"}})
	if text := toolText(t, resultOf(t, readResponse())); !strings.Contains(text, "app_1") {
		t.Fatalf("text = %q", text)
	}

	fake.mu.Lock()
	forced := fake.force
	fake.mu.Unlock()
	if forced {
		t.Fatal("force must default to false")
	}
	call(111, "tools/call", map[string]any{"name": "start-app", "arguments": map[string]any{"app_id": "app_1", "screen_id": "tv_1", "force": true}})
	_ = toolText(t, resultOf(t, readResponse()))
	fake.mu.Lock()
	forced = fake.force
	fake.mu.Unlock()
	if !forced {
		t.Fatal("force=true did not reach the service")
	}

	call(12, "tools/call", map[string]any{"name": "stop-app", "arguments": map[string]any{"app_id": "app_1"}})
	if text := toolText(t, resultOf(t, readResponse())); !strings.Contains(text, "app_1") {
		t.Fatalf("text = %q", text)
	}

	call(13, "tools/call", map[string]any{"name": "send-request", "arguments": map[string]any{
		"app_id":       "app_1",
		"operation_id": "rollDice",
		"parameters":   map[string]any{"count": 2},
		"body":         map[string]any{"sides": 6},
	}})
	if text := toolText(t, resultOf(t, readResponse())); !strings.Contains(text, "ok") {
		t.Fatalf("text = %q", text)
	}
	fake.mu.Lock()
	if fake.operationID != "rollDice" || fake.parameters["count"] != float64(2) {
		t.Fatalf("send-request = %q %+v", fake.operationID, fake.parameters)
	}
	if body, _ := fake.body.(map[string]any); body["sides"] != float64(6) {
		t.Fatalf("body = %+v", fake.body)
	}
	fake.err = errors.New("service exploded")
	fake.mu.Unlock()
	call(14, "tools/call", map[string]any{"name": "get-screens"})
	errorResult := resultOf(t, readResponse())
	if errorResult["isError"] != true {
		t.Fatalf("error result = %+v", errorResult)
	}
	if text := toolText(t, errorResult); !strings.Contains(text, "service exploded") {
		t.Fatalf("text = %q", text)
	}
	fake.mu.Lock()
	fake.err = nil
	fake.mu.Unlock()

	call(7, "tools/call", map[string]any{"name": "unknown"})
	payload := readResponse()
	rpcErr, _ := payload["error"].(map[string]any)
	if rpcErr == nil || rpcErr["code"] != float64(-32602) || !strings.Contains(strings.ToLower(fmt.Sprint(rpcErr["message"])), "unknown tool") {
		t.Fatalf("payload = %+v", payload)
	}

	call(8, "tools/call", map[string]any{"name": "get-app-status", "arguments": map[string]any{"app_id": "app_1", "extra": true}})
	payload = readResponse()
	result = resultOf(t, payload)
	if result["isError"] != true {
		t.Fatalf("payload = %+v", payload)
	}
	if text := toolText(t, result); !strings.Contains(text, "additional") && !strings.Contains(text, "validating") {
		t.Fatalf("text = %q", text)
	}

	unknown, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 9, "method": "missing/method"})
	if err != nil {
		t.Fatal(err)
	}
	post, err := http.Post(messageURL, "application/json", bytes.NewReader(unknown))
	if err != nil {
		t.Fatal(err)
	}
	post.Body.Close()
	if post.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown method status = %d", post.StatusCode)
	}

	identity := fake.currentIdentity()
	server.Notify(identity, app.Notification{
		AppID:    "app_1",
		State:    app.StateError,
		ScreenID: "tv_1",
		Message:  "应用进程已退出",
		Error:    &app.Error{Stage: app.StageRunning, Code: "APP_EXITED"},
	})
	payload = readResponse()
	if payload["method"] != "notifications/app_status" {
		t.Fatalf("payload = %+v", payload)
	}
	if _, ok := payload["id"]; ok {
		t.Fatalf("notification has id: %+v", payload)
	}
	params, _ := payload["params"].(map[string]any)
	if params["app_id"] != "app_1" || params["screen_id"] != "tv_1" {
		t.Fatalf("params = %+v", params)
	}

	server.Notify(app.Identity{DeviceID: "other", SessionID: "other"}, app.Notification{AppID: "app_2"})
	unexpected := make(chan error, 1)
	go func() {
		_, _, err := readFrame(reader)
		unexpected <- err
	}()
	select {
	case err := <-unexpected:
		if err == nil {
			t.Fatal("notification for another identity was delivered")
		}
	case <-time.After(150 * time.Millisecond):
	}

	response404, err := http.Post(httpServer.URL+"/mcp/message?sessionId=missing", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	response404.Body.Close()
	if response404.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown session status = %d", response404.StatusCode)
	}
	response400, err := http.Post(messageURL, "application/json", strings.NewReader("{"))
	if err != nil {
		t.Fatal(err)
	}
	response400.Body.Close()
	if response400.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid body status = %d", response400.StatusCode)
	}
}

func schemaAllowsType(schema map[string]any, want string) bool {
	switch value := schema["type"].(type) {
	case string:
		return value == want
	case []any:
		for _, item := range value {
			if item == want {
				return true
			}
		}
	}
	return false
}

func resultOf(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	if raw, ok := payload["error"]; ok && raw != nil {
		t.Fatalf("rpc error = %v", raw)
	}
	result, ok := payload["result"].(map[string]any)
	if !ok {
		t.Fatalf("missing result: %+v", payload)
	}
	return result
}

func toolText(t *testing.T, result map[string]any) string {
	t.Helper()
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("content = %+v", result)
	}
	block, _ := content[0].(map[string]any)
	text, _ := block["text"].(string)
	return text
}

func readFrame(reader *bufio.Reader) (string, string, error) {
	var (
		event string
		data  []string
	)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", "", err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if event == "" && len(data) == 0 {
				continue
			}
			return event, strings.Join(data, "\n"), nil
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			event = value
		case "data":
			data = append(data, value)
		}
	}
}
