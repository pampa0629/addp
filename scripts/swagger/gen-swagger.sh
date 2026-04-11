#!/bin/bash
# 用途：为指定模块（或全部模块）重新生成 Swagger 文档
# 使用：bash scripts/swagger/gen-swagger.sh [module1 module2 ...] 或 all
# 示例：
#   bash scripts/swagger/gen-swagger.sh              # 生成所有模块
#   bash scripts/swagger/gen-swagger.sh all          # 同上
#   bash scripts/swagger/gen-swagger.sh system       # 只生成 system
#   bash scripts/swagger/gen-swagger.sh system standard model

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${ROOT_DIR}"

# 所有支持 Swagger 的 Go 后端模块
ALL_MODULES=(system manager meta transfer orchestrator develop service monitor standard model quality portal graph)

# 查找 swag 可执行文件
find_swag() {
    if command -v swag &>/dev/null; then
        echo "swag"
    elif [ -f "${HOME}/go/bin/swag" ]; then
        echo "${HOME}/go/bin/swag"
    else
        echo ""
    fi
}

# 安装 swag（如果未安装）
ensure_swag() {
    local swag_bin
    swag_bin=$(find_swag)
    if [ -z "$swag_bin" ]; then
        echo "⚙️  swag 未安装，正在安装..."
        go install github.com/swaggo/swag/cmd/swag@v1.16.4
        swag_bin="${HOME}/go/bin/swag"
        echo "✅ swag 安装完成: $swag_bin"
    fi
    echo "$swag_bin"
}

# 为单个模块生成 Swagger 文档
gen_module() {
    local module=$1
    local swag_bin=$2
    local module_dir="${module}/backend"

    if [ ! -d "$module_dir" ]; then
        echo "  ⚠️  [$module] 目录不存在，跳过"
        return 0
    fi

    if [ ! -f "${module_dir}/cmd/server/main.go" ]; then
        echo "  ⚠️  [$module] 未找到 cmd/server/main.go，跳过"
        return 0
    fi

    echo "  📄 [$module] 生成中..."
    if (cd "$module_dir" && ${swag_bin} init -g cmd/server/main.go -o docs --parseDependency --parseInternal -q 2>&1); then
        echo "  ✅ [$module] 完成"
    else
        echo "  ❌ [$module] 生成失败"
        return 1
    fi
}

# 解析参数
TARGETS=()
for arg in "$@"; do
    if [ "$arg" = "all" ]; then
        TARGETS=("${ALL_MODULES[@]}")
        break
    else
        TARGETS+=("$arg")
    fi
done

# 无参数时默认生成所有
if [ ${#TARGETS[@]} -eq 0 ]; then
    TARGETS=("${ALL_MODULES[@]}")
fi

# 验证模块名
for t in "${TARGETS[@]}"; do
    valid=false
    for m in "${ALL_MODULES[@]}"; do
        [ "$t" = "$m" ] && valid=true && break
    done
    if [ "$valid" = false ]; then
        echo "❌ 未知模块: $t"
        echo "支持的模块: ${ALL_MODULES[*]}"
        exit 1
    fi
done

echo "🔧 生成 Swagger 文档: ${TARGETS[*]}"
echo ""

SWAG_BIN=$(ensure_swag)

# 并行生成
PIDS=()
FAILED_MODULES=()

for module in "${TARGETS[@]}"; do
    gen_module "$module" "$SWAG_BIN" &
    PIDS+=($!)
done

# 等待所有并行任务完成
for i in "${!PIDS[@]}"; do
    if ! wait "${PIDS[$i]}"; then
        FAILED_MODULES+=("${TARGETS[$i]}")
    fi
done

echo ""
if [ ${#FAILED_MODULES[@]} -eq 0 ]; then
    echo "✅ 全部完成（${#TARGETS[@]} 个模块）"
else
    echo "⚠️  完成，但以下模块失败: ${FAILED_MODULES[*]}"
    exit 1
fi
