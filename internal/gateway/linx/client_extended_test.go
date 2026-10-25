package linx

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	devicelinx "github.com/open-anywhere/open-anywhere/internal/voiceentry/linx"
)

func TestErrorFormattingAndUnwrap(t *testing.T) {
	cause := errors.New("connection reset")
	withCause := &Error{Message: "platform request failed", Cause: cause}
	if got, want := withCause.Error(), "platform request failed: connection reset"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if !errors.Is(withCause, cause) {
		t.Fatal("Unwrap did not expose the cause")
	}
	if got, want := (&Error{Message: "already exists", Code: 614, Status: http.StatusOK}).Error(), "already exists (code=614, HTTP 200)"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if got, want := (&Error{Message: "plain failure"}).Error(), "plain failure"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestPredicatesIgnoreErrorsFromOtherSources(t *testing.T) {
	foreign := errors.New("plain")
	if IsCode(foreign, 614) || IsAuth(foreign) || IsUnavailable(foreign) || IsBindRejected(foreign) {
		t.Fatal("a non-platform error matched a platform predicate")
	}
	if IsCode(nil, 0) || IsAuth(nil) || IsUnavailable(nil) || IsBindRejected(nil) {
		t.Fatal("a nil error matched a platform predicate")
	}
	if !IsAuth(&Error{Status: http.StatusForbidden}) {
		t.Fatal("403 should be an authentication failure")
	}
	if !IsAuth(&Error{Status: http.StatusOK, Code: 401}) {
		t.Fatal("business code 401 should be an authentication failure")
	}
	if !IsBindRejected(&Error{Status: http.StatusNotFound}) {
		t.Fatal("404 should be a rejected binding")
	}
	if !IsUnavailable(&Error{Cause: errors.New("dial")}) {
		t.Fatal("a transport cause should be unavailable")
	}
	if IsBindRejected(&Error{Status: http.StatusConflict}) {
		t.Fatal("409 is handled by the caller, not as a rejection")
	}
}

func TestCheckVerifiesThePlatformAddressAndKey(t *testing.T) {
	var path, query, authorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, query, authorization = r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization")
		_, _ = io.WriteString(w, `{"code":0}`)
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL + "/", APIKey: "secret-key"})
	if err := client.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if path != "/xiaozhi/agent/list" || query != "limit=1" {
		t.Fatalf("request = %s?%s", path, query)
	}
	if authorization != "Bearer secret-key" {
		t.Fatalf("authorization = %q", authorization)
	}
}

func TestCreateAgentRequiresANonBlankID(t *testing.T) {
	for _, body := range []string{`{"code":0}`, `{"code":0,"data":null}`, `{"code":0,"data":"   "}`} {
		body := body
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, body)
			}))
			defer server.Close()
			id, err := New(Config{BaseURL: server.URL}).CreateAgent(context.Background(), AgentRequest{Name: "test"})
			if err == nil || id != "" || !strings.Contains(err.Error(), "returned no id") {
				t.Fatalf("id=%q err=%v", id, err)
			}
		})
	}
}

func TestCreateAgentAcceptsAPlainJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `"agent_plain"`)
	}))
	defer server.Close()

	id, err := New(Config{BaseURL: server.URL}).CreateAgent(context.Background(), AgentRequest{Name: "test"})
	if err != nil || id != "agent_plain" {
		t.Fatalf("id=%q err=%v", id, err)
	}
}

func TestCreateAgentRejectsUndecodableData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"code":0,"data":{"id":"agent_1"}}`)
	}))
	defer server.Close()

	_, err := New(Config{BaseURL: server.URL}).CreateAgent(context.Background(), AgentRequest{Name: "test"})
	if err == nil || !strings.Contains(err.Error(), "decode platform data") {
		t.Fatalf("err = %v", err)
	}
}

type stubResponse struct {
	status int
	body   string
}

// stubPlatform answers each method/path with scripted responses in order. A
// test that forgets a request gets a 501 so the client's behavior is visible.
type stubPlatform struct {
	mu     sync.Mutex
	routes map[string][]stubResponse
}

func newStubPlatform(t *testing.T, routes map[string][]stubResponse) (*stubPlatform, *httptest.Server) {
	t.Helper()
	platform := &stubPlatform{routes: routes}
	server := httptest.NewServer(http.HandlerFunc(platform.serve))
	t.Cleanup(server.Close)
	return platform, server
}

func (p *stubPlatform) serve(w http.ResponseWriter, r *http.Request) {
	key := r.Method + " " + r.URL.Path
	p.mu.Lock()
	responses := p.routes[key]
	if len(responses) == 0 {
		p.mu.Unlock()
		http.Error(w, "unexpected request "+key, http.StatusNotImplemented)
		return
	}
	response := responses[0]
	p.routes[key] = responses[1:]
	p.mu.Unlock()
	status := response.status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = io.WriteString(w, response.body)
}

func (p *stubPlatform) remaining(t *testing.T) int {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	total := 0
	for _, responses := range p.routes {
		total += len(responses)
	}
	return total
}

func TestEnsureMCPHandlesPlatformFailures(t *testing.T) {
	const (
		serversRoute = "GET /v1/mcp-store/servers"
		serversPost  = "POST /v1/mcp-store/servers"
		mcpsRoute    = "GET /v1/mcps"
		mcpsPost     = "POST /v1/mcps"
		serverJSON   = `{"id":"server_1","name":"Open Anywhere","config_schema":{"sse":{"url":"https://gateway.example/sse"}}}`
		instanceJSON = `{"id":"mcp_1","mcp_id":"server_1","name":"Open Anywhere"}`
	)
	var (
		emptyList  = `{"code":0,"data":[]}`
		serverOK   = `{"code":0,"data":[` + serverJSON + `]}`
		instanceOK = `{"code":0,"data":[` + instanceJSON + `]}`
		serverDown = stubResponse{status: http.StatusInternalServerError, body: `{"code":0,"msg":"down"}`}
		conflict   = stubResponse{body: `{"code":614,"msg":"already exists"}`}
		found      = func(err error) bool { return err == nil }
		unavail    = func(err error) bool { return IsUnavailable(err) }
		contains   = func(fragment string) func(error) bool {
			return func(err error) bool { return err != nil && strings.Contains(err.Error(), fragment) }
		}
	)
	cases := []struct {
		name         string
		routes       map[string][]stubResponse
		check        func(error) bool
		wantServer   string
		wantInstance string
	}{
		{
			name:   "server list fails",
			routes: map[string][]stubResponse{serversRoute: {serverDown}},
			check:  unavail,
		},
		{
			name:   "server list cannot be decoded",
			routes: map[string][]stubResponse{serversRoute: {{body: `{"code":0,"data":"oops"}`}}},
			check:  contains("decode MCP servers"),
		},
		{
			name: "server create fails",
			routes: map[string][]stubResponse{
				serversRoute: {{body: `{"code":0}`}},
				serversPost:  {serverDown},
			},
			check: unavail,
		},
		{
			name: "server create conflicts and the relist fails",
			routes: map[string][]stubResponse{
				serversRoute: {{body: `{"code":0}`}, serverDown},
				serversPost:  {conflict},
			},
			check: unavail,
		},
		{
			name: "server create conflicts and the relist finds it",
			routes: map[string][]stubResponse{
				serversRoute: {{body: `{"code":0}`}, {body: serverOK}},
				serversPost:  {conflict},
				mcpsRoute:    {{body: instanceOK}},
			},
			check:        found,
			wantServer:   "server_1",
			wantInstance: "mcp_1",
		},
		{
			name: "server create conflicts and the relist misses it",
			routes: map[string][]stubResponse{
				serversRoute: {{body: `{"code":0}`}, {body: emptyList}},
				serversPost:  {conflict},
			},
			check: func(err error) bool { return IsCode(err, 614) },
		},
		{
			name: "server create succeeds but the relist fails",
			routes: map[string][]stubResponse{
				serversRoute: {{body: `{"code":0}`}, serverDown},
				serversPost:  {{body: `{"code":0}`}},
			},
			check: unavail,
		},
		{
			name: "created server cannot be found",
			routes: map[string][]stubResponse{
				serversRoute: {{body: `{"code":0}`}, {body: emptyList}},
				serversPost:  {{body: `{"code":0}`}},
			},
			check: contains("created MCP server was not found"),
		},
		{
			name: "instance list fails",
			routes: map[string][]stubResponse{
				serversRoute: {{body: serverOK}},
				mcpsRoute:    {serverDown},
			},
			check: unavail,
		},
		{
			name: "instance list cannot be decoded",
			routes: map[string][]stubResponse{
				serversRoute: {{body: serverOK}},
				mcpsRoute:    {{body: `{"code":0,"data":"oops"}`}},
			},
			check: contains("decode MCP instances"),
		},
		{
			name: "instance create fails",
			routes: map[string][]stubResponse{
				serversRoute: {{body: serverOK}},
				mcpsRoute:    {{body: emptyList}},
				mcpsPost:     {serverDown},
			},
			check: unavail,
		},
		{
			name: "instance create conflicts and the relist fails",
			routes: map[string][]stubResponse{
				serversRoute: {{body: serverOK}},
				mcpsRoute:    {{body: emptyList}, serverDown},
				mcpsPost:     {conflict},
			},
			check: unavail,
		},
		{
			name: "instance create conflicts and the relist finds it",
			routes: map[string][]stubResponse{
				serversRoute: {{body: serverOK}},
				mcpsRoute:    {{body: emptyList}, {body: instanceOK}},
				mcpsPost:     {conflict},
			},
			check:        found,
			wantServer:   "server_1",
			wantInstance: "mcp_1",
		},
		{
			name: "instance is visible only after the final list",
			routes: map[string][]stubResponse{
				serversRoute: {{body: serverOK}},
				mcpsRoute:    {{body: emptyList}, {body: instanceOK}},
				mcpsPost:     {{body: `{"code":0}`}},
			},
			check:        found,
			wantServer:   "server_1",
			wantInstance: "mcp_1",
		},
		{
			name: "instance create succeeds but the final list fails",
			routes: map[string][]stubResponse{
				serversRoute: {{body: serverOK}},
				mcpsRoute:    {{body: emptyList}, serverDown},
				mcpsPost:     {{body: `{"code":0}`}},
			},
			check: unavail,
		},
		{
			name: "created instance cannot be found",
			routes: map[string][]stubResponse{
				serversRoute: {{body: serverOK}},
				mcpsRoute:    {{body: emptyList}, {body: emptyList}},
				mcpsPost:     {{body: `{"code":0}`}},
			},
			check: contains("created MCP instance was not found"),
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			platform, server := newStubPlatform(t, testCase.routes)
			client := New(Config{BaseURL: server.URL, APIKey: "secret-key"})
			serverID, instanceID, err := client.EnsureMCP(context.Background(), MCPSpec{
				Name:  "Open Anywhere",
				URL:   "https://gateway.example/sse",
				Token: "mcp-token",
			})
			if !testCase.check(err) {
				t.Fatalf("err = %v", err)
			}
			if err == nil {
				if serverID != testCase.wantServer || instanceID != testCase.wantInstance {
					t.Fatalf("server=%q instance=%q", serverID, instanceID)
				}
			}
			if left := platform.remaining(t); left != 0 {
				t.Fatalf("%d scripted responses were not requested", left)
			}
		})
	}
}

func TestEnsureMCPAcceptsWrappedLists(t *testing.T) {
	platform, server := newStubPlatform(t, map[string][]stubResponse{
		"GET /v1/mcp-store/servers": {{body: `{"code":0,"data":{"servers":[
			{"id":"other","name":"Other","config_schema":{"sse":{"url":"https://other.example"}}},
			{"id":"server_1","name":"Open Anywhere","config_schema":{"sse":{"url":""}}}]}}`}},
		"GET /v1/mcps": {{body: `{"code":0,"data":{"mcps":[
			{"id":"mcp_1","mcp_id":"server_1","name":"Open Anywhere"}]}}`}},
	})
	client := New(Config{BaseURL: server.URL})
	serverID, instanceID, err := client.EnsureMCP(context.Background(), MCPSpec{Name: "Open Anywhere", URL: "https://gateway.example/sse"})
	if err != nil || serverID != "server_1" || instanceID != "mcp_1" {
		t.Fatalf("server=%q instance=%q err=%v", serverID, instanceID, err)
	}
	if left := platform.remaining(t); left != 0 {
		t.Fatalf("%d scripted responses were not requested", left)
	}
}

func TestAttachMCPDefaultsToAnEmptyBindingList(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/v1/agents/agent_1/mcps" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = io.WriteString(w, `{"code":0}`)
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL})
	if err := client.AttachMCP(context.Background(), "   ", nil); err == nil || !strings.Contains(err.Error(), "agent id is required") {
		t.Fatalf("err = %v", err)
	}
	if err := client.AttachMCP(context.Background(), "agent_1", nil); err != nil {
		t.Fatalf("AttachMCP: %v", err)
	}
	ids, ok := body["mcp_ids"].([]any)
	if !ok || len(ids) != 0 {
		t.Fatalf("mcp_ids = %#v", body["mcp_ids"])
	}
}

func TestBindDeviceReportsSuccessAndRefusal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"code":0}`)
	}))
	defer server.Close()
	client := New(Config{BaseURL: server.URL, APIKey: "secret-key"})
	if err := client.BindDevice(context.Background(), "agent_1", "123456"); err != nil {
		t.Fatalf("successful bind: %v", err)
	}

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `not-json`)
	}))
	defer failing.Close()
	err := New(Config{BaseURL: failing.URL}).BindDevice(context.Background(), "agent_1", "123456")
	if err == nil || IsBindRejected(err) || !IsUnavailable(err) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "platform refused the device binding") {
		t.Fatalf("err = %v", err)
	}
}

type errorTransport struct{ err error }

func (t errorTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, t.err }

type readErrorBody struct{}

func (readErrorBody) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (readErrorBody) Close() error             { return nil }

type readErrorTransport struct{}

func (readErrorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: readErrorBody{}}, nil
}

func TestBindDeviceReportsRequestAndTransportErrors(t *testing.T) {
	broken := New(Config{BaseURL: "http://[::1"})
	if err := broken.BindDevice(context.Background(), "agent_1", "123456"); err == nil || IsBindRejected(err) {
		t.Fatalf("malformed base URL err = %v", err)
	}

	client := New(Config{
		BaseURL:    "http://platform.example",
		APIKey:     "secret-key",
		HTTPClient: &http.Client{Transport: errorTransport{err: errors.New("connection reset")}},
	})
	err := client.BindDevice(context.Background(), "agent_1", "123456")
	if !IsUnavailable(err) || !strings.Contains(err.Error(), "bind request failed") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "secret-key") {
		t.Fatalf("the API key leaked into the error: %v", err)
	}
}

func TestDoReportsEncodingBuildAndReadFailures(t *testing.T) {
	client := New(Config{})
	if err := client.do(context.Background(), http.MethodPost, "/x", make(chan int), nil); err == nil || !strings.Contains(err.Error(), "encode request") {
		t.Fatalf("encode err = %v", err)
	}

	broken := New(Config{BaseURL: "http://[::1"})
	if err := broken.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "build request") {
		t.Fatalf("build err = %v", err)
	}

	readFailing := New(Config{
		BaseURL:    "http://platform.example",
		HTTPClient: &http.Client{Transport: readErrorTransport{}},
	})
	err := readFailing.Check(context.Background())
	if !IsUnavailable(err) || !strings.Contains(err.Error(), "read platform response") {
		t.Fatalf("read err = %v", err)
	}
}

func TestReportOTASendsTheDeviceIdentity(t *testing.T) {
	var path string
	var headers http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, headers = r.URL.Path, r.Header.Clone()
		_, _ = io.WriteString(w, `{"activation":{"code":"654321"}}`)
	}))
	defer server.Close()

	client := New(Config{OTAClient: devicelinx.OTAClient{BaseURL: server.URL, HTTPClient: server.Client()}})
	response, err := client.ReportOTA(context.Background(), devicelinx.Device{MAC: "aa:bb:cc", ClientID: "client-1"})
	if err != nil {
		t.Fatalf("ReportOTA: %v", err)
	}
	if response.Activation == nil || response.Activation.Code != "654321" {
		t.Fatalf("response = %+v", response)
	}
	if path != "/" {
		t.Fatalf("path = %q", path)
	}
	if headers.Get("Device-Id") != "aa:bb:cc" || headers.Get("Client-Id") != "client-1" {
		t.Fatalf("headers = %v", headers)
	}
}

func TestReportOTAWrapsPlatformFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":"denied"}`)
	}))
	defer server.Close()

	client := New(Config{OTAClient: devicelinx.OTAClient{BaseURL: server.URL, HTTPClient: server.Client()}})
	response, err := client.ReportOTA(context.Background(), devicelinx.Device{MAC: "aa:bb:cc", ClientID: "client-1"})
	if !IsUnavailable(err) || !strings.Contains(err.Error(), "OTA report failed") {
		t.Fatalf("err = %v", err)
	}
	if response.Error != "denied" {
		t.Fatalf("response = %+v", response)
	}
}
