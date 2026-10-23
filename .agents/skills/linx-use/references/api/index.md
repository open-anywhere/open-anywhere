<!-- 来源：https://linx.qiniu.com/docs/xrobot/api | 由 sync-docs.py 自动生成，请勿手改 -->

# 平台API

## API基本信息

**Base URL**: `https://xrobo.qiniu.com`

**认证方式**: Bearer Token/API-KEY

1. 所有API接口都需要有效的认证令牌
2. 开放API推荐使用 API-KEY，认证格式为 `Bearer <api_key>` `<api_key>` 在[灵矽AI控制台](https://xrobo.qiniu.com/)创建。

### 通用响应格式

所有API都遵循统一的响应格式：

```
{
  "code": 0,           // 0表示成功，其他值表示失败
  "msg": "",           // 消息内容
  "data": {}           // 响应数据，根据具体API而定
}
```

### 常见错误码

- `0` : 操作成功
- `401` : 未登录或认证令牌无效/过期

当API调用失败时，响应中的 `code` 字段将不为0，`msg` 字段会包含具体的错误信息。请根据错误信息进行相应的处理。

## 目录

### 平台API

1. [智能体API](agent.md)
2. [大语言模型API](llm.md)
3. [知识库API](knowledge.md)
4. [聊天记录API](chat-history.md)
5. [设备API](device.md)
6. [设备工具描述API](device-tool.md)
7. [音色克隆API](voice-clone.md)
8. [长期记忆API](longterm-memory.md)
9. [固件管理API](firmware.md)
10. [声纹识别V2 API](voiceprint-v2.md)
11. [获取语言列表API](language.md)
12. [其他API](others.md)
