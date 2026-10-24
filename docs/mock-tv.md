# 模拟电视 mock-tv

`mock-tv` 是一个本地开发用的假 DLNA MediaRenderer。投屏服务通过 SSDP 发现它、读取设备描述并通过 AVTransport 控制它，和面对一台真实电视没有区别；它把收到的流地址交给同一进程提供的浏览器页面，由页面播放 MPEG-TS 流。

适合以下场景：

- 手边没有电视，或电视不在同一局域网；
- 想在没有硬件的情况下联调 Casting、Gateway、Render 的完整投屏链路；
- 想直接观察 Render 生成的画面，而不经过真实电视的解码器。

链路如下：

```text
Casting ──SSDP M-SEARCH──▶ mock-tv
Casting ──HTTP GET /device.xml──▶ mock-tv
Casting ──SOAP SetAVTransportURI / Play──▶ mock-tv
mock-tv ──SSE /events──▶ 浏览器页面
浏览器 ──GET /stream?url=...──▶ mock-tv ──HTTP──▶ Render 的 live.ts
浏览器 ──mpegts.js / MSE──▶ 画面
```

Casting、Gateway 和 Render 不需要任何改动，也不需要知道对面是模拟电视。

## 快速开始

先启动模拟电视。`-iface` 填本机连接局域网的网卡，macOS Wi-Fi 通常是 `en0`：

```sh
go run ./cmd/mock-tv -addr :8095 -iface en0
```

打开 <http://127.0.0.1:8095/>，页面会显示「等待投屏…」。

另开终端启动投屏服务，使用同一张网卡：

```sh
go run ./cmd/casting -addr :8080 -interface en0
```

确认发现成功：

```sh
curl -s http://127.0.0.1:8080/v1/targets
```

结果中应出现：

```json
{
  "id": "dlna-open-anywhere-mock-tv",
  "name": "Mock TV",
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
    }
  ]
}
```

之后按正常流程创建投屏会话，或让 Gateway 启动应用。页面收到 `playing` 后开始播放。

## 接入 Gateway

Gateway 通过 `screens` 配置把屏幕 ID 映射到投屏目标。新建 `screens.json`：

```json
[
  {
    "screen_id": "tv-living-room",
    "name": "客厅电视",
    "target_id": "dlna-open-anywhere-mock-tv"
  }
]
```

在 `gateway.yaml` 中指向它：

```yaml
screens: "screens.json"
```

启动 Gateway 后，灵矽调用 `get-screens` 就能看到这块屏幕，`start-app` 会通过 Casting 把 Render 的流投到 `mock-tv`。

需要同时模拟多台电视时，为每个实例指定不同的 HTTP 端口和 UDN，例如：

```sh
go run ./cmd/mock-tv -addr :8096 -iface en0 -udn uuid:open-anywhere-mock-tv-2 -name "卧室电视"
```

对应的目标 ID 为 `dlna-open-anywhere-mock-tv-2`，在 `screens.json` 中再增加一项即可。

## 命令行参数

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `-addr` | `:8095` | HTTP 监听地址；页面、设备描述、SOAP 和流代理都在这个端口 |
| `-iface` | 系统默认 | SSDP 使用的网卡，例如 `en0`；与 Casting 的 `-interface` 保持一致 |
| `-public-url` | `http://127.0.0.1:<端口>` | 写入 SSDP `LOCATION` 的根地址，也是提示用户打开的地址 |
| `-name` | `Mock TV` | DLNA `friendlyName`，会显示在 Casting 的屏幕列表里 |
| `-udn` | `uuid:open-anywhere-mock-tv` | DLNA 设备唯一标识；同一网段内不能重复 |
| `-max-age` | `30m` | SSDP 广告有效期，Casting 据此判断设备是否离线 |
| `-no-ssdp` | `false` | 不加入 SSDP 组播，仅通过 HTTP API 手动驱动页面 |

默认的 `LOCATION` 是 `127.0.0.1`，因此 Casting 必须和 `mock-tv` 在同一台机器上。如果要从另一台设备打开页面，或让另一台机器上的 Casting 发现它，需要监听所有网卡并公布局域网地址：

```sh
go run ./cmd/mock-tv -addr :8095 -iface en0 -public-url http://192.168.1.4:8095
```

注意 `192.168.1.4` 要换成本机在局域网中的实际地址。

## 手动驱动

不想经过 Casting 时，可以直接调用 HTTP API，适合单独调试 Render（端口以 Render 实际监听地址为准）：

```sh
# 播放一个本机可达的 MPEG-TS 流
curl -X POST http://127.0.0.1:8095/api/play \
  -H 'Content-Type: application/json' \
  -d '{"url":"http://127.0.0.1:8091/streams/INSTANCE/live.ts"}'

curl http://127.0.0.1:8095/api/state
curl -X POST http://127.0.0.1:8095/api/stop
```

`/api/play` 只接受 HTTP(S) 且解析到本机、局域网、链路本地或 `100.64.0.0/10` 地址的 URL；公网地址会被拒绝。用 `-no-ssdp` 启动时，页面仍然可以通过这两个 API 驱动。

也可以直接访问页面使用的接口：

| 接口 | 用途 |
| --- | --- |
| `GET /` | 播放页面 |
| `GET /device.xml` | UPnP 设备描述 |
| `POST /control/avtransport` | AVTransport SOAP，支持 `SetAVTransportURI`、`Play`、`Stop`、`GetTransportInfo` |
| `GET /events` | SSE，事件名 `state`，数据为当前状态 JSON |
| `GET /api/state` | 当前状态 |
| `POST /api/play` | 设置并立即播放一个流 |
| `POST /api/stop` | 停止并清空画面 |
| `GET /stream?url=...` | 同源代理 MPEG-TS 流，供浏览器页面使用 |
| `GET /healthz` | 健康检查 |

## 浏览器与媒体要求

- 页面使用 [mpegts.js](https://github.com/xqq/mpegts.js) 1.8.2 和 MSE，推荐 Chrome 或 Edge。不支持 MSE 的浏览器会显示提示。
- `mock-tv` 不转码、不转封装，只把 Render 的 `video/mp2t` 原样代理给页面。Render 当前输出 H.264、无音频，浏览器可以正常解码。
- 代理是为了避开跨域限制，并保证页面只访问同源地址。代理只允许本机和私有网段地址，不是通用 HTTP 代理。
- 浏览器可能阻止自动播放；页面会显示「点击开始播放」，点一下即可。首次使用建议先与页面交互一次，或把站点加入自动播放白名单。

## 当前边界

- 只实现 AVTransport 的最小子集：设置 URI、播放、停止、查询传输状态。没有暂停、跳转、音量、静音和 `SUBSCRIBE` 事件订阅；Casting 会主动查询状态，不依赖事件。
- `GetTransportInfo` 只返回 `PLAYING` 或 `STOPPED`，不区分暂停和过渡状态。
- 状态只保存在内存中，进程退出后丢失；页面上同时只维护一个播放器。
- 控制接口没有鉴权。默认只监听本机；在可信局域网内用 `-addr :8095` 暴露时，请自行确认网络边界。
- 它模拟的是「电视如何接收投屏」，不模拟电视的解码能力、HDCP、遥控器或待机状态，因此不能替代真实设备的兼容性测试。

## 排查

**Casting 列表为空：**

1. 确认两边使用同一张网卡，`mock-tv` 日志中出现 `answered M-SEARCH from ...`；
2. 确认 UDP 1900 未被防火墙拦截，VPN 未把组播路由到其他接口；
3. 直接访问 `curl http://127.0.0.1:8095/device.xml`，确认设备描述可读；
4. 如果运行环境不允许加入组播，使用 `-no-ssdp`，并通过 `/api/play` 手动验证页面。

**页面提示浏览器不支持 MSE：** 换 Chrome 或 Edge；Safari 对 MSE 的支持有限。

**页面一直停在等待投屏：** 检查 `/api/state` 是否已经是 `playing`，以及浏览器能否访问 `mock-tv`。若从另一台设备打开页面，`-public-url` 必须是那台设备可达的地址。

**画面黑屏或不断重试：** 先用 `curl -v "http://127.0.0.1:8095/stream?url=..."` 确认代理能读到数据；再确认 Render 的流仍在输出、编码为 H.264。

## 测试

```sh
go test ./internal/mocktv/... ./cmd/mock-tv/... -race
```

测试覆盖：

- 用 `internal/casting/dlna.Transport` 真实驱动 `mock-tv` 的 SOAP 接口，保证与 Casting 客户端不漂移；
- SSDP M-SEARCH 响应、设备描述 XML、转义和 UDN；
- 状态流转、HTTP API、SSE 事件、流代理和公网 URL 拒绝；
- `-public-url` 的默认值与校验。

需要联调组播时，可以同时启动 `mock-tv` 和 `casting`，用 `/v1/targets` 验证发现，再创建一个指向本地流的投屏会话，确认 `/api/state` 从 `idle` 变成 `playing`，停止后回到 `idle`。
