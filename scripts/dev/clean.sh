#!/bin/bash
# 清理开发环境的编译产物

echo "🧹 清理 ADDP 开发环境编译产物"
echo ""

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

cd "${ROOT_DIR}"

# 清理二进制文件
if [ -d ".dev-bins" ]; then
  echo "🗑️  清理开发二进制..."
  rm -rf .dev-bins
  echo "✓ 开发二进制已清理"
else
  echo "✓ 无需清理开发二进制（目录不存在）"
fi

echo ""

# 清理 Go 构建缓存
echo "🗑️  清理 Go 构建缓存..."
go clean -cache 2>/dev/null || true
echo "✓ Go 构建缓存已清理"

echo ""

# 清理 Python 虚拟环境
echo "🗑️  清理 Python 虚拟环境..."
cleaned_py=0
if [ -d "engines/geopandas/venv" ]; then
  rm -rf engines/geopandas/venv
  echo "✓ GeoPandas Engine venv 已清理"
  ((cleaned_py++))
fi
if [ -d "copilot/backend/venv" ]; then
  rm -rf copilot/backend/venv
  echo "✓ Copilot Backend venv 已清理"
  ((cleaned_py++))
fi
if [ $cleaned_py -eq 0 ]; then
  echo "✓ 无需清理 Python 虚拟环境（目录不存在）"
fi

echo ""

# 清理前端缓存
echo "🗑️  清理前端缓存..."
for frontend_dir in portal/frontend system/frontend manager/frontend meta/frontend transfer/frontend orchestrator/frontend develop/frontend service/frontend; do
  if [ -d "$frontend_dir" ]; then
    rm -rf "$frontend_dir/node_modules/.vite" "$frontend_dir/.vite" 2>/dev/null || true
  fi
done
echo "✓ 前端缓存已清理"

echo ""
echo "✅ 清理完成"
echo ""
echo "提示："
echo "  - 下次启动时将重新编译所有模块"
echo "  - Python 虚拟环境将重新创建并安装依赖"
echo "  - 启动命令: bash scripts/dev/start.sh"
