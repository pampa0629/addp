#!/usr/bin/env bash
# 验证 Business 首次避让、固定端口复用和容器归属，不启动真实容器。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/../../business/scripts/ports.sh"
PROJECT_ROOT=$(mktemp -d)
trap 'rm -rf "$PROJECT_ROOT"' EXIT
MOCK_RUNNING=0
MOCK_FOREIGN=0
MOCK_BUSY='9002 9003'
MOCK_NETWORK_EXISTS=0
MOCK_NETWORK_SUBNET=172.19.0.0/16
MOCK_NETWORK_IP_OWNER=''
MOCK_NETWORK_CREATED_SUBNET=''

docker() {
  local container format='' service
  case "$1" in
    inspect)
      if [ "${2:-}" = --format ]; then format="$3"; container="$4"; else container="$2"; fi
      [ "$MOCK_RUNNING" = 1 ] || return 1
      case "$container" in
        business-postgres) service=postgres ;;
        business-mysql) service=mysql ;;
        business-minio) service=minio ;;
        *) return 1 ;;
      esac
      case "$format" in
        *com.docker.compose.project*)
          if [ "$MOCK_FOREIGN" = 1 ]; then
            echo "scp|${service}|${PROJECT_ROOT}"
          else
            echo "business|${service}|${PROJECT_ROOT}"
          fi
          ;;
        *State.Running*) echo true ;;
      esac
      ;;
    port)
      [ "$MOCK_RUNNING" = 1 ] || return 1
      case "$2:$3" in
        business-postgres:5432/tcp) echo 0.0.0.0:5433 ;;
        business-mysql:3306/tcp) echo 0.0.0.0:3306 ;;
        business-minio:9000/tcp) echo 0.0.0.0:19002 ;;
        business-minio:9001/tcp) echo 0.0.0.0:19003 ;;
        *) return 1 ;;
      esac
      ;;
    network)
      case "$2" in
        inspect)
          [ "$MOCK_NETWORK_EXISTS" = 1 ] || return 1
          case "${5:-}" in
            *'.Labels'*) echo '<no value>|business' ;;
            *'.IPAM.Config'*) echo "$MOCK_NETWORK_SUBNET" ;;
            *'.Containers'*)
              [ -z "$MOCK_NETWORK_IP_OWNER" ] || echo "$MOCK_NETWORK_IP_OWNER|172.19.0.6/16"
              ;;
          esac
          ;;
        create)
          local item previous=''
          for item in "$@"; do
            if [ "$previous" = --subnet ]; then
              MOCK_NETWORK_SUBNET="$item"
              MOCK_NETWORK_CREATED_SUBNET="$item"
              break
            fi
            previous="$item"
          done
          MOCK_NETWORK_EXISTS=1
          ;;
        *) return 1 ;;
      esac
      ;;
    *) return 1 ;;
  esac
}

lsof() {
  local arg port=''
  for arg in "$@"; do case "$arg" in -iTCP:*) port="${arg#-iTCP:}" ;; esac; done
  case " $MOCK_BUSY " in *" $port "*) return 0 ;; *) return 1 ;; esac
}

POSTGRES_PORT=5433
MYSQL_PORT=3306
MINIO_API_PORT=9002
MINIO_CONSOLE_PORT=9003
addp_business_resolve_ports postgres mysql minio >/dev/null
[ "$POSTGRES_PORT" = 5433 ]
[ "$MYSQL_PORT" = 3306 ]
[ "$MINIO_API_PORT" = 19002 ]
[ "$MINIO_CONSOLE_PORT" = 19003 ]

MOCK_RUNNING=1
addp_business_save_actual_ports
state=$(addp_business_port_state)
grep -Fxq 'MINIO_API_PORT=19002' "$state"
grep -Fxq 'MINIO_CONSOLE_PORT=19003' "$state"

MOCK_RUNNING=0
MOCK_BUSY='9002 9003 19002'
MINIO_API_PORT=9002
if addp_business_resolve_ports minio >/dev/null 2>&1; then
  echo 'fixed Business endpoint moved after another service claimed its port' >&2
  exit 1
fi

MOCK_BUSY='9002 9003'
MINIO_API_PORT=9002
MINIO_CONSOLE_PORT=9003
addp_business_resolve_ports minio >/dev/null
[ "$MINIO_API_PORT" = 19002 ]
[ "$MINIO_CONSOLE_PORT" = 19003 ]

MOCK_RUNNING=1
MOCK_FOREIGN=1
if addp_business_resolve_ports minio >/dev/null 2>&1; then
  echo 'foreign container was accepted as this Business service' >&2
  exit 1
fi

MOCK_RUNNING=0
MOCK_FOREIGN=0
rm "$state"
MINIO_API_PORT=19000
if addp_business_resolve_ports minio >/dev/null 2>&1; then
  echo 'Business MinIO accepted the System MinIO preferred port' >&2
  exit 1
fi

MYSQL_PORT=3306
MOCK_BUSY='3306 13306'
addp_business_resolve_ports mysql >/dev/null
[ "$MYSQL_PORT" = 13307 ]

if addp_business_ensure_network 172.19.0.6 >/dev/null 2>&1; then
  echo 'old OceanBase cluster accepted a missing network and missing subnet record' >&2
  exit 1
fi

MOCK_NETWORK_EXISTS=1
addp_business_ensure_network 172.19.0.6
grep -Fxq 'BUSINESS_NETWORK_SUBNET=172.19.0.0/16' "$(addp_business_network_state)"
grep -Fxq '    external: true' "$(addp_business_network_override)"
addp_business_use_network_override
[ "$COMPOSE_FILE" = "$PROJECT_ROOT/docker-compose.yml:$PROJECT_ROOT/.business-state/docker-compose.network.yml" ]

MOCK_NETWORK_EXISTS=0
addp_business_ensure_network 172.19.0.6
[ "$MOCK_NETWORK_EXISTS" = 1 ]
[ "$MOCK_NETWORK_CREATED_SUBNET" = 172.19.0.0/16 ]

MOCK_NETWORK_IP_OWNER=business-tidb-tikv
if addp_business_check_oceanbase_ip 172.19.0.6 >/dev/null 2>&1; then
  echo 'OceanBase accepted an IP address held by TiKV' >&2
  exit 1
fi
MOCK_NETWORK_IP_OWNER=business-oceanbase
addp_business_check_oceanbase_ip 172.19.0.6
if addp_business_ensure_network 172.20.0.6 >/dev/null 2>&1; then
  echo 'OceanBase accepted an IP address outside the saved subnet' >&2
  exit 1
fi

echo 'ADDP Business port resolution OK'
