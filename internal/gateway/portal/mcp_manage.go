package portal

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sort"

	"github.com/open-anywhere/open-anywhere/internal/gateway/linx"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

// settingMCPShared holds the IDs of the MCP services the administrator
// offers to users, as a JSON array.
const settingMCPShared = "mcp.shared_ids"

// MCPServiceView is one MCP service on the platform account.
type MCPServiceView struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Desc    string `json:"desc,omitempty"`
	LogoURL string `json:"logo_url,omitempty"`
	// Private is true for a service the platform account created itself.
	Private bool `json:"private"`
	// Required marks the gateway's own MCP service. Every agent keeps it.
	Required bool `json:"required"`
	// Shared marks a service the administrator offers to users.
	Shared bool `json:"shared"`
	// Selected is only set on a user's view of their own agent.
	Selected bool `json:"selected,omitempty"`
}

// OurMCPInstance returns the platform instance ID of the gateway's own MCP
// service, or "" before the first agent has been provisioned.
func (p *Provisioner) OurMCPInstance(ctx context.Context) (string, error) {
	_, instanceID, err := p.mcpIDs(ctx)
	return instanceID, err
}

// MCPInstances lists the MCP services on the platform account.
func (p *Provisioner) MCPInstances(ctx context.Context) ([]linx.MCPInstance, error) {
	client, err := p.client(ctx)
	if err != nil {
		return nil, err
	}
	return client.MCPInstances(ctx)
}

// AgentMCPs lists the MCP service IDs bound to an agent.
func (p *Provisioner) AgentMCPs(ctx context.Context, agentID string) ([]string, error) {
	client, err := p.client(ctx)
	if err != nil {
		return nil, err
	}
	return client.AgentMCPs(ctx, agentID)
}

// SetAgentMCPs binds exactly ids to the agent. The gateway's own service is
// always kept: without it the agent cannot make or control applications.
func (p *Provisioner) SetAgentMCPs(ctx context.Context, agentID string, ids []string) error {
	client, err := p.client(ctx)
	if err != nil {
		return err
	}
	ours, err := p.OurMCPInstance(ctx)
	if err != nil {
		return err
	}
	return client.AttachMCP(ctx, agentID, withRequired(ours, ids))
}

// SharedMCPs returns the IDs of the services offered to users.
func (p *Provisioner) SharedMCPs(ctx context.Context) ([]string, error) {
	raw, err := p.db.PortalSetting(ctx, settingMCPShared)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return nil, err
	}
	return ids, nil
}

// SetSharedMCPs stores the IDs of the services offered to users.
func (p *Provisioner) SetSharedMCPs(ctx context.Context, ids []string) error {
	if ids == nil {
		ids = []string{}
	}
	raw, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	return p.db.SetPortalSetting(ctx, settingMCPShared, string(raw))
}

// withRequired returns ids with required first, without duplicates or blanks.
func withRequired(required string, ids []string) []string {
	out := make([]string, 0, len(ids)+1)
	seen := map[string]bool{"": true}
	for _, id := range append([]string{required}, ids...) {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func contains(ids []string, id string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

func mcpView(instance linx.MCPInstance, ours string, shared []string) MCPServiceView {
	return MCPServiceView{
		ID: instance.ID, Name: instance.Name, Desc: instance.Desc, LogoURL: instance.LogoURL,
		Private:  instance.IsPrivate,
		Required: instance.ID == ours,
		Shared:   contains(shared, instance.ID),
	}
}

type mcpPlatform struct {
	instances []linx.MCPInstance
	ours      string
	shared    []string
}

func (a *API) loadMCPPlatform(ctx context.Context) (mcpPlatform, error) {
	var out mcpPlatform
	var err error
	if out.instances, err = a.provision.MCPInstances(ctx); err != nil {
		return out, err
	}
	if out.ours, err = a.provision.OurMCPInstance(ctx); err != nil {
		return out, err
	}
	out.shared, err = a.provision.SharedMCPs(ctx)
	return out, err
}

// adminGetMCP lists every MCP service on the platform account and marks the
// gateway's own and the ones offered to users.
func (a *API) adminGetMCP(w http.ResponseWriter, r *http.Request, _ store.PortalUser) {
	if a.provision == nil {
		writeError(w, apiError(http.StatusServiceUnavailable, "PROVISION_UNAVAILABLE", "开通组件暂不可用"))
		return
	}
	platform, err := a.loadMCPPlatform(r.Context())
	if err != nil {
		writeError(w, mapBindError(err))
		return
	}
	services := make([]MCPServiceView, 0, len(platform.instances))
	for _, instance := range platform.instances {
		services = append(services, mcpView(instance, platform.ours, platform.shared))
	}
	sort.SliceStable(services, func(i, j int) bool { return services[i].Required && !services[j].Required })
	writeJSON(w, http.StatusOK, map[string]any{
		"provisioned": platform.ours != "",
		"services":    services,
	})
}

type sharedMCPRequest struct {
	IDs []string `json:"ids"`
}

// adminSetSharedMCP chooses which MCP services users may add to their agents.
func (a *API) adminSetSharedMCP(w http.ResponseWriter, r *http.Request, _ store.PortalUser) {
	var in sharedMCPRequest
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, err)
		return
	}
	if a.provision == nil {
		writeError(w, apiError(http.StatusServiceUnavailable, "PROVISION_UNAVAILABLE", "开通组件暂不可用"))
		return
	}
	platform, err := a.loadMCPPlatform(r.Context())
	if err != nil {
		writeError(w, mapBindError(err))
		return
	}
	known := map[string]bool{}
	for _, instance := range platform.instances {
		known[instance.ID] = true
	}
	ids := make([]string, 0, len(in.IDs))
	seen := map[string]bool{}
	for _, id := range in.IDs {
		switch {
		case seen[id]:
		case id == platform.ours:
			writeError(w, invalidArgument("网关自己的 MCP 服务始终启用，不需要放进可选列表"))
			return
		case !known[id]:
			writeError(w, invalidArgument("灵矽账号下没有这个 MCP 服务"))
			return
		default:
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if err := a.provision.SetSharedMCPs(r.Context(), ids); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ids": ids})
}

// adminSyncMCP pushes the current MCP address and token to the gateway's own
// service on the platform.
func (a *API) adminSyncMCP(w http.ResponseWriter, r *http.Request, _ store.PortalUser) {
	if a.provision == nil {
		writeError(w, apiError(http.StatusServiceUnavailable, "PROVISION_UNAVAILABLE", "开通组件暂不可用"))
		return
	}
	if err := a.provision.SyncMCP(r.Context()); err != nil {
		a.logf("portal: sync MCP: %v", err)
		writeError(w, mapBindError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"synced": true})
}

// getDeviceMCPs shows a user the services their agent can use: the gateway's
// own one, which cannot be removed, and the ones the administrator offers.
func (a *API) getDeviceMCPs(w http.ResponseWriter, r *http.Request, user store.PortalUser) {
	device, err := a.agentDevice(r, user)
	if err != nil {
		writeError(w, err)
		return
	}
	if a.provision == nil {
		writeError(w, apiError(http.StatusServiceUnavailable, "PROVISION_UNAVAILABLE", "开通组件暂不可用"))
		return
	}
	platform, err := a.loadMCPPlatform(r.Context())
	if err != nil {
		writeError(w, mapBindError(err))
		return
	}
	bound, err := a.provision.AgentMCPs(r.Context(), device.LinxAgentID)
	if err != nil {
		writeError(w, mapBindError(err))
		return
	}
	var required *MCPServiceView
	available := []MCPServiceView{}
	for _, instance := range platform.instances {
		view := mcpView(instance, platform.ours, platform.shared)
		view.Selected = contains(bound, instance.ID)
		switch {
		case view.Required:
			required = &view
		case view.Shared:
			available = append(available, view)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"required": required, "available": available})
}

// setDeviceMCPs replaces the optional services on a user's agent. Only
// services the administrator offers can be chosen, and the gateway's own is
// always kept.
func (a *API) setDeviceMCPs(w http.ResponseWriter, r *http.Request, user store.PortalUser) {
	device, err := a.agentDevice(r, user)
	if err != nil {
		writeError(w, err)
		return
	}
	var in sharedMCPRequest
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, err)
		return
	}
	if a.provision == nil {
		writeError(w, apiError(http.StatusServiceUnavailable, "PROVISION_UNAVAILABLE", "开通组件暂不可用"))
		return
	}
	shared, err := a.provision.SharedMCPs(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	for _, id := range in.IDs {
		if !contains(shared, id) {
			writeError(w, invalidArgument("这个 MCP 服务没有开放给用户选择"))
			return
		}
	}
	// Without the gateway's own service on record, binding would drop it from
	// the agent, which could then no longer make or control applications.
	if ours, err := a.provision.OurMCPInstance(r.Context()); err != nil || ours == "" {
		writeError(w, apiError(http.StatusConflict, "MCP_NOT_READY", "平台的 MCP 服务还没有准备好，请稍后再试"))
		return
	}
	if err := a.provision.SetAgentMCPs(r.Context(), device.LinxAgentID, in.IDs); err != nil {
		a.logf("portal: set agent MCPs %s: %v", device.LinxAgentID, err)
		writeError(w, mapBindError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ids": in.IDs})
}
