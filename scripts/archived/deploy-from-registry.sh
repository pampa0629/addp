#!/bin/bash
# ADDP 服务器端部署脚本
# 从本地私有 Registry 拉取镜像并部署到服务器
#
# 使用方法:
#   1. 传输到服务器: scp scripts/deploy-from-registry.sh user@server:/opt/addp/scripts/
#   2. 在服务器上执行: REGISTRY=192.168.1.100:5000 ./scripts/deploy-from-registry.sh

set -e

# 配置参数
REGISTRY="${REGISTRY:-localhost:5000}"
DEPLOY_DIR="/opt/addp"
COMPOSE_FILE="docker-compose.prod.yml"

echo "=========================================="
echo "  ADDP 服务器端部署脚本"
echo "=========================================="
echo ""
echo "配置信息："
echo "  - Registry: $REGISTRY"
echo "  - 部署目录: $DEPLOY_DIR"
echo "  - Compose 文件: $COMPOSE_FILE"
echo ""

# 检查是否以 root 权限运行（可选，但推荐）
if [[ $EUID -ne 0 ]]; then
   echo "⚠️  警告: 建议使用 sudo 运行此脚本"
   echo ""
fi

# 步骤 1: 检查 Docker 安装
echo "=========================================="
echo "  步骤 1/6: 检查环境"
echo "=========================================="
echo ""

echo "==> 检查 Docker 安装..."
if ! command -v docker &> /dev/null; then
    echo "❌ Docker 未安装"
    echo ""
    echo "请先安装 Docker:"
    echo "  curl -fsSL https://get.docker.com | sh"
    echo "  sudo usermod -aG docker \$USER"
    exit 1
fi
echo "✅ Docker 版本: $(docker --version)"

echo "==> 检查 Docker Compose 安装..."
if ! command -v docker-compose &> /dev/null; then
    echo "❌ Docker Compose 未安装"
    echo ""
    echo "请先安装 Docker Compose:"
    echo "  sudo curl -L \"https://github.com/docker/compose/releases/latest/download/docker-compose-\$(uname -s)-\$(uname -m)\" -o /usr/local/bin/docker-compose"
    echo "  sudo chmod +x /usr/local/bin/docker-compose"
    exit 1
fi
echo "✅ Docker Compose 版本: $(docker-compose --version)"
echo ""

# 步骤 2: 配置 Docker 信任私有 Registry
echo "=========================================="
echo "  步骤 2/6: 配置 Docker Registry"
echo "=========================================="
echo ""

DAEMON_JSON="/etc/docker/daemon.json"
echo "==> 检查 Docker daemon 配置..."

if [ -f "$DAEMON_JSON" ]; then
    if grep -q "$REGISTRY" "$DAEMON_JSON"; then
        echo "✅ Registry $REGISTRY 已在信任列表中"
    else
        echo "⚠️  需要添加 Registry 到信任列表"
        echo ""
        echo "请手动编辑 $DAEMON_JSON，添加以下内容："
        echo '{'
        echo '  "insecure-registries": ["'$REGISTRY'"]'
        echo '}'
        echo ""
        echo "然后重启 Docker: sudo systemctl restart docker"
        read -p "是否已完成配置? (y/N): " confirm
        if [[ ! "$confirm" =~ ^[Yy]$ ]]; then
            exit 1
        fi
    fi
else
    echo "⚠️  Docker daemon 配置文件不存在"
    echo ""
    echo "创建配置文件："

    # 确保 /etc/docker 目录存在
    sudo mkdir -p /etc/docker

    cat > /tmp/daemon.json <<EOF
{
  "insecure-registries": ["$REGISTRY"]
}
EOF
    sudo mv /tmp/daemon.json "$DAEMON_JSON"
    echo "✅ 已创建 $DAEMON_JSON"

    echo "==> 重启 Docker 服务..."
    # 检测操作系统并使用对应的重启命令
    if [[ "$OSTYPE" == "darwin"* ]]; then
        # macOS - 重启 Docker Desktop
        echo "macOS 检测到，请手动重启 Docker Desktop"
        echo "1. 点击菜单栏 Docker 图标"
        echo "2. 选择 'Restart'"
        read -p "按任意键继续..." -n1 -s
        echo ""
    elif command -v systemctl &> /dev/null; then
        # Linux with systemd
        sudo systemctl restart docker
    else
        # Other Linux
        sudo service docker restart
    fi
    sleep 3
fi
echo ""

# 步骤 3: 测试 Registry 连接
echo "=========================================="
echo "  步骤 3/6: 测试 Registry 连接"
echo "=========================================="
echo ""

echo "==> 测试连接到 $REGISTRY..."
if curl -sf "http://$REGISTRY/v2/" > /dev/null; then
    echo "✅ Registry 连接成功"

    # 显示可用镜像
    echo ""
    echo "==> 查询可用镜像..."
    IMAGES=$(curl -s "http://$REGISTRY/v2/_catalog" | grep -o '"repositories":\[.*\]')
    echo "可用镜像:"
    echo "$IMAGES" | sed 's/,/\n/g' | sed 's/.*"\(addp-[^"]*\)".*/  - \1/g'
else
    echo "❌ 无法连接到 Registry: $REGISTRY"
    echo ""
    echo "请检查："
    echo "  1. 开发机 Registry 是否运行: docker ps | grep addp-registry"
    echo "  2. 开发机 IP 是否正确: $REGISTRY"
    echo "  3. 服务器是否在同一局域网"
    echo "  4. 开发机防火墙是否开放 5000 端口"
    echo ""
    echo "测试命令: curl http://$REGISTRY/v2/"
    exit 1
fi
echo ""

# 步骤 4: 准备部署目录
echo "=========================================="
echo "  步骤 4/6: 准备部署环境"
echo "=========================================="
echo ""

echo "==> 创建部署目录..."
# 检测操作系统
if [[ "$OSTYPE" == "darwin"* ]]; then
  # macOS
  mkdir -p "$DEPLOY_DIR" 2>/dev/null || sudo mkdir -p "$DEPLOY_DIR"
  sudo chown -R $USER "$DEPLOY_DIR" 2>/dev/null || true
else
  # Linux
  sudo mkdir -p "$DEPLOY_DIR"
  sudo chown -R $USER:$USER "$DEPLOY_DIR"
fi
cd "$DEPLOY_DIR"

# 检查必要文件
if [ ! -f "$COMPOSE_FILE" ]; then
    echo "❌ 错误: 未找到 $COMPOSE_FILE"
    echo ""
    echo "请先传输部署文件到服务器:"
    echo "  scp docker-compose.prod.yml user@server:$DEPLOY_DIR/"
    echo "  scp .env.example user@server:$DEPLOY_DIR/"
    echo "  scp -r scripts user@server:$DEPLOY_DIR/"
    exit 1
fi
echo "✅ 找到 $COMPOSE_FILE"

# 配置环境变量
if [ ! -f ".env" ]; then
    echo "==> 创建 .env 配置文件..."
    if [ -f ".env.example" ]; then
        cp .env.example .env
        echo "✅ 已从 .env.example 创建 .env"
        echo ""
        echo "⚠️  重要: 请编辑 .env 文件，修改以下配置:"
        echo "  - JWT_SECRET (生产环境密钥)"
        echo "  - POSTGRES_PASSWORD"
        echo "  - REDIS_PASSWORD"
        echo "  - MINIO_SYSTEM_ROOT_PASSWORD"
        echo "  - ENCRYPTION_KEY"
        echo ""
        read -p "是否现在编辑? (y/N): " edit_env
        if [[ "$edit_env" =~ ^[Yy]$ ]]; then
            ${EDITOR:-vim} .env
        fi
    else
        echo "❌ 错误: 未找到 .env.example"
        exit 1
    fi
else
    echo "✅ 已存在 .env 配置文件"
fi
echo ""

# 步骤 5: 拉取镜像
echo "=========================================="
echo "  步骤 5/6: 拉取镜像"
echo "=========================================="
echo ""

echo "==> 设置 Registry 环境变量..."
export REGISTRY="$REGISTRY"
export IMAGE_TAG="${IMAGE_TAG:-latest}"

echo "==> 开始拉取所有镜像..."
echo "这可能需要几分钟时间，请耐心等待..."
echo ""

if docker-compose -f "$COMPOSE_FILE" pull; then
    echo ""
    echo "✅ 所有镜像拉取成功"
else
    echo ""
    echo "❌ 镜像拉取失败"
    echo ""
    echo "故障排查:"
    echo "  1. 检查 Registry 连接: curl http://$REGISTRY/v2/"
    echo "  2. 检查镜像是否存在: curl http://$REGISTRY/v2/_catalog"
    echo "  3. 查看详细错误: docker-compose -f $COMPOSE_FILE pull --verbose"
    exit 1
fi
echo ""

# 步骤 6: 启动服务
echo "=========================================="
echo "  步骤 6/6: 启动服务"
echo "=========================================="
echo ""

echo "==> 停止旧服务（如果存在）..."
docker-compose -f "$COMPOSE_FILE" --profile full down 2>/dev/null || true

echo "==> 启动所有服务..."
if docker-compose -f "$COMPOSE_FILE" --profile full up -d; then
    echo "✅ 服务启动成功"
else
    echo "❌ 服务启动失败"
    echo ""
    echo "查看日志: docker-compose -f $COMPOSE_FILE logs"
    exit 1
fi
echo ""

# 等待服务启动
echo "==> 等待服务启动..."
sleep 10

# 检查服务状态
echo "==> 检查服务状态..."
docker-compose -f "$COMPOSE_FILE" ps
echo ""

# 健康检查
echo "=========================================="
echo "  健康检查"
echo "=========================================="
echo ""

SERVICES=("system-backend:8080" "gateway:8000")
ALL_HEALTHY=true

for service_port in "${SERVICES[@]}"; do
    service="${service_port%%:*}"
    port="${service_port##*:}"

    echo -n "检查 $service... "
    if curl -sf "http://localhost:$port/health" > /dev/null; then
        echo "✅ 健康"
    else
        echo "❌ 不健康"
        ALL_HEALTHY=false
    fi
done
echo ""

# 部署完成
echo "=========================================="
echo "  ✅ 部署完成！"
echo "=========================================="
echo ""
echo "服务访问地址："
echo "  - Portal (统一门户): http://$(hostname -I | awk '{print $1}'):5170"
echo "  - Gateway (API): http://$(hostname -I | awk '{print $1}'):8000"
echo "  - System Backend: http://$(hostname -I | awk '{print $1}'):8080"
echo "  - Manager Backend: http://$(hostname -I | awk '{print $1}'):8081"
echo "  - Meta Backend: http://$(hostname -I | awk '{print $1}'):8082"
echo ""
echo "管理命令："
echo "  - 查看状态: docker-compose -f $COMPOSE_FILE ps"
echo "  - 查看日志: docker-compose -f $COMPOSE_FILE logs -f"
echo "  - 停止服务: docker-compose -f $COMPOSE_FILE --profile full down"
echo "  - 重启服务: docker-compose -f $COMPOSE_FILE --profile full restart"
echo ""

if [ "$ALL_HEALTHY" = false ]; then
    echo "⚠️  警告: 部分服务健康检查失败"
    echo "请查看日志排查问题: docker-compose -f $COMPOSE_FILE logs"
    echo ""
fi

echo "数据持久化："
echo "  - PostgreSQL: docker volume inspect addp_postgres_data"
echo "  - Redis: docker volume inspect addp_redis_data"
echo "  - MinIO: docker volume inspect addp_minio_system_data"
echo ""
echo "=========================================="
