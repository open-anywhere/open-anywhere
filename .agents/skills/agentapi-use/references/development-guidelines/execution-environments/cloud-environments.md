<!-- 来源：https://developer-doc.qiniu.com/products/agentapi/development_guidelines/agentapi-execution-environments/13609-agentapi-execution-environments-cloud-environments | 由 sync-docs.py 自动生成，请勿手改 -->

# 云端沙箱参考



最近更新时间: 2026-10-20 14:09:22

AgentAPI 的云端环境运行在七牛 LAS Sandbox 上。基础环境记录预装软件及默认配置。

使用 `claudecode` 或 `codex` 时，可在下方“预装工具版本”中查看 Claude Code、Codex 的版本，并核对任务依赖的软件包。`native` 是 AgentAPI 服务内的运行时，不随沙箱预装；调用文件和命令工具时，仍会使用云端环境的软件与网络配置。运行时选择见[运行时和模型](../agent-configuration/runtime-and-models.md)。

创建环境时由平台选择版本，已有环境不会自动升级。额外安装的软件包由环境配置指定。

## 当前版本

| 项目   | 内容         |
| ---- | ---------- |
| 版本   | v20261016  |
| 发布日期 | 2026-10-16 |

## 基础环境

| 项目   | 内容                 |
| ---- | ------------------ |
| 操作系统 | Ubuntu 24.04.5 LTS |
| 架构   | amd64              |
| 默认用户 | root               |
| HOME | /root              |

## 默认资源

以下为当前版本云端模板记录的资源配置。

| 项目      | 配置   |
| ------- | ---- |
| CPU（核）  | 4    |
| 内存（MiB） | 8192 |

## 各地域磁盘

以下数值来自当前版本新模板的沙箱探针，表示探针时根分区的容量和可用空间。

| 地域 | 总容量（MiB） | 可用空间（MiB） |
| -- | -------- | --------- |
| 华东 | 20179    | 10511     |
| 美西 | 20248    | 10518     |

## 预装工具版本

以下版本来自当前版本的环境清单；环境额外安装的软件包以环境配置为准。对应安装包也会出现在下方的包清单中。

| 组件          | 版本      |
| ----------- | ------- |
| Claude Code | 2.1.278 |
| Codex       | 0.155.1 |
| OpenCode    | 1.18.31 |
| Pi          | 0.86.1  |

## 基础软件

| 软件        | 版本              |
| --------- | --------------- |
| chromium  | 153.0.8010.12   |
| cmake     | 3.28.3          |
| docker    | 29.1.3          |
| file      | 5.45            |
| gcc       | 13.3.0-6ubuntu2 |
| gh        | 2.101.0         |
| git       | 2.43.0          |
| go        | 1.24.13         |
| java      | 21.0.12.1       |
| node      | 22.23.2         |
| php       | 8.3.6           |
| psql      | 16.15           |
| python    | 3.12.3          |
| redis-cli | 7.0.15          |
| ruby      | 3.2.11          |
| rust      | 1.99.1          |
| sqlite3   | 3.45.1          |

## 包管理器

| 包管理器     | 版本      |
| -------- | ------- |
| bun      | 1.4.2   |
| bundler  | 2.4.19  |
| cargo    | 1.99.1  |
| composer | 2.10.3  |
| gem      | 3.4.19  |
| gradle   | 4.4.1   |
| maven    | 3.8.7   |
| npm      | 12.0.2  |
| pip      | 24.0    |
| pnpm     | 12.5.1  |
| uv       | 0.11.31 |
| yarn     | 1.22.22 |

## 预装包

以下按实际管理这些包的工具列出清单。npm、pnpm、Yarn 记录全局顶层包；pip 记录默认 Python 环境的已安装包，gem 记录本地 gems，cargo、uv、Composer 记录安装的工具或全局包。清单不包含项目依赖和下载缓存。

同一个软件可能同时出现在运行时或包管理器说明和包清单中，例如通过 npm 安装的 Codex、pnpm。全局安装的 npm 包不代表项目已经声明这些依赖。

### cargo

未安装包。

### composer

未安装包。

### gem

| 包名               | 版本      |
| ---------------- | ------- |
| abbrev           | 0.1.1   |
| base64           | 0.1.1   |
| benchmark        | 0.2.1   |
| bigdecimal       | 3.1.3   |
| bundler          | 2.4.19  |
| cgi              | 0.3.7   |
| csv              | 3.2.6   |
| date             | 3.3.3   |
| debug            | 1.7.1   |
| delegate         | 0.3.0   |
| did\_you\_mean   | 1.6.3   |
| digest           | 3.1.1   |
| drb              | 2.1.1   |
| english          | 0.7.2   |
| erb              | 4.0.2   |
| error\_highlight | 0.5.1   |
| etc              | 1.4.2   |
| fcntl            | 1.0.2   |
| fiddle           | 1.1.1   |
| fileutils        | 1.7.0   |
| find             | 0.1.1   |
| forwardable      | 1.3.3   |
| getoptlong       | 0.2.0   |
| io-console       | 0.6.0   |
| io-nonblock      | 0.2.0   |
| io-wait          | 0.3.0   |
| ipaddr           | 1.2.5   |
| irb              | 1.6.2   |
| json             | 2.6.3   |
| logger           | 1.5.3   |
| matrix           | 0.4.2   |
| minitest         | 5.25.1  |
| mutex\_m         | 0.1.2   |
| net-ftp          | 0.2.1   |
| net-http         | 0.4.1   |
| net-imap         | 0.3.9   |
| net-pop          | 0.1.2   |
| net-protocol     | 0.2.1   |
| net-smtp         | 0.3.4   |
| nkf              | 0.1.2   |
| observer         | 0.1.1   |
| open-uri         | 0.3.0   |
| open3            | 0.1.2   |
| openssl          | 3.1.0   |
| optparse         | 0.3.1   |
| ostruct          | 0.5.5   |
| pathname         | 0.2.1   |
| power\_assert    | 2.0.3   |
| pp               | 0.4.0   |
| prettyprint      | 0.1.1   |
| prime            | 0.1.2   |
| pstore           | 0.1.2   |
| psych            | 5.0.1   |
| racc             | 1.6.2   |
| rake             | 13.0.6  |
| rbs              | 2.8.2   |
| rdoc             | 6.5.1.1 |
| readline         | 0.0.3   |
| readline-ext     | 0.1.5   |
| reline           | 0.3.2   |
| resolv           | 0.2.3   |
| resolv-replace   | 0.1.1   |
| rexml            | 3.4.4   |
| rinda            | 0.1.1   |
| rss              | 0.3.1   |
| ruby2\_keywords  | 0.0.5   |
| securerandom     | 0.2.2   |
| set              | 1.0.3   |
| shellwords       | 0.1.0   |
| singleton        | 0.1.1   |
| stringio         | 3.0.4   |
| strscan          | 3.0.7   |
| syntax\_suggest  | 1.1.0   |
| syslog           | 0.1.1   |
| tempfile         | 0.1.3   |
| test-unit        | 3.5.7   |
| time             | 0.2.2   |
| timeout          | 0.3.1   |
| tmpdir           | 0.1.3   |
| tsort            | 0.1.1   |
| typeprof         | 0.21.3  |
| un               | 0.2.1   |
| uri              | 0.12.5  |
| weakref          | 0.1.2   |
| yaml             | 0.2.1   |
| zlib             | 3.0.1   |

### npm

| 包名                              | 版本      |
| ------------------------------- | ------- |
| @anthropic-ai/claude-code       | 2.1.278 |
| @earendil-works/pi-coding-agent | 0.86.1  |
| @openai/codex                   | 0.155.1 |
| @playwright/cli                 | 0.1.22  |
| bun                             | 1.4.2   |
| corepack                        | 0.34.6  |
| npm                             | 12.0.2  |
| opencode-ai                     | 1.18.31 |
| playwright                      | 1.63.0  |
| pnpm                            | 12.5.1  |
| yarn                            | 1.22.22 |

### pip

| 包名                 | 版本           |
| ------------------ | ------------ |
| anyio              | 4.15.1       |
| argcomplete        | 3.1.4        |
| backports.zstd     | 1.8.0        |
| build              | 1.6.1        |
| CacheControl       | 0.14.4       |
| certifi            | 2026.7.22    |
| cffi               | 2.1.1        |
| charset-normalizer | 3.5.2        |
| cleo               | 2.1.0        |
| contourpy          | 1.4.0        |
| crashtest          | 0.4.1        |
| cryptography       | 50.0.2       |
| cycler             | 0.12.1       |
| distlib            | 0.4.3        |
| dulwich            | 1.2.17       |
| et\_xmlfile        | 2.0.0        |
| fastjsonschema     | 2.22.2       |
| filelock           | 4.1.1        |
| findpython         | 0.8.0        |
| fonttools          | 4.66.1       |
| greenlet           | 3.5.6        |
| h11                | 0.16.0       |
| httpcore           | 1.0.9        |
| httpx              | 0.28.1       |
| idna               | 3.20         |
| installer          | 1.1.0        |
| jaraco.classes     | 3.4.0        |
| jaraco.context     | 6.1.2        |
| jaraco.functools   | 4.6.0        |
| jeepney            | 0.9.0        |
| keyring            | 25.7.0       |
| kiwisolver         | 1.5.1        |
| lxml               | 6.1.3        |
| matplotlib         | 3.11.2       |
| more-itertools     | 11.2.0       |
| msgpack            | 1.2.3        |
| numpy              | 2.5.4        |
| openpyxl           | 3.1.5        |
| packaging          | 26.3         |
| pandas             | 3.0.6        |
| pbs-installer      | 2026.10.3    |
| pillow             | 12.3.0       |
| pip                | 24.0         |
| pkginfo            | 1.13         |
| platformdirs       | 4.13.0       |
| playwright         | 1.63.0       |
| poetry             | 2.5.1        |
| poetry-core        | 2.5.0        |
| pycparser          | 3.11         |
| pyee               | 13.0.1       |
| pyparsing          | 3.3.3        |
| pypdf              | 6.20.1       |
| pyproject\_hooks   | 1.3.3        |
| python-dateutil    | 2.9.0.post0  |
| python-discovery   | 1.6.2        |
| python-docx        | 1.2.0        |
| python-pptx        | 1.0.2        |
| PyYAML             | 6.0.1        |
| RapidFuzz          | 3.14.6       |
| requests           | 2.34.2       |
| requests-toolbelt  | 1.0.0        |
| SecretStorage      | 3.5.0        |
| setuptools         | 68.1.2       |
| shellingham        | 1.5.4        |
| six                | 1.17.0       |
| toml               | 0.10.2       |
| tomlkit            | 0.15.1       |
| trove-classifiers  | 2026.9.21.13 |
| typing\_extensions | 4.16.0       |
| urllib3            | 2.8.0        |
| virtualenv         | 21.14.7      |
| wheel              | 0.42.0       |
| xlsxwriter         | 3.2.9        |
| xmltodict          | 0.13.0       |
| yq                 | 3.1.0        |

### pnpm

未安装包。

### uv

未安装包。

### yarn

未安装包。

## 环境变量

| 变量                         | 值                         |
| -------------------------- | ------------------------- |
| CHROME\_BIN                | /opt/pw-browsers/chromium |
| PLAYWRIGHT\_BROWSERS\_PATH | /opt/pw-browsers          |

## 本版本更新

* 预装 Task v3.44.1，支持运行仓库 Taskfile 中的任务。
* 预装 libopus-dev、libopusfile-dev，支持依赖 Opus 的项目编译。
* 预装 Playwright CLI 0.1.22 及匹配 Chromium；修正启动包装器，仅在 open 时传入浏览器配置，支持页面截图。

\<!-- environment:end -->

## 相关页面

* [云端环境](https://developer-doc.qiniu.com/core/environment)：配置软件包和网络策略。
* [启动会话](https://developer-doc.qiniu.com/core/session)：在指定云端环境中启动会话。
