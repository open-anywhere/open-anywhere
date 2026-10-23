<!-- 来源：https://developer-doc.qiniu.com/products/agentapi/api/13618-agentapi-api-conventions | 由 sync-docs.py 自动生成，请勿手改 -->

# API 通用约定



最近更新时间: 2026-10-13 17:21:31

本页供查阅公共 API 的请求约定、资源范围和事件类型。完整请求与响应字段以 [OpenAPI 规范](openapi.md)为准；首次接入的任务流程见[通过 API 构建](../quick-start/quickstart.md)，按能力使用接口的步骤见下表中的使用指南。

AgentAPI 公共 API 会随版本更新增加资源和字段。客户端应忽略响应中的未知字段，并处理错误响应和事件中可选字段缺失的情况。

## 请求约定

### 服务地址和认证

AgentAPI 服务地址按区域选择，并通过环境变量传入：

```bash
export AGENTAPI_BASE_URL="https://your-agentapi-endpoint"
export AGENTAPI_API_KEY="your-maas-api-key"
```

`/v1/*` 请求使用七牛 MaaS API Key 鉴权。推荐使用 `x-api-key` 请求头；JSON 请求还应设置 `content-type: application/json`。

```bash
curl --request GET "$AGENTAPI_BASE_URL/v1/agents" \
  --header "x-api-key: $AGENTAPI_API_KEY"
```

### 资源范围

AgentAPI 当前提供以下资源 API：

| 资源    | 主要路径                       | 使用指南                                                                                                                                                                                                                                                                        |
| ----- | -------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 智能体   | `/v1/agents`               | [创建与管理智能体](../development-guidelines/agent-configuration/agent.md)                                                                                                                                                 |
| 环境    | `/v1/environments`         | [配置云端环境](../development-guidelines/execution-environments/environment.md)、[自托管执行器](../development-guidelines/execution-environments/self-hosted.md) |
| 会话    | `/v1/sessions`             | [创建会话](../development-guidelines/sessions-and-tasks/session.md)、[会话操作](../development-guidelines/sessions-and-tasks/session-lifecycle.md)                   |
| 会话事件  | `/v1/sessions/{id}/events` | [事件和事件流](../development-guidelines/sessions-and-tasks/events.md)                                                                                                                                                    |
| 文件    | `/v1/files`                | [附加和下载文件](../development-guidelines/data-and-integrations/files.md)                                                                                                                                              |
| 技能    | `/v1/skills`               | [Agent Skills](../development-guidelines/agent-configuration/skills.md)                                                                                                                                            |
| 凭证保管库 | `/v1/vaults`               | [凭证管理](../development-guidelines/data-and-integrations/vaults.md)                                                                                                                                                |
| 持久记忆  | `/v1/memory_stores`        | [构建持久记忆](../development-guidelines/data-and-integrations/memory.md)                                                                                                                                              |
| 定时部署  | `/v1/deployments`          | [定时部署](../development-guidelines/sessions-and-tasks/scheduled-deployments.md)                                                                                                                                       |
| 部署运行  | `/v1/deployment_runs`      | [查询运行结果](../development-guidelines/sessions-and-tasks/scheduled-deployments.md)                                                                                                                                     |

### 响应和分页

* 成功响应使用 JSON；删除操作可能返回 `204 No Content`。
* 所有响应都包含 `X-Trace-ID`，可用于关联请求日志。
* 智能体、环境、会话、事件、技能、保管库、记忆和部署列表使用 `{ "data": [...], "next_page": "..." }`。将 `next_page` 原样作为下一次请求的 `page` 参数，不要自行解析或修改；没有更多数据时该字段为 `null`。
* Files API 使用另一种游标格式：`{ "data": [...], "has_more": true, "first_id": "...", "last_id": "...", "next_page": "..." }`。有下一页时优先把响应中的 `next_page` 原样作为 `page` 查询参数传回；兼容旧客户端的 `after_id` 和 `before_id` 仍可使用，但不能把 `last_id` 直接当作 `page`。
* 列表接口默认返回 20 条，具体上限以接口定义为准；Files API 的 `limit` 最大为 1,000，部署运行列表最多返回 1,000 条。
* 时间字段使用 RFC 3339 格式。

### 常见错误

| 状态码   | 含义                |
| ----- | ----------------- |
| `400` | 请求体、查询参数或事件格式无效   |
| `401` | API Key 缺失或无效     |
| `404` | 资源不存在，或不属于当前账户    |
| `409` | 资源状态不允许当前操作，例如已归档 |
| `503` | 依赖的运行时或服务暂时不可用    |

## 事件类型

会话事件通过 `POST /v1/sessions/{id}/events` 写入，通过事件列表或 SSE 接口读取。用户消息的结构化内容使用 `input.parts`，详见 [事件和事件流](../development-guidelines/sessions-and-tasks/events.md)。

### 用户事件

| 类型                        | 描述             |
| ------------------------- | -------------- |
| `user.message`            | 发送文本、图像或文件输入   |
| `user.interrupt`          | 中断当前执行         |
| `user.custom_tool_result` | 返回自定义工具的执行结果   |
| `user.tool_confirmation`  | 批准或拒绝需要确认的工具调用 |
| `user.define_outcome`     | 定义需要智能体完成的结果   |

### 智能体事件

| 类型                      | 描述           |
| ----------------------- | ------------ |
| `agent.message`         | 智能体输出        |
| `agent.thinking`        | 智能体正在处理的进度信号 |
| `agent.tool_use`        | 调用内置工具       |
| `agent.tool_result`     | 内置工具返回结果     |
| `agent.mcp_tool_use`    | 调用 MCP 工具    |
| `agent.mcp_tool_result` | MCP 工具返回结果   |
| `agent.custom_tool_use` | 调用应用提供的自定义工具 |

### 会话状态事件

| 类型                           | 描述            |
| ---------------------------- | ------------- |
| `session.status_running`     | 会话正在执行        |
| `session.status_idle`        | 当前轮次完成或等待用户操作 |
| `session.status_rescheduled` | 会话将在稍后重试      |
| `session.status_terminated`  | 会话已终止         |
| `session.deleted`            | 会话已删除         |
| `session.updated`            | 会话配置已更新       |
| `session.error`              | 会话执行发生错误      |

`session.status_idle` 表示当前轮次暂停或结束执行，不能作为任务成功的唯一依据。请读取其 `stop_reason.type`，确认停止原因：

| `stop_reason.type`  | 含义                                                  |
| ------------------- | --------------------------------------------------- |
| `end_turn`          | 正常完成当前轮次，或 `native` 本轮被中断；需结合 `user.interrupt` 事件区分 |
| `interrupted`       | `claudecode`、`codex` 本轮被 `user.interrupt` 中断        |
| `requires_action`   | 等待工具结果或工具确认；按 `event_ids` 处理后会恢复执行                  |
| `retries_exhausted` | 瞬时失败重试次数耗尽；请结合相邻的 `session.error` 排查                |
| `budget_reached`    | Native 本轮执行达到限制；核对已完成的部分后决定是否继续                     |

### Span 事件

Span 事件用于标记模型请求或结果评估的开始、进行中和结束：

* `span.model_request_start`、`span.model_request_end`
* `span.outcome_evaluation_start`、`span.outcome_evaluation_ongoing`、`span.outcome_evaluation_end`

### 系统事件

* `system.message`：为当前及后续轮次追加系统级指导。

## 支持的 MCP 服务器类型

AgentAPI 支持在智能体上配置远程 MCP 服务器。服务器端点和工具权限通过 [MCP 连接器](../development-guidelines/agent-configuration/mcp.md) 配置。`claudecode` 和 `codex` 从会话沙箱连接 MCP 服务；`native` 由 AgentAPI 服务端连接，但仍会校验会话环境的网络配置。使用 `limited` 网络时，MCP 服务主机必须包含在 `allowed_hosts` 中。Native 中显式设置 `allow_mcp_servers: false` 会禁止 MCP 工具接入；设置为 `true` 不会绕过主机白名单。

## 相关页面

* [事件和事件流](../development-guidelines/sessions-and-tasks/events.md)：发送事件、读取历史和建立 SSE 连接。
* [使用 SDK](../quick-start/quickstart.md)：配置客户端并传入 AgentAPI 扩展字段。
