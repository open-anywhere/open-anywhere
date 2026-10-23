<!-- 来源：https://linx.qiniu.com/docs/xrobot/api/device | 由 sync-docs.py 自动生成，请勿手改 -->

# 设备 API

## 1. 获取已绑定设备

### 获取已绑定设备

获取指定智能体下所有已绑定的设备列表，包含设备的详细信息如MAC地址、连接状态、版本信息等。

**`GET`** `https://xrobo.qiniu.com/xiaozhi/device/bind/{agentId}`

#### 基本信息

- **Host**: `https://xrobo.qiniu.com`
- **Base Path**: `/xiaozhi`
- **Method**: `GET`
- **返回类型**: `application/json`

#### 请求参数

| 参数名 | 类型 | 必填 | 位置 | 说明 |
| --- | --- | --- | --- | --- |
| `agentId` | string | 是 | - | 智能体ID，用于指定要查询设备的智能体 |

#### 请求头

| Header名 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Authorization` | string | 是 | Bearer token，用户认证令牌 |

#### 响应示例

```
{
  "code": 0,
  "message": "success",
  "data": [
    {
      "id": "aa:bb:cc:dd:ee:ff",
      "userId": "user123",
      "macAddress": "aa:bb:cc:dd:ee:ff",
      "lastConnectedAt": "2024-01-15T10:30:00Z",
      "autoUpdate": 1,
      "board": "ESP32-S3",
      "alias": "客厅音箱",
      "agentId": "4057d21254664408a2f58200d7ba01e0",
      "appVersion": "1.2.3",
      "sort": 1,
      "updater": "user123",
      "updateDate": "2024-01-15T10:30:00Z",
      "creator": "user123",
      "createDate": "2024-01-10T08:00:00Z"
    }
  ]
}
```

#### 状态码

- `0` 成功获取设备列表
- `401` 未授权，token无效或已过期
- `404` 智能体不存在
- `500` 服务器内部错误

## 2. 解绑设备

### 解绑设备

解除设备与用户账户的绑定关系。解绑后设备将无法接收智能体指令。

**`POST`** `https://xrobo.qiniu.com/xiaozhi/device/unbind`

#### 基本信息

- **Host**: `https://xrobo.qiniu.com`
- **Base Path**: `/xiaozhi`
- **Method**: `POST`
- **返回类型**: `application/json`

#### 请求参数

| 参数名 | 类型 | 必填 | 位置 | 说明 |
| --- | --- | --- | --- | --- |
| `deviceId` | string | 是 | - | 要解绑的设备ID（通常是MAC地址） |

#### 请求头

| Header名 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Authorization` | string | 是 | Bearer token，用户认证令牌 |
| `Content-Type` | string | 是 | application/json |

#### 请求示例

```
{
  "deviceId": "aa:bb:cc:dd:ee:ff"
}
```

#### 响应示例

```
{
  "code": 0,
  "message": "设备解绑成功"
}
```

#### 状态码

- `0` 解绑成功

## 3. 绑定设备

### 绑定设备

使用设备激活码将设备绑定到指定的智能体。绑定成功后设备可以接收该智能体的指令。

**`POST`** `https://xrobo.qiniu.com/xiaozhi/device/bind/{agentId}/{deviceCode}`

#### 基本信息

- **Host**: `https://xrobo.qiniu.com`
- **Base Path**: `/xiaozhi`
- **Method**: `POST`
- **返回类型**: `application/json`

#### 请求参数

| 参数名 | 类型 | 必填 | 位置 | 说明 |
| --- | --- | --- | --- | --- |
| `agentId` | string | 是 | - | 智能体ID，设备将绑定到此智能体 |
| `deviceCode` | string | 是 | - | 设备激活码，用于验证设备身份 |

#### 请求头

| Header名 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Authorization` | string | 是 | Bearer token，用户认证令牌 |

#### 响应示例

```
{
  "code": 0,
  "msg": "success",
  "data": {
    "macAddress": "aa:bb:cc:dd:ee:ff"
  }
}
```

#### 状态码

- `0` 绑定成功
- `400` 设备激活码无效或已使用
- `401` 未授权，token无效或已过期
- `404` 智能体不存在
- `409` 设备已被绑定
- `500` 服务器内部错误

## 4. 批量导入设备（通过MAC地址绑定）

### 批量导入设备（通过MAC地址绑定）

通过MAC地址批量导入设备到指定智能体。支持同时导入多个设备，返回成功和失败的统计信息。

**`POST`** `https://xrobo.qiniu.com/xiaozhi/device/preregister`

#### 基本信息

- **Host**: `https://xrobo.qiniu.com`
- **Base Path**: `/xiaozhi`
- **Method**: `POST`
- **返回类型**: `application/json`

#### 请求参数

| 参数名 | 类型 | 必填 | 位置 | 说明 |
| --- | --- | --- | --- | --- |
| `agent_id` | string | 是 | - | 智能体ID，设备将关联到此智能体 |
| `mac_addresses` | array | 是 | - | MAC地址数组，要导入的设备MAC地址列表 |
| `serial_numbers` | array | 是 | - | 序列号数组，设备序列号列表（可为空） |

#### 请求头

| Header名 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Authorization` | string | 是 | Bearer token，用户认证令牌 |
| `Content-Type` | string | 是 | application/json |

#### 请求示例

```
{
  "agent_id": "4057d21254664408a2f58200d7ba01e0",
  "mac_addresses": [
    "F2:F1:A8:6A:C4:6B"
  ],
  "serial_numbers": []
}
```

#### 响应示例

```
{
  "success_count": 1,
  "failed": []
}

 // 失败示例
{
  "success_count": 0,
  "failed": [
    {
      "value": "F2:F1:A8:6A:C4:6B",
      "reason": "已存在"
    }
  ]
}
```

#### 状态码

- `0` 批量导入完成（可能部分失败）

## 5. 查询预注册设备列表

### 查询预注册设备列表

获取指定智能体下所有预注册设备的列表，支持分页查询。

**`GET`** `https://xrobo.qiniu.com/v1/pre-register/agents/{agent_id}/devices`

#### 基本信息

- **Host**: `https://xrobo.qiniu.com`
- **Base Path**: `/v1/pre-register`
- **Method**: `GET`
- **返回类型**: `application/json`

#### 请求参数

| 参数名 | 类型 | 必填 | 位置 | 说明 |
| --- | --- | --- | --- | --- |
| `agent_id` | string | 是 | - | 智能体ID，用于指定要查询预注册设备的智能体 |
| `cursor` | string | 否 | - | 游标（上一页最后一条记录的 id），为空表示从第一页开始 |
| `limit` | number | 否 | - | 每页条数，默认 100 |

#### 请求头

| Header名 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Authorization` | string | 是 | Bearer token，用户认证令牌 |

#### 响应示例

```
{
  "code": 0,
  "msg": "success",
  "data": {
    "devices": [
      {
        "id": "abc123def456",
        "agent_id": "4f3a8c7e0b6f4b5c9d3d0b8a2a1f0c9d",
        "mac_address": "00:1A:2B:3C:4D:5E",
        "serial_number": "SN123456",
        "status": 0,
        "created_at": "2026-04-03 10:00:00"
      }
    ],
    "next_cursor": "下一页游标"
  }
}
```

#### 状态码

- `0` 成功获取预注册设备列表
- `401` 未授权，token无效或已过期
- `404` 智能体不存在
- `500` 服务器内部错误

## 6. 通过 MAC 地址查询预注册设备

### 通过 MAC 地址查询预注册设备

根据 MAC 地址查询预注册设备的详细信息。

**`GET`** `https://xrobo.qiniu.com/v1/pre-register/devices/mac-address/{mac_address}`

#### 基本信息

- **Host**: `https://xrobo.qiniu.com`
- **Base Path**: `/v1/pre-register`
- **Method**: `GET`
- **返回类型**: `application/json`

#### 请求参数

| 参数名 | 类型 | 必填 | 位置 | 说明 |
| --- | --- | --- | --- | --- |
| `mac_address` | string | 是 | - | MAC 地址（格式：1a:2b:3c:4d:5e:6f） |

#### 请求头

| Header名 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Authorization` | string | 是 | Bearer token，用户认证令牌 |

#### 响应示例

```
{
  "code": 0,
  "msg": "success",
  "data": {
    "id": "abc123def456",
    "agent_id": "4f3a8c7e0b6f4b5c9d3d0b8a2a1f0c9d",
    "mac_address": "00:1A:2B:3C:4D:5E",
    "serial_number": "",
    "status": 0,
    "created_at": "2026-04-03 10:00:00"
  }
}
```

#### 状态码

- `0` 成功获取预注册设备
- `400` 参数格式错误（如 MAC 地址格式不正确）
- `401` 未授权，token无效或已过期
- `404` 设备未找到
- `500` 服务器内部错误

## 7. 删除预注册设备

### 删除预注册设备

根据 MAC 地址删除预注册设备。

**`DELETE`** `https://xrobo.qiniu.com/v1/pre-register/devices/mac-address/{mac_address}`

#### 基本信息

- **Host**: `https://xrobo.qiniu.com`
- **Base Path**: `/v1/pre-register`
- **Method**: `DELETE`
- **返回类型**: `application/json`

#### 请求参数

| 参数名 | 类型 | 必填 | 位置 | 说明 |
| --- | --- | --- | --- | --- |
| `mac_address` | string | 是 | - | MAC 地址（格式：1a:2b:3c:4d:5e:6f） |

#### 请求头

| Header名 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `Authorization` | string | 是 | Bearer token，用户认证令牌 |

#### 响应示例

```
{
  "code": 0,
  "msg": "success",
  "data": {}
}
```

#### 状态码

- `0` 删除成功
- `400` 参数格式错误（如 MAC 地址格式不正确）
- `401` 未授权，token无效或已过期
- `403` 无权限操作该设备
- `404` 设备未找到
- `500` 服务器内部错误

## 设备实体结构

设备对象包含以下字段：

- **id** : 设备唯一标识符（通常是MAC地址）
- **userId** : 关联用户ID
- **macAddress** : 设备MAC地址
- **lastConnectedAt** : 最后连接时间（ISO 8601格式）
- **autoUpdate** : 自动更新开关（0关闭/1开启）
- **board** : 设备硬件型号
- **alias** : 设备别名
- **agentId** : 绑定的智能体ID
- **appVersion** : 固件版本号
- **sort** : 排序值
- **updater** : 更新者ID
- **updateDate** : 更新时间（ISO 8601格式）
- **creator** : 创建者ID
- **createDate** : 创建时间（ISO 8601格式）

## 认证说明

所有设备管理API都需要在请求头中包含有效的Bearer token：

```
Authorization: Bearer {token}
```
