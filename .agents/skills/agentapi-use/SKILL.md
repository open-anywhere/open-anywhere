---
name: agentapi-use
description: 七牛云 AgentAPI（托管智能体框架）使用手册。当任务是接入或调用 AgentAPI（创建/配置智能体、选择 claudecode/codex/native 运行时、云端环境与沙箱、会话与事件流、定时部署、文件产物、持久记忆、MCP、Agent Skills、GitHub、Webhook、凭证保管库、自托管 worker、用 Anthropic SDK 接入）时使用。关键词：AgentAPI、七牛 AgentAPI、托管智能体、agent.qiniuapi.com、agent.sufy.com、claudecode、codex、native、session、events、deployment。
---

# 七牛云 AgentAPI 使用

AgentAPI 是七牛云在托管环境中运行的可配置智能体框架，适合运行数分钟到数小时、需要多次工具调用的长任务与异步工作。应用通过 API 或 Anthropic SDK 配置智能体、提交任务，并通过事件流获取回答、工具调用和文件产物。API 兼容 Claude Managed Agents 协议。

本 skill 的 `references/` 是官方文档站 <https://developer-doc.qiniu.com/products/agentapi/> 的完整离线镜像（34 篇，由脚本抓取转换），**回答具体问题前先读对应参考文档，不要凭记忆编接口字段**。

## 平台速览

| 项目 | 值 |
| --- | --- |
| Portal（控制台） | <https://portal.qiniu.com/agentapi> |
| 服务地址（华东） | `https://agent.qiniuapi.com` |
| 服务地址（美西） | `https://agent.sufy.com` |
| API 认证 | `x-api-key: <七牛 MaaS API Key>`；Key 在 Portal 创建并自动绑定到 AgentAPI 账户；JSON 请求另加 `content-type: application/json` |
| 区域隔离 | 华东与美西的资源、数据彼此独立，不跨区域共享；切换地址不会迁移资源 |
| 通用响应 | JSON；响应头含 `X-Trace-ID`；列表返回 `{ "data": [...], "next_page": "..." }`，`next_page` 原样作为下次 `page` 参数 |
| 常见错误 | `400` 参数/事件格式无效，`401` Key 缺失或无效，`404` 资源不存在，`409` 状态不允许，`503` 依赖服务暂不可用 |
| SDK | Anthropic 官方 SDK（Python/TypeScript/Java/Go/C#/Ruby/PHP），设置 `base_url` 为区域地址（不加 `/v1`）、`api_key` 为 MaaS Key |
| API 参考 | 端点速查 `references/api-quick-reference.md`；完整索引 `references/api/openapi.md`（含 OpenAPI YAML 下载） |

## 核心概念

- **智能体（agent）**：带版本的可复用配置，包含 `runtime`、`model`、系统提示、工具、MCP 服务器和技能。创建只保存配置；更新生成新版本；会话固定创建时引用的版本。
- **运行时（runtime）**：`claudecode`、`codex`、`native`。前两者分别由 Claude Code SDK 与 Codex harness 驱动，继续会话依赖原沙箱可恢复；`native` 由 AgentAPI 管理模型循环，按事件重建上下文，不依赖沙箱存续。省略时默认 `claudecode`，建议显式指定。
- **模型（model）**：必须使用当前账户和区域可调用的七牛 MaaS 模型 ID，可附 `effort`（如 `{"type": "high"}`）。
- **工具与技能**：内置工具集 `agent_toolset_20260401`；MCP 工具；应用执行并回传结果的自定义工具；Agent Skills 是基于文件系统的技能包（只支持上传的 `custom` 技能，`type: "anthropic"` 无法解析）。
- **环境（environment）**：可复用的执行配置。`cloud` 由七牛 LAS Sandbox 为每个会话创建独立沙箱；`self_hosted` 由自有 worker 提供工作区。运行时会话间不共享文件系统。
- **会话（session）**：保存任务的输入、输出、工具活动和历史，引用固定智能体版本与 `environment_id`；发送 `user.message` 后才开始执行，一次输入触发的执行称为轮次（Turn）。
- **事件（event）**：记录输入、回答、工具调用和状态变化。`POST /v1/sessions/{id}/events` 写入，`GET .../events` 查历史，`GET .../events/stream` 走 SSE。
- **结果判断**：`session.status_idle` 只表示本轮停止，必须结合 `stop_reason.type`（`end_turn`/`interrupted`/`requires_action`/`retries_exhausted`/`budget_reached`）和产物验收，不能把空闲当作成功。
- **定时部署**：`/v1/deployments` 按 cron 定期启动会话，运行记录在 `/v1/deployment_runs`。
- **数据与集成**：Files（输入/产物）、Memory Stores（跨会话记忆）、GitHub、Webhook、Vaults（第三方 MCP 凭证）。

## 任务路由：先读哪一篇

| 你要做的事 | 先读 |
| --- | --- |
| 了解能力边界、选择 Portal 还是 API | `references/product-introduction/overview.md` |
| 理解智能体、运行时、环境、会话、事件的关系 | `references/product-introduction/concepts.md` |
| 创建 API Key、选区域、配置鉴权 | `references/quick-start/access.md` |
| 在 Portal 中配置并测试智能体 | `references/quick-start/portal.md` |
| 用 HTTP 或 SDK 跑通完整任务 | `references/quick-start/quickstart.md` |
| 选择运行时、模型与执行环境 | `references/development-guidelines/agent-configuration/runtime-and-models.md` |
| 创建/更新智能体、版本与生命周期 | `references/development-guidelines/agent-configuration/agent.md` |
| 内置工具、自定义工具、网页访问 | `references/development-guidelines/agent-configuration/tools.md` |
| 连接远程 MCP 服务器与工具权限 | `references/development-guidelines/agent-configuration/mcp.md` |
| 配置工具调用权限与人工确认 | `references/development-guidelines/agent-configuration/permissions.md` |
| 编写、上传并附加 Agent Skills | `references/development-guidelines/agent-configuration/skills.md` |
| 创建会话、挂载文件/GitHub/记忆 | `references/development-guidelines/sessions-and-tasks/session.md` |
| 发送事件、SSE 消费、判断停止原因 | `references/development-guidelines/sessions-and-tasks/events.md` |
| 查询、更新、归档、删除会话 | `references/development-guidelines/sessions-and-tasks/session-lifecycle.md` |
| 定义可衡量的结果与评分标准 | `references/development-guidelines/sessions-and-tasks/outcomes.md` |
| 配置 cron 定时部署与运行记录 | `references/development-guidelines/sessions-and-tasks/scheduled-deployments.md` |
| 上传任务材料、下载并验收产物 | `references/development-guidelines/data-and-integrations/files.md` |
| 跨会话持久记忆 | `references/development-guidelines/data-and-integrations/memory.md` |
| 挂载 GitHub 仓库与提交 Pull Request | `references/development-guidelines/data-and-integrations/github.md` |
| 配置 Webhook 通知与验签 | `references/development-guidelines/data-and-integrations/webhooks.md` |
| 管理第三方 MCP 凭证（Vault） | `references/development-guidelines/data-and-integrations/vaults.md` |
| 创建云端环境、网络与软件依赖 | `references/development-guidelines/execution-environments/environment.md` |
| 云端沙箱的预装软件与版本 | `references/development-guidelines/execution-environments/cloud-environments.md` |
| 部署自托管 worker | `references/development-guidelines/execution-environments/self-hosted.md` |
| 自托管的数据流与安全责任 | `references/development-guidelines/execution-environments/self-hosted-security.md` |
| 从 Responses API 或 LangChain 迁移 | `references/development-guidelines/migration.md` |
| 认证、请求格式、分页、事件类型 | `references/api/api-conventions.md` |
| 查询全部端点与 OpenAPI 下载 | `references/api/openapi.md`、`references/api-quick-reference.md` |
| 安装各语言 Anthropic SDK | `references/sdk/sdk-download.md` |
| 查中英文术语 | `references/glossary/glossary.md` |
| 参考完整业务案例 | `references/best-practices/code-review-assistant.md`、`sre-intelligent-o-m.md`、`handle-operational-tasks-with-scheduled-tasks.md` |

完整镜像目录（含原文链接）见 `references/INDEX.md`，接口端点总表见 `references/api-quick-reference.md`。

## 常见动作

### 配置环境并验证鉴权

```bash
export AGENTAPI_BASE_URL="https://agent.qiniuapi.com"   # 美西使用 https://agent.sufy.com
export AGENTAPI_API_KEY="<maas api key>"
export AGENTAPI_MODEL_ID="<当前区域可用的模型 id>"

curl -s "$AGENTAPI_BASE_URL/v1/agents" -H "x-api-key: $AGENTAPI_API_KEY"
```

- `AGENTAPI_MODEL_ID` 从[七牛 MaaS 模型管理页](https://portal.qiniu.com/ai-inference/model)获取，须确认 Key 可调用。
- `401` 先检查 Key 是否已在 Portal 绑定；换 Key 时先在 Portal 创建新 Key 再替换，撤销访问要在 Portal 删除旧 Key。
- 不要把真实 Key 写入代码、文档或版本库。

### 跑通一次任务（HTTP）

1. `POST /v1/agents`：传入 `name`、`runtime`、`model`、`system`、`tools`，保存返回的 `id` 与 `version`。
2. `POST /v1/environments`：例如 `{"name":"python-dev","config":{"type":"cloud","networking":{"type":"unrestricted"}}}`，保存 `id`。
3. `POST /v1/sessions`：传入 `agent`（ID 或带 `version` 的对象）和 `environment_id`，保存会话 `id`。
4. 先 `GET /v1/sessions/{id}/events/stream` 订阅 SSE，再 `POST /v1/sessions/{id}/events` 发送 `user.message`，内容放在 `input.parts`。
5. 等待本轮的 `session.status_idle`，检查 `stop_reason` 与回答；用 `GET /v1/files?scope_id={session_id}` 找到产物，再 `GET /v1/files/{id}/content` 下载验收。

完整可复制脚本（含事件去重、停止判断与文件验收）见 `references/quick-start/quickstart.md`。云端环境默认引导智能体把交付文件写到 `/mnt/session/outputs/`。

### 判断任务结果

- `end_turn`：本轮结束，仍需验收回答和产物；`native` 的人工中断也用 `end_turn`，结合 `user.interrupt` 区分。
- `requires_action`：按 `stop_reason.event_ids` 处理自定义工具结果或工具确认，处理后会话恢复执行。
- `interrupted`：记录中断并检查已完成的外部修改；中断不会回滚操作。
- `retries_exhausted` / `budget_reached`：结合 `session.error` 或预算与部分产物决定是否继续。
- 进入 `terminated` 的会话不能再接收输入；`session.error` 本身不代表会话终止。

### 用 Anthropic SDK 接入

```python
import os
from anthropic import Anthropic

client = Anthropic(
    api_key=os.environ["AGENTAPI_API_KEY"],
    base_url=os.environ["AGENTAPI_BASE_URL"],   # 区域根地址，不加 /v1
)

agent = client.beta.agents.create(
    name="编程助手",
    model={"id": os.environ["AGENTAPI_MODEL_ID"]},
    system="你是一名编程助手。",
    extra_body={"runtime": "claudecode"},       # AgentAPI 扩展字段放在请求体顶层
)
```

- `runtime`、`agentapi_options` 等扩展字段 SDK 类型未必声明：Python 用 `extra_body`，TypeScript 用第二个参数的 `body`。
- 会话必须引用 `environment_id`；`runtime` 在智能体上配置，不要作为会话字段传入。

## 使用约定

- 文档镜像只读：不要直接改 `references/` 下的文件，需要更新时运行同步脚本。
- 引用文档时给出本地路径（如 `references/api/api-conventions.md`）和原始 URL，便于用户核对。
- 接口细节（字段名、必填项、事件类型、错误码）以 `references/api/` 和对应专题为准；站点文档更新后跑一次同步即可。
- 涉及生产凭证、删除/归档资源、自托管 worker 等操作时，先确认用户意图并说明影响。

## 更新文档

```bash
python3 .claude/skills/agentapi-use/scripts/sync-docs.py
```

脚本从 <https://developer-doc.qiniu.com/llms.txt> 发现 AgentAPI 章节，逐页抓取 `llms.mdx` 的 Markdown 源，本地化链接、重建 `references/INDEX.md`、`references/api-quick-reference.md` 与 `docs-manifest.json`。仅依赖 Python 标准库；`--check` 只比对不写盘，`--only api,quick_start` 只同步指定章节（不改全量索引）。计费文档已按产品范围排除。
