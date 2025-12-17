#!/bin/bash
# check-deps.sh - Go Workspace 依赖版本一致性检查
#
# 功能:
#   1. 同步工作区依赖 (go work sync)
#   2. 遍历所有模块的 go.mod 文件
#   3. 检测同一依赖库的版本差异
#   4. 生成版本差异报告
#
# 用法: bash scripts/utils/check-deps.sh

set -e

# 项目根目录 (脚本所在目录向上两级)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# 定义所有业务模块
MODULES=(
    "common"
    "gateway"
    "system/backend"
    "manager/backend"
    "meta/backend"
    "transfer/backend"
    "orchestrator/backend"
    "develop/backend"
)

echo "🔍 检查 Go Workspace 依赖版本一致性..."
echo ""
echo "项目根目录: $PROJECT_ROOT"
echo ""

# 1. 同步工作区
echo "📦 同步工作区依赖..."
cd "$PROJECT_ROOT"
go work sync
echo "✅ 同步完成"
echo ""

# 2. 收集所有模块的依赖
# 使用临时文件存储依赖信息,避免关联数组在子shell中的问题
TEMP_FILE=$(mktemp)
trap "rm -f $TEMP_FILE" EXIT

echo "📥 收集各模块依赖..."
for module in "${MODULES[@]}"; do
    cd "$PROJECT_ROOT/$module"

    # 提取 go.mod 中 require 块内的直接依赖
    # 处理单行 require 和多行 require (
    {
        # 单行 require
        grep -E '^require [a-z]' go.mod 2>/dev/null | sed 's/^require //' || true
        # 多行 require ( ... )
        awk '/^require \(/,/^\)/' go.mod 2>/dev/null | grep -E '^\s+[a-z]' | sed 's/^\s*//' || true
    } | while read -r pkg version rest; do
        # 过滤掉注释和空行
        if [[ -n "$pkg" && ! "$pkg" =~ ^// ]]; then
            # 移除版本号中可能的注释
            version=$(echo "$version" | awk '{print $1}')
            # 输出格式: 包名|版本|模块
            echo "$pkg|$version|$module" >> "$TEMP_FILE"
        fi
    done
done

# 3. 检测版本冲突
echo ""
echo "📊 版本差异报告"
echo "================================"

# 按包名分组并检测版本差异
cat "$TEMP_FILE" | sort | awk -F'|' '
BEGIN {
    current_pkg = ""
}
{
    pkg = $1
    version = $2
    module = $3

    if (pkg != current_pkg) {
        # 新包,输出之前包的统计
        if (current_pkg != "") {
            check_conflicts()
        }
        # 重置
        current_pkg = pkg
        delete versions
        delete modules_by_version
        version_count = 0
    }

    # 记录版本和使用该版本的模块
    if (!(version in versions)) {
        versions[version] = 1
        version_count++
    }

    if (modules_by_version[version] == "") {
        modules_by_version[version] = module
    } else {
        modules_by_version[version] = modules_by_version[version] ", " module
    }
}

END {
    # 处理最后一个包
    if (current_pkg != "") {
        check_conflicts()
    }

    if (has_conflicts == 0) {
        print ""
        print "✅ 所有依赖版本一致,无冲突!"
    }
}

function is_local_replace_version(ver) {
    # 检测是否为本地 replace 的版本号
    # v0.0.0 或 v0.0.0-00010101000000-000000000000 都表示本地替换
    return (ver == "v0.0.0" || ver ~ /^v0\.0\.0-00010101000000/)
}

function check_conflicts() {
    if (version_count > 1) {
        # 检查是否所有版本都是本地替换版本
        all_local_replace = 1
        for (ver in versions) {
            if (!is_local_replace_version(ver)) {
                all_local_replace = 0
                break
            }
        }

        # 如果不是全部都是本地替换,才报告冲突
        if (!all_local_replace) {
            has_conflicts = 1
            print ""
            print "⚠️  " current_pkg " 存在版本差异:"

            # 按版本显示使用的模块
            for (ver in modules_by_version) {
                print "    " ver " <- " modules_by_version[ver]
            }
        }
    }
}
'

echo ""
echo "================================"
echo "✨ 检查完成!"
