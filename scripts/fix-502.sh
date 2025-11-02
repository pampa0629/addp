#!/bin/bash
# =============================================================================
# ADDP 快速修复脚本 - 502 错误和页面空白
# =============================================================================
# 使用方法:
# 1. 将此文件传输到服务器: scp fix-502.sh pampa@192.168.1.182:~/
# 2. SSH 到服务器并运行: bash ~/fix-502.sh
# =============================================================================

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

DEPLOY_DIR="$HOME/addp"
COMPOSE_FILE="$DEPLOY_DIR/docker-compose.prod.yml"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}ADDP 502 错误快速修复${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# 检查部署目录
if [ ! -d "$DEPLOY_DIR" ]; then
    echo -e "${RED}错误: 部署目录不存在 ($DEPLOY_DIR)${NC}"
    exit 1
fi

cd "$DEPLOY_DIR"

if [ ! -f "$COMPOSE_FILE" ]; then
    echo -e "${RED}错误: docker-compose.prod.yml 不存在${NC}"
    exit 1
fi

# Step 1: 检查当前状态
echo -e "${YELLOW}Step 1: 检查服务状态...${NC}"
docker compose -f docker-compose.prod.yml ps
echo ""

# Step 2: 检查不健康的服务
echo -e "${YELLOW}Step 2: 检查不健康服务...${NC}"
UNHEALTHY=$(docker compose -f docker-compose.prod.yml ps --format json | jq -r 'select(.Health == "unhealthy") | .Service' 2>/dev/null || true)
if [ -n "$UNHEALTHY" ]; then
    echo -e "${RED}发现不健康的服务:${NC}"
    echo "$UNHEALTHY"
else
    echo -e "${GREEN}所有服务健康状态正常${NC}"
fi
echo ""

# Step 3: 重启核心服务
echo -e "${YELLOW}Step 3: 重启核心服务 (Nginx + Gateway + Portal)...${NC}"
docker compose -f docker-compose.prod.yml restart nginx gateway portal
echo -e "${GREEN}✓ 核心服务已重启${NC}"
echo ""

# Step 4: 等待服务启动
echo -e "${YELLOW}Step 4: 等待服务启动 (30秒)...${NC}"
sleep 30
echo ""

# Step 5: 测试连接
echo -e "${YELLOW}Step 5: 测试服务连接...${NC}"

# Test Nginx
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8000/ || echo "000")
echo "Nginx (localhost:8000): HTTP $HTTP_CODE"
if [ "$HTTP_CODE" = "200" ] || [ "$HTTP_CODE" = "302" ]; then
    echo -e "${GREEN}✓ Nginx 正常${NC}"
else
    echo -e "${RED}✗ Nginx 返回 $HTTP_CODE${NC}"
fi

# Test Gateway health
GATEWAY_HEALTH=$(docker compose -f docker-compose.prod.yml exec -T gateway wget -qO- http://localhost:8000/health 2>&1 || echo "failed")
if [ "$GATEWAY_HEALTH" = "healthy" ]; then
    echo -e "${GREEN}✓ Gateway 健康检查通过${NC}"
else
    echo -e "${RED}✗ Gateway 健康检查失败${NC}"
fi

# Test Portal
PORTAL_STATUS=$(docker compose -f docker-compose.prod.yml ps portal --format json | jq -r '.State' 2>/dev/null || echo "unknown")
echo "Portal 状态: $PORTAL_STATUS"

echo ""

# Step 6: 显示最近错误日志
echo -e "${YELLOW}Step 6: 检查最近错误日志...${NC}"
echo ""
echo "=== Nginx 错误日志 (最近10行) ==="
docker compose -f docker-compose.prod.yml logs --tail=10 nginx | grep -i error || echo "无错误日志"
echo ""
echo "=== Gateway 错误日志 (最近10行) ==="
docker compose -f docker-compose.prod.yml logs --tail=10 gateway | grep -i error || echo "无错误日志"
echo ""

# Step 7: 给出建议
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}修复完成${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

if [ "$HTTP_CODE" = "200" ] || [ "$HTTP_CODE" = "302" ]; then
    echo -e "${GREEN}✓ 服务已恢复正常!${NC}"
    echo ""
    echo "请在浏览器中访问: http://192.168.1.182:8000/"
    echo ""
else
    echo -e "${RED}问题仍然存在,建议:${NC}"
    echo ""
    echo "1. 查看完整日志:"
    echo "   docker compose -f docker-compose.prod.yml logs -f | grep -E '(error|Error|ERROR)'"
    echo ""
    echo "2. 完全重启所有服务:"
    echo "   docker compose -f docker-compose.prod.yml down"
    echo "   docker compose -f docker-compose.prod.yml up -d"
    echo ""
    echo "3. 检查服务状态:"
    echo "   docker compose -f docker-compose.prod.yml ps"
    echo ""
fi
