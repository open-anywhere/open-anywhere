// Package config parses the control plane's deployment configuration from a
// YAML file, environment variables and command-line flags.
package config

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
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
	AgentVersion  int // 0 means "use the agent's latest version"
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

	PortalEnabled           bool
	PortalDataDir           string
	PortalWebDir            string
	PortalWebEmbedded       bool
	PortalUsername          string
	PortalPassword          string
	PortalSessionTTL        time.Duration
	PortalCookieSecure      bool
	PortalAllowRegistration bool

	AllowPrivateTargets bool

	LinxAPIBase           string
	LinxOTAURL            string
	LinxAPIKey            string
	LinxPublicMCPURL      string
	LinxMCPName           string
	LinxAssistantName     string
	LinxLLMModelID        string
	LinxMemModelID        string
	LinxIntentModelID     string
	LinxSystemPromptFile  string
	LinxProvisionTimeout  time.Duration
	LinxTokenPollInterval time.Duration
	LinxTokenPollAttempts int
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
	Services struct {
		AllowPrivateTargets bool `yaml:"allow_private_targets"`
	} `yaml:"services"`
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
	Portal struct {
		Enabled           bool   `yaml:"enabled"`
		DataDir           string `yaml:"data_dir"`
		WebDir            string `yaml:"web_dir"`
		WebEmbedded       bool   `yaml:"web_embedded"`
		Username          string `yaml:"username"`
		Password          string `yaml:"password"`
		SessionTTL        string `yaml:"session_ttl"`
		CookieSecure      *bool  `yaml:"cookie_secure"`
		AllowRegistration bool   `yaml:"allow_registration"`
	} `yaml:"portal"`
	Linx struct {
		APIBase           string `yaml:"api_base"`
		OTAURL            string `yaml:"ota_url"`
		APIKey            string `yaml:"api_key"`
		PublicMCPURL      string `yaml:"public_mcp_url"`
		MCPName           string `yaml:"mcp_name"`
		AssistantName     string `yaml:"assistant_name"`
		LLMModelID        string `yaml:"llm_model_id"`
		MemModelID        string `yaml:"mem_model_id"`
		IntentModelID     string `yaml:"intent_model_id"`
		SystemPromptFile  string `yaml:"system_prompt_file"`
		ProvisionTimeout  string `yaml:"provision_timeout"`
		TokenPollInterval string `yaml:"token_poll_interval"`
		TokenPollAttempts int    `yaml:"token_poll_attempts"`
	} `yaml:"linx"`
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

		PortalDataDir:         "portal-data",
		PortalWebDir:          "portal/dist",
		PortalSessionTTL:      720 * time.Hour,
		PortalCookieSecure:    true,
		LinxAPIBase:           "https://xrobo.qiniu.com",
		LinxOTAURL:            "https://xrobo.qiniuapi.com/v1/ota/",
		LinxMCPName:           "Open Anywhere",
		LinxAssistantName:     "小灵",
		LinxLLMModelID:        "f3626c105383d71654a57f2bb8a973f3",
		LinxMemModelID:        "Memory_nomem",
		LinxIntentModelID:     "Intent_function_call",
		LinxProvisionTimeout:  60 * time.Second,
		LinxTokenPollInterval: 2 * time.Second,
		LinxTokenPollAttempts: 10,
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
	flags.IntVar(&cfg.AgentVersion, "agent-version", envInt(getenv, "AGENTAPI_AGENT_VERSION", base.AgentVersion), "build agent version (0 = latest)")
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
	flags.BoolVar(&cfg.PortalEnabled, "portal", envBool(getenv, "PORTAL_ENABLED", base.PortalEnabled), "serve the user portal")
	flags.StringVar(&cfg.PortalDataDir, "portal-data-dir", env(getenv, "PORTAL_DATA_DIR", base.PortalDataDir), "portal credential and runtime directory")
	flags.StringVar(&cfg.PortalWebDir, "portal-web-dir", env(getenv, "PORTAL_WEB_DIR", base.PortalWebDir), "built portal frontend directory")
	flags.BoolVar(&cfg.PortalWebEmbedded, "portal-web-embedded", envBool(getenv, "PORTAL_WEB_EMBEDDED", base.PortalWebEmbedded), "serve the portal frontend embedded in the binary (requires -tags portalembed)")
	flags.DurationVar(&cfg.PortalSessionTTL, "portal-session-ttl", envDuration(getenv, "PORTAL_SESSION_TTL", base.PortalSessionTTL), "portal session lifetime")
	flags.BoolVar(&cfg.PortalCookieSecure, "portal-cookie-secure", envBool(getenv, "PORTAL_COOKIE_SECURE", base.PortalCookieSecure), "mark portal cookies Secure (HTTPS); disable only for loopback HTTP")
	flags.BoolVar(&cfg.PortalAllowRegistration, "portal-allow-registration", envBool(getenv, "PORTAL_ALLOW_REGISTRATION", base.PortalAllowRegistration), "seed open registration for a fresh database")
	flags.BoolVar(&cfg.AllowPrivateTargets, "allow-private-targets", envBool(getenv, "ALLOW_PRIVATE_TARGETS", base.AllowPrivateTargets), "allow loopback and private Render/Casting addresses (debug only)")
	flags.StringVar(&cfg.LinxAPIBase, "linx-api-base", env(getenv, "LINX_API_BASE", base.LinxAPIBase), "Linx management API base URL")
	flags.StringVar(&cfg.LinxOTAURL, "linx-ota-url", env(getenv, "LINX_OTA_URL", base.LinxOTAURL), "Linx OTA endpoint")
	flags.StringVar(&cfg.LinxAPIKey, "linx-api-key", env(getenv, "LINX_API_KEY", base.LinxAPIKey), "Linx platform account API key")
	flags.StringVar(&cfg.LinxPublicMCPURL, "linx-public-mcp-url", env(getenv, "LINX_PUBLIC_MCP_URL", base.LinxPublicMCPURL), "public SSE URL of the gateway MCP")
	flags.StringVar(&cfg.LinxMCPName, "linx-mcp-name", env(getenv, "LINX_MCP_NAME", base.LinxMCPName), "MCP server name on the Linx platform")
	flags.StringVar(&cfg.LinxAssistantName, "linx-assistant-name", env(getenv, "LINX_ASSISTANT_NAME", base.LinxAssistantName), "assistant nickname for created agents")
	flags.StringVar(&cfg.LinxLLMModelID, "linx-llm-model-id", env(getenv, "LINX_LLM_MODEL_ID", base.LinxLLMModelID), "LLM model id for created agents")
	flags.StringVar(&cfg.LinxMemModelID, "linx-mem-model-id", env(getenv, "LINX_MEM_MODEL_ID", base.LinxMemModelID), "memory model id for created agents")
	flags.StringVar(&cfg.LinxIntentModelID, "linx-intent-model-id", env(getenv, "LINX_INTENT_MODEL_ID", base.LinxIntentModelID), "intent model id for created agents")
	flags.StringVar(&cfg.LinxSystemPromptFile, "linx-system-prompt-file", env(getenv, "LINX_SYSTEM_PROMPT_FILE", base.LinxSystemPromptFile), "agent system prompt override file")
	flags.DurationVar(&cfg.LinxProvisionTimeout, "linx-provision-timeout", envDuration(getenv, "LINX_PROVISION_TIMEOUT", base.LinxProvisionTimeout), "device provisioning timeout")
	flags.DurationVar(&cfg.LinxTokenPollInterval, "linx-token-poll-interval", envDuration(getenv, "LINX_TOKEN_POLL_INTERVAL", base.LinxTokenPollInterval), "OTA token poll interval")
	flags.IntVar(&cfg.LinxTokenPollAttempts, "linx-token-poll-attempts", envInt(getenv, "LINX_TOKEN_POLL_ATTEMPTS", base.LinxTokenPollAttempts), "OTA token poll attempts")
	if err := flags.Parse(args); err != nil {
		return Config{}, err
	}
	if flags.NArg() != 0 {
		return Config{}, fmt.Errorf("gateway does not accept positional arguments")
	}
	// The portal account is deployment configuration, not a command-line
	// argument: keeping the password out of argv avoids leaking it through the
	// process list and shell history.
	cfg.PortalUsername = env(getenv, "PORTAL_USERNAME", base.PortalUsername)
	cfg.PortalPassword = env(getenv, "PORTAL_PASSWORD", base.PortalPassword)
	return cfg, nil
}

// Validate checks that required deployment settings are present. In portal
// mode the upstream AgentAPI, Linx, Render and Casting credentials are
// platform settings an administrator can fill in after first start, so only
// the gateway's own deployment values are required.
func (c Config) Validate() error {
	type requiredSetting struct {
		name  string
		value string
	}
	required := []requiredSetting{
		{"artifact-base-url", c.ArtifactBaseURL},
		{"artifacts-dir", c.ArtifactsDir},
		{"db", c.DBPath},
	}
	if !c.PortalEnabled {
		required = append(required,
			requiredSetting{"agentapi-url", c.AgentAPIURL},
			requiredSetting{"agentapi-key", c.AgentAPIKey},
			requiredSetting{"agent-id", c.AgentID},
			requiredSetting{"environment-id", c.EnvironmentID},
			requiredSetting{"render-url", c.RenderURL},
			requiredSetting{"casting-url", c.CastingURL},
		)
	}
	for _, item := range required {
		if item.value == "" {
			return fmt.Errorf("gateway: -%s is required", item.name)
		}
	}
	if c.AgentVersion < 0 {
		return fmt.Errorf("gateway: -agent-version must not be negative")
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
	if c.PortalEnabled {
		// The MCP token and public MCP address are platform settings: the token
		// is generated on first start and both are edited in the admin page.
		if c.LinxPublicMCPURL != "" {
			mcpURL, err := url.Parse(c.LinxPublicMCPURL)
			if err != nil || mcpURL.Host == "" || (mcpURL.Scheme != "http" && mcpURL.Scheme != "https") {
				return fmt.Errorf("gateway: -linx-public-mcp-url must be an absolute HTTP(S) URL")
			}
		}
		if c.PortalDataDir == "" {
			return fmt.Errorf("gateway: portal data directory is required")
		}
		if strings.TrimSpace(c.PortalUsername) == "" || c.PortalPassword == "" {
			return fmt.Errorf("gateway: portal username and password are required when the portal is enabled")
		}
		if c.PortalSessionTTL <= 0 {
			return fmt.Errorf("gateway: -portal-session-ttl must be positive")
		}
		if c.LinxProvisionTimeout <= 0 || c.LinxTokenPollInterval <= 0 || c.LinxTokenPollAttempts < 1 {
			return fmt.Errorf("gateway: portal provisioning timeouts must be positive")
		}
		if warning := c.portalCookieWarning(); warning != "" {
			// The portal still starts: a local HTTP deployment is a legitimate
			// development setup. The warning must be loud enough to catch a
			// public listener that would send session cookies in clear text.
			log.Print(warning)
		}
	}
	return nil
}

// portalCookieWarning reports an exposed portal that disables Secure cookies.
func (c Config) portalCookieWarning() string {
	if !c.PortalEnabled || c.PortalCookieSecure || isLoopbackListen(c.Addr) {
		return ""
	}
	return fmt.Sprintf("WARNING: portal listens on %s with cookie_secure=false; session cookies will be sent over plain HTTP\n"+
		"WARNING: serve the portal over HTTPS and enable Secure cookies, or bind to a loopback address for local use", c.Addr)
}

// isLoopbackListen reports whether addr only accepts local connections. An
// empty or wildcard host listens on every interface and is not loopback.
func isLoopbackListen(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
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
		{"portal.session_ttl", file.Portal.SessionTTL, &cfg.PortalSessionTTL},
		{"linx.provision_timeout", file.Linx.ProvisionTimeout, &cfg.LinxProvisionTimeout},
		{"linx.token_poll_interval", file.Linx.TokenPollInterval, &cfg.LinxTokenPollInterval},
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
	if file.Portal.Enabled {
		cfg.PortalEnabled = true
	}
	if file.Portal.DataDir != "" {
		cfg.PortalDataDir = file.Portal.DataDir
	}
	if file.Portal.WebDir != "" {
		cfg.PortalWebDir = file.Portal.WebDir
	}
	if file.Portal.WebEmbedded {
		cfg.PortalWebEmbedded = true
	}
	if file.Portal.Username != "" {
		cfg.PortalUsername = file.Portal.Username
	}
	if file.Portal.Password != "" {
		cfg.PortalPassword = file.Portal.Password
	}
	if file.Portal.CookieSecure != nil {
		cfg.PortalCookieSecure = *file.Portal.CookieSecure
	}
	if file.Portal.AllowRegistration {
		cfg.PortalAllowRegistration = true
	}
	if file.Services.AllowPrivateTargets {
		cfg.AllowPrivateTargets = true
	}
	if file.Linx.APIBase != "" {
		cfg.LinxAPIBase = file.Linx.APIBase
	}
	if file.Linx.OTAURL != "" {
		cfg.LinxOTAURL = file.Linx.OTAURL
	}
	if file.Linx.APIKey != "" {
		cfg.LinxAPIKey = file.Linx.APIKey
	}
	if file.Linx.PublicMCPURL != "" {
		cfg.LinxPublicMCPURL = file.Linx.PublicMCPURL
	}
	if file.Linx.MCPName != "" {
		cfg.LinxMCPName = file.Linx.MCPName
	}
	if file.Linx.AssistantName != "" {
		cfg.LinxAssistantName = file.Linx.AssistantName
	}
	if file.Linx.LLMModelID != "" {
		cfg.LinxLLMModelID = file.Linx.LLMModelID
	}
	if file.Linx.MemModelID != "" {
		cfg.LinxMemModelID = file.Linx.MemModelID
	}
	if file.Linx.IntentModelID != "" {
		cfg.LinxIntentModelID = file.Linx.IntentModelID
	}
	if file.Linx.SystemPromptFile != "" {
		cfg.LinxSystemPromptFile = file.Linx.SystemPromptFile
	}
	if file.Linx.TokenPollAttempts < 0 {
		return fmt.Errorf("gateway: linx.token_poll_attempts must not be negative")
	}
	if file.Linx.TokenPollAttempts > 0 {
		cfg.LinxTokenPollAttempts = file.Linx.TokenPollAttempts
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

func envBool(getenv func(string) string, key string, fallback bool) bool {
	value := strings.TrimSpace(getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}
