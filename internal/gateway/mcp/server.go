// Package mcp exposes the control plane to Linx through the official MCP Go
// SDK's SSE transport.
package mcp

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/service"
)

// Apps is the application service surface used by the tools.
type Apps interface {
	GetScreens(ctx context.Context, owner app.Identity) ([]service.ScreenView, error)
	CreateApp(ctx context.Context, owner app.Identity, prompt string, playerNames []string) (service.CreateResult, error)
	UpdateApp(ctx context.Context, owner app.Identity, appID, prompt string) (service.CreateResult, error)
	GetAppStatus(ctx context.Context, owner app.Identity, appID string) (service.StatusView, error)
	GetAppOperations(ctx context.Context, owner app.Identity, appID string) (service.OperationsView, error)
	StartApp(ctx context.Context, owner app.Identity, appID, screenID string, force bool) (service.StatusView, error)
	StopApp(ctx context.Context, owner app.Identity, appID string) (service.StatusView, error)
	SendRequest(ctx context.Context, owner app.Identity, appID, operationID string, parameters map[string]any, body any) (service.OperationResult, error)
}

const (
	serverName         = "open-anywhere-gateway"
	defaultTimeout     = 2 * time.Minute
	serverInstructions = "Open Anywhere 用于让应用在身边屏幕上生成并运行。创建应用是异步任务：create-app 后先告诉用户正在制作，再用 get-app-status 查询；构建完成会发送 notifications/app_status。修改已有应用用 update-app，不要重新 create-app。可以同时创建多个应用，用返回的 app_id 区分。启动前先用 get-screens 获取明确的 screen_id。用户想再玩之前做过的应用时，先用 list-apps 找到它，不要重新创建。调用应用业务接口前，必须先用 get-app-operations 获取 operation_id、参数和请求体结构，再用 send-request。只对用户说应用和屏幕的自然名称，不要念内部标识。"
)

// OwnerValidator checks that an MCP identity maps to a registered device of an
// active user. The portal wires the endpoints resolver here; a single-tenant
// deployment leaves it nil.
type OwnerValidator interface {
	ValidateOwner(ctx context.Context, owner app.Identity) error
}

// Server owns one MCP server per SSE connection and routes Linx's POST
// messages to the matching SDK transport.
type Server struct {
	tools       Apps
	callTimeout time.Duration
	version     string
	validator   OwnerValidator

	mu            sync.Mutex
	sessions      map[string]*session
	logf          func(string, ...any)
	Compatibility bool
	SpeakerProbe  bool
}

type session struct {
	identity  app.Identity
	transport *sdk.SSEServerTransport
	cancel    context.CancelFunc

	// mu guards connection and sdk, which are attached after the session is
	// registered but before server.Connect returns.
	mu         sync.Mutex
	connection sdk.Connection
	sdk        *sdk.ServerSession
}

func (s *session) attach(sdkSession *sdk.ServerSession, connection sdk.Connection) {
	s.mu.Lock()
	s.sdk = sdkSession
	s.connection = connection
	s.mu.Unlock()
}

func (s *session) close() {
	s.mu.Lock()
	sdkSession := s.sdk
	s.mu.Unlock()
	if sdkSession != nil {
		_ = sdkSession.Close()
	}
}

// write sends a message to the client. Sessions registered but not yet
// connected have no client to deliver to, so the message is dropped.
func (s *session) write(ctx context.Context, message jsonrpc.Message) error {
	s.mu.Lock()
	connection := s.connection
	s.mu.Unlock()
	if connection == nil {
		return nil
	}
	return connection.Write(ctx, message)
}

// New creates an MCP server. SetTools may be called later to break the wiring
// cycle between the server and the application service.
func New(tools Apps, callTimeout time.Duration) *Server {
	if callTimeout <= 0 {
		callTimeout = defaultTimeout
	}
	return &Server{
		tools:       tools,
		callTimeout: callTimeout,
		version:     "0.1.0",
		sessions:    make(map[string]*session),
		logf:        log.Printf,
	}
}

// SetTools installs the application service.
func (s *Server) SetTools(tools Apps) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools = tools
}

// SetOwnerValidator requires every MCP connection to carry the three Linx
// identity headers and pass the validator. Once installed, missing identity is
// rejected instead of silently treated as an anonymous single tenant.
func (s *Server) SetOwnerValidator(validator OwnerValidator) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.validator = validator
}

func (s *Server) ownerValidator() OwnerValidator {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.validator
}

// Close terminates every SSE session so HTTP shutdown can complete.
func (s *Server) Close() {
	s.mu.Lock()
	sessions := make([]*session, 0, len(s.sessions))
	for _, item := range s.sessions {
		sessions = append(sessions, item)
	}
	s.sessions = make(map[string]*session)
	s.mu.Unlock()
	for _, item := range sessions {
		item.cancel()
		item.close()
	}
}

// ServeSSE handles GET /mcp: it registers a session, binds a server to the
// connection identity, and streams messages until the client disconnects.
func (s *Server) ServeSSE(w http.ResponseWriter, r *http.Request) {
	if _, ok := w.(http.Flusher); !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	identity := app.Identity{
		DeviceID:  r.Header.Get("X-Linx-Device-Id"),
		AgentID:   r.Header.Get("X-Linx-Agent-Id"),
		SessionID: r.Header.Get("X-Linx-Session-Id"),
	}
	validator := s.ownerValidator()
	if validator != nil {
		if identity.DeviceID == "" || identity.AgentID == "" || identity.SessionID == "" {
			writeIdentityFailure(w, "MISSING_IDENTITY", http.StatusBadRequest)
			return
		}
		if err := validator.ValidateOwner(r.Context(), identity); err != nil {
			writeIdentityError(w, err)
			return
		}
	} else if identity.DeviceID == "" || identity.SessionID == "" {
		http.Error(w, "missing X-Linx-Device-Id or X-Linx-Session-Id", http.StatusBadRequest)
		return
	}
	tools := s.currentTools()
	if tools == nil {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	sessionID := rand.Text()
	transport := &sdk.SSEServerTransport{
		Endpoint: "/mcp/message?sessionid=" + url.QueryEscape(sessionID),
		Response: w,
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	server := sdk.NewServer(&sdk.Implementation{Name: serverName, Version: s.version}, &sdk.ServerOptions{
		Instructions: serverInstructions,
		Capabilities: &sdk.ServerCapabilities{Tools: &sdk.ToolCapabilities{ListChanged: false}},
	})
	server.AddReceivingMiddleware(withCallLog(s.logf, identity))
	addTools(server, tools, identity, s.callTimeout)
	if s.Compatibility {
		addCompatibility(server, tools, identity, s.SpeakerProbe)
	}
	binding := &connectionTransport{inner: transport}

	// Register before Connect: Connect writes and flushes the endpoint event,
	// so a client may POST to it before Connect returns. Registering afterwards
	// races with that first POST and serves it a 404.
	item := &session{
		identity:  identity,
		transport: transport,
		cancel:    cancel,
	}
	s.mu.Lock()
	s.sessions[sessionID] = item
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.sessions, sessionID)
		s.mu.Unlock()
	}()

	sdkSession, err := server.Connect(ctx, binding, nil)
	if err != nil {
		http.Error(w, "connection failed", http.StatusInternalServerError)
		return
	}
	item.attach(sdkSession, binding.connection)
	defer func() {
		_ = sdkSession.Close()
	}()

	<-ctx.Done()
}

// ServeMessage handles POST /mcp/message and hands the JSON-RPC message to
// the SDK transport that owns the session.
func (s *Server) ServeMessage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionid")
	if sessionID == "" {
		sessionID = r.URL.Query().Get("sessionId")
	}
	s.mu.Lock()
	item := s.sessions[sessionID]
	s.mu.Unlock()
	if item == nil {
		http.Error(w, "unknown session", http.StatusNotFound)
		return
	}
	validator := s.ownerValidator()
	for key, want := range map[string]string{"X-Linx-Device-Id": item.identity.DeviceID, "X-Linx-Agent-Id": item.identity.AgentID, "X-Linx-Session-Id": item.identity.SessionID} {
		got := r.Header.Get(key)
		if validator != nil {
			// With a validator every header must be present and exact; an empty
			// header must not bypass the check.
			if got == "" || got != want {
				writeIdentityFailure(w, "IDENTITY_MISMATCH", http.StatusForbidden)
				return
			}
			continue
		}
		// Single-tenant deployments predate the agent header, so a missing
		// value is tolerated there; a present but different value never is.
		if got != "" && got != want {
			http.Error(w, "session owner mismatch", http.StatusForbidden)
			return
		}
	}
	if validator != nil {
		if err := validator.ValidateOwner(r.Context(), item.identity); err != nil {
			writeIdentityError(w, err)
			return
		}
	}
	item.transport.ServeHTTP(w, r)
}

// writeIdentityError maps an owner-validation failure to the MCP HTTP status.
func writeIdentityError(w http.ResponseWriter, err error) {
	code := "IDENTITY_CHECK_FAILED"
	status := http.StatusServiceUnavailable
	var toolErr *service.ToolError
	if errors.As(err, &toolErr) && toolErr.Code != "" {
		code = toolErr.Code
		switch code {
		case "OWNER_NOT_FOUND", "IDENTITY_NOT_REGISTERED", "USER_DISABLED":
			status = http.StatusForbidden
		}
	}
	writeIdentityFailure(w, code, status)
}

func writeIdentityFailure(w http.ResponseWriter, code string, status int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code}})
}

// Notify sends notifications/app_status to matching Linx sessions.
func (s *Server) Notify(owner app.Identity, notification app.Notification) {
	params, err := json.Marshal(notification)
	if err != nil {
		return
	}
	message := &jsonrpc.Request{Method: "notifications/app_status", Params: params}
	s.mu.Lock()
	sessions := make([]*session, 0, len(s.sessions))
	for _, item := range s.sessions {
		if item.identity.SameOwner(owner) {
			sessions = append(sessions, item)
		}
	}
	s.mu.Unlock()
	s.logf("mcp: app_status app=%s state=%s subscribers=%d", notification.AppID, notification.State, len(sessions))
	for _, item := range sessions {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := item.write(ctx, message); err != nil {
			s.logf("mcp: notify %s: %v", notification.AppID, err)
		}
		cancel()
	}
}

func (s *Server) currentTools() Apps {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tools
}

// connectionTransport retains the SDK connection so the control plane can
// send its custom app_status notification without reimplementing JSON-RPC.
type connectionTransport struct {
	inner      sdk.Transport
	connection sdk.Connection
}

func (t *connectionTransport) Connect(ctx context.Context) (sdk.Connection, error) {
	connection, err := t.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	t.connection = connection
	return connection, nil
}

func (t *connectionTransport) SupportsProtocolVersion(version string) bool {
	if supporter, ok := t.inner.(sdk.ProtocolVersionSupporter); ok {
		return supporter.SupportsProtocolVersion(version)
	}
	return true
}
