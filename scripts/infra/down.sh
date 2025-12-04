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
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${PROJECT_ROOT}"

usage() {
  cat <<EOF
Usage: bash scripts/infra/down.sh [--rm]

Options:
  --rm   在停止后移除对应容器（不删除命名卷与数据）

说明：
  默认仅执行 stop：postgres redis minio。
  使用 --rm 追加 docker compose rm -f <服务> 以清理容器。
EOF
}

REMOVE=false
if [[ ${1:-} == "-h" || ${1:-} == "--help" ]]; then
  usage; exit 0
elif [[ ${1:-} == "--rm" ]]; then
  REMOVE=true
fi

if ! command -v docker >/dev/null 2>&1; then
  echo -e "${RED}✗ docker 未安装或不可用${NC}"; exit 1
fi
if ! docker compose version >/dev/null 2>&1; then
  echo -e "${RED}✗ docker compose 不可用${NC}"; exit 1
fi

echo -e "${YELLOW}▶ 停止基础设施容器: postgres, redis, minio${NC}"
docker compose stop postgres redis minio

if [[ "$REMOVE" == true ]]; then
  echo -e "${YELLOW}▶ 清理容器（不删除数据卷）${NC}"
  docker compose rm -f postgres redis minio || true
fi

echo -e "${GREEN}✓ 完成${NC}"

