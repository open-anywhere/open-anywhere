<!-- 来源：https://developer-doc.qiniu.com/products/agentapi/development_guidelines/agentapi-sessions-and-tasks/13600-agentapi-sessions-and-tasks-scheduled-deployments | 由 sync-docs.py 自动生成，请勿手改 -->

# 定时部署



最近更新时间: 2026-10-13 15:31:41

定时部署（scheduled deployment）让智能体按固定时间自动创建会话并执行任务。可以使用 Deployments API 创建、运行、暂停、恢复和归档部署。

部署保存任务模板，每次触发生成部署运行记录并创建新会话。需要跨次保留信息时，请在模板中附加[记忆存储](../data-and-integrations/memory.md)。

![定时部署的会话触发流程](https://developer-doc.qiniu.com/docs-assets/products/agentapi/development_guidelines/agentapi-sessions-and-tasks/13600-agentapi-sessions-and-tasks-scheduled-deployments/FjsLuFtxiSwvs5cnqd2zmYBS9EQx-f262c1993561.webp)

## 接入定时任务

1. 完成[接入准备](../../quick-start/access.md)，创建[智能体](../agent-configuration/agent.md)和[云端环境](../execution-environments/environment.md)。智能体显式配置 `runtime`，部署创建的会话继承该运行时。
2. 准备每次执行需要的输入、文件、仓库和 Vault；这些配置保存在部署模板中。
3. 创建部署，核对 cron、时区及 `upcoming_runs_at`。
4. 手动运行一次，使用返回的 `session_id` 查看事件并核对产物。
5. 定时运行后，同时监控部署运行错误和会话执行结果；修复自动暂停的原因后再恢复计划。

以下示例沿用[SDK 客户端初始化](../../quick-start/quickstart.md)中的 `client`，并使用已经创建的智能体和云端环境。智能体所用模型应从[七牛 MaaS 模型管理页](https://portal.qiniu.com/ai-inference/model)选择。首次接入时可先省略 `schedule`，完成手动验证后再更新计划。

## 创建定时部署

创建部署时，需要指定智能体、云端环境、初始事件和计划。部署保存这些配置，每次触发都会创建一个新的会话。

Portal 任务详情会展示当前配置关联的凭证库。您也可以在运行历史中查看该次会话关联的凭证库；这里使用历史会话的 `vault_ids`，后续修改部署不会改变这些关联 ID。尚未创建会话的运行会显示“未创建 Session”；已删除或暂时无法访问的会话会显示加载错误，您可以重试。

部署会固定智能体版本：`agent` 可传 ID 字符串（保存时解析版本），或带 `version` 的引用。更新智能体后，需另行更新部署的 `agent` 引用。请将 cURL 示例中的版本 `1` 替换为要发布的版本。

任务依赖输入文件、仓库或外部服务时，在模板中设置 `resources` 和 `vault_ids`。这些资源会在每次创建会话时重新校验；令牌保存在 Vault 中，仓库挂载凭证按[访问 GitHub](../data-and-integrations/github.md)配置。

* `initial_events` 至少包含一个 `user.message` 或 `user.define_outcome` 事件，用于启动会话。
* `schedule` 使用分钟级 cron 表达式和 IANA 时区；省略 `schedule` 时，只能手动运行部署。

以下示例创建每周项目检查任务。各语言 SDK 用 `content` 构造兼容类型，再通过原始请求体选项将 `initial_events` 替换为 AgentAPI 的规范字段 `input.parts`。

cURL

```bash
DEPLOYMENT_ID=$(
  curl --fail-with-body --silent --show-error --request POST "$AGENTAPI_BASE_URL/v1/deployments" \
    --header "x-api-key: $AGENTAPI_API_KEY" \
    --header "content-type: application/json" \
    --data @- <<EOF | jq -er '.id'
{
  "name": "每周项目检查",
  "agent": {"type": "agent", "id": "$AGENT_ID", "version": 1},
  "environment_id": "$ENVIRONMENT_ID",
  "initial_events": [
    {"type": "user.message", "input": {"parts": [{"type": "text", "text": "检查本周项目变更，汇总风险与待办事项，并生成 Markdown 报告。"}]}}
  ],
  "schedule": {
    "type": "cron",
    "expression": "0 20 * * 5",
    "timezone": "America/New_York"
  }
}
EOF
)
```

Python

```python
deployment = client.beta.deployments.create(
    name="每周项目检查",
    agent=agent.id,
    environment_id=environment.id,
    initial_events=[
        {
            "type": "user.message",
            "content": [{"type": "text", "text": "检查本周项目变更，汇总风险与待办事项，并生成 Markdown 报告。"}],
        },
    ],
    schedule={
        "type": "cron",
        "expression": "0 20 * * 5",
        "timezone": "America/New_York",
    },
    extra_body={
        "initial_events": [{
            "type": "user.message",
            "input": {"parts": [{"type": "text", "text": "检查本周项目变更，汇总风险与待办事项，并生成 Markdown 报告。"}]},
        }],
    },
)
```

TypeScript

````typescript
const params = {
  name: "每周项目检查",
  agent: agent.id,
  environment_id: environment.id,
  initial_events: [
    {
      type: "user.message",
      content: [{ type: "text", text: "检查本周项目变更，汇总风险与待办事项，并生成 Markdown 报告。" }],
    },
  ],
  schedule: {
    type: "cron",
    expression: "0 20 * * 5",
    timezone: "America/New_York",
  },
} satisfies Parameters<typeof client.beta.deployments.create>[0];
const deployment = await client.beta.deployments.create(params, {
  body: {
    ...params,
    initial_events: [{
      type: "user.message",
      input: { parts: [{ type: "text", text: "检查本周项目变更，汇总风险与待办事项，并生成 Markdown 报告。" }] },
    }],
  },
});
```csharp
```csharp
using System.Linq;
using System.Text.Json;
using Anthropic.Models.Beta.Deployments;
using Anthropic.Models.Beta.Sessions.Events;

var parameters = new DeploymentCreateParams
{
    Name = "每周项目检查",
    Agent = agent.ID,
    EnvironmentID = environment.ID,
    InitialEvents =
    [
        new BetaManagedAgentsUserMessageEventParams
        {
            Type = BetaManagedAgentsUserMessageEventParamsType.UserMessage,
            Content =
            [
                new BetaManagedAgentsTextBlock
                {
                    Type = BetaManagedAgentsTextBlockType.Text,
                    Text = "检查本周项目变更，汇总风险与待办事项，并生成 Markdown 报告。",
                },
            ],
        },
    ],
    Schedule = new BetaManagedAgentsScheduleParams
    {
        Type = BetaManagedAgentsScheduleParamsType.Cron,
        Expression = "0 20 * * 5",
        Timezone = "America/New_York",
    },
};
var body = parameters.RawBodyData.ToDictionary(entry => entry.Key, entry => entry.Value);
body["initial_events"] = JsonSerializer.SerializeToElement(new[] {
    new {
        type = "user.message",
        input = new { parts = new[] { new { type = "text", text = "检查本周项目变更，汇总风险与待办事项，并生成 Markdown 报告。" } } },
    },
});
parameters = DeploymentCreateParams.FromRawUnchecked(parameters.RawHeaderData, parameters.RawQueryData, body);
var deployment = await client.Beta.Deployments.Create(parameters);
````

Go

```go
import "github.com/anthropics/anthropic-sdk-go/option"

deployment, err := client.Beta.Deployments.New(ctx, anthropic.BetaDeploymentNewParams{
	Name:          "每周项目检查",
	Agent:         anthropic.BetaDeploymentNewParamsAgentUnion{OfString: anthropic.String(agent.ID)},
	EnvironmentID: environment.ID,
	InitialEvents: []anthropic.BetaManagedAgentsDeploymentInitialEventParamsUnion{{
		OfUserMessage: &anthropic.BetaManagedAgentsUserMessageEventParams{
			Type: anthropic.BetaManagedAgentsUserMessageEventParamsTypeUserMessage,
			Content: []anthropic.BetaManagedAgentsUserMessageEventParamsContentUnion{{
				OfText: &anthropic.BetaManagedAgentsTextBlockParam{
					Type: anthropic.BetaManagedAgentsTextBlockTypeText,
					Text: "检查本周项目变更，汇总风险与待办事项，并生成 Markdown 报告。",
				},
			}},
		},
	}},
	Schedule: anthropic.BetaManagedAgentsScheduleParams{
		Type:       anthropic.BetaManagedAgentsScheduleParamsTypeCron,
		Expression: "0 20 * * 5",
		Timezone:   "America/New_York",
	},
}, option.WithJSONSet("initial_events", []map[string]any{{
    "type": "user.message",
    "input": map[string]any{
        "parts": []map[string]string{{"type": "text", "text": "检查本周项目变更，汇总风险与待办事项，并生成 Markdown 报告。"}},
    },
}}))
if err != nil {
	panic(err)
}
```

Java

```java
import com.anthropic.core.JsonValue;
import java.util.List;
import java.util.Map;

var deployment = client.beta().deployments().create(
    DeploymentCreateParams.builder()
        .name("每周项目检查")
        .agent(agent.id())
        .environmentId(environment.id())
        .addInitialEvent(
            BetaManagedAgentsUserMessageEventParams.builder()
                .type(BetaManagedAgentsUserMessageEventParams.Type.USER_MESSAGE)
                .addTextContent("检查本周项目变更，汇总风险与待办事项，并生成 Markdown 报告。")
                .build()
        )
        .schedule(
            BetaManagedAgentsScheduleParams.builder()
                .type(BetaManagedAgentsScheduleParams.Type.CRON)
                .expression("0 20 * * 5")
                .timezone("America/New_York")
                .build()
        )
        .putAdditionalBodyProperty("initial_events", JsonValue.from(List.of(Map.of(
            "type", "user.message",
            "input", Map.of("parts", List.of(Map.of("type", "text", "text", "检查本周项目变更，汇总风险与待办事项，并生成 Markdown 报告。")))
        ))))
        .build()
);
```

PHP

```php
$deployment = $client->beta->deployments->create(
    name: '每周项目检查',
    agent: $agent->id,
    environmentID: $environment->id,
    initialEvents: [
        [
            'type' => 'user.message',
            'content' => [['type' => 'text', 'text' => '检查本周项目变更，汇总风险与待办事项，并生成 Markdown 报告。']],
        ],
    ],
    schedule: [
        'type' => 'cron',
        'expression' => '0 20 * * 5',
        'timezone' => 'America/New_York',
    ],
    requestOptions: ['extraBodyParams' => [
        'initial_events' => [[
            'type' => 'user.message',
            'input' => ['parts' => [['type' => 'text', 'text' => '检查本周项目变更，汇总风险与待办事项，并生成 Markdown 报告。']]],
        ]],
    ]],
);
```

Ruby

```ruby
deployment = client.beta.deployments.create(
  name: "每周项目检查",
  agent: agent.id,
  environment_id: environment.id,
  initial_events: [
    {
      type: "user.message",
      content: [{type: "text", text: "检查本周项目变更，汇总风险与待办事项，并生成 Markdown 报告。"}]
    }
  ],
  schedule: {
    type: "cron",
    expression: "0 20 * * 5",
    timezone: "America/New_York"
  },
  request_options: {
    extra_body: {
      initial_events: [{
        type: "user.message",
        input: {parts: [{type: "text", text: "检查本周项目变更，汇总风险与待办事项，并生成 Markdown 报告。"}]}
      }]
    }
  }
)
```

响应中的部署对象通过 `schedule.upcoming_runs_at` 字段返回后续触发时间，可用于核对计划设置。

```json
{
  "id": "depl_01xyz",
  "status": "active",
  "paused_reason": null,
  "schedule": {
    "type": "cron",
    "expression": "0 20 * * 5",
    "timezone": "America/New_York",
    "last_run_at": null,
    "upcoming_runs_at": [
      "2026-05-09T00:00:00Z",
      "2026-05-16T00:00:00Z",
      "2026-05-23T00:00:00Z"
    ]
  }
}
```

计划时间不含抖动（jitter）。为分散负载，调度器会在计划时间后增加 5 秒至 9 分钟的延迟，其上限按运行间隔的 15% 计算，再限制在该范围内。实际触发还可能受调度器处理延迟影响。

当前服务对每个账户的未归档定时部署数量限制为 1,000 个。达到上限后，创建定时部署或为现有部署新增计划会返回 `400`。

本页展示定时部署的常用参数和响应；通用认证、分页和错误约定请参阅 [API 通用约定](../../api/api-conventions.md)。

### Cron 和时区语义

* 表达式： 标准 POSIX cron（`minute hour day-of-month month day-of-week`）。
* 时区： IANA 时区标识符（例如 `"America/Los_Angeles"`）。
* 夏令时（DST）： Cron 计划使用字面挂钟时间匹配，因此 `America/New_York` 时区中的 `"0 20 * * *"` 会在当地时间晚上 8:00 触发，无论当前生效的是 EST 还是 EDT。

> 说明
>
> 在夏令时开始（时钟拨快）当天不存在的挂钟时间（例如凌晨 2 点）不会被触发。在夏令时结束（时钟拨慢）当天出现两次的挂钟时间会触发两次。如果无法接受遗漏或重复执行，请将计划安排在当地时间凌晨 1–3 点窗口之外，或使用 UTC。

## 触发手动运行

要在计划之外运行部署，请调用部署的 `run` 端点。这会立即创建一个会话，并写入一条 `trigger_context.type: "manual"` 的部署运行记录，适合在启用计划前测试部署。

cURL

```bash
curl --fail-with-body --silent --show-error --request POST "$AGENTAPI_BASE_URL/v1/deployments/$DEPLOYMENT_ID/run" \
  --header "x-api-key: $AGENTAPI_API_KEY"
```

Python

```python
run = client.beta.deployments.run(deployment.id)
```

TypeScript

````typescript
const run = await client.beta.deployments.run(deployment.id);
```csharp
```csharp
var manualRun = await client.Beta.Deployments.Run(deployment.ID);
````

Go

```go
manualRun, err := client.Beta.Deployments.Run(ctx, deployment.ID, anthropic.BetaDeploymentRunParams{})
if err != nil {
	panic(err)
}
```

Java

```java
var run = client.beta().deployments().run(deployment.id());
```

PHP

```php
$run = $client->beta->deployments->run($deployment->id);
```

Ruby

```ruby
run = client.beta.deployments.run(deployment.id)
```

## 部署运行

> 部署运行与任务结果
>
> 部署运行（deployment run）记录会话创建和初始事件提交的结果，包括环境已归档、资源不可用或初始事件被拒绝等错误。运行成功不代表任务已经完成；任务是否成功，需用返回的 `session_id` 查询会话和事件。

> 防止重复执行
>
> 调度采用“至少一次”语义，不能保证 exactly-once；超时、重启或恢复后可能重复触发。写入、发消息、创建工单等操作应在业务侧去重：可组合 `deployment_id` 与 `trigger_context.scheduled_at` 作为幂等键，在执行前检查是否已处理。

按如下方式列出某个部署的所有部署运行：

cURL

```bash
curl --fail-with-body --silent --show-error --get "$AGENTAPI_BASE_URL/v1/deployment_runs" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --data-urlencode "deployment_id=$DEPLOYMENT_ID"
```

Python

```python
for run in client.beta.deployment_runs.list(
    deployment_id=deployment.id,
):
    print(run.created_at, run.session_id or run.error.type)
```

TypeScript

````typescript
for await (const run of client.beta.deploymentRuns.list({
  deployment_id: deployment.id,
})) {
  console.log(run.created_at, run.session_id ?? run.error?.type);
}
```csharp
```csharp
var runs = await client.Beta.DeploymentRuns.List(
    new() { DeploymentID = deployment.ID }
);
await foreach (var run in runs.Paginate())
{
    // Error 联合类型直接暴露 .Message；在添加通用 .Type 访问器之前，
    // 判别器需从 .Json 中读取。
    var outcome = run.SessionID ?? run.Error!.Json.GetProperty("type").GetString();
    Console.WriteLine($"{run.CreatedAt} {outcome}");
}
````

Go

```go
runs := client.Beta.DeploymentRuns.ListAutoPaging(ctx, anthropic.BetaDeploymentRunListParams{
	DeploymentID: anthropic.String(deployment.ID),
})
for runs.Next() {
	run := runs.Current()
	if run.SessionID != "" {
		fmt.Println(run.CreatedAt.Format(time.RFC3339), run.SessionID)
	} else {
		fmt.Println(run.CreatedAt.Format(time.RFC3339), run.Error.Type)
	}
}
if err := runs.Err(); err != nil {
	panic(err)
}
```

Java

```java
for (var run : client.beta().deploymentRuns().list(
        DeploymentRunListParams.builder()
            .deploymentId(deployment.id())
            .build()).autoPager()) {
    // Error 联合类型尚未暴露通用的 .type()/.message()
    // 访问器；.toString() 包含两者。
    IO.println(run.createdAt() + " "
        + run.sessionId().orElseGet(() -> run.error().orElseThrow().toString()));
}
```

PHP

```php
foreach ($client->beta->deploymentRuns->list(
    deploymentID: $deployment->id,
)->pagingEachItem() as $run) {
    $outcome = $run->sessionID ?? $run->error->type;
    echo "{$run->createdAt->format(DATE_ATOM)} {$outcome}\n";
}
```

Ruby

```ruby
client.beta.deployment_runs.list(
  deployment_id: deployment.id
).auto_paging_each do
  puts "#{it.created_at} #{it.session_id || it.error.type}"
end
```

还可以筛选出带有错误的部署运行：

cURL

```bash
curl --fail-with-body --silent --show-error --get "$AGENTAPI_BASE_URL/v1/deployment_runs" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --data-urlencode "deployment_id=$DEPLOYMENT_ID" \
  --data-urlencode "has_error=true"
```

Python

```python
for run in client.beta.deployment_runs.list(
    deployment_id=deployment.id,
    has_error=True,
):
    print(run.created_at, run.error.type, run.error.message)
```

TypeScript

````typescript
for await (const run of client.beta.deploymentRuns.list({
  deployment_id: deployment.id,
  has_error: true,
})) {
  console.log(run.created_at, run.error?.type, run.error?.message);
}
```csharp
```csharp
var failedRuns = await client.Beta.DeploymentRuns.List(
    new() { DeploymentID = deployment.ID, HasError = true }
);
await foreach (var failedRun in failedRuns.Paginate())
{
    var error = failedRun.Error!;
    var errorType = error.Json.GetProperty("type").GetString();
    Console.WriteLine($"{failedRun.CreatedAt} {errorType} {error.Message}");
}
````

Go

```go
failedRuns := client.Beta.DeploymentRuns.ListAutoPaging(ctx, anthropic.BetaDeploymentRunListParams{
	DeploymentID: anthropic.String(deployment.ID),
	HasError:     anthropic.Bool(true),
})
for failedRuns.Next() {
	failedRun := failedRuns.Current()
	fmt.Println(failedRun.CreatedAt.Format(time.RFC3339), failedRun.Error.Type, failedRun.Error.Message)
}
if err := failedRuns.Err(); err != nil {
	panic(err)
}
```

Java

```java
for (var run : client.beta().deploymentRuns().list(
        DeploymentRunListParams.builder()
            .deploymentId(deployment.id())
            .hasError(true)
            .build()).autoPager()) {
    IO.println(run.createdAt() + " " + run.error().orElseThrow());
}
```

PHP

```php
foreach ($client->beta->deploymentRuns->list(
    deploymentID: $deployment->id,
    hasError: true,
)->pagingEachItem() as $run) {
    echo "{$run->createdAt->format(DATE_ATOM)} {$run->error->type} {$run->error->message}\n";
}
```

Ruby

```ruby
client.beta.deployment_runs.list(
  deployment_id: deployment.id,
  has_error: true
).auto_paging_each do
  puts "#{it.created_at} #{it.error.type} #{it.error.message}"
end
```

失败的运行包含一个 `error`，其 `type` 描述了会话创建被拒绝的原因（例如 `environment_archived_error`、`agent_archived_error` 或 `session_rate_limited_error`）。

```json
{
  "type": "deployment_run",
  "id": "drun_01abc124",
  "deployment_id": "depl_01xyz",
  "trigger_context": { "type": "schedule", "scheduled_at": "2026-05-09T00:00:00Z" },
  "session_id": null,
  "error": {
    "type": "environment_archived_error",
    "message": "environment `env_01abc` is archived"
  },
  "agent": { "type": "agent", "id": "agt_01ghi789", "version": 3 },
  "created_at": "2026-05-09T00:00:01Z"
}
```

## 管理部署生命周期

* 列出部署：`GET /v1/deployments`。
* 查看部署：`GET /v1/deployments/{deployment_id}`。
* 更新部署：`POST /v1/deployments/{deployment_id}`。请求中的 `resources`、`vault_ids`、`initial_events` 整体替换原值；`"schedule": null` 清除计划。

暂停（Pause）会停止后续定时触发；已创建且正在运行的会话会继续执行。暂停期间仍允许通过 `run` 端点手动运行。暂停会将 `paused_reason` 设置为 `{"type": "manual"}`；取消暂停会将其清除。

cURL

```bash
curl --fail-with-body --silent --show-error --request POST "$AGENTAPI_BASE_URL/v1/deployments/$DEPLOYMENT_ID/pause" \
  --header "x-api-key: $AGENTAPI_API_KEY"
```

Python

```python
client.beta.deployments.pause(deployment.id)
```

TypeScript

````typescript
await client.beta.deployments.pause(deployment.id);
```csharp
```csharp
await client.Beta.Deployments.Pause(deployment.ID);
````

Go

```go
if _, err := client.Beta.Deployments.Pause(ctx, deployment.ID, anthropic.BetaDeploymentPauseParams{}); err != nil {
	panic(err)
}
```

Java

```java
client.beta().deployments().pause(deployment.id());
```

PHP

```php
$client->beta->deployments->pause($deployment->id);
```

Ruby

```ruby
client.beta.deployments.pause(deployment.id)
```

取消暂停（Unpause）会从下一个计划时间点恢复计划。错过的触发不会被补执行。

cURL

```bash
curl --fail-with-body --silent --show-error --request POST "$AGENTAPI_BASE_URL/v1/deployments/$DEPLOYMENT_ID/unpause" \
  --header "x-api-key: $AGENTAPI_API_KEY"
```

Python

```python
client.beta.deployments.unpause(deployment.id)
```

TypeScript

````typescript
await client.beta.deployments.unpause(deployment.id);
```csharp
```csharp
await client.Beta.Deployments.Unpause(deployment.ID);
````

Go

```go
if _, err := client.Beta.Deployments.Unpause(ctx, deployment.ID, anthropic.BetaDeploymentUnpauseParams{}); err != nil {
	panic(err)
}
```

Java

```java
client.beta().deployments().unpause(deployment.id());
```

PHP

```php
$client->beta->deployments->unpause($deployment->id);
```

Ruby

```ruby
client.beta.deployments.unpause(deployment.id)
```

归档（Archive）会永久终止计划，归档后的部署无法修改或恢复。

归档后也不能手动运行该部署。部署生命周期操作不会自动归档或删除已经创建的会话；需要停止某次任务或清理产物时，请使用[会话生命周期操作](session-lifecycle.md)。

cURL

```bash
curl --fail-with-body --silent --show-error --request POST "$AGENTAPI_BASE_URL/v1/deployments/$DEPLOYMENT_ID/archive" \
  --header "x-api-key: $AGENTAPI_API_KEY"
```

Python

```python
client.beta.deployments.archive(deployment.id)
```

TypeScript

````typescript
await client.beta.deployments.archive(deployment.id);
```csharp
```csharp
await client.Beta.Deployments.Archive(deployment.ID);
````

Go

```go
if _, err := client.Beta.Deployments.Archive(ctx, deployment.ID, anthropic.BetaDeploymentArchiveParams{}); err != nil {
	panic(err)
}
```

Java

```java
client.beta().deployments().archive(deployment.id());
```

PHP

```php
$client->beta->deployments->archive($deployment->id);
```

Ruby

```ruby
client.beta.deployments.archive(deployment.id)
```

### 失败行为

根据失败发生的阶段，按以下方式排查：

| 观察结果                      | 检查与处理                                                                                                                                      |
| ------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------ |
| 部署运行包含 `error`，未返回成功创建的会话 | 查看资源是否可用、是否限流，以及部署的 `paused_reason`。                                                                                                       |
| 部署运行成功，但会话出现错误或等待确认       | 使用 `session_id` 读取事件，处理模型、工具、权限或输入问题。                                                                                                      |
| 有会话，但没有预期文件               | 核对最终回答、输出路径和[会话文件列表](../data-and-integrations/files.md)。 |
| 部署自动暂停                    | 修复 `paused_reason.error.type` 对应的问题，更新模板，手动验证后再恢复。                                                                                         |

会话创建或初始事件提交失败时，查看运行记录的 `error.type` 和 `error.message`。计划是否继续执行取决于部署是否自动暂停；会话已启动后的模型调用错误则通过会话事件排查。

资源失效时，部署按以下规则处理：

| 原因                       | 部署行为                | 运行记录                                      |
| ------------------------ | ------------------- | ----------------------------------------- |
| 智能体被归档                   | 同时自动归档部署            | 不生成                                       |
| 智能体被删除                   | 同时自动归档部署            | 不生成                                       |
| 引用的子智能体被归档               | 下次触发时自动暂停，更新智能体后可恢复 | 记录失败，`error.type: "agent_archived_error"` |
| 其他不可恢复的会话创建错误，如环境或保管库已归档 | 自动暂停，修复后可恢复         | 记录失败                                      |

自动暂停时，`paused_reason.error.type` 与失败运行的 `error.type` 一致。

## 接收部署通知

在 Portal 的“服务设置 → Webhook”中订阅 `deployment_run.started`、`deployment_run.succeeded` 或 `deployment_run.failed`，接收计划部署运行通知。这些事件仅由计划触发，手动运行不产生 `deployment_run.*` Webhook；部署暂停、恢复和更新可订阅对应的 `deployment.*` 事件。

运行成功表示会话创建和初始化成功，不代表智能体任务完成。收到运行通知后，用 `data.id` 查询运行记录取得 `session_id`，再读取会话事件与产物；也可订阅会话状态通知。接收服务必须处理重复投递和乱序，端点停用期间的事件不会补发，详见 [Webhook 通知](../data-and-integrations/webhooks.md)。
