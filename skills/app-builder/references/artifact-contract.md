# 产物契约

交付物只包含一个 `artifact.tar.gz`。归档根目录必须直接包含 `app/`、`openapi.yaml` 和 `launch.json`，解压后立即得到这些文件，不能多一层 `outputs/`。

## 目录结构

```text
app/
├── app.js                  # 必填，Node.js 入口
├── package.json            # 必填，合法 JSON
└── public/
    └── index.html          # 必填，GET / 返回该页面
openapi.yaml                # 必填，OpenAPI 3.1
launch.json                 # 必填，启动契约
```

- `app/` 必须自包含。前端资源、样式和依赖在构建期处理完毕；运行端不执行 `npm install` 或构建命令。
- 优先使用 Node.js 内置模块。确需第三方依赖时，在构建期安装并把运行所需文件放进 `app/`（例如 `app/node_modules/`），或用打包器合并进 `app/app.js`。不要依赖构建沙箱中的全局安装。
- 使用内存态。不要依赖外部数据库、外部网络服务或 CDN；启动必需的脚本、字体和图片随包交付。
- 不要生成 watch/HMR、交互式启动命令或后台守护逻辑。

## launch.json

```json
{
  "name": "guess-the-movie",
  "runtimeExecutable": "node",
  "runtimeArgs": ["app/app.js"],
  "healthPath": "/health",
  "readyTimeoutMs": 10000
}
```

| 字段 | 必填 | 约束 |
| --- | --- | --- |
| `name` | 是 | 非空字符串，用于展示 |
| `runtimeExecutable` | 是 | 固定为 `node` |
| `runtimeArgs` | 是 | 字符串数组，必须引用归档内 `app/` 下的入口文件；不能包含 `--port` |
| `healthPath` | 否 | 一期只能为 `/health`；省略时按 `/health` 处理 |
| `readyTimeoutMs` | 否 | 正整数，1–600000；默认 10000 |

产物校验会拒绝以下情况：

- `launch.json` 出现未知字段（包括 `port`）。
- `runtimeExecutable` 不是 `node`。
- `runtimeArgs` 为空，或没有引用实际存在的 `app/` 内文件。
- `healthPath` 不是 `/health`。
- `readyTimeoutMs` 非正整数或超出上限。
- 归档内出现绝对路径、`..` 路径段，或 `outputs/` 目录。
- 归档缺少 `launch.json`、`openapi.yaml`、`app/`，或 `app/package.json` 不是合法 JSON。
- 压缩包超过 100 MiB、单文件超过 64 MiB、解压后总计超过 512 MiB。

## 端口与进程

- 必须读取环境变量 `PORT`，监听该端口；`PORT` 缺失、非整数或不在 1–65535 时，向 stderr 说明原因并以非零状态退出。
- `HOST` 未设置时默认 `0.0.0.0`，不要写死。
- 不要写死端口，不要在 `runtimeArgs` 中传 `--port`，不要因端口被占用而自行切换。
- 应用以前台进程运行，日志写 stdout/stderr。
- 收到 `SIGTERM` 后停止接受新请求，关闭 HTTP/SSE 连接和定时器并退出；不要忽略信号。

## HTTP 约定

| 路径 | 要求 |
| --- | --- |
| `GET /` | 返回浏览器可直接加载的 HTML 页面 |
| `GET /health` | 就绪后返回 HTTP 200、`Content-Type: application/json` 和 `{"status":"ok"}` |
| `/api/*` | OpenAPI 中声明的业务操作 |
| `GET /openapi.json` | 可选；若提供，内容必须与 `openapi.yaml` 等价 |

`/health` 必须实际实现，不能只在 `launch.json` 里声明。应用完成初始化、页面和业务接口可用后再返回成功。

## 渲染画布

应用页面会被 Render 在固定画布上截图并投屏，构建消息会给出画布尺寸。按以下约定实现页面：

- 画布为 **1280×720，16:9，设备像素比 1**。页面必须按这个尺寸设计，不依赖浏览器窗口大小变化。
- `html, body` 必须占满画布并禁止页面滚动：

  ```css
  html, body {
    height: 100%;
    margin: 0;
    overflow: hidden;
  }
  ```

- 所有内容必须在一屏内完整显示。长列表、多玩家、长文本要在页面内分页、截断或轮播，不能出现页面级滚动条。
- 关键内容距画布边缘至少 5%（水平 64 px、垂直 36 px），避免电视过扫描。
- 按支持的最大数据量验证布局；内容动态增长时也要保持不溢出。
- 使用 `height: 100%` 铺满画布，不要用固定像素高度或 `transform: scale()` 规避溢出。溢出会被 Render 裁切并记录 `layout_overflow` 告警，但不会自动修复。
- 模板 `app-template/app/public/index.html` 已按画布实现，构建时先复制模板，再替换业务内容。

## 打包

在 `/mnt/session/outputs/` 中执行：

```bash
cd /mnt/session/outputs
rm -f artifact.tar.gz
tar -czf artifact.tar.gz app openapi.yaml launch.json
tar -tzf artifact.tar.gz
```

只把 `app/`、`openapi.yaml`、`launch.json` 放进归档；不要把 `artifact.tar.gz` 自身、测试脚本或 `outputs/` 目录打进去。实际构建由 `scripts/self-test.sh --package` 完成打包和静态检查，不要手工执行。
