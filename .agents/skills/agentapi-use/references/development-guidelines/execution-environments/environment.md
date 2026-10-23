<!-- 来源：https://developer-doc.qiniu.com/products/agentapi/development_guidelines/agentapi-execution-environments/13608-agentapi-execution-environments-environment | 由 sync-docs.py 自动生成，请勿手改 -->

# 配置云端环境



最近更新时间: 2026-10-13 15:31:59

云端环境（Environment）是一份可复用的沙箱配置，定义智能体执行任务时可用的软件包和出站网络策略。即使引用同一个环境，每个会话也会获得独立的云端沙箱，文件系统状态不会在会话之间共享。

环境创建后可以反复引用；[创建会话](../sessions-and-tasks/session.md)时传入 `environment_id` 即可。运行时和模型配置在[智能体](../agent-configuration/agent.md)上，环境负责提供执行代码、处理文件和使用工具所需的软件与网络条件。

![云端环境为每个会话提供独立沙箱](https://developer-doc.qiniu.com/docs-assets/products/agentapi/development_guidelines/agentapi-execution-environments/13608-agentapi-execution-environments-environment/FhSzYsrX726oI4yAFq3M-wtHhMRl-32921928d4b3.webp)

AgentAPI 当前支持两种环境：

* `cloud` 由 AgentAPI 通过七牛 [LAS Sandbox](https://developer-doc.qiniu.com/products/las/13479-sandbox-api-overview) 为每个会话创建独立的云端沙箱；
* `self_hosted` 由自行部署的 worker 提供工作区和执行资源。请参阅 [自托管执行器](self-hosted.md)。

本页介绍 `type: cloud` 类型的环境。它是使用七牛云端执行能力的默认接入路径。

## 创建环境

请先完成[接入准备](../../quick-start/access.md)，配置服务地址和 MaaS API Key。以下示例创建一个允许出站互联网访问的云端环境，SDK 示例沿用[SDK 客户端初始化](../../quick-start/quickstart.md)中的 `client`。

cURL

```bash
environment=$(curl --request POST "$AGENTAPI_BASE_URL/v1/environments" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --data @- <<'EOF'
{
  "name": "python-dev",
  "config": {
    "type": "cloud",
    "networking": {"type": "unrestricted"}
  }
}
EOF
)
environment_id=$(jq -r '.id' <<< "$environment")

echo "Environment ID: $environment_id"
```

Python

```python
environment = client.beta.environments.create(
    name="python-dev",
    config={
        "type": "cloud",
        "networking": {"type": "unrestricted"},
    },
)

print(f"Environment ID: {environment.id}")
```

TypeScript

````typescript
const environment = await client.beta.environments.create({
  name: "python-dev",
  config: {
    type: "cloud",
    networking: { type: "unrestricted" },
  },
});

console.log(`Environment ID: ${environment.id}`);
```csharp
```csharp
var environment = await client.Beta.Environments.Create(new()
{
    Name = "python-dev",
    Config = new BetaCloudConfigParams
    {
        Networking = new BetaUnrestrictedNetwork(),
    },
});

Console.WriteLine($"Environment ID: {environment.ID}");
````

Go

```go
environment, err := client.Beta.Environments.New(ctx, anthropic.BetaEnvironmentNewParams{
	Name: "python-dev",
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

fmt.Printf("Environment ID: %s\n", environment.ID)
```

Java

```java
var environment = client.beta().environments().create(EnvironmentCreateParams.builder()
    .name("python-dev")
    .config(BetaCloudConfigParams.builder()
        .networking(BetaUnrestrictedNetwork.builder().build())
        .build())
    .build());
IO.println("Environment ID: " + environment.id());
```

PHP

```php
$environment = $client->beta->environments->create(
    name: 'python-dev',
    config: ['type' => 'cloud', 'networking' => ['type' => 'unrestricted']],
);
echo "Environment ID: {$environment->id}\n";
```

Ruby

```ruby
environment = client.beta.environments.create(
  name: "python-dev",
  config: {
    type: "cloud",
    networking: {type: "unrestricted"}
  }
)

puts "Environment ID: #{environment.id}"
```

`name` 用于在 Portal 和列表响应中识别环境，建议使用唯一且能表达用途的名称。保存响应中的 `id`，后续创建会话时需要引用它。

## 配置选项

### 预装软件与智能体运行时

七牛云端沙箱已包含常用的语言运行环境、数据库和命令行工具。添加依赖前，请先查看[云端环境参考](cloud-environments.md)，确认已有软件和版本。

Python、Node.js 等用于执行任务代码；智能体的 `runtime` 则选择 `claudecode`、`codex` 或 `native` 执行引擎。不同智能体可以共用一份云端环境配置。

### 添加软件包

`packages` 会在创建或更新环境时预装。包管理器配置支持两种写法：

* 字符串数组，例如 `pip: ["pandas==2.2.0"]`。
* 名称到版本的对象，例如 `pip: {"pandas": "2.2.0"}`。

响应统一返回名称到版本的对象；未指定版本时，由包管理器解析版本。

cURL

```bash
environment=$(curl --request POST "$AGENTAPI_BASE_URL/v1/environments" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --data @- <<'EOF'
{
  "name": "data-analysis",
  "config": {
    "type": "cloud",
    "packages": {
      "pip": {"pandas": "", "numpy": "", "scikit-learn": ""},
      "npm": {"express": ""}
    },
    "networking": {"type": "unrestricted"}
  }
}
EOF
)
```

Python

```python
environment = client.beta.environments.create(
    name="data-analysis",
    config={
        "type": "cloud",
        "packages": {
            "pip": ["pandas", "numpy", "scikit-learn"],
            "npm": ["express"],
        },
        "networking": {"type": "unrestricted"},
    },
)
```

TypeScript

````typescript
const environment = await client.beta.environments.create({
  name: "data-analysis",
  config: {
    type: "cloud",
    packages: {
      pip: ["pandas", "numpy", "scikit-learn"],
      npm: ["express"]
    },
    networking: { type: "unrestricted" }
  }
});
```csharp
```csharp
using Anthropic.Models.Beta.Environments;

var environment = await client.Beta.Environments.Create(new()
{
    Name = "data-analysis",
    Config = new BetaCloudConfigParams
    {
        Packages = new()
        {
            Pip = ["pandas", "numpy", "scikit-learn"],
            Npm = ["express"],
        },
        Networking = new BetaUnrestrictedNetwork(),
    },
});
````

Go

```go
environment, err := client.Beta.Environments.New(ctx, anthropic.BetaEnvironmentNewParams{
	Name: "data-analysis",
	Config: anthropic.BetaEnvironmentNewParamsConfigUnion{
		OfCloud: &anthropic.BetaCloudConfigParams{
			Packages: anthropic.BetaPackagesParams{
				Pip: []string{"pandas", "numpy", "scikit-learn"},
				Npm: []string{"express"},
			},
			Networking: anthropic.BetaCloudConfigParamsNetworkingUnion{
				OfUnrestricted: &anthropic.BetaUnrestrictedNetworkParam{},
			},
		},
	},
})
if err != nil {
	panic(err)
}
_ = environment
```

Java

```java
import com.anthropic.models.beta.environments.*;
import java.util.List;

var environment = client.beta().environments().create(EnvironmentCreateParams.builder()
    .name("data-analysis")
    .config(BetaCloudConfigParams.builder()
        .packages(BetaPackagesParams.builder()
            .pip(List.of("pandas", "numpy", "scikit-learn"))
            .npm(List.of("express"))
            .build())
        .networking(BetaUnrestrictedNetwork.builder().build())
        .build())
    .build());
```

PHP

```php
$environment = $client->beta->environments->create(
    name: 'data-analysis',
    config: [
        'type' => 'cloud',
        'packages' => [
            'pip' => ['pandas', 'numpy', 'scikit-learn'],
            'npm' => ['express'],
        ],
        'networking' => ['type' => 'unrestricted'],
    ],
);
```

Ruby

```ruby
environment = client.beta.environments.create(
  name: "data-analysis",
  config: {
    type: "cloud",
    packages: {
      pip: %w[pandas numpy scikit-learn],
      npm: %w[express]
    },
    networking: {type: "unrestricted"}
  }
)
```

支持的包管理器：

| 字段      | 包管理器           | 示例                                          |
| ------- | -------------- | ------------------------------------------- |
| `apt`   | 系统软件包（apt-get） | `"graphviz"`                                |
| `cargo` | Rust（cargo）    | `"hyperfine@1.18.0"`                        |
| `gem`   | Ruby（gem）      | `"rails:7.1.0"`                             |
| `go`    | Go 模块          | `"golang.org/x/tools/cmd/goimports@latest"` |
| `npm`   | Node.js（npm）   | `"express@4.18.0"`                          |
| `pip`   | Python（pip）    | `"sqlalchemy==2.0.30"`                      |

新安装的 Go 工具放在 `/usr/local/bin`，不依赖会话的 HOME。基础模板需要支持 root 或免密 sudo。已有模板不会自动迁移；若历史工具只在 `/home/user/go/bin`，使用原绝对路径，或在新环境中重新安装。

### 网络

`networking` 控制会话沙箱的出站网络访问：

| 模式             | 行为                                   |
| -------------- | ------------------------------------ |
| `unrestricted` | 允许出站互联网访问，这是默认值。                     |
| `limited`      | 默认拒绝出站访问，仅放行 `allowed_hosts` 中列出的主机。 |

省略 `networking` 时使用默认值 `unrestricted`。仅当需要收紧网络访问时才需显式设置；使用 `limited` 时通过 `allowed_hosts` 声明允许访问的主机：

```json
{
  "type": "cloud",
  "networking": {
    "type": "limited",
    "allowed_hosts": ["mcp.linear.app", "api.github.com"],
    "allow_mcp_servers": true,
    "allow_package_managers": false
  }
}
```

`allow_package_managers` 仅保存并回显，不单独控制包管理器的网络访问。`allow_mcp_servers` 在 `claudecode` 和 `codex` 中不单独控制沙箱网络；Native 显式设置为 `false` 时，会拒绝 MCP 工具接入。该字段为 `true` 或省略时，仍需满足环境的主机白名单。

`limited` 环境中的 MCP 服务和包管理器源应通过 `allowed_hosts` 放行。Native 的 MCP 连接虽然在 AgentAPI 服务端执行，也会检查这份环境配置；环境网络策略不等同于工具权限或第三方授权。

> 模型服务访问
>
> `limited` 环境会自动放行服务端配置的模型服务主机，保障模型调用；这些主机不会加入环境响应中的 `allowed_hosts`。沙箱基础模板所需的 DNS 解析也由服务端放行。环境网络策略不改变智能体权限或 Vault 密钥注入范围。

## 在会话中使用环境

先[创建智能体](../agent-configuration/agent.md)，再在创建会话时以字符串形式传入环境 ID。下面的 cURL 示例使用智能体页保存的 `AGENT_ID` 和 `AGENT_VERSION`。会话创建成功时尚未执行任务；发送第一条用户消息后，AgentAPI 才会按环境配置准备执行所需的沙箱。

cURL

```bash
session=$(curl --request POST "$AGENTAPI_BASE_URL/v1/sessions" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --data @- <<EOF
{
  "agent": {"type": "agent", "id": "$AGENT_ID", "version": $AGENT_VERSION},
  "environment_id": "$environment_id"
}
EOF
)
```

Python

```python
session = client.beta.sessions.create(
    agent=agent.id,
    environment_id=environment.id,
)
```

TypeScript

````typescript
const session = await client.beta.sessions.create({
  agent: agent.id,
  environment_id: environment.id,
});
```csharp
```csharp
var session = await client.Beta.Sessions.Create(new()
{
    Agent = agent.ID,
    EnvironmentID = environment.ID,
});
````

Go

```go
session, err := client.Beta.Sessions.New(ctx, anthropic.BetaSessionNewParams{
	Agent: anthropic.BetaSessionNewParamsAgentUnion{
		OfString: anthropic.String(agent.ID),
	},
	EnvironmentID: environment.ID,
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
    .build());
```

PHP

```php
$session = $client->beta->sessions->create(
    agent: $agent->id,
    environmentID: $environment->id,
);
```

Ruby

```ruby
session = client.beta.sessions.create(
  agent: agent.id,
  environment_id: environment.id
)
```

## 环境生命周期

* 环境会一直保留，直到被显式归档或删除。归档后环境变为只读，不能用于创建新会话；已在运行的会话不受影响。
* 每个会话都会获得自己的沙箱实例，即使多个会话引用同一个环境也是如此。会话之间不共享文件系统状态。
* 更新环境不会创建新版本。若需要审计配置变更，请在 `metadata` 或业务系统中记录版本信息。

## 管理环境

Portal 的运行环境列表支持按名称或 ID 搜索。关键词会去除首尾空白，按字面量包含关系匹配，并在统计总数和分页前执行。对应接口为 `GET /portal/v1/environments?page_index=1&q=关键词`，`q` 必须与 `page_index` 一起使用；公开的 `GET /v1/environments` 不接受该参数。

cURL

```bash
# 列出环境。page 使用上一次响应中的 next_page 游标。
curl --get "$AGENTAPI_BASE_URL/v1/environments" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --data-urlencode "include_archived=false" \
  --data-urlencode "limit=20"

# 检索特定环境
curl --request GET "$AGENTAPI_BASE_URL/v1/environments/$environment_id" \
  --header "x-api-key: $AGENTAPI_API_KEY"

# 更新环境（只提交需要修改的字段）
curl --request POST "$AGENTAPI_BASE_URL/v1/environments/$environment_id" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --data '{"description":"用于数据分析任务的环境"}'

# 归档环境（只读，现有会话继续运行）
curl --request POST "$AGENTAPI_BASE_URL/v1/environments/$environment_id/archive" \
  --header "x-api-key: $AGENTAPI_API_KEY"

# 删除环境（请先确认没有后续会话需要引用它）
curl --request DELETE "$AGENTAPI_BASE_URL/v1/environments/$environment_id" \
  --header "x-api-key: $AGENTAPI_API_KEY"
```

Python

```python
# 列出环境
environments = client.beta.environments.list()

# 检索特定环境
env = client.beta.environments.retrieve(environment.id)

# 归档环境（只读，现有会话继续运行）
client.beta.environments.archive(environment.id)

# 删除环境（请先确认没有后续会话需要引用它）
client.beta.environments.delete(environment.id)
```

TypeScript

````typescript
// 列出环境
const environments = await client.beta.environments.list();

// 检索特定环境
const env = await client.beta.environments.retrieve(environment.id);

// 归档环境（只读，现有会话继续运行）
await client.beta.environments.archive(environment.id);

// 删除环境（请先确认没有后续会话需要引用它）
await client.beta.environments.delete(environment.id);
```csharp
```csharp
// 列出环境
var environments = await client.Beta.Environments.List();

// 检索特定环境
var env = await client.Beta.Environments.Retrieve(environment.ID);

// 归档环境（只读，现有会话继续运行）
await client.Beta.Environments.Archive(environment.ID);

// 删除环境（请先确认没有后续会话需要引用它）
await client.Beta.Environments.Delete(environment.ID);
````

Go

```go
// 列出环境
environments, err := client.Beta.Environments.List(ctx, anthropic.BetaEnvironmentListParams{})
// ...

// 检索特定环境
env, err := client.Beta.Environments.Get(ctx, environment.ID, anthropic.BetaEnvironmentGetParams{})
// ...

// 归档环境（只读，现有会话继续运行）
_, err = client.Beta.Environments.Archive(ctx, environment.ID, anthropic.BetaEnvironmentArchiveParams{})
// ...

// 删除环境（请先确认没有后续会话需要引用它）
_, err = client.Beta.Environments.Delete(ctx, environment.ID, anthropic.BetaEnvironmentDeleteParams{})
```

Java

```java
// 列出环境
var environments = client.beta().environments().list();
// 检索特定环境
var env = client.beta().environments().retrieve(environment.id());
// 归档环境（只读，现有会话继续运行）
client.beta().environments().archive(environment.id());
// 删除环境（请先确认没有后续会话需要引用它）
client.beta().environments().delete(environment.id());
```

PHP

```php
// 列出环境
$environments = $client->beta->environments->list();
// 检索特定环境
$env = $client->beta->environments->retrieve($environment->id);
// 归档环境（只读，现有会话继续运行）
$client->beta->environments->archive($environment->id);
// 删除环境（请先确认没有后续会话需要引用它）
$client->beta->environments->delete($environment->id);
```

Ruby

```ruby
# 列出环境
environments = client.beta.environments.list

# 检索特定环境
env = client.beta.environments.retrieve(environment.id)

# 归档环境（只读，现有会话继续运行）
client.beta.environments.archive(environment.id)

# 删除环境（请先确认没有后续会话需要引用它）
client.beta.environments.delete(environment.id)
```

## 后续步骤

* [云端环境参考](cloud-environments.md)：查看七牛云端沙箱预装软件和版本。
* [会话](../sessions-and-tasks/session.md)：关联智能体和环境，提交任务输入。
* [文件和产物](../data-and-integrations/files.md)：为会话挂载输入文件，并下载任务产物。
