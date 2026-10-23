<!-- 来源：https://developer-doc.qiniu.com/products/agentapi/quick_start/13587-agentapi-quickstart | 由 sync-docs.py 自动生成，请勿手改 -->

# 通过 API 构建



最近更新时间: 2026-10-13 15:31:07

API 方式适合将 AgentAPI 集成到自有智能体产品或业务系统，通过代码管理配置、自动提交任务，并把回答、执行进度和文件产物用于业务流程。

可以直接发送 HTTP 请求，也可以使用 SDK调用相同接口。两种方式都围绕智能体、环境、会话和事件完成任务；SDK 部分说明客户端初始化和 AgentAPI 扩展字段的传入方式。

下面的 HTTP 示例完成一次文件生成任务：创建配置与会话、发送任务、读取事件，再下载并核对前 20 个斐波那契数。完成标志是本地拿到内容正确的 `fibonacci.txt`。已在 Portal 验证的配置可以直接复用，创建会话时引用已有智能体 ID、版本和环境 ID。

![AgentAPI 从接入到事件的快速开始流程](https://developer-doc.qiniu.com/docs-assets/products/agentapi/quick_start/13587-agentapi-quickstart/Fluovb9iJZdUaLQ3_YNHj3Orn3nJ-ee3cc2b1373c.webp)

## 前提条件

* 已完成[接入准备](access.md)
* `AGENTAPI_BASE_URL`、`AGENTAPI_API_KEY` 和 `AGENTAPI_MODEL_ID` 已设置

## 使用 HTTP

以下示例需要 Bash、`curl` 和 `jq`，在同一个 Bash 终端中依次执行。

```bash
set -euo pipefail

: "${AGENTAPI_BASE_URL:?请先设置 AGENTAPI_BASE_URL}"
: "${AGENTAPI_API_KEY:?请先设置 AGENTAPI_API_KEY}"
: "${AGENTAPI_MODEL_ID:?请先设置 AGENTAPI_MODEL_ID}"
```

### 创建智能体

示例使用 `claudecode`、云端环境和内置工具集，任务不需要第三方凭证或额外网络访问。需要其他执行引擎或应用参与工具调用时，先按[运行时和模型](../development-guidelines/agent-configuration/runtime-and-models.md)选择配置。

```bash
agent=$(curl --request POST --fail-with-body --silent --show-error "$AGENTAPI_BASE_URL/v1/agents" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --data @- <<EOF
{
  "name": "快速开始助手",
  "runtime": "claudecode",
  "model": {"id": "$AGENTAPI_MODEL_ID"},
  "system": "你是一名编程助手，请完成用户任务并说明结果。",
  "tools": [{"type": "agent_toolset_20260401"}]
}
EOF
)

AGENT_ID=$(jq -er '.id' <<<"$agent")
AGENT_VERSION=$(jq -er '.version' <<<"$agent")
```

### 创建云端环境

```bash
environment=$(curl --request POST --fail-with-body --silent --show-error "$AGENTAPI_BASE_URL/v1/environments" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --data '{
    "name": "quickstart-environment",
    "config": {
      "type": "cloud",
      "networking": {"type": "limited", "allowed_hosts": []}
    }
  }')

ENVIRONMENT_ID=$(jq -er '.id' <<<"$environment")
```

### 创建会话

```bash
session=$(curl --request POST --fail-with-body --silent --show-error "$AGENTAPI_BASE_URL/v1/sessions" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --data @- <<EOF
{
  "agent": {"type": "agent", "id": "$AGENT_ID", "version": $AGENT_VERSION},
  "environment_id": "$ENVIRONMENT_ID",
  "title": "快速开始会话"
}
EOF
)

SESSION_ID=$(jq -er '.id' <<<"$session")
```

### 发送任务并读取事件

在后台订阅 SSE，再发送 `user.message`。流会回放历史，因此连接建立稍晚也能读到已保存的事件。用户消息的结构化内容使用 `input.parts`。

脚本保存本次输入的事件 ID，只接受该输入之后的停止事件，避免把创建会话时的空闲状态当作完成。它检查事件历史，不依赖轮询恰好看到短暂的 `running` 状态。

```bash
EVENT_LOG=$(mktemp)
curl --fail-with-body --silent --show-error --no-buffer \
  "$AGENTAPI_BASE_URL/v1/sessions/$SESSION_ID/events/stream" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  >"$EVENT_LOG" &
STREAM_PID=$!
trap 'kill "$STREAM_PID" 2>/dev/null || true; wait "$STREAM_PID" 2>/dev/null || true; rm -f "$EVENT_LOG"' EXIT

accepted=$(curl --request POST --fail-with-body --silent --show-error \
  "$AGENTAPI_BASE_URL/v1/sessions/$SESSION_ID/events" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --data '{
    "events": [
      {
        "type": "user.message",
        "input": {
          "parts": [
            {"type": "text", "text": "从 0、1 开始生成前 20 个斐波那契数，将这 20 个整数按每行一个的格式保存到 /mnt/session/outputs/fibonacci.txt，作为可下载文件，不添加标题或其他文字。"}
          ]
        }
      }
    ]
  }')
INPUT_EVENT_ID=$(jq -er '.data[0].id' <<<"$accepted")

stop_event=""
for _ in $(seq 1 120); do
  events=$(jq -Rsc 'split("\n") | map(
    select(startswith("data: ")) | ltrimstr("data: ") | fromjson?
  )' "$EVENT_LOG")
  input_sequence=$(jq -r --arg id "$INPUT_EVENT_ID" '
    [.[] | select(.id == $id and .processed_at != null) | .sequence_number]
    | first // empty
  ' <<<"$events")
  if [[ -n "$input_sequence" ]]; then
    stop_event=$(jq -c --argjson sequence "$input_sequence" '
      [.[] | select(.sequence_number > $sequence)
        | select(.type == "session.status_idle" or .type == "session.status_terminated")]
      | first // empty
    ' <<<"$events")
    [[ -n "$stop_event" ]] && break
  fi
  if ! kill -0 "$STREAM_PID" 2>/dev/null; then
    cat "$EVENT_LOG" >&2
    printf '事件连接已结束，请按会话 ID 恢复查询：%s\n' "$SESSION_ID" >&2
    exit 1
  fi
  sleep 1
done

kill "$STREAM_PID" 2>/dev/null || true
wait "$STREAM_PID" 2>/dev/null || true
jq '.[] | select(.type == "agent.message" or .type == "session.error")' <<<"$events"
rm -f "$EVENT_LOG"
trap - EXIT

[[ -n "$stop_event" ]] || {
  printf '120 秒内未确认本轮停止；保留会话继续查询，不要重复提交：%s\n' "$SESSION_ID" >&2
  exit 1
}
jq . <<<"$stop_event"
[[ $(jq -r '.type' <<<"$stop_event") == "session.status_idle" && \
   $(jq -r '.stop_reason.type' <<<"$stop_event") == "end_turn" ]] || {
  printf '本轮未正常结束，请先按停止原因处理，再验收产物。\n' >&2
  exit 1
}
```

本例是新建会话的一轮执行，等待上限为 120 秒。关闭本地 SSE 观察不会取消云端任务；超时后保留 `SESSION_ID`，通过 [Portal](https://portal.qiniu.com/agentapi) 或[事件历史](../development-guidelines/sessions-and-tasks/events.md)继续检查。生产应用应按[事件和事件流](../development-guidelines/sessions-and-tasks/events.md)恢复消费，并按[文件](../development-guidelines/data-and-integrations/files.md)的规则验收产物。

`end_turn` 只说明本轮结束，仍需下载验收文件。其他停止原因及后续处理见[执行结果判断](../development-guidelines/sessions-and-tasks/events.md)。

### 下载并验证结果

会话文件可能在停止事件之后才收集完成。以下代码等待 `fibonacci.txt` 出现在文件列表中，再下载；本例只生成一个文件。较大任务应按[文件列表分页](../development-guidelines/data-and-integrations/files.md)读取全部结果。

```bash
FILE_ID=""
for _ in $(seq 1 10); do
  files=$(curl --fail-with-body --silent --show-error \
    "$AGENTAPI_BASE_URL/v1/files?scope_id=$SESSION_ID" \
    --header "x-api-key: $AGENTAPI_API_KEY")
  FILE_ID=$(jq -r '[.data[] | select(.filename == "fibonacci.txt")][0].id // empty' <<<"$files")
  [[ -n "$FILE_ID" ]] && break
  sleep 1
done
[[ -n "$FILE_ID" ]] || {
  printf 'fibonacci.txt 尚未收集完成，请检查输出路径、文件限额与会话事件。\n' >&2
  exit 1
}
curl --fail-with-body --silent --show-error \
  "$AGENTAPI_BASE_URL/v1/files/$FILE_ID/content" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --output fibonacci.txt
```

验证应将文件的每一行与预期序列比较。使用下面的命令完成内容验收：

```bash
jq -Rse '
  split("\n") | map(gsub("\r$"; ""))
  | if last == "" then .[:-1] else . end
  | . == ([0,1,1,2,3,5,8,13,21,34,55,89,144,233,377,610,987,1597,2584,4181]
    | map(tostring))
' fibonacci.txt
```

输出 `true` 且退出码为 0 表示文件包含正确的 20 行整数。验收失败时，查看回答与文件内容，在原会话补充要求后重新检查；不要仅因文件存在就判定成功。

### 清理示例资源

文件下载验收后，可以删除本页创建的会话、智能体与环境。会话删除会同时清理事件和会话作用域文件；如需保留历史，改用[归档会话](../development-guidelines/sessions-and-tasks/session-lifecycle.md)。

```bash
curl --request DELETE --fail-with-body --silent --show-error \
  "$AGENTAPI_BASE_URL/v1/sessions/$SESSION_ID" --header "x-api-key: $AGENTAPI_API_KEY"
curl --request DELETE --fail-with-body --silent --show-error \
  "$AGENTAPI_BASE_URL/v1/agents/$AGENT_ID" --header "x-api-key: $AGENTAPI_API_KEY"
curl --request DELETE --fail-with-body --silent --show-error \
  "$AGENTAPI_BASE_URL/v1/environments/$ENVIRONMENT_ID" --header "x-api-key: $AGENTAPI_API_KEY"
```

业务系统中复用的智能体与环境应按自身保留策略管理；这里的删除操作只用于本页专门创建的示例资源。

## 使用 SDK

AgentAPI 的智能体、环境、会话和事件接口可以使用 Anthropic SDK 调用。客户端放在应用服务端，显式配置 AgentAPI 服务地址和已绑定的 MaaS API Key。

### 请求配置

| 配置      | 注意事项                                                                                                     |
| ------- | -------------------------------------------------------------------------------------------------------- |
| 服务地址    | 将 `base_url`、`baseURL` 或对应语言的选项设为 `AGENTAPI_BASE_URL`，使用所选区域的 AgentAPI 服务根地址，不附加 `/v1`。SDK 的资源方法会拼接接口路径。 |
| API Key | 将 `api_key`、`apiKey` 或对应选项设为 `AGENTAPI_API_KEY`，SDK 会通过 `x-api-key` 发送。Key 必须已在 Portal 绑定到 AgentAPI 账户。  |
| 模型      | 创建智能体时使用当前账户和区域可调用的模型 ID，示例从 `AGENTAPI_MODEL_ID` 读取。模型选择与 `runtime` 选择是两个独立字段。                           |

### 获取 SDK

按所用语言安装依赖，软件包、安装命令和官方源码入口见[SDK 下载](../sdk/sdk-download.md)。

### 初始化客户端

以下片段与专题中的调用代码放在同一作用域，按调用补充所需的类型导入。专题涉及的 `agent`、`environment`、`session` 等变量来自对应资源的创建或查询响应。

Python

```python
import os
from anthropic import Anthropic

client = Anthropic(
    api_key=os.environ["AGENTAPI_API_KEY"],
    base_url=os.environ["AGENTAPI_BASE_URL"],
)
```

TypeScript

````typescript
import Anthropic from "@anthropic-ai/sdk";

const client = new Anthropic({
  apiKey: process.env.AGENTAPI_API_KEY!,
  baseURL: process.env.AGENTAPI_BASE_URL,
});
```csharp
```csharp
using Anthropic;
using Anthropic.Core;

var client = new AnthropicClient(new ClientOptions
{
    ApiKey = System.Environment.GetEnvironmentVariable("AGENTAPI_API_KEY")
        ?? throw new InvalidOperationException("AGENTAPI_API_KEY is required"),
    BaseUrl = System.Environment.GetEnvironmentVariable("AGENTAPI_BASE_URL")
        ?? throw new InvalidOperationException("AGENTAPI_BASE_URL is required"),
});
````

Go

```go
import (
    "context"
    "os"

    "github.com/anthropics/anthropic-sdk-go"
    "github.com/anthropics/anthropic-sdk-go/option"
)

client := anthropic.NewClient(
    option.WithoutEnvironmentDefaults(),
    option.WithBaseURL(os.Getenv("AGENTAPI_BASE_URL")),
    option.WithAPIKey(os.Getenv("AGENTAPI_API_KEY")),
)
ctx := context.Background()
```

Java

```java
import com.anthropic.client.okhttp.AnthropicOkHttpClient;

var client = AnthropicOkHttpClient.builder()
    .apiKey(System.getenv("AGENTAPI_API_KEY"))
    .baseUrl(System.getenv("AGENTAPI_BASE_URL"))
    .build();
```

PHP

```php
use Anthropic\Client;

$client = new Client(
    apiKey: getenv('AGENTAPI_API_KEY') ?:
        throw new RuntimeException('AGENTAPI_API_KEY is required'),
    baseUrl: getenv('AGENTAPI_BASE_URL') ?:
        throw new RuntimeException('AGENTAPI_BASE_URL is required'),
);
```

Ruby

```ruby
require "anthropic"

client = Anthropic::Client.new(
  api_key: ENV.fetch("AGENTAPI_API_KEY"),
  base_url: ENV.fetch("AGENTAPI_BASE_URL")
)
```

### 传入扩展字段

`runtime` 是创建和更新智能体时的 AgentAPI 扩展字段，取值为 `claudecode`、`codex` 或 `native`；创建时省略会使用 `claudecode`，建议按任务场景显式指定（见[运行时和模型](../development-guidelines/agent-configuration/runtime-and-models.md)）。SDK 的类型未必声明该字段，需要通过扩展请求参数传入，使其出现在请求体顶层。

Python 使用 `extra_body`；TypeScript 通过第二个参数的 `body` 传入，需保留原请求字段。例如：

Python

```python
agent = client.beta.agents.create(
    name="编程助手",
    model={"id": os.environ["AGENTAPI_MODEL_ID"]},
    extra_body={"runtime": "claudecode"},
)
```

TypeScript

```typescript
const params = {
  name: "编程助手",
  model: { id: process.env.AGENTAPI_MODEL_ID! },
} satisfies Parameters<typeof client.beta.agents.create>[0];

const agent = await client.beta.agents.create(params, {
  body: { ...params, runtime: "claudecode" },
});
```

`agentapi_options` 同样通过扩展请求参数传入，具体配置见[智能体配置字段](../development-guidelines/agent-configuration/agent.md)。其他语言的扩展字段写法见[创建智能体](../development-guidelines/agent-configuration/agent.md)。

按业务步骤使用[智能体创建](../development-guidelines/agent-configuration/agent.md)、[环境创建](../development-guidelines/execution-environments/environment.md)、[会话创建](../development-guidelines/sessions-and-tasks/session.md)和[事件收发](../development-guidelines/sessions-and-tasks/events.md)中的 SDK 示例。HTTP 用户消息的结构化内容以 `input.parts` 为准，SDK 的兼容写法见[提交输入](../development-guidelines/sessions-and-tasks/events.md)；完整字段与限制以 [API 参考](../api/api-conventions.md)为准。

## 后续步骤

* [更换任务配置](../development-guidelines/agent-configuration/runtime-and-models.md)：选择不同运行时、工具或执行环境。
* [事件和事件流](../development-guidelines/sessions-and-tasks/events.md)：恢复消费，判断停止原因并处理工具等待。
* [文件](../development-guidelines/data-and-integrations/files.md)：准备输入材料、下载并验收产物。
