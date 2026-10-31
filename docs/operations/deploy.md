# 部署与备份

本页说明门户模式下 Gateway 部署后要做的第一次配置，以及哪些数据必须备份。启动命令和配置文件见[启动 Gateway](gateway.md)。网页不能启动 Gateway，下面的配置向导只负责启动之后的设置。

## 首次配置

1. 用 `portal.username` / `portal.password` 登录 `/admin`。概览页在平台配置不完整时显示配置向导。
2. 打开 `/admin/settings`，按向导顺序填写。每个字段下方写明了数据从哪里取：
   - **AgentAPI**：服务地址、API Key、构建智能体 ID、环境 ID，用来生成应用。
   - **灵矽平台**：在灵矽智控台创建的 API Key，用来为每位用户创建智能体。
   - **灵矽 MCP 地址**：灵矽云能访问到的 Gateway 公网 HTTPS 地址，以 `/mcp` 结尾。MCP Token 首次启动时已自动生成并加密保存，不需要手填。
   - **默认 Render**：平台托管用户启动应用使用的地址。默认 Casting 地址只用于控制面 `get-screens`/Portal 屏幕页的兼容视图与调试；生产投屏由现场设备完成。
3. 以上都显示已配置后，再打开注册开关。

`gateway.yaml` 里的同名配置只在数据库对应设置还没有值时写入一次。之后以数据库为准，改文件不会生效，可以从文件里删除。

自动化部署想用文件完成首次初始化，把 `gateway.example.yaml` 第 2 部分的配置取消注释并填入真实值即可，空库首次启动时一次性导入。不要保留示例里的占位值，它们会被当成真实配置写入。没有填的项仍然在 `/admin/settings` 里补。

## 通过隧道或反向代理暴露网关

灵矽云经隧道（frp、ssh 转发等）或反向代理访问网关时，请求从本机回环地址进来，而 `Host` 是公网地址。网关的 MCP 入口会拒绝这种请求（`403 invalid Host header`）。

门户模式下不需要额外配置：网关会自动放行管理后台里“公网 MCP 地址”的 `host:port`，在后台改了地址，几秒内生效，不用重启。只有对外使用的 Host 和这个地址不一样时（例如代理改写了 Host），才需要在 `gateway.yaml` 里用 `public_host` 额外放行，它是启动时读取的，修改后需要重启。独立模式没有管理后台，经隧道或代理访问时必须配置 `public_host`。直接把网关端口暴露在公网网卡上时都不需要配置。

## 必须备份的数据

| 数据 | 位置 | 说明 |
| --- | --- | --- |
| 数据库 | `open-anywhere.gateway.db` | 必须。账号、设备、应用记录、平台设置 |
| 主密钥 | `portal-data/secret.key`，或环境变量 `GATEWAY_SECRET_KEY` | 必须。加密平台 API Key、MCP Token 和用户服务 token |
| 应用包 | `artifacts/`（或 `artifacts.dir`） | 必须。丢失后已注册应用无法启动（`ARTIFACT_MISSING`），只能重新构建 |
| 设备凭证 | `portal-data/devices/` | 必须。丢失后设备 token 无法恢复，需要在门户里重新开通或重新绑定硬件 |

这四者必须一起备份，缺任何一项都不算可恢复的备份。数据库和主密钥尤其要成对保存：只有数据库，没有主密钥，密文无法解密，Gateway 会拒绝启动；只有主密钥而没有数据库，设置也恢复不了。不要删除密钥或清空密文来“修复”启动失败。

备份不是原子快照。数据库用 SQLite `.backup` 拿到的是一个时刻的一致快照，`artifacts/` 与 `portal-data/devices/` 是随后复制的文件，三者可能相差几秒到几分钟。恢复后允许出现“数据库有行、文件缺失”或“文件在、数据库没有行”，必须按下面的对账步骤处理，不要假定备份完全一致。

## 备份

```bash
sqlite3 open-anywhere.gateway.db ".backup 'gateway-backup.db'"
cp portal-data/secret.key secret.key.backup
tar -czf artifacts-backup.tar.gz artifacts
tar -czf devices-backup.tar.gz portal-data/devices
```

使用 `GATEWAY_SECRET_KEY` 时，把这个值保存到密码管理器或部署平台的密钥库里。

主密钥和数据库不要放在同一个可被他人读取的位置。拿到两者就能解密所有已保存的凭证。

## 恢复

1. 停止 Gateway。
2. 把备份的数据库放回 `open-anywhere.gateway.db`，并删除同目录下旧的 `-wal` 和 `-shm` 文件。
3. 恢复 `portal-data/secret.key`（0600），或设置原来的 `GATEWAY_SECRET_KEY`。
4. 解包产物与凭证，并收紧权限：

   ```bash
   tar -xzf artifacts-backup.tar.gz
   tar -xzf devices-backup.tar.gz
   chmod 700 portal-data/devices
   chmod 600 portal-data/devices/*
   ```

5. 启动 Gateway。启动时会解密校验全部密文，失败就说明密钥与数据库不是一对。
6. **对账。** 检查数据库与文件是否一致：

   ```bash
   # 有产物行、没有应用行（孤儿元数据）
   sqlite3 open-anywhere.gateway.db \
     "SELECT a.app_id FROM artifacts a LEFT JOIN apps p ON p.id=a.app_id WHERE p.id IS NULL;"
   ```

   - 数据库有应用、`artifacts/` 缺包：`start-app` 会返回 `ARTIFACT_MISSING`，用 `update-app` 或重新 `create-app` 重建。
   - 文件存在、数据库没有对应行：文件不可见也不可下载，确认无引用后手工清理。
   - `portal-data/devices/` 缺某个设备的凭证：该设备需要在门户里重新开通；硬件设备重新绑定后才会恢复。
   - 行与文件都在但无法解密凭证：主密钥与数据库不是一对，回到步骤 3。

恢复与对账的数据库侧背景见[控制面数据库设计](../architecture/database.md#10-schema-版本与迁移)。

## 迁移到新机器

按“备份”和“恢复”操作即可。迁移后检查两件事：

- `artifacts.base_url` 指向新机器，并且 Render 主机能访问。修改后重启即可，下载地址会按新配置重写。
- 灵矽云能访问新的 MCP 地址。地址变化后在 `/admin/settings` 保存新的 MCP 地址，Gateway 会同步到灵矽的 MCP。

## 轮换 MCP Token

在 `/admin/settings` 勾选重新生成 MCP Token 并保存。Gateway 会把新 token 同步到灵矽的 MCP，旧 token 立即失效。同步失败时页面会提示，此时灵矽还在使用旧 token，修复网络或 API Key 后再保存一次即可。
