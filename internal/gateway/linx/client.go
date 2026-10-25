// Package linx talks to the Linx platform's management API: agents, MCP
// servers and device activation. It is used by the portal provisioner only.
package linx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	devicelinx "github.com/open-anywhere/open-anywhere/internal/voiceentry/linx"
)

// Error is one platform or transport failure. Code is the platform's business
// code; Status is the HTTP status. A transport failure has both at zero.
type Error struct {
	Status  int
	Code    int
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	if e.Code != 0 {
		return fmt.Sprintf("%s (code=%d, HTTP %d)", e.Message, e.Code, e.Status)
	}
	return e.Message
}

func (e *Error) Unwrap() error { return e.Cause }

// IsCode reports whether err carries a platform business code.
func IsCode(err error, code int) bool {
	var target *Error
	return errors.As(err, &target) && target.Code == code
}

// IsAuth reports authentication and authorization failures.
func IsAuth(err error) bool {
	var target *Error
	if !errors.As(err, &target) {
		return false
	}
	return target.Status == http.StatusUnauthorized || target.Status == http.StatusForbidden || target.Code == 401
}

// IsUnavailable reports transport failures and 5xx responses.
func IsUnavailable(err error) bool {
	var target *Error
	if !errors.As(err, &target) {
		return false
	}
	return target.Cause != nil || target.Status >= 500
}

// IsBindRejected reports an activation code or agent the platform refused.
func IsBindRejected(err error) bool {
	var target *Error
	if !errors.As(err, &target) {
		return false
	}
	return target.Status == http.StatusBadRequest || target.Status == http.StatusNotFound
}

// Config configures the management client.
type Config struct {
	BaseURL      string
	APIKey       string
	OTAURL       string
	PublicMCPURL string
	MCPName      string
	MCPToken     string
	HTTPClient   *http.Client
	OTAClient    devicelinx.OTAClient
}

// Client is the platform management API client. It is safe for concurrent use.
type Client struct {
	baseURL      string
	apiKey       string
	publicMCPURL string
	mcpName      string
	mcpToken     string
	ota          devicelinx.OTAClient
	http         *http.Client
}

// New returns a client. The API key is only ever written to the Authorization
// header, never to errors or logs.
func New(cfg Config) *Client {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	ota := cfg.OTAClient
	if ota.BaseURL == "" {
		ota.BaseURL = cfg.OTAURL
	}
	if ota.APIBase == "" {
		ota.APIBase = cfg.BaseURL
	}
	if ota.HTTPClient == nil {
		ota.HTTPClient = httpClient
	}
	return &Client{
		baseURL:      strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:       cfg.APIKey,
		publicMCPURL: cfg.PublicMCPURL,
		mcpName:      cfg.MCPName,
		mcpToken:     cfg.MCPToken,
		ota:          ota,
		http:         httpClient,
	}
}

// AgentRequest is the body of POST /xiaozhi/agent.
type AgentRequest struct {
	Name          string `json:"agentName"`
	AssistantName string `json:"assistantName"`
	SystemPrompt  string `json:"systemPrompt,omitempty"`
	LLMModelID    string `json:"llmModelId,omitempty"`
	MemModelID    string `json:"memModelId,omitempty"`
	IntentModelID string `json:"intentModelId,omitempty"`
}

// Check verifies the platform address and API key without changing anything.
func (c *Client) Check(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/xiaozhi/agent/list?limit=1", nil, nil)
}

// CreateAgent creates a platform agent and returns its ID.
func (c *Client) CreateAgent(ctx context.Context, request AgentRequest) (string, error) {
	var id string
	if err := c.do(ctx, http.MethodPost, "/xiaozhi/agent", request, &id); err != nil {
		return "", err
	}
	if strings.TrimSpace(id) == "" {
		return "", errors.New("linx: agent creation returned no id")
	}
	return id, nil
}

// MCPSpec describes the portal's application-management MCP.
type MCPSpec struct {
	Name        string
	URL         string
	Token       string
	Description string
}

// EnsureMCP returns the platform's MCP server and instance IDs, creating them
// when they do not exist yet. A persisted ID is trusted; a 614 (name already
// exists) is resolved by listing again.
func (c *Client) EnsureMCP(ctx context.Context, spec MCPSpec) (serverID, instanceID string, err error) {
	servers, err := c.listServers(ctx)
	if err != nil {
		return "", "", err
	}
	for _, server := range servers {
		if server.Name == spec.Name && (server.ConfigSchema.SSE.URL == "" || server.ConfigSchema.SSE.URL == spec.URL) {
			serverID = server.ID
			break
		}
	}
	if serverID == "" {
		body := mcpServerBody(spec)
		if err := c.do(ctx, http.MethodPost, "/v1/mcp-store/servers", body, nil); err != nil {
			if !IsCode(err, 614) {
				return "", "", err
			}
			servers, listErr := c.listServers(ctx)
			if listErr != nil {
				return "", "", listErr
			}
			for _, server := range servers {
				if server.Name == spec.Name {
					serverID = server.ID
					break
				}
			}
			if serverID == "" {
				return "", "", err
			}
		}
	}
	if serverID == "" {
		servers, err = c.listServers(ctx)
		if err != nil {
			return "", "", err
		}
		for _, server := range servers {
			if server.Name == spec.Name {
				serverID = server.ID
				break
			}
		}
	}
	if serverID == "" {
		return "", "", errors.New("linx: created MCP server was not found")
	}

	instances, err := c.listInstances(ctx)
	if err != nil {
		return "", "", err
	}
	for _, instance := range instances {
		if instance.MCPID == serverID && instance.Name == spec.Name {
			return serverID, instance.ID, nil
		}
	}
	if err := c.do(ctx, http.MethodPost, "/v1/mcps", mcpInstanceBody(spec, serverID), nil); err != nil {
		if !IsCode(err, 614) {
			return "", "", err
		}
		instances, err = c.listInstances(ctx)
		if err != nil {
			return "", "", err
		}
	}
	for _, instance := range instances {
		if instance.MCPID == serverID && instance.Name == spec.Name {
			return serverID, instance.ID, nil
		}
	}
	// The create response embeds the new instance; list again as a last resort.
	instances, err = c.listInstances(ctx)
	if err != nil {
		return "", "", err
	}
	for _, instance := range instances {
		if instance.MCPID == serverID && instance.Name == spec.Name {
			return serverID, instance.ID, nil
		}
	}
	return "", "", errors.New("linx: created MCP instance was not found")
}

// AttachMCP replaces an agent's MCP bindings with mcpIDs.
func (c *Client) AttachMCP(ctx context.Context, agentID string, mcpIDs []string) error {
	if strings.TrimSpace(agentID) == "" {
		return errors.New("linx: agent id is required")
	}
	if mcpIDs == nil {
		mcpIDs = []string{}
	}
	return c.do(ctx, http.MethodPut, "/v1/agents/"+agentID+"/mcps", map[string]any{"mcp_ids": mcpIDs}, nil)
}

// BindDevice binds a device behind an activation code to an agent. An already
// bound device (HTTP 409) is not an error for the caller.
func (c *Client) BindDevice(ctx context.Context, agentID, code string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/xiaozhi/device/bind/"+agentID+"/"+code, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	response, err := c.http.Do(request)
	if err != nil {
		return &Error{Message: "bind request failed", Cause: err}
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<16))
	if response.StatusCode == http.StatusConflict {
		return nil
	}
	var envelope struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	_ = json.Unmarshal(body, &envelope)
	if response.StatusCode >= 200 && response.StatusCode < 300 && envelope.Code == 0 {
		return nil
	}
	message := envelope.Msg
	if message == "" {
		message = "platform refused the device binding"
	}
	return &Error{Status: response.StatusCode, Code: envelope.Code, Message: message}
}

// ReportOTA reports a device identity and returns the platform's answer.
func (c *Client) ReportOTA(ctx context.Context, device devicelinx.Device) (devicelinx.OTAResponse, error) {
	response, err := c.ota.Report(ctx, device)
	if err != nil {
		return response, &Error{Message: "OTA report failed", Cause: err}
	}
	return response, nil
}

type platformServer struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ConfigSchema struct {
		SSE struct {
			URL string `json:"url"`
		} `json:"sse"`
	} `json:"config_schema"`
}

type platformInstance struct {
	ID    string `json:"id"`
	MCPID string `json:"mcp_id"`
	Name  string `json:"name"`
}

func (c *Client) listServers(ctx context.Context) ([]platformServer, error) {
	var data json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/v1/mcp-store/servers", nil, &data); err != nil {
		return nil, err
	}
	if len(data) == 0 || string(data) == "null" {
		return nil, nil
	}
	var direct []platformServer
	if err := json.Unmarshal(data, &direct); err == nil {
		return direct, nil
	}
	var wrapped struct {
		Servers []platformServer `json:"servers"`
	}
	if err := json.Unmarshal(data, &wrapped); err != nil {
		return nil, fmt.Errorf("linx: decode MCP servers: %w", err)
	}
	return wrapped.Servers, nil
}

func (c *Client) listInstances(ctx context.Context) ([]platformInstance, error) {
	var data json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/v1/mcps", nil, &data); err != nil {
		return nil, err
	}
	if len(data) == 0 || string(data) == "null" {
		return nil, nil
	}
	var direct []platformInstance
	if err := json.Unmarshal(data, &direct); err == nil {
		return direct, nil
	}
	var wrapped struct {
		MCPs []platformInstance `json:"mcps"`
	}
	if err := json.Unmarshal(data, &wrapped); err != nil {
		return nil, fmt.Errorf("linx: decode MCP instances: %w", err)
	}
	return wrapped.MCPs, nil
}

const (
	// The Linx console always submits a logo and document URL when it creates
	// a custom MCP server; without them the platform rejects the body with
	// 400 invalid request body.
	mcpLogoURL     = "https://raw.githubusercontent.com/open-anywhere/.github/main/assets/logo/open-anywhere-avatar-1024.png"
	mcpDocumentURL = "https://github.com/open-anywhere/open-anywhere"
	// mcpTransportStreamableHTTP matches the gateway's POST /mcp endpoint.
	// The Linx console only offers legacy SSE when editing an existing server,
	// so new servers must use transport_type 1.
	mcpTransportStreamableHTTP = 1
)

func mcpServerBody(spec MCPSpec) map[string]any {
	header := map[string]string{}
	if spec.Token != "" {
		header["Authorization"] = "Bearer " + spec.Token
	}
	return map[string]any{
		"name":         spec.Name,
		"is_private":   true,
		"logo_url":     mcpLogoURL,
		"document_url": mcpDocumentURL,
		"tags":         []string{},
		"desc":         spec.Description,
		"content":      "",
		"config_schema": map[string]any{
			"types":          []string{"sse"},
			"type":           "sse",
			"transport_type": mcpTransportStreamableHTTP,
			"sse": map[string]any{
				"type":   0,
				"url":    spec.URL,
				"params": map[string]any{},
				"header": header,
			},
		},
	}
}

func mcpInstanceBody(spec MCPSpec, serverID string) map[string]any {
	header := map[string]string{}
	if spec.Token != "" {
		header["Authorization"] = "Bearer " + spec.Token
	}
	return map[string]any{
		"mcp_id":   serverID,
		"name":     spec.Name,
		"desc":     spec.Description,
		"logo_url": mcpLogoURL,
		"config": map[string]any{
			"type":           "sse",
			"transport_type": mcpTransportStreamableHTTP,
			"sse": map[string]any{
				"type":   0,
				"url":    spec.URL,
				"params": map[string]any{},
				"header": header,
			},
		},
	}
}

// do issues one platform request and unwraps {code,msg,data}. Both the HTTP
// status and the business code decide success, because the platform sometimes
// reports failures with HTTP 200.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("linx: encode request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("linx: build request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return &Error{Message: "platform request failed", Cause: err}
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return &Error{Status: response.StatusCode, Message: "read platform response", Cause: err}
	}
	var envelope struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	decoded := json.Unmarshal(raw, &envelope) == nil
	if response.StatusCode == http.StatusUnauthorized || (decoded && envelope.Code == 401) {
		return &Error{Status: response.StatusCode, Code: envelope.Code, Message: "platform authentication failed"}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || (decoded && envelope.Code != 0) {
		message := envelope.Msg
		if message == "" {
			message = fmt.Sprintf("platform returned HTTP %d", response.StatusCode)
		}
		return &Error{Status: response.StatusCode, Code: envelope.Code, Message: message}
	}
	if out != nil {
		if !decoded {
			if err := json.Unmarshal(raw, out); err != nil {
				return fmt.Errorf("linx: decode platform response: %w", err)
			}
			return nil
		}
		if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
			return nil
		}
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return fmt.Errorf("linx: decode platform data: %w", err)
		}
	}
	return nil
}
