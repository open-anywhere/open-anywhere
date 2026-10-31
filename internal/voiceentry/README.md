# 现场语音入口：实现与使用

这是现场语音入口的模拟硬件实现：以一台已绑定灵矽智能体的虚拟设备身份连接灵矽云，收发文字与 Opus 音频。运行时嵌在 Gateway 进程里，由 Portal 的设备页托管，不再有独立进程。

设计、与灵矽的连接方式和验证状态见 [docs/architecture/voice-entry.md](../../docs/architecture/voice-entry.md)。

## 为什么嵌在 Gateway 里

虚拟设备的生命周期本来就由 Portal 管：创建、注册、断线重连、删除都在网关数据库里。把运行时放进同一进程后，一台设备只有一个所有者，也不需要再维护第二个端口、第二套凭证路径和第二个控制台。

## 使用

在 Portal 的设备页创建并注册一台虚拟设备，Gateway 会为它启动运行时；打开「对话」页即可说话或打字。连接状态、重连与断开都在同一个页面。

投屏工具默认关闭。需要让虚拟设备发现局域网电视时，启动 Gateway 时指定网卡：

```bash
go run ./cmd/gateway -config gateway.yaml -casting-interface en0
```

运行时会在进程内直接发 SSDP、控制 DLNA，不需要单独的 casting 服务。

## 目录

```text
internal/voiceentry/            主循环、会话传输、断线重连
internal/voiceentry/linx/       设备文件、OTA、注册底层、WebSocket 会话、MCP 应答
internal/voiceentry/voice/      监听状态机、播报队列、断开/重连
internal/voiceentry/tools/      上报给灵矽的 MCP 音量工具
internal/voiceentry/casting/    投屏工具与进程内 DLNA Caster
internal/voiceentry/controlapi/ 供 Portal 挂载的 HTTP / SSE / WebSocket 接口
internal/voiceentry/audio/      Opus codec、ffmpeg/ffplay 封装
```

## 测试

```powershell
go vet ./...
go test ./internal/voiceentry/...
```

`-race` 需要 cgo。
