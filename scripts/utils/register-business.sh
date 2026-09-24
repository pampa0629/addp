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
source "${PROJECT_ROOT}/business/scripts/ports.sh"

business_mapped_port() (
  PROJECT_ROOT="${PROJECT_ROOT}/business"
  addp_business_verify_container "$1" "$2" || exit 1
  addp_business_mapped_port "$2" "$3"
)

validate_business_pins() (
  local services=(postgres minio)
  if docker ps --format '{{.Names}}' | grep -qx 'business-mysql'; then
    services+=(mysql)
  fi
  PROJECT_ROOT="${PROJECT_ROOT}/business"
  addp_business_resolve_ports "${services[@]}" >/dev/null
)

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
validate_business_pins

# 配置参数
SYSTEM_API_URL="${SYSTEM_URL:-http://localhost:8180}"

# 本脚本用于本机开发：System 等模块作为宿主机进程访问 Business 容器。
# 连接地址以本工作区容器的实际宿主机映射为准。
BUSINESS_PG_HOST=127.0.0.1
BUSINESS_PG_PORT=$(business_mapped_port postgres business-postgres 5432) || {
  echo -e "${RED}✗ 无法读取本工作区 Business PostgreSQL 的宿主机端口${NC}" >&2
  exit 1
}
BUSINESS_PG_USER="${POSTGRES_USER:-business}"
BUSINESS_PG_PASSWORD="${POSTGRES_PASSWORD:-business_password}"
BUSINESS_PG_DB="${POSTGRES_DB:-business}"

BUSINESS_MYSQL_HOST=127.0.0.1
BUSINESS_MYSQL_USER="${MYSQL_USER:-business}"
BUSINESS_MYSQL_PASSWORD="${MYSQL_PASSWORD:-business_password}"
BUSINESS_MYSQL_DB="${MYSQL_DATABASE:-business}"

# Business Oracle 配置
BUSINESS_ORACLE_HOST=127.0.0.1
BUSINESS_ORACLE_PORT="${ORACLE_PORT:-15210}"
BUSINESS_ORACLE_SERVICE_NAME="${ORACLE_SERVICE_NAME:-FREEPDB1}"
BUSINESS_ORACLE_USER="${ORACLE_APP_USER:-business}"
BUSINESS_ORACLE_PASSWORD="${ORACLE_APP_PASSWORD:-business_oracle_password}"

# Business OceanBase CE 配置
BUSINESS_OCEANBASE_HOST=127.0.0.1
BUSINESS_OCEANBASE_PORT="${OCEANBASE_PORT:-2881}"
BUSINESS_OCEANBASE_DATABASE="${OCEANBASE_DATABASE:-business}"
BUSINESS_OCEANBASE_USER="${BUSINESS_OCEANBASE_USER:-root@${OCEANBASE_TENANT_NAME:-test}}"
BUSINESS_OCEANBASE_PASSWORD="${OCEANBASE_PASSWORD:-business_oceanbase_password}"
BUSINESS_OCEANBASE_AVAILABLE=false

# Business TiDB 配置
BUSINESS_TIDB_HOST=127.0.0.1
BUSINESS_TIDB_PORT="${TIDB_PORT:-4000}"
BUSINESS_DOCKER_NETWORK="${BUSINESS_DOCKER_NETWORK:-business_business-network}"
BUSINESS_TIDB_DATABASE="${TIDB_DATABASE:-business}"
BUSINESS_TIDB_USER="${TIDB_USER:-root}"
BUSINESS_TIDB_PASSWORD="${TIDB_PASSWORD:-}"
BUSINESS_TIDB_AVAILABLE=false
TIDB_CLIENT_IMAGE=mysql:8.0@sha256:7dcddc01f13bab2f15cde676d44d01f61fc9f99fe7785e86196dfc07d358ae2b

# Business openGauss 配置
BUSINESS_OPENGAUSS_HOST=127.0.0.1
BUSINESS_OPENGAUSS_PORT="${OPENGAUSS_PORT:-5435}"
BUSINESS_OPENGAUSS_DATABASE="${OPENGAUSS_DATABASE:-business}"
BUSINESS_OPENGAUSS_USER=gaussdb
BUSINESS_OPENGAUSS_PASSWORD="${OPENGAUSS_PASSWORD:-AddpGauss606@}"
BUSINESS_OPENGAUSS_AVAILABLE=false

# Business MinIO 配置
BUSINESS_MINIO_PORT=$(business_mapped_port minio business-minio 9000) || {
  echo -e "${RED}✗ 无法读取本工作区 Business MinIO 的宿主机端口${NC}" >&2
  exit 1
}
BUSINESS_MINIO_ENDPOINT="127.0.0.1:${BUSINESS_MINIO_PORT}"
BUSINESS_MINIO_USER="${BUSINESS_MINIO_ACCESS_KEY:-${MINIO_ROOT_USER:-minioadmin}}"
BUSINESS_MINIO_PASSWORD="${BUSINESS_MINIO_SECRET_KEY:-${MINIO_ROOT_PASSWORD:-minioadmin}}"

echo -e "${YELLOW}▶ 检查服务可用性...${NC}"

# 检查 System API
if ! curl -sf "${SYSTEM_API_URL}/health/ready" >/dev/null 2>&1; then
  echo -e "${RED}✗ System API 不可用: ${SYSTEM_API_URL}${NC}"
  echo -e "${YELLOW}  请先启动 System 服务: ./scripts/dev/start.sh -system${NC}"
  exit 1
fi
echo -e "${GREEN}✓ System API 可用${NC}"

# 检查 Business PostgreSQL
if docker ps --format '{{.Names}}' | grep -qx 'business-postgres'; then
  if ! docker exec business-postgres pg_isready -U "${BUSINESS_PG_USER}" -d "${BUSINESS_PG_DB}" >/dev/null 2>&1; then
    echo -e "${RED}✗ Business PostgreSQL 不可用${NC}"
    echo -e "${YELLOW}  请先启动 Business 数据库: cd business && docker compose up -d${NC}"
    exit 1
  fi
else
  echo -e "${RED}✗ Business PostgreSQL 不可用${NC}"
  exit 1
fi
echo -e "${GREEN}✓ Business PostgreSQL 可用${NC}"

BUSINESS_MYSQL_AVAILABLE=false
if docker ps --format '{{.Names}}' | grep -qx 'business-mysql'; then
  BUSINESS_MYSQL_PORT=$(business_mapped_port mysql business-mysql 3306)
  if ! docker exec business-mysql mysqladmin ping -h 127.0.0.1 -u"${BUSINESS_MYSQL_USER}" -p"${BUSINESS_MYSQL_PASSWORD}" --silent >/dev/null 2>&1; then
    echo -e "${RED}✗ Business MySQL 不可用${NC}"
    exit 1
  fi
  BUSINESS_MYSQL_AVAILABLE=true
  echo -e "${GREEN}✓ Business MySQL 可用${NC}"
fi

# 检查 Business Oracle
BUSINESS_ORACLE_AVAILABLE=false
if docker ps --format '{{.Names}}' | grep -qx 'business-oracle'; then
  if ! docker exec business-oracle healthcheck.sh >/dev/null 2>&1; then
    echo -e "${RED}✗ Business Oracle 不可用${NC}"
    echo -e "${YELLOW}  请先启动: cd business && bash scripts/start.sh -oracle${NC}"
    exit 1
  fi
  BUSINESS_ORACLE_AVAILABLE=true
  echo -e "${GREEN}✓ Business Oracle 可用${NC}"
else
  echo -e "${YELLOW}⚠️  Business Oracle 未启动，跳过注册${NC}"
fi

# OceanBase 是可选业务引擎；只在容器已健康运行时注册。
if docker ps --format '{{.Names}}' | grep -qx 'business-oceanbase'; then
  if docker exec business-oceanbase obclient \
    -h127.0.0.1 -P2881 -u"${BUSINESS_OCEANBASE_USER}" \
    --password="${BUSINESS_OCEANBASE_PASSWORD}" \
    -D"${BUSINESS_OCEANBASE_DATABASE}" -e 'SELECT 1' >/dev/null 2>&1; then
    BUSINESS_OCEANBASE_AVAILABLE=true
    echo -e "${GREEN}✓ Business OceanBase CE 可用${NC}"
  else
    echo -e "${RED}✗ Business OceanBase CE 容器已运行但不可查询${NC}"
    exit 1
  fi
else
  echo -e "${YELLOW}⚠️  Business OceanBase CE 未启动，跳过注册${NC}"
fi

if docker ps --format '{{.Names}}' | grep -qx 'business-tidb'; then
  if docker run --rm --network "${BUSINESS_DOCKER_NETWORK}" \
    -e "MYSQL_PWD=${BUSINESS_TIDB_PASSWORD}" \
    "${TIDB_CLIENT_IMAGE}" mysql \
    -hbusiness-tidb -P4000 \
    -u"${BUSINESS_TIDB_USER}" --protocol=tcp --connect-timeout=5 \
    -D"${BUSINESS_TIDB_DATABASE}" -Nse 'SELECT 1' 2>/dev/null | grep -Fxq '1'; then
    BUSINESS_TIDB_AVAILABLE=true
    echo -e "${GREEN}✓ Business TiDB 8.5.8 可用${NC}"
  else
    echo -e "${RED}✗ Business TiDB 容器已运行但不可查询${NC}"
    exit 1
  fi
else
  echo -e "${YELLOW}⚠️  Business TiDB 未启动，跳过注册${NC}"
fi

# openGauss 是可选业务引擎；只在容器已健康运行时注册。
if docker ps --format '{{.Names}}' | grep -qx 'business-opengauss'; then
  if docker exec --user omm \
    --env GAUSSHOME=/usr/local/opengauss \
    --env PATH=/usr/local/opengauss/bin:/scws/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
    --env LD_LIBRARY_PATH=/usr/local/opengauss/lib:/scws/lib \
    business-opengauss /usr/local/opengauss/bin/gsql \
    -At -d "${BUSINESS_OPENGAUSS_DATABASE}" -p 5432 -c 'SELECT 1' 2>/dev/null | grep -Fxq '1'; then
    BUSINESS_OPENGAUSS_AVAILABLE=true
    echo -e "${GREEN}✓ Business openGauss 可用${NC}"
  else
    echo -e "${RED}✗ Business openGauss 容器已运行但不可查询${NC}"
    exit 1
  fi
else
  echo -e "${YELLOW}⚠️  Business openGauss 未启动，跳过注册${NC}"
fi

# 检查 Business MinIO
if docker ps --format '{{.Names}}' | grep -qx 'business-minio'; then
  if ! docker exec business-minio curl -sf "http://localhost:9000/minio/health/live" >/dev/null 2>&1; then
    echo -e "${RED}✗ Business MinIO 不可用${NC}"
    echo -e "${YELLOW}  请先启动 Business MinIO: cd business && docker compose up -d${NC}"
    exit 1
  fi
else
  echo -e "${RED}✗ Business MinIO 不可用${NC}"
  exit 1
fi
echo -e "${GREEN}✓ Business MinIO 可用${NC}"

BUSINESS_NFS_AVAILABLE=false
NFS_EXPORT_PATH="${PROJECT_ROOT}/business/nfs/data"
if nfsd status 2>/dev/null | grep -q 'nfsd is running' && showmount -e localhost 2>/dev/null | grep -Fq "$NFS_EXPORT_PATH"; then
  BUSINESS_NFS_AVAILABLE=true
  echo -e "${GREEN}✓ Business NFS 可用${NC}"
fi

echo ""
if [ ! -t 0 ]; then
  echo -e "${RED}✗ 必须在交互式终端中输入当前 User Access Token${NC}"
  exit 1
fi
read -r -s -p "请输入具有 System Engine 管理权限的当前 User Access Token: " TOKEN
echo ""

if [ -z "$TOKEN" ]; then
  echo -e "${RED}✗ User Access Token 不得为空${NC}"
  exit 1
fi

if ! curl -sf "${SYSTEM_API_URL}/api/v1/system/auth/context" \
  -H "Authorization: Bearer ${TOKEN}" >/dev/null; then
  echo -e "${RED}✗ User Access Token 无效或已过期${NC}"
  exit 1
fi

echo -e "${GREEN}✓ AuthContext 校验成功${NC}"

# 注册引擎函数
register_engine() {
  local name="$1"
  local engine_type="$2"
  local connection_info="$3"
  local description="$4"
  local payload response engine_id test_response

  echo ""
  echo -e "${YELLOW}▶ 注册引擎: ${name}${NC}"

  payload=$(jq -nc --arg name "$name" --arg engine_type "$engine_type" \
    --arg description "$description" --argjson connection_info "$connection_info" \
    '{name:$name,engine_type:$engine_type,engine_origin:"general",connection_info:$connection_info,description:$description}')

  # System 根据 Tenant、类型和物理身份幂等返回原 ID；端点变化必须新建实例。
  if ! response=$(curl -sS --fail-with-body -X POST "${SYSTEM_API_URL}/api/v1/system/engines" \
      -H "Authorization: Bearer ${TOKEN}" -H 'Content-Type: application/json' -d "$payload"); then
    echo -e "${RED}  ✗ 注册失败${NC}"
    echo "  响应: $response"
    return 1
  fi
  engine_id=$(echo "$response" | jq -r '.id // .data.id // empty')
  if [ -z "$engine_id" ]; then
    echo -e "${RED}  ✗ System 未返回引擎 ID${NC}"
    return 1
  fi

  echo -e "${GREEN}  ✓ 按物理身份注册成功 (ID: ${engine_id})${NC}"

  # 测试连接
  echo -e "${YELLOW}  ▸ 测试连接...${NC}"
  test_response=$(curl -sS --fail-with-body -X POST "${SYSTEM_API_URL}/api/v1/system/engines/${engine_id}/test" \
    -H "Authorization: Bearer ${TOKEN}")

  TEST_SUCCESS=$(echo "$test_response" | jq -r '.success // empty' 2>/dev/null || echo "false")

  if [ "$TEST_SUCCESS" = "true" ]; then
    echo -e "${GREEN}  ✓ 连接测试成功${NC}"
  else
    echo -e "${RED}  ✗ 连接测试失败${NC}"
    echo "  响应: $test_response"
    return 1
  fi
}

echo ""
echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}开始注册 Business 引擎${NC}"
echo -e "${BLUE}========================================${NC}"

# 注册 Business PostgreSQL
register_engine \
  "Business PostgreSQL (${BUSINESS_PG_PORT})" \
  "postgresql" \
  "$(jq -nc --arg host "$BUSINESS_PG_HOST" --argjson port "$BUSINESS_PG_PORT" --arg database "$BUSINESS_PG_DB" --arg user "$BUSINESS_PG_USER" --arg password "$BUSINESS_PG_PASSWORD" '{host:$host,port:$port,database:$database,user:$user,password:$password}')" \
  "业务数据库 - PostgreSQL (带 PostGIS 空间扩展)"

if [ "$BUSINESS_MYSQL_AVAILABLE" = true ]; then
  register_engine \
    "Business MySQL (${BUSINESS_MYSQL_PORT})" \
    "mysql" \
    "$(jq -nc --arg host "$BUSINESS_MYSQL_HOST" --argjson port "$BUSINESS_MYSQL_PORT" --arg database "$BUSINESS_MYSQL_DB" --arg user "$BUSINESS_MYSQL_USER" --arg password "$BUSINESS_MYSQL_PASSWORD" '{host:$host,port:$port,database:$database,user:$user,password:$password}')" \
    "业务数据库 - MySQL"
fi

# 注册 Business Oracle
if [ "$BUSINESS_ORACLE_AVAILABLE" = true ]; then
  register_engine \
    "Business Oracle (${BUSINESS_ORACLE_PORT})" \
    "oracle" \
    "$(jq -nc --arg host "$BUSINESS_ORACLE_HOST" --argjson port "$BUSINESS_ORACLE_PORT" --arg service_name "$BUSINESS_ORACLE_SERVICE_NAME" --arg user "$BUSINESS_ORACLE_USER" --arg password "$BUSINESS_ORACLE_PASSWORD" '{host:$host,port:$port,service_name:$service_name,user:$user,password:$password}')" \
    "业务数据库 - Oracle 普通表与只读快照"
fi

if [ "${BUSINESS_OCEANBASE_AVAILABLE}" = true ]; then
  register_engine \
    "Business OceanBase (${BUSINESS_OCEANBASE_PORT})" \
    "oceanbase" \
    "$(jq -nc --arg host "$BUSINESS_OCEANBASE_HOST" --argjson port "$BUSINESS_OCEANBASE_PORT" --arg database "$BUSINESS_OCEANBASE_DATABASE" --arg user "$BUSINESS_OCEANBASE_USER" --arg password "$BUSINESS_OCEANBASE_PASSWORD" '{host:$host,port:$port,database:$database,user:$user,password:$password}')" \
    "业务数据库 - OceanBase Community Edition (MySQL 模式)"
fi

if [ "${BUSINESS_TIDB_AVAILABLE}" = true ]; then
  register_engine \
    "Business TiDB (${BUSINESS_TIDB_PORT})" \
    "tidb" \
    "$(jq -nc --arg host "$BUSINESS_TIDB_HOST" --argjson port "$BUSINESS_TIDB_PORT" --arg database "$BUSINESS_TIDB_DATABASE" --arg user "$BUSINESS_TIDB_USER" --arg password "$BUSINESS_TIDB_PASSWORD" '{host:$host,port:$port,database:$database,user:$user,password:$password}')" \
    "业务数据库 - TiDB 8.5.8 (MySQL 协议)"
fi

if [ "${BUSINESS_OPENGAUSS_AVAILABLE}" = true ]; then
  register_engine \
    "Business openGauss (${BUSINESS_OPENGAUSS_PORT})" \
    "opengauss" \
    "$(jq -nc --arg host "$BUSINESS_OPENGAUSS_HOST" --argjson port "$BUSINESS_OPENGAUSS_PORT" --arg database "$BUSINESS_OPENGAUSS_DATABASE" --arg user "$BUSINESS_OPENGAUSS_USER" --arg password "$BUSINESS_OPENGAUSS_PASSWORD" '{host:$host,port:$port,database:$database,user:$user,password:$password,sslmode:"disable"}')" \
    "业务数据库 - openGauss 6.0.6 LTS (PG 兼容模式)"
fi

# 注册 Business MinIO
register_engine \
  "Business MinIO (${BUSINESS_MINIO_PORT})" \
  "minio" \
  "$(jq -nc --arg endpoint "$BUSINESS_MINIO_ENDPOINT" --arg access_key "$BUSINESS_MINIO_USER" --arg secret_key "$BUSINESS_MINIO_PASSWORD" '{endpoint:$endpoint,access_key:$access_key,secret_key:$secret_key,use_ssl:false}')" \
  "业务对象存储 - MinIO (S3 兼容)"

if [ "$BUSINESS_NFS_AVAILABLE" = true ]; then
  register_engine \
    "Business NFS (${NFS_EXPORT_PATH})" \
    "nfs" \
    "$(jq -nc --arg server 127.0.0.1 --arg export_path "$NFS_EXPORT_PATH" '{server:$server,export_path:$export_path,access_mode:"rw",nfs_version:"3"}')" \
    "业务 NFS 文件系统"
fi

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}注册完成！${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo "已注册的 Business 引擎："
echo "  1. Business PostgreSQL (${BUSINESS_PG_HOST}:${BUSINESS_PG_PORT})"
if [ "$BUSINESS_MYSQL_AVAILABLE" = true ]; then
  echo "  Business MySQL (${BUSINESS_MYSQL_HOST}:${BUSINESS_MYSQL_PORT})"
fi
if [ "$BUSINESS_ORACLE_AVAILABLE" = true ]; then
  echo "  Business Oracle (${BUSINESS_ORACLE_HOST}:${BUSINESS_ORACLE_PORT}/${BUSINESS_ORACLE_SERVICE_NAME})"
fi
echo "  Business MinIO (${BUSINESS_MINIO_ENDPOINT})"
if [ "$BUSINESS_NFS_AVAILABLE" = true ]; then
  echo "  Business NFS (${NFS_EXPORT_PATH})"
fi
if [ "${BUSINESS_OCEANBASE_AVAILABLE}" = true ]; then
  echo "  4. Business OceanBase (${BUSINESS_OCEANBASE_HOST}:${BUSINESS_OCEANBASE_PORT}/${BUSINESS_OCEANBASE_DATABASE})"
fi
if [ "${BUSINESS_TIDB_AVAILABLE}" = true ]; then
  echo "  5. Business TiDB (${BUSINESS_TIDB_HOST}:${BUSINESS_TIDB_PORT}/${BUSINESS_TIDB_DATABASE})"
fi
if [ "${BUSINESS_OPENGAUSS_AVAILABLE}" = true ]; then
  echo "  6. Business openGauss (${BUSINESS_OPENGAUSS_HOST}:${BUSINESS_OPENGAUSS_PORT}/${BUSINESS_OPENGAUSS_DATABASE})"
fi
echo ""
echo -e "${YELLOW}提示: 可以在「系统管理 -- 引擎管理」页面查看和管理引擎${NC}"
echo ""
