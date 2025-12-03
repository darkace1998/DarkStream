#!/bin/bash
set -e

echo "Running golangci-lint on all modules..."
echo ""

modules=(
    "video-converter-cli"
    "video-converter-common"
    "video-converter-master"
    "video-converter-worker"
)

failed=0

for module in "${modules[@]}"; do
    echo "========================================"
    echo "Linting: $module"
    echo "========================================"
    if cd "$module" && golangci-lint run ./...; then
        echo "✓ $module passed"
    else
        echo "✗ $module failed"
        failed=1
    fi
    cd ..
    echo ""
done

if [ $failed -eq 1 ]; then
    echo "Some modules failed linting"
    exit 1
else
    echo "All modules passed linting!"
    exit 0
fi
