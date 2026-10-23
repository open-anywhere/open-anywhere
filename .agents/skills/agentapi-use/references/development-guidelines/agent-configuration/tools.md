<!-- 来源：https://developer-doc.qiniu.com/products/agentapi/development_guidelines/agentapi-agent-configuration/13591-agentapi-agent-configuration-tools | 由 sync-docs.py 自动生成，请勿手改 -->

# 工具



最近更新时间: 2026-10-13 15:31:18

工具让智能体在推理之外执行操作，例如处理文件、运行代码、访问网页或调用业务系统。AgentAPI 支持运行时内置工具、MCP 工具和自定义工具；具体能力和执行位置取决于工具类型以及智能体的 `runtime`。

![AgentAPI 运行时与工具](https://developer-doc.qiniu.com/docs-assets/products/agentapi/development_guidelines/agentapi-agent-configuration/13591-agentapi-agent-configuration-tools/FoXzCKHo1rk03QsLsJOfeUu5ya52-6369745e4769.webp)

## 按任务选择能力

| 任务需要           | 使用方式                                                                                                                                                                                                                                                                |
| -------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 读取文件、修改代码、执行命令 | 声明内置工具集，并选择提供相应能力的运行时。                                                                                                                                                                                                                                              |
| 访问已有 MCP 服务    | 在智能体上声明 MCP 服务器和工具集，在会话上关联所需凭证。                                                                                                                                                                                                                                     |
| 查询订单、调用业务 API  | 声明自定义工具，由应用执行并回传结果。                                                                                                                                                                                                                                                 |
| 复用领域知识或工作流程    | 附加 [Agent Skills](skills.md)，让智能体按任务需要加载说明和辅助文件。                                                                                                             |
| 提供任务数据或代码仓库    | 在会话中挂载[文件](../data-and-integrations/files.md)或 [GitHub 仓库](../data-and-integrations/github.md)。 |

工具提供可执行的操作，技能提供工作方法，文件和仓库提供任务材料。例如，分析一份 CSV 时，可以挂载文件、启用代码执行工具，再附加自定义分析技能。

## 可用工具

| 运行时          | 内置工具                                                                |
| ------------ | ------------------------------------------------------------------- |
| `claudecode` | Claude Code 原生工具                                                    |
| `codex`      | Codex 原生工具                                                          |
| `native`     | `bash`、`read`、`write`、`edit`、`glob`、`grep`、`web_fetch`、`web_search` |

MCP 工具通过 [MCP 连接器](mcp.md)接入外部服务。自定义工具由应用执行，并通过会话事件返回结果。

智能体的 `tools` 数组最多包含 64 个条目。`native` 将工具集展开后，可用工具总数最多为 128 个，包括内置、自定义和 MCP 工具；一个 MCP 工具集可能包含多个工具。

## 配置内置工具集

在智能体的 `tools` 中声明 `agent_toolset_20260401` 可启用内置工具集，适用于 `claudecode`、`codex`、`native`。[通过 API 构建](../../quick-start/quickstart.md)中的 HTTP 示例使用此配置。

最简形式只需声明该工具集类型：

```json
{"type": "agent_toolset_20260401"}
```

工具集还支持通过 `configs` / `default_config` 启用或禁用单个工具。将工具的 `enabled` 设为 `false` 即可禁用该工具：

```json
{
  "type": "agent_toolset_20260401",
  "configs": [
    {"name": "web_search", "enabled": false}
  ]
}
```

也可以先禁用全部工具，再只启用需要的工具：

```json
{
  "type": "agent_toolset_20260401",
  "default_config": {"enabled": false},
  "configs": [
    {"name": "bash", "enabled": true}
  ]
}
```

> 单个工具的启用和禁用配置仅对 \`native\` 生效
>
> 上面的 `enabled` / `default_config` 逐工具配置只在 `native` 运行时被应用，对应 `bash`、`read`、`write`、`edit`、`glob`、`grep`、`web_fetch`、`web_search`。不写 `permission_policy` 时，这些工具默认直接执行。`claudecode` 和 `codex` 使用各自运行时自带的工具集，不响应这些逐工具的启用/禁用开关。

工具确认策略参阅[权限策略](permissions.md)。

## 网页访问

`claudecode` 和 `codex` 的网页访问能力由对应运行时提供。`native` 的网页工具由 AgentAPI 执行，不在自托管 worker 上执行：

* `web_fetch` 请求网址并返回页面文本，不执行页面脚本。可用 `config.max_content_tokens` 限制返回给模型的正文长度。
* `web_search` 调用服务端配置的搜索服务，返回搜索结果文本。可在 `config.user_location` 中设置 `type: approximate` 和两位国家代码 `country`；城市、地区和时区也可以写入该对象，但当前搜索服务不会使用这三项。
* 两个工具均可通过 `config.allowed_domains` 或 `config.blocked_domains` 限制域名。两项不能同时填写；省略时不额外限制域名。

## 自定义工具

自定义工具由智能体决定是否调用、由应用执行，适合查询订单、读取内部数据库或调用业务 API。

![自定义工具执行流程](https://developer-doc.qiniu.com/docs-assets/products/agentapi/development_guidelines/agentapi-agent-configuration/13591-agentapi-agent-configuration-tools/FqP3AWcm2myXxbS90_g7Zhtbdqrs-645621907649.webp)

图中两类工具的执行位置不同：

* 自定义工具：业务代码始终运行在应用进程中。AgentAPI 只负责传递调用和结果，不执行这段代码或通过它访问内网。
* 文件工具和代码执行：在会话的执行环境中运行，和应用进程相互隔离。Native 网页工具由 AgentAPI 执行，远程 MCP 工具则由对应服务执行。

以下示例使用 `native`，通过事件直接完成自定义工具调用和结果回传。`claudecode`、`codex` 需要服务侧已经配置自定义工具桥接，才能使用同一类调用；运行时差异见[智能体](agent.md)。

自定义工具调用流程包含以下四个步骤：

1. 在智能体的 `tools` 中声明工具名称、用途和 `input_schema`。
2. 监听 `agent.custom_tool_use`，执行对应业务函数；会话会通过 `session.status_idle` 暂停等待。
3. 用 `user.custom_tool_result` 回传结果，并通过 `custom_tool_use_id` 对应原调用。
4. 所有待处理的工具调用都返回后，会话才恢复运行，模型再根据结果继续推理。一次可能发出多个工具调用，因此回传单个结果后不一定会立即恢复。

应用侧还需要自行处理鉴权、参数校验、权限确认、超时和错误。失败时也应回传可供模型理解的错误结果，避免会话一直等待。完整事件字段和处理示例请参阅[处理自定义工具调用](../sessions-and-tasks/events.md)。

下面以天气查询为例，在智能体中声明自定义工具；`input_schema` 描述模型传给应用的参数。请先完成[接入准备](../../quick-start/access.md)，从[七牛 MaaS 模型管理页](https://portal.qiniu.com/ai-inference/model)选择支持工具调用的模型，并设置 `AGENTAPI_MODEL_ID`。SDK 示例沿用[SDK 客户端初始化](../../quick-start/quickstart.md)中的 `client`。

cURL

```bash
agent=$(curl --request POST "$AGENTAPI_BASE_URL/v1/agents" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  -d @- <<EOF
{
  "name": "天气助手",
  "runtime": "native",
  "model": {"id": "$AGENTAPI_MODEL_ID"},
  "tools": [
    {
      "type": "agent_toolset_20260401"
    },
    {
      "type": "custom",
      "name": "get_weather",
      "description": "查询指定城市当前的天气。",
      "input_schema": {
        "type": "object",
        "properties": {
          "location": {"type": "string", "description": "城市名称"}
        },
        "required": ["location"]
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
    name="天气助手",
    model={"id": os.environ["AGENTAPI_MODEL_ID"]},
    tools=[
        {
            "type": "agent_toolset_20260401",
        },
        {
            "type": "custom",
            "name": "get_weather",
            "description": "查询指定城市当前的天气。",
            "input_schema": {
                "type": "object",
                "properties": {
                    "location": {"type": "string", "description": "城市名称"},
                },
                "required": ["location"],
            },
        },
    ],
    extra_body={"runtime": "native"},
)
```

TypeScript

````typescript
const params = {
  name: "天气助手",
  model: { id: process.env.AGENTAPI_MODEL_ID! },
  tools: [
    { type: "agent_toolset_20260401" },
    {
      type: "custom",
      name: "get_weather",
      description: "查询指定城市当前的天气。",
      input_schema: {
        type: "object",
        properties: { location: { type: "string", description: "城市名称" } },
        required: ["location"]
      }
    }
  ]
} satisfies Parameters<typeof client.beta.agents.create>[0];
const agent = await client.beta.agents.create(params, {
  body: { ...params, runtime: "native" },
});
```csharp
```csharp
using System.Linq;
using System;
using System.Text.Json;
using Anthropic.Models.Beta.Agents;

var parameters = new AgentCreateParams
{
    Name = "天气助手",
    Model = new BetaManagedAgentsModelConfigParams { ID = Environment.GetEnvironmentVariable("AGENTAPI_MODEL_ID")! },
    Tools =
    [
        new BetaManagedAgentsAgentToolset20260401Params
        {
            Type = "agent_toolset_20260401",
        },
        new BetaManagedAgentsCustomToolParams
        {
            Type = "custom",
            Name = "get_weather",
            Description = "查询指定城市当前的天气。",
            InputSchema = new()
            {
                Properties = new Dictionary<string, JsonElement>
                {
                    ["location"] = JsonSerializer.SerializeToElement(
                        new { type = "string", description = "城市名称" }
                    ),
                },
                Required = ["location"],
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
	Name: "天气助手",
	Model: anthropic.BetaManagedAgentsModelConfigParams{
		ID: os.Getenv("AGENTAPI_MODEL_ID"),
	},
	Tools: []anthropic.BetaAgentNewParamsToolUnion{{
		OfAgentToolset20260401: &anthropic.BetaManagedAgentsAgentToolset20260401Params{
			Type: anthropic.BetaManagedAgentsAgentToolset20260401ParamsTypeAgentToolset20260401,
		},
	}, {
		OfCustom: &anthropic.BetaManagedAgentsCustomToolParams{
			Type:        anthropic.BetaManagedAgentsCustomToolParamsTypeCustom,
			Name:        "get_weather",
			Description: "查询指定城市当前的天气。",
			InputSchema: anthropic.BetaManagedAgentsCustomToolInputSchemaParam{
				Properties: map[string]any{
					"location": map[string]any{
						"type":        "string",
						"description": "City name",
					},
				},
				Required: []string{"location"},
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
import java.util.Map;

var agent = client.beta().agents().create(AgentCreateParams.builder()
    .name("Weather Agent")
    .model(BetaManagedAgentsModelConfigParams.builder().id(System.getenv("AGENTAPI_MODEL_ID")).build())
    .addTool(BetaManagedAgentsAgentToolset20260401Params.builder()
        .type(BetaManagedAgentsAgentToolset20260401Params.Type.AGENT_TOOLSET_20260401)
        .build())
    .addTool(BetaManagedAgentsCustomToolParams.builder()
        .type(BetaManagedAgentsCustomToolParams.Type.CUSTOM)
        .name("get_weather")
        .description("Get current weather for a location")
        .inputSchema(BetaManagedAgentsCustomToolInputSchema.builder()
            .properties(BetaManagedAgentsCustomToolInputSchema.Properties.builder()
                .putAdditionalProperty("location", JsonValue.from(Map.of(
                    "type", "string",
                    "description", "City name")))
                .build())
            .addRequired("location")
            .build())
        .build())
    .putAdditionalBodyProperty("runtime", JsonValue.from("native"))
        .build());
```

PHP

```php
use Anthropic\Beta\Agents\BetaManagedAgentsAgentToolset20260401Params;
use Anthropic\Beta\Agents\BetaManagedAgentsCustomToolInputSchema;
use Anthropic\Beta\Agents\BetaManagedAgentsCustomToolParams;

$agent = $client->beta->agents->create(
    name: '天气助手',
    model: ['id' => getenv('AGENTAPI_MODEL_ID')],
    tools: [
        BetaManagedAgentsAgentToolset20260401Params::with(
            type: 'agent_toolset_20260401',
        ),
        BetaManagedAgentsCustomToolParams::with(
            type: 'custom',
            name: 'get_weather',
            description: '查询指定城市当前的天气。',
            inputSchema: BetaManagedAgentsCustomToolInputSchema::with(
                properties: ['location' => ['type' => 'string', 'description' => '城市名称']],
                required: ['location'],
            ),
        ),
    ],
    requestOptions: ['extraBodyParams' => ['runtime' => 'native']],
);
```

Ruby

```ruby
agent = client.beta.agents.create(
  name: "天气助手",
  model: {id: ENV.fetch("AGENTAPI_MODEL_ID")},
  tools: [
    {type: :agent_toolset_20260401},
    {
      type: :custom,
      name: "get_weather",
      description: "查询指定城市当前的天气。",
      input_schema: {
        type: :object,
        properties: {location: {type: "string", description: "城市名称"}},
        required: ["location"]
      }
    }
  ],
  request_options: {extra_body: {runtime: "native"}}
)
```

声明工具后，创建会话并发送包含天气查询要求的 `user.message`。工具声明本身不会执行查询，也不会自动提供天气服务；应用需要消费调用事件、执行查询并回传结果。

### 自定义工具定义的最佳实践

* 明确用途和参数。 说明适用场景、参数含义和重要限制，便于智能体选择工具。
* 按权限划分职责。 读取、写入和审批要求不同的操作应独立声明。避免用一个 `action` 参数包揽拉取请求的创建、审查和合并。
* 使用资源前缀。 跨服务或资源时，用 `db_query`、`storage_read` 等名称区分工具。
* 仅返回必要字段。 使用稳定的标识符（例如 slug 或 UUID），只返回智能体确定下一步所需的信息，减少上下文占用。

## 后续步骤

* [MCP 连接器](mcp.md)：将 MCP 服务器连接到智能体，以访问外部工具和数据源。

* [权限策略](permissions.md)：控制智能体和 MCP 工具何时执行。

* [事件和事件流](../sessions-and-tasks/events.md)：发送事件、流式传输响应，并在执行过程中中断执行或补充任务要求。
