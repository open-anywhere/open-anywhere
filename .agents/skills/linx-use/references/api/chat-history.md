<!-- 来源：https://linx.qiniu.com/docs/xrobot/api/chat-history | 由 sync-docs.py 自动生成，请勿手改 -->

# 聊天记录 API

### 获取聊天记录列表

获取指定智能体的聊天会话列表，支持分页查询

**`GET`** `https://xrobo.qiniu.com/xiaozhi/agent/{agentId}/sessions`

#### 基本信息

- **Host**: `https://xrobo.qiniu.com`
- **Base Path**: `/xiaozhi`
- **Method**: `GET`
- **返回类型**: `application/json`

#### 请求参数

| 参数名 | 类型 | 必填 | 位置 | 说明 |
| --- | --- | --- | --- | --- |
| `agentId` | string | 是 | path | 智能体ID |
| `page` | integer | 否 | query | 页码，从1开始 |
| `limit` | integer | 否 | query | 每页记录数 |

#### 请求头

| Header名 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Authorization` |  | 是 | 用户认证令牌，格式为 Bearer + 空格 + token |

#### 请求示例

```
GET /xiaozhi/agent/09689edfb5a74846ad8f2a6512c26a73/sessions?page=1&limit=20 HTTP/1.1
Host: https://xrobo.qiniu.com
Authorization: Bearer <token>
```

#### 响应示例

```
{
  "code": 0,
  "msg": "success",
  "data": {
    "total": 139,
    "list": [
      {
        "sessionId": "7465966b-4582-4dae-99be-420364d422d7",
        "createdAt": "2025-08-28 16:02:49",
        "chatCount": 75
      },
      {
        "sessionId": "9eab0c2b-79c0-402c-a695-09d802bd977a",
        "createdAt": "2025-08-28 12:25:01",
        "chatCount": 3
      },
      ...(limit=20, 共20条)
    ]
  }
}
```

#### 状态码

- `0` OK - 成功获取聊天记录列表
- `401` Unauthorized - 未登录或token无效

> **INFO**
>
> 响应中的list包含会话的基本信息，包括sessionId、创建时间和聊天数量。total表示总记录数。

### 获取聊天记录详情

获取指定智能体和会话的详细聊天记录

**`GET`** `https://xrobo.qiniu.com/xiaozhi/agent/{agentId}/chat-history/{sessionId}`

#### 基本信息

- **Host**: `https://xrobo.qiniu.com`
- **Base Path**: `/xiaozhi`
- **Method**: `GET`
- **返回类型**: `application/json`

#### 请求参数

| 参数名 | 类型 | 必填 | 位置 | 说明 |
| --- | --- | --- | --- | --- |
| `agentId` | string | 是 | path | 智能体ID |
| `sessionId` | string | 是 | path | 会话ID |

#### 请求头

| Header名 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Authorization` |  | 是 | 用户认证令牌，格式为 Bearer + 空格 + token |

#### 请求示例

```
GET /xiaozhi/agent/09689edfb5a74846ad8f2a6512c26a73/chat-history/7465966b-4582-4dae-99be-420364d422d7 HTTP/1.1
Host: https://xrobo.qiniu.com
Authorization: Bearer <token>
```

#### 响应示例

```
{
  "code": 0,
  "msg": "success",
  "data": [
    {
      "createdAt": "2026-07-28 11:57:26",
      "chatType": 1,
      "content": "你好，请问我是谁？",
      "audioId": null,
      "audioUrl": "https://xrobot-obj.qnlinx.com/chat-history/2026/07/28/audio.wav",
      "macAddress": "09689edfb5a74846ad8f2a6512c26a73",
      "toolInfo": "",
      "toolDuration": 0,
      "clientListenMode": "auto"
    },
    {
      "createdAt": "2026-07-28 11:57:27",
      "chatType": 2,
      "content": "哦？这不是那个整天围着电子屏幕打转的人吗？",
      "audioId": null,
      "audioUrl": "https://xrobot-obj.qnlinx.com/chat-history/2026/07/28/response.wav",
      "macAddress": "09689edfb5a74846ad8f2a6512c26a73",
      "toolInfo": "",
      "toolDuration": 0,
      "clientListenMode": "auto"
    },
    ...
  ]
}
```

#### 状态码

- `0` OK - 成功获取聊天记录列表
- `401` Unauthorized - 未登录或token无效

> **INFO**
>
> 响应中的data是一个聊天消息数组，按时间顺序排列，每条消息包含：
>
> - `createdAt` ：创建时间
> - `chatType` ：消息类型（1=用户，2=AI，3=工具调用）
> - `content` ：消息内容
> - `audioId` ：音频ID（已废弃，始终为null）
> - `audioUrl` ：音频文件地址，需要签名后才能访问
> - `macAddress` ：设备MAC地址
> - `toolInfo` ：工具调用信息
> - `toolDuration` ：工具执行耗时（毫秒）
> - `clientListenMode` ：客户端聆听模式（realtime/auto）

### 生成音频签名 URL

为聊天记录中的音频文件生成签名 URL，支持直接播放或下载

**`POST`** `https://xrobo.qiniu.com/v1/objects/signed-url`

#### 基本信息

- **Host**: `https://xrobo.qiniu.com`
- **Base Path**: `/v1`
- **Method**: `POST`
- **返回类型**: `application/json`

#### 请求参数

| 参数名 | 类型 | 必填 | 位置 | 说明 |
| --- | --- | --- | --- | --- |
| `url` | string | 是 | body | 需要签名的音频 URL（从聊天记录详情中获取的 audioUrl） |

#### 请求头

| Header名 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Authorization` |  | 是 | 用户认证令牌，格式为 Bearer + 空格 + token |

#### 请求示例

```
POST /v1/objects/signed-url HTTP/1.1
Host: https://xrobo.qiniu.com
Authorization: Bearer <token>
Content-Type: application/json

{
  "url": "https://xrobot-obj.qnlinx.com/chat-history/2026/07/28/audio.wav"
}
```

#### 响应示例

```
{
  "code": 0,
  "msg": "success",
  "data": {
    "signed_url": "https://xrobot-obj.qnlinx.com/chat-history/2026/07/28/audio.wav?e=<Unix时间戳>&token=<签名token>"
  }
}
```

#### 状态码

- `0` OK - 操作成功
- `401` Unauthorized - 未登录或token无效

> **注意事项**
>
> - **需认证** ：此接口需要用户登录 token
> - **签名有效期** ：1 小时（ `e` 参数为 Unix 时间戳）
> - **自动兼容** ：旧 bucket URL 会直接返回原 URL，无需二次处理

### 删除设备聊天历史

**Base URL：** `https://xrobo.qiniu.com/v1`

```
Authorization: Bearer <用户登录 Token 或 API Key>
```

响应 HTTP 状态码当前统一为 `200 OK`；请以响应体的 `code` 判断业务是否成功。`code = 0` 表示成功，非 `0` 表示失败。

删除指定设备的聊天历史。可通过 `agent_id` 限定删除某一个智能体的历史；不传时删除该设备下所有智能体的历史记录。

> **INFO**
>
> 该操作只删除数据库聊天记录；已上传的 OSS 音频文件仍按既有生命周期策略自动清理（当前为 60 天），不会立即删除。

### 删除设备聊天历史

删除指定设备的聊天历史；可选按智能体 ID 限定删除范围

**`DELETE`** `https://xrobo.qiniu.com/v1/devices/{mac_address}/chat-history`

#### 基本信息

- **Host**: `https://xrobo.qiniu.com`
- **Base Path**: `/v1`
- **Method**: `DELETE`
- **返回类型**: `application/json`

#### 请求参数

| 参数名 | 类型 | 必填 | 位置 | 说明 |
| --- | --- | --- | --- | --- |
| `mac_address` | string | 是 | path | 设备 MAC 地址，格式必须为 00:1A:2B:3C:4D:5E，大小写均可 |
| `agent_id` | string | 否 | query | 指定时仅删除该设备关联此智能体的聊天历史；省略时删除该设备的全部聊天历史 |

#### 请求头

| Header名 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Authorization` |  | 是 | 用户认证凭证 |

#### 请求示例

```
DELETE /v1/devices/AB:CA:A9:60:D8:48/chat-history?agent_id=03fe2c47ec8c47a28c7f382a47b4f838 HTTP/1.1
Host: xrobo.qiniu.com
Authorization: Bearer <用户登录 Token 或 API Key>
```

#### 响应示例

```
{
  "code": 0,
  "reqid": "request-id",
  "data": {
    "deleted_count": 3
  }
}
```

#### 状态码

- `0` 成功
- `400` MAC 地址格式不合法
- `401` 未携带认证凭证或认证凭证无效
- `403` Token 已过期或当前用户不是设备拥有者
- `404` 设备及当前用户的预注册设备记录均不存在
- `599` 服务端数据库等内部异常

| 响应字段 | 类型 | 说明 |
| --- | --- | --- |
| `deleted_count` | integer | 实际删除的聊天记录数量；没有匹配记录时为 `0`，仍视为成功。 |

#### 删除指定智能体的历史

```
curl -X DELETE 'https://xrobo.qiniu.com/v1/devices/AB:CA:A9:60:D8:48/chat-history?agent_id=03fe2c47ec8c47a28c7f382a47b4f838' \
  -H 'Authorization: Bearer <TOKEN>'
```

#### 删除该设备全部历史

```
curl -X DELETE 'https://xrobo.qiniu.com/v1/devices/AB:CA:A9:60:D8:48/chat-history' \
  -H 'Authorization: Bearer <TOKEN>'
```

#### 常见失败响应

```
{
  "code": 400,
  "msg": "invalid mac address. format: 1a:2b:3c:4d:5e:6f",
  "reqid": "request-id",
  "data": null
}
```

| `code` | `msg` | 场景 |
| --- | --- | --- |
| `400` | `invalid mac address. format: 1a:2b:3c:4d:5e:6f` | MAC 地址格式不合法。 |
| `401` | `authorization header required` / `invalid token` / `get token failed` | 未携带、格式错误或无效的认证凭证。 |
| `403` | `token is expired` | 登录 Token 已过期。 |
| `403` | `permission denied` | 当前用户不是该已注册设备的拥有者。 |
| `404` | `device not found` | 设备及当前用户的预注册设备记录均不存在。 |
| `599` | 具体错误信息 | 服务端数据库等内部异常。 |
