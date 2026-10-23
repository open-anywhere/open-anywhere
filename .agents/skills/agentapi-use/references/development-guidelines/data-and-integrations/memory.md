<!-- 来源：https://developer-doc.qiniu.com/products/agentapi/development_guidelines/agentapi-data-and-integrations/13603-agentapi-data-and-integrations-memory | 由 sync-docs.py 自动生成，请勿手改 -->

# 构建持久记忆



最近更新时间: 2026-10-13 15:31:46

多个独立任务需要共用项目约定时，可以将资料保存到 Memory Store（记忆存储），在各自的会话中显式引用。存储由 UTF-8 文档组成，资料的读取与写回方式取决于运行时和访问模式。

生成 CSV 报告时，可以只读报告规范，将经确认的新经验保存到另一份可写存储；报告作为[输出文件](files.md)交付。

## 区分任务材料与跨会话资料

| 需要保存的信息         | 使用方式                                                                                                                                      |
| --------------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| 当前任务的对话和执行历史    | 保留会话，并在同一会话继续交互。                                                                                                                          |
| 当前任务的数据或最终产物    | 使用[文件资源与 Files API](files.md)。 |
| 跨任务复用的用户偏好、项目约定 | 创建记忆存储，在新会话中显式引用。                                                                                                                         |
| 访问外部系统的令牌与密钥    | 保存到 [Vault](vaults.md)。        |

先确定哪些信息需要进入下一个任务，再选择保存方式。只用于当前任务的数据不必写入记忆；第三方令牌与密钥应交给 Vault。项目约定需要跨任务复用，但通常不应由每次任务自行修改，因此应以 `read_only` 附加。

## 确定运行时与写入责任

存储属于当前账户，HTTP API 不依赖运行时。将它用于任务时，需要先确认由 AgentAPI、worker 还是应用负责准备和保存内容：

| 执行方式                    | 内容如何进入任务                | 修改如何保存                                                                                                                                              |
| ----------------------- | ----------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| 云端 `claudecode`、`codex` | 自动准备挂载目录，由文件工具读取        | 成功执行后同步 `read_write` 目录                                                                                                                             |
| 云端 `native`             | 应用通过 API 获取内容，或准备普通文件资源 | 应用显式调用记忆 API                                                                                                                                        |
| 自托管 `native`            | 支持记忆的 worker 下载并挂载      | worker 负责同步，见[自托管记忆接入](../execution-environments/self-hosted.md) |

应用也可以通过 API 或 Portal 维护存储，与运行时是否支持自动挂载无关。

存储中的每条记忆（memory）用路径组织，例如 `/project/report-format.md`；返回的 `mem_*` ID 用于检索、更新和删除接口。路径必须以 `/` 开头，不能包含空路径段、`.` 或 `..`，内容必须是 UTF-8 文本。智能体访问的是挂载目录内的文件路径，应用调用 API 时使用的是存储路径或记忆 ID。

每次内容变更都会生成不可变的记忆版本（memory version）。版本用于检查写入方和历史内容；恢复旧内容需要通过 API 重新写入，不会自动恢复沙箱中的文件。

## 记忆读取与更新可见性

![云端 CLI 会话从记忆存储准备挂载目录，智能体通过文件工具访问；成功执行后同步 read\_write 改动并生成版本，云端 Native 不自动挂载](https://developer-doc.qiniu.com/docs-assets/products/agentapi/development_guidelines/agentapi-data-and-integrations/13603-agentapi-data-and-integrations-memory/FkD6N1R22lpDIF6r8OAXEhMs6iO6-a6276f822400.webp)

对于支持挂载的运行时，任务读取沙箱中的文件，应用通过 API 查询服务端内容。系统提示包含存储说明与路径，但不会自动包含全部文档。排查“任务没有使用最新资料”时，先区分修改发生在哪个位置：

* 挂载路径。 请读取会话记忆存储资源返回的 `mount_path`，不要自行构造。目录名由存储显示名转换而来，例如“Demo Memory”对应 `/mnt/memory/demo-memory/`。
* 任务读写。 智能体通过[文件工具](../agent-configuration/tools.md)操作挂载目录。云端成功执行后，`read_write` 目录的修改才会写回服务端并形成版本；文件工具成功不等于持久化完成。
* 写回范围。 挂载目录外的文件不参与记忆同步。需要交付的报告应作为会话输出文件收集，需要跨任务复用的经验才写入记忆目录。
* 模型获知的内容。 系统提示包含显示名、挂载路径、访问模式、`description` 和 `instructions`。应用应在任务要求中说明读取哪些约定，而非假定模型已经获得全文。

云端环境中的 `native` 不会创建上述挂载，请改用本页的 HTTP API 示例。

> 记忆同步范围
>
> 新建沙箱时读取存储内容；复用沙箱时保留已有文件，不会每轮重新下载。API 或其他会话的修改不会实时同步到已有挂载；需要最新内容时，请通过 API 查询或创建新会话。

对于 `self_hosted`，AgentAPI 只下发 `memory_store` 资源，记忆的下载、挂载和同步则由自托管执行器（worker）或启动器（launcher）负责，详见 [自托管执行器](../execution-environments/self-hosted.md)。

云端 `read_write` 目录成功写回后，会生成归属于该会话的记忆版本。自托管 worker 的保护措施见[保护记忆副本](../execution-environments/self-hosted-security.md)。

> 说明
>
> 云端 `read_only` 目录移除写权限位，不同步回存储。root 进程可绕过这些权限位，因此它不构成文件系统隔离；若执行后检测到内容变化，AgentAPI 会拒绝写回并报告错误。

智能体使用工具访问挂载目录时，读写操作会在[事件流](../sessions-and-tasks/events.md)中记录为普通的 `agent.tool_use` 和 `agent.tool_result` 事件。

## 控制写入范围与存储容量

当存储达到 2,000 条记忆的上限时，新建记忆会失败，包括直接调用 `memories.create` 和智能体在挂载目录中新建文件。现有记忆仍可读取和更新。

按业务决定存储的写入方，而不是把每次任务的全部信息都追加进去：

| 内容             | 写入方与维护方式                          |
| -------------- | --------------------------------- |
| 报告格式、验收口径等共享基准 | 应用审核后通过 API 更新，任务只读               |
| 单个用户或项目的已确认经验  | 对应任务可写，应用核对持久化内容及版本               |
| 多个会话同时维护的内容    | 通过 API 使用内容哈希前置条件协调更新，不依赖挂载副本实时同步 |

存储接近容量上限时，整理失效内容或按项目、保留周期拆分。旧资料不再增长时，可以只读引用旧存储，将新信息写入另一份存储；每个会话合计仍不能超过 8 个存储。

## 准备可复用的资料

将稳定的共享约定和任务产生的可写经验放到不同存储，分别决定访问模式与保留周期。下面的示例创建存储、预填充一条报告格式约定，再将存储附加到新会话。

### 创建资料存储

`name` 用于识别存储，`description` 会进入智能体上下文。描述应说明资料适用的任务范围，不应代替具体文档内容。

请先完成[接入准备](../../quick-start/access.md)，SDK 客户端初始化见[通过 API 构建](../../quick-start/quickstart.md)。以下操作创建独立的记忆存储，不会自动修改现有会话。

cURL

```bash
store=$(curl --request POST --fail-with-body --silent --show-error "$AGENTAPI_BASE_URL/v1/memory_stores" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --data '{"name": "项目报告规范", "description": "CSV 报告的格式和验收约定。"}')
store_id=$(jq -r '.id' <<< "$store")
echo "$store_id"  # memstore_01Hx...
```

Python

```python
store = client.beta.memory_stores.create(
    name="项目报告规范",
    description="CSV 报告的格式和验收约定。",
)
print(store.id)  # memstore_01Hx...
```

TypeScript

````typescript
const store = await client.beta.memoryStores.create({
  name: "项目报告规范",
  description: "CSV 报告的格式和验收约定。"
});
console.log(store.id); // memstore_01Hx...
```csharp
```csharp
var store = await client.Beta.MemoryStores.Create(new()
{
    Name = "项目报告规范",
    Description = "CSV 报告的格式和验收约定。",
});
Console.WriteLine(store.ID);  // memstore_01Hx...
````

Go

```go
store, err := client.Beta.MemoryStores.New(ctx, anthropic.BetaMemoryStoreNewParams{
	Name:        "项目报告规范",
	Description: anthropic.String("CSV 报告的格式和验收约定。"),
})
if err != nil {
	panic(err)
}
fmt.Println(store.ID) // memstore_01Hx...
```

Java

```java
var store = client.beta().memoryStores().create(
    MemoryStoreCreateParams.builder()
        .name("项目报告规范")
        .description("CSV 报告的格式和验收约定。")
        .build()
);
IO.println(store.id());  // memstore_01Hx...
```

PHP

```php
$store = $client->beta->memoryStores->create(
    name: '项目报告规范',
    description: 'CSV 报告的格式和验收约定。',
);
echo "{$store->id}\n"; // memstore_01Hx...
```

Ruby

```ruby
store = client.beta.memory_stores.create(
  name: "项目报告规范",
  description: "CSV 报告的格式和验收约定。"
)
puts store.id # memstore_01Hx...
```

记忆存储的 `id`（`memstore_...`）是附加记忆存储时需要传入的值。

### 写入初始约定

首次任务开始前，可以由应用直接写入已经审核的资料。示例在 `/project/report-format.md` 写入 CSV 报告格式要求；后续任务应读取约定，而不是从旧会话里猜测格式。

cURL

```bash
curl --request POST --fail-with-body --silent --show-error "$AGENTAPI_BASE_URL/v1/memory_stores/$store_id/memories" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --data '{"path": "/project/report-format.md", "content": "报告使用 Markdown，包含数据来源、区域汇总表、合计和计算方法。"}' > /dev/null
```

Python

```python
client.beta.memory_stores.memories.create(
    store.id,
    path="/project/report-format.md",
    content="报告使用 Markdown，包含数据来源、区域汇总表、合计和计算方法。",
)
```

TypeScript

````typescript
await client.beta.memoryStores.memories.create(store.id, {
  path: "/project/report-format.md",
  content: "报告使用 Markdown，包含数据来源、区域汇总表、合计和计算方法。"
});
```csharp
```csharp
await client.Beta.MemoryStores.Memories.Create(store.ID, new()
{
    Path = "/project/report-format.md",
    Content = "报告使用 Markdown，包含数据来源、区域汇总表、合计和计算方法。",
});
````

Go

```go
_, err = client.Beta.MemoryStores.Memories.New(ctx, store.ID, anthropic.BetaMemoryStoreMemoryNewParams{
	Path:    "/project/report-format.md",
	Content: anthropic.String("报告使用 Markdown，包含数据来源、区域汇总表、合计和计算方法。"),
})
if err != nil {
	panic(err)
}
```

Java

```java
client.beta().memoryStores().memories().create(
    store.id(),
    MemoryCreateParams.builder()
        .path("/project/report-format.md")
        .content("报告使用 Markdown，包含数据来源、区域汇总表、合计和计算方法。")
        .build()
);
```

PHP

```php
$client->beta->memoryStores->memories->create(
    $store->id,
    path: '/project/report-format.md',
    content: '报告使用 Markdown，包含数据来源、区域汇总表、合计和计算方法。',
);
```

Ruby

```ruby
client.beta.memory_stores.memories.create(
  store.id,
  path: "/project/report-format.md",
  content: "报告使用 Markdown，包含数据来源、区域汇总表、合计和计算方法。"
)
```

> 说明
>
> 单条记忆最大为 100 KiB，一个存储最多包含 2,000 条记忆。请按主题拆分文件，避免把无关内容集中到一个大文档中。

### 为任务选择访问模式

先创建 `claudecode` 或 `codex` [智能体](../agent-configuration/agent.md)和[云端环境](../execution-environments/environment.md)，再通过会话的 `resources` 引用存储。自托管环境使用 `native` 智能体，由 worker 挂载。

* 存储只能在会话创建时附加。更换存储或访问模式需创建新会话，不能在运行中通过会话资源接口修改。
* `instructions` 可选，最多 4,096 个字符，与存储的 `name`、`description` 一起展示给智能体。
* `access` 默认为 `read_write`，仅读取时请设置为 `read_only`。

以下示例以 `read_only` 附加已预填的报告规范，并说明任务需要读取的文件。云端 Native 不自动挂载这份资源，应由应用通过 API 获取内容并提供给任务。需要沉淀新经验时，使用独立可写存储。

cURL

```bash
curl --request POST --fail-with-body --silent --show-error "$AGENTAPI_BASE_URL/v1/sessions" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --data @- <<EOF
{
  "agent": {"type": "agent", "id": "$agent_id", "version": 1},
  "environment_id": "$environment_id",
  "resources": [
    {
      "type": "memory_store",
      "memory_store_id": "$store_id",
      "access": "read_only",
      "instructions": "生成报告前从本存储的实际挂载目录读取 project/report-format.md，按规范交付报告，不修改共享约定。"
    }
  ]
}
EOF
```

Python

```python
session = client.beta.sessions.create(
    agent=agent.id,
    environment_id=environment.id,
    resources=[
        {
            "type": "memory_store",
            "memory_store_id": store.id,
            "access": "read_only",
            "instructions": "生成报告前从本存储的实际挂载目录读取 project/report-format.md，按规范交付报告，不修改共享约定。",
        }
    ],
)
```

TypeScript

````typescript
const session = await client.beta.sessions.create({
  agent: agent.id,
  environment_id: environment.id,
  resources: [
    {
      type: "memory_store",
      memory_store_id: store.id,
      access: "read_only",
      instructions: "生成报告前从本存储的实际挂载目录读取 project/report-format.md，按规范交付报告，不修改共享约定。"
    }
  ]
});
```csharp
```csharp
var session = await client.Beta.Sessions.Create(new()
{
    Agent = agent.ID,
    EnvironmentID = environment.ID,
    Resources =
    [
        new BetaManagedAgentsMemoryStoreResourceParam
        {
            Type = "memory_store",
            MemoryStoreID = store.ID,
            Access = "read_only",
            Instructions = "生成报告前从本存储的实际挂载目录读取 project/report-format.md，按规范交付报告，不修改共享约定。",
        },
    ],
});
````

Go

```go
session, err := client.Beta.Sessions.New(ctx, anthropic.BetaSessionNewParams{
	Agent: anthropic.BetaSessionNewParamsAgentUnion{
		OfString: anthropic.String(agent.ID),
	},
	EnvironmentID: environment.ID,
	Resources: []anthropic.BetaSessionNewParamsResourceUnion{{
		OfMemoryStore: &anthropic.BetaManagedAgentsMemoryStoreResourceParam{
			Type:          anthropic.BetaManagedAgentsMemoryStoreResourceParamTypeMemoryStore,
			MemoryStoreID: store.ID,
			Access:        anthropic.BetaManagedAgentsMemoryStoreResourceParamAccessReadOnly,
			Instructions:  anthropic.String("生成报告前从本存储的实际挂载目录读取 project/report-format.md，按规范交付报告，不修改共享约定。"),
		},
	}},
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
        .addResource(
            BetaManagedAgentsMemoryStoreResourceParam.builder()
                .type(BetaManagedAgentsMemoryStoreResourceParam.Type.MEMORY_STORE)
                .memoryStoreId(store.id())
                .access(BetaManagedAgentsMemoryStoreResourceParam.Access.READ_ONLY)
                .instructions("生成报告前从本存储的实际挂载目录读取 project/report-format.md，按规范交付报告，不修改共享约定。")
                .build()
        )
        .build()
);
```

PHP

```php
$session = $client->beta->sessions->create(
    agent: $agent->id,
    environmentID: $environment->id,
    resources: [
        [
            'type' => 'memory_store',
            'memory_store_id' => $store->id,
            'access' => 'read_only',
            'instructions' => '生成报告前从本存储的实际挂载目录读取 project/report-format.md，按规范交付报告，不修改共享约定。',
        ],
    ],
);
```

Ruby

```ruby
session = client.beta.sessions.create(
  agent: agent.id,
  environment_id: environment.id,
  resources: [
    {
      type: "memory_store",
      memory_store_id: store.id,
      access: "read_only",
      instructions: "生成报告前从本存储的实际挂载目录读取 project/report-format.md，按规范交付报告，不修改共享约定。"
    }
  ]
)
```

创建会话只会保存资源配置。请继续发送任务消息，要求智能体按需读取记忆；存储说明与 `instructions` 会进入上下文，但全部文档内容不会自动成为完整的对话输入。

> 说明
>
> 不受信任的用户输入、网页或工具输出可能通过提示注入污染记忆，影响后续会话。参考资料、共享查询数据等无需修改的存储，请使用 `read_only`。

每个会话最多附加 8 个记忆存储。可按用户、团队或项目拆分，分别管理访问权限和保留周期；共享只读资料与会话读写记忆应分开存储。

### 将已确认的经验保存到独立存储

需要保存经业务确认的新经验时，另建存储，并在附加资源时设置以下访问模式。此资源片段用于支持云端记忆挂载的 `claudecode` 或 `codex` 会话：

```json
{
  "resources": [
    {"type": "memory_store", "memory_store_id": "your-rules-store-id", "access": "read_only"},
    {"type": "memory_store", "memory_store_id": "your-experience-store-id", "access": "read_write",
     "instructions": "仅保存业务明确确认的新经验，使用本存储的实际挂载路径。"}
  ]
}
```

共享规范由应用审核后维护，可写经验不替代规范。任务结束后，按下节查询服务端内容与版本确认写回。

## 核对本轮持久化结果

任务结束后，应用应读取服务端记忆并核对预期内容。报告任务的回答中出现“已保存”或文件工具返回成功，均不能代替这一步。读写异常应先检查运行时是否支持挂载、是否以只读方式引用，以及修改是否位于挂载目录。

先列出任务涉及的文档，再读取服务端内容。发现误写时，通过版本历史追查来源，再修正资料。业务资料的审核规则由应用执行；AgentAPI 不会自动判断一次写入是否适合供后续任务使用。

### 确定检查范围

按项目路径列出需要检查的文档，而不是每次将整个存储加入任务输入。列表顺序由服务端稳定定义，不表示内容的重要性。

* `path_prefix` 将列表范围限定到一个目录。它必须以 `/` 结尾，并匹配完整的路径段，因此 `path_prefix=/notes/` 会返回 `/notes/todo.md`，但不会返回 `/notes-archive/todo.md`。
* `depth` 控制列表在 `path_prefix` 之下深入的层级：省略它以列出整个子树；传递正整数 `N` 则展开到第 `N` 层（`1` 仅列出直接子项）。`0` 或任何非正整数会返回 `400 depth must be a positive integer` 错误。

cURL

```bash
curl --request GET --fail-with-body --silent --show-error "$AGENTAPI_BASE_URL/v1/memory_stores/$store_id/memories?path_prefix=/" \
  --header "x-api-key: $AGENTAPI_API_KEY"
```

Python

```python
page = client.beta.memory_stores.memories.list(
    store.id,
    path_prefix="/",
)
for item in page.data:
    print(item.type, item.path)
```

TypeScript

````typescript
const page = await client.beta.memoryStores.memories.list(store.id, {
  path_prefix: "/"
});
for (const item of page.data) {
  console.log(item.type, item.path);
}
```csharp
```csharp
var page = await client.Beta.MemoryStores.Memories.List(store.ID, new()
{
    PathPrefix = "/",
});
await foreach (var item in page.Paginate())
{
    var line = item.Match(m => $"memory  {m.Path}", p => $"memory_prefix  {p.Path}");
    Console.WriteLine(line);
}
````

Go

```go
page, err := client.Beta.MemoryStores.Memories.List(ctx, store.ID, anthropic.BetaMemoryStoreMemoryListParams{
	PathPrefix: anthropic.String("/"),
})
if err != nil {
	panic(err)
}
for _, item := range page.Data {
	fmt.Println(item.Type, item.Path)
}
```

Java

```java
var page = client.beta().memoryStores().memories().list(
    store.id(),
    MemoryListParams.builder()
        .pathPrefix("/")
        .build()
);
for (var item : page.data()) {
    item.memory().ifPresent(m -> IO.println("memory  " + m.path()));
    item.memoryPrefix().ifPresent(p -> IO.println("memory_prefix  " + p.path()));
}
```

PHP

```php
$page = $client->beta->memoryStores->memories->list(
    $store->id,
    pathPrefix: '/',
);
foreach ($page->data as $item) {
    echo "{$item->type}  {$item->path}\n";
}
```

Ruby

```ruby
page = client.beta.memory_stores.memories.list(
  store.id,
  path_prefix: "/"
)
page.data.each do |entry|
  puts "#{entry.type}  #{entry.path}"
end
```

### 读取服务端内容

使用记忆 ID 获取完整内容，核对本轮要求保存的约定或经验是否已经出现在服务端。尚未写回时，不要仅凭会话回答向后续任务宣布资料已更新。

cURL

```bash
curl --request GET --fail-with-body --silent --show-error "$AGENTAPI_BASE_URL/v1/memory_stores/$store_id/memories/$mem_id" \
  --header "x-api-key: $AGENTAPI_API_KEY"
```

Python

```python
retrieved = client.beta.memory_stores.memories.retrieve(
    mem.id,
    memory_store_id=store.id,
)
print(retrieved.content)
```

TypeScript

````typescript
const retrieved = await client.beta.memoryStores.memories.retrieve(mem.id, {
  memory_store_id: store.id
});
console.log(retrieved.content);
```csharp
```csharp
var retrieved = await client.Beta.MemoryStores.Memories.Retrieve(mem.ID, new()
{
    MemoryStoreID = store.ID,
});
Console.WriteLine(retrieved.Content);
````

Go

```go
retrieved, err := client.Beta.MemoryStores.Memories.Get(ctx, mem.ID, anthropic.BetaMemoryStoreMemoryGetParams{
	MemoryStoreID: store.ID,
})
if err != nil {
	panic(err)
}
fmt.Println(retrieved.Content)
```

Java

```java
var retrieved = client.beta().memoryStores().memories().retrieve(
    mem.id(),
    MemoryRetrieveParams.builder().memoryStoreId(store.id()).build()
);
IO.println(retrieved.content().orElseThrow());
```

PHP

```php
$retrieved = $client->beta->memoryStores->memories->retrieve($mem->id, memoryStoreID: $store->id);
echo "{$retrieved->content}\n";
```

Ruby

```ruby
retrieved = client.beta.memory_stores.memories.retrieve(
  mem.id,
  memory_store_id: store.id
)
puts retrieved.content
```

## 检查变更来源并恢复资料

后续任务使用了错误约定时，先读取当前内容，再查看版本历史，确定是哪次写入引入了变化。记忆版本（`memver_...`）保存变更的执行方、时间和内容；模型在当前任务中的解释不能替代这份记录。

版本接口属于记忆存储。`memories.retrieve` 返回当前内容，版本接口返回历史快照；删除当前记忆不会删除已经生成的版本。处理误写敏感信息时，必须同时检查当前内容与历史版本，不能只删除当前文档。

恢复流程由应用完成：读取仍被保留的目标版本，用 `memories.update` 将其 `content` 写回；父记忆已删除时使用 `memories.create` 重新创建。没有单独的恢复端点，恢复后也不能假定已有沙箱已下载新内容。

AgentAPI 当前不在公共契约中承诺固定的版本保留时长。需要长期留存审计记录时，请定期通过 API 导出版本。

### 定位发生变化的版本

列出存储的版本历史，最新的在前。以下示例筛选出单条记忆的历史：

cURL

```bash
versions=$(curl --request GET --fail-with-body --silent --show-error "$AGENTAPI_BASE_URL/v1/memory_stores/$store_id/memory_versions?memory_id=$mem_id" \
  --header "x-api-key: $AGENTAPI_API_KEY")
jq -r '.data[] | "\(.id): \(.operation)"' <<< "$versions"
version_id=$(jq -r '.data[1].id' <<< "$versions")
```

Python

```python
versions = client.beta.memory_stores.memory_versions.list(
    store.id,
    memory_id=mem.id,
)
for version in versions:
    print(f"{version.id}: {version.operation}")

version_id = versions.data[1].id
```

TypeScript

````typescript
const versions = await client.beta.memoryStores.memoryVersions.list(store.id, {
  memory_id: mem.id
});
for await (const v of versions) {
  console.log(`${v.id}: ${v.operation}`);
}

const versionId = versions.data[1].id;
```csharp
```csharp
var versions = await client.Beta.MemoryStores.MemoryVersions.List(store.ID, new()
{
    MemoryID = mem.ID,
});
var versionIds = new List<string>();
await foreach (var v in versions.Paginate())
{
    Console.WriteLine($"{v.ID}: {v.Operation.Raw()}");
    versionIds.Add(v.ID);
}

var versionId = versionIds[1];
````

Go

```go
versions := client.Beta.MemoryStores.MemoryVersions.ListAutoPaging(ctx, store.ID, anthropic.BetaMemoryStoreMemoryVersionListParams{
	MemoryID: anthropic.String(mem.ID),
})
for versions.Next() {
	v := versions.Current()
	fmt.Printf("%s: %s\n", v.ID, v.Operation)
}
if err := versions.Err(); err != nil {
	panic(err)
}

vpage, err := client.Beta.MemoryStores.MemoryVersions.List(ctx, store.ID, anthropic.BetaMemoryStoreMemoryVersionListParams{
	MemoryID: anthropic.String(mem.ID),
})
if err != nil {
	panic(err)
}
versionID := vpage.Data[1].ID
```

Java

```java
var versions = client.beta().memoryStores().memoryVersions().list(
    store.id(),
    MemoryVersionListParams.builder().memoryId(mem.id()).build()
);
for (var v : versions.autoPager()) {
    IO.println(v.id() + ": " + v.operation());
}

var versionId = versions.data().get(1).id();
```

PHP

```php
$versions = $client->beta->memoryStores->memoryVersions->list(
    $store->id,
    memoryID: $mem->id,
);
foreach ($versions->pagingEachItem() as $v) {
    echo "{$v->id}: {$v->operation}\n";
}

$versionId = $versions->data[1]->id;
```

Ruby

```ruby
versions = client.beta.memory_stores.memory_versions.list(
  store.id,
  memory_id: mem.id
)
versions.auto_paging_each do |version|
  puts "#{version.id}: #{version.operation}"
end

version_id = versions.data[1].id
```

### 读取待恢复的快照

获取单个版本会返回与列表响应相同的字段，外加完整的 `content` 正文。

cURL

```bash
curl --request GET --fail-with-body --silent --show-error "$AGENTAPI_BASE_URL/v1/memory_stores/$store_id/memory_versions/$version_id" \
  --header "x-api-key: $AGENTAPI_API_KEY"
```

Python

```python
version = client.beta.memory_stores.memory_versions.retrieve(
    version_id,
    memory_store_id=store.id,
)
print(version.content)
```

TypeScript

````typescript
const version = await client.beta.memoryStores.memoryVersions.retrieve(versionId, {
  memory_store_id: store.id
});
console.log(version.content);
```csharp
```csharp
var version = await client.Beta.MemoryStores.MemoryVersions.Retrieve(versionId, new()
{
    MemoryStoreID = store.ID,
});
Console.WriteLine(version.Content);
````

Go

```go
version, err := client.Beta.MemoryStores.MemoryVersions.Get(ctx, versionID, anthropic.BetaMemoryStoreMemoryVersionGetParams{
	MemoryStoreID: store.ID,
})
if err != nil {
	panic(err)
}
fmt.Println(version.Content)
```

Java

```java
var version = client.beta().memoryStores().memoryVersions().retrieve(
    versionId,
    MemoryVersionRetrieveParams.builder().memoryStoreId(store.id()).build()
);
IO.println(version.content().orElseThrow());
```

PHP

```php
$version = $client->beta->memoryStores->memoryVersions->retrieve(
    $versionId,
    memoryStoreID: $store->id,
);
echo "{$version->content}\n";
```

Ruby

```ruby
version = client.beta.memory_stores.memory_versions.retrieve(
  version_id,
  memory_store_id: store.id
)
puts version.content
```

### 清除历史中的敏感内容

脱敏会从历史版本中清除内容，同时保留执行方、变更时间等审计信息。可将其用于合规工作流，例如移除泄露的密钥、个人身份信息（PII）或处理用户删除请求。

当前记忆正在使用的最新版本不能脱敏。请先写入一个新版本（或删除该记忆），然后再对旧版本进行脱敏。

cURL

```bash
curl --fail-with-body --silent --show-error --request POST "$AGENTAPI_BASE_URL/v1/memory_stores/$store_id/memory_versions/$version_id/redact" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --data '{}'
```

Python

```python
client.beta.memory_stores.memory_versions.redact(
    version_id,
    memory_store_id=store.id,
)
```

TypeScript

````typescript
await client.beta.memoryStores.memoryVersions.redact(versionId, {
  memory_store_id: store.id
});
```csharp
```csharp
await client.Beta.MemoryStores.MemoryVersions.Redact(versionId, new()
{
    MemoryStoreID = store.ID,
});
````

Go

```go
_, err = client.Beta.MemoryStores.MemoryVersions.Redact(ctx, versionID, anthropic.BetaMemoryStoreMemoryVersionRedactParams{
	MemoryStoreID: store.ID,
})
if err != nil {
	panic(err)
}
```

Java

```java
client.beta().memoryStores().memoryVersions().redact(
    versionId,
    MemoryVersionRedactParams.builder().memoryStoreId(store.id()).build()
);
```

PHP

```php
$client->beta->memoryStores->memoryVersions->redact(
    $versionId,
    memoryStoreID: $store->id,
);
```

Ruby

```ruby
client.beta.memory_stores.memory_versions.redact(
  version_id,
  memory_store_id: store.id
)
```

## 审核后修正资料

确认错误来源后，再更新或删除当前内容。涉及多个写入方时，使用内容哈希前置条件，避免审核期间产生的新修改被覆盖。修正服务端资料后，已有沙箱的文件仍可能保留旧内容；后续任务需要最新资料时，通过 API 获取或创建新会话。

### 补充已审核的资料

应用确认新资料适合跨任务使用后，通过 `memories.create` 写入指定路径。该操作不覆盖已有内容；已存在的约定需要用 `memories.update` 修改，不能通过重复创建替换。

cURL

```bash
mem=$(curl --request POST --fail-with-body --silent --show-error "$AGENTAPI_BASE_URL/v1/memory_stores/$store_id/memories" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --data '{"path": "/project/report-checks.md", "content": "报告必须说明数据来源、统计范围和计算方法。"}')
mem_id=$(jq -r '.id' <<< "$mem")
mem_sha=$(jq -r '.content_sha256' <<< "$mem")
```

Python

```python
mem = client.beta.memory_stores.memories.create(
    store.id,
    path="/project/report-checks.md",
    content="报告必须说明数据来源、统计范围和计算方法。",
)
```

TypeScript

````typescript
const mem = await client.beta.memoryStores.memories.create(store.id, {
  path: "/project/report-checks.md",
  content: "报告必须说明数据来源、统计范围和计算方法。"
});
```csharp
```csharp
var mem = await client.Beta.MemoryStores.Memories.Create(store.ID, new()
{
    Path = "/project/report-checks.md",
    Content = "报告必须说明数据来源、统计范围和计算方法。",
});
````

Go

```go
mem, err := client.Beta.MemoryStores.Memories.New(ctx, store.ID, anthropic.BetaMemoryStoreMemoryNewParams{
	Path:    "/project/report-checks.md",
	Content: anthropic.String("报告必须说明数据来源、统计范围和计算方法。"),
})
if err != nil {
	panic(err)
}
```

Java

```java
var mem = client.beta().memoryStores().memories().create(
    store.id(),
    MemoryCreateParams.builder()
        .path("/project/report-checks.md")
        .content("报告必须说明数据来源、统计范围和计算方法。")
        .build()
);
```

PHP

```php
$mem = $client->beta->memoryStores->memories->create(
    $store->id,
    path: '/project/report-checks.md',
    content: '报告必须说明数据来源、统计范围和计算方法。',
);
```

Ruby

```ruby
mem = client.beta.memory_stores.memories.create(
  store.id,
  path: "/project/report-checks.md",
  content: "报告必须说明数据来源、统计范围和计算方法。"
)
```

### 修正或迁移已有内容

纠正内容时通过 ID 更新 `content`；调整资料目录时更新 `path`，也可以同时修改。下面的示例将原路径改为归档目录，这是内容组织方式，不等同于归档整个存储。

cURL

```bash
curl --fail-with-body --silent --show-error --request POST "$AGENTAPI_BASE_URL/v1/memory_stores/$store_id/memories/$mem_id" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --data '{"path": "/archive/report-checks.md"}' > /dev/null
```

Python

```python
client.beta.memory_stores.memories.update(
    mem.id,
    memory_store_id=store.id,
    path="/archive/report-checks.md",
)
```

TypeScript

````typescript
await client.beta.memoryStores.memories.update(mem.id, {
  memory_store_id: store.id,
  path: "/archive/report-checks.md"
});
```csharp
```csharp
await client.Beta.MemoryStores.Memories.Update(mem.ID, new()
{
    MemoryStoreID = store.ID,
    Path = "/archive/report-checks.md",
});
````

Go

```go
_, err = client.Beta.MemoryStores.Memories.Update(ctx, mem.ID, anthropic.BetaMemoryStoreMemoryUpdateParams{
	MemoryStoreID: store.ID,
	Path:          anthropic.String("/archive/report-checks.md"),
})
if err != nil {
	panic(err)
}
```

Java

```java
client.beta().memoryStores().memories().update(
    mem.id(),
    MemoryUpdateParams.builder()
        .memoryStoreId(store.id())
        .path("/archive/report-checks.md")
        .build()
);
```

PHP

```php
$client->beta->memoryStores->memories->update(
    $mem->id,
    memoryStoreID: $store->id,
    path: '/archive/report-checks.md',
);
```

Ruby

```ruby
client.beta.memory_stores.memories.update(
  mem.id,
  memory_store_id: store.id,
  path: "/archive/report-checks.md"
)
```

#### 避免覆盖其他写入方的修改

为避免覆盖并发写入，请先读取当前记忆，再传递 `content_sha256` 前置条件。只有当存储的内容哈希仍与读取时的哈希匹配时，更新才会生效；不匹配时返回 `409`。请重新读取并合并变更后重试，避免直接去掉前置条件覆盖其他写入。

cURL

```bash
curl --fail-with-body --silent --show-error --request POST "$AGENTAPI_BASE_URL/v1/memory_stores/$store_id/memories/$mem_id" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --data @- > /dev/null <<EOF
{
  "content": "报告必须说明数据来源、统计范围和计算方法，并核对区域数据与合计。",
  "precondition": {"type": "content_sha256", "content_sha256": "$mem_sha"}
}
EOF
```

Python

```python
client.beta.memory_stores.memories.update(
    memory_id=mem.id,
    memory_store_id=store.id,
    content="报告必须说明数据来源、统计范围和计算方法，并核对区域数据与合计。",
    precondition={"type": "content_sha256", "content_sha256": mem.content_sha256},
)
```

TypeScript

````typescript
await client.beta.memoryStores.memories.update(mem.id, {
  memory_store_id: store.id,
  content: "报告必须说明数据来源、统计范围和计算方法，并核对区域数据与合计。",
  precondition: { type: "content_sha256", content_sha256: mem.content_sha256 }
});
```csharp
```csharp
await client.Beta.MemoryStores.Memories.Update(mem.ID, new()
{
    MemoryStoreID = store.ID,
    Content = "报告必须说明数据来源、统计范围和计算方法，并核对区域数据与合计。",
    Precondition = new BetaManagedAgentsPrecondition
    {
        Type = "content_sha256",
        ContentSha256 = mem.ContentSha256,
    },
});
````

Go

```go
_, err = client.Beta.MemoryStores.Memories.Update(ctx, mem.ID, anthropic.BetaMemoryStoreMemoryUpdateParams{
	MemoryStoreID: store.ID,
	Content:       anthropic.String("报告必须说明数据来源、统计范围和计算方法，并核对区域数据与合计。"),
	Precondition: anthropic.BetaManagedAgentsPreconditionParam{
		Type:          anthropic.BetaManagedAgentsPreconditionTypeContentSha256,
		ContentSha256: anthropic.String(mem.ContentSha256),
	},
})
if err != nil {
	panic(err)
}
```

Java

```java
client.beta().memoryStores().memories().update(
    mem.id(),
    MemoryUpdateParams.builder()
        .memoryStoreId(store.id())
        .content("报告必须说明数据来源、统计范围和计算方法，并核对区域数据与合计。")
        .precondition(
            BetaManagedAgentsPrecondition.builder()
                .type(BetaManagedAgentsPrecondition.Type.CONTENT_SHA256)
                .contentSha256(mem.contentSha256())
                .build()
        )
        .build()
);
```

PHP

```php
$client->beta->memoryStores->memories->update(
    $mem->id,
    memoryStoreID: $store->id,
    content: '报告必须说明数据来源、统计范围和计算方法，并核对区域数据与合计。',
    precondition: ['type' => 'content_sha256', 'content_sha256' => $mem->contentSha256],
);
```

Ruby

```ruby
client.beta.memory_stores.memories.update(
  mem.id,
  memory_store_id: store.id,
  content: "报告必须说明数据来源、统计范围和计算方法，并核对区域数据与合计。",
  precondition: {type: "content_sha256", content_sha256: mem.content_sha256}
)
```

### 移除不再使用的资料

cURL

```bash
curl --fail-with-body --silent --show-error --request DELETE "$AGENTAPI_BASE_URL/v1/memory_stores/$store_id/memories/$mem_id" \
  --header "x-api-key: $AGENTAPI_API_KEY"
```

Python

```python
client.beta.memory_stores.memories.delete(
    mem.id,
    memory_store_id=store.id,
)
```

TypeScript

````typescript
await client.beta.memoryStores.memories.delete(mem.id, {
  memory_store_id: store.id
});
```csharp
```csharp
await client.Beta.MemoryStores.Memories.Delete(mem.ID, new()
{
    MemoryStoreID = store.ID,
});
````

Go

```go
_, err = client.Beta.MemoryStores.Memories.Delete(ctx, mem.ID, anthropic.BetaMemoryStoreMemoryDeleteParams{
	MemoryStoreID: store.ID,
})
if err != nil {
	panic(err)
}
```

Java

```java
client.beta().memoryStores().memories().delete(
    mem.id(),
    MemoryDeleteParams.builder().memoryStoreId(store.id()).build()
);
```

PHP

```php
$client->beta->memoryStores->memories->delete($mem->id, memoryStoreID: $store->id);
```

Ruby

```ruby
client.beta.memory_stores.memories.delete(
  mem.id,
  memory_store_id: store.id
)
```

## 退出复用与清理资料

项目结束或约定不再适用时，先停止为新任务选择该存储，再决定归档或删除。归档保留资料、停止新会话引用及后续写回；删除用于永久移除存储、记忆和版本。需要长期审计的内容应在删除前导出。

以下示例用于找到待清理的存储并归档。分页和响应格式见 [API 通用约定](../../api/api-conventions.md)。

### 列出存储

列出当前账户下的记忆存储。默认排除已归档的存储；传递 `include_archived: true` 可将其包含在内。

Portal 的 Memory Store 列表支持按名称或 ID 搜索，并与可用、已归档、全部状态筛选组合。关键词会去除首尾空白，按字面量包含关系匹配；搜索和状态筛选均在统计总数和分页前执行。对应接口为 `GET /portal/v1/memory_stores?page_index=1&q=关键词&status=active`，`status` 可取 `active` 或 `archived`；查看全部状态时省略 `status` 并传入 `include_archived=true`。`q` 和 `status` 必须与 `page_index` 一起使用，公开的 `GET /v1/memory_stores` 不接受这两个参数。

cURL

```bash
curl --request GET --fail-with-body --silent --show-error "$AGENTAPI_BASE_URL/v1/memory_stores?include_archived=true" \
  --header "x-api-key: $AGENTAPI_API_KEY"
```

Python

```python
for memory_store in client.beta.memory_stores.list(include_archived=True):
    print(memory_store.id, memory_store.name, memory_store.archived_at)
```

TypeScript

````typescript
for await (const s of client.beta.memoryStores.list({ include_archived: true })) {
  console.log(s.id, s.name, s.archived_at);
}
```csharp
```csharp
var stores = await client.Beta.MemoryStores.List(new() { IncludeArchived = true });
await foreach (var s in stores.Paginate())
{
    Console.WriteLine($"{s.ID} {s.Name} {s.ArchivedAt}");
}
````

Go

```go
stores := client.Beta.MemoryStores.ListAutoPaging(ctx, anthropic.BetaMemoryStoreListParams{
	IncludeArchived: anthropic.Bool(true),
})
for stores.Next() {
	s := stores.Current()
	fmt.Println(s.ID, s.Name, s.ArchivedAt)
}
if err := stores.Err(); err != nil {
	panic(err)
}
```

Java

```java
for (var s : client.beta().memoryStores().list(
    MemoryStoreListParams.builder().includeArchived(true).build()
).autoPager()) {
    IO.println(s.id() + " " + s.name() + " " + s.archivedAt());
}
```

PHP

```php
foreach ($client->beta->memoryStores->list(includeArchived: true)->pagingEachItem() as $s) {
    // archivedAt 仅在已归档的存储上设置。
    $archivedAt = isset($s->archivedAt) ? $s->archivedAt->format(DATE_ATOM) : '';
    echo "{$s->id} {$s->name} {$archivedAt}\n";
}
```

Ruby

```ruby
client.beta.memory_stores.list(include_archived: true).auto_paging_each do |memory_store|
  puts "#{memory_store.id} #{memory_store.name} #{memory_store.archived_at}"
end
```

### 归档存储

归档会使存储变为只读，并阻止其被附加到新会话。归档是单向的；没有取消归档操作。已有会话再次执行时也不能向已归档的存储写回。

cURL

```bash
curl --fail-with-body --silent --show-error --request POST "$AGENTAPI_BASE_URL/v1/memory_stores/$store_id/archive" \
  --header "x-api-key: $AGENTAPI_API_KEY"
```

Python

```python
client.beta.memory_stores.archive(store.id)
```

TypeScript

````typescript
await client.beta.memoryStores.archive(store.id);
```csharp
```csharp
await client.Beta.MemoryStores.Archive(store.ID);
````

Go

```go
_, err = client.Beta.MemoryStores.Archive(ctx, store.ID, anthropic.BetaMemoryStoreArchiveParams{})
if err != nil {
	panic(err)
}
```

Java

```java
client.beta().memoryStores().archive(store.id());
```

PHP

```php
$client->beta->memoryStores->archive($store->id);
```

Ruby

```ruby
client.beta.memory_stores.archive(store.id)
```

要永久移除存储及其所有记忆和版本，请调用 `DELETE /v1/memory_stores/{memory_store_id}`。
