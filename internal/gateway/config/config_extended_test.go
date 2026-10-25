package config

import (
	"testing"
	"time"
)

func TestParseBooleanEnvironmentValues(t *testing.T) {
	env := fullEnv()
	env["PORTAL_ENABLED"] = "true"
	env["PORTAL_COOKIE_SECURE"] = "not-a-bool"
	cfg, err := Parse(nil, envLookup(env))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.PortalEnabled {
		t.Fatal("PORTAL_ENABLED=true was ignored")
	}
	if cfg.PortalCookieSecure {
		t.Fatal("an invalid PORTAL_COOKIE_SECURE should fall back to the default")
	}

	env["PORTAL_ENABLED"] = "false"
	env["PORTAL_COOKIE_SECURE"] = "1"
	cfg, err = Parse(nil, envLookup(env))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PortalEnabled {
		t.Fatal("PORTAL_ENABLED=false was ignored")
	}
	if !cfg.PortalCookieSecure {
		t.Fatal("PORTAL_COOKIE_SECURE=1 was ignored")
	}
}

func TestParseYAMLPortalAndLinxSettings(t *testing.T) {
	path := writeConfig(t, `
portal:
  enabled: true
  data_dir: "custom-portal"
  web_dir: "custom-web"
  web_embedded: true
  cookie_secure: true
  allow_registration: true
  session_ttl: "90m"
services:
  allow_private_targets: true
linx:
  api_base: "http://linx.local"
  ota_url: "http://ota.local/"
  api_key: "linx-key-2"
  public_mcp_url: "https://gateway.example/mcp"
  mcp_name: "Custom MCP"
  assistant_name: "小助"
  llm_model_id: "llm-x"
  mem_model_id: "mem-x"
  intent_model_id: "intent-x"
  system_prompt_file: "prompt.txt"
  provision_timeout: "45s"
  token_poll_interval: "3s"
  token_poll_attempts: 4
`)
	cfg, err := Parse([]string{"-config", path}, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.PortalEnabled || cfg.PortalDataDir != "custom-portal" || cfg.PortalWebDir != "custom-web" {
		t.Fatalf("portal = %+v", cfg)
	}
	if !cfg.PortalWebEmbedded || !cfg.PortalCookieSecure || !cfg.PortalAllowRegistration {
		t.Fatalf("portal switches = %+v", cfg)
	}
	if cfg.PortalSessionTTL != 90*time.Minute {
		t.Fatalf("session TTL = %v", cfg.PortalSessionTTL)
	}
	if !cfg.AllowPrivateTargets {
		t.Fatal("services.allow_private_targets was ignored")
	}
	if cfg.LinxAPIBase != "http://linx.local" || cfg.LinxOTAURL != "http://ota.local/" || cfg.LinxAPIKey != "linx-key-2" {
		t.Fatalf("linx endpoints = %+v", cfg)
	}
	if cfg.LinxPublicMCPURL != "https://gateway.example/mcp" || cfg.LinxMCPName != "Custom MCP" {
		t.Fatalf("linx mcp = %+v", cfg)
	}
	if cfg.LinxAssistantName != "小助" || cfg.LinxLLMModelID != "llm-x" || cfg.LinxMemModelID != "mem-x" || cfg.LinxIntentModelID != "intent-x" {
		t.Fatalf("linx models = %+v", cfg)
	}
	if cfg.LinxSystemPromptFile != "prompt.txt" {
		t.Fatalf("system prompt = %q", cfg.LinxSystemPromptFile)
	}
	if cfg.LinxProvisionTimeout != 45*time.Second || cfg.LinxTokenPollInterval != 3*time.Second || cfg.LinxTokenPollAttempts != 4 {
		t.Fatalf("linx timeouts = %+v", cfg)
	}
}

func TestParseRejectsNegativeTokenPollAttempts(t *testing.T) {
	path := writeConfig(t, "linx:\n  token_poll_attempts: -1\n")
	if _, err := Parse([]string{"-config", path}, func(string) string { return "" }); err == nil {
		t.Fatal("a negative token poll attempt count was accepted")
	}
}

func validPortalConfig() Config {
	return Config{
		ArtifactBaseURL:   "https://gateway.example",
		ArtifactsDir:      "artifacts",
		DBPath:            "gateway.db",
		PortMin:           39000,
		PortMax:           39999,
		BuildTimeout:      time.Minute,
		StartTimeout:      time.Minute,
		ProjectionTimeout: time.Minute,
		MonitorInterval:   time.Second,
		CallTimeout:       time.Minute,
		HTTPTimeout:       time.Minute,

		PortalEnabled:         true,
		MCPToken:              "mcp-token",
		LinxPublicMCPURL:      "https://gateway.example/mcp",
		PortalDataDir:         "portal-data",
		PortalUsername:        "owner",
		PortalPassword:        "password",
		PortalSessionTTL:      time.Hour,
		LinxProvisionTimeout:  time.Minute,
		LinxTokenPollInterval: time.Second,
		LinxTokenPollAttempts: 1,
	}
}

func TestValidatePortalDeploymentSettings(t *testing.T) {
	base := validPortalConfig()
	if err := base.Validate(); err != nil {
		t.Fatalf("the baseline portal configuration is invalid: %v", err)
	}
	tests := map[string]func(*Config){
		"invalid public mcp url":   func(c *Config) { c.LinxPublicMCPURL = "http://[::1" },
		"private public mcp url":   func(c *Config) { c.LinxPublicMCPURL = "ftp://gateway.example/mcp" },
		"missing data directory":   func(c *Config) { c.PortalDataDir = "" },
		"missing session ttl":      func(c *Config) { c.PortalSessionTTL = 0 },
		"missing provision budget": func(c *Config) { c.LinxProvisionTimeout = 0 },
		"missing poll interval":    func(c *Config) { c.LinxTokenPollInterval = 0 },
		"missing poll attempts":    func(c *Config) { c.LinxTokenPollAttempts = 0 },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := base
			mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
