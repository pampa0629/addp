#!/usr/bin/env bash

# ADDP Infrastructure Down Script
# 停止 ADDP 系统基础设施容器
# 不影响 business 容器

set -euo pipefail

BLUE='\033[0;34m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${PROJECT_ROOT}"

usage() {
  cat <<EOF
Usage: bash scripts/infra/down.sh [-v|--volumes] [--force]

Options:
  -v, --volumes   同时删除数据卷（警告：会删除所有数据）
  --force         跳过容器验证确认（用于自动化脚本）

说明：
  默认停止并删除容器、网络，但保留数据卷。
  使用 -v 或 --volumes 会同时删除数据卷（PostgreSQL、Redis、MinIO、Meilisearch、Kafka 的所有数据将丢失）。

职责范围：
  仅停止 addp-* 容器（包括 addp-postgres、addp-redis、addp-minio、addp-meilisearch、addp-kafka、addp-kafka-init、addp-kafka-connect）
  不影响 business-* 容器
EOF
}

REMOVE_VOLUMES=false
FORCE_DOWN=0

# 解析参数
while [[ $# -gt 0 ]]; do
  case $1 in
    -h|--help)
      usage; exit 0
      ;;
    -v|--volumes)
      REMOVE_VOLUMES=true
      shift
      ;;
    --force)
      FORCE_DOWN=1
      shift
      ;;
    *)
      echo -e "${RED}未知参数: $1${NC}"
      usage; exit 1
      ;;
  esac
done

if ! command -v docker >/dev/null 2>&1; then
  echo -e "${RED}✗ docker 未安装或不可用${NC}"; exit 1
fi
if ! docker compose version >/dev/null 2>&1; then
  echo -e "${RED}✗ docker compose 不可用${NC}"; exit 1
fi

# 验证将要删除的容器范围
echo -e "${YELLOW}▶ 检查即将停止的容器...${NC}"

CONTAINERS_TO_REMOVE=$(docker compose -f docker-compose.infra.yml ps -a --format "{{.Name}}" 2>/dev/null || true)

if [ -n "$CONTAINERS_TO_REMOVE" ]; then
  echo -e "${BLUE}将停止并删除以下容器:${NC}"
  echo "$CONTAINERS_TO_REMOVE" | while read -r container; do
    echo "  - $container"
  done
  echo ""

  # 检查是否包含非 addp-* 容器
  NON_ADDP_CONTAINERS=$(echo "$CONTAINERS_TO_REMOVE" | grep -v "^addp-" || true)

  if [ -n "$NON_ADDP_CONTAINERS" ]; then
    echo -e "${RED}⚠️  警告：检测到非 ADDP 系统容器！${NC}"
    echo -e "${YELLOW}  预期容器：docker-compose.infra.yml 中定义的 addp-* 基础设施容器${NC}"
    echo -e "${YELLOW}  检测到的异常容器：${NC}"
    echo "$NON_ADDP_CONTAINERS" | while read -r container; do
      echo "    - $container"
    done
    echo ""
    echo -e "${YELLOW}  这可能表明 Docker Compose 项目隔离存在问题。${NC}"
    echo -e "${YELLOW}  建议检查 docker-compose.infra.yml 和 business/docker-compose.yml 配置。${NC}"
    echo ""

    if [[ "$FORCE_DOWN" != "1" ]]; then
      read -p "是否继续？(yes/no): " confirm
      if [[ "$confirm" != "yes" ]]; then
        echo -e "${YELLOW}✗ 操作已取消${NC}"
        exit 0
      fi
    fi
  fi
else
  echo -e "${YELLOW}  没有检测到运行中的基础设施容器${NC}"
fi

echo ""

# 执行停止操作
if [[ "$REMOVE_VOLUMES" == true ]]; then
  echo -e "${RED}⚠️  警告：即将删除所有数据卷，所有数据将丢失！${NC}"
  echo -e "${YELLOW}▶ 停止并删除基础设施容器和数据卷${NC}"
  docker compose -f docker-compose.infra.yml down -v
else
  echo -e "${YELLOW}▶ 停止并删除基础设施容器（保留数据卷）${NC}"
  docker compose -f docker-compose.infra.yml down
fi

echo -e "${GREEN}✓ 完成${NC}"
