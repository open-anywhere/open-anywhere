#!/usr/bin/env bash
# 把技能包内的应用模板复制到产物目录，一条命令完成。
# 用法: init-template.sh [outputs-dir]，默认 /mnt/session/outputs。
set -euo pipefail

SKILL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="${1:-/mnt/session/outputs}"
TPL="$SKILL_DIR/references/app-template"

[ -d "$TPL" ] || {
  printf 'FAIL: template not found: %s\n' "$TPL" >&2
  exit 1
}

mkdir -p "$OUT"
cp -R "$TPL/." "$OUT/"

printf 'OK: template copied to %s\n' "$OUT"
printf '%s\n' '--- copied files ---'
(cd "$OUT" && find . -type f | sort)
