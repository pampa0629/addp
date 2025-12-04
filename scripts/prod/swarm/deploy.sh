#!/bin/bash
# Docker Swarm 部署脚本
# 部署 ADDP 平台到 Swarm 集群

set -e

echo "=================================================="
echo "  ADDP Docker Swarm 部署"
echo "=================================================="
echo ""

# 检查是否已初始化 Swarm
if ! docker info 2>/dev/null | grep -q "Swarm: active"; then
    echo "❌ Swarm 未初始化，请先运行:"
    echo "    ./scripts/prod/swarm/init.sh"
    exit 1
fi

# 切换到项目根目录
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
cd "$PROJECT_ROOT"

echo "📁 项目目录: $PROJECT_ROOT"
echo ""

# 检查配置文件
if [ ! -f ".env" ]; then
    echo "⚠️  .env 文件不存在，从 .env.example 复制..."
    cp .env.example .env
    echo "⚠️  请编辑 .env 文件设置密钥和密码后重新运行此脚本"
    exit 1
fi

if [ ! -f "docker-compose.prod.yml" ]; then
    echo "❌ docker-compose.prod.yml 不存在"
    exit 1
fi

echo "✅ 配置文件检查通过"
echo ""

# 询问部署模式
echo "选择部署模式:"
echo "  1) Full - 完整平台（System + Manager + Meta + Transfer）"
echo "  2) System Only - 仅 System 模块"
read -p "请选择 [1/2]: " -n 1 -r DEPLOY_MODE
echo ""
echo ""

# 显示即将部署的服务
echo "准备部署以下服务:"
echo ""
if [[ $DEPLOY_MODE =~ ^[2]$ ]]; then
    echo "  - postgres (PostgreSQL)"
    echo "  - redis (Redis)"
    echo "  - minio (MinIO)"
    echo "  - system-backend"
    echo "  - system-frontend"
else
    echo "  - postgres (PostgreSQL)"
    echo "  - redis (Redis)"
    echo "  - minio (MinIO)"
    echo "  - system-backend"
    echo "  - system-frontend"
    echo "  - manager-backend"
    echo "  - manager-frontend"
    echo "  - meta-backend"
    echo "  - meta-frontend"
    echo "  - transfer-backend"
    echo "  - transfer-worker (高可用: 2 副本)"
    echo "  - transfer-frontend"
    echo "  - gateway"
fi
echo ""

read -p "确认部署？[Y/n]: " -n 1 -r
echo ""
if [[ $REPLY =~ ^[Nn]$ ]]; then
    echo "取消部署"
    exit 0
fi

echo ""
echo "🚀 开始部署..."
echo ""

# 部署
if [[ $DEPLOY_MODE =~ ^[2]$ ]]; then
    # System Only
    export COMPOSE_PROFILES=""
    docker stack deploy -c docker-compose.prod.yml addp
else
    # Full
    export COMPOSE_PROFILES="full"
    COMPOSE_PROFILES=full docker stack deploy -c docker-compose.prod.yml addp
fi

echo ""
echo "⏳ 等待服务启动..."
sleep 10

echo ""
echo "📊 服务状态:"
docker service ls

echo ""
echo "=================================================="
echo "  部署完成！"
echo "=================================================="
echo ""
echo "查看服务详情:"
echo "  docker service ls"
echo "  docker service ps addp_transfer-worker"
echo ""
echo "查看日志:"
echo "  docker service logs -f addp_transfer-worker"
echo "  docker service logs -f addp_system-backend"
echo ""
echo "扩展 Worker:"
echo "  docker service scale addp_transfer-worker=3"
echo ""
echo "更新服务:"
echo "  docker service update --image addp-transfer-worker:v2.0 addp_transfer-worker"
echo ""
echo "停止服务:"
echo "  docker stack rm addp"
echo ""
echo "详细文档: docs/DOCKER_SWARM.md"
echo ""
