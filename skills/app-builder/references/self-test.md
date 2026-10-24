# 自测

交付前必须实际运行应用和归档检查，不能只读代码。以下命令在产物根目录（包含 `app/`、`openapi.yaml`、`launch.json` 的目录）执行。

## 1. 静态检查

优先运行技能目录下的脚本：

```bash
bash <skill-dir>/scripts/validate-artifact.sh .
```

脚本会检查必填文件、JavaScript 语法、`launch.json` 字段、OpenAPI 基本结构和外部资源引用。必须看到 `artifact validation passed`。

如果技能目录不可用，手工完成同样的检查：

- `node --check app/app.js`
- `node -e 'JSON.parse(require("node:fs").readFileSync("app/package.json","utf8"))'`
- `node -e 'JSON.parse(require("node:fs").readFileSync("launch.json","utf8"))'`
- 确认 `launch.json` 的字段、`runtimeExecutable`、入口文件、`healthPath`、`readyTimeoutMs` 符合产物契约。
- 确认 OpenAPI 为 3.1、路径均以 `/api/` 开头、`operationId` 唯一。

## 2. 启动应用

```bash
PORT=4310 node app/app.js
```

- 应用应在前台运行，日志可见。
- 启动时不得要求输入、不得打开浏览器、不得自动切换端口。
- 端口被占用时，换一个测试端口重试；不要把换端口逻辑写进应用。

## 3. 健康检查

另开终端：

```bash
curl -sS -i http://127.0.0.1:4310/health
```

期望 HTTP 200、`Content-Type: application/json`、响应体包含 `"status": "ok"` 或 `{"status":"ok"}`。可重复请求确认稳定。

## 4. 页面

```bash
curl -sS -o /dev/null -w '%{http_code} %{content_type}\n' http://127.0.0.1:4310/
```

期望 200 和 `text/html`。页面必须能离线加载：不要引用外部 CDN、远程字体或远程脚本。

## 5. 业务接口

按 `openapi.yaml` 中的每个操作至少成功调用一次，并覆盖主要错误分支。以模板的猜电影接口为例：

```bash
curl -sS http://127.0.0.1:4310/api/state
curl -sS -X POST http://127.0.0.1:4310/api/join \
  -H 'content-type: application/json' \
  -d '{"user_name":"小明"}'
curl -sS -X POST http://127.0.0.1:4310/api/guess \
  -H 'content-type: application/json' \
  -d '{"user_name":"小明","guess":"这个杀手不太冷"}'
curl -sS -X POST http://127.0.0.1:4310/api/guess \
  -H 'content-type: application/json' \
  -d '{"user_name":"小明","guess":"霸王别姬"}'
curl -sS http://127.0.0.1:4310/api/state
```

同时验证：

- 空白名字 `{"user_name":"   "}` 返回 400 和 JSON `message`。
- 重复名字 `{"user_name":"小明"}` 不产生重复玩家，也不重置分数；按需求返回 200 或 409，响应体包含 `message`。
- 未加入的玩家提交猜测返回 400 或 404 和 JSON `message`。
- 空猜测 `{"user_name":"小明","guess":"   "}` 返回 400 和 JSON `message`。
- 错误猜测保留当前回合；正确猜测后 `GET /api/state` 显示该玩家分数增加并进入下一轮。
- 最近猜测列表同时包含正确和错误记录。
- 每个响应都是合法 JSON，成功和错误响应都包含字符串 `message`。

实际业务接口以本次 `openapi.yaml` 为准，以上只是检查方法。

## 6. 进程与退出

- 停止应用：`kill -TERM <pid>`。期望进程在几秒内以 0 退出，不留下端口占用或子进程。
- 再用临时命令验证 `PORT` 缺失或非法时非零退出，例如 `env -u PORT node app/app.js`，确认 stderr 有明确原因。验证后立即结束。
- 不要用 `nohup`、`&` 长期驻留测试服务；结束时清理后台进程。

## 7. 归档测试

把最终归档复制到空的临时目录，解压后从归档内容启动，而不是从构建目录启动：

```bash
tmp="$(mktemp -d)"
tar -xzf artifact.tar.gz -C "$tmp"
(cd "$tmp" && tar -tzf - >/dev/null 2>&1 || true)
(cd "$tmp" && PORT=4311 node app/app.js)
```

再次检查 `/health`、`/` 和至少一个业务操作。这样可以发现遗漏文件、错误的相对路径和未打进归档的依赖。结束后删除临时目录。

## 完成标准

- 静态检查通过。
- `/health` 返回 200 且内容正确。
- `/` 可加载且无外部资源依赖。
- OpenAPI 中每个操作至少成功调用一次，错误分支返回 JSON `message`。
- `SIGTERM` 后正常退出，无残留进程。
- 从解压后的归档启动仍通过以上检查。
- 以上任一项未实际执行时，不能声称应用已完成。
