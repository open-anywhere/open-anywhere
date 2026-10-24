// Package config parses the control plane's deployment configuration from a
// YAML file, environment variables and command-line flags.
package config

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the gateway process configuration.
type Config struct {
	MCPToken     string
	PublicHost   string
	SpeakerProbe bool
	ConfigPath   string
	Addr         string

	AgentAPIURL   string
	AgentAPIKey   string
	AgentID       string
	AgentVersion  int
	EnvironmentID string

	RenderURL  string
	CastingURL string

	ScreensPath     string
	ArtifactsDir    string
	ArtifactBaseURL string
	DBPath          string

	PortMin int
	PortMax int

	BuildTimeout      time.Duration
	StartTimeout      time.Duration
	ProjectionTimeout time.Duration
	MonitorInterval   time.Duration
	CallTimeout       time.Duration
	HTTPTimeout       time.Duration
}

// fileConfig mirrors the YAML configuration file. Zero values mean "not set"
// and fall back to the built-in defaults.
type fileConfig struct {
	Addr         string `yaml:"addr"`
	MCPToken     string `yaml:"mcp_token"`
	PublicHost   string `yaml:"public_host"`
	SpeakerProbe bool   `yaml:"speaker_probe"`
	AgentAPI     struct {
		BaseURL       string `yaml:"base_url"`
		APIKey        string `yaml:"api_key"`
		AgentID       string `yaml:"agent_id"`
		AgentVersion  int    `yaml:"agent_version"`
		EnvironmentID string `yaml:"environment_id"`
	} `yaml:"agentapi"`
	Render struct {
		BaseURL string `yaml:"base_url"`
	} `yaml:"render"`
	Casting struct {
		BaseURL string `yaml:"base_url"`
	} `yaml:"casting"`
	Screens   string `yaml:"screens"`
	Artifacts struct {
		Dir     string `yaml:"dir"`
		BaseURL string `yaml:"base_url"`
	} `yaml:"artifacts"`
	Database struct {
		Path string `yaml:"path"`
	} `yaml:"database"`
	Ports struct {
		Min int `yaml:"min"`
		Max int `yaml:"max"`
	} `yaml:"ports"`
	Timeouts struct {
		Build           string `yaml:"build"`
		Start           string `yaml:"start"`
		Projection      string `yaml:"projection"`
		MonitorInterval string `yaml:"monitor_interval"`
		Call            string `yaml:"call"`
		HTTP            string `yaml:"http"`
	} `yaml:"timeouts"`
}

// Parse reads YAML, environment and flag configuration. Precedence is
// flags > environment > YAML file > built-in defaults. Without an explicit
// -config or GATEWAY_CONFIG, gateway.yaml is loaded when it exists.
func Parse(args []string, getenv func(string) string) (Config, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	path, explicit := locateConfig(args, getenv)
	file, err := loadFile(path, explicit)
	if err != nil {
		return Config{}, err
	}
	base := Config{
		Addr:              ":8090",
		AgentVersion:      1,
		ArtifactsDir:      "artifacts",
		DBPath:            "open-anywhere.gateway.db",
		PortMin:           39000,
		PortMax:           39999,
		BuildTimeout:      60 * time.Minute,
		StartTimeout:      60 * time.Second,
		ProjectionTimeout: 30 * time.Second,
		MonitorInterval:   5 * time.Second,
		CallTimeout:       2 * time.Minute,
		HTTPTimeout:       30 * time.Second,
	}
	if err := applyFile(&base, file); err != nil {
		return Config{}, err
	}

	var cfg Config
	cfg.MCPToken = file.MCPToken
	cfg.PublicHost = file.PublicHost
	cfg.SpeakerProbe = file.SpeakerProbe
	flags := flag.NewFlagSet("gateway", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&cfg.MCPToken, "mcp-token", cfg.MCPToken, "MCP bearer token")
	flags.StringVar(&cfg.PublicHost, "public-host", cfg.PublicHost, "allowed reverse proxy Host")
	flags.BoolVar(&cfg.SpeakerProbe, "speaker-probe", cfg.SpeakerProbe, "enable diagnostic speaker tool")
	flags.StringVar(&cfg.ConfigPath, "config", path, "YAML configuration file")
	flags.StringVar(&cfg.Addr, "addr", env(getenv, "GATEWAY_ADDR", base.Addr), "HTTP listen address")
	flags.StringVar(&cfg.AgentAPIURL, "agentapi-url", env(getenv, "AGENTAPI_BASE_URL", base.AgentAPIURL), "AgentAPI base URL")
	flags.StringVar(&cfg.AgentAPIKey, "agentapi-key", env(getenv, "AGENTAPI_API_KEY", base.AgentAPIKey), "AgentAPI API key")
	flags.StringVar(&cfg.AgentID, "agent-id", env(getenv, "AGENTAPI_AGENT_ID", base.AgentID), "build agent ID")
	flags.IntVar(&cfg.AgentVersion, "agent-version", envInt(getenv, "AGENTAPI_AGENT_VERSION", base.AgentVersion), "build agent version")
	flags.StringVar(&cfg.EnvironmentID, "environment-id", env(getenv, "AGENTAPI_ENVIRONMENT_ID", base.EnvironmentID), "build environment ID")
	flags.StringVar(&cfg.RenderURL, "render-url", env(getenv, "RENDER_BASE_URL", base.RenderURL), "Render base URL")
	flags.StringVar(&cfg.CastingURL, "casting-url", env(getenv, "CASTING_BASE_URL", base.CastingURL), "casting service base URL")
	flags.StringVar(&cfg.ScreensPath, "screens", env(getenv, "SCREENS_CONFIG", base.ScreensPath), "screen mapping JSON file")
	flags.StringVar(&cfg.ArtifactsDir, "artifacts-dir", env(getenv, "ARTIFACTS_DIR", base.ArtifactsDir), "artifact storage directory")
	flags.StringVar(&cfg.ArtifactBaseURL, "artifact-base-url", env(getenv, "ARTIFACT_BASE_URL", base.ArtifactBaseURL), "public base URL Render uses to download artifacts")
	flags.StringVar(&cfg.DBPath, "db", env(getenv, "GATEWAY_DB", base.DBPath), "SQLite database file")
	flags.IntVar(&cfg.PortMin, "port-min", envInt(getenv, "PORT_MIN", base.PortMin), "first application port")
	flags.IntVar(&cfg.PortMax, "port-max", envInt(getenv, "PORT_MAX", base.PortMax), "last application port")
	flags.DurationVar(&cfg.BuildTimeout, "build-timeout", envDuration(getenv, "BUILD_TIMEOUT", base.BuildTimeout), "overall build timeout")
	flags.DurationVar(&cfg.StartTimeout, "start-timeout", envDuration(getenv, "START_TIMEOUT", base.StartTimeout), "Render start timeout")
	flags.DurationVar(&cfg.ProjectionTimeout, "projection-timeout", envDuration(getenv, "PROJECTION_TIMEOUT", base.ProjectionTimeout), "projection start timeout")
	flags.DurationVar(&cfg.MonitorInterval, "monitor-interval", envDuration(getenv, "MONITOR_INTERVAL", base.MonitorInterval), "running application poll interval")
	flags.DurationVar(&cfg.CallTimeout, "call-timeout", envDuration(getenv, "CALL_TIMEOUT", base.CallTimeout), "MCP tool call timeout")
	flags.DurationVar(&cfg.HTTPTimeout, "http-timeout", envDuration(getenv, "HTTP_TIMEOUT", base.HTTPTimeout), "ordinary outbound HTTP timeout")
	if err := flags.Parse(args); err != nil {
		return Config{}, err
	}
	if flags.NArg() != 0 {
		return Config{}, fmt.Errorf("gateway does not accept positional arguments")
	}
	return cfg, nil
}

// Validate checks that required deployment settings are present.
func (c Config) Validate() error {
	required := []struct {
		name  string
		value string
	}{
		{"agentapi-url", c.AgentAPIURL},
		{"agentapi-key", c.AgentAPIKey},
		{"agent-id", c.AgentID},
		{"environment-id", c.EnvironmentID},
		{"render-url", c.RenderURL},
		{"casting-url", c.CastingURL},
		{"artifact-base-url", c.ArtifactBaseURL},
		{"artifacts-dir", c.ArtifactsDir},
		{"db", c.DBPath},
	}
	for _, item := range required {
		if item.value == "" {
			return fmt.Errorf("gateway: -%s is required", item.name)
		}
	}
	if c.AgentVersion <= 0 {
		return fmt.Errorf("gateway: -agent-version must be positive")
	}
	if c.PortMin <= 0 || c.PortMax < c.PortMin || c.PortMax > 65535 {
		return fmt.Errorf("gateway: invalid port range %d-%d", c.PortMin, c.PortMax)
	}
	for name, duration := range map[string]time.Duration{
		"build-timeout":      c.BuildTimeout,
		"start-timeout":      c.StartTimeout,
		"projection-timeout": c.ProjectionTimeout,
		"monitor-interval":   c.MonitorInterval,
		"call-timeout":       c.CallTimeout,
		"http-timeout":       c.HTTPTimeout,
	} {
		if duration <= 0 {
			return fmt.Errorf("gateway: -%s must be positive", name)
		}
	}
	parsed, err := url.Parse(c.ArtifactBaseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("gateway: -artifact-base-url must be an absolute HTTP(S) URL")
	}
	return nil
}

func locateConfig(args []string, getenv func(string) string) (string, bool) {
	for index, arg := range args {
		switch {
		case arg == "-config" || arg == "--config":
			if index+1 < len(args) {
				return args[index+1], true
			}
			return "", true
		case strings.HasPrefix(arg, "-config="):
			return strings.TrimPrefix(arg, "-config="), true
		case strings.HasPrefix(arg, "--config="):
			return strings.TrimPrefix(arg, "--config="), true
		}
	}
	if value := getenv("GATEWAY_CONFIG"); value != "" {
		return value, true
	}
	return "gateway.yaml", false
}

func loadFile(path string, explicit bool) (fileConfig, error) {
	var file fileConfig
	if path == "" {
		return file, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if explicit {
			return file, fmt.Errorf("gateway: config file %q does not exist", path)
		}
		return file, nil
	}
	if err != nil {
		return file, fmt.Errorf("gateway: read config file: %w", err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&file); err != nil {
		if errors.Is(err, io.EOF) {
			return file, nil
		}
		return file, fmt.Errorf("gateway: parse config file %q: %w", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return file, fmt.Errorf("gateway: config file %q must contain one YAML document", path)
	}
	return file, nil
}

func applyFile(cfg *Config, file fileConfig) error {
	if file.Addr != "" {
		cfg.Addr = file.Addr
	}
	if file.AgentAPI.BaseURL != "" {
		cfg.AgentAPIURL = file.AgentAPI.BaseURL
	}
	if file.AgentAPI.APIKey != "" {
		cfg.AgentAPIKey = file.AgentAPI.APIKey
	}
	if file.AgentAPI.AgentID != "" {
		cfg.AgentID = file.AgentAPI.AgentID
	}
	if file.AgentAPI.AgentVersion < 0 {
		return fmt.Errorf("gateway: agentapi.agent_version must not be negative")
	}
	if file.AgentAPI.AgentVersion > 0 {
		cfg.AgentVersion = file.AgentAPI.AgentVersion
	}
	if file.AgentAPI.EnvironmentID != "" {
		cfg.EnvironmentID = file.AgentAPI.EnvironmentID
	}
	if file.Render.BaseURL != "" {
		cfg.RenderURL = file.Render.BaseURL
	}
	if file.Casting.BaseURL != "" {
		cfg.CastingURL = file.Casting.BaseURL
	}
	if file.Screens != "" {
		cfg.ScreensPath = file.Screens
	}
	if file.Artifacts.Dir != "" {
		cfg.ArtifactsDir = file.Artifacts.Dir
	}
	if file.Artifacts.BaseURL != "" {
		cfg.ArtifactBaseURL = file.Artifacts.BaseURL
	}
	if file.Database.Path != "" {
		cfg.DBPath = file.Database.Path
	}
	if file.Ports.Min < 0 || file.Ports.Max < 0 {
		return fmt.Errorf("gateway: ports must not be negative")
	}
	if file.Ports.Min > 0 {
		cfg.PortMin = file.Ports.Min
	}
	if file.Ports.Max > 0 {
		cfg.PortMax = file.Ports.Max
	}
	for _, item := range []struct {
		name   string
		value  string
		target *time.Duration
	}{
		{"timeouts.build", file.Timeouts.Build, &cfg.BuildTimeout},
		{"timeouts.start", file.Timeouts.Start, &cfg.StartTimeout},
		{"timeouts.projection", file.Timeouts.Projection, &cfg.ProjectionTimeout},
		{"timeouts.monitor_interval", file.Timeouts.MonitorInterval, &cfg.MonitorInterval},
		{"timeouts.call", file.Timeouts.Call, &cfg.CallTimeout},
		{"timeouts.http", file.Timeouts.HTTP, &cfg.HTTPTimeout},
	} {
		if item.value == "" {
			continue
		}
		parsed, err := time.ParseDuration(item.value)
		if err != nil || parsed <= 0 {
			return fmt.Errorf("gateway: invalid %s duration %q", item.name, item.value)
		}
		*item.target = parsed
	}
	return nil
}

func env(getenv func(string) string, key, fallback string) string {
	if value := getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt(getenv func(string) string, key string, fallback int) int {
	value := getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDuration(getenv func(string) string, key string, fallback time.Duration) time.Duration {
	value := getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}
