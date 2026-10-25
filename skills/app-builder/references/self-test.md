# 模板复制与自测脚本

构建完成后只跑一次 `scripts/self-test.sh`，全部通过（看到 `self-test passed`）即可结束。不要做浏览器截图、UI 自动化、逐项遍历错误分支、反复换端口重启或性能测试；这些都不是交付要求。

## init-template.sh

```bash
bash <skill-dir>/scripts/init-template.sh /mnt/session/outputs
```

把技能包内 `references/app-template/` 的完整内容复制到产物目录，并列出复制后的文件。不要用 Read/Write 工具逐个搬运模板文件；`package.json`、`launch.json` 不需要读，保持原样。

## self-test.sh

```bash
bash <skill-dir>/scripts/self-test.sh /mnt/session/outputs \
  --op-method POST --op-path /api/join --op-body '{"user_name":"验证玩家"}' \
  --bad-op-method POST --bad-op-path /api/join --bad-op-body '{"user_name":"  "}' \
  --package
```

参数：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| 第一个位置参数 | 是 | 产物目录，包含 `app/`、`openapi.yaml`、`launch.json` |
| `--op-method`、`--op-path` | 是 | 调用一个能改变状态的业务操作；method 取 GET/POST/PUT/PATCH/DELETE，路径以 `/api/` 开头 |
| `--op-body` | 否 | JSON 请求体，无请求体时省略 |
| `--bad-op-method`、`--bad-op-path` | 否 | 错误分支抽查，例如空白 `user_name`；预期 4xx/5xx，响应仍需 JSON 且含 `message` |
| `--bad-op-body` | 否 | 错误分支的 JSON 请求体 |
| `--package` | 建议 | 打包 `artifact.tar.gz` 并对归档做校验 |

脚本按顺序执行：

1. **静态检查**：运行 `validate-artifact.sh`，覆盖 JavaScript 语法、JSON 合法性、`launch.json` 字段、OpenAPI 结构和外部资源引用。
2. **启动**：在 4310–4314 中挑一个空闲测试端口启动应用，按 `launch.json` 的 `readyTimeoutMs`（上限 30 秒）等待 `/health` 就绪。
3. **HTTP 检查**：`/health` 返回 `{"status":"ok"}`；`/` 返回 200 和 text/html。
4. **业务检查**：脚本发起 `--op-*` 请求，要求返回 2xx、响应是 JSON 且包含字符串 `message`；`--bad-op-*` 接受 4xx/5xx，只要求响应是 JSON 且包含字符串 `message`。脚本会打印响应原文供核对。
5. **打包**：`--package` 时生成 `artifact.tar.gz` 并再次运行 `validate-artifact.sh` 检查归档。

失败时脚本打印 FAIL 行和应用日志尾部，按输出修复后重跑整个脚本；必须看到 `self-test passed` 才能结束。脚本不测试 SIGTERM、退出码或残留进程，测试进程由脚本在结束时清理；沙箱结束时会自动清理。

`self-test.sh` 不启动浏览器，也不检查页面布局。画布适配（1280×720、一屏放下、无页面滚动）按 `artifact-contract.md` 的“渲染画布”和模板实现；不要用额外浏览器截图代替这一步。
