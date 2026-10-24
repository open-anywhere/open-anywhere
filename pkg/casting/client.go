package casting

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client calls the standalone casting service.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient creates a casting client.
func NewClient(baseURL string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{Timeout: timeout}}
}

// ClientError is a non-2xx casting response.
type ClientError struct {
	Status  int
	Code    string
	Message string
	// SessionID identifies a projection session that the service created but
	// could not clean up. It is set on creation failures so callers can query
	// or retry stopping that session instead of leaking the device occupation.
	SessionID string
}

func (e *ClientError) Error() string {
	return fmt.Sprintf("casting: %d %s: %s", e.Status, e.Code, e.Message)
}

// IsStatus reports whether err is a casting HTTP error with status.
func IsStatus(err error, status int) bool {
	var target *ClientError
	return errors.As(err, &target) && target.Status == status
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
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			SessionID string `json:"session_id"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &payload)
	message := payload.Error.Message
	if message == "" {
		message = strings.TrimSpace(string(body))
	}
	if message == "" {
		message = response.Status
	}
	return &ClientError{Status: response.StatusCode, Code: payload.Error.Code, Message: message, SessionID: payload.Error.SessionID}
}

// ListTargets returns the casting targets currently known to the service.
func (c *Client) ListTargets(ctx context.Context) ([]Target, error) {
	var payload struct {
		Targets []Target `json:"targets"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/targets", nil, &payload); err != nil {
		return nil, err
	}
	return payload.Targets, nil
}

// GetTarget returns one casting target.
func (c *Client) GetTarget(ctx context.Context, id string) (Target, error) {
	var target Target
	err := c.do(ctx, http.MethodGet, "/v1/targets/"+url.PathEscape(id), nil, &target)
	return target, err
}

// CreateSession starts projection on the target.
func (c *Client) CreateSession(ctx context.Context, request CreateSessionRequest) (Session, error) {
	var session Session
	err := c.do(ctx, http.MethodPost, "/v1/projection-sessions", request, &session)
	return session, err
}

// GetSession refreshes one projection session.
func (c *Client) GetSession(ctx context.Context, id string) (Session, error) {
	var session Session
	err := c.do(ctx, http.MethodGet, "/v1/projection-sessions/"+url.PathEscape(id), nil, &session)
	return session, err
}

// StopSession stops projection and releases the target.
func (c *Client) StopSession(ctx context.Context, id string) (Session, error) {
	var session Session
	err := c.do(ctx, http.MethodDelete, "/v1/projection-sessions/"+url.PathEscape(id), nil, &session)
	return session, err
}

// Health checks the casting service.
func (c *Client) Health(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/healthz", nil, nil)
}
