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
