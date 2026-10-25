package portal

import (
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

const maxAssistantNameRunes = 20

// VoiceView is one voice a user can choose for their agent.
type VoiceView struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Demo   string   `json:"demo,omitempty"`
	Gender string   `json:"gender,omitempty"`
	Age    string   `json:"age,omitempty"`
	Styles []string `json:"styles,omitempty"`
}

// AgentSettingsView is what a user may change on their Linx agent. The system
// prompt and models stay with the platform: the prompt tells the agent how to
// drive the application MCP, so editing it could silently break the product.
type AgentSettingsView struct {
	AssistantName string `json:"assistant_name"`
	TTSVoiceID    string `json:"tts_voice_id"`
}

type updateAgentRequest struct {
	AssistantName *string `json:"assistant_name"`
	TTSVoiceID    *string `json:"tts_voice_id"`
}

// agentDevice loads an owned device that already has a Linx agent.
func (a *API) agentDevice(r *http.Request, user store.PortalUser) (store.PortalDevice, error) {
	device, err := a.ownedDevice(r, user, r.PathValue("id"))
	if err != nil {
		return store.PortalDevice{}, err
	}
	if device.LinxAgentID == "" {
		return store.PortalDevice{}, apiError(http.StatusConflict, "AGENT_NOT_READY", "智能体还没有开通完成")
	}
	return device, nil
}

func (a *API) listVoices(w http.ResponseWriter, r *http.Request, _ store.PortalUser) {
	if a.provision == nil {
		writeError(w, apiError(http.StatusServiceUnavailable, "PROVISION_UNAVAILABLE", "开通组件暂不可用"))
		return
	}
	voices, err := a.provision.Voices(r.Context())
	if err != nil {
		writeError(w, mapBindError(err))
		return
	}
	out := make([]VoiceView, 0, len(voices))
	for _, voice := range voices {
		out = append(out, VoiceView{
			ID: voice.ID, Name: voice.Name, Demo: voice.Demo,
			Gender: voice.Tags.Gender, Age: voice.Tags.Age, Styles: voice.Tags.Styles,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"voices": out})
}

func (a *API) getDeviceAgent(w http.ResponseWriter, r *http.Request, user store.PortalUser) {
	device, err := a.agentDevice(r, user)
	if err != nil {
		writeError(w, err)
		return
	}
	if a.provision == nil {
		writeError(w, apiError(http.StatusServiceUnavailable, "PROVISION_UNAVAILABLE", "开通组件暂不可用"))
		return
	}
	info, err := a.provision.AgentInfo(r.Context(), device.LinxAgentID)
	if err != nil {
		writeError(w, mapBindError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"agent": AgentSettingsView{AssistantName: info.AssistantName, TTSVoiceID: info.TTSVoiceID},
	})
}

func (a *API) updateDeviceAgent(w http.ResponseWriter, r *http.Request, user store.PortalUser) {
	device, err := a.agentDevice(r, user)
	if err != nil {
		writeError(w, err)
		return
	}
	var in updateAgentRequest
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, err)
		return
	}
	if in.AssistantName == nil && in.TTSVoiceID == nil {
		writeError(w, invalidArgument("没有需要修改的内容"))
		return
	}
	if a.provision == nil {
		writeError(w, apiError(http.StatusServiceUnavailable, "PROVISION_UNAVAILABLE", "开通组件暂不可用"))
		return
	}
	if in.AssistantName != nil {
		name := strings.TrimSpace(*in.AssistantName)
		if name == "" || utf8.RuneCountInString(name) > maxAssistantNameRunes {
			writeError(w, invalidArgument("助手名称需要 1 到 20 个字符"))
			return
		}
		for _, r := range name {
			if unicode.IsControl(r) {
				writeError(w, invalidArgument("助手名称不能包含控制字符"))
				return
			}
		}
		in.AssistantName = &name
	}
	if in.TTSVoiceID != nil {
		id := strings.TrimSpace(*in.TTSVoiceID)
		// Only voices the platform lists are accepted, so a user cannot point
		// their agent at an arbitrary identifier.
		voices, err := a.provision.Voices(r.Context())
		if err != nil {
			writeError(w, mapBindError(err))
			return
		}
		found := false
		for _, voice := range voices {
			if voice.ID == id {
				found = true
				break
			}
		}
		if !found {
			writeError(w, invalidArgument("没有这个音色"))
			return
		}
		in.TTSVoiceID = &id
	}
	if err := a.provision.UpdateAgent(r.Context(), device.LinxAgentID, in.AssistantName, in.TTSVoiceID); err != nil {
		a.logf("portal: update agent %s: %v", device.LinxAgentID, err)
		writeError(w, mapBindError(err))
		return
	}
	info, err := a.provision.AgentInfo(r.Context(), device.LinxAgentID)
	if err != nil {
		// The change went through; only the read-back failed.
		writeJSON(w, http.StatusOK, map[string]any{"agent": AgentSettingsView{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"agent": AgentSettingsView{AssistantName: info.AssistantName, TTSVoiceID: info.TTSVoiceID},
	})
}
