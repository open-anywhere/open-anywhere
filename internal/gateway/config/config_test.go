package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fullEnv() map[string]string {
	return map[string]string{
		"AGENTAPI_BASE_URL":       "http://agentapi",
		"AGENTAPI_API_KEY":        "key",
		"AGENTAPI_AGENT_ID":       "agent",
		"AGENTAPI_ENVIRONMENT_ID": "env",
		"RENDER_BASE_URL":         "http://render",
		"CASTING_BASE_URL":        "http://casting",
		"ARTIFACT_BASE_URL":       "http://gateway:8090",
		"AGENTAPI_AGENT_VERSION":  "3",
		"GATEWAY_ADDR":            ":9000",
		"BUILD_TIMEOUT":           "30m",
		"MONITOR_INTERVAL":        "2s",
		"PORT_MIN":                "40000",
		"PORT_MAX":                "40010",
		"SCREENS_CONFIG":          "/tmp/screens.json",
		"ARTIFACTS_DIR":           "/tmp/artifacts",
		"GATEWAY_DB":              "/tmp/gateway.db",
	}
}

func envLookup(env map[string]string) func(string) string {
	return func(key string) string { return env[key] }
}

func TestParseUsesEnvironmentDefaults(t *testing.T) {
	cfg, err := Parse(nil, envLookup(fullEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if cfg.Addr != ":9000" || cfg.AgentVersion != 3 || cfg.PortMin != 40000 || cfg.PortMax != 40010 {
		t.Fatalf("cfg = %+v", cfg)
	}
	if cfg.BuildTimeout != 30*time.Minute || cfg.MonitorInterval != 2*time.Second {
		t.Fatalf("durations = %+v", cfg)
	}
	if cfg.ScreensPath != "/tmp/screens.json" || cfg.ArtifactsDir != "/tmp/artifacts" || cfg.DBPath != "/tmp/gateway.db" {
		t.Fatalf("paths = %+v", cfg)
	}
	if cfg.ConfigPath != "gateway.yaml" {
		t.Fatalf("config path = %q", cfg.ConfigPath)
	}
}

func TestParseFlagsOverrideEnvironment(t *testing.T) {
	cfg, err := Parse([]string{"-addr", ":7000", "-agent-version", "5", "-build-timeout", "10m", "-port-min", "41000", "-port-max", "41001", "-db", "/tmp/custom.db"}, envLookup(fullEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":7000" || cfg.AgentVersion != 5 || cfg.BuildTimeout != 10*time.Minute || cfg.PortMin != 41000 || cfg.DBPath != "/tmp/custom.db" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestParseRejectsPositionalArgumentsAndBadFlags(t *testing.T) {
	if _, err := Parse([]string{"extra"}, func(string) string { return "" }); err == nil {
		t.Fatal("expected positional argument error")
	}
	if _, err := Parse([]string{"-unknown"}, func(string) string { return "" }); err == nil {
		t.Fatal("expected flag parse error")
	}
}

func TestValidateRejectsMissingAndInvalidValues(t *testing.T) {
	base, err := Parse(nil, envLookup(fullEnv()))
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(*Config){
		"missing agent":     func(c *Config) { c.AgentAPIURL = "" },
		"missing key":       func(c *Config) { c.AgentAPIKey = "" },
		"missing agent id":  func(c *Config) { c.AgentID = "" },
		"missing env id":    func(c *Config) { c.EnvironmentID = "" },
		"missing render":    func(c *Config) { c.RenderURL = "" },
		"missing casting":   func(c *Config) { c.CastingURL = "" },
		"missing artifact":  func(c *Config) { c.ArtifactBaseURL = "" },
		"missing artifacts": func(c *Config) { c.ArtifactsDir = "" },
		"missing db":        func(c *Config) { c.DBPath = "" },
		"bad version":       func(c *Config) { c.AgentVersion = -1 },
		"bad ports":         func(c *Config) { c.PortMin = 0 },
		"bad range":         func(c *Config) { c.PortMax = 1 },
		"bad timeout":       func(c *Config) { c.BuildTimeout = 0 },
		"relative artifact": func(c *Config) { c.ArtifactBaseURL = "/artifacts" },
		"bad artifact":      func(c *Config) { c.ArtifactBaseURL = "ftp://host" },
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

func TestValidateAllowsLatestAgentVersion(t *testing.T) {
	cfg, err := Parse(nil, envLookup(fullEnv()))
	if err != nil {
		t.Fatal(err)
	}
	cfg.AgentVersion = 0
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestParseInvalidEnvironmentFallsBack(t *testing.T) {
	env := map[string]string{
		"AGENTAPI_AGENT_VERSION": "nope",
		"BUILD_TIMEOUT":          "nope",
		"PORT_MIN":               "nope",
	}
	cfg, err := Parse(nil, envLookup(env))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AgentVersion != 0 || cfg.BuildTimeout != 60*time.Minute || cfg.PortMin != 39000 || cfg.DBPath != "open-anywhere.gateway.db" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestParseYAMLConfig(t *testing.T) {
	path := writeConfig(t, `
addr: ":7000"
agentapi:
  base_url: "http://yaml-agent"
  api_key: "yaml-key"
  agent_id: "yaml-agent-id"
  agent_version: 2
  environment_id: "yaml-env"
render:
  base_url: "http://yaml-render"
casting:
  base_url: "http://yaml-casting"
screens: "screens.json"
artifacts:
  dir: "data/artifacts"
  base_url: "http://gateway:7000"
database:
  path: "data/gateway.db"
ports:
  min: 41000
  max: 41010
timeouts:
  build: "45m"
  start: "30s"
  projection: "20s"
  monitor_interval: "3s"
  call: "90s"
  http: "10s"
`)
	cfg, err := Parse([]string{"-config", path}, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if cfg.ConfigPath != path || cfg.Addr != ":7000" {
		t.Fatalf("cfg = %+v", cfg)
	}
	if cfg.AgentAPIURL != "http://yaml-agent" || cfg.AgentAPIKey != "yaml-key" || cfg.AgentID != "yaml-agent-id" || cfg.AgentVersion != 2 || cfg.EnvironmentID != "yaml-env" {
		t.Fatalf("agentapi = %+v", cfg)
	}
	if cfg.RenderURL != "http://yaml-render" || cfg.CastingURL != "http://yaml-casting" {
		t.Fatalf("services = %+v", cfg)
	}
	if cfg.ScreensPath != "screens.json" || cfg.ArtifactsDir != "data/artifacts" || cfg.ArtifactBaseURL != "http://gateway:7000" || cfg.DBPath != "data/gateway.db" {
		t.Fatalf("paths = %+v", cfg)
	}
	if cfg.PortMin != 41000 || cfg.PortMax != 41010 {
		t.Fatalf("ports = %+v", cfg)
	}
	if cfg.BuildTimeout != 45*time.Minute || cfg.StartTimeout != 30*time.Second || cfg.ProjectionTimeout != 20*time.Second || cfg.MonitorInterval != 3*time.Second || cfg.CallTimeout != 90*time.Second || cfg.HTTPTimeout != 10*time.Second {
		t.Fatalf("timeouts = %+v", cfg)
	}
}

func TestPortalCredentialsComeFromYAMLAndEnvironment(t *testing.T) {
	path := writeConfig(t, `
portal:
  enabled: true
  username: "owner"
  password: "yaml-password"
mcp_token: "test-token"
linx:
  api_key: "linx-key"
  public_mcp_url: "https://example.com/mcp"
`)
	cfg, err := Parse([]string{"-config", path}, envLookup(fullEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !cfg.PortalEnabled || cfg.PortalUsername != "owner" || cfg.PortalPassword != "yaml-password" {
		t.Fatalf("portal = %+v", cfg)
	}

	env := fullEnv()
	env["PORTAL_USERNAME"] = "env-owner"
	env["PORTAL_PASSWORD"] = "env-password"
	cfg, err = Parse([]string{"-config", path}, envLookup(env))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PortalUsername != "env-owner" || cfg.PortalPassword != "env-password" {
		t.Fatalf("environment should override YAML: %+v", cfg)
	}

	cfg.PortalUsername = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected a missing portal username to be rejected")
	}
	cfg.PortalUsername = "owner"
	cfg.PortalPassword = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected a missing portal password to be rejected")
	}
}

func TestConfigPrecedence(t *testing.T) {
	path := writeConfig(t, `
addr: ":7000"
agentapi:
  base_url: "http://yaml-agent"
  agent_version: 2
database:
  path: "yaml.db"
`)
	env := map[string]string{
		"GATEWAY_ADDR":           ":7100",
		"AGENTAPI_BASE_URL":      "http://env-agent",
		"AGENTAPI_AGENT_VERSION": "3",
		"GATEWAY_DB":             "env.db",
	}
	cfg, err := Parse([]string{"-config", path}, envLookup(env))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":7100" || cfg.AgentAPIURL != "http://env-agent" || cfg.AgentVersion != 3 || cfg.DBPath != "env.db" {
		t.Fatalf("environment should override YAML: %+v", cfg)
	}
	cfg, err = Parse([]string{"-config", path, "-addr", ":7200", "-agentapi-url", "http://flag-agent", "-agent-version", "4", "-db", "flag.db"}, envLookup(env))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":7200" || cfg.AgentAPIURL != "http://flag-agent" || cfg.AgentVersion != 4 || cfg.DBPath != "flag.db" {
		t.Fatalf("flags should override environment: %+v", cfg)
	}
}

func TestParseConfigErrors(t *testing.T) {
	tests := map[string]string{
		"unknown field": `
agentapi:
  base_url: "http://agent"
  unknown: true
`,
		"invalid yaml":     "{",
		"invalid timeout":  "timeouts:\n  build: \"soon\"\n",
		"zero timeout":     "timeouts:\n  start: \"0s\"\n",
		"negative version": "agentapi:\n  agent_version: -1\n",
		"negative ports":   "ports:\n  min: -1\n",
		"two documents":    "addr: \":8090\"\n---\naddr: \":8091\"\n",
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			path := writeConfig(t, content)
			if _, err := Parse([]string{"-config", path}, func(string) string { return "" }); err == nil {
				t.Fatal("expected config error")
			}
		})
	}

	if _, err := Parse([]string{"-config", filepath.Join(t.TempDir(), "missing.yaml")}, func(string) string { return "" }); err == nil {
		t.Fatal("explicit missing config should fail")
	}
	if _, err := Parse(nil, func(string) string { return "" }); err != nil {
		t.Fatalf("missing default gateway.yaml should be ignored: %v", err)
	}
	empty := writeConfig(t, "")
	if _, err := Parse([]string{"-config", empty}, func(string) string { return "" }); err != nil {
		t.Fatalf("empty config should be accepted: %v", err)
	}
}

func TestConfigFromEnvironmentConfigPath(t *testing.T) {
	path := writeConfig(t, "addr: \":7300\"\n")
	env := map[string]string{"GATEWAY_CONFIG": path}
	cfg, err := Parse(nil, envLookup(env))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":7300" || cfg.ConfigPath != path {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gateway.yaml")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
