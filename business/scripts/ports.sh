#!/usr/bin/env bash
# Business 本地地址解析。首次选择空闲宿主机端口和 Docker 网段；成功后固定实际映射。

addp_business_network_name() {
  printf 'business_business-network\n'
}

addp_business_network_state() {
  printf '%s/.business-state/network.env\n' "$PROJECT_ROOT"
}

addp_business_network_override() {
  printf '%s/.business-state/docker-compose.network.yml\n' "$PROJECT_ROOT"
}

addp_business_use_network_override() {
  local override
  override=$(addp_business_network_override)
  [ -f "$override" ] || { echo "✗ Business 网络配置不存在: $override" >&2; return 1; }
  COMPOSE_FILE="$PROJECT_ROOT/docker-compose.yml:$override"
  export COMPOSE_FILE
}

addp_business_saved_network_subnet() {
  local state
  state=$(addp_business_network_state)
  [ -f "$state" ] || return 0
  sed -n 's/^BUSINESS_NETWORK_SUBNET=//p' "$state" | head -n 1
}

addp_business_valid_subnet() {
  python3 -c 'import ipaddress, sys; ipaddress.IPv4Network(sys.argv[1], strict=True)' "$1" >/dev/null 2>&1
}

addp_business_ip_in_subnet() {
  python3 -c 'import ipaddress, sys; assert ipaddress.IPv4Address(sys.argv[1]) in ipaddress.IPv4Network(sys.argv[2])' "$1" "$2" >/dev/null 2>&1
}

addp_business_oceanbase_persisted_ip() {
  docker volume inspect business_oceanbase_obd_data >/dev/null 2>&1 || return 0
  docker run --rm --network none --read-only \
    -v business_oceanbase_obd_data:/obd:ro \
    --entrypoint /bin/sh "${OCEANBASE_IMAGE:-oceanbase/oceanbase-ce:4.4.2-lts}" \
    -c 'if [ -f /obd/obcluster/config.yaml ]; then sed -n "/^  servers:/,/^  global:/s/^  - //p" /obd/obcluster/config.yaml; fi'
}

addp_business_network_ip_owner() {
  local network
  network=$(addp_business_network_name)
  docker network inspect "$network" --format '{{range .Containers}}{{.Name}}|{{.IPv4Address}}{{println}}{{end}}' |
    awk -F '[|/]' -v target="$1" '$2 == target { print $1 }'
}

addp_business_check_oceanbase_ip() {
  local ip="$1" owner
  owner=$(addp_business_network_ip_owner "$ip") || return 1
  if [ -n "$owner" ] && [ "$owner" != business-oceanbase ]; then
    echo "✗ OceanBase 旧地址 $ip 已被 $owner 占用" >&2
    return 1
  fi
}

addp_business_ensure_network() {
  local network saved subnet owner probe state temporary expected_ip="${1:-}"
  network=$(addp_business_network_name)
  state=$(addp_business_network_state)
  saved=$(addp_business_saved_network_subnet)
  if [ -f "$state" ] && [ -z "$saved" ]; then
    echo "✗ Business 网段记录为空: $state" >&2
    return 1
  fi
  if [ -n "$saved" ] && ! addp_business_valid_subnet "$saved"; then
    echo "✗ Business 已保存的 Docker 网段无效: $saved" >&2
    return 1
  fi

  if docker network inspect "$network" >/dev/null 2>&1; then
    owner=$(docker network inspect "$network" --format '{{index .Labels "com.addp.owner"}}|{{index .Labels "com.docker.compose.project"}}')
    case "$owner" in
      business\|*|*\|business) ;;
      *) echo "✗ $network 不属于本工作区的 Business 网络" >&2; return 1 ;;
    esac
  else
    if [ -z "$saved" ]; then
      if [ -z "$expected_ip" ]; then
        expected_ip=$(addp_business_oceanbase_persisted_ip) || return 1
      fi
      if [ -n "$expected_ip" ]; then
        echo '✗ OceanBase 旧集群仍在，但 Business 网段记录已丢失；拒绝以新网段启动旧数据' >&2
        return 1
      fi
      probe="${network}-probe-$$"
      docker network create --driver bridge "$probe" >/dev/null || return 1
      subnet=$(docker network inspect "$probe" --format '{{(index .IPAM.Config 0).Subnet}}')
      docker network rm "$probe" >/dev/null || return 1
      addp_business_valid_subnet "$subnet" || { echo "✗ Docker 未分配有效的 Business 网段: $subnet" >&2; return 1; }
    else
      subnet="$saved"
    fi
    docker network create --driver bridge --subnet "$subnet" \
      --label com.addp.owner=business "$network" >/dev/null || {
      echo "✗ 无法恢复 Business Docker 网段 $subnet；请检查与其他 Docker 网络的冲突" >&2
      return 1
    }
  fi

  subnet=$(docker network inspect "$network" --format '{{(index .IPAM.Config 0).Subnet}}')
  addp_business_valid_subnet "$subnet" || { echo "✗ Business Docker 网段无效: $subnet" >&2; return 1; }
  if [ -n "$saved" ] && [ "$saved" != "$subnet" ]; then
    echo "✗ Business Docker 网段 $subnet 与已保存的 $saved 不一致" >&2
    return 1
  fi
  if [ -n "$expected_ip" ] && ! addp_business_ip_in_subnet "$expected_ip" "$subnet"; then
    echo "✗ OceanBase 旧地址 $expected_ip 不在 Business 网段 $subnet 中" >&2
    return 1
  fi
  if [ ! -f "$state" ]; then
    mkdir -p "${PROJECT_ROOT}/.business-state"
    temporary=$(mktemp "${state}.XXXXXX") || return 1
    printf 'BUSINESS_NETWORK_SUBNET=%s\n' "$subnet" > "$temporary"
    mv "$temporary" "$state"
  fi
  temporary=$(mktemp "$(addp_business_network_override).XXXXXX") || return 1
  cat > "$temporary" <<'EOF'
networks:
  business-network:
    name: business_business-network
    external: true
EOF
  mv "$temporary" "$(addp_business_network_override)"
}

addp_business_port_specs() {
  cat <<'EOF'
postgres POSTGRES_PORT 5432 5433 business-postgres
mysql MYSQL_PORT 3306 3306 business-mysql
minio MINIO_API_PORT 9000 9002 business-minio
minio MINIO_CONSOLE_PORT 9001 9003 business-minio
EOF
}

addp_business_port_state() {
  printf '%s/.business-state/ports.env\n' "$PROJECT_ROOT"
}

addp_business_saved_port() {
  local state
  state=$(addp_business_port_state)
  [ -f "$state" ] || return 0
  sed -n "s/^${1}=//p" "$state" | head -n 1
}

addp_business_verify_container() {
  local service="$1" container="$2" labels
  labels=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}|{{index .Config.Labels "com.docker.compose.service"}}|{{index .Config.Labels "com.docker.compose.project.working_dir"}}' "$container" 2>/dev/null) || return 1
  [ "$labels" = "business|${service}|${PROJECT_ROOT}" ] || {
    echo "✗ ${container} 不属于当前工作区的 Business ${service}" >&2
    return 2
  }
}

addp_business_mapped_port() {
  local mapping
  mapping=$(docker port "$1" "${2}/tcp" 2>/dev/null | sed -n '1p') || return 1
  [ -n "$mapping" ] || return 1
  printf '%s\n' "${mapping##*:}"
}

addp_business_port_busy() {
  lsof -nP -iTCP:"$1" -sTCP:LISTEN >/dev/null 2>&1
}

addp_business_service_selected() {
  case " $ADDP_BUSINESS_SELECTED_SERVICES " in
    *" $1 "*) return 0 ;;
    *) return 1 ;;
  esac
}

addp_business_port_reserved() {
  local variable="$1" candidate="$2" state other value
  state=$(addp_business_port_state)
  [ -f "$state" ] || return 1
  while IFS='=' read -r other value; do
    [ "$other" = "$variable" ] && continue
    [ "$value" = "$candidate" ] && return 0
  done < "$state"
  return 1
}

addp_business_resolve_ports() {
  local service variable internal preferred container desired saved selected candidate mapped running
  local allocated=' '
  ADDP_BUSINESS_SELECTED_SERVICES=" $* "
  export ADDP_BUSINESS_SELECTED_SERVICES

  while read -r service variable internal preferred container; do
    addp_business_service_selected "$service" || continue
    desired="${!variable:-$preferred}"
    if ! [[ "$desired" =~ ^[0-9]+$ ]] || (( 10#$desired < 1 || 10#$desired > 65535 )); then
      echo "✗ ${variable} 不是有效 TCP 端口: ${desired}" >&2
      return 1
    fi
    if [ "$service" = minio ] && { [ "$desired" = 19000 ] || [ "$desired" = 19001 ]; }; then
      echo "✗ Business MinIO 的 ${variable} 不得使用 System MinIO 首选端口 ${desired}" >&2
      return 1
    fi
    saved=$(addp_business_saved_port "$variable")
    if [ -n "$saved" ] && { ! [[ "$saved" =~ ^[0-9]+$ ]] || (( 10#$saved < 1 || 10#$saved > 65535 )); }; then
      echo "✗ Business 已保存的 ${variable} 无效: ${saved}" >&2
      return 1
    fi

    running=false
    if docker inspect "$container" >/dev/null 2>&1; then
      addp_business_verify_container "$service" "$container" || return 1
      [ "$(docker inspect --format '{{.State.Running}}' "$container")" = true ] && running=true
    fi
    if [ "$running" = true ]; then
      mapped=$(addp_business_mapped_port "$container" "$internal") || {
        echo "✗ 无法读取 ${container} 的实际宿主机端口" >&2
        return 1
      }
      if [ -n "$saved" ] && [ "$saved" != "$mapped" ]; then
        echo "✗ ${container} 实际端口 ${mapped} 与已登记的固定端口 ${saved} 不一致" >&2
        return 1
      fi
      selected="$mapped"
    elif [ -n "$saved" ]; then
      selected="$saved"
      if addp_business_port_busy "$selected"; then
        echo "✗ ${variable} 的固定端口 ${selected} 已被其他服务占用；已登记 Engine Instance 不能静默换端口" >&2
        return 1
      fi
    else
      selected="$desired"
      if addp_business_port_busy "$selected" || [[ "$allocated" == *" $selected "* ]] || addp_business_port_reserved "$variable" "$selected"; then
        selected=''
        for ((candidate = preferred + 10000; candidate < preferred + 10100; candidate++)); do
          [ "$candidate" = 13306 ] && continue # 本地 CI disposable MySQL 专用
          if ! addp_business_port_busy "$candidate" && [[ "$allocated" != *" $candidate "* ]] && ! addp_business_port_reserved "$variable" "$candidate"; then
            selected="$candidate"
            break
          fi
        done
        [ -n "$selected" ] || { echo "✗ ${variable} 找不到空闲宿主机端口" >&2; return 1; }
      fi
    fi

    if [[ "$allocated" == *" $selected "* ]]; then
      echo "✗ Business 端口 ${selected} 被重复分配" >&2
      return 1
    fi
    allocated+="${selected} "
    printf -v "$variable" '%s' "$selected"
    export "$variable"
    [ "$selected" = "$desired" ] || echo "  ${service} ${variable}: ${desired} → ${selected}"
  done < <(addp_business_port_specs)
}

addp_business_save_actual_ports() {
  local state temporary service variable internal preferred container actual selected saved
  state=$(addp_business_port_state)
  mkdir -p "${PROJECT_ROOT}/.business-state"
  temporary=$(mktemp "${state}.XXXXXX") || return 1
  while read -r service variable internal preferred container; do
    if addp_business_service_selected "$service"; then
      addp_business_verify_container "$service" "$container" || { rm -f "$temporary"; return 1; }
      [ "$(docker inspect --format '{{.State.Running}}' "$container")" = true ] || { rm -f "$temporary"; return 1; }
      actual=$(addp_business_mapped_port "$container" "$internal") || { rm -f "$temporary"; return 1; }
      selected="${!variable}"
      if [ "$actual" != "$selected" ]; then
        echo "✗ ${container} 实际端口 ${actual} 与启动时选择的 ${selected} 不一致" >&2
        rm -f "$temporary"
        return 1
      fi
      printf '%s=%s\n' "$variable" "$actual" >> "$temporary"
    else
      saved=$(addp_business_saved_port "$variable")
      [ -n "$saved" ] && printf '%s=%s\n' "$variable" "$saved" >> "$temporary"
    fi
  done < <(addp_business_port_specs)
  mv "$temporary" "$state"
}
