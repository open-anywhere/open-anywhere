<!-- 来源：https://linx.qiniu.com/docs/xrobot/api/voice-clone | 由 sync-docs.py 自动生成，请勿手改 -->

# 音色克隆API

## 音色状态说明

音色在训练过程中会经历以下状态：

- **Init** : 初始状态，刚创建的音色栏位
- **Training** : 训练中，正在处理音频文件
- **Success** : 训练成功，音色可以正常使用
- **Failed** : 训练失败，需要重新训练

## 1. 创建音色栏位

### 创建音色栏位

创建一个新的音色栏位，为后续的音色训练做准备。创建成功后会返回音色ID和默认名称。(注意：音色栏位创建后，语言不可修改！)

**`POST`** `https://xrobo.qiniu.com/v1/voice-clones`

#### 基本信息

- **Host**: `https://xrobo.qiniu.com`
- **Base Path**: ``
- **Method**: `POST`
- **返回类型**: `application/json`

#### 请求参数

| 参数名 | 类型 | 必填 | 位置 | 说明 |
| --- | --- | --- | --- | --- |
| `language` | string | 否 | - | 音色语言，默认为 "zh"。免费版仅支持 zh(中文) 和 en(英语)；付费版支持 zh(中文)、en(英语)、fr(法语)、de(德语)、ja(日语)、ko(韩语)、ru(俄语)、pt(葡萄牙语)、th(泰语)、id(印尼语)、vi(越南语)；付费（商务版）支持 zh(中文)、en(英语)、ko(韩语)、es(西班牙语)、ar(阿拉伯语)、hi(印地语)、it(意大利语)、tr(土耳其语)、yue(粤语)、ms(马来语)、he(希伯来语) |
| `tier` | string | 否 | - | 资源包档位，非必填，字符串枚举。不传时，默认使用 "free"。支持：free(免费)、lite(付费)、pro(付费商务版)。使用购买的资源包时，需要显式指定传入该字段。 |

#### 请求头

| Header名 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Authorization` | string | 是 | Bearer token认证 |
| `Content-Type` | string | 是 | 请求内容类型 |

#### 请求示例

```
{
  "language": "zh",
  "tier": "lite"
}
```

#### 响应示例

```
{
    "code": 0,
    "reqid": "0zooABHhub9fUgYA",
    "data": {
        "id": "95da00f77ad24c5ea246618b2a678cc6",
        "name": "复刻音色-78cc6",
        "language": "zh",
        "demo_url": "",
        "state": "Init",
        "tier": "lite",
        "expired_time": ""
    }
}
```

#### 状态码

- `0` 创建成功
- `400` 请求参数错误
- `401` 未授权访问
- `403` 指定档位没有可用额度
- `500` 服务器内部错误

## 2. 训练音色

### 训练音色

使用音频文件训练指定的音色，或仅更新音色名称。 如果提供音频URL，系统将根据音频进行训练；如果仅提供名称，则只更新音色名称。 更新请求不需要也不能修改 tier，系统会沿用音色当前档位。免费版音色续期后档位变为付费版，后续重新训练使用付费版档位额度。

**`PUT`** `https://xrobo.qiniu.com/v1/voice-clones/{id}`

#### 基本信息

- **Host**: `https://xrobo.qiniu.com`
- **Base Path**: ``
- **Method**: `PUT`
- **返回类型**: `application/json`

#### 请求参数

| 参数名 | 类型 | 必填 | 位置 | 说明 |
| --- | --- | --- | --- | --- |
| `id` | string | 是 | - | 音色唯一标识符，由创建音色栏位接口返回 |
| `key` | string | 否 | - | 音频文件URL。为空时仅修改名称，不为空时根据音频文件进行训练 |
| `name` | string | 是 | - | 音色名称，限制20字符以内（汉字/字母/数字都算一个字符） |

#### 请求头

| Header名 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Authorization` | string | 是 | Bearer token认证 |
| `Content-Type` | string | 是 | 请求内容类型 |

#### 请求示例

```
{
  "key": "https://example.com/voice-sample.wav",
  "name": "我的专属音色"
}
```

#### 响应示例

```
{
    "code": 0,
    "reqid": "-qM3AFjdjsVfUgYA",
    "data": {
        "id": "f54b728eb9eb4faf960d82fdfcc6403a",
        "name": "我的专属音色",
        "language": "zh",
        "demo_url": "",
        "state": "Training",
        "tier": "lite",
        "expired_time": ""
    }
}
```

#### 状态码

- `0` 训练请求提交成功
- `400` 请求参数错误
- `401` 未授权访问
- `404` 音色不存在
- `403` 对应档位没有可用额度
- `500` 服务器内部错误

> **INFO**
>
> 1. **档位与额度** : 创建接口的 `tier` 为非必填字段，不传时，默认使用 `free` ；不会自动匹配账户已购买的资源包。使用购买的资源包时，请显式传入对应的 `tier` 。
> 2. **更新接口不修改档位** : `PUT /v1/voice-clones/{id}` 不接收 `tier` ，音色会沿用当前档位。免费版音色续期后档位变为付费版，后续重新训练使用付费版档位额度。
> 3. **额度扣减规则** : 更新请求包含音频 URL 时会执行复刻并扣减对应档位额度；仅更新名称不会扣减额度。
> 4. **音色名称限制** : 音色名称最多20个字符，汉字、字母、数字都算作一个字符
> 5. **音频文件建议** : 训练音频建议时长在20-30秒之间，音质清晰，无背景噪音。
> 6. **训练时间** : 音色训练通常需要几分钟到十几分钟，请耐心等待

## 3. 获取音色信息

### 获取音色信息

根据音色ID获取指定音色的详细信息，包括名称、语言、试听链接和当前状态。

**`GET`** `https://xrobo.qiniu.com/v1/voice-clones/{id}`

#### 基本信息

- **Host**: `https://xrobo.qiniu.com`
- **Base Path**: ``
- **Method**: `GET`
- **返回类型**: `application/json`

#### 请求参数

| 参数名 | 类型 | 必填 | 位置 | 说明 |
| --- | --- | --- | --- | --- |
| `id` | string | 是 | - | 音色唯一标识符 |

#### 请求头

| Header名 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Authorization` | string | 是 | Bearer token认证 |

#### 响应示例

```
{
  "code": 0,
  "reqid": "req_12345678",
  "data": {
    "id": "voice_clone_abc123",
    "name": "我的专属音色",
    "language": "zh",
    "demo_url": "https://example.com/demo.wav",
    "state": "Success",
    "tier": "free",
    "expired_time": "2027-10-14T12:00:00+08:00"
  }
}
```

#### 状态码

- `0` 获取成功
- `401` 未授权访问
- `500` 服务器内部错误

> **INFO**
>
> **状态检查**: 只有状态为"Success"的音色才能正常使用

## 4. 获取音色列表

### 获取音色列表

获取当前用户账户下的音色克隆列表，包括各种状态的音色。

**`GET`** `https://xrobo.qiniu.com/v1/voice-clones`

#### 基本信息

- **Host**: `https://xrobo.qiniu.com`
- **Base Path**: ``
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
  "reqid": "req_12345678",
  "data": {
    "voices": [
      {
        "id": "voice_clone_abc123",
        "name": "我的专属音色",
        "language": "zh",
        "demo_url": "https://example.com/demo1.wav",
        "state": "Success",
        "tier": "free",
        "expired_time": "2027-10-14T12:00:00+08:00"
      },
      {
        "id": "voice_clone_def456",
        "name": "复刻音色-X9Y8Z",
        "language": "ko",
        "demo_url": "",
        "state": "Training",
        "tier": "pro",
        "expired_time": ""
      }
    ]
  }
}
```

#### 状态码

- `0` 获取成功
- `401` 未授权访问
- `500` 服务器内部错误

## 5. 音色克隆额度

查询指定档位的音色复刻资源包可用数量。

### 查询音色复刻额度

查询当前用户指定档位的音色复刻资源包可用数量。

**`GET`** `https://xrobo.qiniu.com/v1/voice-clones/quota`

#### 基本信息

- **Host**: `https://xrobo.qiniu.com`
- **Base Path**: ``
- **Method**: `GET`
- **返回类型**: `application/json`

#### 请求参数

| 参数名 | 类型 | 必填 | 位置 | 说明 |
| --- | --- | --- | --- | --- |
| `tier` | string | 是 | - | 音色复刻档位，仅支持 free、lite、pro |

#### 请求头

| Header名 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Authorization` | string | 是 | Bearer token认证 |

#### 响应示例

```
{
  "code": 0,
  "reqid": "req_12345678",
  "data": {
    "available_count": 8
  }
}
```

#### 状态码

- `0` 查询成功
- `400` tier 参数错误
- `401` 未授权访问
- `500` 服务器内部错误

> **INFO**
>
> - `tier` 为必填查询参数，仅支持 `free` 、 `lite` 、 `pro` 。
> - `available_count` 表示该档位当前可用的音色复刻次数汇总。
> - 该接口不返回资源包明细、 `usage_count` 、 `total_count` 或用户使用记录。

## 6. 续期音色

`expired_time` 是音色有效期，按 RFC 3339 时间格式返回。尚未完成复刻或没有有效期时为空字符串；复刻成功后从成功时间起算一年，重新复刻成功会重新起算。

对已复刻成功的私有音色续期。请求无需 body；每次续期消耗一次额度。有效期未过时从原到期时间延长一年，已过期时从续期时间起算一年。免费版音色优先使用免费额度，不足时使用付费版资源包额度，续期后档位变为付费版；其他档位使用同档位额度。

### 续期音色

续期已复刻成功且支持续期的私有音色，返回更新后的音色信息。

**`POST`** `https://xrobo.qiniu.com/v1/voice-clones/{id}/renewals`

#### 基本信息

- **Host**: `https://xrobo.qiniu.com`
- **Base Path**: ``
- **Method**: `POST`
- **返回类型**: `application/json`

#### 请求参数

| 参数名 | 类型 | 必填 | 位置 | 说明 |
| --- | --- | --- | --- | --- |
| `id` | string | 是 | - | 待续期的音色 ID |

#### 请求头

| Header名 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Authorization` | string | 是 | Bearer token认证 |

#### 响应示例

```
{
  "code": 0,
  "reqid": "req_12345678",
  "data": {
    "id": "voice_clone_abc123",
    "name": "我的专属音色",
    "language": "zh",
    "demo_url": "https://example.com/demo.wav",
    "state": "Success",
    "tier": "lite",
    "expired_time": "2028-10-14T12:00:00+08:00"
  }
}
```

#### 状态码

- `0` 续期成功
- `401` 未授权访问
- `403` 没有可用的续期额度
- `404` 音色不存在
- `409` 音色不支持续期
- `500` 服务器内部错误

## 7. 删除音色

### 删除音色

删除指定的音色克隆。删除后该音色将无法恢复，请谨慎操作。(付费商务版音色栏位不可删除！)

**`DELETE`** `https://xrobo.qiniu.com/v1/voice-clones/{id}`

#### 基本信息

- **Host**: `https://xrobo.qiniu.com`
- **Base Path**: ``
- **Method**: `DELETE`
- **返回类型**: `application/json`

#### 请求参数

| 参数名 | 类型 | 必填 | 位置 | 说明 |
| --- | --- | --- | --- | --- |
| `id` | string | 是 | - | 待删除的音色唯一标识符 |

#### 请求头

| Header名 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Authorization` | string | 是 | Bearer token认证 |

#### 响应示例

```
{
  "code": 0,
  "reqid": "req_12345678",
  "data": {}
}
```

#### 状态码

- `0` 删除成功
- `401` 未授权访问
- `403` 付费音色（商务版）栏位不可删除
- `404` 音色不存在
- `500` 服务器内部错误

> **WARNING**
>
> 删除音色后无法恢复，请谨慎操作
