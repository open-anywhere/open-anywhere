// Package downstream implements the documented Render and Casting HTTP contracts.
package downstream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/open-anywhere/open-anywhere/gateway/internal/application"
	"github.com/open-anywhere/open-anywhere/pkg/casting"
)

type Client struct {
	Base  string
	Token string
	HTTP  *http.Client
}

func New(base, token string) (Client, error) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return Client{}, fmt.Errorf("invalid downstream URL")
	}
	return Client{strings.TrimRight(base, "/"), token, &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c Client) Request(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("downstream request failed: %w", err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil {
		return err
	}
	if len(b) > 1<<20 {
		return fmt.Errorf("downstream response too large")
	}
	if out != nil && len(b) > 0 {
		if err = json.Unmarshal(b, out); err != nil {
			return fmt.Errorf("invalid downstream JSON")
		}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		var detail struct {
			Error application.Failure `json:"error"`
		}
		_ = json.Unmarshal(b, &detail)
		switch detail.Error.Code {
		case "INVALID_REQUEST", "PORT_IN_USE", "APP_ALREADY_RUNNING", "TARGET_BUSY", "TARGET_NOT_FOUND":
			return &application.Rejected{Code: detail.Error.Code, Message: detail.Error.Message}
		}
		return fmt.Errorf("downstream HTTP %d: %s", res.StatusCode, detail.Error.Code)
	}
	return nil
}

type Render struct{ Client }

func (r Render) PortFree(ctx context.Context, port int) (bool, error) {
	var out struct {
		Occupied *bool `json:"occupied"`
	}
	err := r.Request(ctx, "GET", "/v1/ports/"+strconv.Itoa(port), nil, &out)
	if err != nil {
		return false, err
	}
	if out.Occupied == nil {
		return false, fmt.Errorf("missing occupied")
	}
	return !*out.Occupied, nil
}
func (r Render) Start(ctx context.Context, id, artifact string, port int) (application.Instance, error) {
	var out application.Instance
	err := r.Request(ctx, "POST", "/v1/apps", map[string]any{"app_id": id, "artifact_url": artifact, "port": port}, &out)
	return out, err
}
func (r Render) Get(ctx context.Context, id string) (application.Instance, error) {
	var out application.Instance
	err := r.Request(ctx, "GET", "/v1/apps/"+url.PathEscape(id), nil, &out)
	return out, err
}
func (r Render) Stop(ctx context.Context, id string) (application.Instance, error) {
	var out application.Instance
	err := r.Request(ctx, "DELETE", "/v1/apps/"+url.PathEscape(id), nil, &out)
	return out, err
}

type Casting struct{ Client }

func (c Casting) Targets(ctx context.Context) ([]casting.Target, error) {
	var out struct {
		Targets []casting.Target `json:"targets"`
	}
	err := c.Request(ctx, "GET", "/v1/targets", nil, &out)
	return out.Targets, err
}
func (c Casting) Start(ctx context.Context, in casting.CreateSessionRequest) (casting.Session, error) {
	var out casting.Session
	err := c.Request(ctx, "POST", "/v1/projection-sessions", in, &out)
	return out, err
}
func (c Casting) Get(ctx context.Context, id string) (casting.Session, error) {
	var out casting.Session
	err := c.Request(ctx, "GET", "/v1/projection-sessions/"+url.PathEscape(id), nil, &out)
	return out, err
}
func (c Casting) Stop(ctx context.Context, id string) (casting.Session, error) {
	var out casting.Session
	err := c.Request(ctx, "DELETE", "/v1/projection-sessions/"+url.PathEscape(id), nil, &out)
	return out, err
}
