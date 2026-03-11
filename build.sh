#!/bin/bash

# ViSiON/3 BBS Build Script
#
# Compiles all ViSiON/3 binaries. Run ./setup.sh first for initial installation.
#
# Usage: ./build.sh

# Check Go is installed and meets the minimum required version
MIN_GO_MAJOR=1
MIN_GO_MINOR=24
MIN_GO_PATCH=2
if ! command -v go &>/dev/null; then
    echo "Error: Go is not installed. Download it from https://go.dev/dl/"
    exit 1
fi
GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
GO_MAJOR=$(echo "$GO_VERSION" | cut -d. -f1 | sed 's/[^0-9]//g')
GO_MINOR=$(echo "$GO_VERSION" | cut -d. -f2 | sed 's/[^0-9]//g')
GO_PATCH=$(echo "$GO_VERSION" | cut -d. -f3 | sed 's/[^0-9]//g')
GO_MAJOR=${GO_MAJOR:-0}
GO_MINOR=${GO_MINOR:-0}
GO_PATCH=${GO_PATCH:-0}
if [ "$GO_MAJOR" -lt "$MIN_GO_MAJOR" ] || \
   { [ "$GO_MAJOR" -eq "$MIN_GO_MAJOR" ] && [ "$GO_MINOR" -lt "$MIN_GO_MINOR" ]; } || \
   { [ "$GO_MAJOR" -eq "$MIN_GO_MAJOR" ] && [ "$GO_MINOR" -eq "$MIN_GO_MINOR" ] && [ "$GO_PATCH" -lt "$MIN_GO_PATCH" ]; }; then
    echo "Error: Go $MIN_GO_MAJOR.$MIN_GO_MINOR.$MIN_GO_PATCH or later is required (found $GO_VERSION). Download from https://go.dev/dl/"
    exit 1
fi

echo "=== Building ViSiON/3 BBS ==="
BUILT=()

if ! go build -o vision3 ./cmd/vision3; then echo "Build failed (vision3)!"; exit 1; fi
BUILT+=("  vision3   — BBS server")

if ! go build -o helper ./cmd/helper; then echo "Build failed (helper)!"; exit 1; fi
BUILT+=("  helper    — helper process")

if ! go build -o v3mail ./cmd/v3mail; then echo "Build failed (v3mail)!"; exit 1; fi
BUILT+=("  v3mail    — mail processor")

if ! go build -o strings ./cmd/strings; then echo "Build failed (strings)!"; exit 1; fi
BUILT+=("  strings   — strings editor")

if ! go build -o ue ./cmd/ue; then echo "Build failed (ue)!"; exit 1; fi
BUILT+=("  ue        — user editor")

if ! go build -o config ./cmd/config; then echo "Build failed (config)!"; exit 1; fi
BUILT+=("  config    — config editor")

if ! go build -o menuedit ./cmd/menuedit; then echo "Build failed (menuedit)!"; exit 1; fi
BUILT+=("  menuedit  — menu editor")

echo "============================="
echo "Build successful!"
echo
for item in "${BUILT[@]}"; do echo "$item"; done
echo
