#!/bin/bash
# 配置 Docker 信任私有 Registry
# 使用方法: ./scripts/registry/configure.sh 192.168.31.238:5001

set -e

if [ -z "$1" ]; then
  echo "使用方法: $0 registry-address"
  echo "示例: $0 192.168.31.238:5001"
  exit 1
fi

REGISTRY="$1"

echo "==========================================="
echo "  配置 Docker 信任私有 Registry"
echo "==========================================="
echo ""
echo "Registry: $REGISTRY"
echo ""

# 检测操作系统
if [[ "$OSTYPE" == "darwin"* ]]; then
  # macOS
  echo "检测到 macOS 系统"
  echo ""
  echo "Docker Desktop 配置方法:"
  echo ""
  echo "1. 打开 Docker Desktop"
  echo "2. 点击菜单栏 Docker 图标 → Settings (⚙️)"
  echo "3. 选择 'Docker Engine'"
  echo "4. 在 JSON 配置中添加："
  echo ""
  echo '{'
  echo '  "insecure-registries": ["'$REGISTRY'"]'
  echo '}'
  echo ""
  echo "5. 点击 'Apply & Restart'"
  echo ""

  # 检查是否已配置
  CONFIG_FILE="$HOME/Library/Group Containers/group.com.docker/settings.json"
  if [ -f "$CONFIG_FILE" ]; then
    if grep -q "$REGISTRY" "$CONFIG_FILE" 2>/dev/null; then
      echo "✅ Registry 已在配置中"
    else
      echo "⚠️  请按照上述步骤手动配置"
    fi
  else
    echo "⚠️  Docker Desktop 配置文件未找到，请手动配置"
  fi

  echo ""
  read -p "配置完成后按任意键继续..." -n1 -s
  echo ""

else
  # Linux
  echo "检测到 Linux 系统"
  echo ""

  DAEMON_JSON="/etc/docker/daemon.json"

  if [ -f "$DAEMON_JSON" ]; then
    # 配置文件存在，检查是否已配置
    if grep -q "$REGISTRY" "$DAEMON_JSON"; then
      echo "✅ Registry $REGISTRY 已在信任列表中"
    else
      echo "⚠️  需要添加 Registry 到信任列表"
      echo ""
      echo "当前配置:"
      cat "$DAEMON_JSON"
      echo ""
      echo "请手动编辑 $DAEMON_JSON，添加以下内容："
      echo '{'
      echo '  "insecure-registries": ["'$REGISTRY'"]'
      echo '}'
      echo ""
      echo "或运行以下命令（需要 sudo）:"
      echo "  sudo nano $DAEMON_JSON"
      echo ""
      echo "然后重启 Docker:"
      echo "  sudo systemctl restart docker"
      echo ""
      read -p "是否现在编辑? (y/N): " confirm
      if [[ "$confirm" =~ ^[Yy]$ ]]; then
        sudo nano "$DAEMON_JSON"
        sudo systemctl restart docker
        echo "✅ Docker 已重启"
      fi
    fi
  else
    # 配置文件不存在，创建新的
    echo "创建新的 Docker daemon 配置..."

    # 确保目录存在
    sudo mkdir -p /etc/docker

    # 创建配置文件
    cat > /tmp/daemon.json <<EOF
{
  "insecure-registries": ["$REGISTRY"]
}
EOF

    sudo mv /tmp/daemon.json "$DAEMON_JSON"
    sudo chmod 644 "$DAEMON_JSON"

    echo "✅ 已创建 $DAEMON_JSON"
    cat "$DAEMON_JSON"
    echo ""

    # 重启 Docker
    echo "==> 重启 Docker 服务..."
    if command -v systemctl &> /dev/null; then
      sudo systemctl restart docker
    else
      sudo service docker restart
    fi

    echo "✅ Docker 已重启"
  fi
fi

echo ""
echo "==========================================="
echo "  验证配置"
echo "==========================================="
echo ""

# 测试连接
echo "测试 Registry 连接: $REGISTRY"
if curl -s "http://$REGISTRY/v2/" > /dev/null 2>&1; then
  echo "✅ Registry 连接成功"
else
  echo "⚠️  无法连接到 Registry"
  echo "   请确保:"
  echo "   1. Registry 正在运行"
  echo "   2. 网络连接正常"
  echo "   3. 防火墙未阻止连接"
fi

echo ""
echo "配置完成！现在可以从 $REGISTRY 拉取镜像了。"
echo ""
