<!-- 来源：https://linx.qiniu.com/docs/xrobot/api/agent | 由 sync-docs.py 自动生成，请勿手改 -->

# 智能体 API

## 认证说明

所有API接口都需要在请求头中包含有效的认证令牌：

```
Authorization: Bearer <token>
```

获取token方式参见 [平台api概要](index.md)

当认证失败时，请求响应状态码为`200`，但返回以下响应：

```
{
  "code": 401,
  "msg": "未登录",
  "data": []
}
```

## API列表

### 获取用户智能体列表（支持分页）

获取当前用户的智能体列表，支持分页查询。包含智能体的基本信息和配置状态

**`GET`** `https://xrobo.qiniu.com/xiaozhi/agent/list`

#### 基本信息

- **Host**: `https://xrobo.qiniu.com`
- **Base Path**: `/xiaozhi`
- **Method**: `GET`
- **返回类型**: `application/json`

#### 请求参数

| 参数名 | 类型 | 必填 | 位置 | 说明 |
| --- | --- | --- | --- | --- |
| `limit` | integer | 否 | query | 单页数量，默认20，范围1-100 |
| `cursor` | string | 否 | query | 游标（上页最后一条ID，32位小写hex，格式：[a-f0-9]{32}） |

#### 请求头

| Header名 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Authorization` |  | 是 | 用户认证令牌，格式为 Bearer + 空格 + token |

#### 请求示例

```
GET /xiaozhi/agent/list?limit=20&cursor=4f3a8c7e0b6f4b5c9d3d0b8a2a1f0c9d HTTP/1.1
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
      "id": "4f3a8c7e0b6f4b5c9d3d0b8a2a1f0c9d",
      "agentName": "小智助手",
      "assistantName": "助手阿伟",
      "ttsModelName": "",
      "ttsVoiceName": "豪放可爱女",
      "llmModelName": "qwen3极速版",
      "vllmModelName": "智谱视觉AI",
      "memModelId": "Memory_mem_local_short",
      "systemPrompt": "[整体人设指导]\n核心原则:你是一个名为\"{{assistant_name}}\"的AI助手，你的所有输出和行为都......",
      "summaryMemory": null,
      "lastConnectedAt": "2024-03-20 10:00:00",
      "deviceCount": 5,
      "extra": null
    }
  ],
  "nextCursor": "5a1b2c3d4e5f67890123456789abcdef"
}
```

#### 状态码

- `0` OK - 成功获取智能体列表
- `401` Unauthorized - 未登录或token无效

> **分页规则说明**
>
> **默认行为**：
>
> - `limit` 和 `cursor` 都不传：返回全量列表（兼容旧版本）， `nextCursor` 为 `null`
> - 只传 `cursor` ： `limit` 默认为 20
> - `limit <= 0` ：自动修正为 20
> - `limit > 100` ：自动修正为 100
>
> **游标说明**：
>
> - `nextCursor` 为 `null` 表示无更多数据
> - 游标格式为32位小写十六进制字符串（如： `4f3a8c7e0b6f4b5c9d3d0b8a2a1f0c9d` ）

**分页使用示例**

**示例1：获取全量列表（兼容模式）**

```
GET /xiaozhi/agent/list
```

返回全量列表，`nextCursor` 为 `null`

**示例2：首页查询**

```
GET /xiaozhi/agent/list?limit=20
```

获取前20条记录

**示例3：翻页查询**

```
GET /xiaozhi/agent/list?limit=20&cursor=4f3a8c7e0b6f4b5c9d3d0b8a2a1f0c9d
```

从指定游标位置继续获取20条记录

**示例4：参数自动修正**

```
GET /xiaozhi/agent/list?limit=0
# 服务端自动修正为 limit=20

GET /xiaozhi/agent/list?limit=1000
# 服务端自动修正为 limit=100
```

**示例5：无效游标的错误响应**

```
GET /xiaozhi/agent/list?limit=20&cursor=invalid-cursor
```

```
{
  "code": 500,
  "msg": "无效的游标参数",
  "data": null
}
```

**示例6：数据已全部获取**

```
{
  "code": 0,
  "msg": "success",
  "data": [],
  "nextCursor": null
}
```

### 搜索智能体

支持按设备 MAC 地址或智能体 ID 搜索智能体

**`GET`** `https://xrobo.qiniu.com/xiaozhi/agent/search`

#### 基本信息

- **Host**: `https://xrobo.qiniu.com`
- **Base Path**: `/xiaozhi`
- **Method**: `GET`
- **返回类型**: `application/json`

#### 请求参数

| 参数名 | 类型 | 必填 | 位置 | 说明 |
| --- | --- | --- | --- | --- |
| `q` | string | 是 | query | 搜索关键词 |
| `type` | string | 是 | query | 搜索类型，支持 mac（设备 MAC 地址）、agent_id（智能体 ID） |

#### 请求头

| Header名 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Authorization` |  | 是 | 用户认证令牌，格式为 Bearer + 空格 + token |

#### 请求示例

```
// 示例 1：按设备 MAC 地址精确搜索
GET /xiaozhi/agent/search?q=AA:BB:CC:DD:EE:FF&type=mac HTTP/1.1
Host: https://xrobo.qiniu.com
Authorization: Bearer <token>

// 示例 2：按智能体 ID 精确搜索
GET /xiaozhi/agent/search?q=4f3a8c7e0b6f4b5c9d3d0b8a2a1f0c9d&type=agent_id HTTP/1.1
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
      "id": "4f3a8c7e0b6f4b5c9d3d0b8a2a1f0c9d",
      "agentName": "小智助手",
      "assistantName": "助手阿伟",
      "ttsModelName": "",
      "ttsVoiceName": "豪放可爱女",
      "llmModelName": "qwen3极速版",
      "vllmModelName": "智谱视觉AI",
      "memModelId": "Memory_mem_local_short",
      "systemPrompt": "[整体人设指导]\n核心原则:你是一个名为\"{{assistant_name}}\"的AI助手，你的所有输出和行为都......",
      "summaryMemory": null,
      "lastConnectedAt": "2024-03-20 10:00:00",
      "deviceCount": 5,
      "extra": null
    }
  ],
  "nextCursor": null
}
```

#### 状态码

- `0` OK - 成功搜索智能体
- `401` Unauthorized - 未登录或token无效

> **搜索类型与匹配规则**
>
> - **mac（设备 MAC 地址）** ：采用 **精确匹配** ，查询绑定该 MAC 设备且归属于当前用户的智能体
> - **agent_id（智能体 ID）** ：采用 **精确匹配** ，按智能体 ID 查询归属于当前用户的智能体
>
> 📌 **说明**：搜索结果目前为全量返回，响应体中的 `nextCursor` 固定为 `null`，无需处理游标分页。

> **INFO**
>
> 创建智能体时可指定大语言模型和意图模型；未传模型ID时使用默认模板配置，也可在创建后通过更新接口修改

### 创建智能体

创建一个新的智能体，可在创建时指定大语言模型和意图模型。未指定的模型使用默认模板配置，返回data为新智能体的ID，可用于更新、删除等api

**`POST`** `https://xrobo.qiniu.com/xiaozhi/agent`

#### 基本信息

- **Host**: `https://xrobo.qiniu.com`
- **Base Path**: `/xiaozhi`
- **Method**: `POST`
- **返回类型**: `application/json`

#### 请求参数

| 参数名 | 类型 | 必填 | 位置 | 说明 |
| --- | --- | --- | --- | --- |
| `agentName` | string | 是 | body | 智能体名称 |
| `assistantName` | string | 是 | body | 助手昵称 |
| `langCode` | string | 否 | body | 语言编码，不传时使用默认模板。可通过 GET /v1/languages 获取可选语言 |
| `ttsVoiceId` | string | 否 | body | 音色ID，不传时使用系统或默认模板。可通过 GET /v1/voices 获取可选音色 |
| `systemPrompt` | string | 否 | body | 角色设定，不传时使用默认模板配置 |
| `llmModelId` | string | 否 | body | 大语言模型配置ID，不传时使用默认模板。DeepSeek V4 Flash（推荐）：f3626c105383d71654a57f2bb8a973f3；其他可选模型通过 GET /v1/agents/models/llm 获取 |
| `memModelId` | string | 否 | body | 记忆模型ID，不传时使用默认模板。无记忆：Memory_nomem；长期记忆：Memory_long_term_memory |
| `intentModelId` | string | 否 | body | 意图模型ID，不传时使用默认模板。无意图识别：Intent_nointent；统一意图识别：Intent_function_call |

#### 请求头

| Header名 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Content-Type` |  | 是 | 请求内容类型 |
| `Authorization` |  | 是 | 用户认证令牌，格式为 Bearer + 空格 + token |

#### 请求示例

```
POST /xiaozhi/agent HTTP/1.1
Host: https://xrobo.qiniu.com
Content-Type: application/json
Authorization: Bearer <token>

{
  "agentName": "客服助手",
  "assistantName": "助手阿伟",
  "llmModelId": "f3626c105383d71654a57f2bb8a973f3",
  "intentModelId": "Intent_function_call"
}
```

#### 响应示例

```
{
  "code": 0,
  "msg": "success",
  "data": "6f99512f6b55429f8d2e3ddd0bcbe23f"
}
```

#### 状态码

- `0` OK - 智能体创建成功，返回智能体ID
- `401` Unauthorized - 未登录或token无效

### 更新智能体

更新指定智能体的配置信息，包括模型配置、系统提示词、记忆设置、插件函数等

**`PUT`** `https://xrobo.qiniu.com/xiaozhi/agent/{id}`

#### 基本信息

- **Host**: `https://xrobo.qiniu.com`
- **Base Path**: `/xiaozhi`
- **Method**: `PUT`
- **返回类型**: `application/json`

#### 请求参数

| 参数名 | 类型 | 必填 | 位置 | 说明 |
| --- | --- | --- | --- | --- |
| `id` | string | 是 | path | 智能体ID |
| `agentUpdateObj` | AgentUpdateObj | 是 | body | 智能体更新对象 |

#### 请求头

| Header名 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Content-Type` |  | 是 | 请求内容类型 |
| `Authorization` |  | 是 | 用户认证令牌，格式为 Bearer + 空格 + token |

#### 请求示例

```
PUT /xiaozhi/agent/31dad2a8042a40ec879ef92a7bc240ae HTTP/1.1
Host: https://xrobo.qiniu.com
Content-Type: application/json
Authorization: Bearer <token>

{
  "agentCode": "AGT_1754966279238",
  "agentName": "123test",
  "assistantName": "助手阿伟",
  "asrModelId": "ASR_DoubaoASR",
  "vadModelId": "VAD_SileroVAD",
  "llmModelId": "LLM_AliLLM",
  "vllmModelId": "VLLM_QwenVLVLLM",
  "vllmPrompt": "请识别图片中的主要物体和文字。",
  "voiceprintModelId": "VOICEPRINT_3DSPEAKER",
  "ttsVoiceId": "a5b85a7ba5b24a9a96e24aa88b500d2f",
  "chatHistoryConf": 0,
  "memModelId": "Memory_mem_local_short",
  "intentModelId": "Intent_intent_llm",
  "systemPrompt": "*新的角色介绍",
  "summaryMemory": null,
  "langCode": "zh",
  "language": "中文",
  "sort": 0,
  "functions": [
    {
      "pluginId": "SYSTEM_PLUGIN_MUSIC",
      "paramInfo": {}
    },
    {
      "pluginId": "SYSTEM_PLUGIN_NEWS_NEWSNOW",
      "paramInfo": {
        "url": "https://newsnow.busiyi.world/api/s?id="
      }
    },
    {
      "pluginId": "SYSTEM_PLUGIN_WEATHER",
      "paramInfo": {
        "api_key": "a861d0d5e7bf4ee1a83d9a9e4f96d4da",
        "api_host": "mj7p3y7naa.re.qweatherapi.com",
        "default_location": "广州"
      }
    }
  ],
  "extra": {
    "llm": {
      "temperature": 0.7,
      "top_p": 1.0,
      "frequency_penalty": 0.0,
      "max_tokens": 500,
      "enable_search": false,
      "custom_params": {
        "body": {}
      }
    },
    "voice": {
      "volume": 50,
      "speed": 1.0,
      "pitch": 1.0,
      "emotion": "default",
      "quality": "medium",
      "enable_tts_emotion": false
    },
    "asr": {
      "disable_emotion": false,
      "enable_auto_lang": false
    },
    "goodbye": {
      "idle_timeout_seconds": 120
    },
    "voiceprint": {
      "chat_only_enabled": false
    },
    "disabled_builtin_tools": ["get_time"]
  }
}
```

#### 响应示例

```
{
  "code": 0,
  "msg": "success",
  "data": null
}
```

#### 状态码

- `0` OK - 操作成功
- `401` Unauthorized - 未登录或token无效

> **INFO**
>
> 更新智能体时，只需传递需要修改的字段。普通字段省略则保留原值；`datasetIds` 和 `extra.disabled_builtin_tools` 使用完整列表替换语义，传空数组会清空列表。`extra.llm.custom_params` 提供非 `null` 值时整体替换，传 `null` 清空，省略则保留。`extra.voiceprint: {}` 会将 `chat_only_enabled` 设置为 `false`。`extra.goodbye` 目前只支持 `idle_timeout_seconds`，范围为 60~3600 秒。

> **选择音色**
>
> 如需修改语音合成音色，请更新 `ttsVoiceId`。

> **更新示例**
>
> 关闭唤醒问候并设置智能体空闲断连时长：
>
> ```
> {
>   "extra": {
>     "disable_greeting": true,
>     "goodbye": {
>       "idle_timeout_seconds": 120
>     }
>   }
> }
> ```

> **LLM 参数说明**
>
> `extra.llm` 下的标准参数（temperature、top_p 等）越界会被自动夹取、不报错；`extra.llm.custom_params.body` 下的自定义参数会按所选模型的 schema 校验，非法参数直接拒绝。支持哪些自定义参数取决于所选模型、且会随模型调整而变化，具体 key/类型请以模型 schema 查询结果为准，详见 [大语言模型 API](llm.md) 的「大模型自定义参数（custom_params）」一节。

> **记忆模型说明**
>
> `memModelId` 用于配置智能体的记忆模式。详细说明及系统行为请参见 [长期记忆 API](longterm-memory.md)。

### 删除智能体

删除指定的智能体，此操作不可逆，请谨慎使用

**`DELETE`** `https://xrobo.qiniu.com/xiaozhi/agent/{id}`

#### 基本信息

- **Host**: `https://xrobo.qiniu.com`
- **Base Path**: `/xiaozhi`
- **Method**: `DELETE`
- **返回类型**: `application/json`

#### 请求参数

| 参数名 | 类型 | 必填 | 位置 | 说明 |
| --- | --- | --- | --- | --- |
| `id` | string | 是 | path | 要删除的智能体ID |

#### 请求头

| Header名 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Authorization` |  | 是 | 用户认证令牌，格式为 Bearer + 空格 + token |

#### 请求示例

```
DELETE /xiaozhi/agent/31dad2a8042a40ec879ef92a7bc240ae HTTP/1.1
Host: https://xrobo.qiniu.com
Authorization: Bearer <token>
```

#### 响应示例

```
{
  "code": 0,
  "msg": "删除成功",
  "data": {}
}
```

#### 状态码

- `0` OK - 操作成功
- `401` Unauthorized - 未登录或token无效

> **WARNING**
>
> 删除操作不可逆，请确认后再执行

### 切换设备关联智能体

**Base URL：** `https://xrobo.qiniu.com/v1`

将设备切换到目标智能体。对于已正式注册的设备，默认会把该设备的**全部聊天历史**迁移至目标智能体；可通过请求体禁止迁移。若设备尚未注册、但存在预注册记录，则仅更新预注册设备绑定的智能体，不涉及聊天记录迁移。

响应 HTTP 状态码当前统一为 `200 OK`；请以响应体的 `code` 判断业务是否成功。`code = 0` 表示成功，非 `0` 表示失败。

### 切换设备关联智能体

切换设备到目标智能体；已注册设备默认迁移该 MAC 下全部聊天历史，预注册设备仅更新绑定关系

**`PUT`** `https://xrobo.qiniu.com/v1/devices/{mac_address}/agent/{agent_id}`

#### 基本信息

- **Host**: `https://xrobo.qiniu.com`
- **Base Path**: `/v1`
- **Method**: `PUT`
- **返回类型**: `application/json`

#### 请求参数

| 参数名 | 类型 | 必填 | 位置 | 说明 |
| --- | --- | --- | --- | --- |
| `mac_address` | string | 是 | path | 设备 MAC 地址，格式为 00:1A:2B:3C:4D:5E，大小写均可 |
| `agent_id` | string | 是 | path | 目标智能体 ID（32 位小写 hex，必须归属当前认证用户） |
| `disable_chat_history_migration` | boolean | 否 | body | 是否禁止迁移聊天历史；`false` 或省略时迁移，`true` 时仅切换设备绑定 |

#### 请求头

| Header名 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Authorization` |  | 是 | 用户认证凭证 |
| `Content-Type` |  | 是 | 请求体格式 |

#### 请求示例

```
PUT /v1/devices/AB:CA:A9:60:D8:48/agent/03fe2c47ec8c47a28c7f382a47b4f838 HTTP/1.1
Host: xrobo.qiniu.com
Authorization: Bearer <用户登录 Token 或 API Key>
Content-Type: application/json

{
  "disable_chat_history_migration": true
}
```

#### 响应示例

```
{
  "code": 0,
  "reqid": "request-id",
  "data": {}
}
```

#### 状态码

- `0` 成功
- `400` MAC 地址或智能体 ID 不合法；请求体缺失或 JSON 格式错误
- `401` 未携带认证凭证或认证凭证无效
- `403` Token 已过期或无设备操作权限
- `404` 已注册和预注册记录中均不存在该设备
- `599` 服务端内部异常

> **请求体必填**
>
> 请求体**必须存在**，即使使用默认行为也必须传递 `{}`。请求体缺失或不是合法 JSON 时，接口返回业务错误 `code: 400`。

#### 请求体

```
{
  "disable_chat_history_migration": false
}
```

`disable_chat_history_migration` 为可选布尔值，默认 `false`：

| 值 | 已注册设备行为 | 预注册设备行为 |
| --- | --- | --- |
| `false` 或省略 | 切换设备绑定，并将该 MAC 下全部聊天记录迁移至目标智能体。 | 仅更新预注册设备绑定，不迁移聊天记录。 |
| `true` | 仅切换设备绑定，原聊天记录保留在原智能体名下。 | 仅更新预注册设备绑定，不迁移聊天记录。 |

#### 切换且迁移聊天历史（默认）

```
curl -X PUT 'https://xrobo.qiniu.com/v1/devices/AB:CA:A9:60:D8:48/agent/03fe2c47ec8c47a28c7f382a47b4f838' \
  -H 'Authorization: Bearer <TOKEN>' \
  -H 'Content-Type: application/json' \
  -d '{}'
```

#### 切换但保留原聊天历史归属

```
curl -X PUT 'https://xrobo.qiniu.com/v1/devices/AB:CA:A9:60:D8:48/agent/03fe2c47ec8c47a28c7f382a47b4f838' \
  -H 'Authorization: Bearer <TOKEN>' \
  -H 'Content-Type: application/json' \
  -d '{"disable_chat_history_migration":true}'
```

#### 权限和行为说明

| 情况 | 行为 |
| --- | --- |
| 目标智能体不属于当前用户 | 拒绝切换。 |
| 已注册设备不属于当前用户 | 拒绝切换。 |
| 已注册设备的原智能体不属于当前用户 | 拒绝切换。 |
| 预注册设备不属于当前用户 | 拒绝切换。 |
| 已注册和预注册表中均找不到设备 | 返回设备不存在。 |

#### 常见失败响应

```
{
  "code": 400,
  "msg": "invalid request body",
  "reqid": "request-id",
  "data": null
}
```

| `code` | `msg` | 场景 |
| --- | --- | --- |
| `400` | `invalid mac address` / `invalid mac address. format: ...` | MAC 地址为空或格式不合法。 |
| `400` | `invalid agent id` | 目标智能体 ID 为空。 |
| `400` | `invalid request body` | 没有请求体或 JSON 格式不正确。 |
| `401` | `authorization header required` / `invalid token` | 未认证或认证凭证无效。 |
| `403` | `token is expired` | 登录 Token 已过期。 |
| `403` | `permission denied` | 当前用户无该设备或预注册设备的操作权限。 |
| `404` | `device not found` | 已注册和预注册记录中均不存在该 MAC。 |
| 非 `0` | 智能体不存在或无智能体权限 | 目标智能体不存在、不归属当前用户，或原智能体归属校验失败。 |
| `599` | 具体错误信息 | 服务端内部异常。 |

### 获取智能体详情

获取指定智能体的配置详情，包括基础信息、模型配置、提示词、知识库绑定、设备数量和最近连接时间

**`GET`** `https://xrobo.qiniu.com/xiaozhi/agent/{id}`

#### 基本信息

- **Host**: `https://xrobo.qiniu.com`
- **Base Path**: `/xiaozhi`
- **Method**: `GET`
- **返回类型**: `application/json`

#### 请求参数

| 参数名 | 类型 | 必填 | 位置 | 说明 |
| --- | --- | --- | --- | --- |
| `id` | string | 是 | path | 智能体ID |

#### 请求头

| Header名 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Authorization` |  | 是 | 用户认证令牌，格式为 Bearer + 空格 + token |

#### 请求示例

```
GET /xiaozhi/agent/xxxxxx HTTP/1.1
Host: https://xrobo.qiniu.com
Authorization: Bearer <token>
```

#### 响应示例

```
{
  "code": 0,
  "msg": "success",
  "data": {
    "id": "xxxx",
    "agentName": "小智助手",
    "assistantName": "助手阿伟",
    "llmModelId": "LLM_AliLLM",
    "ttsVoiceId": "xxxxxxx",
    "memModelId": "Memory_mem_local_short",
    "intentModelId": "Intent_intent_llm",
    "chatHistoryConf": 1,
    "systemPrompt": "...",
    "summaryMemory": null,
    "language": "中文",
    "langCode": "zh",
    "datasetIds": [],
    "deviceCount": 5,
    "lastConnectedAt": "2026-07-09 09:00:00",
    "createdAt": "2026-07-01 12:00:00",
    "updatedAt": "2026-07-09 09:00:00"
  }
}
```

#### 状态码

- `0` OK - 成功获取智能体详情
- `401` Unauthorized - 未授权或token无效
- `500` Internal Server Error - 智能体不存在

## 其他说明项
