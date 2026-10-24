# Gateway 自审与接通清单

日期：2026-10-24。结论：可进入受控联调，不能宣称完整真实链路已经验收。单元/HTTP 模拟通过只证明实现符合所覆盖场景，不能替代 AgentAPI 真实产物、Render 媒体和电视实测。

## 已确认并修复

1. send_request 和重复 start-app 使用缓存 running 状态，投屏失败后仍可能调用业务或返回运行成功。新增失败测试复现，再抽取统一 refresh，在调用前检查 Render/Casting；失败后拒绝操作并清理。
2. launch.json 只检查参数中是否出现包内文件，会接受 node -e、--require 或外部入口。回归测试证实；改为第一个参数必须是规范的包内 app/ 普通文件，不允许路径归一化逃逸或 Node 选项作为入口。
3. Render 返回其他 app_id 的实例时失败清理可能停止 其他应用。新增测试；仅接管同一 app_id 的实例，否则保留未知状态，不删除他人实例。
4. 正常 stop-app 响应补 stopped_at；error 响应补已记录的 last_agent_message。

## 仍需完善的代码/契约

- 应用归属精确包含 device/agent/session。灵矽重连产生不同 session 后会 APP_NOT_FOUND。符合现有严格会话归属，但需要明确安全的会话接管机制，否则长构建/重连体验不完整。不能直接绕过检查。
- stateless MCP 没有主动 notifications/app_status。只能 get-app-status 查询；真机通知通道需单独验证或定义设备桥接方式。
- AgentAPI 中断后等待 idle/归档补偿不完整；未知启动结果保留资源但没有自动对账接口。生产前需补恢复操作。
- 运行操作用进程级锁，慢启动会阻塞其他应用的查询/调用。适用于单实例 demo，多应用需要按应用/屏幕锁和可取消等待。
- OpenAPI 仅支持已列明子集，产物校验并非完整 OpenAPI 全规范验证；首包需核对构建输出与 Render 校验器的一致性。
- 状态文件没有跨进程锁；只允许一个进程使用同一路径。原子改名的 Windows 行为只做编译检查，未实机测。
- 动态屏幕依赖人工配置 target/endpoint，get-screens 不会自动注册任意新屏幕。

## 实际部署审计

本机 8789 为管理模式；AgentAPI、render_url、casting_url、screens 均未配置。只检查环境变量是否存在，未打印任何密钥。真实 tools/call 实测：create-app 返回 BUILD_UNAVAILABLE；get-screens 返回 DEPENDENCY_UNAVAILABLE。

公网 6004 转发本机 8787，仍是静态 quiz 模式；它通过了游戏调用，但没有发布新的应用管理工具。不可把公网 demo 正常误认为完整管理服务已上线。

Render 的 origin/feat/render-runtime 分支有实现及运行说明，默认接口 8090，依赖 Node/Chrome/FFmpeg/lsof；本机未发现其正在监听。该分支未合入本工作区。Casting 代码在主线，本机未配置运行地址。没有探测未知远端地址或抢占其他设备。

## 需要提供/确认的部署信息

- AgentAPI 区域地址、可用 API Key（通过本地环境变量提供，不贴到 PR）、agent_id + 固定版本、environment_id；agent 装好 app-builder，云端沙箱可用。
- Render 和 Casting 实际可达 URL；电视所在机器的部署由对应 agent 提供。需要真实 target_id、endpoint_id，并选一个 screen_id。
- Render 能访问的 Gateway public_url；电视能访问 Render 的 source.url。EasyTier 只解决加入网络的节点互通，普通电视未必能访问 EasyTier 地址。
- MCP 公网入口鉴权及 HTTPS；确认客户端接受切换到 app_id 工具输入，并刷新会话。部署配置完成后由本 agent 负责写配置和验证。

## 验收顺序

1. 下游 health/端口/targets 只读检查和网络可达性。
2. 真实 AgentAPI 创建一个小应用，等待产物校验并注册 idle。
3. Render 实际下载该包、启动应用并生成流；Casting 电视达到 playing。
4. MCP app_id 获取 OpenAPI、发 user_name 请求，观察屏幕变化。
5. 停止并检查进程/投屏/端口释放，再测重启恢复、构建失败、掉线和清理重试。

本轮修复后执行全仓库 race、vet、macOS 构建和 Windows 无 cgo 交叉编译。真实云端+电视链路尚未执行。
