package linx

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCreateAgentUnwrapsThePlatformEnvelope(t *testing.T) {
	var authorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		if r.Method != http.MethodPost || r.URL.Path != "/xiaozhi/agent" {
			t.Errorf("request=%s %s", r.Method, r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"code":0,"msg":"success","data":"agent_123"}`)
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL, APIKey: "secret-key"})
	id, err := client.CreateAgent(context.Background(), AgentRequest{Name: "test", AssistantName: "小灵"})
	if err != nil || id != "agent_123" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if authorization != "Bearer secret-key" {
		t.Fatalf("authorization=%q", authorization)
	}
}

func TestPlatformBusinessCodesBecomeErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		check  func(error) bool
	}{
		{"success status with failure code", http.StatusOK, `{"code":614,"msg":"already exists"}`, func(err error) bool { return IsCode(err, 614) }},
		{"unauthorized", http.StatusUnauthorized, `{"code":401,"msg":"token invalid"}`, IsAuth},
		{"server error", http.StatusBadGateway, `{"code":0,"msg":""}`, IsUnavailable},
		{"malformed success", http.StatusOK, `not-json`, func(err error) bool { return err != nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			client := New(Config{BaseURL: server.URL, APIKey: "secret-key"})
			_, err := client.CreateAgent(context.Background(), AgentRequest{Name: "test"})
			if err == nil || !tc.check(err) {
				t.Fatalf("err=%v", err)
			}
			if strings.Contains(err.Error(), "secret-key") {
				t.Fatalf("the API key leaked into the error: %v", err)
			}
		})
	}
}

func TestEnsureMCPCreatesAndThenReusesTheSameResources(t *testing.T) {
	var (
		mu                 sync.Mutex
		servers            []map[string]any
		instances          []map[string]any
		creates            int
		attached           []string
		serverCreateBody   map[string]any
		instanceCreateBody map[string]any
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/mcp-store/servers":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"servers": servers}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/mcp-store/servers":
			creates++
			_ = json.NewDecoder(r.Body).Decode(&serverCreateBody)
			servers = append(servers, map[string]any{
				"id": "server_1", "name": "Open Anywhere",
				"config_schema": map[string]any{"sse": map[string]any{"url": "https://gateway.example/sse"}},
			})
			_, _ = io.WriteString(w, `{"code":0,"data":{"id":"server_1"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/mcps":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": instances})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/mcps":
			creates++
			_ = json.NewDecoder(r.Body).Decode(&instanceCreateBody)
			instances = append(instances, map[string]any{"id": "mcp_1", "mcp_id": "server_1", "name": "Open Anywhere"})
			_, _ = io.WriteString(w, `{"code":0,"data":{"id":"mcp_1"}}`)
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/v1/agents/"):
			attached = append(attached, r.URL.Path)
			_, _ = io.WriteString(w, `{"code":0}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL, APIKey: "secret-key"})
	spec := MCPSpec{Name: "Open Anywhere", URL: "https://gateway.example/sse", Token: "mcp-token"}
	serverID, instanceID, err := client.EnsureMCP(context.Background(), spec)
	if err != nil || serverID != "server_1" || instanceID != "mcp_1" {
		t.Fatalf("server=%q instance=%q err=%v", serverID, instanceID, err)
	}
	if creates != 2 {
		t.Fatalf("creates=%d", creates)
	}
	if serverCreateBody["logo_url"] != mcpLogoURL {
		t.Fatalf("the platform requires a logo_url, got %v", serverCreateBody["logo_url"])
	}
	if serverCreateBody["document_url"] != mcpDocumentURL {
		t.Fatalf("document_url=%v", serverCreateBody["document_url"])
	}
	serverSchema, _ := serverCreateBody["config_schema"].(map[string]any)
	if serverSchema["type"] != "sse" || serverSchema["transport_type"] != float64(mcpTransportStreamableHTTP) {
		t.Fatalf("config_schema=%v", serverSchema)
	}
	if instanceCreateBody["logo_url"] != mcpLogoURL {
		t.Fatalf("instance logo_url=%v", instanceCreateBody["logo_url"])
	}
	instanceConfig, _ := instanceCreateBody["config"].(map[string]any)
	if instanceConfig["transport_type"] != float64(mcpTransportStreamableHTTP) {
		t.Fatalf("instance config=%v", instanceConfig)
	}
	serverID, instanceID, err = client.EnsureMCP(context.Background(), spec)
	if err != nil || serverID != "server_1" || instanceID != "mcp_1" {
		t.Fatalf("second call server=%q instance=%q err=%v", serverID, instanceID, err)
	}
	if creates != 2 {
		t.Fatalf("the second call created resources again: creates=%d", creates)
	}

	if err := client.AttachMCP(context.Background(), "agent_1", []string{instanceID}); err != nil {
		t.Fatalf("AttachMCP: %v", err)
	}
	if len(attached) != 1 || attached[0] != "/v1/agents/agent_1/mcps" {
		t.Fatalf("attached=%v", attached)
	}
}

func TestBindDeviceTreatsConflictAsAlreadyBound(t *testing.T) {
	status := http.StatusConflict
	body := `{"code":0}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/xiaozhi/device/bind/agent_1/123456" {
			t.Errorf("path=%s", r.URL.Path)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	defer server.Close()
	client := New(Config{BaseURL: server.URL, APIKey: "secret-key"})

	if err := client.BindDevice(context.Background(), "agent_1", "123456"); err != nil {
		t.Fatalf("409 was not treated as bound: %v", err)
	}

	status, body = http.StatusBadRequest, `{"code":400,"msg":"invalid code"}`
	if err := client.BindDevice(context.Background(), "agent_1", "123456"); !IsBindRejected(err) {
		t.Fatalf("err=%v", err)
	}
}

func TestHTTPClientTimeoutIsReportedAsUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = io.WriteString(w, `{"code":0,"data":"agent_1"}`)
	}))
	defer server.Close()
	client := New(Config{
		BaseURL:    server.URL,
		APIKey:     "secret-key",
		HTTPClient: &http.Client{Timeout: 20 * time.Millisecond},
	})
	if _, err := client.CreateAgent(context.Background(), AgentRequest{Name: "test"}); !IsUnavailable(err) {
		t.Fatalf("err=%v", err)
	}
}
