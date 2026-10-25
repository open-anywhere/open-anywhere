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

<sub>参赛议题：灵矽应用 —— 通过 DLNA 把画面投到电视上，并通过语音控制 code agent 进行编程开发（AirPlay 为后续扩展）。</sub>

## 一块屏幕，什么都可能是

同一个能力——描述、生成、上屏、语音操作——长出来的东西，由你那句话决定：

| 现场 | 你说 | 屏幕上长出来什么 |
| --- | --- | --- |
| 家庭知识竞赛 | “一家三口，十道自然知识题，你出题、你记分。” | 题目、抢答顺序和三人的比分；一家人一起上桌，不用留一个人全程操作 |
| 做饭 | “今晚做糖醋排骨，把步骤和计时放到屏幕上。” | 分步菜谱、火候提醒和倒计时；手上沾着油，说一句“下一步”就翻页 |
| 陪伴 | “把你自己放到电视上，陪我们聊会儿天。” | 智能体以形象出现在屏幕上，随对话做出表情和动作，声音变成一个在场的角色 |
| 大富翁 | “我们四个想玩大富翁，棋盘上电视，你来当银行。” | 棋盘、骰子、地契和资产表；掷骰子、买地、结算都由它接手 |
| 科普读物 | “给孩子做一本讲恐龙灭绝的书，能一页页翻。” | 图文分页、语音翻页，读到哪儿都能停下来问一句“为什么” |

这些应用没有一款是提前开发的，也不需要进菜单。**场景不在代码里，在你那句话里。** 想玩什么，说出来就行。

## 为什么值得做

**这道题的默认解法，是给灵矽写一个应用。我们反过来：让它当场写一个。**

灵矽没有屏幕，但中国有 4.39 亿台电视。只靠预设技能，语音硬件最终都会变成一台会说话的故事机；能当场写应用、投到身边的电视上，它就从玩具变成了现场的活动服务——每一块电视，都可以是灵矽的屏幕。

**一场三个人的知识竞赛，不会有公司为它立项。**

太具体、太短、太不值钱。但这样的场合每天都在发生：民宿今晚要接待客人，轰趴馆这周要办活动，他们本来就在为“这一场”付钱。我们按场定价：199 元一场，商家一年 3,999 元，多带来一场活动就回本。

**按场卖的不是成品，是产能。**

成品在出厂时就做完了，用户几天就能摸到边界，新鲜感过去就放进抽屉。产能是“你需要什么，它当场生产什么”：每遇到一个新场合，它就重新长出一个应用——在孩子房间是亲子问答，在客厅是三人竞赛，在店里是活动大屏。它可能一个月才用一次，但可以在客厅待五年。

**我们不是“一句话生成网页”。**

生成只是第一步：画面要上屏，几个人要一起玩，语音还要能继续操作——开始、下一题、查比分。四个人、四个当场生成的应用、一场之内全部玩完，中途还切换过。付费与复购尚未验证。

**先卖给谁**：民宿、轰趴馆、桌游店——每周都要为不同客人组织活动的场地方。

商业规划见[商业计划书](docs/product/business-plan.md)：市场机会、成品与产能的商业模型、七个年龄段需求地图、定价体系、第一周闭环、单元经济与三年目标。市场背景、产品定位与收费模式的完整论证见[商业策略](docs/product/business.md)。

## 它是怎么工作的

你说一句话：几个人、什么主题、投到哪块屏。系统听懂它，把它变成一个能跑的应用，再把画面送到电视上——**说话 → 听懂 → 生成应用 → 上屏 → 继续用语音操作**。

| 这一步 | 由谁完成 | 做什么 |
| --- | --- | --- |
| 听懂 | 灵矽（语音入口） | 识别场景、人数、主题与屏幕，决定调哪个工具 |
| 编排 | 控制面 | 校验请求、调度构建、转发操作 |
| 写代码 | AgentAPI | 在隔离环境里生成应用并自测 |
| 上屏 | Render | 启动应用、采集画面、送出媒体流 |

生成的应用把每个操作都声明成 OpenAPI，控制面提供稳定的 MCP 工具，所以语音不只能创建它，还能在运行时操作它：开始、下一题、查比分，都是一次业务调用。

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

- **先做技能，再接平台。** 灵矽和 AgentAPI 都是新平台、模型不熟悉，也没有现成技能包。我们先把官方文档封装成仓库内的 skill（[linx-use](.agents/skills/linx-use/SKILL.md) 41 页、[agentapi-use](.agents/skills/agentapi-use/SKILL.md) 34 页），让 Agent 基于文档接入，而不是猜接口。
- **先定架构契约，再让 Agent 并行。** 第一天下午三个人一起把架构拆分定下来；晚饭前给各自的 Agent 派发长任务，并写好验收标准。吃完饭回来，MVP 已经写完——一个多小时，一次跑通。架构清楚让任务能拆开，验收标准让 Agent 知道什么时候算完成。
- **用 mock 顶住并行开发的等待。** 三个人进度有快有慢，快的要等慢的接口。架构边界足够清楚，我们让 AI 按接口写大量 mock 先跑冒烟测试（59 个文件使用 `httptest`）。结果是**三个人第一次联调就一次性调通**。
- **把 OpenAPI 抽象成固定工具。** 不同应用有不同玩法，逐个对接会让语音侧随应用数量不断膨胀。我们让应用用 OpenAPI 声明自己的操作，语音侧只保留两个通用工具：读操作表、发请求。新应用上线不需要重新接入。
- **用对抗性视角写测试。** 让 AI 以网络安全里的对抗视角写测试：先假设这段代码会被恶意使用，再去找最容易攻破的地方。这样它不会为了覆盖率数字堆无效断言，而是认真检查边界上的风险；反过来也倒逼实现写得更可测试。仓库中有 15 个 `*_adversarial_test.go`。
- **10 个角度的独立评审。** 用 [code-review skill](.agents/skills/code-review/SKILL.md) 把 diff 交给 10 个互不通气的角度（5 个正确性、3 个清理、1 个改动层次、1 个约定），每个角度至多 8 条候选，再经单票三态验证与一轮补漏扫描，收敛到至多 15 条。独立会话避免被主对话带偏，也避免一个角度压制另一个；召回优先，宁可多报。

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

四个部件分布在三个互不重叠的平面上：输入、计算、显示。平面之间只有三条契约——MCP 让语音只看到工具；OpenAPI + `launch.json` + HTTP 让应用只暴露 HTTP；控制面调用投屏服务，只传媒体流地址与目标。当前投屏走 DLNA（MPEG-TS），AirPlay 是后续扩展。详见[架构设计](docs/architecture/overview.md)与[模块规格](docs/architecture/modules.md)。

## 快速开始

外部前置条件：Render 主机需要 Node.js 20 或更新版本、Chromium、带 `libx264` 与 AAC 的 FFmpeg，以及 `lsof`；阶段 0 与阶段 1 都不需要灵矽账号。只有最后接灵矽时才需要账号与公网 HTTPS。

```bash
cp gateway.example.yaml gateway.yaml
go build ./... && go test ./... -race
go run ./cmd/render -addr :8090 -public-url http://192.168.1.20:8090
go run ./cmd/casting -addr :8080 -interface en0
go run ./cmd/gateway -config gateway.yaml     # 控制面；独立模式需先填写 gateway.yaml 第 2 部分，门户模式需先构建前端并在 /admin 配置
go run ./cmd/voiceentry -serve                # 打开 http://127.0.0.1:8788/
```

本地或局域网调试时，把 `gateway.yaml` 的 `services.allow_private_targets` 设为 `true`；它同时放宽 custom 用户地址，只用于受信单租户环境。

先启动 Render 与 Casting，再启动 Gateway。端口分工：

| 进程 | 默认端口 | 用途 |
| --- | --- | --- |
| Gateway | 8091 | MCP、编排、Portal；`gateway.example.yaml` 已显式设置 |
| Render | 8090 | 应用进程、Chromium 渲染与 MPEG-TS 媒体流 |
| Casting | 8080 | DLNA 投屏控制 |
| 静态产物服务 | 8092 | 手工验收时给 Render 提供 `e2e/artifacts` 下载 |
| voiceentry 控制台 | 8788 | 模拟硬件的本地 Web 控制台 |

Gateway 的内置默认值仍是 `:8090`，与 Render 冲突；示例配置已改为 `:8091`，请保证 `addr`、`render.base_url`、`casting.base_url` 三者互不冲突。`artifacts.base_url` 必须指向 Gateway 自己（示例 `http://127.0.0.1:8091`），否则 Render 拉不到产物。

### 让第一个应用上屏

不需要先接灵矽，按阶段验收：

0. **只验 Render + Casting。** 用 `./scripts/package-scoreboard.sh` 打包 `e2e/testdata/scoreboard`，用 `python3 -m http.server 8092 --directory e2e/artifacts` 提供下载，再用 `curl` 在 Render 上启动计分板。拿到 `source` 后，用 `GET /v1/targets` 取 `target_id`，再 `POST /v1/projection-sessions` 把流交给 Casting。命令见 [Render 运行说明](docs/operations/render.md#可下载的计分板应用包) 与 [Casting 服务](docs/operations/casting.md#api-示例)。
1. **本地 MCP 建应用。** 保持 `portal.enabled: false`，在 `gateway.yaml` 填好 AgentAPI、Render、Casting（Render/Casting 在 `127.0.0.1` 或 `192.168.x` 时必须把 `services.allow_private_targets` 设为 `true`），然后连接 `http://127.0.0.1:8091/mcp`（Streamable HTTP，每次请求都带身份头）：

   ```bash
   curl -s http://127.0.0.1:8091/mcp \
     -H 'Content-Type: application/json' \
     -H 'Accept: application/json, text/event-stream' \
     -H 'X-Linx-Device-Id: dev-local' \
     -H 'X-Linx-Session-Id: sess-local' \
     -d '{"jsonrpc":"2.0","id":1,"method":"tools/call",
          "params":{"name":"create-app","arguments":{"prompt":"做一个两人掷骰子游戏"}}}'
   ```

   门户模式下还要带 `X-Linx-Agent-Id` 和 `Authorization: Bearer <MCP Token>`。之后用 `get-app-status` 等构建完成，再用 `get-screens` + `start-app` 上屏；工具与错误码见[应用管理 MCP](docs/architecture/interfaces/app-management-mcp.md)。
2. **接灵矽。** 准备灵矽账号、灵矽云能访问的公网 HTTPS 地址（`https://<域名>/mcp`），在 `/admin/settings` 完成平台配置。局域网地址只适合本机联调。

逐步说明见 [Gateway](docs/operations/gateway.md)、[Render](docs/operations/render.md)、[Casting](docs/operations/casting.md) 启动指南。Portal 前端在 `portal/` 下用 pnpm 构建。

## 文档

| 我想…… | 去哪 |
| --- | --- |
| 了解产品与商业 | [产品设计](docs/product/vision.md)、[商业策略](docs/product/business.md)、[商业计划书](docs/product/business-plan.md) |
| 看懂分层与契约 | [架构设计](docs/architecture/overview.md) |
| 看模块职责与分工 | [模块规格](docs/architecture/modules.md) |
| 查接口、数据与错误码 | [应用包](docs/architecture/interfaces/app-artifact.md)、[应用管理 MCP](docs/architecture/interfaces/app-management-mcp.md)、[控制面数据库](docs/architecture/database.md) |
| 看投屏与语音 | [Casting](docs/operations/casting.md)、[现场语音入口](docs/architecture/voice-entry.md) |

完整索引见 [docs/README.md](docs/README.md)。

## 团队

| 成员 | 分工 |
| --- | --- |
| 张之阳 [@luoliwoshang](https://github.com/luoliwoshang) | 架构设计、产品设计、商业规划、视频剪辑、Render 技术实现、Cast 端技术实现 |
| 邓军辉 [@minorcell](https://github.com/minorcell) | 架构设计、Gateway 技术、控制面业务架构、Portal 设计、质量保障 |
| 韩鲁鹏 [@LUPENGHAN](https://github.com/LUPENGHAN) | 灵矽技术实现、Portal 技术实现、测试环境搭建、架构设计、质量保障 |
