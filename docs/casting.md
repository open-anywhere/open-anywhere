# Casting 服务

Casting 是独立的局域网投屏控制服务。第一版支持通过 SSDP 发现 DLNA MediaRenderer，并控制它播放 Client 已准备好的 HTTP(S) MPEG-TS 实时流。Client 负责媒体生成、编码和 URL 可达性；电视直接拉流，Casting 不读取、代理或转码媒体。

## 目录

```text
cmd/casting/                  配置、依赖组装、启动与关闭
pkg/casting/types.go          Client 和服务端共用的 JSON 契约
internal/casting/             HTTP、会话管理、设备注册表和 Adapter 接口
internal/casting/dlna/        SSDP 发现、设备描述解析及 SOAP 控制
```

协议细节及设备控制 URL 留在 internal 包中。未来可在 `pkg/casting` 增加 Go HTTP Client，并通过新的 Adapter 接入其他协议。

## 启动

```sh
go run ./cmd/casting -addr :8080 -interface en0
```

`-interface` 是本机局域网网卡名称，macOS Wi-Fi 常见为 `en0`，Linux 可能是 `eth0` 或 `wlan0`。省略时使用系统默认路由；启用 VPN 时建议显式指定。启动即扫描，之后默认每 30 秒重新扫描，可通过 `-scan-interval` 修改。

设备 ID 来自 UPnP UDN，与 DHCP 地址无关。设备按 SSDP `max-age` 过期后标记 `offline`，再次发现时更新地址并恢复在线。

## API 示例

```sh
curl http://localhost:8080/v1/targets
curl http://localhost:8080/v1/targets/TARGET_ID
```

Client 根据返回的 `target.id` 和 `endpoint.id` 创建会话：

```sh
curl -i http://localhost:8080/v1/projection-sessions \
  -H 'Content-Type: application/json' \
  -d '{
    "target_id": "TARGET_ID",
    "endpoint_id": "dlna-renderer",
    "source": {
      "type": "stream",
      "url": "http://client.lan:8765/live.ts",
      "format": "mpegts",
      "video_codec": "h264",
      "audio_codec": "aac"
    }
  }'
```

返回 `201 Created`，包含 `session_id` 和 `Location`。纯视频可以将 `audio_codec` 置为空字符串。

服务端先执行 `SetAVTransportURI` 和 `Play`，再查询 `GetTransportInfo`。仅当设备报告 `PLAYING` 时才返回 `playing`；设备仍在切换或暂时无法确认时保留 `starting`。通过查询会话刷新状态：

```sh
curl http://localhost:8080/v1/projection-sessions/SESSION_ID
curl -X DELETE http://localhost:8080/v1/projection-sessions/SESSION_ID
curl http://localhost:8080/healthz
```

停止是幂等的，重复停止旧会话不会停止新会话。停止只控制电视，不关闭 Client 的媒体服务。

| 状态码 | 含义 |
| --- | --- |
| `400` | 请求字段、JSON 或 URL 无效 |
| `404` | Target、Endpoint 或 Session 不存在 |
| `409` | Target 离线或已有活动 Session |
| `413` | 请求体超过 64 KiB |
| `415` | 请求 Content-Type 不是 application/json |
| `422` | 不支持请求的流格式、编码或协议 |
| `502` | 设备控制或状态查询失败 |

创建失败时，如果已分配会话，错误响应包含 `error.session_id`，可用于查询或重试停止。服务会尝试清理未完成的投屏；如果清理也失败，会保留设备占用，直到成功停止，避免新会话覆盖不确定的播放状态。

## 当前边界

- 只注册 `dlna-renderer`；AirPlay 是架构中的后续扩展，不会作为可用 Endpoint 返回。
- `capabilities` 表示当前 Adapter 开放的 MPEG-TS/H.264/AAC 配置范围，不枚举电视的全部硬件解码能力。具体设备能否播放仍以设备反馈和实机兼容性验证为准。
- `streaming` 表示可边生成边播放，不保证电视缓冲延迟。
- 设备和会话保存在内存中。服务重启不恢复之前的会话，也不会自动停止电视上已经开始的播放。
- 状态按 GET 请求刷新；没有后台推送、数据库或自动重连。
- 仅在可信局域网使用，当前没有用户鉴权。

## 测试

```sh
go test ./... -race -coverprofile=coverage.out
go vet ./...
go tool cover -func=coverage.out
```

测试使用 `httptest` HTTP 服务和 `127.0.0.1` 临时 UDP 端口，覆盖 SSDP 报文、设备描述、SOAP 操作、媒体 URL 与元数据转义、错误响应、会话状态、取消、设备过期及并发占用。集成测试中的模拟电视会拉取临时媒体地址，验证 API 到设备的播放/停止流程。

`go test` 不探测实际局域网、不访问固定 IP 或外网、不需要电视、浏览器或 FFmpeg。实机验证独立进行：启动本机媒体流，运行 Casting，再按上述 API 选择真实 Target 创建、查询和停止会话。
