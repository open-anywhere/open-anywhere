<!-- 来源：https://developer-doc.qiniu.com/products/agentapi/best_practices/13629-handle-operational-tasks-with-scheduled-tasks | 由 sync-docs.py 自动生成，请勿手改 -->

# 使用定时任务处理运营任务



最近更新时间: 2026-10-20 13:56:39

电商平台每天上午需要核对前一天的订单流水与账务记录，输出差异清单，并把需人工跟进的异常推送到运营群。下面这条流程将核对规则配置到 AgentAPI 的定时部署中：先手动验证会话和产物，再开启计划；每次运行都经过结果验收、幂等处理和通知对账。

## 一次订单核对如何完成

一次核对从业务数据到运营动作，经过以下步骤：

1. 应用准备一个能访问订单和账务数据的智能体，以及承载执行的云端环境。
2. 部署模板中的初始消息写明日期范围、差异判定规则和产物路径。智能体通过已经配置的业务查询工具读取数据，把结果写入 `/mnt/session/outputs/order-reconciliation.md`；没有差异时也要写出“无差异”。
3. 应用先调用部署的手动运行接口，取得 `session_id`，再从事件和文件列表确认本次核对确实完成。
4. 验证通过后开启 cron 计划。每次计划触发都会创建一个新的会话和部署运行记录。
5. 只有产物通过业务校验后，应用才把异常推送到运营群或创建后续工单。

首次创建部署时省略 `schedule`，此时只能手动运行；验证通过后再更新计划。部署模板的核心请求如下：

```json
{
  "name": "每日订单对账",
  "agent": {"type": "agent", "id": "agt_...", "version": 1},
  "environment_id": "env_...",
  "initial_events": [
    {
      "type": "user.message",
      "input": {
        "parts": [
          {"type": "text", "text": "按 Asia/Shanghai 时区核对前一自然日的订单与账务记录，生成 /mnt/session/outputs/order-reconciliation.md；列出差异、证据和需人工处理的项目；没有差异时写出无差异。"}
        ]
      }
    }
  ]
}
```

应用应把部署 ID、业务日期和每次运行返回的 `session_id` 关联保存，后续通知和补偿都以这组 ID 回查事实。

## 固定任务输入和交付格式

定时运行最容易出现的问题，是同一份任务在不同日期输出不同格式，导致下游无法解析。把每次都不变的规则写进部署的初始消息；需要具体日期、批次号时，由业务系统在启动核对任务前提供：

| 内容       | 写入位置                         | 示例                                             |
| -------- | ---------------------------- | ---------------------------------------------- |
| 数据范围     | 初始消息                         | 只处理前一天 `00:00:00` 至 `23:59:59` 的订单             |
| 差异规则     | 初始消息                         | 金额差异、状态不一致、缺少账务记录分别统计                          |
| 交付路径     | 初始消息                         | `/mnt/session/outputs/order-reconciliation.md` |
| 业务日期、批次号 | 业务系统记录；需要传入具体值时，写入新会话的首条任务消息 | `business_date=2026-05-08`                     |
| 后续动作     | 应用逻辑                         | 产物通过校验后才推送运营群                                  |

任务消息应同时规定成功和失败的输出，避免智能体只返回一句自然语言：

```text
按 `Asia/Shanghai` 时区核对前一自然日的订单与账务记录。
先读取订单系统和账务系统的数据，分别统计订单数、账务记录数、金额合计和状态分布。
将差异按“金额不一致、状态不一致、缺少账务记录、缺少订单”分类，逐条列出订单号、两侧值和证据。
把结果写入 /mnt/session/outputs/order-reconciliation.md，文件必须包含业务日期、数据源查询时间、统计摘要、差异清单和需人工处理项。
没有差异时也必须生成文件，并在摘要中明确写出“无差异”。无法访问数据源或无法完成比对时，必须说明失败原因，不得把“没有数据”当作“没有差异”。
```

定时部署的 `initial_events` 是静态模板，AgentAPI 不会渲染 `{{business_date}}` 这类模板变量。本例在消息中明确时区和相对日期规则，应用仍应保存业务日期，并在产物验收时核对报告中的日期。

如果任务必须接收业务系统计算的日期或批次号，由业务调度器先创建新会话，订阅事件流后，再一次性发送包含具体值和完整核对要求的首条 `user.message`。部署的 `run` 会在返回前提交并启动 `initial_events`，不能通过向返回的会话追加消息，保证这些参数用于已经启动的任务。历史日期补跑也应采用显式日期输入。

## 手动验证和结果判断

开启计划前先调用部署的 `run` 端点。运行成功只表示会话创建和初始事件提交成功，不表示订单已经核对完成：

```bash
run=$(curl --fail-with-body --silent --show-error \
  --request POST "$AGENTAPI_BASE_URL/v1/deployments/$DEPLOYMENT_ID/run" \
  --header "x-api-key: $AGENTAPI_API_KEY")
session_id=$(jq -er '.session_id' <<<"$run")
```

如果响应没有 `session_id`，先读取运行记录中的 `error.type`；不要继续按成功流程处理。拿到 `session_id` 后，先订阅事件流，再持续到本轮停止：

```bash
curl --fail-with-body --silent --show-error --no-buffer \
  "$AGENTAPI_BASE_URL/v1/sessions/$session_id/events/stream" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "accept: text/event-stream"
```

`session.status_idle` 只表示本轮停止，应用还要读取 `stop_reason.type` 和产物：

| 停止原因                | 处理                                        |
| ------------------- | ----------------------------------------- |
| `end_turn`          | 检查最终回答、产物和业务规则；不能直接判定成功                   |
| `requires_action`   | 按 `stop_reason.event_ids` 处理工具调用或确认，再继续消费 |
| `interrupted`       | 记录中断；已执行的外部操作不会回滚                         |
| `budget_reached`    | 检查已完成部分和产物，再决定是否继续                        |
| `retries_exhausted` | 读取相邻 `session.error`，修复后重新提交输入            |

对订单对账来说，真正的成功条件是“产物存在且内容通过业务校验”。例如，智能体如实报告“没有找到上周数据”也可能以 `end_turn` 结束，应用仍需根据业务日期和数据源状态判断这是否合格。

如果需要让内容质量由系统判定，可以给会话定义结果，读取 `span.outcome_evaluation_end.result` 或 `outcome_evaluations[].result`；只有 `satisfied` 表示达标，`max_iterations_reached` 表示轮数耗尽仍未达标。

## 产物交付

在部署提示中约定智能体把最终文件写入 `/mnt/session/outputs/`，应用不需要在每次任务消息中重复目录规则。文件收集可能比会话进入 `idle` 晚几秒；列表中暂时没有文件时，可以最多重试 3 次，间隔 2 秒、5 秒和 10 秒，并结合事件流确认本轮执行已经结束，再判定产物缺失。

输出限制是硬边界：单个文件和本轮输出目录中的文件总计都不能超过 100 MiB；任一限制超过时，本轮输出文件收集整体失败。大量图表或原始数据应留在业务系统，交付文件只保留汇总和必要证据。

文件列表采用分页响应，下一页请求时把 `next_page` 原样作为 `page` 参数传入。业务侧下载文件后，还应解析报告中的日期、记录数和差异数量，不能只检查文件存在。

列出并下载本次会话的产物：

```bash
curl --fail-with-body --silent --show-error \
  --get "$AGENTAPI_BASE_URL/v1/files" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --data-urlencode "scope_id=$session_id"

curl --fail-with-body --silent --show-error \
  "$AGENTAPI_BASE_URL/v1/files/$FILE_ID/content" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --output order-reconciliation.md
```

应用可以把报告解析成结构化记录，再决定是否发送通知：业务日期不匹配、统计字段缺失、差异数量与明细条数不一致时，运行应标记为“待人工复核”，不能直接进入成功分支。

## 验证通过后开启计划

只有手动运行的会话和产物都通过验收后，才为部署加入计划。下面的请求将任务设置为每天北京时间凌晨 4 点运行；更新成功后核对响应中的 `schedule.upcoming_runs_at`：

```bash
curl --fail-with-body --silent --show-error \
  --request POST "$AGENTAPI_BASE_URL/v1/deployments/$DEPLOYMENT_ID" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --data '{"schedule":{"type":"cron","expression":"0 4 * * *","timezone":"Asia/Shanghai"}}'
```

## 重复触发和业务幂等

超时或恢复过程中可能重试执行，发送通知、创建工单和写入对账表都必须有业务幂等键。调度中断也可能漏掉计划：如果触发窗口已过期且尚未创建运行记录，该次计划会被跳过，不会补建运行记录或自动补执行。应用必须按业务日期核对缺失任务，不能仅靠运行记录或 Webhook 判断是否全部完成。

部署运行记录会返回 `deployment_id` 和 `trigger_context.scheduled_at`，可以把它们组合作为幂等键。下面是运行记录中的字段示例，不是创建部署时提交的请求字段：

```json
{
  "type": "deployment_run",
  "deployment_id": "depl_01xyz",
  "trigger_context": {"type": "schedule", "scheduled_at": "2026-05-09T00:00:00Z"}
}
```

建议在业务表中对 `(deployment_id, scheduled_at)` 建唯一索引，用来归并同一计划的重复处理。通知交付还需要可恢复的状态，不能把记录插入成功当成通知已发送：

1. 产物验收后，在同一业务事务中保存核对结果和待发送通知记录（outbox），通知初始状态为 `pending`。
2. 发送器领取记录后标记为 `sending` 并设置领取期限；发送成功后才标记为 `sent`。失败或领取超时的未完成记录应能重新领取。
3. 如果通知已发出但成功状态尚未保存，重试仍可能重复发送。向下游传递同一幂等键；下游不支持去重时，先查询通知或工单是否已创建，无法确认的交付进入人工复核。

`trigger_context.type` 为 `manual` 的运行不参与计划去重。手动补偿和显式日期补跑还应使用业务幂等键，例如 `(任务类型, business_date)`，与原计划的核对结果和通知记录关联，避免重复交付。

## 计划时间和通知

cron 使用标准 POSIX 分钟级表达式和 IANA 时区。夏令时开始时不存在的挂钟时间不会触发，夏令时结束时重复的挂钟时间会触发两次。调度器会在计划时间后增加 5 秒至 9 分钟的抖动，上限按运行间隔的 15% 计算后再限制在该范围内。

创建或更新部署后，核对 `schedule.upcoming_runs_at`。业务不能接受遗漏或重复时，把计划安排在当地时间凌晨 1–3 点窗口之外，或直接使用 UTC。

Webhook 只负责通知，不是任务结果：

* `deployment_run.succeeded` 只表示会话创建和初始化成功；收到后仍要用 `data.id` 查询运行记录，再读取会话事件和产物。
* `deployment_run.*` 只由计划触发产生，手动运行不会产生这些事件。
* Webhook 可能重复或乱序；端点停用期间的事件不会补发，关键业务需要按计划时间定期对账。

对账时，按计划时间列出应有的运行和业务结果，与业务表中的完成记录比较，补齐缺失项。

## 失败与补偿

| 现象                  | 处理                                                        |
| ------------------- | --------------------------------------------------------- |
| 运行记录没有 `session_id` | 查看 `error.type`，修复环境、资源或凭证问题后手动重跑。                        |
| 会话停止但没有产物           | 检查 `stop_reason`、最终回答、输出路径和 100 MiB 限制；确认后再重跑。            |
| 同一差异重复推送            | 用计划或业务幂等键关联交付记录；下游使用同一键去重，结果不明时先查询再重试。                    |
| 已有通知记录但未发送          | 检查 `pending` 或领取超时的 `sending` 记录，重新领取；确认发送成功后才标记为 `sent`。 |
| 某个业务日期没有运行记录或产物     | 调度中断可能跳过已过期计划；按该业务日期创建新会话补跑，并复用业务幂等键。                     |
| 部署自动暂停              | 修复 `paused_reason.error.type`，手动验证后再恢复；暂停期间错过的计划不会补执行。    |
| Webhook 没有到达        | 回查运行记录和业务对账表，不要把“没收到通知”当成“没有任务”。                          |
| 智能体版本需要升级           | 更新部署的 `agent` 引用；已有部署不会自动跟随智能体新版本。                        |

## 上线验收

* [ ] 手动运行能取得 `session_id`，事件流能读到停止原因。
* [ ] 产物内容通过日期、记录数和差异规则校验。
* [ ] 手动验收通过后才加入 `schedule`。
* [ ] 业务表对 `(deployment_id, scheduled_at)` 建了唯一索引。
* [ ] 通知记录可重新领取，发送结果不明时能查询、去重或转人工复核。
* [ ] 按业务日期检查遗漏，并用显式日期和业务幂等键补跑。
* [ ] `upcoming_runs_at` 符合预期，已评估时区和夏令时。
* [ ] Webhook 验签、去重、持久化后再返回 2xx，并有定期对账。
* [ ] 产物大小在 100 MiB 限制内，收集失败能被识别和补偿。
* [ ] 智能体升级流程包含更新部署 `agent` 引用的步骤。
