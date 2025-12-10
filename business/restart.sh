#!/bin/bash
# Business Database Restart Script
# 业务库重启脚本
#
# 功能: 检测架构、清理旧镜像、重启服务
# 特性: 幂等执行

set -e

# 获取脚本所在目录并切换
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  Business Database Restart${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""

# 1. 检测架构
ARCH=$(uname -m)
echo -e "${YELLOW}🔍 检测 CPU 架构: ${ARCH}${NC}"

case "${ARCH}" in
    aarch64|arm64)
        export POSTGRES_IMAGE="imresamu/postgis-arm64:15-3.4"
        echo -e "${GREEN}✓ ARM64，使用: ${POSTGRES_IMAGE}${NC}"
        ;;
    x86_64)
        export POSTGRES_IMAGE="postgis/postgis:15-3.4"
        echo -e "${GREEN}✓ AMD64，使用: ${POSTGRES_IMAGE}${NC}"
        ;;
    *)
        echo -e "${RED}✗ 不支持的架构: ${ARCH}${NC}"
        exit 1
        ;;
esac
echo ""

# 2. 停止服务
echo -e "${YELLOW}🛑 停止服务...${NC}"
./stop.sh
echo ""

# 3. 清理旧镜像（可选）
echo -e "${YELLOW}🧹 清理旧镜像...${NC}"
docker image prune -f >/dev/null 2>&1 || true
echo -e "${GREEN}✓ 清理完成${NC}"
echo ""

# 4. 启动服务
echo -e "${YELLOW}🚀 启动服务...${NC}"
./start.sh
