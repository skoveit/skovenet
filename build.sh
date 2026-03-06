#!/bin/bash
set -e

echo "=== SkoveNet Build ==="
echo

# ──────────────────────────────────────
# Controller
# ──────────────────────────────────────
echo "Building controller (linux/amd64)..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o bin/linux/controller ./controller
echo "  ✅ bin/linux/controller"

echo "Building controller (windows/amd64)..."
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o bin/windows/controller.exe ./controller
echo "  ✅ bin/windows/controller.exe"

echo "Building controller (darwin/arm64)..."
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o bin/darwin/controller ./controller
echo "  ✅ bin/darwin/controller"

echo

# ──────────────────────────────────────
# sgen (Agent Generator)
# ──────────────────────────────────────
echo "Building sgen..."
make sgen
echo "  ✅ bin/linux/sgen"

echo
echo "=== Done ==="
echo
echo "Generate agents with:"
echo "  ./bin/linux/sgen generate --os linux --arch amd64"
