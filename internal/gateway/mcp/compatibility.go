package mcp

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/service"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

type extendedApps interface {
	GetOpenAPI(context.Context, app.Identity, string) (map[string]any, error)
	ListApps(context.Context, app.Identity) ([]service.AppSummary, error)
}

func addCompatibility(server *sdk.Server, tools Apps, identity app.Identity, probe bool) {
	add := func(name, description, schema string, fn func(context.Context, json.RawMessage) (any, error)) {
		var input any
		_ = json.Unmarshal([]byte(schema), &input)
		compiler := jsonschema.NewCompiler()
		_ = compiler.AddResource("https://mcp.invalid/input", input)
		validator := compiler.MustCompile("https://mcp.invalid/input")
		server.AddTool(&sdk.Tool{Name: name, Description: description, InputSchema: input}, func(ctx context.Context, r *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			var args any
			err := json.Unmarshal(r.Params.Arguments, &args)
			if err == nil {
				err = validator.Validate(args)
			}
			var out any
			if err != nil {
				out = map[string]any{"error": map[string]string{"code": "INVALID_ARGUMENT", "message": "参数不符合工具定义"}}
			} else {
				out, err = fn(ctx, r.Params.Arguments)
				if err != nil {
					out = map[string]any{"error": err.Error()}
				}
			}
			failed := err != nil
			if m, ok := out.(map[string]any); ok && m["ok"] == false {
				failed = true
			}
			b, _ := json.Marshal(out)
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(b)}}, StructuredContent: out, IsError: failed}, nil
		})
	}
	ext, ok := tools.(extendedApps)
	if ok {
		const id = `{"type":"object","properties":{"app_id":{"type":"string","minLength":1}},"required":["app_id"],"additionalProperties":false}`
		getID := func(b json.RawMessage) string {
			var a struct {
				ID string `json:"app_id"`
			}
			_ = json.Unmarshal(b, &a)
			return a.ID
		}
		add("get_openapi", "读取应用完整 OpenAPI 文档，先分析再调用。", id, func(ctx context.Context, b json.RawMessage) (any, error) {
			return ext.GetOpenAPI(ctx, identity, getID(b))
		})
		add("list-apps", "列出你构建过的应用，最新的在前。历史最多保留最近 10 个，旧应用可能被挤掉。每项有 app_id、名字、标题、需求摘要、状态、操作列表 operations，以及 startable（能否用 start-app 启动）。用户说“玩上次的/之前那个”时先用它，选定后用 start-app 启动，需要时再用 get-app-operations 或 get_openapi 查看可调用的操作。", `{"type":"object","properties":{},"additionalProperties":false}`, func(ctx context.Context, b json.RawMessage) (any, error) {
			a, err := ext.ListApps(ctx, identity)
			return map[string]any{"apps": a}, err
		})
	}
	add("send_request", "兼容调用：按已读取 OpenAPI 的 operationId 执行应用操作。parameters 放参数，requestBody 放 JSON body。", `{"type":"object","properties":{"app_id":{"type":"string"},"operationId":{"type":"string"},"parameters":{"type":"object"},"requestBody":{}},"required":["app_id","operationId"],"additionalProperties":false}`, func(ctx context.Context, b json.RawMessage) (any, error) {
		var a struct {
			ID     string         `json:"app_id"`
			Op     string         `json:"operationId"`
			Params map[string]any `json:"parameters"`
			Body   any            `json:"requestBody"`
		}
		_ = json.Unmarshal(b, &a)
		out, err := tools.SendRequest(ctx, identity, a.ID, a.Op, a.Params, a.Body)
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": out.Status >= 200 && out.Status < 300, "status": out.Status, "data": out.Body}, nil
	})
	if probe {
		add("probe_speaker", "测试声纹：传本轮平台声纹 speaker_name；speaker_id 未知传 null，不把姓名填作 ID，也不要猜测。", `{"type":"object","properties":{"speaker_id":{"type":["string","null"]},"speaker_name":{"type":["string","null"]},"context_source":{"enum":["platform_voiceprint","unavailable"]}},"required":["speaker_id","speaker_name","context_source"],"additionalProperties":false}`, func(ctx context.Context, b json.RawMessage) (any, error) {
			var a any
			_ = json.Unmarshal(b, &a)
			log.Printf("mcp: probe_speaker received=%s", b)
			return map[string]any{"received": a, "verified": false}, nil
		})
	}
}
func identityFrom(r *http.Request) app.Identity {
	return app.Identity{DeviceID: r.Header.Get("X-Linx-Device-Id"), AgentID: r.Header.Get("X-Linx-Agent-Id"), SessionID: r.Header.Get("X-Linx-Session-Id")}
}

// StreamableHTTP preserves the deployed request/response transport. Server-initiated
// notifications are delivered over the separate SSE endpoint, not this stateless channel.
// StreamableHTTP serves /mcp and accepts one extra public Host. See
// StreamableHTTPHosts for the Host rules.
func (s *Server) StreamableHTTP(publicHost string, probe bool) http.Handler {
	return s.StreamableHTTPHosts(func(context.Context) []string {
		if publicHost == "" {
			return nil
		}
		return []string{publicHost}
	}, probe)
}

// HostsFromURL returns the Host header values a client sends when it calls
// the given URL: the host with its port, plus the bare host when the port is
// the scheme's default. It returns nil for an empty or unparsable URL.
func HostsFromURL(raw string) []string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return nil
	}
	hosts := []string{parsed.Host}
	port := parsed.Port()
	if (parsed.Scheme == "http" && port == "80") || (parsed.Scheme == "https" && port == "443") {
		hosts = append(hosts, parsed.Hostname())
	}
	return hosts
}

func isLoopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// hostAllowed decides whether a request may reach /mcp by its Host header.
// Loopback Hosts are always fine. When Hosts are configured, any other Host
// must be one of them. When none are configured, a request that arrives over
// a loopback connection with a foreign Host is refused, which is the DNS
// rebinding guard the SDK applies on its own; the guard is done here so the
// allowed Hosts can change while the gateway runs.
func hostAllowed(r *http.Request, allowed []string) bool {
	if isLoopbackHost(r.Host) {
		return true
	}
	for _, host := range allowed {
		if strings.EqualFold(r.Host, host) {
			return true
		}
	}
	if len(allowed) > 0 {
		return false
	}
	if local, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok && local != nil {
		return !isLoopbackHost(local.String())
	}
	return true
}

// hostRefusals remembers which refused Hosts were logged lately, so a client
// that retries every second does not fill the log.
type hostRefusals struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

const (
	hostRefusalLogInterval = time.Minute
	hostRefusalMaxHosts    = 64
	hostRefusalMaxLength   = 128
)

// note logs a refused Host unless the same one was logged within the last minute.
func (h *hostRefusals) note(logf func(string, ...any), host string) {
	if len(host) > hostRefusalMaxLength {
		host = host[:hostRefusalMaxLength] + "..."
	}
	now := time.Now()
	h.mu.Lock()
	if h.seen == nil || len(h.seen) >= hostRefusalMaxHosts {
		// A Host header is attacker-controlled; keep the table small.
		h.seen = make(map[string]time.Time)
	}
	last, logged := h.seen[host]
	if logged && now.Sub(last) < hostRefusalLogInterval {
		h.mu.Unlock()
		return
	}
	h.seen[host] = now
	h.mu.Unlock()
	logf("mcp: refused a request with Host %q; if this is the address the Linx cloud uses, "+
		"set it as the public MCP address in the admin settings or as public_host in gateway.yaml", host)
}

// StreamableHTTPHosts serves /mcp. hosts is asked on every request for the
// extra Host values to accept, so an address changed in the admin settings
// applies without a restart.
func (s *Server) StreamableHTTPHosts(hosts func(context.Context) []string, probe bool) http.Handler {
	refusals := &hostRefusals{}
	handler := sdk.NewStreamableHTTPHandler(func(r *http.Request) *sdk.Server {
		identity := identityFrom(r)
		if identity.DeviceID == "" || identity.SessionID == "" {
			return nil
		}
		if s.ownerValidator() != nil && identity.AgentID == "" {
			return nil
		}
		tools := s.currentTools()
		if tools == nil {
			return nil
		}
		server := sdk.NewServer(&sdk.Implementation{Name: serverName, Version: "0.2.0"}, nil)
		server.AddReceivingMiddleware(withCallLog(s.logf, identity))
		addTools(server, tools, identity, s.callTimeout)
		addCompatibility(server, tools, identity, probe)
		return server
	}, &sdk.StreamableHTTPOptions{Stateless: true, MaxRequestBodyBytes: 1 << 20, DisableLocalhostProtection: true})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hostAllowed(r, hosts(r.Context())) {
			refusals.note(s.logf, r.Host)
			http.Error(w, "invalid host", http.StatusForbidden)
			return
		}
		// The SSE transport validates the owner once per connection. Streamable
		// HTTP is stateless, so every request carries its own identity and must
		// pass the same check before the SDK creates a server.
		if validator := s.ownerValidator(); validator != nil {
			identity := identityFrom(r)
			if identity.DeviceID == "" || identity.AgentID == "" || identity.SessionID == "" {
				writeIdentityFailure(w, "MISSING_IDENTITY", http.StatusBadRequest)
				return
			}
			if err := validator.ValidateOwner(r.Context(), identity); err != nil {
				writeIdentityError(w, err)
				return
			}
		}
		http.NewCrossOriginProtection().Handler(handler).ServeHTTP(w, r)
	})
}
