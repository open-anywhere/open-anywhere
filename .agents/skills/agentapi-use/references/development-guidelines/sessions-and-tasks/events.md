<!-- 来源：https://developer-doc.qiniu.com/products/agentapi/development_guidelines/agentapi-sessions-and-tasks/13597-agentapi-sessions-and-tasks-events | 由 sync-docs.py 自动生成，请勿手改 -->

# 事件和事件流



最近更新时间: 2026-10-22 15:08:10

应用提交任务后，需要持续接收回答、工具调用和状态变化，才能判断云端执行的结果。AgentAPI 将这些记录保存在会话中，应用可以通过 SSE 实时消费，也可以查询历史。

涉及通知、写库或其他外部副作用时，应用应在自己的事务中同时记录业务状态和消费进度，并为可重试操作设置业务幂等键。

无需持续保持 SSE 连接的资源变更或会话状态通知，可在 Portal 配置 [Webhook](../data-and-integrations/webhooks.md)。Webhook 不转发所有会话事件；收到通知后仍需读取停止原因、回答和产物，不能把会话空闲视为任务成功。

## 执行结果判断

应用收到 `agent.message` 时可以展示回答；收到 `session.status_idle` 时，才知道本轮已停止。停止不一定代表任务完成，需要根据 `stop_reason` 决定下一步：

| 停止原因                | 应用的下一步                                         |
| ------------------- | ---------------------------------------------- |
| `end_turn`          | 检查最终回答和任务产物，确认满足业务要求。                          |
| `requires_action`   | 按 `stop_reason.event_ids` 找到阻塞事件，处理自定义工具或工具确认。 |
| `interrupted`       | 记录中断，检查已完成的操作；中断不会撤销外部修改。                      |
| `budget_reached`    | 检查预算和部分产物，再决定是否继续。                             |
| `retries_exhausted` | 读取错误信息，解决问题后再发送输入。                             |

> 状态与停止原因
>
> `native` 的人工中断也使用 `end_turn`，需结合 `user.interrupt` 事件判断。`session.error` 本身不代表会话已终止，应继续检查后续状态；进入 `terminated` 的会话不能继续接收任务输入。

## 建立事件消费循环

### 事件接入方式

创建[会话](session.md)后，保存会话 ID，并用它完成以下操作。请求路径均以 `/v1/sessions/{id}` 为前缀。

| 操作     | 请求方法与路径后缀            | 使用场景                                        |
| ------ | -------------------- | ------------------------------------------- |
| 发送事件   | `POST /events`       | 提交用户消息、中断、工具结果或确认。请求返回表示事件已接收，任务结果需从后续事件读取。 |
| 订阅事件流  | `GET /events/stream` | 通过 SSE 展示执行进度。连接先回放历史，再推送实时事件。              |
| 读取事件历史 | `GET /events`        | 分页读取已保存的记录，用于查询、审计或恢复应用状态。                  |

消费循环应先订阅事件流，再发送用户消息，并持续处理回答和状态变化。应用重启或连接中断后，按回放和去重恢复消费。

### 订阅回答和执行进度

消费端先打开 SSE 连接，再提交输入。事件流按顺序回放历史并继续推送新事件；收到 `session.status_idle` 后，连接仍保持打开，应用根据执行结果决定后续操作。

完整的 `agent.message` 用于展示回答。若 Native 应用需要提前展示生成中的文字，应在发送输入前订阅增量预览；这类预览不写入历史，重连后不能补回。

Native 会话可通过 `event_deltas[]=agent.message` 订阅当前模型请求的文本预览，其他运行时忽略此参数：

* 正常结束：依次收到 `event_start`、若干 `event_delta` 和同一 ID 的完整 `agent.message`。
* 请求失败或中断：不返回完整消息，以 `span.model_request_end` 结束预览。
* 增量仅对当前连接有效，不写入历史，按模型输出尽力推送，不保证逐字符或逐 token。

例如，客户端可能收到以下 SSE 帧（`event_delta` 的数量和每段文本长度由模型服务决定）：

```text
event: event_start
data: {"type":"event_start","event":{"type":"agent.message","id":"evt_..."}}

event: event_delta
data: {"type":"event_delta","event_id":"evt_...","delta":{"type":"content_delta","index":0,"content":{"type":"text","text":"你"}}}

event: event_delta
data: {"type":"event_delta","event_id":"evt_...","delta":{"type":"content_delta","index":0,"content":{"type":"text","text":"好"}}}

event: agent.message
data: {"type":"agent.message","id":"evt_...","content":[{"type":"text","text":"你好"}]}
```

> 单轮示例的适用范围
>
> 下面的示例用于新建会话的一轮执行，在收到 `session.status_idle` 或错误时退出。已有会话的流会先回放历史，其中的旧 `idle` 不代表新一轮已经结束。持续运行的应用应使用回放和去重示例，并按停止原因处理工具等待和错误，不能仅凭 `idle` 判定任务成功。

cURL

```bash
# 先打开流，再发送用户消息
exec {stream}< <(
  curl --fail-with-body -sS -N \
    "$AGENTAPI_BASE_URL/v1/sessions/$SESSION_ID/events/stream?event_deltas%5B%5D=agent.message" \
    -H "x-api-key: $AGENTAPI_API_KEY" \
    -H "content-type: application/json" \
    -H "accept: text/event-stream"
)

curl --fail-with-body -sS \
  "$AGENTAPI_BASE_URL/v1/sessions/$SESSION_ID/events" \
  -H "x-api-key: $AGENTAPI_API_KEY" \
  -H "content-type: application/json" \
  -d @- >/dev/null <<'EOF'
{
  "events": [
    {
      "type": "user.message",
      "input": {"parts": [{"type": "text", "text": "Summarize the repo README"}]}
    }
  ]
}
EOF

while IFS= read -r -u "$stream" event_line; do
  [[ $event_line == data:* ]] || continue
  event_json=${event_line#data: }
  case $(jq -r '.type' <<<"$event_json") in
    agent.message)
      jq -j '.content[] | select(.type == "text") | .text' <<<"$event_json"
      ;;
    session.status_idle)
      break
      ;;
    session.error)
      printf '\n[Error: %s]\n' "$(jq -r '.error.message // "unknown"' <<<"$event_json")"
      break
      ;;
  esac
done
exec {stream}<&-
```

Python

```python
# 先打开流，再发送用户消息
with client.beta.sessions.events.stream(session.id) as stream:
    client.beta.sessions.events.send(
        session.id,
        events=[
            {
                "type": "user.message",
                "content": [{"type": "text", "text": "Summarize the repo README"}],
            },
        ],
    )

    for event in stream:
        match event.type:
            case "agent.message":
                for block in event.content:
                    if block.type == "text":
                        print(block.text, end="")
            case "session.status_idle":
                break
            case "session.error":
                error_message = event.error.message if event.error else "unknown"
                print(f"\n[Error: {error_message}]")
                break
```

TypeScript

````typescript
// 先打开流，再发送用户消息
const stream = await client.beta.sessions.events.stream(session.id);
await client.beta.sessions.events.send(session.id, {
  events: [
    {
      type: "user.message",
      content: [{ type: "text", text: "Summarize the repo README" }]
    }
  ]
});

for await (const event of stream) {
  if (event.type === "agent.message") {
    for (const block of event.content) {
      if (block.type === "text") {
        process.stdout.write(block.text);
      }
    }
  } else if (event.type === "session.status_idle") {
    break;
  } else if (event.type === "session.error") {
    console.log(`\n[Error: ${event.error?.message ?? "unknown"}]`);
    break;
  }
}
```csharp
```csharp
// 先打开流，然后再发送用户消息
using var stream = await client.Beta.Sessions.Events.WithRawResponse.StreamStreaming(session.ID);
await client.Beta.Sessions.Events.Send(session.ID, new()
{
    Events =
    [
        new BetaManagedAgentsUserMessageEventParams
        {
            Type = BetaManagedAgentsUserMessageEventParamsType.UserMessage,
            Content =
            [
                new BetaManagedAgentsTextBlock
                {
                    Type = BetaManagedAgentsTextBlockType.Text,
                    Text = "Summarize the repo README",
                },
            ],
        },
    ],
});

await foreach (var streamEvent in stream.Enumerate())
{
    if (streamEvent.Value is BetaManagedAgentsAgentMessageEvent message)
    {
        foreach (var block in message.Content)
        {
            Console.Write(block.Text);
        }
    }
    else if (streamEvent.Value is BetaManagedAgentsSessionStatusIdleEvent)
    {
        break;
    }
    else if (streamEvent.Value is BetaManagedAgentsSessionErrorEvent error)
    {
        Console.WriteLine($"\n[Error: {error.Error?.Message ?? "unknown"}]");
        break;
    }
}
````

Go

```go
	// 先打开流，然后发送用户消息
	stream := client.Beta.Sessions.Events.StreamEvents(ctx, session.ID, anthropic.BetaSessionEventStreamParams{})
	defer stream.Close()

	if _, err := client.Beta.Sessions.Events.Send(ctx, session.ID, anthropic.BetaSessionEventSendParams{
		Events: []anthropic.BetaManagedAgentsEventParamsUnion{{
			OfUserMessage: &anthropic.BetaManagedAgentsUserMessageEventParams{
				Type: anthropic.BetaManagedAgentsUserMessageEventParamsTypeUserMessage,
				Content: []anthropic.BetaManagedAgentsUserMessageEventParamsContentUnion{{
					OfText: &anthropic.BetaManagedAgentsTextBlockParam{
						Type: anthropic.BetaManagedAgentsTextBlockTypeText,
						Text: "Summarize the repo README",
					},
				}},
			},
		}},
	}); err != nil {
		panic(err)
	}

events:
	for stream.Next() {
		switch event := stream.Current().AsAny().(type) {
		case anthropic.BetaManagedAgentsAgentMessageEvent:
			// 具体类型列表：BetaManagedAgentsTextBlock
			for _, block := range event.Content {
				fmt.Print(block.Text)
			}
		case anthropic.BetaManagedAgentsSessionStatusIdleEvent:
			break events
		case anthropic.BetaManagedAgentsSessionErrorEvent:
			fmt.Printf("\n[Error: %s]\n", cmp.Or(event.Error.Message, "unknown"))
			break events
		}
	}
	if err := stream.Err(); err != nil {
		panic(err)
	}
```

Java

```java
// 先打开流，然后发送用户消息
try (var stream = client.beta().sessions().events().streamStreaming(session.id())) {
    client.beta().sessions().events().send(
        session.id(),
        EventSendParams.builder()
            .addEvent(BetaManagedAgentsUserMessageEventParams.builder()
                .type(BetaManagedAgentsUserMessageEventParams.Type.USER_MESSAGE)
                .addTextContent("Summarize the repo README")
                .build())
            .build()
    );

    Iterable<BetaManagedAgentsStreamSessionEvents> events = stream.stream()::iterator;
    for (var event : events) {
        if (event.isAgentMessage()) {
            event.asAgentMessage().content().forEach(block -> block.text().ifPresent(textBlock -> IO.print(textBlock.text())));
        } else if (event.isSessionStatusIdle()) {
            break;
        } else if (event.isSessionError()) {
            // `message` 字段在所有错误变体中都存在；从原始 JSON 中读取它。
            var errorMessage =
                event.asSessionError().error()._json().orElse(null) instanceof JsonObject json
                    ? json.values().get("message").asStringOrThrow()
                    : "unknown";
            IO.println("\n[Error: " + errorMessage + "]");
            break;
        }
    }
}
```

PHP

```php
// 先打开流，再发送用户消息
$stream = $client->beta->sessions->events->streamStream($session->id);
$client->beta->sessions->events->send(
    $session->id,
    events: [
        [
            'type' => 'user.message',
            'content' => [['type' => 'text', 'text' => 'Summarize the repo README']],
        ],
    ],
);

foreach ($stream as $event) {
    match ($event->type) {
        'agent.message' => array_walk(
            $event->content,
            static fn ($block) => $block->type === 'text' ? print($block->text) : null,
        ),
        'session.error' => printf("\n[Error: %s]", $event->error?->message ?? 'unknown'),
        default => null,
    };
    if ($event->type === 'session.status_idle' || $event->type === 'session.error') {
        break;
    }
}
$stream->close();
```

Ruby

```ruby
# 先打开流，再发送用户消息
stream = client.beta.sessions.events.stream_events(session.id)

client.beta.sessions.events.send_(
  session.id,
  events: [{
    type: "user.message",
    content: [{type: "text", text: "Summarize the repo README"}]
  }]
)

stream.each do |event|
  case event.type
  in :"agent.message"
    event.content.each { print it.text }
  in :"session.status_idle"
    break
  in :"session.error"
    puts "\n[Error: #{event.error&.message || "unknown"}]"
    break
  else
    # 忽略其他事件类型
  end
end
```

### 提交任务输入

订阅建立后，发送 `user.message` 启动执行。下面提交文件性能分析任务；请求返回仅表示输入已接收，回答和执行结果仍需从事件流读取。

cURL

```bash
curl --fail-with-body -sS "$AGENTAPI_BASE_URL/v1/sessions/$SESSION_ID/events" \
  -H "x-api-key: $AGENTAPI_API_KEY" \
  -H "content-type: application/json" \
  -d @- <<'EOF'
{
  "events": [
    {
      "type": "user.message",
      "input": {"parts": [
        {"type": "text", "text": "Analyze the performance of the sort function in utils.py"}
      ]}
    }
  ]
}
EOF
```

Python

```python
client.beta.sessions.events.send(
    session.id,
    events=[
        {
            "type": "user.message",
            "content": [
                {
                    "type": "text",
                    "text": "Analyze the performance of the sort function in utils.py",
                },
            ],
        },
    ],
)
```

TypeScript

````typescript
await client.beta.sessions.events.send(session.id, {
  events: [
    {
      type: "user.message",
      content: [
        {
          type: "text",
          text: "Analyze the performance of the sort function in utils.py",
        },
      ],
    },
  ],
});
```csharp
```csharp
await client.Beta.Sessions.Events.Send(session.ID, new()
{
    Events =
    [
        new BetaManagedAgentsUserMessageEventParams
        {
            Type = BetaManagedAgentsUserMessageEventParamsType.UserMessage,
            Content =
            [
                new BetaManagedAgentsTextBlock
                {
                    Type = BetaManagedAgentsTextBlockType.Text,
                    Text = "Analyze the performance of the sort function in utils.py",
                },
            ],
        },
    ],
});
````

Go

```go
if _, err := client.Beta.Sessions.Events.Send(ctx, session.ID, anthropic.BetaSessionEventSendParams{
	Events: []anthropic.BetaManagedAgentsEventParamsUnion{{
		OfUserMessage: &anthropic.BetaManagedAgentsUserMessageEventParams{
			Type: anthropic.BetaManagedAgentsUserMessageEventParamsTypeUserMessage,
			Content: []anthropic.BetaManagedAgentsUserMessageEventParamsContentUnion{{
				OfText: &anthropic.BetaManagedAgentsTextBlockParam{
					Type: anthropic.BetaManagedAgentsTextBlockTypeText,
					Text: "Analyze the performance of the sort function in utils.py",
				},
			}},
		},
	}},
}); err != nil {
	panic(err)
}
```

Java

```java
client.beta().sessions().events().send(
    session.id(),
    EventSendParams.builder()
        .addEvent(BetaManagedAgentsUserMessageEventParams.builder()
            .type(BetaManagedAgentsUserMessageEventParams.Type.USER_MESSAGE)
            .addTextContent("Analyze the performance of the sort function in utils.py")
            .build())
        .build());
```

PHP

```php
$client->beta->sessions->events->send(
    $session->id,
    events: [
        [
            'type' => 'user.message',
            'content' => [
                [
                    'type' => 'text',
                    'text' => 'Analyze the performance of the sort function in utils.py',
                ],
            ],
        ],
    ],
);
```

Ruby

```ruby
client.beta.sessions.events.send_(
  session.id,
  events: [
    {
      type: "user.message",
      content: [
        {
          type: "text",
          text: "Analyze the performance of the sort function in utils.py"
        }
      ]
    }
  ]
)
```

## 恢复事件消费

页面刷新、网络断开或应用重启后，重新订阅同一个会话。服务端会回放历史，应用需要保存已处理的事件 ID，避免重复展示回答或重复触发业务处理：

1. 打开一个新流。
2. 依次消费服务端先回放的历史事件和随后到达的实时事件。
3. 对每个事件按 ID 去重，再交给业务逻辑；不要仅因回放历史中的 `session.status_idle` 而关闭连接。

下面的 `loadSeenEventIds`、`persistSeenEventId`（以及其他语言的对应函数）需由应用实现。示例持续消费回放和实时事件；页面关闭、会话终止或其他业务条件满足时，由应用关闭连接。

> 业务处理与消费进度
>
> 下面的展示去重示例在处理前记录 ID，崩溃时可能遗漏展示。涉及通知、写库或其他副作用时，应在应用自己的事务中同时提交业务状态、消费进度和待发送通知，并使用业务幂等键处理重试。

cURL

```bash
exec {stream}< <(
  curl --fail-with-body -sS -N \
    "$AGENTAPI_BASE_URL/v1/sessions/$SESSION_ID/events/stream" \
    -H "x-api-key: $AGENTAPI_API_KEY" \
  -H "content-type: application/json" \
  -H "accept: text/event-stream"
)

# 从应用自己的持久化状态加载已经交给业务逻辑的事件 ID。
declare -A seen_event_ids

# SSE 会先回放历史，再继续推送实时事件。
while IFS= read -r -u "$stream" event_line; do
  [[ $event_line == data:* ]] || continue
  event_json=${event_line#data: }
  event_id=$(jq -r '.id' <<<"$event_json")
  [[ -n ${seen_event_ids[$event_id]+seen} ]] && continue
  seen_event_ids[$event_id]=1
  case $(jq -r '.type' <<<"$event_json") in
    agent.message)
      jq -j '.content[] | select(.type == "text") | .text' <<<"$event_json"
      ;;
    session.status_idle)
      # 历史事件中的 idle 不代表新一轮已完成；连接继续等待。
      ;;
  esac
done
exec {stream}<&-
```

Python

```python
seen_event_ids = load_seen_event_ids(session.id)  # 从应用存储恢复
with client.beta.sessions.events.stream(session.id) as stream:
    # 服务端会先回放历史，然后继续推送实时事件。
    for event in stream:
        if event.id in seen_event_ids:
            continue
        seen_event_ids.add(event.id)
        persist_seen_event_id(session.id, event.id)
        match event.type:
            case "agent.message":
                for block in event.content:
                    if block.type == "text":
                        print(block.text, end="")
```

TypeScript

````typescript
const seenEventIds = await loadSeenEventIds(session.id); // 应用自己的持久化状态
const stream = await client.beta.sessions.events.stream(session.id);

// 服务端会先回放历史，然后继续推送实时事件。
for await (const event of stream) {
  if (seenEventIds.has(event.id)) continue;
  seenEventIds.add(event.id);
  await persistSeenEventId(session.id, event.id);
  if (event.type === "agent.message") {
    for (const block of event.content) {
      if (block.type === "text") {
        process.stdout.write(block.text);
      }
    }
  }
}
```csharp
```csharp
using var stream = await client.Beta.Sessions.Events.WithRawResponse.StreamStreaming(session.ID);

// 从应用自己的持久化状态加载已处理的事件 ID。
HashSet<string> seenEventIds = await LoadSeenEventIds(session.ID);

// 服务端会先回放历史，然后继续推送实时事件。
await foreach (var streamEvent in stream.Enumerate())
{
    if (!seenEventIds.Add(streamEvent.ID))
    {
        continue;
    }
    await PersistSeenEventId(session.ID, streamEvent.ID);
    if (streamEvent.Value is BetaManagedAgentsAgentMessageEvent message)
    {
        foreach (var block in message.Content)
        {
            Console.Write(block.Text);
        }
    }
}
````

Go

```go
	stream := client.Beta.Sessions.Events.StreamEvents(ctx, session.ID, anthropic.BetaSessionEventStreamParams{})
	defer stream.Close()

	// 从应用自己的持久化状态加载已处理的事件 ID。
	seenEventIDs := loadSeenEventIDs(session.ID)

	// 服务端会先回放历史，然后继续推送实时事件。
	for stream.Next() {
		event := stream.Current()
		if _, seen := seenEventIDs[event.ID]; seen {
			continue
		}
		seenEventIDs[event.ID] = struct{}{}
		persistSeenEventID(session.ID, event.ID)
		switch event := event.AsAny().(type) {
		case anthropic.BetaManagedAgentsAgentMessageEvent:
			// 具体类型列表：BetaManagedAgentsTextBlock
			for _, block := range event.Content {
				fmt.Print(block.Text)
			}
		}
	}
	if err := stream.Err(); err != nil {
		panic(err)
	}
```

Java

```java
try (var stream = client.beta().sessions().events().streamStreaming(session.id())) {
    // 从应用自己的持久化状态加载已处理的事件 ID。
    var seenEventIds = loadSeenEventIds(session.id());

    // 服务端会先回放历史，然后继续推送实时事件。
    stream.stream()
        .filter(event -> event._json().orElseThrow() instanceof JsonObject json
            && seenEventIds.add(json.values().get("id").asStringOrThrow()))
        .filter(BetaManagedAgentsStreamSessionEvents::isAgentMessage)
        .forEach(event -> event.asAgentMessage().content()
            .forEach(block -> block.text().ifPresent(textBlock -> IO.print(textBlock.text()))));
}
```

PHP

```php
$stream = $client->beta->sessions->events->streamStream($session->id);

// 从应用自己的持久化状态加载已处理的事件 ID。
$seenEventIds = loadSeenEventIds($session->id);

// 服务端会先回放历史，然后继续推送实时事件。
foreach ($stream as $event) {
    if (isset($seenEventIds[$event->id])) {
        continue;
    }
    $seenEventIds[$event->id] = true;
    persistSeenEventId($session->id, $event->id);
    match ($event->type) {
        'agent.message' => array_walk(
            $event->content,
            static fn ($block) => $block->type === 'text' ? print($block->text) : null,
        ),
        default => null,
    };
}
$stream->close();
```

Ruby

```ruby
stream = client.beta.sessions.events.stream_events(session.id)

# 从应用自己的持久化状态加载已处理的事件 ID。
seen_event_ids = load_seen_event_ids(session.id)

# 服务端会先回放历史，然后继续推送实时事件。
stream.each do |event|
  next unless seen_event_ids.add?(event.id)
  persist_seen_event_id(session.id, event.id)
  case event.type
  in :"agent.message"
    event.content.each { print it.text }
  else
    # 忽略其他事件类型
  end
end
```

### 查询历史事件

需要重建对话记录或排查已结束的任务时，使用事件列表接口。列表默认分页：以下 cURL 示例持续读取 `next_page`，直到取完历史；SDK 应使用自动分页迭代器，或将 `next_page` 作为下一次请求的 `page` 参数。

cURL

```bash
page=""
while :; do
  url="$AGENTAPI_BASE_URL/v1/sessions/$SESSION_ID/events"
  [[ -z "$page" ]] || url+="?page=$(printf '%s' "$page" | jq -sRr @uri)"
  response=$(curl --fail-with-body -sS "$url" \
    -H "x-api-key: $AGENTAPI_API_KEY" \
    -H "content-type: application/json")
  jq -r '.data[] | "[\(.type)] \(.processed_at)"' <<<"$response"
  page=$(jq -r '.next_page // empty' <<<"$response")
  [[ -n "$page" ]] || break
done
```

Python

```python
events = client.beta.sessions.events.list(session.id)
for event in events.data:
    print(f"[{event.type}] {event.processed_at}")
```

TypeScript

````typescript
const events = await client.beta.sessions.events.list(session.id);
for (const event of events.data) {
  console.log(`[${event.type}] ${event.processed_at}`);
}
```csharp
```csharp
var events = await client.Beta.Sessions.Events.List(session.ID);
foreach (var sessionEvent in events.Items)
{
    Console.WriteLine($"[{sessionEvent.Json.GetProperty("type").GetString()}] {sessionEvent.ProcessedAt}");
}
````

Go

```go
events, err := client.Beta.Sessions.Events.List(ctx, session.ID, anthropic.BetaSessionEventListParams{})
if err != nil {
	panic(err)
}
for _, event := range events.Data {
	fmt.Printf("[%s] %s\n", event.Type, event.ProcessedAt)
}
```

Java

```java
var events = client.beta().sessions().events().list(session.id());
for (var event : events.data()) {
    var eventJson = event._json().orElseThrow().convert(JsonNode.class);
    var processedAt = eventJson.path("processed_at");
    IO.println("[" + eventJson.get("type").asText() + "] "
        + (processedAt.isTextual() ? processedAt.asText() : "null"));
}
```

PHP

```php
$events = $client->beta->sessions->events->list($session->id);
foreach ($events->data as $event) {
    $processedAt = ($event->processedAt ?? null)?->format(DATE_RFC3339) ?? 'null';
    echo "[{$event->type}] {$processedAt}\n";
}
```

Ruby

```ruby
events = client.beta.sessions.events.list(session.id)
events.data.each { puts "[#{it.type}] #{it.processed_at}" }
```

上述 SDK 片段仅展示当前页响应。读取全部历史时，需要继续按 `next_page` 分页请求；单次 `list` 调用不保证返回完整记录。

传入 `types` 过滤器，即可只返回指定类型的事件：

cURL

```bash
curl --fail-with-body -sS "$AGENTAPI_BASE_URL/v1/sessions/$SESSION_ID/events?types[]=agent.tool_use&types[]=agent.tool_result" \
  -H "x-api-key: $AGENTAPI_API_KEY" \
  | jq -r '.data[] | "[\(.type)] \(.processed_at)"'
```

Python

```python
events = client.beta.sessions.events.list(
    session.id,
    types=["agent.tool_use", "agent.tool_result"],
)
for event in events.data:
    print(f"[{event.type}] {event.processed_at}")
```

TypeScript

````typescript
const events = await client.beta.sessions.events.list(session.id, {
  types: ["agent.tool_use", "agent.tool_result"],
});
for (const event of events.data) {
  console.log(`[${event.type}] ${event.processed_at}`);
}
```csharp
```csharp
var events = await client.Beta.Sessions.Events.List(session.ID, new()
{
    Types = ["agent.tool_use", "agent.tool_result"],
});
foreach (var sessionEvent in events.Items)
{
    Console.WriteLine($"[{sessionEvent.Json.GetProperty("type").GetString()}] {sessionEvent.ProcessedAt}");
}
````

Go

```go
events, err := client.Beta.Sessions.Events.List(ctx, session.ID, anthropic.BetaSessionEventListParams{
	Types: []string{"agent.tool_use", "agent.tool_result"},
})
if err != nil {
	panic(err)
}
for _, event := range events.Data {
	fmt.Printf("[%s] %s\n", event.Type, event.ProcessedAt)
}
```

Java

```java
var events = client.beta().sessions().events().list(
    session.id(),
    EventListParams.builder()
        .addType("agent.tool_use")
        .addType("agent.tool_result")
        .build());
for (var event : events.data()) {
    event.agentToolUse().ifPresent(toolUse ->
        IO.println("[" + toolUse.type() + "] " + toolUse.processedAt()));
    event.agentToolResult().ifPresent(toolResult ->
        IO.println("[" + toolResult.type() + "] " + toolResult.processedAt()));
}
```

PHP

```php
// 在 PHP 中，通过 EventListParams 传入需要的事件类型。
```

Ruby

```ruby
events = client.beta.sessions.events.list(
  session.id,
  types: ["agent.tool_use", "agent.tool_result"]
)
events.data.each { puts "[#{it.type}] #{it.processed_at}" }
```

## 调整执行中的任务

任务目标发生变化时，先用 `user.interrupt` 停止当前执行，再提交新要求。以下示例将文件分析改为修复指定位置的问题，合并发送中断和新消息的方式仅适用于 AgentAPI 托管的 `native`；其他运行时按下方警告分两步发送。

cURL

```bash
# 智能体当前正在分析文件...
# 用新的指令中断：
curl --fail-with-body -sS "$AGENTAPI_BASE_URL/v1/sessions/$SESSION_ID/events" \
  -H "x-api-key: $AGENTAPI_API_KEY" \
  -H "content-type: application/json" \
  -d @- <<'EOF'
{
  "events": [
    {"type": "user.interrupt"},
    {
      "type": "user.message",
      "input": {"parts": [
        {"type": "text", "text": "Instead, focus on fixing the bug in line 42."}
      ]}
    }
  ]
}
EOF
```

Python

```python
# 智能体当前正在分析文件...
# 用新的指令中断：
client.beta.sessions.events.send(
    session.id,
    events=[
        {"type": "user.interrupt"},
        {
            "type": "user.message",
            "content": [
                {
                    "type": "text",
                    "text": "Instead, focus on fixing the bug in line 42.",
                },
            ],
        },
    ],
)
```

TypeScript

````typescript
// 智能体当前正在分析文件……
// 用新的指令中断：
await client.beta.sessions.events.send(session.id, {
  events: [
    { type: "user.interrupt" },
    {
      type: "user.message",
      content: [
        {
          type: "text",
          text: "Instead, focus on fixing the bug in line 42.",
        },
      ],
    },
  ],
});
```csharp
```csharp
// 智能体当前正在分析文件...
// 用新的指令进行中断：
await client.Beta.Sessions.Events.Send(session.ID, new()
{
    Events =
    [
        new BetaManagedAgentsUserInterruptEventParams
        {
            Type = BetaManagedAgentsUserInterruptEventParamsType.UserInterrupt,
        },
        new BetaManagedAgentsUserMessageEventParams
        {
            Type = BetaManagedAgentsUserMessageEventParamsType.UserMessage,
            Content =
            [
                new BetaManagedAgentsTextBlock
                {
                    Type = BetaManagedAgentsTextBlockType.Text,
                    Text = "Instead, focus on fixing the bug in line 42.",
                },
            ],
        },
    ],
});
````

Go

```go
// 智能体当前正在分析文件...
// 以新的指示中断：
if _, err := client.Beta.Sessions.Events.Send(ctx, session.ID, anthropic.BetaSessionEventSendParams{
	Events: []anthropic.BetaManagedAgentsEventParamsUnion{
		{
			OfUserInterrupt: &anthropic.BetaManagedAgentsUserInterruptEventParams{
				Type: anthropic.BetaManagedAgentsUserInterruptEventParamsTypeUserInterrupt,
			},
		},
		{
			OfUserMessage: &anthropic.BetaManagedAgentsUserMessageEventParams{
				Type: anthropic.BetaManagedAgentsUserMessageEventParamsTypeUserMessage,
				Content: []anthropic.BetaManagedAgentsUserMessageEventParamsContentUnion{{
					OfText: &anthropic.BetaManagedAgentsTextBlockParam{
						Type: anthropic.BetaManagedAgentsTextBlockTypeText,
						Text: "Instead, focus on fixing the bug in line 42.",
					},
				}},
			},
		},
	},
}); err != nil {
	panic(err)
}
```

Java

```java
// 智能体当前正在分析文件……
// 以新的指示进行中断：
client.beta().sessions().events().send(
    session.id(),
    EventSendParams.builder()
        .addEvent(BetaManagedAgentsUserInterruptEventParams.builder()
            .type(BetaManagedAgentsUserInterruptEventParams.Type.USER_INTERRUPT)
            .build())
        .addEvent(BetaManagedAgentsUserMessageEventParams.builder()
            .type(BetaManagedAgentsUserMessageEventParams.Type.USER_MESSAGE)
            .addTextContent("Instead, focus on fixing the bug in line 42.")
            .build())
        .build());
```

PHP

```php
// 智能体当前正在分析文件……
// 用新的指示中断：
$client->beta->sessions->events->send(
    $session->id,
    events: [
        ['type' => 'user.interrupt'],
        [
            'type' => 'user.message',
            'content' => [
                [
                    'type' => 'text',
                    'text' => 'Instead, focus on fixing the bug in line 42.',
                ],
            ],
        ],
    ],
);
```

Ruby

```ruby
# 智能体当前正在分析一个文件……
# 用新的指令中断：
client.beta.sessions.events.send_(
  session.id,
  events: [
    {type: "user.interrupt"},
    {
      type: "user.message",
      content: [
        {type: "text", text: "Instead, focus on fixing the bug in line 42."}
      ]
    }
  ]
)
```

请求在事件入队后立即返回。在智能体处理中断之前，中断事件的 `processed_at` 为 `null`。中断的生效时机取决于当前执行阶段：

* 如果模型正在生成响应，响应会立即停止。
* 如果有工具调用正在运行，中断可能延迟生效，在此之前会话保持 `running` 状态。

> 运行中的消息与中断
>
> 会话处于 `running` 时，各运行时的处理方式不同：
>
> * AgentAPI 托管的 `native`：可在同一请求提交 `user.interrupt` + `user.message`，事件按序入队执行。此保证不适用于自托管外部 worker。
> * `claudecode` 和 `codex`：发送 `user.message` 会返回 `409`（agent busy）。请先发送 `user.interrupt`，等待 `idle` 后再发送新消息。
>
> 中断不会回滚已完成的工具操作或外部副作用。

被中断的执行以 `session.status_idle` 结束。`stop_reason.type` 在 `native` 中为 `end_turn`，与正常结束相同；在 `claudecode` 和 `codex` 的普通 `user.message` 轮次中为 `interrupted`。Outcome 轮次沿用自身的中断处理约定，不应仅凭 `stop_reason` 判断是否发生中断。请结合事件流中的 `user.interrupt` 判断中断。随后发送 `user.message` 可启动下一轮执行。

`claudecode` 和 `codex` 的普通 `user.message` 轮次在准备 GitHub 仓库时也可被中断。如果 `user.interrupt` 取消了正在执行的 Git 命令，本轮同样以 `idle / interrupted` 结束，不会仅因这次主动取消产生 `session.error` 或标记为 `retries_exhausted`。此时尚未启动运行时，新建的沙箱会被清理，下次输入重新准备环境。仓库命令自身的失败、超时和沙箱清理失败仍按错误处理。这一事件约定不适用于 Outcome 轮次。

### 在原会话中继续任务

文件分析或修改结束后，可以在原会话提交测试要求。处于 `idle` 的会话仍能接收 `user.message`，AgentAPI 会尽量复用可用的运行时上下文。

> 上下文与产物保存
>
> `claudecode` 或 `codex` 的云端沙箱失效后，会话随之终止，需要新建会话。运行时上下文不保证长期留存；需要保存的产物应写入 `/mnt/session/outputs/`，再通过 [Files API](../data-and-integrations/files.md) 下载。

![CLI 会话状态生命周期：running 停止本轮后进入 idle，可发送后续输入继续执行；claudecode 和 codex 的原沙箱丢失后进入 terminated](https://developer-doc.qiniu.com/docs-assets/products/agentapi/development_guidelines/agentapi-sessions-and-tasks/13597-agentapi-sessions-and-tasks-events/FgB2gdmBcB04Gec6FGDADZXjutmN-3cc5db973469.webp)

向空闲会话发送 `user.message` 事件可启动下一轮执行：

cURL

```bash
# 在生产环境中，请传入待恢复会话的已存储 ID。
curl --fail-with-body -sS "$AGENTAPI_BASE_URL/v1/sessions/$SESSION_ID/events" \
  -H "x-api-key: $AGENTAPI_API_KEY" \
  -H "content-type: application/json" \
  -d @- <<'EOF'
{
  "events": [
    {
      "type": "user.message",
      "input": {"parts": [
        {"type": "text", "text": "Now run the tests against the changes you made earlier."}
      ]}
    }
  ]
}
EOF
```

Python

```python
# 通过发送新的 user.message 事件来恢复先前创建的会话。
# 在生产环境中，传入待恢复会话的已存储 ID。
client.beta.sessions.events.send(
    session.id,
    events=[
        {
            "type": "user.message",
            "content": [
                {
                    "type": "text",
                    "text": "Now run the tests against the changes you made earlier.",
                },
            ],
        },
    ],
)
```

TypeScript

````typescript
// 通过向先前创建的会话发送新的用户事件来恢复该会话。
// 在生产环境中，请传入待恢复会话的已存储 ID。
await client.beta.sessions.events.send(session.id, {
  events: [
    {
      type: "user.message",
      content: [
        {
          type: "text",
          text: "Now run the tests against the changes you made earlier.",
        },
      ],
    },
  ],
});
```csharp
```csharp
// 通过 ID 恢复先前创建的会话。在生产环境中，请传入
// 创建会话时存储的会话 ID。
await client.Beta.Sessions.Events.Send(session.ID, new()
{
    Events =
    [
        new BetaManagedAgentsUserMessageEventParams
        {
            Type = BetaManagedAgentsUserMessageEventParamsType.UserMessage,
            Content =
            [
                new BetaManagedAgentsTextBlock
                {
                    Type = BetaManagedAgentsTextBlockType.Text,
                    Text = "Now run the tests against the changes you made earlier.",
                },
            ],
        },
    ],
});
````

Go

```go
// 恢复先前创建的会话：向其发送新的 user.message
// 事件。在生产环境中，请传入要恢复的会话的已存储 ID。
if _, err := client.Beta.Sessions.Events.Send(ctx, session.ID, anthropic.BetaSessionEventSendParams{
	Events: []anthropic.BetaManagedAgentsEventParamsUnion{{
		OfUserMessage: &anthropic.BetaManagedAgentsUserMessageEventParams{
			Type: anthropic.BetaManagedAgentsUserMessageEventParamsTypeUserMessage,
			Content: []anthropic.BetaManagedAgentsUserMessageEventParamsContentUnion{{
				OfText: &anthropic.BetaManagedAgentsTextBlockParam{
					Type: anthropic.BetaManagedAgentsTextBlockTypeText,
					Text: "Now run the tests against the changes you made earlier.",
				},
			}},
		},
	}},
}); err != nil {
	panic(err)
}
```

Java

```java
// 通过 ID 恢复先前创建的会话。在生产环境中，请传入
// 创建会话时存储的会话 ID。
client.beta().sessions().events().send(
    session.id(),
    EventSendParams.builder()
        .addEvent(BetaManagedAgentsUserMessageEventParams.builder()
            .type(BetaManagedAgentsUserMessageEventParams.Type.USER_MESSAGE)
            .addTextContent("Now run the tests against the changes you made earlier.")
            .build())
        .build());
```

PHP

```php
// 通过发送新的 user.message 事件来恢复先前创建的会话。
// 在生产环境中，请传入创建会话时存储的会话 ID。
$client->beta->sessions->events->send(
    $session->id,
    events: [
        [
            'type' => 'user.message',
            'content' => [
                [
                    'type' => 'text',
                    'text' => 'Now run the tests against the changes you made earlier.',
                ],
            ],
        ],
    ],
);
```

Ruby

```ruby
# 恢复会话只需向其发送下一个事件。在生产环境中，
# 请传入创建会话时存储的会话 ID。
client.beta.sessions.events.send_(
  session.id,
  events: [
    {
      type: "user.message",
      content: [
        {type: "text", text: "Now run the tests against the changes you made earlier."}
      ]
    }
  ]
)
```

## 完成需要应用处理的操作

`requires_action` 表示本轮等待应用参与。先读取阻塞事件，区分是业务系统执行工具，还是批准云端工具调用，再发送对应结果。观察工具事件和执行工具是两种职责，不能将所有 `agent.tool_use` 都交给应用重复执行。

### 处理自定义工具调用

例如，任务需要通过业务系统查询数据时，可将查询声明为[自定义工具](../agent-configuration/tools.md)。应用保存 `agent.custom_tool_use` 的事件 ID、工具名称和输入；会话以 `requires_action` 停止后，按 `stop_reason.event_ids` 逐个处理：

1. 从已接收事件中找到对应调用，在业务系统中执行工具。
2. 发送 `user.custom_tool_result`，将调用事件 ID 填入 `custom_tool_use_id`，并附上结果。
3. 全部阻塞事件处理完后，继续消费回到 `running` 的会话。

cURL

```bash
exec {stream_fd}< <(curl --fail-with-body -sS -N \
  "$AGENTAPI_BASE_URL/v1/sessions/$SESSION_ID/events/stream" \
  -H "x-api-key: $AGENTAPI_API_KEY" \
  -H "content-type: application/json" \
  -H "accept: text/event-stream")

while IFS= read -r -u "$stream_fd" line; do
  [[ $line == data:* ]] || continue
  event_json="${line#data: }"
  stop_reason=$(jq -r 'select(.type == "session.status_idle") | .stop_reason.type // empty' <<<"$event_json")
  case "$stop_reason" in
    requires_action)
      while IFS= read -r event_id; do
        # 执行该工具并将结果发回
        result=$(call_tool "$event_id")
        jq -n --arg id "$event_id" --arg result "$result" \
          '{events: [{type: "user.custom_tool_result", custom_tool_use_id: $id, content: [{type: "text", text: $result}]}]}' |
          curl --fail-with-body -sS \
            "$AGENTAPI_BASE_URL/v1/sessions/$SESSION_ID/events" \
            -H "x-api-key: $AGENTAPI_API_KEY" \
            -H "content-type: application/json" \
            -d @-
      done < <(jq -r '.stop_reason.event_ids[]' <<<"$event_json")
      ;;
    end_turn)
      break
      ;;
  esac
done
exec {stream_fd}<&-
```

Python

```python
with client.beta.sessions.events.stream(session.id) as stream:
    for event in stream:
        if event.type == "session.status_idle" and (stop_reason := event.stop_reason):
            match stop_reason.type:
                case "requires_action":
                    for event_id in stop_reason.event_ids:
                        # 查找自定义工具使用事件并执行它
                        tool_event = events_by_id[event_id]
                        result = call_tool(tool_event.name, tool_event.input)

                        # 将结果发送回去
                        client.beta.sessions.events.send(
                            session.id,
                            events=[
                                {
                                    "type": "user.custom_tool_result",
                                    "custom_tool_use_id": event_id,
                                    "content": [{"type": "text", "text": result}],
                                },
                            ],
                        )
                case "end_turn":
                    break
```

TypeScript

````typescript
const stream = await client.beta.sessions.events.stream(session.id);

for await (const event of stream) {
  if (event.type !== "session.status_idle") continue;
  if (event.stop_reason.type === "end_turn") break;
  if (event.stop_reason.type !== "requires_action") continue;

  for (const eventId of event.stop_reason.event_ids) {
    // 查找该自定义工具使用事件并执行它
    const toolEvent = eventsById.get(eventId);
    if (!toolEvent) continue;
    const result = await callTool(toolEvent.name, toolEvent.input);

    // 将结果发送回去
    await client.beta.sessions.events.send(session.id, {
      events: [
        {
          type: "user.custom_tool_result",
          custom_tool_use_id: eventId,
          content: [{ type: "text", text: result }],
        },
      ],
    });
  }
}
```csharp
```csharp
await foreach (var streamEvent in client.Beta.Sessions.Events.StreamStreaming(session.ID))
{
    if (streamEvent.Value is not BetaManagedAgentsSessionStatusIdleEvent idle) continue;

    if (idle.StopReason?.Value is BetaManagedAgentsSessionRequiresAction requiresAction)
    {
        foreach (var eventId in requiresAction.EventIds)
        {
            // 查找自定义工具使用事件并执行它
            var toolEvent = eventsById[eventId];
            var result = await CallTool(toolEvent.Name, toolEvent.Input);

            // 将结果发送回去
            await client.Beta.Sessions.Events.Send(session.ID, new()
            {
                Events =
                [
                    new BetaManagedAgentsUserCustomToolResultEventParams
                    {
                        Type = BetaManagedAgentsUserCustomToolResultEventParamsType.UserCustomToolResult,
                        CustomToolUseID = eventId,
                        Content =
                        [
                            new BetaManagedAgentsTextBlock
                            {
                                Type = BetaManagedAgentsTextBlockType.Text,
                                Text = result,
                            },
                        ],
                    },
                ],
            });
        }
    }
    else if (idle.StopReason?.Value is BetaManagedAgentsSessionEndTurn)
    {
        break;
    }
}
````

Go

```go
	stream := client.Beta.Sessions.Events.StreamEvents(ctx, session.ID, anthropic.BetaSessionEventStreamParams{})
	defer stream.Close()

loop:
	for stream.Next() {
		event, ok := stream.Current().AsAny().(anthropic.BetaManagedAgentsSessionStatusIdleEvent)
		if !ok {
			continue
		}
		switch stopReason := event.StopReason.AsAny().(type) {
		case anthropic.BetaManagedAgentsSessionRequiresAction:
			for _, eventID := range stopReason.EventIDs {
				// 查找自定义工具使用事件并执行它
				toolEvent := eventsByID[eventID]
				result := callTool(toolEvent.Name, toolEvent.Input)
				// 将结果发送回去
				if _, err := client.Beta.Sessions.Events.Send(ctx, session.ID, anthropic.BetaSessionEventSendParams{
					Events: []anthropic.BetaManagedAgentsEventParamsUnion{{
						OfUserCustomToolResult: &anthropic.BetaManagedAgentsUserCustomToolResultEventParams{
							Type:            anthropic.BetaManagedAgentsUserCustomToolResultEventParamsTypeUserCustomToolResult,
							CustomToolUseID: eventID,
							Content: []anthropic.BetaManagedAgentsUserCustomToolResultEventParamsContentUnion{{
								OfText: &anthropic.BetaManagedAgentsTextBlockParam{
									Type: anthropic.BetaManagedAgentsTextBlockTypeText,
									Text: result,
								},
							}},
						},
					}},
				}); err != nil {
					panic(err)
				}
			}
		case anthropic.BetaManagedAgentsSessionEndTurn:
			break loop
		}
	}
	if err := stream.Err(); err != nil {
		panic(err)
	}
```

Java

```java
try (var stream = client.beta().sessions().events().streamStreaming(session.id())) {
    stream.stream()
        .filter(BetaManagedAgentsStreamSessionEvents::isSessionStatusIdle)
        .map(idleEvent -> idleEvent.asSessionStatusIdle().stopReason())
        .takeWhile(stopReason -> !stopReason.isEndTurn())
        .filter(stopReason -> stopReason.isRequiresAction())
        .flatMap(stopReason -> stopReason.asRequiresAction().eventIds().stream())
        .forEach(eventId -> {
            // 查找自定义工具使用事件并执行它
            var toolEvent = eventsById.get(eventId);
            var result = callTool(toolEvent.name(), toolEvent.input());

            // 将结果发送回去
            client.beta().sessions().events().send(
                session.id(),
                EventSendParams.builder()
                    .addEvent(BetaManagedAgentsUserCustomToolResultEventParams.builder()
                        .type(BetaManagedAgentsUserCustomToolResultEventParams.Type.USER_CUSTOM_TOOL_RESULT)
                        .customToolUseId(eventId)
                        .addTextContent(result)
                        .build())
                    .build());
        });
}
```

PHP

```php
$stream = $client->beta->sessions->events->streamStream($session->id);

foreach ($stream as $event) {
    if ($event->type === 'session.status_idle' && $event->stopReason) {
        if ($event->stopReason->type === 'requires_action') {
            foreach ($event->stopReason->eventIDs as $eventId) {
                // 查找自定义工具使用事件并执行它
                $toolEvent = $eventsById[$eventId];
                $result = callTool($toolEvent->name, $toolEvent->input);

                // 将结果发送回去
                $client->beta->sessions->events->send(
                    $session->id,
                    events: [
                        [
                            'type' => 'user.custom_tool_result',
                            'custom_tool_use_id' => $eventId,
                            'content' => [['type' => 'text', 'text' => $result]],
                        ],
                    ],
                );
            }
        } elseif ($event->stopReason->type === 'end_turn') {
            break;
        }
    }
}
```

Ruby

```ruby
client.beta.sessions.events.stream_events(session.id).each do |event|
  case event
  in {type: :"session.status_idle", stop_reason: {type: :requires_action, event_ids:}}
    event_ids.each do |event_id|
      # 查找自定义工具使用事件并执行它
      tool_event = events_by_id[event_id]
      result = call_tool.call(tool_event.name, tool_event.input)
      # 将结果发送回去
      client.beta.sessions.events.send_(
        session.id,
        events: [
          {
            type: "user.custom_tool_result",
            custom_tool_use_id: event_id,
            content: [{type: "text", text: result}]
          }
        ]
      )
    end
  in {type: :"session.status_idle", stop_reason: {type: :end_turn}}
    break
  else
  end
end
```

### 工具确认

当[权限策略](../agent-configuration/permissions.md)要求确认时，应用负责审批，工具仍由云端执行。收到 `agent.tool_use` 或 `agent.mcp_tool_use` 后，会话以 `requires_action` 停止；应用按 `stop_reason.event_ids` 提取调用，展示工具及参数供审批。

为每个阻塞事件发送 `user.tool_confirmation`：`tool_use_id` 填调用事件 ID，`result` 填 `"allow"` 或 `"deny"`，拒绝时可附 `deny_message`。所有阻塞事件处理完后，会话回到 `running`。下面示例展示批准请求的发送方式；业务应用应在审批完成后发送。

cURL

```bash
exec {stream_fd}< <(curl --fail-with-body -sS -N \
  "$AGENTAPI_BASE_URL/v1/sessions/$SESSION_ID/events/stream" \
  -H "x-api-key: $AGENTAPI_API_KEY" \
  -H "content-type: application/json" \
  -H "accept: text/event-stream")

while IFS= read -r -u "$stream_fd" line; do
  [[ $line == data:* ]] || continue
  event_json="${line#data: }"
  stop_reason=$(jq -r 'select(.type == "session.status_idle") | .stop_reason.type // empty' <<<"$event_json")
  case "$stop_reason" in
    requires_action)
      while IFS= read -r event_id; do
        # 批准待处理的工具调用
        jq -n --arg id "$event_id" \
          '{events: [{type: "user.tool_confirmation", tool_use_id: $id, result: "allow"}]}' |
          curl --fail-with-body -sS \
            "$AGENTAPI_BASE_URL/v1/sessions/$SESSION_ID/events" \
            -H "x-api-key: $AGENTAPI_API_KEY" \
            -H "content-type: application/json" \
            -d @-
      done < <(jq -r '.stop_reason.event_ids[]' <<<"$event_json")
      ;;
    end_turn)
      break
      ;;
  esac
done
exec {stream_fd}<&-
```

Python

```python
with client.beta.sessions.events.stream(session.id) as stream:
    for event in stream:
        if event.type == "session.status_idle" and (stop_reason := event.stop_reason):
            match stop_reason.type:
                case "requires_action":
                    for event_id in stop_reason.event_ids:
                        # 批准待处理的工具调用
                        client.beta.sessions.events.send(
                            session.id,
                            events=[
                                {
                                    "type": "user.tool_confirmation",
                                    "tool_use_id": event_id,
                                    "result": "allow",
                                },
                            ],
                        )
                case "end_turn":
                    break
```

TypeScript

````typescript
const stream = await client.beta.sessions.events.stream(session.id);

for await (const event of stream) {
  if (event.type !== "session.status_idle") continue;
  if (event.stop_reason.type === "end_turn") break;
  if (event.stop_reason.type !== "requires_action") continue;

  for (const eventId of event.stop_reason.event_ids) {
    // 批准待处理的工具调用
    await client.beta.sessions.events.send(session.id, {
      events: [
        {
          type: "user.tool_confirmation",
          tool_use_id: eventId,
          result: "allow",
        },
      ],
    });
  }
}
```csharp
```csharp
await foreach (var streamEvent in client.Beta.Sessions.Events.StreamStreaming(session.ID))
{
    if (streamEvent.Value is not BetaManagedAgentsSessionStatusIdleEvent idle) continue;

    if (idle.StopReason?.Value is BetaManagedAgentsSessionRequiresAction requiresAction)
    {
        foreach (var eventId in requiresAction.EventIds)
        {
            // 批准待处理的工具调用
            await client.Beta.Sessions.Events.Send(session.ID, new()
            {
                Events =
                [
                    new BetaManagedAgentsUserToolConfirmationEventParams
                    {
                        Type = BetaManagedAgentsUserToolConfirmationEventParamsType.UserToolConfirmation,
                        ToolUseID = eventId,
                        Result = BetaManagedAgentsUserToolConfirmationEventParamsResult.Allow,
                    },
                ],
            });
        }
    }
    else if (idle.StopReason?.Value is BetaManagedAgentsSessionEndTurn)
    {
        break;
    }
}
````

Go

```go
	stream := client.Beta.Sessions.Events.StreamEvents(ctx, session.ID, anthropic.BetaSessionEventStreamParams{})
	defer stream.Close()

loop:
	for stream.Next() {
		event, ok := stream.Current().AsAny().(anthropic.BetaManagedAgentsSessionStatusIdleEvent)
		if !ok {
			continue
		}
		switch stopReason := event.StopReason.AsAny().(type) {
		case anthropic.BetaManagedAgentsSessionRequiresAction:
			for _, eventID := range stopReason.EventIDs {
				// 批准待处理的工具调用
				if _, err := client.Beta.Sessions.Events.Send(ctx, session.ID, anthropic.BetaSessionEventSendParams{
					Events: []anthropic.BetaManagedAgentsEventParamsUnion{{
						OfUserToolConfirmation: &anthropic.BetaManagedAgentsUserToolConfirmationEventParams{
							Type:      anthropic.BetaManagedAgentsUserToolConfirmationEventParamsTypeUserToolConfirmation,
							ToolUseID: eventID,
							Result:    anthropic.BetaManagedAgentsUserToolConfirmationEventParamsResultAllow,
						},
					}},
				}); err != nil {
					panic(err)
				}
			}
		case anthropic.BetaManagedAgentsSessionEndTurn:
			break loop
		}
	}
	if err := stream.Err(); err != nil {
		panic(err)
	}
```

Java

```java
try (var stream = client.beta().sessions().events().streamStreaming(session.id())) {
    stream.stream()
        .filter(BetaManagedAgentsStreamSessionEvents::isSessionStatusIdle)
        .map(idleEvent -> idleEvent.asSessionStatusIdle().stopReason())
        .takeWhile(stopReason -> !stopReason.isEndTurn())
        .filter(stopReason -> stopReason.isRequiresAction())
        .flatMap(stopReason -> stopReason.asRequiresAction().eventIds().stream())
        // 批准每个待处理的工具调用
        .forEach(toolUseId -> client.beta().sessions().events().send(
            session.id(),
            EventSendParams.builder()
                .addEvent(BetaManagedAgentsUserToolConfirmationEventParams.builder()
                    .type(BetaManagedAgentsUserToolConfirmationEventParams.Type.USER_TOOL_CONFIRMATION)
                    .toolUseId(toolUseId)
                    .result(BetaManagedAgentsUserToolConfirmationEventParams.Result.ALLOW)
                    .build())
                .build()));
}
```

PHP

```php
$stream = $client->beta->sessions->events->streamStream($session->id);

foreach ($stream as $event) {
    if ($event->type === 'session.status_idle' && $event->stopReason) {
        if ($event->stopReason->type === 'requires_action') {
            foreach ($event->stopReason->eventIDs as $eventId) {
                // 批准待处理的工具调用
                $client->beta->sessions->events->send(
                    $session->id,
                    events: [
                        [
                            'type' => 'user.tool_confirmation',
                            'tool_use_id' => $eventId,
                            'result' => 'allow',
                        ],
                    ],
                );
            }
        } elseif ($event->stopReason->type === 'end_turn') {
            break;
        }
    }
}
```

Ruby

```ruby
client.beta.sessions.events.stream_events(session.id).each do |event|
  case event
  in {type: :"session.status_idle", stop_reason: {type: :requires_action, event_ids:}}
    event_ids.each do |event_id|
      # 批准待处理的工具调用
      client.beta.sessions.events.send_(
        session.id,
        events: [
          {type: "user.tool_confirmation", tool_use_id: event_id, result: "allow"}
        ]
      )
    end
  in {type: :"session.status_idle", stop_reason: {type: :end_turn}}
    break
  else
  end
end
```

## 核对用量与排查异常

用量与时长用于观察执行成本。

### 核对模型用量与执行时长

会话通过 `stats` 返回时长，通过 `usage` 返回所有轮次的累计模型用量。会话进入 `idle` 后，查询可获取基于已提交事件计算的最新汇总；运行时报告的用量明细见 `span.model_request_end`。

```json
{
  "id": "ses_01...",
  "status": "idle",
  "usage": {
    "input_tokens": 2400,
    "output_tokens": 350,
    "cache_read_input_tokens": 1800
  },
  "stats": {
    "active_seconds": 342.5,
    "duration_seconds": 401.2
  }
}
```

读取响应时，请区分用量与时长：

* `usage`：累计用量，可能包含 `input_tokens`、`output_tokens`、`cache_read_input_tokens`、`cache_creation` 和 `server_tool_use`；不包含 `active_seconds`。缺失或有歧义的计数按未知处理，不应视为零。
* `stats.active_seconds`：累计执行时间。
* `stats.duration_seconds`：从创建到当前的经过时间，会话终止后冻结。
* `span.model_request_end.model_usage`：Native 中表示单次模型请求用量；CLI 运行时可能报告整次执行或线程累计用量，不能按单次请求直接累加。字段由运行时和模型决定，可能包含 `input_tokens`、`output_tokens` 或缓存用量。

计数至少上报一次后才会出现，客户端需兼容缺失和额外字段。完整结构见 [API 通用约定](../../api/api-conventions.md)。

### 在 Portal 定位执行问题

在 Portal 会话详情的“轨迹”中检查事件和停止原因，需要完整记录时通过“Raw JSONL”导出。界面使用说明见[在 Portal 中构建](../../quick-start/portal.md)。

## 事件字段与消费职责

实现分发逻辑时，按事件的用途选择处理方式。应用发送用户事件；AgentAPI 返回回答、工具调用、执行阶段（Span）和会话状态。

![事件双向流动：用户事件从应用上行进入会话，会话事件、执行阶段事件（Span）和智能体事件从会话下行返回应用](https://developer-doc.qiniu.com/docs-assets/products/agentapi/development_guidelines/agentapi-sessions-and-tasks/13597-agentapi-sessions-and-tasks-events/FuHJB0aM8PDf93dTXniwnqCARMVf-4d4d8d0ec5f6.webp)

| 消费目的   | 事件                                                                         | 应用职责                                                 |
| ------ | -------------------------------------------------------------------------- | ---------------------------------------------------- |
| 展示回答   | `agent.message`                                                            | 提取回答内容；Native 增量预览另按 `event_start`、`event_delta` 处理。 |
| 观察云端工具 | `agent.tool_use`、`agent.mcp_tool_use`                                      | 展示调用过程，按权限策略处理确认。                                    |
| 执行业务工具 | `agent.custom_tool_use`                                                    | 保存调用，按阻塞事件 ID 回传结果。                                  |
| 更新任务状态 | `session.status_running`、`session.status_idle`、`session.status_terminated` | 展示执行状态；停止时检查原因，终止后停止提交输入。                            |
| 排查模型请求 | `span.model_request_start`、`span.model_request_end`                        | 关联请求阶段，检查运行时报告的用量。                                   |
| 记录错误   | `session.error`                                                            | 保存错误信息，继续检查后续状态。                                     |

持久化事件类型遵循 `{domain}.{action}` 命名约定；仅供流预览的 `event_start`、`event_delta` 例外。完整目录见[事件类型](../../api/api-conventions.md)。

持久化事件包含 `id`、`type`、`session_id`、`sequence_number` 和 `processed_at`。排队中的用户输入在接收响应中可能返回 `processed_at: null`；执行时，事件列表和事件流中的对应事件会带上处理时间。不要依赖事件包含 `created_at`。
