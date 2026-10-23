<!-- 来源：https://developer-doc.qiniu.com/products/agentapi/development_guidelines/agentapi-agent-configuration/13590-agentapi-agent-configuration-agent | 由 sync-docs.py 自动生成，请勿手改 -->

# 创建与管理智能体



最近更新时间: 2026-10-13 15:31:16

智能体（Agent）是一种可复用、带版本的配置，用于定义角色和能力。它将运行时、模型、系统提示、工具、MCP 服务器和技能组合在一起，共同决定智能体在会话中的行为。

智能体可供多个[会话](../sessions-and-tasks/session.md)复用。创建智能体仅保存配置，不启动任务或分配沙箱。更新配置时生成新版本；配置未变化时返回现有版本。

![运行时、模型、系统提示和工具能力共同组成智能体配置](https://developer-doc.qiniu.com/docs-assets/products/agentapi/development_guidelines/agentapi-agent-configuration/13590-agentapi-agent-configuration-agent/FqNUvdxm9GAx1XmKD1r5NmzyP95L-5d1ede6a5f57.webp)

## 运行时、模型和云端环境

智能体配置 `runtime` 和 `model`，创建会话时通过 `environment_id` 选择环境。运行时的执行位置与任务选型见[运行时和模型](runtime-and-models.md)；本页说明智能体字段与版本管理。

## 创建智能体

以下示例定义一个编程助手，使用 `claudecode` 运行时和所选模型，并启用内置工具集。该工具集允许智能体编写代码、读取文件和访问网页。有关支持的工具，请参阅[工具](tools.md)。

> 生成可下载的文件
>
> 云端环境默认引导智能体将下载产物写入 `/mnt/session/outputs/`，执行后可通过 Portal 文件页或 Files API 获取，无需重复添加提示。临时文件留在工作目录或临时目录，项目文件在原路径修改。自托管 worker 不提供这条目录指引。

开始前，请先按照[接入准备](../../quick-start/access.md)配置 `AGENTAPI_BASE_URL`、`AGENTAPI_API_KEY` 和 `AGENTAPI_MODEL_ID`。SDK 示例使用已经初始化的 `client`，初始化代码和依赖导入见[SDK 客户端初始化](../../quick-start/quickstart.md)。`runtime` 通过各 SDK 的扩展请求参数传入。

cURL

```bash
agent=$(curl --request POST -fsSL "$AGENTAPI_BASE_URL/v1/agents" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --data "$(jq -n --arg model "$AGENTAPI_MODEL_ID" '{
    name: "编程助手",
    runtime: "claudecode",
    model: {id: $model},
    system: "你是一名编程助手，请编写结构清晰、注释完善的代码。",
    tools: [{type: "agent_toolset_20260401"}]
  }')")

AGENT_ID=$(jq -r '.id' <<< "$agent")
AGENT_VERSION=$(jq -r '.version' <<< "$agent")
```

Python

```python
import os

agent = client.beta.agents.create(
    name="编程助手",
    model={"id": os.environ["AGENTAPI_MODEL_ID"]},
    system="你是一名编程助手，请编写结构清晰、注释完善的代码。",
    tools=[
        {"type": "agent_toolset_20260401"},
    ],
    extra_body={"runtime": "claudecode"},
)
```

TypeScript

````typescript
const params = {
  name: "编程助手",
  model: { id: process.env.AGENTAPI_MODEL_ID! },
  system: "你是一名编程助手，请编写结构清晰、注释完善的代码。",
  tools: [{ type: "agent_toolset_20260401" }],
} satisfies Parameters<typeof client.beta.agents.create>[0];
const agent = await client.beta.agents.create(params, {
  body: { ...params, runtime: "claudecode" },
});
```csharp
```csharp
using System.Linq;
using System.Text.Json;

var parameters = new AgentCreateParams
{
    Name = "编程助手",
    Model = new BetaManagedAgentsModelConfigParams
    {
        ID = Environment.GetEnvironmentVariable("AGENTAPI_MODEL_ID")!,
    },
    System = "你是一名编程助手，请编写结构清晰、注释完善的代码。",
    Tools =
    [
        new BetaManagedAgentsAgentToolset20260401Params
        {
            Type = "agent_toolset_20260401",
        },
    ],
};
var body = parameters.RawBodyData.ToDictionary(entry => entry.Key, entry => entry.Value);
body["runtime"] = JsonSerializer.SerializeToElement("claudecode");
parameters = AgentCreateParams.FromRawUnchecked(
    parameters.RawHeaderData, parameters.RawQueryData, body);
var agent = await client.Beta.Agents.Create(parameters);
````

Go

```go
agent, err := client.Beta.Agents.New(ctx, anthropic.BetaAgentNewParams{
	Name: "编程助手",
	Model: anthropic.BetaManagedAgentsModelConfigParams{
		ID: os.Getenv("AGENTAPI_MODEL_ID"),
	},
	System: anthropic.String("你是一名编程助手，请编写结构清晰、注释完善的代码。"),
	Tools: []anthropic.BetaAgentNewParamsToolUnion{{
		OfAgentToolset20260401: &anthropic.BetaManagedAgentsAgentToolset20260401Params{
			Type: anthropic.BetaManagedAgentsAgentToolset20260401ParamsTypeAgentToolset20260401,
		},
	}},
}, option.WithJSONSet("runtime", "claudecode"))
if err != nil {
	panic(err)
}
```

Java

```java
import com.anthropic.core.JsonValue;

var agent = client.beta().agents().create(
    AgentCreateParams.builder()
        .name("编程助手")
        .model(BetaManagedAgentsModelConfigParams.builder()
            .id(System.getenv("AGENTAPI_MODEL_ID")).build())
        .putAdditionalBodyProperty("runtime", JsonValue.from("claudecode"))
        .system("你是一名编程助手，请编写结构清晰、注释完善的代码。")
        .addTool(
            BetaManagedAgentsAgentToolset20260401Params.builder()
                .type(BetaManagedAgentsAgentToolset20260401Params.Type.AGENT_TOOLSET_20260401)
                .build()
        )
        .build()
);
```

PHP

```php
$agent = $client->beta->agents->create(
    name: '编程助手',
    model: ['id' => getenv('AGENTAPI_MODEL_ID')],
    system: '你是一名编程助手，请编写结构清晰、注释完善的代码。',
    tools: [
        BetaManagedAgentsAgentToolset20260401Params::with(
            type: 'agent_toolset_20260401',
        ),
    ],
    requestOptions: ['extraBodyParams' => ['runtime' => 'claudecode']],
);
```

Ruby

```ruby
agent = client.beta.agents.create(
  name: "编程助手",
  model: {id: ENV.fetch("AGENTAPI_MODEL_ID")},
  system_: "你是一名编程助手，请编写结构清晰、注释完善的代码。",
  tools: [{type: "agent_toolset_20260401"}],
  request_options: {extra_body: {runtime: "claudecode"}}
)
```

响应返回保存的配置，以及 `id`、`type`、`version`、`runtime`、`status`、`created_at` 和 `updated_at`。`version` 从 1 开始，配置发生变化时递增；未提交的可选模型配置不会自动加入响应。

```json
{
  "id": "agt_01HqR2k7vXbZ9mNpL3wYcT8f",
  "type": "agent",
  "name": "编程助手",
  "runtime": "claudecode",
  "model": { "id": "your-model-id" },
  "system": "你是一名编程助手，请编写结构清晰、注释完善的代码。",
  "tools": [{"type": "agent_toolset_20260401"}],
  "version": 1,
  "status": "ready",
  "created_at": "2026-04-03T18:24:10.412Z",
  "updated_at": "2026-04-03T18:24:10.412Z"
}
```

## 智能体配置字段

| 字段                 | 描述                                                                                                                                                                                                                                                                                                                                                                               |
| ------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `name`             | 必填。用于在 Portal 和资源列表中识别智能体的名称。                                                                                                                                                                                                                                                                                                                                                    |
| `runtime`          | 可选。指定智能体使用的运行时：`claudecode`、`codex` 或 `native`。省略时使用 `claudecode`，建议按任务场景显式指定，选择方法见[运行时和模型](runtime-and-models.md)。                                                                                                                                                       |
| `model`            | 必填。驱动智能体的模型。可以传入模型 ID 字符串，也可以传入对象，例如 `{"id": "your-model-id", "effort": {"type": "high"}}`。可用模型取决于当前账户和服务配置，请前往[七牛 MaaS 模型管理页](https://portal.qiniu.com/ai-inference/model)查看。                                                                                                                                                                                                   |
| `agentapi_options` | 可选。AgentAPI 扩展配置。`thinking` 控制 Native 思考，详见思考配置。`system_prompt_mode` 控制 `claudecode` 和 `codex` 如何放置 `system`，取值为 `append` 或 `replace`，详见系统提示词放置方式。                                                                                                                                                                                                                               |
| `system`           | 定义智能体行为和角色设定的系统提示。系统提示用于描述智能体应如何工作，[用户消息](../../api/api-conventions.md)用于描述本次要完成的任务。默认与云端环境说明一起追加到运行时产品提示词之后；需要用它替换产品提示词时，见系统提示词放置方式。                                                                                                                                                                                                                   |
| `tools`            | 智能体可用的[内置工具集](tools.md)、[MCP 工具](mcp.md)和[自定义工具](tools.md)。 |
| `mcp_servers`      | 提供标准化第三方能力的 [MCP 服务器](mcp.md)。                                                                                                                                                                                                                                            |
| `skills`           | 智能体按任务需要加载的[技能](skills.md)，包含工作说明和辅助文件。                                                                                                                                                                                                                                   |
| `description`      | 对智能体功能的描述。                                                                                                                                                                                                                                                                                                                                                                       |
| `metadata`         | 用于业务跟踪的键值对。                                                                                                                                                                                                                                                                                                                                                                      |

## 思考配置

`model.effort` 调整思考强度，支持 `low`、`medium`、`high`、`xhigh` 和 `max`。`low` 不关闭思考，`none` 是无效值。

省略 effort 时：创建智能体不发送思考强度；更新时模型 `id` 不变则保留原值，模型 `id` 改变则使用新模型的默认值。

如需关闭思考，请显式使用 `runtime: "native"`，并设置 `agentapi_options.thinking`。这是 AgentAPI 扩展字段，可以通过 SDK 的扩展请求参数传入。

| 设置                         | 行为                                                                 |
| -------------------------- | ------------------------------------------------------------------ |
| `disabled`                 | 要求模型关闭思考，优先于已保存的 effort，但不删除 effort。                               |
| `default`                  | 取消智能体层面的关闭要求，恢复服务端模型服务默认配置和已保存 effort 的正常映射；不代表强制开启思考，也不代表 medium。 |
| 创建时省略整个 `agentapi_options` | 沿用默认行为，不额外设置思考开关。                                                  |
| 更新时省略整个 `agentapi_options` | 保留已有设置；若思考已关闭，则继续保持关闭。                                             |

恢复默认、更新与校验规则如下：

* Messages 协议下，服务端若没有配置 thinking 默认值，`default` 会让上游模型自行决定是否思考；若服务端默认配置本身就是关闭，恢复默认后仍然关闭。
* Responses 协议下恢复已保存的 `reasoning.effort`，未保存 effort 时省略 reasoning 字段。
* `thinking` 取 `null`、空字符串或其他值，以及对象中出现未知字段，在创建和更新时都会返回 400。
* `claudecode` 和 `codex` 不支持此关闭选项，发送 `disabled` 会返回错误。
* `agentapi_options: {}` 是合法请求：创建时按 `thinking: "default"` 保存，更新时不改变已保存的 `thinking` 和 `system_prompt_mode`。只提交其中一个字段时，另一个字段保持原值。`system_prompt_mode` 的取值见系统提示词放置方式。

下面的示例创建一个关闭思考的智能体。请先按照[接入准备](../../quick-start/access.md)设置 `AGENTAPI_BASE_URL`、`AGENTAPI_API_KEY` 和 `AGENTAPI_MODEL_ID`，并选用支持关闭思考的 Native 模型。

cURL

```bash
agent=$(curl --request POST -fsSL "$AGENTAPI_BASE_URL/v1/agents" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --data "$(jq -n --arg model "$AGENTAPI_MODEL_ID" '{
    name: "复盘助手",
    runtime: "native",
    model: {id: $model},
    agentapi_options: {thinking: "disabled"}
  }')")

AGENT_ID=$(jq -r '.id' <<< "$agent")
AGENT_VERSION=$(jq -r '.version' <<< "$agent")
echo "$agent" | jq '.agentapi_options'
```

Go

```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/anthropics/anthropic-sdk-go"
    "github.com/anthropics/anthropic-sdk-go/option"
)

func main() {
    client := anthropic.NewClient(
        option.WithBaseURL(os.Getenv("AGENTAPI_BASE_URL")),
        option.WithAPIKey(os.Getenv("AGENTAPI_API_KEY")),
    )
    params := anthropic.BetaAgentNewParams{
        Name: "复盘助手",
        Model: anthropic.BetaManagedAgentsModelConfigParams{
            ID: os.Getenv("AGENTAPI_MODEL_ID"),
        },
    }
    params.SetExtraFields(map[string]any{
        "runtime": "native",
        "agentapi_options": map[string]any{"thinking": "disabled"},
    })
    agent, err := client.Beta.Agents.New(context.Background(), params)
    if err != nil {
        panic(err)
    }
    fmt.Println(agent.ID, agent.Version)
    fmt.Println(agent.JSON.ExtraFields["agentapi_options"].Raw())
}
```

Agent 和 AgentVersion 响应会返回显式保存的 `agentapi_options`；从未设置时省略该字段。Go SDK 从 `JSON.ExtraFields` 读取，更新时可在 `BetaAgentUpdateParams` 上使用同样的 `SetExtraFields`，并携带当前版本号。

要取消上述关闭设置，请更新为 `default`：

```bash
updated_agent=$(curl --request POST -fsSL "$AGENTAPI_BASE_URL/v1/agents/$AGENT_ID" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --data "$(jq -n --argjson version "$AGENT_VERSION" '{
    version: $version,
    agentapi_options: {thinking: "default"}
  }')")

AGENT_VERSION=$(jq -r '.version' <<< "$updated_agent")
```

> 思考配置与会话版本
>
> `agentapi_options.thinking` 随智能体版本保存。已有会话继续使用固定版本，修改智能体后需创建新会话才能使用新配置；会话覆盖 `model` 不会改变这个思考开关，但可以指定模型的 `effort`。将已关闭思考的智能体切换到其他运行时时，需要同时传入 `{"thinking":"default"}`。

Native 的 Messages 协议会发送 `thinking: {"type":"disabled"}`，不发送 `output_config.effort`；Responses 协议会发送 `reasoning: {"effort":"none"}`。具体模型必须支持对应参数，不支持时会返回模型服务的错误，AgentAPI 不会移除关闭设置后重试。关闭思考不保证固定的首字响应时间，实际延迟仍取决于模型和请求内容。

## 系统提示词放置方式

`agentapi_options.system_prompt_mode` 决定 `claudecode` 和 `codex` 如何使用智能体的 `system`。它和 `thinking` 一样是 AgentAPI 扩展，可以通过 SDK 的扩展请求参数传入。

| 取值            | 行为                                                                                                                                                                                                                                                   |
| ------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 省略，或 `append` | 默认。`system` 与云端环境的目录、文件说明组成追加内容，附加在运行时自带的产品提示词之后；云端说明位于 `system` 之后。`claudecode` 使用 `--append-system-prompt`，`codex` 使用 `developer_instructions`。保存前会去除 `system` 首尾空白。                                                                               |
| `replace`     | `system` 替换运行时产品提示词，并且不再追加到产品提示词之后。`claudecode` 使用 `--system-prompt`。`codex` 将正文写入模型指令文件，并在沙箱的 `config.toml` 中通过 `model_instructions_file` 引用；新建会话和续接会话均读取此配置，正文不再写入 `developer_instructions`。缩进和末尾换行会原样保存并传给运行时。云端环境的目录和文件说明仍然追加在正文之后，不属于被替换的产品提示词。 |

> 系统提示词替换限制
>
> `native` 不支持 `replace`，创建或更新时会返回 400。`system` 去除空白后为空时，即使保存了 `replace`，运行时仍按追加方式处理，不会清除产品提示词。

配置随智能体版本保存。已有会话继续使用创建时固定的版本；修改放置方式后，需要创建新会话才会生效。之后只更新 `system`、不带 `agentapi_options` 时，仍按已保存的模式决定是否保留原文。

```bash
agent=$(curl --request POST -fsSL "$AGENTAPI_BASE_URL/v1/agents" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  -d '{
    "name": "编程助手",
    "runtime": "claudecode",
    "model": {"id": "gpt-5.6-sol"},
    "system": "你是一名编程助手，请编写结构清晰、注释完善的代码。",
    "agentapi_options": {"system_prompt_mode": "replace"}
  }')

echo "$agent" | jq '.agentapi_options'
```

Go SDK 用 `SetExtraFields` 传入，读取时从 `JSON.ExtraFields` 取回，写法与思考配置相同。

## 智能体版本与会话

智能体的 `version` 从 1 开始。创建会话时可指定版本，或省略版本号以使用当时的最新版本。已有会话始终使用固定版本，不随智能体更新。

更新请求需要携带当前版本号，规则见更新智能体。

创建会话时，可以使用 `agent_with_overrides` 为这次会话覆盖 `model`、`system`、`tools`、`mcp_servers` 或 `skills`。覆盖只对当前会话生效，不修改智能体资源，也不会生成新版本；未覆盖的字段继承所固定的智能体版本。详见[为会话覆盖智能体配置](../sessions-and-tasks/session.md)。

![会话级智能体配置覆盖](https://developer-doc.qiniu.com/docs-assets/products/agentapi/development_guidelines/agentapi-agent-configuration/13590-agentapi-agent-configuration-agent/Fq0QhStHjqLCutUdJSoJieI4j8Vz-0a03e9f9f4a3.webp)

## 更新智能体

当配置发生变化时，更新智能体会生成一个新版本。请求必须携带当前 `version`，用于防止覆盖其他调用方的更新；版本不匹配时返回 409。

![智能体更新后生成新版本](https://developer-doc.qiniu.com/docs-assets/products/agentapi/development_guidelines/agentapi-agent-configuration/13590-agentapi-agent-configuration-agent/FpxunNTBoIZ0Xc7fVM0gihnv4KDz-ce7bef388134.webp)

cURL

```bash
updated_agent=$(curl --request POST -fsSL "$AGENTAPI_BASE_URL/v1/agents/$AGENT_ID" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  -d @- <<EOF
{
  "version": $AGENT_VERSION,
  "system": "你是一名编程助手，请始终为代码编写测试。"
}
EOF
)

echo "New version: $(jq -r '.version' <<< "$updated_agent")"
```

Python

```python
updated_agent = client.beta.agents.update(
    agent.id,
    version=agent.version,
    system="你是一名编程助手，请始终为代码编写测试。",
)

print(f"New version: {updated_agent.version}")
```

TypeScript

````typescript
const updatedAgent = await client.beta.agents.update(agent.id, {
  version: agent.version,
  system: "你是一名编程助手，请始终为代码编写测试。",
});

console.log(`New version: ${updatedAgent.version}`);
```csharp
```csharp
var updatedAgent = await client.Beta.Agents.Update(agent.ID, new()
{
    Version = agent.Version,
    System = "你是一名编程助手，请始终为代码编写测试。",
});

Console.WriteLine($"New version: {updatedAgent.Version}");
````

Go

```go
updatedAgent, err := client.Beta.Agents.Update(ctx, agent.ID, anthropic.BetaAgentUpdateParams{
	Version: anthropic.Int(agent.Version),
	System:  anthropic.String("你是一名编程助手，请始终为代码编写测试。"),
})
if err != nil {
	panic(err)
}

fmt.Printf("New version: %d\n", updatedAgent.Version)
```

Java

```java
var updatedAgent = client.beta().agents().update(
    agent.id(),
    AgentUpdateParams.builder()
        .version(agent.version())
        .system("你是一名编程助手，请始终为代码编写测试。")
        .build()
);

IO.println("New version: " + updatedAgent.version());
```

PHP

```php
$updatedAgent = $client->beta->agents->update(
    $agent->id,
    version: $agent->version,
    system: '你是一名编程助手，请始终为代码编写测试。',
);

echo "New version: {$updatedAgent->version}\n";
```

Ruby

```ruby
updated_agent = client.beta.agents.update(
  agent.id,
  version: agent.version,
  system_: "你是一名编程助手，请始终为代码编写测试。"
)

puts "New version: #{updated_agent.version}"
```

示例使用创建响应中的 `version` 更新智能体。若配置已被其他调用方修改，更新将失败。`POST /v1/agents/{id}` 要求版本号至少为 1；省略或传入更小的值会返回 400。

### 更新语义

* `version` 必填，且必须至少为 1。如果它与智能体当前版本不匹配，请求返回 409，即使提交的其他字段已与存储值一致。此时请重新读取智能体，确认最新配置后再提交更新。

* 省略的字段会被保留。 仅提交需要更改的字段。

* `agentapi_options`：省略对象或其中字段时保留原值，`{}` 不改变配置。`thinking` 和 `system_prompt_mode` 独立更新。`null`、空字符串或非法枚举会返回 400。恢复思考默认行为用 `{"thinking":"default"}`；恢复提示词追加模式用 `{"system_prompt_mode":"append"}`。详见思考配置和系统提示词放置方式。

* `name`、`system`、`description` 会整体替换为新值。`name` 无法清除；要清空 `system` 或 `description`，请传入空字符串。`replace` 模式下，`system` 的缩进和末尾换行原样保存；其他模式会去掉首尾空白。

* `model` 无法清除。提交模型对象时，`inference_geo` 和 `speed` 随对象整体替换，省略会清除已保存的旧值。`effort` 的继承规则见下表。

* 数组字段（`tools`、`mcp_servers`、`skills`）会被新数组完全替换。若要完全清除某个数组字段，请传递空数组。

* Metadata 按键合并：提交的键新增或更新，省略的键保留，值为 `null` 的键删除。

* 未发生配置变化的更新。 如果更新相对于当前版本没有产生任何变化，则不会创建新版本，并返回现有版本。

更新智能体时，模型思考强度按以下规则处理。这与[会话级模型覆盖](../sessions-and-tasks/session.md)的整体替换规则不同。

| 模型更新内容               | `effort` 行为                   |
| -------------------- | ----------------------------- |
| 模型 ID 不变，省略 `effort` | 保留该智能体已保存的思考强度。               |
| 更换模型 ID，省略 `effort`  | 清除已保存的显式强度，使用新模型的默认值。         |
| 显式提供 `effort`        | 使用指定强度，例如 `{"type": "high"}`。 |

## 智能体生命周期

您可以在 Portal 的智能体列表中按名称或 ID 搜索。搜索会去掉关键词首尾的空白，并按字面量包含关系匹配；`%`、`_` 和 `!` 不作为通配符。分页总数只统计符合搜索条件的智能体。

对应的 Portal 接口为 `GET /portal/v1/agents?page_index=1&q=关键词`，`q` 必须与 `page_index` 一起使用。公开接口 `GET /v1/agents` 不接受 `q`；使用 SDK 的游标列表时，请继续沿用该接口原有的分页参数。

| 操作   | 行为                                      |
| ---- | --------------------------------------- |
| 更新   | 当配置发生变化时生成新的智能体版本。                      |
| 列出版本 | 返回完整的版本历史，用于跟踪配置变化。                     |
| 归档   | 归档后不能创建新会话引用它；现有会话会继续运行。当前实现仍允许更新归档智能体。 |

### 列出版本

获取完整的版本历史，以跟踪智能体随时间的变化。结果是分页的，SDK 示例会自动获取每一页。

cURL

```bash
curl --request GET -fsSL "$AGENTAPI_BASE_URL/v1/agents/$AGENT_ID/versions" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  | jq -r '.data[] | "Version \(.version): \(.updated_at)"'
```

Python

```python
for version in client.beta.agents.versions.list(agent.id):
    print(f"Version {version.version}: {version.updated_at.isoformat()}")
```

TypeScript

````typescript
for await (const version of client.beta.agents.versions.list(agent.id)) {
  console.log(`Version ${version.version}: ${version.updated_at}`);
}
```csharp
```csharp
var versions = await client.Beta.Agents.Versions.List(agent.ID);
await foreach (var version in versions.Paginate())
{
    Console.WriteLine($"Version {version.Version}: {version.UpdatedAt:O}");
}
````

Go

```go
iter := client.Beta.Agents.Versions.ListAutoPaging(ctx, agent.ID, anthropic.BetaAgentVersionListParams{})
for iter.Next() {
	version := iter.Current()
	fmt.Printf("Version %d: %s\n", version.Version, version.UpdatedAt.Format(time.RFC3339))
}
if err := iter.Err(); err != nil {
	panic(err)
}
```

Java

```java
for (var version : client.beta().agents().versions().list(agent.id()).autoPager()) {
    IO.println("Version " + version.version() + ": " + version.updatedAt());
}
```

PHP

```php
foreach ($client->beta->agents->versions->list($agent->id)->pagingEachItem() as $version) {
    echo "Version {$version->version}: {$version->updatedAt->format(DateTimeInterface::ATOM)}\n";
}
```

Ruby

```ruby
client.beta.agents.versions.list(agent.id).auto_paging_each do |agent_version|
  puts "Version #{agent_version.version}: #{agent_version.updated_at.iso8601}"
end
```

### 归档智能体

归档无法撤销。现有会话会继续运行，但新会话无法引用该智能体。当前实现仍允许更新归档智能体；响应会将 `archived_at` 设置为归档时间戳。

cURL

```bash
archived=$(curl --request POST -fsSL "$AGENTAPI_BASE_URL/v1/agents/$AGENT_ID/archive" \
  --header "x-api-key: $AGENTAPI_API_KEY")

echo "Archived at: $(jq -r '.archived_at' <<< "$archived")"
```

Python

```python
archived = client.beta.agents.archive(agent.id)

print(f"Archived at: {archived.archived_at.isoformat()}")
```

TypeScript

````typescript
const archived = await client.beta.agents.archive(agent.id);
console.log(`Archived at: ${archived.archived_at}`);
```csharp
```csharp
var archived = await client.Beta.Agents.Archive(agent.ID);
Console.WriteLine($"Archived at: {archived.ArchivedAt:O}");
````

Go

```go
archived, err := client.Beta.Agents.Archive(ctx, agent.ID, anthropic.BetaAgentArchiveParams{})
if err != nil {
	panic(err)
}
fmt.Printf("Archived at: %s\n", archived.ArchivedAt.Format(time.RFC3339))
```

Java

```java
var archived = client.beta().agents().archive(agent.id());
IO.println("Archived at: " + archived.archivedAt().orElseThrow());
```

PHP

```php
$archived = $client->beta->agents->archive($agent->id);

echo "Archived at: {$archived->archivedAt->format(DateTimeInterface::ATOM)}\n";
```

Ruby

```ruby
archived = client.beta.agents.archive(agent.id)
puts "Archived at: #{archived.archived_at.iso8601}"
```

## 后续步骤

* [工具](tools.md)：配置智能体可用的操作。
* [技能](skills.md)：提供任务所需的专业知识和工作流程。
* [会话](../sessions-and-tasks/session.md)：使用已创建的智能体启动任务。
* [API 约定](../../api/api-conventions.md)：查看事件类型、速率限制和接口约束。
