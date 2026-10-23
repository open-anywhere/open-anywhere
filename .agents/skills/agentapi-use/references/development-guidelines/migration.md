<!-- 来源：https://developer-doc.qiniu.com/products/agentapi/development_guidelines/13617-agentapi-migration | 由 sync-docs.py 自动生成，请勿手改 -->

# 迁移到 AgentAPI



最近更新时间: 2026-10-19 11:33:23

本页介绍从自建智能体循环（如 LangChain 自定义循环）及其他智能体托管方案迁移到 AgentAPI 的方法。

迁移后，应用管理智能体、云端环境、会话及业务状态；AgentAPI 在云端运行智能体循环、执行内置工具并保存事件。请先按下表确定迁移范围。

## 迁移范围

| 原有架构               | 需要保留在应用中的内容          | 迁移到 AgentAPI 的内容                                                              |
| ------------------ | -------------------- | ----------------------------------------------------------------------------- |
| Managed Agents API | 业务鉴权、业务状态和结果展示       | Base URL、MaaS API Key、Agent/Environment/Session 资源，以及 AgentAPI 的 `runtime` 扩展 |
| Agent SDK（本地运行）    | 业务工具实现、审批界面和业务数据     | 云端环境、会话事件流、文件/仓库资源；本地 SDK 的进程生命周期不再由应用管理                                      |
| Responses API      | 业务输入输出转换和自定义函数       | 智能体循环、内置工具执行、云端沙箱和事件持久化                                                       |
| LangChain 自定义循环    | 检查点中的业务状态、自定义工具和业务超时 | 智能体版本、会话状态机和事件流                                                               |

使用 CMA Session API 的应用，请先按[接入准备](../quick-start/access.md)验证地址和 MaaS API Key，并核对三项差异：

* 智能体增加 AgentAPI 扩展字段 `runtime`。
* 结构化 HTTP 输入使用 `input.parts`。
* SDK 可能仍生成兼容字段 `content`。

![迁移到 AgentAPI：执行边界上移](https://developer-doc.qiniu.com/docs-assets/products/agentapi/development_guidelines/13617-agentapi-migration/Fp1VIolY-zDe2S52CohrGjysGp5L-bbddbbe016f5.webp)

## 从 Responses API 迁移

如果应用通过 `responses.create` 调用模型、遍历函数调用并将 `function_call_output` 追加到下一轮输入，那么应用本身承担了智能体循环的职责。迁移到 AgentAPI 后，这部分循环和内置工具执行可以交给云端服务。

![从 Responses API 循环迁移到 AgentAPI 事件流](https://developer-doc.qiniu.com/docs-assets/products/agentapi/development_guidelines/13617-agentapi-migration/FnH8R_YN9LR_DM-6tBk9CMdP8d-4-39ee901dc2e5.webp)

### 执行职责迁移

| 之前                                                       | 之后                                                               |
| -------------------------------------------------------- | ---------------------------------------------------------------- |
| 应用维护 Responses 输入，并在每轮调用中传递已有输出或 `previous_response_id`。 | AgentAPI 在服务端保存会话历史；应用只需发送和接收事件。                                 |
| 应用遍历 `function_call`，执行函数，再追加 `function_call_output`。    | 文件和命令工具在云端沙箱中执行；网页工具按运行时执行。应用通过 `agent.custom_tool_use` 处理自定义工具。 |
| 应用自行准备沙箱，运行智能体生成的代码。                                     | AgentAPI 根据云端环境配置准备沙箱，并在其中执行代码、文件操作和 Bash 命令。                    |
| 应用自行判断智能体循环何时结束。                                         | AgentAPI 通过状态事件报告本轮停止，应用需检查停止原因、错误与业务结果。                         |

### 代码对比

迁移前（简化的 Responses API 函数调用循环）：

Python

```python
import json

input_items = [{"role": "user", "content": task}]
while True:
    response = client.responses.create(
        model="deepseek-v3",
        input=input_items,
        tools=tools,
    )

    input_items.extend(response.output)
    function_calls = [item for item in response.output if item.type == "function_call"]
    if not function_calls:
        break

    for call in function_calls:
        result = execute_tool(call.name, json.loads(call.arguments))
        input_items.append(
            {
                "type": "function_call_output",
                "call_id": call.call_id,
                "output": result,
            }
        )
```

TypeScript

```typescript
const input: any[] = [{ role: "user", content: task }];
while (true) {
  const response = await client.responses.create({
    model: "gpt-5.6",
    input,
    tools
  });

  input.push(...response.output);
  const functionCalls = response.output.filter(
    (item) => item.type === "function_call"
  );
  if (functionCalls.length === 0) {
    break;
  }

  for (const call of functionCalls) {
    const result = await executeTool(call.name, JSON.parse(call.arguments));
    input.push({
      type: "function_call_output",
      call_id: call.call_id,
      output: result
    });
  }
}
```

迁移后（AgentAPI）：

以下示例假设已按照[接入准备](../quick-start/access.md)配置 SDK 客户端，并创建了云端环境。智能体和云端环境可以跨多个会话复用。
以下内置工具示例显式选择 `native`。模型从 `AGENTAPI_MODEL_ID` 读取，请先在[七牛 MaaS 模型管理页](https://portal.qiniu.com/ai-inference/model)获取模型 ID，并确认所用 Key 在所选区域可调用该模型。SDK 通过扩展请求参数传入运行时，客户端初始化见[通过 API 构建](../quick-start/quickstart.md)。

cURL

```bash
TASK="${TASK:-请分析当前项目并给出改进建议。}"

agent=$(
  curl --fail-with-body -sS "$AGENTAPI_BASE_URL/v1/agents" \
    -H "x-api-key: $AGENTAPI_API_KEY" \
    -H "content-type: application/json" --data @- <<EOF
{
      "name": "任务执行助手",
      "runtime": "native",
      "model": {"id": "$AGENTAPI_MODEL_ID"},
      "tools": [{"type": "agent_toolset_20260401"}]
}
EOF
)
agent_id=$(jq -r '.id' <<< "${agent}")

session_id=$(
  curl --fail-with-body -sS "$AGENTAPI_BASE_URL/v1/sessions" \
    -H "x-api-key: $AGENTAPI_API_KEY" \
    -H "content-type: application/json" --data "$(jq -n --argjson a "${agent}" --arg env "${ENVIRONMENT_ID}" \
      '{agent: {type: "agent", id: $a.id, version: $a.version}, environment_id: $env}')" \
  | jq -r '.id'
)

# 在后台打开 SSE 流，然后发送用户消息。
stream_log=$(mktemp)
curl --fail-with-body -sS -N \
  "$AGENTAPI_BASE_URL/v1/sessions/${session_id}/events/stream" \
  -H "x-api-key: $AGENTAPI_API_KEY" \
  > "${stream_log}" &
stream_pid=$!

curl --fail-with-body -sS \
  "$AGENTAPI_BASE_URL/v1/sessions/${session_id}/events" \
  -H "x-api-key: $AGENTAPI_API_KEY" \
  -H "content-type: application/json" --data "$(jq -n --arg text "${TASK}" \
    '{events: [{type: "user.message", input: {parts: [{type: "text", text: $text}]}}]}')" \
  > /dev/null

# 等待会话进入空闲状态。grep 在首次匹配时退出，并且
# 通过进程替换读取意味着 shell 不会等待
# tail（前台的 `tail -f | grep -m1` 管道会挂起：tail
# 只会在下一次写入时终止，而流空闲后不会再有写入）。
grep -m1 '"session.status_idle"' <(tail -f -n +1 "${stream_log}") > /dev/null

kill "${stream_pid}" 2>/dev/null || true
rm -f "${stream_log}"
```

Python

```python
import os

agent = client.beta.agents.create(
    name="任务执行助手",
    model={"id": os.environ["AGENTAPI_MODEL_ID"]},
    tools=[{"type": "agent_toolset_20260401"}],
    extra_body={"runtime": "native"},
)

session = client.beta.sessions.create(
    agent={"type": "agent", "id": agent.id, "version": agent.version},
    environment_id=environment.id,
)

with client.beta.sessions.events.stream(session.id) as stream:
    client.beta.sessions.events.send(
        session.id,
        events=[{"type": "user.message", "content": [{"type": "text", "text": task}]}],
    )
    for event in stream:
        if event.type == "session.status_idle":
            break
```

TypeScript

````typescript
const agentParams = {
  name: "任务执行助手",
  model: { id: process.env.AGENTAPI_MODEL_ID! },
  tools: [{ type: "agent_toolset_20260401" }]
} satisfies Parameters<typeof client.beta.agents.create>[0];
const agent = await client.beta.agents.create(agentParams, {
  body: { ...agentParams, runtime: "native" },
});

const session = await client.beta.sessions.create({
  agent: { type: "agent", id: agent.id, version: agent.version },
  environment_id: environment.id
});

const stream = await client.beta.sessions.events.stream(session.id);

await client.beta.sessions.events.send(session.id, {
  events: [
    {
      type: "user.message",
      content: [{ type: "text", text: task }]
    }
  ]
});

for await (const event of stream) {
  if (event.type === "session.status_idle") {
    break;
  }
}
```csharp
```csharp
using Anthropic.Models.Beta.Agents;
using System.Text.Json;
using System.Linq;
var agentParams = new AgentCreateParams
{
    Name = "任务执行助手",
    Model = new BetaManagedAgentsModelConfigParams { ID = System.Environment.GetEnvironmentVariable("AGENTAPI_MODEL_ID")! },
    Tools =
    [
        new BetaManagedAgentsAgentToolset20260401Params
        {
            Type = "agent_toolset_20260401",
        },
    ],
};
var agentBody = agentParams.RawBodyData.ToDictionary(entry => entry.Key, entry => entry.Value);
agentBody["runtime"] = JsonSerializer.SerializeToElement("native");
agentParams = AgentCreateParams.FromRawUnchecked(
    agentParams.RawHeaderData, agentParams.RawQueryData, agentBody);
var agent = await client.Beta.Agents.Create(agentParams);

var session = await client.Beta.Sessions.Create(new()
{
    Agent = new BetaManagedAgentsAgentParams
    {
        Type = "agent",
        ID = agent.ID,
        Version = agent.Version,
    },
    EnvironmentID = environment.ID,
});

var stream = client.Beta.Sessions.Events.StreamStreaming(session.ID);

await client.Beta.Sessions.Events.Send(session.ID, new()
{
    Events =
    [
        new BetaManagedAgentsUserMessageEventParams
        {
            Type = "user.message",
            Content = [new BetaManagedAgentsTextBlock { Type = "text", Text = task }],
        },
    ],
});

await foreach (var streamEvent in stream)
{
    if (streamEvent.Value is BetaManagedAgentsSessionStatusIdleEvent)
    {
        break;
    }
}
````

Go

```go
	agent, err := client.Beta.Agents.New(ctx, anthropic.BetaAgentNewParams{
		Name: "任务执行助手",
		Model: anthropic.BetaManagedAgentsModelConfigParams{
			ID: os.Getenv("AGENTAPI_MODEL_ID"),
		},
		Tools: []anthropic.BetaAgentNewParamsToolUnion{{
			OfAgentToolset20260401: &anthropic.BetaManagedAgentsAgentToolset20260401Params{
				Type: anthropic.BetaManagedAgentsAgentToolset20260401ParamsTypeAgentToolset20260401,
			},
		}},
	}, option.WithJSONSet("runtime", "native"))
	if err != nil {
		log.Fatal(err)
	}

	session, err := client.Beta.Sessions.New(ctx, anthropic.BetaSessionNewParams{
		Agent: anthropic.BetaSessionNewParamsAgentUnion{
			OfBetaManagedAgentsAgents: &anthropic.BetaManagedAgentsAgentParams{
				Type:    anthropic.BetaManagedAgentsAgentParamsTypeAgent,
				ID:      agent.ID,
				Version: anthropic.Int(agent.Version),
			},
		},
		EnvironmentID: environment.ID,
	})
	if err != nil {
		log.Fatal(err)
	}

	stream := client.Beta.Sessions.Events.StreamEvents(ctx, session.ID, anthropic.BetaSessionEventStreamParams{})
	defer stream.Close()

	_, err = client.Beta.Sessions.Events.Send(ctx, session.ID, anthropic.BetaSessionEventSendParams{
		Events: []anthropic.BetaManagedAgentsEventParamsUnion{{
			OfUserMessage: &anthropic.BetaManagedAgentsUserMessageEventParams{
				Type: anthropic.BetaManagedAgentsUserMessageEventParamsTypeUserMessage,
				Content: []anthropic.BetaManagedAgentsUserMessageEventParamsContentUnion{{
					OfText: &anthropic.BetaManagedAgentsTextBlockParam{
						Type: anthropic.BetaManagedAgentsTextBlockTypeText,
						Text: task,
					},
				}},
			},
		}},
	})
	if err != nil {
		log.Fatal(err)
	}

	for stream.Next() {
		event := stream.Current()
		if event.Type == "session.status_idle" {
			break
		}
	}
	if err := stream.Err(); err != nil {
		log.Fatal(err)
	}
```

Java

```java
    var agent = client.beta().agents().create(
        AgentCreateParams.builder()
        .putAdditionalBodyProperty("runtime", com.anthropic.core.JsonValue.from("native"))
            .name("任务执行助手")
            .model(BetaManagedAgentsModelConfigParams.builder().id(System.getenv("AGENTAPI_MODEL_ID")).build())
            .addTool(
                BetaManagedAgentsAgentToolset20260401Params.builder()
                    .type(BetaManagedAgentsAgentToolset20260401Params.Type.AGENT_TOOLSET_20260401)
                    .build()
            )
            .build()
    );

    var session = client.beta().sessions().create(
        SessionCreateParams.builder()
            .agent(
                BetaManagedAgentsAgentParams.builder()
                    .type(BetaManagedAgentsAgentParams.Type.AGENT)
                    .id(agent.id())
                    .version(agent.version())
                    .build()
            )
            .environmentId(environment.id())
            .build()
    );

    try (var stream = client.beta().sessions().events().streamStreaming(session.id())) {
        client.beta().sessions().events().send(
            session.id(),
            EventSendParams.builder()
                .addEvent(
                    BetaManagedAgentsUserMessageEventParams.builder()
                        .type(BetaManagedAgentsUserMessageEventParams.Type.USER_MESSAGE)
                        .addTextContent(task)
                        .build()
                )
                .build()
        );
        stream.stream()
            .takeWhile(event -> !event.isSessionStatusIdle())
            .forEach(_ -> {});
    }
```

PHP

```php
$agent = $client->beta->agents->create(
    name: '任务执行助手',
    model: ['id' => getenv('AGENTAPI_MODEL_ID')],
    tools: [
        BetaManagedAgentsAgentToolset20260401Params::with(
            type: 'agent_toolset_20260401',
        ),
    ],
    requestOptions: ['extraBodyParams' => ['runtime' => 'native']],
);

$session = $client->beta->sessions->create(
    agent: BetaManagedAgentsAgentParams::with(
        type: 'agent',
        id: $agent->id,
        version: $agent->version,
    ),
    environmentID: $environment->id,
);

$stream = $client->beta->sessions->events->streamStream($session->id);

$client->beta->sessions->events->send(
    $session->id,
    events: [
        [
            'type' => 'user.message',
            'content' => [['type' => 'text', 'text' => $task]],
        ],
    ],
);

foreach ($stream as $event) {
    if ($event->type === 'session.status_idle') {
        break;
    }
}
```

Ruby

```ruby
agent = client.beta.agents.create(
  name: "任务执行助手",
  model: {id: ENV.fetch("AGENTAPI_MODEL_ID")},
  tools: [{type: "agent_toolset_20260401"}],
  request_options: {extra_body: {runtime: "native"}}
)

session = client.beta.sessions.create(
  agent: {type: "agent", id: agent.id, version: agent.version},
  environment_id: environment.id
)

stream = client.beta.sessions.events.stream_events(session.id)
client.beta.sessions.events.send_(
  session.id,
  events: [{type: "user.message", content: [{type: "text", text: task}]}]
)
stream.each do
  break if it.type == :"session.status_idle"
end
```

### 应用职责

* 系统提示和模型： 仍由应用决定，但配置从单次模型请求移到智能体定义中。
* 自定义工具： 仍然使用 JSON Schema 声明。执行方式从循环内直接调用，改为处理 `agent.custom_tool_use` 事件并返回结果。请参阅[事件和事件流](sessions-and-tasks/events.md)。
* 网页访问： `AI工具` 使用各自运行时提供的网页能力；`native` 提供 `web_search` 和 `web_fetch`，由 AgentAPI 执行，不在自托管 worker 上运行。
* 任务上下文： 可以通过系统提示、[文件资源](data-and-integrations/files.md)或[技能](agent-configuration/skills.md)向智能体提供上下文。

## 从基于 LangChain 的自定义智能体循环迁移

如果应用使用 LangChain 的模型和工具抽象，但通过 `bind_tools` 或 `bindTools` 自行维护消息列表、分派工具并控制循环结束条件，那么迁移方式与直接调用模型类似。主要变化是将这套循环交给 AgentAPI 在云端运行，并将原有配置映射为智能体、云端环境、会话和事件。

![从 LangChain 自定义循环映射到 AgentAPI 资源](https://developer-doc.qiniu.com/docs-assets/products/agentapi/development_guidelines/13617-agentapi-migration/Fv3XV4IUNSoLf1v_vUjGvQogFkSj-3bbef18e51e4.webp)

### 架构映射

| LangChain 智能体循环                                          | AgentAPI                                                                                                                                                                                      |
| -------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 使用 `model.bind_tools(...)` 或 `model.bindTools(...)` 绑定工具 | 在[智能体定义](agent-configuration/agent.md)中配置工具，AgentAPI 在服务端保存配置并管理版本。                                        |
| 应用保存消息列表并反复调用 `model.invoke(...)`                        | 创建会话并发送用户事件；AgentAPI 保存会话历史并运行智能体循环。                                                                                                                                                          |
| 应用读取 `tool_calls`、执行工具并追加 `ToolMessage`                  | 内置工具由 AgentAPI 在云端沙箱中执行；自定义工具通过 `agent.custom_tool_use` 和 `user.custom_tool_result` 事件衔接。                                                                                                     |
| 本地工具直接访问应用所在机器的文件系统                                      | 将[文件](data-and-integrations/files.md)作为会话资源上传或挂载；智能体在隔离的云端沙箱中处理文件。                                       |
| 每次调用时传入系统提示和模型配置                                         | 在智能体的 `system` 和 `model` 中配置；更新智能体时生成新版本。                                                                                                                                                     |
| 应用自行保存检查点并恢复消息历史                                         | AgentAPI 持久保存会话和事件；应用通过会话 ID 继续交互。                                                                                                                                                            |
| 应用在循环中实现工具确认逻辑                                           | 使用 [`permission_policy`](agent-configuration/permissions.md) 控制工具是否需要确认，并通过 `user.tool_confirmation` 返回选择。 |

### 代码对比

迁移前（使用 LangChain 自行维护工具调用循环）：

Python

```python
from langchain.chat_models import init_chat_model
from langchain.tools import tool

@tool
def get_weather(city: str) -> str:
    """查询城市当前的天气。"""
    return f"{city}：18°C，晴"

model = init_chat_model("deepseek-v3", model_provider="openai", base_url="https://api.qnaigc.com/v1")
model_with_tools = model.bind_tools([get_weather])
messages = [{"role": "user", "content": "东京天气怎么样？"}]

while True:
    response = model_with_tools.invoke(messages)
    messages.append(response)
    if not response.tool_calls:
        print(response.text)
        break
    for tool_call in response.tool_calls:
        messages.append(get_weather.invoke(tool_call))
```

TypeScript

```typescript
import { initChatModel, tool } from "langchain";
import { z } from "zod";

const getWeather = tool(
  ({ city }) => `${city}：18°C，晴`,
  {
    name: "get_weather",
    description: "查询城市当前的天气。",
    schema: z.object({ city: z.string() })
  }
);

const model = await initChatModel("deepseek-v3", { modelProvider: "openai", configuration: { baseURL: "https://api.qnaigc.com/v1" } });
const modelWithTools = model.bindTools([getWeather]);
const messages: any[] = [{ role: "user", content: "东京天气怎么样？" }];

while (true) {
  const response = await modelWithTools.invoke(messages);
  messages.push(response);
  if (!response.tool_calls?.length) {
    console.log(response.text);
    break;
  }
  for (const toolCall of response.tool_calls) {
    if (toolCall.name === getWeather.name) {
      messages.push(await getWeather.invoke(toolCall));
    }
  }
}
```

迁移后（AgentAPI）：

下面选择 `native`，由应用处理天气查询工具并回传结果。天气数据在示例中由本地函数返回；接入真实服务时，在该函数中完成鉴权、参数校验、超时和错误处理。

Python

```python
import os

from anthropic import Anthropic

client = Anthropic(
    api_key=os.environ["AGENTAPI_API_KEY"],
    base_url=os.environ["AGENTAPI_BASE_URL"],
)

agent = client.beta.agents.create(
    name="天气助手",
    model={"id": os.environ["AGENTAPI_MODEL_ID"]},
    system="你是一名简洁、准确的天气助手。",
    tools=[
        {
            "type": "custom",
            "name": "get_weather",
            "description": "查询城市当前的天气。",
            "input_schema": {
                "type": "object",
                "properties": {"city": {"type": "string"}},
                "required": ["city"],
            },
        }
    ],
    extra_body={"runtime": "native"},
)
environment = client.beta.environments.create(
    name="weather-env",
    config={"type": "cloud", "networking": {"type": "unrestricted"}},
)

session = client.beta.sessions.create(
    agent={"type": "agent", "id": agent.id, "version": agent.version},
    environment_id=environment.id,
)

def get_weather(city: str) -> str:
    return f"{city}：18°C，晴"

with client.beta.sessions.events.stream(session.id) as stream:
    client.beta.sessions.events.send(
        session.id,
        events=[
            {
                "type": "user.message",
                "content": [{"type": "text", "text": "东京天气怎么样？"}],
            }
        ],
    )
    for event in stream:
        if event.type == "agent.message":
            print(
                "".join(block.text for block in event.content if block.type == "text")
            )
        elif event.type == "agent.custom_tool_use":
            result = get_weather(**event.input)
            client.beta.sessions.events.send(
                session.id,
                events=[
                    {
                        "type": "user.custom_tool_result",
                        "custom_tool_use_id": event.id,
                        "content": [{"type": "text", "text": result}],
                    }
                ],
            )
        elif (
            event.type == "session.status_idle"
            and event.stop_reason
            and event.stop_reason.type == "end_turn"
        ):
            break
```

TypeScript

````typescript
import Anthropic from "@anthropic-ai/sdk";

const client = new Anthropic({
  apiKey: process.env.AGENTAPI_API_KEY!,
  baseURL: process.env.AGENTAPI_BASE_URL,
});

const agentParams = {
  name: "天气助手",
  model: { id: process.env.AGENTAPI_MODEL_ID! },
  system: "你是一名简洁、准确的天气助手。",
  tools: [
    {
      type: "custom",
      name: "get_weather",
      description: "查询城市当前的天气。",
      input_schema: {
        type: "object",
        properties: { city: { type: "string" } },
        required: ["city"]
      }
    }
  ]
} satisfies Parameters<typeof client.beta.agents.create>[0];
const agent = await client.beta.agents.create(agentParams, {
  body: { ...agentParams, runtime: "native" },
});
const environment = await client.beta.environments.create({
  name: "weather-env",
  config: { type: "cloud", networking: { type: "unrestricted" } }
});

const session = await client.beta.sessions.create({
  agent: { type: "agent", id: agent.id, version: agent.version },
  environment_id: environment.id
});

function getWeather({ city }: Record<string, unknown>): string {
  return `${city}：18°C，晴`;
}

const stream = await client.beta.sessions.events.stream(session.id);

await client.beta.sessions.events.send(session.id, {
  events: [
    {
      type: "user.message",
      content: [{ type: "text", text: "东京天气怎么样？" }]
    }
  ]
});

for await (const event of stream) {
  if (event.type === "agent.message") {
    for (const block of event.content) {
      if (block.type === "text") {
        console.log(block.text);
      }
    }
  } else if (event.type === "agent.custom_tool_use") {
    const result = getWeather(event.input);
    await client.beta.sessions.events.send(session.id, {
      events: [
        {
          type: "user.custom_tool_result",
          custom_tool_use_id: event.id,
          content: [{ type: "text", text: result }]
        }
      ]
    });
  } else if (event.type === "session.status_idle" && event.stop_reason?.type === "end_turn") {
    break;
  }
}
```csharp
```csharp
using System.Linq;
using System.Text.Json;

using Anthropic;
using Anthropic.Core;
using Anthropic.Models.Beta.Agents;
using Anthropic.Models.Beta.Environments;
using Anthropic.Models.Beta.Sessions;
using Anthropic.Models.Beta.Sessions.Events;

var client = new AnthropicClient(new ClientOptions
{
    ApiKey = System.Environment.GetEnvironmentVariable("AGENTAPI_API_KEY")
        ?? throw new InvalidOperationException("AGENTAPI_API_KEY is required"),
    BaseUrl = System.Environment.GetEnvironmentVariable("AGENTAPI_BASE_URL")
        ?? throw new InvalidOperationException("AGENTAPI_BASE_URL is required"),
});

var agentParams = new AgentCreateParams
{
    Name = "天气助手",
    Model = new BetaManagedAgentsModelConfigParams { ID = System.Environment.GetEnvironmentVariable("AGENTAPI_MODEL_ID")! },
    System = "你是一名简洁、准确的天气助手。",
    Tools =
    [
        new BetaManagedAgentsCustomToolParams
        {
            Type = "custom",
            Name = "get_weather",
            Description = "查询城市当前的天气。",
            InputSchema = new()
            {
                Properties = new Dictionary<string, JsonElement>
                {
                    ["city"] = JsonSerializer.SerializeToElement(new { type = "string" }),
                },
                Required = ["city"],
            },
        },
    ],
};
var agentBody = agentParams.RawBodyData.ToDictionary(entry => entry.Key, entry => entry.Value);
agentBody["runtime"] = JsonSerializer.SerializeToElement("native");
agentParams = AgentCreateParams.FromRawUnchecked(
    agentParams.RawHeaderData, agentParams.RawQueryData, agentBody);
var agent = await client.Beta.Agents.Create(agentParams);
var environment = await client.Beta.Environments.Create(new()
{
    Name = "weather-env",
    Config = new BetaCloudConfigParams
    {
        Networking = new BetaUnrestrictedNetwork(),
    },
});

var session = await client.Beta.Sessions.Create(new()
{
    Agent = new BetaManagedAgentsAgentParams
    {
        Type = "agent",
        ID = agent.ID,
        Version = agent.Version,
    },
    EnvironmentID = environment.ID,
});

static string GetWeather(string city) => $"{city}：18°C，晴";

using var stream = await client.Beta.Sessions.Events.WithRawResponse.StreamStreaming(session.ID);

await client.Beta.Sessions.Events.Send(session.ID, new()
{
    Events =
    [
        new BetaManagedAgentsUserMessageEventParams
        {
            Type = "user.message",
            Content = [new BetaManagedAgentsTextBlock { Type = "text", Text = "东京天气怎么样？" }],
        },
    ],
});

await foreach (var streamEvent in stream.Enumerate())
{
    if (streamEvent.Value is BetaManagedAgentsAgentMessageEvent message)
    {
        Console.WriteLine(string.Concat(message.Content.Select(block => block.Text)));
    }
    else if (streamEvent.Value is BetaManagedAgentsAgentCustomToolUseEvent toolUse)
    {
        var result = GetWeather(toolUse.Input["city"].GetString()!);
        await client.Beta.Sessions.Events.Send(session.ID, new()
        {
            Events =
            [
                new BetaManagedAgentsUserCustomToolResultEventParams
                {
                    Type = "user.custom_tool_result",
                    CustomToolUseID = toolUse.ID,
                    Content =
                    [
                        new BetaManagedAgentsTextBlock
                        {
                            Type = "text",
                            Text = result,
                        },
                    ],
                },
            ],
        });
    }
    else if (streamEvent.Value is BetaManagedAgentsSessionStatusIdleEvent idle
        && idle.StopReason?.Value is BetaManagedAgentsSessionEndTurn)
    {
        break;
    }
}
````

Go

```go
client := anthropic.NewClient(
	option.WithoutEnvironmentDefaults(),
	option.WithBaseURL(os.Getenv("AGENTAPI_BASE_URL")),
	option.WithAPIKey(os.Getenv("AGENTAPI_API_KEY")),
)
ctx := context.Background()

agent, err := client.Beta.Agents.New(ctx, anthropic.BetaAgentNewParams{
	Name: "天气助手",
	Model: anthropic.BetaManagedAgentsModelConfigParams{
		ID: os.Getenv("AGENTAPI_MODEL_ID"),
	},
	System: anthropic.String("你是一名简洁、准确的天气助手。"),
	Tools: []anthropic.BetaAgentNewParamsToolUnion{{
		OfCustom: &anthropic.BetaManagedAgentsCustomToolParams{
			Type:        anthropic.BetaManagedAgentsCustomToolParamsTypeCustom,
			Name:        "get_weather",
			Description: "查询城市当前的天气。",
			InputSchema: anthropic.BetaManagedAgentsCustomToolInputSchemaParam{
				Properties: map[string]any{
					"city": map[string]any{"type": "string"},
				},
				Required: []string{"city"},
			},
		},
	}},
}, option.WithJSONSet("runtime", "native"))
if err != nil {
	panic(err)
}
environment, err := client.Beta.Environments.New(ctx, anthropic.BetaEnvironmentNewParams{
	Name: "weather-env",
	Config: anthropic.BetaEnvironmentNewParamsConfigUnion{
		OfCloud: &anthropic.BetaCloudConfigParams{
			Networking: anthropic.BetaCloudConfigParamsNetworkingUnion{
				OfUnrestricted: &anthropic.BetaUnrestrictedNetworkParam{},
			},
		},
	},
})
if err != nil {
	panic(err)
}

session, err := client.Beta.Sessions.New(ctx, anthropic.BetaSessionNewParams{
	Agent: anthropic.BetaSessionNewParamsAgentUnion{
		OfBetaManagedAgentsAgents: &anthropic.BetaManagedAgentsAgentParams{
			Type:    anthropic.BetaManagedAgentsAgentParamsTypeAgent,
			ID:      agent.ID,
			Version: anthropic.Int(agent.Version),
		},
	},
	EnvironmentID: environment.ID,
})
if err != nil {
	panic(err)
}

getWeather := func(city string) string {
	return fmt.Sprintf("%s：18°C，晴", city)
}

stream := client.Beta.Sessions.Events.StreamEvents(ctx, session.ID, anthropic.BetaSessionEventStreamParams{})
defer stream.Close()

_, err = client.Beta.Sessions.Events.Send(ctx, session.ID, anthropic.BetaSessionEventSendParams{
	Events: []anthropic.BetaManagedAgentsEventParamsUnion{{
		OfUserMessage: &anthropic.BetaManagedAgentsUserMessageEventParams{
			Type: anthropic.BetaManagedAgentsUserMessageEventParamsTypeUserMessage,
			Content: []anthropic.BetaManagedAgentsUserMessageEventParamsContentUnion{{
				OfText: &anthropic.BetaManagedAgentsTextBlockParam{
					Type: anthropic.BetaManagedAgentsTextBlockTypeText,
					Text: "东京天气怎么样？",
				},
			}},
		},
	}},
})
if err != nil {
	panic(err)
}

loop:
for stream.Next() {
	event := stream.Current()
	switch event.Type {
	case "agent.message":
		for _, block := range event.AsAgentMessage().Content {
			if block.Type == "text" {
				fmt.Println(block.Text)
			}
		}
	case "agent.custom_tool_use":
		toolUse := event.AsAgentCustomToolUse()
		result := getWeather(toolUse.Input["city"].(string))
		if _, err := client.Beta.Sessions.Events.Send(ctx, session.ID, anthropic.BetaSessionEventSendParams{
			Events: []anthropic.BetaManagedAgentsEventParamsUnion{{
				OfUserCustomToolResult: &anthropic.BetaManagedAgentsUserCustomToolResultEventParams{
					Type:            anthropic.BetaManagedAgentsUserCustomToolResultEventParamsTypeUserCustomToolResult,
					CustomToolUseID: toolUse.ID,
					Content: []anthropic.BetaManagedAgentsUserCustomToolResultEventParamsContentUnion{{
						OfText: &anthropic.BetaManagedAgentsTextBlockParam{
							Type: anthropic.BetaManagedAgentsTextBlockTypeText,
							Text: result,
						},
					}},
				},
			}},
		}); err != nil {
			panic(err)
		}
	case "session.status_idle":
		idle := event.AsSessionStatusIdle()
		if _, ok := idle.StopReason.AsAny().(anthropic.BetaManagedAgentsSessionEndTurn); ok {
			break loop
		}
	}
}
if err := stream.Err(); err != nil {
	panic(err)
}
```

Java

```java
import java.util.Map;
import java.util.function.Function;

import com.anthropic.client.okhttp.AnthropicOkHttpClient;
import com.anthropic.models.beta.agents.AgentCreateParams;
import com.anthropic.models.beta.agents.BetaManagedAgentsCustomToolInputSchema;
import com.anthropic.models.beta.agents.BetaManagedAgentsCustomToolParams;
import com.anthropic.models.beta.agents.BetaManagedAgentsModelConfigParams;
import com.anthropic.models.beta.environments.BetaCloudConfigParams;
import com.anthropic.models.beta.environments.BetaUnrestrictedNetwork;
import com.anthropic.models.beta.environments.EnvironmentCreateParams;
import com.anthropic.models.beta.sessions.BetaManagedAgentsAgentParams;
import com.anthropic.models.beta.sessions.SessionCreateParams;
import com.anthropic.models.beta.sessions.events.BetaManagedAgentsStreamSessionEvents;
import com.anthropic.models.beta.sessions.events.BetaManagedAgentsUserCustomToolResultEventParams;
import com.anthropic.models.beta.sessions.events.BetaManagedAgentsUserMessageEventParams;
import com.anthropic.models.beta.sessions.events.EventSendParams;

var client = AnthropicOkHttpClient.builder()
    .apiKey(System.getenv("AGENTAPI_API_KEY"))
    .baseUrl(System.getenv("AGENTAPI_BASE_URL"))
    .build();

var agent = client.beta().agents().create(AgentCreateParams.builder()
        .putAdditionalBodyProperty("runtime", com.anthropic.core.JsonValue.from("native"))
    .name("天气助手")
    .model(BetaManagedAgentsModelConfigParams.builder().id(System.getenv("AGENTAPI_MODEL_ID")).build())
    .system("你是一名简洁、准确的天气助手。")
    .addTool(BetaManagedAgentsCustomToolParams.builder()
        .type(BetaManagedAgentsCustomToolParams.Type.CUSTOM)
        .name("get_weather")
        .description("查询城市当前的天气。")
        .inputSchema(BetaManagedAgentsCustomToolInputSchema.builder()
            .properties(BetaManagedAgentsCustomToolInputSchema.Properties.builder()
                .putAdditionalProperty("city", JsonValue.from(Map.of("type", "string")))
                .build())
            .addRequired("city")
            .build())
        .build())
    .build());
var environment = client.beta().environments().create(EnvironmentCreateParams.builder()
    .name("weather-env")
    .config(BetaCloudConfigParams.builder()
        .networking(BetaUnrestrictedNetwork.builder().build())
        .build())
    .build());

var session = client.beta().sessions().create(SessionCreateParams.builder()
    .agent(BetaManagedAgentsAgentParams.builder()
        .type(BetaManagedAgentsAgentParams.Type.AGENT)
        .id(agent.id())
        .version(agent.version())
        .build())
    .environmentId(environment.id())
    .build());

Function<String, String> getWeather = city -> city + "：18°C，晴";

try (var stream = client.beta().sessions().events().streamStreaming(session.id())) {
    client.beta().sessions().events().send(
        session.id(),
        EventSendParams.builder()
            .addEvent(BetaManagedAgentsUserMessageEventParams.builder()
                .type(BetaManagedAgentsUserMessageEventParams.Type.USER_MESSAGE)
                .addTextContent("东京天气怎么样？")
                .build())
            .build());

    for (var event : (Iterable<BetaManagedAgentsStreamSessionEvents>) stream.stream()::iterator) {
        if (event.isAgentMessage()) {
            for (var block : event.asAgentMessage().content()) {
                block.text().ifPresent(textBlock -> IO.println(textBlock.text()));
            }
        } else if (event.isAgentCustomToolUse()) {
            var toolUse = event.asAgentCustomToolUse();
            var city = toolUse.input()._additionalProperties().get("city").asStringOrThrow();
            var result = getWeather.apply(city);
            client.beta().sessions().events().send(
                session.id(),
                EventSendParams.builder()
                    .addEvent(BetaManagedAgentsUserCustomToolResultEventParams.builder()
                        .type(BetaManagedAgentsUserCustomToolResultEventParams.Type.USER_CUSTOM_TOOL_RESULT)
                        .customToolUseId(toolUse.id())
                        .addTextContent(result)
                        .build())
                    .build());
        } else if (event.isSessionStatusIdle()
            && event.asSessionStatusIdle().stopReason().isEndTurn()) {
            break;
        }
    }
}
```

PHP

```php
use Anthropic\Client;
use Anthropic\Beta\Agents\BetaManagedAgentsCustomToolInputSchema;
use Anthropic\Beta\Agents\BetaManagedAgentsCustomToolParams;
use Anthropic\Beta\Sessions\BetaManagedAgentsAgentParams;

$client = new Client(
    apiKey: getenv('AGENTAPI_API_KEY') ?:
        throw new RuntimeException('AGENTAPI_API_KEY is required'),
    baseUrl: getenv('AGENTAPI_BASE_URL') ?:
        throw new RuntimeException('AGENTAPI_BASE_URL is required'),
);

$agent = $client->beta->agents->create(
    name: '天气助手',
    model: ['id' => getenv('AGENTAPI_MODEL_ID')],
    system: '你是一名简洁、准确的天气助手。',
    tools: [
        BetaManagedAgentsCustomToolParams::with(
            type: 'custom',
            name: 'get_weather',
            description: '查询城市当前的天气。',
            inputSchema: BetaManagedAgentsCustomToolInputSchema::with(
                properties: ['city' => ['type' => 'string']],
                required: ['city'],
            ),
        ),
    ],
    requestOptions: ['extraBodyParams' => ['runtime' => 'native']],
);
$environment = $client->beta->environments->create(
    name: 'weather-env',
    config: ['type' => 'cloud', 'networking' => ['type' => 'unrestricted']],
);

$session = $client->beta->sessions->create(
    agent: BetaManagedAgentsAgentParams::with(
        type: 'agent',
        id: $agent->id,
        version: $agent->version,
    ),
    environmentID: $environment->id,
);

function getWeather(string $city): string
{
    return "{$city}：18°C，晴";
}

$stream = $client->beta->sessions->events->streamStream($session->id);

$client->beta->sessions->events->send(
    $session->id,
    events: [
        [
            'type' => 'user.message',
            'content' => [['type' => 'text', 'text' => "东京天气怎么样？"]],
        ],
    ],
);

foreach ($stream as $event) {
    if ($event->type === 'agent.message') {
        foreach ($event->content as $block) {
            if ($block->type === 'text') {
                echo $block->text . "\n";
            }
        }
    } elseif ($event->type === 'agent.custom_tool_use') {
        $result = getWeather($event->input['city']);
        $client->beta->sessions->events->send(
            $session->id,
            events: [
                [
                    'type' => 'user.custom_tool_result',
                    'custom_tool_use_id' => $event->id,
                    'content' => [['type' => 'text', 'text' => $result]],
                ],
            ],
        );
    } elseif ($event->type === 'session.status_idle' && $event->stopReason?->type === 'end_turn') {
        break;
    }
}
$stream->close();
```

Ruby

```ruby
require "anthropic"

client = Anthropic::Client.new(
  api_key: ENV.fetch("AGENTAPI_API_KEY"),
  base_url: ENV.fetch("AGENTAPI_BASE_URL")
)

agent = client.beta.agents.create(
  name: "天气助手",
  model: {id: ENV.fetch("AGENTAPI_MODEL_ID")},
  system_: "你是一名简洁、准确的天气助手。",
  tools: [
    {
      type: "custom",
      name: "get_weather",
      description: "查询城市当前的天气。",
      input_schema: {
        type: "object",
        properties: {city: {type: "string"}},
        required: ["city"]
      }
    }
  ],
  request_options: {extra_body: {runtime: "native"}}
)
environment = client.beta.environments.create(
  name: "weather-env",
  config: {type: "cloud", networking: {type: "unrestricted"}}
)

session = client.beta.sessions.create(
  agent: {type: "agent", id: agent.id, version: agent.version},
  environment_id: environment.id
)

def get_weather(city)
  "#{city}：18°C，晴"
end

stream = client.beta.sessions.events.stream_events(session.id)
client.beta.sessions.events.send_(
  session.id,
  events: [{type: "user.message", content: [{type: "text", text: "东京天气怎么样？"}]}]
)

stream.each do |event|
  case event.type
  when :"agent.message"
    event.content.each do |block|
      puts block.text if block.type == :text
    end
  when :"agent.custom_tool_use"
    result = get_weather(event.input[:city])
    client.beta.sessions.events.send_(
      session.id,
      events: [
        {
          type: "user.custom_tool_result",
          custom_tool_use_id: event.id,
          content: [{type: "text", text: result}]
        }
      ]
    )
  when :"session.status_idle"
    break if event.stop_reason&.type == :end_turn
  end
end
```

智能体和云端环境创建后可在多个会话中复用。自定义工具函数仍在应用进程中运行。迁移后，应用通过读取 `agent.custom_tool_use` 事件并返回工具结果参与执行，模型调用循环由 AgentAPI 管理。

### 业务逻辑保留范围

迁移到云端智能体循环后，以下与应用业务相关的逻辑仍需要保留：

| 原有逻辑                | AgentAPI 中的处理方式                                                                                                              |
| ------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| 消息输入和输出转换           | 在发送 `user.message` 前或接收 `agent.message` 后，由应用继续完成格式转换和业务处理。                                                                  |
| 自定义工具前后的校验或审计       | 在处理 `agent.custom_tool_use` 和发送 `user.custom_tool_result` 时执行。对于 `native` 内置工具，可使用 `permission_policy: always_ask` 在执行前请求确认。 |
| LangChain 检查点中的业务状态 | 将业务状态保留在应用自己的存储中；会话消息和执行事件由 AgentAPI 保存。                                                                                     |
| 循环次数或业务超时限制         | 由应用根据事件和时间进行控制，必要时发送 `user.interrupt` 中断当前任务。                                                                                |

## 迁移清单

1. [创建一个云端环境](execution-environments/environment.md)，配置任务所需的软件包；使用 `limited` 网络时，放行 MCP 服务和软件源的实际主机。
2. 将系统提示、模型、运行时和工具配置迁移到[智能体定义](agent-configuration/agent.md)中，并保存返回的版本号。
3. 使用明确的智能体版本创建会话。传入智能体 ID 字符串时，只会在创建会话时解析并固定最新版本；已有会话不会随智能体更新切换。
4. 使用 [`sessions.create`](sessions-and-tasks/session.md) 和 [`sessions.events.stream`](sessions-and-tasks/events.md) 替换原有的智能体循环。客户端必须处理历史事件回放、`requires_action`、`interrupted`、`retries_exhausted` 和 `terminated`。
5. 智能体需要读取本地文件时，通过 [Files API](data-and-integrations/files.md) 上传文件，并将其作为 `resources` 挂载到会话中。
6. 将自定义工具处理程序接入事件循环，用于响应 `agent.custom_tool_use` 事件并返回结果；`AI工具` 还需要服务端已配置自定义工具桥接；本页自定义工具示例选择 `native`，直接通过事件回传结果。
7. 用同一批任务对比旧系统与 AgentAPI 的事件、产物和失败恢复；业务状态仍保留在应用自己的存储中。
8. 在将生产流量指向新流程之前，先用测试会话进行验证，并为调度任务设计业务幂等键。

## 更新智能体使用的模型

需要切换模型或模型版本时，更新[智能体定义](agent-configuration/agent.md)中的 `model` 即可。更新会生成新的智能体版本；新建会话时引用该版本，现有会话仍然使用创建时固定的智能体版本。

下面将目标模型 ID 保存在 `AGENTAPI_MODEL_ID` 中。请先在[七牛 MaaS 模型管理页](https://portal.qiniu.com/ai-inference/model)确认账户和区域可用的模型，再设置该变量。

cURL

```bash
curl --request POST "$AGENTAPI_BASE_URL/v1/agents/$AGENT_ID" \
  -H "x-api-key: $AGENTAPI_API_KEY" \
  -H "content-type: application/json" \
  --data "$(jq -n --argjson version "$AGENT_VERSION" --arg model "$AGENTAPI_MODEL_ID" \
    '{version: $version, model: {id: $model}}')"
```

Python

```python
client.beta.agents.update(
    agent.id,
    version=agent.version,
    model={"id": os.environ["AGENTAPI_MODEL_ID"]},
)
```

TypeScript

````typescript
await client.beta.agents.update(agent.id, {
  version: agent.version,
  model: { id: process.env.AGENTAPI_MODEL_ID! }
});
```csharp
```csharp
await client.Beta.Agents.Update(agent.ID, new()
{
    Version = agent.Version,
    Model = new BetaManagedAgentsModelConfigParams
    {
        ID = System.Environment.GetEnvironmentVariable("AGENTAPI_MODEL_ID")
            ?? throw new InvalidOperationException("AGENTAPI_MODEL_ID is required"),
    },
});
````

Go

```go
_, err = client.Beta.Agents.Update(ctx, agent.ID, anthropic.BetaAgentUpdateParams{
	Version: agent.Version,
	Model: anthropic.BetaManagedAgentsModelConfigParams{
		ID: os.Getenv("AGENTAPI_MODEL_ID"),
	},
})
if err != nil {
	panic(err)
}
```

Java

```java
client.beta().agents().update(
    agent.id(),
    AgentUpdateParams.builder()
        .version(agent.version())
        .model(BetaManagedAgentsModelConfigParams.builder()
            .id(System.getenv("AGENTAPI_MODEL_ID"))
            .build())
        .build()
);
```

PHP

```php
$client->beta->agents->update(
    $agent->id,
    version: $agent->version,
    model: ['id' => getenv('AGENTAPI_MODEL_ID')],
);
```

Ruby

```ruby
client.beta.agents.update(
  agent.id,
  version: agent.version,
  model: {id: ENV.fetch("AGENTAPI_MODEL_ID")}
)
```

更新模型时，还需要注意以下差异：

* 模型行为： 不同模型的工具选择、输出风格和任务完成效果可能不同，切换前应使用测试会话验证关键任务。
* 请求参数： 模型请求由 AgentAPI 在智能体循环中发起。应用无需在每一轮请求中重复设置 `max_tokens` 或推理参数。
* 会话输入： AgentAPI 使用事件驱动的会话模型，不支持通过预填充助手消息来约束下一段输出。
* 工具参数： `agent.custom_tool_use` 事件提供结构化的工具输入，应用无需自行解析原始 JSON 字符串。
