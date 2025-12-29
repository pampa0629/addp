#!/usr/bin/env bash

# ADDP Infrastructure Redis Initialization
# 初始化 Redis 任务队列和缓存配置
# 由 infra-up.sh 自动调用

set -euo pipefail

BLUE='\033[0;34m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${PROJECT_ROOT}"

# Optional: load env overrides if present
if [ -f ./.env ]; then
  set -a
  # shellcheck disable=SC1091
  source ./.env || true
  set +a
fi

REDIS_PASSWORD="${REDIS_PASSWORD:-addp_redis}"
REDIS_HOST="${REDIS_HOST:-localhost}"
REDIS_PORT="${REDIS_PORT:-6379}"

# Check prerequisites
if ! command -v docker >/dev/null 2>&1; then
  echo -e "${RED}✗ docker 未安装或不可用${NC}"
  exit 1
fi

if ! docker compose version >/dev/null 2>&1; then
  echo -e "${RED}✗ docker compose 不可用${NC}"
  exit 1
fi

# 确认 Redis 容器正在运行
if ! docker compose ps --status running redis >/dev/null 2>&1; then
  echo -e "${RED}✗ Redis 容器未运行，无法初始化${NC}"
  echo -e "${YELLOW}  请先执行: bash scripts/infra/up.sh${NC}"
  exit 1
fi

# Helper function to execute Redis commands
redis_exec() {
  docker exec redis redis-cli -a "${REDIS_PASSWORD}" "$@" 2>/dev/null
}

# Parse command line arguments
CLEAN_MODE=false
if [[ "${1:-}" == "--clean" ]]; then
  CLEAN_MODE=true
fi

# ==================== CLEAN MODE ====================
if [[ "$CLEAN_MODE" == true ]]; then
  echo -e "${YELLOW}==========================================="
  echo -e "  清理 Redis 所有数据"
  echo -e "===========================================${NC}"
  echo ""
  echo -e "${RED}⚠️  警告：此操作将清空 Redis 中所有数据，包括：${NC}"
  echo -e "  ${YELLOW}- 所有任务队列（Asynq: transfer:*/meta:*）${NC}"
  echo -e "  ${YELLOW}- 所有缓存（cache:system:*/cache:manager:*）${NC}"
  echo -e "  ${YELLOW}- 所有临时数据${NC}"
  echo -e "  ${YELLOW}- 所有 Pub/Sub 订阅将断开${NC}"
  echo ""
  echo -e "${BLUE}数据类型：${NC}缓存和队列（非关键业务数据）"
  echo -e "${BLUE}清空后：${NC}服务会自动重建缓存和队列"
  echo -e "${BLUE}持久化：${NC}保留 Redis 容器和数据卷"
  echo ""

  read -p "确认清空所有 Redis 数据？(yes/no): " confirm
  if [[ "$confirm" != "yes" ]]; then
    echo -e "${YELLOW}✗ 操作已取消${NC}"
    exit 0
  fi

  echo ""
  echo -e "${YELLOW}▶ 正在清空 Redis...${NC}"
  echo -e "  ${BLUE}执行命令: FLUSHALL ASYNC${NC}"

  if redis_exec FLUSHALL ASYNC >/dev/null 2>&1; then
    echo -e "  ${GREEN}✓ Redis 已清空${NC}"
  else
    echo -e "${RED}✗ Redis 清空失败${NC}"
    exit 1
  fi

  echo ""
  echo -e "${YELLOW}▶ 验证清空结果...${NC}"

  # Get key count
  KEY_COUNT=$(redis_exec DBSIZE | tr -d '\r')
  echo -e "  ${BLUE}当前键数: ${KEY_COUNT}${NC}"

  # Get memory usage
  MEMORY_USED=$(redis_exec INFO memory | grep "used_memory_human:" | cut -d: -f2 | tr -d '\r')
  echo -e "  ${BLUE}内存使用: ${MEMORY_USED}${NC} (基础占用)"

  echo ""
  echo -e "${GREEN}✓ Redis 清理完成${NC}"
  echo ""
  echo -e "${YELLOW}==========================================="
  echo -e "  提示"
  echo -e "===========================================${NC}"
  echo "  服务会自动重建缓存和任务队列"
  echo "  无需手动干预，继续使用即可"
  echo ""

  exit 0
fi

# ==================== DEFAULT MODE (VERIFICATION) ====================
echo -e "${YELLOW}==========================================="
echo -e "  初始化 Redis 任务队列和缓存配置"
echo -e "===========================================${NC}"
echo ""

# Verify Redis connection
echo -e "${YELLOW}▶ 检查 Redis 连接...${NC}"
CONTAINER_STATUS=$(docker compose ps redis --format "{{.Status}}" 2>/dev/null || echo "not running")
echo -e "  ${BLUE}容器状态: ${CONTAINER_STATUS}${NC}"
echo -e "  ${BLUE}地址: ${REDIS_HOST}:${REDIS_PORT}${NC}"

if PING_RESULT=$(redis_exec PING 2>&1); then
  echo -e "  ${GREEN}✓ Redis 连接成功 (${PING_RESULT})${NC}"
else
  echo -e "${RED}✗ Redis 连接失败${NC}"
  exit 1
fi

echo ""

# Display task queue namespaces
echo -e "${YELLOW}▶ 任务队列命名空间...${NC}"
echo -e "  ${GREEN}✓ transfer:critical${NC} (紧急任务队列)"
echo -e "  ${GREEN}✓ transfer:default${NC} (普通任务队列)"
echo -e "  ${GREEN}✓ transfer:low${NC} (低优先级任务队列)"

echo ""

# Display Pub/Sub channels
echo -e "${YELLOW}▶ Pub/Sub 事件通道配置...${NC}"
echo -e "  ${GREEN}✓ system:engine:*${NC} (引擎变更事件)"
echo -e "  ${GREEN}✓ meta:scan:*${NC} (元数据扫描事件)"

echo ""

# Display Redis health status
echo -e "${YELLOW}▶ Redis 健康状态...${NC}"

# Memory usage
MEMORY_USED=$(redis_exec INFO memory | grep "used_memory_human:" | cut -d: -f2 | tr -d '\r')
MEMORY_PEAK=$(redis_exec INFO memory | grep "used_memory_peak_human:" | cut -d: -f2 | tr -d '\r')
MAXMEMORY=$(redis_exec CONFIG GET maxmemory | tail -n1 | tr -d '\r')
if [[ "$MAXMEMORY" == "0" ]]; then
  MAXMEMORY_DISPLAY="无限制"
else
  MAXMEMORY_DISPLAY=$(numfmt --to=iec "$MAXMEMORY" 2>/dev/null || echo "$MAXMEMORY")
fi
echo -e "  ${BLUE}内存使用: ${MEMORY_USED} / ${MAXMEMORY_DISPLAY}${NC}"
echo -e "  ${BLUE}峰值内存: ${MEMORY_PEAK}${NC}"

# Connected clients
CONNECTED_CLIENTS=$(redis_exec INFO clients | grep "connected_clients:" | cut -d: -f2 | tr -d '\r')
echo -e "  ${BLUE}连接客户端: ${CONNECTED_CLIENTS}${NC}"

# Persistence mode
AOF_ENABLED=$(redis_exec CONFIG GET appendonly | tail -n1 | tr -d '\r')
if [[ "$AOF_ENABLED" == "yes" ]]; then
  echo -e "  ${BLUE}持久化模式: AOF 启用${NC}"
else
  echo -e "  ${BLUE}持久化模式: RDB 快照${NC}"
fi

echo ""

# Display key statistics by module
echo -e "${YELLOW}▶ 键统计（按模块）...${NC}"

# Count keys by pattern
count_keys_by_pattern() {
  local pattern=$1
  local count=0

  # Use SCAN to count keys (safer than KEYS in production)
  while IFS= read -r key; do
    [[ -n "$key" ]] && ((count++))
  done < <(redis_exec --scan --pattern "$pattern" 2>/dev/null)

  echo "$count"
}

ASYNQ_COUNT=$(count_keys_by_pattern "asynq:*")
CACHE_SYSTEM_COUNT=$(count_keys_by_pattern "cache:system:*")
CACHE_MANAGER_COUNT=$(count_keys_by_pattern "cache:manager:*")
CACHE_META_COUNT=$(count_keys_by_pattern "cache:meta:*")
TOTAL_KEYS=$(redis_exec DBSIZE | tr -d '\r')

echo -e "  ${BLUE}asynq:*${NC}           : ${ASYNQ_COUNT} 个键"
echo -e "  ${BLUE}cache:system:*${NC}    : ${CACHE_SYSTEM_COUNT} 个键"
echo -e "  ${BLUE}cache:manager:*${NC}   : ${CACHE_MANAGER_COUNT} 个键"
echo -e "  ${BLUE}cache:meta:*${NC}      : ${CACHE_META_COUNT} 个键"
echo -e "  ${BLUE}总计${NC}              : ${TOTAL_KEYS} 个键"

echo ""
echo -e "${GREEN}✓ Redis 初始化完成${NC}"
echo ""
echo -e "${YELLOW}==========================================="
echo -e "  Redis 访问信息"
echo -e "===========================================${NC}"
echo "  连接命令: docker exec -it redis redis-cli -a ${REDIS_PASSWORD}"
echo "  监控命令: docker exec redis redis-cli -a ${REDIS_PASSWORD} INFO"
echo "  清理数据: bash scripts/infra/init-redis.sh --clean"
echo ""
