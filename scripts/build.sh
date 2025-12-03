#!/usr/bin/env bash
set -euo pipefail

# Build only main packages for the modules that contain them.
# This avoids attempting to build library modules like video-converter-common
# which do not define a main package and will error out when using 'go build -o'.

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
BIN_DIR="$ROOT_DIR/bin"

mkdir -p "$BIN_DIR"

# List of module directories to consider building
MODULES=(
  "video-converter-master"
  "video-converter-worker"
  "video-converter-cli"
)

for module in "${MODULES[@]}"; do
  module_dir="$ROOT_DIR/$module"
  if [ ! -d "$module_dir" ]; then
    echo "Skipping non-existent module: $module"
    continue
  fi

  echo "Checking module for main packages: $module"
  # Find import paths of packages named 'main' in the module
  main_imports=$(cd "$module_dir" && go list -f '{{if eq .Name "main"}}{{.ImportPath}}{{end}}' ./... | sed '/^$/d')

  if [ -z "$main_imports" ]; then
    echo "No main packages found in $module - skipping build"
    continue
  fi

  # Build each main package and name the binary after the module
  # If there are multiple main packages, we append a suffix to the binary name
  idx=0
  while IFS= read -r importpath; do
    idx=$((idx+1))
    bin_name="$module"
    if [ "$idx" -gt 1 ]; then
      bin_name="$module-$idx"
    fi
    echo "Building $importpath -> $BIN_DIR/$bin_name"
    (cd "$module_dir" && go build -v -o "$BIN_DIR/$bin_name" "$importpath")
  done <<<"$main_imports"

done

ls -la "$BIN_DIR"