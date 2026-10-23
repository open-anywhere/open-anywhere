<!-- 来源：https://developer-doc.qiniu.com/products/agentapi/development_guidelines/agentapi-data-and-integrations/13604-agentapi-data-and-integrations-github | 由 sync-docs.py 自动生成，请勿手改 -->

# 访问 GitHub



最近更新时间: 2026-10-14 17:39:34

GitHub 任务应先确定交付结果：读取代码形成审查结论，查询 Issue 等协作信息，还是修改代码并提交 Pull Request。三类任务需要的访问能力不同，仓库挂载和 GitHub MCP 分别提供文件操作与远程 API 调用能力。

## 按任务选择访问能力

按任务选择接入路径：

| 任务                                        | 所需配置                                                        |
| ----------------------------------------- | ----------------------------------------------------------- |
| 读取代码并生成审查结论                               | 给会话添加 `github_repository` 资源，使用能读取目标仓库的令牌。                  |
| 查询 Issue、创建 Pull Request 或调用其他 GitHub API | 给智能体配置 GitHub MCP 工具，并通过会话引用的 Vault 提供 MCP 凭证。              |
| 修改代码并提交 Pull Request                      | 同时配置仓库资源和 GitHub MCP；仓库访问需允许提交和推送，MCP 访问需允许创建 Pull Request。 |

仓库挂载后，`claudecode`、`codex` 或已启用沙箱执行工具的 `native` 在云端沙箱中读取和修改代码。只做代码审查时，可以使用[已创建的智能体](../agent-configuration/agent.md)直接挂载仓库；最终报告可通过[文件交付](files.md)下载。

## 划定读取与写入权限

仓库访问和 MCP 访问分别授权，凭证互不复用：

* 仓库资源用 `authorization_token` 克隆仓库。
* GitHub MCP 用会话引用 Vault 中与服务器 URL 匹配的凭证连接服务。

![GitHub 两条认证路径：仓库挂载用 github\_repository 资源的 authorization\_token 克隆仓库；GitHub MCP 由 AgentAPI 按 mcp\_server\_url 从会话引用的 Vault 匹配凭证注入](https://developer-doc.qiniu.com/docs-assets/products/agentapi/development_guidelines/agentapi-data-and-integrations/13604-agentapi-data-and-integrations-github/FtNFrUCMmpOB0ISbLbGcSSAGjOAN-26659cb61a1d.webp)

代码审查通常只需读取权限；提交变更才需要写入权限。先限定任务可访问的仓库，再为所需操作授予权限：

* 读取私有仓库：至少需要仓库内容的读取权限。
* 提交和推送：授予仓库内容的写入权限。
* 创建 Pull Request 或 Issue：再分别授予对应资源的读写权限。

> 说明
>
> 优先使用细粒度个人访问令牌（fine-grained personal access token），将授权限定到任务需要的仓库和操作。

## 配置任务访问

完成[接入准备](../../quick-start/access.md)，从[七牛 MaaS 模型管理页](https://portal.qiniu.com/ai-inference/model)选择可用模型并设置 `AGENTAPI_MODEL_ID`。SDK 客户端初始化见[通过 API 构建](../../quick-start/quickstart.md)。下面示例组合了仓库资源与 GitHub MCP，适用于修改代码后提交 Pull Request 的任务。

### 配置 GitHub MCP

远程协作操作由 GitHub MCP 工具完成。以下智能体使用 `claudecode` 并显式允许工具执行；服务器地址写入智能体，认证凭证留在 Vault。需要应用逐次确认 MCP 操作时，使用 `native` 和 `always_ask`，并处理[工具确认事件](../sessions-and-tasks/events.md)。

cURL

```bash
agent_id=$(curl --request POST "$AGENTAPI_BASE_URL/v1/agents" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --data @- <<JSON | jq -r '.id'
{
  "name": "代码审查助手",
  "runtime": "claudecode",
  "model": {"id": "$AGENTAPI_MODEL_ID"},
  "system": "You are a code review assistant with access to GitHub.",
  "mcp_servers": [
    {
      "type": "url",
      "name": "github",
      "url": "https://api.githubcopilot.com/mcp/"
    }
  ],
  "tools": [
    {"type": "agent_toolset_20260401"},
    {
      "type": "mcp_toolset",
      "mcp_server_name": "github",
      "default_config": {"permission_policy": {"type": "always_allow"}}
    }
  ]
}
JSON
)
```

Python

```python
import os

agent = client.beta.agents.create(
    name="代码审查助手",
    model={"id": os.environ["AGENTAPI_MODEL_ID"]},
    system="You are a code review assistant with access to GitHub.",
    mcp_servers=[
        {
            "type": "url",
            "name": "github",
            "url": "https://api.githubcopilot.com/mcp/",
        },
    ],
    tools=[
        {"type": "agent_toolset_20260401"},
        {
            "type": "mcp_toolset",
            "mcp_server_name": "github",
            "default_config": {"permission_policy": {"type": "always_allow"}},
        },
    ],
    extra_body={"runtime": "claudecode"},
)
```

TypeScript

````typescript
const params = {
  name: "代码审查助手",
  model: { id: process.env.AGENTAPI_MODEL_ID! },
  system: "You are a code review assistant with access to GitHub.",
  mcp_servers: [
    {
      type: "url",
      name: "github",
      url: "https://api.githubcopilot.com/mcp/",
    },
  ],
  tools: [
    { type: "agent_toolset_20260401" },
    {
      type: "mcp_toolset",
      mcp_server_name: "github",
      default_config: { permission_policy: { type: "always_allow" } },
    },
  ]
} satisfies Parameters<typeof client.beta.agents.create>[0];
const agent = await client.beta.agents.create(params, {
  body: { ...params, runtime: "claudecode" },
});
```csharp
```csharp
using Anthropic.Models.Beta.Agents;
using System.Text.Json;
using System.Linq;
using System;
var parameters = new AgentCreateParams
{
    Name = "代码审查助手",
    Model = new BetaManagedAgentsModelConfigParams { ID = Environment.GetEnvironmentVariable("AGENTAPI_MODEL_ID")! },
    System = "You are a code review assistant with access to GitHub.",
    McpServers =
    [
        new() { Type = "url", Name = "github", Url = "https://api.githubcopilot.com/mcp/" },
    ],
    Tools =
    [
        new BetaManagedAgentsAgentToolset20260401Params
        {
            Type = "agent_toolset_20260401",
        },
        new BetaManagedAgentsMcpToolsetParams
        {
            Type = "mcp_toolset",
            McpServerName = "github",
            DefaultConfig = new()
            {
                PermissionPolicy = new BetaManagedAgentsAlwaysAllowPolicy { Type = "always_allow" },
            },
        },
    ],
};
var body = parameters.RawBodyData.ToDictionary(entry => entry.Key, entry => entry.Value);
body["runtime"] = JsonSerializer.SerializeToElement("claudecode");
parameters = AgentCreateParams.FromRawUnchecked(parameters.RawHeaderData, parameters.RawQueryData, body);
var agent = await client.Beta.Agents.Create(parameters);
````

Go

```go
import "github.com/anthropics/anthropic-sdk-go/option"
import "os"
agent, err := client.Beta.Agents.New(ctx, anthropic.BetaAgentNewParams{
	Name: "代码审查助手",
	Model: anthropic.BetaManagedAgentsModelConfigParams{
		ID: os.Getenv("AGENTAPI_MODEL_ID"),
	},
	System: anthropic.String("You are a code review assistant with access to GitHub."),
	MCPServers: []anthropic.BetaManagedAgentsURLMCPServerParams{
		{
			Type: anthropic.BetaManagedAgentsURLMCPServerParamsTypeURL,
			Name: "github",
			URL:  "https://api.githubcopilot.com/mcp/",
		},
	},
	Tools: []anthropic.BetaAgentNewParamsToolUnion{
		{
			OfAgentToolset20260401: &anthropic.BetaManagedAgentsAgentToolset20260401Params{
				Type: anthropic.BetaManagedAgentsAgentToolset20260401ParamsTypeAgentToolset20260401,
			},
		},
		{
			OfMCPToolset: &anthropic.BetaManagedAgentsMCPToolsetParams{
				Type:          anthropic.BetaManagedAgentsMCPToolsetParamsTypeMCPToolset,
				MCPServerName: "github",
			},
		},
	},
}, option.WithJSONSet("runtime", "claudecode"),
    option.WithJSONSet("tools.1.default_config.permission_policy", map[string]string{"type": "always_allow"}))
if err != nil {
	panic(err)
}
```

Java

```java
import com.anthropic.core.JsonValue;
var agent = client.beta().agents().create(AgentCreateParams.builder()
    .name("代码审查助手")
    .model(BetaManagedAgentsModelConfigParams.builder().id(System.getenv("AGENTAPI_MODEL_ID")).build())
    .system("You are a code review assistant with access to GitHub.")
    .addMcpServer(BetaManagedAgentsUrlMcpServerParams.builder()
        .type(BetaManagedAgentsUrlMcpServerParams.Type.URL)
        .name("github")
        .url("https://api.githubcopilot.com/mcp/")
        .build())
    .addTool(BetaManagedAgentsAgentToolset20260401Params.builder()
        .type(BetaManagedAgentsAgentToolset20260401Params.Type.AGENT_TOOLSET_20260401)
        .build())
    .addTool(BetaManagedAgentsMcpToolsetParams.builder()
        .type(BetaManagedAgentsMcpToolsetParams.Type.MCP_TOOLSET)
        .mcpServerName("github")
        .defaultConfig(BetaManagedAgentsMcpToolsetDefaultConfigParams.builder()
            .permissionPolicy(BetaManagedAgentsAlwaysAllowPolicy.builder()
                .type(BetaManagedAgentsAlwaysAllowPolicy.Type.ALWAYS_ALLOW)
                .build())
            .build())
        .build())
    .putAdditionalBodyProperty("runtime", JsonValue.from("claudecode"))
        .build());
```

PHP

```php
$agent = $client->beta->agents->create(
    name: '代码审查助手',
    model: ['id' => getenv('AGENTAPI_MODEL_ID')],
    system: 'You are a code review assistant with access to GitHub.',
    mcpServers: [
        [
            'type' => 'url',
            'name' => 'github',
            'url' => 'https://api.githubcopilot.com/mcp/',
        ],
    ],
    tools: [
        ['type' => 'agent_toolset_20260401'],
        [
            'type' => 'mcp_toolset',
            'mcpServerName' => 'github',
            'defaultConfig' => ['permissionPolicy' => ['type' => 'always_allow']],
        ],
    ],
    requestOptions: ['extraBodyParams' => ['runtime' => 'claudecode']],
);
```

Ruby

```ruby
agent = client.beta.agents.create(
  name: "代码审查助手",
  model: {id: ENV.fetch("AGENTAPI_MODEL_ID")},
  system_: "You are a code review assistant with access to GitHub.",
  mcp_servers: [
    {
      type: "url",
      name: "github",
      url: "https://api.githubcopilot.com/mcp/"
    }
  ],
  tools: [
    {type: "agent_toolset_20260401"},
    {
      type: "mcp_toolset",
      mcp_server_name: "github",
      default_config: {permission_policy: {type: "always_allow"}}
    }
  ],
  request_options: {extra_body: {runtime: "claudecode"}}
)
```

### 挂载仓库并创建会话

会话把仓库、云端环境和 MCP 授权组合成一次任务。先创建[云端环境](../execution-environments/environment.md)；使用上面的智能体时，再按[凭证管理](vaults.md)创建 `static_bearer` 或 `mcp_oauth` 凭证，确保 `mcp_server_url` 与智能体中的 GitHub MCP URL 匹配。

下面的会话用仓库令牌克隆代码，通过 `vault_ids` 提供 MCP 授权。将仓库 URL 和 `GITHUB_TOKEN` 替换为任务目标及其令牌。只挂载仓库时，引用不含 MCP 的智能体并省略 `vault_ids`；AgentAPI 请求仍使用 MaaS API Key。

cURL

```bash
session_id=$(curl --request POST "$AGENTAPI_BASE_URL/v1/sessions" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --data @- <<JSON | jq -r '.id'
{
  "agent": {"type": "agent", "id": "$agent_id", "version": 1},
  "environment_id": "$environment_id",
  "vault_ids": ["$vault_id"],
  "resources": [
    {
      "type": "github_repository",
      "url": "https://github.com/org/repo",
      "mount_path": "/workspace/repo",
      "authorization_token": "$GITHUB_TOKEN"
    }
  ]
}
JSON
)
```

Python

```python
session = client.beta.sessions.create(
    agent=agent.id,
    environment_id=environment.id,
    vault_ids=[vault.id],
    resources=[
        {
            "type": "github_repository",
            "url": "https://github.com/org/repo",
            "mount_path": "/workspace/repo",
            "authorization_token": "ghp_your_github_token",
        },
    ],
)
```

TypeScript

````typescript
const session = await client.beta.sessions.create({
  agent: agent.id,
  environment_id: environment.id,
  vault_ids: [vault.id],
  resources: [
    {
      type: "github_repository",
      url: "https://github.com/org/repo",
      mount_path: "/workspace/repo",
      authorization_token: "ghp_your_github_token",
    },
  ],
});
```csharp
```csharp
var session = await client.Beta.Sessions.Create(new()
{
    Agent = agent.ID,
    EnvironmentID = environment.ID,
    VaultIds = [vault.ID],
    Resources =
    [
        new BetaManagedAgentsGitHubRepositoryResourceParams
        {
            Type = "github_repository",
            Url = "https://github.com/org/repo",
            MountPath = "/workspace/repo",
            AuthorizationToken = "ghp_your_github_token",
        },
    ],
});
````

Go

```go
session, err := client.Beta.Sessions.New(ctx, anthropic.BetaSessionNewParams{
		Agent:         anthropic.BetaSessionNewParamsAgentUnion{OfString: anthropic.String(agent.ID)},
		EnvironmentID: environment.ID,
		VaultIDs:      []string{vault.ID},
	Resources: []anthropic.BetaSessionNewParamsResourceUnion{
		{
			OfGitHubRepository: &anthropic.BetaManagedAgentsGitHubRepositoryResourceParams{
				Type:               anthropic.BetaManagedAgentsGitHubRepositoryResourceParamsTypeGitHubRepository,
				URL:                "https://github.com/org/repo",
				MountPath:          anthropic.String("/workspace/repo"),
				AuthorizationToken: "ghp_your_github_token",
			},
		},
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
    .vaultIds(List.of(vault.id()))
    .addResource(BetaManagedAgentsGitHubRepositoryResourceParams.builder()
        .type(BetaManagedAgentsGitHubRepositoryResourceParams.Type.GITHUB_REPOSITORY)
        .url("https://github.com/org/repo")
        .mountPath("/workspace/repo")
        .authorizationToken("ghp_your_github_token")
        .build())
    .build());
```

PHP

```php
$session = $client->beta->sessions->create(
    agent: $agent->id,
    environmentID: $environment->id,
    vaultIDs: [$vault->id],
    resources: [
        [
            'type' => 'github_repository',
            'url' => 'https://github.com/org/repo',
            'mountPath' => '/workspace/repo',
            'authorizationToken' => 'ghp_your_github_token',
        ],
    ],
);
```

Ruby

```ruby
session = client.beta.sessions.create(
  agent: agent.id,
  environment_id: environment.id,
  vault_ids: [vault.id],
  resources: [
    {
      type: "github_repository",
      url: "https://github.com/org/repo",
      mount_path: "/workspace/repo",
      authorization_token: "ghp_your_github_token"
    }
  ]
)
```

`github_repository` 资源接受以下字段：

| 字段                    | 描述                                                                                                                                                                   |
| --------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `type`                | 必填。必须为 `"github_repository"`。                                                                                                                                        |
| `url`                 | 必填。仓库的 HTTPS 克隆 URL，例如 `https://github.com/&lt;owner&gt;/&lt;repo&gt;`。必须使用 `https`（SSH 等非 https URL 一律拒绝），且主机不能是 localhost 或私网地址；结尾的 `.git` 会自动去除。                  |
| `authorization_token` | 必填。用于克隆仓库的 GitHub 令牌。它不会在 API 响应中回显。请参阅[令牌权限](github.md)。 |
| `mount_path`          | 可选。沙箱内用于克隆仓库的绝对路径，不能为根目录或包含 `..`。默认为 `/workspace/&lt;repo-name&gt;`。                                                                                                 |
| `checkout`            | 可选。要检出的分支（`{"type": "branch", "name": "main"}`）或提交（`{"type": "commit", "sha": "..."}`）。默认为仓库的默认分支。                                                                   |

挂载仓库后，运行时会按自身规则发现仓库级技能：Claude Code 读取 `.claude/skills`，Codex 读取 `.agents/skills`。AgentAPI 不会替运行时扫描这些目录；技能只在会话启动时发现一次，依据是当时检出的仓库状态。请参阅[从 GitHub 仓库加载技能](../agent-configuration/skills.md)。

### 为跨仓库任务配置材料

分析多个项目的接口关系时，为每个仓库添加一个 `resources` 条目。每项资源可以使用独立的 `authorization_token`，例如来自不同 GitHub App installation 的短期 token；任务要求中应明确各仓库的用途和允许修改的范围。

cURL

```bash
resources='[
  {
    "type": "github_repository",
    "url": "https://github.com/org/frontend",
    "mount_path": "/workspace/frontend",
    "authorization_token": "ghp_frontend_repository_token"
  },
  {
    "type": "github_repository",
    "url": "https://github.com/org/backend",
    "mount_path": "/workspace/backend",
    "authorization_token": "ghp_backend_repository_token"
  }
]'
```

Python

```python
resources = [
    {
        "type": "github_repository",
        "url": "https://github.com/org/frontend",
        "mount_path": "/workspace/frontend",
        "authorization_token": "ghp_frontend_repository_token",
    },
    {
        "type": "github_repository",
        "url": "https://github.com/org/backend",
        "mount_path": "/workspace/backend",
        "authorization_token": "ghp_backend_repository_token",
    },
]
```

TypeScript

````typescript
const resources = [
  {
    type: "github_repository",
    url: "https://github.com/org/frontend",
    mount_path: "/workspace/frontend",
    authorization_token: "ghp_frontend_repository_token",
  },
  {
    type: "github_repository",
    url: "https://github.com/org/backend",
    mount_path: "/workspace/backend",
    authorization_token: "ghp_backend_repository_token",
  },
];
```csharp
```csharp
BetaManagedAgentsGitHubRepositoryResourceParams[] resources =
[
    new()
    {
        Type = "github_repository",
        Url = "https://github.com/org/frontend",
        MountPath = "/workspace/frontend",
        AuthorizationToken = "ghp_frontend_repository_token",
    },
    new()
    {
        Type = "github_repository",
        Url = "https://github.com/org/backend",
        MountPath = "/workspace/backend",
        AuthorizationToken = "ghp_backend_repository_token",
    },
];
````

Go

```go
resources := []anthropic.BetaSessionNewParamsResourceUnion{
	{
		OfGitHubRepository: &anthropic.BetaManagedAgentsGitHubRepositoryResourceParams{
			Type:               anthropic.BetaManagedAgentsGitHubRepositoryResourceParamsTypeGitHubRepository,
			URL:                "https://github.com/org/frontend",
			MountPath:          anthropic.String("/workspace/frontend"),
			AuthorizationToken: "ghp_frontend_repository_token",
		},
	},
	{
		OfGitHubRepository: &anthropic.BetaManagedAgentsGitHubRepositoryResourceParams{
			Type:               anthropic.BetaManagedAgentsGitHubRepositoryResourceParamsTypeGitHubRepository,
			URL:                "https://github.com/org/backend",
			MountPath:          anthropic.String("/workspace/backend"),
			AuthorizationToken: "ghp_backend_repository_token",
		},
	},
}
```

Java

```java
var resources = List.of(
    BetaManagedAgentsGitHubRepositoryResourceParams.builder()
        .type(BetaManagedAgentsGitHubRepositoryResourceParams.Type.GITHUB_REPOSITORY)
        .url("https://github.com/org/frontend")
        .mountPath("/workspace/frontend")
        .authorizationToken("ghp_frontend_repository_token")
        .build(),
    BetaManagedAgentsGitHubRepositoryResourceParams.builder()
        .type(BetaManagedAgentsGitHubRepositoryResourceParams.Type.GITHUB_REPOSITORY)
        .url("https://github.com/org/backend")
        .mountPath("/workspace/backend")
        .authorizationToken("ghp_backend_repository_token")
        .build());
```

PHP

```php
$resources = [
    [
        'type' => 'github_repository',
        'url' => 'https://github.com/org/frontend',
        'mountPath' => '/workspace/frontend',
        'authorizationToken' => 'ghp_frontend_repository_token',
    ],
    [
        'type' => 'github_repository',
        'url' => 'https://github.com/org/backend',
        'mountPath' => '/workspace/backend',
        'authorizationToken' => 'ghp_backend_repository_token',
    ],
];
```

Ruby

```ruby
resources = [
  {
    type: "github_repository",
    url: "https://github.com/org/frontend",
    mount_path: "/workspace/frontend",
    authorization_token: "ghp_frontend_repository_token"
  },
  {
    type: "github_repository",
    url: "https://github.com/org/backend",
    mount_path: "/workspace/backend",
    authorization_token: "ghp_backend_repository_token"
  }
]
```

## 提交变更并验收结果

配置完成后，在任务要求中写清变更目标、验证要求和交付形式。以下示例要求修复类型错误并创建拉取请求：智能体修改代码、创建分支、提交并推送，再调用 GitHub MCP。执行前需具备仓库写入权限和 Pull Request 权限，见权限配置。

发送消息成功只表示任务已接收。继续读取[事件流](../sessions-and-tasks/events.md)，检查代码修改、验证命令和远程操作是否出错；任务结束后打开返回的 Pull Request 链接，核对目标仓库、分支和变更内容。只要求审查报告的任务则检查报告文件，无需执行推送和创建 Pull Request。

cURL

```bash
curl -fsS "$AGENTAPI_BASE_URL/v1/sessions/$session_id/events" \
  -H "x-api-key: $AGENTAPI_API_KEY" \
  -H "content-type: application/json" \
  -o /dev/null \
  --data @- <<JSON
{
  "events": [
    {
      "type": "user.message",
      "input": {"parts": [
        {
          "type": "text",
          "text": "修复 src/utils.ts 中的类型错误，运行相关测试，将变更提交到新分支并推送，然后创建拉取请求，在最终回复中返回链接。"
        }
      ]}
    }
  ]
}
JSON
```

Python

```python
client.beta.sessions.events.send(
    session.id,
    events=[
        {
            "type": "user.message",
            "content": [
                {
                    "type": "text",
                    "text": "修复 src/utils.ts 中的类型错误，运行相关测试，将变更提交到新分支并推送，然后创建拉取请求，在最终回复中返回链接。",
                },
            ],
        },
    ],
)
```

TypeScript

````typescript
await client.beta.sessions.events.send(session.id, {
  events: [
    {
      type: "user.message",
      content: [
        {
          type: "text",
          text: "修复 src/utils.ts 中的类型错误，运行相关测试，将变更提交到新分支并推送，然后创建拉取请求，在最终回复中返回链接。",
        },
      ],
    },
  ],
});
```csharp
```csharp
await client.Beta.Sessions.Events.Send(session.ID, new()
{
    Events =
    [
        new BetaManagedAgentsUserMessageEventParams
        {
            Type = "user.message",
            Content =
            [
                new BetaManagedAgentsTextBlock
                {
                    Type = "text",
                    Text = "修复 src/utils.ts 中的类型错误，运行相关测试，将变更提交到新分支并推送，然后创建拉取请求，在最终回复中返回链接。",
                },
            ],
        },
    ],
});
````

Go

```go
_, err = client.Beta.Sessions.Events.Send(ctx, session.ID, anthropic.BetaSessionEventSendParams{
	Events: []anthropic.BetaManagedAgentsEventParamsUnion{
		{
			OfUserMessage: &anthropic.BetaManagedAgentsUserMessageEventParams{
				Type: anthropic.BetaManagedAgentsUserMessageEventParamsTypeUserMessage,
				Content: []anthropic.BetaManagedAgentsUserMessageEventParamsContentUnion{
					{
						OfText: &anthropic.BetaManagedAgentsTextBlockParam{
							Type: anthropic.BetaManagedAgentsTextBlockTypeText,
							Text: "修复 src/utils.ts 中的类型错误，运行相关测试，将变更提交到新分支并推送，然后创建拉取请求，在最终回复中返回链接。",
						},
					},
				},
			},
		},
	},
})
if err != nil {
	panic(err)
}
```

Java

```java
client.beta().sessions().events().send(session.id(), EventSendParams.builder()
    .addEvent(BetaManagedAgentsUserMessageEventParams.builder()
        .type(BetaManagedAgentsUserMessageEventParams.Type.USER_MESSAGE)
        .addContent(BetaManagedAgentsTextBlock.builder()
            .type(BetaManagedAgentsTextBlock.Type.TEXT)
            .text("修复 src/utils.ts 中的类型错误，运行相关测试，将变更提交到新分支并推送，然后创建拉取请求，在最终回复中返回链接。")
            .build())
        .build())
    .build());
```

PHP

```php
$client->beta->sessions->events->send(
    $session->id,
    events: [
        [
            'type' => 'user.message',
            'content' => [
                [
                    'type' => 'text',
                    'text' => '修复 src/utils.ts 中的类型错误，运行相关测试，将变更提交到新分支并推送，然后创建拉取请求，在最终回复中返回链接。',
                ],
            ],
        ],
    ],
);
```

Ruby

```ruby
client.beta.sessions.events.send_(
  session.id,
  events: [
    {
      type: "user.message",
      content: [
        {
          type: "text",
          text: "修复 src/utils.ts 中的类型错误，运行相关测试，将变更提交到新分支并推送，然后创建拉取请求，在最终回复中返回链接。"
        }
      ]
    }
  ]
)
```

## 维护后续任务的授权

复用会话继续工作时，先确认仓库令牌仍可用于后续操作。需要轮换时，通过 `resources.list` 找到目标仓库的资源 `id`，再更新其授权令牌；会话创建响应也会返回该 ID。

* 更新只影响选中的仓库，不会覆盖其他仓库的 token。
* 复用会话不会自动轮换 token。
* 仓库在会话生命周期内保持挂载；更换仓库需创建新会话。

cURL

```bash
# 列出会话上的资源
repo_resource_id=$(curl --request GET "$AGENTAPI_BASE_URL/v1/sessions/$session_id/resources" \
  --header "x-api-key: $AGENTAPI_API_KEY" | jq -r '.data[] | select(.type == "github_repository") | .id')
echo "$repo_resource_id"  # "sr_01ABC..."

# 轮换授权令牌
curl --request POST "$AGENTAPI_BASE_URL/v1/sessions/$session_id/resources/$repo_resource_id" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --output /dev/null \
  --data @- <<JSON
{
  "authorization_token": "ghp_your_new_github_token"
}
JSON
```

Python

```python
# 列出会话上的资源
listed = client.beta.sessions.resources.list(session.id)
repo_resource_id = listed.data[0].id
print(repo_resource_id)  # "sr_01ABC..."

# 轮换授权令牌
client.beta.sessions.resources.update(
    repo_resource_id,
    session_id=session.id,
    authorization_token="ghp_your_new_github_token",
)
```

TypeScript

````typescript
// 列出会话上的资源
const listed = await client.beta.sessions.resources.list(session.id);
const repoResource = listed.data.find(
  (entry) => entry.type === "github_repository",
);
if (!repoResource) {
  throw new Error("No GitHub repository resource on the session");
}
const repoResourceId = repoResource.id;
console.log(repoResourceId); // "sr_01ABC..."

// 轮换授权令牌
await client.beta.sessions.resources.update(repoResourceId, {
  session_id: session.id,
  authorization_token: "ghp_your_new_github_token",
});
```csharp
```csharp
// 列出会话上的资源
var listed = await client.Beta.Sessions.Resources.List(session.ID);
var repoResourceId = (await listed.Paginate().FirstAsync()).ID;
Console.WriteLine(repoResourceId); // "sr_01ABC..."

// 轮换授权令牌
await client.Beta.Sessions.Resources.Update(repoResourceId, new()
{
    SessionID = session.ID,
    AuthorizationToken = "ghp_your_new_github_token",
});
````

Go

```go
// 列出会话上的资源
listed, err := client.Beta.Sessions.Resources.List(ctx, session.ID, anthropic.BetaSessionResourceListParams{})
if err != nil {
	panic(err)
}
repoResourceID := listed.Data[0].ID
fmt.Println(repoResourceID) // "sr_01ABC..."

// 轮换授权令牌
_, err = client.Beta.Sessions.Resources.Update(ctx, repoResourceID, anthropic.BetaSessionResourceUpdateParams{
	SessionID:          session.ID,
	AuthorizationToken: "ghp_your_new_github_token",
})
if err != nil {
	panic(err)
}
```

Java

```java
// 列出会话上的资源
var listed = client.beta().sessions().resources().list(session.id());
var repoResourceId = listed.data().getFirst().asGitHubRepository().id();
IO.println(repoResourceId);  // "sr_01ABC..."

// 轮换授权令牌
client.beta().sessions().resources().update(repoResourceId, ResourceUpdateParams.builder()
    .sessionId(session.id())
    .authorizationToken("ghp_your_new_github_token")
    .build());
```

PHP

```php
// 列出会话上的资源
$listed = $client->beta->sessions->resources->list($session->id);
$repoResourceId = $listed->data[0]->id;
echo $repoResourceId, PHP_EOL; // "sr_01ABC..."

// 轮换授权令牌
$client->beta->sessions->resources->update(
    $repoResourceId,
    sessionID: $session->id,
    authorizationToken: 'ghp_your_new_github_token',
);
```

Ruby

```ruby
# 列出会话上的资源
listed = client.beta.sessions.resources.list(session.id)
repo_resource_id = listed.data.first.id
puts repo_resource_id # "sr_01ABC..."

# 轮换授权令牌
client.beta.sessions.resources.update(
  repo_resource_id,
  session_id: session.id,
  authorization_token: "ghp_your_new_github_token"
)
```

## 后续步骤

* [事件和事件流](../sessions-and-tasks/events.md)：跟踪代码任务执行，处理错误与工具确认

* [MCP 连接器](../agent-configuration/mcp.md)：配置远程工具与连接授权

* [附加和下载文件](files.md)：补充参考材料并下载审查报告
