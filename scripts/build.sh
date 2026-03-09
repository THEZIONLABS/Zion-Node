#!/usr/bin/env bash
set -euo pipefail

echo "🔨 Building Zion Node..."
mkdir -p release
go build -o release/zion-node ./cmd/zion-node/

if [ -f release/zion-node ]; then
    echo "✅ Build successful: release/zion-node"
    ls -lh release/zion-node
else
    echo "❌ Build failed"
    exit 1
fi
