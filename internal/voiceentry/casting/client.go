// Package casting exposes the casting service running next to the device to
// Linx, so a spoken turn can put an application's stream on a nearby screen.
//
// The control plane builds and runs the application in the cloud and hands the
// stream address to Linx; this package only talks to the local casting service,
// which owns discovery and playback control. Nothing here reaches the cloud, so
// there is no identity header, token or Linx session to carry: the casting
// service is expected to be on the same trusted network as the device.
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

	api "github.com/open-anywhere/open-anywhere/pkg/casting"
)

// Config points at the local casting service.
type Config struct {
	// URL is the casting service base address, for example http://192.168.1.20:8091.
	URL string
	// CallTimeout bounds one projection request, which waits for the TV to accept.
	CallTimeout time.Duration
	// ListTimeout bounds discovery, which should answer quickly or not at all.
	ListTimeout time.Duration
	// HTTPClient overrides the transport; tests use it.
	HTTPClient *http.Client
}

func (c *Config) applyDefaults() {
	if c.CallTimeout <= 0 {
		c.CallTimeout = 15 * time.Second
	}
	if c.ListTimeout <= 0 {
		c.ListTimeout = 3 * time.Second
	}
}

// Client is safe for concurrent use.
type Client struct {
	cfg  Config
	http *http.Client
}

// New returns a client. It does not connect until a call is made.
func New(cfg Config) *Client {
	cfg.applyDefaults()
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Client{cfg: cfg, http: httpClient}
}

// APIError is an error the casting service reported for a well-formed request.
type APIError struct {
	Status    int
	Code      string
	Message   string
	SessionID string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

// ListTargets returns the screens the casting service currently discovers.
func (c *Client) ListTargets(ctx context.Context) ([]api.Target, error) {
	var out struct {
		Targets []api.Target `json:"targets"`
	}
	if err := c.do(ctx, c.cfg.ListTimeout, http.MethodGet, "/v1/targets", nil, &out); err != nil {
		return nil, err
	}
	return out.Targets, nil
}

// GetTarget returns one discovered screen, including its endpoints.
func (c *Client) GetTarget(ctx context.Context, targetID string) (api.Target, error) {
	var out api.Target
	path := "/v1/targets/" + url.PathEscape(targetID)
	if err := c.do(ctx, c.cfg.ListTimeout, http.MethodGet, path, nil, &out); err != nil {
		return api.Target{}, err
	}
	return out, nil
}

// CreateSession asks the casting service to play source on targetID.
func (c *Client) CreateSession(ctx context.Context, request api.CreateSessionRequest) (api.Session, error) {
	var out api.Session
	if err := c.do(ctx, c.cfg.CallTimeout, http.MethodPost, "/v1/projection-sessions", request, &out); err != nil {
		return api.Session{}, err
	}
	return out, nil
}

// StopSession stops a session. Stopping a session that is already gone returns
// a 404 APIError with CodeNotFound; callers may treat that as success.
func (c *Client) StopSession(ctx context.Context, sessionID string) (api.Session, error) {
	var out api.Session
	path := "/v1/projection-sessions/" + url.PathEscape(sessionID)
	if err := c.do(ctx, c.cfg.CallTimeout, http.MethodDelete, path, nil, &out); err != nil {
		return api.Session{}, err
	}
	return out, nil
}

func (c *Client) do(ctx context.Context, timeout time.Duration, method, path string, body, out any) error {
	if strings.TrimSpace(c.cfg.URL) == "" {
		return errors.New("未配置本地投屏服务地址")
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("投屏请求无法序列化: %w", err)
		}
		reader = bytes.NewReader(raw)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	endpoint := strings.TrimRight(c.cfg.URL, "/") + path
	request, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return fmt.Errorf("投屏服务地址不合法: %w", err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("本地投屏服务超过 %s 没有响应", timeout)
		}
		return fmt.Errorf("连不上本地投屏服务（%s）: %w", c.cfg.URL, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("读取投屏服务响应失败: %w", err)
	}
	if response.StatusCode >= 300 {
		return decodeAPIError(response.StatusCode, raw)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("投屏服务返回无法解析: %w", err)
	}
	return nil
}

func decodeAPIError(status int, raw []byte) error {
	failure := &APIError{Status: status}
	var envelope struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			SessionID string `json:"session_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil && (envelope.Error.Code != "" || envelope.Error.Message != "") {
		failure.Code = envelope.Error.Code
		failure.Message = envelope.Error.Message
		failure.SessionID = envelope.Error.SessionID
		return failure
	}
	failure.Message = strings.TrimSpace(string(raw))
	if failure.Message == "" {
		failure.Message = http.StatusText(status)
	}
	return failure
}

// Error codes the casting service reports.
const (
	CodeInvalid              = "invalid_request"
	CodeNotFound             = "not_found"
	CodeTargetBusy           = "target_busy"
	CodeTargetOffline        = "target_offline"
	CodeUnsupportedMedia     = "unsupported_media"
	CodeDeviceControlFailure = "device_control_failed"
)
