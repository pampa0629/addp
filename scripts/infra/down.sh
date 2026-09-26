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
  --force         跳过非 ADDP 容器确认；不会跳过数据卷删除确认

说明：
  默认停止并删除容器、网络，但保留数据卷。
  使用 -v 或 --volumes 会同时删除数据卷（PostgreSQL、Redis、FalkorDB、MinIO、Meilisearch、Kafka 的所有数据将丢失）。

职责范围：
  仅停止 addp-* 容器（包括 addp-postgres、addp-redis、addp-falkordb、addp-minio、addp-meilisearch、addp-redpanda、addp-redpanda-init、addp-kafka-connect）
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

COMPOSE_FILES=(-f docker-compose.infra.yml)

compose() {
  docker compose "${COMPOSE_FILES[@]}" "$@"
}

if ! command -v docker >/dev/null 2>&1; then
  echo -e "${RED}✗ docker 未安装或不可用${NC}"; exit 1
fi
if ! docker compose version >/dev/null 2>&1; then
  echo -e "${RED}✗ docker compose 不可用${NC}"; exit 1
fi

# 删除数据卷是不可逆操作。只有经过独立准入的 Online 一次性 Runner 可无交互清理；
# 本地 --force 仅影响容器名称检查，不能授权非交互删除开发数据。
if [[ "$REMOVE_VOLUMES" == true ]]; then
  if [[ "${GITHUB_ACTIONS:-}" == true && "${ADDP_ONLINE_HOST:-}" == 1 &&
        "${ADDP_ONLINE_TEST:-}" == 1 && "${POSTGRES_DB:-}" == addp_online &&
        ( "${ADDP_ONLINE_HOSTED:-}" == 1 || "${ADDP_ONLINE_OWNER_MANAGED:-}" == 1 ) &&
        ! -e ./.env ]]; then
    :
  else
    if [[ ! -t 0 ]]; then
      echo -e "${RED}✗ 本地非交互环境禁止删除 addp-infra 数据卷${NC}" >&2
      exit 1
    fi
    read -r -p '输入 DELETE addp-infra VOLUMES 确认删除全部基础设施数据卷: ' confirm || exit 1
    if [[ "$confirm" != 'DELETE addp-infra VOLUMES' ]]; then
      echo -e "${YELLOW}✗ 操作已取消${NC}" >&2
      exit 1
    fi
  fi
fi

# 验证将要删除的容器范围
echo -e "${YELLOW}▶ 检查即将停止的容器...${NC}"

CONTAINERS_TO_REMOVE=$(compose ps -a --format "{{.Name}}" 2>/dev/null || true)

if [ -n "$CONTAINERS_TO_REMOVE" ]; then
  while IFS= read -r container; do
    [[ -n "$container" ]] || continue
    owner_dir=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project.working_dir"}}' "$container" 2>/dev/null) || {
      echo -e "${RED}✗ 无法验证容器 $container 的 Compose 工作目录${NC}" >&2
      exit 1
    }
    if [[ "$owner_dir" != "$PROJECT_ROOT" ]]; then
      echo -e "${RED}✗ 容器 $container 属于其他工作区：$owner_dir${NC}" >&2
      exit 1
    fi
  done <<< "$CONTAINERS_TO_REMOVE"

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
  compose down -v
else
  echo -e "${YELLOW}▶ 停止并删除基础设施容器（保留数据卷）${NC}"
  compose down
fi

echo -e "${GREEN}✓ 完成${NC}"
