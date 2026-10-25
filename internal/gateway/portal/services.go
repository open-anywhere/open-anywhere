package portal

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/endpoints"
	"github.com/open-anywhere/open-anywhere/internal/gateway/netguard"
	"github.com/open-anywhere/open-anywhere/internal/gateway/service"
	"github.com/open-anywhere/open-anywhere/internal/gateway/settings"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

// servicesRequest is the body of a binding save or connection test. Empty
// token fields keep the stored secret; the clear flags remove it.
type servicesRequest struct {
	Mode              string `json:"mode"`
	RenderURL         string `json:"render_url"`
	CastingURL        string `json:"casting_url"`
	RenderToken       string `json:"render_token"`
	CastingToken      string `json:"casting_token"`
	ClearRenderToken  bool   `json:"clear_render_token"`
	ClearCastingToken bool   `json:"clear_casting_token"`
}

func (in servicesRequest) override() endpoints.BindingOverride {
	return endpoints.BindingOverride{
		Mode:              in.Mode,
		RenderURL:         in.RenderURL,
		CastingURL:        in.CastingURL,
		RenderToken:       in.RenderToken,
		CastingToken:      in.CastingToken,
		ClearRenderToken:  in.ClearRenderToken,
		ClearCastingToken: in.ClearCastingToken,
	}
}

func (a *API) getServices(w http.ResponseWriter, r *http.Request, user store.PortalUser) {
	view, err := a.serviceBindingView(r.Context(), user.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (a *API) updateServices(w http.ResponseWriter, r *http.Request, user store.PortalUser) {
	if a.resolver == nil {
		writeError(w, apiError(http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "服务绑定暂不可用"))
		return
	}
	var in servicesRequest
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, err)
		return
	}
	if in.Mode != "" && in.Mode != store.ServiceModeManaged && in.Mode != store.ServiceModeCustom {
		writeError(w, apiError(http.StatusBadRequest, "INVALID_ARGUMENT", "服务模式无效"))
		return
	}
	identities, err := a.ownedIdentities(r.Context(), user.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	if a.apps != nil && a.apps.HasActiveRuntime(ownerIdentities(identities)) {
		writeError(w, apiError(http.StatusConflict, "APPS_RUNNING",
			"有应用正在运行，请先在屏幕页停止后再修改服务"))
		return
	}
	if _, err := a.resolver.SaveBinding(r.Context(), user.ID, in.override()); err != nil {
		writeError(w, mapBindingError(err))
		return
	}
	view, err := a.serviceBindingView(r.Context(), user.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	a.logf("portal: user %s updated the service binding", user.ID)
	writeJSON(w, http.StatusOK, view)
}

type serviceCheck struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

func (a *API) testServices(w http.ResponseWriter, r *http.Request, user store.PortalUser) {
	if wait, allowed := a.serviceTest.allow(user.ID); !allowed {
		writeError(w, apiError(http.StatusTooManyRequests, "SERVICE_TEST_RATE_LIMITED",
			"测试过于频繁，请在 "+secondsText(wait)+" 秒后重试"))
		return
	}
	if a.resolver == nil || a.pool == nil {
		writeError(w, apiError(http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "服务测试暂不可用"))
		return
	}
	var in servicesRequest
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, err)
		return
	}
	binding, err := a.resolver.PreviewBinding(r.Context(), user.ID, in.override())
	if err != nil {
		writeError(w, mapBindingError(err))
		return
	}
	if !binding.Managed {
		if err := a.resolver.CheckURL(r.Context(), binding.RenderURL); err != nil {
			writeError(w, mapURLPolicyError(err))
			return
		}
		if err := a.resolver.CheckURL(r.Context(), binding.CastingURL); err != nil {
			writeError(w, mapURLPolicyError(err))
			return
		}
	}
	render := a.checkRender(r.Context(), binding)
	casting, screens := a.checkCasting(r.Context(), binding)
	writeJSON(w, http.StatusOK, map[string]any{
		"render":  render,
		"casting": casting,
		"screens": screens,
	})
}

func (a *API) serviceBindingView(ctx context.Context, userID string) (map[string]any, error) {
	saved, err := a.db.ServiceBindingByUser(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		saved = store.PortalServiceBinding{UserID: userID, Mode: store.ServiceModeManaged}
	} else if err != nil {
		return nil, err
	}
	mode := saved.Mode
	if mode == "" {
		mode = store.ServiceModeManaged
	}
	var snapshot settings.Snapshot
	if a.settings != nil {
		if snapshot, err = a.settings.Snapshot(ctx); err != nil {
			return nil, err
		}
	}
	ready := false
	if a.resolver != nil {
		binding, err := a.resolver.BindingForUser(ctx, userID)
		switch {
		case err == nil:
			ready = binding.RenderURL != "" && binding.CastingURL != ""
		case toolCode(err) == "SERVICE_NOT_CONFIGURED":
			// The user has not finished configuring services. Other failures
			// (for example an undecryptable token) must not look like "unset".
			ready = false
		default:
			return nil, err
		}
	}
	return map[string]any{
		"mode":  mode,
		"ready": ready,
		"managed": map[string]any{
			"available":   snapshot.Services.Ready(),
			"render_url":  snapshot.Services.RenderURL,
			"casting_url": snapshot.Services.CastingURL,
		},
		"custom": map[string]any{
			"render_url":               saved.RenderURL,
			"casting_url":              saved.CastingURL,
			"render_token_configured":  saved.RenderTokenEnc != "",
			"casting_token_configured": saved.CastingTokenEnc != "",
		},
	}, nil
}

type healthChecker interface {
	Health(context.Context) error
}

func (a *API) checkRender(ctx context.Context, binding service.Binding) serviceCheck {
	client := a.pool.Render(binding)
	checker, ok := client.(healthChecker)
	if !ok {
		return serviceCheck{Message: "渲染服务不可用"}
	}
	probe, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := checker.Health(probe); err != nil {
		return serviceCheck{Message: "无法连接渲染服务"}
	}
	return serviceCheck{OK: true}
}

func (a *API) checkCasting(ctx context.Context, binding service.Binding) (serviceCheck, int) {
	client := a.pool.Casting(binding)
	checker, ok := client.(healthChecker)
	if !ok {
		return serviceCheck{Message: "投屏服务不可用"}, 0
	}
	probe, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := checker.Health(probe); err != nil {
		return serviceCheck{Message: "无法连接投屏服务"}, 0
	}
	listCtx, cancelList := context.WithTimeout(ctx, 10*time.Second)
	defer cancelList()
	targets, err := client.ListTargets(listCtx)
	if err != nil {
		return serviceCheck{Message: "无法获取投屏服务的屏幕列表"}, 0
	}
	return serviceCheck{OK: true}, len(targets)
}

func mapURLPolicyError(err error) error {
	switch netguard.CodeOf(err) {
	case netguard.CodePrivateTarget:
		return apiError(http.StatusBadRequest, netguard.CodePrivateTarget, "服务地址不能指向内网或本机")
	case netguard.CodeInvalidURL:
		return apiError(http.StatusBadRequest, netguard.CodeInvalidURL, "服务地址无效")
	case netguard.CodeTargetUnreach:
		return apiError(http.StatusBadGateway, netguard.CodeTargetUnreach, "无法解析服务地址")
	}
	return err
}

func mapBindingError(err error) error {
	if code := netguard.CodeOf(err); code != "" {
		return mapURLPolicyError(err)
	}
	switch toolCode(err) {
	case "SERVICE_NOT_CONFIGURED":
		return apiError(http.StatusConflict, "SERVICE_NOT_CONFIGURED", "请先填写渲染和投屏服务地址")
	case "INVALID_ARGUMENT":
		return apiError(http.StatusBadRequest, "INVALID_ARGUMENT", "服务模式无效")
	case "OWNER_NOT_FOUND":
		return apiError(http.StatusNotFound, "OWNER_NOT_FOUND", "无法识别智能体")
	}
	return err
}

func secondsText(wait time.Duration) string {
	seconds := int(wait.Seconds() + 0.5)
	if seconds < 1 {
		seconds = 1
	}
	return strconv.Itoa(seconds)
}
