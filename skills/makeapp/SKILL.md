---
name: makeapp
description: Use AgentAPI to build and validate a self-contained Open Anywhere Node.js web-app artifact from a user requirement, with OpenAPI business operations, Render launch contract, and private agent context for voice control.
---

# Open Anywhere 应用构建

本技能用于把一个自然语言应用需求转换为可由 Open Anywhere Render 直接运行的 `artifact.tar.gz`。典型输入是“创建一个多人知识竞赛”“做一个聚餐投票屏幕”或其他需要在大屏上运行、由语音入口控制的应用。

应用本身只接收结构化 HTTP 参数。调用 Agent 负责语音识别、声纹得到 `user_id`、自然语言理解、语义判断和选择 operation；应用负责业务状态、校验、幂等和公共 UI。不要把灵矽声纹字段、MCP 配置或 AgentAPI 凭证写进应用包。

## 先确认边界

- 一期只生成 Node.js 20+ 应用；运行端不执行安装、构建或编译。
- 产物必须自包含、使用内存状态，不依赖外部数据库、CDN 或运行时网络。
- Render 负责 Chromium、截图/视频流和投屏；应用只提供页面、业务 API 和健康检查。
- 需求中的语音动作必须转成稳定的结构化操作，不能让应用解析自然语言答案。
- 不要把用户原话、标准答案或评分规则放进公共页面、SSE、公共 GET 响应或可朗读文本。

## 产物契约

在 `/mnt/session/workspace/build-app/` 创建应用，并把其内容归档到 `/mnt/session/outputs/artifact.tar.gz`。归档根目录必须直接包含：

```text
app/
  app.js
  package.json
  public/
    index.html
    styles.css   # 可选
    ui.js        # 可选
  assets/        # 可选
openapi.yaml
launch.json
```

`launch.json` 只能声明应用名、Node 入口和健康检查约定，例如：

```json
{
  "name": "quiz",
  "runtimeExecutable": "node",
  "runtimeArgs": ["app/app.js"],
  "healthPath": "/health",
  "readyTimeoutMs": 10000
}
```

不要写入 `port`，不要在 `runtimeArgs` 固定 `--port`。应用读取 Render 注入的 `PORT`，`HOST` 未设置时使用 `0.0.0.0`；缺少或非法 `PORT` 时应向 stderr 报错并非零退出。入口以 artifact 根目录为工作目录，必须能直接执行 `node app/app.js`。

## 必须实现的运行接口

- `GET /` 返回可由 Chromium 直接加载的大屏 UI。
- `GET /health` 在页面和业务路由准备好后返回 HTTP 200、JSON `{"status":"ok"}`。
- `/api/*` 提供 OpenAPI 中声明的业务操作；UI 和 API 共享同一份内存状态。
- 推荐提供 `GET /openapi.json`，内容必须与根目录 `openapi.yaml` 等价。
- 页面通过轮询或 SSE 反映外部 HTTP 操作；不要要求应用必须连接灵矽才能工作。
- 应以前台进程运行，日志写 stdout/stderr，收到 SIGTERM 时关闭 HTTP/SSE/定时器并退出。

## OpenAPI 和响应模型

`openapi.yaml` 必须是合法 OpenAPI 3.1。每个 `/api/*` 操作必须有唯一稳定的 `operationId`、面向调用 Agent 的 `description`、完整参数、请求体和成功/错误响应。使用相对服务地址，例如 `servers: [{url: "/"}]`。

成功响应统一使用：

```json
{
  "message": "已进入下一题。",
  "data": { "question_id": "q2" },
  "agent_context": {
    "allowed_operations": ["submit_judgement", "next_question"]
  }
}
```

- `message` 必须是安全、简短、可朗读的语义描述；成功和错误响应 schema 都必须声明必需的字符串 `message`。
- `data` 是业务结果，不能包含本应只给模型的答案上下文。
- `agent_context` 是模型内部辅助上下文，不是安全边界。控制面/工具桥接必须确保它不进入 TTS、大屏公共状态或浏览器可见接口；若应用无法区分工具调用和浏览器调用，应把私有上下文改为独立的受控工具响应。
- 错误 JSON 使用扁平结构 `{message, code}`，不要嵌套 `{status, body}`。用稳定 code 区分参数错误、阶段冲突、过期 ID 和幂等冲突。

对知识竞赛等需要模型判定的场景，推荐接口形状：

```text
POST /api/confirm_ready   {user_id, action_id}
POST /api/start_game      {action_id}
POST /api/reply           {user_id, question_id, answer_right, action_id}
POST /api/next_question   {action_id}
POST /api/reset_game      {round_id?, action_id}
```

`reply` 只接收结构化 `answer_right`，不要接收或回显 `answer_text`。调用 Agent 从私有上下文判断“月亮”是否等价于“月球”，然后发送 `answer_right: true`。不明确的回答先澄清，不能自动判错。

## 状态、身份和幂等

- 读操作不得推进状态；开始、提交、下一步、结束和重置必须是显式写操作。
- 参与者使用通用 `user_id` 和 `display_name`。声纹映射由调用方完成，应用不猜身份。
- 如果需求要求依次确认参与者，必须维护 roster 顺序；乱序请求返回 `409 SEQUENCE_REQUIRED`，且不改变状态。
- 所有写操作支持 `action_id`。同一 ID 和相同请求载荷重放原始响应，不重复改变状态；同一 ID 携带不同载荷返回 `409 ACTION_CONFLICT`。
- 参数、用户、阶段、`round_id`、`question_id` 校验必须先完成，再写状态或幂等表。非法请求不能消耗 action ID。
- 旧轮次、旧题目和旧操作 ID 必须返回可识别错误，例如 `STALE_ROUND`、`QUESTION_EXPIRED`、`ACTION_CONFLICT`。
- 题目标准答案、接受的同义表达和评分规则只通过受控的 `agent_context` 提供给模型；绝不能进入 `/api/state`、SSE、HTML、JS 或公共 UI。

## 构建和自测顺序

1. 从需求提取参与者、状态、操作、可朗读 `message`、模型私有上下文和验收案例。
2. 先设计 OpenAPI，再实现状态机和 UI；每个页面按钮必须调用同一 `/api/*` 操作。
3. 用注入的测试 `PORT` 从 artifact 根目录启动；禁止依赖当前目录、全局包或构建沙箱文件。
4. 验证 `/health`、`/`、`/openapi.json`、每个成功操作和主要错误操作。
5. 验证完整成功流程、乱序确认、重复请求、同 ID 异载荷、未知 ID、旧轮次/题目、重置和 SIGTERM。
6. 扫描公共响应、SSE、HTML、JS 和静态资源，确认没有标准答案、评分规则、原始回答或凭证。
7. 用另一个端口重新启动，确认应用没有固化端口；校验 OpenAPI 3.1，并核对运行时 `/openapi.json` 与 YAML 等价。
8. 只有实际通过的检查才能写入最终说明。清理测试进程和临时文件，再执行：

```sh
tar -czf /mnt/session/outputs/artifact.tar.gz -C /mnt/session/workspace/build-app .
```

最终回答必须说明产物路径、实际运行时、通过的检查和未完成的检查；不要把测试脚本或密钥打进 artifact。

## AgentAPI 控制面使用

本技能由 AgentAPI 构建智能体执行时，控制面按 AgentAPI 官方文档创建资源和会话。接口字段必须以随仓库提供的 `agentapi-use` 技能及其 references 为准，不要凭记忆猜字段。

推荐生命周期：

1. 发布阶段创建或复用云端 environment，创建/更新带本技能的 builder agent，保存固定的 `agent_id`、版本和 `environment_id`。
2. 每个 `create-app` 创建一个 session，固定 agent 版本，在 metadata 中保存 `app_id`；同一 `app_id` 不重复创建会话。
3. 订阅 session 事件流后发送 `user.message`，内容包含具体需求、参与者、状态和验收案例；不要把 API key 放进消息。
4. 等待 `session.status_idle`，同时检查 `stop_reason.type`。只有 `end_turn` 且产物实际存在、可下载、可校验时才算构建成功；`idle` 本身不是成功证明。
5. 通过 Files API 找到 `artifact.tar.gz`，下载到控制面临时目录，校验归档根结构、OpenAPI、`launch.json`、启动和健康检查，再交给 Render。
6. 失败时保存事件和诊断，按需向同一会话提交明确的修复任务；不要在控制面直接替换生成代码并宣称 AgentAPI 已修复。
7. 构建结束归档或清理会话、临时文件和测试进程。API key 只从控制面安全配置读取，不写入 skill、日志、artifact 或响应。

详细字段、事件分页、Files 下载和会话生命周期请按 `.agents/skills/agentapi-use/references/quick-start/quickstart.md`、`references/development-guidelines/sessions-and-tasks/events.md`、`references/development-guidelines/data-and-integrations/files.md` 和 `references/development-guidelines/agent-configuration/skills.md` 查阅。
