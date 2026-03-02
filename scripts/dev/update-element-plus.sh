#!/bin/bash

# 统一更新所有前端模块的 Element Plus 版本到 2.8.8
# 使用方法: bash scripts/dev/update-element-plus.sh

set -e

ELEMENT_PLUS_VERSION="2.8.8"
ICONS_VERSION="2.3.2"

echo "========================================="
echo "统一更新 Element Plus 到 ${ELEMENT_PLUS_VERSION}"
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
)

for MODULE in "${MODULES[@]}"; do
  if [ -f "$MODULE/package.json" ]; then
    echo "更新 $MODULE ..."

    cd "$MODULE"

    # 更新 Element Plus 和图标库
    npm install element-plus@${ELEMENT_PLUS_VERSION} @element-plus/icons-vue@${ICONS_VERSION} --save

    cd - > /dev/null

    echo "✓ $MODULE 更新完成"
    echo ""
  else
    echo "⚠ 跳过 $MODULE (未找到 package.json)"
  fi
done

echo "========================================="
echo "✓ 所有模块已更新完成"
echo "========================================="
echo ""
echo "建议操作："
echo "1. 执行 npm install 重新安装依赖"
echo "2. 重启所有前端服务验证"
