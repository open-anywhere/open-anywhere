# 现场语音入口：实现与使用

这是现场语音入口的模拟硬件实现：以一台已绑定灵矽智能体的虚拟设备身份连接灵矽云，提供本地 Web 控制台和 HTTP/SSE 控制接口。设计、与灵矽的连接方式和验证状态见 [docs/voice-entry.md](../../docs/voice-entry.md)。

## 启动

需要 Go。项目根目录的 `.device.json`（`mac`、`clientId`、`token`、`wsUrl`）是设备凭证，已在 `.gitignore`，不要提交，也不要打印 token。没有这个文件时 `-serve` 也能启动，在网页里注册设备即可（见下）。

```powershell
go run ./cmd/voiceentry -serve -listen 127.0.0.1:8788 -no-play
```

浏览器打开 `http://127.0.0.1:8788/`。网页麦克风和语音播放需要 Chrome 或 Edge（WebCodecs），并通过 `localhost` 或 HTTPS 访问。Opus 由浏览器编解码，服务端只转发，不需要 cgo、libopus、ffmpeg。

命令行一次性模式：

```powershell
go run ./cmd/voiceentry -text "你好"
go run ./cmd/voiceentry -announce "游戏开始了"
```

主要参数：`-serve`、`-listen`、`-device`（默认 `.device.json`）、`-mode`（默认 `realtime`）、`-no-play`。`-file`、`-mic` 需要用 `-tags "opus nolibopusfile"` 构建并安装 libopus。

## Web 控制台

- 显示连接状态、模式、会话、设备，实时事件流（可展开灵矽原始消息，`token` 字段已遮蔽）。
- 文字发送、主动播报、暂停/继续监听、打断回复。
- 一键断开重连、仅断开连接。手动断开后不会自动重连。
- 网页麦克风、语音播放、播放音量滑块。
- 设备注册：未注册时左侧显示“设备注册”。点“获取激活码”会生成新设备身份并向平台上报，拿到激活码后到灵矽控制台绑定，再点一次检查，拿到凭证后自动写入 `.device.json` 并连接。也可填 Agent ID 和 API Key 自动绑定；API Key 只用于这次请求，不保存。

## 本地接口

| 接口 | 说明 |
| --- | --- |
| `GET /status` | `state`、`mode`、`session_id`、`mac`、`connected`、`volume`、`registered`、`activation_code` |
| `GET /events` | SSE 事件流：`hello`、`stt`、`tts`、`llm`、`mcp`、`audio`、`volume` |
| `POST /say` | `{"text":"…"}` 以用户身份发一句话 |
| `POST /announce` | `{"text":"…"}` 让灵矽主动播报 |
| `POST /pause` `/resume` `/abort` | 暂停、继续监听，打断当前回复 |
| `POST /device/register` | `{"agent_id":"…","api_key":"…"}`（可为空对象）；返回 `pending`+激活码，或 `registered`。需 `Content-Type: application/json` |
| `POST /disconnect` `/reconnect` | 断开；断开后重连（已连接时换新会话，失败返回 502） |
| `WS /audio?codec=opus` | 浏览器上行 Opus 包 |
| `WS /playback` | 下行 Opus 包，首条文本消息给出采样率 |

这些接口只用于调试和演示，不是系统之间的契约。控制接口没有鉴权，只应监听 `127.0.0.1`，不要把该端口暴露到公网。

## 端侧 MCP 工具

模拟硬件向灵矽上报两个仅作用于网页播放器的工具，用于验证 `tools/list` / `tools/call` 链路：

- `self.audio_speaker.set_volume`：`volume` 为 0–100 整数
- `self.audio_speaker.get_volume`

这不是 Windows 系统音量。

加 `-casting-url <本地 casting 地址>` 后，另上报 3 个 `self.cast.*` 投屏工具：查屏（`GET /v1/targets`）、投放（`POST /v1/projection-sessions`）、停止（`DELETE`）。它只连本机局域网里的 casting，不连云端，也没有身份头或 token；应用由控制面启动并返回 `stream_url`，设备把地址原样投到现场电视上。设计背景见 [小智云聊 S3 MAX 固件方案](../../docs/operations/yunliao-s3-max-firmware-mcp.md)。

## 目录

```text
cmd/voiceentry/                 参数解析、启动与退出信号
internal/voiceentry/            主循环、会话传输、断线重连
internal/voiceentry/linx/       设备文件、OTA、注册底层、WebSocket 会话、MCP 应答
internal/voiceentry/voice/      监听状态机、播报队列、断开/重连
internal/voiceentry/register/   设备注册流程
internal/voiceentry/tools/      上报给灵矽的 MCP 工具
internal/voiceentry/casting/    投屏工具：本地 casting HTTP 客户端与 self.cast.* 工具
internal/voiceentry/controlapi/ HTTP/SSE/WebSocket 接口和 console.html
internal/voiceentry/audio/      Opus codec、ffmpeg/ffplay 封装
```

## 测试

```powershell
go vet ./...
go test ./cmd/voiceentry/... ./internal/voiceentry/...
```

`-race` 需要 cgo。
