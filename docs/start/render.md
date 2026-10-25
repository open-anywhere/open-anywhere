# 启动 Render

Render 负责运行应用、捕获页面画面，并为 Casting 提供电视可以访问的 MPEG-TS 媒体流。

## 前置条件

Render 主机需要安装：

- Node.js 20 或更新版本
- Chromium 或 Google Chrome
- 带 `libx264` 和 AAC 编码器的 FFmpeg
- `lsof`

如果需要页面声音，还需要准备一个虚拟音频设备，让 Chromium 的声音可以被 FFmpeg 采集。

## 纯视频启动

在仓库根目录执行：

```bash
go run ./cmd/render \
  -addr 0.0.0.0:8090 \
  -public-url http://192.168.1.20:8090 \
  -work-dir /tmp/open-anywhere-render
```

把 `192.168.1.20` 换成 Render 主机在局域网中的 IP。`-public-url` 必须同时被 Gateway 和电视访问。

启动成功后检查：

```bash
curl http://192.168.1.20:8090/healthz
```

预期返回：

```json
{"status":"ok"}
```

## macOS 启动并采集音频

macOS 需要 BlackHole 或其他虚拟音频设备。先列出 FFmpeg 能看到的音频设备：

```bash
ffmpeg -f avfoundation -list_devices true -i ""
```

假设虚拟音频设备编号是 `6`，使用下面的命令启动：

```bash
go run ./cmd/render \
  -addr 0.0.0.0:8090 \
  -public-url http://192.168.1.20:8090 \
  -work-dir /tmp/open-anywhere-render \
  -audio-format avfoundation \
  -audio-input :6
```

`avfoundation` 是 FFmpeg 在 macOS 上的采集格式，`:6` 中的 `6` 是音频设备编号，实际编号以列表输出为准。

## Linux 启动并采集音频

使用 PulseAudio 或 PipeWire 的 PulseAudio 兼容接口创建虚拟输出：

```bash
pactl load-module module-null-sink sink_name=render
pactl set-default-sink render
```

然后启动 Render：

```bash
go run ./cmd/render \
  -addr 0.0.0.0:8090 \
  -public-url http://192.168.1.20:8090 \
  -work-dir /tmp/open-anywhere-render \
  -audio-format pulse \
  -audio-input render.monitor
```

`-audio-format` 和 `-audio-input` 必须同时设置。配置音频后，Render 会把页面声音编码为 AAC，并在实例的 `source.audio_codec` 中返回 `aac`；不配置时输出纯视频。

## 启动应用

Render 启动后，还需要通过 `POST /v1/apps` 启动一个应用实例：

```bash
curl -X POST http://192.168.1.20:8090/v1/apps \
  -H 'Content-Type: application/json' \
  -d '{
    "app_id": "demo-app",
    "artifact_url": "http://192.168.1.20:8091/app.tar.gz",
    "port": 39101
  }'
```

`artifact_url` 必须是 Render 主机可以访问的应用包地址，`port` 是应用在 Render 主机上监听的端口。响应中的 `source` 可以直接交给 Casting。

## 日志与停止

服务日志直接输出到终端。每个实例的临时目录位于 `-work-dir` 下，包含：

- `app.log`：应用进程日志
- `encoder.log`：FFmpeg 编码日志

停止实例：

```bash
curl -X DELETE http://192.168.1.20:8090/v1/apps/<instance_id>
```

停止 Render 进程可以在终端按 `Ctrl-C`。

## 常见问题

- 局域网无法访问：确认 `-addr` 使用 `0.0.0.0:8090`，macOS 防火墙已允许该端口，并且 `-public-url` 使用局域网 IP。
- 音频启动失败：重新执行设备列表命令，确认音频编号有效；音频格式和输入必须成对配置。
- 电视没有声音：确认 Chromium 的系统输出切到了虚拟音频设备，并检查实例响应中的 `source.audio_codec` 是否为 `aac`。
- 电视无法播放：先使用 `ffprobe <source.url>` 检查媒体流，再确认电视端支持 MPEG-TS、H.264 和 AAC。
