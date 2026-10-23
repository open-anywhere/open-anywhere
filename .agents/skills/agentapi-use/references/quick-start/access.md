<!-- 来源：https://developer-doc.qiniu.com/products/agentapi/quick_start/13585-agentapi-access | 由 sync-docs.py 自动生成，请勿手改 -->

# 接入准备



最近更新时间: 2026-10-16 10:13:03

开始调用 AgentAPI 前，先在七牛 Portal 创建 API Key，再根据所在区域选择服务地址。

## 创建 API Key

1. 打开[七牛 Portal 的 AgentAPI 页面](https://portal.qiniu.com/agentapi)。
2. 在 API Key 设置中创建 Key。Portal 会创建七牛 MaaS（大模型推理服务）的 API Key，并自动绑定到当前 AgentAPI 账户。
3. 将创建的 Key 用于后续 API 请求。同一账户可以保留多个已绑定的 Key，分别用于不同应用。

> 更换 API Key
>
> 先在 Portal 创建新 Key，再替换客户端的 Key；确认切换完成后，再停用或删除旧 Key。创建新 Key 不会覆盖已有绑定。Portal 中删除 Key 会同时删除 MaaS API Key 和 AgentAPI 绑定记录，其他 Key 不受影响。
>
> 停用 MaaS API Key 不会自动解除 AgentAPI 绑定。AgentAPI 鉴权当前仅检查绑定记录，因此不能以停用操作确认旧 Key 已失去 AgentAPI 访问权限；需要撤销访问时，应在 Portal 删除旧 Key。

请勿将真实 Key 写入代码、文档或版本库。本文使用环境变量占位符。

## 选择服务区域

AgentAPI 当前提供两个服务入口：

| 区域 | 服务地址                         |
| -- | ---------------------------- |
| 华东 | `https://agent.qiniuapi.com` |
| 美西 | `https://agent.sufy.com`     |

> 区域数据独立
>
> 不同区域的资源与数据彼此独立，不跨区域共享。在某一区域创建的智能体、环境、会话和文件等资源，应通过该区域的服务地址访问。切换 Portal 地域或 API 服务地址不会迁移已有资源与数据。

通用示例使用 `AGENTAPI_BASE_URL`，请根据所在区域和 Portal 中可用的模型权限设置服务地址。

## 配置环境变量

```bash
export AGENTAPI_BASE_URL="https://your-agentapi-endpoint"
export AGENTAPI_API_KEY="your-maas-api-key"
export AGENTAPI_MODEL_ID="your-model-id"
```

> 选择模型
>
> `AGENTAPI_MODEL_ID` 必须替换为当前账户和区域可用的模型 ID。请前往[七牛 MaaS 模型管理页](https://portal.qiniu.com/ai-inference/model)获取模型 ID，并确认所用 Key 可调用该模型。

## 验证鉴权

```bash
curl --request GET "$AGENTAPI_BASE_URL/v1/agents" \
  --header "x-api-key: $AGENTAPI_API_KEY"
```

请求 JSON 接口时，同时设置 `content-type: application/json`。AgentAPI 使用七牛 MaaS API Key，并通过 `x-api-key` 请求头鉴权。

## 常见问题

| 现象          | 检查项                          |
| ----------- | ---------------------------- |
| `401`       | Key 是否填写正确，是否已在 Portal 绑定    |
| 模型不可用       | 模型是否属于当前账户和区域，是否具备使用权限       |
| 请求地址错误      | `AGENTAPI_BASE_URL` 是否对应所选区域 |
| 更换 Key 后仍失败 | 新 Key 是否已绑定，是否处于启用状态         |

准备完成后，继续阅读[通过 API 构建](quickstart.md)。
