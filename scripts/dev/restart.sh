#!/bin/bash
set -e

# 使用说明
show_usage() {
  echo "用法: $0 [-all] [-system] [-manager] [-meta] [-transfer] [-orchestrator] [-develop] [-service] [-monitor] [-gateway] [-python-workflow] [-math-workflow] [-copilot] [-spark-workflow] [-jupyter]"
  echo ""
  echo "选项:"
  echo "  无参数        只重启服务,自动检测 common 模块变化并增量编译受影响的模块"
  echo "  -all         强制重新编译所有 Go 模块 + 重启 Python 服务"
  echo "  -system      强制重新编译 System 模块"
  echo "  -manager     强制重新编译 Manager 模块"
  echo "  -meta        强制重新编译 Meta 模块"
  echo "  -transfer    强制重新编译 Transfer 模块"
  echo "  -orchestrator 强制重新编译 Orchestrator 模块"
  echo "  -develop     强制重新编译 Develop 模块"
  echo "  -service     强制重新编译 Service 模块"
  echo "  -monitor     强制重新编译 Monitor 模块"
  echo "  -gateway     强制重新编译 Gateway 模块"
  echo "  -python-workflow   重启 Python Workflow Engine (Python 服务)"
  echo "  -math-workflow     重启 Math Workflow Engine (Python 服务)"
  echo "  -copilot     重启 Copilot Backend (Python 服务)"
  echo "  -spark-workflow 重启 Spark 工作流 Engine (Python 服务)"
  echo "  -jupyter     重启 Jupyter Engine (Python 服务)"
  echo ""
  echo "智能检测说明:"
  echo "  - 无参数时会自动检测 common 模块是否有变化"
  echo "  - 如果检测到 common 变化,会自动重新编译所有依赖的 Go 模块"
  echo "  - 指定 Go 模块参数时,不执行智能检测,直接按参数编译"
  echo "  - 只指定 Python 服务参数时,仍会执行智能检测"
  echo ""
  echo "注意:"
  echo "  - Python Workflow Engine、Spark 工作流 Engine、Jupyter Engine 和 Copilot (Python) 会自动重启"
  echo "  - 只有 Go 后端模块支持选择性编译"
  echo ""
  echo "示例:"
  echo "  $0                    # 智能检测 + 重启 (推荐)"
  echo "  $0 -system -meta      # 重启并重新编译 system 和 meta"
  echo "  $0 -python-workflow         # 智能检测 + 重启 Python Workflow Engine"
  echo "  $0 -all               # 重启并重新编译所有模块 (完整)"
  exit 1
}

echo "🔄 重启 ADDP 开发环境"
echo ""

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

cd "${ROOT_DIR}"

# 解析参数
FORCE_BUILD_ALL=false
FORCE_BUILD_MODULES=()

for arg in "$@"; do
  case $arg in
    -h|--help)
      show_usage
      ;;
    -all)
      FORCE_BUILD_ALL=true
      ;;
    -system|-manager|-meta|-transfer|-orchestrator|-develop|-service|-monitor|-gateway|-python-workflow|-math-workflow|-copilot|-spark-workflow|-jupyter)
      module="${arg#-}"  # 移除前导的 -
      FORCE_BUILD_MODULES+=("$module")
      ;;
    *)
      echo "❌ 未知参数: $arg"
      show_usage
      ;;
  esac
done

# ============================================================
# 智能检测 Common 依赖
# ============================================================

# 检查是否有 Go 模块参数（排除 Python 服务参数）
has_go_module_params() {
    for module in "${FORCE_BUILD_MODULES[@]}"; do
        # Python 服务列表
        if [[ "$module" != "python-workflow" &&
              "$module" != "math-workflow" &&
              "$module" != "copilot" &&
              "$module" != "spark-workflow" &&
              "$module" != "jupyter" ]]; then
            return 0  # 有 Go 模块参数
        fi
    done
    return 1  # 只有 Python 服务参数或无参数
}

# 在用户未指定 Go 模块编译选项时，自动检测 common 变化
if [ "$FORCE_BUILD_ALL" = false ] && ! has_go_module_params; then
    echo "🔍 检测 common 模块依赖..."

    # 加载检测函数
    source "${SCRIPT_DIR}/../utils/detect-common.sh"

    # 执行智能检测
    AFFECTED_MODULES=$(detect_common_affected_modules)

    if [ -n "$AFFECTED_MODULES" ]; then
        echo "📦 检测到 common 模块已更新，以下模块需要重新编译:"
        echo "   ${AFFECTED_MODULES}"
        echo ""

        # 自动标记受影响的模块需要重新编译
        for module in $AFFECTED_MODULES; do
            FORCE_BUILD_MODULES+=("$module")
        done

        echo "✅ 已自动标记 ${#FORCE_BUILD_MODULES[@]} 个模块需要重新编译"
    else
        echo "✅ common 模块无变化，使用增量编译"
    fi
    echo ""
fi

# ============================================================
# 显示编译计划
# ============================================================

# 显示编译计划
if [ "$FORCE_BUILD_ALL" = true ]; then
  echo "📦 编译计划: 重新编译所有模块"
elif [ ${#FORCE_BUILD_MODULES[@]} -gt 0 ]; then
  echo "📦 编译计划: 重新编译 ${FORCE_BUILD_MODULES[*]}"
else
  echo "📦 编译计划: 仅重启服务,按需增量编译"
fi
echo ""

# 1. 停止服务
if "${SCRIPT_DIR}/stop.sh"; then
  echo ""
  echo "✅ 已停止现有服务"
else
  echo ""
  echo "⚠️ 停止脚本返回非零状态,继续执行启动流程"
fi

# 2. 强制重新编译(如果需要)
if [ "$FORCE_BUILD_ALL" = true ]; then
  echo ""
  echo "🔨 强制重新编译所有模块..."

  # Touch 所有 .go 文件
  find . -type f -name "*.go" -path "*/backend/*" -exec touch {} \; 2>/dev/null || true
  find . -type f -name "*.go" -path "*/common/*" -exec touch {} \; 2>/dev/null || true
  find . -type f -name "*.go" -path "*/gateway/*" -exec touch {} \; 2>/dev/null || true

  # 删除所有二进制
  rm -rf .dev-bins 2>/dev/null || true

  # 清理构建缓存
  go clean -cache 2>/dev/null || true

  echo "✅ 已标记所有模块需要重新编译"

elif [ ${#FORCE_BUILD_MODULES[@]} -gt 0 ]; then
  echo ""
  echo "🔨 强制重新编译指定模块..."

  for module in "${FORCE_BUILD_MODULES[@]}"; do
    echo "  处理 $module 模块..."

    # Touch 指定模块的源文件
    if [ "$module" = "gateway" ]; then
      find gateway -type f -name "*.go" -exec touch {} \; 2>/dev/null || true
    elif [ "$module" = "copilot" ]; then
      # Copilot 是 Python 服务，不需要编译，只需清理虚拟环境
      echo "  标记 Copilot Backend 需要重启（无需编译）"
    elif [ "$module" = "python-workflow" ]; then
      # Python Workflow Engine 是 Python 服务，不需要编译
      echo "  标记 Python Workflow Engine 需要重启（无需编译）"
    elif [ "$module" = "math-workflow" ]; then
      # Math Workflow Engine 是 Python 服务，不需要编译
      echo "  标记 Math Workflow Engine 需要重启（无需编译）"
    elif [ "$module" = "spark-workflow" ]; then
      # Spark 工作流 Engine 是 Python 服务，不需要编译
      echo "  标记 Spark 工作流 Engine 需要重启（无需编译）"
    elif [ "$module" = "jupyter" ]; then
      # Jupyter Engine 是 Python 服务，不需要编译
      echo "  标记 Jupyter Engine 需要重启（无需编译）"
    else
      find "${module}/backend" -type f -name "*.go" -exec touch {} \; 2>/dev/null || true
    fi

    # 删除指定模块的二进制
    if [ "$module" = "gateway" ]; then
      rm -f .dev-bins/addp-gateway 2>/dev/null || true
    elif [ "$module" = "copilot" ]; then
      # Python 服务无二进制文件
      :
    elif [ "$module" = "python-workflow" ]; then
      # Python 服务无二进制文件
      :
    elif [ "$module" = "math-workflow" ]; then
      # Python 服务无二进制文件
      :
    elif [ "$module" = "spark-workflow" ]; then
      # Python 服务无二进制文件
      :
    elif [ "$module" = "jupyter" ]; then
      # Python 服务无二进制文件
      :
    else
      rm -f .dev-bins/addp-${module} 2>/dev/null || true
      rm -f .dev-bins/addp-${module}-worker 2>/dev/null || true
    fi

    # 清理指定模块的构建缓存
    if [ "$module" = "gateway" ]; then
      (cd gateway && go clean -cache 2>/dev/null) || true
    elif [ "$module" = "copilot" ]; then
      # Python 服务无需清理 Go 缓存
      :
    elif [ "$module" = "python-workflow" ]; then
      # Python 服务无需清理 Go 缓存
      :
    elif [ "$module" = "math-workflow" ]; then
      # Python 服务无需清理 Go 缓存
      :
    elif [ "$module" = "spark-workflow" ]; then
      # Python 服务无需清理 Go 缓存
      :
    elif [ "$module" = "jupyter" ]; then
      # Python 服务无需清理 Go 缓存
      :
    else
      (cd "${module}/backend" && go clean -cache 2>/dev/null) || true
    fi
  done

  # 总是 touch common 模块(因为其他模块可能依赖它)
  find common -type f -name "*.go" -exec touch {} \; 2>/dev/null || true

  echo "✅ 已标记 ${FORCE_BUILD_MODULES[*]} 需要重新编译"
fi

echo ""

# 3. 启动服务
exec "${SCRIPT_DIR}/start.sh"
