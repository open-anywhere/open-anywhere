<!-- 来源：https://developer-doc.qiniu.com/products/agentapi/development_guidelines/agentapi-sessions-and-tasks/13596-agentapi-sessions-and-tasks-session | 由 sync-docs.py 自动生成，请勿手改 -->

# 创建会话



最近更新时间: 2026-10-13 15:31:33

会话（Session）保存一个任务的输入、智能体输出、工具调用和交互历史。创建时引用[智能体](../agent-configuration/agent.md)与[云端环境](../execution-environments/environment.md)，并固定智能体版本；发送用户事件后才开始执行。

![会话消息与事件流](https://developer-doc.qiniu.com/docs-assets/products/agentapi/development_guidelines/agentapi-sessions-and-tasks/13596-agentapi-sessions-and-tasks-session/FqtI0KwhVzMFwEnlT0HhD74hSVSv-628ee27bb049.webp)

## 任务与执行轮次

数据分析任务可以在同一会话中提交数据、补充统计口径和修订报告，共用历史与文件；无关任务应创建新会话。新会话以 `idle` 状态开始，首次输入才准备云端沙箱。

会话状态与恢复边界见[会话操作](session-lifecycle.md)，本轮结果按[停止原因](events.md)判断。

## 创建会话

创建会话必须提供 `agent` 和 `environment_id`。智能体是版本化资源；建议明确传入版本对象，使同一任务始终使用可追溯的智能体配置。会话使用的 `runtime` 继承自该智能体版本，请在[定义智能体](../agent-configuration/agent.md)时选择 `claudecode`、`codex` 或 `native`，不要把 `runtime` 作为会话字段传入。

开始前，请完成[接入准备](../../quick-start/access.md)，并保存创建智能体和云端环境时返回的 ID。以下 SDK 示例沿用[SDK 客户端初始化](../../quick-start/quickstart.md)中的 `client`。以下示例仅传入智能体 ID：会话固定创建时的最新版本，后续智能体更新不会改变会话配置。

cURL

```bash
session=$(curl --request POST "$AGENTAPI_BASE_URL/v1/sessions" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --data @- <<EOF
{
  "agent": "$AGENT_ID",
  "environment_id": "$ENVIRONMENT_ID"
}
EOF
)
SESSION_ID=$(jq -r '.id' <<< "$session")
```

Python

```python
session = client.beta.sessions.create(
    agent=agent.id,
    environment_id=environment.id,
)
```

TypeScript

````typescript
const session = await client.beta.sessions.create({
  agent: agent.id,
  environment_id: environment.id
});
```csharp
```csharp
var session = await client.Beta.Sessions.Create(new()
{
    Agent = agent.ID,
    EnvironmentID = environment.ID,
});
````

Go

```go
session, err := client.Beta.Sessions.New(ctx, anthropic.BetaSessionNewParams{
	Agent: anthropic.BetaSessionNewParamsAgentUnion{
		OfString: anthropic.String(agent.ID),
	},
	EnvironmentID: environment.ID,
})
if err != nil {
	panic(err)
}
```

Java

```java
var session = client.beta().sessions().create(SessionCreateParams.builder()
    .agent(agent.id())
    .environmentId(environment.id())
    .build());
```

PHP

```php
$session = $client->beta->sessions->create(
    agent: $agent->id,
    environmentID: $environment->id,
);
```

Ruby

```ruby
session = client.beta.sessions.create(
  agent: agent.id,
  environment_id: environment.id
)
```

## 发送消息启动执行

会话创建后，向 `POST /v1/sessions/{id}/events` 提交 `user.message` 才会启动执行。结构化输入使用 `input.parts`；HTTP 与 SDK 的发送方式见[提交任务输入](events.md)，进度通过[事件流](events.md)读取。

## 配置会话的资源与行为

### 任务信息和资源

创建请求还可以包含以下信息：

* `title`：便于在 Portal 和列表中识别任务的标题。
* `metadata`：字符串键值对，用于关联业务 ID、任务来源等信息。
* `resources`：挂载[文件](../data-and-integrations/files.md)、[GitHub 仓库](../data-and-integrations/github.md)或[记忆存储](../data-and-integrations/memory.md)。单个会话最多可以添加 500 个资源，其中记忆存储最多 8 个。
* `vault_ids`：为需要认证的 MCP 服务关联凭证，详见MCP 身份验证。

GitHub 仓库需要沙箱的 Git 能力。`claudecode`、`codex` 支持这类资源；`native` 在启用沙箱执行工具时也支持，并会在首个模型请求前异步准备沙箱，模型调用沙箱工具时等待沙箱就绪。

Portal 会话详情顶部的凭证库入口显示关联数量。点击后，您可以查看各凭证库的名称、ID 和归档状态，并进入[凭证库](../data-and-integrations/vaults.md)详情；没有关联时显示“未关联凭证库”。

### 固定智能体版本

要将会话固定到特定的智能体版本，请传入一个对象，以控制执行版本并分阶段发布新版本。

cURL

```bash
pinned_session=$(curl --request POST "$AGENTAPI_BASE_URL/v1/sessions" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --data @- <<EOF
{
  "agent": {"type": "agent", "id": "$AGENT_ID", "version": 1},
  "environment_id": "$ENVIRONMENT_ID"
}
EOF
)
PINNED_SESSION_ID=$(jq -r '.id' <<< "$pinned_session")
```

Python

```python
pinned_session = client.beta.sessions.create(
    agent={"type": "agent", "id": agent.id, "version": 1},
    environment_id=environment.id,
)
```

TypeScript

````typescript
const pinnedSession = await client.beta.sessions.create({
  agent: { type: "agent", id: agent.id, version: 1 },
  environment_id: environment.id
});
```csharp
```csharp
var pinnedSession = await client.Beta.Sessions.Create(new()
{
    Agent = new BetaManagedAgentsAgentParams
    {
        Type = BetaManagedAgentsAgentParamsType.Agent,
        ID = agent.ID,
        Version = 1,
    },
    EnvironmentID = environment.ID,
});
````

Go

```go
pinnedSession, err := client.Beta.Sessions.New(ctx, anthropic.BetaSessionNewParams{
	Agent: anthropic.BetaSessionNewParamsAgentUnion{
		OfBetaManagedAgentsAgents: &anthropic.BetaManagedAgentsAgentParams{
			Type:    anthropic.BetaManagedAgentsAgentParamsTypeAgent,
			ID:      agent.ID,
			Version: anthropic.Int(1),
		},
	},
	EnvironmentID: environment.ID,
})
if err != nil {
	panic(err)
}
```

Java

```java
var pinnedSession = client.beta().sessions().create(SessionCreateParams.builder()
    .agent(BetaManagedAgentsAgentParams.builder()
        .type(BetaManagedAgentsAgentParams.Type.AGENT)
        .id(agent.id())
        .version(1)
        .build())
    .environmentId(environment.id())
    .build());
```

PHP

```php
$pinnedSession = $client->beta->sessions->create(
    agent: ['type' => 'agent', 'id' => $agent->id, 'version' => 1],
    environmentID: $environment->id,
);
```

Ruby

```ruby
pinned_session = client.beta.sessions.create(
  agent: {type: :agent, id: agent.id, version: 1},
  environment_id: environment.id
)
```

### 为会话覆盖智能体配置

创建会话时，可以使用 `agent_with_overrides` 只为当前会话覆盖智能体配置，而不修改智能体，也不会创建新的智能体版本。传入智能体的 `id` 和可选的 `version`（省略 `version` 时使用最新版本），并可覆盖 `model`、`system`、`tools`、`mcp_servers`、`skills` 中的一项或多项。

| 请求内容           | 会话使用的配置                                        |
| -------------- | ---------------------------------------------- |
| 省略某个覆盖字段       | 继承所固定的智能体版本。                                   |
| 提交字段值          | 整体替换对应字段，不与基础配置合并。                             |
| `system: null` | 清空系统提示。                                        |
| 列表字段为空数组       | 清空对应的工具、MCP 服务器或技能列表。                          |
| 提交 `model` 对象  | 使用新模型对象；显式提供的 `effort` 会生效，省略时不继承基础配置的 effort。 |

例如，`"model": {"id": "your-model-id", "effort": {"type": "high"}}` 会为这次会话设置模型和思考强度；只提供 `id` 则使用该模型的默认强度。请从[七牛 MaaS 模型管理页](https://portal.qiniu.com/ai-inference/model)获取可用的模型 ID。

会话仍会固定一个智能体版本。`runtime` 和 `agentapi_options` 从该版本继承，会话覆盖不能改变它们；因此 `thinking` 和 `system_prompt_mode` 也保持该版本的设置。

如需调整这些配置，请更新智能体并创建新会话。用法见[思考配置](../agent-configuration/agent.md)和[系统提示词放置方式](../agent-configuration/agent.md)。

创建响应的 `agent` 是覆盖后的配置快照，`id` 和 `version` 仍指向原智能体版本。快照不回显思考开关和系统提示词放置方式；请通过 `id` 和 `version` 查询对应版本。

cURL

```bash
override_session=$(curl --request POST "$AGENTAPI_BASE_URL/v1/sessions" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --data @- <<EOF
{
  "agent": {
    "type": "agent_with_overrides",
    "id": "$AGENT_ID",
    "version": 1,
    "model": {"id": "$AGENTAPI_MODEL_ID"}
  },
  "environment_id": "$ENVIRONMENT_ID"
}
EOF
)
OVERRIDE_SESSION_ID=$(jq -r '.id' <<< "$override_session")
```

Python

```python
override_session = client.beta.sessions.create(
    agent={
        "type": "agent_with_overrides",
        "id": agent.id,
        "version": 1,
        "model": {"id": "gpt-5.6-sol"},
    },
    environment_id=environment.id,
)
```

TypeScript

````typescript
const overrideSession = await client.beta.sessions.create({
  agent: {
    type: "agent_with_overrides",
    id: agent.id,
    version: 1,
    model: { id: "gpt-5.6-sol" }
  },
  environment_id: environment.id
});
```csharp
```csharp
var overrideSession = await client.Beta.Sessions.Create(new()
{
    Agent = new BetaManagedAgentsAgentWithOverridesParams
    {
        Type = BetaManagedAgentsAgentWithOverridesParamsType.AgentWithOverrides,
        ID = agent.ID,
        Version = 1,
        Model = new BetaManagedAgentsModelConfigParams { ID = "gpt-5.6-sol" },
    },
    EnvironmentID = environment.ID,
});
````

Go

```go
overrideSession, err := client.Beta.Sessions.New(ctx, anthropic.BetaSessionNewParams{
	Agent: anthropic.BetaSessionNewParamsAgentUnion{
		OfBetaManagedAgentsAgentWithOverridess: &anthropic.BetaManagedAgentsAgentWithOverridesParams{
			Type:    anthropic.BetaManagedAgentsAgentWithOverridesParamsTypeAgentWithOverrides,
			ID:      agent.ID,
			Version: anthropic.Int(1),
			Model: anthropic.BetaManagedAgentsModelConfigParams{
				ID: "gpt-5.6-sol",
			},
		},
	},
	EnvironmentID: environment.ID,
})
if err != nil {
	panic(err)
}
```

Java

```java
var overrideSession = client.beta().sessions().create(
    SessionCreateParams.builder()
        .agent(BetaManagedAgentsAgentWithOverridesParams.builder()
            .type(BetaManagedAgentsAgentWithOverridesParams.Type.AGENT_WITH_OVERRIDES)
            .id(agent.id())
            .version(1)
            .model(BetaManagedAgentsModelConfigParams.builder()
                .id("gpt-5.6-sol")
                .build())
            .build())
        .environmentId(environment.id())
        .build()
);
```

PHP

```php
$overrideSession = $client->beta->sessions->create(
    agent: BetaManagedAgentsAgentWithOverridesParams::with(
        type: 'agent_with_overrides',
        id: $agent->id,
        version: 1,
        model: ['id' => 'gpt-5.6-sol'],
    ),
    environmentID: $environment->id,
);
```

Ruby

```ruby
override_session = client.beta.sessions.create(
  agent: Anthropic::Beta::BetaManagedAgentsAgentWithOverridesParams.new(
    type: :agent_with_overrides,
    id: agent.id,
    version: 1,
    model: {id: "gpt-5.6-sol"}
  ),
  environment_id: environment.id
)
```

## MCP 身份验证

创建会话时通过 `vault_ids` 引用凭证。一个会话最多引用 50 个 Vault，执行时最多匹配并注入 20 个凭证。Vault 创建与凭证匹配规则见[凭证管理](../data-and-integrations/vaults.md)，会话接入示例见[MCP 身份验证](../agent-configuration/mcp.md)。

## 后续步骤

* [会话操作](session-lifecycle.md)：检索、列出、更新、归档和删除会话。
* [事件和事件流](events.md)：订阅响应，处理中断、工具结果和工具确认。
* [定时部署](scheduled-deployments.md)：按 cron 周期运行智能体并查看执行历史。
