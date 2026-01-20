#!/bin/bash

# Create output directories
mkdir -p bin/windows
mkdir -p bin/linux

echo "Building for Windows (amd64)..."

## 💻 Windows Build

# Build Agent
# -s: Omit the symbol table and debug information
# -w: Omit the DWARF symbol table
# -H=windowsgui: (Optional) Hides the console window. Remove this if you want to see logs.
# CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w -H=windowsgui" -o bin/windows/agent.exe ./agent

# CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o bin/windows/agent.exe ./agent
# echo "Agent ✅"

# # Build Controller
# CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o bin/windows/controller.exe ./controller
# echo "Controller ✅"

# # Build Keygen
# CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o bin/windows/keygen.exe ./cmd/keygen
# echo "Keygen ✅"

# echo "Windows build complete! Binaries are in bin/windows/"

echo "Building for Linux (amd64)..."

## 🐧 Linux Build

# Build Agent
echo "Agent ✅"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o bin/linux/agent ./agent

# Build Controller
echo "Controller ✅"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o bin/linux/controller ./controller

# Build Keygen
echo "Keygen ✅"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o bin/linux/keygen ./cmd/keygen

echo "Linux build complete! Binaries are in bin/linux/"
