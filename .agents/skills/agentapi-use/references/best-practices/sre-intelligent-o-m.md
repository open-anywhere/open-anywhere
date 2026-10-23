<!-- 来源：https://developer-doc.qiniu.com/products/agentapi/best_practices/13628-sre-intelligent-o-m | 由 sync-docs.py 自动生成，请勿手改 -->

# 使用自托管 worker 处理内网运维



最近更新时间: 2026-10-20 13:58:32

互联网公司的 SRE 团队每天处理来自生产环境的告警：先查日志，再比对发布和配置变更，最后检索内部运行手册。把这条处置链路接入 AgentAPI 后，智能体可以在同一个会话中汇总证据、生成处置建议，并在重启服务等高风险动作前停下来等待人工决定。

本文以支付服务的生产告警为例，说明如何把 AgentAPI 会话、自托管 worker、内网工具、人工确认和记忆存储组合起来。worker 只负责访问内网和执行工具，模型请求、会话状态和事件仍由 AgentAPI 管理。

## 一次告警如何完成

一次告警从触发到关闭，通常经过以下步骤：

1. 告警系统把告警 ID、服务名、环境、时间窗口和当前操作者写入会话输入。应用先校验操作者是否有权访问该服务和环境。
2. 会话引用 `self_hosted` 环境和 `runtime: "native"` 智能体。环境中的 worker 领取工作项，在自有网络执行工具。
3. 智能体先调用 `logs_query`、`changes_list` 和 `runbook_search` 等只读工具，交叉比对日志、变更和历史处置记录。
4. 智能体输出根因假设、证据、影响范围和建议动作。worker 或负责启动它的 launcher 将处置报告保存到约定的工作目录；自托管环境不会自动收集云端输出目录。
5. 如果建议包含重启服务、修改配置或回滚发布，智能体进入 `requires_action`。应用检查工单状态和审批人后，再决定是否执行写操作。
6. 告警关闭后，把已经人工确认的根因和处置步骤写入记忆存储；实时日志和当前变更仍通过工具查询。

可以先用一条只读任务验证链路：

```text
调查生产环境支付服务告警 ALRT-128，时间范围为最近 30 分钟。
先使用只读工具查询日志、发布变更和运行手册，输出根因假设、证据、影响范围和建议动作。
将处置报告写入工作目录中的 incident-ALRT-128.md；任何重启或配置修改先停下并等待人工确认。
```

## 配置告警处置智能体

告警处置智能体的核心是调查顺序和审批边界。把稳定的处置流程写入工作流提示词或 Skill，把实时数据和动作拆成工具，再用 `self_hosted` worker 承担内网访问：

| 配置项                  | 负责什么                      | 本例中的配置位置                                                |
| -------------------- | ------------------------- | ------------------------------------------------------- |
| 工作流提示词或 Skill        | 规定先调查、后判断、再审批的处置顺序，以及报告格式 | 智能体配置或仓库中的 Skill                                        |
| 只读工具                 | 查询日志、发布变更和运行手册，提供实时证据     | worker 注册的 `logs_query`、`changes_list`、`runbook_search` |
| 高风险工具                | 执行重启、回滚等会改变生产状态的动作        | worker 注册的 `service_restart`，由业务服务审批                    |
| `self_hosted` worker | 在内网执行工具，并回传工具结果           | 环境工作队列和 worker SDK                                      |
| 记忆存储                 | 保存人工确认过的根因和处置步骤           | 会话的记忆挂载                                                 |

可以把下面的流程写入智能体的 `system` 或 Skill。每次告警输入只需要补充告警 ID、服务、环境和时间窗口：

```text
你是生产告警处置助手。先使用只读工具查询日志、发布变更和运行手册，交叉验证根因假设；再输出证据、影响范围和建议动作。任何重启、配置修改或回滚都必须先检查工单和审批人，等待业务系统批准后才能调用高风险工具。完成调查后生成包含告警 ID、时间窗口、证据、结论和后续动作的处置报告；无法确认根因时明确列出缺失证据，不要把查询失败当成服务正常。
```

### 创建告警处置会话

先在 Portal 或 API 中准备上面的智能体、工具和一个 `self_hosted` 环境。应用收到告警后创建会话：

```json
{
  "agent": {"type": "agent", "id": "agt_...", "version": 1},
  "environment_id": "env_self_hosted_...",
  "metadata": {
    "alert_id": "ALRT-128",
    "service": "payment-api",
    "environment": "prod"
  }
}
```

创建会话后，先订阅事件流，再发送告警输入。结构化输入使用 `input.parts`：

```bash
curl --fail-with-body --silent --show-error --no-buffer \
  "$AGENTAPI_BASE_URL/v1/sessions/$SESSION_ID/events/stream" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "accept: text/event-stream" &

curl --fail-with-body --silent --show-error \
  --request POST "$AGENTAPI_BASE_URL/v1/sessions/$SESSION_ID/events" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --data @- <<'JSON'
{
  "events": [
    {
      "type": "user.message",
      "input": {
        "parts": [
          {"type": "text", "text": "调查生产环境支付服务告警 ALRT-128，先只读查询日志、变更和运行手册，生成处置报告；任何写操作先等待人工确认。"}
        ]
      }
    }
  ]
}
JSON
```

`self_hosted` 会把工作项放入环境队列；没有可用 worker 时，工作项会留在队列中，不会自动改用云端环境。

## 工具设计和内网访问

把内网能力拆成模型可以理解、应用可以授权的独立工具。读取、写入和高风险操作不要共用一个 `action` 参数：

| 工具                | 用途                       | 执行方式               |
| ----------------- | ------------------------ | ------------------ |
| `logs_query`      | 按服务、时间窗和关键字查询日志          | worker 执行的只读工具     |
| `changes_list`    | 查询发布、配置和 feature flag 变更 | worker 执行的只读工具     |
| `runbook_search`  | 检索内部知识库和历史处置记录           | worker 执行的只读工具     |
| `service_restart` | 重启指定服务                   | worker 执行，调用前由应用审批 |

应用在创建会话前校验操作者身份和服务范围；worker 在工具实现中再次校验参数、限制时间窗口、裁剪结果并脱敏。工具返回的内容会进入模型上下文，因此日志工具只返回摘要和必要行，不返回整份原始日志。

智能体上的工具声明必须与 worker 注册的名称一致：

```json
{
  "type": "custom",
  "name": "logs_query",
  "description": "查询指定服务在时间窗内的错误日志，返回摘要和最多 20 条相关日志行。",
  "input_schema": {
    "type": "object",
    "properties": {
      "service": {"type": "string", "description": "服务名，例如 payment-api"},
      "environment": {"type": "string", "enum": ["prod", "staging"]},
      "since": {"type": "string", "description": "RFC3339 起始时间"},
      "keyword": {"type": "string", "description": "可选关键字过滤"}
    },
    "required": ["service", "environment", "since"]
  }
}
```

worker 侧使用 SDK 注册同名实现。下面是 Go worker 的关键注册片段：

```go
logsQuery := toolrunner.NewBetaTool(
    "logs_query",
    "查询指定服务在时间窗内的错误日志，返回摘要和最多 20 条相关日志行。",
    anthropic.BetaToolInputSchemaParam{
        Properties: map[string]any{
            "service":     map[string]any{"type": "string"},
            "environment": map[string]any{"type": "string", "enum": []string{"prod", "staging"}},
            "since":       map[string]any{"type": "string"},
            "keyword":     map[string]any{"type": "string"},
        },
        Required: []string{"service", "environment", "since"},
    },
    func(ctx context.Context, input logsQueryInput) (anthropic.BetaToolResultBlockParamContentUnion, error) {
        rows, err := queryInternalLogs(ctx, input)
        if err != nil {
            return anthropic.BetaToolResultBlockParamContentUnion{}, fmt.Errorf("日志查询失败: %w", err)
        }
        return textResult(rows), nil
    },
)

worker := environments.NewEnvironmentWorker(client, environments.EnvironmentWorkerOptions{
    EnvironmentID:  environmentID,
    EnvironmentKey: environmentKey,
    Workdir:        "/workspace",
    ToolsFunc: func(env *agenttoolset.AgentToolContext) []anthropic.BetaTool {
        return append(agenttoolset.BetaAgentToolset20260401(env), logsQuery)
    },
})
```

上面的片段需要导入 `fmt`。单次工具调用应设置短于 worker 上限的业务超时；查询失败时返回非空 `error`，SDK 会把错误说明转换为 `is_error: true` 的工具结果并回传，会话可以继续处理失败。返回的错误说明也应脱敏，不包含凭证或整份原始日志。`ant` CLI worker 不能注册自定义工具，需要使用 SDK worker 或应用自己实现环境工作队列协议。

## 数据边界和凭证

告警输入、工具定义、调用参数和工具结果会经过 AgentAPI 与模型服务；只有 worker 发起的内部系统请求留在自有网络。直接配置的远程 MCP 由 AgentAPI 执行，服务端必须能够访问对应端点；需要使用内网网络或本地凭证的能力，应封装成 worker 自定义工具。

把三类凭证分开管理：

| 凭证                | 用途               | 保存位置             |
| ----------------- | ---------------- | ---------------- |
| 已绑定的 MaaS API Key | 创建会话、读取事件和查询账户资源 | 应用服务端，不放入 worker |
| 环境密钥              | 领取指定环境的工作项并回传结果  | worker 的密钥管理系统   |
| 会话级 `secret`      | 访问本会话获准使用的记忆存储   | 只传给处理该会话的执行实例    |

`vault_ids` 只能选择账户下的凭证，不能替代业务用户的权限校验。谁能查看哪个环境的日志，应由应用根据登录身份、服务目录和工单权限决定。

## 人工确认和记忆沉淀

权限策略的确认范围取决于工具执行位置：

* AgentAPI 执行的 Native 内置工具或 MCP 工具可以使用 `always_ask`，会话会停在 `requires_action` 等待确认。
* 自定义工具由 worker 或应用执行，审批必须写在工具实现和业务服务中；`always_ask` 不会自动拦截重启等内部操作。

一次可能有多个待处理工具调用，应用必须遍历 `stop_reason.event_ids`，逐个审批或回传结果。只处理其中一个会让会话继续停在 `requires_action`。

本例中的 `service_restart` 是自定义工具，`always_ask` 和 `user.tool_confirmation` 都不会自动拦截它。审批必须写在工具实现和业务服务中：先查询工单状态和审批人，再决定是否调用重启接口；拒绝时返回可解释的工具结果，并把审批人、工单号和决定记录到审计日志。下面的 `user.tool_confirmation` 示例只适用于由 AgentAPI 执行的 Native 内置工具，不适用于本例的自定义工具。

对于由 AgentAPI 执行的 Native 内置工具，审批完成后回传 `user.tool_confirmation`：

```bash
curl --fail-with-body --silent --show-error \
  --request POST "$AGENTAPI_BASE_URL/v1/sessions/$SESSION_ID/events" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --data '{
    "events": [
      {"type": "user.tool_confirmation", "tool_use_id": "<event_id>", "result": "allow"}
    ]
  }'
```

记忆存储用于保存已经确认的结论，不用于代替实时查询：

* Skill 保存稳定的排查手册和输出规范。
* 自定义工具读取实时日志、变更和运行手册。
* 记忆存储保存经过人工确认的根因、修复步骤和适用条件。

自托管 SDK worker 会下载、挂载并同步记忆目录。写入工具返回成功只代表本地副本已修改；任务结束后应查询服务端记忆内容确认上传完成。`read_only` 只限制文件工具和记忆写回，不会把整个本地目录变成只读。

告警是否可以关闭，也应由业务系统根据事实判定，而不是直接使用会话的 `end_turn`：

| 业务状态          | 必须具备的证据             | 后续动作             |
| ------------- | ------------------- | ---------------- |
| 已定位并已恢复       | 根因、影响范围、修复动作和监控恢复时间 | 更新工单并关闭告警        |
| 已定位但未恢复       | 根因和回滚或修复计划          | 保持告警，创建待办并通知值班人员 |
| 证据不足          | 日志、变更或运行手册查询失败记录    | 不关闭告警，补充数据后继续会话  |
| 工具或 worker 失败 | 工具错误、工作项状态和实例日志     | 按故障级别转人工处理       |

## 隔离、清理和失败恢复

worker 以自身进程权限运行，隔离和清理由部署方负责：

* 使用可信镜像、非 root 用户和最小 Linux capabilities。
* 通过 VPC、防火墙或沙箱网络策略限制 worker 出站访问。
* 只挂载本任务需要的目录，不挂载宿主机凭证和其他会话数据。
* 停止 worker 前先取消正在处理的工作，为工具结果回传、记忆同步和目录清理留出时间。
* 强制停止只改变服务端工作项状态，不保证宿主机进程已经退出；部署方还要清理实际进程和资源。

| 现象                       | 处理                                                     |
| ------------------------ | ------------------------------------------------------ |
| 会话一直停在 `requires_action` | 遍历全部 `stop_reason.event_ids`，逐个处理工具结果或确认。              |
| worker 领取工作后立即失败         | 检查工具名称是否一致、记忆 `mount_path` 是否预先存在，以及工作项是否带会话 `secret`。 |
| 工具返回成功但记忆没有更新            | 查询服务端内容，检查同步间隔、挂载路径和 worker 退出流程。                      |
| 归档后内网仍有文件副本              | 按组织的保留策略清理 worker 工作目录、缓存和日志。                          |
| 强制停止后仍有进程                | 在自有基础设施上检查并清理实际进程、容器和网络资源。                             |

## 上线验收

* [ ] 告警输入包含告警 ID、服务、环境和时间窗口，并经过应用授权校验。
* [ ] 智能体使用 `runtime: "native"`，环境类型为 `self_hosted`。
* [ ] 只读工具和高风险工具分开声明，高风险动作在应用侧审批。
* [ ] 智能体声明与 worker 注册的工具名称完全一致。
* [ ] 工具结果经过裁剪和脱敏，内部实现设置了业务超时。
* [ ] MaaS API Key、环境密钥和会话 `secret` 没有进入同一执行环境。
* [ ] 报告由 launcher 从工作目录收集，记忆写入后通过服务端内容确认。
* [ ] 已验证 `read_only`、强制停止、重复工具调用和 worker 清理路径。
