---
name: linx-use
description: 灵矽（Linx / Xrobot / xrobo.qiniu.com）AI 智能硬件平台使用手册。当任务是接入或调用灵矽平台（创建/配置智能体、绑定设备、调用开放 API、知识库、长期记忆、音色克隆、MCP、固件 OTA、Wi-Fi/BluFi 配网、WebSocket 设备协议、小智硬件接入、写智能体提示词）时使用。关键词：灵矽、linx、xrobot、xrobo、智控台、小智、MCP、声纹。
---

# 灵矽（Linx）平台使用

灵矽是七牛云的 AI 智能硬件平台：在云端创建「智能体」（人设 + 大模型 + 音色 + 技能），把「设备」绑定到智能体，设备通过 WebSocket 与云端做语音对话，并可调用知识库、长期记忆、MCP 工具和端侧能力。

本 skill 的 `references/` 是官方文档站 <https://linx.qiniu.com/docs/xrobot/> 的完整离线镜像（43 篇，由脚本抓取转换），**回答具体问题前先读对应参考文档，不要凭记忆编接口字段**。

## 平台速览

| 项目 | 值 |
| --- | --- |
| 智控台（控制台） | <https://xrobo.qiniu.com/> |
| 开放 API Base URL | `https://xrobo.qiniu.com` |
| API 认证 | `Authorization: Bearer <api_key>`，API Key 在智控台「API Key 管理」创建 |
| 通用响应 | `{ "code": 0, "msg": "", "data": {} }`，`code != 0` 即失败；`401` 为未登录/令牌失效 |
| 设备 WebSocket | `wss://xrobo-io.qiniuapi.com/v1/ws/`（也可由 OTA 接口动态下发） |
| 文档源仓库 | <https://github.com/qiniu/Xrobot-docs> |

## 核心概念

- **智能体（agent）**：一整套人设与能力配置。包含角色提示词（systemPrompt）、LLM/视觉模型、TTS 音色、意图识别模式、知识库、长期记忆、MCP 插件、声纹等。一个智能体可绑定多台设备。
- **设备（device）**：通过验证码/MAC 绑定到智能体；上电后走 OTA → WebSocket 流程进入待命。
- **意图识别**：决定设备语音指令能否触发工具调用。查天气、报时、调用 MCP 必须选支持 Function Call 的模式。
- **知识库**：为智能体注入领域文档，按语义检索（`/v1/datasets` 系列接口）。
- **长期记忆**：跨会话提取事实与偏好；按「记忆容器 → 变量 → 值/片段」组织。
- **MCP**：端侧 MCP（设备本机能力，走设备协议）与云端 MCP（平台托管服务），在智能体里勾选启用。
- **音色克隆 / 声纹 V2**：自定义 TTS 音色；声纹用于识别说话人并绑定到智能体。

## 任务路由：先读哪一篇

| 你要做的事 | 先读 |
| --- | --- |
| 从头跑通一个智能体 + 设备 | `references/guide/quick-start.md` |
| 配置角色、模型、音色、意图识别、记忆、隐私 | `references/guide/agent-config.md` |
| 用开放 API 管理智能体 | `references/api/agent.md`（端点总表见 `references/api-quick-reference.md`） |
| 绑定/解绑/预注册设备 | `references/api/device.md` |
| 聊天记录、音频签名 URL | `references/api/chat-history.md` |
| 知识库增删查与检索 | `references/api/knowledge.md` + `references/guide/knowledge-start.md` |
| 长期记忆容器/变量/片段 | `references/api/longterm-memory.md` + `references/guide/longterm-memory-start.md` |
| 音色克隆、音色列表 | `references/api/voice-clone.md`、`references/api/others.md` |
| 声纹识别与说话人绑定 | `references/api/voiceprint-v2.md` |
| 设备 WebSocket 协议（音频/ASR/TTS/工具调用） | `references/platform/websocket.md` |
| OTA 上报与固件升级 | `references/platform/OTA.md` + `references/api/firmware.md` |
| Wi-Fi / BluFi 蓝牙配网 | `references/platform/wifi-config.md`、`references/platform/blufi-config.md` |
| 设备端切换智能体/音色 | `references/platform/agent-voice-switch.md` |
| 端侧 MCP 协议 | `references/mcp/hardware-mcp.md` |
| 云端 MCP 接入 | `references/mcp/software-mcp.md` + `references/guide/mcp-store.md` |
| 写智能体提示词 | `references/guide/hardware-agent-prompt-guide.md` |
| 小智开源固件/硬件接入 | `references/guide/xiaozhi-firmware.md`、`references/guide/xiaozhi-hardware.md` |
| 第三方应用/小程序接入 | `references/guide/third-app.md`、`references/guide/platform-mp.md` |
| 排障（固件、绑定、配网等） | `references/faq/faq.md` |

## 常见动作

### 调用开放 API

```bash
export LINX_API_KEY="<your api key>"
# 列出智能体
curl -s "https://xrobo.qiniu.com/xiaozhi/agent/list?limit=20" \
  -H "Authorization: Bearer $LINX_API_KEY"
```

- 分页用 `limit` + `cursor`（游标为上一页最后一条 ID）；搜索结果不分页。
- 所有接口的路径、参数、请求/响应示例都在 `references/api/` 下对应文件里。

### 把设备接到智能体

1. 智控台创建智能体 → 配置角色（`references/guide/agent-config.md`）。
2. 在智能体下「添加设备」，输入设备验证码，或调用 `POST /xiaozhi/device/bind/{agentId}/{deviceCode}`。
3. 设备重启 → OTA 上报 → 建 WebSocket → 待命。
4. 网页体验前先结束旧连接再重新开始；LLM/音色弹窗的「确定」只作用于表单，仍需保存整页配置。

### 设备协议要点

- 先读 `references/platform/websocket.md` 的握手与消息帧定义，音频按协议指定的采样率/格式发送。
- 配网失败先看 `references/faq/faq.md` 与 `references/platform/wifi-config.md` 的排查章节。

## 使用约定

- 文档镜像只读：不要直接改 `references/` 下的文件，需要更新时运行同步脚本。
- 引用文档时给出本地路径（如 `references/api/agent.md`）和原始 URL，便于用户核对。
- 接口细节（字段名、必填项、错误码）以 `references/api/` 为准；站点文档更新后跑一次同步即可。

## 更新文档

```bash
python3 .claude/skills/linx-use/scripts/sync-docs.py
```

脚本会重新抓取 <https://linx.qiniu.com/docs/xrobot/> 全部页面、转换 Markdown、重建 `references/api-quick-reference.md` 与 `docs-manifest.json`。依赖 `beautifulsoup4`；`--check` 只比对不写盘，`--only api,guide` 只同步指定章节。
