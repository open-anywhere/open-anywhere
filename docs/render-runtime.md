# 运行 Render 与视频 E2E

Render 的第一期实现位于 `cmd/render`、`internal/render` 和 `pkg/render`。HTTP 字段及生命周期见[控制面与 Render 接口](gateway-render-interface.md)。支持 macOS 和 Linux，应用运行时为 Node.js。

## 依赖与启动

主机需安装 Node.js 20 或更新版本、Chromium / Google Chrome、带 `libx264` 和 AAC 编码器的 FFmpeg，以及 `lsof`。Render 使用独立的 headless Chromium，不采集用户桌面，不需要 macOS 屏幕录制权限。需要页面声音时，还需准备 Chromium 能输出到的虚拟音频设备和 FFmpeg 能读取的设备输入。

```sh
go run ./cmd/render \
  -addr :8090 \
  -public-url http://192.168.1.20:8090 \
  -work-dir /tmp/open-anywhere-render
```

将 `192.168.1.20` 换成控制面和电视可达的主机地址。应用地址默认使用同一主机名；控制面通过不同地址访问应用时，可配置 `-app-host`。`-chrome`、`-node`、`-ffmpeg` 可以指定可执行文件路径。

Linux 上 Chromium 需要可用的浏览器沙箱。Ubuntu 23.10+ 的 AppArmor 可能限制自行下载的浏览器创建 user namespace；E2E CI 为测试用 Chromium 的确切路径加载专用 AppArmor 配置，保留浏览器沙箱。

API 和媒体流共用 `-addr`，应用按请求中的 `port` 监听。默认视频为 1280×720（16:9）、10 fps，可用 `-width`、`-height`、`-fps` 配置；宽高必须保持 16:9。服务拒绝非法尺寸、比例、帧率或缺少公开地址的配置。默认只输出视频；需要声音时同时设置 `-audio-format` 和 `-audio-input`。

## 音频采集

Render 不负责创建或选择系统音频设备。部署方应先创建 loopback/null sink，让 Chromium 的音频输出进入该设备，再把对应的 monitor/input 交给 Render。Render 会把截图和设备采样交给同一个 FFmpeg 进程，编码为 MPEG-TS/H.264/AAC；返回的 `source.audio_codec` 才会是 `aac`。两个音频参数必须同时设置；都为空时保持纯视频。

Linux（PulseAudio 或 PipeWire 兼容 PulseAudio）示例：

```sh
pactl load-module module-null-sink sink_name=render
pactl set-default-sink render
go run ./cmd/render \
  -addr :8090 \
  -public-url http://192.168.1.20:8090 \
  -work-dir /tmp/open-anywhere-render \
  -audio-format pulse \
  -audio-input render.monitor
```

macOS 可使用 BlackHole 等 loopback 音频设备。把系统输出切换到该设备后，用 `ffmpeg -f avfoundation -list_devices true -i ""` 找到音频输入编号，再启动：

```sh
go run ./cmd/render \
  -addr :8090 \
  -public-url http://192.168.1.20:8090 \
  -work-dir /tmp/open-anywhere-render \
  -audio-format avfoundation \
  -audio-input :6
```

开启音频时 Chromium 会关闭静音并允许页面自动播放，页面中的 `<audio>`、Web Audio 和游戏音效会进入同一条投屏流。音频设备不可用会导致实例启动失败，不会错误地声明 AAC 或静默降级。一个 Render 进程中的多个实例共享配置的音频输入，若需要隔离应分别运行 Render 进程和音频设备。

## 可下载的计分板应用包

`e2e/testdata/scoreboard` 是自包含应用产物，包含 `app/app.js`、`app/package.json`、`app/public/`、`openapi.yaml` 和 `launch.json`。打包：

```sh
./scripts/package-scoreboard.sh
```

生成 `e2e/artifacts/scoreboard.tar.gz`。在一个终端提供文件下载：

```sh
python3 -m http.server 8091 --directory e2e/artifacts
```

本机调用示例：

```sh
curl http://localhost:8090/v1/ports/39101
curl http://localhost:8090/v1/apps \
  -H 'Content-Type: application/json' \
  -d '{"app_id":"app-scoreboard","artifact_url":"http://127.0.0.1:8091/scoreboard.tar.gz","port":39101}'
```

这里的 `127.0.0.1` 假设文件服务与 Render 在同一主机，远程部署须使用 Render 可访问的下载地址。调用业务操作：

```sh
curl http://localhost:39101/api/score \
  -H 'Content-Type: application/json' \
  -d '{"team_id":"red","points":1}'
curl http://localhost:39101/api/state
curl http://localhost:39101/api/reset \
  -H 'Content-Type: application/json' -d '{}'
```

响应中的 `source` 可以直接交给 Casting。也可以用 `ffplay '<source.url>'` 查看媒体流。停止时调用 `DELETE /v1/apps/<instance_id>`；不要用 `app_id` 替代路径里的运行实例 ID。

## E2E 检查

```sh
RENDER_E2E_OUTPUT="$PWD/e2e/results" \
  go test -race -tags=e2e -v ./e2e -count=1 -timeout=180s
```

Chrome 不在默认路径时，额外设置 `RENDER_CHROME`。E2E 缺少依赖会直接失败，不会跳过并显示成功。

测试将产物目录打成真实 tar.gz，通过临时 HTTP 服务交给 Render，再验证：

- 应用按注入端口启动，健康检查、页面和业务 API 可访问。
- 应用 ID 和端口占用关联正确，重复 `app_id`、重复端口返回冲突。
- FFprobe 识别实际 HTTP 视频流为 H.264、1280×720；默认配置无音轨，配置音频输入后为 H.264 + AAC。
- FFmpeg 持续解码真实视频，状态块从灰色变为红色、蓝色，再重置为灰色；同时核对业务分数。
- 中途新建拉流连接也能解码当前状态。
- 停止结束媒体连接、释放端口和运行目录；同一应用可换端口重新启动。
- 真实 Node.js 进程被终止后进入 `error`；缺少 `/health` 的包启动失败并完成清理。

状态色块固定在页面坐标中。断言使用区域平均色与压缩容差，不依赖 OCR 或整页像素完全一致。`RENDER_E2E_OUTPUT` 指定的目录会保存解码后的视频帧，便于检查和展示。

设置 `RENDER_AUDIO_FORMAT` 与 `RENDER_AUDIO_INPUT` 后，同一 E2E 会要求 HTTP 流包含 AAC 音轨并检查 `source.audio_codec`；未设置时继续验证纯视频。

普通的 `go test ./... -race` 覆盖归档与契约校验、并发期间的实例/端口预留、启动失败清理、HTTP 输入错误和慢速拉流客户端隔离；需要 `lsof`，但不启动浏览器或 FFmpeg。专门的 Render E2E CI 安装媒体依赖并执行真实链路。

## 实现边界

- 捕获采用 Chromium DevTools 截图采样，经 FFmpeg 编码为 MPEG-TS/H.264；默认 10 fps，适用于计分板等低动态页面。配置音频输入后，FFmpeg 同时采集并编码 AAC，音视频在同一个复用器中按实时输入时间戳封装。它不是桌面镜像或高帧率游戏串流。
- 页面按固定 1280×720 画布截图，超出 viewport 的内容会被裁切。Render 会检测并记录 `layout_overflow` 告警，但不会自动缩放或中断播放。
- 每个实例具有独立进程、工作目录、浏览器和编码器。同一 Render 上按 `app_id` 原子预留；跨节点单实例仍由控制面保证，本服务不实现分布式锁。
- 进程退出、连续三次健康检查失败或视频输出中断会触发错误与清理；不自动重启应用。状态和实例记录仅在内存中，服务重启不恢复旧实例。
- 归档拒绝路径穿越、链接和特殊文件，压缩及展开数据各限制 100 MiB。OpenAPI 校验目前覆盖版本、业务路径和操作元信息，不是完整的 OpenAPI / JSON Schema 校验器；构建端仍需完整校验及业务自测。
- 端口列表合并 Render 预留和 `lsof` 可见的 TCP 监听记录；端口检查还进行与应用监听行为一致的绑定探测。系统权限可能限制其他用户进程的可见范围，列表不能代替启动时的绑定结果。
- 这是运行可信构建产物的本地服务，尚无用户鉴权和 Node.js 沙箱隔离。应用启动时只继承必要环境变量；应用日志与编码日志在实例工作目录中，清理后删除，生命周期错误写服务日志。
- 已实现视频流准备与 HTTP 拉流验证；电视播放兼容性、网络延迟和 Casting 实机测试需另行验证。`ready` 不代表电视已经 `playing`。
