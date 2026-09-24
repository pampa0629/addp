#!/usr/bin/env bash
# Business 本地宿主机端口解析。首次选空闲端口；成功启动后固定实际映射。

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
