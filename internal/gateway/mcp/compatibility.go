package mcp

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/service"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

type extendedApps interface {
	GetOpenAPI(context.Context, app.Identity, string) (map[string]any, error)
	ListApps(context.Context, app.Identity) ([]service.StatusView, error)
	ClaimApp(context.Context, app.Identity, string) (service.StatusView, error)
}

func addCompatibility(server *sdk.Server, tools Apps, identity app.Identity, probe, claim bool) {
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
		if claim {
			add("list-apps", "重新连接后查询同一设备和智能体创建的应用；选择 app_id 后用 claim-app 接管。", `{"type":"object","properties":{},"additionalProperties":false}`, func(ctx context.Context, b json.RawMessage) (any, error) {
				a, err := ext.ListApps(ctx, identity)
				return map[string]any{"apps": a}, err
			})
			add("claim-app", "同一设备和智能体的新会话显式接管应用，旧会话随后失去访问权。", id, func(ctx context.Context, b json.RawMessage) (any, error) {
				return ext.ClaimApp(ctx, identity, getID(b))
			})
		}
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
		add("probe_speaker", "测试声纹：传本轮平台声纹 speaker_name；speaker_id 未知传 null，不把姓名填作 ID。不从玩家列表或设备 ID 猜测。", `{"type":"object","properties":{"speaker_id":{"type":["string","null"]},"speaker_name":{"type":["string","null"]},"context_source":{"enum":["platform_voiceprint","unavailable"]}},"required":["speaker_id","speaker_name","context_source"],"additionalProperties":false}`, func(ctx context.Context, b json.RawMessage) (any, error) {
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
func (s *Server) StreamableHTTP(publicHost string, probe, claim bool) http.Handler {
	handler := sdk.NewStreamableHTTPHandler(func(r *http.Request) *sdk.Server {
		identity := identityFrom(r)
		if identity.DeviceID == "" || identity.SessionID == "" {
			return nil
		}
		tools := s.currentTools()
		if tools == nil {
			return nil
		}
		server := sdk.NewServer(&sdk.Implementation{Name: serverName, Version: "0.2.0"}, nil)
		server.AddReceivingMiddleware(withCallLog(s.logf, identity))
		addTools(server, tools, identity, s.callTimeout)
		addCompatibility(server, tools, identity, probe, claim)
		return server
	}, &sdk.StreamableHTTPOptions{Stateless: true, MaxRequestBodyBytes: 1 << 20, DisableLocalhostProtection: publicHost != ""})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if publicHost != "" && r.Host != publicHost && !strings.HasPrefix(r.Host, "127.0.0.1:") && !strings.HasPrefix(r.Host, "localhost:") {
			http.Error(w, "invalid host", 403)
			return
		}
		http.NewCrossOriginProtection().Handler(handler).ServeHTTP(w, r)
	})
}
