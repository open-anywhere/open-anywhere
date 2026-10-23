<!-- 来源：https://developer-doc.qiniu.com/products/agentapi/glossary/13621-agentapi-glossary | 由 sync-docs.py 自动生成，请勿手改 -->

# 术语表



最近更新时间: 2026-10-13 17:20:58

| 术语      | 英文或字段        | 定义与详细说明                                                                                                                                                                                                                                                                                      |
| ------- | ------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| MaaS    | MaaS         | 七牛大模型推理服务，提供模型调用与 API Key 管理。AgentAPI 使用 MaaS API Key 鉴权，见[接入准备](../quick-start/access.md)。                                                                                                                                                                      |
| 智能体     | Agent        | 可复用、带版本的模型、提示、工具与运行时配置。见[智能体](../development-guidelines/agent-configuration/agent.md)。                                                                                                                                              |
| 智能体版本   | AgentVersion | 智能体的一份固定配置；已有会话继续引用固定版本。见[版本与会话](../development-guidelines/agent-configuration/agent.md)。                                                                                                                                           |
| 运行时     | `runtime`    | 智能体的执行方式，可选择 `claudecode`、`codex` 或 `native`。见[运行时和模型](../development-guidelines/agent-configuration/runtime-and-models.md)。                                                                                                        |
| 模型      | `model`      | 执行推理与生成的模型配置。见[模型选择](../development-guidelines/agent-configuration/runtime-and-models.md)。                                                                                                                                          |
| 环境      | Environment  | 定义工具执行所需的软件与网络条件。见[云端环境](../development-guidelines/execution-environments/environment.md)和[自托管执行器](../development-guidelines/execution-environments/self-hosted.md)。 |
| 会话      | Session      | 保存持续交互的历史、执行状态和文件，并引用固定的智能体版本。见[会话](../development-guidelines/sessions-and-tasks/session.md)。                                                                                                                                        |
| 执行轮次    | Turn         | 会话中一次由输入触发的执行，可以包含多次模型请求和工具调用。见[任务与执行轮次](../development-guidelines/sessions-and-tasks/session.md)。                                                                                                                                   |
| 事件      | Event        | 任务输入、智能体输出、工具活动或生命周期变化的记录。见[事件和事件流](../development-guidelines/sessions-and-tasks/events.md)。                                                                                                                                         |
| 事件流     | SSE          | 通过持久 HTTP 连接回放历史并推送实时事件。见[订阅事件流](../development-guidelines/sessions-and-tasks/events.md)。                                                                                                                                            |
| 工具      | Tool         | 智能体调用的执行能力，包括内置工具和应用提供的自定义工具。见[工具](../development-guidelines/agent-configuration/tools.md)。                                                                                                                                         |
| MCP 连接器 | MCP          | 通过 Model Context Protocol 接入远程工具服务。见[MCP 连接器](../development-guidelines/agent-configuration/mcp.md)。                                                                                                                                |
| 智能体技能   | Agent Skills | 按任务需要加载的工作说明和辅助文件。见[Agent Skills](../development-guidelines/agent-configuration/skills.md)。                                                                                                                                         |
| 凭证保管库   | Vault        | 管理会话使用的凭证及其注入配置。见[凭证管理](../development-guidelines/data-and-integrations/vaults.md)。                                                                                                                                               |
| 持久记忆    | Memory Store | 在会话之外管理可复用的记忆及其版本。见[构建持久记忆](../development-guidelines/data-and-integrations/memory.md)。                                                                                                                                           |
| 结果      | Outcome      | 通过评分标准定义预期结果，供执行后的评估与修订使用。见[定义结果](../development-guidelines/sessions-and-tasks/outcomes.md)。                                                                                                                                         |
| 定时部署    | Deployment   | 按计划或手动触发会话的配置。见[定时部署](../development-guidelines/sessions-and-tasks/scheduled-deployments.md)。                                                                                                                                        |
| 自托管执行器  | Worker       | 领取自托管环境中的工作并回传执行结果的程序。见[自托管执行器](../development-guidelines/execution-environments/self-hosted.md)。                                                                                                                                |
