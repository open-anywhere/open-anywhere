<!-- 来源：https://developer-doc.qiniu.com/products/agentapi/development_guidelines/agentapi-data-and-integrations/13606-agentapi-data-and-integrations-vaults | 由 sync-docs.py 自动生成，请勿手改 -->

# 凭证管理



最近更新时间: 2026-10-22 15:09:20

智能体配置决定使用哪些工具，Vault（保管库）决定访问第三方 MCP 服务时使用谁的身份。两者分别配置：同一智能体可以处理多个业务用户的任务，由应用在创建会话时选择对应的 `vault_ids`。

例如，应用为不同业务用户生成协作系统摘要时，应分别保存用户授权，并在每次创建会话前校验授权归属。不要把第三方令牌放进任务消息或系统提示，也不要让客户端直接决定使用哪个 Vault。

在 Portal 中，您可以从会话详情的凭证库入口查看关联的 Vault 名称、ID 和归档状态，并进入凭证库详情。任务详情显示当前配置的凭证库；运行历史则显示该次会话的关联，后续修改任务配置不会改变历史关联 ID。信息暂时不可用时，页面保留 Vault ID 并提供重试。

## 划分调用身份与业务授权

| 配置            | 负责的身份               | 应用需要控制的边界                                                              |
| ------------- | ------------------- | ---------------------------------------------------------------------- |
| MaaS API Key  | 调用 AgentAPI 的账户     | 保留在应用服务端，见[接入准备](../../quick-start/access.md) |
| 智能体的 MCP 配置   | 任务可使用的服务与工具         | 只声明任务需要的服务；声明服务器不代表已经获得第三方授权                                           |
| Vault 中的第三方凭证 | 访问 MCP 服务的业务用户或服务身份 | 维护用户与 Vault 的映射，创建会话前检查归属与权限                                           |

> 账户内的访问范围
>
> Vault 和凭证属于当前账户，账户内有权访问这些资源的 API Key 可以引用它们。`metadata` 中的业务用户 ID 只是标记，不形成用户级访问隔离。应用服务端必须校验客户端发起的任务与允许使用的 Vault，不能将 `vault_ids` 或 `external_user_id` 直接作为授权依据。

## 确定会话的访问范围

创建会话前，核对三个条件：

1. 服务地址一致。 MCP 凭证通过 `mcp_server_url` 匹配智能体配置的服务地址；没有匹配凭证时，AgentAPI 会尝试匿名连接。
2. 第三方权限满足任务。 摘要查询通常只需读取权限；发布消息等写操作需另行授权。Vault 保存令牌，不会缩小令牌在第三方系统的权限。
3. 授权属于当前任务。 根据应用服务端的用户映射选出 Vault，再将 ID 传入会话；智能体配置可以复用，用户授权不能混用。

一个会话最多引用 50 个 Vault，执行时最多匹配并注入 20 个凭证。多个 Vault 匹配同一 MCP 地址时，`vault_ids` 中第一个匹配的 Vault 优先；应避免用数组顺序掩盖重复授权配置。

![保管库凭证注入 MCP 请求的流程](https://developer-doc.qiniu.com/docs-assets/products/agentapi/development_guidelines/agentapi-data-and-integrations/13606-agentapi-data-and-integrations-vaults/FpGzsu4EkTrIjHJQheWhGSnjfmBT-caa7331e05ae.webp)

## 配置并使用授权

下面的示例按“保存业务用户标识 → 添加第三方令牌 → 创建会话引用”的顺序配置授权。示例中的 Alice 是业务用户标记，不是 AgentAPI 创建的独立安全主体。

### 创建保管库并关联业务用户

为业务用户或服务身份创建 Vault，保存返回的 ID，并在应用自己的授权记录中关联该 ID。`display_name` 用于识别，`metadata` 用于检索与标记。

cURL

```bash
vault_id=$(curl --request POST "$AGENTAPI_BASE_URL/v1/vaults" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --data @- <<'EOF' | jq -r '.id'
{
  "display_name": "Alice",
  "metadata": {"external_user_id": "usr_abc123"}
}
EOF
)
echo "$vault_id"  # "vlt_01ABC..."
```

Python

```python
vault = client.beta.vaults.create(
    display_name="Alice",
    metadata={"external_user_id": "usr_abc123"},
)
print(vault.id)  # "vlt_01ABC..."
```

TypeScript

````typescript
const vault = await client.beta.vaults.create({
  display_name: "Alice",
  metadata: { external_user_id: "usr_abc123" },
});
console.log(vault.id); // "vlt_01ABC..."
```csharp
```csharp
var vault = await client.Beta.Vaults.Create(new()
{
    DisplayName = "Alice",
    Metadata = new Dictionary<string, string> { ["external_user_id"] = "usr_abc123" },
});
Console.WriteLine(vault.ID); // "vlt_01ABC..."
````

Go

```go
vault, err := client.Beta.Vaults.New(ctx, anthropic.BetaVaultNewParams{
	DisplayName: "Alice",
	Metadata:    map[string]string{"external_user_id": "usr_abc123"},
})
if err != nil {
	panic(err)
}
fmt.Println(vault.ID) // "vlt_01ABC..."
```

Java

```java
var vault = client.beta().vaults().create(VaultCreateParams.builder()
    .displayName("Alice")
    .metadata(VaultCreateParams.Metadata.builder()
        .putAdditionalProperty("external_user_id", JsonValue.from("usr_abc123"))
        .build())
    .build());
IO.println(vault.id()); // "vlt_01ABC..."
```

PHP

```php
$vault = $client->beta->vaults->create(
    displayName: 'Alice',
    metadata: ['external_user_id' => 'usr_abc123'],
);
echo $vault->id . "\n"; // "vlt_01ABC..."
```

Ruby

```ruby
vault = client.beta.vaults.create(
  display_name: "Alice",
  metadata: {external_user_id: "usr_abc123"}
)
puts vault.id # "vlt_01ABC..."
```

响应返回完整的 Vault 记录：

```json
{
  "type": "vault",
  "id": "vlt_01ABC...",
  "display_name": "Alice",
  "metadata": { "external_user_id": "usr_abc123" },
  "created_at": "2026-03-18T10:00:00Z",
  "updated_at": "2026-03-18T10:00:00Z",
  "archived_at": null
}
```

### 选择第三方鉴权方式

鉴权方式由 MCP 服务决定，不由智能体模型决定：

| MCP 服务接受的授权                                        | 凭证类型                   | 令牌维护责任                              |
| -------------------------------------------------- | ---------------------- | ----------------------------------- |
| 业务用户通过 OAuth 授予的访问令牌                               | `mcp_oauth`            | 应用完成授权与刷新，再更新 Vault 中的令牌            |
| 固定 API Key 或个人访问令牌，以 Bearer 方式发送                   | `static_bearer`        | 应用根据第三方令牌有效期安排轮换                    |
| 沙箱进程把密钥原样放进 `Authorization: Bearer $变量`，并访问预先列出的主机 | `environment_variable` | 应用提供密钥和主机；AgentAPI 只在匹配的出站请求头中替换完整值 |

`mcp_oauth` 和 `static_bearer` 都以 `mcp_server_url` 匹配目标服务；`environment_variable` 以 `secret_name` 和 `networking.allowed_hosts` 限定替换范围，配置见下文“环境变量密钥”。凭证值（`token`、`access_token`、`refresh_token`、`client_secret`、`secret_value`）只写入、不在 API 响应中返回；应用应保存凭证 ID，用于后续更新或撤销。

#### 保存 OAuth 授权

下面的示例保存业务用户的访问令牌及刷新信息。保存 `refresh` 配置不会启动自动刷新：AgentAPI 当前不执行 OAuth 刷新，应用必须在令牌过期后完成刷新并更新凭证。

`refresh.token_endpoint_auth.type` 字段指定令牌刷新请求的认证方式：

* `none`：公共客户端
* `client_secret_basic`：使用客户端密钥的 HTTP Basic 身份验证
* `client_secret_post`：客户端密钥位于 POST 请求体中

cURL

```bash
credential_id=$(curl --fail-with-body -sS "$AGENTAPI_BASE_URL/v1/vaults/$vault_id/credentials" \
  -H "x-api-key: $AGENTAPI_API_KEY" \
  -H "content-type: application/json" \
  --data @- <<'EOF' | jq -r '.id'
{
  "display_name": "Alice's Slack",
  "auth": {
    "type": "mcp_oauth",
    "mcp_server_url": "https://mcp.slack.com/mcp",
    "access_token": "xoxp-...",
    "expires_at": "2099-12-31T23:59:59Z",
    "refresh": {
      "token_endpoint": "https://slack.com/api/oauth.v2.access",
      "client_id": "1234567890.0987654321",
      "scope": "channels:read chat:write",
      "refresh_token": "xoxe-1-...",
      "token_endpoint_auth": {"type": "client_secret_post", "client_secret": "abc123..."}
    }
  }
}
EOF
)
```

Python

```python
credential = client.beta.vaults.credentials.create(
    vault_id=vault.id,
    display_name="Alice's Slack",
    auth={
        "type": "mcp_oauth",
        "mcp_server_url": "https://mcp.slack.com/mcp",
        "access_token": "xoxp-...",
        "expires_at": "2099-12-31T23:59:59Z",
        "refresh": {
            "token_endpoint": "https://slack.com/api/oauth.v2.access",
            "client_id": "1234567890.0987654321",
            "scope": "channels:read chat:write",
            "refresh_token": "xoxe-1-...",
            "token_endpoint_auth": {"type": "client_secret_post", "client_secret": "abc123..."},
        },
    },
)
```

TypeScript

````typescript
const credential = await client.beta.vaults.credentials.create(vault.id, {
  display_name: "Alice's Slack",
  auth: {
    type: "mcp_oauth",
    mcp_server_url: "https://mcp.slack.com/mcp",
    access_token: "xoxp-...",
    expires_at: "2099-12-31T23:59:59Z",
    refresh: {
      token_endpoint: "https://slack.com/api/oauth.v2.access",
      client_id: "1234567890.0987654321",
      scope: "channels:read chat:write",
      refresh_token: "xoxe-1-...",
      token_endpoint_auth: {
        type: "client_secret_post",
        client_secret: "abc123...",
      },
    },
  },
});
```csharp
```csharp
var credential = await client.Beta.Vaults.Credentials.Create(vault.ID, new()
{
    DisplayName = "Alice's Slack",
    Auth = new BetaManagedAgentsMcpOAuthCreateParams
    {
        Type = BetaManagedAgentsMcpOAuthCreateParamsType.McpOAuth,
        McpServerUrl = "https://mcp.slack.com/mcp",
        AccessToken = "xoxp-...",
        ExpiresAt = DateTimeOffset.Parse("2099-12-31T23:59:59Z"),
        Refresh = new()
        {
            TokenEndpoint = "https://slack.com/api/oauth.v2.access",
            ClientID = "1234567890.0987654321",
            Scope = "channels:read chat:write",
            RefreshToken = "xoxe-1-...",
            TokenEndpointAuth = new BetaManagedAgentsTokenEndpointAuthPostParam
            {
                Type = BetaManagedAgentsTokenEndpointAuthPostParamType.ClientSecretPost,
                ClientSecret = "abc123...",
            },
        },
    },
});
````

Go

```go
credential, err := client.Beta.Vaults.Credentials.New(ctx, vault.ID, anthropic.BetaVaultCredentialNewParams{
	DisplayName: anthropic.String("Alice's Slack"),
	Auth: anthropic.BetaVaultCredentialNewParamsAuthUnion{
		OfMCPOAuth: &anthropic.BetaManagedAgentsMCPOAuthCreateParams{
			Type:         anthropic.BetaManagedAgentsMCPOAuthCreateParamsTypeMCPOAuth,
			MCPServerURL: "https://mcp.slack.com/mcp",
			AccessToken:  "xoxp-...",
			ExpiresAt:    anthropic.Time(time.Date(2099, time.December, 31, 23, 59, 59, 0, time.UTC)),
			Refresh: anthropic.BetaManagedAgentsMCPOAuthRefreshParams{
				TokenEndpoint: "https://slack.com/api/oauth.v2.access",
				ClientID:      "1234567890.0987654321",
				Scope:         anthropic.String("channels:read chat:write"),
				RefreshToken:  "xoxe-1-...",
				TokenEndpointAuth: anthropic.BetaManagedAgentsMCPOAuthRefreshParamsTokenEndpointAuthUnion{
					OfClientSecretPost: &anthropic.BetaManagedAgentsTokenEndpointAuthPostParam{
						Type:         anthropic.BetaManagedAgentsTokenEndpointAuthPostParamTypeClientSecretPost,
						ClientSecret: "abc123...",
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
var credential = client.beta().vaults().credentials().create(vault.id(),
    CredentialCreateParams.builder()
        .displayName("Alice's Slack")
        .auth(BetaManagedAgentsMcpOAuthCreateParams.builder()
            .type(BetaManagedAgentsMcpOAuthCreateParams.Type.MCP_OAUTH)
            .mcpServerUrl("https://mcp.slack.com/mcp")
            .accessToken("xoxp-...")
            .expiresAt(OffsetDateTime.parse("2099-12-31T23:59:59Z"))
            .refresh(BetaManagedAgentsMcpOAuthRefreshParams.builder()
                .tokenEndpoint("https://slack.com/api/oauth.v2.access")
                .clientId("1234567890.0987654321")
                .scope("channels:read chat:write")
                .refreshToken("xoxe-1-...")
                .clientSecretPostTokenEndpointAuth("abc123...")
                .build())
            .build())
        .build());
```

PHP

```php
$credential = $client->beta->vaults->credentials->create(
    vaultID: $vault->id,
    displayName: "Alice's Slack",
    auth: ManagedAgentsMCPOAuthCreateParams::with(
        type: 'mcp_oauth',
        mcpServerURL: 'https://mcp.slack.com/mcp',
        accessToken: 'xoxp-...',
        expiresAt: new DateTimeImmutable('2099-12-31T23:59:59Z'),
        refresh: ManagedAgentsMCPOAuthRefreshParams::with(
            tokenEndpoint: 'https://slack.com/api/oauth.v2.access',
            clientID: '1234567890.0987654321',
            scope: 'channels:read chat:write',
            refreshToken: 'xoxe-1-...',
            tokenEndpointAuth: ManagedAgentsTokenEndpointAuthPostParam::with(
                type: 'client_secret_post',
                clientSecret: 'abc123...',
            ),
        ),
    ),
);
```

Ruby

```ruby
credential = client.beta.vaults.credentials.create(
  vault.id,
  display_name: "Alice's Slack",
  auth: {
    type: "mcp_oauth",
    mcp_server_url: "https://mcp.slack.com/mcp",
    access_token: "xoxp-...",
    expires_at: "2099-12-31T23:59:59Z",
    refresh: {
      token_endpoint: "https://slack.com/api/oauth.v2.access",
      client_id: "1234567890.0987654321",
      scope: "channels:read chat:write",
      refresh_token: "xoxe-1-...",
      token_endpoint_auth: {
        type: "client_secret_post",
        client_secret: "abc123..."
      }
    }
  }
)
```

#### 保存固定 Bearer 令牌

固定令牌不需要 OAuth 刷新配置。下面的示例将令牌绑定到一个 MCP 地址；如服务地址发生变化，应创建新的凭证，而非只更新令牌值。

cURL

```bash
curl --fail-with-body -sS "$AGENTAPI_BASE_URL/v1/vaults/$vault_id/credentials" \
  -H "x-api-key: $AGENTAPI_API_KEY" \
  -H "content-type: application/json" \
  --data @- <<'EOF'
{
  "display_name": "Linear API key",
  "auth": {
    "type": "static_bearer",
    "mcp_server_url": "https://mcp.linear.app/mcp",
    "token": "lin_api_your_linear_key"
  }
}
EOF
```

Python

```python
bearer_credential = client.beta.vaults.credentials.create(
    vault_id=vault.id,
    display_name="Linear API key",
    auth={
        "type": "static_bearer",
        "mcp_server_url": "https://mcp.linear.app/mcp",
        "token": "lin_api_your_linear_key",
    },
)
```

TypeScript

````typescript
const bearerCredential = await client.beta.vaults.credentials.create(vault.id, {
  display_name: "Linear API key",
  auth: {
    type: "static_bearer",
    mcp_server_url: "https://mcp.linear.app/mcp",
    token: "lin_api_your_linear_key",
  },
});
```csharp
```csharp
var bearerCredential = await client.Beta.Vaults.Credentials.Create(vault.ID, new()
{
    DisplayName = "Linear API key",
    Auth = new BetaManagedAgentsStaticBearerCreateParams
    {
        Type = BetaManagedAgentsStaticBearerCreateParamsType.StaticBearer,
        McpServerUrl = "https://mcp.linear.app/mcp",
        Token = "lin_api_your_linear_key",
    },
});
````

Go

```go
bearerCredential, err := client.Beta.Vaults.Credentials.New(ctx, vault.ID, anthropic.BetaVaultCredentialNewParams{
	DisplayName: anthropic.String("Linear API key"),
	Auth: anthropic.BetaVaultCredentialNewParamsAuthUnion{
		OfStaticBearer: &anthropic.BetaManagedAgentsStaticBearerCreateParams{
			Type:         anthropic.BetaManagedAgentsStaticBearerCreateParamsTypeStaticBearer,
			MCPServerURL: "https://mcp.linear.app/mcp",
			Token:        "lin_api_your_linear_key",
		},
	},
})
if err != nil {
	panic(err)
}
_ = bearerCredential
```

Java

```java
var bearerCredential = client.beta().vaults().credentials().create(vault.id(),
    CredentialCreateParams.builder()
        .displayName("Linear API key")
        .auth(BetaManagedAgentsStaticBearerCreateParams.builder()
            .type(BetaManagedAgentsStaticBearerCreateParams.Type.STATIC_BEARER)
            .mcpServerUrl("https://mcp.linear.app/mcp")
            .token("lin_api_your_linear_key")
            .build())
        .build());
```

PHP

```php
$bearerCredential = $client->beta->vaults->credentials->create(
    vaultID: $vault->id,
    displayName: 'Linear API key',
    auth: ManagedAgentsStaticBearerCreateParams::with(
        type: 'static_bearer',
        mcpServerURL: 'https://mcp.linear.app/mcp',
        token: 'lin_api_your_linear_key',
    ),
);
```

Ruby

```ruby
bearer_credential = client.beta.vaults.credentials.create(
  vault.id,
  display_name: "Linear API key",
  auth: {
    type: "static_bearer",
    mcp_server_url: "https://mcp.linear.app/mcp",
    token: "lin_api_your_linear_key"
  }
)
```

### 保存环境变量密钥

沙箱里的 CLI、SDK 或脚本如果把密钥原样放进 `Authorization: Bearer <密钥>`，可以用 `environment_variable` 凭证。沙箱进程只能看到占位符；真实密钥只出现在离开沙箱、且请求头完整匹配的出站请求里。自托管 worker 不接收这个占位符。

创建时必须显式指定请求头注入和有限主机。省略 `injection_location` 会返回 400，当前不会把它解释成同时启用请求头和请求体。

cURL

```bash
curl --fail-with-body -sS "$AGENTAPI_BASE_URL/v1/vaults/$vault_id/credentials" \
  -H "x-api-key: $AGENTAPI_API_KEY" \
  -H "content-type: application/json" \
  --data @- <<'EOF'
{
  "display_name": "Notion API key",
  "auth": {
    "type": "environment_variable",
    "secret_name": "NOTION_API_KEY",
    "secret_value": "ntn_your-secret-here",
    "networking": {
      "type": "limited",
      "allowed_hosts": ["api.notion.com"]
    },
    "injection_location": {"header": true}
  }
}
EOF
```

响应不返回 `secret_value` 或 `mcp_server_url`。`injection_location` 固定为 `{"header": true, "body": false}`，`allowed_hosts` 返回规范化后的主机。

```json
{
  "type": "credential",
  "id": "cred_01ABC",
  "vault_id": "vlt_01ABC",
  "display_name": "Notion API key",
  "auth": {
    "type": "environment_variable",
    "secret_name": "NOTION_API_KEY",
    "networking": {
      "type": "limited",
      "allowed_hosts": ["api.notion.com"]
    },
    "injection_location": {"header": true, "body": false}
  },
  "metadata": {},
  "created_at": "2026-03-18T10:00:00Z",
  "updated_at": "2026-03-18T10:00:00Z",
  "archived_at": null
}
```

沙箱中的变量值是 `agentapi_secret_` 加上凭证 ID，例如 `NOTION_API_KEY=agentapi_secret_cred_01ABC`。出站到 `https://api.notion.com` 时，只有 `Authorization` 的完整值等于 `Bearer agentapi_secret_cred_01ABC`，才会替换成 `Bearer ntn_your-secret-here`。其它请求头、请求体、多出来的前后缀、大小写不同的值，以及未列出的主机，都保持原样。替换地址是 `https://<主机>`，不含路径。七牛沙箱按主机精确匹配，`https://example.com` 不会替换发往 `https://example.com.attacker.com` 的请求。

因此客户端需要读取该环境变量，并把占位符原样放进出站请求头。变量名不决定替换哪个头。请求体、URL 或签名材料不参与替换。

沙箱目前只能替换一个指定请求头，而且匹配值和替换值都必须完整。它还不能在任意请求头或请求体里查找占位符。在沙箱补上这个能力之前，AgentAPI 按运行时临时选择模型请求实际使用的头：Codex 替换完整的 `Authorization: Bearer <占位符>`，Claude Code 替换完整的 `X-Api-Key: <占位符>`。Native 的模型请求不经过沙箱，不写这条注入；AgentAPI 在发出模型请求时按主机匹配后直接替换。这个限制使 Environment Vault 目前主要覆盖模型请求，还不能表达 CMA 里“任意请求头替换”的完整含义。沙箱支持任意请求头替换后，会去掉按运行时选择请求头的临时逻辑。

> 与通用环境变量注入的差异
>
> 当前只支持上面按运行时选择的那一个请求头。请求体注入、不限主机、通配符、端口，以及在任意请求头中扫描占位符，都会被拒绝。凭证上的允许主机只决定替换范围，不开放网络；云端环境仍需允许访问这些主机。

约束：

* `networking.type` 只能是 `limited`。每个主机是不带协议、端口、路径和通配符的主机名或 IP，单个凭证最多 20 个。这些主机和同一会话的其它注入规则共用 20 条上限。IPv6 可以写成 `[2001:db8::1]`，保存后显示为 `2001:db8::1`；更新时可以原样提交这个保存后的地址。
* `secret_name` 是环境变量名，以字母或下划线开头，只含字母、数字和下划线，长度 1 到 128。不保留任何变量名；显式配置的变量优先于运行时默认值，包括 `PATH`、`HOME` 和模型 Key。覆盖路径等进程变量也会改变运行时行为。同一 Vault 的活动凭证里按大小写区分且必须唯一，`API_TOKEN` 和 `api_token` 可以同时存在，完全相同的名称返回 409。
* `secret_name` 和 `networking` 创建后不可修改。轮换时在 `auth` 中携带 `type: "environment_variable"` 和新的 `secret_value`；省略 `auth.type` 会返回 400，无需重传名称、主机和注入位置。占位符不变，出站替换值更新。要改名称或主机，先归档再新建。归档保留名称和允许主机，清除密钥，并释放该活动名称。
* 同一会话的多个 Vault 使用相同 `secret_name` 时，按 `vault_ids` 的顺序保留第一个。每个允许主机占一条沙箱注入规则，并与默认模型鉴权、MCP、GitHub 和自定义工具桥共用每个会话 20 条规则的上限。
* `codex` 和 `claudecode` 在每次启动时读取当前占位符。Native 持久 bash 在启动时先清理继承的凭据，再应用 Vault 占位符并固化环境变量；之后新增的 `secret_name` 需要重启该 shell 才能看到。轮换密钥不需要重启。
* 这种凭证不参与进程内 MCP 目标凭证，也不使用 `mcp_server_url`。

### 覆盖模型凭据

环境变量凭证不按变量名选择模型 Key。`secret_name` 只决定沙箱进程里的占位符变量名。出站替换只看主机和该运行时临时选定的请求头。Codex 在 `allowed_hosts` 命中且 `Authorization` 的完整值等于 `Bearer <占位符>` 时替换。Claude Code 在主机命中且 `X-Api-Key` 的完整值等于占位符时替换。Native 不把这条规则写入沙箱。

`codex` 和 `claudecode` 的默认模型 Key 仍由沙箱按模型服务地址注入，进程里只有 `agentapi_model_key_...` 占位符。默认规则和 Vault 规则使用同一个请求头、不同的占位符，所以可以同时存在。进程如果改发 Vault 占位符，就由 Vault 规则接管该主机上的请求。默认规则始终占用 20 条规则中的一条，不会因为 Vault 里出现某个变量名而停用。Codex 的默认规则写入 `Authorization: Bearer`；Claude Code 的默认规则写入 `X-Api-Key`。

Native 的模型请求发生在 AgentAPI 进程内，不经过沙箱代理。某条环境变量凭证的允许主机与实际 provider 主机规范化后完全一致时，该凭证的密钥写入 provider 已配置的鉴权头。变量名不参与选择，也不改变鉴权头。没有凭证匹配该主机时，继续使用默认模型 Key。多条凭证同时允许该主机时，该路由失败。匹配到凭证但 provider URL 不是合法的绝对 HTTPS 地址时，该路由同样失败。只调用模型、未启用沙箱工具或自托管时也按这个规则解析，不创建沙箱，也不把密钥写入进程环境或 worker。启用沙箱工具的 Native 会话仍会把占位符放进沙箱环境，但不写环境变量请求头注入。

创建 Vault 凭据并把 Vault 加入会话的 `vault_ids` 即可使用。凭据解析、解密或注入配置无效时执行失败，不会忽略 Vault 并恢复默认模型 Key。模型地址和协议仍由原模型配置决定，调用 AgentAPI 仍使用已绑定的 API Key。Native 每个 Turn 读取当前 Vault；正在执行的重试和摘要沿用本 Turn 已选中的密钥，新的 Turn 读取轮换后的密钥；AgentAPI 重启恢复未完成 Turn 时会重新读取当前 Vault。沙箱复用会更新默认模型注入，Vault 轮换会保留仍需使用的默认模型规则。

### 在会话中引用授权

确认业务用户有权使用前面创建的 Vault 后，将其 ID 写入会话的 `vault_ids`。示例为同一业务用户启动协作系统摘要任务：

cURL

```bash
session_id=$(curl --fail-with-body -sS $AGENTAPI_BASE_URL/v1/sessions \
  -H "x-api-key: $AGENTAPI_API_KEY" \
  -H "content-type: application/json" \
  --data @- <<EOF | jq -r '.id'
{
  "agent": {"type": "agent", "id": "$agent_id", "version": 1},
  "environment_id": "$environment_id",
  "vault_ids": ["$vault_id"],
  "title": "Alice's Slack digest"
}
EOF
)
```

Python

```python
session = client.beta.sessions.create(
    agent=agent.id,
    environment_id=environment.id,
    vault_ids=[vault.id],
    title="Alice's Slack digest",
)
```

TypeScript

````typescript
const session = await client.beta.sessions.create({
  agent: agent.id,
  environment_id: environment.id,
  vault_ids: [vault.id],
  title: "Alice's Slack digest",
});
```csharp
```csharp
var session = await client.Beta.Sessions.Create(new()
{
    Agent = agent.ID,
    EnvironmentID = environment.ID,
    VaultIds = [vault.ID],
    Title = "Alice's Slack digest",
});
````

Go

```go
session, err := client.Beta.Sessions.New(ctx, anthropic.BetaSessionNewParams{
	Agent: anthropic.BetaSessionNewParamsAgentUnion{
		OfString: anthropic.String(agent.ID),
	},
	EnvironmentID: environment.ID,
	VaultIDs:      []string{vault.ID},
	Title:         anthropic.String("Alice's Slack digest"),
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
    .title("Alice's Slack digest")
    .build());
```

PHP

```php
$session = $client->beta->sessions->create(
    agent: $agent->id,
    environmentID: $environment->id,
    vaultIDs: [$vault->id],
    title: "Alice's Slack digest",
);
```

Ruby

```ruby
session = client.beta.sessions.create(
  agent: agent.id,
  environment_id: environment.id,
  vault_ids: [vault.id],
  title: "Alice's Slack digest"
)
```

会话创建成功只说明配置已保存，不能证明第三方鉴权已通过。开始任务后，结合[会话事件](../sessions-and-tasks/events.md)核对 MCP 调用结果：

| 现象              | 优先检查                        |
| --------------- | --------------------------- |
| MCP 连接或调用返回鉴权错误 | 服务地址是否匹配，令牌是否过期或已被第三方撤销     |
| 可以读取但无法执行写操作    | 第三方令牌是否具有所需权限，工具配置是否允许该操作   |
| 使用了不符合预期的业务身份   | 应用选择的 Vault 是否正确，是否存在多个匹配凭证 |

## 更新授权并验证生效

凭证更新分为两类：更换令牌值时更新原凭证；变更服务或 OAuth 客户端时创建新凭证。

| 变更                                                   | 操作          |
| ---------------------------------------------------- | ----------- |
| 密钥值、`display_name`、OAuth 刷新信息                        | 更新原凭证       |
| `mcp_server_url`、`token_endpoint`、`client_id` 等结构性字段 | 归档旧凭证，创建新凭证 |

应用完成 OAuth 刷新或取得新令牌后，使用已保存的凭证 ID 更新。运行中的会话需要异步同步；同步完成后，后续出站请求才使用新值。

cURL

```bash
curl --fail-with-body -sS \
  "$AGENTAPI_BASE_URL/v1/vaults/$vault_id/credentials/$credential_id" \
  -H "x-api-key: $AGENTAPI_API_KEY" \
  -H "content-type: application/json" \
  --data @- <<'EOF' > /dev/null
{
  "auth": {
    "type": "mcp_oauth",
    "access_token": "xoxp-new-...",
    "expires_at": "2099-12-31T23:59:59Z",
    "refresh": {"refresh_token": "xoxe-1-new-..."}
  }
}
EOF
```

Python

```python
client.beta.vaults.credentials.update(
    credential.id,
    vault_id=vault.id,
    auth={
        "type": "mcp_oauth",
        "access_token": "xoxp-new-...",
        "expires_at": "2099-12-31T23:59:59Z",
        "refresh": {"refresh_token": "xoxe-1-new-..."},
    },
)
```

TypeScript

````typescript
await client.beta.vaults.credentials.update(credential.id, {
  vault_id: vault.id,
  auth: {
    type: "mcp_oauth",
    access_token: "xoxp-new-...",
    expires_at: "2099-12-31T23:59:59Z",
    refresh: {
      refresh_token: "xoxe-1-new-...",
    },
  },
});
```csharp
```csharp
await client.Beta.Vaults.Credentials.Update(credential.ID, new()
{
    VaultID = vault.ID,
    Auth = new BetaManagedAgentsMcpOAuthUpdateParams
    {
        Type = BetaManagedAgentsMcpOAuthUpdateParamsType.McpOAuth,
        AccessToken = "xoxp-new-...",
        ExpiresAt = DateTimeOffset.Parse("2099-12-31T23:59:59Z"),
        Refresh = new() { RefreshToken = "xoxe-1-new-..." },
    },
});
````

Go

```go
_, err = client.Beta.Vaults.Credentials.Update(ctx, credential.ID, anthropic.BetaVaultCredentialUpdateParams{
	VaultID: vault.ID,
	Auth: anthropic.BetaVaultCredentialUpdateParamsAuthUnion{
		OfMCPOAuth: &anthropic.BetaManagedAgentsMCPOAuthUpdateParams{
			Type:        anthropic.BetaManagedAgentsMCPOAuthUpdateParamsTypeMCPOAuth,
			AccessToken: anthropic.String("xoxp-new-..."),
			ExpiresAt:   anthropic.Time(time.Date(2099, time.December, 31, 23, 59, 59, 0, time.UTC)),
			Refresh: anthropic.BetaManagedAgentsMCPOAuthRefreshUpdateParams{
				RefreshToken: anthropic.String("xoxe-1-new-..."),
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
client.beta().vaults().credentials().update(credential.id(),
    CredentialUpdateParams.builder()
        .vaultId(vault.id())
        .auth(BetaManagedAgentsMcpOAuthUpdateParams.builder()
            .type(BetaManagedAgentsMcpOAuthUpdateParams.Type.MCP_OAUTH)
            .accessToken("xoxp-new-...")
            .expiresAt(OffsetDateTime.parse("2099-12-31T23:59:59Z"))
            .refresh(BetaManagedAgentsMcpOAuthRefreshUpdateParams.builder()
                .refreshToken("xoxe-1-new-...")
                .build())
            .build())
        .build());
```

PHP

```php
$client->beta->vaults->credentials->update(
    $credential->id,
    vaultID: $vault->id,
    auth: ManagedAgentsMCPOAuthUpdateParams::with(
        type: 'mcp_oauth',
        accessToken: 'xoxp-new-...',
        expiresAt: new DateTimeImmutable('2099-12-31T23:59:59Z'),
        refresh: ManagedAgentsMCPOAuthRefreshUpdateParams::with(refreshToken: 'xoxe-1-new-...'),
    ),
);
```

Ruby

```ruby
client.beta.vaults.credentials.update(
  credential.id,
  vault_id: vault.id,
  auth: {
    type: "mcp_oauth",
    access_token: "xoxp-new-...",
    expires_at: "2099-12-31T23:59:59Z",
    refresh: {refresh_token: "xoxe-1-new-..."}
  }
)
```

### 核对同步与撤销状态

凭证轮换会为正在运行的云端沙箱创建异步同步任务。归档或删除 Vault、凭证时，AgentAPI 同样会更新仍可复用的沙箱注入规则。

> 凭证同步
>
> 轮换成功仅表示新密文已保存，运行中的沙箱仍需异步同步。同步重试耗尽时没有可查询的用户事件；请在下一次会话或沙箱连接时验证新凭证，并保存轮换请求的 `X-Trace-ID` 供排障。

下表为 Vault 生命周期的 Webhook 事件，不通过会话 SSE 返回。可在 Portal 的“服务设置 → Webhook”选择订阅，接收服务的验签与可靠处理见 [Webhook 通知](webhooks.md)。未接入 Webhook 时，应用应通过资源查询核对归档或删除状态，再通过第三方服务调用验证凭证可用性。

| 事件                                | 触发条件                                                     |
| --------------------------------- | -------------------------------------------------------- |
| `vault.archived`                  | Vault 已归档。同时会为每个底层凭证发出一个 `vault_credential.archived` 事件。 |
| `vault.deleted`                   | Vault 已删除。同时会为每个底层凭证发出一个 `vault_credential.deleted` 事件。  |
| `vault_credential.archived`       | 凭证已归档，可能是直接归档，也可能是 vault 归档的结果。                          |
| `vault_credential.deleted`        | 凭证已删除，可能是直接删除，也可能是 vault 删除的结果。                          |
| `vault_credential.refresh_failed` | 预留事件类型；AgentAPI 当前不执行 OAuth token 自动刷新，也不会用它报告沙箱同步失败。    |

这些事件用于确认资源状态变化，不等同于第三方在线鉴权校验。AgentAPI 当前不提供 OAuth 在线校验接口，令牌可用性仍需通过实际服务访问确认。

## 停止使用并保留审计记录

业务用户撤销授权后，应用应停止为新会话选择该 Vault 或凭证，并归档或删除相应资源。归档适用于保留非敏感审计记录；删除适用于不再保留资源记录。两种操作都会触发沙箱注入规则更新，但运行中的同步仍是异步的，不能承诺立即撤回已经发出的请求。

* 列出 Vault 或凭证： 分页返回，最新的在前。默认排除已归档记录；传入 `include_archived=true` 可一并返回。
* 归档 Vault： `POST /v1/vaults/{id}/archive`。同时归档其中的凭证并清除密钥，保留记录用于审计。新会话不能再引用该 Vault。
* 归档凭证： `POST /v1/vaults/{id}/credentials/{cred_id}/archive`。清除密钥负载，但保留 `mcp_server_url` 等非敏感描述信息用于审计。
* 删除 Vault 或凭证： 永久删除记录。需要保留审计信息时，请使用归档。

在 Portal 的 Vault 列表中，您可以按名称或 ID 搜索，并选择活跃、已归档或全部状态。搜索会去掉关键词首尾的空白，按字面量包含关系匹配 `display_name` 或 `id`；`%`、`_` 和 `!` 不作为通配符。搜索和状态筛选均在统计总数和分页前完成。

对应的 Portal 接口为 `GET /portal/v1/vaults?page_index=1&q=关键词`。`q` 和 `status` 必须与 `page_index` 一起使用；`status=active` 仅返回活跃 Vault，`status=archived` 仅返回已归档 Vault。查看全部状态时，省略 `status` 并传入 `include_archived=true`，两者不能同时使用。公开接口 `GET /v1/vaults` 不接受 `q` 或 `status`，凭证列表也不支持这两个参数。
