#!/bin/bash
set -e

# 使用说明
show_usage() {
  echo "用法: $0 [-all] [-system] [-manager] [-meta] [-transfer] [-orchestrator] [-develop] [-service] [-gateway] [-geopandas] [-copilot] [-spark-sedona]"
  echo ""
  echo "选项:"
  echo "  无参数        只重启服务,不重新编译"
  echo "  -all         强制重新编译所有 Go 模块 + 重启 Python 服务"
  echo "  -system      强制重新编译 System 模块"
  echo "  -manager     强制重新编译 Manager 模块"
  echo "  -meta        强制重新编译 Meta 模块"
  echo "  -transfer    强制重新编译 Transfer 模块"
  echo "  -orchestrator 强制重新编译 Orchestrator 模块"
  echo "  -develop     强制重新编译 Develop 模块"
  echo "  -service     强制重新编译 Service 模块"
  echo "  -gateway     强制重新编译 Gateway 模块"
  echo "  -geopandas   重启 GeoPandas Engine (Python 服务)"
  echo "  -copilot     重启 Copilot Backend (Python 服务)"
  echo "  -spark-sedona 重启 Spark Sedona Engine (Python 服务)"
  echo ""
  echo "注意:"
  echo "  - GeoPandas Engine、Spark Sedona Engine 和 Copilot (Python) 会自动重启"
  echo "  - 只有 Go 后端模块支持选择性编译"
  echo ""
  echo "示例:"
  echo "  $0                    # 只重启,不编译 (快速)"
  echo "  $0 -system -meta      # 重启并重新编译 system 和 meta"
  echo "  $0 -geopandas         # 重启 GeoPandas Engine"
  echo "  $0 -copilot           # 重启 Copilot Backend"
  echo "  $0 -spark-sedona      # 重启 Spark Sedona Engine"
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
    -system|-manager|-meta|-transfer|-orchestrator|-develop|-service|-gateway|-geopandas|-copilot|-spark-sedona)
      module="${arg#-}"  # 移除前导的 -
      FORCE_BUILD_MODULES+=("$module")
      ;;
    *)
      echo "❌ 未知参数: $arg"
      show_usage
      ;;
  esac
done

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
    elif [ "$module" = "spark-sedona" ]; then
      # Spark Sedona Engine 是 Python 服务，不需要编译
      echo "  标记 Spark Sedona Engine 需要重启（无需编译）"
    else
      find "${module}/backend" -type f -name "*.go" -exec touch {} \; 2>/dev/null || true
    fi

    # 删除指定模块的二进制
    if [ "$module" = "gateway" ]; then
      rm -f .dev-bins/addp-gateway 2>/dev/null || true
    elif [ "$module" = "copilot" ]; then
      # Python 服务无二进制文件
      :
    elif [ "$module" = "spark-sedona" ]; then
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
    elif [ "$module" = "spark-sedona" ]; then
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
