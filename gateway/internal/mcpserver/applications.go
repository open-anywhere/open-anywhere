package mcpserver

import (
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/open-anywhere/open-anywhere/gateway/internal/application"
	"github.com/open-anywhere/open-anywhere/gateway/internal/openapi"
)

func owner(r *mcp.CallToolRequest) application.Owner {
	if r.Extra == nil {
		return application.Owner{}
	}
	h := r.Extra.Header
	return application.Owner{Device: h.Get("X-Linx-Device-Id"), Agent: h.Get("X-Linx-Agent-Id"), Session: h.Get("X-Linx-Session-Id")}
}

type register func(string, string, string, mcp.ToolHandler)

func registerApplications(add register, apps *application.Manager) {
	wrap := func(fn func(context.Context, *mcp.CallToolRequest) (any, error)) mcp.ToolHandler {
		return func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			out, err := fn(ctx, r)
			if err != nil {
				return toolError(err), nil
			}
			return result(out, false), nil
		}
	}
	add("create-app", "创建应用并异步构建，立即返回 app_id。多人游戏传入已确认的 player_names，名字是玩家唯一身份，未知名字不要猜测，名单不表示准备完成。", `{"type":"object","properties":{"prompt":{"type":"string","minLength":1},"player_names":{"type":"array","items":{"type":"string","minLength":1},"uniqueItems":true}},"required":["prompt"],"additionalProperties":false}`, wrap(func(ctx context.Context, r *mcp.CallToolRequest) (any, error) {
		var a struct {
			Prompt  string   `json:"prompt"`
			Players []string `json:"player_names"`
		}
		_ = json.Unmarshal(r.Params.Arguments, &a)
		return apps.Create(owner(r), a.Prompt, a.Players)
	}))
	const idSchema = `{"type":"object","properties":{"app_id":{"type":"string","minLength":1}},"required":["app_id"],"additionalProperties":false}`
	add("get-app-status", "查询应用构建、启动、运行或失败状态；building 时可稍后查询。", idSchema, wrap(func(ctx context.Context, r *mcp.CallToolRequest) (any, error) {
		return apps.Status(ctx, owner(r), appID(r))
	}))
	add("get-screens", "查询本会话可用屏幕及占用；只能选择 available=true 且 current_app=null 的屏幕。", `{"type":"object","properties":{},"additionalProperties":false}`, wrap(func(ctx context.Context, r *mcp.CallToolRequest) (any, error) { return apps.Screens(ctx, owner(r)) }))
	add("start-app", "在选定 screen_id 上启动 idle 应用；只有应用健康且屏幕播放成功才返回 running。", `{"type":"object","properties":{"app_id":{"type":"string","minLength":1},"screen_id":{"type":"string","minLength":1}},"required":["app_id","screen_id"],"additionalProperties":false}`, wrap(func(ctx context.Context, r *mcp.CallToolRequest) (any, error) {
		var a struct {
			App    string `json:"app_id"`
			Screen string `json:"screen_id"`
		}
		_ = json.Unmarshal(r.Params.Arguments, &a)
		return apps.Start(ctx, owner(r), a.App, a.Screen)
	}))
	add("stop-app", "停止应用和关联投屏，等待资源清理；building 应用不可停止。", idSchema, wrap(func(ctx context.Context, r *mcp.CallToolRequest) (any, error) {
		return apps.Stop(ctx, owner(r), appID(r))
	}))
	add("get_openapi", "根据 app_id 获取应用注册的完整 OpenAPI；先分析规范再调用，不朗读文档。", idSchema, wrap(func(ctx context.Context, r *mcp.CallToolRequest) (any, error) {
		return apps.Document(owner(r), appID(r))
	}))
	add("send_request", "根据应用 OpenAPI 的 operationId 调用运行中的应用。只传 app_id、声明参数和 JSON body，不传 URL。应用使用 user_name 时直接传识别姓名，不生成用户 ID。", `{"type":"object","properties":{"app_id":{"type":"string","minLength":1},"operationId":{"type":"string","minLength":1},"parameters":{"type":"object"},"requestBody":{}},"required":["app_id","operationId"],"additionalProperties":false}`, func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var a struct {
			AppID      string          `json:"app_id"`
			Operation  string          `json:"operationId"`
			Parameters map[string]any  `json:"parameters"`
			Body       json.RawMessage `json:"requestBody"`
		}
		_ = json.Unmarshal(r.Params.Arguments, &a)
		out, err := apps.Send(ctx, owner(r), a.AppID, openapi.Arguments{OperationID: a.Operation, Parameters: a.Parameters, Body: a.Body})
		if err != nil {
			return toolError(err), nil
		}
		return result(out, !out.OK), nil
	})
}
func appID(r *mcp.CallToolRequest) string {
	var a struct {
		ID string `json:"app_id"`
	}
	_ = json.Unmarshal(r.Params.Arguments, &a)
	return a.ID
}
