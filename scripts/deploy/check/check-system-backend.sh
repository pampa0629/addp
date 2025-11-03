#!/bin/bash
# =============================================================================
# System Backend 健康检查和修复脚本
# =============================================================================
# 在服务器上运行此脚本来诊断和修复 System Backend 问题
# 用法: bash ~/check-system-backend.sh
# =============================================================================

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

cd ~/addp

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}System Backend 健康检查${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# 1. 检查容器状态
echo -e "${YELLOW}[1/8] 检查 System Backend 容器状态...${NC}"
docker compose -f docker-compose.prod.yml ps system-backend
echo ""

# 2. 检查容器是否在运行
STATUS=$(docker compose -f docker-compose.prod.yml ps system-backend --format json 2>/dev/null | jq -r '.State' 2>/dev/null || echo "unknown")
echo "System Backend 状态: $STATUS"

if [ "$STATUS" != "running" ]; then
    echo -e "${RED}✗ System Backend 未运行!${NC}"
    echo "尝试启动..."
    docker compose -f docker-compose.prod.yml up -d system-backend
    sleep 10
fi
echo ""

# 3. 检查依赖服务
echo -e "${YELLOW}[2/8] 检查依赖服务 (PostgreSQL, Redis)...${NC}"
echo "PostgreSQL:"
docker compose -f docker-compose.prod.yml ps postgres
echo ""
echo "Redis:"
docker compose -f docker-compose.prod.yml ps redis
echo ""

# 4. 查看最近的日志
echo -e "${YELLOW}[3/8] System Backend 最近日志 (最后50行)...${NC}"
docker compose -f docker-compose.prod.yml logs --tail=50 system-backend
echo ""

# 5. 检查错误日志
echo -e "${YELLOW}[4/8] 查找错误日志...${NC}"
docker compose -f docker-compose.prod.yml logs system-backend | grep -i "error\|fatal\|panic" | tail -20 || echo "未发现明显错误"
echo ""

# 6. 测试内部健康检查
echo -e "${YELLOW}[5/8] 测试内部健康检查端点...${NC}"
HEALTH_RESPONSE=$(docker compose -f docker-compose.prod.yml exec -T system-backend wget -qO- http://localhost:8080/health 2>&1 || echo "failed")
echo "Health check response: $HEALTH_RESPONSE"
echo ""

# 7. 检查环境变量
echo -e "${YELLOW}[6/8] 检查环境变量配置...${NC}"
echo "检查 .env.prod 文件:"
if [ -f ".env.prod" ]; then
    echo "JWT_SECRET: $(grep JWT_SECRET .env.prod | grep -v '^#' || echo 'NOT SET')"
    echo "POSTGRES_PASSWORD: $(grep POSTGRES_PASSWORD .env.prod | grep -v '^#' || echo 'NOT SET')"
    echo "POSTGRES_USER: $(grep POSTGRES_USER .env.prod | grep -v '^#' || echo 'NOT SET')"
    echo "POSTGRES_DB: $(grep POSTGRES_DB .env.prod | grep -v '^#' || echo 'NOT SET')"
else
    echo -e "${RED}.env.prod 文件不存在!${NC}"
fi
echo ""

# 8. 检查网络连接
echo -e "${YELLOW}[7/8] 检查网络连接...${NC}"
echo "从 System Backend 到 PostgreSQL:"
docker compose -f docker-compose.prod.yml exec -T system-backend nc -zv postgres 5432 2>&1 || echo "无法连接到 PostgreSQL"
echo ""
echo "从 System Backend 到 Redis:"
docker compose -f docker-compose.prod.yml exec -T system-backend nc -zv redis 6379 2>&1 || echo "无法连接到 Redis"
echo ""

# 9. 尝试重启
echo -e "${YELLOW}[8/8] 建议的修复步骤${NC}"
echo ""
echo "如果 System Backend 有问题,请尝试以下步骤:"
echo ""
echo "1. 重启 System Backend:"
echo "   docker compose -f docker-compose.prod.yml restart system-backend"
echo ""
echo "2. 重启依赖服务:"
echo "   docker compose -f docker-compose.prod.yml restart postgres redis"
echo "   sleep 30"
echo "   docker compose -f docker-compose.prod.yml restart system-backend"
echo ""
echo "3. 查看实时日志:"
echo "   docker compose -f docker-compose.prod.yml logs -f system-backend"
echo ""
echo "4. 完全重启:"
echo "   docker compose -f docker-compose.prod.yml down"
echo "   docker compose -f docker-compose.prod.yml up -d"
echo ""

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}诊断完成${NC}"
echo -e "${BLUE}========================================${NC}"
