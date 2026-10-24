# Gateway 服务

Gateway 是单进程 Go 控制层服务，MCP 是它的接入模块。应用生命周期、应用注册表和 OpenAPI 执行是内部能力，分别调用 AgentAPI、Render 和 Casting；应用仍维护自己的业务状态。

契约依据：[应用管理 MCP](app-management-mcp.md)、[控制面与 AgentAPI](gateway-agentapi-interface.md)、[控制面与 Render](gateway-render-interface.md)、[应用包](app-artifact.md)、[Casting](casting.md)。

## 实现范围

| 能力 | 实现及证据 |
| --- | --- |
| MCP 接入 | Go SDK Streamable HTTP，七个应用工具；协议测试通过 |
| 应用注册表 | JSON 文件原子替换、设备/智能体/会话归属、已注册 OpenAPI；重启恢复测试通过 |
| 异步构建 | create-app 立即返回，玩家名称作为构建输入，重复构建不覆盖原输入 |
| AgentAPI | 创建会话、先订阅再提交、SSE 去重与重连、stop_reason 判断、产物分页下载；HTTP 假服务测试通过 |
| 产物 | tar.gz 路径/类型/大小检查、launch/OpenAPI 校验、摘要和不可猜下载 URL；测试通过 |
| 运行编排 | 端口查询、Render 启动、Casting 播放确认、占用、清理、周期状态检查；HTTP 集成测试通过 |
| OpenAPI | 按 app_id 取得规范与运行地址，校验并执行；通用 HTTP 测试和静态 quiz 公网联调通过 |
| 声纹探针 | 可选，用户已确认名字来自声纹，服务记录传参；不生成玩家 ID |

真实 AgentAPI 构建及 Render/Casting 屏幕联调需要部署配置，本机尚未提供 AgentAPI 密钥、agent/environment ID 和下游地址。上述模拟集成测试不等于真实云端构建或电视播放验收。

## 目录与运行

所有服务实现和配置位于 `gateway/`，复用根 go.mod，独立编译为 Go 二进制：

```bash
go run ./gateway -config gateway/config/gateway.local.json -listen 127.0.0.1:8789
go build -o ./bin/gateway ./gateway
```

文件职责及配置样例见 [gateway/README.md](../gateway/README.md)。运行进程无需 Node；构建产出的应用按应用包契约仍可使用 Node，由 Render 启动。

`gateway/config/gateway.local.json` 是默认的本地管理模式配置，由 Git 忽略。AgentAPI Key 写入 `agentapi.api_key`，与智能体/环境 ID 保存在同一个文件，程序不再读取 AGENTAPI_API_KEY。`agentapi.enabled=false` 可暂时禁用构建；启用后缺少 Key 或 ID 会明确报错。`managed.example.json` 是不含真实密钥的配置模板。`services.json` 只用于静态联调，不能代替应用注册表。

## 模块与请求流程

```text
灵矽 / Agent
  → MCP handler（工具 schema、上下文、结果封装）
  → Application Manager（归属、持久状态、幂等与资源协调）
      → AgentAPI：构建
      → Artifact Store：校验并注册文档
      → Render：启动应用，取得 app_base_url 与媒体流
      → Casting：播放，确认 playing
      → OpenAPI executor：调用已运行应用
```

注册表保存 app_id、归属、prompt/player_names、构建 session、去重事件、产物及 OpenAPI、Render 实例和投屏关系。对外状态不暴露内部端口、实例 ID 或下载凭证。

create-app 去除用户名首尾空白，拒绝空名和重名。构建 prompt 指定 user_name 为身份键，初始玩家未准备好。构建端提供真实游戏逻辑，Gateway 不生成用户 ID、不自动把名字转换为 ID。

## 工具契约

管理模式公开：create-app、get-app-status、get-screens、start-app、stop-app、get_openapi、send_request。生命周期工具名遵循应用管理文档；保留已联调的两个 snake_case OpenAPI 工具名。

```json
{"prompt":"做一个两人知识竞赛","player_names":["韩鲁鹏","张之阳"]}
```

创建返回 app_id 后查询状态。应用 idle 后可以读取规范：

```json
{"app_id":"app-example"}
```

指定屏幕启动：

```json
{"app_id":"app-example","screen_id":"living-room"}
```

仅当 Render ready 且 Casting playing 才返回 running。运行后按规范调用：

```json
{
  "app_id":"app-example",
  "operationId":"submitAnswer",
  "requestBody":{"user_name":"韩鲁鹏","answer":"地球"}
}
```

操作和字段由具体应用 OpenAPI 定义，上例不是硬编码接口。get_openapi 从已校验的产物记录读取文档；send_request 在调用前检查归属和运行状态，从记录取得 Render 地址。停止或状态错误后拒绝继续发送业务请求。

MCP 的完整文档和调用结果同时放在 text 与 structuredContent，以兼容模型只读取文本的客户端。structuredContent 不是秘密通道，应用不得在公开响应里泄露不应给模型的信息。

## 状态、并发与恢复

同一 device/agent/session 的重复 building 请求返回同一个 app_id，不覆盖输入。运行操作在单 Gateway 内串行协调，防止同一应用或屏幕被重复占用。应用状态以持久记录为准，业务调用不允许使用 caller 提供的 URL。

启动后每 5 秒查询运行实例和投屏状态；明确失败进入 error 并清理，临时 HTTP 查询失败保留记录，等待下一次检查。正常停止先关闭投屏，再停止 Render。清理失败保留资源占用。

网络超时或进程崩溃导致启动结果不明时，保留 unknown_runtime/unknown_projection 和屏幕占用，返回 RECONCILIATION_REQUIRED。不会以重试 POST 猜测结果。该场景尚需运维核对下游并修复状态，没有自动发现未知实例的接口。

构建 session 和事件去重记录持久化。进程正常关闭保留构建以供重启续读；构建超时尽力发送 interrupt。提交任务前先记录提交意图，响应不明不会重复提交。恢复时重放事件；已经收到正常结束的构建直接继续收集产物。没有保存 session 的崩溃构建进入 error，不自动创建重复会话。

## 安全及部署边界

- 单 Gateway 实例使用一个状态文件，没有跨进程/多副本锁。
- 外部 MCP 应配置 HTTPS 与 MCP_TOKEN。Linx 三个 header 提供访问范围，依赖受信调用方传入，本身不是身份认证。
- Artifact 下载 URL 使用随机能力令牌，只有获得完整 URL 的 Render 才能拉取；持有 URL 等同于获得下载授权。部署应使用 HTTPS，产物清理与 URL 过期尚未实现。
- Go OpenAPI 执行器支持 3.0/3.1、本地引用、标量 path/query/header、JSON body。复杂序列化、递归或远程引用、上传及流式代理拒绝。
- 请求目标来自应用记录或静态配置，禁止重定向、保留 header 覆盖、路径逃逸。业务 HTTP 超时 10 秒、请求/响应限制 1 MiB；不自动重试写操作。
- 产物压缩上限 100 MiB、解压扫描上限 300 MiB；拒绝链接与路径逃逸。控制面做静态校验，不在本机执行构建产物；Render 仍须独立校验和健康检查。

## 尚未覆盖的架构能力

应用创建和运行管理主体已实现，以下仍需补齐或真实验收：

- `notifications/app_status` 主动 MCP 通知尚未实现；stateless Streamable HTTP 使用 get-app-status 查询，不声称通知已送达。
- AgentAPI 超时中断后的最长 5 分钟等待与失败归档、归档重试、失效下载 URL 续期尚未完整实现。
- 注册表持久化使用本地 JSON，尚无多副本锁、跨平台文件系统崩溃一致性保证和未知资源的自动对账。
- 第一版配置单台 Render/Casting 与屏幕映射；没有多 Render 调度和按用户细分的屏幕 ACL。
- 原始 quiz demo 使用 user_id；新生成应用要求 user_name。静态 demo 保持原 OpenAPI，Gateway 不隐式改写。

## 验证

```bash
go test ./... -race
go vet ./...
GATEWAY_MANAGED_URL=http://127.0.0.1:8789/mcp go test ./gateway/integration -run TestManagedDiscovery -v
```

自动化覆盖构建 SSE 回放/断线/停止原因、文件分页与包校验、归属和状态持久化、并发启动、屏幕互斥、清理失败、失效投屏、真实 HTTP 下游客户端和七工具 MCP 链路。外部依赖使用 httptest 测试替身；真实公网 quiz 另行通过静态模式验证。详细记录见 [Gateway 工作记录](../gateway/work.MD)。
