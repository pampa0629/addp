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

echo 'ADDP Business port resolution OK'
