<!-- 来源：https://developer-doc.qiniu.com/products/agentapi/development_guidelines/agentapi-data-and-integrations/13602-agentapi-data-and-integrations-files | 由 sync-docs.py 自动生成，请勿手改 -->

# 附加和下载文件



最近更新时间: 2026-10-20 17:43:05

文件交付包含两个结果：任务能在云端沙箱中读取输入材料，应用能下载最终产物。Files API 保存文件对象，会话资源负责把输入文件挂载到沙箱；沙箱中的任意文件不会自动成为可下载的产物。

以 CSV 分析任务为例，约定输入路径 `/data.csv` 与报告文件 `report.md`，再按任务阶段操作：

1. 上传 CSV，保存源文件的 `file_id`。
2. 创建会话并挂载文件，指定沙箱内的读取路径。
3. 向会话发送分析要求，让智能体读取数据并把报告写入 `/mnt/session/outputs/report.md`。
4. 本轮执行结束后，列出并下载会话文件，确认预期产物已经收集。

任务中途收到新的数据，可补充会话文件。报告下载并验收后，再清理文件对象。会话创建和消息发送见[会话](../sessions-and-tasks/session.md)。

![File Object 的数据来源与生命周期](https://developer-doc.qiniu.com/docs-assets/products/agentapi/development_guidelines/agentapi-data-and-integrations/13602-agentapi-data-and-integrations-files/FlVhNRdts2sP9ww5zSUdbOlSKxg7-411aeaeade7d.webp)

同一份数据上传、挂载和生成输出时对应不同文件对象。保留策略需要区分这些对象，不能仅按沙箱路径判断：

| 文件对象   | 如何产生                               | `scope_id` | 沙箱中删除路径 | 归档会话 | 删除会话           |
| ------ | ---------------------------------- | ---------- | ------- | ---- | -------------- |
| 上传源文件  | `POST /v1/files`                   | 无          | 不适用     | 不受影响 | 不受影响           |
| 会话挂载副本 | 将源文件加入 `resources`                 | 会话 ID      | 不影响文件对象 | 保留   | 保留但公开 API 不再返回 |
| 会话输出文件 | 智能体写入 `/mnt/session/outputs/` 后被收集 | 会话 ID      | 不影响文件对象 | 保留   | 保留但公开 API 不再返回 |

## 准备任务输入

先确认输入格式能由所选运行时、模型或工具处理，再上传材料并确定读取路径。需要分析多份数据时，把每个文件及其用途写入任务要求，避免智能体自行猜测。

### 判断材料能否处理

Files API 将文件按原始字节保存，不限制扩展名。智能体是否能理解或处理某种格式取决于所选运行时、模型和已安装工具。常见输入包括：

* 源代码（`.py`、`.js`、`.ts`、`.go`、`.rs` 等）
* 数据文件（`.csv`、`.json`、`.xml`、`.yaml`）
* 文档（`.txt`、`.md`）
* 归档文件（`.zip`、`.tar.gz`）
* 图片、PDF 和其他二进制文件

### 上传源材料

完成[接入准备](../../quick-start/access.md)后，上传本地 `data.csv`，保存返回的 `file_id`，供会话引用。此时文件仍只是源材料，尚未进入任何会话。以下 SDK 示例沿用[SDK 客户端初始化](../../quick-start/quickstart.md)中的 `client`。

cURL

```bash
file=$(curl --request POST "$AGENTAPI_BASE_URL/v1/files" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --form "file=@data.csv")
file_id=$(jq -er '.id' <<<"${file}")
printf 'File ID: %s\n' "${file_id}"
```

Python

```python
file = client.beta.files.upload(file=Path("data.csv"))
print(f"File ID: {file.id}")
```

TypeScript

````typescript
const file = await client.beta.files.upload({
  file: await toFile(readFile("data.csv"), "data.csv", { type: "text/csv" }),
});
console.log(`File ID: ${file.id}`);
```csharp
```csharp
await using var stream = File.OpenRead(csvPath);
var file = await client.Beta.Files.Upload(new() { File = stream });
Console.WriteLine($"File ID: {file.ID}");
````

Go

```go
csvFile, err := os.Open("data.csv")
if err != nil {
	panic(err)
}
defer csvFile.Close()

file, err := client.Beta.Files.Upload(ctx, anthropic.BetaFileUploadParams{
	File: csvFile,
})
if err != nil {
	panic(err)
}
fmt.Printf("File ID: %s\n", file.ID)
```

Java

```java
var file = client.beta().files().upload(
    FileUploadParams.builder().file(dataCsv).build()
);
IO.println("File ID: " + file.id());
```

PHP

```php
$file = $client->beta->files->upload(
    file: FileParam::fromResource(fopen($csvPath, 'r'), filename: 'data.csv', contentType: 'text/csv'),
);
echo "File ID: {$file->id}\n";
```

Ruby

```ruby
file = client.beta.files.upload(file: Pathname(csv_path))
puts "File ID: #{file.id}"
```

### 约定会话读取路径

创建[智能体](../agent-configuration/agent.md)和[云端环境](../execution-environments/environment.md)后，用会话的 `resources` 引用源材料。以下示例把 CSV 挂载到 `/data.csv`；后续任务要求应使用同一路径，而不是本地上传路径。

> 说明
>
> 可省略 `mount_path`，此时读取路径为 `/mnt/session/uploads/<file_id>`。显式指定路径便于在任务要求中定位材料。

cURL

```bash
session=$(
  jq -n \
    --arg agent_id "${agent_id}" \
    --arg environment_id "${environment_id}" \
    --arg file_id "${file_id}" \
    '{
      agent: $agent_id,
      environment_id: $environment_id,
      resources: [
        {
          type: "file",
          file_id: $file_id,
          mount_path: "/data.csv"
        }
      ]
    }' | curl --fail-with-body -sS -H "x-api-key: $AGENTAPI_API_KEY" "$AGENTAPI_BASE_URL/v1/sessions" -H "content-type: application/json" --data @-
)
session_id=$(jq -er '.id' <<<"${session}")
```

Python

```python
session = client.beta.sessions.create(
    agent=agent.id,
    environment_id=environment.id,
    resources=[
        {
            "type": "file",
            "file_id": file.id,
            "mount_path": "/data.csv",
        },
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
      type: "file",
      file_id: file.id,
      mount_path: "/data.csv",
    },
  ],
});
```csharp
```csharp
var session = await client.Beta.Sessions.Create(new()
{
    Agent = agent.ID,
    EnvironmentID = environment.ID,
    Resources =
    [
        new BetaManagedAgentsFileResourceParams
        {
            Type = "file",
            FileID = file.ID,
            MountPath = "/data.csv",
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
		OfFile: &anthropic.BetaManagedAgentsFileResourceParams{
			Type:      anthropic.BetaManagedAgentsFileResourceParamsTypeFile,
			FileID:    file.ID,
			MountPath: anthropic.String("/data.csv"),
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
            BetaManagedAgentsFileResourceParams.builder()
                .type(BetaManagedAgentsFileResourceParams.Type.FILE)
                .fileId(file.id())
                .mountPath("/data.csv")
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
        BetaManagedAgentsFileResourceParams::with(
            type: 'file',
            fileID: $file->id,
            mountPath: '/data.csv',
        ),
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
      type: "file",
      file_id: file.id,
      mount_path: "/data.csv"
    }
  ]
)
```

使用上述 `mount_path`，智能体将在 `/data.csv` 读取该文件。`mount_path` 是沙箱内的绝对路径，不会自动拼接 uploads 目录；省略时才使用 `/mnt/session/uploads/<file_id>`。请参阅文件路径。

AgentAPI 会为会话中的只读副本创建新的 `file_id`。删除原始上传文件不会影响已经挂载到会话中的副本。

### 区分输入路径与输出路径

> 说明
>
> 挂载到沙箱中的文件是只读副本。智能体可以读取它们，但无法修改原始上传的文件。若要使用修改后的版本，智能体会将其写入沙箱内的新路径。

* `mount_path` 是沙箱内的绝对路径：`/data.csv` 会挂载到 `/data.csv`
* 如果省略 `mount_path`，文件将被放置在 `/mnt/session/uploads/<file_id>`
* 父目录会自动创建
* 路径应为绝对路径（以 `/` 开头）
* 智能体写入 `/mnt/session/outputs/` 的文件可通过 Files API 获取，且限定于该会话范围内；请参阅列出和下载会话文件

### 为一项任务准备多份材料

例如同时分析原始数据和参考数据时，为每份材料添加一个 `resources` 条目，并分别指定读取路径：

cURL

```json
"resources": [
  { "type": "file", "file_id": "file_abc123", "mount_path": "/data.csv" },
  { "type": "file", "file_id": "file_def456", "mount_path": "/config.json" },
  { "type": "file", "file_id": "file_ghi789", "mount_path": "/src/main.py" }
]
```

Python

```python
resources = [
    {"type": "file", "file_id": "file_abc123", "mount_path": "/data.csv"},
    {"type": "file", "file_id": "file_def456", "mount_path": "/config.json"},
    {"type": "file", "file_id": "file_ghi789", "mount_path": "/src/main.py"},
]
```

TypeScript

````typescript
resources: [
  { type: "file", file_id: "file_abc123", mount_path: "/data.csv" },
  { type: "file", file_id: "file_def456", mount_path: "/config.json" },
  { type: "file", file_id: "file_ghi789", mount_path: "/src/main.py" }
]
```csharp
```csharp
using Anthropic.Models.Beta.Sessions;

var resources = new[]
{
    new BetaManagedAgentsFileResourceParams { Type = BetaManagedAgentsFileResourceParamsType.File, FileID = "file_abc123", MountPath = "/data.csv" },
    new BetaManagedAgentsFileResourceParams { Type = BetaManagedAgentsFileResourceParamsType.File, FileID = "file_def456", MountPath = "/config.json" },
    new BetaManagedAgentsFileResourceParams { Type = BetaManagedAgentsFileResourceParamsType.File, FileID = "file_ghi789", MountPath = "/src/main.py" },
};
````

Go

```go
resources := []anthropic.BetaSessionNewParamsResourceUnion{
	{OfFile: &anthropic.BetaManagedAgentsFileResourceParams{Type: "file", FileID: "file_abc123", MountPath: anthropic.String("/data.csv")}},
	{OfFile: &anthropic.BetaManagedAgentsFileResourceParams{Type: "file", FileID: "file_def456", MountPath: anthropic.String("/config.json")}},
	{OfFile: &anthropic.BetaManagedAgentsFileResourceParams{Type: "file", FileID: "file_ghi789", MountPath: anthropic.String("/src/main.py")}},
}
```

Java

```java
import com.anthropic.models.beta.sessions.*;
import java.util.List;

var resources = List.of(
    BetaManagedAgentsFileResourceParams.builder()
        .type(BetaManagedAgentsFileResourceParams.Type.FILE).fileId("file_abc123").mountPath("/data.csv").build(),
    BetaManagedAgentsFileResourceParams.builder()
        .type(BetaManagedAgentsFileResourceParams.Type.FILE).fileId("file_def456").mountPath("/config.json").build(),
    BetaManagedAgentsFileResourceParams.builder()
        .type(BetaManagedAgentsFileResourceParams.Type.FILE).fileId("file_ghi789").mountPath("/src/main.py").build()
);
```

PHP

```php
$resources = [
    ['type' => 'file', 'fileID' => 'file_abc123', 'mountPath' => '/data.csv'],
    ['type' => 'file', 'fileID' => 'file_def456', 'mountPath' => '/config.json'],
    ['type' => 'file', 'fileID' => 'file_ghi789', 'mountPath' => '/src/main.py'],
];
```

Ruby

```ruby
resources = [
  {type: "file", file_id: "file_abc123", mount_path: "/data.csv"},
  {type: "file", file_id: "file_def456", mount_path: "/config.json"},
  {type: "file", file_id: "file_ghi789", mount_path: "/src/main.py"}
]
```

一个会话最多可以包含 500 个资源，文件、GitHub 仓库和记忆存储资源共同计数。

## 执行中补充或撤下材料

任务开始后新增材料，无需为了附加文件重新创建会话。先上传文件，再用会话资源 API 添加到现有会话，并在后续消息中说明新增路径和用途。资源响应中的 `id` 用于管理会话关联，与上传返回的 `file_id` 分别保存。

cURL

```bash
resource=$(
  jq -n --arg file_id "${file_id}" '{type: "file", file_id: $file_id}' \
    | curl --fail-with-body -sS -H "x-api-key: $AGENTAPI_API_KEY" \
        "$AGENTAPI_BASE_URL/v1/sessions/${session_id}/resources" -H "content-type: application/json" --data @-
)
resource_id=$(jq -er '.id' <<<"${resource}")
printf '%s\n' "${resource_id}"  # "sr_01ABC..."
```

Python

```python
resource = client.beta.sessions.resources.add(
    session.id,
    type="file",
    file_id=file.id,
)
print(resource.id)  # "sr_01ABC..."
```

TypeScript

````typescript
const resource = await client.beta.sessions.resources.add(session.id, {
  type: "file",
  file_id: file.id,
});
if (resource.type !== "file") {
  throw new Error(`Unexpected resource type: ${resource.type}`);
}
console.log(resource.id); // "sr_01ABC..."
```csharp
```csharp
var resource = await client.Beta.Sessions.Resources.Add(session.ID, new()
{
    Type = "file",
    FileID = file.ID,
});
Console.WriteLine(resource.ID);  // "sr_01ABC..."
````

Go

```go
resource, err := client.Beta.Sessions.Resources.Add(ctx, session.ID, anthropic.BetaSessionResourceAddParams{
	BetaManagedAgentsFileResourceParams: anthropic.BetaManagedAgentsFileResourceParams{
		Type:   anthropic.BetaManagedAgentsFileResourceParamsTypeFile,
		FileID: file.ID,
	},
})
if err != nil {
	panic(err)
}
fmt.Println(resource.ID) // "sr_01ABC..."
```

Java

```java
var resource = client.beta().sessions().resources().add(
    session.id(),
    ResourceAddParams.builder()
        .betaManagedAgentsFileResourceParams(
            BetaManagedAgentsFileResourceParams.builder()
                .type(BetaManagedAgentsFileResourceParams.Type.FILE)
                .fileId(file.id())
                .build()
        )
        .build()
);
IO.println(resource.id()); // "sr_01ABC..."
```

PHP

```php
$resource = $client->beta->sessions->resources->add(
    $session->id,
    type: 'file',
    fileID: $file->id,
);
echo "{$resource->id}\n";  // "sr_01ABC..."
```

Ruby

```ruby
resource = client.beta.sessions.resources.add(
  session.id,
  type: "file",
  file_id: file.id
)
puts resource.id # "sr_01ABC..."
```

材料不再作为会话输入时，先用 `resources.list` 查找对应资源，再以资源 ID 调用 `resources.delete`。文件对象的删除和保留规则见清理说明。

cURL

```bash
curl --fail-with-body -sS -H "x-api-key: $AGENTAPI_API_KEY" \
  "$AGENTAPI_BASE_URL/v1/sessions/${session_id}/resources" \
  | jq -r '.data[] | "\(.id) \(.type)"'

curl --fail-with-body -sS -H "x-api-key: $AGENTAPI_API_KEY" -X DELETE \
  "$AGENTAPI_BASE_URL/v1/sessions/${session_id}/resources/${resource_id}" >/dev/null
```

Python

```python
listed = client.beta.sessions.resources.list(session.id)
for entry in listed.data:
    print(entry.id, entry.type)

client.beta.sessions.resources.delete(resource.id, session_id=session.id)
```

TypeScript

````typescript
const listed = await client.beta.sessions.resources.list(session.id);
for (const entry of listed.data) {
  if (entry.type !== "memory_store") {
    console.log(entry.id, entry.type);
  }
}

await client.beta.sessions.resources.delete(resource.id, {
  session_id: session.id,
});
```csharp
```csharp
var listed = await client.Beta.Sessions.Resources.List(session.ID);
await foreach (var entry in listed.Paginate())
{
    var type = entry.Match<string>(repo => repo.Type, fileRes => fileRes.Type, memoryStore => memoryStore.Type);
    Console.WriteLine($"{entry.ID} {type}");
}

await client.Beta.Sessions.Resources.Delete(resource.ID, new() { SessionID = session.ID });
````

Go

```go
listed, err := client.Beta.Sessions.Resources.List(ctx, session.ID, anthropic.BetaSessionResourceListParams{})
if err != nil {
	panic(err)
}
for _, entry := range listed.Data {
	fmt.Println(entry.ID, entry.Type)
}

if _, err := client.Beta.Sessions.Resources.Delete(ctx, resource.ID, anthropic.BetaSessionResourceDeleteParams{
	SessionID: session.ID,
}); err != nil {
	panic(err)
}
```

Java

```java
var listed = client.beta().sessions().resources().list(session.id());
for (var entry : listed.data()) {
    if (entry.isFile()) {
        var fileResource = entry.asFile();
        IO.println(fileResource.id() + " " + fileResource.type());
    } else if (entry.isGitHubRepository()) {
        var repoResource = entry.asGitHubRepository();
        IO.println(repoResource.id() + " " + repoResource.type());
    }
}

client.beta().sessions().resources().delete(
    resource.id(),
    ResourceDeleteParams.builder().sessionId(session.id()).build()
);
```

PHP

```php
$listed = $client->beta->sessions->resources->list($session->id);
foreach ($listed->data as $entry) {
    echo "{$entry->id} {$entry->type}\n";
}

$client->beta->sessions->resources->delete($resource->id, sessionID: $session->id);
```

Ruby

```ruby
listed = client.beta.sessions.resources.list(session.id)
listed.data.each { puts "#{it.id} #{it.type}" }

client.beta.sessions.resources.delete(resource.id, session_id: session.id)
```

## 验收与下载任务产物

任务要求应同时约定交付文件，例如分析报告、处理后的数据或补丁。智能体的最终回答与文件交付是两个检查项：收到完成消息后，还应查询会话文件，确认预期产物存在并下载检查。

Portal 文件列表支持按文件名或 ID 搜索。搜索去除首尾空白，按字面量包含关系匹配，并在页码分页前计算总数；可以同时使用 `scope_id` 限定 Session。对应接口为 `GET /portal/v1/files?page_index=1&q=关键词`，`q` 必须与 `page_index` 一起使用，公开的 `GET /v1/files` 不接受该参数。

> 智能体生成的交付物
>
> 云端环境默认提供文件交付指引：供用户下载的最终文件写入 `/mnt/session/outputs/`，临时文件和项目文件保留在各自的工作路径。普通会话与 Outcome 均适用，无需客户端额外拼接目录提示。自托管 worker 不使用这条云端指引，其文件不会通过云端输出目录自动收集。
>
> AgentAPI 在执行完成后收集输出目录中的文件。文件出现在会话列表中后才能下载；若尚未出现，稍后重新查询。持续缺失时，结合[事件](../sessions-and-tasks/events.md)检查执行结果，并确认产物确实写入交付目录且未超过输出限额。

cURL

```bash
# 列出与会话关联的文件
curl --request GET "$AGENTAPI_BASE_URL/v1/files?scope_id=$SESSION_ID" \
  --header "x-api-key: $AGENTAPI_API_KEY"

# 下载文件
curl --request GET "$AGENTAPI_BASE_URL/v1/files/$FILE_ID/content" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --output output.txt
```

Python

```python
# 列出与会话关联的文件
files = client.beta.files.list(scope_id="ses_abc123")
for file in files:
    print(file.id, file.filename)

# 下载文件
content = client.beta.files.download(files.data[0].id)
content.write_to_file("output.txt")
```

TypeScript

````typescript
import { writeFile } from "node:fs/promises";

// 列出与会话关联的文件
const files = await client.beta.files.list({
  scope_id: "ses_abc123"
});
for (const file of files.data) {
  console.log(file.id, file.filename);
}

// 下载文件
const content = await client.beta.files.download(files.data[0].id);
await writeFile("output.txt", new Uint8Array(await content.arrayBuffer()));
```csharp
```csharp
// 列出与会话关联的文件
var files = await client.Beta.Files.List(new()
{
    ScopeID = "ses_abc123",
});

// 下载文件
using var content = await client.Beta.Files.Download(files.Items[0].ID);
await using var output = File.Create("output.txt");
await (await content.ReadAsStream()).CopyToAsync(output);
````

Go

```go
// 列出与会话关联的文件
files, err := client.Beta.Files.List(ctx, anthropic.BetaFileListParams{
	ScopeID: anthropic.String("ses_abc123"),
})
if err != nil {
	panic(err)
}

// 下载文件
resp, err := client.Beta.Files.Download(ctx, files.Data[0].ID)
if err != nil {
	panic(err)
}
defer resp.Body.Close()
out, err := os.Create("output.txt")
if err != nil {
	panic(err)
}
defer out.Close()
if _, err := io.Copy(out, resp.Body); err != nil {
	panic(err)
}
```

Java

```java
// 列出与会话关联的文件
var files = client.beta().files().list(FileListParams.builder()
    .scopeId("ses_abc123")
    .build());

// 下载文件
try (HttpResponse response = client.beta().files().download(files.data().get(0).id())) {
    try (InputStream body = response.body()) {
        Files.copy(body, Path.of("output.txt"), StandardCopyOption.REPLACE_EXISTING);
    }
}
```

PHP

```php
// 列出与会话关联的文件
$files = $client->beta->files->list(
    scopeID: 'ses_abc123',
);
foreach ($files->getItems() as $file) {
    echo "{$file->id} {$file->filename}\n";
}

// 下载文件
$content = $client->beta->files->download($files->getItems()[0]->id);
file_put_contents('output.txt', $content);
```

Ruby

```ruby
# 列出与会话关联的文件
files = client.beta.files.list(
  scope_id: "ses_abc123"
)

# 下载文件
content = client.beta.files.download(files.data[0].id)
File.binwrite("output.txt", content.read)
```

## 控制交付大小与清理文件

交付前控制输入和输出大小，下载时处理分页：

* 单个文件的上传上限默认为 100 MiB；会话沙箱挂载的输入文件不能超过 100 MiB。自部署服务可调整上传上限，但不会改变沙箱挂载上限。
* 执行结束后收集到 Files API 的输出文件，单文件和本轮输出目录中的文件总计均不能超过 100 MiB。任一限制被超过时，本轮输出文件收集失败，并非只跳过超限文件；请缩小产物或移出不需要下载的文件后重试。
* 文件列表采用分页响应，包含 `has_more`、`first_id`、`last_id` 和 `next_page`。查询下一页时，将 `next_page` 原样作为 `page` 参数传入；也兼容 `after_id`/`before_id`。详见 [API 通用约定](../../api/api-conventions.md)。

报告或数据下载验收后，按本页开头的对象类型决定保留范围。归档会话会保留会话文件；需要删除时分别处理源材料和会话对象：

* 删除单个对象：调用 `DELETE /v1/files/{file_id}`；会话作用域的对象还会移除对应资源关联。
* 让某个会话对用户不可见：删除会话。删除是软删除，公开 API 不再返回该会话的文件对象，服务侧仍保留；目前没有按会话批量删除文件对象的专用接口。向已删除会话的 `scope_id` 继续上传会返回 404，不会写入新的文件对象。
* 独立上传文件需单独删除；删除源文件不会清理已挂载的副本。
