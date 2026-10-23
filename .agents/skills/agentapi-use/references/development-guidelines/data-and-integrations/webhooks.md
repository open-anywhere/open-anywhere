<!-- 来源：https://developer-doc.qiniu.com/products/agentapi/development_guidelines/agentapi-data-and-integrations/13605-agentapi-data-and-integrations-webhooks | 由 sync-docs.py 自动生成，请勿手改 -->

# Webhook 通知



最近更新时间: 2026-10-20 17:43:44

应用需要在会话停止、定时部署触发或资源变更后更新业务状态时，可以配置 Webhook。AgentAPI 将订阅事件以 JSON POST 发送到应用的接收地址，无需一直保持连接。

Webhook 提供资源与生命周期通知，不包含完整回答、文本增量或逐条工具调用。展示实时执行进度时使用[会话事件流](../sessions-and-tasks/events.md)；收到通知后按资源 ID 查询详情和历史事件，确认实际业务结果。

## 创建通知端点

1. 准备公网 HTTPS 接收地址，例如 `https://hooks.example.com/agentapi`。仅支持 443 端口；域名必须只解析到公网地址，不支持 IP 地址、内网域名或 HTTP 重定向。
2. 打开 [AgentAPI Portal 的 Webhook 页面](https://portal.qiniu.com/agentapi/webhooks)，选择资源所在的地域。端点与订阅属于当前账户和当前地域，其他地域需要单独配置。
3. 点击“创建 Webhook”，在右侧抽屉中填写接收地址、可选描述，并选择需要的事件。事件按资源类别分组，可使用每组的“全选”勾选该组可新增的事件，也可取消整组选择。至少选择一项；新增订阅只提供已有触发点的事件。
4. 创建成功后，在“Webhook 签名密钥”弹窗中点击“复制密钥”，将一次性显示的 `whsec_...` 配置到接收服务中，用于验证通知来源和内容。完成配置后关闭弹窗；“关闭”按钮不执行保存，后续编辑和列表也不会再次返回密钥。丢失时需要轮换。
5. 在同一地域执行一次对应操作，例如更新智能体，检查接收服务是否已验证签名并可靠保存事件。页面上的“已启用”仅表示允许投递，不代表接收服务已通过验证。

端点归属于当前登录账户和当前地域。Portal 从登录身份确定 owner，用户无需填写任何租户字段；不同账户之间的端点和事件投递相互隔离。

签名密钥用于验证 AgentAPI 发来的通知，与[调用 AgentAPI 的 MaaS API Key](../../quick-start/access.md)用途不同。Webhook 管理由已登录的 Portal 通过专用控制面完成；MaaS API Key 不提供端点管理权限。页面无法加载或提示服务不可用时，需联系服务管理员检查配置，无需完成服务开通流程。

列表展示接收地址、订阅数量、状态和连续失败次数。点击表格内容、接收地址或在“操作”菜单中选择“编辑”，可在侧栏按资源分组查看和修改订阅；菜单还提供启停、删除和签名密钥轮换。停用原因可在状态提示和编辑侧栏查看。当前不提供用户侧投递历史，需在接收服务保存收到的消息与处理结果。

## 验证签名并读取资源

每个请求包含三个签名头：

| 请求头                   | 内容                                    |
| --------------------- | ------------------------------------- |
| \`webhook-id\`        | \`whe\_...\` 事件 ID；同一事件的重试保持不变。       |
| \`webhook-timestamp\` | 本次发送时的 Unix 秒数；重试会重新签名。               |
| \`webhook-signature\` | \`v1,\<Base64 签名>\`，签名使用 HMAC-SHA256。 |

去掉密钥的 `whsec_` 前缀并 Base64 解码，得到 HMAC 密钥。签名输入是 `webhook-id + "." + webhook-timestamp + "." + 原始请求体` 的字节序列。必须使用 JSON 解码前的原始字节，不能重新序列化 JSON 后验签。建议同时拒绝与接收时间相差超过 5 分钟的请求，并保持服务器时钟同步；此时间窗口校验由接收端执行。

示例通知：

```json
{
  "type": "event",
  "id": "whe_example",
  "created_at": "2026-10-10T06:30:00Z",
  "data": {
    "type": "session.status_idled",
    "id": "sess_example"
  }
}
```

外层 `id` 用于去重，`data.type` 是订阅类型，`data.id` 是发生变化的资源 ID。线程事件另含 `session_thread_id`，凭证事件另含 `vault_id`。Webhook 的 `session.status_idled` 与 SSE 的 `session.status_idle` 名称不同，不应直接复用 SSE 类型判断。

例如，会话停止通知不能证明任务成功：用 `data.id` 查询会话事件中的停止原因、回答和产物。请求使用资源所在地域的服务地址与已绑定 API Key：

```bash
curl --fail-with-body -sS \
  "$AGENTAPI_BASE_URL/v1/sessions/$SESSION_ID/events?limit=100" \
  -H "x-api-key: $AGENTAPI_API_KEY"
```

根据[接入准备](../../quick-start/access.md)选择 `AGENTAPI_BASE_URL`；将 `$SESSION_ID` 替换为通知中的 `data.id`，有下一页时继续读取。删除事件对应的资源可能已无法查询，应按删除通知清理本地状态。

### 接收服务示例

下面的 Python 标准库示例验证原始请求体、时间戳和签名，然后把通知提交到带唯一事件 ID 的 SQLite 持久收件箱，提交成功后才返回 204。业务处理由另一个 worker 消费 `pending` 记录；不是收到 HTTP 请求就直接完成业务操作。

运行前设置 `AGENTAPI_WEBHOOK_SECRET` 和 `WEBHOOK_INBOX_PATH`，后者指向可写的持久磁盘路径。通过 HTTPS 反向代理把公网 443 地址转发至 `127.0.0.1:8000/agentapi`，保留原始请求体和三个签名头。此示例仅提供本地接收与持久化，不实现公网 TLS 部署或业务 worker。

```python
import base64
import hashlib
import hmac
import json
import os
import sqlite3
import time
from http.server import BaseHTTPRequestHandler, HTTPServer

secret = os.environ["AGENTAPI_WEBHOOK_SECRET"]
if not secret.startswith("whsec_"):
    raise ValueError("expected a whsec_ signing secret")
key = base64.b64decode(secret[6:], validate=True)
if len(key) != 32:
    raise ValueError("expected a 32-byte signing key")

inbox_path = os.environ["WEBHOOK_INBOX_PATH"]
with sqlite3.connect(inbox_path) as db:
    db.execute("PRAGMA journal_mode=WAL")
    db.execute("""
        CREATE TABLE IF NOT EXISTS webhook_inbox (
            event_id TEXT PRIMARY KEY,
            body BLOB NOT NULL,
            received_at INTEGER NOT NULL,
            status TEXT NOT NULL DEFAULT 'pending'
        )
    """)


def verify(headers, body):
    event_id = headers.get("webhook-id", "")
    timestamp = headers.get("webhook-timestamp", "")
    signatures = headers.get("webhook-signature", "")
    if not event_id or not timestamp.isascii() or not timestamp.isdigit():
        raise ValueError("missing or invalid signature headers")
    if abs(time.time() - int(timestamp)) > 300:
        raise ValueError("signature timestamp outside tolerance")
    signed = event_id.encode("utf-8") + b"." + timestamp.encode("ascii") + b"." + body
    expected = hmac.new(key, signed, hashlib.sha256).digest()
    for entry in signatures.split():
        version, separator, encoded = entry.partition(",")
        if version != "v1" or not separator:
            continue
        try:
            actual = base64.b64decode(encoded, validate=True)
        except ValueError:
            continue
        if hmac.compare_digest(expected, actual):
            return event_id
    raise ValueError("invalid signature")


class Receiver(BaseHTTPRequestHandler):
    def do_POST(self):
        if self.path != "/agentapi":
            self.send_error(404)
            return
        try:
            size = int(self.headers.get("Content-Length", "0"))
        except ValueError:
            self.send_error(400)
            return
        if not 0 < size <= 1024 * 1024:
            self.send_error(413)
            return
        self.connection.settimeout(10)
        try:
            body = self.rfile.read(size)
        except OSError:
            self.send_error(400)
            return
        if len(body) != size:
            self.send_error(400)
            return
        try:
            event_id = verify(self.headers, body)
        except ValueError:
            self.send_error(401)
            return
        try:
            event = json.loads(body)
            if (not isinstance(event, dict)
                    or event.get("type") != "event"
                    or event.get("id") != event_id
                    or not isinstance(event.get("data"), dict)):
                raise ValueError("invalid envelope")
        except (ValueError, UnicodeError):
            self.send_error(400)
            return
        try:
            with sqlite3.connect(inbox_path, timeout=5) as db:
                db.execute("PRAGMA synchronous=FULL")
                db.execute("""
                    INSERT INTO webhook_inbox (event_id, body, received_at)
                    VALUES (?, ?, ?)
                    ON CONFLICT(event_id) DO NOTHING
                """, (event_id, body, int(time.time())))
            # Exiting the transaction commits before acknowledgement.
        except sqlite3.Error:
            self.send_error(503)
            return
        self.send_response(204)
        self.end_headers()


HTTPServer(("127.0.0.1", 8000), Receiver).serve_forever()
```

业务 worker 应在完成可恢复的业务更新后再标记 `done`。同库的业务状态和消费进度可在一个事务中提交；调用第三方服务时使用业务幂等键或事务性 outbox，避免崩溃后重复执行。不要先把 ID 记为“处理完成”再执行业务操作。收件箱存储、访问权限、保留期限和 worker 失败重试由接收应用维护。

## 处理重试、重复与乱序

* 确认与重试：任意 2xx 表示确认。网络错误、超时或非 2xx 会触发有限重试；每条投递最多尝试 3 次，退避使用抖动，范围限制在 5–120 秒。只有可靠持久化、入队或事务性处理成功后才应返回 2xx。
* 重复投递：请求处理完成但确认响应丢失等情况可能产生重复。按外层 `id` 或 `webhook-id` 去重，不能按资源 ID 去重，同一资源可以有多个变更事件。
* 乱序：通知不保证顺序。需要当前状态时回查资源，避免较晚收到的旧通知覆盖新状态；已删除资源应结合本地记录处理。
* 有限重试后的恢复：端点有 MySQL 持久投递队列，可跨 AgentAPI 重启恢复，但不会无限重试，也不提供用户侧投递历史、测试发送或手动重放。关键业务应定期查询资源或事件历史对账。

接收服务应快速完成可靠入队，耗时业务交给 worker。默认单次发送超时为 10 秒（自托管部署可在服务配置中调整）；不要依赖长时间同步处理来确认通知。

## 管理和恢复端点

列表显示接收地址、订阅数量、状态及连续失败尝试次数；订阅配置在编辑侧栏按资源分组展示，停用原因可在状态提示和编辑侧栏查看。连续失败尝试次数不等于总投递次数或失败事件数；任意成功确认会重置失败窗口和计数。

| 操作   | 结果与注意事项                                               |
| ---- | ----------------------------------------------------- |
| 编辑   | 修改 URL、描述或订阅。修改 URL 不会自动恢复已停用端点。                      |
| 停用   | 不再接收新通知，并放弃未完成投递。已在途的 HTTP 请求无法保证撤回。                  |
| 重新启用 | 修复接收服务后恢复后续新事件；停用、创建之前或未订阅期间的事件不补发。                   |
| 轮换密钥 | 只显示一次新密钥，后续投递立即使用新密钥，没有新旧密钥宽限期。接收端应及时切换；已在途请求可能仍带旧签名。 |
| 删除   | 永久移除端点并放弃未完成投递。已在途请求无法保证撤回；重新接入需创建新端点。                |

3xx 重定向或解析到非公网地址会使端点立即停用。其他持续失败默认经过 24 小时失败窗口后自动停用，该阈值为服务端配置（自托管部署可调整）；“最多 3 次投递”不表示三次失败就立即停用整个端点。自动停用后先排查 URL、DNS、公网网络、签名配置及响应状态，再重新启用，并通过资源查询补齐业务状态。

轮换前准备接收服务的密钥更新流程。若需要不中断切换，可先创建使用新密钥的端点，接收端分别验签并统一去重，确认新端点正常后删除旧端点。同一资源变更的重复通知仍应按事件 ID 幂等处理。

## 可订阅事件

当前目录含 38 类事件，按以下资源分组。事件目录列出可订阅能力，事件是否发生仍取决于对应资源操作或执行结果。

| 资源     | 类型                                                                                                                                                                  |
| ------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 会话状态   | \`session.status\_scheduled\`、\`session.status\_run\_started\`、\`session.status\_idled\`、\`session.status\_rescheduled\`、\`session.status\_terminated\`             |
| 会话与线程  | \`session.thread\_created\`、\`session.thread\_idled\`、\`session.thread\_terminated\`、\`session.outcome\_evaluation\_ended\`、\`session.updated\`、\`session.deleted\` |
| Vault  | \`vault.created\`、\`vault.archived\`、\`vault.deleted\`                                                                                                              |
| 凭证     | \`vault\_credential.created\`、\`vault\_credential.archived\`、\`vault\_credential.deleted\`、\`vault\_credential.refresh\_failed\`（尚无触发点）                             |
| 智能体    | \`agent.created\`、\`agent.updated\`、\`agent.archived\`、\`agent.deleted\`                                                                                            |
| 部署     | \`deployment.created\`、\`deployment.updated\`、\`deployment.paused\`、\`deployment.unpaused\`、\`deployment.archived\`、\`deployment.deleted\`（尚无触发点）                   |
| 计划部署运行 | \`deployment\_run.started\`、\`deployment\_run.succeeded\`、\`deployment\_run.failed\`                                                                                |
| 环境     | \`environment.created\`、\`environment.updated\`、\`environment.archived\`、\`environment.deleted\`                                                                    |
| 持久记忆   | \`memory\_store.created\`、\`memory\_store.archived\`、\`memory\_store.deleted\`                                                                                      |

`deployment_run.*` 仅用于计划触发，不由手动运行触发；`succeeded` 表示成功创建并初始化会话，不代表智能体任务最终成功。会话的后续结果需同时订阅相应会话状态并读取事件，详见[定时部署](../sessions-and-tasks/scheduled-deployments.md)。

## Portal 管理接口边界

Portal 使用以下专用控制面接口，身份由登录会话和 Portal 代理确定。这些路径不接受 MaaS API Key，不是应用直接调用的 CMA API；共享 `/v1` 不提供 Webhook 管理接口。

| 操作       | 方法与路径                                                              |
| -------- | ------------------------------------------------------------------ |
| 列表与创建    | \`GET\` / \`POST /portal/v1/webhook\_endpoints\`                   |
| 详情、更新和删除 | \`GET\` / \`POST\` / \`DELETE /portal/v1/webhook\_endpoints/{id}\` |
| 轮换签名密钥   | \`POST /portal/v1/webhook\_endpoints/{id}/rotate\_secret\`         |
| 事件目录     | \`GET /portal/v1/webhook\_event\_types\`                           |

创建只提交 `url`、可选 `description` 和 `event_types`；更新可提交这些字段及 `status: enabled \| disabled`。服务端限制账户和地域范围，拒绝用户指定 owner 或租户 ID。列表使用 `limit` / `page` 游标；事件目录提供类型、分组、说明和 `emitted` 状态，便于区分历史可订阅项与当前有触发点的项。管理操作通过 Portal 完成，应用接收通知时无需调用这些控制面接口。
