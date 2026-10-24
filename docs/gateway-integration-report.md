# Gateway 整合与验证记录

2026-10-24，分支 feat/openapi-mcp。整合主线 PR #40、#44、#39（main 至 754bc2f）与本分支已联调的 Go Gateway。决策和交接步骤见 [work.MD](gateway/work.MD)。

## 保留与收敛

采用主线 cmd/gateway、internal/gateway、pkg/agentapi/pkg/casting，SQLite 应用和产物持久化，官方 AgentAPI SDK，应用生命周期、屏幕发现及 SSE 通知。原 gateway/ 重复代码归档在本地不跟踪的 doc/pre-main-gateway-9d0599b；凭证及旧 JSON 状态保留，本轮没有把失败的旧应用伪装成成功迁入 SQLite。

迁入完整 OpenAPI 文档及局部引用支持、schema 校验、目标和大小限制；两个已部署工具 get_openapi/send_request 保留兼容。Streamable HTTP /mcp 保留，传统 SSE 使用 /sse 和 /mcp/message。支持可选声纹探针、反向代理 Host 与 Bearer 认证。

## 四个问题的结论

| 项目 | 结果 | 实际证据与边界 |
| --- | --- | --- |
| 构建完成主动通知 | 服务端已接通 | 真实构建结束后 SSE 订阅收到 notifications/app_status；灵矽收到并播报未验证。stateless /mcp 不提供后台推送 |
| 完整产物验收 | 本轮真实通过 | app_TZZNOIM7522QB7TKF6FDRLPVAU 构建 539 秒后 idle；包 7288 bytes，5 个 OpenAPI 操作；下载摘要和 SQLite 元数据一致，再次校验通过 |
| 跨会话接管 | 已实现并测试 | 受鉴权保护的 list-apps/claim-app，限定同 device+agent，显式改变 session；旧会话失去访问权，持久化失败不发布转移 |
| 异常对账 | 可识别资源的路径已实现 | Render 端口记录按 app_id 查回实例；Casting active_session_id 对照目标/Endpoint/流 URL；清理失败保留占用并重试。无法唯一确认时仍须人工处理，不能安全地猜测释放 |

## 真实构建结果

此前三次旧实现记录仍为 error：第一次 launch.json 用错字段，其后两轮 retries_exhausted。它们不是本轮成功记录。

本轮用整合版官方 SDK 创建极简计数器，SQLite 注册为 counter / idle。日志记录：app_status state=idle subscribers=1。测试订阅端实际收到：

```json
{
  "jsonrpc":"2.0",
  "method":"notifications/app_status",
  "params":{
    "app_id":"app_TZZNOIM7522QB7TKF6FDRLPVAU",
    "state":"idle",
    "message":"APP 创建完成"
  }
}
```

launch.json 使用 runtimeExecutable=node、runtimeArgs=[app/app.js]、healthPath=/health、readyTimeoutMs=10000。真实包通过主线产物校验和迁入的 schema 编译；get_openapi 能保留 components，运行请求按该规范校验。

本轮又用真正的 Render 启动这个包：通过已保存的 OpenAPI 调用加入玩家、加一和查询，total 从 0 变为 1；错误类型的 user_name 被 schema 阻断。Chromium 页面输出的视频能被 FFmpeg 解码，停止后进程端口释放。测试不会新建云端构建，也不改真实应用的归属和生命周期。

主线计分板 E2E 同样通过：HTTP 操作后真实视频色块变化、重新拉流、停止和换端口重启、进程退出和缺少健康接口的失败清理。两个测试都不代表电视播放验收。

## 自动化

- go test ./... -race、go vet ./...、Go 构建与 Windows 无 cgo 交叉编译。
- 主线生命周期、SQLite、产物、SDK 测试全部保留。
- 新增完整规范持久化、局部引用、严格参数/body 校验、Streamable HTTP 兼容工具测试。
- 同设备跨会话 claim、跨 agent/device 拒绝、旧 session 访问失效、SQLite 重启和持久化失败回滚。
- 未知 Render 响应对账、未知投屏响应对账、其他应用投屏拒绝清理、失败清理占用保留和后台重试、启动中崩溃恢复。
- SSE 实际接收和错误会话隔离测试；真实构建另有 SSE 订阅证据。
- Node 入口参数、归档重复路径、gzip 完整性检查，禁止静默接受无效包。
- 数据库写入失败时阻断云端构建和后续 Render/Casting 启动；清理状态无法持久化时保留占用。尚未发出启动请求的失败不会留下无法查回的虚假占用。
- 部署地址变化后，恢复产物路径及下载 URL，保留下载 token，并同步到应用记录。

运行本轮真实产物验收（需本机保留的数据库和产物）：

```bash
GATEWAY_LIVE_DB="$PWD/gateway/.runtime/merged.db" \
GATEWAY_LIVE_APP=app_TZZNOIM7522QB7TKF6FDRLPVAU \
RENDER_E2E_OUTPUT="$PWD/doc/integration-render-results" \
go test -race -tags=e2e -run TestLiveBuiltCounter -v ./e2e -count=1 -timeout=120s
```

该测试专门验证本次 counter 的 operationId，不能用它验收任意生成的游戏。视频帧、真实 SSE 通知和公网检查记录保留在本机忽略目录中，不提交凭证或带 token 的下载地址。

## 当前部署

整合版已接到现有 frp 公网入口 `http://118.89.179.167:6004/mcp`，后台 SSE 入口是同源 `/sse`。本机 Gateway 为 8787，Render 为 8091，Casting 为 8080。公网健康检查、10 个工具发现、idle 状态查询、完整 OpenAPI 和 7288 字节产物的 SHA-256 下载核对通过。Casting 当前没有发现可用屏幕。

真实配置使用本机忽略的 `gateway.yaml`；SQLite 和产物仍保留在本机 `gateway/.runtime/`。切换前备份为 `before-public-switch.db`。旧 JSON 失败记录保留，但没有混入新 SQLite。当前公网没有设置 Bearer token，因而不暴露接管工具；这不代表跨会话接管已经在灵矽正式启用。后台进程也不等同于开机自启。

## 仍需实机验证

灵矽若要接收后台通知，需配置 SSE /sse 并重新开会话；不能把“工具返回后播报”或“设备 text_response 播报”算作该能力通过。跨会话接管要求入口认证与可信 Linx header；公网未启用认证时不暴露接管工具。状态严格隔离仍为默认。

未知资源没有可查唯一标识时保留占用是预期兜底。旧 JSON 记录和 SQLite 不可互换；历史 JSON 保留用于排查。SSE 通知是尽力发送，断线期间没有持久化通知队列或重放，需要查询状态。后续优先做灵矽 SSE 通知实测、配置受信入口鉴权后验收会话接管，以及实际电视上的 Casting 联调。
