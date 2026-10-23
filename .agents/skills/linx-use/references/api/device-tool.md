<!-- 来源：https://linx.qiniu.com/docs/xrobot/api/device-tool | 由 sync-docs.py 自动生成，请勿手改 -->

# 设备工具描述API

设备工具描述API用于管理并自定义当前用户的设备工具描述。

> **更新语义**
>
> `desc` 和 `pre_prompt_texts` 至少提供一项。更新接口会用请求中的两个字段整体替换已有值；字段缺省或传空值都会写入空值。若要保留已有前置提示词，请在更新请求中携带原配置。

`pre_prompt_texts` 是语言代码到提示词数组的映射。语言代码必须与系统语言表中的代码完全匹配；每种语言最多10条提示词，普通用户的描述和单条提示词最长200个字符。

> **使用说明**
>
> 不同设备支持的工具可能不同，请填写设备实际上报的工具名称。

## 1. 查询设备工具描述

### 查询设备工具描述

获取当前用户已配置的设备工具描述列表。

**`GET`** `https://xrobo.qiniu.com/v1/device/tools`

#### 基本信息

- **Host**: `https://xrobo.qiniu.com`
- **Base Path**: `/v1`
- **Method**: `GET`
- **返回类型**: `application/json`

#### 请求头

| Header名 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Authorization` | string | 是 | Bearer token认证 |

#### 响应示例

```
{
  "code": 0,
  "data": {
    "tools": [
      {
        "id": "9f4b1d7c2e8a4f5b9c0d1e2f3a4b5c6d",
        "name": "self.audio_speaker.set_volume",
        "desc": "设置音量。",
        "pre_prompt_texts": {
          "zh-CN": ["请使用简洁的中文回答。"]
        }
      }
    ]
  }
}
```

#### 状态码

- `0` 查询成功

## 2. 新增设备工具描述

### 新增设备工具描述

**`POST`** `https://xrobo.qiniu.com/v1/device/tools`

#### 基本信息

- **Host**: `https://xrobo.qiniu.com`
- **Base Path**: `/v1`
- **Method**: `POST`
- **返回类型**: `application/json`

#### 请求参数

| 参数名 | 类型 | 必填 | 位置 | 说明 |
| --- | --- | --- | --- | --- |
| `name` | string | 是 | - | 设备工具名称 |
| `desc` | string | 否 | - | 自定义工具描述，最长200个字符。与 pre_prompt_texts 至少提供一项，也可以同时提供 |
| `pre_prompt_texts` | object | 否 | - | 按语言代码映射前置提示词数组。与 desc 至少提供一项；每种语言最多10条，单条最长200个字符 |

#### 请求头

| Header名 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Authorization` | string | 是 | Bearer token认证 |
| `Content-Type` | string | 是 | 请求内容类型 |

#### 请求示例

```
{
  "name": "self.audio_speaker.set_volume",
  "desc": "设置音量。",
  "pre_prompt_texts": {
    "zh-CN": ["请使用简洁的中文回答。"]
  }
}
```

#### 响应示例

```
{
  "code": 0,
  "data": {
    "id": "9f4b1d7c2e8a4f5b9c0d1e2f3a4b5c6d",
    "name": "self.audio_speaker.set_volume",
    "desc": "设置音量。",
    "pre_prompt_texts": {
      "zh-CN": ["请使用简洁的中文回答。"]
    }
  }
}
```

#### 状态码

- `0` 新增成功
- `400` 请求参数错误
- `614` 同一用户已配置相同工具名称

## 3. 更新设备工具描述

### 更新设备工具描述

**`PUT`** `https://xrobo.qiniu.com/v1/device/tools/{id}`

#### 基本信息

- **Host**: `https://xrobo.qiniu.com`
- **Base Path**: `/v1`
- **Method**: `PUT`
- **返回类型**: `application/json`

#### 请求参数

| 参数名 | 类型 | 必填 | 位置 | 说明 |
| --- | --- | --- | --- | --- |
| `id` | string | 是 | - | 设备工具描述配置ID，由查询、新增或更新接口返回 |
| `name` | string | 是 | - | 设备工具名称 |
| `desc` | string | 否 | - | 自定义工具描述，最长200个字符。与 pre_prompt_texts 至少提供一项，也可以同时提供 |
| `pre_prompt_texts` | object | 否 | - | 按语言代码映射前置提示词数组。与 desc 至少提供一项；每种语言最多10条，单条最长200个字符 |

#### 请求头

| Header名 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Authorization` | string | 是 | Bearer token认证 |
| `Content-Type` | string | 是 | 请求内容类型 |

#### 请求示例

```
{
  "name": "self.audio_speaker.set_volume",
  "desc": "设置音量。",
  "pre_prompt_texts": {
    "zh-CN": ["请使用简洁的中文回答。"]
  }
}
```

#### 响应示例

```
{
  "code": 0,
  "data": {
    "id": "9f4b1d7c2e8a4f5b9c0d1e2f3a4b5c6d",
    "name": "self.audio_speaker.set_volume",
    "desc": "设置音量。",
    "pre_prompt_texts": {
      "zh-CN": ["请使用简洁的中文回答。"]
    }
  }
}
```

#### 状态码

- `0` 更新成功
- `400` 请求参数错误
- `612` 配置不存在，或配置不属于当前用户
- `614` 同一用户已配置相同工具名称

## 4. 删除设备工具描述

### 删除设备工具描述

**`DELETE`** `https://xrobo.qiniu.com/v1/device/tools/{id}`

#### 基本信息

- **Host**: `https://xrobo.qiniu.com`
- **Base Path**: `/v1`
- **Method**: `DELETE`
- **返回类型**: `application/json`

#### 请求参数

| 参数名 | 类型 | 必填 | 位置 | 说明 |
| --- | --- | --- | --- | --- |
| `id` | string | 是 | - | 设备工具描述配置ID，由查询、新增或更新接口返回 |

#### 请求头

| Header名 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Authorization` | string | 是 | Bearer token认证 |

#### 响应示例

```
{
  "code": 0,
  "data": null
}
```

#### 状态码

- `0` 删除成功
- `612` 配置不存在，或配置不属于当前用户
