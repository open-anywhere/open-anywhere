<!-- 来源：https://developer-doc.qiniu.com/products/agentapi/development_guidelines/agentapi-sessions-and-tasks/13598-agentapi-sessions-and-tasks-session-lifecycle | 由 sync-docs.py 自动生成，请勿手改 -->

# 会话操作



最近更新时间: 2026-10-20 17:42:02

一次云端任务结束后，会话仍可保留历史并继续接收输入。将 AgentAPI 接入业务系统时，需要根据会话状态决定何时继续任务、修改配置、停止执行，以及保留或清理产物。

本页从已创建的会话开始，说明读取、更新、归档和删除操作。创建会话与发送首条消息见[会话](session.md)，SDK 客户端初始化见[通过 API 构建](../../quick-start/quickstart.md)。

## 选择生命周期操作

| 业务目标           | 操作                         | 对已有记录的影响                                |
| -------------- | -------------------------- | --------------------------------------- |
| 在同一任务中补充要求     | 发送 `user.message`          | 保留会话历史，继续使用现有会话。                        |
| 停止当前执行，之后继续    | 发送 `user.interrupt`，观察返回事件 | 保留历史；已经发生的工具副作用不会回滚。                    |
| 修改标题、业务标识或工具配置 | 更新会话                       | 工具与 MCP 配置要求会话处于 `idle`；不会更新底层智能体。      |
| 停用会话并保留历史      | 归档会话                       | 停止执行并拒绝新事件；保留历史与文件对象。                   |
| 让用户侧不可见并停止继续使用 | 删除会话                       | 软删除会话；公开 API 不再返回它，事件、Turn、资源和文件在服务侧保留。 |

## 会话状态

会话状态描述执行进度，不等同于业务任务是否成功。请结合下表、状态事件的停止原因和任务产物判断执行结果。

| 状态             | 描述                                                                                        |
| -------------- | ----------------------------------------------------------------------------------------- |
| `idle`         | 智能体正在等待输入，包括用户消息或工具确认。通过 `POST /v1/sessions` 创建的会话以 `idle` 状态开始，需再发送 `user.message` 才会执行。 |
| `running`      | 智能体正在主动执行。                                                                                |
| `rescheduling` | 发生了瞬时错误，正在自动重试。                                                                           |
| `terminated`   | 会话已结束，原因可能是发生了不可恢复的错误，或者会话已被归档。完成工作的会话会进入 `idle` 状态，而不是 `terminated`。                     |

![会话状态流转](https://developer-doc.qiniu.com/docs-assets/products/agentapi/development_guidelines/agentapi-sessions-and-tasks/13598-agentapi-sessions-and-tasks-session-lifecycle/FvjBfGeLZrEi8enFCVEtuaN3SfYW-b0dddb5590fe.webp)

接收到 `session.status_idle` 时，还需按[停止原因](events.md)判断本轮结果；断线后按[事件恢复流程](events.md)继续观察同一会话。

## 云端环境的运行与暂停

会话进入 `idle` 后，AgentAPI 会记录一次 `pause_pending`，生命周期管理器每 90–150 秒处理这些待暂停 context。暂停保留沙箱中的文件和运行时上下文。收到下一轮输入后，AgentAPI 清除待暂停或已暂停状态，按需恢复同一沙箱，并重新设置完整的运行时长。

暂停成功、或 provider 返回 `409`/`404` 表示沙箱已经暂停时，状态会持久化为 `paused`。这个状态在实例之间共享，服务重启后仍然有效；只有暂停失败才会保留待重试状态。

沙箱每次创建或恢复时设置运行时长，执行中不再定时续期。服务端的沙箱运行时长和 CLI 命令执行上限默认均为 1 小时。需要 12 小时等更长时限时，应在部署配置中显式设置，且沙箱服务必须支持相应上限。这两项限制互相独立：命令达到执行上限时，即使沙箱仍可用，AgentAPI 也会停止命令；沙箱寿命先开始计时，两项配置相同时，沙箱可能先到期。需要为命令退出和产物收集留出时间的部署，应将命令上限设得短于沙箱运行时长。

`native` 的模型请求和工具调用还受各自的执行预算限制。会话历史的保留不保证原沙箱能够恢复。对于 `claudecode` 和 `codex`，如果原沙箱已经丢失，会话会终止；请创建新会话并提供继续任务所需材料，AgentAPI 不会仅凭事件历史重建替代沙箱。延长沙箱运行时长不会改变这一限制。

## 检索会话

使用会话 ID 读取当前配置与状态。需要判断一次具体执行为何结束时，应同时读取事件中的停止原因和错误；会话查询返回的是当前快照。

cURL

```bash
retrieved=$(curl --request GET --fail-with-body --silent --show-error "$AGENTAPI_BASE_URL/v1/sessions/$SESSION_ID" \
  --header "x-api-key: $AGENTAPI_API_KEY")
echo "Status: $(jq -r '.status' <<< "$retrieved")"
```

Python

```python
retrieved = client.beta.sessions.retrieve(session.id)
print(f"Status: {retrieved.status}")
```

TypeScript

````typescript
const retrieved = await client.beta.sessions.retrieve(session.id);
console.log(`Status: ${retrieved.status}`);
```csharp
```csharp
var retrieved = await client.Beta.Sessions.Retrieve(session.ID);
Console.WriteLine($"Status: {retrieved.Status.Raw()}");
````

Go

```go
retrieved, err := client.Beta.Sessions.Get(ctx, session.ID, anthropic.BetaSessionGetParams{})
if err != nil {
	panic(err)
}
fmt.Printf("Status: %s\n", retrieved.Status)
```

Java

```java
var retrieved = client.beta().sessions().retrieve(session.id());
IO.println("Status: " + retrieved.status());
```

PHP

```php
$retrieved = $client->beta->sessions->retrieve($session->id);
echo "Status: {$retrieved->status}\n";
```

Ruby

```ruby
retrieved = client.beta.sessions.retrieve(session.id)
puts "Status: #{retrieved.status}"
```

## 列出会话

`GET /v1/sessions` 的结果是分页的。使用 `limit` 查询参数控制页面大小。每个响应都包含一个 `next_page` 游标；在下一次请求中将其作为 `page` 参数传递即可获取下一页。当没有更多结果时，`next_page` 为 `null`。

分页时，将响应中的 `next_page` 原样传入下一次请求的 `page`，不要解析或修改游标；`next_page` 为 `null` 时结束。可另行设置 `limit` 和筛选条件；当前 `/v1/sessions` 不支持 `prev_page` 或 `order`。

列表默认排除归档会话。可以按 `agent_id`、`memory_store_id`、`status` 和 `created_at[gte]`/`created_at[lte]` 筛选；需要查询归档历史时使用 `include_archived=true`。保留同一组筛选条件，再用返回的游标获取下一页。

cURL

```bash
first_page=$(curl --request GET --fail-with-body --silent --show-error \
  "$AGENTAPI_BASE_URL/v1/sessions?agent_id=$AGENT_ID&limit=1" \
  --header "x-api-key: $AGENTAPI_API_KEY")
jq '{data: [.data[] | {id, status}], next_page}' <<< "$first_page"

next_cursor=$(jq -r '.next_page' <<< "$first_page")
second_page=$(curl --request GET --fail-with-body --silent --show-error \
  "$AGENTAPI_BASE_URL/v1/sessions?agent_id=$AGENT_ID&limit=1&page=$next_cursor" \
  --header "x-api-key: $AGENTAPI_API_KEY")
jq '{data: [.data[] | {id, status}], next_page}' <<< "$second_page"
```

Python

```python
# 将 `limit` 设得较低，使结果跨越多页。
first_page = client.beta.sessions.list(limit=1, agent_id=agent.id)
# 最后一页的 `next_page` 为 None。
print(f"next_page: {first_page.next_page}")

# 将 `next_page` 作为 `page` 传回以获取下一页。
second_page = client.beta.sessions.list(
    limit=1, agent_id=agent.id, page=first_page.next_page
)
for listed_session in second_page.data:
    print(f"{listed_session.id}: {listed_session.status}")
# 继续读取时，重复将 second_page.next_page 传给 page，直到它变为 None。
```

TypeScript

````typescript
const firstPage = await client.beta.sessions.list({ limit: 1, agent_id: agent.id });
// 当存在更多会话时会设置 next_page。
console.log(`next_page: ${firstPage.next_page}`);

// 将 next_page 作为 `page` 游标传入以获取第二页。
const secondPage = await client.beta.sessions.list({
  limit: 1,
  agent_id: agent.id,
  page: firstPage.next_page
});
for (const listedSession of secondPage.data) {
  console.log(`Page 2 has ${listedSession.id}: ${listedSession.status}`);
}
// 继续读取时，将 secondPage.next_page 传给 page，直到它为 null。
```csharp
```csharp
// `List` 返回当前页条目；需要继续读取时，请使用响应中的 next_page。
using var page1Response = await client.Beta.Sessions.WithRawResponse.List(
    new SessionListParams { Limit = 1, AgentID = agent.ID }
);
var page1 = await page1Response.Deserialize<SessionListPageResponse>();
Console.WriteLine($"next_page: {page1.NextPage ?? "null"}");

// 前进：将第 1 页的 `next_page` 作为 `page` 游标传入。
using var page2Response = await client.Beta.Sessions.WithRawResponse.List(
    new SessionListParams { Limit = 1, AgentID = agent.ID, Page = page1.NextPage }
);
var page2 = await page2Response.Deserialize<SessionListPageResponse>();
foreach (var listedSession in page2.Data ?? [])
{
    Console.WriteLine($"Page 2: {listedSession.ID}: {listedSession.Status.Raw()}");
}

// 继续读取时，将 page2.NextPage 传给 Page，直到它为空。
````

Go

```go
// 第 1 页：响应中的 NextPage 指向下一页。
firstPage, err := client.Beta.Sessions.List(ctx, anthropic.BetaSessionListParams{
	AgentID: anthropic.String(agent.ID),
	Limit:   anthropic.Int(1),
})
if err != nil {
	panic(err)
}
fmt.Printf("Page 1 next_page: %q\n", firstPage.NextPage)

// 前进：将 next_page 作为 Page 游标传入以获取第 2 页。
secondPage, err := client.Beta.Sessions.List(ctx, anthropic.BetaSessionListParams{
	AgentID: anthropic.String(agent.ID),
	Limit:   anthropic.Int(1),
	Page:    anthropic.String(firstPage.NextPage),
})
if err != nil {
	panic(err)
}
for _, listedSession := range secondPage.Data {
	fmt.Printf("Page 2: %s: %s\n", listedSession.ID, listedSession.Status)
}

// 继续读取时，将 secondPage.NextPage 传给 Page，直到它为空。
```

Java

```java
var params = SessionListParams.builder()
    .agentId(agent.id())
    .limit(1)
    .build();
var firstPage = client.beta().sessions().list(params);
for (var listedSession : firstPage.data()) {
    IO.println(listedSession.id() + ": " + listedSession.status());
}
// next_page 指向第 2 页；最后一页没有 next_page。
IO.println("next_page: " + firstPage.response().nextPage());

// 通过将 next_page 作为页面游标传入来前进。
var nextCursor = firstPage.response().nextPage().orElseThrow();
var secondPage = client.beta().sessions().list(params.toBuilder().page(nextCursor).build());

// 继续读取时，将 secondPage.response().nextPage() 传给 page，直到 Optional 为空。
```

PHP

```php
// 第 1 页：nextPage 指向下一页。
$firstPage = $client->beta->sessions->list(agentID: $agent->id, limit: 1);
echo 'Page 1 next_page: ' . ($firstPage->nextPage ?? 'null') . "\n";

// 前进：将 nextPage 作为 `page` 游标传回以获取第 2 页。
$secondPage = $client->beta->sessions->list(
    agentID: $agent->id,
    limit: 1,
    page: $firstPage->nextPage,
);
foreach ($secondPage->getItems() as $listedSession) {
    echo "Page 2: {$listedSession->id}: {$listedSession->status}\n";
}

// 继续读取时，将 secondPage->nextPage 传给 page，直到它为空。
```

Ruby

```ruby
first_page = client.beta.sessions.list(agent_id: agent.id, limit: 1)
first_page.data.each do |listed_session|
  puts "#{listed_session.id}: #{listed_session.status}"
end

# `next_page_` 是下一页游标；当它为 nil 时表示已经读完。
puts "next_page: #{first_page.next_page_.inspect}"

# 将 `next_page_` 作为 `page` 传回，即可继续向后读取。
second_page = client.beta.sessions.list(
  agent_id: agent.id,
  limit: 1,
  page: first_page.next_page_
)
# 继续读取时，重复传入 second_page.next_page_，直到它为 nil。
```

## 更新会话

`POST /v1/sessions/{id}` 用于更新会话的 `title`、`metadata`，以及在会话处于 `idle` 状态时更新智能体的 `tools` 和 `mcp_servers`。请求体接受以下字段：

* `title`：会话标题（字符串），会覆盖现有标题。
* `metadata`：键值对，按键合并到现有元数据上；将某个键的值设为 `null` 可删除该键。
* `agent`：包含 `tools` 和/或 `mcp_servers` 的对象，对提供的字段做整体替换（提供的数组即为新值；如需保留原有条目，请先 `GET` 会话，修改数组后再提交）。这两项的更新是会话本地的，不会回写到底层智能体。

请求体中出现 `environment_id` 或 `resources` 字段会被服务端拒绝并返回 `400`（`<字段> is not supported by this endpoint`）。`agent` 中若包含 `model`、`system`、`skills` 等不可更新字段，会返回 `400`（`agent.<字段> cannot be updated after session creation`）。

> 部分智能体配置在会话创建后不可更改
>
> 会话创建后的配置限制如下：
>
> * `model`、`system`、`skills` 不可更改。需要不同配置时，请在创建会话时使用[智能体配置覆盖](session.md)，同时也可覆盖 `tools` 和 `mcp_servers`。
> * `tools` 和 `mcp_servers` 只能在 `idle` 状态更新，否则返回 `409`（`session must be idle to update agent configuration`）。运行中的会话可先发送 [`user.interrupt` 事件](events.md)，等待 `idle` 后再更新。
> * 模型支持时，可发送 [`system.message` 事件](../../api/api-conventions.md)追加系统级指导。

cURL

```bash
curl --request POST --fail-with-body --silent --show-error "$AGENTAPI_BASE_URL/v1/sessions/$SESSION_ID" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --data @- <<EOF
{
  "title": "重构登录流程",
  "metadata": {"ticket": "ENG-1234"}
}
EOF
```

Python

```python
client.beta.sessions.update(
    session.id,
    title="重构登录流程",
    metadata={"ticket": "ENG-1234"},
)
```

TypeScript

```typescript
await client.beta.sessions.update(session.id, {
  title: "重构登录流程",
  metadata: { ticket: "ENG-1234" }
});
```

## 归档会话

归档会话可阻止发送新事件，同时保留其历史记录。归档后，`status` 变为 `terminated`，AgentAPI 会取消排队任务并请求停止正在执行的任务。若需要在归档前确认当前执行已经停止，请先发送一个 [`user.interrupt` 事件](events.md)并等待会话变为 `idle`。

归档不会回滚已经执行的工具操作。需要再次开展工作时，请创建新会话；查询归档历史时，在会话列表中使用 `include_archived=true`。

cURL

```bash
curl --fail-with-body --silent --show-error --request POST "$AGENTAPI_BASE_URL/v1/sessions/$SESSION_ID/archive" \
  --header "x-api-key: $AGENTAPI_API_KEY"
```

Python

```python
client.beta.sessions.archive(session.id)
```

TypeScript

````typescript
await client.beta.sessions.archive(session.id);
```csharp
```csharp
await client.Beta.Sessions.Archive(session.ID);
````

Go

```go
_, err = client.Beta.Sessions.Archive(ctx, session.ID, anthropic.BetaSessionArchiveParams{})
if err != nil {
	panic(err)
}
```

Java

```java
client.beta().sessions().archive(session.id());
```

PHP

```php
$client->beta->sessions->archive($session->id);
```

Ruby

```ruby
client.beta.sessions.archive(session.id)
```

## 删除会话

删除后，用户接口无法再检索、继续使用或列出该会话；服务只更新会话的删除状态，不清理历史数据。会话事件、Turn、资源元数据、输出文件和挂载到会话的文件副本都会保留，但不再通过公开 API 返回。GitHub 仓库资源上的 `authorization_token` 会在删除时清空，仓库地址和其他资源字段仍保留。

记忆存储、保管库、技能、环境、智能体和独立上传文件不受影响。删除会停止当前执行并撤销自托管访问；删除接口不保证立即销毁关联沙箱，自托管进程的停止和本地文件清理由执行器负责。

cURL

```bash
curl --fail-with-body --silent --show-error --request DELETE "$AGENTAPI_BASE_URL/v1/sessions/$SESSION_ID" \
  --header "x-api-key: $AGENTAPI_API_KEY"
```

Python

```python
client.beta.sessions.delete(session.id)
```

TypeScript

````typescript
await client.beta.sessions.delete(session.id);
```csharp
```csharp
await client.Beta.Sessions.Delete(session.ID);
````

Go

```go
_, err = client.Beta.Sessions.Delete(ctx, session.ID, anthropic.BetaSessionDeleteParams{})
if err != nil {
	panic(err)
}
```

Java

```java
client.beta().sessions().delete(session.id());
```

PHP

```php
$client->beta->sessions->delete($session->id);
```

Ruby

```ruby
client.beta.sessions.delete(session.id)
```
