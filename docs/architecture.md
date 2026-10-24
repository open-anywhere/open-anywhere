# Open Anywhere 架构设计

> 状态：Proposal（供 PR 评审）
> 版本：v0.1
> 日期：2026-10-23

## 1. 摘要

Open Anywhere 让用户通过语音描述需求，由云端编码 Agent 生成一个可运行的 Web 应用，并在身边的屏幕上通过 AirPlay 呈现和控制。

系统由四个组件构成：

| 组件 | 角色 |
| --- | --- |
| 灵矽云（Linx） | 语音入口：ASR、TTS、意图识别、MCP Client |
| 控制面（Control Plane） | MCP Server、任务编排、应用注册表、OpenAPI 到 HTTP 的适配 |
| AgentAPI | 异步应用工厂：编码 Agent + 沙箱，产出应用 artifact |
| Render | 应用运行时与舞台：启动应用、转发 HTTP、AirPlay 投屏 |

系统只有两条契约：

1. **MCP**：灵矽云与控制面之间。灵矽只看见 MCP Tool，不直接接触 AgentAPI 或 Render。
2. **OpenAPI + launch.json + HTTP**：控制面、Render 与应用之间。应用只暴露 HTTP，不感知 MCP。

核心原则：**MCP 面稳定，应用能力动态；构建慢而异步，控制快而同步；投屏是会话级通道，应用是通道里的内容。**

## 2. 目标与非目标

### 2.1 目标

- 用户用自然语言创建应用，不需要知道代码、构建或部署。
- 灵矽只依赖一组稳定的 MCP Tool；新应用不需要在灵矽侧重新注册工具。
- 构建是长任务，可以异步执行、按需查询状态和已用时间，不阻塞语音对话。
- 应用生成后可以在 Render 上稳定启动，并通过 OpenAPI 被控制面调用。
- 应用画面通过 AirPlay 常驻在屏幕上；切换应用不重新建立投屏。
- Agent 产出的应用必须经过模板和检查脚本的硬约束，保证“Render 能跑、控制面能调”。

### 2.2 非目标

- 多租户、计费、应用市场、任意第三方应用托管。
- 为每个 OpenAPI 操作动态注册一个 MCP Tool；MVP 使用通用的 `send_request`。
- 服务端主动 TTS 推送；MVP 由灵矽在用户询问时轮询任务状态。
- 应用状态跨重启持久化；MVP 允许内存态，重启后状态丢失。
- 在 macOS 上程序化发起 AirPlay 系统镜像；MVP 手动连接，浏览器或 HDMI 兜底。
- 把 AgentAPI 的沙箱当作应用运行时；AgentAPI 只负责构建。

## 3. 术语

| 术语 | 含义 |
| --- | --- |
| 灵矽（Linx） | 七牛云的 AI 智能硬件平台，负责语音入口、ASR、TTS 和意图识别 |
| 控制面（Control Plane） | 对灵矽暴露 MCP、对 AgentAPI 编排构建、对 Render 转发请求的服务 |
| AgentAPI | 托管智能体框架，运行编码 Agent 和沙箱，产出持久化文件 |
| Render | 现场的应用运行时，负责启动应用、提供 HTTP、维持 AirPlay |
| Task | 一次 `create_app` 构建任务，对外用 `task_id` 标识 |
| App | AgentAPI 产出的、已经在 Render 上运行的应用 |
| Artifact | 应用产物目录，包含 `app/`、OpenAPI 文档、`launch.json` 和自检报告 |
| Stage | Render 上常驻的展示页面；AirPlay 镜像的就是它 |
| OpenAPI | 应用能力的 HTTP 契约，控制面据此校验和转发请求 |
| launch.json | 应用启动契约，描述可执行文件、参数和端口 |

## 4. 系统上下文

```mermaid
flowchart LR
  U["用户"] -->|语音| L["灵矽云<br/>ASR / TTS / 意图"]
  L -->|MCP Tool| C["控制面<br/>MCP Server / 编排 / 注册表"]
  C -->|创建 / 查询 session| A["AgentAPI<br/>编码 Agent + 沙箱"]
  A -->|artifact| C
  C -->|部署 / HTTP 转发| R["Render<br/>运行时 / Stage / AirPlay"]
  R -->|HTTP 响应| C
  C -->|MCP Tool 结果| L
  R -->|AirPlay| T["电视 / 大屏"]
```

### 4.1 组件职责

| 组件 | 负责 | 不负责 |
| --- | --- | --- |
| 灵矽云 | ASR、TTS、意图识别、选择并调用 MCP Tool | 构建应用、运行应用、投屏 |
| 控制面 | MCP Server、工具路由、任务编排、应用注册表、OpenAPI 校验与 HTTP 转发、artifact 下载与二次校验 | 写代码、运行应用、理解应用的业务语义 |
| AgentAPI | 运行编码 Agent、在沙箱中写代码和自测、持久化 artifact | 承载应用运行时、直接面对灵矽 |
| Render | 解压 artifact、按 `launch.json` 启动、健康检查、进程保活、接收控制面 HTTP 转发、Stage 与 AirPlay | 理解应用业务、决定应用能力 |
| 应用 | 提供 UI 和 `/api/*` HTTP 接口、维护自己的状态 | 感知 MCP、直接面对灵矽 |

## 5. 投屏服务开放 API

投屏服务是一个独立的局域网投屏端，对外开放 HTTP API。Client 可以使用它把自己的实时媒体流投放到局域网中的电视或其他显示设备。

投屏服务负责探测局域网设备、维护可用的投屏目标，并通过目标支持的协议建立和停止投屏会话。Client 负责准备实时媒体流，并提供目标设备可以访问的流地址。

### 5.1 Client 调用流程

```text
Client → 查询投屏服务有哪些 Target
Client → 选择 Target 上的 Endpoint
Client → 创建投屏 Session，并提供自己的实时流 URL
投屏服务 → 调用 DLNA / AirPlay 等协议开始播放
Client → 查询 Session 状态
Client → 停止 Session
```

第一版只支持 Client 提供实时 MPEG-TS 流。投屏服务不负责生成 Client 的媒体内容，只负责协议控制和会话生命周期。

### 5.2 Target 和 Endpoint

`Target` 表示一个物理投屏设备；`Endpoint` 表示该设备上的一种投屏入口。同一台电视可以同时拥有 DLNA 和 AirPlay 两个 Endpoint。

```json
{
  "id": "tv-9c1221d09d00",
  "name": "客厅电视",
  "state": "online",
  "endpoints": [
    {
      "id": "dlna-renderer",
      "protocol": "dlna",
      "capabilities": {
        "formats": ["mpegts"],
        "video_codecs": ["h264"],
        "audio_codecs": ["aac"],
        "streaming": true
      }
    },
    {
      "id": "airplay-mirror",
      "protocol": "airplay",
      "capabilities": {
        "formats": ["airplay-mirror"],
        "video_codecs": ["h264"],
        "audio_codecs": ["aac"],
        "streaming": true
      }
    }
  ]
}
```

Client 根据 Endpoint 的 `capabilities` 选择协议、封装格式和编码方式。

### 5.3 开放 API

| 方法 | 路径 | 作用 |
| --- | --- | --- |
| `GET` | `/v1/targets` | 获取已发现的投屏目标 |
| `GET` | `/v1/targets/{target_id}` | 获取目标详情和能力 |
| `POST` | `/v1/projection-sessions` | 创建并开始投屏 |
| `GET` | `/v1/projection-sessions/{session_id}` | 查询投屏状态 |
| `DELETE` | `/v1/projection-sessions/{session_id}` | 停止投屏 |
| `GET` | `/healthz` | 服务健康检查 |

创建投屏会话：

```http
POST /v1/projection-sessions
Content-Type: application/json
```

```json
{
  "target_id": "tv-9c1221d09d00",
  "endpoint_id": "dlna-renderer",
  "source": {
    "type": "stream",
    "url": "http://192.168.31.203:8765/live.ts",
    "format": "mpegts",
    "video_codec": "h264",
    "audio_codec": "aac"
  }
}
```

服务端校验目标能力后，使用 DLNA 的 `SetAVTransportURI` 和 `Play` 操作，让电视开始拉取 `source.url`。返回投屏会话：

```json
{
  "session_id": "projection-session-001",
  "state": "playing",
  "target_id": "tv-9c1221d09d00",
  "endpoint_id": "dlna-renderer"
}
```

`source.url` 必须是目标设备可以访问的地址。Client 负责启动流服务并持续输出符合声明的 MPEG-TS、H.264 和 AAC 数据。

查询会话：

```http
GET /v1/projection-sessions/{session_id}
```

停止会话：

```http
DELETE /v1/projection-sessions/{session_id}
```

服务端停止目标设备的播放并释放投屏会话；该操作不负责停止 Client 自己的流服务。

## 6. 应用管理 MCP

本章定义灵矽通过控制面管理应用生命周期的四个 MCP Tool。工具名按灵矽侧约定使用 kebab-case；应用运行后的能力调用仍走既有的 OpenAPI / HTTP 契约，不在本章展开。

### 6.1 工具集

| Tool | 作用 | 输入 | 输出 |
| --- | --- | --- | --- |
| `create-app` | 创建应用并启动 AgentAPI 构建 | `prompt` | `app_id`, `state: building` |
| `get-app-status` | 查询应用状态 | `app_id` | 随状态变化，见 6.3 |
| `start-app` | 启动已构建的应用 | `app_id` | `app_id`, `state: running` |
| `stop-app` | 停止运行中的应用 | `app_id` | `app_id`, `state: idle` |

应用状态有四种：`building`、`idle`、`running`、`error`。`error` 表示构建、启动或运行失败；`error.stage` 区分失败阶段，构建失败不会产生可启动的 `idle` 应用。

`app_id` 在 `create-app` 时分配，后续三个工具都用它定位应用；AgentAPI 的 session id 只在控制面内部使用。`app_id` 必须属于当前 device/session，控制面拒绝跨会话访问。

### 6.2 `create-app`

输入是灵矽整理后的用户诉求，不是原始 ASR 文本：

```json
{
  "prompt": "做一个掷骰子游戏，点击按钮掷出两个骰子，显示点数和最近几次结果"
}
```

控制面创建 AgentAPI session 后立即返回，不等待构建完成：

```json
{
  "app_id": "app_01J...",
  "state": "building"
}
```

同一 device/session 已有 `building` 中的应用时，重复 `create-app` 返回现有 `app_id`，不重复创建 session。

### 6.3 `get-app-status`

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
| `running` | 进程已启动并通过健康检查 | `app_id`, `state`, `name`, `started_at`, `uptime_seconds` |
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

`building` 的 `message` 取 AgentAPI Session Event 中最近一次 agent 消息；`idle` 完成后由控制面发送“APP 创建完成”通知，通知里的 `message` 才是给灵矽播报的文案。`name` 来自 artifact 的 `launch.json`。

`error.stage` 取 `building`、`starting` 或 `running`；`error.last_agent_message` 保留最近一次 agent 消息。构建失败时进入 `error`，不会进入 `idle`。

### 6.4 `start-app`

输入：

```json
{
  "app_id": "app_01J..."
}
```

控制面让 Render 按 artifact 中已有的 `launch.json` 启动应用；`launch.json` 协议不变。健康检查通过后返回 `running` 状态：

```json
{
  "app_id": "app_01J...",
  "state": "running",
  "name": "掷骰子游戏",
  "started_at": "2026-10-24T08:30:00Z",
  "uptime_seconds": 0
}
```

`launch.json` 的 `port` 只在控制面和 Render 内部使用，不通过 MCP 返回给灵矽；应用访问由控制面按 `app_id` 路由，灵矽不需要知道端口。

只允许从 `idle` 启动。已经在 `running` 时幂等返回当前状态；`building` 时返回 `APP_BUILDING`；`error` 时返回 `APP_IN_ERROR`；启动失败时应用进入 `error`（`stage=starting`），工具返回 `isError: true`，`error.code=START_FAILED`。

启动成功后，后续应用操作仍通过既有的 OpenAPI / HTTP 契约完成。

### 6.5 `stop-app`

输入：

```json
{
  "app_id": "app_01J..."
}
```

停止应用进程并回到 `idle`：

```json
{
  "app_id": "app_01J...",
  "state": "idle",
  "stopped_at": "2026-10-24T08:45:00Z"
}
```

已经在 `idle` 时幂等返回；`building` 时返回 `APP_BUILDING`；`error` 时幂等返回当前 `error`；停止失败返回 `STOP_FAILED`。用户主动停止后回到 `idle`；应用进程异常退出则进入 `error`（`stage=running`，`code=APP_EXITED`），由后续 `get-app-status` 反映。

### 6.6 创建到启动

```mermaid
sequenceDiagram
  autonumber
  participant U as 用户
  participant L as 灵矽云
  participant C as 控制面
  participant A as AgentAPI
  participant R as Render

  U->>L: 帮我做一个掷骰子游戏
  L->>C: create-app(prompt)
  C->>A: 创建编码 session
  A-->>C: session_id
  C-->>L: app_id, state=building
  L-->>U: TTS：开始制作

  A-->>C: session 结束，artifact 已持久化
  C->>A: 下载 artifact
  C->>C: 校验、注册 app
  C-->>L: notifications/app_status(app_id, state=idle)
  L-->>U: TTS：APP 创建完成，要开始吗？

  U->>L: 开始吧
  L->>C: start-app(app_id)
  C->>R: 按 launch.json 启动
  R-->>C: health OK
  C-->>L: state=running
  L-->>U: TTS：游戏开始了
```

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

### 6.7 状态转换

```mermaid
stateDiagram-v2
  [*] --> building: create-app
  building --> idle: 构建完成
  building --> error: 构建失败
  idle --> running: start-app
  idle --> error: 启动失败
  running --> idle: stop-app
  running --> error: 进程退出
  error --> [*]
```

`error` 是终止态；重试需要重新调用 `create-app`，生成新的 `app_id`。
