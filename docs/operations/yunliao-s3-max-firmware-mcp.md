# 小智云聊 S3 MAX 接入投屏：固件内置 `self.cast.*` 设备工具

> 本文描述固件侧的投屏实现：设备注册一组 `self.cast.*` MCP 工具，把云端启动的应用画面投到现场局域网里的电视上。应用由控制面构建和运行，设备只负责屏幕发现与投放。
>
> 代码在固件仓库 `LUPENGHAN/xiaozhi-esp32`（fork 自 `78/xiaozhi-esp32`，基于 `addb1ed`）的分支 `feat/casting-mcp`，路径 `main/casting/`。截至本文更新，已在 ESP-IDF v6.1 上**编译通过**，**尚未上机验证**，验证清单见最后一节。

## 为什么放在固件里

灵犀云本身是 MCP 客户端，智能体挂载控制面 MCP 后可以直接调用 `create-app`、`start-app`。但只有设备知道现场有哪些屏幕：SSDP 是链路本地组播，云端看不见。把 `self.cast.*` 做进固件后：

1. **屏幕发现留在现场。** 设备与电视同处一个广播域，调用本地 casting 完成协议控制。
2. **能力随硬件走。** 换一个智能体、或用户自己配的智能体，投屏工具仍在，不依赖智能体是否挂载了控制面 MCP。
3. **控制面保持单一职责。** `start-app` 只启动 Render 并返回 `stream_url`，不接触任何投屏服务。

代价是设备端需要一份 casting HTTP 契约的实现，与模拟硬件 `internal/voiceentry/casting` 同构。

## 结构

```
语音请求
  → 灵犀（MCP 客户端）
    → 设备端 MCP Server（main/mcp_server.cc + main/casting/）
      ├─ 控制面 POST /mcp（由灵犀调用，设备不参与）
      └─ self.cast.* → 本地 casting HTTP（局域网）
                        → 电视（DLNA）
                          → 云端 Render 拉流
```

设备端只做两件事：发现屏幕、把 `stream_url` 交给 casting。它不理解应用的业务含义，也不代理媒体。

## 工具

| 设备工具 | 转发到本地 casting | 语义 |
| --- | --- | --- |
| `self.cast.list_screens` | `GET /v1/targets` | 列出电视及其状态，结果缓存在设备上，供 `cast` 解析屏幕 |
| `self.cast.cast` | `POST /v1/projection-sessions` | `url` 是控制面 `start-app` 返回的 `stream_url`，`target_id` 用 `list_screens` 报出的屏幕名或 id |
| `self.cast.stop` | `DELETE /v1/projection-sessions/{id}` | 不传 `session_id` 时停止本设备最近一次投屏 |

说话用的屏幕名和投屏用的 id 不是一回事：`list_screens` 只报名字和状态（模型和用户说的都是名字），请求里要用发现结果的 id。所以 `cast` 拿到 `target_id` 后按 id、再按名字（精确匹配）查缓存，命中唯一一块就用它；名字撞车时让模型重新 `list_screens` 确认，不会瞎投。缓存没命中才 `GET /v1/targets/{id}` 刷新；目标没有任何支持 `streaming + mpegts + h264 + aac` 的 endpoint 时直接拒绝，不发请求。

控制面侧 `internal/voiceentry/casting` 的实现与固件一致：同样按 id → 唯一名字 → 远端查询的顺序解析。

工具名沿用仓库既有约定 `self.<域>.<动作>`。灵犀侧工具有数量上限，超限会静默丢弃；固件启动日志会打印工具总数与 `casting tools registered` 一行。

## 请求与错误

创建会话的正文与 `pkg/casting` 的 `CreateSessionRequest` 一致：

```json
{
  "target_id": "uuid-living-room",
  "endpoint_id": "dlna",
  "source": {
    "type": "stream",
    "url": "https://render.example.com/streams/inst_01J/live.ts",
    "format": "mpegts",
    "video_codec": "h264",
    "audio_codec": "aac"
  }
}
```

casting 的错误码被翻成可以直接说出口的话：

| 错误码 | 对用户的话 |
| --- | --- |
| `target_busy` | 这块电视正在播放别的内容，先停掉再投 |
| `target_offline` | 这块电视现在离线了 |
| `unsupported_media` | 这块电视不支持这种画面格式 |
| `device_control_failed` | 电视没有响应投屏指令 |
| `invalid_request` | 投屏请求不合法 |
| `not_found`（停止时） | 这块电视已经不在投屏了 |

停止时收到 404 视为已经停好，不再让用户重试。

## 配置

Kconfig（`main/Kconfig.projbuild` 的 `CASTING_CONFIG`）给编译期默认值，NVS 命名空间 `casting` 可运行时覆盖，无需重新烧录：

| Kconfig | NVS key | 说明 |
| --- | --- | --- |
| `CONFIG_CASTING_URL` | `url` | 本地 casting 地址，例如 `http://192.168.1.20:8091`；**为空则不注册任何工具** |

casting **没有鉴权**，设备也不发任何身份头或 token。地址必须是局域网内可信的服务，不要暴露到公网。

## 改动清单

固件仓库 `feat/casting-mcp` 分支：

- `main/casting/cast_client.{h,cc}`：本地 casting HTTP 客户端，走板卡自带的 HTTP 抽象（Wi-Fi 与 ML307 4G 同一条路径），负责错误码翻译。
- `main/casting/cast_tools.{h,cc}`：3 个工具、屏幕缓存、endpoint 选择、最近一次投屏会话。
- `main/application.cc`：`Initialize()` 里注册投屏工具。
- `main/Kconfig.projbuild`、`main/CMakeLists.txt`：配置项与源文件。

与旧的 8 工具方案相比，`CastIdentity`、`X-Linx-*` 三个头、`mcp_token`、`create_and_cast` 的后台构建任务、`protocol.*` 的主动播报入口都已删除。设备不再直接调用控制面 MCP。

## 构建与刷机

```sh
source /path/to/esp-idf/export.sh   # IDF release/v6.1（最低 v6.0.1）
cd xiaozhi-esp32
idf.py set-target esp32s3
idf.py menuconfig                   # CASTING_CONFIG: CASTING_URL
idf.py build flash monitor
```

仓库自带的脚本会一并产出各板卡的合并镜像：

```sh
python scripts/build.py yunliao-s3 --name yunliao-s3 --language zh-CN
```

验证过一次完整构建（2026-10-31，Windows + ESP-IDF `release/v6.1`，目标 esp32s3）：一次通过，无报错；`build/xiaozhi.bin` 为 2702160 字节，`build/merged-binary.bin` 为 10052873 字节。镜像里能查到 `self.cast.list_screens` / `self.cast.cast` / `self.cast.stop`，旧的 8 个工具名已不存在。

## 还没验证的部分

1. **上机。** 编译已通过，但固件没有烧到真机：工具能否出现在灵犀下发的 `tools/list`、设备能否访问到同网段的 casting，都还没有实测。
2. **真实 casting 与电视。** `cast` 的完整路径（DLNA SetAVTransportURI + Play）需要一台能出网的电视才能验证；`playing` 只代表电视接受了 URI，不代表画面已经在显示。
3. **主任务阻塞。** 工具回调在设备主任务上执行，`cast` 会同步等待 casting 的响应（创建会话超时 12 秒，列表和停止 6 秒）。局域网内通常不到 1 秒，但慢速网络下会短暂阻塞音频处理，上机时需要观察。
4. **工具数量上限。** 设备原有工具加 3 个新工具，若灵犀侧超限会丢工具；需要看真实 `tools/list` 结果。
5. **模型是否先确认屏幕再投放。** 工具描述里要求"投屏前先向用户确认要投到哪块屏幕"，约束力取决于模型。

## 与模拟硬件的关系

`internal/voiceentry/casting`（模拟硬件，`-casting-url` 开关，默认关闭）与固件是同一套设计：3 个工具、只连本地 casting、不发身份头。模拟硬件是固件行为的参照实现，两者的用户可见话术保持一致。
