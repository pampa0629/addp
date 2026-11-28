#!/usr/bin/env bash

# ADDP platform object-store initialization
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

COMPOSE_FILE="docker-compose.infra.yml"
if [ "$#" -gt 0 ]; then
  [ "$#" -eq 2 ] && [ "$1" = --compose-file ] && [[ "$2" = /* ]] && [ -f "$2" ] || {
    echo 'Usage: init-minio.sh [--compose-file /absolute/owned-compose.json]' >&2
    exit 1
  }
  COMPOSE_FILE="$2"
fi

compose_store() {
  docker compose "${COMPOSE_FILES[@]}" "$@"
}
store_client() {
  compose_store exec -T "$objectstore_service" "$objectstore_client" "$@"
}

# Optional: load env overrides if present
if [ "${ADDP_INFRA_RESOLVED:-0}" != "1" ] && [ -f ./.env ]; then
  set -a
  # shellcheck disable=SC1091
  source ./.env || true
  set +a
fi

# 平台 S3 连接配置
MINIO_ENDPOINT="${MINIO_API_PORT:-19000}"
MINIO_ACCESS_KEY="${MINIO_ROOT_USER:-minioadmin}"
MINIO_SECRET_KEY="${MINIO_ROOT_PASSWORD:-minioadmin}"

# Keep the deployment binding in sync with common/infra/objectstore/bootstrap.OpenObjectArea.
source "$SCRIPT_DIR/platform-objectstore.sh"
addp_select_objectstore
if [[ "$COMPOSE_FILE" == docker-compose.infra.yml ]]; then
  addp_objectstore_compose_files
else
  COMPOSE_FILES=(-f "$COMPOSE_FILE")
fi
standard_bucket=standard
develop_bucket=develop
if [[ -n ${ADDP_INFRA_OBJECTSTORE_BINDINGS:-} ]]; then
  declare -a binding_entries
  IFS=',' read -r -a binding_entries <<< "$ADDP_INFRA_OBJECTSTORE_BINDINGS"
  [[ $ADDP_INFRA_OBJECTSTORE_BINDINGS != *, ]] || { echo 'Empty object binding' >&2; exit 1; }
  seen_standard=0
  seen_develop=0
  for entry in "${binding_entries[@]}"; do
    name=${entry%%=*}
    target=${entry#*=}
    case "$name" in
      standard) [[ $seen_standard == 0 ]] || { echo "Duplicate object binding: $name" >&2; exit 1; }; seen_standard=1 ;;
      develop) [[ $seen_develop == 0 ]] || { echo "Duplicate object binding: $name" >&2; exit 1; }; seen_develop=1 ;;
      *) echo "Unsupported object area: $name" >&2; exit 1 ;;
    esac
    if [[ $entry != *=* || ! $target =~ ^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$ || $target =~ ^([0-9]+\.){3}[0-9]+$ ||
          $target == *..* || $target == *.-* || $target == *-.* ]]; then
      echo "Invalid object store bucket: $target" >&2; exit 1
    fi
    case "$name" in standard) standard_bucket=$target ;; develop) develop_bucket=$target ;; esac
  done
fi

if ! command -v docker >/dev/null 2>&1; then
  echo -e "${RED}✗ docker 未安装或不可用${NC}"
  exit 1
fi

if ! docker compose version >/dev/null 2>&1; then
  echo -e "${RED}✗ docker compose 不可用${NC}"
  exit 1
fi

# 确认当前 Compose 项目已启动选定的服务
if ! compose_store ps --status running "$objectstore_service" >/dev/null 2>&1; then
  echo -e "${RED}✗ ${objectstore_service} 容器未运行，无法初始化 buckets${NC}"
  echo -e "${YELLOW}  请先执行: bash scripts/infra/up.sh${NC}"
  exit 1
fi

echo -e "${YELLOW}========================================${NC}"
echo -e "${YELLOW}${objectstore_service} Buckets 初始化（模块化组织）${NC}"
echo -e "${YELLOW}========================================${NC}"
echo ""

# 在所选服务内配置部署客户端的连接
# 注意：容器内部使用 9000 端口
echo -e "${YELLOW}▶ 配置 ${objectstore_service} 连接...${NC}"
if ! store_client alias set local "http://localhost:9000" "${MINIO_ACCESS_KEY}" "${MINIO_SECRET_KEY}" >/dev/null 2>&1; then
  echo -e "${RED}✗ ${objectstore_service} 连接配置失败${NC}"
  exit 1
fi
echo -e "  ${GREEN}✓ ${objectstore_service} 连接成功${NC}"
echo ""

# 创建模块化 buckets（幂等操作，已存在不报错）
echo -e "${YELLOW}▶ 创建模块化 Buckets...${NC}"
BUCKETS=(
  "${standard_bucket}:Standard 文档文件"
  "system:System 模块（用户头像、系统配置、审计日志归档）"
  "manager:Manager 模块（预览缓存、MVT 瓦片）"
  "meta:Meta 模块（元数据相关文件）"
  "transfer:Transfer 模块（传输临时文件）"
  "orchestrator:Orchestrator 模块（编排文件）"
  "${develop_bucket}:Develop Notebook 文件"
  "service:Service 模块（服务注册文件）"
  "graph:Graph 模块（知识图谱构建材料）"
)

for bucket_info in "${BUCKETS[@]}"; do
  IFS=':' read -r bucket_name bucket_desc <<< "$bucket_info"

  # 检查 bucket 是否存在
  if store_client ls "local/${bucket_name}" >/dev/null 2>&1; then
    echo -e "  ${GREEN}✓ ${bucket_name}${NC} - 已存在"
  else
    echo -e "  ${BLUE}▸ ${bucket_name}${NC} - 创建中..."
    if store_client mb "local/${bucket_name}" >/dev/null 2>&1; then
      echo -e "  ${GREEN}✓ ${bucket_name}${NC} - 创建成功"
    else
      echo -e "  ${RED}✗ ${bucket_name}${NC} - 创建失败"
      exit 1
    fi
  fi
done

echo ""
echo -e "${YELLOW}▶ 设置 Bucket 访问策略...${NC}"

# Document and Notebook buckets remain private, including deployment exceptions.
for private_bucket in "$standard_bucket" "$develop_bucket"; do
  if ! store_client anonymous set private "local/$private_bucket" >/dev/null 2>&1; then
    echo "Failed to set private object bucket: $private_bucket" >&2
    exit 1
  fi
done

# Manager 产物统一通过 Manager API 访问，bucket 保持私有。
echo -e "  ${BLUE}▸ manager - 设置为私有（预览缓存、瓦片缓存对象通过 Manager API 访问）${NC}"
if store_client anonymous set private "local/manager" >/dev/null 2>&1; then
  echo -e "  ${GREEN}✓ manager - 访问策略设置完成${NC}"
else
  echo -e "  ${YELLOW}⚠️  manager - 访问策略设置失败（可能已设置）${NC}"
fi

echo ""

# 设置 manager 桶 temp/ 前缀的 Lifecycle Policy（7天后自动过期删除）
echo -e "${YELLOW}▶ 配置 manager 桶 Lifecycle Policy（临时文件 7 天后自动清理）...${NC}"
if [[ "$objectstore_service" == rustfs ]]; then
  expiry_flag=--expiry-days
else
  expiry_flag=--expire-days
fi
if store_client ilm rule add \
  --prefix "temp/" \
  "$expiry_flag" 7 \
  "local/manager" >/dev/null 2>&1; then
  echo -e "  ${GREEN}✓ manager:temp/ 的 7 天过期策略设置成功${NC}"
else
  echo -e "  ${YELLOW}⚠️  manager Lifecycle 策略设置失败（可能已存在）${NC}"
fi

echo ""
echo -e "${GREEN}✓ ${objectstore_service} Buckets 初始化完成${NC}"
echo ""
echo -e "${YELLOW}========================================${NC}"
echo -e "${YELLOW}Bucket 访问信息${NC}"
echo -e "${YELLOW}========================================${NC}"
echo "  访问地址: http://localhost:${MINIO_ENDPOINT}"
echo "  控制台:   http://localhost:${MINIO_CONSOLE_PORT:-19001}"
echo ""
echo "  已创建的 Buckets:"
echo "  - ${standard_bucket} (私有)  : Standard 文档文件"
echo "  - system        (私有)  : 用户头像、系统配置、审计日志归档"
echo "  - manager       (私有)  : 预览缓存、瓦片缓存对象、导入临时文件（temp/ 7天后自动过期）"
echo "  - meta          (私有)  : 元数据文件"
echo "  - transfer      (私有)  : 传输临时文件"
echo "  - orchestrator  (私有)  : 编排文件"
echo "  - ${develop_bucket} (私有) : Notebook 文件"
echo "  - service       (私有)  : 服务注册文件"
echo "  - graph         (私有)  : 知识图谱构建材料"
echo ""
