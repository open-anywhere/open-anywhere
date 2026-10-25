package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/service"
)

type fakeOwnerValidator struct {
	mu       sync.Mutex
	calls    int
	err      error
	failFrom int
}

func (v *fakeOwnerValidator) ValidateOwner(context.Context, app.Identity) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.calls++
	if v.err != nil && (v.failFrom == 0 || v.calls >= v.failFrom) {
		return v.err
	}
	return nil
}

func (v *fakeOwnerValidator) setError(err error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.err = err
}

func (v *fakeOwnerValidator) callCount() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.calls
}

func identityHeaders() map[string]string {
	return map[string]string{
		"X-Linx-Device-Id":  "device",
		"X-Linx-Agent-Id":   "agent",
		"X-Linx-Session-Id": "session",
	}
}

func newOwnerServer(tools Apps, validator OwnerValidator) *Server {
	server := New(tools, time.Second)
	server.SetOwnerValidator(validator)
	return server
}

func decodeErrorCode(t *testing.T, body io.Reader) string {
	t.Helper()
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(body).Decode(&payload); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	return payload.Error.Code
}

func TestServeSSEWithValidatorRequiresAllIdentityHeaders(t *testing.T) {
	validator := &fakeOwnerValidator{}
	server := newOwnerServer(&fakeApps{}, validator)
	defer server.Close()
	httpServer := httptest.NewServer(http.HandlerFunc(server.ServeSSE))
	defer httpServer.Close()

	cases := []map[string]string{
		{},
		{"X-Linx-Device-Id": "device", "X-Linx-Session-Id": "session"},
		{"X-Linx-Device-Id": "device", "X-Linx-Agent-Id": "agent"},
		{"X-Linx-Agent-Id": "agent", "X-Linx-Session-Id": "session"},
	}
	for _, headers := range cases {
		request, err := http.NewRequest(http.MethodGet, httpServer.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		for key, value := range headers {
			request.Header.Set(key, value)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		code := decodeErrorCode(t, response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest || code != "MISSING_IDENTITY" {
			t.Fatalf("headers %+v: status=%d code=%q", headers, response.StatusCode, code)
		}
	}
	if calls := validator.callCount(); calls != 0 {
		t.Fatalf("validator was called %d times for missing identity", calls)
	}
}

func TestServeSSEWithValidatorMapsIdentityErrors(t *testing.T) {
	validator := &fakeOwnerValidator{}
	server := newOwnerServer(&fakeApps{}, validator)
	defer server.Close()
	httpServer := httptest.NewServer(http.HandlerFunc(server.ServeSSE))
	defer httpServer.Close()

	cases := []struct {
		name     string
		err      error
		status   int
		wantCode string
	}{
		{"unknown owner", &service.ToolError{Code: "OWNER_NOT_FOUND"}, http.StatusForbidden, "OWNER_NOT_FOUND"},
		{"not registered", &service.ToolError{Code: "IDENTITY_NOT_REGISTERED"}, http.StatusForbidden, "IDENTITY_NOT_REGISTERED"},
		{"disabled user", &service.ToolError{Code: "USER_DISABLED"}, http.StatusForbidden, "USER_DISABLED"},
		{"other tool error", &service.ToolError{Code: "SERVICE_NOT_CONFIGURED"}, http.StatusServiceUnavailable, "SERVICE_NOT_CONFIGURED"},
		{"plain error", errors.New("database down"), http.StatusServiceUnavailable, "IDENTITY_CHECK_FAILED"},
	}
	for _, testCase := range cases {
		validator.setError(testCase.err)
		request, err := http.NewRequest(http.MethodGet, httpServer.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		for key, value := range identityHeaders() {
			request.Header.Set(key, value)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		code := decodeErrorCode(t, response.Body)
		response.Body.Close()
		if response.StatusCode != testCase.status || code != testCase.wantCode {
			t.Fatalf("%s: status=%d code=%q", testCase.name, response.StatusCode, code)
		}
	}
}

func TestServeMessageValidatesEveryPost(t *testing.T) {
	validator := &fakeOwnerValidator{
		err:      &service.ToolError{Code: "USER_DISABLED"},
		failFrom: 1,
	}
	server := newOwnerServer(&fakeApps{}, validator)
	identity := app.Identity{DeviceID: "device", AgentID: "agent", SessionID: "session"}
	server.mu.Lock()
	server.sessions["s1"] = &session{identity: identity, cancel: func() {}}
	server.mu.Unlock()
	defer server.Close()

	post := func(headers map[string]string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, "/mcp/message?sessionid=s1", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		for key, value := range headers {
			request.Header.Set(key, value)
		}
		recorder := httptest.NewRecorder()
		server.ServeMessage(recorder, request)
		return recorder
	}

	headers := identityHeaders()
	for attempt := 1; attempt <= 2; attempt++ {
		recorder := post(headers)
		if recorder.Code != http.StatusForbidden || decodeErrorCode(t, recorder.Body) != "USER_DISABLED" {
			t.Fatalf("attempt %d: status=%d body=%s", attempt, recorder.Code, recorder.Body.String())
		}
		if calls := validator.callCount(); calls != attempt {
			t.Fatalf("validator calls = %d after %d posts", calls, attempt)
		}
	}

	for _, missing := range []string{"X-Linx-Device-Id", "X-Linx-Agent-Id", "X-Linx-Session-Id"} {
		headers := identityHeaders()
		delete(headers, missing)
		recorder := post(headers)
		if recorder.Code != http.StatusForbidden || decodeErrorCode(t, recorder.Body) != "IDENTITY_MISMATCH" {
			t.Fatalf("missing %s: status=%d body=%s", missing, recorder.Code, recorder.Body.String())
		}
	}
	headers = identityHeaders()
	headers["X-Linx-Agent-Id"] = "other-agent"
	if recorder := post(headers); recorder.Code != http.StatusForbidden || decodeErrorCode(t, recorder.Body) != "IDENTITY_MISMATCH" {
		t.Fatalf("wrong agent: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if calls := validator.callCount(); calls != 2 {
		t.Fatalf("a mismatched header reached the validator: calls=%d", calls)
	}

	recorder := httptest.NewRecorder()
	server.ServeMessage(recorder, httptest.NewRequest(http.MethodPost, "/mcp/message?sessionid=missing", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unknown session status = %d", recorder.Code)
	}
}

func TestServeSSEWithValidatedIdentityScopesToolCalls(t *testing.T) {
	apps := &fakeApps{screens: []service.ScreenView{{ScreenID: "tv_1", Name: "客厅", Status: service.ScreenFree}}}
	validator := &fakeOwnerValidator{}
	server := newOwnerServer(apps, validator)
	defer server.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /mcp", server.ServeSSE)
	mux.HandleFunc("POST /mcp/message", server.ServeMessage)
	httpServer := httptest.NewServer(mux)
	defer httpServer.Close()

	headers := identityHeaders()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, httpServer.URL+"/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("SSE status = %d", response.StatusCode)
	}
	reader := bufio.NewReader(response.Body)
	event, endpoint, err := readFrame(reader)
	if err != nil || event != "endpoint" {
		t.Fatalf("endpoint frame = %q %q %v", event, endpoint, err)
	}

	post := func(payload any) {
		t.Helper()
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		postRequest, err := http.NewRequest(http.MethodPost, httpServer.URL+endpoint, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		postRequest.Header.Set("Content-Type", "application/json")
		for key, value := range headers {
			postRequest.Header.Set(key, value)
		}
		postResponse, err := http.DefaultClient.Do(postRequest)
		if err != nil {
			t.Fatal(err)
		}
		defer postResponse.Body.Close()
		if postResponse.StatusCode != http.StatusAccepted {
			t.Fatalf("POST status = %d", postResponse.StatusCode)
		}
	}
	readMessage := func() map[string]any {
		t.Helper()
		event, data, err := readFrame(reader)
		if err != nil || event != "message" {
			t.Fatalf("message frame = %q %q %v", event, data, err)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			t.Fatal(err)
		}
		return payload
	}

	post(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2024-11-05"}})
	resultOf(t, readMessage())
	post(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	post(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "get-screens", "arguments": map[string]any{}}})
	result := resultOf(t, readMessage())
	if isError, _ := result["isError"].(bool); isError {
		t.Fatalf("result = %+v", result)
	}
	if identity := apps.currentIdentity(); identity != (app.Identity{DeviceID: "device", AgentID: "agent", SessionID: "session"}) {
		t.Fatalf("identity = %+v", identity)
	}
	if calls := validator.callCount(); calls < 4 {
		t.Fatalf("validator calls = %d; every connection and POST must be checked", calls)
	}
}

func TestStreamableHTTPWithValidatorEnforcesIdentity(t *testing.T) {
	const payload = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"probe","version":"1"}}}`
	post := func() *http.Request {
		request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(payload))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		return request
	}
	validator := &fakeOwnerValidator{}
	server := newOwnerServer(&fakeApps{}, validator)
	defer server.Close()
	handler := server.StreamableHTTP("", false)

	headers := identityHeaders()
	for _, missing := range []string{"X-Linx-Device-Id", "X-Linx-Agent-Id", "X-Linx-Session-Id"} {
		request := post()
		for key, value := range headers {
			if key != missing {
				request.Header.Set(key, value)
			}
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest || decodeErrorCode(t, recorder.Body) != "MISSING_IDENTITY" {
			t.Fatalf("missing %s: status=%d body=%s", missing, recorder.Code, recorder.Body.String())
		}
	}
	if calls := validator.callCount(); calls != 0 {
		t.Fatalf("validator called %d times for missing identity", calls)
	}

	// All headers and a passing validator: a real initialize handshake works
	// and the validator is consulted on every stateless request.
	request := post()
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("initialize status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	body, err := io.ReadAll(recorder.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "protocolVersion") || !strings.Contains(string(body), "serverInfo") {
		t.Fatalf("initialize body = %s", body)
	}
	if calls := validator.callCount(); calls != 1 {
		t.Fatalf("validator calls = %d after initialize", calls)
	}

	// Validator failures map to the same statuses as the SSE transport.
	cases := []struct {
		name     string
		err      error
		status   int
		wantCode string
	}{
		{"unknown owner", &service.ToolError{Code: "OWNER_NOT_FOUND"}, http.StatusForbidden, "OWNER_NOT_FOUND"},
		{"not registered", &service.ToolError{Code: "IDENTITY_NOT_REGISTERED"}, http.StatusForbidden, "IDENTITY_NOT_REGISTERED"},
		{"disabled user", &service.ToolError{Code: "USER_DISABLED"}, http.StatusForbidden, "USER_DISABLED"},
		{"other tool error", &service.ToolError{Code: "SERVICE_NOT_CONFIGURED"}, http.StatusServiceUnavailable, "SERVICE_NOT_CONFIGURED"},
		{"plain error", errors.New("database down"), http.StatusServiceUnavailable, "IDENTITY_CHECK_FAILED"},
	}
	for _, testCase := range cases {
		validator.setError(testCase.err)
		request := post()
		for key, value := range headers {
			request.Header.Set(key, value)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != testCase.status || decodeErrorCode(t, recorder.Body) != testCase.wantCode {
			t.Fatalf("%s: status=%d body=%s", testCase.name, recorder.Code, recorder.Body.String())
		}
	}
	if calls := validator.callCount(); calls != 1+len(cases) {
		t.Fatalf("validator calls = %d; every request must be checked", calls)
	}
}
