package portal

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/linx"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
)

type fakeAgentPlatform struct {
	mu      sync.Mutex
	agent   map[string]any
	updates []map[string]any
	fail    bool
}

func (f *fakeAgentPlatform) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/voices":
			_, _ = w.Write([]byte(`{"code":0,"data":{"voices":[
				{"id":"voice_a","name":"小雅","demo":"https://example.com/a.mp3","tags":{"gender":"female","age":"young","styles":["温柔"]}},
				{"id":"voice_b","name":"阿伟","tags":{"gender":"male"}}]}}`))
		case r.URL.Path == "/v1/languages":
			_, _ = w.Write([]byte(`{"code":0,"data":{"languages":[{"language":"中文","lang_code":"zh"},{"language":"英文","lang_code":"en"}]}}`))
		case r.URL.Path == "/v1/agents/models/llm":
			_, _ = w.Write([]byte(`{"code":0,"data":{"publics":[{"id":"llm_a","name":"模型甲"},{"id":"llm_b","name":"模型乙","comment":"更快"}],"privates":[{"id":"llm_secret","name":"私有"}]}}`))
		case r.URL.Path == "/xiaozhi/agent/agent_1" && r.Method == http.MethodGet:
			body, _ := json.Marshal(map[string]any{"code": 0, "data": f.agent})
			_, _ = w.Write(body)
		case r.URL.Path == "/xiaozhi/agent/agent_1" && r.Method == http.MethodPut:
			if f.fail {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"code":500,"msg":"boom"}`))
				return
			}
			raw, _ := io.ReadAll(r.Body)
			update := map[string]any{}
			_ = json.Unmarshal(raw, &update)
			f.updates = append(f.updates, update)
			for key, value := range update {
				f.agent[key] = value
			}
			_, _ = w.Write([]byte(`{"code":0,"msg":"success","data":null}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func newAgentEnv(t *testing.T) (*portalHTTPEnv, *fakeAgentPlatform) {
	t.Helper()
	env := newPortalHTTPEnv(t)
	platform := &fakeAgentPlatform{agent: map[string]any{"assistantName": "小灵", "ttsVoiceId": "voice_a", "langCode": "zh", "llmModelId": "llm_a", "memModelId": "Memory_nomem"}}
	server := httptest.NewServer(platform.handler())
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	client := linx.New(linx.Config{BaseURL: server.URL, APIKey: "key"})
	env.api.provision = NewProvisioner(ctx, env.db, client, env.api.voice, ProvisionConfig{DataDir: env.dataDir})
	env.login(t)
	user, err := env.db.PortalUserByUsername(context.Background(), testPortalUsername)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, device := range []store.PortalDevice{
		{ID: "dev_base", UserID: user.ID, Kind: store.PortalDeviceVirtual, MAC: "02:00:00:00:00:01", ClientID: "c1",
			LinxAgentID: "agent_1", State: store.PortalDeviceRegistered, CreatedAt: now, UpdatedAt: now},
		{ID: "dev_new", UserID: user.ID, Kind: store.PortalDeviceVirtual, MAC: "02:00:00:00:00:02", ClientID: "c2",
			State: store.PortalDeviceProvisioning, CreatedAt: now, UpdatedAt: now},
	} {
		if err := env.db.CreatePortalDevice(context.Background(), device); err != nil {
			t.Fatal(err)
		}
	}
	return env, platform
}

func TestUserReadsAndChangesTheirAgentNameAndVoice(t *testing.T) {
	env, platform := newAgentEnv(t)
	origin := map[string]string{"Origin": env.server.URL}

	response, payload := env.do(t, http.MethodGet, "/api/portal/agent-options", "", nil)
	if response.StatusCode != http.StatusOK || !strings.Contains(string(payload), `"id":"voice_b"`) ||
		!strings.Contains(string(payload), `"id":"en"`) || !strings.Contains(string(payload), `"id":"llm_b"`) ||
		!strings.Contains(string(payload), `"id":"Memory_long_term_memory"`) {
		t.Fatalf("options status=%d body=%s", response.StatusCode, payload)
	}
	if strings.Contains(string(payload), "llm_secret") {
		t.Fatalf("private models must not be offered: %s", payload)
	}
	response, payload = env.do(t, http.MethodGet, "/api/portal/devices/dev_base/agent", "", nil)
	if response.StatusCode != http.StatusOK || !strings.Contains(string(payload), `"assistant_name":"小灵"`) {
		t.Fatalf("agent status=%d body=%s", response.StatusCode, payload)
	}

	response, payload = env.do(t, http.MethodPut, "/api/portal/devices/dev_base/agent",
		`{"assistant_name":"  阿福 ","tts_voice_id":"voice_b"}`, origin)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("update status=%d body=%s", response.StatusCode, payload)
	}
	if !strings.Contains(string(payload), `"assistant_name":"阿福"`) || !strings.Contains(string(payload), `"tts_voice_id":"voice_b"`) {
		t.Fatalf("read-back = %s", payload)
	}
	platform.mu.Lock()
	updates := append([]map[string]any(nil), platform.updates...)
	platform.mu.Unlock()
	if len(updates) != 1 || updates[0]["assistantName"] != "阿福" || updates[0]["ttsVoiceId"] != "voice_b" || len(updates[0]) != 2 {
		t.Fatalf("platform updates = %v", updates)
	}
}

func TestOnlyChangedAgentFieldsAreSent(t *testing.T) {
	env, platform := newAgentEnv(t)
	response, payload := env.do(t, http.MethodPut, "/api/portal/devices/dev_base/agent",
		`{"assistant_name":"阿福"}`, map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}
	platform.mu.Lock()
	defer platform.mu.Unlock()
	if len(platform.updates) != 1 || len(platform.updates[0]) != 1 || platform.updates[0]["assistantName"] != "阿福" {
		t.Fatalf("platform updates = %v", platform.updates)
	}
}

func TestAgentSettingsRejectBadInput(t *testing.T) {
	env, platform := newAgentEnv(t)
	origin := map[string]string{"Origin": env.server.URL}
	cases := []struct {
		name, path, body, code string
		status                 int
	}{
		{"nothing to change", "dev_base", `{}`, "INVALID_ARGUMENT", http.StatusBadRequest},
		{"empty name", "dev_base", `{"assistant_name":"  "}`, "INVALID_ARGUMENT", http.StatusBadRequest},
		{"long name", "dev_base", `{"assistant_name":"` + strings.Repeat("名", 21) + `"}`, "INVALID_ARGUMENT", http.StatusBadRequest},
		{"unknown voice", "dev_base", `{"tts_voice_id":"voice_zzz"}`, "INVALID_ARGUMENT", http.StatusBadRequest},
		{"unknown language", "dev_base", `{"lang_code":"xx"}`, "INVALID_ARGUMENT", http.StatusBadRequest},
		{"private model", "dev_base", `{"llm_model_id":"llm_secret"}`, "INVALID_ARGUMENT", http.StatusBadRequest},
		{"unknown memory", "dev_base", `{"mem_model_id":"Memory_x"}`, "INVALID_ARGUMENT", http.StatusBadRequest},
		{"speed too fast", "dev_base", `{"voice_speed":3}`, "INVALID_ARGUMENT", http.StatusBadRequest},
		{"volume too loud", "dev_base", `{"voice_volume":101}`, "INVALID_ARGUMENT", http.StatusBadRequest},
		{"temperature too high", "dev_base", `{"llm_temperature":2}`, "INVALID_ARGUMENT", http.StatusBadRequest},
		{"reply too long", "dev_base", `{"llm_max_tokens":9000}`, "INVALID_ARGUMENT", http.StatusBadRequest},
		{"idle too short", "dev_base", `{"idle_timeout_seconds":30}`, "INVALID_ARGUMENT", http.StatusBadRequest},
		{"not your device", "dev_nobody", `{"assistant_name":"x"}`, "NOT_FOUND", http.StatusNotFound},
		{"agent not created yet", "dev_new", `{"assistant_name":"x"}`, "AGENT_NOT_READY", http.StatusConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response, payload := env.do(t, http.MethodPut, "/api/portal/devices/"+tc.path+"/agent", tc.body, origin)
			if response.StatusCode != tc.status || errorCode(t, payload) != tc.code {
				t.Fatalf("status=%d body=%s", response.StatusCode, payload)
			}
		})
	}
	platform.mu.Lock()
	defer platform.mu.Unlock()
	if len(platform.updates) != 0 {
		t.Fatalf("rejected requests reached the platform: %v", platform.updates)
	}
}

func TestAgentSettingsReportPlatformFailure(t *testing.T) {
	env, platform := newAgentEnv(t)
	platform.mu.Lock()
	platform.fail = true
	platform.mu.Unlock()
	response, payload := env.do(t, http.MethodPut, "/api/portal/devices/dev_base/agent",
		`{"assistant_name":"阿福"}`, map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusBadGateway || errorCode(t, payload) != "PLATFORM_UNAVAILABLE" {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}
}

func TestUserTunesModelVoiceAndConversationSettings(t *testing.T) {
	env, platform := newAgentEnv(t)
	response, payload := env.do(t, http.MethodPut, "/api/portal/devices/dev_base/agent",
		`{"lang_code":"en","llm_model_id":"llm_b","mem_model_id":"Memory_long_term_memory",
		"voice_speed":1.2,"voice_volume":70,"llm_temperature":0.5,"llm_max_tokens":300,
		"idle_timeout_seconds":300,"disable_greeting":true}`, map[string]string{"Origin": env.server.URL})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}
	platform.mu.Lock()
	defer platform.mu.Unlock()
	if len(platform.updates) != 1 {
		t.Fatalf("platform updates = %v", platform.updates)
	}
	update := platform.updates[0]
	if update["langCode"] != "en" || update["llmModelId"] != "llm_b" || update["memModelId"] != "Memory_long_term_memory" {
		t.Fatalf("top-level fields = %v", update)
	}
	for _, forbidden := range []string{"systemPrompt", "intentModelId"} {
		if _, ok := update[forbidden]; ok {
			t.Fatalf("%s must never be sent: %v", forbidden, update)
		}
	}
	extra, _ := update["extra"].(map[string]any)
	voice, _ := extra["voice"].(map[string]any)
	llm, _ := extra["llm"].(map[string]any)
	goodbye, _ := extra["goodbye"].(map[string]any)
	if voice["speed"] != 1.2 || voice["volume"] != 70.0 || llm["temperature"] != 0.5 || llm["max_tokens"] != 300.0 ||
		goodbye["idle_timeout_seconds"] != 300.0 || extra["disable_greeting"] != true {
		t.Fatalf("extra = %v", extra)
	}
}
