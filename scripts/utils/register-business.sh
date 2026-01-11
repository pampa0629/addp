#!/usr/bin/env bash

# ADDP Business Engines Registration Script
# 自动注册 business 数据库中的引擎到 ADDP system

set -euo pipefail

BLUE='\033[0;34m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${PROJECT_ROOT}"

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}ADDP Business Engines Registration${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""

# 加载配置
if [ -f ./.env ]; then
  set -a
  source ./.env || true
  set +a
fi

if [ -f ./business/.env ]; then
  set -a
  source ./business/.env || true
  set +a
fi

# 配置参数
SYSTEM_API_URL="${SYSTEM_SERVICE_URL:-http://localhost:8080}"
ADMIN_USERNAME="${DEFAULT_ADMIN_USERNAME:-admin}"
ADMIN_PASSWORD="${DEFAULT_ADMIN_PASSWORD:-123456}"

# Business PostgreSQL 配置
BUSINESS_PG_HOST="${BUSINESS_PG_HOST:-localhost}"
BUSINESS_PG_PORT="${POSTGRES_PORT:-5433}"
BUSINESS_PG_USER="${POSTGRES_USER:-business}"
BUSINESS_PG_PASSWORD="${POSTGRES_PASSWORD:-business_password}"
BUSINESS_PG_DB="${POSTGRES_DB:-business}"

# Business MinIO 配置
BUSINESS_MINIO_HOST="${BUSINESS_MINIO_HOST:-localhost}"
BUSINESS_MINIO_PORT="${MINIO_API_PORT:-9002}"
BUSINESS_MINIO_USER="${MINIO_ROOT_USER:-minioadmin}"
BUSINESS_MINIO_PASSWORD="${MINIO_ROOT_PASSWORD:-minioadmin}"

echo -e "${YELLOW}▶ 检查服务可用性...${NC}"

# 检查 System API
if ! curl -sf "${SYSTEM_API_URL}/health" >/dev/null 2>&1; then
  echo -e "${RED}✗ System API 不可用: ${SYSTEM_API_URL}${NC}"
  echo -e "${YELLOW}  请先启动 System 服务: ./scripts/dev/start.sh -system${NC}"
  exit 1
fi
echo -e "${GREEN}✓ System API 可用${NC}"

# 检查 Business PostgreSQL
if ! PGPASSWORD="${BUSINESS_PG_PASSWORD}" psql -h "${BUSINESS_PG_HOST}" -p "${BUSINESS_PG_PORT}" -U "${BUSINESS_PG_USER}" -d "${BUSINESS_PG_DB}" -c "\q" >/dev/null 2>&1; then
  echo -e "${RED}✗ Business PostgreSQL 不可用${NC}"
  echo -e "${YELLOW}  请先启动 Business 数据库: cd business && docker compose up -d${NC}"
  exit 1
fi
echo -e "${GREEN}✓ Business PostgreSQL 可用${NC}"

# 检查 Business MinIO
if ! curl -sf "http://${BUSINESS_MINIO_HOST}:${BUSINESS_MINIO_PORT}/minio/health/live" >/dev/null 2>&1; then
  echo -e "${RED}✗ Business MinIO 不可用${NC}"
  echo -e "${YELLOW}  请先启动 Business MinIO: cd business && docker compose up -d${NC}"
  exit 1
fi
echo -e "${GREEN}✓ Business MinIO 可用${NC}"

echo ""
echo -e "${YELLOW}▶ 登录 ADDP 系统（用户: ${ADMIN_USERNAME}）...${NC}"

# 登录获取 token
LOGIN_RESPONSE=$(curl -s -X POST "${SYSTEM_API_URL}/api/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"${ADMIN_USERNAME}\",\"password\":\"${ADMIN_PASSWORD}\"}")

# 检查是否触发速率限制
if echo "$LOGIN_RESPONSE" | grep -q "Limit exceeded"; then
  echo -e "${RED}✗ 登录失败：触发速率限制${NC}"
  echo -e "${YELLOW}  请等待几分钟后再试，或检查 system/backend 的速率限制配置${NC}"
  exit 1
fi

TOKEN=$(echo "$LOGIN_RESPONSE" | jq -r '.access_token // .token // empty' 2>/dev/null)

if [ -z "$TOKEN" ] || [ "$TOKEN" = "null" ]; then
  echo -e "${RED}✗ 登录失败${NC}"
  echo "响应: $LOGIN_RESPONSE"
  exit 1
fi

echo -e "${GREEN}✓ 登录成功${NC}"

# 注册引擎函数
register_engine() {
  local name="$1"
  local engine_type="$2"
  local connection_info="$3"
  local description="$4"

  echo ""
  echo -e "${YELLOW}▶ 注册引擎: ${name}${NC}"

  # 检查引擎是否已存在
  EXISTING=$(curl -s -X GET "${SYSTEM_API_URL}/api/engines" \
    -H "Authorization: Bearer ${TOKEN}" \
    | jq -r ".data[] | select(.name == \"${name}\") | .id // empty")

  if [ -n "$EXISTING" ]; then
    echo -e "${YELLOW}  ⚠️  引擎已存在 (ID: ${EXISTING})，跳过注册${NC}"
    return 0
  fi

  # 创建引擎
  PAYLOAD=$(cat <<EOF
{
  "name": "${name}",
  "engine_type": "${engine_type}",
  "engine_category": "standard",
  "connection_info": ${connection_info},
  "description": "${description}",
  "scan_config": {
    "enabled": false,
    "immediate_scan": false,
    "scheduled_scan": false
  }
}
EOF
)

  RESPONSE=$(curl -s -X POST "${SYSTEM_API_URL}/api/engines" \
    -H "Authorization: Bearer ${TOKEN}" \
    -H 'Content-Type: application/json' \
    -d "$PAYLOAD")

  ENGINE_ID=$(echo "$RESPONSE" | jq -r '.id // .data.id // empty')

  if [ -z "$ENGINE_ID" ] || [ "$ENGINE_ID" = "null" ]; then
    echo -e "${RED}  ✗ 注册失败${NC}"
    echo "  响应: $RESPONSE"
    return 1
  fi

  echo -e "${GREEN}  ✓ 注册成功 (ID: ${ENGINE_ID})${NC}"

  # 测试连接
  echo -e "${YELLOW}  ▸ 测试连接...${NC}"
  TEST_RESPONSE=$(curl -s -X POST "${SYSTEM_API_URL}/api/engines/${ENGINE_ID}/test" \
    -H "Authorization: Bearer ${TOKEN}")

  TEST_SUCCESS=$(echo "$TEST_RESPONSE" | jq -r '.success // empty' 2>/dev/null || echo "false")

  if [ "$TEST_SUCCESS" = "true" ]; then
    echo -e "${GREEN}  ✓ 连接测试成功${NC}"
  else
    echo -e "${RED}  ✗ 连接测试失败${NC}"
    echo "  响应: $TEST_RESPONSE"
  fi
}

echo ""
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}开始注册 Business 引擎${NC}"
echo -e "${BLUE}========================================${NC}"

# 注册 Business PostgreSQL
register_engine \
  "Business PostgreSQL" \
  "postgresql" \
  "{\"host\":\"${BUSINESS_PG_HOST}\",\"port\":${BUSINESS_PG_PORT},\"database\":\"${BUSINESS_PG_DB}\",\"username\":\"${BUSINESS_PG_USER}\",\"password\":\"${BUSINESS_PG_PASSWORD}\"}" \
  "业务数据库 - PostgreSQL (带 PostGIS 空间扩展)"

# 注册 Business MinIO
register_engine \
  "Business MinIO" \
  "s3" \
  "{\"endpoint\":\"${BUSINESS_MINIO_HOST}:${BUSINESS_MINIO_PORT}\",\"access_key\":\"${BUSINESS_MINIO_USER}\",\"secret_key\":\"${BUSINESS_MINIO_PASSWORD}\",\"use_ssl\":false}" \
  "业务对象存储 - MinIO (S3 兼容)"

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}注册完成！${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo "已注册的 Business 引擎："
echo "  1. Business PostgreSQL (${BUSINESS_PG_HOST}:${BUSINESS_PG_PORT})"
echo "  2. Business MinIO (${BUSINESS_MINIO_HOST}:${BUSINESS_MINIO_PORT})"
echo ""
echo -e "${YELLOW}提示: 可以在「系统管理 -- 引擎管理」页面查看和管理引擎${NC}"
echo ""
