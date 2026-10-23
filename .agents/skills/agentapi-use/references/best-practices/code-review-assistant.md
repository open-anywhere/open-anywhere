<!-- 来源：https://developer-doc.qiniu.com/products/agentapi/best_practices/13627-code-review-assistant | 由 sync-docs.py 自动生成，请勿手改 -->

# 使用 MCP、Skills 做代码审查助手



最近更新时间: 2026-10-20 13:59:10

某效能团队希望每个 Pull Request 在合并前都完成一次自动审查：读取完整仓库，检查缺陷、安全问题和团队规范，并把带文件与行号的结论直接提交到 PR。下面这条流程从 PR 的 head commit 开始，创建一次隔离的 AgentAPI 会话，运行检查并通过 GitHub MCP 提交 review。

## 一次审查如何完成

一次 PR 审查由业务系统或 CI 触发，输入是仓库地址、PR 编号和当前 head SHA：

1. 应用创建一个使用 `claudecode` 运行时的智能体，配置 GitHub MCP 服务器和云端环境。
2. 应用创建新会话，挂载目标仓库，把 `checkout` 固定到触发时的 PR head SHA，并通过 `vault_ids` 关联 GitHub 凭证。
3. 应用先订阅会话事件流，再发送审查要求。智能体读取 diff、相关文件并运行仓库测试。
4. 智能体通过 GitHub MCP 读取 PR 信息，并提交带文件和行号的 review；审查任务不修改代码、不推送分支。
5. 应用检查事件流和 PR 页面，确认 review 已创建、结论类型正确、意见引用的是本次 head SHA。

审查结果的交付物是 GitHub PR review。只在 `agent.message` 中输出一段报告，不能完成这条业务流程。

## 配置代码审查智能体

代码审查智能体的核心是审查流程。工作流提示词、审查规范 Skill 和 GitHub MCP 分别配置流程、判断标准和交付动作；仓库资源与凭证则为本次会话提供代码和访问权限：

| 配置项        | 负责什么                        | 本例中的配置位置                           |
| ---------- | --------------------------- | ---------------------------------- |
| 工作流提示词     | 固定审查步骤、范围、输出格式和禁止的操作        | 智能体的 `system`                      |
| 审查规范 Skill | 固定团队要检查的规则和意见格式             | 仓库的 `.claude/skills` 或智能体附加的 Skill |
| GitHub MCP | 读取 PR 上下文、提交 review 和查询提交结果 | `mcp_servers` 与 `mcp_toolset`      |
| 仓库资源       | 提供本次审查使用的代码和 head commit    | 会话的 `resources`                    |

先把不随 PR 变化的流程写入智能体配置，再在每次任务中传入仓库、PR 编号和 head SHA。这样可以复用同一套审查标准，同时保证每次审查都针对确定的代码版本。

### 配置 GitHub MCP

仓库挂载和 GitHub MCP 分别承担两件事：

| 能力               | 配置                                                 | 用途                           |
| ---------------- | -------------------------------------------------- | ---------------------------- |
| 读取待审查代码          | 会话资源中的 `github_repository` 和 `authorization_token` | 克隆仓库、检出指定 commit、读取文件和运行测试   |
| 读取 PR 并提交 review | 智能体声明 GitHub MCP，会话 `vault_ids` 引用匹配的凭证            | 查询 PR、提交 review；不把令牌写入提示词或仓库 |

GitHub MCP 服务器地址使用 `https://api.githubcopilot.com/mcp/`。凭证的 `mcp_server_url` 必须与该地址一致，可以使用 `static_bearer` 或 `mcp_oauth`；仓库令牌和 GitHub MCP 凭证分别按最小权限配置。审查通常只需要仓库内容读取权限和 Pull Request 的读写权限，不需要仓库内容写入权限。

### 创建代码审查智能体

使用 `claudecode` 运行时，并在 `tools` 中启用与服务器名称一致的 `mcp_toolset`。工作流提示词负责约束审查过程，Skill 负责团队规则，GitHub MCP 负责与 PR 交互。下面的 cURL 示例把三者配置到同一个代码审查智能体中：

```bash
agent_id=$(curl --fail-with-body --silent --show-error \
  --request POST "$AGENTAPI_BASE_URL/v1/agents" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --data @- <<JSON | jq -r '.id'
{
  "name": "代码审查助手",
  "runtime": "claudecode",
  "model": {"id": "$AGENTAPI_MODEL_ID"},
  "system": "你是代码审查助手。每次任务都按以下流程执行：先确认仓库、Pull Request 编号和当前 head SHA；读取完整 diff 及相关文件；运行仓库已有的检查命令；按照 code-review Skill 判断问题等级并给出文件、行号、证据和修改建议；通过 GitHub MCP 将结论提交到当前 PR，并在最终回答中汇总检查范围、命令和 review ID。只审查当前 head SHA，不修改代码，不推送分支；无法确认的风险标记为待讨论。",
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

需要逐次审批 GitHub MCP 操作时，应改用支持 `always_ask` 的 Native 智能体，在 `requires_action` 时由应用确认是否允许工具调用；`claudecode` 的 GitHub MCP 示例使用 `always_allow`。

### 配置审查规范 Skill

把团队真正会执行的规则写成 Skill，并在智能体配置中附加它。Skill 只描述检查范围、判断标准和输出格式；代码读取、测试执行和 GitHub 操作由智能体的工具完成。每次任务只传入本次 PR 的上下文，不要把稳定规则重复写进任务消息。

```md
---
name: code-review
description: 按团队的后端服务规范审查 Pull Request，输出可执行的修改建议。
---

# 后端服务代码审查

## 必须检查
- 错误处理：返回的 error 是否被检查，是否有被忽略的 `_`。
- 并发安全：共享 map/slice 是否加锁，goroutine 是否有退出路径。
- 资源释放：文件、连接、response body 是否关闭。
- 对外接口变更：是否同步更新 swagger 和调用方。

## 输出要求
- 每条意见包含文件路径、行号和修改建议。
- 区分「必须修改」与「建议改进」。
- 不确定的问题标注为待讨论，不要断言。
```

`claudecode` 会在会话启动时从已挂载仓库发现 `.claude/skills`；也可以上传自定义技能后在智能体上显式附加。每次审查使用新会话，避免复用旧会话中的技能和仓库状态。

## 创建审查会话

应用从 PR webhook 或 CI 事件中取得 `PR_NUMBER`、`REPOSITORY_URL` 和 `PR_HEAD_SHA`，然后创建会话。`agent` 必须引用上一步创建的 `runtime: "claudecode"` 智能体；`vault_ids` 中的凭证用于 GitHub MCP，资源中的 `authorization_token` 用于检出仓库。

```json
{
  "agent": {"type": "agent", "id": "agt_...", "version": 1},
  "environment_id": "env_...",
  "vault_ids": ["vlt_github_..."],
  "title": "审查 PR #128",
  "metadata": {
    "repository": "org/payment-api",
    "pull_request": "128",
    "head_sha": "<PR head SHA>"
  },
  "resources": [
    {
      "type": "github_repository",
      "url": "https://github.com/org/payment-api",
      "mount_path": "/workspace/payment-api",
      "checkout": {"type": "commit", "sha": "<PR head SHA>"},
      "authorization_token": "<repository read token>"
    }
  ]
}
```

`checkout` 只在创建会话时应用。PR head 变化后不要复用旧会话；应用应重新取得 SHA 并创建新会话。若提交不存在、PR 引用在拉取期间变化或仓库准备失败，本轮应失败并回到触发系统处理，不要审查其他 commit。

## 发送审查任务

先打开事件流，再发送 `user.message`，只传入本次 PR 的上下文和交付对象。审查步骤和输出规范已经由智能体的工作流提示词与 Skill 固定：

```text
请审查 org/payment-api 的 PR #128，当前 head SHA 为 <PR head SHA>。
本次范围是该 head SHA 的全部变更；审查结果提交到这个 PR。若发现必须修改的问题，提交为 REQUEST_CHANGES；只有建议时提交为 COMMENT。
```

本例使用 `claudecode`，GitHub MCP 调用会作为 `agent.tool_use` 和 `agent.tool_result` 出现。应用通过调用事件的 `tool_type` 识别 GitHub MCP 工具，再用 `tool_use_id` 关联对应结果。至少保存以下事件，便于审计和补偿：

```text
session.status_running
agent.tool_use           查询 PR、读取 review 或提交 review
agent.tool_result        对应调用的 GitHub MCP 返回结果
agent.message            审查摘要和 review ID
session.status_idle      stop_reason.type = end_turn
```

`end_turn` 只表示本轮结束。应用还要检查提交 review 的调用所对应的 `agent.tool_result`，确认没有 `is_error: true` 且结果显示提交成功，再到 PR 页面确认 review 确实存在。若调用结果缺失或无法确认，先查询 PR 上已有的 review，再决定是否重试提交。

## 验收和补偿

建议在业务侧为 `(repository, pull_request, head_sha)` 建立一次审查记录。相同 head SHA 的重复触发先查询已有结果；head SHA 变化时创建新会话。

| 现象               | 原因                                       | 处理                                                                                              |
| ---------------- | ---------------------------------------- | ----------------------------------------------------------------------------------------------- |
| PR 上没有 review    | GitHub MCP 凭证缺失、服务器地址不匹配、权限不足，或任务只要求输出报告 | 检查智能体的服务器和工具配置、Vault 凭证、Pull Request 权限及对应的 `agent.tool_result`，确认没有已提交的 review 后再重跑本次 head SHA |
| review 引用了错误的行号  | 会话复用了旧检出，或 PR head 在任务期间变化               | 重新取得 head SHA，创建新会话并重新审查                                                                        |
| 只覆盖了部分文件         | 审查范围未写清，或上下文/执行预算不足                      | 在任务中要求逐文件检查，必要时按目录拆分审查                                                                          |
| 测试失败但 review 已提交 | 审查意见和测试结果没有分开验收                          | 将测试结果记录为独立状态，不把 review 创建成功当作测试通过                                                               |
| 凭证过期后仍能读取代码      | 仓库检出和 GitHub MCP 调用使用了不同凭证               | 同时轮换仓库令牌和 GitHub MCP 凭证，再创建新会话                                                                  |

## 验收清单

* [ ] 触发记录保存了仓库、PR 编号和 head SHA。
* [ ] 智能体使用 `claudecode`，声明了 GitHub MCP 服务器和 `mcp_toolset`。
* [ ] GitHub MCP 的 `mcp_server_url` 与服务器地址完全一致。
* [ ] 仓库资源固定到本次 PR head SHA。
* [ ] 仓库读取令牌和 GitHub MCP 凭证均限制到目标仓库。
* [ ] 任务明确要求通过 GitHub MCP 提交 review，不修改代码、不推送。
* [ ] 事件流中保存了文件读取、测试和 GitHub MCP 工具结果。
* [ ] PR 页面存在 review，意见带文件与行号，结论类型正确。
* [ ] 相同 head SHA 的重复触发不会重复交付，head SHA 变化会启动新会话。
