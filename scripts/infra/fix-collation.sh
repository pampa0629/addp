#!/usr/bin/env bash

# ADDP - Fix PostgreSQL Collation Version Mismatch
# 修复 PostgreSQL collation 版本不匹配警告

set -euo pipefail

BLUE='\033[0;34m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}修复 PostgreSQL Collation 版本${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# Check if PostgreSQL is running
if ! docker compose ps postgres 2>/dev/null | grep -q "Up"; then
    echo -e "${RED}✗ PostgreSQL 容器未运行${NC}"
    echo -e "${YELLOW}请先启动 PostgreSQL: bash scripts/infra/up.sh${NC}"
    exit 1
fi

echo -e "${YELLOW}▶ 检查当前 collation 版本...${NC}"
docker exec addp-postgres psql -U addp -d addp -c "SHOW lc_collate; SHOW lc_ctype;" 2>&1 | head -10

echo ""
echo -e "${YELLOW}▶ 刷新数据库 collation 版本...${NC}"

# Refresh collation version for the database
if docker exec addp-postgres psql -U addp -d addp -c "ALTER DATABASE addp REFRESH COLLATION VERSION;" 2>&1; then
    echo -e "${GREEN}✓ 数据库 collation 版本已刷新${NC}"
else
    echo -e "${RED}✗ 刷新失败,尝试备用方案...${NC}"
    echo ""
    echo -e "${YELLOW}▶ 使用系统表更新 collation 版本${NC}"

    # Alternative: Update system catalog directly
    docker exec addp-postgres psql -U addp -d addp <<'EOSQL'
-- Get current collation version from OS
SELECT version FROM pg_collation WHERE collname = 'default';

-- Update database collation version to match OS
UPDATE pg_database
SET datcollversion = (SELECT collversion FROM pg_collation WHERE collname = 'default' LIMIT 1)
WHERE datname = 'addp';

-- Verify
SELECT datname, datcollversion FROM pg_database WHERE datname = 'addp';
EOSQL

    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✓ Collation 版本已通过系统表更新${NC}"
    else
        echo -e "${RED}✗ 更新失败${NC}"
        echo ""
        echo -e "${YELLOW}说明: Collation 版本不匹配通常发生在:${NC}"
        echo "  1. 使用不同版本的 PostgreSQL 镜像"
        echo "  2. 数据卷从旧镜像迁移到新镜像"
        echo ""
        echo -e "${YELLOW}解决方案:${NC}"
        echo "  1. 重建索引: REINDEX DATABASE addp;"
        echo "  2. 重新创建受影响的对象"
        echo "  3. 或接受警告(不影响功能,仅影响排序和比较)"
        exit 1
    fi
fi

echo ""
echo -e "${YELLOW}▶ 验证修复结果...${NC}"
RESULT=$(docker exec addp-postgres psql -U addp -d addp -c "SELECT datname, datcollversion FROM pg_database WHERE datname = 'addp';" 2>&1)

if echo "$RESULT" | grep -q "collation version mismatch"; then
    echo -e "${RED}✗ 警告仍然存在${NC}"
    echo "$RESULT"
    exit 1
else
    echo -e "${GREEN}✓ Collation 版本警告已消除!${NC}"
    echo "$RESULT"
fi

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}✓ 修复完成!${NC}"
echo -e "${GREEN}========================================${NC}"
