#!/bin/bash
# Docker Swarm 初始化脚本
# 用于首次设置 Swarm 集群

set -e

echo "=================================================="
echo "  ADDP Docker Swarm 初始化"
echo "=================================================="
echo ""

# 检查 Docker 是否已安装
if ! command -v docker &> /dev/null; then
    echo "❌ Docker 未安装，请先安装 Docker Engine 19.03+"
    exit 1
fi

# 检查 Docker 版本
DOCKER_VERSION=$(docker version --format '{{.Server.Version}}' 2>/dev/null || echo "0.0.0")
echo "✅ Docker 版本: $DOCKER_VERSION"
echo ""

# 检查是否已经初始化 Swarm
if docker info 2>/dev/null | grep -q "Swarm: active"; then
    echo "⚠️  Swarm 已经初始化"
    echo ""
    echo "当前节点信息:"
    docker node ls
    echo ""
    read -p "是否要重新初始化 Swarm？(这将删除所有现有服务) [y/N]: " -n 1 -r
    echo ""
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "跳过初始化，退出"
        exit 0
    fi

    echo "⚠️  停止所有 Stack..."
    docker stack ls --format '{{.Name}}' | xargs -r -n1 docker stack rm || true
    sleep 5

    echo "⚠️  离开 Swarm..."
    docker swarm leave --force
    sleep 2
fi

# 获取本机 IP（用于多机集群）
echo "检测本机 IP 地址..."
if [[ "$OSTYPE" == "darwin"* ]]; then
    # macOS
    DEFAULT_IP=$(ifconfig | grep "inet " | grep -v 127.0.0.1 | awk '{print $2}' | head -1)
else
    # Linux
    DEFAULT_IP=$(hostname -I | awk '{print $1}')
fi

echo "检测到的 IP 地址: $DEFAULT_IP"
read -p "使用此 IP 初始化 Swarm 吗？[Y/n]: " -n 1 -r
echo ""

if [[ $REPLY =~ ^[Nn]$ ]]; then
    read -p "请输入 IP 地址: " ADVERTISE_IP
else
    ADVERTISE_IP=$DEFAULT_IP
fi

# 初始化 Swarm
echo ""
echo "🚀 初始化 Docker Swarm..."
if [ -n "$ADVERTISE_IP" ]; then
    docker swarm init --advertise-addr "$ADVERTISE_IP"
else
    docker swarm init
fi

echo ""
echo "✅ Swarm 初始化成功！"
echo ""

# 显示节点信息
echo "当前节点信息:"
docker node ls
echo ""

# 显示加入命令
echo "=================================================="
echo "  如需添加 Worker 节点，在其他机器上执行:"
echo "=================================================="
docker swarm join-token worker | grep "docker swarm join"
echo ""

# 询问是否立即部署
read -p "是否立即部署 ADDP 服务？[y/N]: " -n 1 -r
echo ""
if [[ $REPLY =~ ^[Yy]$ ]]; then
    SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
    if [ -f "$SCRIPT_DIR/swarm-deploy.sh" ]; then
        bash "$SCRIPT_DIR/swarm-deploy.sh"
    else
        echo "⚠️  未找到 swarm-deploy.sh，请手动部署:"
        echo "    docker stack deploy -c docker-compose.prod.yml addp"
    fi
else
    echo ""
    echo "初始化完成！稍后可以运行以下命令部署服务:"
    echo "    ./scripts/swarm-deploy.sh"
    echo "    或"
    echo "    docker stack deploy -c docker-compose.prod.yml addp"
fi

echo ""
echo "✅ 完成！"
