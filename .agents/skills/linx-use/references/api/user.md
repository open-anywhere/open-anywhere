<!-- 来源：https://linx.qiniu.com/docs/xrobot/api/user | 由 sync-docs.py 自动生成，请勿手改 -->

# 用户API文档

本文档描述了用户相关的API接口，包括用户注册、登录和验证码获取等功能。

## 1. 用户注册

### 用户注册

创建新用户账户。需要提供用户名和密码；根据平台配置使用图形验证码或手机验证码。

**`POST`** `https://xrobo.qiniu.com/v1/users`

#### 基本信息

- **Host**: `https://xrobo.qiniu.com`
- **Base Path**: `/v1`
- **Method**: `POST`
- **返回类型**: `application/json`

#### 请求参数

| 参数名 | 类型 | 必填 | 位置 | 说明 |
| --- | --- | --- | --- | --- |
| `username` | string | 是 | - | 用户名 |
| `password` | string | 是 | - | 用户密码 |
| `captcha` | string | 否 | - | 图形验证码 |
| `mobile_captcha` | string | 否 | - | 手机验证码 |
| `captcha_id` | string | 否 | - | 验证码ID |

#### 请求头

| Header名 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Content-Type` |  | 是 | 请求内容类型 |

#### 请求示例

```
{
  "username": "example",
  "password": "password",
  "captcha": "ABCD",
  "mobile_captcha": "123456",
  "captcha_id": "cfa94872-48b6-425b-8e6a-17b912b6b6f4"
}
```

#### 响应示例

```
{
  "code": 0,
  "msg": "注册成功",
  "data": {}
}
```

#### 状态码

- `0` 注册成功
- `400` 请求参数错误
- `409` 用户名已存在
- `500` 服务器内部错误

## 2. 获取验证码

### 获取图形验证码

获取用于注册和登录的图形验证码。返回PNG格式的验证码图片，uuid 通过 query 参数传入。

**`GET`** `https://xrobo.qiniu.com/v1/auth/captcha`

#### 基本信息

- **Host**: `https://xrobo.qiniu.com`
- **Base Path**: `/v1`
- **Method**: `GET`
- **返回类型**: `application/json`

#### 请求参数

| 参数名 | 类型 | 必填 | 位置 | 说明 |
| --- | --- | --- | --- | --- |
| `uuid` | string | 是 | - | 随机生成的UUID |

#### 状态码

- `0` 成功返回验证码图片
- `400` UUID参数错误
- `500` 服务器内部错误

> **提示**
>
> UUID需要随机生成，建议使用标准的UUID v4格式。验证码图片为PNG格式，需要用户识别并输入验证码内容。

## 3. 用户登录

### 用户登录

用户登录认证。成功后返回访问令牌，该令牌用于后续API调用的身份验证。

**`POST`** `https://xrobo.qiniu.com/v1/auth/login`

#### 基本信息

- **Host**: `https://xrobo.qiniu.com`
- **Base Path**: `/v1`
- **Method**: `POST`
- **返回类型**: `application/json`

#### 请求参数

| 参数名 | 类型 | 必填 | 位置 | 说明 |
| --- | --- | --- | --- | --- |
| `captcha` | string | 是 | - | 图形验证码 |
| `captcha_id` | string | 是 | - | 验证码ID |
| `password` | string | 是 | - | 用户密码 |
| `username` | string | 是 | - | 用户名 |

#### 请求头

| Header名 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Content-Type` |  | 是 | 请求内容类型 |

#### 请求示例

```
{
  "captcha": "ABCD",
  "captcha_id": "d4224c42-a0a2-4e38-87a5-edc3ad03c014",
  "password": "password123",
  "username": "john_doe"
}
```

#### 响应示例

```
{
  "code": 0,
  "msg": "success",
  "data": {
    "token": "4fxxxxxxxxxxxxxxxxxxxxxxx",
    "expire": 43200
  }
}
```

#### 状态码

- `0` 登录成功
- `400` 请求参数错误
- `401` 用户名或密码错误
- `403` 验证码错误
- `500` 服务器内部错误
