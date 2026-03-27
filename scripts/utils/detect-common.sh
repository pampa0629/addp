#!/bin/bash

# ============================================================
# Common 依赖检测工具
# ============================================================
# 功能: 检测 common 模块变化，自动标记需要重新编译的 Go 模块
# 用法: source detect-common.sh; detect_common_affected_modules
# 作者: ADDP 开发团队
# 日期: 2025-12-27
# ============================================================

# 获取文件时间戳（跨平台兼容）
# 参数: $1 - 文件路径
# 返回: 文件的修改时间戳（Unix 时间），如果文件不存在返回 0
get_file_mtime() {
    local file=$1

    # 检查文件是否存在
    if [[ ! -f "$file" ]]; then
        echo "0"
        return
    fi

    # 检测操作系统并使用相应的 stat 命令
    if [[ "$OSTYPE" == "darwin"* ]]; then
        # macOS: stat -f "%m"
        stat -f "%m" "$file" 2>/dev/null || echo "0"
    else
        # Linux: stat -c "%Y"
        stat -c "%Y" "$file" 2>/dev/null || echo "0"
    fi
}

# 获取目录下所有 .go 文件的最新时间戳
# 参数: $1 - 目录路径
# 返回: 目录中所有 .go 文件的最新修改时间戳
get_latest_go_mtime() {
    local dir=$1
    local latest=0

    # 检查目录是否存在
    if [[ ! -d "$dir" ]]; then
        echo "0"
        return
    fi

    # 查找目录下所有 .go 文件的时间戳，找出最新的
    while IFS= read -r file; do
        local mtime=$(get_file_mtime "$file")
        if [[ $mtime -gt $latest ]]; then
            latest=$mtime
        fi
    done < <(find "$dir" -type f -name "*.go" 2>/dev/null)

    echo "$latest"
}

# 检测 common 模块是否比指定二进制更新
# 参数: $1 - 二进制文件路径
#       $2 - common 模块的最新时间戳
# 返回: 0 (true) - 需要重编译, 1 (false) - 不需要重编译
is_common_newer_than_binary() {
    local binary_path=$1
    local common_mtime=$2

    # 如果二进制不存在，不需要强制重编译
    # （首次编译场景，让 start.sh 的正常编译流程处理）
    if [[ ! -f "$binary_path" ]]; then
        return 1  # false - 不需要强制重编译
    fi

    # 获取二进制文件时间戳
    local binary_mtime=$(get_file_mtime "$binary_path")

    # 如果 common 更新时间晚于二进制，返回 true
    if [[ $common_mtime -gt $binary_mtime ]]; then
        return 0  # true - 需要重编译
    fi

    return 1  # false - 不需要重编译
}

# 智能检测需要重新编译的 Go 模块
# 功能: 检测 common 模块的变化，识别所有受影响的 Go 后端模块
# 返回: 空格分隔的受影响模块列表，如果没有受影响的模块返回空字符串
detect_common_affected_modules() {
    local affected_modules=()
    local output_dir=".dev-bins"

    # 检查 common 目录是否存在
    if [[ ! -d "common" ]]; then
        # common 目录不存在，静默返回空
        echo ""
        return
    fi

    # 获取 common 目录最新时间戳（只遍历一次，性能优化）
    local common_mtime=$(get_latest_go_mtime "common")

    # 如果 common 没有 .go 文件或无法读取，跳过检测
    if [[ $common_mtime -eq 0 ]]; then
        echo ""
        return
    fi

    # Go 后端模块列表（所有模块）
    # 注意: gateway 模块的二进制名称不同，需要特殊处理
    local go_modules=(
        "system"
        "manager"
        "meta"
        "transfer"
        "orchestrator"
        "develop"
        "service"
        "monitor"
        "standard"
        "model"
        "quality"
        "asset"
        "portal"
        "gateway"
    )

    local has_affected=false

    # 遍历所有 Go 模块，检测是否需要重新编译
    for module in "${go_modules[@]}"; do
        local binary_path

        # gateway 模块的二进制文件名特殊处理
        if [[ "$module" == "gateway" ]]; then
            binary_path="${output_dir}/addp-gateway"
        else
            binary_path="${output_dir}/addp-${module}"
        fi

        # 检测 common 是否比二进制更新
        if is_common_newer_than_binary "$binary_path" "$common_mtime"; then
            affected_modules+=("$module")
            has_affected=true
        fi
    done

    # 返回受影响的模块列表（空格分隔）
    if [[ $has_affected == true ]]; then
        echo "${affected_modules[@]}"
    else
        echo ""
    fi
}
