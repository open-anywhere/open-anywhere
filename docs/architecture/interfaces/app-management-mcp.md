# 应用管理 MCP

> 多应用并发构建、`app_id`↔session 绑定与 `update-app` 见 [multi-app-sessions.md](multi-app-sessions.md)；多租户 Portal 的角色、注册与服务绑定见 [多租户 Portal](portal.md)。

## 1. 范围

本文定义灵矽云（Linx）与控制面（gateway）之间的应用管理 MCP 契约：灵矽通过控制面管理应用从构建到运行的生命周期，并由现场设备发现屏幕、完成投屏。控制面不持有屏幕状态，也不提供屏幕查询工具。

- 灵矽只看见 MCP Tool，不直接接触 AgentAPI、Render 或投屏服务。
- 应用运行后的能力调用仍走 OpenAPI / HTTP 契约，不在本文展开。
- 控制面与 Render 的接口见[控制面与 Render 接口设计](gateway-render.md)，应用产物见[应用包设计](app-artifact.md)，系统整体架构与现场投屏见[架构总览](../overview.md)与[现场侧投屏](../home-casting.md)。

### 1.1 角色与调用方向

| 组件 | 本文中的职责 | 调用方 |
| --- | --- | --- |
| 灵矽云（Linx） | 理解用户意图、选择并调用 MCP Tool、组织播报话术 | 用户 |
| 控制面（gateway） | MCP Server、工具路由、应用注册表、构建与启动编排、按 owner 解析 Render | 灵矽 |
| AgentAPI | 异步构建应用 artifact | 控制面 |
| Render | 启动应用、提供应用 HTTP 与媒体流 | 控制面 |
| 设备端 MCP（固件 SSDP + DLNA） | 发现现场屏幕，把 `stream_url` 投到电视上 | 灵矽（设备侧），不经控制面 |
| Portal | 管理员引导账号、用户注册、为每个用户创建虚拟智能体并绑定服务 | 浏览器 |

灵矽是本文所述控制面 MCP 的唯一调用方。`app_id` 在 `create-app` 时分配，`get-app-status`、`start-app` 和 `stop-app` 都用它定位应用。AgentAPI 的 session id、Render 的 `instance_id` 和应用端口只在控制面内部使用，不通过 MCP 返回；唯一离开控制面的地址是 `start-app` 返回的 `stream_url`。

### 1.2 契约原则

- 工具名按灵矽侧约定使用 kebab-case。
- 只暴露稳定、与运行时无关的业务标识（`app_id`），加上设备端投屏需要的 `stream_url`；不暴露端口、运行目录或 Render 标识。
- `app_id` 必须属于当前设备和智能体（device + agent）。Linx 会话重连不影响归属，控制面拒绝跨设备或跨智能体访问。
- 构建是长任务：`create-app` 立即返回，状态通过 `get-app-status` 查询，构建完成通过 MCP 通知告知。
- 写操作按业务键收敛：同一 `app_id` 不重复创建构建 session 或应用进程。但 `create-app` / `update-app` 本身不是可重试的幂等调用——每次调用都会新建一轮构建；修订走 `update-app` 复用绑定 session。
- 应用按 owner 作用域：`app_id` 只在创建它的设备和智能体下可见，跨 owner 一律表现为 `APP_NOT_FOUND`。

### 1.3 身份与租户隔离（Portal 模式）

Portal 启用时，MCP 请求必须携带三个头：

| 头 | 含义 |
| --- | --- |
| `X-Linx-Device-Id` | 虚拟设备的 MAC，对应 `portal_devices.mac` |
| `X-Linx-Agent-Id` | Linx 智能体 ID，对应 `portal_devices.linx_agent_id` |
| `X-Linx-Session-Id` | 本次 MCP 会话 ID |

端点与校验时机：

| 端点 | 传输 | 校验时机 |
| --- | --- | --- |
| `/mcp` | Streamable HTTP，stateless | 每个请求独立校验三个头与 owner |
| `/sse` | Legacy SSE | 建立连接时校验一次 |
| `/mcp/message` | Legacy SSE 的消息端点 | 每次 POST 都要与已建立的 SSE 会话精确一致 |

校验规则：

- 缺少任一头：`/mcp` 和 `/sse` 返回 `400 MISSING_IDENTITY`，不建立会话。
- 设备不存在、未完成开通或用户被停用：`403 OWNER_NOT_FOUND` / `IDENTITY_NOT_REGISTERED` / `USER_DISABLED`。
- `/mcp/message` 的头缺失或与 SSE 会话不一致：`403 IDENTITY_MISMATCH`；随后再查库校验设备与用户状态，不只依赖建连时的检查。
- 身份校验器自身失败（例如数据库不可用，无法归入上面的归属错误）：`503 IDENTITY_CHECK_FAILED`，不返回工具结果。
- Portal 模式下 MCP Token 首次启动时自动生成并加密保存在数据库，管理员可在平台设置里重新生成；MCP 端点每次请求都读取当前值校验，读取失败时拒绝请求。
- 单租户部署（`portal.enabled=false`）不注入 owner 校验器，兼容旧的 `X-Linx-Device-Id` / `X-Linx-Session-Id` 行为。
- 设备必须在 `registered` 状态才能调用工具。每个账号最多 10 台设备（虚拟与硬件合计），一个智能体可以挂多台硬件；应用归属仍是 `(device, agent)`，与 v4 schema 和 [Portal 设计](portal.md) 一致。

## 2. 工具与状态

### 2.1 工具集

本节是工具清单的唯一权威来源；[架构总览](../overview.md)与[模块规格](../modules.md)只引用本节，不再各列一份。

**typed 工具**（`internal/gateway/mcp/tools.go`，带 JSON Schema 校验）：

| Tool | 作用 | 输入 | 输出 |
| --- | --- | --- | --- |
| `create-app` | 创建应用并启动 AgentAPI 构建 | `prompt`、可选 `player_names` | `app_id`, `state: building` |
| `update-app` | 在已有应用的绑定会话上提交修订并重建产物 | `app_id`, `prompt` | `app_id`, `state: building` |
| `get-app-status` | 查询应用状态 | `app_id` | 随状态变化，见第 4 章 |
| `get-app-operations` | 查询应用声明的 OpenAPI 业务操作 | `app_id` | `app_id`, `state`, `operations` |
| `start-app` | 启动已构建应用的云端运行时，返回流地址；不涉及屏幕 | `app_id` | `app_id`, `state: running`, `stream_url` |
| `stop-app` | 停止运行中的应用 | `app_id` | `app_id`, `state: idle` |
| `send-request` | 调用应用的业务操作 | `app_id`, `operation_id`, `parameters`, `body` | `status`, `body` |

**兼容层工具**（`internal/gateway/mcp/compatibility.go`，为旧版灵矽提示词保留）：

| Tool | 说明 | 输入 | 输出 |
| --- | --- | --- | --- |
| `get_openapi` | 读取应用完整 OpenAPI 文档 | `app_id` | 完整 OpenAPI JSON |
| `list-apps` | 列出当前设备和智能体的历史应用，最新的在前 | 无 | `apps` 数组，见 3.1 |
| `send_request` | `send-request` 的旧别名（下划线命名） | `app_id`、`operationId`、可选 `parameters`、`requestBody` | `{ok, status, data}`：`ok` 表示 2xx，`data` 是应用响应体 |
| `probe_speaker` | 声纹诊断，仅在 `speaker_probe: true` 时注册 | `speaker_id`、`speaker_name`、`context_source` | `{received, verified}`，不参与业务 |

`send-request` 与 `send_request` 走同一套转发逻辑，但返回结构不同：typed 版返回应用原始 `status` 与 `body`，兼容版返回 `{ok, status, data}`。灵矽新提示词应使用 typed 工具；兼容工具不要写进新的接入文档。

### 2.2 应用状态

应用状态只描述云端运行时的可用性。`running` 表示应用健康检查通过并已产出媒体流；画面投到哪块屏幕由现场设备决定，因此 `running` 的应用可以没有绑定任何屏幕。不向灵矽暴露独立的 `display.state`。

| state | 含义 |
| --- | --- |
| `building` | AgentAPI 正在构建，应用还不可启动 |
| `idle` | 构建完成并注册，进程未启动 |
| `running` | 应用健康检查通过，`stream_url` 可用；是否已投到屏幕不由本接口表示 |
| `error` | 构建、启动或运行失败 |

`error.stage` 区分失败阶段，取 `building`、`starting` 或 `running`；构建失败不会产生可启动的 `idle` 应用。构建成功但启动失败的应用仍处于 `error`，保留失败原因；再次 `start-app` 或 `stop-app` 会先清理上次启动遗留的资源，成功后回到 `idle`，因此连接类失败可以重试。产物结构错误（`ARTIFACT_INVALID`）不能靠重试解决，必须先 `update-app` 或重新 `create-app`。清理尚未完成时工具返回 `CLEANUP_PENDING`。错误结构与错误码见第 9 章。

## 3. `create-app`

输入是灵矽整理后的用户诉求，不是原始 ASR 文本：

```json
{
  "prompt": "做一个掷骰子游戏，点击按钮掷出两个骰子，显示点数和最近几次结果"
}
```

多人游戏同时传入已确认的初始玩家名称。名单使用独立字段，不只写在自由文本里：

```json
{
  "prompt": "做一个三人五题知识竞赛，依次确认玩家准备后开始答题",
  "player_names": ["韩鲁鹏", "邓军辉", "张之阳"]
}
```

`create-app` 的输入 schema：

```json
{
  "type": "object",
  "properties": {
    "prompt": {"type": "string", "minLength": 1},
    "player_names": {
      "type": "array",
      "items": {"type": "string", "minLength": 1},
      "uniqueItems": true,
      "description": "用户已确认的初始玩家名称，按加入或点名顺序填写；未知时不要编造"
    }
  },
  "required": ["prompt"],
  "additionalProperties": false
}
```

控制面启动构建时还会注入固定渲染画布（1280×720，16:9）和页面不得溢出的约束。画布是部署级运行参数，不出现在 `create-app` 的 MCP 输入中，调用方不需要也无法指定尺寸。

名单规则：

* 非游戏应用或尚未确定名单时可省略 `player_names`；空数组与省略均表示没有提供初始名单，不代表零人游戏。可先构建支持后续加入的应用。
* 灵矽只传已确认的参与者名称。当前说话人的声纹名字不代表所有玩家，也不表示其他人已加入。用户只说人数时不得补造姓名；应用必须预置完整名单时先询问。
* 控制面去除名称首尾空白，拒绝空白名称、非字符串和规范化后的重复名称，返回 `INVALID_PLAYER_NAMES`。保持输入顺序，不做模糊匹配或同音替换。重名时先请用户选择可区分的称呼。
* `prompt` 中明确指定的人数或名单与非空 `player_names` 冲突时，灵矽在调用前向用户确认，不能静默增删玩家。控制面对自然语言不再做第二次语义推断。
* 控制面将规范化后的 `player_names` 与 `prompt` 一起保存为构建输入，并通过构建消息交给 AgentAPI。生成应用据此建立初始玩家，直接使用 `user_name` 作为本局唯一标识和展示名称，不另行生成用户 ID 或维护名字到 ID 的映射；见 [应用包约定](app-artifact.md#32-openapi-和业务响应)。
* 名单中的名字表示参与者，不表示已准备好；准备状态由运行时的加入/确认操作更新。后续增减玩家走应用 OpenAPI，不通过再次 `create-app` 修改构建输入。
* 一期直接以用户名标识玩家。运行时灵矽把声纹 `speaker_name` 作为业务接口的 `user_name` 传入，例如 `{"user_name":"韩鲁鹏"}`。应用去除首尾空白后与本局名单精确匹配，不生成额外 ID，不要求声纹 `speaker_id`。未知名字不能直接参与答题或计分，应先确认或通过应用加入操作登记。本局内名字保持不变，改名流程不在一期范围内。

建议工具 description 包含："创建应用。多人游戏请在 player_names 中传入用户已确认的初始玩家姓名，保留顺序。未知姓名不要猜测；名单不完整且需要预置全部玩家时先询问。名单只定义初始玩家，不代表已准备好。"

控制面创建 AgentAPI session 后立即返回，不等待构建完成：

```json
{
  "app_id": "app_01J...",
  "state": "building"
}
```

同一设备和智能体可以同时有多个 `building` 应用：每次 `create-app` 都分配新的 `app_id`，并创建独立的 AgentAPI 构建会话。控制面在内部把 `app_id` 与 `session_id` 一对一绑定；`session_id` 不通过 MCP 返回。已返回的 `app_id` 用 `get-app-status` 或通知跟踪。**`create-app` 不是幂等操作**（MCP 注解 `idempotentHint=false`）：请求超时或结果不明时先 `list-apps` / `get-app-status` 确认，不要直接重试，否则会多建一个应用和一整轮构建。修改已有应用走 `update-app`，见第 3.2 节与 [multi-app-sessions.md](multi-app-sessions.md)。

### 3.1 `list-apps`

输入为空对象。返回当前设备和智能体构建过的应用，最新的在前，不受 Linx 会话重连影响。历史按 `(device, agent)` 分组，每组保留最近 10 个应用，不同设备或智能体之间互不挤占；超出时淘汰该组最旧且不在 `building` / `running`、也不持有下游资源的应用及其产物。`list-apps` 只返回当前设备和智能体的应用。

```json
{
  "apps": [
    {
      "app_id": "app_01J...",
      "name": "掷骰子游戏",
      "title": "掷骰子",
      "summary": "做一个掷骰子游戏…",
      "state": "idle",
      "startable": true,
      "built_at": "2026-10-24T08:20:00Z",
      "build_duration_seconds": 95,
      "operation_count": 1,
      "operations": ["rollDice"]
    }
  ]
}
```

- `startable` 表示现在能否直接 `start-app`；false 时 `not_startable_reason` 说明原因（构建中、运行中、构建失败、产物无效需 `update-app` 重建、产物丢失，或上次启动还在核对/清理）。
- 用户说“玩上次那个/之前那个”时，先 `list-apps` 让用户确认，再用 `start-app` 启动；不要重新 `create-app`。
- `list-apps` 是历史快照，启动时仍以 `get-app-status` / `start-app` 的结果为准。

### 3.2 `update-app`

向已有应用绑定的 AgentAPI 会话提交补充需求，异步重建产物。新应用用 `create-app`；运行中的业务操作仍用 `send-request`。

```json
{
  "app_id": "app_01J...",
  "prompt": "题目改成动物主题，仍保持五题、三人依次作答"
}
```

输入 schema：

```json
{
  "type": "object",
  "properties": {
    "app_id": {"type": "string", "minLength": 1},
    "prompt": {"type": "string", "minLength": 1}
  },
  "required": ["app_id", "prompt"],
  "additionalProperties": false
}
```

`prompt` 是整理后的修改说明，不是原始 ASR。本期不传 `player_names`。

立即返回：

```json
{
  "app_id": "app_01J...",
  "state": "building"
}
```

`update-app` 同样不是幂等操作（MCP 注解 `idempotentHint=false`）：每次成功提交都会多跑一轮构建并替换产物。提交超时或连接中断时先用 `get-app-status` 确认是否已进入 `building`，不要直接重发；返回 `APP_BUILDING` 也不代表可以插队。

状态与错误：

| 当前状态 | 行为 |
| --- | --- |
| `idle` | 进入 `building`，复用绑定 session 提交 `user.message`；保留上一版产物直到新产物校验成功 |
| `running` | 返回 `APP_RUNNING`，不改状态；先 `stop-app` 再更新 |
| `building` | 返回 `APP_BUILDING`，不插队 |
| `error` 且会话仍可用、或仍有可启动产物 | 允许更新；成功后 `idle` |
| 会话缺失、已终止或已归档 | 返回 `SESSION_UNAVAILABLE`；有旧产物时仍可 `start-app`，修订需重新 `create-app` |

更新失败且此前已有可启动产物时，恢复 `idle` 并保留旧产物，通过通知说明更新失败。无产物时进入 `error(stage=building)`。完成通知与 `get-app-status` 的字段约定与初次构建相同；通知文案可区分「创建完成」与「更新完成」。完整绑定与恢复规则见 [multi-app-sessions.md](multi-app-sessions.md)。

建议工具 description 包含："修订已有应用。app_id 必填。prompt 为整理后的修改说明。应用 building 时不要调用；running 时先 stop-app。新应用用 create-app；答题计分等用 send-request。"

## 4. `get-app-status`

输入：

```json
{
  "app_id": "app_01J..."
}
```

不同状态返回不同字段：

| state | 含义 | 字段 |
| --- | --- | --- |
| `building` | AgentAPI 正在构建，应用还不可启动 | `app_id`, `state`, `elapsed_seconds`, `message` |
| `idle` | 构建完成并注册，进程未启动 | `app_id`, `state`, `name`, `build_duration_seconds` |
| `running` | 应用健康检查通过，媒体流地址可用 | `app_id`, `state`, `name`, `stream_url`, `started_at`, `uptime_seconds` |
| `error` | 构建、启动或运行失败 | `app_id`, `state`, `error` |

`building`：

```json
{
  "app_id": "app_01J...",
  "state": "building",
  "elapsed_seconds": 42,
  "message": "正在写页面"
}
```

`idle`：

```json
{
  "app_id": "app_01J...",
  "state": "idle",
  "name": "掷骰子游戏",
  "build_duration_seconds": 95
}
```

`running`：

```json
{
  "app_id": "app_01J...",
  "state": "running",
  "name": "掷骰子游戏",
  "stream_url": "https://render.example.com/streams/inst_01J/live.ts",
  "started_at": "2026-10-24T08:30:00Z",
  "uptime_seconds": 12
}
```

`error`：

```json
{
  "app_id": "app_01J...",
  "state": "error",
  "error": {
    "stage": "building",
    "code": "BUILD_FAILED",
    "message": "构建完成，但产物校验失败",
    "last_agent_message": "代码已经写完，但启动检查报错"
  }
}
```

`building` 的 `message` 取 AgentAPI Session Event 中最近的进度事件，按到达顺序取最后一条：`agent.message`、`agent.thinking` 或 `session.error`。agent 长时间调用工具、没有正式消息输出时，thinking 也会更新进度，`message` 因此不会长时间停留在旧文案；thinking 内容不保证存在（可能为空或被脱敏），此时沿用上一条进度。`idle` 完成后由控制面发送“APP 创建完成”通知，通知里的 `message` 才是给灵矽播报的文案。`name` 是应用的展示名称，由控制面在注册应用时确定。

`error.stage` 取 `building`、`starting` 或 `running`；`error.last_agent_message` 保留最近一次 agent 消息。构建失败时进入 `error`，不会进入 `idle`。

## 5. 屏幕（已移除）

控制面不再提供 `get-screens`。屏幕发现、投放和停止都由现场设备完成：设备用 SSDP 找到局域网里的电视，再用 `self.cast.*` 工具把 `stream_url` 交给电视播放。控制面既看不到电视，也不维护屏幕状态，因此不再返回屏幕列表或兼容视图。灵矽需要屏幕信息时，以设备端工具的结果为准。

## 6. `start-app`

输入：

```json
{
  "app_id": "app_01J..."
}
```

`start-app` 只负责让应用的云端运行时跑起来：控制面使用当前 owner 绑定的 Render（平台默认或用户自有服务）启动实例，等待健康检查和媒体流就绪，然后返回 `running` 和 `stream_url`。

```json
{
  "app_id": "app_01J...",
  "state": "running",
  "name": "掷骰子游戏",
  "stream_url": "https://render.example.com/streams/inst_01J/live.ts",
  "started_at": "2026-10-24T08:30:00Z",
  "uptime_seconds": 0
}
```

它不建立投屏会话，不接受 `screen_id`，也没有 `force`。`running` 的应用可以不绑定屏幕；`stream_url`（mpegts，H.264/AAC）是设备端投屏的输入，由设备自己的投屏工具投到现场发现的电视上。拿不到 `stream_url` 时不算启动成功：Render 已就绪但流地址尚未生成，返回 `STREAM_START_FAILED`，应用进入 `error`。

应用端口、Render 的 `instance_id` 只在控制面内部使用，不通过 MCP 返回给灵矽；应用访问由控制面按 `app_id` 路由，灵矽不需要知道端口。`stream_url` 是唯一需要离开控制面的地址。

可以从 `idle` 启动，也可以从“构建成功但启动失败”的 `error` 重试；`start-app` 会先清理上次失败遗留的资源，成功后回到 `running`。`ARTIFACT_INVALID` 是例外：产物结构不合法时重试不会成功，先 `update-app` 或重新 `create-app`。已经在 `running` 时幂等返回当前状态和同一个 `stream_url`，不会重复创建实例。`building` 时返回 `APP_BUILDING`；构建失败、没有可启动产物的 `error` 返回 `APP_IN_ERROR`；应用不属于当前 owner 时返回 `APP_NOT_FOUND`。

启动流程有时限，由控制面配置超时。连接过程中请求等待最终结果，不返回 `running + connecting`；超时或失败时进入 `error`，工具返回 `isError: true`。同一应用的并发启动请求不得重复创建进程。输入校验不通过时拒绝操作，不改变已有应用状态。

失败时进入 `error`，具体错误码见第 9 章。控制面只跟踪应用进程和媒体流；运行中一旦确认进程退出或流不可用，状态进入 `error`。屏幕上的画面不在控制面视野内：`stop-app` 释放云端运行时；新路径不会写投屏会话，现场屏幕上的画面需要用设备端的投屏工具另外停止。若旧记录仍有 `ProjectionSessionID`，`stop-app` 会先做兼容清理。

启动请求超时或响应丢失时，结果未知。控制面会在本次工具调用内连续核对最多三次：查到本应用资源就清理，确认没有资源就结束；三次都无法完成核对时返回 `CLEANUP_PENDING`，稍后重试即可，不要重新 `create-app`。控制面仍会在后台继续核对，恢复可达且确认没有资源后立即结束，不再按固定时间等待；应用保持在可重试的 `error`。Render 不可达时保留待清理标记，直到能完成核对。

启动成功后，后续应用操作仍通过既有的 OpenAPI / HTTP 契约完成。

### 6.1 调用应用能力

应用构建完成后，灵矽先调用 `get-app-operations(app_id)` 获取 `operation_id`、说明、路径/查询参数和 JSON 请求体 schema。该工具从应用的 `openapi.yaml` 读取信息，`building` 时返回 `APP_BUILDING`，`error` 时返回 `APP_IN_ERROR`。

```json
{
  "app_id": "app_01J...",
  "state": "idle",
  "operations": [
    {
      "operation_id": "rollDice",
      "method": "POST",
      "path": "/api/roll",
      "description": "掷两个骰子并记录结果",
      "request_body": {
        "required": false,
        "schema": {"type": "object", "properties": {"count": {"type": "integer"}}}
      }
    }
  ]
}
```

随后用 `send-request` 调用。路径参数放在 `parameters` 中，名称与路径模板一致；其余标量参数作为查询参数；JSON 请求体放在 `body`。返回应用原始 HTTP `status` 和解析后的 `body`。只有 `running` 状态可调用，否则返回 `APP_NOT_RUNNING`；`operation_id` 不在应用 OpenAPI 中时返回 `OPERATION_NOT_FOUND`。

工具列表保持稳定，不随应用动态注册；`get-app-operations` 只返回当前应用的操作，避免灵矽侧工具列表热更新问题。

## 7. `stop-app`

输入：

```json
{
  "app_id": "app_01J..."
}
```

停止应用的云端运行时并释放资源后回到 `idle`。控制面只停止 Render 实例；现场屏幕上的画面由设备端的投屏工具负责停止，控制面看不到也不去动它。`idle` 响应不再携带 `stream_url`：

```json
{
  "app_id": "app_01J...",
  "state": "idle",
  "stopped_at": "2026-10-24T08:45:00Z"
}
```

已经在 `idle` 时幂等返回；`building` 时返回 `APP_BUILDING`；应用处于构建成功但启动失败的 `error` 时，会先清理上次启动遗留的资源并回到 `idle`；其他 `error` 幂等返回当前错误。清理未完成返回 `CLEANUP_PENDING`。停止失败返回 `STOP_FAILED`。用户主动停止后回到 `idle`；应用进程异常退出则进入 `error`（`stage=running`，`code=APP_EXITED`），由后续 `get-app-status` 反映。

## 8. 创建到启动

```mermaid
sequenceDiagram
  autonumber
  participant U as 用户
  participant L as 灵矽云
  participant C as 控制面
  participant A as AgentAPI
  participant R as Render
  participant D as 设备端 MCP
  participant T as 电视

  U->>L: 帮我做一个掷骰子游戏
  L->>C: create-app(prompt, player_names 可选)
  C->>A: 创建编码 session
  A-->>C: session_id
  C-->>L: app_id, state=building
  L-->>U: TTS：开始制作

  A-->>C: session 结束，artifact 已持久化
  C->>A: 下载 artifact
  C->>C: 校验、注册 app
  C-->>L: notifications/app_status(app_id, state=idle)
  L-->>U: TTS：APP 创建完成，要开始吗？

  U->>L: 放到电视上吧
  L->>C: start-app(app_id)
  C->>R: 启动实例、准备页面和媒体流
  R-->>C: health OK / stream_url
  C-->>L: state=running, stream_url

  L->>D: self.cast.list_screens()
  D-->>L: 电视名与状态（工具返回文本）
  L->>D: self.cast.cast(stream_url, target_id)
  D->>T: SOAP SetAVTransportURI + Play（电视拉流）
  D-->>L: 已投到电视
  L-->>U: TTS：游戏已投到电视
```

控制面完全不知道画面投到了哪台电视；`start-app` 返回 `stream_url` 之后，投屏由设备端的工具完成，详见[固件投屏工具](../../operations/yunliao-s3-max-firmware-mcp.md)。控制面不提供屏幕查询，现场屏幕以设备端 `self.cast.list_screens` 为准。

构建完成后，控制面通过已有的 MCP SSE 连接发送通知：

```json
{
  "jsonrpc": "2.0",
  "method": "notifications/app_status",
  "params": {
    "app_id": "app_01J...",
    "state": "idle",
    "message": "APP 创建完成"
  }
}
```

构建失败时发送同一方法，`state` 为 `error`，`error` 结构与 `get-app-status` 一致。

灵矽收到通知后决定是否播报并等待用户指令；控制面不直接推 TTS。通知是 best-effort，断线或丢失时以 `get-app-status` 为准。

## 9. 错误模型

应用进入 `error` 时，`get-app-status` 返回的错误结构与 `notifications/app_status` 的 `params` 一致：

```json
{
  "app_id": "app_01J...",
  "state": "error",
  "error": {
    "stage": "starting",
    "code": "START_FAILED"
  }
}
```

`error.stage` 与 `error.code`：

| 场景 | `error.stage` | `error.code` 示例 |
| --- | --- | --- |
| 构建或产物校验失败 | `building` | `BUILD_FAILED` |
| 启动时 Render 判定产物不合法（HTTP 422、`INVALID_ARTIFACT`） | `starting` | `ARTIFACT_INVALID`（不可重试） |
| 应用启动或健康检查失败 | `starting` | `START_FAILED` |
| 应用已就绪但媒体流未生成 | `starting` | `STREAM_START_FAILED` |
| 应用运行后进程异常退出或流不可用 | `running` | `APP_EXITED` |

`ARTIFACT_INVALID` 表示产物结构或启动契约不合法（例如入口不是 `app/app.js`、缺必填文件、`launch.json` 非法），由 Render 的 `422 INVALID_ARTIFACT` 映射而来；产物下载地址缺失也归入这一类。重试 `start-app` 不会成功：应先 `update-app` 让构建端重新交付，或重新 `create-app`。`START_FAILED` 只表示应用进程启动或健康检查失败，不再承载产物结构错误。

`error.message` 可选，用于补充失败原因；`error.last_agent_message` 保留最近一次 agent 消息。

以下错误由工具直接返回，不改变应用状态：

| `code` | 工具 | 场景 |
| --- | --- | --- |
| `INVALID_PLAYER_NAMES` | `create-app` | 名称为空白、类型错误或去除首尾空白后重名 |
| `BUILD_START_FAILED` | `create-app` | 无法启动 AgentAPI 构建 |
| `UPDATE_START_FAILED` | `update-app` | 绑定会话存在但提交修订消息失败 |
| `SESSION_UNAVAILABLE` | `update-app` | 绑定会话缺失、已终止、已归档或无法接收消息 |
| `APP_RUNNING` | `update-app` | 应用仍在运行，需先 `stop-app` |
| `MISSING_CONTEXT` | `list-apps` 等 | 缺少设备标识 |
| `APP_BUILDING` | `start-app`, `stop-app`, `get-app-operations`, `update-app` | 应用仍在构建或修订中 |
| `APP_IN_ERROR` | `start-app`, `get-app-operations`, `update-app` | 应用处于不可启动或不可修订的 `error` |
| `ARTIFACT_MISSING` | `start-app` | 应用包已丢失，需要重新创建 |
| `ARTIFACT_INVALID` | `start-app` | Render 判定产物结构或启动契约不合法（422）；重试无效，需要 `update-app` 或 `create-app` |
| `START_FAILED` | `start-app` | 应用进程启动或健康检查失败 |
| `STREAM_START_FAILED` | `start-app` | 应用媒体流未就绪 |
| `CLEANUP_PENDING` | `start-app`, `stop-app` | 上次启动结果还在核对或资源正在清理，稍后重试 |
| `STOP_FAILED` | `stop-app` | 停止进程失败 |
| `APP_NOT_RUNNING` | `send-request` | 应用未在运行 |
| `RUNTIME_UNAVAILABLE` | `start-app`, `send-request` | 无法确认应用进程状态 |
| `OPERATION_NOT_FOUND` | `send-request` | 应用 OpenAPI 中没有该 operationId |
| `APP_NOT_FOUND` | 所有接收 `app_id` 的工具 | 应用不存在，或不属于当前设备和智能体 |
| `PERSISTENCE_FAILED` | 写操作 | 无法保存控制面状态，后续操作已停止 |
| `INVALID_ARGUMENT` | 所有工具 | 参数为空、类型错误或无法序列化 |

`CLEANUP_PENDING` 是临时状态：屏幕和资源还在核对或清理，稍后用同一 `app_id` 重试即可，不要重新 `create-app`。`APP_NOT_FOUND` 同时覆盖“不存在”和“不属于当前设备和智能体”，控制面不区分两者，避免泄露其他设备的应用是否存在。

Portal 模式下，身份校验失败发生在进入工具层之前，不属于工具错误（`/mcp` 每个请求都校验，`/sse` 建连时校验，`/mcp/message` 每次 POST 校验）：

| HTTP | `code` | 场景 |
| --- | --- | --- |
| `400` | `MISSING_IDENTITY` | `/mcp` 或 `/sse` 缺少任一 `X-Linx-*` 头 |
| `401` | — | 配置了 MCP Token（Portal 模式始终有）但 Bearer token 缺失或错误 |
| `403` | `IDENTITY_MISMATCH` | `/mcp/message` 的头缺失或与 SSE 会话不一致 |
| `403` | `OWNER_NOT_FOUND` | 设备不存在，或 `(MAC, AgentID)` 无法匹配 |
| `403` | `IDENTITY_NOT_REGISTERED` | 设备存在但尚未完成开通（`state != registered`） |
| `403` | `USER_DISABLED` | 设备所属账号已被停用 |
| `503` | `IDENTITY_CHECK_FAILED` | 身份校验器自身失败（例如数据库不可用），请求未进入工具层 |

身份通过后，工具层仍按 owner 校验应用；跨 owner 的应用返回 `APP_NOT_FOUND`。
