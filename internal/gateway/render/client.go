// Package render is the control plane's client for the Render runtime API.
package render

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/open-anywhere/open-anywhere/pkg/casting"
)

// State is a Render instance state.
type State string

const (
	StateStarting State = "starting"
	StateReady    State = "ready"
	StateError    State = "error"
	StateStopping State = "stopping"
	StateStopped  State = "stopped"
)

// Error is the structured error of a failed instance.
type Error struct {
	Stage   string `json:"stage"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// App is a Render instance.
type App struct {
	AppID      string               `json:"app_id"`
	InstanceID string               `json:"instance_id"`
	State      State                `json:"state"`
	Port       int                  `json:"port"`
	AppBaseURL string               `json:"app_base_url"`
	Source     casting.StreamSource `json:"source"`
	Error      *Error               `json:"error,omitempty"`
	StartedAt  time.Time            `json:"started_at"`
}

// CreateRequest starts an application instance.
type CreateRequest struct {
	AppID       string `json:"app_id"`
	ArtifactURL string `json:"artifact_url"`
	Port        int    `json:"port"`
}

// Port describes one TCP port known to Render.
type Port struct {
	AppID      string `json:"app_id,omitempty"`
	Port       int    `json:"port"`
	Occupied   bool   `json:"occupied"`
	Owner      string `json:"owner"`
	InstanceID string `json:"instance_id,omitempty"`
}

// HTTPError is a non-2xx Render response.
type HTTPError struct {
	Status  int
	Code    string
	Message string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("render: %d %s: %s", e.Status, e.Code, e.Message)
}

// IsStatus reports whether err is an HTTP error with status.
func IsStatus(err error, status int) bool {
	var target *HTTPError
	return errors.As(err, &target) && target.Status == status
}

// IsCode reports whether err is an HTTP error with code.
func IsCode(err error, code string) bool {
	var target *HTTPError
	return errors.As(err, &target) && target.Code == code
}

// Client calls the Render HTTP API.
type Client struct {
	baseURL string
	http    *http.Client
}

// New creates a Render client.
func New(baseURL string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{Timeout: timeout}}
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return decodeError(response)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(out)
}

func decodeError(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	var payload struct {
		Error Error `json:"error"`
	}
	_ = json.Unmarshal(body, &payload)
	message := payload.Error.Message
	if message == "" {
		message = strings.TrimSpace(string(body))
	}
	if message == "" {
		message = response.Status
	}
	return &HTTPError{Status: response.StatusCode, Code: payload.Error.Code, Message: message}
}

// Create downloads and starts the application, returning the ready instance.
func (c *Client) Create(ctx context.Context, request CreateRequest) (App, error) {
	var app App
	err := c.do(ctx, http.MethodPost, "/v1/apps", request, &app)
	return app, err
}

// Get returns one instance.
func (c *Client) Get(ctx context.Context, instanceID string) (App, error) {
	var app App
	err := c.do(ctx, http.MethodGet, "/v1/apps/"+url.PathEscape(instanceID), nil, &app)
	return app, err
}

// Delete stops one instance and releases its resources.
func (c *Client) Delete(ctx context.Context, instanceID string) (App, error) {
	var app App
	err := c.do(ctx, http.MethodDelete, "/v1/apps/"+url.PathEscape(instanceID), nil, &app)
	return app, err
}

// ListPorts returns the occupied TCP ports known to Render.
func (c *Client) ListPorts(ctx context.Context) ([]Port, error) {
	var payload struct {
		Ports []Port `json:"ports"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/ports", nil, &payload); err != nil {
		return nil, err
	}
	return payload.Ports, nil
}

// GetPort inspects one TCP port.
func (c *Client) GetPort(ctx context.Context, port int) (Port, error) {
	var value Port
	err := c.do(ctx, http.MethodGet, "/v1/ports/"+strconv.Itoa(port), nil, &value)
	return value, err
}

// Health checks the Render service itself.
func (c *Client) Health(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/healthz", nil, nil)
}
