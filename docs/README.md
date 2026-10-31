# 文档

本目录共 21 个 Markdown 文件：这份索引加 20 篇正文，按**你要解决的问题**分三类，不按文件类型堆在一起。

## 我怎么找

| 我想…… | 去哪 |
| --- | --- |
| 知道这东西为什么值得做 | [产品愿景](product/vision.md)、[商业策略](product/business.md)、[商业计划书](product/business-plan.md) |
| 看懂系统怎么分层、模块怎么切 | [架构总览](architecture/overview.md)、[模块规格](architecture/modules.md) |
| 弄清现场怎么部署、云端与现场怎么连 | [现场侧投屏](architecture/home-casting.md) |
| 查某条契约、某个接口 | [architecture/interfaces/](architecture/interfaces/) |
| 把服务跑起来、排查现场问题 | [operations/](operations/) |
| 看后续方向与同类调研 | [产品调研](product/research.md) |

## 目录

```text
docs/
├── product/                    为什么做、卖给谁
│   ├── vision.md               产品愿景、用户、体验
│   ├── business.md             市场、定位、收费与验证
│   ├── business-plan.md        商业模型、第一周闭环、年龄分段、定价与三年目标
│   └── research.md             同类产品调研与扩展方向
├── architecture/               系统怎么切、契约是什么
│   ├── overview.md             分层、上下文、三条契约、应用即接口与关键取舍
│   ├── modules.md              模块职责、接口、不变量、分工
│   ├── database.md             控制面持久化模型
│   ├── voice-entry.md          现场语音入口与设备协议
│   ├── home-casting.md         现场部署与控制链路
│   └── interfaces/             六份契约
│       ├── app-artifact.md     应用包与 launch.json
│       ├── app-management-mcp.md
│       ├── gateway-agentapi.md
│       ├── gateway-render.md
│       ├── portal.md
│       └── multi-app-sessions.md
├── operations/                 怎么跑、怎么排查
│   ├── gateway.md              控制面启动、MCP 接入与门户
│   ├── render.md               应用运行时、媒体流与端到端验收
│   ├── casting.md              设备发现与 DLNA 投屏会话
│   ├── yunliao-s3-max-firmware-mcp.md  小智云聊 S3 MAX 固件内置 self.cast.*
│   └── deploy.md               首次配置、备份与恢复
├── agent-system-prompts/       构建与主持用的系统提示词
└── assets/                     品牌与社交预览素材
```

## 约定

- **一个事实只有一个权威来源。** 总览只讲结构与取舍，细节一律链接到契约或运维文档。
- **契约变更必须同时改文档。** 接口、状态机、错误码三者保持一致。
- **术语以[架构总览](architecture/overview.md)的术语表为准。**
- 部署、进程与端口只出现在 `operations/`，不混进架构与产品文档。
