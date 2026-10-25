package portal

import (
	"context"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/open-anywhere/open-anywhere/internal/gateway/linx"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

const (
	maxAssistantNameRunes = 20
	// Linx accepts role descriptions up to this many characters.
	maxSystemPromptRunes = 6000
)

// Ranges follow the Linx console. Linx clamps most of them itself, but a
// request that is out of range is a mistake worth reporting to the user.
const (
	minTemperature = 0.0
	maxTemperature = 1.9
	minMaxTokens   = 1
	maxMaxTokens   = 8192
	minSpeed       = 0.5
	maxSpeed       = 2.0
	minVolume      = 0
	maxVolume      = 100
	minIdleSeconds = 60
	maxIdleSeconds = 3600
)

// memoryModes are the memory settings offered to users. Other modes an
// existing agent may carry are kept as they are until the user changes them.
var memoryModes = []OptionView{
	{ID: "Memory_nomem", Name: "无记忆"},
	{ID: "Memory_long_term_memory", Name: "长期记忆"},
}

// OptionView is one choice in a select.
type OptionView struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Comment string `json:"comment,omitempty"`
}

// VoiceView is one voice a user can choose for their agent.
type VoiceView struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Demo   string   `json:"demo,omitempty"`
	Gender string   `json:"gender,omitempty"`
	Age    string   `json:"age,omitempty"`
	Styles []string `json:"styles,omitempty"`
	// Languages are the language codes the voice can speak; Tier is "free" or "pro".
	Languages []string `json:"languages,omitempty"`
	Tier      string   `json:"tier,omitempty"`
}

// AgentOptionsView lists everything a user can pick from.
type AgentOptionsView struct {
	Voices      []VoiceView  `json:"voices"`
	Languages   []OptionView `json:"languages"`
	LLMModels   []OptionView `json:"llm_models"`
	MemoryModes []OptionView `json:"memory_modes"`
	// DefaultSystemPrompt is what new agents start with, so a user can go back to it.
	DefaultSystemPrompt string `json:"default_system_prompt"`
}

// AgentSettingsView is what a user may change on their Linx agent. The system
// prompt and the intent model stay with the platform: the prompt tells the
// agent how to drive the application MCP and the intent model decides whether
// it may call tools, so editing either could silently break the product.
// Numeric fields are omitted when the platform has no value for them.
type AgentSettingsView struct {
	AssistantName      string   `json:"assistant_name"`
	TTSVoiceID         string   `json:"tts_voice_id"`
	LangCode           string   `json:"lang_code"`
	LLMModelID         string   `json:"llm_model_id"`
	MemModelID         string   `json:"mem_model_id"`
	VoiceVolume        *int     `json:"voice_volume,omitempty"`
	VoiceSpeed         *float64 `json:"voice_speed,omitempty"`
	LLMTemperature     *float64 `json:"llm_temperature,omitempty"`
	LLMMaxTokens       *int     `json:"llm_max_tokens,omitempty"`
	IdleTimeoutSeconds *int     `json:"idle_timeout_seconds,omitempty"`
	DisableGreeting    *bool    `json:"disable_greeting,omitempty"`
	// SystemPrompt is the full role description of the agent.
	SystemPrompt string `json:"system_prompt"`
	// ChatHistory is 0 for no chat log, 1 for text only, 2 for text and audio.
	ChatHistory *int `json:"chat_history,omitempty"`
}

type updateAgentRequest struct {
	AssistantName      *string  `json:"assistant_name"`
	TTSVoiceID         *string  `json:"tts_voice_id"`
	LangCode           *string  `json:"lang_code"`
	LLMModelID         *string  `json:"llm_model_id"`
	MemModelID         *string  `json:"mem_model_id"`
	VoiceVolume        *int     `json:"voice_volume"`
	VoiceSpeed         *float64 `json:"voice_speed"`
	LLMTemperature     *float64 `json:"llm_temperature"`
	LLMMaxTokens       *int     `json:"llm_max_tokens"`
	IdleTimeoutSeconds *int     `json:"idle_timeout_seconds"`
	DisableGreeting    *bool    `json:"disable_greeting"`
	SystemPrompt       *string  `json:"system_prompt"`
	ChatHistory        *int     `json:"chat_history"`
}

func agentSettingsView(info linx.AgentInfo) AgentSettingsView {
	return AgentSettingsView{
		AssistantName:      info.AssistantName,
		TTSVoiceID:         info.TTSVoiceID,
		LangCode:           info.LangCode,
		LLMModelID:         info.LLMModelID,
		MemModelID:         info.MemModelID,
		VoiceVolume:        info.Extra.Voice.Volume,
		VoiceSpeed:         info.Extra.Voice.Speed,
		LLMTemperature:     info.Extra.LLM.Temperature,
		LLMMaxTokens:       info.Extra.LLM.MaxTokens,
		IdleTimeoutSeconds: info.Extra.Goodbye.IdleTimeoutSeconds,
		DisableGreeting:    info.Extra.DisableGreeting,
		SystemPrompt:       info.SystemPrompt,
		ChatHistory:        info.ChatHistoryConf,
	}
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

func (a *API) loadVoices(ctx context.Context) ([]VoiceView, error) {
	voices, err := a.provision.Voices(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]VoiceView, 0, len(voices))
	for _, voice := range voices {
		out = append(out, VoiceView{
			ID: voice.ID, Name: voice.Name, Demo: voice.Demo,
			Gender: voice.Tags.Gender, Age: voice.Tags.Age, Styles: voice.Tags.Styles,
			Languages: voice.Tags.Languages, Tier: voice.Tier,
		})
	}
	return out, nil
}

func (a *API) loadLanguages(ctx context.Context) ([]OptionView, error) {
	languages, err := a.provision.Languages(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]OptionView, 0, len(languages))
	for _, language := range languages {
		out = append(out, OptionView{ID: language.Code, Name: language.Name})
	}
	return out, nil
}

func (a *API) loadLLMModels(ctx context.Context) ([]OptionView, error) {
	models, err := a.provision.LLMModels(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]OptionView, 0, len(models))
	for _, model := range models {
		out = append(out, OptionView{ID: model.ID, Name: model.Name, Comment: model.Comment})
	}
	return out, nil
}

func (a *API) listAgentOptions(w http.ResponseWriter, r *http.Request, _ store.PortalUser) {
	if a.provision == nil {
		writeError(w, apiError(http.StatusServiceUnavailable, "PROVISION_UNAVAILABLE", "开通组件暂不可用"))
		return
	}
	var (
		options AgentOptionsView
		err     error
	)
	options.MemoryModes = memoryModes
	options.DefaultSystemPrompt = a.provision.DefaultSystemPrompt()
	if options.Voices, err = a.loadVoices(r.Context()); err != nil {
		writeError(w, mapBindError(err))
		return
	}
	if options.Languages, err = a.loadLanguages(r.Context()); err != nil {
		writeError(w, mapBindError(err))
		return
	}
	if options.LLMModels, err = a.loadLLMModels(r.Context()); err != nil {
		writeError(w, mapBindError(err))
		return
	}
	writeJSON(w, http.StatusOK, options)
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
	writeJSON(w, http.StatusOK, map[string]any{"agent": agentSettingsView(info)})
}

func hasChange(in updateAgentRequest) bool {
	return in.AssistantName != nil || in.TTSVoiceID != nil || in.LangCode != nil ||
		in.LLMModelID != nil || in.MemModelID != nil || in.VoiceVolume != nil ||
		in.VoiceSpeed != nil || in.LLMTemperature != nil || in.LLMMaxTokens != nil ||
		in.IdleTimeoutSeconds != nil || in.DisableGreeting != nil || in.SystemPrompt != nil || in.ChatHistory != nil
}

// inList reports whether id is one of options. Only values the platform
// lists are accepted, so a user cannot point their agent at an arbitrary
// identifier.
func inList(options []OptionView, id string) bool {
	for _, option := range options {
		if option.ID == id {
			return true
		}
	}
	return false
}

// validateSystemPrompt checks a full role description before it reaches the
// platform. Newlines and tabs are normal in a prompt; other control
// characters are not.
func validateSystemPrompt(prompt string) (string, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return "", invalidArgument("角色介绍不能为空")
	}
	if utf8.RuneCountInString(prompt) > maxSystemPromptRunes {
		return "", invalidArgument("角色介绍最长 6000 个字符")
	}
	for _, r := range prompt {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return "", invalidArgument("角色介绍不能包含控制字符")
		}
	}
	return prompt, nil
}

func trimmed(value *string) *string {
	if value == nil {
		return nil
	}
	v := strings.TrimSpace(*value)
	return &v
}

// buildPatch validates a request and turns it into a Linx update.
func (a *API) buildPatch(ctx context.Context, in updateAgentRequest) (linx.AgentPatch, error) {
	var patch linx.AgentPatch
	if in.SystemPrompt != nil {
		prompt, err := validateSystemPrompt(*in.SystemPrompt)
		if err != nil {
			return patch, err
		}
		patch.SystemPrompt = &prompt
	}
	if in.AssistantName != nil {
		name := strings.TrimSpace(*in.AssistantName)
		if name == "" || utf8.RuneCountInString(name) > maxAssistantNameRunes {
			return patch, invalidArgument("助手名称需要 1 到 20 个字符")
		}
		for _, r := range name {
			if unicode.IsControl(r) {
				return patch, invalidArgument("助手名称不能包含控制字符")
			}
		}
		patch.AssistantName = &name
	}
	if id := trimmed(in.TTSVoiceID); id != nil {
		voices, err := a.loadVoices(ctx)
		if err != nil {
			return patch, err
		}
		found := false
		for _, voice := range voices {
			if voice.ID == *id {
				found = true
				break
			}
		}
		if !found {
			return patch, invalidArgument("没有这个音色")
		}
		patch.TTSVoiceID = id
	}
	if code := trimmed(in.LangCode); code != nil {
		languages, err := a.loadLanguages(ctx)
		if err != nil {
			return patch, err
		}
		if !inList(languages, *code) {
			return patch, invalidArgument("不支持这种语言")
		}
		patch.LangCode = code
	}
	if id := trimmed(in.LLMModelID); id != nil {
		models, err := a.loadLLMModels(ctx)
		if err != nil {
			return patch, err
		}
		if !inList(models, *id) {
			return patch, invalidArgument("没有这个大模型")
		}
		patch.LLMModelID = id
	}
	if id := trimmed(in.MemModelID); id != nil {
		if !inList(memoryModes, *id) {
			return patch, invalidArgument("没有这种记忆模式")
		}
		patch.MemModelID = id
	}

	if v := in.ChatHistory; v != nil {
		if *v < 0 || *v > 2 {
			return patch, invalidArgument("聊天保存只能是不记录、仅文本或文本加语音")
		}
		patch.ChatHistoryConf = v
	}

	var extra linx.AgentExtra
	touched := false
	if v := in.VoiceVolume; v != nil {
		if *v < minVolume || *v > maxVolume {
			return patch, invalidArgument("音量范围是 0 到 100")
		}
		extra.Voice.Volume, touched = v, true
	}
	if v := in.VoiceSpeed; v != nil {
		if *v < minSpeed || *v > maxSpeed {
			return patch, invalidArgument("语速范围是 0.5 到 2.0")
		}
		extra.Voice.Speed, touched = v, true
	}
	if v := in.LLMTemperature; v != nil {
		if *v < minTemperature || *v > maxTemperature {
			return patch, invalidArgument("回答随机性范围是 0 到 1.9")
		}
		extra.LLM.Temperature, touched = v, true
	}
	if v := in.LLMMaxTokens; v != nil {
		if *v < minMaxTokens || *v > maxMaxTokens {
			return patch, invalidArgument("回复长度上限范围是 1 到 8192")
		}
		extra.LLM.MaxTokens, touched = v, true
	}
	if v := in.IdleTimeoutSeconds; v != nil {
		if *v < minIdleSeconds || *v > maxIdleSeconds {
			return patch, invalidArgument("空闲断开时间范围是 60 到 3600 秒")
		}
		extra.Goodbye.IdleTimeoutSeconds, touched = v, true
	}
	if v := in.DisableGreeting; v != nil {
		extra.DisableGreeting, touched = v, true
	}
	if touched {
		patch.Extra = &extra
	}
	return patch, nil
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
	if !hasChange(in) {
		writeError(w, invalidArgument("没有需要修改的内容"))
		return
	}
	if a.provision == nil {
		writeError(w, apiError(http.StatusServiceUnavailable, "PROVISION_UNAVAILABLE", "开通组件暂不可用"))
		return
	}
	patch, err := a.buildPatch(r.Context(), in)
	if err != nil {
		if _, ok := err.(*APIError); ok {
			writeError(w, err)
		} else {
			writeError(w, mapBindError(err))
		}
		return
	}
	if err := a.provision.UpdateAgent(r.Context(), device.LinxAgentID, patch); err != nil {
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
	writeJSON(w, http.StatusOK, map[string]any{"agent": agentSettingsView(info)})
}
