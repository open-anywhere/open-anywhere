// Package mcpserver exposes the Gateway tools over MCP Streamable HTTP.
package mcpserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/open-anywhere/open-anywhere/gateway/internal/application"
	"github.com/open-anywhere/open-anywhere/gateway/internal/openapi"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

type Options struct {
	Applications *application.Manager
	PublicHost   string
	SpeakerProbe bool
	Token        string
	Logger       *slog.Logger
}

func result(value any, failed bool) *mcp.CallToolResult {
	b, _ := json.Marshal(value)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}, StructuredContent: value, IsError: failed}
}
func toolError(err error) *mcp.CallToolResult {
	code, message := "INTERNAL_ERROR", "Tool failed"
	var e *openapi.Error
	if errors.As(err, &e) {
		code, message = e.Code, e.Message
	}
	var appErr *application.Failure
	if errors.As(err, &appErr) {
		code, message = appErr.Code, appErr.Message
	}
	return result(map[string]any{"ok": false, "error": map[string]string{"code": code, "message": message}, "message": message}, true)
}
func invalid() *mcp.CallToolResult {
	return toolError(&openapi.Error{Code: "INVALID_PARAMETERS", Message: "Arguments do not match tool schema"})
}

func Handler(c *openapi.Controller, opts Options) http.Handler {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "open-anywhere-gateway", Version: "0.2.0"}, nil)
	add := func(name, description, schemaJSON string, handler mcp.ToolHandler) {
		var schema any
		if err := json.Unmarshal([]byte(schemaJSON), &schema); err != nil {
			panic(err)
		}
		compiler := jsonschema.NewCompiler()
		if err := compiler.AddResource("https://tools.invalid/input", schema); err != nil {
			panic(err)
		}
		compiled, err := compiler.Compile("https://tools.invalid/input")
		if err != nil {
			panic(err)
		}
		server.AddTool(&mcp.Tool{Name: name, Description: description, InputSchema: schema}, func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args any
			if json.Unmarshal(r.Params.Arguments, &args) != nil || compiled.Validate(args) != nil {
				return invalid(), nil
			}
			out, err := handler(ctx, r)
			logger.Info("tool", "name", name, "is_error", out != nil && out.IsError)
			return out, err
		})
	}
	if opts.Applications != nil {
		registerApplications(add, opts.Applications)
	} else {
		add("get_openapi", "读取完整 OpenAPI 文档，分析后再按 operationId 调用。service_id 可选值："+strings.Join(c.IDs(), ", ")+"。文档供分析，不要朗读。", `{"type":"object","properties":{"service_id":{"type":"string"}},"required":["service_id"],"additionalProperties":false}`, func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var a struct {
				ID string `json:"service_id"`
			}
			_ = json.Unmarshal(r.Params.Arguments, &a)
			doc, err := c.Document(a.ID)
			if err != nil {
				return toolError(err), nil
			}
			return result(doc, false), nil
		})
		add("send_request", "根据已读取的 OpenAPI 调用 HTTP 操作。parameters 放声明的 path/query/header 参数；仅在接口声明 body 时传 requestBody。根据响应继续选择操作。播报 user_message/message，不朗读 judge_context 或标准答案。", `{"type":"object","properties":{"service_id":{"type":"string"},"operationId":{"type":"string"},"parameters":{"type":"object"},"requestBody":{}},"required":["service_id","operationId"],"additionalProperties":false}`, func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var a openapi.Arguments
			_ = json.Unmarshal(r.Params.Arguments, &a)
			out, err := c.Send(ctx, a)
			if err != nil {
				return toolError(err), nil
			}
			logger.Info("http_operation", "service", a.ServiceID, "operation", a.OperationID, "status", out.Status)
			return result(out, !out.OK), nil
		})

	}
	if opts.SpeakerProbe {
		add("probe_speaker", "声纹身份探针。用户说测试声纹或我是谁时调用。只从本轮平台声纹上下文读取 speaker_id（speakerId/speakid）和名字（name/speaker_name/speakerName），未知传 null。不要使用自报姓名、游戏玩家、设备 ID 或猜测。context_source 仅在明确收到平台声纹信息时填 platform_voiceprint，否则 unavailable。", `{"type":"object","properties":{"speaker_id":{"type":["string","null"],"minLength":1,"maxLength":256},"speaker_name":{"type":["string","null"],"minLength":1,"maxLength":256},"context_source":{"enum":["platform_voiceprint","unavailable"]}},"required":["speaker_id","speaker_name","context_source"],"additionalProperties":false}`, func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args map[string]any
			_ = json.Unmarshal(r.Params.Arguments, &args)
			headers := map[string]string{}
			if r.Extra != nil {
				for _, k := range []string{"x-linx-device-id", "x-linx-agent-id", "x-linx-session-id"} {
					if v := r.Extra.Header.Get(k); v != "" {
						headers[k] = v
					}
				}
			}
			out := map[string]any{"received": args, "verified": false, "transport_context": headers, "message": "已记录模型声纹参数，未独立验证身份。"}
			logger.Info("probe_speaker", "received", args, "transport_context", headers)
			return result(out, false), nil
		})
	}
	transport := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, MaxRequestBodyBytes: openapi.Limit, DisableLocalhostProtection: opts.PublicHost != ""})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "runtime": "go", "services": serviceIDs(c), "application_management": opts.Applications != nil})
	})
	mux.Handle("/mcp", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if opts.Token != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+opts.Token)) != 1 {
			http.Error(w, "Unauthorized", 401)
			return
		}
		// An explicit reverse-proxy host replaces the SDK's localhost-only rule.
		// All other hosts and cross-origin requests remain rejected.
		if opts.PublicHost != "" {
			host, _, err := net.SplitHostPort(r.Host)
			if err != nil {
				host = r.Host
			}
			ip := net.ParseIP(host)
			if r.Host != opts.PublicHost && host != "localhost" && (ip == nil || !ip.IsLoopback()) {
				http.Error(w, "Forbidden host", 403)
				return
			}
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != r.Host || (u.Scheme != "http" && u.Scheme != "https") {
				http.Error(w, "Forbidden origin", 403)
				return
			}
		}
		transport.ServeHTTP(w, r)
	}))
	return mux
}

func serviceIDs(c *openapi.Controller) []string {
	if c == nil {
		return []string{}
	}
	return c.IDs()
}
