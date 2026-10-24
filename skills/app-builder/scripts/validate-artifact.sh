#!/usr/bin/env bash
set -euo pipefail

ROOT="${1:-outputs}"
ARCHIVE="${2:-artifact.tar.gz}"

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

note() {
  printf 'OK: %s\n' "$*"
}

[ -d "$ROOT" ] || fail "artifact root not found: $ROOT"

for file in app/app.js app/package.json app/public/index.html openapi.yaml launch.json; do
  [ -f "$ROOT/$file" ] || fail "missing $file"
done
note "required files exist"

command -v node >/dev/null 2>&1 || fail "node is required for validation"
node --check "$ROOT/app/app.js" || fail "app/app.js has a syntax error"
node -e 'JSON.parse(require("node:fs").readFileSync(process.argv[1], "utf8"))' "$ROOT/app/package.json" \
  || fail "app/package.json is not valid JSON"
note "app/app.js syntax and app/package.json are valid"

LAUNCH_PATH="$ROOT/launch.json" ROOT_DIR="$ROOT" node -e '
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
  const entry = launch.runtimeArgs.find(
    (arg) => arg.startsWith("app/") && fs.existsSync(path.join(root, arg))
  );
  check(entry !== undefined, "runtimeArgs must reference an existing file under app/");
  check(
    launch.healthPath === undefined || launch.healthPath === "/health",
    "healthPath must be /health"
  );
  if (launch.readyTimeoutMs !== undefined) {
    check(
      Number.isInteger(launch.readyTimeoutMs) && launch.readyTimeoutMs > 0 && launch.readyTimeoutMs <= 600000,
      "readyTimeoutMs must be an integer between 1 and 600000"
    );
  }
  console.log("OK: launch.json");
} catch (error) {
  console.error(`FAIL: ${error.message}`);
  process.exit(1);
}
'

grep -Eq '^openapi:[[:space:]]*["'"'"']?3\.1' "$ROOT/openapi.yaml" \
  || fail "openapi.yaml must declare OpenAPI 3.1"
operation_count=$(grep -Ec '^[[:space:]]*operationId:[[:space:]]*[^[:space:]]+' "$ROOT/openapi.yaml" || true)
[ "${operation_count:-0}" -ge 1 ] || fail "openapi.yaml must declare at least one operationId"
bad_paths=$(grep -E '^[[:space:]]{2}/[^:]*:' "$ROOT/openapi.yaml" | grep -Ev '^[[:space:]]{2}/api/' || true)
[ -z "$bad_paths" ] || fail "openapi.yaml paths must start with /api/: $bad_paths"
note "openapi.yaml structure"

if grep -REq "(src|href)[[:space:]]*=[[:space:]]*['\"]https?://" "$ROOT/app/public"; then
  fail "app/public must not reference external resources"
fi
note "app/public has no external resources"

if [ -f "$ARCHIVE" ]; then
  size=$(wc -c < "$ARCHIVE" | tr -d ' ')
  [ "$size" -le $((100 * 1024 * 1024)) ] || fail "archive exceeds 100 MiB"
  tar -tzf "$ARCHIVE" >/dev/null 2>&1 || fail "archive is not a valid tar.gz"
  entries=$(tar -tzf "$ARCHIVE")
  printf '%s\n' "$entries" | grep -Eq '^(\./)?launch\.json$' || fail "archive root is missing launch.json"
  printf '%s\n' "$entries" | grep -Eq '^(\./)?openapi\.yaml$' || fail "archive root is missing openapi.yaml"
  printf '%s\n' "$entries" | grep -Eq '^(\./)?app/' || fail "archive root is missing app/"
  if printf '%s\n' "$entries" | grep -Eq '^(\./)?outputs(/|$)'; then
    fail "archive must not contain an outputs/ prefix"
  fi
  if printf '%s\n' "$entries" | grep -Eq '(^|/)\.\.(/|$)|^/'; then
    fail "archive contains absolute or parent-directory paths"
  fi
  note "archive structure"
else
  note "archive not found; skipping archive checks ($ARCHIVE)"
fi

printf 'artifact validation passed\n'
