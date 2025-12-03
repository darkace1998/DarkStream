#!/usr/bin/env bash
set -euo pipefail

# Run Go tests with the race detector for all modules
# Usage: ./scripts/run-race-tests.sh [module...]
# If no module list supplied, runs tests for all modules: common, master, worker, cli

MODULES=(
  "video-converter-common"
  "video-converter-master"
  "video-converter-worker"
  "video-converter-cli"
)

if [ "$#" -gt 0 ]; then
  MODULES=("$@")
fi

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "$SCRIPT_DIR/.." && pwd)

failed=0

for m in "${MODULES[@]}"; do
  echo "========================================"
  echo "Running race tests in: $m"
  echo "========================================"
  pushd "$REPO_ROOT/$m" >/dev/null || { echo "Failed to cd into $m"; exit 2; }

  # Build coverage file name using module name
  COVERAGE_FILE="coverage-$(basename $m).out"

  if go test -race -v -coverprofile="$COVERAGE_FILE" ./...; then
    echo "✓ $m passed race tests"
  else
    echo "✗ $m failed race tests"
    failed=1
  fi

  popd >/dev/null
  echo ""
done

if [ $failed -eq 1 ]; then
  echo "Some modules failed race tests"
  exit 1
else
  echo "All modules passed race detector tests"
fi
