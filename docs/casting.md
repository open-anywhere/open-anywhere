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

`-interface` 是本机局域网网卡名称，macOS Wi-Fi 常见为 `en0`，Linux 可能是 `eth0` 或 `wlan0`。省略时使用系统默认路由；启用 VPN 时建议显式指定。启动即扫描，之后默认每 30 秒重新扫描，可通过 `-scan-interval` 修改。每次扫描后还会顺带检查本服务记录的会话，见下文“设备失联与占用释放”。

`-unreachable-grace` 控制设备连续不可达多久后释放屏幕占用，默认 30 秒；传负值关闭自动释放。设备最后一次确认状态为 `playing` 时使用两倍宽限期（默认 60 秒）。

设备 ID 来自 UPnP UDN，与 DHCP 地址无关。设备按 SSDP `max-age` 过期后标记 `offline`，再次发现时更新地址并恢复在线。已有会话在每次访问设备前都会重新解析地址，因此电视换 IP 后仍能继续控制。

## API 示例

```sh
curl http://localhost:8080/v1/targets
curl http://localhost:8080/v1/targets/TARGET_ID
```

Target 有本服务管理的活动会话时，列表和详情会包含 `active_session_id`；空闲时省略该字段。例如：

```json
{
  "id": "TARGET_ID",
  "name": "客厅电视",
  "state": "online",
  "active_session_id": "projection-SESSION_ID",
  "endpoints": []
}
```

上例省略了 Endpoint 内容。拿到 `active_session_id` 后，可以查询播放状态或停止投屏：

```sh
curl http://localhost:8080/v1/projection-sessions/projection-SESSION_ID
curl -X DELETE http://localhost:8080/v1/projection-sessions/projection-SESSION_ID
```

该字段表示本服务的设备占用，包含正在启动的会话，以及失败后尚未清理成功的会话；它不代表电视一定正在播放，也不反映其他应用发起的投屏。读取 Target 不会查询设备；查询 Session 或每 30 秒的后台扫描会刷新状态并释放占用。电视自行停止后，下一次刷新会把会话标记为 `stopped` 并释放占用。服务重启后不保留该字段对应的会话。

Client 根据返回的 `target.id` 和 `endpoint.id` 创建会话。`source` 中的 AAC 音轨由 Render 或其他媒体源直接提供；Casting 只把 URI 交给电视，不读取、解码或转码音频：

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

状态查询失败时会话保持原状态并返回 `502`。如果失败持续超过宽限期，服务把会话标记为 `failed`、释放设备占用，之后查询返回 `200`，`error.code` 为 `device_unreachable`；设备重新响应会清零失败计数。

停止是幂等的，重复停止旧会话不会停止新会话。停止只控制电视，不关闭 Client 的媒体服务。停止持续失败超过宽限期时，服务同样释放占用，把会话标记为 `stopped`，并带上 `error.code = stop_unconfirmed`，表示设备从未确认停止；重复停止会保留这个结果。

| 状态码 | 含义 |
| --- | --- |
| `400` | 请求字段、JSON 或 URL 无效 |
| `404` | Target、Endpoint 或 Session 不存在 |
| `409` | Target 离线或已有活动 Session |
| `413` | 请求体超过 64 KiB |
| `415` | 请求 Content-Type 不是 application/json |
| `422` | 不支持请求的流格式、编码或协议 |
| `502` | 设备控制或状态查询失败 |

Session 的 `error.code`：

| code | 含义 |
| --- | --- |
| `device_control_failed` | 创建会话时设备拒绝或无法执行播放 |
| `device_unreachable` | 状态查询持续失败超过宽限期，服务已释放占用 |
| `stop_unconfirmed` | 停止持续失败超过宽限期，已释放占用但设备未确认停止 |

创建失败时，如果已分配会话，错误响应包含 `error.session_id`，可用于查询或重试停止。服务会尝试清理未完成的投屏；如果清理也失败，会保留设备占用，并在后续扫描中继续重试停止，避免新会话覆盖不确定的播放状态。超过宽限期仍未清理成功时，服务释放占用，避免电视永久停留在“被占用”状态。

## 设备失联与占用释放

每次扫描后，服务会检查所有会话：对没有被查询的会话刷新状态，对启动失败但尚未清理的会话重试停止。状态查询或停止连续失败时，服务记录失败起点；连续失败超过宽限期后：

- 状态查询路径把会话标记为 `failed`，`error.code = device_unreachable`，并释放设备占用。
- 停止路径把会话标记为 `stopped`，`error.code = stop_unconfirmed`，并释放设备占用。

设备重新响应会清除失败计数。设备最后一次确认状态为 `playing` 时宽限期翻倍，避免短暂的网络抖动中断正在观看的节目。`-unreachable-grace` 为负值时关闭自动释放，此时需要设备恢复后手动停止才能释放占用。重复停止已释放的旧会话不会再次控制设备，也不会影响后来创建的新会话。

## 当前边界

- 只注册 `dlna-renderer`；AirPlay 是架构中的后续扩展，不会作为可用 Endpoint 返回。
- `capabilities` 表示当前 Adapter 开放的 MPEG-TS/H.264/AAC 配置范围，不枚举电视的全部硬件解码能力。具体设备能否播放以设备反馈为准。
- `streaming` 表示可边生成边播放，不保证电视缓冲延迟。
- 设备和会话保存在内存中。服务重启不恢复之前的会话，也不会自动停止电视上已经开始的播放。
- 状态在查询 Session 时刷新，并由每次设备扫描后的后台 sweep 兜底；没有后台推送、数据库或自动重连。
- 自动释放依赖连续失败判断，是一种保守估计。设备最后一次确认 `playing` 时默认等待 60 秒，因此电视立即断电到占用释放之间会有一段时间。
- 仅在可信局域网使用，当前没有用户鉴权。

## 测试

```sh
go test ./... -race -coverprofile=coverage.out
go vet ./...
go tool cover -func=coverage.out
```

测试使用 `httptest` HTTP 服务和 `127.0.0.1` 临时 UDP 端口，覆盖 SSDP 报文、设备描述、SOAP 操作、媒体 URL 与元数据转义、错误响应、会话状态、取消、设备过期及并发占用。集成测试中的模拟电视会拉取临时媒体地址，验证 API 到设备的播放/停止流程。

测试还覆盖设备连续失败的宽限期、`playing` 双倍宽限、失败计数随恢复或间隔重置、停止未确认后的占用释放、后台 sweep、取消探针不计失败，以及设备换地址后会话仍使用最新控制地址。

测试还覆盖 CLI 参数与退出码、网卡读取失败、扫描部分成功、取消时丢弃过期结果、UDP 绑定/收发错误、HTTP 响应截断，以及优雅关机超时后的强制关闭。强制关闭测试会实际等待约 5 秒的关闭期限。

`go test` 不探测实际局域网、不访问固定 IP 或外网、不需要电视、浏览器或 FFmpeg。真实电视上的联调独立进行：启动本机媒体流，运行 Casting，再按上述 API 选择真实 Target 创建、查询和停止会话。
