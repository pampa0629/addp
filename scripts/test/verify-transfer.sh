#!/bin/bash

# Transfer 模块配置验证脚本
# 用途: 在启动前验证所有配置是否正确

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 项目根目录
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)" pwd)"
cd "$PROJECT_ROOT"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}  Transfer 模块配置验证${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

ERRORS=0
WARNINGS=0

# 1. 检查 transfer/backend/.env 文件
echo -e "${YELLOW}[1/8]${NC} 检查配置文件..."
if [ ! -f "transfer/backend/.env" ]; then
    echo -e "${RED}✗ transfer/backend/.env 不存在${NC}"
    echo -e "  ${YELLOW}运行: cp transfer/backend/.env.example transfer/backend/.env${NC}"
    ERRORS=$((ERRORS + 1))
else
    echo -e "${GREEN}✓ transfer/backend/.env 存在${NC}"
fi
echo ""

# 2. 检查 INTERNAL_API_KEY 配置
echo -e "${YELLOW}[2/8]${NC} 检查内部 API Key..."
if [ -f ".env" ] && [ -f "transfer/backend/.env" ]; then
    SYSTEM_KEY=$(grep "^INTERNAL_API_KEY=" .env 2>/dev/null | cut -d '=' -f2 | tr -d '"' | tr -d "'" || echo "")
    TRANSFER_KEY=$(grep "^INTERNAL_API_KEY=" transfer/backend/.env 2>/dev/null | cut -d '=' -f2 | tr -d '"' | tr -d "'" || echo "")

    if [ -z "$SYSTEM_KEY" ]; then
        echo -e "${RED}✗ System INTERNAL_API_KEY 未配置（.env）${NC}"
        ERRORS=$((ERRORS + 1))
    elif [ -z "$TRANSFER_KEY" ]; then
        echo -e "${YELLOW}⚠ Transfer INTERNAL_API_KEY 未配置${NC}"
        echo -e "  ${YELLOW}将使用 BaseConfig fallback: $SYSTEM_KEY${NC}"
        WARNINGS=$((WARNINGS + 1))
    elif [ "$SYSTEM_KEY" != "$TRANSFER_KEY" ]; then
        echo -e "${RED}✗ INTERNAL_API_KEY 不匹配${NC}"
        echo -e "  System:   ${SYSTEM_KEY}"
        echo -e "  Transfer: ${TRANSFER_KEY}"
        echo -e "  ${YELLOW}必须保持一致！${NC}"
        ERRORS=$((ERRORS + 1))
    else
        echo -e "${GREEN}✓ INTERNAL_API_KEY 配置正确${NC}"
        echo -e "  Key: ${SYSTEM_KEY}"
    fi
else
    echo -e "${RED}✗ 配置文件缺失，无法验证${NC}"
    ERRORS=$((ERRORS + 1))
fi
echo ""

# 3. 检查 PostgreSQL 连接
echo -e "${YELLOW}[3/8]${NC} 检查 PostgreSQL..."
if ! command -v psql &> /dev/null; then
    echo -e "${YELLOW}⚠ psql 命令不可用，跳过数据库检查${NC}"
    WARNINGS=$((WARNINGS + 1))
elif ! PGPASSWORD=addp_password psql -h localhost -U addp -d addp -c "SELECT 1" > /dev/null 2>&1; then
    echo -e "${RED}✗ 无法连接到 PostgreSQL${NC}"
    echo -e "  ${YELLOW}请确保 PostgreSQL 正在运行: docker-compose up -d postgres${NC}"
    ERRORS=$((ERRORS + 1))
else
    echo -e "${GREEN}✓ PostgreSQL 连接正常${NC}"

    # 检查 transfer schema
    if PGPASSWORD=addp_password psql -h localhost -U addp -d addp -tAc "SELECT 1 FROM pg_namespace WHERE nspname='transfer'" | grep -q 1; then
        echo -e "${GREEN}✓ transfer schema 存在${NC}"
    else
        echo -e "${YELLOW}⚠ transfer schema 不存在${NC}"
        echo -e "  ${YELLOW}将在首次启动时自动创建${NC}"
        WARNINGS=$((WARNINGS + 1))
    fi
fi
echo ""

# 4. 检查 Redis 连接
echo -e "${YELLOW}[4/8]${NC} 检查 Redis..."
if ! command -v redis-cli &> /dev/null; then
    echo -e "${YELLOW}⚠ redis-cli 命令不可用，跳过 Redis 检查${NC}"
    WARNINGS=$((WARNINGS + 1))
elif ! redis-cli -h localhost -p 6379 -a addp_redis PING > /dev/null 2>&1; then
    echo -e "${RED}✗ 无法连接到 Redis${NC}"
    echo -e "  ${YELLOW}请确保 Redis 正在运行: docker-compose up -d redis${NC}"
    ERRORS=$((ERRORS + 1))
else
    echo -e "${GREEN}✓ Redis 连接正常${NC}"
fi
echo ""

# 5. 检查 System Backend 是否运行
echo -e "${YELLOW}[5/8]${NC} 检查 System Backend..."
if curl -s http://localhost:8080/health > /dev/null 2>&1; then
    echo -e "${GREEN}✓ System Backend 正在运行${NC}"
else
    echo -e "${YELLOW}⚠ System Backend 未运行${NC}"
    echo -e "  ${YELLOW}Transfer 需要 System 提供配置和资源管理${NC}"
    WARNINGS=$((WARNINGS + 1))
fi
echo ""

# 6. 检查 Portal 前端文件
echo -e "${YELLOW}[6/8]${NC} 检查 Portal 集成..."
if grep -q 'disabled' portal/frontend/src/views/Portal.vue 2>/dev/null; then
    echo -e "${RED}✗ Portal 中 Transfer 仍然被禁用${NC}"
    echo -e "  ${YELLOW}请检查 portal/frontend/src/views/Portal.vue${NC}"
    ERRORS=$((ERRORS + 1))
else
    echo -e "${GREEN}✓ Portal 中 Transfer 已启用${NC}"
fi

if grep -q "transfer: 'http://localhost:5176'" portal/frontend/src/views/Portal.vue 2>/dev/null; then
    echo -e "${GREEN}✓ Portal moduleUrls 包含 Transfer${NC}"
else
    echo -e "${RED}✗ Portal moduleUrls 未配置 Transfer${NC}"
    ERRORS=$((ERRORS + 1))
fi
echo ""

# 7. 检查 Transfer 前端目录
echo -e "${YELLOW}[7/8]${NC} 检查 Transfer Frontend..."
if [ ! -d "transfer/frontend" ]; then
    echo -e "${RED}✗ transfer/frontend 目录不存在${NC}"
    ERRORS=$((ERRORS + 1))
elif [ ! -f "transfer/frontend/package.json" ]; then
    echo -e "${RED}✗ transfer/frontend/package.json 不存在${NC}"
    ERRORS=$((ERRORS + 1))
else
    echo -e "${GREEN}✓ Transfer Frontend 目录存在${NC}"

    # 检查是否安装了依赖
    if [ ! -d "transfer/frontend/node_modules" ]; then
        echo -e "${YELLOW}⚠ Transfer Frontend 依赖未安装${NC}"
        echo -e "  ${YELLOW}运行: cd transfer/frontend && npm install${NC}"
        WARNINGS=$((WARNINGS + 1))
    else
        echo -e "${GREEN}✓ Transfer Frontend 依赖已安装${NC}"
    fi
fi
echo ""

# 8. 检查启动脚本
echo -e "${YELLOW}[8/8]${NC} 检查启动脚本..."
if ! grep -q "transfer/backend" scripts/dev/start.sh 2>/dev/null; then
    echo -e "${RED}✗ start.sh 未包含 Transfer Backend${NC}"
    ERRORS=$((ERRORS + 1))
else
    echo -e "${GREEN}✓ start.sh 包含 Transfer Backend${NC}"
fi

if ! grep -q "transfer/frontend" scripts/dev/start.sh 2>/dev/null; then
    echo -e "${RED}✗ start.sh 未包含 Transfer Frontend${NC}"
    ERRORS=$((ERRORS + 1))
else
    echo -e "${GREEN}✓ start.sh 包含 Transfer Frontend${NC}"
fi

if ! grep -q "transfer.pid" scripts/dev/stop.sh 2>/dev/null; then
    echo -e "${RED}✗ stop.sh 未包含 Transfer 停止逻辑${NC}"
    ERRORS=$((ERRORS + 1))
else
    echo -e "${GREEN}✓ stop.sh 包含 Transfer 停止逻辑${NC}"
fi
echo ""

# 总结
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}  验证结果${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

if [ $ERRORS -eq 0 ] && [ $WARNINGS -eq 0 ]; then
    echo -e "${GREEN}✓ 所有检查通过！${NC}"
    echo ""
    echo -e "${GREEN}可以启动 Transfer 模块:${NC}"
    echo -e "  ${BLUE}./scripts/dev/start.sh${NC}"
    echo ""
    exit 0
elif [ $ERRORS -eq 0 ]; then
    echo -e "${YELLOW}⚠ 检查完成，有 $WARNINGS 个警告${NC}"
    echo ""
    echo -e "${YELLOW}建议修复警告后再启动，但不是必须的${NC}"
    echo ""
    exit 0
else
    echo -e "${RED}✗ 发现 $ERRORS 个错误和 $WARNINGS 个警告${NC}"
    echo ""
    echo -e "${RED}请修复上述错误后再启动服务${NC}"
    echo ""
    exit 1
fi
