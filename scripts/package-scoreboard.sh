#!/bin/sh
set -eu
repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
artifact_path=${1:-"$repo_dir/e2e/artifacts/scoreboard.tar.gz"}
mkdir -p "$(dirname -- "$artifact_path")"
COPYFILE_DISABLE=1 tar -czf "$artifact_path" -C "$repo_dir/e2e/testdata/scoreboard" .
printf '%s\n' "$artifact_path"
