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

  extract_dir=$(mktemp -d "${TMPDIR:-/tmp}/artifact-check.XXXXXX")
  trap 'rm -rf "$extract_dir"' EXIT
  tar -xzf "$ARCHIVE" -C "$extract_dir" || fail "archive could not be extracted"
  expanded_total=0
  while IFS= read -r -d '' file; do
    file_size=$(wc -c < "$file" | tr -d ' ')
    if [ "$file_size" -gt "$limit" ]; then
      fail "expanded file exceeds 100 MiB: ${file#"$extract_dir"/} ($file_size bytes)"
    fi
    expanded_total=$((expanded_total + file_size))
  done < <(find "$extract_dir" -type f -print0)
  [ "$expanded_total" -le "$limit" ] || fail "expanded archive exceeds 100 MiB ($expanded_total bytes)"
  note "archive structure (entries: $entry_count, expanded: $expanded_total bytes)"
else
  note "archive not found; skipping archive checks ($ARCHIVE)"
fi

printf 'artifact validation passed\n'
