#!/bin/bash
# Dependency security check script

set -e

echo "=========================================="
echo "Zion Node Dependency Security Audit"
echo "=========================================="
echo ""

# Check if govulncheck is installed
if ! command -v govulncheck &> /dev/null; then
    echo "⚠️  govulncheck not installed, installing..."
    go install golang.org/x/vuln/cmd/govulncheck@latest
fi

echo "1. Checking for known vulnerabilities..."
echo "----------------------------------------"
govulncheck ./... || true
echo ""

echo "2. Checking for outdated dependencies..."
echo "----------------------------------------"
go list -u -m all | grep -E "\[.*\]" || echo "✅ All dependencies are up to date"
echo ""

echo "3. Verifying dependency integrity..."
echo "----------------------------------------"
go mod verify && echo "✅ Dependency integrity verified" || echo "❌ Dependency integrity verification failed"
echo ""

echo "4. Checking direct dependency versions..."
echo "----------------------------------------"
echo "Direct dependencies:"
go list -m -f '{{.Path}} {{.Version}}' $(go list -m -f '{{if not .Indirect}}{{.Path}}{{end}}' all) | grep -v "^github.com/zion-protocol"
echo ""

echo "5. Generating dependency report..."
echo "----------------------------------------"
go list -m all > dependencies-$(date +%Y%m%d).txt
echo "✅ Dependency list saved to dependencies-$(date +%Y%m%d).txt"
echo ""

echo "6. Checking high-risk dependencies..."
echo "----------------------------------------"
echo "Checking gogo/protobuf..."
if go list -m all | grep -q "gogo/protobuf"; then
    VERSION=$(go list -m -f '{{.Version}}' github.com/gogo/protobuf 2>/dev/null || echo "not found")
    echo "⚠️  Found gogo/protobuf: $VERSION"
    echo "   Recommendation: check if it can be updated or replaced"
else
    echo "✅ gogo/protobuf not found"
fi
echo ""

echo "=========================================="
echo "Security audit complete"
echo "=========================================="
