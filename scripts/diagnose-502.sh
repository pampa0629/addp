#!/bin/bash
# =============================================================================
# ADDP 502 错误诊断脚本
# =============================================================================
# 使用方法: 在服务器上运行此脚本
# ssh pampa@192.168.1.182
# cd ~/addp
# bash scripts/diagnose-502.sh
# =============================================================================

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}ADDP 502 错误诊断${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# 1. 检查所有服务状态
echo -e "${YELLOW}[1/7] 检查服务状态...${NC}"
docker compose -f docker-compose.prod.yml ps
echo ""

# 2. 检查哪些服务不健康
echo -e "${YELLOW}[2/7] 检查不健康的服务...${NC}"
docker compose -f docker-compose.prod.yml ps --filter "health=unhealthy"
echo ""

# 3. 检查 Nginx 日志
echo -e "${YELLOW}[3/7] 检查 Nginx 错误日志 (最近50行)...${NC}"
docker compose -f docker-compose.prod.yml logs --tail=50 nginx
echo ""

# 4. 检查 Gateway 日志
echo -e "${YELLOW}[4/7] 检查 Gateway 日志 (最近50行)...${NC}"
docker compose -f docker-compose.prod.yml logs --tail=50 gateway
echo ""

# 5. 检查后端服务日志
echo -e "${YELLOW}[5/7] 检查 System Backend 日志 (最近30行)...${NC}"
docker compose -f docker-compose.prod.yml logs --tail=30 system-backend
echo ""

# 6. 测试内部连接
echo -e "${YELLOW}[6/7] 测试内部服务连接...${NC}"
echo "测试 Gateway 健康检查:"
docker compose -f docker-compose.prod.yml exec -T gateway wget -qO- http://localhost:8000/health 2>&1 || echo "Gateway 不可访问"
echo ""

echo "测试 System Backend 健康检查:"
docker compose -f docker-compose.prod.yml exec -T system-backend wget -qO- http://localhost:8080/health 2>&1 || echo "System Backend 不可访问"
echo ""

# 7. 检查端口监听
echo -e "${YELLOW}[7/7] 检查端口监听状态...${NC}"
echo "宿主机端口监听:"
netstat -tlnp | grep -E ":(8000|8080|8081|8082)" || echo "未找到监听的端口"
echo ""

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}诊断完成${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""
echo -e "${YELLOW}常见问题排查:${NC}"
echo ""
echo -e "1. ${RED}502 Bad Gateway${NC} - Nginx 无法连接到 Gateway"
echo "   检查: Gateway 服务是否启动并健康"
echo "   修复: docker compose -f docker-compose.prod.yml restart gateway"
echo ""
echo -e "2. ${RED}页面空白${NC} - 前端服务未正确加载"
echo "   检查: Portal/System/Manager Frontend 是否启动"
echo "   修复: docker compose -f docker-compose.prod.yml restart portal system-frontend"
echo ""
echo -e "3. ${RED}API 调用失败${NC} - 后端服务不可用"
echo "   检查: System/Manager/Meta Backend 是否健康"
echo "   修复: docker compose -f docker-compose.prod.yml restart system-backend"
echo ""
echo -e "查看完整日志:"
echo "  docker compose -f docker-compose.prod.yml logs -f"
echo ""
echo -e "重启所有服务:"
echo "  docker compose -f docker-compose.prod.yml restart"
