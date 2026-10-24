// Package register walks a virtual device through Linx OTA activation.
//
// A fresh identity is reported to OTA, which answers with an activation code.
// The code is bound to an agent either in the Linx console or, when an agent
// id and API key are supplied, by this package. Once the platform issues a
// token the credentials are stored and the caller is asked to connect.
package register

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/voiceentry/linx"
)

// Request optionally carries what is needed to bind the activation code
// automatically; leave both empty to bind in the Linx console instead.
type Request struct {
	AgentID string `json:"agent_id"`
	APIKey  string `json:"api_key"`
}

// Result reports how far registration got: "pending" with an activation
// code, or "registered".
type Result struct {
	Status string `json:"status"`
	MAC    string `json:"mac,omitempty"`
	Code   string `json:"code,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Config wires a Registrar to its environment.
type Config struct {
	// Path is where the credential file is written once registration succeeds.
	Path string
	// OTA talks to the platform; the zero value uses the production endpoints.
	OTA linx.OTAClient
	// Device holds the active credentials; Register stores into it on success.
	Device *atomic.Pointer[linx.Device]
	// Poll is the wait between OTA checks after binding. Defaults to 2s.
	Poll time.Duration
	// Apply connects with the stored credentials after a successful registration.
	Apply func() error
}

// Registrar drives registration. It is safe for concurrent use.
type Registrar struct {
	cfg     Config
	mu      sync.Mutex
	pending *linx.Device
	code    string
}

// New returns a Registrar.
func New(cfg Config) *Registrar {
	if cfg.Poll <= 0 {
		cfg.Poll = 2 * time.Second
	}
	return &Registrar{cfg: cfg}
}

// Registered reports whether credentials are already available.
func (r *Registrar) Registered() bool { return r.cfg.Device.Load() != nil }

// ActivationCode returns the code of the in-progress registration, if any.
func (r *Registrar) ActivationCode() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.code
}

// Register advances registration by one step and is safe to call repeatedly:
// the same identity and activation code are reused until the device is bound.
func (r *Registrar) Register(ctx context.Context, in Request) (Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d := r.cfg.Device.Load(); d != nil {
		return Result{Status: "registered", MAC: d.MAC}, nil
	}
	in.AgentID, in.APIKey = strings.TrimSpace(in.AgentID), strings.TrimSpace(in.APIKey)
	if (in.AgentID == "") != (in.APIKey == "") {
		return Result{}, errors.New("自动绑定需要同时填写 Agent ID 和 API Key")
	}
	if r.pending == nil {
		id, err := linx.NewIdentity()
		if err != nil {
			return Result{}, err
		}
		r.pending = &id
	}
	res, err := r.cfg.OTA.Report(ctx, *r.pending)
	if err != nil {
		return Result{}, err
	}
	if !hasToken(res) && res.Activation != nil && res.Activation.Code != "" {
		r.code = res.Activation.Code
		if in.APIKey != "" {
			if err := r.cfg.OTA.Bind(ctx, in.APIKey, in.AgentID, r.code); err != nil {
				return Result{Status: "pending", MAC: r.pending.MAC, Code: r.code}, err
			}
			if res, err = r.waitForToken(ctx, res); err != nil {
				return Result{}, err
			}
		}
	}
	if !hasToken(res) {
		if r.code == "" {
			return Result{}, errors.New("平台没有返回激活码或凭证")
		}
		return Result{Status: "pending", MAC: r.pending.MAC, Code: r.code}, nil
	}
	d := *r.pending
	d.Token, d.WSURL = res.WebSocket.Token, res.WebSocket.URL
	if err := linx.SaveDevice(r.cfg.Path, d); err != nil {
		return Result{}, fmt.Errorf("保存凭证失败: %w", err)
	}
	r.cfg.Device.Store(&d)
	r.pending, r.code = nil, ""
	out := Result{Status: "registered", MAC: d.MAC}
	if r.cfg.Apply != nil {
		if err := r.cfg.Apply(); err != nil {
			return out, fmt.Errorf("已注册但连接失败，可点“一键断开重连”: %w", err)
		}
	}
	return out, nil
}

// waitForToken re-reports until the platform hands out a token after binding.
func (r *Registrar) waitForToken(ctx context.Context, res linx.OTAResponse) (linx.OTAResponse, error) {
	for i := 0; i < 5 && !hasToken(res); i++ {
		select {
		case <-ctx.Done():
			return res, ctx.Err()
		case <-time.After(r.cfg.Poll):
		}
		var err error
		if res, err = r.cfg.OTA.Report(ctx, *r.pending); err != nil {
			return res, err
		}
	}
	return res, nil
}

func hasToken(res linx.OTAResponse) bool {
	return res.WebSocket != nil && res.WebSocket.Token != "" && res.WebSocket.URL != ""
}
