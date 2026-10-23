<!-- 来源：https://developer-doc.qiniu.com/products/agentapi/sdk/13620-agentapi-sdk-download | 由 sync-docs.py 自动生成，请勿手改 -->

# SDK 下载



最近更新时间: 2026-10-13 17:21:25

AgentAPI 的智能体、环境、会话和事件接口可以使用 Anthropic SDK 调用。以下列出各语言的安装方式与官方源码入口。

| 语言                      | 安装方式                                                         | 官方源码                                                                               |
| ----------------------- | ------------------------------------------------------------ | ---------------------------------------------------------------------------------- |
| Python                  | `pip install anthropic`                                      | [anthropic-sdk-python](https://github.com/anthropics/anthropic-sdk-python)         |
| TypeScript / JavaScript | `npm install @anthropic-ai/sdk`                              | [anthropic-sdk-typescript](https://github.com/anthropics/anthropic-sdk-typescript) |
| Java（Gradle）            | `implementation("com.anthropic:anthropic-java:2.62.0")`      | [anthropic-sdk-java](https://github.com/anthropics/anthropic-sdk-java)             |
| Go                      | `go get github.com/anthropics/anthropic-sdk-go`              | [anthropic-sdk-go](https://github.com/anthropics/anthropic-sdk-go)                 |
| C#                      | `dotnet add package Anthropic`                               | [anthropic-sdk-csharp](https://github.com/anthropics/anthropic-sdk-csharp)         |
| Ruby                    | `bundle add anthropic`                                       | [anthropic-sdk-ruby](https://github.com/anthropics/anthropic-sdk-ruby)             |
| PHP                     | `composer require "anthropic-ai/sdk" "guzzlehttp/guzzle:^7"` | [anthropic-sdk-php](https://github.com/anthropics/anthropic-sdk-php)               |

接入 AgentAPI 时，需要显式设置服务地址、已绑定的 MaaS API Key，并通过扩展请求参数传入 `runtime` 等 AgentAPI 字段。客户端初始化与接入注意事项见[通过 API 构建：使用 SDK](../quick-start/quickstart.md)。
