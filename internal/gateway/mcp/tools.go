package mcp

import (
	"context"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/open-anywhere/open-anywhere/internal/gateway/app"
	"github.com/open-anywhere/open-anywhere/internal/gateway/service"
)

// emptyInput is the argument type for tools that take no input.
type emptyInput struct{}

type createAppInput struct {
	Prompt      string   `json:"prompt" jsonschema:"整理后的中文应用需求，描述页面、交互和业务规则；不能为空"`
	PlayerNames []string `json:"player_names,omitempty" jsonschema:"用户已确认的初始玩家名称，按加入或点名顺序填写；未知时不要编造。名称不能为空白或重复"`
}

type appIDInput struct {
	AppID string `json:"app_id" jsonschema:"create-app 返回的应用 ID，仅当前 Linx 会话可访问"`
}

type startAppInput struct {
	AppID    string `json:"app_id" jsonschema:"create-app 返回的应用 ID，仅当前 Linx 会话可访问"`
	ScreenID string `json:"screen_id" jsonschema:"get-screens 返回的屏幕 ID"`
	Force    bool   `json:"force,omitempty" jsonschema:"默认 false。为 true 时强制投屏：先停止目标屏幕上正在展示的应用，再启动本应用"`
}

type sendRequestInput struct {
	AppID       string         `json:"app_id" jsonschema:"create-app 返回的应用 ID，仅当前 Linx 会话可访问"`
	OperationID string         `json:"operation_id" jsonschema:"get-app-operations 返回的 operation_id"`
	Parameters  map[string]any `json:"parameters,omitempty" jsonschema:"路径参数和查询参数，值只支持字符串、数字或布尔值；复杂结构请放进 body"`
	Body        any            `json:"body,omitempty" jsonschema:"可选 JSON 请求体，结构由 get-app-operations 的 request_body 描述"`
}

type screensOutput struct {
	Screens []service.ScreenView `json:"screens" jsonschema:"屏幕列表，允许为空数组"`
}

// addTools registers the Linx-facing tools with typed arguments, inferred
// schemas, validation, annotations and tool error conversion provided by the
// MCP SDK.
func addTools(server *sdk.Server, tools Apps, identity app.Identity, timeout time.Duration) {
	sdk.AddTool(server, &sdk.Tool{
		Name:        "get-screens",
		Title:       "查询可用屏幕",
		Description: "列出当前的屏幕以及每块屏幕正在展示的应用。用户询问有哪些屏幕、投到哪块屏幕，或调用 start-app 前使用。status 表示这块屏幕现在能做什么：free 可以直接启动；showing_app 被你的应用占用，换应用要先征得用户同意；external 被其他来源占用，不能接管，建议换一块屏幕；unavailable 当前不能显示应用。current_app 非 null 表示占用它的是你的哪个应用。",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, _ emptyInput) (*sdk.CallToolResult, screensOutput, error) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		screens, err := tools.GetScreens(ctx, identity)
		if err != nil {
			return nil, screensOutput{}, err
		}
		return nil, screensOutput{Screens: screens}, nil
	})

	sdk.AddTool(server, &sdk.Tool{
		Name:        "create-app",
		Title:       "创建应用",
		Description: "根据用户已确认的需求创建应用，并异步启动构建。prompt 必须是整理后的需求，包含页面、交互和规则，不要把原始语音转写直接传入。多人游戏请在 player_names 中传入用户已确认的初始玩家姓名，保留顺序；未知姓名不要猜测，名单不完整且需要预置全部玩家时先询问。名单只定义初始玩家，不代表已准备好。立即返回 app_id 和 building；之后用 get-app-status 查询进度，构建完成会收到 notifications/app_status。同一 Linx 会话已有构建中的应用时返回原 app_id，不会重复创建，也不会覆盖原构建的 prompt 或 player_names。",
		Annotations: mutatingAnnotations(false, true),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, input createAppInput) (*sdk.CallToolResult, service.CreateResult, error) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		result, err := tools.CreateApp(ctx, identity, input.Prompt, input.PlayerNames)
		if err != nil {
			return nil, service.CreateResult{}, err
		}
		return nil, result, nil
	})

	sdk.AddTool(server, &sdk.Tool{
		Name:        "get-app-status",
		Title:       "查询应用状态",
		Description: "查询应用的构建、空闲、运行或错误状态。create-app 之后用它轮询构建进度；收到 notifications/app_status 或用户询问时也用它确认最新状态。building 返回已用时间和最近进度；idle 表示可以启动；running 返回屏幕、启动时间和运行时长；error 返回结构化 error，不要只根据通知判断失败。",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, input appIDInput) (*sdk.CallToolResult, service.StatusView, error) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		result, err := tools.GetAppStatus(ctx, identity, input.AppID)
		if err != nil {
			return nil, service.StatusView{}, err
		}
		return nil, result, nil
	})

	sdk.AddTool(server, &sdk.Tool{
		Name:        "get-app-operations",
		Title:       "查询应用业务操作",
		Description: "返回已构建应用声明的业务操作，包括 operation_id、说明、路径/查询参数和 JSON 请求体 schema。调用 send-request 前必须先调用本工具，不要猜测 operation_id。应用仍在 building 时返回 APP_BUILDING，构建失败返回 APP_IN_ERROR。",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, input appIDInput) (*sdk.CallToolResult, service.OperationsView, error) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		result, err := tools.GetAppOperations(ctx, identity, input.AppID)
		if err != nil {
			return nil, service.OperationsView{}, err
		}
		return nil, result, nil
	})

	sdk.AddTool(server, &sdk.Tool{
		Name:        "start-app",
		Title:       "在屏幕上启动应用",
		Description: "把已构建完成的应用（idle，或上次启动失败但构建成功的应用）启动到指定屏幕，历史应用也可以直接启动，等待应用健康检查和投屏都就绪后返回 running。screen_id 必须来自 get-screens；status 为 external 或 unavailable 的屏幕不要选，showing_app 时只有用户明确同意替换才能传 force=true。一块屏幕同时只能展示一个应用；默认被其他应用占用返回 TARGET_BUSY。仅当用户明确要求替换或抢占屏幕时才传 force=true：它会先停止该屏幕上通过本工具启动的应用（返回里的 replaced_app_id 是被停止的应用），再启动本应用；停不掉时返回错误，不会覆盖；其他来源占用的屏幕不能被 force 接管。已在其他屏幕运行返回 APP_ALREADY_RUNNING。启动可能需要几十秒；失败时应用进入 error，并返回 error.stage=starting 的结构化原因。",
		Annotations: mutatingAnnotations(false, true),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, input startAppInput) (*sdk.CallToolResult, service.StatusView, error) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		result, err := tools.StartApp(ctx, identity, input.AppID, input.ScreenID, input.Force)
		if err != nil {
			return nil, service.StatusView{}, err
		}
		return nil, result, nil
	})

	sdk.AddTool(server, &sdk.Tool{
		Name:        "stop-app",
		Title:       "停止应用",
		Description: "停止运行中的应用，先断开投屏再停止应用进程，释放屏幕后返回 idle。已处于 idle 时幂等返回；应用仍在 building 时返回 APP_BUILDING；应用已处于 error 时幂等返回错误状态。停止失败返回 STOP_FAILED，不会假装已释放屏幕。",
		Annotations: mutatingAnnotations(true, true),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, input appIDInput) (*sdk.CallToolResult, service.StatusView, error) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		result, err := tools.StopApp(ctx, identity, input.AppID)
		if err != nil {
			return nil, service.StatusView{}, err
		}
		return nil, result, nil
	})

	sdk.AddTool(server, &sdk.Tool{
		Name:        "send-request",
		Title:       "调用应用业务接口",
		Description: "按 get-app-operations 返回的 operation_id 调用运行中应用的业务接口。路径参数放在 parameters 中，名称与路径模板一致；其余标量参数作为查询参数；JSON 请求体放在 body。只有 running 状态的应用可调用。返回应用原始 HTTP status 和解析后的 body；业务错误由应用通过状态码和 body 表达。",
		Annotations: mutatingAnnotations(true, false),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, input sendRequestInput) (*sdk.CallToolResult, service.OperationResult, error) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		result, err := tools.SendRequest(ctx, identity, input.AppID, input.OperationID, input.Parameters, input.Body)
		if err != nil {
			return nil, service.OperationResult{}, err
		}
		return nil, result, nil
	})
}

func readOnlyAnnotations() *sdk.ToolAnnotations {
	return &sdk.ToolAnnotations{
		ReadOnlyHint:    true,
		DestructiveHint: boolPointer(false),
		IdempotentHint:  true,
		OpenWorldHint:   boolPointer(true),
	}
}

func mutatingAnnotations(destructive, idempotent bool) *sdk.ToolAnnotations {
	return &sdk.ToolAnnotations{
		ReadOnlyHint:    false,
		DestructiveHint: boolPointer(destructive),
		IdempotentHint:  idempotent,
		OpenWorldHint:   boolPointer(true),
	}
}

func boolPointer(value bool) *bool { return &value }
