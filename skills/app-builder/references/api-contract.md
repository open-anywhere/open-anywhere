# OpenAPI 与业务响应契约

`openapi.yaml` 用于声明应用可调用的业务操作。下面的猜电影操作来自模板，仅用于说明写法；实际应用按需求替换路径、`operationId` 和 schema。

## 文档要求

- 必须是 OpenAPI 3.1 文档，`openapi` 字段以 `3.1` 开头。
- `paths` 非空，且每个路径都以 `/api/` 开头；`/`、`/health`、`/openapi.json` 和 SSE 通道不写入 `paths`。
- `servers` 使用相对地址，例如 `[{url: "/"}]`。不要写入具体主机或端口。
- 每个操作必须有唯一、稳定、语义化的 `operationId`；重复会导致产物校验失败。
- 每个操作提供调用者能理解的 `summary`、`description`、参数和请求体 schema。
- 响应使用 JSON，并声明实际会返回的结构。

## 业务响应

- 成功和错误响应都返回 JSON 对象，并包含必需的字符串 `message`。
- `message` 使用简短、面向用户的中文，作为语音播报候选，例如“已收到回答”“小明已准备”。
- 错误时也返回 JSON `message`，不要返回纯文本或 HTML。
- 建议使用明确的 HTTP 状态码：成功 200，参数错误 400，业务冲突 409，未找到 404。
- `/health`、`/openapi.json` 和 SSE 事件不受 `message` 约束。

示例：

```yaml
openapi: 3.1.0
info:
  title: Guess the Movie
  version: 1.0.0
servers:
  - url: /
paths:
  /api/guess:
    post:
      operationId: submitGuess
      summary: 提交电影名猜测
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [user_name, guess]
              additionalProperties: false
              properties:
                user_name:
                  type: string
                guess:
                  type: string
      responses:
        "200":
          description: 猜测已记录
          content:
            application/json:
              schema:
                type: object
                required: [message, correct]
                properties:
                  message:
                    type: string
                  correct:
                    type: boolean
        "400":
          description: 参数错误
          content:
            application/json:
              schema:
                type: object
                required: [message]
                properties:
                  message:
                    type: string
```

完整的状态、加入和猜测响应字段见模板的 `openapi.yaml`。

## 参数与请求体

- 路径参数写在路径模板中，例如 `/api/players/{user_name}`，并在 `parameters` 中声明 `in: path` 和 `required: true`。
- 查询参数声明 `in: query` 和 schema。查询参数只支持字符串、数字、布尔值；复杂结构放在 JSON 请求体中。
- `requestBody` 使用 `application/json`，声明 `required`、`schema` 和 `additionalProperties: false`（除非确实需要额外字段）。
- 数组和对象可以放在请求体；不要把复杂 JSON 塞进查询参数。
- 业务接口不要依赖认证头、Cookie 或登录状态；身份通过 `user_name` 等业务字段传入。

## 玩家身份

- 一期用 `user_name` 同时表示玩家身份和展示名称，不生成 `user_id`，也不维护名字到 ID 的映射。
- 构建消息提供初始名单时，按顺序建立玩家，并在玩家或状态接口中返回 `user_name`。
- 所有入口去除名字首尾空白；空白名字、重复名字拒绝；按名字精确匹配，不做模糊匹配或同音替换。
- 本局内名字保持不变；改名和跨重启持久化不在 MVP 范围内。
- 初始玩家加入后只具有初始状态；不要伪造进度或分数。未登记名字不能直接参与游戏或计分。

## UI 与 API 共享状态

- 页面和业务 API 必须读写同一份内存状态。
- 外部 HTTP 操作改变状态后，已打开的页面要自动更新；MVP 可以用 2 秒左右的轮询，也可以用 SSE。
- 页面操作也应调用同一套 `/api/*` 接口，不要在前端维护另一份状态。
- 不要依赖外部 CDN 或在线资源；页面在离线环境中也能加载。
