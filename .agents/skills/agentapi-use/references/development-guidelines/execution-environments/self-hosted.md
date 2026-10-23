<!-- 来源：https://developer-doc.qiniu.com/products/agentapi/development_guidelines/agentapi-execution-environments/13614-agentapi-execution-environments-self-hosted | 由 sync-docs.py 自动生成，请勿手改 -->

# 自托管执行器



最近更新时间: 2026-10-13 15:32:12

自托管环境（`self_hosted`）把文件和命令工具放到自有基础设施执行。AgentAPI 仍负责模型请求、会话状态、事件和工作队列；worker 领取工作项，在本地完成工具调用，再把结果回传给 AgentAPI。

接入前需明确配置与数据边界：

1. 会话使用 `runtime: "native"` 的[智能体](../agent-configuration/agent.md)，模型仍在智能体配置中选择；自托管只决定工具执行位置。
2. 用户输入、工具参数和工具结果仍会经过 AgentAPI 和模型服务。技能与记忆存储由 AgentAPI 保存，支持这些能力的 worker 会下载副本并按规则同步。网页工具和服务端 MCP 连接由 AgentAPI 执行，不会自动转移到 worker。

完整的数据流、凭证范围和网络边界见[自托管安全责任](self-hosted-security.md)。

## 确定执行边界

|                 | 云端环境                                            | 自托管环境                      |
| --------------- | ----------------------------------------------- | -------------------------- |
| 文件和命令执行位置       | 七牛云端沙箱                                          | 自有基础设施                     |
| 执行环境网络出口        | 云端环境的网络策略                                       | 自有网络策略                     |
| 文件和 GitHub 仓库挂载 | 由 AgentAPI 管理                                   | 自行管理                       |
| 记忆存储            | `claudecode`、`codex` 挂载并在成功执行后写回；`native` 不自动挂载 | 由支持记忆的 SDK worker 下载、挂载和同步 |
| 生命周期            | 由 AgentAPI 管理                                   | 自行管理                       |

自托管适合需要访问内网服务、复用既有执行基础设施，或由应用控制隔离和审计的任务。内网访问应封装为 worker 中的自定义工具；直接配置的远程 MCP 服务仍必须能被 AgentAPI 服务端访问。

| 需求                | 选择                             |
| ----------------- | ------------------------------ |
| 只需要云端执行文件和命令      | 使用云端环境，无需运行 worker             |
| 需要固定主机、私有网络或自有审计  | 使用 `self_hosted`，由 worker 执行工具 |
| 每个会话需要独立文件系统或资源配额 | 由轮询器为每个工作项创建独立沙箱               |
| 多个会话共享一组执行资源      | 使用常驻 worker，按自身调度策略复用主机        |

最小验证路径是：创建 `self_hosted` 环境，启动一个 worker，创建引用该环境的 `native` 会话，发送一条消息，然后从队列统计和会话事件确认工具结果。后文的命令和代码都围绕这条路径展开。

## 配置最小接入条件

> SDK 版本
>
> 本页的 worker 同步行为和运行依赖已核验于 Python SDK 1.12.1、TypeScript SDK 0.132.1、Go SDK 1.79.1 和 `ant` CLI 1.39.1。其他版本的默认值可能不同，升级时需核对所用版本。

先准备一个 `native` 智能体和一台执行主机。暂不使用记忆存储时，无需配置记忆目录：

* 一个使用 `native` 的智能体。 按[创建智能体](../agent-configuration/agent.md)的示例创建，并将 `runtime` 改为 `native`，保存返回的 ID 和版本。
* 一台 Linux 执行主机，必须提供 `/bin/bash`，worker 不通过 `PATH` 查找 bash。此外，TypeScript SDK 还要求 `PATH` 上有 `unzip` 和 `tar`，以及 Node.js 22 或更高版本；Python 和 Go SDK 用各自标准库解压归档，因此没有这些额外的二进制依赖。
* worker 主机上的 `ant` CLI，或支持 AgentAPI 协议的 SDK（Python、TypeScript 或 Go）。

接入中的三类凭证分别服务于账户操作、领取工作和读取会话资源，不应混用：

| 凭证                | 获取方式                | 使用位置                                                                                                                                                   |
| ----------------- | ------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 已绑定的 MaaS API Key | Portal 的 API Key 设置 | worker 主机之外的应用，用于创建会话、读取队列统计                                                                                                                           |
| 环境密钥（Worker key）  | Portal 的环境详情页       | worker，用于访问指定环境的工作队列                                                                                                                                   |
| 会话级 `secret`      | 服务端生成，随已认领工作项下发     | 处理该工作项的 worker，用于访问[记忆存储](self-hosted.md) |

若会话需要记忆存储，启动前还须准备可写的 `/mnt/memory`，见[准备主机](self-hosted.md)。采用独立沙箱时，启动器必须把当前工作项的 `secret` 转发给对应实例。

### 创建自托管环境

请先完成[接入准备](../../quick-start/access.md)。在 [七牛 Portal](https://portal.qiniu.com/agentapi) 的环境页面创建自托管环境，或使用下面的 API。账户级资源操作使用已绑定的 MaaS API Key，客户端初始化见[通过 API 构建](../../quick-start/quickstart.md)。

cURL

```bash
curl -sS --fail-with-body $AGENTAPI_BASE_URL/v1/environments \
  -H "x-api-key: $AGENTAPI_API_KEY" \
  -H "content-type: application/json" \
  -d '{
    "name": "self-hosted",
    "config": {"type": "self_hosted"}
  }'
```

Python

```python
import os
client = anthropic.Anthropic(api_key=os.environ["AGENTAPI_API_KEY"], base_url=os.environ["AGENTAPI_BASE_URL"])

environment = client.beta.environments.create(
    name="self-hosted", config={"type": "self_hosted"}
)
print(environment.id)
```

TypeScript

````typescript
const client = new Anthropic({ apiKey: process.env.AGENTAPI_API_KEY!, baseURL: process.env.AGENTAPI_BASE_URL! });

const environment = await client.beta.environments.create({
  name: "self-hosted",
  config: { type: "self_hosted" }
});
console.log(environment.id);
```csharp
```csharp
using Anthropic.Models.Beta.Environments;

var client = new AnthropicClient(new Anthropic.Core.ClientOptions
{
    ApiKey = System.Environment.GetEnvironmentVariable("AGENTAPI_API_KEY")!,
    BaseUrl = System.Environment.GetEnvironmentVariable("AGENTAPI_BASE_URL")!,
});

var environment = await client.Beta.Environments.Create(
    new EnvironmentCreateParams
    {
        Name = "self-hosted",
        Config = new BetaSelfHostedConfigParams(),
    }
);
Console.WriteLine(environment.ID);
````

Go

```go
client := anthropic.NewClient(
	option.WithoutEnvironmentDefaults(),
	option.WithBaseURL(os.Getenv("AGENTAPI_BASE_URL")),
	option.WithAPIKey(os.Getenv("AGENTAPI_API_KEY")),
)

environment, err := client.Beta.Environments.New(context.Background(), anthropic.BetaEnvironmentNewParams{
	Name: "self-hosted",
	Config: anthropic.BetaEnvironmentNewParamsConfigUnion{
		OfSelfHosted: &anthropic.BetaSelfHostedConfigParams{},
	},
})
if err != nil {
	panic(err)
}
fmt.Println(environment.ID)
```

Java

```java
import com.anthropic.models.beta.environments.BetaSelfHostedConfigParams;
import com.anthropic.models.beta.environments.EnvironmentCreateParams;

void main() {
    var client = AnthropicOkHttpClient.builder()
        .apiKey(System.getenv("AGENTAPI_API_KEY"))
        .baseUrl(System.getenv("AGENTAPI_BASE_URL"))
        .build();

    var environment = client.beta().environments().create(
        EnvironmentCreateParams.builder()
            .name("self-hosted")
            .config(BetaSelfHostedConfigParams.builder().build())
            .build()
    );
    IO.println(environment.id());
}
```

PHP

```php
$client = new Anthropic\Client(apiKey: getenv('AGENTAPI_API_KEY'), baseUrl: getenv('AGENTAPI_BASE_URL'));

$environment = $client->beta->environments->create(
    name: 'self-hosted',
    config: ['type' => 'self_hosted'],
);
echo $environment->id, PHP_EOL;
```

Ruby

```ruby
client = Anthropic::Client.new(api_key: ENV.fetch("AGENTAPI_API_KEY"), base_url: ENV.fetch("AGENTAPI_BASE_URL"))

environment = client.beta.environments.create(
  name: "self-hosted",
  config: {type: :self_hosted}
)
puts environment.id
```

### 创建环境密钥

在 [七牛 Portal](https://portal.qiniu.com/agentapi) 打开对应自托管环境的详情页，创建连接凭证。一个环境可以同时使用多个凭证，以区分不同 worker；撤销其中一个凭证不会影响其他凭证。密钥明文仅在创建成功后展示一次，之后无法再次获取，请及时保存到 worker 主机。

创建完成后，在 worker 主机上设置环境 ID、环境密钥和服务地址。以下 `ANTHROPIC_*` 名称来自兼容 CLI / SDK 的变量约定，值使用 AgentAPI 的对应配置。`AGENTAPI_BASE_URL` 应与创建环境时的区域一致：

```bash
export ANTHROPIC_ENVIRONMENT_KEY="$worker_key"
export ANTHROPIC_ENVIRONMENT_ID="$AGENTAPI_ENVIRONMENT_ID"
export ANTHROPIC_BASE_URL="$AGENTAPI_BASE_URL"
```

> 说明
>
> 技能包可以包含供智能体直接运行的可执行文件。CLI 和 SDK worker 在解压技能包时，会保留包内记录的可执行权限。自行实现技能下载时，需要保留或设置这些可执行权限。

## 启动验证用 worker

首次接入可先让一个进程持续轮询环境队列，通过出站 HTTPS 连接 AgentAPI。只需验证文件和命令工具时可使用 CLI；需要私有工具或记忆同步时使用 SDK worker。验证通过后，再按执行基础设施中的方案配置隔离与回收。

当前服务提供轮询接入；需要按事件唤醒 worker 时，由应用侧启动实例并触发轮询。

### 使用 ant CLI

#### 安装 CLI

在 worker 主机上运行此命令。

##### Linux / WSL 安装

对于 Linux 环境，直接下载发布的二进制文件。

```bash
VERSION=1.39.1
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case $(uname -m) in
  x86_64) ARCH=amd64 ;;
  aarch64) ARCH=arm64 ;;
esac
curl -fsSL "https://github.com/anthropics/anthropic-cli/releases/download/v${VERSION}/ant_${VERSION}_${OS}_${ARCH}.tar.gz" \
  | sudo tar -xz -C /usr/local/bin ant
```

可以在 [GitHub 发布页面](https://github.com/anthropics/anthropic-cli/releases)上找到所有版本。

##### macOS 安装

以下命令用于安装 CLI；worker 执行主机仍需满足本页的 Linux 和文件系统要求。

```bash
brew install anthropics/tap/ant
```

#### 启动 CLI worker

`ant beta:worker poll` 从环境变量读取 `ANTHROPIC_ENVIRONMENT_KEY` 和 `ANTHROPIC_ENVIRONMENT_ID`，持续领取该环境中的工作。在这个验证方案中，领取和执行发生在同一进程；技能下载及工具文件操作都使用指定工作目录。

```bash
ant beta:worker poll --workdir "/workspace"
```

worker 收到 SIGTERM 或 SIGINT 时会取消调用并释放工作项：它会取消所有进行中的工具调用、发布对应的错误结果，并在停止前释放工作项。

### 使用 SDK worker

SDK 的 `EnvironmentWorker` 封装领取、技能下载、工具执行和结果回传。下面的示例以常驻进程运行，并把退出信号转换为取消操作，使 worker 有机会完成上传和清理；鉴权使用[环境密钥](self-hosted.md)。

Python

```python
import asyncio
import contextlib
import os
import signal
from anthropic import AsyncAnthropic
from anthropic.lib.environments import EnvironmentWorker

async def main() -> None:
    environment_key = os.environ["ANTHROPIC_ENVIRONMENT_KEY"]
    environment_id = os.environ["ANTHROPIC_ENVIRONMENT_ID"]
    async with AsyncAnthropic(auth_token=environment_key, base_url=os.environ["ANTHROPIC_BASE_URL"]) as client:
        worker = EnvironmentWorker(
            client,
            environment_id=environment_id,
            environment_key=environment_key,
            workdir="/workspace",
        )
        task = asyncio.create_task(worker.run())
        # 取消任务（而非终止进程）可让 worker 停止其
        # 正在处理的工作项，并在退出前上传已更改的内存文件。
        loop = asyncio.get_running_loop()
        for signum in (signal.SIGINT, signal.SIGTERM):
            loop.add_signal_handler(signum, task.cancel)
        with contextlib.suppress(asyncio.CancelledError):
            await task

asyncio.run(main())
```

TypeScript

````typescript
import Anthropic from "@anthropic-ai/sdk";
import { EnvironmentWorker } from "@anthropic-ai/sdk/helpers/beta/environments";

const environmentKey = process.env.ANTHROPIC_ENVIRONMENT_KEY!;
const environmentId = process.env.ANTHROPIC_ENVIRONMENT_ID!;
const client = new Anthropic({ authToken: environmentKey, baseURL: process.env.ANTHROPIC_BASE_URL! });
const controller = new AbortController();
// 在任一信号上中止可让 worker 上传已更改的内存文件并移除其
// 存储目录，然后进程退出。
process.once("SIGINT", () => controller.abort());
process.once("SIGTERM", () => controller.abort());

await new EnvironmentWorker({
  client,
  environmentId,
  environmentKey,
  workdir: "/workspace",
  signal: controller.signal
}).run();
```csharp
```csharp
// EnvironmentWorker 目前在 C# SDK 中不可用。请参阅 本页的 ant CLI 方式。
````

Go

```go
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/lib/environments"
	"github.com/anthropics/anthropic-sdk-go/option"
)

func main() {
	environmentKey := os.Getenv("ANTHROPIC_ENVIRONMENT_KEY")
	environmentID := os.Getenv("ANTHROPIC_ENVIRONMENT_ID")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := anthropic.NewClient(option.WithAuthToken(environmentKey), option.WithBaseURL(os.Getenv("ANTHROPIC_BASE_URL")))

	worker := environments.NewEnvironmentWorker(client, environments.EnvironmentWorkerOptions{
		EnvironmentID:  environmentID,
		EnvironmentKey: environmentKey,
		Workdir:        "/workspace",
	})
	if err := worker.Run(ctx); err != nil {
		log.Fatalf("worker: %v", err)
	}
}

```

Java

```java
// Java SDK 目前不支持 EnvironmentWorker。请参阅 本页的 ant CLI 方式。
```

PHP

```php
// PHP SDK 目前不提供 EnvironmentWorker。请参阅 本页的 ant CLI 方式。
```

Ruby

```ruby
# Ruby SDK 目前不支持 EnvironmentWorker。请参阅 本页的 ant CLI 方式。
```

## 发送任务并核对执行结果

使用已绑定的 MaaS API Key 创建会话，并引用刚创建的环境。`AGENT_ID` 必须来自 `runtime: "native"` 的智能体，不能复用快速开始中的 `claudecode` 智能体。

以下示例把输入文件引用写入 `metadata`。它只向启动器传递信息，不会自动下载文件；首次验证可以省略这项元数据，发送不依赖外部文件的任务。文件、仓库和产物的准备方式见任务工作目录与产物。

cURL

```bash
curl -sS --fail-with-body $AGENTAPI_BASE_URL/v1/sessions \
  -H "x-api-key: $AGENTAPI_API_KEY" \
  -H "content-type: application/json" \
  -d @- <<EOF
{
  "agent": "$AGENT_ID",
  "environment_id": "$ANTHROPIC_ENVIRONMENT_ID",
  "metadata": {"input_file": "s3://my-bucket/data.csv"}
}
EOF
```

Python

```python
session = client.beta.sessions.create(
    agent=agent.id,
    environment_id=environment.id,
    metadata={"input_file": "s3://my-bucket/data.csv"},
)
```

TypeScript

````typescript
const session = await client.beta.sessions.create({
  agent: agent.id,
  environment_id: environment.id,
  metadata: { input_file: "s3://my-bucket/data.csv" }
});
```csharp
```csharp
var session = await client.Beta.Sessions.Create(new()
{
    Agent = agent.ID,
    EnvironmentID = environment.ID,
    Metadata = new Dictionary<string, string> { ["input_file"] = "s3://my-bucket/data.csv" },
});
````

Go

```go
session, err := client.Beta.Sessions.New(ctx, anthropic.BetaSessionNewParams{
	Agent:         anthropic.BetaSessionNewParamsAgentUnion{OfString: anthropic.String(agent.ID)},
	EnvironmentID: environment.ID,
	Metadata: map[string]string{
		"input_file": "s3://my-bucket/data.csv",
	},
})
if err != nil {
	panic(err)
}
```

Java

```java
var session = client.beta().sessions().create(SessionCreateParams.builder()
    .agent(agent.id())
    .environmentId(environment.id())
    .metadata(SessionCreateParams.Metadata.builder()
        .putAdditionalProperty("input_file", JsonValue.from("s3://my-bucket/data.csv"))
        .build())
    .build());
```

PHP

```php
$session = $client->beta->sessions->create(
    agent: $agent->id,
    environmentID: $environment->id,
    metadata: ['input_file' => 's3://my-bucket/data.csv'],
);
```

Ruby

```ruby
session = client.beta.sessions.create(
  agent: agent.id,
  environment_id: environment.id,
  metadata: {input_file: "s3://my-bucket/data.csv"}
)
```

> 说明
>
> 自托管会话接受 `file`、`github_repository` 和 `memory_store` 三种资源。AgentAPI 保存资源声明，worker 或启动器负责下载文件、准备仓库，并在自己的执行环境中挂载资源。记忆的下载、挂载和同步需要支持该能力的 SDK worker，见使用记忆存储。上文的 `metadata` 用于传递由自有启动器解析的资源引用，不会触发 AgentAPI 自动下载。
>
> 以自托管环境为目标的[部署](../sessions-and-tasks/scheduled-deployments.md)遵循相同的规则。

确认智能体已配置任务所需的工具，再按[发送用户消息](../sessions-and-tasks/session.md)启动任务。例如，启用文件工具后，请求在工作目录中创建一个文本文件并读取其内容。核对三处结果：

1. 队列统计中存在近期轮询的 worker；若工作积压，检查领取流程。
2. 会话事件中出现工具调用及其结果，而非只停留在已创建会话的状态。
3. worker 的工作目录中存在预期文件；若需要保留产物，由应用另行收集。

创建会话只保存配置，发送消息才会产生工作项。没有 worker 连接时，工作会等待处理。

### 核对队列连接

打开新的终端，将 `ANTHROPIC_API_KEY` 设置为已绑定的 MaaS API Key。该检查使用账户级 API Key。确认 `workers_polling` 至少为 1：

```bash
ant beta:environments:work stats --environment-id "$ANTHROPIC_ENVIRONMENT_ID"
```

如果 `workers_polling` 持续为 0，说明 worker 尚未连接队列。请检查 worker 主机是否已设置 `ANTHROPIC_ENVIRONMENT_KEY` 和 `ANTHROPIC_ENVIRONMENT_ID`。完整的统计响应字段和其他语言示例见[读取队列深度](self-hosted.md)。

## 从验证进程转为部署实例

验证通过后，先决定工作目录是否需要跨任务复用。常驻模式把执行过程留在同一主机，由应用负责目录、配额和清理；独立沙箱模式把领取与执行分开，由外部轮询器或启动器管理每个工作项的实例。

可以使用[七牛 LAS Sandbox](https://developer-doc.qiniu.com/products/las/13479-sandbox-api-overview) 为每个会话创建短生命周期的容器，也可以使用自有容器或虚拟机。LAS 负责沙箱实例、模板、进程、终端和文件系统；AgentAPI 负责智能体、会话、事件和工作队列。

使用独立实例时，启动器需要完成以下流程：

1. 从 AgentAPI 工作队列领取工作项。
2. 调用 LAS Sandbox API 创建或复用执行实例。
3. 准备 `/workspace`，将会话资源和必要的 `secret` 提供给执行实例。
4. 在实例中运行 worker，回传工具结果。
5. 工作结束后停止并删除沙箱，或按资源策略回收。

LAS Sandbox API 使用区域服务地址和七牛 `X-API-Key`。请根据沙箱所在区域选择服务地址，并参考[沙箱概述](https://developer-doc.qiniu.com/products/las/13479-sandbox-api-overview)获取最新区域、认证和实例管理接口；请勿将 LAS API Key 写入会话的 `metadata` 或 worker 镜像。

### CLI 隔离执行实例

需要独立文件系统、资源配额或网络控制时，请为每个会话创建单独的沙箱。下面的镜像安装 `ant`，以 `ant beta:worker run` 为入口；基础镜像必须提供 `/bin/bash`，构建阶段还需要 `curl`。执行实例从环境变量读取会话信息，处理对应工作后退出：

```text
FROM your-base-image
ARG ANT_VERSION=1.39.1
ARG TARGETARCH
RUN ARCH=$([ "$TARGETARCH" = "arm64" ] && echo arm64 || echo amd64) && \
    curl -fsSL "https://github.com/anthropics/anthropic-cli/releases/download/v${ANT_VERSION}/ant_${ANT_VERSION}_linux_${ARCH}.tar.gz" \
      | tar -xz -C /usr/local/bin ant
WORKDIR /workspace
VOLUME /workspace
ENTRYPOINT ["ant", "beta:worker", "run"]
```

随后编写启动脚本（spawn script），将会话详情转发到新建沙箱。轮询器通过以下方式向脚本提供信息：

* 通过环境变量注入 `ANTHROPIC_SESSION_ID`、`ANTHROPIC_WORK_ID`、`ANTHROPIC_ENVIRONMENT_ID` 和 `ANTHROPIC_ENVIRONMENT_KEY`。
* 把已认领的工作项以 JSON 形式写入脚本的标准输入，其中包含工作项的每会话 `secret`（前提是 AgentAPI 签发了它）。
* `ANTHROPIC_BASE_URL` 由轮询器传给执行实例。接入 AgentAPI 时必须按上文设置它，确保 worker 连接创建环境所在的服务区域。

示例将主机目录 `/host/outputs` 绑定到沙箱工作目录 `/workspace`，以保留退出后的产物、下载的 `skills/` 及中间文件。自托管不自动收集 `/mnt/session/outputs`，产物应保存在工作目录中，见[沙箱文件系统](self-hosted.md)。

```bash
#!/bin/bash
# spawn.sh：每个已认领的工作项调用一次
mkdir -p "/host/outputs/$ANTHROPIC_SESSION_ID"
exec docker run --rm \
  -e ANTHROPIC_SESSION_ID -e ANTHROPIC_ENVIRONMENT_KEY \
  -e ANTHROPIC_WORK_ID -e ANTHROPIC_ENVIRONMENT_ID -e ANTHROPIC_BASE_URL \
  -v "/host/outputs/$ANTHROPIC_SESSION_ID":/workspace \
  your-image
```

`ant beta:worker run` 入口点不会挂载[记忆存储](self-hosted.md)。如果环境中的会话附加了记忆存储，请保留轮询器，将会话镜像改为使用 SDK worker，并在启动脚本中将工作项的 `secret` 转发到沙箱。具体方法见[每个会话运行一个沙箱](self-hosted.md)。

启动指向该脚本的轮询器：

```bash
ant beta:worker poll --on-work ./spawn.sh
```

### 每个会话运行一个沙箱

当任务需要自定义工具或记忆挂载时，执行实例改用 SDK worker。若多个会话引用同一个记忆存储，独立文件系统还能避免挂载路径冲突，见[准备主机](self-hosted.md)。实例之外仍由 `ant beta:worker poll --on-work` 或 SDK 工作轮询器领取工作，见[选择并启动 worker](self-hosted.md)。

`ant beta:worker run` 不支持记忆挂载。请使用 SDK worker 构建会话镜像，并在入口点中：

1. 创建 `EnvironmentWorker`，调用 `handle_item()`（TypeScript 为 `handleItem`，Go 为 `HandleItem`）。
2. 通过 `ANTHROPIC_*` 变量提供会话、工作和环境标识符。
3. 将工作项的会话级 `secret` 写入 `ANTHROPIC_WORK_SECRET`；也可显式传入 `work_secret`（TypeScript 为 `workSecret`，Go 为 `WorkSecret`）。

Python

```python
import asyncio
import contextlib
import os
import signal
from anthropic import AsyncAnthropic
from anthropic.lib.environments import EnvironmentWorker

async def main() -> None:
    async with AsyncAnthropic(auth_token=os.environ["ANTHROPIC_ENVIRONMENT_KEY"], base_url=os.environ["ANTHROPIC_BASE_URL"]) as client:
        worker = EnvironmentWorker(client, workdir="/workspace")
        # 不带参数时，handle_item() 会读取 spawn 脚本转发的 ANTHROPIC_* 变量，
        # 包括 ANTHROPIC_WORK_SECRET。
        task = asyncio.create_task(worker.handle_item())
        # 在容器停止时取消任务，可让工作进程在退出前上传
        # 已更改的内存文件并移除存储目录。
        loop = asyncio.get_running_loop()
        for signum in (signal.SIGINT, signal.SIGTERM):
            loop.add_signal_handler(signum, task.cancel)
        with contextlib.suppress(asyncio.CancelledError):
            await task

asyncio.run(main())
```

TypeScript

````typescript
import Anthropic from "@anthropic-ai/sdk";
import { EnvironmentWorker } from "@anthropic-ai/sdk/helpers/beta/environments";

const client = new Anthropic({ authToken: process.env.ANTHROPIC_ENVIRONMENT_KEY, baseURL: process.env.ANTHROPIC_BASE_URL! });
const controller = new AbortController();
// 在容器停止时中止，可让 worker 在退出前上传已更改的内存
// 文件并移除存储目录。
process.once("SIGTERM", () => controller.abort());
process.once("SIGINT", () => controller.abort());

// 不带参数时，handleItem() 会读取 spawn 脚本转发的 ANTHROPIC_* 变量，
// 包括 ANTHROPIC_WORK_SECRET。
await new EnvironmentWorker({
  client,
  workdir: "/workspace",
  signal: controller.signal
}).handleItem();
```csharp
```csharp
// EnvironmentWorker 目前在 C# SDK 中不可用。
````

Go

```go
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/lib/environments"
	"github.com/anthropics/anthropic-sdk-go/option"
)

func main() {
	// 在容器停止时取消 context，可让 worker 在退出前上传
	// 已更改的内存文件并移除存储目录。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := anthropic.NewClient(option.WithAuthToken(os.Getenv("ANTHROPIC_ENVIRONMENT_KEY")), option.WithBaseURL(os.Getenv("ANTHROPIC_BASE_URL")))
	worker := environments.NewEnvironmentWorker(client, environments.EnvironmentWorkerOptions{
		Workdir: "/workspace",
	})
	// 使用零值选项时，HandleItem 会读取 spawn 脚本转发的 ANTHROPIC_* 变量，
	// 包括 ANTHROPIC_WORK_SECRET。
	if err := worker.HandleItem(ctx, environments.HandleItemOptions{}); err != nil {
		log.Fatalf("worker: %v", err)
	}
}

```

Java

```java
// Java SDK 目前暂不提供 EnvironmentWorker。
```

PHP

```php
// PHP SDK 目前暂不支持 EnvironmentWorker。
```

Ruby

```ruby
# Ruby SDK 目前暂不支持 EnvironmentWorker。
```

`ant beta:worker poll --on-work` 不会根据工作项自动设置 `ANTHROPIC_WORK_SECRET`；派生脚本仍会继承父进程的环境变量。启动脚本需要从标准输入中的当前工作项 JSON 读取会话凭证，再传入沙箱：

```bash
#!/bin/bash
# spawn.sh：每个已领取的工作项调用一次
# 已领取的工作项以 JSON 形式通过 stdin 传入。其 secret 是
# 记忆存储端点所需的每会话凭据。
ANTHROPIC_WORK_SECRET="$(jq -r '.secret // empty')"
export ANTHROPIC_WORK_SECRET
mkdir -p "/host/outputs/$ANTHROPIC_SESSION_ID"
exec docker run --rm \
  -e ANTHROPIC_SESSION_ID -e ANTHROPIC_ENVIRONMENT_KEY \
  -e ANTHROPIC_WORK_ID -e ANTHROPIC_ENVIRONMENT_ID -e ANTHROPIC_BASE_URL \
  -e ANTHROPIC_WORK_SECRET \
  -v "/host/outputs/$ANTHROPIC_SESSION_ID":/workspace \
  your-sdk-worker-image
```

使用 SDK 工作轮询器领取工作时，也需要将每个工作项的 `secret` 传入对应沙箱。会话凭证仅应提供给执行该会话的沙箱，禁止写入日志。

沙箱镜像还需要可写的 `/mnt/memory`，配置要求见[准备主机](self-hosted.md)。每个沙箱仅服务一个会话，使用后即回收，因此不需要清理上次会话遗留的目录，也不需要将记忆目录绑定挂载到主机。worker 会在沙箱退出前将目录内容上传到存储。停止容器时需要满足以下条件：

* 在会话结束前停止容器时，请发送入口点能够转换为取消操作的信号，使 worker 完成上传。配置方法见[准备主机](self-hosted.md)。
* 为容器预留足够的上传时间。通过 `docker run` 的 `--stop-timeout` 或编排器的终止宽限期，预留至少 60 秒，并留出本地文件清理时间。

## 任务入队与执行流程

> 说明
>
> 无论采用哪种执行基础设施，worker 都需通过 AgentAPI 工作队列领取任务、维持租约并回传结果。使用 CLI 或 SDK worker 时，这些操作由客户端实现；使用自定义执行器时，需要自行实现。

向自托管环境中的[会话](../sessions-and-tasks/session.md)发送用户消息后，AgentAPI 将一个工作项加入环境队列。worker 领取工作项，准备工作目录和[技能](../agent-configuration/skills.md)，执行工具并回传结果。没有可用 worker 时，工作项会留在队列中，不会自动改用云端环境。

worker 通过轮询领取工作项。可以持续运行 worker，也可以在应用收到任务事件后启动执行实例，再触发轮询。

![自托管执行器数据流：AgentAPI 管理工作队列，worker 在自有基础设施上领取工作、执行工具并回传结果与事件](https://developer-doc.qiniu.com/docs-assets/products/agentapi/development_guidelines/agentapi-execution-environments/13614-agentapi-execution-environments-self-hosted/FmsxHIiRufs-sCEKzRCS9vMZQIv3-53f1b10ae124.webp)

CLI 和 SDK 均提供 worker 实现。CLI 参数可通过 `ant beta:worker poll --help` 查看，SDK 用法见辅助工具参考。需要自行实现执行器时，请按 [OpenAPI 参考](../../api/api-conventions.md)中的环境工作队列接口实现领取、确认、保活和结果回传。

## 私有工具与 MCP 边界

AgentAPI 的 MCP 连接由服务端执行，不会进入 worker 的待执行队列。只有需要使用 worker 网络或本地凭证的能力，才应实现为自定义工具，并由 worker 回传 `user.custom_tool_result`。自定义工具的声明和注册见从沙箱提供自定义工具。

## 接入私有业务工具

例如，任务需要查询内网订单系统时，可把查询封装为[自定义工具](../agent-configuration/tools.md)。工具声明供模型选择调用，返回结果进入模型上下文；实际请求在 worker 的网络中发起，工具代码负责业务鉴权、参数校验和超时。

接入分成两部分：在智能体上声明可调用的接口，在 worker 中注册同名实现。智能体发出 `agent.custom_tool_use` 后，worker 执行实现并回传 `user.custom_tool_result`。回传使用环境密钥，无需将账户级 MaaS API Key 放入执行实例。

> 说明
>
> `ant` CLI worker 无法注册自定义工具实现。此处使用 SDK worker；独立沙箱的入口应调用 `EnvironmentWorker` 的 `handle_item()`（TypeScript 为 `handleItem`，Go 为 `HandleItem`），替代 `ant beta:worker run`。

### 在智能体上声明工具

以下声明让模型按订单 ID 调用查询接口。把这个 `custom` 条目加入智能体的 `tools`，并确保 `name` 与 worker 注册名称一致。声明结构见[自定义工具](../agent-configuration/tools.md)。

```json
{
  "type": "custom",
  "name": "get_order_status",
  "description": "Look up an order in the internal fulfillment system by order ID.",
  "input_schema": {
    "type": "object",
    "properties": {
      "order_id": { "type": "string", "description": "The order ID" }
    },
    "required": ["order_id"]
  }
}
```

### 向 worker 注册实现

worker 的 `tools` 工厂返回本次执行使用的完整工具列表。下面的实现保留内置工具，并加入订单查询工具；工厂用法见[SDK 辅助工具](self-hosted.md)：

Python

```python
import asyncio
import os
from anthropic import AsyncAnthropic, beta_async_tool
from anthropic.lib.environments import EnvironmentWorker
from anthropic.lib.tools.agent_toolset import beta_agent_toolset_20260401

@beta_async_tool
async def get_order_status(order_id: str) -> str:
    """Look up an order in the internal fulfillment system by order ID."""
    # 在 worker 主机上运行：可调用沙箱能够访问的任何内容。
    return f"Order {order_id}: shipped"

async def main() -> None:
    environment_key = os.environ["ANTHROPIC_ENVIRONMENT_KEY"]
    environment_id = os.environ["ANTHROPIC_ENVIRONMENT_ID"]
    async with AsyncAnthropic(auth_token=environment_key, base_url=os.environ["ANTHROPIC_BASE_URL"]) as client:
        await EnvironmentWorker(
            client,
            environment_id=environment_id,
            environment_key=environment_key,
            workdir="/workspace",
            tools=lambda env: [*beta_agent_toolset_20260401(env), get_order_status],
        ).run()

asyncio.run(main())
```

TypeScript

````typescript
import Anthropic from "@anthropic-ai/sdk";
import { EnvironmentWorker } from "@anthropic-ai/sdk/helpers/beta/environments";
import { betaTool } from "@anthropic-ai/sdk/helpers/beta/json-schema";
import { betaAgentToolset20260401 } from "@anthropic-ai/sdk/tools/agent-toolset/node";

const getOrderStatus = betaTool({
  name: "get_order_status",
  description: "Look up an order in the internal fulfillment system by order ID.",
  inputSchema: {
    type: "object",
    properties: { order_id: { type: "string", description: "The order ID" } },
    required: ["order_id"]
  },
  // 在 worker 主机上运行：可调用沙箱能访问的任何内容。
  run: async ({ order_id }) => `Order ${order_id}: shipped`
});

const environmentKey = process.env.ANTHROPIC_ENVIRONMENT_KEY!;
const environmentId = process.env.ANTHROPIC_ENVIRONMENT_ID!;
const client = new Anthropic({ authToken: environmentKey, baseURL: process.env.ANTHROPIC_BASE_URL! });
const controller = new AbortController();
process.once("SIGTERM", () => controller.abort());

await new EnvironmentWorker({
  client,
  environmentId,
  environmentKey,
  workdir: "/workspace",
  signal: controller.signal,
  tools: (ctx) => [...betaAgentToolset20260401(ctx), getOrderStatus]
}).run();
```csharp
```csharp
// EnvironmentWorker 目前在 C# SDK 中不可用。
// 如需直接响应自定义工具调用，请参阅会话事件流。
````

Go

```go
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/lib/environments"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/toolrunner"
	"github.com/anthropics/anthropic-sdk-go/tools/agenttoolset"
)

type orderStatusInput struct {
	OrderID string `json:"order_id"`
}

func main() {
	environmentKey := os.Getenv("ANTHROPIC_ENVIRONMENT_KEY")
	environmentID := os.Getenv("ANTHROPIC_ENVIRONMENT_ID")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	getOrderStatus := toolrunner.NewBetaTool(
		"get_order_status",
		"Look up an order in the internal fulfillment system by order ID.",
		anthropic.BetaToolInputSchemaParam{
			Properties: map[string]any{
				"order_id": map[string]any{"type": "string", "description": "The order ID"},
			},
			Required: []string{"order_id"},
		},
		// 在工作进程主机上运行：可调用沙箱能够访问的任何内容。
		func(ctx context.Context, input orderStatusInput) (anthropic.BetaToolResultBlockParamContentUnion, error) {
			return anthropic.BetaToolResultBlockParamContentUnion{
				OfText: &anthropic.BetaTextBlockParam{Text: "Order " + input.OrderID + ": shipped"},
			}, nil
		},
	)

	client := anthropic.NewClient(option.WithAuthToken(environmentKey), option.WithBaseURL(os.Getenv("ANTHROPIC_BASE_URL")))

	worker := environments.NewEnvironmentWorker(client, environments.EnvironmentWorkerOptions{
		EnvironmentID:  environmentID,
		EnvironmentKey: environmentKey,
		Workdir:        "/workspace",
		ToolsFunc: func(env *agenttoolset.AgentToolContext) []anthropic.BetaTool {
			return append(agenttoolset.BetaAgentToolset20260401(env), getOrderStatus)
		},
	})
	if err := worker.Run(ctx); err != nil {
		log.Fatalf("worker: %v", err)
	}
}

```

Java

```java
// EnvironmentWorker 目前在 Java SDK 中不可用。
// 如需直接响应自定义工具调用，请参阅会话事件流。
```

PHP

```php
// PHP SDK 目前暂不提供 EnvironmentWorker。
// 如需直接响应自定义工具调用，请参阅会话事件流。
```

Ruby

```ruby
# Ruby SDK 目前暂不提供 EnvironmentWorker。
# 如需直接响应自定义工具调用，请参阅会话事件流。
```

worker 只会响应向它注册过的工具。如果一个自定义工具在智能体上声明了、却没向任何 worker 或客户端注册，那么会话会以 `requires_action` 停止原因挂起，直到应用或 worker 回传对应结果；事件流程见 [处理自定义工具调用](../sessions-and-tasks/events.md)。

### 将 MCP 服务器包装为自定义工具

如果订单查询等能力已经由内网 MCP 服务提供，无需重写其业务逻辑。让 worker 连接该服务器，把选定工具注册为自定义工具即可。它与直接配置[MCP 连接器](../agent-configuration/mcp.md)的差别在连接位置：前者由 worker 发起请求，后者要求 AgentAPI 能访问服务器端点。

这种包装方式无需向 AgentAPI 开放内网服务的入站访问，但工具定义、调用参数和结果仍进入 AgentAPI。调用流程为：

1. 智能体发出一个 `agent.custom_tool_use` 事件。
2. worker 在沙箱内通过已建立的 MCP 会话，将工具调用转发给内部服务器。
3. worker 将服务器响应作为 `user.custom_tool_result` 回传。

SDK 的 MCP 辅助工具将服务器工具转换为 worker 可注册的工具。除 Worker SDK 外，还需安装对应的 MCP SDK：

| 语言         | 安装命令                                            | 需要认证时的传输配置    |
| ---------- | ----------------------------------------------- | ------------- |
| Python     | `pip install "anthropic[mcp]" "mcp>=1.24"`      | `http_client` |
| TypeScript | `npm install @modelcontextprotocol/sdk`         | `requestInit` |
| Go         | `go get github.com/modelcontextprotocol/go-sdk` | `HTTPClient`  |

以下示例连接无需认证的服务器。

### 在智能体上声明服务器的工具

先在应用侧取得服务器工具列表，把所需工具的 `name`、`description` 和 `inputSchema` 转为 `custom` 声明，再创建或更新智能体。声明和 worker 注册必须使用相同的工具列表；若服务器分页返回工具，请先取完全部分页，避免模型可以调用而 worker 未注册的工具。

Python

```python
import os

import asyncio
from typing import Any, cast
from anthropic import AsyncAnthropic
from anthropic.types.beta import BetaManagedAgentsCustomToolParams
from mcp import ClientSession, types
# 需要 mcp >= 1.24，该版本将 streamablehttp_client 重命名为 streamable_http_client。
from mcp.client.streamable_http import streamable_http_client

MCP_SERVER_URL = "http://mcp.internal.example.com:8000/mcp"

def to_custom_tool(tool: types.Tool) -> BetaManagedAgentsCustomToolParams:
    # MCP 字段与自定义工具声明一一对应。cast
    # 将 schema 字典原样传递给 SDK 的类型化参数。
    return {
        "type": "custom",
        "name": tool.name,
        "description": tool.description or tool.name,
        "input_schema": cast(Any, tool.inputSchema),
    }

async def main() -> None:
    # 请在创建智能体的应用服务端运行此代码，而不是在工作主机上：
    # 它使用已绑定的 MaaS API Key（AGENTAPI_API_KEY）进行身份验证。
    async with (
        streamable_http_client(MCP_SERVER_URL) as (read, write, _),
        ClientSession(read, write) as mcp_session,
        AsyncAnthropic(api_key=os.environ["AGENTAPI_API_KEY"], base_url=os.environ["AGENTAPI_BASE_URL"]) as client,
    ):
        await mcp_session.initialize()
        listed = await mcp_session.list_tools()
        agent = await client.beta.agents.create(
            name="Internal tools agent",
            model={"id": os.environ["AGENTAPI_MODEL_ID"]},
            tools=[
                {"type": "agent_toolset_20260401"},
                *[to_custom_tool(tool) for tool in listed.tools],
            ],
            extra_body={"runtime": "native"},
        )
        print(agent.id)

asyncio.run(main())
```

TypeScript

````typescript
import Anthropic from "@anthropic-ai/sdk";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";

const MCP_SERVER_URL = "http://mcp.internal.example.com:8000/mcp";

// 请在创建智能体的应用服务端运行此代码，而不是在工作主机上：它
// 使用已绑定的 MaaS API Key（AGENTAPI_API_KEY）进行身份验证。
const client = new Anthropic({ apiKey: process.env.AGENTAPI_API_KEY!, baseURL: process.env.AGENTAPI_BASE_URL! });

const mcpClient = new Client({ name: "declare-agent-tools", version: "1.0.0" });
await mcpClient.connect(new StreamableHTTPClientTransport(new URL(MCP_SERVER_URL)));
const { tools } = await mcpClient.listTools();

const agentParams = {
  name: "Internal tools agent",
  model: { id: process.env.AGENTAPI_MODEL_ID! },
  tools: [
    { type: "agent_toolset_20260401" },
    // MCP 字段与自定义工具声明一一对应。
    ...tools.map((tool) => ({
      type: "custom" as const,
      name: tool.name,
      description: tool.description || tool.name,
      input_schema: tool.inputSchema
    }))
  ]
} satisfies Parameters<typeof client.beta.agents.create>[0];
const agent = await client.beta.agents.create(agentParams, {
  body: { ...agentParams, runtime: "native" },
});
console.log(agent.id);

await mcpClient.close();
```csharp
```csharp
// 请参阅 Python、TypeScript 和 Go 选项卡。使用 MCP 客户端列出服务器的工具后，
// 在 C# 中声明自定义工具的方式与之相同。
````

Go

```go
package main

import (
	"os"
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/anthropics/anthropic-sdk-go"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const mcpServerURL = "http://mcp.internal.example.com:8000/mcp"

// toCustomTool 将一个 MCP 工具定义映射为一个自定义工具声明。
// 字段一一对应：类型化参数承载 `properties` 和
// `required`，服务器输出的其他所有 JSON Schema 关键字都放入
// ExtraFields，以便声明的 schema 与服务器的 schema 保持一致。
func toCustomTool(tool *mcpsdk.Tool) (anthropic.BetaAgentNewParamsToolUnion, error) {
	raw, err := json.Marshal(tool.InputSchema)
	if err != nil {
		return anthropic.BetaAgentNewParamsToolUnion{}, err
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return anthropic.BetaAgentNewParamsToolUnion{}, err
	}

	inputSchema := anthropic.BetaManagedAgentsCustomToolInputSchemaParam{ExtraFields: map[string]any{}}
	for keyword, value := range schema {
		switch keyword {
		case "type":
			// 该参数类型始终序列化为 "type": "object"。
		case "properties":
			properties, _ := value.(map[string]any)
			inputSchema.Properties = properties
		case "required":
			entries, _ := value.([]any)
			for _, entry := range entries {
				if name, isString := entry.(string); isString {
					inputSchema.Required = append(inputSchema.Required, name)
				}
			}
		default:
			inputSchema.ExtraFields[keyword] = value
		}
	}

	description := tool.Description
	if description == "" {
		description = tool.Name
	}
	return anthropic.BetaAgentNewParamsToolUnion{
		OfCustom: &anthropic.BetaManagedAgentsCustomToolParams{
			Type:        anthropic.BetaManagedAgentsCustomToolParamsTypeCustom,
			Name:        tool.Name,
			Description: description,
			InputSchema: inputSchema,
		},
	}, nil
}

func main() {
	ctx := context.Background()

	// 请在创建智能体的应用服务端运行此程序，而非在工作节点主机上：
	// 它使用已绑定的 MaaS API Key（AGENTAPI_API_KEY）进行身份验证。
	client := anthropic.NewClient(
	option.WithoutEnvironmentDefaults(),
	option.WithBaseURL(os.Getenv("AGENTAPI_BASE_URL")),
	option.WithAPIKey(os.Getenv("AGENTAPI_API_KEY")),
)

	mcpClient := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "declare-agent-tools", Version: "1.0.0"}, nil)
	session, err := mcpClient.Connect(ctx, &mcpsdk.StreamableClientTransport{Endpoint: mcpServerURL}, nil)
	if err != nil {
		log.Fatalf("connect to MCP server: %v", err)
	}
	defer session.Close()

	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		log.Fatalf("list MCP tools: %v", err)
	}

	tools := []anthropic.BetaAgentNewParamsToolUnion{
		{OfAgentToolset20260401: &anthropic.BetaManagedAgentsAgentToolset20260401Params{
			Type: anthropic.BetaManagedAgentsAgentToolset20260401ParamsTypeAgentToolset20260401,
		}},
	}
	for _, tool := range listed.Tools {
		custom, err := toCustomTool(tool)
		if err != nil {
			log.Fatalf("convert MCP tool %s: %v", tool.Name, err)
		}
		tools = append(tools, custom)
	}

	agent, err := client.Beta.Agents.New(ctx, anthropic.BetaAgentNewParams{
		Name:  "Internal tools agent",
		Model: anthropic.BetaManagedAgentsModelConfigParams{ID: os.Getenv("AGENTAPI_MODEL_ID")},
		Tools: tools,
	}, option.WithJSONSet("runtime", "native"))
	if err != nil {
		log.Fatalf("create agent: %v", err)
	}
	fmt.Println(agent.ID)
}

```

Java

```java
// 请参阅 Python、TypeScript 和 Go 选项卡。使用 MCP 客户端列出服务器的工具后，
// 在 Java 中声明自定义工具的方式与之相同。
```

PHP

```php
// 请参阅 Python、TypeScript 和 Go 选项卡。使用 MCP 客户端列出服务器的工具后，
// 在 PHP 中声明自定义工具的方式与之相同。
```

Ruby

```ruby
# 请参阅 Python、TypeScript 和 Go 选项卡。使用 MCP 客户端列出服务器的工具后，
# 在 Ruby 中声明自定义工具的方式与之相同。
```

### 从 worker 提供工具

worker 启动时连接同一 MCP 服务器，使用 MCP 辅助工具转换服务器工具，并与内置工具集一起注册。在整个 worker 生命周期内保持该 MCP 会话连接。

Python

```python
import asyncio
import os
from datetime import timedelta
from anthropic import AsyncAnthropic
from anthropic.lib.environments import EnvironmentWorker
from anthropic.lib.tools.agent_toolset import beta_agent_toolset_20260401
from anthropic.lib.tools.mcp import async_mcp_tool
from mcp import ClientSession
# 需要 mcp >= 1.24，该版本将 streamablehttp_client 重命名为 streamable_http_client。
from mcp.client.streamable_http import streamable_http_client

MCP_SERVER_URL = "http://mcp.internal.example.com:8000/mcp"

async def main() -> None:
    environment_key = os.environ["ANTHROPIC_ENVIRONMENT_KEY"]
    environment_id = os.environ["ANTHROPIC_ENVIRONMENT_ID"]
    # 在启动时连接一次 MCP 服务器，并在工作进程的整个生命周期内
    # 保持会话打开。超时设置会将挂起的工具调用转为错误
    # 结果，而不是让调用一直停滞。
    async with (
        streamable_http_client(MCP_SERVER_URL) as (read, write, _),
        ClientSession(read, write, read_timeout_seconds=timedelta(seconds=60)) as mcp_session,
        AsyncAnthropic(auth_token=environment_key, base_url=os.environ["ANTHROPIC_BASE_URL"]) as client,
    ):
        await mcp_session.initialize()
        listed = await mcp_session.list_tools()
        mcp_tools = [async_mcp_tool(tool, mcp_session) for tool in listed.tools]
        await EnvironmentWorker(
            client,
            environment_id=environment_id,
            environment_key=environment_key,
            workdir="/workspace",
            tools=lambda env: [*beta_agent_toolset_20260401(env), *mcp_tools],
        ).run()

asyncio.run(main())
```

TypeScript

````typescript
import Anthropic from "@anthropic-ai/sdk";
import { EnvironmentWorker } from "@anthropic-ai/sdk/helpers/beta/environments";
import {
  mcpTools,
  type MCPCallToolResultLike,
  type MCPClientLike
} from "@anthropic-ai/sdk/helpers/beta/mcp";
import { betaAgentToolset20260401 } from "@anthropic-ai/sdk/tools/agent-toolset/node";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";

const MCP_SERVER_URL = "http://mcp.internal.example.com:8000/mcp";

const environmentKey = process.env.ANTHROPIC_ENVIRONMENT_KEY!;
const environmentId = process.env.ANTHROPIC_ENVIRONMENT_ID!;
const client = new Anthropic({ authToken: environmentKey, baseURL: process.env.ANTHROPIC_BASE_URL! });
const controller = new AbortController();
process.once("SIGTERM", () => controller.abort());

// 在启动时连接一次 MCP 服务器，并在工作进程的整个生命周期内
// 保持连接打开。
const mcpClient = new Client({ name: "sandbox-worker", version: "1.0.0" });
await mcpClient.connect(new StreamableHTTPClientTransport(new URL(MCP_SERVER_URL)));
const { tools } = await mcpClient.listTools();

// MCP SDK 的 callTool 返回类型仍包含 mcpTools 不接受的旧版结果形状；
// 需要收窄类型。待 MCPClientLike 放宽后删除此处理。
const mcpClientForTools: MCPClientLike = {
  callTool: (params) => mcpClient.callTool(params) as Promise<MCPCallToolResultLike>
};

await new EnvironmentWorker({
  client,
  environmentId,
  environmentKey,
  workdir: "/workspace",
  signal: controller.signal,
  tools: (ctx) => [...betaAgentToolset20260401(ctx), ...mcpTools(tools, mcpClientForTools)]
}).run();
```csharp
```csharp
// EnvironmentWorker 目前在 C# SDK 中不可用。
````

Go

```go
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/lib/environments"
	"github.com/anthropics/anthropic-sdk-go/mcp"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/tools/agenttoolset"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const mcpServerURL = "http://mcp.internal.example.com:8000/mcp"

func main() {
	environmentKey := os.Getenv("ANTHROPIC_ENVIRONMENT_KEY")
	environmentID := os.Getenv("ANTHROPIC_ENVIRONMENT_ID")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := anthropic.NewClient(option.WithAuthToken(environmentKey), option.WithBaseURL(os.Getenv("ANTHROPIC_BASE_URL")))

	// 在启动时连接一次 MCP 服务器，并在工作进程的整个生命周期内
	// 保持会话打开。
	mcpClient := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "sandbox-worker", Version: "1.0.0"}, nil)
	session, err := mcpClient.Connect(ctx, &mcpsdk.StreamableClientTransport{Endpoint: mcpServerURL}, nil)
	if err != nil {
		log.Fatalf("connect to MCP server: %v", err)
	}
	defer session.Close()

	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		log.Fatalf("list MCP tools: %v", err)
	}
	mcpTools, err := mcp.NewBetaTools(listed.Tools, session)
	if err != nil {
		log.Fatalf("convert MCP tools: %v", err)
	}

	worker := environments.NewEnvironmentWorker(client, environments.EnvironmentWorkerOptions{
		EnvironmentID:  environmentID,
		EnvironmentKey: environmentKey,
		Workdir:        "/workspace",
		ToolsFunc: func(env *agenttoolset.AgentToolContext) []anthropic.BetaTool {
			return append(agenttoolset.BetaAgentToolset20260401(env), mcpTools...)
		},
	})
	if err := worker.Run(ctx); err != nil {
		log.Fatalf("worker: %v", err)
	}
}

```

Java

```java
// Java SDK 目前暂不支持 EnvironmentWorker。
```

PHP

```php
// PHP SDK 目前不支持 EnvironmentWorker。
```

Ruby

```ruby
# Ruby SDK 目前不支持 EnvironmentWorker。
```

包装 MCP 服务器时需遵循以下要求：

* 工具列表在启动时确定。 worker 启动时获取一次 MCP 服务器工具列表，不会自动更新正在运行的会话配置。服务器工具发生变更后，请在智能体上重新声明工具，或通过[更新智能体配置](../sessions-and-tasks/session-lifecycle.md)更新空闲会话，并重启 worker。
* 名称和描述。 工具名和描述不能为空。Native 执行时要求工具名长度为 1–64 个字符，以字母或下划线开头，其余字符只能是字母、数字、下划线或连字符；展开后的工具名称不能冲突，包括仅大小写不同的名称。建议避免与内置工具同名或使用 `mcp__` 前缀。
* 工具数量。 智能体配置的 `tools` 最多 64 个条目，每个包装工具或内置工具集各占一个条目；Native 执行时，展开后的工具总数最多 128 个。
* 同名工具处理。 MCP 辅助工具保留服务器原始名称和描述。多个服务器提供同名工具时，为包装工具添加前缀，调用时映射回原名。
* JSON Schema。 `input_schema` 必须是非空对象。Native 执行时会编译并校验 Schema：顶层 `type` 如有设置必须为 `object`，禁止外部 `$ref`，允许以 `#` 开头的本地引用。Schema 最多 256 KiB，嵌套深度最多 64 层。具体模型和 worker 可能有更严格的要求，建议将引用结构内联展开以提高兼容性。
* 错误结果。 MCP 工具错误及无法转换的内容（如音频块、资源链接）均作为错误工具结果返回模型。
* 调用超时。 worker 与 MCP 客户端分别控制超时。在上述已核验版本中，Python worker 的单次工具调用上限为 150 秒，TypeScript 和 Go worker 默认为 120 秒；Go 可通过 `ToolTimeout` 调整。MCP 客户端可能更早超时，建议显式设置其超时，如 Python 的 `read_timeout_seconds`。
* 仅包装可信服务器。 工具名称、描述和结果会进入模型上下文，可能影响其他工具调用，包括 worker 主机上的 `bash`。仅声明自行运营或信任、并授权智能体使用的工具。
* 自定义工具由应用控制权限。 [权限策略](../agent-configuration/permissions.md)控制内置工具集和 MCP 工具集。包装后的 MCP 工具属于自定义工具，worker 会执行已注册的调用；审批逻辑应由工具代码实现。

## 准备任务文件并保留产物

自托管环境保存资源声明，不负责替应用准备文件系统。任务文件和 GitHub 仓库需要由 worker 或启动器下载、检出并放到工作目录。可以使用会话的 `resources` 声明 AgentAPI 资源，或使用 `metadata` 传递自有系统的对象存储路径、提交 SHA 等引用。

领取的工作项包含会话 ID。启动器可查询 `GET /v1/sessions/{session_id}`，读取资源声明和元数据，在执行工具前准备目录；普通产物应在实例回收前收集，记忆文件则通过 SDK 同步。目录职责如下：

* `/workspace`： 工具执行和技能下载的系统默认工作目录。CLI 的 `--workdir` 标志默认为当前目录；传入 `--workdir /workspace` 以匹配系统默认值。技能会下载到 `<workdir>/skills/<name>/`。如果使用不同的工作目录，请更新智能体的系统提示，以便智能体能够找到技能文件。
* 输出： 自托管环境不自动收集 `/mnt/session/outputs`。请在系统提示中约定输出路径，并由启动器收集或上传产物；通常可以将工作目录挂载到独立的会话存储目录。
* `/mnt/memory/`： SDK worker 将会话挂载的记忆存储下载到本地，每个存储对应一个目录，路径由该存储的 `mount_path` 决定（例如 `/mnt/memory/user-preferences/`）。这些目录在 worker 认领会话时创建、在会话结束时删除；详见[使用记忆存储](self-hosted.md)。

## 将跨任务状态写入记忆存储

需要让后续任务读取本次更新时，将[记忆存储](../data-and-integrations/memory.md)加入会话的 `resources`，而不是依赖 worker 工作目录永久保留。每个会话最多附加 8 个存储，声明方式见[将记忆存储附加到会话](../data-and-integrations/memory.md)。自托管会话仍须使用 `native` 智能体。

使用 Python、TypeScript 或 Go SDK 的 `EnvironmentWorker` 下载和同步记忆。常驻进程调用 `run()`，独立执行实例调用 `handle_item()`；两种方式都需为同步留出退出时间。

`ant` CLI worker（`ant beta:worker poll` 和 `ant beta:worker run`）不会挂载记忆存储。需要将 CLI 轮询器与记忆存储结合使用时，请按[每个会话运行一个沙箱](self-hosted.md)在会话沙箱内运行 SDK worker。

### 记忆下载与同步流程

记忆同步的验收依据是服务端存储内容，而非模型已完成文件写入。SDK worker 在本地副本和服务端之间执行以下操作：

1. 领取时下载。 用工作项的会话级 `secret` 读取各存储，在 `mount_path` 创建本地目录。路径与云端会话的 `/mnt/memory/` 约定一致，例如 `/mnt/memory/user-preferences/`；系统提示会告知智能体挂载位置。
2. 执行时访问。 worker 将记忆目录加入文件工具的允许根目录，`access: "read_only"` 的目录同时加入只读根目录。智能体通过 `read`、`write`、`edit`、`glob`、`grep` 操作副本。
3. 工具调用后同步。 worker 下载服务端更新、上传本地修改；每个同步间隔（默认 15 秒）最多同步一次，因此写入工具返回成功不代表内容已上传。
4. 结束时上传并清理。 正常结束先进行最终同步，再执行退出上传，两个网络阶段各有 30 秒超时；之后删除本次创建的存储目录。取消工作会跳过完整同步，但仍尝试上传修改并清理目录。

任务结束后，通过记忆 API 核对最终写回内容。其他会话只有在更新上传且自身下载完成后，才能读到新内容。云端沙箱的写回规则见[记忆访问与同步机制](../data-and-integrations/memory.md)。

请保留存储目录中的 `.anthropic-memory-store` 标记文件。标记丢失或变更且目录中仍有文件时，worker 暂停同步，避免误删服务端内容；目录清空时，worker 会重新下载存储内容。

### 准备主机

自托管记忆存储要求 worker 主机具备 POSIX 文件系统，主机要求见[准备环境与凭证](self-hosted.md)。由于 worker 打开记忆文件时需要 `O_NOFOLLOW`，因此不支持 Windows 主机。建议使用区分大小写的文件系统，避免仅大小写不同的记忆路径发生冲突。

在启动 worker 之前，创建父目录并使其对 worker 运行所用的用户可写：

```bash
sudo mkdir -p /mnt/memory && sudo chown "$USER" /mnt/memory
```

请勿预先创建存储的 `mount_path` 目录（例如 `/mnt/memory/user-preferences`）。worker 会自行创建并在会话结束时删除；路径已存在时，会拒绝启动工作。还需满足以下要求：

* 同一文件系统中，同一存储只能由一个会话挂载。 多个会话同时挂载同一存储会使用相同路径。请按[每个会话运行一个沙箱](self-hosted.md)为每个会话配置独立沙箱，以隔离挂载路径。
* 退出时完成上传和清理。 为进程配置取消流程，见退出与目录清理；直接强制终止会遗留目录并丢失尚未上传的内容。

### 选择同步间隔和删除策略

应用需要分别决定更新延迟和删除策略。以下两个 `EnvironmentWorker` 选项控制这些行为：

* `memory_sync_interval`： 同步间隔，默认 15 秒，最小 5 秒。Python 单位为秒；TypeScript 使用 `memorySyncIntervalMs`，单位为毫秒；Go 使用 `MemorySyncInterval`，类型为 duration。缩短间隔可减少记忆更新延迟，但会增加请求频率。

  Python 的 `None`、TypeScript 的 `null` 或 Go 的负 duration 会关闭记忆下载和同步。仅当所有会话均不附加记忆存储时才应关闭；否则系统提示虽包含存储信息，记忆内容却不可用。

  启用记忆支持后，附加了记忆存储的工作项必须携带会话凭证 `secret`，否则 worker 会拒绝处理该工作项。排查方法见记忆挂载问题。
* `memory_sync_deletions`： 是否将本地删除同步到存储，不影响上传和下载。TypeScript 使用 `memorySyncDeletions`，Go 使用 `MemorySyncDeletions`。

| Python / TypeScript | Go                                            | 行为                                                |
| ------------------- | --------------------------------------------- | ------------------------------------------------- |
| `"enabled"`（默认）     | `environments.MemorySyncDeletionsEnabled`（零值） | 本地文件持续缺失至少 30 秒，后续同步再次确认后删除服务端记忆；正常结束时的最终同步不等待此窗口 |
| `"log_only"`        | `environments.MemorySyncDeletionsLogOnly`     | 同样检查，但仅记录拟删除项；可用于核对删除范围                           |
| `"disabled"`        | `environments.MemorySyncDeletionsDisabled`    | 不删除服务端记忆                                          |

创建 worker 时设置这些选项，可使用 `EnvironmentWorker` 构造函数，或 Python 和 TypeScript 的 `client.beta.environments.work.worker()` 工厂。

以下示例每 10 秒同步一次，并仅记录拟删除的记忆：

Python

```python
worker = EnvironmentWorker(
    client,
    environment_id=environment_id,
    environment_key=environment_key,
    workdir="/workspace",
    memory_sync_interval=10,  # seconds
    memory_sync_deletions="log_only",
)
```

TypeScript

````typescript
const worker = new EnvironmentWorker({
  client,
  environmentId,
  environmentKey,
  workdir: "/workspace",
  memorySyncIntervalMs: 10_000,
  memorySyncDeletions: "log_only"
});
```csharp
```csharp
// EnvironmentWorker 目前在 C# SDK 中不可用。
````

Go

```go
worker := environments.NewEnvironmentWorker(client, environments.EnvironmentWorkerOptions{
	EnvironmentID:       environmentID,
	EnvironmentKey:      environmentKey,
	Workdir:             "/workspace",
	MemorySyncInterval:  10 * time.Second,
	MemorySyncDeletions: environments.MemorySyncDeletionsLogOnly,
})
```

Java

```java
// Java SDK 目前暂不支持 EnvironmentWorker。
```

PHP

```php
// PHP SDK 目前暂不支持 EnvironmentWorker。
```

Ruby

```ruby
# Ruby SDK 目前不支持 EnvironmentWorker。
```

### 只读存储与冲突

`access: "read_only"` 控制文件工具和记忆上传：`write`、`edit` 拒绝写入，worker 不上传本地修改。它不是整个文件系统的只读挂载；`bash`、自定义工具和本地 MCP 服务仍可能修改副本，随后远程更新会覆盖这些改动。

若需保护执行期间的本地副本，禁用智能体的 `bash`，并移除能够写入该目录的自定义工具。目录仍须允许 worker 写入，以便创建挂载目录和下载内容。

发生同一文件的并发修改时，下一次同步保留服务端版本，覆盖本地并记录警告。此前成功的 `write` 或 `edit` 结果不代表修改最终保留；应用需要在同步后重新读取，再决定是否修改。

## 监控积压、停止工作与回收实例

部署验收需要同时检查队列和执行实例：队列反映领取与确认情况，worker 日志和实例状态反映工具执行、上传及清理结果。以下示例由执行环境之外的监控或运维程序调用，使用已绑定的 MaaS API Key。

使用 CLI 或 SDK worker 时，领取和保活由辅助工具处理；应用仍需负责进程健康检查、实例回收，以及上传失败后的排查。

> 说明
>
> 这些端点接受已绑定的 MaaS API Key 或环境密钥。账户级运维调用应从 worker 执行环境之外发起，使用 MaaS API Key。请避免将 `AGENTAPI_API_KEY` 传入智能体可以执行命令的进程环境，以免工具读取账户级凭证。

### 读取队列深度

用 `work.stats` 区分“没有 worker”“领取后未确认”和“已有任务积压”，再决定检查接入还是扩容：

* `depth`：正在等待被领取的项数。可据此扩容 worker 集群，或在积压时告警。
* `pending`：worker 已领取但尚未确认的工作项数量。worker 辅助工具在处理工作项前会先确认，因此正常运行时该值应接近零；持续非零说明 worker 在领取后尚未完成确认。
* `oldest_queued_at`：队列中最早入队工作项的时间戳，包括等待领取和已领取但未确认的工作项；队列为空时为 `null`。
* `workers_polling`：过去 30 秒内轮询过的 worker 数量。可用于存活告警。

cURL

```bash
curl -sS "$AGENTAPI_BASE_URL/v1/environments/$ANTHROPIC_ENVIRONMENT_ID/work/stats" \
  -H "x-api-key: $AGENTAPI_API_KEY"
```

Python

```python
import os

import anthropic

client = anthropic.Anthropic(api_key=os.environ["AGENTAPI_API_KEY"], base_url=os.environ["AGENTAPI_BASE_URL"])

stats = client.beta.environments.work.stats(os.environ["ANTHROPIC_ENVIRONMENT_ID"])
print(f"depth={stats.depth} pending={stats.pending}")
```

TypeScript

````typescript
import Anthropic from "@anthropic-ai/sdk";

const client = new Anthropic({ apiKey: process.env.AGENTAPI_API_KEY!, baseURL: process.env.AGENTAPI_BASE_URL! });

const stats = await client.beta.environments.work.stats(process.env.ANTHROPIC_ENVIRONMENT_ID!);

console.log(`depth=${stats.depth} pending=${stats.pending}`);
```csharp
```csharp
using Anthropic;

var client = new AnthropicClient(new Anthropic.Core.ClientOptions
{
    ApiKey = System.Environment.GetEnvironmentVariable("AGENTAPI_API_KEY")!,
    BaseUrl = System.Environment.GetEnvironmentVariable("AGENTAPI_BASE_URL")!,
});

var environmentId = Environment.GetEnvironmentVariable("ANTHROPIC_ENVIRONMENT_ID")!;

var stats = await client.Beta.Environments.Work.Stats(environmentId);

Console.WriteLine($"depth={stats.Depth} pending={stats.Pending}");
````

Go

```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/anthropics/anthropic-sdk-go"
)

func main() {
	client := anthropic.NewClient(
	option.WithoutEnvironmentDefaults(),
	option.WithBaseURL(os.Getenv("AGENTAPI_BASE_URL")),
	option.WithAPIKey(os.Getenv("AGENTAPI_API_KEY")),
)
	environmentID := os.Getenv("ANTHROPIC_ENVIRONMENT_ID")

	stats, err := client.Beta.Environments.Work.Stats(
		context.Background(),
		environmentID,
		anthropic.BetaEnvironmentWorkStatsParams{},
	)
	if err != nil {
		panic(err)
	}

	fmt.Printf("depth=%d pending=%d\n", stats.Depth, stats.Pending)
}
```

Java

```java
import com.anthropic.client.AnthropicClient;
import com.anthropic.client.okhttp.AnthropicOkHttpClient;
import com.anthropic.models.beta.environments.work.BetaSelfHostedWorkQueueStats;

void main() {
    AnthropicClient client = AnthropicOkHttpClient.builder()
        .apiKey(System.getenv("AGENTAPI_API_KEY"))
        .baseUrl(System.getenv("AGENTAPI_BASE_URL"))
        .build();

    BetaSelfHostedWorkQueueStats stats = client.beta()
        .environments()
        .work()
        .stats(System.getenv("ANTHROPIC_ENVIRONMENT_ID"));

    IO.println("depth=" + stats.depth() + " pending=" + stats.pending());
}
```

PHP

```php
<?php

use Anthropic\Client;

$client = new Client(apiKey: getenv('AGENTAPI_API_KEY'), baseUrl: getenv('AGENTAPI_BASE_URL'));

$stats = $client->beta->environments->work->stats(getenv('ANTHROPIC_ENVIRONMENT_ID'));

printf("depth=%d pending=%d\n", $stats->depth, $stats->pending);
```

Ruby

```ruby
require "anthropic"

client = Anthropic::Client.new(api_key: ENV.fetch("AGENTAPI_API_KEY"), base_url: ENV.fetch("AGENTAPI_BASE_URL"))

stats = client.beta.environments.work.stats(ENV.fetch("ANTHROPIC_ENVIRONMENT_ID"))

puts "depth=#{stats.depth} pending=#{stats.pending}"
```

```text [wrap]
{
  "type": "work_queue_stats",
  "depth": 0,
  "pending": 0,
  "oldest_queued_at": null,
  "workers_polling": 0
}
```

### 停止指定工作项

> 停止工作与进程退出
>
> 使用 `work.stop` 请求 worker 停止指定会话的工作。默认情况下，工作项先转为 `stopping`；worker 在下一次租约心跳时检测到停止请求，取消该会话进行中的工具调用并确认关闭，随后工作项转为 `stopped`。需要立即标记停止时，可以在请求体中传入 `force: true`，或在 CLI 中使用 `--force`，将工作项标记为 `stopped`。该强制标记不保证工具进程已经退出；回收执行实例前，还需核对进程停止和资源清理结果。

运维系统需要自行设置 `ANTHROPIC_WORK_ID`，其值为待停止的工作项 ID。可以先通过环境工作项列表查询，具体请求字段见 [OpenAPI 参考](../../api/api-conventions.md)。

cURL

```bash
curl -sS "$AGENTAPI_BASE_URL/v1/environments/$ANTHROPIC_ENVIRONMENT_ID/work/$ANTHROPIC_WORK_ID/stop" \
  -H "x-api-key: $AGENTAPI_API_KEY" \
  -H "content-type: application/json" \
  -d '{}'
```

Python

```python
import os

import anthropic

client = anthropic.Anthropic(api_key=os.environ["AGENTAPI_API_KEY"], base_url=os.environ["AGENTAPI_BASE_URL"])

work = client.beta.environments.work.stop(
    os.environ["ANTHROPIC_WORK_ID"],
    environment_id=os.environ["ANTHROPIC_ENVIRONMENT_ID"],
)
print(work.state)
```

TypeScript

````typescript
import Anthropic from "@anthropic-ai/sdk";

const client = new Anthropic({ apiKey: process.env.AGENTAPI_API_KEY!, baseURL: process.env.AGENTAPI_BASE_URL! });

const work = await client.beta.environments.work.stop(process.env.ANTHROPIC_WORK_ID!, {
  environment_id: process.env.ANTHROPIC_ENVIRONMENT_ID!
});

console.log(work.state);
```csharp
```csharp
using Anthropic;

var client = new AnthropicClient(new Anthropic.Core.ClientOptions
{
    ApiKey = System.Environment.GetEnvironmentVariable("AGENTAPI_API_KEY")!,
    BaseUrl = System.Environment.GetEnvironmentVariable("AGENTAPI_BASE_URL")!,
});

var work = await client.Beta.Environments.Work.Stop(
    Environment.GetEnvironmentVariable("ANTHROPIC_WORK_ID")!,
    new()
    {
        EnvironmentID = Environment.GetEnvironmentVariable("ANTHROPIC_ENVIRONMENT_ID")!
    }
);

Console.WriteLine(work.State);
````

Go

```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/anthropics/anthropic-sdk-go"
)

func main() {
	client := anthropic.NewClient(
	option.WithoutEnvironmentDefaults(),
	option.WithBaseURL(os.Getenv("AGENTAPI_BASE_URL")),
	option.WithAPIKey(os.Getenv("AGENTAPI_API_KEY")),
)

	work, err := client.Beta.Environments.Work.Stop(
		context.Background(),
		os.Getenv("ANTHROPIC_WORK_ID"),
		anthropic.BetaEnvironmentWorkStopParams{
			EnvironmentID: os.Getenv("ANTHROPIC_ENVIRONMENT_ID"),
		},
	)
	if err != nil {
		panic(err)
	}
	fmt.Println(work.State)
}
```

Java

```java
import com.anthropic.client.AnthropicClient;
import com.anthropic.client.okhttp.AnthropicOkHttpClient;
import com.anthropic.models.beta.environments.work.BetaSelfHostedWork;
import com.anthropic.models.beta.environments.work.BetaSelfHostedWorkStopRequest;
import com.anthropic.models.beta.environments.work.WorkStopParams;

void main() {
    AnthropicClient client = AnthropicOkHttpClient.builder()
        .apiKey(System.getenv("AGENTAPI_API_KEY"))
        .baseUrl(System.getenv("AGENTAPI_BASE_URL"))
        .build();

    BetaSelfHostedWork work = client.beta().environments().work().stop(
        WorkStopParams.builder()
            .environmentId(System.getenv("ANTHROPIC_ENVIRONMENT_ID"))
            .workId(System.getenv("ANTHROPIC_WORK_ID"))
            .betaSelfHostedWorkStopRequest(BetaSelfHostedWorkStopRequest.builder().build())
            .build()
    );

    IO.println(work.state());
}
```

PHP

```php
<?php

use Anthropic\Client;

$client = new Client(apiKey: getenv('AGENTAPI_API_KEY'), baseUrl: getenv('AGENTAPI_BASE_URL'));

$work = $client->beta->environments->work->stop(
    getenv('ANTHROPIC_WORK_ID'),
    environmentID: getenv('ANTHROPIC_ENVIRONMENT_ID'),
);

echo $work->state . "\n";
```

Ruby

```ruby
require "anthropic"

client = Anthropic::Client.new(api_key: ENV.fetch("AGENTAPI_API_KEY"), base_url: ENV.fetch("AGENTAPI_BASE_URL"))

work = client.beta.environments.work.stop(
  ENV.fetch("ANTHROPIC_WORK_ID"),
  environment_id: ENV.fetch("ANTHROPIC_ENVIRONMENT_ID")
)

puts work.state
```

### 退出与目录清理

停止工作项后，还需确认 worker 的上传、目录清理和执行实例退出。`EnvironmentWorker` 不会自动安装信号处理程序，应用必须把退出转换为取消操作：

1. 将 SIGTERM、SIGINT 映射到 SDK 取消机制：Python 取消运行 `run()` 或 `handle_item()` 的任务，TypeScript 中止传给 worker 的 `signal`，Go 取消 context。
2. 独立 worker 进程在信号处理程序中取消；嵌入服务的 worker 在服务关闭钩子中取消，避免接管服务的信号处理。
3. 用 SIGTERM 请求退出，建议在强制终止前至少预留 60 秒，再留出本地文件清理时间。正常结束可能执行两个各有 30 秒超时的网络阶段，此建议不是总退出耗时保证。
4. 核对记忆写回、产物收集和进程退出结果，再回收实例。如果强制终止留下了 `/mnt/memory/` 存储目录，应在下次挂载前删除遗留目录；其中尚未同步的修改将丢失。

### 排查记忆挂载问题

记忆故障需检查 worker 日志，不能仅依赖会话错误事件：

* 挂载失败：工作项失败，会话保持空闲，不发出错误事件。
* 后台同步失败：仅记录日志。
* 只读访问拒绝：向智能体返回工具错误，见[只读存储与冲突](self-hosted.md)。

| 症状                                                                                                            | 原因                                     | 修复方法                                                                                                                                                                                   |
| ------------------------------------------------------------------------------------------------------------- | -------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| worker 日志包含 `the work item carried no sessions token`（Go 中为 `ErrSessionMemoryNoToken`），工作项失败。                 | 工作项未提供会话凭证 `secret`，或启动脚本未将它传给执行实例。    | 按每个会话运行一个沙箱传入 `ANTHROPIC_WORK_SECRET`。同一进程领取并处理工作仍出现该错误时，请联系支持团队。                                                                                                                      |
| worker 日志包含 `something already exists at the memory store's path`。                                            | 上一个会话遗留了存储目录，通常是因为 worker 在完成清理前被强制终止。 | 删除日志中指定的遗留目录。其中尚未同步的修改将丢失。                                                                                                                                                             |
| worker 日志包含 `cannot create the memory store's folder` 和 `the worker host must make this mount path writable`。 | worker 运行所用的用户无法在 `/mnt/memory` 下创建目录。 | 创建 `/mnt/memory` 并将其 `chown` 给该用户；请参阅 [准备主机](self-hosted.md)。             |
| 在 worker 领取会话后不久，会话处于 `idle` 状态，停止原因为 `requires_action`，且没有错误事件。                                              | 由于前述原因之一，worker 无法挂载记忆存储，因此使工作项失败。     | 在主机上修复原因，然后发送一个 [`user.interrupt`](../sessions-and-tasks/events.md) 事件：会话的工作会再次排队，下一个领取它的 worker 会重试挂载。 |

## SDK 接入方式查询

前面的方案分别对应三类 SDK 辅助工具。先确定哪个进程负责领取工作，再选择入口，避免轮询器和执行实例重复管理同一个工作项：

| 应用职责                       | SDK 入口                                          |
| -------------------------- | ----------------------------------------------- |
| 同一进程领取并执行，或执行实例处理一个工作项     | `EnvironmentWorker` 的 `run()` 或 `handle_item()` |
| 外部启动器领取后创建执行实例             | `work.poller()`                                 |
| 应用已领取工作并自行管理生命周期，只需要执行会话工具 | `client.beta.sessions.events.tool_runner()`     |

* `EnvironmentWorker`： 负责轮询、资源准备和工具执行。主要方法与配置项如下：

  * `.run()`：持续运行并处理新领取的会话工作。
  * `.handle_item()`：处理单个已认领工作项后退出。工作、会话和环境标识符可显式传入，或从 `ant beta:worker poll --on-work` 设置的 `ANTHROPIC_*` 变量读取。
  * `memory_sync_interval`（TypeScript 中为 `memorySyncIntervalMs`，Go 中为 `MemorySyncInterval`）和 `memory_sync_deletions`（`memorySyncDeletions`、`MemorySyncDeletions`）：前者控制会话运行期间已附加记忆存储的同步间隔，后者控制是否将本地文件删除同步到服务端存储。单位、默认值及关闭记忆支持的方法见[配置同步](self-hosted.md)。

* `work.poller()`： 轮询队列并返回已领取的工作项，由应用代码决定如何处理，例如为每个会话启动独立沙箱。它接受以下参数：

  * `drain`：队列为空时是停止轮询，还是继续等待新工作。
  * `block_ms`：单次轮询在返回前等待工作到达的时长（毫秒），取值必须在 1 到 999 之间；辅助工具会自动重新发起下一次轮询。AgentAPI 在省略此参数时使用 999 毫秒长轮询；不提供通过 `0` 或省略参数进行非阻塞检查的公共接口。
  * `reclaim_older_than_ms`：重新领取已被认领、但超过指定毫秒数仍未确认的工作项。
  * `auto_stop`（TypeScript 中为 `autoStop`，Go 中为 `AutoStop`）：是否在处理工作项后自动报告停止。将工作交给 `handle_item()`，或由沙箱内部报告停止时，应设为 `false`，避免重复报告。

* `client.beta.sessions.events.tool_runner()`： 根据会话 ID 和注册工具列表执行该会话的工具调用。适用于已领取工作，并自行管理工作生命周期的场景。

> 说明
>
> 使用 `.handle_item()` 挂载[记忆存储](self-hosted.md)时，还需传入工作项的 `secret`：Python 使用 `work_secret`，TypeScript 使用 `workSecret`，Go 使用 `WorkSecret`，或设置 `ANTHROPIC_WORK_SECRET`。
>
> `ant beta:worker poll --on-work` 不会设置该变量，启动脚本须从标准输入的工作项 JSON 中读取凭证，见[每个会话运行一个沙箱](self-hosted.md)。

需要自行启动每个会话的执行进程时，例如为每个已认领会话创建沙箱，请使用工作轮询器：

cURL

```bash
# work poller 是一个 SDK 辅助工具（Python、TypeScript、Go），而非原始
# 端点。在 shell 中，请改用 `ant beta:worker poll --on-work`；
# 请参阅 本页的 ant CLI 方式。
```

Python

```python
import asyncio
import os

from anthropic import AsyncAnthropic
from anthropic.types.beta.environments import BetaSelfHostedWork

SANDBOX_ENV = (
    "ANTHROPIC_ENVIRONMENT_ID",
    "ANTHROPIC_ENVIRONMENT_KEY",
    "ANTHROPIC_WORK_ID",
    "ANTHROPIC_SESSION_ID",
    "ANTHROPIC_WORK_SECRET",
    "ANTHROPIC_BASE_URL",  # forwarded only when set on this host
)

async def launch_container(work: BetaSelfHostedWork) -> None:
    print(f"claimed session {work.data.id}")
    # 请将 `docker run` 替换为实际使用的沙箱启动器。转发环境
    # 密钥（切勿转发账户级 API 密钥）以及工作项的每会话密钥：内部的 worker
    # 需要该密钥来挂载会话的内存存储。
    env = os.environ | {
        "ANTHROPIC_WORK_ID": work.id,
        "ANTHROPIC_SESSION_ID": work.data.id,
        "ANTHROPIC_WORK_SECRET": work.secret or "",
    }
    forward = [arg for name in SANDBOX_ENV for arg in ("-e", name)]
    launcher = await asyncio.create_subprocess_exec(
        "docker", "run", "--rm", "--detach", *forward, "your-sdk-worker-image", env=env
    )
    await launcher.wait()

async def main() -> None:
    environment_key = os.environ["ANTHROPIC_ENVIRONMENT_KEY"]
    environment_id = os.environ["ANTHROPIC_ENVIRONMENT_ID"]
    async with AsyncAnthropic(auth_token=environment_key, base_url=os.environ["ANTHROPIC_BASE_URL"]) as client:
        async for work in client.beta.environments.work.poller(
            environment_id=environment_id,
            environment_key=environment_key,
            auto_stop=False,  # the launched sandbox owns the stop call
        ):
            await launch_container(work)

asyncio.run(main())
```

TypeScript

````typescript
import { spawn } from "node:child_process";
import { once } from "node:events";
import Anthropic from "@anthropic-ai/sdk";
import { WorkPoller } from "@anthropic-ai/sdk/helpers/beta/environments";
import type { BetaSelfHostedWork } from "@anthropic-ai/sdk/resources/beta/environments";

const SANDBOX_ENV = [
  "ANTHROPIC_ENVIRONMENT_ID",
  "ANTHROPIC_ENVIRONMENT_KEY",
  "ANTHROPIC_WORK_ID",
  "ANTHROPIC_SESSION_ID",
  "ANTHROPIC_WORK_SECRET",
  "ANTHROPIC_BASE_URL" // forwarded only when set on this host
];

const environmentKey = process.env.ANTHROPIC_ENVIRONMENT_KEY!;
const environmentId = process.env.ANTHROPIC_ENVIRONMENT_ID!;
const client = new Anthropic({ authToken: environmentKey, baseURL: process.env.ANTHROPIC_BASE_URL! });

async function launchContainer(work: BetaSelfHostedWork): Promise<void> {
  console.log(`claimed session ${work.data.id}`);
  // 请将 `docker run` 替换为实际使用的沙箱启动器。转发环境
  // 密钥（切勿转发账户级 API 密钥）以及工作项的每会话密钥：
  // 内部的 worker 需要该密钥来挂载会话的内存存储。
  const env = {
    ...process.env,
    ANTHROPIC_WORK_ID: work.id,
    ANTHROPIC_SESSION_ID: work.data.id,
    ANTHROPIC_WORK_SECRET: work.secret ?? ""
  };
  const forward = SANDBOX_ENV.flatMap((name) => ["-e", name]);
  const launcher = spawn(
    "docker",
    ["run", "--rm", "--detach", ...forward, "your-sdk-worker-image"],
    { env, stdio: "inherit" }
  );
  await once(launcher, "close");
}

const poller = new WorkPoller({
  client,
  environmentId,
  environmentKey,
  autoStop: false // the launched sandbox owns the stop call
});

for await (const work of poller) {
  await launchContainer(work);
}
```csharp
```csharp
// C# SDK 目前尚未提供工作轮询辅助工具。
// 如需直接领取工作，请参阅 环境工作队列端点。
````

Go

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/lib/environments"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
)

var sandboxEnv = []string{
	"ANTHROPIC_ENVIRONMENT_ID",
	"ANTHROPIC_ENVIRONMENT_KEY",
	"ANTHROPIC_WORK_ID",
	"ANTHROPIC_SESSION_ID",
	"ANTHROPIC_WORK_SECRET",
	"ANTHROPIC_BASE_URL", // forwarded only when set on this host
}

func launchContainer(ctx context.Context, work *anthropic.BetaSelfHostedWork) error {
	fmt.Printf("claimed session %s\n", work.Data.ID)
	// 请将 `docker run` 替换为实际使用的沙箱启动器。转发环境
	// 密钥（不得转发账户级 API 密钥）以及工作项的每会话密钥：内部的
	// worker 需要该密钥来挂载会话的内存存储。
	args := []string{"run", "--rm", "--detach"}
	for _, name := range sandboxEnv {
		args = append(args, "-e", name)
	}
	launcher := exec.CommandContext(ctx, "docker", append(args, "your-sdk-worker-image")...)
	launcher.Env = append(os.Environ(),
		"ANTHROPIC_WORK_ID="+work.ID,
		"ANTHROPIC_SESSION_ID="+work.Data.ID,
		"ANTHROPIC_WORK_SECRET="+work.Secret,
	)
	launcher.Stdout, launcher.Stderr = os.Stdout, os.Stderr
	return launcher.Run()
}

func main() {
	environmentID := os.Getenv("ANTHROPIC_ENVIRONMENT_ID")
	environmentKey := os.Getenv("ANTHROPIC_ENVIRONMENT_KEY")

	client := anthropic.NewClient(option.WithAuthToken(environmentKey), option.WithBaseURL(os.Getenv("ANTHROPIC_BASE_URL")))

	ctx := context.Background()

	poller := environments.NewWorkPoller(ctx, client, environments.WorkPollerOptions{
		EnvironmentID:  environmentID,
		EnvironmentKey: environmentKey,
		AutoStop:       param.NewOpt(false), // the launched sandbox owns the stop call
	})
	defer poller.Close()

	for work, err := range poller.All() {
		if err != nil {
			log.Fatal(err)
		}
		if err := launchContainer(ctx, work); err != nil {
			log.Fatal(err)
		}
	}
}
```

Java

```java
// Java SDK 目前尚未提供工作轮询辅助工具。
// 如需直接领取工作，请参阅 环境工作队列端点。
```

PHP

```php
// PHP SDK 目前尚未提供工作轮询辅助函数。
// 如需直接领取工作，请参阅 环境工作队列端点。
```

Ruby

```ruby
# Ruby SDK 目前尚未提供工作轮询辅助方法。
# 如需直接领取工作，请参阅 环境工作队列端点。
```

启动沙箱时，必须将已认领工作项的 `secret` 连同会话、工作和环境标识符一起传入，例如通过 `ANTHROPIC_WORK_SECRET` 提供会话凭证，供沙箱内 worker 挂载会话的[记忆存储](self-hosted.md)。详见[每个会话运行一个沙箱](self-hosted.md)。

`AgentToolContext` 是工具调用的执行上下文，定义工作目录和路径策略，并支持下载会话技能。文件工具（`read`、`write`、`edit`、`glob`、`grep`）受以下路径限制：

* 这些工具只能在工作目录、以及 `allowed_roots`（TypeScript 中为 `allowedRoots`，Go 中为 `AllowedRoots`）列出的目录范围内操作。
* `write` 和 `edit` 还会额外拒绝 `read_only_roots`（`readOnlyRoots`、`ReadOnlyRoots`）下的路径。
* `EnvironmentWorker` 会自动将会话的记忆存储目录加入这些列表。

这些路径限制只作用于文件工具，不限制 `bash`，也不能替代沙箱隔离。`beta_agent_toolset_20260401(env)` 接收 `AgentToolContext`，返回 `bash`、`read`、`write`、`edit`、`glob`、`grep` 的实现。`native` 的 `web_fetch` 和 `web_search` 由 AgentAPI 执行，不会进入 worker 的工具队列。

使用 `EnvironmentWorker`： worker 自动管理工具上下文和内置工具实例。需要加入自定义工具时，传入 `tools` 工厂返回完整工具列表：

Python

```python
EnvironmentWorker(client, ..., tools=lambda env: [beta_bash_tool(env), my_custom_tool])
```

TypeScript

````typescript
new EnvironmentWorker({
  client,
  environmentId,
  environmentKey,
  tools: (ctx) => [betaBashTool(ctx), myCustomTool]
});
```csharp
```csharp
// EnvironmentWorker 目前在 C# SDK 中不可用。
// 如需直接响应自定义工具调用，请参阅会话事件流。
````

Go

```go
worker := environments.NewEnvironmentWorker(client, environments.EnvironmentWorkerOptions{
	EnvironmentID:  environmentID,
	EnvironmentKey: environmentKey,
	ToolsFunc: func(env *agenttoolset.AgentToolContext) []anthropic.BetaTool {
		return []anthropic.BetaTool{agenttoolset.BetaBashTool(env), myCustomTool}
	},
})
```

Java

```java
// Java SDK 目前不提供 EnvironmentWorker。
// 如需直接响应自定义工具调用，请参阅会话事件流。
```

PHP

```php
// PHP SDK 目前尚不支持 EnvironmentWorker。
// 如需直接响应自定义工具调用，请参阅会话事件流。
```

Ruby

```ruby
# Ruby SDK 目前暂不提供 EnvironmentWorker。
# 如需直接响应自定义工具调用，请参阅会话事件流。
```

使用 `work.poller()` 和 `tool_runner()`： 将工具列表作为 `tools` 传递给 `client.beta.sessions.events.tool_runner()`。若要构建该列表，请自行设置 `AgentToolContext` 并调用 `beta_agent_toolset_20260401(env)`：

Python

```python
from anthropic.lib.tools.agent_toolset import (
    AgentToolContext,
    beta_agent_toolset_20260401,
)

async with AgentToolContext(
    workdir="/workspace", client=client, session_id=work.data.id
) as env:
    # skills 已下载到 /workspace/skills/<name>/
    tools = beta_agent_toolset_20260401(env)
```

TypeScript

````typescript
import {
  setupSkills,
  betaAgentToolset20260401
} from "@anthropic-ai/sdk/tools/agent-toolset/node";

const ctx = { workdir: "/workspace", client, sessionId: work.data.id };
await setupSkills(ctx);
const tools = betaAgentToolset20260401(ctx);
```csharp
```csharp
// AgentToolContext 目前在 C# SDK 中不可用。
````

Go

```go
env := &agenttoolset.AgentToolContext{Workdir: "/workspace"}
if err := env.SetupSkills(ctx, client, work.Data.ID); err != nil {
	panic(err)
}
// skills 已下载到 /workspace/skills/<name>/
tools := agenttoolset.BetaAgentToolset20260401(env)
```

Java

```java
// AgentToolContext 目前在 Java SDK 中不可用。
```

PHP

```php
// PHP SDK 目前不支持 AgentToolContext。
```

Ruby

```ruby
# Ruby SDK 目前不支持 AgentToolContext。
```

## 后续步骤

* [自托管安全责任](self-hosted-security.md)：核对数据流、凭证和执行隔离。
* [会话](../sessions-and-tasks/session.md)：创建任务并发送输入。
* [OpenAPI 参考](../../api/api-conventions.md)：查看环境工作队列的请求与响应字段。
