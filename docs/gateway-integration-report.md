# Gateway 整合与验证记录

2026-10-24，分支 feat/openapi-mcp。整合主线 PR #40 与本分支已联调的 Go Gateway。

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

本轮没有把该应用投到电视：Render/Casting 实际地址仍未配置。应用功能是否完整满足需求也不能仅用产物静态校验判断，仍需运行时验收。

## 自动化

- go test ./... -race、go vet ./...、Go 构建与 Windows 无 cgo 交叉编译。
- 主线生命周期、SQLite、产物、SDK 测试全部保留。
- 新增完整规范持久化、局部引用、严格参数/body 校验、Streamable HTTP 兼容工具测试。
- 同设备跨会话 claim、跨 agent/device 拒绝、旧 session 访问失效、SQLite 重启和持久化失败回滚。
- 未知 Render 响应对账、未知投屏响应对账、其他应用投屏拒绝清理、失败清理占用保留和后台重试、启动中崩溃恢复。
- SSE 实际接收和错误会话隔离测试；真实构建另有 SSE 订阅证据。
- Node 入口参数、归档重复路径、gzip 完整性检查，禁止静默接受无效包。

## 仍需实机验证

灵矽若要接收后台通知，需配置 SSE /sse 并重新开会话；不能把“工具返回后播报”或“设备 text_response 播报”算作该能力通过。跨会话接管要求入口认证与可信 Linx header；公网未启用认证时不暴露接管工具。状态严格隔离仍为默认。

Render/Casting 未部署时只测试构建、规范和通知，不能测试真实电视。未知资源没有可查唯一标识时保留占用是预期兜底。旧 JSON 记录和 SQLite 不可互换；历史 JSON 保留用于排查。
