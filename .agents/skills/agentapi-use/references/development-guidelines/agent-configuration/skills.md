<!-- 来源：https://developer-doc.qiniu.com/products/agentapi/development_guidelines/agentapi-agent-configuration/13594-agentapi-agent-configuration-skills | 由 sync-docs.py 自动生成，请勿手改 -->

# Agent Skills



最近更新时间: 2026-10-13 15:31:29

Agent Skills 是基于文件系统的可复用技能包，为云端智能体提供领域知识和操作步骤。附加技能会占用会话上下文，请仅选择当前任务需要的技能。

AgentAPI 支持上传自定义技能后通过智能体的 `skills` 数组附加，也支持 `claudecode`、`codex` 从挂载的 GitHub 仓库发现技能。`native` 只使用显式附加的自定义技能。

![Agent Skills 进入云端运行的两种方式](https://developer-doc.qiniu.com/docs-assets/products/agentapi/development_guidelines/agentapi-agent-configuration/13594-agentapi-agent-configuration-skills/Flc9jwigHC6J7iWcY2VhTZB6wUgR-2ffb015c3af6.webp)

> 说明
>
> 当前 AgentAPI 没有内置的供应商技能目录。虽然 API 为兼容请求结构接受 `type: "anthropic"`，但这类技能在执行时无法解析。请上传自定义技能，并使用 `type: "custom"`。

## 运行时差异

技能的来源和加载方式取决于智能体的 `runtime`：

| 运行时          | 显式附加的自定义技能                                   | GitHub 仓库中的技能              |
| ------------ | -------------------------------------------- | -------------------------- |
| `native`     | AgentAPI 在云端按需加载 `SKILL.md`，并提供技能包中的辅助文件。    | 不自动发现；请先上传并附加技能。           |
| `claudecode` | AgentAPI 会在云端运行环境启动前准备技能目录，供 Claude Code 使用。 | 由 Claude Code 的原生技能发现能力处理。 |
| `codex`      | AgentAPI 会在云端运行环境启动前准备技能目录，供 Codex 使用。       | 由 Codex 的原生技能发现能力处理。       |

自定义技能的主体是一个包含 `SKILL.md` 的目录。要编写技能，请在 `SKILL.md` 的 YAML frontmatter 中提供 `name` 和 `description`，再加入说明正文、脚本和参考文件。

## 编写技能包

技能适合保存重复使用的工作方法，例如代码审查规则、报表分析步骤或文档格式要求。在 `description` 中说明适用场景和任务目标，在正文中编写具体操作步骤；较长的参考资料和脚本存放在包内的辅助文件中，并在正文中引用。

技能提供指令和文件，实际操作仍依赖智能体的工具以及会话环境。例如，技能中的 Python 脚本需要代码执行能力和相应依赖；涉及业务系统时，还需要 MCP 或自定义工具。请同时检查[工具](tools.md)和[云端环境](../execution-environments/environment.md)配置。

最小的 `SKILL.md` 示例：

```md
---
name: code-review
description: 审查代码变更并输出可执行的修改建议。
---

# 代码审查

先检查变更范围，再根据项目约定给出问题、风险和修改建议。
```

## 创建自定义技能

自定义技能可以打包成 zip 压缩包上传，也可以通过技能 API 上传一个或多个文件。创建技能会返回 `skill_*` ID，之后在智能体的 `skills` 数组中引用它即可。上传的文件必须位于同一个顶层目录下，且该目录根部必须包含 `SKILL.md`。

请先完成[接入准备](../../quick-start/access.md)。以下示例假定已将技能目录打包为 `example_skill.zip`，压缩包内保留顶层目录，例如 `code-review/SKILL.md`。技能包直接上传到 `/v1/skills`，无需先上传到 Files API。

以下示例省略了可选的 `display_title` 字段，因此技能的显示标题使用 `SKILL.md` frontmatter 中的 `name`。

| 限制                         | 当前值                           |
| -------------------------- | ----------------------------- |
| 每个技能版本的文件数                 | 默认最多 200 个，可由服务配置调整           |
| 每个技能版本的文件总大小               | 默认最多 8 MiB，按解压后的文件计算，可由服务配置调整 |
| 单个 multipart 请求大小          | 最多 32 MiB，包含请求封装，不随技能包限制调整    |
| `native` 加载的 `SKILL.md` 大小 | 最多 128 KiB，内容必须为 UTF-8        |

cURL

```bash
skill=$(curl --request POST -fsSL "$AGENTAPI_BASE_URL/v1/skills" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --form 'files=@example_skill.zip;filename=example_skill.zip;type=application/zip')
SKILL_ID=$(jq -er '.id' <<<"$skill")
```

Python

```python
import os
from pathlib import Path

from anthropic import Anthropic

client = Anthropic(
    api_key=os.environ["AGENTAPI_API_KEY"],
    base_url=os.environ["AGENTAPI_BASE_URL"],
)

skill = client.beta.skills.create(
    files=[
        (
            "example_skill.zip",
            Path("example_skill.zip").read_bytes(),
            "application/zip",
        )
    ],
)

print(f"Created skill: {skill.id}")
print(f"Latest version: {skill.latest_version}")
```

TypeScript

````typescript
import Anthropic from "@anthropic-ai/sdk";
import { toFile } from "@anthropic-ai/sdk";
import fs from "node:fs";

const client = new Anthropic({
  apiKey: process.env.AGENTAPI_API_KEY!,
  baseURL: process.env.AGENTAPI_BASE_URL,
});

const skill = await client.beta.skills.create({
  files: [await toFile(fs.createReadStream("example_skill.zip"), "example_skill.zip")]
});

console.log(`Created skill: ${skill.id}`);
console.log(`Latest version: ${skill.latest_version}`);
```csharp
```csharp
using System;
using System.IO;
using System.Net.Http.Headers;
using Anthropic;
using Anthropic.Core;
using Anthropic.Models.Beta.Skills;

var client = new AnthropicClient(new ClientOptions
{
    ApiKey = Environment.GetEnvironmentVariable("AGENTAPI_API_KEY"),
    BaseUrl = Environment.GetEnvironmentVariable("AGENTAPI_BASE_URL"),
});

await using var skillFile = File.OpenRead("example_skill.zip");
var parameters = new SkillCreateParams {
    Files = [
        new BinaryContent {
            Stream = skillFile,
            FileName = "example_skill.zip",
            ContentType = new MediaTypeHeaderValue("application/zip"),
        },
    ],
};

var skill = await client.Beta.Skills.Create(parameters);

Console.WriteLine($"Created skill: {skill.ID}");
Console.WriteLine($"Latest version: {skill.LatestVersion}");
````

Go

```go
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

func main() {
	client := anthropic.NewClient(
		option.WithoutEnvironmentDefaults(),
		option.WithBaseURL(os.Getenv("AGENTAPI_BASE_URL")),
		option.WithAPIKey(os.Getenv("AGENTAPI_API_KEY")),
	)

	zipFile, err := os.Open("example_skill.zip")
	if err != nil {
		log.Fatal(err)
	}
	defer zipFile.Close()

	skill, err := client.Beta.Skills.New(context.TODO(), anthropic.BetaSkillNewParams{
		Files: []io.Reader{anthropic.File(zipFile, "example_skill.zip", "application/zip")},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Created skill: %s\n", skill.ID)
	fmt.Printf("Latest version: %s\n", skill.LatestVersion)
}
```

Java

```java
import com.anthropic.client.AnthropicClient;
import com.anthropic.client.okhttp.AnthropicOkHttpClient;
import com.anthropic.core.MultipartField;
import com.anthropic.models.beta.skills.Skill;
import com.anthropic.models.beta.skills.SkillCreateParams;
import java.io.IOException;
import java.io.InputStream;
import java.nio.file.Files;
import java.nio.file.Path;

void main() throws IOException {
    AnthropicClient client = AnthropicOkHttpClient.builder()
        .apiKey(System.getenv("AGENTAPI_API_KEY"))
        .baseUrl(System.getenv("AGENTAPI_BASE_URL"))
        .build();

    SkillCreateParams params = SkillCreateParams.builder()
        .addFile(MultipartField.<InputStream>builder()
            .value(Files.newInputStream(Path.of("example_skill.zip")))
            .filename("example_skill.zip")
            .contentType("application/zip")
            .build())
        .build();

    Skill skill = client.beta().skills().create(params);

    IO.println("Created skill: " + skill.id());
    IO.println("Latest version: " + skill.latestVersion());
}
```

PHP

```php
<?php

use Anthropic\Client;
use Anthropic\Core\FileParam;

$client = new Client(
    apiKey: getenv('AGENTAPI_API_KEY') ?: throw new RuntimeException('AGENTAPI_API_KEY is required'),
    baseUrl: getenv('AGENTAPI_BASE_URL') ?: throw new RuntimeException('AGENTAPI_BASE_URL is required'),
);

$skill = $client->beta->skills->create(
    files: [
        FileParam::fromResource(fopen('example_skill.zip', 'r')),
    ],
);

echo "Created skill: {$skill->id}\n";
echo "Latest version: {$skill->latestVersion}\n";
```

Ruby

```ruby
require "anthropic"

client = Anthropic::Client.new(
  api_key: ENV.fetch("AGENTAPI_API_KEY"),
  base_url: ENV.fetch("AGENTAPI_BASE_URL")
)

skill = client.beta.skills.create(
  files: [
    File.open("example_skill.zip", "rb")
  ]
)

puts "Created skill: #{skill.id}"
puts "Latest version: #{skill.latest_version}"
```

创建完成后，可通过以下接口管理技能和版本。

## 管理技能和版本

Portal 的 Skill 列表支持按名称或 ID 搜索。关键词会去除首尾空白，按字面量包含关系匹配，并在统计总数和分页前执行。对应接口为 `GET /portal/v1/skills?page_index=1&q=关键词`，`q` 必须与 `page_index` 一起使用；公开的 `GET /v1/skills` 不接受该参数。

技能和技能版本分别有独立的资源接口：

| 操作      | API                                                                                       |
| ------- | ----------------------------------------------------------------------------------------- |
| 列出或查看技能 | `GET /v1/skills`、`GET /v1/skills/{skill_id}`                                              |
| 删除技能    | `DELETE /v1/skills/{skill_id}`                                                            |
| 创建版本    | `POST /v1/skills/{skill_id}/versions`，请求格式与创建技能相同                                         |
| 列出或查看版本 | `GET /v1/skills/{skill_id}/versions`、`GET /v1/skills/{skill_id}/versions/{version}`       |
| 下载完整技能包 | `GET /v1/skills/{skill_id}/versions/{version}/content`，请求头设置 `accept: application/zip`    |
| 查看包内文件  | `GET /v1/skills/{skill_id}/versions/{version}/files`，或通过 `/files/content?path=...` 下载单个文件 |
| 删除版本    | `DELETE /v1/skills/{skill_id}/versions/{version}`                                         |

版本号由 AgentAPI 在上传时生成：

* 省略 `version` 或传入 `latest`：使用当前最新的有效版本。
* 删除最新版本：`latest_version` 更新为最近的有效版本。
* 已删除的技能或版本：不能再附加到智能体。

需要复现某次任务或分阶段发布技能时，请保存上传响应中的版本号并明确引用它。智能体版本和技能版本是两套独立的版本：把智能体固定到某个版本，不能把其中的 `latest` 技能引用变成固定技能版本。

> 最新版本的生效范围
>
> `native` 在每个新轮次重新解析 `latest`。CLI 运行时复用已有沙箱时，不会重新初始化技能文件，因此已有会话不保证立即使用新版本。需要确定技能内容时，应固定版本并创建新会话。

## 将技能附加到智能体

> 说明
>
> 挂载更多技能会增加会话沙箱启动所需的时间。请仅附加每个智能体完成其任务所需的技能。

`skills` 数组中的每个条目使用以下字段：

| 字段         | 描述                                                      |
| ---------- | ------------------------------------------------------- |
| `type`     | 当前使用 `custom`。`anthropic` 仅为请求结构兼容而保留，AgentAPI 执行时无法解析。 |
| `skill_id` | 自定义技能标识符，使用创建技能时返回的 `skill_*` ID。                       |
| `version`  | 可固定到某个版本，也可以使用 `latest`；省略时使用当前最新版本。                    |

一个智能体最多附加 20 个技能。以下示例使用上传响应中的技能 ID，选择 `native` 运行时，并引用最新有效技能版本。请先从[七牛 MaaS 模型管理页](https://portal.qiniu.com/ai-inference/model)选择可用模型，设置 `AGENTAPI_MODEL_ID`。如需固定技能版本，将 `latest` 替换为上传响应中的版本号。

cURL

```bash
agent=$(curl --request POST "$AGENTAPI_BASE_URL/v1/agents" \
  --header "x-api-key: $AGENTAPI_API_KEY" \
  --header "content-type: application/json" \
  --data @- <<EOF
{
  "name": "代码审查助手",
  "runtime": "native",
  "model": {"id": "$AGENTAPI_MODEL_ID"},
  "system": "你是一名严谨的代码审查助手，请按技能中的检查项审查代码。",
  "skills": [
    {"type": "custom", "skill_id": "$SKILL_ID", "version": "latest"}
  ]
}
EOF
)
```

Python

```python
import os

agent = client.beta.agents.create(
    name="代码审查助手",
    model={"id": os.environ["AGENTAPI_MODEL_ID"]},
    system="你是一名严谨的代码审查助手，请按技能中的检查项审查代码。",
    skills=[
        {
            "type": "custom",
            "skill_id": skill.id,
            "version": "latest",
        },
    ],
    extra_body={"runtime": "native"},
)
```

TypeScript

````typescript
const params = {
  name: "代码审查助手",
  model: { id: process.env.AGENTAPI_MODEL_ID! },
  system: "你是一名严谨的代码审查助手，请按技能中的检查项审查代码。",
  skills: [
    {
      type: "custom",
      skill_id: skill.id,
      version: "latest"
    }
  ]
} satisfies Parameters<typeof client.beta.agents.create>[0];
const agent = await client.beta.agents.create(params, {
  body: { ...params, runtime: "native" },
});
```csharp
```csharp
using System;
using System.Linq;
using System.Text.Json;
using Anthropic;
using Anthropic.Core;
using Anthropic.Models.Beta.Agents;

var parameters = new AgentCreateParams
{
    Name = "代码审查助手",
    Model = new BetaManagedAgentsModelConfigParams { ID = Environment.GetEnvironmentVariable("AGENTAPI_MODEL_ID")! },
    System = "你是一名严谨的代码审查助手，请按技能中的检查项审查代码。",
    Skills =
    [
        new BetaManagedAgentsCustomSkillParams { Type = BetaManagedAgentsCustomSkillParamsType.Custom, SkillID = skill.ID, Version = "latest" },
    ],
};
var body = parameters.RawBodyData.ToDictionary(entry => entry.Key, entry => entry.Value);
body["runtime"] = JsonSerializer.SerializeToElement("native");
parameters = AgentCreateParams.FromRawUnchecked(parameters.RawHeaderData, parameters.RawQueryData, body);
var agent = await client.Beta.Agents.Create(parameters);
````

Go

```go
import "os"
import "github.com/anthropics/anthropic-sdk-go/option"

agent, err := client.Beta.Agents.New(ctx, anthropic.BetaAgentNewParams{
	Name: "代码审查助手",
	Model: anthropic.BetaManagedAgentsModelConfigParams{
		ID: os.Getenv("AGENTAPI_MODEL_ID"),
	},
	System: anthropic.String("你是一名严谨的代码审查助手，请按技能中的检查项审查代码。"),
	Skills: []anthropic.BetaManagedAgentsSkillParamsUnion{
		{OfCustom: &anthropic.BetaManagedAgentsCustomSkillParams{
			SkillID: skill.ID,
			Type:    anthropic.BetaManagedAgentsCustomSkillParamsTypeCustom,
			Version: anthropic.String("latest"),
		}},
	},
}, option.WithJSONSet("runtime", "native"))
if err != nil {
	panic(err)
}
_ = agent
```

Java

```java
import com.anthropic.core.JsonValue;
import com.anthropic.models.beta.agents.*;

var agent = client.beta().agents().create(
    AgentCreateParams.builder()
        .name("代码审查助手")
        .model(BetaManagedAgentsModelConfigParams.builder().id(System.getenv("AGENTAPI_MODEL_ID")).build())
        .system("你是一名严谨的代码审查助手，请按技能中的检查项审查代码。")
        .addSkill(
            BetaManagedAgentsCustomSkillParams.builder()
                .type(BetaManagedAgentsCustomSkillParams.Type.CUSTOM)
                .skillId(skill.id())
                .version("latest")
                .build()
        )
        .putAdditionalBodyProperty("runtime", JsonValue.from("native"))
        .build()
);
```

PHP

```php
$agent = $client->beta->agents->create(
    name: '代码审查助手',
    model: ['id' => getenv('AGENTAPI_MODEL_ID')],
    system: '你是一名严谨的代码审查助手，请按技能中的检查项审查代码。',
    skills: [
        ['type' => 'custom', 'skillID' => $skill->id, 'version' => 'latest'],
    ],
    requestOptions: ['extraBodyParams' => ['runtime' => 'native']],
);
```

Ruby

```ruby
agent = client.beta.agents.create(
  name: "代码审查助手",
  model: {id: ENV.fetch("AGENTAPI_MODEL_ID")},
  system_: "你是一名严谨的代码审查助手，请按技能中的检查项审查代码。",
  skills: [
    {type: "custom", skill_id: skill.id, version: "latest"}
  ],
  request_options: {extra_body: {runtime: "native"}}
)
```

## 从 GitHub 仓库加载技能

挂载 GitHub 仓库后，`claudecode` 和 `codex` 可以按各自运行时的规则发现仓库中的 Skill。AgentAPI 不扫描这些目录。

`native` 不会自动发现仓库中的 Skill。使用 `native` 时，请先上传 Skill，再通过智能体的 `skills` 字段引用。

> 说明
>
> 仓库中的 Skill 会作为智能体指令加载。请只挂载可信仓库。

仓库挂载方式参阅[访问 GitHub](../data-and-integrations/github.md)。

## 后续步骤

* [云端环境](../execution-environments/environment.md)：为会话自定义云沙箱。

* [创建自定义技能](skills.md)：上传包含 `SKILL.md` 的自定义技能包，并在智能体中引用它。

* [文件](../data-and-integrations/files.md)：上传文件并在会话资源中引用。

* [通过 API 构建](../../quick-start/quickstart.md)：了解如何创建智能体、云端环境和会话，并运行云端任务。
