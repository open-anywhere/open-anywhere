package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/open-anywhere/open-anywhere/gateway/internal/application"
	"github.com/open-anywhere/open-anywhere/gateway/internal/artifact"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedWithoutDependencies(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(`{"mode":"managed"}`), 0600); err != nil {
		t.Fatal(err)
	}
	d, err := loadDependencies(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	defer d.apps.Close()
	if d.apps == nil || d.controller != nil || d.artifacts == nil {
		t.Fatal("managed dependencies missing")
	}
}
func TestInvalidConfiguration(t *testing.T) {
	for _, config := range []string{`{"mode":"wrong"}`, `{"mode":"managed","unknown":1}`, `{"mode":"managed","render_url":"file:///etc/passwd"}`, `{"mode":"managed","port_min":50000,"port_max":1}`} {
		p := filepath.Join(t.TempDir(), "config.json")
		_ = os.WriteFile(p, []byte(config), 0600)
		if d, err := loadDependencies(context.Background(), p); err == nil {
			if d.apps != nil {
				d.apps.Close()
			}
			t.Fatalf("accepted %s", config)
		}
	}
}

func TestAgentAPIKeyFromLocalConfig(t *testing.T) {
	t.Setenv("AGENTAPI_API_KEY", "environment-must-not-override-file")
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("x-api-key")
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "test-file-secret") || strings.Contains(string(body), "api_key") {
			t.Error("key leaked into session body")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"session-test"}`))
	}))
	defer server.Close()
	var cfg config
	input := fmt.Sprintf(`{"agentapi":{"base_url":%q,"agent_id":"agent","agent_version":1,"environment_id":"environment","api_key":"test-file-secret"}}`, server.URL)
	if err := json.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatal(err)
	}
	client, err := buildClient(cfg.AgentAPI, artifact.Store{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Create(context.Background(), application.BuildRequest{AppID: "app-test"}); err != nil {
		t.Fatal(err)
	}
	if got != "test-file-secret" {
		t.Fatalf("wrong key source")
	}
	cfg.AgentAPI.APIKey = ""
	if _, err = buildClient(cfg.AgentAPI, artifact.Store{}); err == nil {
		t.Fatal("unexpected environment fallback")
	}
}

func TestDisabledAgentAPIAllowsEmptyLocalTemplate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "gateway.local.json")
	input := `{"mode":"managed","agentapi":{"enabled":false,"api_key":"","agent_id":"","agent_version":1,"environment_id":"","base_url":"https://agent.qiniuapi.com"}}`
	if err := os.WriteFile(p, []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	d, err := loadDependencies(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	defer d.apps.Close()
	input = strings.Replace(input, `"enabled":false`, `"enabled":true`, 1)
	input = strings.Replace(input, `"mode":"managed"`, `"mode":"managed","public_url":"http://127.0.0.1:8787"`, 1)
	_ = os.WriteFile(p, []byte(input), 0600)
	if _, err = loadDependencies(context.Background(), p); err == nil || !strings.Contains(err.Error(), "agentapi.api_key") {
		t.Fatalf("missing-key diagnostic: %v", err)
	}
}
