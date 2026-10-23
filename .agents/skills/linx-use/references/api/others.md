<!-- 来源：https://linx.qiniu.com/docs/xrobot/api/others | 由 sync-docs.py 自动生成，请勿手改 -->

# 其他API文档

## 获取语音列表

获取系统中所有可用的语音模型列表，包含语音的详细信息、标签和情感支持

**`GET`** `https://xrobo.qiniu.com/v1/voices`

#### 基本信息

- **Host**: `https://xrobo.qiniu.com`
- **Base Path**: `/v1`
- **Method**: `GET`
- **返回类型**: `application/json`

#### 请求头

| Header名 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Authorization` | string | 是 | Bearer token认证 |

#### 请求示例

```
GET /v1/voices HTTP/1.1
Host: xrobo.qiniu.com
Authorization: Bearer cab2efa1c82b6b6fb153c123962be6ce
```

#### 响应示例

```
{
  "code": 0,
  "reqid": "req_12345",
  "data": {
    "voices": [
      {
        "id": "voice_001",
        "name": "小雅/Xiaoya",
        "demo": "https://example.com/demo.mp3",
        "tags": {
          "languages": ["zh", "en"],
          "age": "young",
          "gender": "female",
          "styles": ["温柔", "甜美"],
          "scenes": ["客服", "播音"],
          "profile": "standard",
          "emotions": [
            {
              "emotion": "开心",
              "demo_url": "https://example.com/happy.mp3"
            },
            {
              "emotion": "平静",
              "demo_url": "https://example.com/calm.mp3"
            }
          ]
        }
      }
    ]
  }
}
```

#### 状态码

- `0` 请求成功
- `401` 认证失败，token无效或过期
- `500` 服务器内部错误

### 认证说明

此API需要Bearer token认证，请在请求头中包含有效的访问令牌：

```
Authorization: Bearer {token}
```

## 更新音色标签

> **注意：** 请不要输入不合法的字段和数据，可能导致音色不可用。各字段的可选值请严格参照下方参数说明中的枚举值。

### 更新音色标签

更新指定音色的标签信息，包括年龄、性别、风格、场景和头像图片。所有字段均为可选，仅更新传递的字段。

**`PUT`** `https://xrobo.qiniu.com/v1/voices/{voice_id}/tags`

#### 基本信息

- **Host**: `https://xrobo.qiniu.com`
- **Base Path**: `/v1`
- **Method**: `PUT`
- **返回类型**: `application/json`

#### 请求参数

| 参数名 | 类型 | 必填 | 位置 | 说明 |
| --- | --- | --- | --- | --- |
| `voice_id` | string | 是 | - | 音色ID |

#### 请求头

| Header名 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Authorization` | string | 是 | Bearer token认证 |
| `Content-Type` | string | 是 | 请求内容类型 |

#### 请求示例

```
PUT /v1/voices/{voice_id}/tags HTTP/1.1
Host: xrobo.qiniu.com
Authorization: Bearer cab2efa1c82b6b6fb153c123962be6ce
Content-Type: application/json

{
  "age": "young",
  "gender": "female",
  "styles": ["wenrou", "tianmei"],
  "scenes": ["supporter", "assistant"],
  "header_image_url": "https://example.com/avatar.png"
}
```

#### 响应示例

```
{
  "code": 0,
  "reqid": "req_12345",
  "data": {}
}
```

#### 状态码

- `0` 请求成功
- `400` 请求参数错误（如无效的标签值、不支持的字段等）
- `401` 认证失败，token无效或过期
- `403` 无权限修改该音色
- `500` 服务器内部错误

### 请求体字段说明

| 字段 | 类型 | 必填 | 可选值 |
| --- | --- | --- | --- |
| `age` | string | 否 | `child`（儿童）、`young`（青年）、`adult`（壮年）、`middle`（中年）、`senior`（老年） |
| `gender` | string | 否 | `male`（男）、`female`（女） |
| `styles` | string[] | 否 | `acgn`（二次元）、`yansu`（严肃）、`lengjing`（冷静）、`youhao`（友好）、`wenrou`（温柔）、`qiangshi`（强势）、`kailang`（开朗）、`zhixing`（知性）、`huopo`（活泼）、`zhibo`（直播）、`chenwen`（沉稳）、`langman`（浪漫）、`tianmei`（甜美）、`daiban`（呆板）、`youyu`（忧郁）、`zhishuai`（直率）、`rexue`（热血）、`gufeng`（古风）、`default`（默认） |
| `scenes` | string[] | 否 | `supporter`（客服）、`assistant`（语音助手）、`live`（直播）、`audiobook`（有声书）、`companion`（社交陪伴）、`toddler`（童声）、`dialect`（方言）、`recitation`（诗词朗诵）、`news`（新闻播报）、`default`（默认） |
| `header_image_url` | string | 否 | 需为合法的图片URL |

### 认证说明

此API需要Bearer token认证，请在请求头中包含有效的访问令牌：

```
Authorization: Bearer {token}
```
