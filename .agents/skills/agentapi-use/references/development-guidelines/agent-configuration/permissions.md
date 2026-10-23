<!-- 来源：https://developer-doc.qiniu.com/products/agentapi/development_guidelines/agentapi-agent-configuration/13593-agentapi-agent-configuration-permissions | 由 sync-docs.py 自动生成，请勿手改 -->

# 权限策略



最近更新时间: 2026-10-13 15:31:22

权限策略控制云端运行时的工具执行方式：直接执行或等待应用确认。它适用于 AgentAPI 执行的内置工具和 MCP 工具；自定义工具由应用执行，其执行权限由应用管理。

创建智能体时，请根据运行时显式设置权限策略：

* `native` 支持自动执行和人工确认。
* `claudecode`、`codex` 的 MCP 工具必须使用 `always_allow`，不支持 AgentAPI 确认流程。

运行时差异见[运行时](agent.md)和[工具](tools.md)。

![工具调用的权限策略流程](https://developer-doc.qiniu.com/docs-assets/products/agentapi/development_guidelines/agentapi-agent-configuration/13593-agentapi-agent-configuration-permissions/FgBNbtJm3QDj23HMslc_JZAMTM98-3ffdcf308cb2.webp)

## 权限策略类型

| 策略             | 行为                                                                                                                                                         |
| -------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `always_allow` | 工具自动执行，无需确认。                                                                                                                                               |
| `always_ask`   | 工具调用前暂停会话，等待应用程序确认。确认流程请参阅[响应确认请求](permissions.md)。 |

MCP 工具集在 AgentAPI 中默认使用 `always_ask`。内置工具的默认策略由运行时决定；为避免行为差异，建议在 `default_config.permission_policy` 中显式设置策略。对于 `claudecode` 和 `codex`，MCP 工具必须配置为 `always_allow`。

权限策略控制已启用的工具何时运行。若要阻止某个工具被调用，请将它设为禁用。逐工具开关的运行时支持范围见[配置内置工具集](tools.md)。

允许工具执行后，仍需配置[环境网络策略](../execution-environments/environment.md)和 [Vault](../data-and-integrations/vaults.md)，分别提供目标服务的网络访问权限和认证凭证。

## 为工具集设置策略

创建智能体时，在 `tools` 配置中设置权限策略；之后可以通过[更新智能体](agent.md)修改。已经创建的会话会保留创建时的工具配置，更新只对之后创建的会话生效。

> 说明
>
> * 完成[接入准备](../../quick-start/access.md)，从[七牛 MaaS 模型管理页](https://portal.qiniu.com/ai-inference/model)获取模型 ID，设置 `AGENTAPI_MODEL_ID`。
> * SDK 使用[SDK 客户端初始化](../../quick-start/quickstart.md)中的 `client`，通过扩展请求参数指定 `runtime`。
> * MCP 服务器 URL 替换为实际服务地址。

### 智能体工具集权限

创建智能体时，可以使用 `default_config.permission_policy` 为 `agent_toolset_20260401` 中的工具设置统一策略。下面的示例选择 `native` 运行时，并要求调用工具前确认：

cURL

```bash
agent=$(curl --request POST "$AGENTAPI_BASE_URL/v1/agents" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --data @- <<EOF
{
    "name": "编程助手",
    "runtime": "native",
    "model": {"id": "$AGENTAPI_MODEL_ID"},
    "tools": [
      {
        "type": "agent_toolset_20260401",
        "default_config": {
          "permission_policy": {"type": "always_ask"}
        }
      }
    ]
  }
EOF
)
```

Python

```python
import os

agent = client.beta.agents.create(
    name="编程助手",
    model={"id": os.environ["AGENTAPI_MODEL_ID"]},
    tools=[
        {
            "type": "agent_toolset_20260401",
            "default_config": {
                "permission_policy": {"type": "always_ask"},
            },
        },
    ],
    extra_body={"runtime": "native"},
)
```

TypeScript

````typescript
const params = {
  name: "编程助手",
  model: { id: process.env.AGENTAPI_MODEL_ID! },
  tools: [
    {
      type: "agent_toolset_20260401",
      default_config: {
        permission_policy: { type: "always_ask" }
      }
    }
  ]
} satisfies Parameters<typeof client.beta.agents.create>[0];
const agent = await client.beta.agents.create(params, {
  body: { ...params, runtime: "native" },
});
```csharp
```csharp
using System.Text.Json;
using System.Linq;
using System;
using Anthropic.Models.Beta.Agents;

var parameters = new AgentCreateParams
{
    Name = "编程助手",
    Model = new BetaManagedAgentsModelConfigParams { ID = Environment.GetEnvironmentVariable("AGENTAPI_MODEL_ID")! },
    Tools =
    [
        new BetaManagedAgentsAgentToolset20260401Params
        {
            Type = BetaManagedAgentsAgentToolset20260401ParamsType.AgentToolset20260401,
            DefaultConfig = new()
            {
                PermissionPolicy = new BetaManagedAgentsAlwaysAskPolicy { Type = "always_ask" },
            },
        },
    ],
};
var body = parameters.RawBodyData.ToDictionary(entry => entry.Key, entry => entry.Value);
body["runtime"] = JsonSerializer.SerializeToElement("native");
parameters = AgentCreateParams.FromRawUnchecked(parameters.RawHeaderData, parameters.RawQueryData, body);
var agent = await client.Beta.Agents.Create(parameters);
````

Go

```go
import "github.com/anthropics/anthropic-sdk-go/option"
import "os"
agent, err := client.Beta.Agents.New(ctx, anthropic.BetaAgentNewParams{
	Name: "编程助手",
	Model: anthropic.BetaManagedAgentsModelConfigParams{
		ID: os.Getenv("AGENTAPI_MODEL_ID"),
	},
	Tools: []anthropic.BetaAgentNewParamsToolUnion{{
		OfAgentToolset20260401: &anthropic.BetaManagedAgentsAgentToolset20260401Params{
			Type: anthropic.BetaManagedAgentsAgentToolset20260401ParamsTypeAgentToolset20260401,
			DefaultConfig: anthropic.BetaManagedAgentsAgentToolsetDefaultConfigParams{
				PermissionPolicy: anthropic.BetaManagedAgentsAgentToolsetDefaultConfigParamsPermissionPolicyUnion{
					OfAlwaysAsk: &anthropic.BetaManagedAgentsAlwaysAskPolicyParam{
						Type: anthropic.BetaManagedAgentsAlwaysAskPolicyTypeAlwaysAsk,
					},
				},
			},
		},
	}},
}, option.WithJSONSet("runtime", "native"))
if err != nil {
	panic(err)
}
_ = agent
```

Java

```java
import com.anthropic.core.JsonValue;
import com.anthropic.models.beta.agents.*;

var agent = client.beta().agents().create(
    AgentCreateParams.builder()
        .name("编程助手")
        .model(BetaManagedAgentsModelConfigParams.builder().id(System.getenv("AGENTAPI_MODEL_ID")).build())
        .addTool(
            BetaManagedAgentsAgentToolset20260401Params.builder()
                .type(BetaManagedAgentsAgentToolset20260401Params.Type.AGENT_TOOLSET_20260401)
                .defaultConfig(
                    BetaManagedAgentsAgentToolsetDefaultConfigParams.builder()
                        .permissionPolicy(
                            BetaManagedAgentsAlwaysAskPolicy.builder()
                                .type(BetaManagedAgentsAlwaysAskPolicy.Type.ALWAYS_ASK)
                                .build()
                        )
                        .build()
                )
                .build()
        )
        .putAdditionalBodyProperty("runtime", JsonValue.from("native"))
        .build()
);
```

PHP

```php
use Anthropic\Beta\Agents\BetaManagedAgentsAgentToolset20260401Params;
use Anthropic\Beta\Agents\BetaManagedAgentsAgentToolsetDefaultConfigParams;
use Anthropic\Beta\Agents\BetaManagedAgentsAlwaysAskPolicy;

$agent = $client->beta->agents->create(
    name: '编程助手',
    model: ['id' => getenv('AGENTAPI_MODEL_ID')],
    tools: [
        BetaManagedAgentsAgentToolset20260401Params::with(
            type: 'agent_toolset_20260401',
            defaultConfig: BetaManagedAgentsAgentToolsetDefaultConfigParams::with(
                permissionPolicy: BetaManagedAgentsAlwaysAskPolicy::with(type: 'always_ask'),
            ),
        ),
    ],
    requestOptions: ['extraBodyParams' => ['runtime' => 'native']],
);
```

Ruby

```ruby
agent = client.beta.agents.create(
  name: "编程助手",
  model: {id: ENV.fetch("AGENTAPI_MODEL_ID")},
  tools: [
    {
      type: "agent_toolset_20260401",
      default_config: {
        permission_policy: {type: "always_ask"}
      }
    }
  ],
  request_options: {extra_body: {runtime: "native"}}
)
```

`default_config` 是可选的。省略时，内置工具的默认策略由所选运行时决定；如果应用程序依赖确认流程，请显式设置 `always_ask` 并选择支持该流程的 `native` 运行时。

### MCP 工具集权限

MCP 工具集默认为 `always_ask`，新增工具也需要确认。自动执行受信任服务器的工具时，在 `mcp_toolset` 上将 `default_config.permission_policy` 设置为 `always_allow`。`claudecode` 和 `codex` 只支持 `always_allow`；需要确认时请选择 `native`。

`mcp_server_name` 必须与 `mcp_servers` 数组中某个服务器的 `name` 匹配。

此示例连接一个 GitHub MCP 服务器，并允许其工具无需确认即可运行：

cURL

```bash
agent=$(curl --request POST "$AGENTAPI_BASE_URL/v1/agents" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --data @- <<EOF
{
    "name": "研发助手",
    "runtime": "codex",
    "model": {"id": "$AGENTAPI_MODEL_ID"},
    "mcp_servers": [
      {"type": "url", "name": "github", "url": "https://mcp.example.com/github"}
    ],
    "tools": [
      {"type": "agent_toolset_20260401"},
      {
        "type": "mcp_toolset",
        "mcp_server_name": "github",
        "default_config": {
          "permission_policy": {"type": "always_allow"}
        }
      }
    ]
  }
EOF
)
```

Python

```python
import os

agent = client.beta.agents.create(
    name="研发助手",
    model={"id": os.environ["AGENTAPI_MODEL_ID"]},
    mcp_servers=[
        {"type": "url", "name": "github", "url": "https://mcp.example.com/github"},
    ],
    tools=[
        {"type": "agent_toolset_20260401"},
        {
            "type": "mcp_toolset",
            "mcp_server_name": "github",
            "default_config": {
                "permission_policy": {"type": "always_allow"},
            },
        },
    ],
    extra_body={"runtime": "codex"},
)
```

TypeScript

````typescript
const params = {
  name: "研发助手",
  model: { id: process.env.AGENTAPI_MODEL_ID! },
  mcp_servers: [{ type: "url", name: "github", url: "https://mcp.example.com/github" }],
  tools: [
    { type: "agent_toolset_20260401" },
    {
      type: "mcp_toolset",
      mcp_server_name: "github",
      default_config: {
        permission_policy: { type: "always_allow" }
      }
    }
  ]
} satisfies Parameters<typeof client.beta.agents.create>[0];
const agent = await client.beta.agents.create(params, {
  body: { ...params, runtime: "codex" },
});
```csharp
```csharp
using System.Text.Json;
using System.Linq;
using System;
using Anthropic.Models.Beta.Agents;

var parameters = new AgentCreateParams
{
    Name = "研发助手",
    Model = new BetaManagedAgentsModelConfigParams { ID = Environment.GetEnvironmentVariable("AGENTAPI_MODEL_ID")! },
    McpServers =
    [
        new()
        {
            Type = BetaManagedAgentsUrlMcpServerParamsType.Url,
            Name = "github",
            Url = "https://mcp.example.com/github",
        },
    ],
    Tools =
    [
        new BetaManagedAgentsAgentToolset20260401Params
        {
            Type = BetaManagedAgentsAgentToolset20260401ParamsType.AgentToolset20260401,
        },
        new BetaManagedAgentsMcpToolsetParams
        {
            Type = BetaManagedAgentsMcpToolsetParamsType.McpToolset,
            McpServerName = "github",
            DefaultConfig = new()
            {
                PermissionPolicy = new BetaManagedAgentsAlwaysAllowPolicy { Type = "always_allow" },
            },
        },
    ],
};
var body = parameters.RawBodyData.ToDictionary(entry => entry.Key, entry => entry.Value);
body["runtime"] = JsonSerializer.SerializeToElement("codex");
parameters = AgentCreateParams.FromRawUnchecked(parameters.RawHeaderData, parameters.RawQueryData, body);
var agent = await client.Beta.Agents.Create(parameters);
````

Go

```go
import "github.com/anthropics/anthropic-sdk-go/option"
import "os"
agent, err := client.Beta.Agents.New(ctx, anthropic.BetaAgentNewParams{
	Name: "研发助手",
	Model: anthropic.BetaManagedAgentsModelConfigParams{
		ID: os.Getenv("AGENTAPI_MODEL_ID"),
	},
	MCPServers: []anthropic.BetaManagedAgentsURLMCPServerParams{{
		Type: anthropic.BetaManagedAgentsURLMCPServerParamsTypeURL,
		Name: "github",
		URL:  "https://mcp.example.com/github",
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
				DefaultConfig: anthropic.BetaManagedAgentsMCPToolsetDefaultConfigParams{
					PermissionPolicy: anthropic.BetaManagedAgentsMCPToolsetDefaultConfigParamsPermissionPolicyUnion{
						OfAlwaysAllow: &anthropic.BetaManagedAgentsAlwaysAllowPolicyParam{
							Type: anthropic.BetaManagedAgentsAlwaysAllowPolicyTypeAlwaysAllow,
						},
					},
				},
			},
		},
	},
}, option.WithJSONSet("runtime", "codex"))
if err != nil {
	panic(err)
}
_ = agent
```

Java

```java
import com.anthropic.core.JsonValue;
import com.anthropic.models.beta.agents.*;

var agent = client.beta().agents().create(
    AgentCreateParams.builder()
        .name("研发助手")
        .model(BetaManagedAgentsModelConfigParams.builder().id(System.getenv("AGENTAPI_MODEL_ID")).build())
        .addMcpServer(
            BetaManagedAgentsUrlMcpServerParams.builder()
                .type(BetaManagedAgentsUrlMcpServerParams.Type.URL)
                .name("github")
                .url("https://mcp.example.com/github")
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
                .defaultConfig(
                    BetaManagedAgentsMcpToolsetDefaultConfigParams.builder()
                        .permissionPolicy(
                            BetaManagedAgentsAlwaysAllowPolicy.builder()
                                .type(BetaManagedAgentsAlwaysAllowPolicy.Type.ALWAYS_ALLOW)
                                .build()
                        )
                        .build()
                )
                .build()
        )
        .putAdditionalBodyProperty("runtime", JsonValue.from("codex"))
        .build()
);
```

PHP

```php
use Anthropic\Beta\Agents\BetaManagedAgentsAgentToolset20260401Params;
use Anthropic\Beta\Agents\BetaManagedAgentsAlwaysAllowPolicy;
use Anthropic\Beta\Agents\BetaManagedAgentsMCPToolsetDefaultConfigParams;
use Anthropic\Beta\Agents\BetaManagedAgentsMCPToolsetParams;
use Anthropic\Beta\Agents\BetaManagedAgentsURLMCPServerParams;

$agent = $client->beta->agents->create(
    name: '研发助手',
    model: ['id' => getenv('AGENTAPI_MODEL_ID')],
    mcpServers: [
        BetaManagedAgentsURLMCPServerParams::with(
            type: 'url',
            name: 'github',
            url: 'https://mcp.example.com/github',
        ),
    ],
    tools: [
        BetaManagedAgentsAgentToolset20260401Params::with(
            type: 'agent_toolset_20260401',
        ),
        BetaManagedAgentsMCPToolsetParams::with(
            type: 'mcp_toolset',
            mcpServerName: 'github',
            defaultConfig: BetaManagedAgentsMCPToolsetDefaultConfigParams::with(
                permissionPolicy: BetaManagedAgentsAlwaysAllowPolicy::with(type: 'always_allow'),
            ),
        ),
    ],
    requestOptions: ['extraBodyParams' => ['runtime' => 'codex']],
);
```

Ruby

```ruby
agent = client.beta.agents.create(
  name: "研发助手",
  model: {id: ENV.fetch("AGENTAPI_MODEL_ID")},
  mcp_servers: [
    {type: "url", name: "github", url: "https://mcp.example.com/github"}
  ],
  tools: [
    {type: "agent_toolset_20260401"},
    {
      type: "mcp_toolset",
      mcp_server_name: "github",
      default_config: {
        permission_policy: {type: "always_allow"}
      }
    }
  ],
  request_options: {extra_body: {runtime: "codex"}}
)
```

## 覆盖单个工具的策略

在 `native` 运行时中，使用 `configs` 数组覆盖单个工具的默认值。智能体工具集的 `name` 值列于[可用工具](tools.md)中。此示例默认允许完整的智能体工具集，但要求在运行任何 bash 命令之前进行确认：

cURL

```bash
tools='[
  {
    "type": "agent_toolset_20260401",
    "default_config": {
      "permission_policy": {"type": "always_allow"}
    },
    "configs": [
      {
        "name": "bash",
        "permission_policy": {"type": "always_ask"}
      }
    ]
  }
]'
```

Python

```python
tools = [
    {
        "type": "agent_toolset_20260401",
        "default_config": {
            "permission_policy": {"type": "always_allow"},
        },
        "configs": [
            {
                "name": "bash",
                "permission_policy": {"type": "always_ask"},
            },
        ],
    },
]
```

TypeScript

````typescript
const tools = [
  {
    type: "agent_toolset_20260401",
    default_config: {
      permission_policy: { type: "always_allow" }
    },
    configs: [
      {
        name: "bash",
        permission_policy: { type: "always_ask" }
      }
    ]
  }
] satisfies Anthropic.Beta.AgentCreateParams["tools"];
```csharp
```csharp
using Anthropic.Models.Beta.Agents;
using Tool = Anthropic.Models.Beta.Agents.Tool;

Tool[] tools =
[
    new BetaManagedAgentsAgentToolset20260401Params
    {
        Type = BetaManagedAgentsAgentToolset20260401ParamsType.AgentToolset20260401,
        DefaultConfig = new()
        {
            PermissionPolicy = new BetaManagedAgentsAlwaysAllowPolicy { Type = "always_allow" },
        },
        Configs =
        [
            new BetaManagedAgentsBashToolConfigParams
            {
                PermissionPolicy = new BetaManagedAgentsAlwaysAskPolicy { Type = "always_ask" },
            },
        ],
    },
];
````

Go

```go
tools := []anthropic.BetaAgentNewParamsToolUnion{{
	OfAgentToolset20260401: &anthropic.BetaManagedAgentsAgentToolset20260401Params{
		Type: anthropic.BetaManagedAgentsAgentToolset20260401ParamsTypeAgentToolset20260401,
		DefaultConfig: anthropic.BetaManagedAgentsAgentToolsetDefaultConfigParams{
			PermissionPolicy: anthropic.BetaManagedAgentsAgentToolsetDefaultConfigParamsPermissionPolicyUnion{
				OfAlwaysAllow: &anthropic.BetaManagedAgentsAlwaysAllowPolicyParam{
					Type: anthropic.BetaManagedAgentsAlwaysAllowPolicyTypeAlwaysAllow,
				},
			},
		},
		Configs: []anthropic.BetaManagedAgentsAgentToolConfigParamsUnion{{
			OfBash: &anthropic.BetaManagedAgentsBashToolConfigParams{
				PermissionPolicy: anthropic.BetaManagedAgentsBashToolConfigParamsPermissionPolicyUnion{
					OfAlwaysAsk: &anthropic.BetaManagedAgentsAlwaysAskPolicyParam{
						Type: anthropic.BetaManagedAgentsAlwaysAskPolicyTypeAlwaysAsk,
					},
				},
			},
		}},
	},
}}
_ = tools
```

Java

```java
import com.anthropic.models.beta.agents.*;
import java.util.List;

var tools = List.of(
    AgentCreateParams.Tool.ofAgentToolset20260401(
        BetaManagedAgentsAgentToolset20260401Params.builder()
            .type(BetaManagedAgentsAgentToolset20260401Params.Type.AGENT_TOOLSET_20260401)
            .defaultConfig(
                BetaManagedAgentsAgentToolsetDefaultConfigParams.builder()
                    .permissionPolicy(
                        BetaManagedAgentsAlwaysAllowPolicy.builder()
                            .type(BetaManagedAgentsAlwaysAllowPolicy.Type.ALWAYS_ALLOW)
                            .build()
                    )
                    .build()
            )
            .addConfig(
                BetaManagedAgentsBashToolConfigParams.builder()
                    .permissionPolicy(
                        BetaManagedAgentsAlwaysAskPolicy.builder()
                            .type(BetaManagedAgentsAlwaysAskPolicy.Type.ALWAYS_ASK)
                            .build()
                    )
                    .build()
            )
            .build()
    )
);
```

PHP

```php
use Anthropic\Beta\Agents\BetaManagedAgentsBashToolConfigParams;
use Anthropic\Beta\Agents\BetaManagedAgentsAgentToolset20260401Params;
use Anthropic\Beta\Agents\BetaManagedAgentsAgentToolsetDefaultConfigParams;
use Anthropic\Beta\Agents\BetaManagedAgentsAlwaysAllowPolicy;
use Anthropic\Beta\Agents\BetaManagedAgentsAlwaysAskPolicy;

$tools = [
    BetaManagedAgentsAgentToolset20260401Params::with(
        type: 'agent_toolset_20260401',
        defaultConfig: BetaManagedAgentsAgentToolsetDefaultConfigParams::with(
            permissionPolicy: BetaManagedAgentsAlwaysAllowPolicy::with(type: 'always_allow'),
        ),
        configs: [
            BetaManagedAgentsBashToolConfigParams::with(
                permissionPolicy: BetaManagedAgentsAlwaysAskPolicy::with(type: 'always_ask'),
            ),
        ],
    ),
];
```

Ruby

```ruby
tools = [
  {
    type: "agent_toolset_20260401",
    default_config: {
      permission_policy: {type: "always_allow"}
    },
    configs: [
      {
        name: "bash",
        permission_policy: {type: "always_ask"}
      }
    ]
  }
]
```

在智能体创建请求中传递此 `tools` 配置。MCP 工具集支持相同的按工具覆盖，其中 `name` 设置为 MCP 服务器报告的工具名称。请参阅[配置哪些 MCP 工具可用](mcp.md)。

`claudecode`、`codex` 不应用这些逐工具的确认策略。需要在 AgentAPI 中等待应用确认时，请选择 `native`。

## 响应确认请求

Native 的 `always_ask` 调用会以 `requires_action` 等待应用审批。批准或拒绝时，必须使用实际阻塞调用的事件 ID；消息结构、批量确认与恢复消费见[工具确认流程](../sessions-and-tasks/events.md)。

## 自定义工具

权限策略不适用于自定义工具。当智能体调用自定义工具时，应用会收到 `agent.custom_tool_use` 事件，并负责在发回 `user.custom_tool_result` 之前决定是否执行它。有关完整流程，请参阅[事件和事件流](../sessions-and-tasks/events.md)。

## 后续步骤

* [技能](skills.md)：为智能体附加可复用的、基于文件系统的专业知识，以支持特定领域的工作流程。

* [事件和事件流](../sessions-and-tasks/events.md)：发送事件、流式传输响应，并在执行过程中中断执行或补充任务要求。
