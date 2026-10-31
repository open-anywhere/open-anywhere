# 启动 Gateway

Gateway 是控制面：对灵矽提供 MCP 工具，向 AgentAPI 提交构建任务，把应用操作转发给 Render，并把 `stream_url` 返回给灵矽。投屏由现场设备自己完成，控制面不建立投屏会话；语音入口、云端的编码 Agent 和 Render 之间发生的事情经过这里。

## 前置条件

- Go，版本以 `go.mod` 为准
- 已经启动 Render，见[启动 Render](render.md)。只有模拟硬件、桌面调试或控制面 `get-screens` 兼容视图需要 Casting，见[启动 Casting](casting.md)
- AgentAPI 的构建智能体与云端环境。`agent_id`、`environment_id` 在发布阶段创建一次，创建方法见[控制面与 AgentAPI 接口设计](../architecture/interfaces/gateway-agentapi.md)
- 如果启用用户门户，还需要 Node.js、pnpm，以及灵矽云可访问的公网地址

## 准备配置

在仓库根目录复制配置示例：

```bash
cp gateway.example.yaml gateway.yaml
```

`gateway.yaml` 不进仓库。配置优先级为命令行参数 > 环境变量 > YAML > 内置默认值；不指定 `-config` 时读取当前目录的 `gateway.yaml`。

仓库根目录的 `gateway.example.yaml` 列出了所有配置项，启动前至少确认下面几组：

```yaml
addr: ":8091"

agentapi:
  base_url: "https://agent.qiniuapi.com"
  api_key: "<AgentAPI API Key>"
  agent_id: "agent_xxx"
  environment_id: "env_xxx"

render:
  base_url: "http://192.168.1.20:8090"

casting:
  base_url: "http://127.0.0.1:8080"

artifacts:
  dir: "artifacts"
  base_url: "http://192.168.1.20:8091"
```

几点容易配错的地方：

- Gateway 的内置默认值是 `:8090`，与 Render 的示例端口相同。两者必须分开：`gateway.example.yaml` 已把 `addr` 设为 `:8091`，`render.base_url` 指向 Render 的 `:8090`。用 `0.0.0.0:8091` 可让局域网访问，只在本机测试用 `127.0.0.1:8091`。
- `artifacts.base_url` 是 Render 下载应用包时使用的地址，必须指向 Gateway 自己（示例 `:8091`），并且从 Render 主机可以访问。写 `127.0.0.1` 会导致 Render 拉不到产物。
- `render.base_url` 从 Gateway 进程访问，必须填 Render 主机实际监听的地址。`casting.base_url` 只用于 `get-screens`/Portal 屏幕页的兼容视图与旧版路径；独立模式启动参数仍要求它，真机生产投屏不访问它。

## 启动

```bash
go run ./cmd/gateway -config gateway.yaml
```

看到下面的日志表示 HTTP 服务已经启动：

```text
gateway listening on :8091
```

`addr` 写成 `:8091` 时等价于监听 `0.0.0.0:8091`，局域网其他设备可以访问；只在本机测试时可以改成 `127.0.0.1:8091`。日志会原样打印配置里的 `addr`。

## 检查服务

```bash
curl http://192.168.1.20:8091/healthz
```

预期返回：

```json
{"status":"ok"}
```

健康检查会同时探测 SQLite，数据库不可用时返回 `503` 和 `{"status":"unavailable"}`。

## 接入 MCP

灵矽通过 MCP 使用控制面，端点如下：

| 端点 | 方法 | 用途 | 身份校验 |
| --- | --- | --- | --- |
| `/mcp` | Streamable HTTP（stateless） | 新接入使用这个地址 | 每个请求都重新校验三个身份头 |
| `/sse` | `GET` | 旧版 SSE 连接 | 建连时校验一次 |
| `/mcp/message` | `POST` | 旧版 SSE 的消息端点 | 每次请求都要与已建立的 SSE 会话一致 |

设置了 `mcp_token` 后，这三个端点都要求 `Authorization: Bearer <token>`，其他路径不受影响。门户模式下 MCP Token 在 `/admin/settings` 管理，首次启动自动生成并加密入库；`gateway.yaml` 的 `mcp_token` 只用于独立模式或空库首次导入。

每个 MCP 请求还要带灵矽身份头，控制面据此确定应用、屏幕和语音会话的归属：

| 请求头 | 说明 |
| --- | --- |
| `X-Linx-Device-Id` | 设备标识 |
| `X-Linx-Session-Id` | 本次语音会话标识 |
| `X-Linx-Agent-Id` | 智能体标识，门户模式下必填 |

这些头由灵矽侧携带。门户模式下手工联调：

- 缺任一头，`/mcp` 返回 `400 MISSING_IDENTITY`。
- 设备不存在、未完成开通或账号被停用，返回 `403`（`OWNER_NOT_FOUND` / `IDENTITY_NOT_REGISTERED` / `USER_DISABLED`）。
- `/mcp/message` 的头与会话不一致时返回 `403 IDENTITY_MISMATCH`；`/sse` 没有逐请求比对，建连时按上面的 400/403 处理。
- 身份查询本身失败（例如数据库不可用）返回 `503 IDENTITY_CHECK_FAILED`，不会放行请求。

单租户部署（`portal.enabled=false`）不启用 owner 校验器，但仍要求 `X-Linx-Device-Id` 与 `X-Linx-Session-Id`；缺了会连接失败。

灵矽云需要能直接访问这个地址。局域网地址只适合本机联调，正式接入需要把 Gateway 暴露到公网，并在灵矽平台上把 MCP 地址配置为 `https://<域名>/mcp`。如果灵矽云经隧道或反向代理访问 Gateway（请求从本机回环地址进来），MCP 入口只放行配置过的 Host，否则返回 `403 invalid Host header`。门户模式下管理后台里的公网 MCP 地址会自动放行，通常不用再配；独立模式或代理改写了 Host 时，把 `public_host` 设为对外使用的 `host:port`，修改后需要重启。直接暴露在公网网卡上时可以留空。

## 屏幕映射（可选，兼容视图）

控制面 `get-screens` 的兼容视图可以读取 Casting 发现到的电视。如果想给屏幕起一个稳定的业务标识，配置 `screens` 指向一个 JSON 文件：

```json
[
  {"screen_id": "tv-living-room", "name": "客厅电视", "target_id": "<Casting target id>"}
]
```

`target_id` 来自 Casting 的 `/v1/targets`。配置后 `get-screens` 会返回这些别名；它们只用于控制面屏幕视图，不参与 `start-app`。现场投屏用设备端 `self.cast.list_screens`，屏幕名以电视实际广播的 `friendlyName` 为准。

## 启用用户门户（可选）

门户与 Gateway 同进程，多租户、注册登录和管理后台都在其中。前端需要先构建：

```bash
cd portal
pnpm install
pnpm build
```

然后在 `gateway.yaml` 中打开门户，并设置管理员引导账号：

```yaml
portal:
  enabled: true
  username: "<管理员账号>"
  password: "<管理员密码>"
```

管理员密码也可以放在 `PORTAL_USERNAME` / `PORTAL_PASSWORD` 环境变量里，避免写进配置文件。

门户模式与独立模式的区别：

- 只有 `portal.username` / `portal.password` 变为必填。
- AgentAPI、Linx（含公网 MCP 地址、智能体模板）、默认 Render/Casting 都在首次启动后由管理员在 `/admin/settings` 配置，页面上每个字段都写明了数据从哪里取，配置文件里不需要再写这些项。
- MCP Token 首次启动时自动生成并加密保存，在 `/admin/settings` 里可以重新生成，网关会同步到灵矽的 MCP。
- 旧版 `gateway.yaml` 里已有的这些值只在数据库对应设置为空时写入一次，之后以数据库为准，改配置文件不再生效，可以从文件里删除。
- 默认拒绝内网地址。本地调试时要把 `services.allow_private_targets` 设为 `true`，才能绑定 `127.0.0.1` 或局域网服务；公网暴露或开放注册时保持 `false`，受信内网单租户部署可设为 `true`，但不要与开放注册同时使用。该开关同时放宽 custom 用户填写的 Render/Casting 地址。

启动后打开 <http://192.168.1.20:8091/> 查看 Home，管理员登录后进入 `/admin`，普通用户在 `/register` 注册后使用 `/portal`。浏览器麦克风需要 HTTPS 或 `localhost`，详见[多租户用户 Portal 设计](../architecture/interfaces/portal.md)。

## 数据与重启

- 应用状态、构建进度、产物下载令牌保存在 SQLite，默认 `open-anywhere.gateway.db`。
- 应用包保存在 `artifacts/`，通过 `GET /artifacts/{app_id}/{name}` 提供给 Render。
- 重启后会恢复应用记录、继续监听未完成的构建，并根据当前配置修正产物下载地址。
- 门户模式下 `portal-data/secret.key` 用于加密平台密钥和用户服务 token。**丢失后已保存的凭证无法解密，必须与数据库一起备份。** 首次配置、备份与恢复步骤见[部署与备份](deploy.md)。

## 日志与停止

服务日志直接输出到终端。按 `Ctrl-C` 会关闭 MCP 会话并在 5 秒内排空 HTTP 连接，不会停止 Render 上的应用实例、电视上的投屏或兼容 Casting 会话。

## 常见问题

| 现象 | 排查方向 |
| --- | --- |
| 启动报 `-xxx is required` | 独立模式下 AgentAPI、Render、Casting 配置必填（Casting 只用于兼容屏幕视图）；门户模式下只有管理员账号必填 |
| 启动报凭证无法解密 | 数据库里的密钥与当前主密钥不匹配，恢复原来的 `portal-data/secret.key` 或 `GATEWAY_SECRET_KEY` |
| Render 启动应用失败 | 用 Render 主机访问 `artifacts.base_url` 下的下载地址，确认 Render 能拉到产物 |
| MCP 返回 401 | 请求没有带 `Bearer` token，或 token 与当前 MCP Token 不一致（门户模式在 `/admin/settings` 里重新生成会让旧 token 立即失效） |
| 应用能构建但投屏失败 | 确认现场设备与电视同广播域、设备端 `self.cast.list_screens` 能看到电视，并检查 Render 的 `-public-url` 是否是电视能访问的地址；`casting.base_url` 只影响 `get-screens` 兼容视图 |
| 门户登录后无法保存平台配置 | 检查主密钥是否可用；查看服务日志中的解密错误 |

- **端口冲突**：Gateway、Render 以及（如果运行）Casting 必须使用不同端口，启动前确认 `addr`、`render.base_url`、`casting.base_url` 没有互相占用。
- **灵矽调用不了 MCP**：`/mcp` 必须是灵矽云能访问的 HTTPS 地址；`/sse` 只用于已经创建好的旧 MCP，新接入不要用。
- **构建一直停在 `building`**：检查 `agentapi.base_url`、API Key、`agent_id`、`environment_id` 是否正确，以及 AgentAPI 侧智能体版本是否可用。
- **更换部署地址后应用下载失败**：修改 `artifacts.base_url` 后重启 Gateway 即可，下载地址会按新配置重写，不需要重建应用。
