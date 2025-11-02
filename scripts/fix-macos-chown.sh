#!/bin/bash
# 快速修复脚本 - 在 macOS 服务器上运行
# 用于修复 deploy-from-registry.sh 的 chown 兼容性问题

set -e

SCRIPT_PATH="/opt/addp/scripts/deploy-from-registry.sh"
BACKUP_PATH="/opt/addp/scripts/deploy-from-registry.sh.backup"

echo "=========================================="
echo "  macOS chown 兼容性修复脚本"
echo "=========================================="
echo ""

# 检查脚本是否存在
if [ ! -f "$SCRIPT_PATH" ]; then
  echo "❌ 错误: 未找到 $SCRIPT_PATH"
  echo ""
  echo "请先传输脚本到服务器:"
  echo "  scp scripts/deploy-from-registry.sh user@server:/opt/addp/scripts/"
  exit 1
fi

echo "==> 检查脚本是否需要修复..."

# 检查是否已经修复
if grep -q 'if \[\[ "\$OSTYPE" == "darwin"\* \]\]' "$SCRIPT_PATH" 2>/dev/null; then
  echo "✅ 脚本已经包含 macOS 兼容性修复"
  echo ""
  echo "如果仍然报错，可能是运行了错误的脚本路径。"
  echo "请确保运行: $SCRIPT_PATH"
  exit 0
fi

echo "⚠️  脚本需要修复"
echo ""

# 备份原始文件
echo "==> 备份原始脚本..."
cp "$SCRIPT_PATH" "$BACKUP_PATH"
echo "✅ 备份到: $BACKUP_PATH"

# 应用修复
echo "==> 应用 macOS 兼容性修复..."

# 使用 sed 替换 chown 命令
if [[ "$OSTYPE" == "darwin"* ]]; then
  # macOS 使用 BSD sed
  sed -i '' 's/sudo chown -R \$USER:\$USER "\$DEPLOY_DIR"/# 检测操作系统\
if [[ "\$OSTYPE" == "darwin"* ]]; then\
  # macOS\
  mkdir -p "\$DEPLOY_DIR" 2>\/dev\/null || sudo mkdir -p "\$DEPLOY_DIR"\
  sudo chown -R \$USER "\$DEPLOY_DIR" 2>\/dev\/null || true\
else\
  # Linux\
  sudo mkdir -p "\$DEPLOY_DIR"\
  sudo chown -R \$USER:\$USER "\$DEPLOY_DIR"\
fi/' "$SCRIPT_PATH"
else
  # Linux 使用 GNU sed
  sed -i 's/sudo chown -R $USER:$USER "$DEPLOY_DIR"/# 检测操作系统\nif [[ "$OSTYPE" == "darwin"* ]]; then\n  # macOS\n  mkdir -p "$DEPLOY_DIR" 2>\/dev\/null || sudo mkdir -p "$DEPLOY_DIR"\n  sudo chown -R $USER "$DEPLOY_DIR" 2>\/dev\/null || true\nelse\n  # Linux\n  sudo mkdir -p "$DEPLOY_DIR"\n  sudo chown -R $USER:$USER "$DEPLOY_DIR"\nfi/' "$SCRIPT_PATH"
fi

echo "✅ 修复完成"
echo ""
echo "=========================================="
echo "  修复成功！"
echo "=========================================="
echo ""
echo "现在可以运行部署脚本:"
echo "  REGISTRY=192.168.1.100:5001 $SCRIPT_PATH"
echo ""
echo "如果需要恢复原始脚本:"
echo "  cp $BACKUP_PATH $SCRIPT_PATH"
echo ""
