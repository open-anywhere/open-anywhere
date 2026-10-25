<p align="center">
  <img src="docs/assets/open-anywhere-mascot.png" alt="Open Anywhere 吉祥物百变怪" height="96">
  &nbsp;&nbsp;&nbsp;
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/open-anywhere-wordmark-dark.svg">
    <img src="docs/assets/open-anywhere-wordmark.svg" alt="Open Anywhere" height="96">
  </picture>
</p>

<p align="center">
  说出你的需要，让应用在身边的屏幕上诞生。
</p>

<p align="center">
  <a href="https://github.com/open-anywhere/open-anywhere/releases/download/v1.0.0/open-anywhere-demo-1080p.mp4"><img src="https://img.shields.io/badge/%E4%B8%8B%E8%BD%BD_Demo_%E8%A7%86%E9%A2%91-2EA44F?style=for-the-badge" alt="下载 Demo 视频" height="36"></a>
  <a href="https://www.bilibili.com/video/BV1AznU6YELm/"><img src="https://img.shields.io/badge/%E8%A7%82%E7%9C%8B_Demo_%E8%A7%86%E9%A2%91-Bilibili-FB7299?style=for-the-badge&amp;logo=bilibili&amp;logoColor=white" alt="观看 Demo 视频（Bilibili，4K）" height="36"></a>
  <a href="https://www.bilibili.com/video/BV1AznU6YELm/"><img src="https://img.shields.io/badge/4K-2160p-6C5CE7?style=for-the-badge" alt="4K" height="36"></a>
</p>

<p align="center">
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-00ADD8?logo=go&logoColor=white" alt="Go"></a>
  <a href="https://codecov.io/gh/open-anywhere/open-anywhere"><img src="https://img.shields.io/codecov/c/github/open-anywhere/open-anywhere?token=U6SNW7NYZQ&amp;component=casting&amp;label=casting" alt="casting coverage"></a>
  <a href="https://codecov.io/gh/open-anywhere/open-anywhere"><img src="https://img.shields.io/codecov/c/github/open-anywhere/open-anywhere?token=U6SNW7NYZQ&amp;component=gateway&amp;label=gateway" alt="gateway coverage"></a>
  <a href="https://codecov.io/gh/open-anywhere/open-anywhere"><img src="https://img.shields.io/codecov/c/github/open-anywhere/open-anywhere?token=U6SNW7NYZQ&amp;component=render&amp;label=render" alt="render coverage"></a>
  <a href="https://codecov.io/gh/open-anywhere/open-anywhere"><img src="https://img.shields.io/codecov/c/github/open-anywhere/open-anywhere?token=U6SNW7NYZQ&amp;component=voiceentry&amp;label=voiceentry" alt="voiceentry coverage"></a>
</p>

---

**Open Anywhere** 让人用一句话描述此刻的需要，由云端编码 Agent 当场生成一个可运行的 Web 应用，投到身边的屏幕上，并继续用语音操作它：

> 我们有三个人，想比较一下每个人的知识储备量，请你做个电影知识竞赛 App，你来主持，放到电视上。

应用不在菜单里，是被当场写出来的；玩法不在代码里，是被当场理解并驱动的。**有块屏幕，就能开场。**

<sub>参赛议题：灵矽应用 —— 通过 AirPlay 协议连接屏幕，并通过语音控制 code agent 进行编程开发。</sub>

## 为什么值得做

- **长尾需求不值得开发一款软件。** 一场只属于今晚三个人的聚会，没有现成应用，也不值得排期；模型把“为一次需求写一个应用”的边际成本降到接近零。
- **现成应用要求人先找工具、再学操作**，最后把现场需要迁就已有功能。
- **现场需要一块大家都能看的屏幕**，而不是每个人手里的一块小屏。

**给谁用**：身边有屏幕、要组织一次互动或完成一件事的人——聚会发起者、家长或活动组织者、小团队主持人、商家。

商业规划见[商业计划书](docs/business-plan.md)：市场机会、七个年龄段需求地图、定价体系、第一周闭环、单元经济与三年目标。市场背景、产品定位与收费模式的完整论证见[商业策略](docs/product/business.md)。

## 它是怎么工作的

你说一句话：几个人、什么主题、投到哪块屏。系统听懂它，把它变成一个能跑的应用，再把画面送到电视上——**说话 → 听懂 → 生成应用 → 上屏 → 继续用语音操作**。

| 这一步 | 由谁完成 | 做什么 |
| --- | --- | --- |
| 听懂 | 灵矽（语音入口） | 识别场景、人数、主题与屏幕，决定调哪个工具 |
| 编排 | 控制面 | 校验请求、调度构建、转发操作 |
| 写代码 | AgentAPI | 在隔离环境里生成应用并自测 |
| 上屏 | Render | 启动应用、采集画面、送出媒体流 |

生成的应用把每个操作都声明成 OpenAPI，控制面提供稳定的 Mcp 工具，所以语音不只能创建它，还能在运行时操作它：开始、下一题、查比分，都是一次业务调用。

## AI 价值呈现

产品最核心的五件事都发生在模型这一侧：

| 环节 | 模型做什么 |
| --- | --- |
| 听懂需求 | 识别场景、人数、主题与目标屏幕，整理成可构建的需求 |
| 创造应用 | 在隔离环境里写页面、写接口、写 OpenAPI，自测通过后产出可运行的应用包 |
| 理解应用 | 读取应用的操作表，自行判断该调哪一个、带什么参数 |
| 操作应用 | 把“开始”“下一题”“查比分”翻译成一次业务操作 |
| 主持现场 | 按提示词组织流程、判题计分，并用语音接住现场的变化 |

只有最后一步“把画面送到屏幕上”交给确定性工程。投屏、编码、进程管理需要毫秒级稳定与可预期的失败处理——**不确定性留给模型，确定性留给链路。**

## AI 工程实践

这个作品本身，也是用同一套方法做出来的。六件我们实际做了的事：

- **先做技能，再接平台。** 灵矽和 AgentAPI 都是新平台、模型不熟悉，也没有现成技能包。我们先把官方文档（41 页 / 34 页）封装成仓库内的 skill，让 Agent 基于文档接入，而不是猜接口。
- **先定架构契约，再并行开发。** 三个人同时推进控制面、运行端、投屏与语音侧，前提是先把模块和接口定下来，靠契约联调而不是靠口头约定，实际跑通 MVP 只花了一顿饭的时间。
- **用假下游补齐联调。** 真实服务与硬件不可重复，我们让 Agent 按接口写假下游（59 个文件使用 `httptest`），先跑通协议、状态机和错误路径，再接真实服务。
- **把动态能力收敛成固定工具。** 应用是当场生成的、能力各不相同，但语音侧只看到两个通用工具：读操作表与发请求。接入面不随应用数量增长。
- **用对抗性测试补实现层。** 15 个 `*_adversarial_test.go` 专门找边界、并发、资源清理与失败路径的问题。
- **10 个 Subagent 多领域评审。** 一次评审覆盖投屏、渲染、构建、语音四条链路，25 个文件，每条修复都补回归测试（[PR #67](https://github.com/open-anywhere/open-anywhere/pull/67)）。

我们认为其中最重要的三条经验是：

1. **人做判断，AI 做执行。** 拆分方式、接口定义、什么算缺陷、什么属于既有设计，都由人决定。
2. **证据先于结论。** 没有测量过的收益不写成数字，只保留为团队观察。
3. **测试驱动开发。** 设定 99% 目标的测试覆盖率，不仅仅是为了找到 bug，更能倒逼 AI 写出更可测试的代码。

## 当前状态

| 状态 | 内容 |
| --- | --- |
| **真实可用** | 语音创建应用、应用上屏、语音操作应用、多租户 Portal、MCP 工具集、控制面重启恢复 |
| **模拟** | 现场语音入口由本地程序模拟灵矽硬件，按同一套设备协议接入 |

## 架构

四个部件分布在三个互不重叠的平面上：输入、计算、显示。平面之间只有三条契约——MCP 让语音只看到工具；OpenAPI + `launch.json` + HTTP 让应用只暴露 HTTP；投屏 HTTP 只传媒体流地址。详见[架构设计](docs/architecture.md)与[模块规格](docs/modules.md)。

## 快速开始

```bash
go build ./... && go test ./... -race
go run ./cmd/gateway -config gateway.yaml     # 控制面：MCP、编排、Portal
go run ./cmd/render -addr :8090 -public-url http://192.168.1.20:8090
go run ./cmd/casting -addr :8080 -interface en0
go run ./cmd/voiceentry -serve                # 打开 http://127.0.0.1:8788/
```

配置文件从 `gateway.example.yaml` 复制。Render 需要 Node.js、Chromium、FFmpeg 与 lsof；Portal 前端在 `portal/` 下用 pnpm 构建。逐步说明见 [Gateway](docs/start/gateway.md)、[Render](docs/start/render.md)、[Casting](docs/start/casting.md) 启动指南。

## 文档

| 我想…… | 去哪 |
| --- | --- |
| 了解产品与商业 | [产品设计](docs/product.md)、[商业策略](docs/business-strategy.md) |
| 看懂分层与契约 | [架构设计](docs/architecture.md) |
| 看模块职责与分工 | [模块规格](docs/modules.md) |
| 查接口、数据与错误码 | [应用包](docs/app-artifact.md)、[应用管理 MCP](docs/app-management-mcp.md)、[控制面数据库](docs/gateway-database.md) |
| 看投屏与语音 | [Casting](docs/casting.md)、[现场语音入口](docs/voice-entry.md) |

## 团队

| 成员 | 分工 |
| --- | --- |
| 张之阳 [@luoliwoshang](https://github.com/luoliwoshang) | 架构设计、产品设计、商业规划、视频剪辑、Render 技术实现、Cast 端技术实现 |
| 邓军辉 [@minorcell](https://github.com/minorcell) | 架构设计、Gateway 技术、控制面业务架构、Portal 设计、质量保障 |
| 韩鲁鹏 [@LUPENGHAN](https://github.com/LUPENGHAN) | 灵矽技术实现、架构设计、质量保障 |
