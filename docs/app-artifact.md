# 应用包设计与构建约定

> 状态：Proposal（供 PR 评审）
> 日期：2026-10-24

应用包是 AgentAPI 交付的可独立运行的 Web 应用。包内声明启动方法、页面和业务 API；端口在部署时注入，不固化到产物。本文定义产物格式、应用运行约定及构建 Agent 提示，不定义 Render 的 HTTP 接口。

**一期（MVP）仅支持 Node.js 运行时，`runtimeExecutable` 固定为 `node`。** 其他语言运行时和预编译可执行文件不在一期支持范围内。

## 1. 交付格式

构建结果打包为 `tar.gz`，发布到运行端可下载的 HTTP(S) URL。下载地址是交付信息，不写入 `launch.json`。解压后的 artifact 根目录如下：

```text
outputs/
  app/
    app.js            # Node.js 自包含入口
    package.json      # 应用元信息；依赖须预打包，运行端不 install
    public/
      index.html      # UI
      styles.css      # 可选
      ui.js           # 可选
    assets/           # 可选，随包提供的静态资源
  openapi.yaml        # OpenAPI 3.1，仅描述 /api/* 业务操作
  launch.json         # 启动契约
```

- `outputs/` 是构建端的产物目录名。归档其内容，解压后直接得到 `app/`、`launch.json` 等文件，不额外嵌套一层 `outputs/`。
- `app/` 必须自包含。前端 JSX、TypeScript、样式和依赖在构建端处理完成；运行端不执行 install 或 build，不依赖构建沙箱里的文件或全局安装的应用依赖。
- MVP 的 Node.js 程序使用内置模块，或把可打包的依赖预先打包到入口文件；运行端提供约定版本的 Node.js。
- `openapi.yaml` 和 `launch.json` 必填。

打包示例：

```sh
tar -czf artifact.tar.gz -C outputs .
```

## 2. 启动契约：launch.json

Node.js 示例：

```json
{
  "name": "quiz",
  "runtimeExecutable": "node",
  "runtimeArgs": ["app/app.js"],
  "healthPath": "/health",
  "readyTimeoutMs": 10000
}
```

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `name` | 是 | 应用名称，用于日志、注册表和展示 |
| `runtimeExecutable` | 是 | 一期固定为 `node`，其他值不予接受 |
| `runtimeArgs` | 是 | Node.js 启动参数数组，包含包内入口文件路径，例如 `["app/app.js"]`；不含部署端口 |
| `healthPath` | 否 | 默认 `/health`；MVP 固定使用 `/health`，若声明必须为该值 |
| `readyTimeoutMs` | 否 | 就绪超时，单位毫秒，默认 `10000`，须为正整数；运行端可设置上限 |

所有相对路径以 artifact 根目录为基准，进程工作目录也是该目录。`runtimeArgs` 按参数数组直接传递，不进行 shell 展开。

**启动契约没有 `port` 字段，也不在参数中固定 `--port`。** 运行环境注入 `PORT` 和 `HOST`，应用必须据此监听：

| 环境变量 | 约定 |
| --- | --- |
| `PORT` | 必填，部署时确定的端口；必须是 1–65535 的整数 |
| `HOST` | 部署时注入 `0.0.0.0`；未设置时应用默认使用 `0.0.0.0` |

应用不得用写死的端口覆盖注入值。缺少或非法 `PORT` 时，向 stderr 写明原因并以非零状态退出；端口被占用时启动失败，不自行切换端口。Node.js 应用读取上述环境变量，不根据 `runtimeExecutable` 猜测或拼接端口参数。

## 3. 应用运行约定

### 3.1 HTTP 页面和状态

同一应用进程、同一监听端口提供以下能力：

| 路径 | 要求 |
| --- | --- |
| `GET /` | 必需，返回浏览器可直接加载的 HTML UI |
| `GET /health` | 必需，应用就绪时返回 HTTP `200`、JSON `{"status":"ok"}` |
| `/api/*` | 必需，提供 OpenAPI 中声明的业务操作 |
| `GET /openapi.json` | 推荐，返回与根目录 `openapi.yaml` 等价的 JSON 格式规范 |

`/` 指 HTTP 根路径，不是归档根目录里的某个 HTML 文件。页面可以由程序内联返回，也可以从 `app/public/index.html` 提供；运行端只需用 Chromium 访问 `/`，不需要寻找 HTML 文件或理解前端框架。

UI 和 API 必须共享同一份业务状态。外部客户端发起 HTTP 操作后，已经打开的 UI 必须自动更新，MVP 可采用轮询或 SSE。应用应同时支持页面操作和外部 HTTP 调用，不依赖灵矽才能使用。

`/health` 是应用必须实现的 HTTP 路由，不是只在 `launch.json` 填一个路径。应用完成必要初始化、能够提供页面和业务接口后才返回健康成功；尚未就绪时返回非 `200`。健康检查不依赖游戏已经开始、参与者已经加入或屏幕已经连接。

### 3.2 OpenAPI 和业务响应

- 根目录 `openapi.yaml` 必须是合法的 OpenAPI 3.1 文档。每个业务操作提供唯一、稳定、语义化的 `operationId`，以及调用者能理解的 `description`、参数、请求体和响应 schema。
- `paths` 只描述 `/api/*` 业务操作；`/`、`/health` 和 `/openapi.json` 属于固定运行约定，不放入业务操作列表。SSE 等传输通道不作为业务 JSON 操作，可放在 `/events`。
- 使用相对服务地址，如 `servers: [{url: "/"}]`，避免把构建机器的主机和端口写进规范。调用方按实际部署地址解析。
- 业务 API 的成功和错误响应均为 JSON 对象，schema 中把 `message` 声明为必需字符串。`message` 表达操作结果，供控制面作为优先 TTS 候选；实际是否播报由语音入口决定。
- 健康检查、规范文档及 SSE 事件不受业务响应 `message` 约束。
- 应用的用户标识使用通用 `user_id`，不包含灵矽声纹字段或 MCP 协议。身份识别由调用方完成，应用只处理业务身份。

例如，知识竞赛的提交回答接口可以接收：

```json
{
  "user_id": "u1",
  "answer": "地球"
}
```

返回：

```json
{
  "message": "已收到回答",
  "answer_id": "answer-001",
  "user_id": "u1"
}
```

这里仅演示字段约定，具体业务路径、评分规则和状态模型由应用自己的 OpenAPI 定义。

### 3.3 进程与依赖

- 应用以前台进程持续运行，不自行后台化；日志写 stdout/stderr，由运行端采集。
- 收到 `SIGTERM` 时停止接受新请求，关闭 HTTP 服务、SSE 连接和定时器并退出。
- 禁止 watch/HMR、交互式启动命令、启动时构建或安装依赖。
- MVP 使用内存态，不依赖外部数据库或外部网络服务；静态资源随包交付，不从 CDN 拉取脚本、字体等启动必需资源。
- 应用只负责页面和业务服务。Chromium 渲染、音视频捕获、编码和投屏由包外的运行设施负责，不生成在应用包中。

## 4. 给构建 Agent 的提示

下面这段可以作为构建任务的公共约束，与具体应用需求一起交给编码 Agent：

```text
请在 outputs/ 中生成一个可直接运行的独立 Node.js Web 应用，并遵守以下交付契约：

1. 产物包括 app/、openapi.yaml、launch.json。app/ 以 app.js 为入口，可包含 package.json、public/ 和 assets/；前端及依赖须预先构建；运行时不执行 install/build，不访问外部数据库或网络服务。
2. 一期仅支持 Node.js。launch.json 的 runtimeExecutable 必须为 node，
   runtimeArgs 包含包内入口文件路径，例如 ["app/app.js"]，并填写应用 name。
   所有相对路径以 outputs/ 为工作目录。不要写入 port 字段或固定端口参数。
   应用必须读取环境变量 PORT；HOST 默认 0.0.0.0。
   PORT 缺失或非法时退出并说明原因，不擅自使用默认端口。
3. 必须实际实现 GET /health。只有页面和业务接口准备好后，才返回
   HTTP 200、Content-Type: application/json 和 {"status":"ok"}。
   不要只在 launch.json 中声明 healthPath 而遗漏路由。
4. GET / 返回可由 Chromium 直接加载的 UI；/api/* 提供业务操作。
   UI 与 API 使用同一份状态，外部 HTTP 操作后 UI 自动刷新，可用轮询或 SSE。
5. openapi.yaml 使用 OpenAPI 3.1，只描述 /api/* 业务操作。
   各操作具有唯一且稳定的 operationId、description、参数和响应 schema。
   业务成功/错误响应都是 JSON，包含必需的字符串 message。
   不把 /health 加入业务操作列表；它不需要 message 字段。
   不写死部署地址或端口。若提供 /openapi.json，其内容须与 YAML 等价。
6. 应用身份字段使用 user_id，不接入声纹、灵矽或 MCP。
   应用以前台进程运行，日志写 stdout/stderr，收到 SIGTERM 时退出并清理资源。
7. 请实际自测：在干净的解压目录中按 launch.json 启动，注入一个测试 PORT；
   验证 /health、/、一个改变状态的业务请求，以及打开的 UI 随之更新。
   停止后换一个可用 PORT 再次启动，确认产物未写死端口。
   校验 OpenAPI 并核对实现，修复自测发现的问题；未运行的检查不得宣称通过。
8. 打包 outputs/ 的内容为 tar.gz，使 launch.json 位于归档根目录。
```

## 5. 产物验收

接收方须独立检查产物。准入检查至少覆盖：

- 归档目录结构、入口文件和启动契约符合要求；一期仅接受 `runtimeExecutable: "node"` 的 Node.js 应用。
- `launch.json` 不携带端口，OpenAPI 合法且与实际业务操作一致。
- 无 install/build 步骤，仅注入 `HOST`、`PORT` 即可从 artifact 根目录启动。
- 在就绪时限内，`GET /health` 返回 HTTP `200` 和 `{"status":"ok"}`；缺失该路由则验收失败。
- `/` 能加载，业务 HTTP 请求能改变应用状态，已打开的页面能同步显示结果。
- 停止后换端口可重新启动；`SIGTERM` 后进程与连接能够退出。

这些是新产物的设计要求，不代表仓库现有示例已经全部满足，也不代表运行端的验收机制已经实现。
