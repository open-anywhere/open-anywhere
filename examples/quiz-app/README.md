# Open Anywhere Quiz App

这是一个可以单独运行的 Web 应用：Node 提供 HTTP API 和 HTML 页面，页面通过 SSE 订阅状态。应用对外只使用通用 `user_id`，调用方可以使用声纹、账号、按钮或其他方式得到这个 ID。

## 启动

```bash
cd examples/quiz-app
node server.js
```

然后打开 <http://127.0.0.1:38120/>。

OpenAPI 文档位于：<http://127.0.0.1:38120/openapi.yaml>。

## 用 curl / Postman 调用

先让三位玩家就位：

```bash
curl -X POST http://127.0.0.1:38120/api/sessions/quiz-session-001/players/p1/ready
curl -X POST http://127.0.0.1:38120/api/sessions/quiz-session-001/players/p2/ready
curl -X POST http://127.0.0.1:38120/api/sessions/quiz-session-001/players/p3/ready
```

开始游戏：

```bash
curl -X POST http://127.0.0.1:38120/api/sessions/quiz-session-001/start
```

提交回答：

```bash
curl -X POST http://127.0.0.1:38120/api/sessions/quiz-session-001/answers \
  -H 'Content-Type: application/json' \
  -d '{"user_id":"u2","transcript":"火车进站"}'
```

再使用返回的 `answer_id` 提交判断：

```bash
curl -X POST http://127.0.0.1:38120/api/sessions/quiz-session-001/answers/answer-001/evaluate \
  -H 'Content-Type: application/json' \
  -d '{"answer_right":true,"reason":"回答正确"}'
```

浏览器页面会通过 `/api/sessions/quiz-session-001/events` 立即显示外部请求带来的玩家、题目和比分变化。
