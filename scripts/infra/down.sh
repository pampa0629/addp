#!/usr/bin/env bash

# ADDP Infrastructure Down Script
# 停止系统库容器（PostgreSQL、Redis、MinIO）

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
Usage: bash scripts/infra/down.sh [-v|--volumes]

Options:
  -v, --volumes   同时删除数据卷（警告：会删除所有数据）

说明：
  默认执行 docker compose down：停止并删除容器、网络，但保留数据卷。
  使用 -v 或 --volumes 会同时删除数据卷（PostgreSQL、Redis、MinIO、Meilisearch 的所有数据将丢失）。
EOF
}

REMOVE_VOLUMES=false
if [[ ${1:-} == "-h" || ${1:-} == "--help" ]]; then
  usage; exit 0
elif [[ ${1:-} == "-v" || ${1:-} == "--volumes" ]]; then
  REMOVE_VOLUMES=true
fi

if ! command -v docker >/dev/null 2>&1; then
  echo -e "${RED}✗ docker 未安装或不可用${NC}"; exit 1
fi
if ! docker compose version >/dev/null 2>&1; then
  echo -e "${RED}✗ docker compose 不可用${NC}"; exit 1
fi

if [[ "$REMOVE_VOLUMES" == true ]]; then
  echo -e "${RED}⚠️  警告：即将删除所有数据卷，所有数据将丢失！${NC}"
  echo -e "${YELLOW}▶ 停止并删除基础设施容器和数据卷${NC}"
  docker compose -f docker-compose.infra.yml down -v
else
  echo -e "${YELLOW}▶ 停止并删除基础设施容器（保留数据卷）${NC}"
  docker compose -f docker-compose.infra.yml down
fi

echo -e "${GREEN}✓ 完成${NC}"

