#!/usr/bin/env bash
# check-deps-version.sh - 根据技术栈规约验证全部 Go 模块的依赖版本
#
# 功能:
#   1. 从 docs/spec/addp技术栈规约.md 读取唯一目标版本
#   2. 自动发现仓库内全部 go.mod 并校验其中的规约依赖声明
#
# 用法: bash scripts/utils/check-deps-version.sh

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
PROJECT_ROOT=$(cd "$SCRIPT_DIR/../.." && pwd)
SPEC_FILE="$PROJECT_ROOT/docs/spec/addp技术栈规约.md"

cd "$PROJECT_ROOT"

echo "🔍 检查 ADDP 所有 Go 模块的依赖版本一致性..."
echo "规约事实源: ${SPEC_FILE#$PROJECT_ROOT/}"
echo ""

if ! command -v rg >/dev/null 2>&1; then
    echo "❌ 缺少 ripgrep (rg)，无法发现全部 go.mod"
    exit 1
fi

if [ ! -f "$SPEC_FILE" ]; then
    echo "❌ 技术栈规约不存在: $SPEC_FILE"
    exit 1
fi

SPEC_REFS=$(
    grep -oE '`[[:alnum:]._-]+/[[:alnum:]_.~/-]+@v[^`[:space:]]+`' "$SPEC_FILE" \
        | tr -d '`' \
        | sort -u \
        || true
)

if [ -z "$SPEC_REFS" ]; then
    echo "❌ 技术栈规约中未找到 Go 依赖版本声明"
    exit 1
fi

MODULE_FILES=$(rg --files -g 'go.mod' | sort)
if [ -z "$MODULE_FILES" ]; then
    echo "❌ 仓库中未找到 go.mod"
    exit 1
fi

INVALID_SPEC=0
PREVIOUS_DEP=""
PREVIOUS_VERSION=""
while IFS= read -r ref; do
    dep=${ref%@*}
    version=${ref##*@}
    if [ "$dep" = "$PREVIOUS_DEP" ] && [ "$version" != "$PREVIOUS_VERSION" ]; then
        echo "❌ 规约中同一依赖存在多个目标版本: $dep ($PREVIOUS_VERSION, $version)"
        INVALID_SPEC=1
    fi
    PREVIOUS_DEP=$dep
    PREVIOUS_VERSION=$version
done <<< "$SPEC_REFS"

if [ "$INVALID_SPEC" -ne 0 ]; then
    exit 1
fi

INCONSISTENT=0
CHECKED_DEPENDENCIES=0
CHECKED_DECLARATIONS=0

while IFS= read -r ref; do
    dep=${ref%@*}
    expected=${ref##*@}
    dependency_declarations=0
    dependency_inconsistent=0

    while IFS= read -r mod; do
        actual=$(awk -v dep="$dep" '$1 == dep { print $2; exit }' "$mod")
        if [ -z "$actual" ]; then
            continue
        fi

        dependency_declarations=$((dependency_declarations + 1))
        CHECKED_DECLARATIONS=$((CHECKED_DECLARATIONS + 1))
        if [ "$actual" != "$expected" ]; then
            echo "  ❌ $dep: 规约 $expected，$mod 声明 $actual"
            dependency_inconsistent=1
            INCONSISTENT=1
        fi
    done <<< "$MODULE_FILES"

    if [ "$dependency_declarations" -gt 0 ]; then
        CHECKED_DEPENDENCIES=$((CHECKED_DEPENDENCIES + 1))
        if [ "$dependency_inconsistent" -eq 0 ]; then
            echo "  ✅ $dep: $expected ($dependency_declarations 个模块声明)"
        fi
    fi
done <<< "$SPEC_REFS"

echo ""
if [ "$INCONSISTENT" -eq 0 ]; then
    echo "✨ 检查通过：$CHECKED_DEPENDENCIES 个规约依赖，$CHECKED_DECLARATIONS 处 go.mod 声明。"
    exit 0
fi

echo "⚠️  检查失败：请先修订技术栈规约确定唯一目标版本，再统一各模块 go.mod。"
exit 1
