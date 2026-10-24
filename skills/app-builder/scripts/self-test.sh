#!/usr/bin/env bash
# 一键自测：静态检查 -> 测试端口启动 -> /health、/、业务操作、错误分支抽查 -> 打包 -> 归档校验。
# 用法:
#   self-test.sh <outputs-dir> \
#     --op-method POST --op-path /api/join --op-body '{"user_name":"验证玩家"}' \
#     [--bad-op-method POST --bad-op-path /api/join --bad-op-body '{"user_name":"  "}'] \
#     [--package]
# 业务操作由脚本发起：主操作要求 2xx；错误分支抽查接受 4xx/5xx，只要求响应是 JSON 且含 message。
set -euo pipefail

usage() {
  printf 'usage: self-test.sh <outputs-dir> --op-method M --op-path P [--op-body B]\n' >&2
  printf '                   [--bad-op-method M --bad-op-path P [--bad-op-body B]] [--package]\n' >&2
  printf '  --op-method/--op-path  必填，调用一个能改变状态的业务操作，路径以 /api/ 开头\n' >&2
  printf '  --op-body              可选，JSON 请求体\n' >&2
  printf '  --bad-op-*             可选，错误分支抽查，接受 4xx/5xx，响应仍需 JSON 且含 message\n' >&2
  printf '  --package              打包 artifact.tar.gz 并做归档校验\n' >&2
  exit 2
}

OUT=""
OP_METHOD="" OP_PATH="" OP_BODY=""
BAD_OP_METHOD="" BAD_OP_PATH="" BAD_OP_BODY=""
PACKAGE=0

while [ $# -gt 0 ]; do
  case "$1" in
    --op-method) [ $# -ge 2 ] || usage; OP_METHOD="$2"; shift 2 ;;
    --op-path) [ $# -ge 2 ] || usage; OP_PATH="$2"; shift 2 ;;
    --op-body) [ $# -ge 2 ] || usage; OP_BODY="$2"; shift 2 ;;
    --bad-op-method) [ $# -ge 2 ] || usage; BAD_OP_METHOD="$2"; shift 2 ;;
    --bad-op-path) [ $# -ge 2 ] || usage; BAD_OP_PATH="$2"; shift 2 ;;
    --bad-op-body) [ $# -ge 2 ] || usage; BAD_OP_BODY="$2"; shift 2 ;;
    --package) PACKAGE=1; shift ;;
    -*) usage ;;
    *) [ -z "$OUT" ] || usage; OUT="$1"; shift ;;
  esac
done

[ -n "$OUT" ] || usage
[ -d "$OUT" ] || { printf 'FAIL: outputs dir not found: %s\n' "$OUT" >&2; exit 1; }
[ -n "$OP_METHOD" ] && [ -n "$OP_PATH" ] || { printf 'FAIL: --op-method and --op-path are required\n' >&2; usage; }
case "$OP_METHOD" in GET|POST|PUT|PATCH|DELETE) ;; *) printf 'FAIL: unsupported --op-method: %s\n' "$OP_METHOD" >&2; usage ;; esac
case "$OP_PATH" in /api/*) ;; *) printf 'FAIL: --op-path must start with /api/: %s\n' "$OP_PATH" >&2; usage ;; esac
if [ -n "$BAD_OP_METHOD" ] || [ -n "$BAD_OP_PATH" ]; then
  [ -n "$BAD_OP_METHOD" ] && [ -n "$BAD_OP_PATH" ] || { printf 'FAIL: --bad-op-method and --bad-op-path must be given together\n' >&2; usage; }
  case "$BAD_OP_METHOD" in GET|POST|PUT|PATCH|DELETE) ;; *) printf 'FAIL: unsupported --bad-op-method: %s\n' "$BAD_OP_METHOD" >&2; usage ;; esac
  case "$BAD_OP_PATH" in /api/*) ;; *) printf 'FAIL: --bad-op-path must start with /api/: %s\n' "$BAD_OP_PATH" >&2; usage ;; esac
fi

command -v node >/dev/null 2>&1 || { printf 'FAIL: node is required for self-test\n' >&2; exit 1; }
command -v curl >/dev/null 2>&1 || { printf 'FAIL: curl is required for self-test\n' >&2; exit 1; }

SKILL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VALIDATE="$SKILL_DIR/scripts/validate-artifact.sh"
LOG="$(mktemp /tmp/self-test.XXXXXX.log)"
PID=""

note() { printf 'OK: %s\n' "$*"; }

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  if [ -s "$LOG" ]; then
    printf '%s\n' '--- app log tail ---' >&2
    tail -n 30 "$LOG" >&2 || true
  fi
  exit 1
}

cleanup() {
  if [ -n "$PID" ] && kill -0 "$PID" 2>/dev/null; then
    kill "$PID" 2>/dev/null || true
    wait "$PID" 2>/dev/null || true
  fi
  rm -f "$LOG"
}
trap cleanup EXIT

# 1. 静态检查
bash "$VALIDATE" "$OUT" || fail 'static validation failed'

# 就绪等待上限取 launch.json 的 readyTimeoutMs（不超过 30s）
WAIT_MS="$(node -e '
  const fs = require("node:fs");
  try {
    const launch = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
    const ms = Number(launch.readyTimeoutMs);
    process.stdout.write(String(Number.isInteger(ms) && ms > 0 ? Math.min(ms, 30000) : 10000));
  } catch { process.stdout.write("10000"); }
' "$OUT/launch.json")"

# 2. 挑一个空闲测试端口，启动应用并等待 /health
port_busy() {
  # 端口上有 HTTP 服务响应即视为占用；连接被拒或超时视为空闲（超时情况由下面的稳定窗口兜底）
  local code=""
  code=$(curl -sS --max-time 1 -o /dev/null -w '%{http_code}' "http://127.0.0.1:$1/" 2>/dev/null) || true
  case "$code" in ''|000) return 1 ;; *) return 0 ;; esac
}

started=0
for PORT in 4310 4311 4312 4313 4314; do
  if port_busy "$PORT"; then
    note "port $PORT busy, trying next port"
    continue
  fi
  export PORT
  PORT="$PORT" node "$OUT/app/app.js" >"$LOG" 2>&1 &
  PID=$!
  # 稳定窗口：端口被抢或启动即失败时，应用会立刻退出
  settle=$((SECONDS * 1000 + 1000))
  while [ $((SECONDS * 1000)) -lt $settle ]; do
    if ! kill -0 "$PID" 2>/dev/null; then
      if grep -qi 'EADDRINUSE' "$LOG"; then
        note "port $PORT busy, trying next port"
        continue 2
      fi
      fail "app exited before /health was ready (port $PORT)"
    fi
    sleep 0.2
  done
  note "started app on port $PORT, waiting for /health (up to ${WAIT_MS}ms)"
  deadline=$((SECONDS * 1000 + WAIT_MS))
  health_body=""
  while [ $((SECONDS * 1000)) -lt $deadline ]; do
    if ! kill -0 "$PID" 2>/dev/null; then
      if grep -qi 'EADDRINUSE' "$LOG"; then
        note "port $PORT busy, trying next port"
        continue 2
      fi
      fail "app exited before /health was ready (port $PORT)"
    fi
    if health_body=$(curl -fsS "http://127.0.0.1:$PORT/health" 2>/dev/null); then
      if kill -0 "$PID" 2>/dev/null; then
        started=1
        break
      fi
      # 应用进程已死而 /health 有响应：回应来自占用端口的其他服务，交给下一轮的死进程检查换端口
    fi
    sleep 0.3
  done
  [ "$started" = 1 ] && break
done
[ "$started" = 1 ] || fail '/health not ready on any test port'
[ "$health_body" = '{"status":"ok"}' ] || fail "unexpected /health body: $health_body"
note '/health returns {"status":"ok"}'

# 3. / 返回 200 + text/html
root_meta=$(curl -fsS -o /dev/null -w '%{http_code} %{content_type}' "http://127.0.0.1:$PORT/" 2>/dev/null) \
  || fail 'GET / failed'
case "$root_meta" in
  200*text/html*) note '/ returns 200 text/html' ;;
  *) fail "unexpected / response: $root_meta" ;;
esac

# 4. 业务操作与错误分支抽查：响应必须是 JSON 且包含字符串 message
request() {
  # $1=method $2=path $3=body，输出响应体加最后一行 HTTP 状态码
  local method="$1" path="$2" body="$3" args=()
  args=(-sS -X "$method" "http://127.0.0.1:$PORT$path" -H 'content-type: application/json' -w $'\n%{http_code}')
  [ -z "$body" ] || args+=(-d "$body")
  curl "${args[@]}"
}

check_json_message() {
  local label="$1" body="$2"
  printf '%s' "$body" | node -e '
    let s = "";
    process.stdin.setEncoding("utf8");
    process.stdin.on("data", (c) => { s += c; });
    process.stdin.on("end", () => {
      let data;
      try { data = JSON.parse(s); }
      catch { console.error("FAIL: response is not valid JSON"); process.exit(1); }
      if (typeof data.message !== "string") {
        console.error("FAIL: response JSON has no string message");
        process.exit(1);
      }
      console.log("OK: response JSON contains message");
    });
  ' || fail "$label response is not JSON with string message"
}

raw=$(request "$OP_METHOD" "$OP_PATH" "$OP_BODY")
status=$(printf '%s\n' "$raw" | sed -n '$p')
body=$(printf '%s\n' "$raw" | sed '$d')
case "$status" in
  2*) note "business op $OP_METHOD $OP_PATH returned HTTP $status" ;;
  *) fail "business op $OP_METHOD $OP_PATH returned HTTP $status: $body" ;;
esac
printf '%s\n' '--- business op response ---'
printf '%s\n' "$body"
check_json_message 'business op' "$body"

if [ -n "$BAD_OP_METHOD" ]; then
  raw=$(request "$BAD_OP_METHOD" "$BAD_OP_PATH" "$BAD_OP_BODY")
  status=$(printf '%s\n' "$raw" | sed -n '$p')
  body=$(printf '%s\n' "$raw" | sed '$d')
  note "error branch spot check $BAD_OP_METHOD $BAD_OP_PATH returned HTTP $status"
  printf '%s\n' '--- error branch spot check response ---'
  printf '%s\n' "$body"
  check_json_message 'error branch spot check' "$body"
fi

# 5. 打包并校验归档
if [ "$PACKAGE" = 1 ]; then
  cd "$OUT"
  rm -f artifact.tar.gz
  tar -czf artifact.tar.gz app openapi.yaml launch.json || fail 'tar failed'
  note 'artifact.tar.gz created'
  bash "$VALIDATE" "$OUT" "$OUT/artifact.tar.gz" || fail 'archive validation failed'
fi

printf 'self-test passed\n'
