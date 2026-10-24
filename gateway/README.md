# Go Gateway

独立 Go 控制层服务器。管理应用构建、注册、启动、停止和投屏；Agent 通过 MCP 获取 OpenAPI，再按 operationId 调用应用。Gateway 不执行应用代码，不内置游戏规则。

## 启动

```bash
# 从仓库根目录启动；默认管理模式，不要求预先登记 quiz 服务
# 首次部署复制示例，填写本地文件
cp gateway/config/managed.example.json gateway/config/gateway.local.json
go run ./gateway -config gateway/config/gateway.local.json -listen 127.0.0.1:8789
```

`GET /health` 检查进程，`/mcp` 使用 Streamable HTTP。缺少下游配置时服务仍可启动；对应工具返回 BUILD_UNAVAILABLE 或 DEPENDENCY_UNAVAILABLE，不伪造成功。

`config/managed.example.json` 提供真实部署的完整配置格式。需要替换：

- AgentAPI 的智能体 ID、固定版本、环境 ID；密钥与这些 ID 一起放入本地文件的 `agentapi.api_key`，不再读取 AGENTAPI_API_KEY 环境变量。
- Render、Casting 的可达 HTTP 地址；可选 RENDER_TOKEN、CASTING_TOKEN 提供 Bearer 凭证。
- screen_id 到 Casting target_id / endpoint_id 的映射。第一版配置一台 Render 和一个 Casting 服务。
- public_url：Render 能下载应用包的 Gateway 地址，不能误用远端机器不可达的 localhost。

生产入口使用 HTTPS 反向代理并配置 MCP_TOKEN。frp 时通过 `-public-host HOST:PORT` 显式允许代理 Host。认证与 Linx 上下文分别处理：客户端必须提供 X-Linx-Device-Id、X-Linx-Agent-Id、X-Linx-Session-Id，应用归属按三者精确匹配。这些 header 自身不是认证凭证，公网入口必须由受信平台调用。

状态文件与 artifact_dir 相对配置文件解析，默认放在 `.runtime/`。仅运行一个 Gateway 进程写同一个状态文件。MVP 单实例串行协调运行操作，应用构建异步执行；没有多副本锁。

## MCP 工具

| 工具 | 输入 |
| --- | --- |
| create-app | prompt、可选 player_names |
| get-app-status | app_id |
| get-screens | 空对象 |
| start-app | app_id、screen_id |
| stop-app | app_id |
| get_openapi | app_id |
| send_request | app_id、operationId、可选 parameters/requestBody |

`-speaker-probe` 增加可选诊断工具 probe_speaker。用户名使用 user_name，player_names 仅作为构建输入，Gateway 不生成用户 ID。

管理模式从应用记录读取 OpenAPI 与 Render 返回的地址，新增构建应用无需重启。`services.json` 仅作为兼容的静态联调入口：

```bash
go run ./gateway -config gateway/config/services.json -listen 127.0.0.1:8787
```

静态模式只有 get_openapi/send_request，输入仍使用 service_id。管理模式改为 app_id，需要客户端重新建立会话刷新工具列表。不要混用两种 schema。

## 文件

```text
gateway/
  main.go, config.go        启动、配置和依赖组装
  internal/mcpserver/       MCP schema、上下文和工具适配
  internal/application/     持久应用记录、归属、状态机、资源占用与编排
  internal/agentapi/        会话、SSE、回放去重、构建任务和文件收集
  internal/artifact/        tar.gz 检查、OpenAPI 校验、摘要、下载服务
  internal/downstream/      Render 和 Casting HTTP 客户端
  internal/openapi/         schema 校验、operationId 解析、受控 HTTP 执行
  integration/             可选真实端点冒烟测试
  config/                  管理模式及联调示例
```

## 测试

```bash
go test ./... -race
go vet ./...
# 只读取管理版工具列表，不修改游戏
GATEWAY_MANAGED_URL=http://127.0.0.1:8789/mcp go test ./gateway/integration -run TestManagedDiscovery -v
# 静态模式真实 quiz 测试，会重置并操作 demo
GATEWAY_SMOKE_URL=http://127.0.0.1:8787/mcp go test ./gateway/integration -run TestLiveQuiz -v
```

详细范围和限制见 [Gateway 服务](../docs/gateway.md)，工作及测试记录见 [work.MD](work.MD)。

## 本地 AgentAPI 配置

默认读取 `gateway/config/gateway.local.json`，该文件被 Git 忽略。仓库只保留不含真实密钥的示例；已有本地文件不要再次用 cp 覆盖。

```json
{
  "agentapi": {
    "enabled": true,
    "base_url": "https://agent.qiniuapi.com",
    "api_key": "填写你的Key",
    "agent_id": "填写智能体ID",
    "agent_version": 1,
    "environment_id": "填写环境ID"
  }
}
```

上例是本地配置的 agentapi 部分，保留同文件中的其他 Gateway 设置。准备阶段可设 enabled=false；填写完毕改为 true 后重启，Key 与 ID 一起生效。示例文件不要填写真实值。Render/Casting/MCP 的可选认证环境变量保持原用法。
