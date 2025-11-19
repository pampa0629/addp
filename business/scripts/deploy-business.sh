#!/bin/bash
# Business Infrastructure - 服务器端部署脚本
# 用于在生产服务器上部署 Business 基础设施（PostgreSQL + MinIO）
#
# 使用方法:
#   1. 传输到服务器: scp business/scripts/deploy-business.sh user@server:/opt/
#   2. 在服务器上执行: ./deploy-business.sh

set -e

# 配置参数
DEPLOY_DIR="${DEPLOY_DIR:-/opt/addp-business}"
COMPOSE_FILE="docker-compose.prod.yml"

echo "=========================================="
echo "  Business 基础设施部署脚本"
echo "=========================================="
echo ""
echo "配置信息："
echo "  - 部署目录: $DEPLOY_DIR"
echo "  - Compose 文件: $COMPOSE_FILE"
echo ""

# 步骤 1: 检查环境
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

# 步骤 2: 准备部署目录
echo "=========================================="
echo "  步骤 2/6: 准备部署目录"
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
echo "✅ 部署目录: $DEPLOY_DIR"

# 检查必要文件
if [ ! -f "$COMPOSE_FILE" ]; then
    echo "❌ 错误: 未找到 $COMPOSE_FILE"
    echo ""
    echo "请先传输部署文件到服务器:"
    echo "  方法一: 使用部署包"
    echo "    scp business-deploy-*.tar.gz user@server:$DEPLOY_DIR/"
    echo "    cd $DEPLOY_DIR && tar -xzf business-deploy-*.tar.gz"
    echo ""
    echo "  方法二: 直接传输文件"
    echo "    scp business/docker-compose.prod.yml user@server:$DEPLOY_DIR/"
    echo "    scp business/.env.prod.example user@server:$DEPLOY_DIR/.env.example"
    echo "    scp business/init-db.sql user@server:$DEPLOY_DIR/"
    exit 1
fi
echo "✅ 找到 $COMPOSE_FILE"
echo ""

# 步骤 3: 配置环境变量
echo "=========================================="
echo "  步骤 3/6: 配置环境变量"
echo "=========================================="
echo ""

if [ ! -f ".env" ]; then
    echo "==> 创建 .env 配置文件..."
    if [ -f ".env.example" ]; then
        cp .env.example .env
        echo "✅ 已从 .env.example 创建 .env"
    elif [ -f ".env.prod.example" ]; then
        cp .env.prod.example .env
        echo "✅ 已从 .env.prod.example 创建 .env"
    else
        echo "❌ 错误: 未找到环境变量模板文件"
        exit 1
    fi

    echo ""
    echo "⚠️  重要: 必须修改 .env 文件中的密码！"
    echo ""
    echo "需要修改的配置项："
    echo "  - BUSINESS_POSTGRES_PASSWORD"
    echo "  - BUSINESS_MINIO_ROOT_PASSWORD"
    echo ""
    echo "生成随机密码命令: openssl rand -base64 32"
    echo ""

    read -p "是否现在编辑 .env 文件? (Y/n): " edit_env
    if [[ ! "$edit_env" =~ ^[Nn]$ ]]; then
        ${EDITOR:-vim} .env
    else
        echo "⚠️  警告: 请稍后手动编辑 .env 文件！"
    fi
else
    echo "✅ 已存在 .env 配置文件"

    # 检查是否使用了默认密码
    if grep -q "CHANGE_THIS_PASSWORD_IN_PRODUCTION" .env 2>/dev/null; then
        echo ""
        echo "❌ 错误: .env 文件中仍包含默认密码！"
        echo "生产环境必须修改所有密码。"
        echo ""
        read -p "是否现在编辑? (Y/n): " edit_now
        if [[ ! "$edit_now" =~ ^[Nn]$ ]]; then
            ${EDITOR:-vim} .env
        else
            echo "部署已取消。请修改密码后重新运行。"
            exit 1
        fi
    fi
fi
echo ""

# 步骤 4: 检查端口冲突
echo "=========================================="
echo "  步骤 4/6: 检查端口冲突"
echo "=========================================="
echo ""

# 从 .env 文件读取端口配置
source .env

POSTGRES_PORT="${BUSINESS_POSTGRES_PORT:-5433}"
MINIO_API_PORT="${BUSINESS_MINIO_API_PORT:-9002}"
MINIO_CONSOLE_PORT="${BUSINESS_MINIO_CONSOLE_PORT:-9003}"

PORTS=("$POSTGRES_PORT" "$MINIO_API_PORT" "$MINIO_CONSOLE_PORT")
PORT_NAMES=("PostgreSQL" "MinIO API" "MinIO Console")

ALL_AVAILABLE=true
for i in "${!PORTS[@]}"; do
    port="${PORTS[$i]}"
    name="${PORT_NAMES[$i]}"

    echo -n "检查端口 $port ($name)... "
    if lsof -Pi :$port -sTCP:LISTEN -t >/dev/null 2>&1 || netstat -tuln 2>/dev/null | grep -q ":$port "; then
        echo "❌ 已被占用"
        ALL_AVAILABLE=false
    else
        echo "✅ 可用"
    fi
done

if [ "$ALL_AVAILABLE" = false ]; then
    echo ""
    echo "⚠️  警告: 部分端口已被占用"
    echo "请修改 .env 文件中的端口配置，或停止占用端口的服务"
    echo ""
    read -p "是否继续部署? (y/N): " continue_deploy
    if [[ ! "$continue_deploy" =~ ^[Yy]$ ]]; then
        echo "部署已取消"
        exit 1
    fi
fi
echo ""

# 步骤 5: 拉取镜像
echo "=========================================="
echo "  步骤 5/6: 拉取官方镜像"
echo "=========================================="
echo ""

# Detect CPU architecture
ARCH=$(uname -m)
echo "==> 检测 CPU 架构: ${ARCH}"

case "${ARCH}" in
    aarch64|arm64)
        POSTGRES_IMAGE="imresamu/postgis-arm64:15-3.4"
        DOCKER_PLATFORM="linux/arm64"
        DISPLAY_ARCH="ARM64"
        echo "✅ 使用 ARM64 镜像"
        echo "   PostgreSQL: ${POSTGRES_IMAGE}"
        echo "   Docker Platform: ${DOCKER_PLATFORM}"
        ;;
    x86_64)
        POSTGRES_IMAGE="postgis/postgis:15-3.4"
        DOCKER_PLATFORM="linux/amd64"
        DISPLAY_ARCH="AMD64"
        echo "✅ 使用 AMD64 镜像"
        echo "   PostgreSQL: ${POSTGRES_IMAGE}"
        echo "   Docker Platform: ${DOCKER_PLATFORM}"
        ;;
    *)
        echo "❌ 不支持的架构: ${ARCH}"
        echo "支持的架构: ARM64 (aarch64/arm64) 或 AMD64 (x86_64)"
        exit 1
        ;;
esac
echo ""

echo "==> 拉取 PostGIS 镜像（${DISPLAY_ARCH} 架构）..."
docker pull "${POSTGRES_IMAGE}"
echo ""

echo "==> 拉取 MinIO 镜像（${DISPLAY_ARCH} 架构）..."
docker pull --platform="${DOCKER_PLATFORM}" minio/minio:latest
echo ""

echo "✅ 所有官方镜像拉取成功"
echo ""

# 步骤 6: 启动服务
echo "=========================================="
echo "  步骤 6/6: 启动服务"
echo "=========================================="
echo ""

echo "==> 停止旧服务（如果存在）..."
docker-compose -f "$COMPOSE_FILE" down 2>/dev/null || true

echo "==> 启动 Business 基础设施..."
# Export POSTGRES_IMAGE and DOCKER_PLATFORM for docker-compose
export POSTGRES_IMAGE
export DOCKER_PLATFORM
if docker-compose -f "$COMPOSE_FILE" up -d; then
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

# PostgreSQL 健康检查
echo -n "PostgreSQL... "
MAX_RETRIES=30
RETRY_COUNT=0
while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
    if docker exec business-postgres pg_isready -U ${BUSINESS_POSTGRES_USER:-business} > /dev/null 2>&1; then
        echo "✅ 健康"
        PG_HEALTHY=true
        break
    fi
    RETRY_COUNT=$((RETRY_COUNT + 1))
    sleep 2
done

if [ "$PG_HEALTHY" != true ]; then
    echo "❌ 不健康（超时）"
fi

# MinIO 健康检查
echo -n "MinIO... "
RETRY_COUNT=0
while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
    if curl -sf "http://localhost:$MINIO_API_PORT/minio/health/live" > /dev/null 2>&1; then
        echo "✅ 健康"
        MINIO_HEALTHY=true
        break
    fi
    RETRY_COUNT=$((RETRY_COUNT + 1))
    sleep 2
done

if [ "$MINIO_HEALTHY" != true ]; then
    echo "❌ 不健康（超时）"
fi

# PostGIS 扩展检查
echo -n "PostGIS扩展... "
if docker exec business-postgres psql -U ${BUSINESS_POSTGRES_USER:-business} -d ${BUSINESS_POSTGRES_DB:-business} -t -c "SELECT COUNT(*) FROM pg_extension WHERE extname='postgis';" 2>/dev/null | grep -q "1"; then
    echo "✅ 已安装"
else
    echo "⚠️  未安装（将在首次使用时自动安装）"
fi

echo ""

# 获取服务器 IP
SERVER_IP=$(hostname -I | awk '{print $1}' || echo "localhost")

# 部署完成
echo "=========================================="
echo "  ✅ Business 基础设施部署完成！"
echo "=========================================="
echo ""
echo "服务访问地址："
echo "  - PostgreSQL: $SERVER_IP:$POSTGRES_PORT"
echo "    连接命令: psql -h $SERVER_IP -p $POSTGRES_PORT -U ${BUSINESS_POSTGRES_USER:-business} -d ${BUSINESS_POSTGRES_DB:-business}"
echo ""
echo "  - MinIO API: http://$SERVER_IP:$MINIO_API_PORT"
echo "  - MinIO Console: http://$SERVER_IP:$MINIO_CONSOLE_PORT"
echo "    用户名: ${BUSINESS_MINIO_ROOT_USER:-minioadmin}"
echo "    密码: (见 .env 文件)"
echo ""
echo "管理命令："
echo "  - 查看状态: docker-compose -f $COMPOSE_FILE ps"
echo "  - 查看日志: docker-compose -f $COMPOSE_FILE logs -f"
echo "  - 停止服务: docker-compose -f $COMPOSE_FILE down"
echo "  - 重启服务: docker-compose -f $COMPOSE_FILE restart"
echo ""
echo "数据持久化："
echo "  - PostgreSQL 数据: docker volume inspect business_postgres_data"
echo "  - MinIO 数据: docker volume inspect business_minio_data"
echo ""
echo "备份命令："
echo "  - 备份 PostgreSQL:"
echo "    docker exec business-postgres pg_dumpall -U ${BUSINESS_POSTGRES_USER:-business} > business-backup-\$(date +%Y%m%d).sql"
echo ""
echo "  - 备份 MinIO 数据卷:"
echo "    docker run --rm -v business_minio_data:/data -v \$(pwd):/backup alpine tar czf /backup/minio-backup-\$(date +%Y%m%d).tar.gz /data"
echo ""
echo "下一步："
echo "  ✅ Business 基础设施已就绪"
echo "  ➡️  现在可以部署 ADDP 系统"
echo ""
echo "=========================================="
