<!-- 来源：https://developer-doc.qiniu.com/products/agentapi/development_guidelines/agentapi-agent-configuration/13592-agentapi-agent-configuration-mcp | 由 sync-docs.py 自动生成，请勿手改 -->

# MCP 连接器



最近更新时间: 2026-10-13 15:31:20

AgentAPI 支持将 [Model Context Protocol (MCP)](https://modelcontextprotocol.io) 服务器连接到云端智能体。连接后，智能体可以通过标准化协议访问外部工具、数据源和服务。

接入 MCP 服务时，需要关联服务器连接、工具集和认证凭证：

1. 在智能体的 `mcp_servers` 中声明服务器名称和 URL。
2. 在同一个智能体的 `tools` 中声明 `mcp_toolset`，用 `mcp_server_name` 引用服务器，并设置工具权限策略。
3. 服务需要认证时，在 Vault（保管库）中保存与服务器 URL 匹配的凭证，再通过会话的 `vault_ids` 引用。无需认证的服务器可以省略这一步。

凭证不保存在智能体定义中，可按会话独立配置。

![MCP 连接配置与凭据分离](https://developer-doc.qiniu.com/docs-assets/products/agentapi/development_guidelines/agentapi-agent-configuration/13592-agentapi-agent-configuration-mcp/Fq9GXY5DgtyE73ephUTXYm6K-0Jc-84ecb9464844.webp)

MCP 服务器必须是远程 URL。`claudecode`、`codex` 在会话沙箱中连接服务器，`native` 由 AgentAPI 服务端连接。使用 `limited` 环境时，MCP 服务主机必须包含在 `allowed_hosts` 中，Native 同样会校验该配置。Native 中 `allow_mcp_servers: false` 会禁止 MCP 工具接入；设为 `true` 不会绕过白名单。网络配置见[云端环境](../execution-environments/environment.md)。

## 运行时支持

MCP 连接会随智能体的 `runtime` 在云端执行：

| 运行时          | MCP 连接方式                                    | 权限策略                                          |
| ------------ | ------------------------------------------- | --------------------------------------------- |
| `claudecode` | 将已启用的 MCP 服务器传递给 Claude Code 运行时。           | MCP 工具必须使用 `always_allow`。                    |
| `codex`      | 将已启用的 MCP 服务器传递给 Codex 运行时。                 | MCP 工具必须使用 `always_allow`。                    |
| `native`     | AgentAPI 在云端直接发现并调用 MCP 工具；服务器端点必须使用 HTTPS。 | 支持 `always_allow` 和 `always_ask`，后者会暂停会话等待确认。 |

> MCP 工具确认
>
> * 每次调用前需要应用确认：选择 `runtime: "native"` 和 `always_ask`。
> * 使用 `claudecode` 或 `codex`：必须设置 `always_allow`，否则智能体创建或会话启动会失败。

## 配置 MCP 服务器

创建智能体时，在 `mcp_servers` 数组中指定 MCP 服务器。每个服务器需要一个 `type`、一个唯一的 `name` 和一个 `url`。此阶段不提供任何身份验证令牌。

要启用某个服务器的工具，还需在 `tools` 数组中声明匹配的 `mcp_toolset` 条目。该工具集的 `mcp_server_name` 必须与服务器的 `name` 匹配；仅声明服务器不会启用其工具。

> 说明
>
> 示例准备：
>
> * 完成[接入准备](../../quick-start/access.md)，从[七牛 MaaS 模型管理页](https://portal.qiniu.com/ai-inference/model)获取模型 ID，设置 `AGENTAPI_MODEL_ID`。
> * 将 `MCP_SERVER_URL` 替换为真实服务地址；`https://mcp.example.com` 为占位符。
> * 按[使用 SDK](../../quick-start/quickstart.md)配置和初始化客户端。
>
> 示例使用 `native`，需处理[工具确认请求](permissions.md)。

cURL

```bash
MCP_SERVER_URL="${MCP_SERVER_URL:-https://mcp.example.com}"

agent_response=$(curl --request POST "$AGENTAPI_BASE_URL/v1/agents" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --data @- <<EOF
{
  "name": "GitHub 助手",
  "model": {"id": "$AGENTAPI_MODEL_ID"},
  "mcp_servers": [
    {
      "type": "url",
      "name": "github",
      "url": "$MCP_SERVER_URL"
    }
  ],
  "tools": [
    {"type": "agent_toolset_20260401"},
    {"type": "mcp_toolset", "mcp_server_name": "github"}
  ],
  "runtime": "native"
}
EOF
)
agent_id=$(jq -r '.id' <<<"$agent_response")
```

Python

```python
import os

agent = client.beta.agents.create(
    name="GitHub 助手",
    model={"id": os.environ["AGENTAPI_MODEL_ID"]},
    mcp_servers=[
        {
            "type": "url",
            "name": "github",
            "url": os.getenv("MCP_SERVER_URL", "https://mcp.example.com"),
        },
    ],
    tools=[
        {"type": "agent_toolset_20260401"},
        {"type": "mcp_toolset", "mcp_server_name": "github"},
    ],
    extra_body={"runtime": "native"},
)
```

TypeScript

````typescript
const params = {
  name: "GitHub 助手",
  model: { id: process.env.AGENTAPI_MODEL_ID! },
  mcp_servers: [
    {
      type: "url",
      name: "github",
      url: process.env.MCP_SERVER_URL || "https://mcp.example.com",
    },
  ],
  tools: [
    { type: "agent_toolset_20260401" },
    { type: "mcp_toolset", mcp_server_name: "github" },
  ]
} satisfies Parameters<typeof client.beta.agents.create>[0];
const agent = await client.beta.agents.create(params, {
  body: { ...params, runtime: "native" },
});
```csharp
```csharp
using System;
using System.Linq;
using System.Text.Json;
using Anthropic;
using Anthropic.Core;
using Anthropic.Models.Beta.Agents;

var parameters = new AgentCreateParams
{
    Name = "GitHub 助手",
    Model = new BetaManagedAgentsModelConfigParams { ID = Environment.GetEnvironmentVariable("AGENTAPI_MODEL_ID")! },
};
var body = parameters.RawBodyData.ToDictionary(entry => entry.Key, entry => entry.Value);
body["mcp_servers"] = JsonSerializer.SerializeToElement(new[] {
    new { type = "url", name = "github", url = Environment.GetEnvironmentVariable("MCP_SERVER_URL") ?? "https://mcp.example.com" },
});
body["tools"] = JsonSerializer.SerializeToElement(new object[] {
    new { type = "agent_toolset_20260401" },
    new { type = "mcp_toolset", mcp_server_name = "github" },
});
body["runtime"] = JsonSerializer.SerializeToElement("native");
parameters = AgentCreateParams.FromRawUnchecked(parameters.RawHeaderData, parameters.RawQueryData, body);
var agent = await client.Beta.Agents.Create(parameters);
````

Go

```go
import "os"
import "github.com/anthropics/anthropic-sdk-go/option"

agent, err := client.Beta.Agents.New(ctx, anthropic.BetaAgentNewParams{
	Name: "GitHub 助手",
	Model: anthropic.BetaManagedAgentsModelConfigParams{
		ID: os.Getenv("AGENTAPI_MODEL_ID"),
	},
	MCPServers: []anthropic.BetaManagedAgentsURLMCPServerParams{{
		Type: anthropic.BetaManagedAgentsURLMCPServerParamsTypeURL,
		Name: "github",
		URL:  envOr("MCP_SERVER_URL", "https://mcp.example.com"),
	}},
	Tools: []anthropic.BetaAgentNewParamsToolUnion{
		{
			OfAgentToolset20260401: &anthropic.BetaManagedAgentsAgentToolset20260401Params{
				Type: anthropic.BetaManagedAgentsAgentToolset20260401ParamsTypeAgentToolset20260401,
			},
		},
		{
			OfMCPToolset: &anthropic.BetaManagedAgentsMCPToolsetParams{
				Type:          anthropic.BetaManagedAgentsMCPToolsetParamsTypeMCPToolset,
				MCPServerName: "github",
			},
		},
	},
}, option.WithJSONSet("runtime", "native"))
if err != nil {
	panic(err)
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
```

Java

```java
import com.anthropic.core.JsonValue;

var agent = client.beta().agents().create(
    AgentCreateParams.builder()
        .name("GitHub 助手")
        .model(BetaManagedAgentsModelConfigParams.builder().id(System.getenv("AGENTAPI_MODEL_ID")).build())
        .addMcpServer(
            BetaManagedAgentsUrlMcpServerParams.builder()
                .type(BetaManagedAgentsUrlMcpServerParams.Type.URL)
                .name("github")
                .url(System.getenv().getOrDefault("MCP_SERVER_URL", "https://mcp.example.com"))
                .build()
        )
        .addTool(
            BetaManagedAgentsAgentToolset20260401Params.builder()
                .type(BetaManagedAgentsAgentToolset20260401Params.Type.AGENT_TOOLSET_20260401)
                .build()
        )
        .addTool(
            BetaManagedAgentsMcpToolsetParams.builder()
                .type(BetaManagedAgentsMcpToolsetParams.Type.MCP_TOOLSET)
                .mcpServerName("github")
                .build()
        )
        .putAdditionalBodyProperty("runtime", JsonValue.from("native"))
        .build()
);
```

PHP

```php
$agent = $client->beta->agents->create(
    name: 'GitHub 助手',
    model: ['id' => getenv('AGENTAPI_MODEL_ID')],
    mcpServers: [
        BetaManagedAgentsURLMCPServerParams::with(
            type: 'url',
            name: 'github',
            url: getenv('MCP_SERVER_URL') ?: 'https://mcp.example.com',
        ),
    ],
    tools: [
        BetaManagedAgentsAgentToolset20260401Params::with(
            type: 'agent_toolset_20260401',
        ),
        BetaManagedAgentsMCPToolsetParams::with(
            type: 'mcp_toolset',
            mcpServerName: 'github',
        ),
    ],
    requestOptions: ['extraBodyParams' => ['runtime' => 'native']],
);
```

Ruby

```ruby
agent = client.beta.agents.create(
  name: "GitHub 助手",
  model: {id: ENV.fetch("AGENTAPI_MODEL_ID")},
  mcp_servers: [
    {
      type: "url",
      name: "github",
      url: ENV.fetch("MCP_SERVER_URL", "https://mcp.example.com")
    }
  ],
  tools: [
    {type: "agent_toolset_20260401"},
    {type: "mcp_toolset", mcp_server_name: "github"}
  ],
  request_options: {extra_body: {runtime: "native"}}
)
```

### `mcp_servers` 字段参考

`mcp_servers` 数组中的每个条目定义一个连接。

| 字段     | 描述                                                                                                                                                                                                              |
| ------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `type` | 必填。必须为 `"url"`。AgentAPI 当前不支持通过 `stdio` 等方式启动 MCP 服务器。                                                                                                                                                          |
| `name` | 必填。此服务器在智能体内的唯一名称（1–64 个字符，必须以字母开头）。用作 `tools` 数组中的 `mcp_server_name`，并显示在[事件和事件流](../sessions-and-tasks/events.md)中的 MCP 工具事件上。 |
| `url`  | 必填。MCP 服务器的 `http` 或 `https` 端点，不能在 URL 中携带用户信息或凭据。                                                                                                                                                             |

约束：

* 一个智能体最多可以声明 16 个 MCP 服务器。服务器名称在数组内必须唯一。
* `mcp_toolset` 必须引用一个已声明的 MCP 服务器。

## 配置可用的 MCP 工具

`native` 支持通过 `default_config` 和 `configs` 启用或禁用单个 MCP 工具：

```json
{
  "type": "mcp_toolset",
  "mcp_server_name": "github",
  "default_config": {"enabled": false},
  "configs": [
    {"name": "get_issue", "enabled": true}
  ]
}
```

`claudecode` 和 `codex` 只支持启用整个 MCP 服务器，不支持逐工具配置。

## 配置会话认证凭证

先按[凭证管理](../data-and-integrations/vaults.md)创建与服务器 URL 匹配的 Vault 凭证，再在创建会话时通过 `vault_ids` 引用。多个会话可引用同一 Vault。

cURL

```bash
session_response=$(curl --request POST "$AGENTAPI_BASE_URL/v1/sessions" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --data @- <<EOF
{
  "agent": {"type": "agent", "id": "$agent_id", "version": 1},
  "environment_id": "$environment_id",
  "vault_ids": ["$vault_id"]
}
EOF
)
session_id=$(jq -r '.id' <<<"$session_response")
```

Python

```python
session = client.beta.sessions.create(
    agent=agent.id,
    environment_id=environment.id,
    vault_ids=[vault.id],
)
```

TypeScript

````typescript
const session = await client.beta.sessions.create({
  agent: agent.id,
  environment_id: environment.id,
  vault_ids: [vault.id],
});
```csharp
```csharp
var session = await client.Beta.Sessions.Create(new()
{
    Agent = agent.ID,
    EnvironmentID = environment.ID,
    VaultIds = [vault.ID],
});
````

Go

```go
session, err := client.Beta.Sessions.New(ctx, anthropic.BetaSessionNewParams{
	Agent:         anthropic.BetaSessionNewParamsAgentUnion{OfString: anthropic.String(agent.ID)},
	EnvironmentID: environment.ID,
	VaultIDs:      []string{vault.ID},
})
if err != nil {
	panic(err)
}
```

Java

```java
var session = client.beta().sessions().create(
    SessionCreateParams.builder()
        .agent(agent.id())
        .environmentId(environment.id())
        .addVaultId(vault.id())
        .build()
);
```

PHP

```php
$session = $client->beta->sessions->create(
    agent: $agent->id,
    environmentID: $environment->id,
    vaultIDs: [$vault->id],
);
```

Ruby

```ruby
session = client.beta.sessions.create(
  agent: agent.id,
  environment_id: environment.id,
  vault_ids: [vault.id]
)
```

Vault 凭据通过 `mcp_server_url` 与 MCP 服务器匹配。没有匹配凭据时，AgentAPI 会尝试不带身份验证连接。凭据配置参阅[添加凭据](../data-and-integrations/vaults.md)。

### 处理连接和身份验证失败

会话创建成功仅表示配置已保存。请发送用户消息，观察连接和工具调用事件，验证 MCP 服务是否可用。

出现连接或认证失败时，依次检查：

1. 服务地址：是否为真实、可访问的远程 MCP 端点；Native 需要 HTTPS。
2. 网络策略：环境是否允许连接 MCP 服务；使用 `limited` 时，目标主机是否在 `allowed_hosts` 中。
3. 工具配置：`mcp_server_name` 是否与服务器名称匹配，所需工具是否启用，权限策略是否受运行时支持。
4. 凭证匹配：会话是否引用正确的 Vault，凭证的 `mcp_server_url` 是否与服务器 URL 匹配，令牌是否有效。
5. 确认等待：若停止原因是 `requires_action`，先处理确认事件；这并不表示连接失败。

完整确认与回传流程见[工具确认](../sessions-and-tasks/events.md)。

## 后续步骤

* [权限策略](permissions.md)：控制智能体工具和 MCP 工具何时运行。

* [事件和事件流](../sessions-and-tasks/events.md)：发送事件、流式传输响应，并在执行过程中中断执行或补充任务要求。

* [支持的 MCP 服务器类型](../../api/api-conventions.md)：远程 MCP 服务器的传输要求。
