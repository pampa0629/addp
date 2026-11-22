#!/bin/bash
# ADDP 生产环境启动脚本
# 用途：按正确顺序启动所有 ADDP 服务（基础设施 + 后端 + 前端 + Console）

set -euo pipefail

prepare_databases=false
case "$#:${1:-}" in
  0:) ;;
  1:--prepare-databases) prepare_databases=true ;;
  *) echo 'Usage: scripts/prod/start.sh [--prepare-databases]' >&2; exit 2 ;;
esac

# 颜色定义
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}ADDP 生产环境启动脚本${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""

PLATFORM_COMPOSE_FILES=(-f docker-compose.yml)
RUNTIME_COMPOSE_FILES=(-f docker-compose.runtimes.yml)
platform_compose() { docker compose "${PLATFORM_COMPOSE_FILES[@]}" "$@"; }
runtime_compose() { docker compose "${RUNTIME_COMPOSE_FILES[@]}" -f "$ADDP_RELEASE_DIR/runtimes-images.yml" "$@"; }

connect_business_network() {
  local network="${BUSINESS_DOCKER_NETWORK:-business_business-network}"

  if ! docker network inspect "$network" > /dev/null 2>&1; then
    echo -e "${YELLOW}ℹ️  未检测到 Business 网络 ${network}，跳过业务引擎直连网络配置${NC}"
    return 0
  fi

  echo -e "${YELLOW}连接 ADDP 服务到 Business 网络 (${network})...${NC}"
  for container in $(platform_compose ps -q 2>/dev/null) $(runtime_compose ps -q 2>/dev/null); do
    if docker inspect "$container" --format '{{json .NetworkSettings.Networks}}' | grep -q "\"${network}\""; then
      continue
    fi

    docker network connect "$network" "$container" > /dev/null 2>&1 || true
  done
  echo -e "${GREEN}✓ Business 网络连接已处理${NC}"
}

# 检查 docker 是否运行
if ! docker info > /dev/null 2>&1; then
    echo -e "${RED}错误: Docker 未运行${NC}"
    exit 1
fi

PROJECT_ROOT=$(pwd)
ROOT_DIR="$PROJECT_ROOT"
source scripts/dev/lifecycle-lock.sh
addp_acquire_lifecycle_lock prod-start "$@"

# 初始化 .env（若不存在则从 .env.example 自动生成随机密钥）
bash scripts/prod/setup-env.sh

# 第一步：启动基础设施层
echo -e "${YELLOW}[1/4] 启动基础设施层...${NC}"
source scripts/utils/observability-env.sh
addp_prepare_observability_env "$PROJECT_ROOT/.env"
export ADDP_RELEASE_MANIFEST
ADDP_RELEASE_MANIFEST="$(python3 scripts/prod/release-images.py verify)"
export ADDP_RELEASE_DIR="$(dirname "$ADDP_RELEASE_MANIFEST")"
if [[ "${ADDP_DEPLOYMENT_SCOPE:-full}" == system-meta-gateway ]]; then
  PLATFORM_COMPOSE_FILES=(-f scripts/prod/system-meta-gateway.yml)
fi
PLATFORM_COMPOSE_FILES+=(-f "$ADDP_RELEASE_DIR/platform-images.yml")
infra_result=0
export ADDP_DEPLOYMENT_DISCOVERY_DIR="${ADDP_DEPLOYMENT_DISCOVERY_DIR:-$PROJECT_ROOT/.dev-state/deployment-discovery}"
if [[ "${ADDP_DEPLOYMENT_SCOPE:-full}" == full ]]; then
  if ! bash scripts/infra/up.sh; then
    source scripts/infra/ports.sh
    addp_infra_ready core || exit 1
    infra_result=1
    echo 'Optional observability startup failed; core services are ready' >&2
  fi
  bash scripts/prod/wait-infra.sh
  if addp_metrics_enabled; then
    if addp_metrics_platform_preflight; then
      PLATFORM_COMPOSE_FILES+=(-f scripts/prod/metrics-platform.yml)
      if python3 scripts/prod/generate-process-metrics-platform.py; then
        PLATFORM_COMPOSE_FILES+=(-f "$ADDP_METRICS_DEPLOYMENT_DIR/platform-process-metrics.json")
      else
        infra_result=1
        echo 'Process metrics/evidence deployment unconfigured; business startup remains independent' >&2
      fi
    else
      infra_result=1
      echo 'Monitor metrics admission unconfigured; business startup remains independent' >&2
    fi
  fi
  if addp_metrics_enabled && [[ -n "${MONITOR_PROMETHEUS_URL:-}" ]]; then
    if addp_metrics_query_preflight; then
      PLATFORM_COMPOSE_FILES+=(-f scripts/prod/metrics-query.yml)
    else
      infra_result=1
      echo 'Monitor metrics query unconfigured; business startup remains independent' >&2
    fi
  fi
  if [[ -n "${GATEWAY_METRICS_CONTROL_LISTEN:-}" ]]; then
    if [[ -n "${GATEWAY_METRICS_CONTROL_SERVER_NAME:-}" && "${GATEWAY_METRICS_CONTROL_TLS_DIR:-}" == /* && -d "$GATEWAY_METRICS_CONTROL_TLS_DIR" ]]; then
      PLATFORM_COMPOSE_FILES+=(-f scripts/prod/gateway-metrics-control.yml)
    else
      infra_result=1
      echo 'Gateway metrics control deployment unconfigured; business startup remains independent' >&2
    fi
  fi
  if addp_traces_enabled; then
    if python3 scripts/prod/generate-trace-platform.py; then
      PLATFORM_COMPOSE_FILES+=(-f "$ADDP_TRACE_DEPLOYMENT_DIR/platform-traces.json")
    else
      infra_result=1
      echo 'Trace producer/query deployment unconfigured; business startup remains independent' >&2
    fi
  fi
  # Containers use the fixed internal query endpoint; native development uses the resolved host mapping.
  export LOKI_URL=''
  if addp_runtime_logs_enabled; then export LOKI_URL=http://runtime-log-api:3100; fi
else
  docker network inspect "${ADDP_DEPLOYMENT_NETWORK:?}" > /dev/null
fi

# Publish desired services from the same Compose files, independently of metrics.
intent_compose_args=()
for ((intent_index=1; intent_index<${#PLATFORM_COMPOSE_FILES[@]}; intent_index+=2)); do
  intent_compose_args+=(--compose-file "${PLATFORM_COMPOSE_FILES[$intent_index]}")
done
if [[ "${ADDP_DEPLOYMENT_SCOPE:-full}" == full ]]; then
  if python3 scripts/utils/deployment-inventory.py service-expectations --state running "${intent_compose_args[@]}"; then
    PLATFORM_COMPOSE_FILES+=(-f "$ADDP_DEPLOYMENT_DISCOVERY_DIR/platform-service-expectations.json")
  else
    infra_result=1
    echo 'Service expectations unavailable; business startup continues' >&2
  fi
fi

# The same deployment identity prepares every module in the selected instance.
prepare_database() {
  local service=$1
  shift
  platform_compose run --rm -T --no-deps --entrypoint /app/server \
    -e ADDP_DATABASE_DEPLOY_USER -e ADDP_DATABASE_DEPLOY_PASSWORD "$service" prepare-database "$@"
}
if [[ "$prepare_databases" == true ]]; then
  prepare_database system-backend system
  prepare_database system-backend common
fi

# 第二步：System 就绪后由部署入口发布声明
echo -e "${YELLOW}[2/4] 等待 System 就绪并发布模块声明...${NC}"
python3 scripts/prod/publish-module-declarations.py --bootstrap-compose "${intent_compose_args[@]}"

# Prepare downstream module schemas only during deployment or upgrade.
if [[ "$prepare_databases" == true ]]; then
  prepare_database meta-backend
  prepare_database gateway
fi

echo -e "${YELLOW}[3/4] 启动并等待平台服务就绪...${NC}"
platform_compose up -d --no-build --pull never --wait --wait-timeout 180

if [[ "${ADDP_DEPLOYMENT_SCOPE:-full}" == system-meta-gateway ]]; then
  platform_compose ps
  exit 0
fi

echo -e "${YELLOW}[4/4] 启动并等待内置 Runtime 就绪...${NC}"
if [[ -n "${ADDP_GEOPYTHON_METRICS_ENDPOINT:-}" ]]; then
  if python3 - <<'PYGEO'
import ipaddress
import os
import re
from pathlib import Path

try:
    listener = re.fullmatch(r'(\[[0-9a-f:]+\]|[0-9.]+):([1-9][0-9]{0,4})', os.environ['ADDP_GEOPYTHON_METRICS_LISTEN'])
    if not listener:
        raise ValueError
    address = ipaddress.ip_address(listener[1].strip('[]'))
    port = int(os.environ['ADDP_GEOPYTHON_METRICS_CONTAINER_PORT'])
    tls = Path(os.environ['ADDP_GEOPYTHON_METRICS_TLS_DIR'])
    if (not (address.is_private or address.is_loopback) or address.is_unspecified or address.is_link_local
        or address.is_multicast or not (1 <= port <= 65535 and int(listener[2]) <= 65535)
        or not tls.is_absolute() or not tls.is_dir() or tls.is_symlink()
        or tls.resolve().is_relative_to(Path.cwd())):
        raise ValueError
except (KeyError, ValueError, OSError):
    raise SystemExit(1) from None
PYGEO
  then
    RUNTIME_COMPOSE_FILES+=(-f scripts/prod/geopython-metrics.yml)
  else
    infra_result=1
    echo 'GeoPython metrics deployment unconfigured; runtime startup remains independent' >&2
  fi
fi
runtime_compose up -d --no-build --pull never --wait --wait-timeout 180

connect_business_network

published_port="$(platform_compose port nginx 80 | sed 's/.*://')"
public_origin="${ADDP_PUBLIC_ORIGIN:-$(sed -n 's/^ADDP_PUBLIC_ORIGIN=//p' .env | tail -n 1)}"
if [ -z "$public_origin" ]; then
  public_origin="http://localhost:${published_port}"
fi

echo -e "${GREEN}✓ ADDP 容器部署已启动${NC}"
echo -e "统一入口: ${public_origin}"
echo ""
platform_compose ps --format "table {{.Service}}\t{{.State}}\t{{.Health}}\t{{.Ports}}"
runtime_compose ps --format "table {{.Service}}\t{{.State}}\t{{.Health}}\t{{.Ports}}"

exit "$infra_result"
