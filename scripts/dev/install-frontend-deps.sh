#!/bin/bash

# 批量安装所有前端模块的依赖
# 使用方法: bash scripts/dev/install-frontend-deps.sh

set -e

echo "========================================="
echo "安装所有前端模块依赖"
echo "========================================="
echo ""

MODULES=(
  "system/frontend"
  "manager/frontend"
  "meta/frontend"
  "transfer/frontend"
  "orchestrator/frontend"
  "develop/frontend"
  "service/frontend"
  "console/frontend"
  "monitor/frontend"
  "catalog/frontend"
  "inference/frontend"
)

# 先清理 common-frontend
echo "清理 common-frontend..."
rm -rf common-frontend/node_modules
rm -f common-frontend/package-lock.json
echo "✓ common-frontend 清理完成"
echo ""

# 安装各模块依赖
for MODULE in "${MODULES[@]}"; do
  if [ -f "$MODULE/package.json" ]; then
    echo "安装 $MODULE 依赖..."

    cd "$MODULE"

    # 删除旧的 node_modules 和 lock 文件
    rm -rf node_modules
    rm -f package-lock.json

    # 安装依赖
    npm install

    cd - > /dev/null

    echo "✓ $MODULE 安装完成"
    echo ""
  else
    echo "⚠ 跳过 $MODULE (未找到 package.json)"
  fi
done

echo "========================================="
echo "✓ 所有模块依赖安装完成"
echo "========================================="
echo ""
echo "统一版本信息："
echo "- Element Plus: 2.8.8"
echo "- Vue: 3.5.13"
echo "- Vite: 6.0.5"
echo ""
echo "建议操作："
echo "1. 执行 bash scripts/dev/restart.sh -all 重启所有服务"
echo "2. 检查浏览器控制台，确认无弃用警告"
