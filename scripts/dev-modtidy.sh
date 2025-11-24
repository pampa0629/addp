#!/bin/bash

# dev-modtidy.sh - Run go mod tidy on all backend Go modules
# Usage: ./scripts/dev-modtidy.sh

set -e

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Project root directory
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

echo -e "${BLUE}=== ADDP Go Modules Tidy ===${NC}\n"

# List of all Go backend modules
GO_MODULES=(
    "common"
    "system/backend"
    "manager/backend"
    "meta/backend"
    "transfer/backend"
    "gateway"
)

# Track success/failure
SUCCESS_COUNT=0
FAILED_MODULES=()

for module in "${GO_MODULES[@]}"; do
    MODULE_PATH="$PROJECT_ROOT/$module"

    if [ ! -d "$MODULE_PATH" ]; then
        echo -e "${RED}⏭  Skipping $module (directory not found)${NC}"
        continue
    fi

    if [ ! -f "$MODULE_PATH/go.mod" ]; then
        echo -e "${RED}⏭  Skipping $module (no go.mod found)${NC}"
        continue
    fi

    echo -e "${BLUE}📦 Processing: $module${NC}"

    cd "$MODULE_PATH"

    if GOPROXY=https://goproxy.cn,direct go mod tidy; then
        echo -e "${GREEN}✓ $module - mod tidy successful${NC}\n"
        SUCCESS_COUNT=$((SUCCESS_COUNT + 1))
    else
        echo -e "${RED}✗ $module - mod tidy failed${NC}\n"
        FAILED_MODULES+=("$module")
    fi
done

# Summary
echo -e "${BLUE}=== Summary ===${NC}"
echo -e "${GREEN}✓ Success: $SUCCESS_COUNT modules${NC}"

if [ ${#FAILED_MODULES[@]} -gt 0 ]; then
    echo -e "${RED}✗ Failed: ${#FAILED_MODULES[@]} modules${NC}"
    for failed in "${FAILED_MODULES[@]}"; do
        echo -e "  - $failed"
    done
    exit 1
else
    echo -e "${GREEN}All modules tidied successfully!${NC}"
fi
