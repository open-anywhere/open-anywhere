#!/usr/bin/env bash
# 一键自测：静态检查 -> 打包 -> 归档校验 -> 从归档解压并按生产环境启动
# -> /health、/、OpenAPI 声明的 /api 操作探测 -> SIGTERM 退出检查。
#
# 用法:
#   self-test.sh <outputs-dir> [--op-method M --op-path P [--op-body B]]
#
# 不带 --op-* 时自动探测 openapi.yaml 中声明的 /api 操作；
# --op-* 用于额外验证一个会改变状态的业务成功路径。
set -euo pipefail

usage() {
  printf 'usage: self-test.sh <outputs-dir> [--op-method M --op-path P [--op-body B]]\n' >&2
  printf '  --op-method/--op-path  可选，调用一个能改变状态的业务操作，路径以 /api/ 开头\n' >&2
  printf '  --op-body              可选，JSON 请求体\n' >&2
  exit 2
}

OUT=""
OP_METHOD="" OP_PATH="" OP_BODY=""
MAX_PROBES=12
TERM_GRACE_SECONDS=5

while [ $# -gt 0 ]; do
  case "$1" in
    --op-method) [ $# -ge 2 ] || usage; OP_METHOD="$2"; shift 2 ;;
    --op-path) [ $# -ge 2 ] || usage; OP_PATH="$2"; shift 2 ;;
    --op-body) [ $# -ge 2 ] || usage; OP_BODY="$2"; shift 2 ;;
    -*) usage ;;
    *) [ -z "$OUT" ] || usage; OUT="$1"; shift ;;
  esac
done

[ -n "$OUT" ] || usage
[ -d "$OUT" ] || { printf 'FAIL: outputs dir not found: %s\n' "$OUT" >&2; exit 1; }
if [ -n "$OP_METHOD" ] || [ -n "$OP_PATH" ] || [ -n "$OP_BODY" ]; then
  [ -n "$OP_METHOD" ] && [ -n "$OP_PATH" ] || { printf 'FAIL: --op-method and --op-path must be given together\n' >&2; usage; }
  case "$OP_METHOD" in GET|POST|PUT|PATCH|DELETE) ;; *) printf 'FAIL: unsupported --op-method: %s\n' "$OP_METHOD" >&2; usage ;; esac
  case "$OP_PATH" in /api/*) ;; *) printf 'FAIL: --op-path must start with /api/: %s\n' "$OP_PATH" >&2; usage ;; esac
fi

command -v node >/dev/null 2>&1 || { printf 'FAIL: node is required for self-test\n' >&2; exit 1; }
command -v curl >/dev/null 2>&1 || { printf 'FAIL: curl is required for self-test\n' >&2; exit 1; }
command -v tar >/dev/null 2>&1 || { printf 'FAIL: tar is required for self-test\n' >&2; exit 1; }

NODE_BIN="$(command -v node)"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/self-test.XXXXXX")"
EXTRACT="$WORK/artifact"
ARCHIVE="$OUT/artifact.tar.gz"
LOG="$WORK/app.log"
ERRLOG="$WORK/app.err"
PID=""
BASE=""

note() { printf 'OK: %s\n' "$*"; }

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  if [ -s "$LOG" ]; then
    printf '%s\n' '--- app log tail ---' >&2
    tail -n 40 "$LOG" >&2 || true
  fi
  if [ -s "$ERRLOG" ]; then
    printf '%s\n' '--- app stderr tail ---' >&2
    tail -n 40 "$ERRLOG" >&2 || true
  fi
  exit 1
}

stop_app() {
  [ -n "$PID" ] || return 0
  if kill -0 "$PID" 2>/dev/null; then
    kill -TERM "$PID" 2>/dev/null || true
    deadline=$((SECONDS * 1000 + 2000))
    while [ $((SECONDS * 1000)) -lt "$deadline" ]; do
      kill -0 "$PID" 2>/dev/null || break
      sleep 0.1
    done
    kill -KILL "$PID" 2>/dev/null || true
  fi
  wait "$PID" 2>/dev/null || true
  PID=""
}

cleanup() {
  stop_app
  rm -rf "$WORK"
}
trap cleanup EXIT

# ---------------------------------------------------------------------------
# 1. 静态检查
# ---------------------------------------------------------------------------

[ -d "$OUT" ] || fail "artifact root not found: $OUT"

for file in app/app.js app/package.json app/public/index.html openapi.yaml launch.json; do
  [ -f "$OUT/$file" ] || fail "missing $file"
done
note "required files exist"

"$NODE_BIN" --check "$OUT/app/app.js" || fail "app/app.js has a syntax error"
"$NODE_BIN" -e '
  const fs = require("node:fs");
  const value = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("app/package.json must be a JSON object");
  }
' "$OUT/app/package.json" || fail "app/package.json is not a valid JSON object"
note "app/app.js syntax and app/package.json are valid"

LAUNCH_PATH="$OUT/launch.json" ROOT_DIR="$OUT" "$NODE_BIN" -e '
const fs = require("node:fs");
const path = require("node:path");
const launchPath = process.env.LAUNCH_PATH;
const root = process.env.ROOT_DIR;

function check(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}

try {
  const launch = JSON.parse(fs.readFileSync(launchPath, "utf8"));
  const allowed = new Set(["name", "runtimeExecutable", "runtimeArgs", "healthPath", "readyTimeoutMs"]);
  for (const key of Object.keys(launch)) {
    check(allowed.has(key), `launch.json has unsupported field: ${key}`);
  }
  check(typeof launch.name === "string" && launch.name.trim() !== "", "name is required");
  check(launch.runtimeExecutable === "node", "runtimeExecutable must be node");
  check(Array.isArray(launch.runtimeArgs) && launch.runtimeArgs.length > 0, "runtimeArgs is required");
  check(
    launch.runtimeArgs.every((arg) => typeof arg === "string"),
    "runtimeArgs must contain strings only"
  );
  check(
    !launch.runtimeArgs.some((arg) => arg === "--port" || arg.startsWith("--port=")),
    "runtimeArgs must not fix a port"
  );
  const entry = launch.runtimeArgs[0];
  check(
    entry === "app/app.js",
    `runtimeArgs[0] must be exactly "app/app.js", got ${JSON.stringify(entry)}`
  );
  check(
    fs.existsSync(path.join(root, "app/app.js")),
    "runtimeArgs[0] must reference the archived file \"app/app.js\""
  );
  check(
    launch.healthPath === undefined || launch.healthPath === "/health",
    "healthPath must be /health"
  );
  if (launch.readyTimeoutMs !== undefined) {
    check(
      Number.isInteger(launch.readyTimeoutMs) && launch.readyTimeoutMs > 0 && launch.readyTimeoutMs <= 60000,
      "readyTimeoutMs must be an integer between 1 and 60000"
    );
  }
  console.log("OK: launch.json");
} catch (error) {
  console.error(`FAIL: ${error.message}`);
  process.exit(1);
}
' || fail "launch.json is invalid"

grep -Eq '^openapi:[[:space:]]*["'"'"']?3\.1' "$OUT/openapi.yaml" \
  || fail "openapi.yaml must declare OpenAPI 3.1"
operation_count=$(grep -Ec '^[[:space:]]*operationId:[[:space:]]*[^[:space:]]+' "$OUT/openapi.yaml" || true)
[ "${operation_count:-0}" -ge 1 ] || fail "openapi.yaml must declare at least one operationId"
operation_ids=$(grep -E '^[[:space:]]*operationId:[[:space:]]*' "$OUT/openapi.yaml" \
  | sed -E 's/^[[:space:]]*operationId:[[:space:]]*//; s/[[:space:]]+$//' \
  | tr -d '"' | tr -d "'" || true)
duplicate_ids=$(printf '%s\n' "$operation_ids" | sort | uniq -d)
[ -z "$duplicate_ids" ] || fail "openapi.yaml has duplicate operationId: $duplicate_ids"
bad_paths=$(grep -E '^[[:space:]]{2}/[^:]*:' "$OUT/openapi.yaml" | grep -Ev '^[[:space:]]{2}/api/' || true)
[ -z "$bad_paths" ] || fail "openapi.yaml paths must start with /api/: $bad_paths"
note "openapi.yaml structure"

if grep -REq "(src|href)[[:space:]]*=[[:space:]]*['\"]https?://" "$OUT/app/public"; then
  fail "app/public must not reference external resources"
fi
note "app/public has no external resources"

# ---------------------------------------------------------------------------
# 2. 打包
# ---------------------------------------------------------------------------

( cd "$OUT" && rm -f artifact.tar.gz && tar -czf artifact.tar.gz app openapi.yaml launch.json ) \
  || fail 'tar failed'
note 'artifact.tar.gz created'

# ---------------------------------------------------------------------------
# 3. 归档校验并解压到临时目录（后续直接从归档内容启动）
# ---------------------------------------------------------------------------

limit=$((100 * 1024 * 1024))
size=$(wc -c < "$ARCHIVE" | tr -d ' ')
[ "$size" -le "$limit" ] || fail "archive exceeds 100 MiB ($size bytes)"
tar -tzf "$ARCHIVE" >/dev/null 2>&1 || fail "archive is not a valid tar.gz"
entries=$(tar -tzf "$ARCHIVE")
entry_count=$(printf '%s\n' "$entries" | grep -c . || true)
[ "${entry_count:-0}" -le 4096 ] || fail "archive has $entry_count entries; limit is 4096"
for required in launch.json openapi.yaml app/app.js app/package.json app/public/index.html; do
  if printf '%s\n' "$entries" | grep -Fxq "$required"; then
    continue
  fi
  printf '%s\n' "$entries" | grep -Fxq "./$required" \
    || fail "archive root is missing $required"
done
if printf '%s\n' "$entries" | grep -Eq '^(\./)?outputs(/|$)'; then
  fail "archive must not contain an outputs/ prefix"
fi
if printf '%s\n' "$entries" | grep -Eq '(^|/)\.\.(/|$)|^/'; then
  fail "archive contains absolute or parent-directory paths"
fi
duplicates=$(printf '%s\n' "$entries" | sort | uniq -d)
[ -z "$duplicates" ] || fail "archive contains duplicate entries: $duplicates"
bad_types=$(tar -tvzf "$ARCHIVE" | grep -Ev '^[-d]' || true)
[ -z "$bad_types" ] || fail "archive contains links or special files: $bad_types"

mkdir -p "$EXTRACT"
tar -xzf "$ARCHIVE" -C "$EXTRACT" || fail "archive could not be extracted"
expanded_total=0
while IFS= read -r -d '' file; do
  file_size=$(wc -c < "$file" | tr -d ' ')
  if [ "$file_size" -gt "$limit" ]; then
    fail "expanded file exceeds 100 MiB: ${file#"$EXTRACT"/} ($file_size bytes)"
  fi
  expanded_total=$((expanded_total + file_size))
done < <(find "$EXTRACT" -type f -print0)
[ "$expanded_total" -le "$limit" ] || fail "expanded archive exceeds 100 MiB ($expanded_total bytes)"
note "archive structure (entries: $entry_count, expanded: $expanded_total bytes)"

# 启动参数取自 launch.json，与 Render 保持一致。
RUNTIME_ARGS=()
while IFS= read -r -d '' arg; do
  RUNTIME_ARGS+=("$arg")
done < <("$NODE_BIN" -e '
  const fs = require("node:fs");
  const launch = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
  process.stdout.write(launch.runtimeArgs.map((arg) => arg + "\0").join(""));
' "$OUT/launch.json")
[ "${#RUNTIME_ARGS[@]}" -gt 0 ] || fail "launch.json runtimeArgs is empty"

# Render 给应用的环境：PATH/LANG/TZ/TMPDIR + HOST/PORT/NODE_ENV，其余一律不传。
COMMON_ENV=("PATH=$PATH" "HOST=0.0.0.0" "NODE_ENV=production")
if [ -n "${LANG:-}" ]; then COMMON_ENV+=("LANG=$LANG"); fi
if [ -n "${TZ:-}" ]; then COMMON_ENV+=("TZ=$TZ"); fi
if [ -n "${TMPDIR:-}" ]; then COMMON_ENV+=("TMPDIR=$TMPDIR"); fi

# ---------------------------------------------------------------------------
# 4. 启动契约负向检查：PORT 缺失或非法时必须失败退出
# ---------------------------------------------------------------------------

expect_startup_failure() {
  local label="$1" case_name="$2"
  local out="$WORK/$case_name.out" err="$WORK/$case_name.err"
  local -a env_args=("${COMMON_ENV[@]}")
  if [ "$case_name" = "port-invalid" ]; then
    env_args+=("PORT=not-a-port")
  fi
  (
    cd "$EXTRACT" || exit 1
    exec env -i "${env_args[@]}" "$NODE_BIN" "${RUNTIME_ARGS[@]}"
  ) >"$out" 2>"$err" &
  local pid=$!
  local deadline=$((SECONDS * 1000 + 5000))
  while [ $((SECONDS * 1000)) -lt "$deadline" ]; do
    if ! kill -0 "$pid" 2>/dev/null; then
      local status=0
      wait "$pid" || status=$?
      if [ "$status" -eq 0 ]; then
        [ ! -s "$out" ] || { printf '%s\n' "--- $case_name stdout ---" >&2; tail -n 20 "$out" >&2; }
        [ ! -s "$err" ] || { printf '%s\n' "--- $case_name stderr ---" >&2; tail -n 20 "$err" >&2; }
        fail "$label: app exited 0; the contract requires a non-zero exit"
      fi
      if [ ! -s "$err" ]; then
        [ ! -s "$out" ] || { printf '%s\n' "--- $case_name stdout ---" >&2; tail -n 20 "$out" >&2; }
        fail "$label: app wrote no error message to stderr"
      fi
      note "$label: exited $status with an error message"
      return 0
    fi
    sleep 0.1
  done
  kill -KILL "$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
  [ ! -s "$out" ] || { printf '%s\n' "--- $case_name stdout ---" >&2; tail -n 20 "$out" >&2; }
  [ ! -s "$err" ] || { printf '%s\n' "--- $case_name stderr ---" >&2; tail -n 20 "$err" >&2; }
  fail "$label: app kept running; the contract requires a non-zero exit"
}

expect_startup_failure "PORT missing" port-missing
expect_startup_failure "PORT invalid" port-invalid

# ---------------------------------------------------------------------------
# 5. 按生产环境启动：cwd 为归档根目录，环境变量与 Render 一致
# ---------------------------------------------------------------------------

WAIT_MS="$("$NODE_BIN" -e '
  const fs = require("node:fs");
  try {
    const launch = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
    const ms = Number(launch.readyTimeoutMs);
    process.stdout.write(String(Number.isInteger(ms) && ms > 0 ? Math.min(ms, 30000) : 10000));
  } catch {
    process.stdout.write("10000");
  }
' "$OUT/launch.json")"

port_busy() {
  # 端口上有 HTTP 服务响应即视为占用；连接被拒或超时视为空闲（启动阶段的 EADDRINUSE 会兜底）。
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
  RUN_ENV=("${COMMON_ENV[@]}" "PORT=$PORT")
  (
    cd "$EXTRACT" || exit 1
    exec env -i "${RUN_ENV[@]}" "$NODE_BIN" "${RUNTIME_ARGS[@]}"
  ) >"$LOG" 2>"$ERRLOG" &
  PID=$!
  BASE="http://127.0.0.1:$PORT"
  settle=$((SECONDS * 1000 + 1000))
  while [ $((SECONDS * 1000)) -lt $settle ]; do
    if ! kill -0 "$PID" 2>/dev/null; then
      if grep -qi 'EADDRINUSE' "$ERRLOG" "$LOG"; then
        note "port $PORT busy, trying next port"
        wait "$PID" 2>/dev/null || true
        PID=""
        continue 2
      fi
      fail "app exited before /health was ready (port $PORT)"
    fi
    sleep 0.2
  done
  note "started app on port $PORT, waiting for /health (up to ${WAIT_MS}ms)"
  deadline=$((SECONDS * 1000 + WAIT_MS))
  ready=0
  while [ $((SECONDS * 1000)) -lt "$deadline" ]; do
    if ! kill -0 "$PID" 2>/dev/null; then
      if grep -qi 'EADDRINUSE' "$ERRLOG" "$LOG"; then
        note "port $PORT busy, trying next port"
        wait "$PID" 2>/dev/null || true
        PID=""
        continue 2
      fi
      fail "app exited before /health was ready (port $PORT)"
    fi
    if curl -fsS --max-time 2 -o /dev/null "$BASE/health" 2>/dev/null; then
      ready=1
      break
    fi
    sleep 0.3
  done
  [ "$ready" = 1 ] || fail "/health not ready within ${WAIT_MS}ms on port $PORT"
  started=1
  break
done
[ "$started" = 1 ] || fail '/health not ready on any test port'

# ---------------------------------------------------------------------------
# 6. /health 与 /
# ---------------------------------------------------------------------------

health_raw=$(curl -sS --max-time 5 -w $'\n%{http_code} %{content_type}' "$BASE/health") \
  || fail 'GET /health failed'
health_meta=${health_raw##*$'\n'}
health_body=${health_raw%$'\n'*}
health_meta_lc=$(printf '%s' "$health_meta" | tr '[:upper:]' '[:lower:]')
[ "$health_body" = '{"status":"ok"}' ] || fail "unexpected /health body: $health_body"
case "$health_meta_lc" in
  200*application/json*) note '/health returns 200 application/json {"status":"ok"}' ;;
  *) fail "unexpected /health response: $health_meta" ;;
esac

root_meta=$(curl -fsS --max-time 5 -o /dev/null -w '%{http_code} %{content_type}' "$BASE/" 2>/dev/null) \
  || fail 'GET / failed'
case "$root_meta" in
  200*text/html*) note '/ returns 200 text/html' ;;
  *) fail "unexpected / response: $root_meta" ;;
esac

# ---------------------------------------------------------------------------
# 7. 自动探测 OpenAPI 声明的 /api/* 操作
# ---------------------------------------------------------------------------

PARSER="$WORK/parse-openapi.js"
cat > "$PARSER" <<'NODE'
const fs = require("node:fs");
const raw = fs.readFileSync(process.argv[2], "utf8");
const methods = new Set(["get", "post", "put", "patch", "delete"]);
const out = [];
const seen = new Set();

function add(method, apiPath) {
  const key = method.toUpperCase() + " " + apiPath;
  if (!seen.has(key)) {
    seen.add(key);
    out.push(key);
  }
}

if (raw.trimStart().startsWith("{")) {
  const document = JSON.parse(raw);
  for (const [apiPath, item] of Object.entries(document.paths || {})) {
    if (!apiPath.startsWith("/api/")) continue;
    for (const method of Object.keys(item || {})) {
      if (methods.has(method.toLowerCase())) add(method, apiPath);
    }
  }
} else {
  const lines = raw.split(/\r?\n/);
  let pathsIndent = -1;
  let pathIndent = -1;
  let current = null;
  for (const line of lines) {
    if (/^\s*(#|$)/.test(line)) continue;
    const indent = line.match(/^\s*/)[0].length;
    if (pathsIndent < 0) {
      if (/^\s*paths:\s*(#.*)?$/.test(line)) pathsIndent = indent;
      continue;
    }
    if (indent <= pathsIndent) break;
    if (pathIndent < 0) pathIndent = indent;
    if (indent === pathIndent) {
      const match = line.match(/^\s*['"]?(\/api\/[^'":#\s]+)['"]?\s*:(.*)$/);
      current = match ? match[1] : null;
      if (match) {
        for (const m of match[2].matchAll(/\b(get|post|put|patch|delete)\s*:/gi)) add(m[1], current);
      }
      continue;
    }
    if (current) {
      const match = line.match(/^\s*(get|post|put|patch|delete)\s*:/i);
      if (match) add(match[1], current);
    }
  }
  if (out.length === 0) {
    // 回退：没有标准 paths 块时，按 /api/ 路径行和其后的方法行扫描。
    let loosePath = null;
    let looseIndent = -1;
    for (const line of lines) {
      if (/^\s*(#|$)/.test(line)) continue;
      const indent = line.match(/^\s*/)[0].length;
      const match = line.match(/^\s*['"]?(\/api\/[^'":#\s]+)['"]?\s*:(.*)$/);
      if (match) {
        loosePath = match[1];
        looseIndent = indent;
        for (const m of match[2].matchAll(/\b(get|post|put|patch|delete)\s*:/gi)) add(m[1], loosePath);
        continue;
      }
      if (loosePath && indent > looseIndent) {
        const methodMatch = line.match(/^\s*(get|post|put|patch|delete)\s*:/i);
        if (methodMatch) add(methodMatch[1], loosePath);
      }
    }
  }
}

process.stdout.write(out.length > 0 ? out.join("\n") + "\n" : "");
NODE

OPS=()
while IFS= read -r op; do
  [ -n "$op" ] && OPS+=("$op")
done < <("$NODE_BIN" "$PARSER" "$OUT/openapi.yaml" 2>/dev/null || true)

if [ "${#OPS[@]}" -eq 0 ]; then
  openapi_json=""
  if openapi_json=$(curl -fsS --max-time 3 "$BASE/openapi.json" 2>/dev/null); then
    printf '%s' "$openapi_json" > "$WORK/openapi.json"
    while IFS= read -r op; do
      [ -n "$op" ] && OPS+=("$op")
    done < <("$NODE_BIN" "$PARSER" "$WORK/openapi.json" 2>/dev/null || true)
  fi
fi
[ "${#OPS[@]}" -gt 0 ] || fail "could not derive any /api operation from openapi.yaml; declare paths in block style or provide /openapi.json"

request() {
  # $1=method $2=path $3=body，输出响应体加最后一行 HTTP 状态码
  local method="$1" path="$2" body="$3" args=()
  args=(-sS --max-time 10 -X "$method" "$BASE$path" -H 'content-type: application/json' -w $'\n%{http_code}')
  [ -z "$body" ] || args+=(-d "$body")
  curl "${args[@]}"
}

check_json_message() {
  local label="$1" body="$2"
  printf '%s' "$body" | "$NODE_BIN" -e '
    let input = "";
    process.stdin.setEncoding("utf8");
    process.stdin.on("data", (chunk) => { input += chunk; });
    process.stdin.on("end", () => {
      let data;
      try { data = JSON.parse(input); }
      catch { console.error("FAIL: response is not valid JSON"); process.exit(1); }
      if (!data || typeof data !== "object" || Array.isArray(data)) {
        console.error("FAIL: response JSON is not an object");
        process.exit(1);
      }
      if (typeof data.message !== "string") {
        console.error("FAIL: response JSON has no string message");
        process.exit(1);
      }
      console.log("OK: response JSON contains message");
    });
  ' >/dev/null || fail "$label response is not a JSON object with a string message"
}

probe_count=0
probe_skipped=0
for op in "${OPS[@]}"; do
  if [ "$probe_count" -ge "$MAX_PROBES" ]; then
    probe_skipped=$((probe_skipped + 1))
    continue
  fi
  method=${op%% *}
  api_path=${op#* }
  api_path=$(printf '%s' "$api_path" | sed -E 's/\{[^}]+\}/verify/g')
  case "$method" in
    POST|PUT|PATCH) probe_body='{}' ;;
    *) probe_body='' ;;
  esac
  raw=$(request "$method" "$api_path" "$probe_body") \
    || fail "probe $method $api_path: request failed (app may have crashed)"
  status=$(printf '%s\n' "$raw" | sed -n '$p')
  body=$(printf '%s\n' "$raw" | sed '$d')
  case "$status" in
    2*|4*) ;;
    5*) fail "probe $method $api_path returned HTTP $status: $body" ;;
    *) fail "probe $method $api_path returned unexpected HTTP $status: $body" ;;
  esac
  check_json_message "probe $method $api_path" "$body"
  note "probe $method $api_path -> HTTP $status"
  probe_count=$((probe_count + 1))
done
[ "$probe_count" -gt 0 ] || fail "no /api operation was probed"
if [ "$probe_skipped" -gt 0 ]; then
  note "skipped $probe_skipped operations beyond the probe limit ($MAX_PROBES)"
fi

# 可选的显式成功路径：必须返回 2xx。
if [ -n "$OP_METHOD" ]; then
  raw=$(request "$OP_METHOD" "$OP_PATH" "$OP_BODY") \
    || fail "business op $OP_METHOD $OP_PATH: request failed (app may have crashed)"
  status=$(printf '%s\n' "$raw" | sed -n '$p')
  body=$(printf '%s\n' "$raw" | sed '$d')
  case "$status" in
    2*) note "business op $OP_METHOD $OP_PATH returned HTTP $status" ;;
    *) fail "business op $OP_METHOD $OP_PATH returned HTTP $status: $body" ;;
  esac
  check_json_message "business op $OP_METHOD $OP_PATH" "$body"
fi

kill -0 "$PID" 2>/dev/null || fail "app exited while requests were being served"
final_health=$(curl -sS --max-time 5 "$BASE/health" 2>/dev/null) \
  || fail '/health failed after requests'
[ "$final_health" = '{"status":"ok"}' ] || fail "/health changed after requests: $final_health"
note 'health still ok after requests'

# ---------------------------------------------------------------------------
# 8. SIGTERM 后必须自行退出
# ---------------------------------------------------------------------------

kill -TERM "$PID" 2>/dev/null || true
deadline=$((SECONDS * 1000 + TERM_GRACE_SECONDS * 1000))
while [ $((SECONDS * 1000)) -lt "$deadline" ]; do
  kill -0 "$PID" 2>/dev/null || break
  sleep 0.1
done
if kill -0 "$PID" 2>/dev/null; then
  kill -KILL "$PID" 2>/dev/null || true
  wait "$PID" 2>/dev/null || true
  PID=""
  fail "app did not exit within ${TERM_GRACE_SECONDS}s after SIGTERM"
fi
exit_status=0
wait "$PID" || exit_status=$?
PID=""
note "SIGTERM: app exited (status $exit_status)"

note "checked: archive, startup contract, /health, /, $probe_count API operations, SIGTERM"
printf 'self-test passed\n'
