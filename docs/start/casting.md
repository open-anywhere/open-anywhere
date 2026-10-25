# 启动 Casting

Casting 负责在局域网中发现电视等 DLNA MediaRenderer，并控制电视播放 Render 提供的 MPEG-TS 媒体流。Casting 不生成、代理或转码媒体，电视需要能够直接访问 Render 的媒体地址。

## 启动

在仓库根目录执行：

```bash
go run ./cmd/casting \
  -addr 0.0.0.0:8080 \
  -interface en0
```

macOS 的 Wi-Fi 通常是 `en0`，Linux 常见为 `eth0` 或 `wlan0`。如果不确定网卡名称，可以查看：

```bash
# macOS
networksetup -listallhardwareports

# Linux
ip -br addr
```

`-addr` 是 Casting HTTP API 的监听地址。使用 `0.0.0.0:8080` 可以让局域网其他设备访问；只在本机测试时也可以使用 `:8080`。

启动后服务会立即执行一次 SSDP 设备发现，默认每 30 秒重新扫描。看到类似下面的日志表示 API 已经启动：

```text
casting API listening on [::]:8080
```

## 检查服务和电视

```bash
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:8080/v1/targets
```

健康检查预期返回：

```json
{"status":"ok"}
```

`/v1/targets` 返回发现到的电视。选择状态为 `online` 的 Target，并记下它的 `id` 和兼容的 Endpoint `id`，通常 Endpoint 是 `dlna-renderer`：

```bash
curl http://127.0.0.1:8080/v1/targets
```

如果发现不到电视，确认 Mac/服务器与电视连接在同一个局域网，网卡参数正确，并且交换机或路由器没有启用 AP 隔离。SSDP 使用局域网组播，不能通过普通 HTTP 代理发现设备。

## 开始投屏

先启动 Render，并获得它返回的 `source`，例如：

```json
{
  "type": "stream",
  "url": "http://192.168.31.203:8090/streams/INSTANCE_ID/live.ts",
  "format": "mpegts",
  "video_codec": "h264",
  "audio_codec": "aac"
}
```

然后创建投屏会话：

```bash
curl -X POST http://127.0.0.1:8080/v1/projection-sessions \
  -H 'Content-Type: application/json' \
  -d '{
    "target_id": "TARGET_ID",
    "endpoint_id": "dlna-renderer",
    "source": {
      "type": "stream",
      "url": "http://192.168.31.203:8090/streams/INSTANCE_ID/live.ts",
      "format": "mpegts",
      "video_codec": "h264",
      "audio_codec": "aac"
    }
  }'
```

纯视频流将 `audio_codec` 设为空字符串。`source.url` 必须能被电视直接访问，因此不能填写 Render 的 `127.0.0.1` 地址。

创建成功后，查询播放状态：

```bash
curl http://127.0.0.1:8080/v1/projection-sessions/SESSION_ID
```

只有当电视报告 `PLAYING` 时，会话才会进入 `playing`；刚创建完成时可能暂时是 `starting`。

## 停止投屏

```bash
curl -X DELETE http://127.0.0.1:8080/v1/projection-sessions/SESSION_ID
```

停止 Casting 不会停止 Render。Render 应用实例需要单独停止：

```bash
curl -X DELETE http://192.168.31.203:8090/v1/apps/INSTANCE_ID
```

## 可选参数

```bash
go run ./cmd/casting \
  -addr 0.0.0.0:8080 \
  -interface en0 \
  -scan-interval 30s \
  -unreachable-grace 30s
```

- `-scan-interval`：SSDP 扫描和会话后台检查间隔，默认 `30s`。
- `-unreachable-grace`：设备连续不可达多久后释放占用，默认 `30s`。
- `-unreachable-grace -1s`：关闭自动释放，适合需要人工确认设备状态的场景。

## 常见问题

- **API 能访问但发现不到电视**：检查 `-interface` 是否是连接电视的网卡；关闭 VPN 或显式指定局域网网卡；确认路由器允许 SSDP 组播。
- **电视无法播放**：用电视所在网络中的设备访问 `source.url`，确认 Render 的 `-public-url` 使用局域网地址；再用 `ffprobe` 检查 H.264/AAC 媒体流。
- **会话一直是 `starting`**：电视尚未报告 `PLAYING`，检查电视是否支持 MPEG-TS/H.264/AAC，以及媒体 URL 是否可达。
- **电视占用没有立即释放**：Casting 会等待连续不可达宽限期，默认最多约 30 秒；最后一次确认状态为 `playing` 时等待时间会加倍。
