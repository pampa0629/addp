#!/usr/bin/env bash

# ADDP Infrastructure MinIO Initialization
# 初始化 MinIO buckets（包括 MVT 瓦片缓存等）
# 由 infra-up.sh 自动调用

set -euo pipefail

BLUE='\033[0;34m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${PROJECT_ROOT}"

# Optional: load env overrides if present
if [ -f ./.env ]; then
  set -a
  # shellcheck disable=SC1091
  source ./.env || true
  set +a
fi

MINIO_ENDPOINT="${BUSINESS_MINIO_ENDPOINT:-localhost:9002}"
MINIO_ACCESS_KEY="${BUSINESS_MINIO_ACCESS_KEY:-minioadmin}"
MINIO_SECRET_KEY="${BUSINESS_MINIO_SECRET_KEY:-minioadmin}"

if ! command -v docker >/dev/null 2>&1; then
  echo -e "${RED}✗ docker 未安装或不可用${NC}"
  exit 1
fi

if ! docker compose version >/dev/null 2>&1; then
  echo -e "${RED}✗ docker compose 不可用${NC}"
  exit 1
fi

# 确认 MinIO 容器正在运行
if ! docker compose ps --status running minio >/dev/null 2>&1; then
  echo -e "${RED}✗ MinIO 容器未运行，无法初始化 buckets${NC}"
  echo -e "${YELLOW}  请先执行: bash scripts/infra-up.sh${NC}"
  exit 1
fi

echo -e "${YELLOW}▶ 初始化 MinIO buckets${NC}"

# 配置 MinIO alias（使用容器内的 mc 命令）
# 注意：容器内部使用 9000 端口，而不是宿主机映射的 9002
echo -e "  ${BLUE}配置 MinIO 连接...${NC}"
if ! docker compose exec -T minio mc alias set local "http://localhost:9000" "${MINIO_ACCESS_KEY}" "${MINIO_SECRET_KEY}" >/dev/null 2>&1; then
  echo -e "${RED}✗ MinIO 连接配置失败${NC}"
  exit 1
fi

# 创建 buckets（幂等操作，已存在不报错）
BUCKETS=(
  "mvt-tiles:MVT 瓦片缓存（Meta模块空间快显）"
  "addp-data:通用数据存储"
)

for bucket_info in "${BUCKETS[@]}"; do
  IFS=':' read -r bucket_name bucket_desc <<< "$bucket_info"

  # 检查 bucket 是否存在
  if docker compose exec -T minio mc ls "local/${bucket_name}" >/dev/null 2>&1; then
    echo -e "  ${GREEN}✓ Bucket '${bucket_name}' 已存在${NC} (${bucket_desc})"
  else
    echo -e "  ${BLUE}创建 Bucket '${bucket_name}'...${NC} (${bucket_desc})"
    if docker compose exec -T minio mc mb "local/${bucket_name}" >/dev/null 2>&1; then
      echo -e "  ${GREEN}✓ Bucket '${bucket_name}' 创建成功${NC}"
    else
      echo -e "  ${RED}✗ Bucket '${bucket_name}' 创建失败${NC}"
      exit 1
    fi
  fi
done

# 设置 mvt-tiles 为公开读（前端需要直接访问瓦片）
echo -e "  ${BLUE}设置 mvt-tiles 访问策略为公开读...${NC}"
if docker compose exec -T minio mc anonymous set download "local/mvt-tiles" >/dev/null 2>&1; then
  echo -e "  ${GREEN}✓ mvt-tiles 访问策略设置完成${NC}"
else
  echo -e "  ${YELLOW}⚠️  mvt-tiles 访问策略设置失败（可能已设置）${NC}"
fi

echo -e "${GREEN}✓ MinIO buckets 初始化完成${NC}"
echo ""
echo "已创建的 Buckets:"
echo "  - mvt-tiles:  http://${MINIO_ENDPOINT}/mvt-tiles  (公开读)"
echo "  - addp-data:  http://${MINIO_ENDPOINT}/addp-data  (私有)"
echo ""
