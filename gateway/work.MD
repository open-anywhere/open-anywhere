# Go Gateway 迁移记录

日期：2026-10-24。分支 feat/openapi-mcp，基于 main 46d409c。所有改动尚未提交。

## 决策和实现

用户指定 Gateway 全部使用 Go。本轮把 Node 原型的 MCP 协议入口、OpenAPI 调用和 speaker probe 迁移为原生 Go，不通过 Go 拉起 Node。使用官方 modelcontextprotocol/go-sdk v1.8.0；schema 校验使用 santhosh-tekuri/jsonschema/v6；YAML 用 yaml/v3。复用根 go.mod，gateway/ 是独立可执行入口。

启动时加载注册的文档，构造不可变索引；get_openapi 返回副本，send_request 校验参数后执行。文档内部引用允许，外部/递归引用拒绝；HTTP 跳转不跟随，目标来自注册表。OpenAPI 3.0 nullable/exclusive bound 转为 JSON Schema 语义；3.1 使用 2020-12。未支持的 schema 关键字返回错误。只有标量 path/query/header 和 JSON body 属于首版范围。

MCP SDK 的默认 localhost 防护会拒绝 frp 公网 Host。实测第一次公网 initialize 返回 403，随后增加 -public-host 显式允许预期 Host；非允许 Host 和跨 Origin 继续拒绝，新增对应回归测试。MCP_TOKEN 保留 Bearer 校验，联调端点沿用无 token 的 demo 配置。

旧 Node 源码、package 文件、测试和 node_modules 移至 doc/local-archive-20261024/gateway-node-reference，Git 忽略。doc/go.mod 隔离历史 Go 源码，避免根 go test ./... 意外编译旧 client。doc/ 不参与提交。

## 测试证据

- Go SDK HTTP 初始化、工具发现、两个正式工具、启用探针后的第三工具均通过。
- schema：3.0.3/3.1.0、局部引用、path/query/body、nullable、exclusive bound、缓存副本。
- 失败路径：缺参、未知操作/服务/参数、非法路径、类型错误、额外 body 字段、未提供 body、应用 409、跳转、非 JSON、1 MiB 超限、超时；断言没有隐式重试。
- MCP 输入 unknown field 拒绝；声纹空值接受、缺字段拒绝、verified:false；Bearer、public Host 和 Origin 测试。
- 启动取消和端口冲突测试。
- Node MCP SDK 连接 Go 8789，工具发现、规范获取、探针回传通过，作为迁移兼容性证据；常规测试本身全部用 Go。
- Go 客户端对 8789、本机 8787 和公网 6004 的真实 MCP 游戏测试：reset、start 409、p1/p2/p3 ready、start、submitAnswer、evaluateAnswer、pause、resume、getGameState、reset。u2 得 1 分并进入第二题。
- 全仓库 go test ./... -race、go vet ./...、Windows amd64 无 cgo 构建通过。Windows 二进制未在 Windows 机器实跑。

## 运行与限制

Go 服务监听 0.0.0.0:8787，通过已有 frp 映射到 http://118.89.179.167:6004/mcp。日志 gateway/.runtime/go-server.log，PID gateway/.runtime/server.pid；以实际 PID 文件为准。声纹探针开启。后台进程不等于系统开机常驻。

Go 迁移后的真人语音/声纹测试尚未重新执行，不抢占另一台 client 的设备连接。SDK 公网调用通过不能代替灵矽模型自主调用的重新验收。尚无服务权限绑定、业务认证注入、自动规范刷新、应用生命周期/AgentAPI、复杂参数样式、文件/流式代理。内存 demo 返回答案等字段的行为由应用负责。


## 通用范围澄清

本轮只修改说明文档，区分通用 OpenAPI 执行器与静态服务来源。services.json 保留为开发配置；正式目标是 ApplicationResolver 按 app_id 查询应用注册表，取得规范、运行地址、状态和归属。该接口尚未实现，公网仍使用 service_id。同步修正文档中的用户名身份约定及未实现的认证注入/归属校验说明。未修改 Go 代码、配置或运行进程。

## 应用管理扩展（2026-10-24）

基线同步到 main 9fc4018，保留本地 Gateway 代码。按用户要求扩展成 Go 控制层应用，采用单 Gateway 进程和模块化内部调用，client 不在此工作区开发。

### 代码工作步骤

1. 将 OpenAPI 文档编译与 HTTP 下载解耦，新增 FromDocument，使应用注册表直接提供已校验的产物规范。
2. 新增 application.Manager：JSON 持久记录、归属检查、异步构建、同会话重复创建去重、恢复事件游标、运行状态和资源占用。
3. 新增 Render/Casting HTTP 客户端；start-app 按端口检查、Render ready、Casting playing 顺序推进。stop-app 先关闭投屏再关闭 Render，失败保留占用，未知结果不盲目重试。
4. 新增 AgentAPI 客户端：固定 agent/version/env 创建会话，先订阅 SSE 再发 user.message，记录提交意图防重复提交，回放去重、忽略任务前 idle，只有 end_turn 收集文件。分页获取 artifact、限制下载并归档成功会话。
5. 新增 artifact.Store：tar.gz 大小/路径/文件类型检查、launch/OpenAPI 校验、SHA256、不可猜 URL、原始包保留供 Render 下载。
6. MCP 管理模式增加五个应用管理工具，OpenAPI 工具输入改为 app_id；应用记录提供实际地址。静态模式仍支持 service_id，仅供已有 quiz 联调。
7. 增加单 Render/Casting 的部署配置，默认管理模式可在无外部依赖时启动，对不可用能力返回明确错误。
8. 竞态测试发现构建协程共享事件 map，已改成持久化时深拷贝。新增并发启动、跨归属拒绝、资源清理失败、未知资源占用和持久化失败测试。
9. 文档拆分保持：architecture.md 简述，docs/gateway.md 说明服务职责、配置、状态、限制；README 提供启动和测试入口。

### 验证范围

Go 单元/协议测试覆盖 AgentAPI SSE 断线回放和停止原因、文件分页、包校验、状态持久化和恢复。真实 HTTP 集成测试使用 httptest 实现 Render/Casting，对 MCP 七工具调用创建、状态、屏幕、启动、业务调用、停止，断言 username 透传和关闭顺序。它验证客户端协议，不代表真实云端构建或电视播放。

管理版测试进程在 127.0.0.1:8789，健康状态 application_management:true；工具发现能列出七个工具和可选探针。公网继续用兼容静态配置为游戏服务，避免在没有下游凭证时把现有灵矽调用改成不可用。

### 已知部署缺口

未提供真实 AgentAPI key/agent/environment、Render/Casting 地址和屏幕映射，因此没有发起真实收费构建或电视投屏。主动 notifications/app_status 未实现；客户端查询状态。超时中断后的最长等待/归档补偿、下载 URL 过期和未知实例自动对账仍需完善。只支持单进程 JSON 注册表，不能多副本写入。Windows 只交叉编译，文件替换/权限和下游运行仍需实机验收。应用状态 error 的资源清理保持原状态，不假装自动恢复可启动。

## 自审补充

用户要求复核真实可接通程度。发现并修复三项：调用前刷新 Render/Casting 状态，严格 Node 包内入口参数，拒绝并避免清理其他 app_id 的实例。回归测试先复现失败再通过。正常停止补 stopped_at，错误补 last_agent_message。部署实际 tools/call 显示 BUILD_UNAVAILABLE 和 DEPENDENCY_UNAVAILABLE。详细边界和交付输入见 REVIEW.md。

## 管理模式公网工具发现

用户要求现在接入灵矽检查工具。停止原 8787 静态进程和 8789 管理测试进程，启动 reviewed 二进制在 0.0.0.0:8787，沿用 frp 6004，显式 public-host 并启用 speaker probe。PID 31069，日志 .runtime/managed-public.log。通过公网执行 TestManagedDiscovery，列出七个管理/调用工具及 probe_speaker，缺少调用上下文的请求正确拒绝。health 返回 application_management:true。

此次只验收工具发现。AgentAPI、Render/Casting 尚未配置；create-app/get-screens 会返回不可用，不能将其解释为工具未接通。原 service_id 静态 quiz 调用已退出公网入口；管理模式使用 app_id。需要灵矽重新建立会话刷新 schema。此轮未进行收费构建或屏幕操作。

## AgentAPI 配置收拢

按用户要求，AgentAPI Key 与 agent/environment ID 放入 gateway/config/gateway.local.json 的 agentapi 对象。api_key 不再从环境变量读取；本地文件已忽略，初始模板权限 0600。enabled=false 允许尚未准备凭证时启动管理服务；配置完成启用并重启。测试验证使用文件 Key、环境不覆盖、Key 不进入 session body、缺少 Key 时报错。原有进程未获得新凭证，未发起真实云端构建。

## 真实 AgentAPI 配置与构建冒烟

用户填入本地 Key 与环境后，只读校验美西 AgentAPI：agents 列表与指定 environment 均 HTTP 200。配置原为 enabled=false、agent_replace_me；账号仅一个 ready 开发助手（claudecode，version=1），选用该智能体，启用本地配置，public_url 改为已有公网 frp 地址。未改云端 agent/environment；该 agent 未挂载 app-builder 技能，本次明确要求按内联产物约束执行。

通过本地 MCP create-app 创建极简零外部依赖计数器，app_id=app-3c6b1f00d5c31e0d99ff412c825be7e7，AgentAPI session=ses_60660f7f42c1472eb24c9e83。已观察 user.message、running、工具执行及应用代码输出；等待最终产物验收。仅本地配置保存 Key，输出不记录 Key。Render/Casting 尚未配置，此轮不投屏。

首次真实构建于约 8 分钟后 end_turn，产物存在且下载成功，但 launch.json 为 command/args/cwd，缺少 runtimeExecutable/runtimeArgs，Gateway 返回 BUILD_FAILED / invalid runtime or fixed port。保存拒绝包于 .runtime/build-smoke-rejected.tar.gz，仅作诊断。不能称为注册完成。

修复构建 prompt：嵌入完整合法 launch.json 示例并明确禁止 command/args/cwd 别名。增加 exact-launch-contract 回归测试，Gateway 测试通过。部署新二进制后可重新创建，不修改失败应用记录、不放宽产物验证。

用户准备自行测试 create-app：按请求重启 Gateway，读取 gateway.local.json（AgentAPI enabled=true，美西、Key 已配置、真实 agent version 1 和 environment）。旧测试 app-7adfa3b04e9b2be038f13b7165f6750f 的构建 session/提交标记保留，由恢复流程继续消费，没有新建测试任务。公网 health 和七工具发现通过。运行 PID 更新到 .runtime/server.pid。后续真实用户通过灵矽 create-app 发起，Render/Casting 仍未配置。
