#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
MODULES=(
  video-converter-common
  video-converter-master
  video-converter-worker
  video-converter-cli
)

echo "Running go vet across modules..."
ret=0
for module in "${MODULES[@]}"; do
  module_dir="$ROOT_DIR/$module"
  if [ ! -d "$module_dir" ]; then
    echo "Skipping non-existent module: $module"
    continue
  fi
  echo "--> vet $module"
  if ! (cd "$module_dir" && go vet ./...); then
    echo "go vet failed for $module"
    ret=1
  fi
done

if [ $ret -ne 0 ]; then
  echo "go vet found issues"
  exit 1
fi

echo "go vet complete: no issues found"
