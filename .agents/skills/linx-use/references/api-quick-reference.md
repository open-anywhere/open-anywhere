<!-- 由 sync-docs.py 自动生成，请勿手改 -->

# 平台 API 速查

共 34 个端点。Base URL `https://xrobo.qiniu.com`，认证头 `Authorization: Bearer <token|api_key>`。

| 方法 | 路径 | 说明 | 详情 |
| --- | --- | --- | --- |
| `GET` | `https://xrobo.qiniu.com/xiaozhi/agent/list` | 获取用户智能体列表（支持分页） | [agent](api/agent.md) |
| `GET` | `https://xrobo.qiniu.com/xiaozhi/agent/search` | 搜索智能体 | [agent](api/agent.md) |
| `POST` | `https://xrobo.qiniu.com/xiaozhi/agent` | 创建智能体 | [agent](api/agent.md) |
| `PUT` | `https://xrobo.qiniu.com/xiaozhi/agent/{id}` | 更新智能体 | [agent](api/agent.md) |
| `DELETE` | `https://xrobo.qiniu.com/xiaozhi/agent/{id}` | 删除智能体 | [agent](api/agent.md) |
| `PUT` | `https://xrobo.qiniu.com/v1/devices/{mac_address}/agent/{agent_id}` | 切换设备关联智能体 | [agent](api/agent.md) |
| `GET` | `https://xrobo.qiniu.com/xiaozhi/agent/{id}` | 获取智能体详情 | [agent](api/agent.md) |
| `GET` | `https://xrobo.qiniu.com/xiaozhi/agent/{agentId}/sessions` | 获取聊天记录列表 | [chat-history](api/chat-history.md) |
| `GET` | `https://xrobo.qiniu.com/xiaozhi/agent/{agentId}/chat-history/{sessionId}` | 获取聊天记录详情 | [chat-history](api/chat-history.md) |
| `POST` | `https://xrobo.qiniu.com/v1/objects/signed-url` | 生成音频签名 URL | [chat-history](api/chat-history.md) |
| `DELETE` | `https://xrobo.qiniu.com/v1/devices/{mac_address}/chat-history` | 删除设备聊天历史 | [chat-history](api/chat-history.md) |
| `GET` | `https://xrobo.qiniu.com/xiaozhi/device/bind/{agentId}` | 获取已绑定设备 | [device](api/device.md) |
| `POST` | `https://xrobo.qiniu.com/xiaozhi/device/unbind` | 解绑设备 | [device](api/device.md) |
| `POST` | `https://xrobo.qiniu.com/xiaozhi/device/bind/{agentId}/{deviceCode}` | 绑定设备 | [device](api/device.md) |
| `POST` | `https://xrobo.qiniu.com/xiaozhi/device/preregister` | 批量导入设备（通过MAC地址绑定） | [device](api/device.md) |
| `GET` | `https://xrobo.qiniu.com/v1/pre-register/agents/{agent_id}/devices` | 查询预注册设备列表 | [device](api/device.md) |
| `GET` | `https://xrobo.qiniu.com/v1/pre-register/devices/mac-address/{mac_address}` | 通过 MAC 地址查询预注册设备 | [device](api/device.md) |
| `DELETE` | `https://xrobo.qiniu.com/v1/pre-register/devices/mac-address/{mac_address}` | 删除预注册设备 | [device](api/device.md) |
| `GET` | `https://xrobo.qiniu.com/v1/device/tools` | 查询设备工具描述 | [device-tool](api/device-tool.md) |
| `POST` | `https://xrobo.qiniu.com/v1/device/tools` | 新增设备工具描述 | [device-tool](api/device-tool.md) |
| `PUT` | `https://xrobo.qiniu.com/v1/device/tools/{id}` | 更新设备工具描述 | [device-tool](api/device-tool.md) |
| `DELETE` | `https://xrobo.qiniu.com/v1/device/tools/{id}` | 删除设备工具描述 | [device-tool](api/device-tool.md) |
| `GET` | `https://xrobo.qiniu.com/v1/voices` | 获取语音列表 | [others](api/others.md) |
| `PUT` | `https://xrobo.qiniu.com/v1/voices/{voice_id}/tags` | 更新音色标签 | [others](api/others.md) |
| `POST` | `https://xrobo.qiniu.com/v1/users` | 用户注册 | [user](api/user.md) |
| `GET` | `https://xrobo.qiniu.com/v1/auth/captcha` | 获取图形验证码 | [user](api/user.md) |
| `POST` | `https://xrobo.qiniu.com/v1/auth/login` | 用户登录 | [user](api/user.md) |
| `POST` | `https://xrobo.qiniu.com/v1/voice-clones` | 创建音色栏位 | [voice-clone](api/voice-clone.md) |
| `PUT` | `https://xrobo.qiniu.com/v1/voice-clones/{id}` | 训练音色 | [voice-clone](api/voice-clone.md) |
| `GET` | `https://xrobo.qiniu.com/v1/voice-clones/{id}` | 获取音色信息 | [voice-clone](api/voice-clone.md) |
| `GET` | `https://xrobo.qiniu.com/v1/voice-clones` | 获取音色列表 | [voice-clone](api/voice-clone.md) |
| `GET` | `https://xrobo.qiniu.com/v1/voice-clones/quota` | 查询音色复刻额度 | [voice-clone](api/voice-clone.md) |
| `POST` | `https://xrobo.qiniu.com/v1/voice-clones/{id}/renewals` | 续期音色 | [voice-clone](api/voice-clone.md) |
| `DELETE` | `https://xrobo.qiniu.com/v1/voice-clones/{id}` | 删除音色 | [voice-clone](api/voice-clone.md) |
