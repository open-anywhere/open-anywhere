# 方案：应用商城（公开、浏览、复制）

> **状态：已实现（M1–M4）。** 数据层、门户接口与页面、语音工具均已完成；前端测试未补（本机 Node 26 无法运行 vitest）。
>
> 与最初设计的差异：发布改为需要管理员审核（`pending/approved/rejected`），"隐藏"由"拒绝"承担，没有单独的 hidden 开关；安装改为用户选择设备（设备级归属）。

## 1. 一句话

**公开一个应用 = 发布它的产物；复制一个应用 = 复制产物并登记成你自己的 `app_id`。** 整个过程不经过 AgentAPI、不重新构建，秒级完成。

## 2. 用户故事

1. 用户 A 做出一个答题应用，在门户里点「公开」，填标题和一句介绍。
2. 用户 B 在「应用商城」看到它，点「添加到我的应用」，改名为「我们的答题夜」，得到一份可以立刻启动的应用；A 后续改版或下架，都不影响 B 手里这一份。
3. 用户 B 想改内容，发现自己拿到的是"能跑但不能改"的副本——需要走一次重新生成（见 §4.7）。

## 3. 现状与约束（决定方案形状的事实）

| 事实 | 位置 | 对方案的影响 |
| --- | --- | --- |
| 应用记录是 `app.App`，持有 `Artifact`、`OpenAPI`、`Name`、`Prompt`、`SessionID` | `internal/gateway/app/app.go` | 复制就是"造一条新记录"，不需要跑构建 |
| 产物是 `artifacts/{app_id}.tar.gz`，带下载 token | `internal/gateway/artifact/store.go:80` | 复制要落一份新文件，不能共享路径 |
| 归属是 `(device MAC, agent id)` | `app.Identity` | 复制出来的应用必须挂在复制者自己的设备下 |
| 每个 `(device, agent)` 只保留最近 10 个应用 | `service/history.go` `HistoryLimit` | 下载商城应用会占用配额，可能挤掉旧应用 |
| `update-app` 依赖绑定 AgentAPI 会话 | `service/service.go:359` | 副本没有会话 → 只能运行，不能修订 |
| 门户目前没有创建/启动应用的接口 | `portal/http.go:112-115` | 商城只需要「复制」和「列表」，不需要启动 |
| 生成的应用视为不可信输入 | CLAUDE.md | 公开不改变校验：Render 启动时仍会再校验产物 |

## 4. 设计

### 4.1 数据模型

新增一张表，不给 `apps` 的 JSON 加字段——发布状态和生命周期与应用本身解耦，删应用时只需删一行。

```sql
CREATE TABLE app_publications (
  app_id        TEXT PRIMARY KEY,     -- 指向 apps.id
  owner_user_id TEXT NOT NULL,        -- 发布者，用于「我的发布」
  title         TEXT NOT NULL,        -- 商城展示名
  summary       TEXT NOT NULL,        -- 一句介绍
  hidden        INTEGER NOT NULL DEFAULT 0,  -- 管理员隐藏
  published_at  TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);
CREATE INDEX app_publications_list ON app_publications(hidden, published_at DESC);
```

### 4.2 公开什么

公开的是**产物和接口**，不是作者的原始输入：

| 字段 | 是否公开 | 理由 |
| --- | --- | --- |
| `title` / `summary` | 是 | 作者自己填的展示信息 |
| 应用名（`launch.json`）、操作列表、参数 schema | 是 | 复制后要用 |
| 产物内容 | 是（复制时读取） | 复制的前提 |
| `Prompt` | **否** | 可能包含玩家姓名、家庭信息等 |
| `PlayerNames` | **否** | 同上 |
| 产物下载地址与 token | **否** | 只在服务端内部使用 |

### 4.3 复制语义

复制 = 一次事务里的三步：

1. 读源应用，确认产物存在（`artifacts.Has(sourceID)`），否则 `ARTIFACT_MISSING`；
2. 复制产物文件到 `artifacts/{newID}.tar.gz`（新增 `Store.Copy(from, to)`，复制后重算摘要、生成新 token）；
3. 在注册表新增一条记录：

```go
app.App{
    ID:            newID,
    Owner:         targetIdentity,         // 复制者自己的 (MAC, agent)
    Name:          requestedName,          // 用户输入的新名字
    Prompt:        "从应用商城复制：" + title, // 不复制原 prompt
    State:         app.StateIdle,          // 直接可启动
    Artifact:      copiedItem,
    OpenAPI:       deepCopy(source.OpenAPI),
    InstalledFrom: sourceID,               // 标记来源，决定 update-app 的行为
}
```

关键取舍：

- **复制而非引用**：单份产物约 10–100 KB，共享路径会让"源应用删除/改版"波及所有副本，得不偿失；
- **不复制 `Prompt` 与 `PlayerNames`**：隐私边界，也让"复制后改内容"必须走一次真正的生成；
- **复制后是 `idle`**：用户点「启动」就能用，当前 `start-app` 路径不需要任何改动。

### 4.4 接口

门户新增六个端点，全部走现有 session 鉴权与 owner 校验：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `PUT` | `/api/portal/apps/{id}/publication` | 发布或更新展示信息 `{title, summary}`；仅 owner |
| `DELETE` | `/api/portal/apps/{id}/publication` | 下架；仅 owner |
| `GET` | `/api/portal/store?query=&limit=&offset=` | 商城列表（隐藏的与已下架的不返回） |
| `GET` | `/api/portal/store/{id}` | 详情：标题、简介、操作列表、体积；不含 prompt 与下载地址 |
| `POST` | `/api/portal/store/{id}/clone` | `{name, device_id}` → `{app_id}`；`device_id` **必填**，用户自己选装到哪台设备 |
| `POST` | `/api/portal/apps/{id}/rename` | `{name}`：只改展示名，不动产物里的 `launch.json` |

管理员：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/admin/store` | 全部发布记录，含 hidden 与发布者 |
| `POST` | `/api/admin/store/{id}/hide` | `{hidden}`：隐藏/恢复，用于处置不合适的内容 |

错误语义沿用现有风格：

| 情况 | 结果 |
| --- | --- |
| 发布别人的应用 | `404 APP_NOT_FOUND`（不区分"不存在"与"不是你的"） |
| 发布 `building` 中的应用 | `409 APP_BUILDING` |
| 发布没有产物的应用 | `409 ARTIFACT_MISSING` |
| 复制未发布/已隐藏的应用 | `404 STORE_ITEM_NOT_FOUND` |
| 复制时产物文件丢失 | `409 ARTIFACT_MISSING`，提示作者重新发布 |
| 复制超过该账号设备上限/配额 | `409 QUOTA_EXCEEDED` |

### 4.5 权限模型

- **发布**：应用的 owner（门户里能看见这个应用的人）才可以。
- **浏览与复制**：本部署内**所有已登录用户**，不区分租户角色；管理员额外能隐藏。
- **删除源应用**：必须同时删掉发布记录（见 §4.6），已复制出去的不受影响。
- **跨部署**：不做。商城是"本控制面内"的概念，与 MCP 身份是同一套边界。

### 4.6 生命周期

| 事件 | 源应用 | 发布记录 | 已复制的应用 |
| --- | --- | --- | --- |
| 作者下架 | 不变 | 删除 | 不受影响 |
| 作者重新发布/更新文案 | 不变 | 更新 | 不受影响 |
| 作者删除应用 | 删除 | **必须一并删除** | 不受影响（各自有产物） |
| 历史裁剪（第 11 个应用挤掉最旧） | 删除 | 同上 | 不受影响 |
| 管理员隐藏 | 不变 | `hidden=1` | 不受影响 |

删除与裁剪发生在 `service` 层，但不需要任何回调：`app_publications.app_id` 是对 `apps(id)` 的外键，`ON DELETE CASCADE`。删应用（含历史裁剪）时发布记录自动消失。已用测试验证（`TestDeletingAnAppRemovesItsPublication`）。

### 4.7 复制出来的应用"能跑不能改"，而且要**明确说出来**

复制时不带 AgentAPI 会话。如果沿用现状，`update-app` 会走进 `SessionID == ""` 这条分支，返回 `SESSION_UNAVAILABLE`——但这个码的含义是"构建会话丢了，重新 `create-app`"，模型照做会反复重建，用户也看不出区别。

所以给应用记录加一个来源标记：

```go
InstalledFrom string // 非空表示它是从商城安装的，值为来源 app_id
```

`update-app` 在进入会话检查之前先判断它：

| 应用来源 | `update-app` 返回 | 给用户的话 |
| --- | --- | --- |
| 自己创建/修订的 | 走现有逻辑（正常提交修订） | — |
| 从商城安装的 | `409 APP_INSTALLED` | "这是从应用商城添加的成品，不能直接改；告诉我新需求，我重新做一个你的版本" |
| 会话确实丢失的自建应用 | `SESSION_UNAVAILABLE`（不变） | "这个应用没法继续修订了，需要重新生成" |

`APP_INSTALLED` 同时出现在 `app-management-mcp.md` 的错误表和主持提示词里。门户则在应用卡片上显示"来自商城"，并在详情页给出同样的说明。

后续可选的增强（另立提案）："基于这个应用定制"——给副本开一个新的 AgentAPI 会话，把产物放进沙箱继续改。技术上要先验证沙箱能否取到我们的产物（现有 `artifacts/...` 下载地址对 Render 开放，未验证 AgentAPI 沙箱可达），以及是否允许把源应用的实现暴露给复制者。

## 5. 一个真实操作走完整条链

```
A 在 /portal/apps 点「公开」→ PUT /api/portal/apps/{app}/publication {title, summary}
   → 写 app_publications 一行

B 打开 /portal/store → GET /api/portal/store
   → 读 app_publications JOIN 注册表中的应用（只取公开字段）

B 点「添加到我的应用」，输入名字 → POST /api/portal/store/{app}/clone {name}
   → 校验发布记录存在且未隐藏
   → artifacts.Copy(source, newID)
   → apps.Add(App{Owner: B 的虚拟设备, State: idle, ...}) + 持久化
   → 返回 {app_id}

B 在 /portal/apps 点「启动」→ 走现有 start-app（门户后续补启停接口，见 tenant-screens 方案）
```

## 6. 数据与状态归属

| 数据 | 归属 | 变化 |
| --- | --- | --- |
| 应用记录与状态 | 控制面 SQLite `apps` | 复制新增一条 `idle` 记录，状态机不变 |
| 产物 | `artifacts/{app_id}.tar.gz` | 复制多一份文件 |
| 发布信息 | 新增 `app_publications` | 与 `apps` 一一对应，可单独删除 |
| 运行时 | Render | 不变（复制不启动） |

## 7. 兼容性与影响面

- **MCP 契约不变**：不新增工具，`list-apps` 仍然只返回当前 owner 的应用；商城是门户能力。
- **状态机不变**：没有新状态，复制出来的应用就是普通的 `idle`。
- **配额**：复制计入每个 `(device, agent)` 的 10 个历史上限，界面需要提前提示。
- **不可信产物**：公开不绕过任何校验；Render 启动时仍会做 `launch.json` 与结构校验。

## 8. 第一块可独立完成和验证的改动

**M1：发布 + 列表 + 权限**，不含复制。

- 建表、`store` 层 CRUD；
- `PUT/DELETE /api/portal/apps/{id}/publication`、`GET /api/portal/store`；
- owner 校验与"别人的应用一律 404"；
- 测试：发布/下架/列表可见性/越权。

它不依赖产物复制，也不改变任何现有行为，可以单独上线验证"发布"这条路是否被接受。

## 9. 测试与验收

| 用例 | 判据 |
| --- | --- |
| 发布自己的应用 | 另一个账号的商城列表出现它 |
| 发布别人的应用 | `404`，列表无变化 |
| 下架 | 列表立即消失，直接访问详情 `404` |
| 复制 | 新 `app_id`、`state=idle`、`start-app` 可成功 |
| 复制后删除源应用 | 副本仍可启动（产物独立） |
| 复制不泄露输入 | 详情与副本记录里都没有源 `Prompt`/`PlayerNames` |
| 隐藏 | 列表与复制入口都返回 `404`，作者仍能在「我的发布」里看到状态 |
| 删除应用 | 发布记录同步删除，不再出现在商城 |
| 历史裁剪 | 被裁掉的源应用，其发布记录一起消失 |
| 超配额 | 返回 `QUOTA_EXCEEDED`，不产生半成品（文件与记录都要回滚） |

## 10. 里程碑

| 阶段 | 内容 | 估计 |
| --- | --- | --- |
| M1 | 表 + 发布/下架 + 列表 + 权限 | 0.5–1 天 |
| M2 | 复制（`Store.Copy` + 注册 + 回滚）+ 门户「添加到我的应用」 | 1 天 |
| M3 | 管理员隐藏 + 删除联动 + 配额提示 | 0.5 天 |
| M4 | MCP：`list-store-apps` / `install-app` + `APP_INSTALLED` + 提示词 | 1–2 天 |

开发阶段四期都在本方案范围内，不再拆分；M4 的 `APP_INSTALLED` 依赖 M2 的 `InstalledFrom` 字段。

## 11. 风险与取舍

| 风险 | 处理 |
| --- | --- |
| 恶意或低质公开内容 | 管理员可隐藏；不做自动审核（生成内容本就是不可信输入） |
| 存储增长 | 单份 10–100 KB；历史裁剪会回收 |
| 用户以为复制后能改 | 界面上写清"复制的是成品"；§4.7 的定制路径列为后续 |
| 名字冲突 | 名字只是展示，不唯一 |
| 隐私 | 只公开作者填写的标题与简介，不公开原始 prompt |

## 12. MCP 面（第二阶段）

商城第一版只做门户。语音侧的入口单独设计，因为 MCP 面有一条硬约束：**工具集稳定，不按应用或 OpenAPI 操作注册工具**（见 CLAUDE.md）。当前稳定面是 7 个工具：`create-app` / `update-app` / `get-app-status` / `get-app-operations` / `start-app` / `stop-app` / `send-request`。

### 12.1 为什么不能复用 create-app

`create-app` 的契约是"立即返回 `building`，异步构建，完成后发通知"。安装商城应用是同步的、没有构建、直接 `idle`。把两种结果塞进同一个工具会让返回值语义分裂（`state` 可能是 `building` 也可能是 `idle`），模型很难写对分支。**新增独立工具比改语义更安全。**

### 12.2 新增两个工具

| Tool | 输入 | 输出 | 说明 |
| --- | --- | --- | --- |
| `list-store-apps` | `query?`、`limit?`（默认 10，上限 20） | `items[]`：`store_id`、`title`、`summary`、`operation_count`、`updated_at` | 只返回已发布且未隐藏的应用；不返回 prompt、作者、产物地址 |
| `install-app` | `store_id`、`name?`（用户确认过的名字） | `app_id`、`name`、`state: idle` | 等价于门户的 clone：复制产物、登记成本调用者的 `(device, agent)` |

这两条把稳定面从 7 个扩到 9 个，属于**有意的契约扩展**：它们是产品能力，不是按应用注册的工具，不违反"工具集不随应用增长"的原则。上线前需要同步：

- `docs/architecture/interfaces/app-management-mcp.md` 的工具表与错误表；
- `docs/agent-system-prompts/open-anywhere-host.txt`（改完再复制到 `portal/agent-prompt.txt`，`TestEmbeddedPromptMatchesDocs` 会校验）；
- 灵矽侧的 `tools/list` 会多两个工具，需确认工具数量上限没有被顶掉（超出会静默丢弃，这是个已知风险）。

### 12.3 语义与错误

| 场景 | 行为 |
| --- | --- |
| `store_id` 不存在 / 已下架 / 被隐藏 | `STORE_ITEM_NOT_FOUND` |
| 对安装来的应用调用 `update-app` | `APP_INSTALLED`（401/409 语义同其它工具错误，不改状态） |
| 源应用产物丢失 | `ARTIFACT_MISSING`（提示作者重新发布） |
| 该账号设备配额已满 | `QUOTA_EXCEEDED` |
| 名字为空或超长 | `INVALID_ARGUMENT`，不改任何状态 |
| 同一应用装两次 | 允许，会产生两个 `app_id`；模型应先用 `list-apps` 看有没有装过 |

安装是**同步**的：不产生通知，不需要 `get-app-status` 轮询。装完直接可以 `start-app`。

### 12.4 装完之后模型必须知道的事

副本没有 AgentAPI 会话，所以 `update-app` 会返回 `SESSION_UNAVAILABLE`。主持提示词要写清：

- 安装后先告诉用户"已添加，可以直接开始"；
- 用户说"改一下内容"时，先看 `update-app` 的返回：`APP_INSTALLED` 说明这是商城里的成品，按新需求用 `create-app` 重新生成，不要重试 `update-app`；`SESSION_UNAVAILABLE` 才是会话真的丢了；
- 不要朗读 `app_id` 或 `store_id`（与 `stream_url` 同一条规则）。

### 12.5 不可见字段

MCP 返回里同样不出现 `Prompt`、`PlayerNames`、产物下载地址与发布者信息——与门户的可见性规则一致，避免"语音侧能问到网页上查不到的东西"。

## 13. 交付后的功能（确认用）

**作者**
- 在自己的应用上点「公开」，填标题和一句简介；可随时改文案、下架。
- 应用被删除或被历史裁剪时，发布记录自动消失。

**取用者**
- 打开「应用商城」，按关键字搜标题与简介，看到标题、简介、操作数量。
- 点「添加到我的应用」，**选择装到哪台设备**并起名，**秒级**得到一份属于自己的应用（独立产物），可以启动、改名、投屏。
- 作者之后改版、下架、删除，都**不影响**已经取走的那份。
- 想改内容：语音里说新需求重新生成；直接改会被明确拒绝并说明原因（`APP_INSTALLED`）。

**管理员**
- 看到全部发布记录（含发布者、隐藏状态），可以隐藏或恢复；隐藏后门户与语音都看不到。

**语音**
- 用户问"有没有现成的答题应用"→ 列出商城里匹配的几条标题 → 用户选中 → 安装 → 告知"已添加，可以直接开始"。
- 用户要改一个装来的应用 → 收到 `APP_INSTALLED` → 说明这是成品，改用新需求重新生成。
- 全程不朗读 `app_id`、`store_id`、任何链接。

**明确不做**：跨部署商城、收费/积分、人工审核、复制后继续编辑（另立提案）。

## 14. 已确认的决策（2026-11-01）

| # | 决策 | 实现影响 |
| --- | --- | --- |
| 1 | 商城对所有登录用户可见 | 列表不做角色过滤，只过滤状态 |
| 2 | 不显示作者用户名 | 列表与详情不返回 owner_user_id 之外的发布者信息 |
| 3 | 同一应用不能重复发布多份 | `app_id` 唯一，重复发布等于更新文案 |
| 4 | **发布需要管理员审核** | 状态机 `pending → approved / rejected`；列表只返回 `approved`；作者修改已通过的条目会退回 `pending` |
| 5 | 暂不做标签 | 只按标题与简介做关键字匹配 |
| 6 | **配额满时拒绝复制** | 账号下已有 10 个应用时返回 `QUOTA_EXCEEDED`，提示先删除；不再自动挤掉最旧的 |
| 7 | **保持设备级归属**；安装时用户选择目标设备，语音安装装到当前对话的设备 | 归属键不变，克隆按 `device_id` 落到指定设备；不做账号级共享 |
| 8 | 模型按需推荐商城 | 只在用户问"有没有现成的"或需求笼统时推荐 |
| 9 | 不统计被安装次数 | v1 不做 |

### 14.1 审核流程

```
作者 PUT /api/portal/apps/{id}/publication  → status=pending（修改已通过的条目也会退回 pending）
管理员 POST /api/admin/store/{id}/review {decision: approve|reject, note?}
   approve → 出现在商城
   reject  → 只在作者的「我的发布」里显示，带审核意见
```

- 审核意见只对作者可见，不进商城列表。
- 管理员可以在任意时刻把已通过的条目改回 `rejected` 或删除（等价于下架）。

### 14.2 归属：保持设备级

应用仍然属于 `(device MAC, agent id)`，不做账号级共享。

- **门户**：登录后能看到名下所有设备的应用（这是现有的账号级视图，不需要改）。
- **安装**：用户选择装到哪台设备，就落在那台设备的 `(MAC, agent)` 下；一台设备装一份。
- **语音**：MCP 的调用身份本身就是设备，所以 `install-app` 装到**当前正在对话的这台设备**，不需要额外参数。
- **两台设备想各玩一份**：各自装一次，得到两个 `app_id` 与两份产物，互不影响。
- **同一应用不共享运行时**：即使两个副本内容相同，它们也是两个独立实例。

这条决定了 M2 的实现：克隆接口必须带 `device_id`，且只能是自己名下、`state=registered` 的设备；语音侧不需要设备参数。

### 14.3 删除应用现在在哪

- 门户：应用详情页的删除按钮 → `DELETE /api/portal/apps/{id}`（`internal/gateway/portal/http.go:115`）。
- MCP：没有删除工具（工具集里只有创建、修订、查询、启动、停止、调用）。
- 自动回收：每个 `(device, agent)` 只保留最近 10 个，超出时最旧且不在运行的应用会被裁剪（`service/history.go`）。
