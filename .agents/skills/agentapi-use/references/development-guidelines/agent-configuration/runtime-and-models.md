<!-- 来源：https://developer-doc.qiniu.com/products/agentapi/development_guidelines/agentapi-agent-configuration/13589-agentapi-agent-configuration-runtime-and-models | 由 sync-docs.py 自动生成，请勿手改 -->

# 运行时和模型



最近更新时间: 2026-10-13 15:31:14

智能体的 `runtime` 选择执行引擎，`model` 选择模型；创建会话时用 `environment_id` 选择工具执行环境。

## 运行时的执行方式

### Claude Code

`claudecode` 由 Claude Code SDK 驱动，在云端沙箱中组织模型与工具调用。继续会话依赖原沙箱可恢复。

### Codex

`codex` 以 Codex harness 作为智能体循环（agent loop）的主体驱动，在云端沙箱中组织模型与工具调用。继续会话同样依赖原沙箱；两个运行时的版本与预装软件见[云端环境参考](../execution-environments/cloud-environments.md)。

### AgentAPI Native

`native` 由 AgentAPI 直接管理模型循环，根据会话事件重建上下文，不依赖特定沙箱存续。文件和命令工具在云端沙箱或自托管 worker 中执行；需要长期保留的产物，写入 `/mnt/session/outputs/` 后可通过[文件交付](../data-and-integrations/files.md)下载。Native 不能使用 Claude Code 与 Codex 运行时特有的工具能力。

## 按场景选择配置

先按场景选定运行时，再确定执行环境与工具：

| 场景                          | 起步配置                             | 选择理由与接入说明                                                                                                                                                                 |
| --------------------------- | -------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 首次接入，验证端到端流程                | `claudecode` + `cloud` + 内置工具集   | 工具链开箱即用，按[快速开始](../../quick-start/quickstart.md)即可跑通。                                                                                            |
| 已有 Claude Code 或 Codex 使用经验 | `claudecode` 或 `codex` + `cloud` | 沿用熟悉的工具行为与提示词习惯，核对原工作流所需工具与软件即可。                                                                                                                                          |
| 应用需要执行自定义业务工具               | `native` + 自定义工具                 | 应用经事件[回传结果](../sessions-and-tasks/events.md)，链路最短；`claudecode`、`codex` 需要额外的工具桥接服务。        |
| MCP 工具调用需要应用审批              | `native` + MCP + `always_ask`    | 应用处理[工具确认](../sessions-and-tasks/events.md)；`claudecode`、`codex` 的 MCP 仅支持 `always_allow`。 |
| 文件与命令须在自有基础设施执行             | `native` + `self_hosted`         | 部署 [worker](../execution-environments/self-hosted.md)，在自有环境完成隔离与产物交付。                  |
| 跨任务读取项目规范                   | 云端 `claudecode`/`codex` + 只读记忆存储 | 云端 Native 不自动挂载记忆；其他方式见[记忆接入](../data-and-integrations/memory.md)。                      |

建议始终显式传入 `runtime`：省略时默认使用 `claudecode`，未必符合任务需要；对执行耗时敏感、或需要应用逐次确认工具的场景，建议评估并显式选择 `native`。网页访问、仓库挂载等细节在[工具](tools.md)与[GitHub](../data-and-integrations/github.md)专题中说明。没有既有偏好时，用相同输入与验收标准比较完成率、恢复行为、耗时和用量，再固定选择。

## 选择模型

从[七牛 MaaS 模型列表](https://portal.qiniu.com/ai-inference/model)选择当前账户与区域可用、Key 有权调用的模型，并核对其思考强度。智能体请求中的 `model` 可只传模型 ID，也可使用对象：

```json
{
  "id": "your-model-id",
  "effort": {"type": "high"}
}
```

模型配置与运行时都属于智能体版本，已有会话不会随更新切换。发布与临时覆盖见[会话配置](../sessions-and-tasks/session.md)。
