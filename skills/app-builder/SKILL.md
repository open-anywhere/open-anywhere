---
name: app-builder
description: 把用户需求构建为应用包：自包含 Node.js Web 应用、OpenAPI 3.1 业务契约和 launch.json，最终产出 /mnt/session/outputs/artifact.tar.gz。包含示例模板、契约、模板复制脚本和自测脚本。
---

# 应用构建

本技能把一条中文应用需求变成一个自包含、可直接运行的独立 Web 应用。完整契约见 `references/artifact-contract.md` 和 `references/api-contract.md`；先用脚本复制模板，再按需求修改，不要从零搭建。

## 交付物

在 `/mnt/session/outputs/` 下生成：

```text
app/
├── app.js
├── package.json
└── public/
    └── index.html
openapi.yaml
launch.json
artifact.tar.gz          # 以上三项的归档，位于 outputs 根目录
```

`artifact.tar.gz` 的根目录必须直接包含 `app/`、`openapi.yaml`、`launch.json`。

模板实现了一个「猜电影」示例：玩家通过 `user_name` 加入，根据线索提交电影名，答对得分并进入下一轮。它只用于展示目录结构、页面轮询和状态接口的写法；实际构建时按需求替换电影玩法、页面和接口，但保留 `/health`、`/api/*`、`message` 响应和玩家身份约定。

## 工作流程

1. **理解需求与契约**：先读 `references/artifact-contract.md` 和 `references/api-contract.md`，再确定业务状态、页面交互和 `/api/*` 操作；为每个操作起稳定、语义化的 `operationId`。需要多人游戏时，先处理构建消息中的初始玩家名单。
2. **复制模板**：运行
   ```bash
   bash <skill-dir>/scripts/init-template.sh /mnt/session/outputs
   ```
   一条命令完成复制。不要用 Read/Write 工具逐个搬运模板文件。
3. **实现后端**：修改 `app/app.js`。只使用 Node.js 内置模块，或在构建期把依赖打包进 `app/`；运行时不执行 `npm install` 或构建命令。
4. **实现页面**：修改 `app/public/index.html`。页面通过 `fetch` 调用 `/api/*`，并轮询或使用 SSE 自动刷新。
5. **更新契约**：修改 `openapi.yaml`，让它与实际 `/api/*` 操作一一对应；业务成功和错误响应都必须包含字符串 `message`。
6. **自测与打包**：运行
   ```bash
   bash <skill-dir>/scripts/self-test.sh /mnt/session/outputs \
     --op-method POST --op-path /api/join --op-body '{"user_name":"验证玩家"}' \
     --bad-op-method POST --bad-op-path /api/join --bad-op-body '{"user_name":"  "}' \
     --package
   ```
   脚本一次完成静态检查、测试端口启动、`/health`、`/`、业务操作、错误分支抽查、打包和归档校验。`--op-*` 必须调用本次构建的一个能改变状态的业务操作（路径以 `/api/` 开头）；`--bad-op-*` 抽查一个错误场景（4xx/5xx 是预期），两者响应都必须是 JSON 且包含字符串 `message`。失败时按输出修复并重跑，直到看到 `self-test passed`。不要做浏览器截图、UI 自动化或穷举错误分支。

缺少细节时按合理假设完成，并在最终说明中列出关键假设。

## 启动契约

- `launch.json` 只允许 `name`、`runtimeExecutable`、`runtimeArgs`、`healthPath`、`readyTimeoutMs` 五个字段，不能出现 `port` 或其他字段。
- `runtimeExecutable` 固定为 `"node"`；`runtimeArgs` 必须引用 `app/` 内的入口文件，例如 `["app/app.js"]`。
- 应用必须读取环境变量 `PORT`；缺失或非法时向 stderr 说明原因并以非零状态退出。`HOST` 未设置时默认 `0.0.0.0`。不要写死端口或在参数中传 `--port`。
- 必须实际实现 `GET /health`，就绪时返回 HTTP 200、`Content-Type: application/json` 和 `{"status":"ok"}`。
- 收到 `SIGTERM` 后停止接受新请求，关闭 HTTP/SSE 连接和定时器并退出。

## 玩家与身份

- 用 `user_name` 同时表示玩家身份和展示名称，不生成 `user_id`，也不维护名字到 ID 的映射。
- 构建消息包含初始玩家名单时，按原顺序写入应用（模板中的 `INITIAL_PLAYERS`），每个名字作为 `user_name`；不要增删、改名或编造。
- 名单为空或未提供时，提供运行时加入能力，不要编造参与者。
- 所有加入和业务请求都去除名字首尾空白；空白名字、重复名字要拒绝。按名字精确匹配；本局内不改名。
- 初始玩家加入后只具有初始状态；不要伪造进度，例如把“已加入”当成“已准备”或“已回答”。
- 未登记的名字不能直接参与游戏或计分；需要时先调用加入操作。

## 业务响应

- 业务接口统一返回 JSON 对象，并包含必需的字符串 `message`；错误响应也用 JSON 和 `message` 表达原因。
- `message` 是给语音入口的播报候选，使用简短、面向用户的中文，例如“已收到回答”“小明已准备”。
- `/health`、`/openapi.json` 和 SSE 事件不受 `message` 约束。
- `/api/*` 之外不要声明业务操作；`/`、`/health`、`/openapi.json` 不写入 OpenAPI 的 `paths`。

## 运行期限制

- 内存态即可，不依赖外部数据库、外部网络服务或 CDN；不读取构建沙箱中的文件。
- 不生成 watch/HMR、交互式命令或启动时安装/构建步骤。
- 应用只提供页面和业务 API；运行环境和展示由外部负责。
- 日志写 stdout/stderr；不要自行后台化。
- 优先使用内置模块，避免无谓依赖。

## 最小验证

没有实际运行的检查不得声称通过，但也只做必要检查：自测只跑一次 `scripts/self-test.sh`，它已覆盖静态检查（`validate-artifact.sh`）、一次启动、`/health`、`/`、一个改变状态的业务操作、一个错误分支抽查，以及 `--package` 时的打包和归档校验。

不要截图、跑浏览器或 UI 自动化，不要逐项测试所有错误分支，也不要反复换端口重启。细节见 `references/self-test.md`。

## 参考文件

- `references/artifact-contract.md`：目录结构、launch.json、进程与运行约定。
- `references/api-contract.md`：OpenAPI 3.1、业务响应、参数和调用方式。
- `references/self-test.md`：init-template.sh 与 self-test.sh 的用法说明。
- `references/app-template/`：最小可运行模板，由 init-template.sh 复制。
- `scripts/init-template.sh`：一条命令复制模板到产物目录。
- `scripts/self-test.sh`：静态检查、启动验证、业务操作抽查、打包与归档校验。
- `scripts/validate-artifact.sh`：归档结构和启动契约的静态检查（self-test 内部调用）。
