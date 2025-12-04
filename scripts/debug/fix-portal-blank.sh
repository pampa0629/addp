#!/bin/bash
# =============================================================================
# 快速修复 Portal 页面空白问题 - 重新构建并部署 Portal
# =============================================================================

set -e

REGISTRY="localhost:5001"
SERVER="$1"

if [ -z "$SERVER" ]; then
    echo "Usage: $0 SERVER_USER@IP"
    echo "Example: $0 pampa@192.168.1.182"
    exit 1
fi

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}Portal 页面空白问题快速修复${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""
echo -e "问题: Portal 硬编码了 localhost URL"
echo -e "修复: 已修改为使用相对路径 (/system, /manager, etc.)"
echo -e "目标服务器: ${GREEN}${SERVER}${NC}"
echo ""

# Step 1: 重新构建 Portal (AMD64 + ARM64)
echo -e "${YELLOW}Step 1/5: 重新构建 Portal 镜像...${NC}"

echo "Building Portal for AMD64..."
docker build --platform linux/amd64 \
    -t ${REGISTRY}/addp-portal:latest-amd64 \
    -f portal/frontend/Dockerfile \
    portal/frontend

echo "Building Portal for ARM64..."
docker build --platform linux/arm64 \
    -t ${REGISTRY}/addp-portal:latest-arm64 \
    -f portal/frontend/Dockerfile \
    portal/frontend

echo -e "${GREEN}✓ Portal 镜像构建完成${NC}"
echo ""

# Step 2: 推送镜像到本地仓库
echo -e "${YELLOW}Step 2/5: 推送镜像到本地仓库...${NC}"
docker push ${REGISTRY}/addp-portal:latest-amd64
docker push ${REGISTRY}/addp-portal:latest-arm64
echo -e "${GREEN}✓ 镜像推送完成${NC}"
echo ""

# Step 3: 创建 multi-arch manifest
echo -e "${YELLOW}Step 3/5: 创建 multi-arch manifest...${NC}"
docker buildx imagetools create \
    --tag ${REGISTRY}/addp-portal:latest \
    ${REGISTRY}/addp-portal:latest-amd64 \
    ${REGISTRY}/addp-portal:latest-arm64
echo -e "${GREEN}✓ Manifest 创建完成${NC}"
echo ""

# Step 4: SSH 到服务器并重新拉取 Portal 镜像
echo -e "${YELLOW}Step 4/5: 在服务器上更新 Portal...${NC}"
ssh "$SERVER" << 'REMOTE_SCRIPT'
#!/bin/bash
cd ~/addp

echo "停止 Portal 容器..."
docker compose -f docker-compose.prod.yml stop portal

echo "删除旧的 Portal 容器..."
docker compose -f docker-compose.prod.yml rm -f portal

echo "拉取新的 Portal 镜像..."
docker compose -f docker-compose.prod.yml pull portal

echo "启动新的 Portal 容器..."
docker compose -f docker-compose.prod.yml up -d portal

echo "等待 Portal 启动..."
sleep 5

echo "检查 Portal 状态..."
docker compose -f docker-compose.prod.yml ps portal
REMOTE_SCRIPT

echo -e "${GREEN}✓ 服务器上的 Portal 已更新${NC}"
echo ""

# Step 5: 验证
echo -e "${YELLOW}Step 5/5: 验证修复...${NC}"
HOST="${SERVER#*@}"
echo "请在浏览器中访问: http://${HOST}:8000/"
echo "登录后点击任意模块,应该能正常显示内容"
echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}修复完成!${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo -e "如果问题依然存在,请检查:"
echo "  1. 浏览器控制台 (F12) 是否有错误"
echo "  2. Portal iframe 的 src 是否为 /system, /manager 等相对路径"
echo "  3. Nginx 日志: ssh $SERVER 'cd ~/addp && docker compose -f docker-compose.prod.yml logs nginx'"
