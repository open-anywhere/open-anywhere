package portal

import (
	"net/http"
	"strings"
	"testing"
)

func TestChatHistoryIsOldestFirstAndKeepsOnlyTheConversation(t *testing.T) {
	env, _ := newAgentEnv(t)
	response, payload := env.do(t, http.MethodGet, "/api/portal/devices/dev_base/chat-history", "", nil)
	body := string(payload)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
	if strings.Contains(body, "secret.wav") || strings.Contains(body, "AA:BB") || strings.Contains(body, "tool") {
		t.Fatalf("unexpected fields: %s", body)
	}
	if strings.Index(body, "你好") < 0 || strings.Index(body, "你好") > strings.Index(body, "好的") {
		t.Fatalf("not oldest first: %s", body)
	}
	response, payload = env.do(t, http.MethodGet, "/api/portal/devices/dev_base/chat-history?limit=0", "", nil)
	if response.StatusCode != http.StatusBadRequest || errorCode(t, payload) != "INVALID_ARGUMENT" {
		t.Fatalf("status=%d body=%s", response.StatusCode, payload)
	}
}
