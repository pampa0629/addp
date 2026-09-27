#!/usr/bin/env bash
# ports.sh - Resolve local ADDP Infra host ports and read their actual Compose mappings.
# Source this file after loading the root .env. It does not edit configuration files.

addp_infra_port_specs() {
  cat <<'EOF'
postgres POSTGRES_PORT 5432 15432 addp-postgres
redis REDIS_PORT 6379 16379 addp-redis
falkordb FALKORDB_PORT 6379 16479 addp-falkordb
minio MINIO_API_PORT 9000 19000 addp-minio
minio MINIO_CONSOLE_PORT 9001 19001 addp-minio
meilisearch MEILISEARCH_PORT 7700 17700 addp-meilisearch
redpanda INFRA_KAFKA_PORT 9092 19092 addp-redpanda
kafka-connect KAFKA_CONNECT_PORT 8083 18083 addp-kafka-connect
EOF
}

addp_infra_verify_container() {
  local service="$1" container="$2" labels root
  root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
  labels=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}|{{index .Config.Labels "com.docker.compose.service"}}|{{index .Config.Labels "com.docker.compose.project.working_dir"}}' "$container" 2>/dev/null) || return 1
  [ "$labels" = "addp-infra|${service}|${root}" ] || {
    echo "✗ 容器名 ${container} 已存在，但不属于当前工作区的 ADDP Infra ${service} 服务" >&2
    return 2
  }
}

addp_infra_mapped_port() {
  local container="$1" internal="$2" mapping
  mapping=$(docker port "$container" "${internal}/tcp" 2>/dev/null | sed -n '1p') || return 1
  [ -n "$mapping" ] || return 1
  printf '%s\n' "${mapping##*:}"
}

# Guard local PostgreSQL T2 gates before they can reset schemas or run migrations.
# Hosted CI and the dedicated local macOS CI own disposable databases outside addp-infra.
addp_infra_verify_test_postgres_target() {
  local host="$1" port="$2" database="$3" mapped state
  if [ "${GITHUB_ACTIONS:-}" = true ] || [ "${ADDP_LOCAL_CI_POSTGRES:-}" = 1 ]; then
    return 0
  fi
  case "$host" in localhost|localhost.|127.*|::1) ;; *) return 0 ;; esac
  case "$database" in
    addp_test|addp_iam_test) ;;
    *) echo "✗ 本地 ADDP PostgreSQL 门禁仅允许 addp_test 或 addp_iam_test" >&2; return 1 ;;
  esac
  if ! [[ "$port" =~ ^[0-9]+$ ]] || (( 10#$port < 1 || 10#$port > 65535 )); then
    echo "✗ 本地 PostgreSQL 测试连接必须显式指定有效端口" >&2
    return 1
  fi
  if ! command -v docker >/dev/null 2>&1; then
    echo "✗ 无法核实 ADDP PostgreSQL 容器映射：Docker 不可用" >&2
    return 1
  fi
  addp_infra_verify_container postgres addp-postgres || {
    echo "✗ 无法核实当前工作区的 addp-postgres；先运行 bash scripts/infra/status.sh" >&2
    return 1
  }
  state=$(docker inspect --format '{{.State.Running}}' addp-postgres 2>/dev/null) || return 1
  [ "$state" = true ] || {
    echo "✗ addp-postgres 未运行" >&2
    return 1
  }
  mapped=$(addp_infra_mapped_port addp-postgres 5432) || {
    echo "✗ 无法读取 addp-postgres 的实际宿主机端口" >&2
    return 1
  }
  [ "$port" = "$mapped" ] || {
    echo "✗ PostgreSQL 测试端口 ${port} 与 addp-postgres 实际映射 ${mapped} 不一致；先运行 bash scripts/infra/status.sh" >&2
    return 1
  }
}

addp_infra_verify_test_postgres_dsn() {
  local parsed host port database
  parsed=$(python3 -c '
import sys
from urllib.parse import unquote, urlsplit

try:
    value = urlsplit(sys.stdin.readline().rstrip("\n"))
    if value.scheme not in ("postgres", "postgresql") or not value.hostname:
        raise ValueError("invalid PostgreSQL URL")
    host = value.hostname
    database = unquote(value.path.lstrip("/"))
    if not database or any(char in host + database for char in "|\r\n"):
        raise ValueError("invalid PostgreSQL URL target")
    print(host + "|" + (str(value.port) if value.port else "") + "|" + database)
except ValueError:
    sys.exit("✗ PostgreSQL 测试 DSN 缺少有效的主机、端口或 database")
' <<< "$1") || return 1
  IFS='|' read -r host port database <<< "$parsed"
  addp_infra_verify_test_postgres_target "$host" "$port" "$database"
}

addp_infra_port_busy() {
  lsof -nP -iTCP:"$1" -sTCP:LISTEN >/dev/null 2>&1
}

addp_infra_resolve_ports() {
  local service variable internal preferred container desired mapped selected candidate
  local allocated=' '

  while read -r service variable internal preferred container; do
    if docker inspect "$container" >/dev/null 2>&1; then
      addp_infra_verify_container "$service" "$container" || return 1
    fi

    desired="${!variable:-$preferred}"
    if ! [[ "$desired" =~ ^[0-9]+$ ]] || (( 10#$desired < 1 || 10#$desired > 65535 )); then
      echo "✗ ${variable} 不是有效 TCP 端口: ${desired}" >&2
      return 1
    fi

    mapped=$(addp_infra_mapped_port "$container" "$internal" || true)
    if [ -n "$mapped" ] && [ "$(docker inspect --format '{{.State.Running}}' "$container")" = true ]; then
      selected="$mapped"
      if [ "${ADDP_ONLINE_HOST:-0}" = 1 ] && [ "$selected" != "$desired" ]; then
        echo "✗ 专用 Runner 的 ${variable} 要求 ${desired}，当前 ADDP Infra 使用 ${selected}" >&2
        return 1
      fi
    else
      selected="$desired"
      if addp_infra_port_busy "$selected" || [[ "$allocated" == *" $selected "* ]]; then
        if [ "${ADDP_ONLINE_HOST:-0}" = 1 ]; then
          echo "✗ 专用 Runner 配置的 ${variable}=${selected} 已被占用" >&2
          return 1
        fi
        selected=''
        for ((candidate = preferred + 10000; candidate < preferred + 10100; candidate++)); do
          # 29092 belongs to the independent Business Kafka cluster.
          [ "$candidate" = 29092 ] && continue
          if ! addp_infra_port_busy "$candidate" && [[ "$allocated" != *" $candidate "* ]]; then
            selected="$candidate"
            break
          fi
        done
      fi
      [ -n "$selected" ] || { echo "✗ ${service} 找不到空闲宿主机端口" >&2; return 1; }
    fi

    if [[ "$allocated" == *" $selected "* ]]; then
      echo "✗ ADDP Infra 重复映射宿主机端口 ${selected}" >&2
      return 1
    fi
    allocated+="${selected} "
    printf -v "$variable" '%s' "$selected"
    export "$variable"
    if [ "$selected" != "$desired" ]; then
      echo "  ${service} ${variable}: ${desired} → ${selected}"
    fi
  done < <(addp_infra_port_specs)

  addp_infra_apply_endpoints
}

addp_infra_read_actual_ports() {
  local service variable internal preferred container mapped
  while read -r service variable internal preferred container; do
    addp_infra_verify_container "$service" "$container" || return 1
    mapped=$(addp_infra_mapped_port "$container" "$internal") || {
      echo "✗ 无法读取 ADDP Infra ${service} 的实际宿主机端口" >&2
      return 1
    }
    printf -v "$variable" '%s' "$mapped"
    export "$variable"
  done < <(addp_infra_port_specs)
  addp_infra_apply_endpoints
}

addp_infra_apply_endpoints() {
  local host="${SERVICE_HOST:-localhost}"
  export INFRA_FALKORDB_ADDRESS="127.0.0.1:${FALKORDB_PORT}"
  export MEILISEARCH_URL="http://${host}:${MEILISEARCH_PORT}"
  export MEILISEARCH_URL_LOCAL="$MEILISEARCH_URL"
  export INFRA_KAFKA_BOOTSTRAP_SERVERS="${host}:${INFRA_KAFKA_PORT}"
  export KAFKA_CONNECT_URL="http://${host}:${KAFKA_CONNECT_PORT}"
}

addp_infra_ready() {
  local service variable internal preferred container state
  while read -r service variable internal preferred container; do
    addp_infra_verify_container "$service" "$container" >/dev/null 2>&1 || return 1
    state=$(docker inspect --format '{{.State.Running}}:{{if .State.Health}}{{.State.Health.Status}}{{end}}' "$container" 2>/dev/null) || return 1
    case "$state" in true:healthy|true:) ;; *) return 1 ;; esac
  done < <(addp_infra_port_specs)
}
