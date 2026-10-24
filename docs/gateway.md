# Go Gateway：整合后的控制层服务

Gateway 使用一个 Go 进程，入口 `cmd/gateway`。主线的应用管理、SQLite、官方 AgentAPI SDK、屏幕发现与 SSE 通知作为基础；补入已验证的 Streamable HTTP、完整 OpenAPI、请求 schema 校验、声纹探针、跨会话接管和异常对账。

## 目录

```text
cmd/gateway/                 配置、依赖组装、启动与关闭
internal/gateway/app/        应用记录、归属、生命周期锁和持久化接口
internal/gateway/store/      SQLite 应用与产物记录、revision 保护
internal/gateway/build/      构建事件、停止原因、产物收集、超时中断
internal/gateway/artifact/   应用包校验、存储和受控下载
internal/gateway/service/    生命周期、屏幕占用、运行监测和恢复
internal/gateway/mcp/        两种传输、工具、主动通知、兼容入口
internal/gateway/openapi/    操作视图和完整规范持久化
internal/gateway/httpapi/    局部引用、schema 校验、受控 HTTP 执行
internal/gateway/render/     Render HTTP 客户端
internal/gateway/screen/     屏幕发现和配置映射
pkg/agentapi/                官方 AgentAPI SDK 适配
pkg/casting/                 Casting 契约及 HTTP 客户端
```

原 `gateway/` 实现归档到本机不跟踪的 `doc/pre-main-gateway-9d0599b/`。原凭证、JSON 状态和日志保留在本机原路径，不能将其直接当 SQLite 使用。

## 启动与配置

```bash
cp gateway.example.yaml gateway.yaml
# 填写同一文件里的 AgentAPI Key、agent/version、environment 及下游地址
go run ./cmd/gateway -config gateway.yaml
```

`gateway.yaml` 被 Git 忽略。配置包含 AgentAPI、Render、Casting、产物可达地址、SQLite 路径、端口范围和超时。真实 Key 不写入示例。支持 `public_host` 处理 frp 代理 Host、`mcp_token` 开启 Bearer 校验、`speaker_probe` 开启诊断。本地进程未配置 Render/Casting 时无法投屏，测试占位地址不代表服务存在。

## MCP 传输与工具

- `/mcp`：Streamable HTTP，兼容已有灵矽配置；无状态请求/响应，不提供后台主动通知。
- `/sse`：传统 SSE，建立长连接后使用服务返回的 `/mcp/message?sessionid=...` 发送请求；用于主动 `notifications/app_status`。
- `/healthz`：进程和 SQLite 健康检查。

主线工具保留：`create-app`、`get-app-status`、`get-screens`、`start-app`、`stop-app`、`get-app-operations`、`send-request`。`get-app-operations` 提供操作视图，`send-request` 使用 `operation_id` 和 `body`。

兼容工具同时提供：`get_openapi(app_id)` 返回完整原始规范；`send_request` 使用 `operationId`、`parameters`、`requestBody`。两种调用都经过严格的 OpenAPI 执行器：缺少必需参数、未声明参数、body schema 错误、保留 header、路径越界和 HTTP 跳转会被拒绝；响应限制 1 MiB。

可选 `probe_speaker` 保留本轮声纹名字参数实验。声纹识别仍由灵矽负责，应用使用 `user_name`，Gateway 不生成用户 ID。

## 完成通知：哪些验证过

之前的实验验证了设备端 `text_response` 主动播报，不等于 MCP 主动通知。整合后保留主线的 `notifications/app_status`，只有匹配 device/agent/session 的活跃 SSE 订阅能收到。发送日志记录 subscriber 数量；零订阅不能称为已送达。

协议自动化测试验证 SSE 通知送达及会话隔离。真实构建通知以本次工作记录为准。即使测试客户端收到通知，也不能宣称灵矽已经支持接收后播报；需在智控台选择 SSE、填写 `/sse` 并重新建立会话做实际验收。`get-app-status` 始终是状态事实来源。

## 跨会话接管

启用 `mcp_token` 后才发布 `list-apps`、`claim-app`。调用方还需由受信平台提供 device/agent/session header。

1. 重连后 `list-apps` 返回同一 device + agent 的应用。
2. `claim-app(app_id)` 显式把归属转移到当前 session，持久化成功后返回。
3. 旧 session 失去后续访问权，通知路由到新归属；已执行的外部操作不会撤销。
4. 不同设备、不同 agent、缺失身份不能接管；没有有效鉴权的公网部署不开启接管工具。

## 异常自动对账

在启动网络请求之前持久化屏幕、目标和未知结果标记；收到实例/投屏 ID 后保存。失败不会丢弃资源信息，重启后继续核对。

- Render 响应丢失：查询端口记录按 app_id 找实例，再 GET 确认归属，随后清理；不重复 POST。
- Casting 响应丢失：读取 target 的 active_session_id，核对 target、endpoint 和流 URL 后再清理。
- 清理失败：保留实例/投屏关系和屏幕占用，周期重试；先停投屏再停进程。
- 下游无法提供唯一匹配、多个实例或疑似他人的投屏：继续保留占用，等待运维核对。没有证据时不会擅自清除。

## 产物与构建验收

保留主线的 AgentAPI SDK、产物分页下载、校验、SQLite 注册和超时中断/归档。构建超时按原始 BuildStartedAt 计算，重启不重置期限。提示包含完整 launch.json 字段示例；禁止 command/args/cwd 别名。入口首参数必须是包内 app/ 文件，禁止 Node -e/--require 充当入口；归档重复路径和 gzip 校验错误拒绝。

严格执行器需要规范完整：路径参数必须声明、引用必须可解析、请求体必须匹配 schema。已有数据库中只保存操作视图的应用需要重新校验原 artifact 补齐完整规范，不能跳过校验继续转发。

## 测试与限制

```bash
go test ./... -race
go vet ./...
go build ./...
```

新增测试覆盖：完整规范与引用持久化、非法请求阻断、Streamable HTTP 工具兼容、SSE 通知隔离、显式跨会话接管、接管持久化失败回滚、SQLite 重启恢复、未知 Render/Casting 结果对账、他人投屏拒绝清理及失败重试。

真实云端模型可能以 retries_exhausted 结束；只有产物下载、校验、注册 idle 全部成功才算构建通过。本轮已通过真实产物的 Render 启动、OpenAPI 操作和视频解码，详见[整合测试报告](gateway-integration-report.md)。电视播放与灵矽主动通知播报仍需实际环境验收。不要把后台启动进程等同于系统开机常驻或多副本高可用。

关键启动记录必须持久化成功后才调用下游；数据库不可写时返回失败。清理记录无法保存时不释放屏幕占用。部署改变产物目录或公开地址时，重启会更新保存的路径和下载地址；迁移文件本身仍由运维完成。
