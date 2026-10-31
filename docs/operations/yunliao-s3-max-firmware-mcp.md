# 小智云聊 S3 MAX 接入投屏：固件内置 `self.cast.*` 设备工具

> 本文描述固件侧的投屏实现：设备自己用 SSDP 发现局域网里的 DLNA 电视，自己用 SOAP 把云端 `stream_url` 交给电视播放。**固件不依赖任何本地投屏服务**；仓库里同协议的 Go 实现只供设备运行时在本地模拟与调试时使用。
>
> 代码在固件仓库 `LUPENGHAN/xiaozhi-esp32`（fork 自 `78/xiaozhi-esp32`，基于 `addb1ed`）的分支 `feat/casting-mcp`，路径 `main/casting/`。已在 ESP-IDF v6.1 上编译通过，并在真机（yunliao-s3，ESP32-S3）上完成发现、投放、停止的联调，验证记录见最后一节。

## 为什么放在固件里

灵犀云本身是 MCP 客户端，智能体挂载控制面 MCP 后可以直接调用 `create-app`、`start-app`。但只有设备知道现场有哪些屏幕：SSDP 是链路本地组播，云端看不见。把发现和 DLNA 控制都做进固件后：

1. **屏幕发现留在现场。** 设备与电视同处一个广播域，自己发 M-SEARCH、自己解析描述、自己发 SOAP。
2. **现场不需要再部署服务。** 没有局域网机器、没有端口、没有地址配置；设备连上 Wi-Fi 就能投。
3. **能力随硬件走。** 换一个智能体、或用户自己配的智能体，投屏工具仍在，不依赖智能体是否挂载了控制面 MCP。
4. **控制面保持单一职责。** `start-app` 只启动 Render 并返回 `stream_url`，不建立投屏会话；控制面不提供屏幕查询。

## 结构

```
语音请求
  → 灵犀（MCP 客户端）
    → 设备端 MCP Server（main/mcp_server.cc + main/casting/）
      ├─ 控制面 POST /mcp（由灵犀调用，设备不参与）
      └─ self.cast.*
           ├─ 后台任务每 60 秒 SSDP 搜索（239.255.255.250:1900）
           ├─ 解析 description.xml，缓存电视名 / UDN / AVTransport controlURL
           └─ cast → SOAP SetAVTransportURI + Play（电视自己向云端拉流）
```

设备端只做两件事：发现屏幕、把 `stream_url` 交给电视。它不理解应用的业务含义，也不代理媒体。

## 工具

| 设备工具 | 参数 | 语义 |
| --- | --- | --- |
| `self.cast.list_screens` | 无 | 列出缓存的电视及状态（空闲 / 正在播放 / 离线）。稳态下不访问网络 |
| `self.cast.cast` | `url`、`target_id` | `url` 是控制面 `start-app` 返回的 `stream_url`；`target_id` 来自 `list_screens` |
| `self.cast.stop` | `session_id`（可选） | 不传时停止本设备最近一次投屏 |

屏幕 id 是 `dlna-<UDN 去掉 uuid: 前缀>`，对用户不可读；`ListScreens` 只报电视名和状态。所以 `cast` 拿到 `target_id` 后按 id、再按名字（精确匹配）查缓存：

- 命中唯一一块 → 直接投。
- 名字撞车 → 返回"附近有好几块叫这个名字的电视，先调用 `self.cast.list_screens` 确认是哪一块"。
- 缓存没命中（模型没先列屏、或电视刚开机）→ 现场同步扫描一次（约 2.5 秒）再解析，仍找不到才报错。

工具名沿用仓库既有约定 `self.<域>.<动作>`。灵犀侧工具有数量上限，超限会静默丢弃；固件启动日志会打印工具总数与 `casting tools registered` 一行。

## 发现与缓存

- 联网后后台任务立即扫描一次，之后每 `CONFIG_CASTING_SCAN_INTERVAL_SEC`（默认 60 秒）扫描一次；IP 变化时立刻重扫。
- 扫描只在扫描任务里做，结果一次性合并进缓存，工具回调只读缓存；两把互斥锁把网络 I/O 和共享状态隔开。
- 连续 3 次扫描没看到、或 SSDP `max-age` 过期，标为「离线」；连续 6 次没看到才从缓存删除。正在投屏的那台不删，停止时还需要它的 `controlURL`。
- 未联网时睡 5 秒轮询，只在状态变化时打日志，不刷屏。4G 模式下 casting 空转，`list_screens` 返回「现在没有网络」。

## 请求与错误

`cast` 先发 `SetAVTransportURI`（含 DIDL-Lite 元数据），成功后才发 `Play`。DIDL 里的 URL 先做一次 XML 转义再嵌进元数据，元数据整体再转义一次，与 Go 服务的行为一致。

UPnP Fault 的 `errorCode` 被翻成可以直接说出口的话：

| UPnP 错误码 | 对用户的话 |
| --- | --- |
| 714 | 这块电视不支持这种画面格式 |
| 715 | 这块电视正在播放别的内容，先停掉再投 |
| 716 | 电视找不到这个视频地址 |
| 718 | 电视拒绝了投屏请求 |
| 701（停止时） | 视为已经停止，不报错 |
| 其他 / 传输层失败 | 电视没有响应投屏指令 |

`stop` 只记录本设备最近一次会话；不传 `session_id` 就停它，传了别的 id 返回「这块电视已经不在投屏了」。投到另一块屏时，设备等新屏接受流之后会停止旧屏，不会留下两个画面同时播放。

## 配置

只有一个编译期配置，没有 NVS 覆盖，也没有服务地址：

| Kconfig | 说明 |
| --- | --- |
| `CONFIG_CASTING_SCAN_INTERVAL_SEC` | 后台扫描间隔，默认 60，范围 10–600 |

casting 链路没有鉴权，但也不再监听任何端口：设备只作为客户端访问局域网电视，不暴露投屏控制面。电视必须与设备在同一广播域。

设备直接对电视发 SSDP 与 DLNA 指令，不调用控制面 MCP，也不需要 `X-Linx-*` 身份头、`mcp_token` 或本地 casting 地址。

## 构建与刷机

```sh
source /path/to/esp-idf/export.sh   # IDF release/v6.1（最低 v6.0.1）
cd xiaozhi-esp32
idf.py set-target esp32s3
idf.py menuconfig                   # CASTING_CONFIG: CASTING_SCAN_INTERVAL_SEC
idf.py build flash monitor
```

仓库自带的脚本会一并产出各板卡的合并镜像：

```sh
python scripts/build.py yunliao-s3 --name yunliao-s3 --language zh-CN
```

## 验证记录（2026-10-31，真机 + 局域网）

环境：yunliao-s3（MAC `e8:f6:0a:8b:f4:e4`，WLAN `192.168.8.153`），开发机 `192.168.8.155`；用一个 Python「假电视」应答 M-SEARCH 并提供 description / control / 710 KB 的 `test.ts`，另有一台本机 Macast 作为真实 DLNA 渲染器。

1. **发现。** 设备日志每 60 秒出现 `M-SEARCH via 192.168.8.153 found 2 renderer(s)`，随后打印两台电视的名字与 `controlURL`，扫描耗时 2.6–4.6 秒。假电视的 SSDP 日志里能看到源地址为 `192.168.8.153` 的 MediaRenderer 搜索。
2. **投放。** `SetAVTransportURI` 与 `Play` 都返回 200；假电视收到的 `CurrentURI` 是 `...?a=1&amp;b=2`，元数据里是 `&amp;amp;`，双重转义正确。
3. **真实播放。** 目标是 Macast 时，Macast 立刻用 Range 请求从假电视的 HTTP 服务拉取 `test.ts` 并播放；说明地址和元数据都被正确接受，不只是"SOAP 返回 200"。
4. **停止。** 20 秒后 `Stop` 返回 200；假电视与 Macast 都回到空闲。
5. **内存。** 连续多轮扫描后 `heap≈4.20 MB` 基本不变，扫描任务没有泄漏迹象。

尚未验证：灵犀云下发的真实 `tools/list` 里是否包含全部 3 个工具（受灵犀工具数量上限影响）、语音说"投到客厅电视"的完整链路、真实商用电视的兼容性、断网/换 IP 时的表现。

## 与模拟硬件的关系

- `internal/voiceentry/casting`（Gateway 的 `-casting-interface` 开关）在进程内直接发 SSDP 与 DLNA，不再有独立的 Casting 服务。
- 固件与模拟硬件共用同一套 DLNA 行为：固件在 ESP32 上实现，模拟硬件在 `internal/casting/dlna` 里实现；两者都不经过控制面。
