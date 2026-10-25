package portal

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/linx"
	"github.com/open-anywhere/open-anywhere/internal/gateway/store"
	devicelinx "github.com/open-anywhere/open-anywhere/internal/voiceentry/linx"
)

// ProvisionConfig carries everything the device provisioner needs.
type ProvisionConfig struct {
	DataDir           string
	MCPName           string
	MCPDescription    string
	PublicMCPURL      string
	MCPToken          string
	AssistantName     string
	LLMModelID        string
	MemModelID        string
	IntentModelID     string
	SystemPrompt      string
	ProvisionTimeout  time.Duration
	TokenPollInterval time.Duration
	TokenPollAttempts int

	// Template, when set, supplies the agent and MCP parameters for every run
	// so an administrator's change applies without a restart. Non-empty
	// values replace the fixed fields above.
	Template func(context.Context) (Template, error)
}

// Template is the part of ProvisionConfig an administrator can change at
// runtime: how the shared MCP is registered and how new agents are created.
type Template struct {
	MCPName       string
	PublicMCPURL  string
	MCPToken      string
	AssistantName string
	LLMModelID    string
	MemModelID    string
	IntentModelID string
}

const (
	settingMCPServer   = "linx.mcp_server_id"
	settingMCPInstance = "linx.mcp_instance_id"

	// deviceErrorCodePlatformNotConfigured is stored in device.Error so the
	// API can expose a stable code; the view localizes it for people.
	deviceErrorCodePlatformNotConfigured = "PLATFORM_NOT_CONFIGURED"

	stepCreateAgent = "create_agent"
	stepAttachMCP   = "attach_mcp"
	stepOTA         = "ota"
	stepBind        = "bind"
	stepToken       = "token"
	stepDone        = "done"
)

// LinxProvider returns the current Linx management client. The portal obtains
// it from platform settings on every provisioning run so an administrator
// change applies without a restart.
type LinxProvider func(context.Context) (*linx.Client, error)

// Provisioner turns a portal device into a Linx agent, MCP binding and
// registered device credentials. It never runs two iterations for one device.
type Provisioner struct {
	db    *store.DB
	linx  LinxProvider
	voice *VoiceManager
	cfg   ProvisionConfig
	ctx   context.Context

	mu      sync.Mutex
	running map[string]bool
	mcpMu   sync.Mutex
	// token is the last MCP token seen, kept only to scrub it from messages.
	token atomic.Value
	sem     chan struct{}
	logf    func(string, ...any)
}

// NewProvisioner returns a provisioner bound to one fixed Linx client.
func NewProvisioner(ctx context.Context, db *store.DB, client *linx.Client, voice *VoiceManager, cfg ProvisionConfig) *Provisioner {
	return NewProvisionerWithProvider(ctx, db, func(context.Context) (*linx.Client, error) {
		return client, nil
	}, voice, cfg)
}

// NewProvisionerWithProvider returns a provisioner that resolves the Linx
// client for every run.
func NewProvisionerWithProvider(ctx context.Context, db *store.DB, provider LinxProvider, voice *VoiceManager, cfg ProvisionConfig) *Provisioner {
	if cfg.ProvisionTimeout <= 0 {
		cfg.ProvisionTimeout = 60 * time.Second
	}
	if cfg.TokenPollInterval <= 0 {
		cfg.TokenPollInterval = 2 * time.Second
	}
	if cfg.TokenPollAttempts <= 0 {
		cfg.TokenPollAttempts = 10
	}
	if cfg.MCPName == "" {
		cfg.MCPName = "Open Anywhere"
	}
	if cfg.AssistantName == "" {
		cfg.AssistantName = "小灵"
	}
	return &Provisioner{
		db:      db,
		linx:    provider,
		voice:   voice,
		cfg:     cfg,
		ctx:     ctx,
		running: make(map[string]bool),
		sem:     make(chan struct{}, 4),
		logf:    log.Printf,
	}
}

// CredentialPath resolves the stored relative credential path.
func (p *Provisioner) CredentialPath(device store.PortalDevice) string {
	if device.CredentialPath == "" {
		return filepath.Join(p.cfg.DataDir, "devices", device.ID+".json")
	}
	if filepath.IsAbs(device.CredentialPath) {
		return device.CredentialPath
	}
	return filepath.Join(p.cfg.DataDir, device.CredentialPath)
}

// Start launches provisioning in the background. It returns false when the
// device is already being provisioned.
func (p *Provisioner) Start(deviceID string) bool {
	p.mu.Lock()
	if p.running[deviceID] {
		p.mu.Unlock()
		return false
	}
	p.running[deviceID] = true
	p.mu.Unlock()
	go func() {
		defer func() {
			p.mu.Lock()
			delete(p.running, deviceID)
			p.mu.Unlock()
		}()
		select {
		case p.sem <- struct{}{}:
			defer func() { <-p.sem }()
		case <-p.ctx.Done():
			return
		}
		ctx, cancel := context.WithTimeout(p.ctx, p.cfg.ProvisionTimeout)
		defer cancel()
		if err := p.Run(ctx, deviceID); err != nil {
			p.logf("portal: provision device %s: %v", deviceID, err)
		}
	}()
	return true
}

// Running reports whether a provisioning iteration is in flight.
func (p *Provisioner) Running(deviceID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running[deviceID]
}

// Run advances one device as far as it can. Every step persists its result
// before moving on, so a retry resumes from the first unfinished step.
func (p *Provisioner) Run(ctx context.Context, deviceID string) error {
	device, err := p.db.PortalDeviceByID(ctx, deviceID)
	if err != nil {
		return fmt.Errorf("portal: load device: %w", err)
	}
	if device.Kind != store.PortalDeviceVirtual {
		return nil
	}
	if device.State == store.PortalDeviceRegistered {
		return p.startVoice(ctx, device)
	}
	device.State = store.PortalDeviceProvisioning
	device.Error = ""
	if err := p.save(ctx, &device); err != nil {
		return err
	}

	client, err := p.client(ctx)
	if err != nil {
		return p.fail(ctx, &device, device.Step, "", err)
	}
	if err := p.ensureAgent(ctx, client, &device); err != nil {
		return err
	}
	if err := p.ensureMCP(ctx, client, &device); err != nil {
		return err
	}
	if err := p.ensureDeviceToken(ctx, client, &device); err != nil {
		return err
	}
	if err := p.startVoice(ctx, device); err != nil {
		// Registration itself succeeded; the runtime can be retried without
		// touching the platform.
		p.logf("portal: start voice for %s: %v", device.ID, err)
	}
	return nil
}

// template returns the effective agent and MCP parameters. With a dynamic
// source, the MCP address and token must both be configured.
func (p *Provisioner) template(ctx context.Context) (Template, error) {
	result := Template{
		MCPName:       p.cfg.MCPName,
		PublicMCPURL:  p.cfg.PublicMCPURL,
		MCPToken:      p.cfg.MCPToken,
		AssistantName: p.cfg.AssistantName,
		LLMModelID:    p.cfg.LLMModelID,
		MemModelID:    p.cfg.MemModelID,
		IntentModelID: p.cfg.IntentModelID,
	}
	if p.cfg.Template != nil {
		dynamic, err := p.cfg.Template(ctx)
		if err != nil {
			return Template{}, err
		}
		for _, field := range []struct {
			value  string
			target *string
		}{
			{dynamic.MCPName, &result.MCPName},
			{dynamic.PublicMCPURL, &result.PublicMCPURL},
			{dynamic.MCPToken, &result.MCPToken},
			{dynamic.AssistantName, &result.AssistantName},
			{dynamic.LLMModelID, &result.LLMModelID},
			{dynamic.MemModelID, &result.MemModelID},
			{dynamic.IntentModelID, &result.IntentModelID},
		} {
			if field.value != "" {
				*field.target = field.value
			}
		}
		if result.PublicMCPURL == "" || result.MCPToken == "" {
			return Template{}, app.ErrPlatformNotConfigured
		}
	}
	p.token.Store(result.MCPToken)
	return result, nil
}

// SyncMCP pushes the current MCP address and token to the shared Linx MCP
// instance, if one has been created. It is a no-op before the first
// provisioning run.
func (p *Provisioner) SyncMCP(ctx context.Context) error {
	client, err := p.client(ctx)
	if err != nil {
		return err
	}
	tmpl, err := p.template(ctx)
	if err != nil {
		return err
	}
	p.mcpMu.Lock()
	defer p.mcpMu.Unlock()
	_, instanceID, err := p.mcpIDs(ctx)
	if err != nil || instanceID == "" {
		return err
	}
	return client.UpdateMCPInstance(ctx, instanceID, linx.MCPSpec{
		Name:        tmpl.MCPName,
		URL:         tmpl.PublicMCPURL,
		Token:       tmpl.MCPToken,
		Description: p.cfg.MCPDescription,
	})
}

// client resolves the current Linx client, rejecting an unconfigured platform.
func (p *Provisioner) client(ctx context.Context) (*linx.Client, error) {
	if p.linx == nil {
		return nil, app.ErrPlatformNotConfigured
	}
	client, err := p.linx(ctx)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, app.ErrPlatformNotConfigured
	}
	return client, nil
}

func (p *Provisioner) ensureAgent(ctx context.Context, client *linx.Client, device *store.PortalDevice) error {
	if device.LinxAgentID != "" {
		return nil
	}
	device.Step = stepCreateAgent
	if err := p.save(ctx, device); err != nil {
		return err
	}
	user, err := p.db.PortalUserByID(ctx, device.UserID)
	if err != nil {
		return p.fail(ctx, device, stepCreateAgent, "无法读取用户信息", err)
	}
	tmpl, err := p.template(ctx)
	if err != nil {
		return p.fail(ctx, device, stepCreateAgent, "", err)
	}
	name := "Open Anywhere · " + user.Username
	agentID, err := client.CreateAgent(ctx, linx.AgentRequest{
		Name:          name,
		AssistantName: tmpl.AssistantName,
		SystemPrompt:  p.cfg.SystemPrompt,
		LLMModelID:    tmpl.LLMModelID,
		MemModelID:    tmpl.MemModelID,
		IntentModelID: tmpl.IntentModelID,
	})
	if err != nil {
		return p.fail(ctx, device, stepCreateAgent, "", err)
	}
	device.LinxAgentID = agentID
	device.AgentName = name
	device.Step = stepAttachMCP
	return p.save(ctx, device)
}

func (p *Provisioner) ensureMCP(ctx context.Context, client *linx.Client, device *store.PortalDevice) error {
	device.Step = stepAttachMCP
	if err := p.save(ctx, device); err != nil {
		return err
	}
	tmpl, err := p.template(ctx)
	if err != nil {
		return p.fail(ctx, device, stepAttachMCP, "", err)
	}
	p.mcpMu.Lock()
	serverID, instanceID, err := p.mcpIDs(ctx)
	if err == nil && (serverID == "" || instanceID == "") {
		spec := linx.MCPSpec{
			Name:        tmpl.MCPName,
			URL:         tmpl.PublicMCPURL,
			Token:       tmpl.MCPToken,
			Description: p.cfg.MCPDescription,
		}
		serverID, instanceID, err = client.EnsureMCP(ctx, spec)
		if err == nil {
			if saveErr := p.db.SetPortalSetting(ctx, settingMCPServer, serverID); saveErr != nil {
				err = saveErr
			}
			if saveErr := p.db.SetPortalSetting(ctx, settingMCPInstance, instanceID); saveErr != nil {
				err = saveErr
			}
		}
	}
	p.mcpMu.Unlock()
	if err != nil {
		return p.fail(ctx, device, stepAttachMCP, "", err)
	}
	if err := client.AttachMCP(ctx, device.LinxAgentID, []string{instanceID}); err != nil {
		return p.fail(ctx, device, stepAttachMCP, "", err)
	}
	device.Step = stepOTA
	return p.save(ctx, device)
}

func (p *Provisioner) mcpIDs(ctx context.Context) (string, string, error) {
	serverID, err := p.db.PortalSetting(ctx, settingMCPServer)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", "", err
	}
	instanceID, err := p.db.PortalSetting(ctx, settingMCPInstance)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", "", err
	}
	return serverID, instanceID, nil
}

func (p *Provisioner) ensureDeviceToken(ctx context.Context, client *linx.Client, device *store.PortalDevice) error {
	identity := devicelinx.Device{MAC: device.MAC, ClientID: device.ClientID}
	device.Step = stepOTA
	if err := p.save(ctx, device); err != nil {
		return err
	}
	response, err := client.ReportOTA(ctx, identity)
	if err != nil {
		return p.fail(ctx, device, stepOTA, "", err)
	}
	if hasToken(response) {
		return p.finish(ctx, device, identity, response)
	}
	if response.Activation == nil || response.Activation.Code == "" {
		return p.fail(ctx, device, stepOTA, "平台没有返回激活码或凭证", nil)
	}
	device.Step = stepBind
	if err := p.save(ctx, device); err != nil {
		return err
	}
	if err := client.BindDevice(ctx, device.LinxAgentID, response.Activation.Code); err != nil {
		return p.fail(ctx, device, stepBind, "激活码或智能体无效，请重试", err)
	}
	device.Step = stepToken
	if err := p.save(ctx, device); err != nil {
		return err
	}
	for attempt := 0; attempt < p.cfg.TokenPollAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return p.fail(ctx, device, stepToken, "设备注册超时，可重试", ctx.Err())
		case <-time.After(p.cfg.TokenPollInterval):
		}
		response, err = client.ReportOTA(ctx, identity)
		if err != nil {
			return p.fail(ctx, device, stepToken, "", err)
		}
		if hasToken(response) {
			return p.finish(ctx, device, identity, response)
		}
	}
	return p.fail(ctx, device, stepToken, "设备注册超时，可重试", nil)
}

func (p *Provisioner) finish(ctx context.Context, device *store.PortalDevice, identity devicelinx.Device, response devicelinx.OTAResponse) error {
	identity.Token = response.WebSocket.Token
	identity.WSURL = response.WebSocket.URL
	relative := filepath.Join("devices", device.ID+".json")
	path := filepath.Join(p.cfg.DataDir, relative)
	if err := saveCredential(path, identity); err != nil {
		return p.fail(ctx, device, stepToken, "保存设备凭证失败", err)
	}
	now := time.Now().UTC()
	device.CredentialPath = relative
	device.State = store.PortalDeviceRegistered
	device.Step = stepDone
	device.Error = ""
	device.RegisteredAt = &now
	if err := p.save(ctx, device); err != nil {
		return err
	}
	p.logf("portal: device %s registered (agent %s)", device.ID, device.LinxAgentID)
	return nil
}

func (p *Provisioner) save(ctx context.Context, device *store.PortalDevice) error {
	device.UpdatedAt = time.Now().UTC()
	if err := p.db.UpdatePortalDevice(ctx, *device); err != nil {
		return fmt.Errorf("portal: save device: %w", err)
	}
	return nil
}

// fail records a provisioning failure and returns the mapped error. The
// platform's own message is kept only when it is safe and useful.
func (p *Provisioner) fail(ctx context.Context, device *store.PortalDevice, step, message string, cause error) error {
	if message == "" {
		message = p.describeError(cause)
	}
	device.State = store.PortalDeviceError
	device.Step = step
	device.Error = message
	if err := p.save(ctx, device); err != nil {
		return err
	}
	if cause == nil {
		return errors.New(message)
	}
	return fmt.Errorf("%s: %w", message, cause)
}

// describeError maps platform failures to text a user can act on.
func (p *Provisioner) describeError(err error) string {
	if err == nil {
		return "设备注册失败，请重试"
	}
	switch {
	case errors.Is(err, app.ErrPlatformNotConfigured):
		return deviceErrorCodePlatformNotConfigured
	case linx.IsAuth(err):
		return "平台鉴权失败，请联系维护者"
	case linx.IsUnavailable(err):
		return "平台暂时不可用，请稍后重试"
	}
	var apiErr *linx.Error
	if errors.As(err, &apiErr) && apiErr.Message != "" {
		return p.scrub(apiErr.Message)
	}
	return p.scrub(err.Error())
}

func (p *Provisioner) scrub(message string) string {
	secrets := []string{p.cfg.MCPToken}
	if token, ok := p.token.Load().(string); ok {
		secrets = append(secrets, token)
	}
	for _, secret := range secrets {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "***")
		}
	}
	return truncate(message, 300)
}

func (p *Provisioner) withCredential(device store.PortalDevice) store.PortalDevice {
	device.CredentialPath = p.CredentialPath(device)
	return device
}

// startVoice launches a device runtime only while its owning account is active.
// A disabled account keeps its device and credentials, but no voice runtime.
func (p *Provisioner) startVoice(ctx context.Context, device store.PortalDevice) error {
	if p.voice == nil {
		return nil
	}
	user, err := p.db.PortalUserByID(ctx, device.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		p.logf("portal: skip voice for %s: owning user is missing", device.ID)
		return nil
	}
	if err != nil {
		return fmt.Errorf("portal: load device owner: %w", err)
	}
	if user.Status != store.PortalUserActive {
		p.logf("portal: skip voice for %s: user %s is %s", device.ID, user.ID, user.Status)
		return nil
	}
	if err := p.voice.Start(p.withCredential(device)); err != nil {
		return err
	}
	// The account may have been disabled while the runtime was starting. Stop
	// it again so a concurrent disable cannot leave voice running.
	if user, err = p.db.PortalUserByID(ctx, device.UserID); err == nil && user.Status != store.PortalUserActive {
		p.voice.Stop(device.ID)
	}
	return nil
}

// Recover restores voice runtimes after a restart. Registering is retried only
// by an explicit user action, never automatically.
func (p *Provisioner) Recover(ctx context.Context) error {
	devices, err := p.db.PortalDevicesByState(ctx, store.PortalDeviceRegistered)
	if err != nil {
		return err
	}
	for _, device := range devices {
		path := p.CredentialPath(device)
		if _, err := devicelinx.LoadDevice(path); err != nil {
			device.State = store.PortalDeviceError
			device.Step = stepToken
			device.Error = "设备凭证缺失或损坏，请重试"
			device.UpdatedAt = time.Now().UTC()
			if saveErr := p.db.UpdatePortalDevice(ctx, device); saveErr != nil {
				return saveErr
			}
			continue
		}
		if err := p.startVoice(ctx, device); err != nil {
			p.logf("portal: restore voice for %s: %v", device.ID, err)
		}
	}
	// A crash between writing credentials and marking the device registered is
	// repaired on the next start.
	for _, state := range []string{store.PortalDeviceProvisioning, store.PortalDeviceError} {
		devices, err := p.db.PortalDevicesByState(ctx, state)
		if err != nil {
			return err
		}
		for _, device := range devices {
			path := p.CredentialPath(device)
			if _, err := devicelinx.LoadDevice(path); err != nil {
				continue
			}
			device.State = store.PortalDeviceRegistered
			device.Step = stepDone
			device.Error = ""
			relative := filepath.Join("devices", device.ID+".json")
			device.CredentialPath = relative
			if device.RegisteredAt == nil {
				now := time.Now().UTC()
				device.RegisteredAt = &now
			}
			device.UpdatedAt = time.Now().UTC()
			if err := p.db.UpdatePortalDevice(ctx, device); err != nil {
				return err
			}
			if err := p.startVoice(ctx, device); err != nil {
				p.logf("portal: restore voice for %s: %v", device.ID, err)
			}
		}
	}
	// A crash can leave a device provisioning without credentials. Automatic
	// provisioning resumes those; a device in error is only retried explicitly.
	devices, err = p.db.PortalDevicesByState(ctx, store.PortalDeviceProvisioning)
	if err != nil {
		return err
	}
	for _, device := range devices {
		if _, loadErr := devicelinx.LoadDevice(p.CredentialPath(device)); loadErr == nil {
			continue
		}
		if !p.Start(device.ID) {
			continue
		}
		p.logf("portal: resume provisioning for %s", device.ID)
	}
	return nil
}

// NewDeviceIdentity returns a fresh virtual device MAC and client id.
func NewDeviceIdentity() (devicelinx.Device, error) { return devicelinx.NewIdentity() }

func hasToken(response devicelinx.OTAResponse) bool {
	return response.WebSocket != nil && response.WebSocket.Token != "" && response.WebSocket.URL != ""
}

func saveCredential(path string, device devicelinx.Device) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(device, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".credential-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(append(encoded, '\n')); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}
