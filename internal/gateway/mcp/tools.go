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

type updateAppInput struct {
	AppID  string `json:"app_id" jsonschema:"要修订的应用 ID，来自 create-app 或 list-apps"`
	Prompt string `json:"prompt" jsonschema:"整理后的修改说明，描述要改的页面、交互或规则；不能为空"`
}

type appIDInput struct {
	AppID string `json:"app_id" jsonschema:"create-app 返回的正在创建中应用 ID"`
}

type startAppInput struct {
	AppID string `json:"app_id" jsonschema:"create-app 返回的正在创建中应用 ID"`
}

type sendRequestInput struct {
	AppID       string         `json:"app_id" jsonschema:"create-app 返回的应用 ID"`
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
		Description: "列出这台控制面连接的屏幕以及每块屏幕正在展示的应用，仅用于查询现状。投屏本身由现场设备的工具完成；不要为了 start-app 而调用本工具，它也不再决定应用投到哪里。status 表示这块屏幕现在能做什么：free 空闲；showing_app 被某个应用占用；external 被其他来源占用；unavailable 当前不能显示应用。",
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
		Description: "根据用户已确认的需求创建应用，并异步启动构建。prompt 必须是整理后的需求，包含页面、交互和规则，不要把原始语音转写直接传入。多人游戏请在 player_names 中传入用户已确认的初始玩家姓名，保留顺序；未知姓名不要猜测，名单不完整且需要预置全部玩家时先询问。名单只定义初始玩家，不代表已准备好。立即返回 app_id 和 building；之后用 get-app-status 查询进度，构建完成会收到 notifications/app_status。同一设备可同时有多个构建中的应用：每次调用都会新建 app_id 与构建会话。已返回的 app_id 用 get-app-status 跟踪；不要为同一意图盲目重试 create-app。修改已有应用用 update-app。",
		Annotations: mutatingAnnotations(false, false),
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
		Name:        "update-app",
		Title:       "修订已有应用",
		Description: "在已有应用的绑定构建会话上提交修改说明，异步重建产物。app_id 必填。prompt 为整理后的修改说明，不是原始语音。应用 building 时不要调用；running 时先 stop-app。新应用用 create-app；答题计分等运行中操作用 send-request。立即返回 building；完成后收到 notifications/app_status。会话不可用时返回 SESSION_UNAVAILABLE，需重新 create-app。",
		Annotations: mutatingAnnotations(false, false),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, input updateAppInput) (*sdk.CallToolResult, service.CreateResult, error) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		result, err := tools.UpdateApp(ctx, identity, input.AppID, input.Prompt)
		if err != nil {
			return nil, service.CreateResult{}, err
		}
		return nil, result, nil
	})

	sdk.AddTool(server, &sdk.Tool{
		Name:        "get-app-status",
		Title:       "查询应用状态",
		Description: "查询应用的构建、空闲、运行或错误状态。create-app 或 update-app 之后用它轮询构建进度；收到 notifications/app_status 或用户询问时也用它确认最新状态。building 返回已用时间和最近进度；idle 表示可以启动；running 返回 stream_url、启动时间和运行时长；error 返回结构化 error，不要只根据通知判断失败。",
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
		Title:       "启动应用",
		Description: "把已构建完成的应用（idle，或上次启动失败但构建成功的应用）在云端运行起来，历史应用也可以直接启动。就绪后返回 running 和 stream_url。本工具只负责让应用开始运行，不涉及任何屏幕：拿到 stream_url 后，投屏由现场设备的工具完成，只有用户明确要求投屏时才去调用它们。应用已经在运行时幂等返回当前状态。启动需要一些时间；失败时应用进入 error，并返回 error.stage=starting 的结构化原因。ARTIFACT_INVALID 表示产物本身无效，不要反复 start-app，改用 update-app 重做一版；连接或就绪超时类失败可以再次 start-app 重试。上次启动还在收尾时返回 CLEANUP_PENDING，稍后再试，不要重新创建应用。",
		Annotations: mutatingAnnotations(false, true),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, input startAppInput) (*sdk.CallToolResult, service.StatusView, error) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		result, err := tools.StartApp(ctx, identity, input.AppID)
		if err != nil {
			return nil, service.StatusView{}, err
		}
		return nil, result, nil
	})

	sdk.AddTool(server, &sdk.Tool{
		Name:        "stop-app",
		Title:       "停止应用",
		Description: "停止运行中的应用并释放云端运行时，返回 idle。已处于 idle 时幂等返回；应用仍在 building 时返回 APP_BUILDING；应用已处于 error 时幂等返回错误状态，若它是构建成功但启动失败的应用，会先清理上次启动遗留的内容并回到 idle。清理未完成返回 CLEANUP_PENDING，稍后再试。停止失败返回 STOP_FAILED，不会假装已释放运行时。现场屏幕上的画面需另外用设备工具停止。",
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

// mutatingAnnotations describes a write tool. create-app and update-app are
// not idempotent: repeating them creates another build instead of converging.
func mutatingAnnotations(destructive, idempotent bool) *sdk.ToolAnnotations {
	return &sdk.ToolAnnotations{
		ReadOnlyHint:    false,
		DestructiveHint: boolPointer(destructive),
		IdempotentHint:  idempotent,
		OpenWorldHint:   boolPointer(true),
	}
}

func boolPointer(value bool) *bool { return &value }
