package portal

import (
	"context"
	"net/http"
	"strconv"

	"github.com/open-anywhere/open-anywhere/internal/gateway/linx"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

const (
	defaultChatHistory = 30
	maxChatHistory     = 100
)

// ChatLineView is one saved chat line, oldest first in a response.
type ChatLineView struct {
	// Role is "user" for what was said to the agent and "agent" for its reply.
	Role string `json:"role"`
	Text string `json:"text"`
	At   string `json:"at"`
}

// ChatHistory returns the most recent saved chat lines of an agent.
func (p *Provisioner) ChatHistory(ctx context.Context, agentID string, limit int) ([]linx.ChatRecord, error) {
	client, err := p.client(ctx)
	if err != nil {
		return nil, err
	}
	return client.ChatHistory(ctx, agentID, limit)
}

// getChatHistory lets a user read back what was saved from their agent's
// chats. It is empty while the agent's chat saving is off.
func (a *API) getChatHistory(w http.ResponseWriter, r *http.Request, user store.PortalUser) {
	device, err := a.agentDevice(r, user)
	if err != nil {
		writeError(w, err)
		return
	}
	if a.provision == nil {
		writeError(w, apiError(http.StatusServiceUnavailable, "PROVISION_UNAVAILABLE", "开通组件暂不可用"))
		return
	}
	limit := defaultChatHistory
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxChatHistory {
			writeError(w, invalidArgument("limit 需要在 1 到 100 之间"))
			return
		}
		limit = n
	}
	records, err := a.provision.ChatHistory(r.Context(), device.LinxAgentID, limit)
	if err != nil {
		a.logf("portal: chat history %s: %v", device.LinxAgentID, err)
		writeError(w, mapBindError(err))
		return
	}
	// The platform lists newest first; the page reads oldest first. Tool calls
	// and other line kinds are not conversation, so they are left out.
	lines := make([]ChatLineView, 0, len(records))
	for i := len(records) - 1; i >= 0; i-- {
		record := records[i]
		role := ""
		switch record.ChatType {
		case 1:
			role = "user"
		case 2:
			role = "agent"
		}
		if role == "" || record.Content == "" {
			continue
		}
		lines = append(lines, ChatLineView{Role: role, Text: record.Content, At: record.CreatedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"records": lines})
}
