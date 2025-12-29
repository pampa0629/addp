#!/bin/bash
#
# 推送 ADDP PostgreSQL 镜像到 Docker Hub
# 用法: bash scripts/infra/push-postgres-image.sh
#
# 前置条件:
# 1. 已构建本地镜像 addp-postgres-pgvector:latest
# 2. 已登录 Docker Hub (docker login)

set -e

DOCKER_HUB_USER="pampa0629"
IMAGE_NAME="addp-postgres"
VERSION="15"

echo "=========================================="
echo "推送 ADDP PostgreSQL 镜像到 Docker Hub"
echo "=========================================="
echo ""

# 检查本地镜像是否存在
if ! docker image inspect addp-postgres-pgvector:latest > /dev/null 2>&1; then
    echo "❌ 错误: 本地镜像 addp-postgres-pgvector:latest 不存在"
    echo "请先运行: docker build -f scripts/infra/Dockerfile.postgres -t addp-postgres-pgvector:latest scripts/infra/"
    exit 1
fi

echo "✅ 本地镜像已找到: addp-postgres-pgvector:latest"
echo ""

# 检测架构
ARCH=$(uname -m)
if [ "$ARCH" = "x86_64" ]; then
    PLATFORM="amd64"
elif [ "$ARCH" = "aarch64" ] || [ "$ARCH" = "arm64" ]; then
    PLATFORM="arm64"
else
    echo "❌ 不支持的架构: $ARCH"
    exit 1
fi

echo "🔍 检测到系统架构: $ARCH -> $PLATFORM"
echo ""

# 打标签
echo "📦 打标签..."
docker tag addp-postgres-pgvector:latest ${DOCKER_HUB_USER}/${IMAGE_NAME}:${VERSION}-${PLATFORM}
docker tag addp-postgres-pgvector:latest ${DOCKER_HUB_USER}/${IMAGE_NAME}:latest-${PLATFORM}
echo "  - ${DOCKER_HUB_USER}/${IMAGE_NAME}:${VERSION}-${PLATFORM}"
echo "  - ${DOCKER_HUB_USER}/${IMAGE_NAME}:latest-${PLATFORM}"
echo ""

# 推送镜像函数（带重试机制）
push_with_retry() {
    local image=$1
    local max_retries=5
    local retry_count=0

    while [ $retry_count -lt $max_retries ]; do
        echo "推送: $image (尝试 $((retry_count + 1))/$max_retries)"

        if docker push "$image"; then
            echo "✅ 推送成功: $image"
            return 0
        else
            retry_count=$((retry_count + 1))
            if [ $retry_count -lt $max_retries ]; then
                echo "⚠️  推送失败，等待 5 秒后重试..."
                sleep 5
            fi
        fi
    done

    echo "❌ 推送失败（已重试 $max_retries 次）: $image"
    echo ""
    echo "可能的原因:"
    echo "  1. 网络连接问题 - 请检查网络并稍后重试"
    echo "  2. 未登录 Docker Hub - 请运行: docker login"
    echo "  3. 没有权限 - 请确认你有访问 ${DOCKER_HUB_USER} 的权限"
    return 1
}

# 推送镜像
echo "⬆️  推送镜像到 Docker Hub (可能需要几分钟)..."
echo "注意: 网络不稳定时会自动重试（最多 5 次）"
echo ""

if ! push_with_retry ${DOCKER_HUB_USER}/${IMAGE_NAME}:${VERSION}-${PLATFORM}; then
    exit 1
fi
echo ""

if ! push_with_retry ${DOCKER_HUB_USER}/${IMAGE_NAME}:latest-${PLATFORM}; then
    exit 1
fi
echo ""

echo "=========================================="
echo "✅ 所有镜像推送成功!"
echo "=========================================="
echo ""
echo "Docker Hub 仓库: https://hub.docker.com/r/${DOCKER_HUB_USER}/${IMAGE_NAME}"
echo ""
echo "其他用户可以使用以下命令拉取:"
echo "  docker pull ${DOCKER_HUB_USER}/${IMAGE_NAME}:${VERSION}-${PLATFORM}"
echo "  docker pull ${DOCKER_HUB_USER}/${IMAGE_NAME}:latest-${PLATFORM}"
echo ""
