<!-- 来源：https://developer-doc.qiniu.com/products/agentapi/development_guidelines/agentapi-sessions-and-tasks/13599-agentapi-sessions-and-tasks-outcomes | 由 sync-docs.py 自动生成，请勿手改 -->

# 定义结果



最近更新时间: 2026-10-13 15:31:39

结果（Outcome）为会话定义交付目标与评分标准，适合需要生成、检查并反复修改产物的任务。例如，让智能体生成一份数据质量检查方案，再逐项检查方案是否包含检查方法和处理建议。

发送 `user.define_outcome` 后，AgentAPI 会运行智能体并配置一个评分器（grader），根据评分标准（rubric）检查产物。评分器与主智能体使用独立的上下文，在同一个会话沙箱中检查输出文件。

评分器说明各项标准是否通过，智能体根据反馈继续修改。

迭代会在满足标准、达到次数上限、评估失败或被中断时停止。`max_iterations` 默认是 3，可设置为 1–20；它限制执行与评估的轮数，不是“保证达标”的承诺。

![面向结果的迭代循环](https://developer-doc.qiniu.com/docs-assets/products/agentapi/development_guidelines/agentapi-sessions-and-tasks/13599-agentapi-sessions-and-tasks-outcomes/FqHjk8KUZqZW4lBDhuj11rgR_ViT-3ddcd4de727e.webp)

## 接入结果任务

1. 创建[智能体](../agent-configuration/agent.md)与[云端环境](../execution-environments/environment.md)，确保运行时能生成并读取任务需要的文件。
2. 编写评分标准，作为文本发送，或上传文件后引用其 ID。
3. 创建会话，发送 `user.define_outcome`，设置交付目标和迭代上限。
4. 消费事件，按 `outcome_id` 跟踪评估；区分需要继续修改的 `needs_revision` 与最终结果。
5. 确认最终结果，列出并下载预期交付物，再进行业务侧验收。

本页示例生成 Markdown 格式的数据质量检查方案。已有输入材料时，可在创建会话时挂载[文件](../data-and-integrations/files.md)或[仓库](../data-and-integrations/github.md)。

## 创建评分标准

评分标准是必填的 Markdown 文本，用于说明每一项通过条件。结果描述说明交付目标，评分标准定义交付物的验收条件；两者应对应同一个目标。

评分标准编写建议

评分标准应包含明确、可验证的条目，例如“CSV 包含数值类型的价格列”。避免使用“数据质量良好”等缺少验收条件的描述。评分器会逐条独立评分，含糊的标准可能导致评分结果不稳定。

如果尚未制定评分标准，可以提供一个符合预期的产物示例，由智能体分析其满足的可验证条件，再将这些条件整理成评分标准。

示例评分标准：

```markdown
# 数据质量方案评分标准

## 检查范围
- 覆盖缺失值、重复记录和字段类型三类问题
- 每类问题都说明检测方法和处理建议

## 输出质量
- 交付一份名为 report.md 的 Markdown 文档
- 文档包含检查步骤、示例和适用限制
```

将评分标准作为内联文本传递给 `user.define_outcome`（参见创建带有结果的会话），或通过 Files API 上传，以便在多个会话中重复使用。

请先完成[接入准备](../../quick-start/access.md)，从[七牛 MaaS 模型管理页](https://portal.qiniu.com/ai-inference/model)选择模型并设置 `AGENTAPI_MODEL_ID`。创建智能体时显式设置 `runtime`；会话继承该运行时。本页 SDK 示例配置 AgentAPI 服务地址与 MaaS API Key。

若使用 cURL 上传评分文件，请先把上面的评分标准写入本地 `/tmp/rubric.md`。以下 SDK 示例会创建该文件并上传；使用内联评分标准时，可跳过上传步骤。

cURL

```bash
rubric=$(curl --request POST --fail-with-body --silent --show-error "$AGENTAPI_BASE_URL/v1/files" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --form "file=@/tmp/rubric.md")
rubric_id=$(jq -er '.id' <<<"$rubric")
printf 'Uploaded rubric: %s\n' "$rubric_id"
```

Python

```python
import os
from pathlib import Path

from anthropic import Anthropic

client = Anthropic(
    api_key=os.environ["AGENTAPI_API_KEY"],
    base_url=os.environ["AGENTAPI_BASE_URL"],
)

RUBRIC = """# 数据质量方案评分标准

## 检查范围
- 覆盖缺失值、重复记录和字段类型三类问题
- 每类问题都说明检测方法和处理建议

## 输出质量
- 交付一份名为 report.md 的 Markdown 文档
- 文档包含检查步骤、示例和适用限制
"""
Path("/tmp/rubric.md").write_text(RUBRIC)

rubric = client.beta.files.upload(file=Path("/tmp/rubric.md"))
print(f"Uploaded rubric: {rubric.id}")
```

TypeScript

````typescript
import { writeFile, readFile } from "node:fs/promises";

import Anthropic from "@anthropic-ai/sdk";
import { toFile } from "@anthropic-ai/sdk";

const client = new Anthropic({
  apiKey: process.env.AGENTAPI_API_KEY,
  baseURL: process.env.AGENTAPI_BASE_URL,
});

const RUBRIC = `# 数据质量方案评分标准

## 检查范围
- 覆盖缺失值、重复记录和字段类型三类问题
- 每类问题都说明检测方法和处理建议

## 输出质量
- 交付一份名为 report.md 的 Markdown 文档
- 文档包含检查步骤、示例和适用限制
`;
await writeFile("/tmp/rubric.md", RUBRIC);

const rubric = await client.beta.files.upload({
  file: await toFile(await readFile("/tmp/rubric.md"), "rubric.md"),
});
console.log(`Uploaded rubric: ${rubric.id}`);
```csharp
```csharp
using Anthropic;
using Anthropic.Core;
using Anthropic.Models.Beta.Agents;
using Anthropic.Models.Beta.Environments;
using Anthropic.Models.Beta.Sessions;
using Anthropic.Models.Beta.Sessions.Events;
using Anthropic.Models.Files;

var client = new AnthropicClient(new ClientOptions
{
    ApiKey = Environment.GetEnvironmentVariable("AGENTAPI_API_KEY")
        ?? throw new InvalidOperationException("AGENTAPI_API_KEY is required"),
    BaseUrl = Environment.GetEnvironmentVariable("AGENTAPI_BASE_URL")
        ?? throw new InvalidOperationException("AGENTAPI_BASE_URL is required"),
});

const string Rubric = """
    # 数据质量方案评分标准

    ## 检查范围
    - 覆盖缺失值、重复记录和字段类型三类问题
    - 每类问题都说明检测方法和处理建议

    ## 输出质量
    - 交付一份名为 report.md 的 Markdown 文档
    - 文档包含检查步骤、示例和适用限制
    """;
await File.WriteAllTextAsync("/tmp/rubric.md", Rubric);

var rubric = await client.Beta.Files.Upload(new()
{
    File = File.OpenRead("/tmp/rubric.md"),
});
Console.WriteLine($"Uploaded rubric: {rubric.ID}");
````

Go

```go
package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const rubric = `# 数据质量方案评分标准

## 检查范围
- 覆盖缺失值、重复记录和字段类型三类问题
- 每类问题都说明检测方法和处理建议

## 输出质量
- 交付一份名为 report.md 的 Markdown 文档
- 文档包含检查步骤、示例和适用限制
`

func main() {
	ctx := context.Background()
	client := anthropic.NewClient(
		option.WithoutEnvironmentDefaults(),
		option.WithBaseURL(os.Getenv("AGENTAPI_BASE_URL")),
		option.WithAPIKey(os.Getenv("AGENTAPI_API_KEY")),
	)

	if err := os.WriteFile("/tmp/rubric.md", []byte(rubric), 0o644); err != nil {
		panic(err)
	}

	f, err := os.Open("/tmp/rubric.md")
	if err != nil {
		panic(err)
	}
	defer f.Close()

	uploaded, err := client.Beta.Files.Upload(ctx, anthropic.BetaFileUploadParams{
		File: anthropic.File(f, "rubric.md", "text/markdown"),
	})
	if err != nil {
		panic(err)
	}
	fmt.Printf("Uploaded rubric: %s\n", uploaded.ID)
}
```

Java

```java
import com.anthropic.client.okhttp.AnthropicOkHttpClient;
import com.anthropic.core.http.HttpResponse;
import com.anthropic.models.beta.AnthropicBeta;
import com.anthropic.models.beta.agents.AgentCreateParams;
import com.anthropic.models.beta.agents.BetaManagedAgentsAgentToolset20260401Params;
import com.anthropic.models.beta.agents.BetaManagedAgentsModel;
import com.anthropic.models.beta.environments.BetaCloudConfigParams;
import com.anthropic.models.beta.environments.EnvironmentCreateParams;
import com.anthropic.models.beta.files.FileListParams;
import com.anthropic.models.beta.sessions.SessionCreateParams;
import com.anthropic.models.beta.sessions.events.BetaManagedAgentsTextRubricParams;
import com.anthropic.models.beta.sessions.events.BetaManagedAgentsUserDefineOutcomeEventParams;
import com.anthropic.models.beta.sessions.events.BetaManagedAgentsUserInterruptEventParams;
import com.anthropic.models.beta.sessions.events.EventSendParams;
import com.anthropic.models.files.FileUploadParams;

import java.io.InputStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.StandardCopyOption;

void main() throws Exception {
    var client = AnthropicOkHttpClient.builder()
        .apiKey(System.getenv("AGENTAPI_API_KEY"))
        .baseUrl(System.getenv("AGENTAPI_BASE_URL"))
        .build();

    var RUBRIC = """
        # 数据质量方案评分标准

        ## 检查范围
        - 覆盖缺失值、重复记录和字段类型三类问题
        - 每类问题都说明检测方法和处理建议

        ## 输出质量
        - 交付一份名为 report.md 的 Markdown 文档
        - 文档包含检查步骤、示例和适用限制
        """;
    Files.writeString(Path.of("/tmp/rubric.md"), RUBRIC);

    var rubric = client.beta().files().upload(
        FileUploadParams.builder()
            .file(Path.of("/tmp/rubric.md"))
            .build());
    IO.println("Uploaded rubric: " + rubric.id());
}
```

PHP

```php
use Anthropic\Client;
use Anthropic\Core\FileParam;

$client = new Client(
    apiKey: getenv('AGENTAPI_API_KEY') ?:
        throw new RuntimeException('AGENTAPI_API_KEY is required'),
    baseUrl: getenv('AGENTAPI_BASE_URL') ?:
        throw new RuntimeException('AGENTAPI_BASE_URL is required'),
);

$rubricText = <<<'MD'
# 数据质量方案评分标准

## 检查范围
- 覆盖缺失值、重复记录和字段类型三类问题
- 每类问题都说明检测方法和处理建议

## 输出质量
- 交付一份名为 report.md 的 Markdown 文档
- 文档包含检查步骤、示例和适用限制
MD;
file_put_contents('/tmp/rubric.md', $rubricText);

$rubric = $client->beta->files->upload(
    file: FileParam::fromResource(fopen('/tmp/rubric.md', 'r'), contentType: 'text/markdown'),
);
echo "Uploaded rubric: {$rubric->id}\n";
```

Ruby

```ruby
require "anthropic"
require "pathname"

client = Anthropic::Client.new(
  api_key: ENV.fetch("AGENTAPI_API_KEY"),
  base_url: ENV.fetch("AGENTAPI_BASE_URL")
)

RUBRIC = <<~MD
  # 数据质量方案评分标准

  ## 检查范围
  - 覆盖缺失值、重复记录和字段类型三类问题
  - 每类问题都说明检测方法和处理建议

  ## 输出质量
  - 交付一份名为 report.md 的 Markdown 文档
  - 文档包含检查步骤、示例和适用限制
MD
File.write("/tmp/rubric.md", RUBRIC)

rubric = client.beta.files.upload(file: Pathname.new("/tmp/rubric.md"))
puts "Uploaded rubric: #{rubric.id}"
```

## 创建带有结果的会话

以下示例使用已创建的智能体和云端环境，先创建会话，再发送 `user.define_outcome`。该事件本身会启动任务，不需要额外发送 `user.message`。智能体应已启用内置工具集，支持写入方案文件与读取评分器反馈；完整创建示例见[智能体](../agent-configuration/agent.md)。

cURL

```bash
# 创建会话
session=$(curl --request POST --fail-with-body --silent --show-error "$AGENTAPI_BASE_URL/v1/sessions" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" --data @- <<EOF
{
  "agent": {"type": "agent", "id": "$agent_id", "version": 1},
  "environment_id": "$environment_id",
  "title": "数据质量检查方案"
}
EOF
)
session_id=$(jq -r '.id' <<<"$session")

# 定义结果，智能体收到后即开始工作
curl --request POST --fail-with-body --silent --show-error "$AGENTAPI_BASE_URL/v1/sessions/$session_id/events" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" --data @- >/dev/null <<EOF
{
  "events": [
    {
      "type": "user.define_outcome",
      "description": "生成一份名为 report.md 的 Markdown 数据质量检查方案，说明如何检测和处理缺失值、重复记录及字段类型问题，并列出示例和适用限制。",
      "rubric": {"type": "file", "file_id": "$rubric_id"},
      "max_iterations": 5
    }
  ]
}
EOF
# 或："rubric": {"type": "text", "content": "- 覆盖缺失值、重复记录和字段类型\n- 每类问题说明检测方法和处理建议\n- 交付 report.md，包含检查步骤、示例和适用限制"}
# "max_iterations" 为可选项；默认 3，最大 20
```

Python

```python
# 创建会话
session = client.beta.sessions.create(
    agent=agent.id,
    environment_id=environment.id,
    title="数据质量检查方案",
)

# 定义结果，智能体收到后即开始工作
client.beta.sessions.events.send(
    session_id=session.id,
    events=[
        {
            "type": "user.define_outcome",
            "description": "生成一份名为 report.md 的 Markdown 数据质量检查方案，说明如何检测和处理缺失值、重复记录及字段类型问题，并列出示例和适用限制。",
            "rubric": {"type": "text", "content": RUBRIC},
            # 或："rubric": {"type": "file", "file_id": rubric.id},
            "max_iterations": 5,  # optional; default 3, max 20
        }
    ],
)
```

TypeScript

````typescript
// 创建会话
const session = await client.beta.sessions.create({
  agent: agent.id,
  environment_id: environment.id,
  title: "数据质量检查方案",
});

// 定义结果，智能体收到后即开始工作
await client.beta.sessions.events.send(session.id, {
  events: [
    {
      type: "user.define_outcome",
      description: "生成一份名为 report.md 的 Markdown 数据质量检查方案，说明如何检测和处理缺失值、重复记录及字段类型问题，并列出示例和适用限制。",
      rubric: { type: "text", content: RUBRIC },
      // 或：rubric: { type: "file", file_id: rubric.id },
      max_iterations: 5, // optional; default 3, max 20
    },
  ],
});
```csharp
```csharp
// 创建会话
var session = await client.Beta.Sessions.Create(new()
{
    Agent = agent.ID,
    EnvironmentID = environment.ID,
    Title = "数据质量检查方案",
});

// 定义结果，智能体收到后即开始工作
await client.Beta.Sessions.Events.Send(session.ID, new()
{
    Events =
    [
        new BetaManagedAgentsUserDefineOutcomeEventParams
        {
            Type = BetaManagedAgentsUserDefineOutcomeEventParamsType.UserDefineOutcome,
            Description = "生成一份名为 report.md 的 Markdown 数据质量检查方案，说明如何检测和处理缺失值、重复记录及字段类型问题，并列出示例和适用限制。",
            Rubric = new BetaManagedAgentsTextRubricParams
            {
                Type = BetaManagedAgentsTextRubricParamsType.Text,
                Content = Rubric,
            },
            // 或：Rubric = new BetaManagedAgentsFileRubricParams
            //     { Type = BetaManagedAgentsFileRubricParamsType.File, FileID = rubric.ID },
            MaxIterations = 5, // optional; default 3, max 20
        },
    ],
});
````

Go

```go
// 创建会话
session, err := client.Beta.Sessions.New(ctx, anthropic.BetaSessionNewParams{
	Agent: anthropic.BetaSessionNewParamsAgentUnion{
		OfString: anthropic.String(agent.ID),
	},
	EnvironmentID: environment.ID,
	Title:         anthropic.String("数据质量检查方案"),
})
if err != nil {
	panic(err)
}

// 定义结果，智能体收到后即开始工作
_, err = client.Beta.Sessions.Events.Send(ctx, session.ID, anthropic.BetaSessionEventSendParams{
	Events: []anthropic.BetaManagedAgentsEventParamsUnion{{
		OfUserDefineOutcome: &anthropic.BetaManagedAgentsUserDefineOutcomeEventParams{
			Type:        anthropic.BetaManagedAgentsUserDefineOutcomeEventParamsTypeUserDefineOutcome,
			Description: "生成一份名为 report.md 的 Markdown 数据质量检查方案，说明如何检测和处理缺失值、重复记录及字段类型问题，并列出示例和适用限制。",
			Rubric: anthropic.BetaManagedAgentsUserDefineOutcomeEventParamsRubricUnion{
				OfText: &anthropic.BetaManagedAgentsTextRubricParams{
					Type:    anthropic.BetaManagedAgentsTextRubricParamsTypeText,
					Content: rubric,
				},
			},
			// 或：OfFile: &anthropic.BetaManagedAgentsFileRubricParams{
			//     Type: anthropic.BetaManagedAgentsFileRubricParamsTypeFile, FileID: uploaded.ID},
			MaxIterations: anthropic.Int(5), // optional; default 3, max 20
		},
	}},
})
if err != nil {
	panic(err)
}
```

Java

```java
// 创建会话
var session = client.beta().sessions().create(
    SessionCreateParams.builder()
        .agent(agent.id())
        .environmentId(environment.id())
        .title("数据质量检查方案")
        .build());

// 定义结果，智能体收到后即开始工作
client.beta().sessions().events().send(
    session.id(),
    EventSendParams.builder()
        .addEvent(BetaManagedAgentsUserDefineOutcomeEventParams.builder()
            .type(BetaManagedAgentsUserDefineOutcomeEventParams.Type.USER_DEFINE_OUTCOME)
            .description("生成一份名为 report.md 的 Markdown 数据质量检查方案，说明如何检测和处理缺失值、重复记录及字段类型问题，并列出示例和适用限制。")
            .rubric(BetaManagedAgentsTextRubricParams.builder()
                .type(BetaManagedAgentsTextRubricParams.Type.TEXT)
                .content(RUBRIC)
                .build())
            // 或：.rubric(BetaManagedAgentsFileRubricParams.builder()
            //     .type(BetaManagedAgentsFileRubricParams.Type.FILE).fileId(rubric.id()).build())
            .maxIterations(5) // optional; default 3, max 20
            .build())
        .build());
```

PHP

```php
// 创建会话
$session = $client->beta->sessions->create(
    agent: $agent->id,
    environmentID: $environment->id,
    title: '数据质量检查方案',
);

// 定义结果，智能体收到后即开始工作
$client->beta->sessions->events->send(
    $session->id,
    events: [
        [
            'type' => 'user.define_outcome',
            'description' => '生成一份名为 report.md 的 Markdown 数据质量检查方案，说明如何检测和处理缺失值、重复记录及字段类型问题，并列出示例和适用限制。',
            'rubric' => ['type' => 'text', 'content' => $rubricText],
            // 或：'rubric' => ['type' => 'file', 'file_id' => $rubric->id],
            'max_iterations' => 5, // optional; default 3, max 20
        ],
    ],
);
```

Ruby

```ruby
# 创建会话
session = client.beta.sessions.create(
  agent: agent.id,
  environment_id: environment.id,
  title: "数据质量检查方案"
)

# 定义结果，智能体收到后即开始工作
client.beta.sessions.events.send_(
  session.id,
  events: [
    {
      type: "user.define_outcome",
      description: "生成一份名为 report.md 的 Markdown 数据质量检查方案，说明如何检测和处理缺失值、重复记录及字段类型问题，并列出示例和适用限制。",
      rubric: {type: "text", content: RUBRIC},
      # 或：rubric: {type: "file", file_id: rubric.id},
      max_iterations: 5 # optional; default 3, max 20
    }
  ]
)
```

> 说明
>
> `POST /v1/sessions` 不接受 `initial_events`，需创建会话后再发送 `user.define_outcome`。需要创建时启动结果任务，可在[定时部署](scheduled-deployments.md)的 `initial_events` 中配置该事件。

## 结果事件

结果任务的生成、工具调用与评估进度都通过同一条[事件和事件流](events.md)返回。应用应按 `outcome_id` 关联评估事件，并保存事件游标以便重连。

* `agent.*` 事件（例如消息和工具调用）记录任务执行进度。
* `span.outcome_evaluation_*` 事件仅针对面向结果的会话发出，显示迭代循环的次数和评分器的反馈过程。
* 也可以向面向结果的会话发送 `user.message` [事件](../../api/api-conventions.md)，在执行过程中补充要求。即使不发送额外消息，智能体也会持续迭代，直到满足标准或达到最大迭代次数。
* `user.interrupt` 事件会暂停当前结果的工作，并将 `span.outcome_evaluation_end.result` 标记为 `interrupted`，允许启动一个新的结果。
* 最终结果评估完成后，会话既可以作为对话式会话继续，也可以启动一个新的结果。会话会保留先前结果的历史记录。

### 结果定义事件

> 说明
>
> 一个会话同时只能执行一个结果任务。执行多个结果任务时，请在前一个任务的终止 `span.outcome_evaluation_end` 事件之后发送新的 `user.define_outcome` 事件。

发送结果定义事件可启动结果任务。AgentAPI 接收后会返回该事件，并包含 `processed_at` 时间戳和 `outcome_id`。

```json
{
  "type": "user.define_outcome",
  "description": "生成一份名为 report.md 的 Markdown 数据质量检查方案，说明如何检测和处理缺失值、重复记录及字段类型问题，并列出示例和适用限制。",
  "rubric": { "type": "file", "file_id": "file_01..." },
  "max_iterations": 5
}
```

### 结果评估开始

当评分器开始评估某一轮迭代时，会发出该事件。`iteration` 字段是从 0 开始计数的修订计数器：`0` 表示第一次评估，`1` 表示第一次修订后的重新评估，依此类推。

```json
{
  "type": "span.outcome_evaluation_start",
  "id": "evt_01def...",
  "outcome_id": "outc_01a...",
  "iteration": 0,
  "processed_at": "2026-03-25T14:01:45Z"
}
```

### 结果评估进行中

该事件表示评分器正在工作，不包含评分器的内部推理。应用可以据此更新评估进度，具体反馈由评估结束事件返回。

```json
{
  "type": "span.outcome_evaluation_ongoing",
  "id": "evt_01ghi...",
  "outcome_id": "outc_01a...",
  "iteration": 0,
  "processed_at": "2026-03-25T14:02:10Z"
}
```

### 结果评估结束

评分器完成某一轮评估后发出，包括评估过程中发生失败或中断。若执行在评估开始前失败或中断，不会产生这一轮的评估结束事件；此时应结合会话状态、停止原因和错误事件判断。

`result` 字段表示评估结果，并决定后续执行行为。

| Result                   | 下一步                                                        |
| ------------------------ | ---------------------------------------------------------- |
| `satisfied`              | 会话转换为 `idle`。                                              |
| `needs_revision`         | 智能体开始一个新的迭代周期。                                             |
| `max_iterations_reached` | 已用完允许的轮数，仍未达标；结束评估并回到 `idle`。请检查反馈和当前产物，再决定是否调整目标或评分标准。    |
| `failed`                 | 评估未能完成，例如评分标准无法读取、评分器出错或返回了失败结果。请结合 `explanation` 和错误事件排查。 |
| `interrupted`            | 评估过程中被中断。评估开始前发生的中断应通过会话状态和停止原因判断。                         |

```json
{
  "type": "span.outcome_evaluation_end",
  "id": "evt_01jkl...",
  "outcome_evaluation_start_id": "evt_01def...",
  "outcome_id": "outc_01a...",
  "result": "satisfied",
  "explanation": "方案覆盖三类数据问题，包含检测方法、处理建议、示例和适用限制，report.md 已生成。",
  "iteration": 0,
  "usage": {
    "input_tokens": 2400,
    "output_tokens": 350,
    "cache_creation_input_tokens": 0,
    "cache_read_input_tokens": 1800
  },
  "processed_at": "2026-03-25T14:03:00Z"
}
```

## 检查结果状态

可以在[事件流](events.md)中监听 `span.outcome_evaluation_end`，或者轮询 `GET /v1/sessions/{session_id}` 并读取 `outcome_evaluations[].result`。结果执行期间为 `running`；结束后可能为 `satisfied`、`max_iterations_reached`、`failed` 或 `interrupted`：

`needs_revision` 只表示本轮评估要求修改，任务会继续执行。会话进入 `idle` 也不代表结果已经达标；请确认目标 `outcome_id` 的最终 `result`。达到上限或失败后，保留反馈与产物用于排查，需要再次评估时重新发送结果定义。

cURL

```bash
session=$(curl --fail-with-body --silent --show-error --request GET "$AGENTAPI_BASE_URL/v1/sessions/$session_id" \
  --header "x-api-key: $AGENTAPI_API_KEY")

jq -r '.outcome_evaluations[] | "\(.outcome_id): \(.result)"' <<<"$session"
# outc_01a...: satisfied
```

Python

```python
session = client.beta.sessions.retrieve(session.id)

for outcome in session.outcome_evaluations:
    print(f"{outcome.outcome_id}: {outcome.result}")
    # outc_01a...: satisfied
```

TypeScript

````typescript
const retrieved = await client.beta.sessions.retrieve(session.id);

for (const outcome of retrieved.outcome_evaluations) {
  console.log(`${outcome.outcome_id}: ${outcome.result}`);
  // outc_01a...: satisfied
}
```csharp
```csharp
session = await client.Beta.Sessions.Retrieve(session.ID);

foreach (var outcome in session.OutcomeEvaluations)
{
    Console.WriteLine($"{outcome.OutcomeID}: {outcome.Result}");
    // outc_01a...: satisfied
}
````

Go

```go
session, err = client.Beta.Sessions.Get(ctx, session.ID, anthropic.BetaSessionGetParams{})
if err != nil {
	panic(err)
}

for _, outcome := range session.OutcomeEvaluations {
	fmt.Printf("%s: %s\n", outcome.OutcomeID, outcome.Result)
	// outc_01a...: satisfied
}
```

Java

```java
var retrieved = client.beta().sessions().retrieve(session.id());

for (var outcome : retrieved.outcomeEvaluations()) {
    IO.println(outcome.outcomeId() + ": " + outcome.result());
    // outc_01a...: satisfied
}
```

PHP

```php
$session = $client->beta->sessions->retrieve($session->id);

foreach ($session->outcomeEvaluations as $outcome) {
    echo "{$outcome->outcomeID}: {$outcome->result}\n";
    // outc_01a...: satisfied
}
```

Ruby

```ruby
session = client.beta.sessions.retrieve(session.id)

session.outcome_evaluations.each do |outcome|
  puts "#{outcome.outcome_id}: #{outcome.result}"
  # outc_01a...: satisfied
end
```

## 检索交付物

> 生成可下载的交付物
>
> 云端环境默认引导智能体将最终文件写入 `/mnt/session/outputs/`，普通会话与 Outcome 均适用，无需在提示词中重复说明。自托管 worker 不注入此指引。

智能体会把输出文件写入沙箱内的 `/mnt/session/outputs/`。按以下两步取回这些文件：

1. 通过 [Files API](../data-and-integrations/files.md) 列出文件，以会话 ID 作为 `scope_id`。
2. 按文件 ID 逐个下载。

AgentAPI 在执行结束后收集文件，可能比会话进入空闲状态晚几秒。若预期文件尚未列出，请稍后重查；列出后即可下载。

cURL

```bash
# 列出此会话生成的文件
files=$(curl --fail-with-body --silent --show-error --request GET "$AGENTAPI_BASE_URL/v1/files?scope_id=$session_id" \
  --header "x-api-key: $AGENTAPI_API_KEY")
jq -r '.data[] | "\(.id) \(.filename)"' <<<"$files"

# 下载文件
file_id=$(jq -r '.data[0].id // empty' <<<"$files")
if [[ -n $file_id ]]; then
  curl --fail-with-body --silent --show-error --request GET "$AGENTAPI_BASE_URL/v1/files/$file_id/content" \
    --header "x-api-key: $AGENTAPI_API_KEY" \
    --output /tmp/report.md
fi
```

Python

```python
# 列出此会话生成的文件
files = client.beta.files.list(scope_id=session.id)
for file in files:
    print(file.id, file.filename)

# 下载文件
if files.data:
    content = client.beta.files.download(files.data[0].id)
    content.write_to_file("/tmp/report.md")
```

TypeScript

````typescript
// 列出此会话生成的文件
const files = await client.beta.files.list({
  scope_id: session.id,
});
for (const file of files.data) {
  console.log(file.id, file.filename);
}

// 下载文件
if (files.data.length > 0) {
  const content = await client.beta.files.download(files.data[0].id);
  await writeFile("/tmp/report.md", new Uint8Array(await content.arrayBuffer()));
}
```csharp
```csharp
// 列出此会话生成的文件
var files = await client.Beta.Files.List(new()
{
    ScopeID = session.ID,
});
foreach (var file in files.Items)
{
    Console.WriteLine($"{file.ID} {file.Filename}");
}

// 下载文件
if (files.Items.Count > 0)
{
    using var download = await client.Beta.Files.Download(files.Items[0].ID);
    await using var output = File.Create("/tmp/report.md");
    await (await download.ReadAsStream()).CopyToAsync(output);
}
````

Go

```go
// 列出此会话生成的文件
files, err := client.Beta.Files.List(ctx, anthropic.BetaFileListParams{
	ScopeID: anthropic.String(session.ID),
})
if err != nil {
	panic(err)
}
for _, file := range files.Data {
	fmt.Println(file.ID, file.Filename)
}

// 下载文件
if len(files.Data) > 0 {
	resp, err := client.Beta.Files.Download(ctx, files.Data[0].ID)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	out, err := os.Create("/tmp/report.md")
	if err != nil {
		panic(err)
	}
	defer out.Close()
	if _, err := io.Copy(out, resp.Body); err != nil {
		panic(err)
	}
}
```

Java

```java
// 列出此会话生成的文件
var files = client.beta().files().list(
    FileListParams.builder()
        .scopeId(session.id())
        .build());
for (var file : files.data()) {
    IO.println(file.id() + " " + file.filename());
}

// 下载文件
if (!files.data().isEmpty()) {
    try (HttpResponse response = client.beta().files().download(files.data().getFirst().id())) {
        try (InputStream body = response.body()) {
            Files.copy(body, Path.of("/tmp/report.md"), StandardCopyOption.REPLACE_EXISTING);
        }
    }
}
```

PHP

```php
// 列出此会话生成的文件
$files = $client->beta->files->list(scopeID: $session->id);
foreach ($files->getItems() as $file) {
    echo "{$file->id} {$file->filename}\n";
}

// 下载文件
if (count($files->getItems()) > 0) {
    $content = $client->beta->files->download($files->getItems()[0]->id);
    file_put_contents('/tmp/report.md', $content);
}
```

Ruby

```ruby
# 列出此会话生成的文件
files = client.beta.files.list(scope_id: session.id)
files.data.each { |file| puts "#{file.id} #{file.filename}" }

# 下载文件
if (first = files.data.first)
  content = client.beta.files.download(first.id)
  File.binwrite("/tmp/report.md", content.read)
end
```

## 后续步骤

* [凭证管理](../data-and-integrations/vaults.md)：在创建会话时注册每个用户的凭据。

* [事件和事件流](events.md)：发送事件、流式传输响应，并在执行过程中中断执行或补充任务要求。

* [附加和下载文件](../data-and-integrations/files.md)：上传文件并挂载到会话沙箱中，供智能体读取和处理。
