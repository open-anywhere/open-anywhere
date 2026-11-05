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
  --op-method POST --op-path /api/join --op-body '{"user_name":"验证玩家"}'
```

参数：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| 第一个位置参数 | 是 | 产物目录，包含 `app/`、`openapi.yaml`、`launch.json` |
| `--op-method`、`--op-path` | 否 | 额外调用一个能改变状态的业务操作，要求 2xx；路径以 `/api/` 开头 |
| `--op-body` | 否 | 业务操作的 JSON 请求体，无请求体时省略 |

脚本按顺序执行：

1. **静态检查**：JavaScript 语法、`package.json` 是合法 JSON 对象、`launch.json` 字段、OpenAPI 结构（含 `operationId` 唯一性）、`app/public` 外部资源引用。
2. **打包**：总是生成 `artifact.tar.gz`，随后所有运行检查都针对这个归档，而不是源目录。
3. **归档校验**：归档大小、条目数、必需文件、路径安全、重复条目、链接/特殊文件，以及解压后的总大小。
4. **启动契约负向检查**：`PORT` 缺失和 `PORT` 非法时，应用必须在 5 秒内以非零状态退出，并向 stderr 输出原因；否则失败。
5. **生产等价启动**：把归档解压到临时目录，以归档根目录为工作目录，只传 `PATH`/`LANG`/`TZ`/`TMPDIR` 和 `HOST=0.0.0.0`、`PORT`、`NODE_ENV=production`，按 `launch.json` 的 `runtimeArgs` 启动，按 `readyTimeoutMs`（上限 30 秒）等待 `/health`。
6. **HTTP 检查**：`/health` 返回 200、`application/json` 和精确的 `{"status":"ok"}`；`/` 返回 200 和 text/html。
7. **API 探测**：从 `openapi.yaml` 提取所有 `/api/*` 操作（GET/POST/PUT/PATCH/DELETE，最多 12 个）。路径参数替换为 `verify`，POST/PUT/PATCH 发送空 JSON 对象，逐个请求；每个响应必须是 JSON 对象、包含字符串 `message`，且状态码为 2xx 或 4xx。5xx、3xx、非 JSON 或缺少 `message` 都算失败。提取不到操作时回退到应用提供的 `GET /openapi.json`。
8. **可选成功路径**：传入 `--op-*` 时额外调用一次，要求 2xx、JSON 对象和字符串 `message`。
9. **存活与退出**：请求完成后应用进程仍须存活，`/health` 仍须返回 `{"status":"ok"}`；发送 `SIGTERM` 后须在 5 秒内退出。

失败时脚本打印 FAIL 行和应用 stdout/stderr 尾部，按输出修复后重跑整个脚本；必须看到 `self-test passed` 才能结束。测试进程和临时目录由脚本清理。

探测请求使用空 JSON 对象和占位路径参数，通过 2xx/4xx 响应确认接口可请求、不崩溃、错误分支返回 JSON `message`；真实成功路径需要的话用 `--op-*` 补一次。脚本不启动浏览器，也不检查页面布局。画布适配（1280×720、一屏放下、无页面滚动）按 `artifact-contract.md` 的“渲染画布”和模板实现；不要用额外浏览器截图代替这一步。
