#!/usr/bin/env bash

# ADDP Infrastructure MinIO Initialization
# 初始化 MinIO buckets（按模块组织）
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

# MinIO 连接配置（系统 MinIO）
MINIO_ENDPOINT="${MINIO_API_PORT:-19000}"
MINIO_ACCESS_KEY="${MINIO_ROOT_USER:-minioadmin}"
MINIO_SECRET_KEY="${MINIO_ROOT_PASSWORD:-minioadmin}"

if ! command -v docker >/dev/null 2>&1; then
  echo -e "${RED}✗ docker 未安装或不可用${NC}"
  exit 1
fi

if ! docker compose version >/dev/null 2>&1; then
  echo -e "${RED}✗ docker compose 不可用${NC}"
  exit 1
fi

# 确认 MinIO 容器正在运行
if ! docker compose -f docker-compose.infra.yml ps --status running minio >/dev/null 2>&1; then
  echo -e "${RED}✗ MinIO 容器未运行，无法初始化 buckets${NC}"
  echo -e "${YELLOW}  请先执行: bash scripts/infra/up.sh${NC}"
  exit 1
fi

echo -e "${YELLOW}========================================${NC}"
echo -e "${YELLOW}MinIO Buckets 初始化（模块化组织）${NC}"
echo -e "${YELLOW}========================================${NC}"
echo ""

# 配置 MinIO alias（使用容器内的 mc 命令）
# 注意：容器内部使用 9000 端口
echo -e "${YELLOW}▶ 配置 MinIO 连接...${NC}"
if ! docker compose -f docker-compose.infra.yml exec -T minio mc alias set local "http://localhost:9000" "${MINIO_ACCESS_KEY}" "${MINIO_SECRET_KEY}" >/dev/null 2>&1; then
  echo -e "${RED}✗ MinIO 连接配置失败${NC}"
  exit 1
fi
echo -e "  ${GREEN}✓ MinIO 连接成功${NC}"
echo ""

# 创建模块化 buckets（幂等操作，已存在不报错）
echo -e "${YELLOW}▶ 创建模块化 Buckets...${NC}"
BUCKETS=(
  "system:System 模块（用户头像、系统配置）"
  "manager:Manager 模块（预览缓存、MVT 瓦片）"
  "meta:Meta 模块（元数据相关文件）"
  "transfer:Transfer 模块（传输临时文件）"
  "orchestrator:Orchestrator 模块（编排文件）"
  "develop:Develop 模块（查询结果导出）"
  "service:Service 模块（服务注册文件）"
  "graph:Graph 模块（知识图谱构建材料）"
)

for bucket_info in "${BUCKETS[@]}"; do
  IFS=':' read -r bucket_name bucket_desc <<< "$bucket_info"

  # 检查 bucket 是否存在
  if docker compose -f docker-compose.infra.yml exec -T minio mc ls "local/${bucket_name}" >/dev/null 2>&1; then
    echo -e "  ${GREEN}✓ ${bucket_name}${NC} - 已存在"
  else
    echo -e "  ${BLUE}▸ ${bucket_name}${NC} - 创建中..."
    if docker compose -f docker-compose.infra.yml exec -T minio mc mb "local/${bucket_name}" >/dev/null 2>&1; then
      echo -e "  ${GREEN}✓ ${bucket_name}${NC} - 创建成功"
    else
      echo -e "  ${RED}✗ ${bucket_name}${NC} - 创建失败"
      exit 1
    fi
  fi
done

echo ""
echo -e "${YELLOW}▶ 设置 Bucket 访问策略...${NC}"

# 设置 manager bucket 公开读（MVT 瓦片需要前端直接访问）
echo -e "  ${BLUE}▸ manager - 设置为公开读（MVT 瓦片访问）${NC}"
if docker compose -f docker-compose.infra.yml exec -T minio mc anonymous set download "local/manager" >/dev/null 2>&1; then
  echo -e "  ${GREEN}✓ manager - 访问策略设置完成${NC}"
else
  echo -e "  ${YELLOW}⚠️  manager - 访问策略设置失败（可能已设置）${NC}"
fi

echo ""

# 设置 manager 桶 temp/ 前缀的 Lifecycle Policy（7天后自动过期删除）
echo -e "${YELLOW}▶ 配置 manager 桶 Lifecycle Policy（临时文件 7 天后自动清理）...${NC}"
if docker compose -f docker-compose.infra.yml exec -T minio mc ilm rule add \
  --prefix "temp/" \
  --expire-days 7 \
  "local/manager" >/dev/null 2>&1; then
  echo -e "  ${GREEN}✓ manager:temp/ 的 7 天过期策略设置成功${NC}"
else
  echo -e "  ${YELLOW}⚠️  manager Lifecycle 策略设置失败（可能已存在）${NC}"
fi

echo ""
echo -e "${GREEN}✓ MinIO Buckets 初始化完成${NC}"
echo ""
echo -e "${YELLOW}========================================${NC}"
echo -e "${YELLOW}Bucket 访问信息${NC}"
echo -e "${YELLOW}========================================${NC}"
echo "  访问地址: http://localhost:${MINIO_ENDPOINT}"
echo "  控制台:   http://localhost:$(($MINIO_ENDPOINT + 1))"
echo ""
echo "  已创建的 Buckets:"
echo "  - system        (私有)  : 用户头像、系统配置"
echo "  - manager       (公开读): 预览缓存、MVT 瓦片、导入临时文件（temp/ 7天后自动过期）"
echo "  - meta          (私有)  : 元数据文件"
echo "  - transfer      (私有)  : 传输临时文件"
echo "  - orchestrator  (私有)  : 编排文件"
echo "  - develop       (私有)  : 查询结果导出"
echo "  - service       (私有)  : 服务注册文件"
echo "  - graph         (私有)  : 知识图谱构建材料"
echo ""
