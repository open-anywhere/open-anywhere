<!-- 来源：https://developer-doc.qiniu.com/products/agentapi/product_introduction/13583-agentapi-concepts | 由 sync-docs.py 自动生成，请勿手改 -->

# 基本概念



最近更新时间: 2026-10-13 15:30:58

AgentAPI 通过智能体与环境保存可复用配置，通过会话与事件管理具体任务的执行和交互。

## 智能体（Agent）

智能体是一份可复用的行为配置，包含运行时、模型、系统提示、工具和技能等内容，用于定义角色、规则和可用能力。创建智能体只保存配置，任务在会话中执行；同一个智能体可以供多个会话使用。配置发生变化时生成新版本。详见[创建与管理智能体](../development-guidelines/agent-configuration/agent.md)。

## 运行时与模型

运行时（`runtime`）组织模型调用与工具执行，模型（`model`）负责推理与生成，两者分别配置在智能体上。AgentAPI 支持 `claudecode`、`codex` 和 `native`：前两者分别由 Claude Code SDK 与 Codex 执行引擎驱动，Native 由 AgentAPI 管理执行循环。运行时会影响工具能力和执行方式，模型需选择当前账户与区域可调用的型号。详见[运行时和模型](../development-guidelines/agent-configuration/runtime-and-models.md)。

## 工具与技能

[工具](../development-guidelines/agent-configuration/tools.md)提供文件读写、代码执行或外部服务调用等操作能力；[技能（Agent Skills）](../development-guidelines/agent-configuration/skills.md)保存可复用的工作说明和辅助文件，例如分析步骤或报告规范。工具决定能够执行哪些操作，技能指导如何完成任务。技能中的脚本仍需相应工具和环境依赖才能运行。

## 环境与沙箱

环境是一份可复用的执行配置，定义工具所需的软件包、网络等条件，在创建会话时引用。沙箱是会话执行文件和代码操作的实际工作空间。多个会话可以共用云端环境配置，各自使用独立沙箱，文件系统状态不会共享。详见[配置云端环境](../development-guidelines/execution-environments/environment.md)；使用自有执行资源时，见[自托管执行器](../development-guidelines/execution-environments/self-hosted.md)。

## 会话与执行轮次

会话保存一个任务的输入、输出、工具活动和交互历史，引用固定的智能体版本与环境。向会话发送消息后开始执行，一次由输入触发的执行称为轮次（Turn），可以包含多次模型请求和工具调用。同一任务的补充与修订可继续使用原会话，独立任务应创建新会话；已有会话不会随智能体更新而切换版本。详见[创建会话](../development-guidelines/sessions-and-tasks/session.md)。

## 事件与任务结果

事件记录用户输入、智能体回答、工具活动和状态变化。应用可以查询事件历史，或通过事件流（SSE）实时观察进度。提交输入成功表示任务已接收；会话进入 `idle` 表示当前执行已停止，仍需结合停止原因、回答和产物判断任务结果。详见[事件和事件流](../development-guidelines/sessions-and-tasks/events.md)。

## 配置与任务的关系

![应用通过 Portal 或 API 提交任务；会话引用智能体版本与环境，并通过事件返回执行记录](https://developer-doc.qiniu.com/docs-assets/products/agentapi/product_introduction/13583-agentapi-concepts/FsLKckGhC3SggayLZ2pj9q9GdSxr-30b532f10481.webp)

具体操作见[在 Portal 中构建](../quick-start/portal.md)或[通过 API 构建](../quick-start/quickstart.md)。
