#!/bin/bash

# Script to run 'go mod tidy' in all directories containing go.mod (in parallel)
# Usage: ./scripts/tidy-all.sh

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

echo "🔍 Searching for go.mod files in: $ROOT_DIR"
echo ""

# Find all directories containing go.mod
GO_MODULES=$(find "$ROOT_DIR" -name "go.mod" -type f -exec dirname {} \;)

if [ -z "$GO_MODULES" ]; then
    echo "❌ No go.mod files found"
    exit 1
fi

# Temporary directory for results
TEMP_DIR=$(mktemp -d)
trap "rm -rf $TEMP_DIR" EXIT

# Function to process a single module
process_module() {
    local MODULE_DIR="$1"
    local ROOT_DIR="$2"
    local TEMP_DIR="$3"

    RELATIVE_PATH="${MODULE_DIR#$ROOT_DIR/}"
    RESULT_FILE="$TEMP_DIR/$(echo "$RELATIVE_PATH" | sed 's/\//_/g')"

    echo "📦 Processing: $RELATIVE_PATH"

    if (cd "$MODULE_DIR" && go mod tidy 2>&1); then
        echo "success" > "$RESULT_FILE"
        echo "✅ Success: $RELATIVE_PATH"
    else
        echo "failed" > "$RESULT_FILE"
        echo "❌ Failed: $RELATIVE_PATH"
    fi
}

export -f process_module

# Process all modules in parallel
echo "🚀 Running go mod tidy in parallel..."
echo ""

while IFS= read -r MODULE_DIR; do
    process_module "$MODULE_DIR" "$ROOT_DIR" "$TEMP_DIR" &
done <<< "$GO_MODULES"

# Wait for all background jobs to complete
wait

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

# Count results
SUCCESS_COUNT=0
FAIL_COUNT=0
FAILED_MODULES=()

for RESULT_FILE in "$TEMP_DIR"/*; do
    if [ -f "$RESULT_FILE" ]; then
        STATUS=$(cat "$RESULT_FILE")
        MODULE_NAME=$(basename "$RESULT_FILE" | sed 's/_/\//g')

        if [ "$STATUS" = "success" ]; then
            ((SUCCESS_COUNT++))
        else
            ((FAIL_COUNT++))
            FAILED_MODULES+=("$MODULE_NAME")
        fi
    fi
done

# Summary
echo "Summary:"
echo "  ✅ Successful: $SUCCESS_COUNT"
echo "  ❌ Failed: $FAIL_COUNT"

if [ $FAIL_COUNT -gt 0 ]; then
    echo ""
    echo "Failed modules:"
    for MODULE in "${FAILED_MODULES[@]}"; do
        echo "  - $MODULE"
    done
    exit 1
fi

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "🎉 All modules tidied successfully!"
