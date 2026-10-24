# 最小验证

构建完成后只做下面的检查，全部通过即可结束。不要做浏览器截图、UI 自动化、逐项遍历错误分支、反复换端口重启或性能测试；这些都不是交付要求。

## 1. 静态检查

```bash
bash <skill-dir>/scripts/validate-artifact.sh .
```

脚本已经覆盖 JavaScript 语法、JSON 合法性、`launch.json` 字段、OpenAPI 结构和外部资源引用。必须看到 `artifact validation passed`。脚本可用时不要手工重复同样的检查。

## 2. 启动一次

在产物根目录（包含 `app/`、`openapi.yaml`、`launch.json`）执行：

```bash
PORT=4310 node app/app.js
```

保持前台运行。不要使用 `nohup`、`&` 或打开浏览器。端口被占用时只换一次测试端口，不要把换端口逻辑写进应用。

## 3. 三项 HTTP 检查

另开终端执行：

```bash
curl -fsS http://127.0.0.1:4310/health
curl -fsS -o /dev/null -w '%{http_code} %{content_type}\n' http://127.0.0.1:4310/
curl -fsS -X POST http://127.0.0.1:4310/api/join \
  -H 'content-type: application/json' \
  -d '{"user_name":"验证玩家"}'
```

期望：`/health` 返回 `{"status":"ok"}`，`/` 返回 200 和 `text/html`，业务操作返回包含字符串 `message` 的 JSON。业务路径以本次 `openapi.yaml` 为准，只调用一个能改变状态的操作即可。

错误处理抽查一个场景（例如空白 `user_name`）返回 JSON `message` 就够了，不要穷举所有错误分支。

## 4. 停止并校验归档

```bash
kill -TERM <pid>
cd /mnt/session/outputs
rm -f artifact.tar.gz
tar -czf artifact.tar.gz app openapi.yaml launch.json
bash <skill-dir>/scripts/validate-artifact.sh . artifact.tar.gz
```

进程应在几秒内退出；归档校验必须再次看到 `artifact validation passed`。无需为了测试再解压启动一遍，脚本会检查归档结构和必填文件。

## 完成标准

- 两次 `validate-artifact.sh` 通过。
- `/health` 返回 200 且内容正确，`/` 返回 HTML。
- 一个业务操作返回 JSON，并包含 `message`。
- 进程收到 `SIGTERM` 后退出。

以上通过即视为完成，不要再追加验证步骤。
