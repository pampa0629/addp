#!/usr/bin/env bash
# 本地开发端口的唯一解析入口；须在根 .env 加载和 Infra 就绪之后调用。

addp_dev_port_specs() {
  cat <<'EOF'
gateway GATEWAY_PORT 8000
system SYSTEM_BACKEND_PORT 8180
manager MANAGER_BACKEND_PORT 8081
meta META_BACKEND_PORT 8082
transfer TRANSFER_BACKEND_PORT 8083
orchestrator ORCHESTRATOR_BACKEND_PORT 8084
develop DEVELOP_BACKEND_PORT 8185
service SERVICE_BACKEND_PORT 8086
copilot-backend COPILOT_BACKEND_PORT 8087
monitor MONITOR_BACKEND_PORT 8100
standard STANDARD_BACKEND_PORT 8110
model MODEL_BACKEND_PORT 8181
quality QUALITY_BACKEND_PORT 8182
security SECURITY_BACKEND_PORT 8194
asset ASSET_BACKEND_PORT 8183
portal PORTAL_BACKEND_PORT 8184
catalog CATALOG_BACKEND_PORT 8192
ontology ONTOLOGY_BACKEND_PORT 8195
workbench WORKBENCH_BACKEND_PORT 8193
agent-backend AGENT_BACKEND_PORT 8190
graph GRAPH_BACKEND_PORT 8186
inference INFERENCE_BACKEND_PORT 8191
console-frontend CONSOLE_FE_PORT 5170
system-frontend SYSTEM_FE_PORT 5173
manager-frontend MANAGER_FE_PORT 5174
meta-frontend META_FE_PORT 5175
transfer-frontend TRANSFER_FE_PORT 5176
orchestrator-frontend ORCHESTRATOR_FE_PORT 5177
develop-frontend DEVELOP_FE_PORT 5178
monitor-frontend MONITOR_FE_PORT 5179
service-frontend SERVICE_FE_PORT 5180
standard-frontend STANDARD_FE_PORT 5181
model-frontend MODEL_FE_PORT 5182
quality-frontend QUALITY_FE_PORT 5183
asset-frontend ASSET_FE_PORT 5184
portal-frontend PORTAL_FE_PORT 5185
agent-frontend AGENT_FE_PORT 5186
graph-frontend GRAPH_FE_PORT 5187
inference-frontend INFERENCE_FE_PORT 5188
catalog-frontend CATALOG_FE_PORT 5189
workbench-frontend WORKBENCH_FE_PORT 5190
security-frontend SECURITY_FE_PORT 5191
ontology-frontend ONTOLOGY_FE_PORT 5192
math-workflow MATH_WORKFLOW_PORT 8089
jupyter JUPYTER_API_PORT 8097
spark-workflow SPARK_WORKFLOW_PORT 8098
geopython-workflow GEOPYTHON_WORKFLOW_PORT 8099
model3d-workflow MODEL3D_WORKFLOW_PORT 8101
pointcloud-workflow POINTCLOUD_WORKFLOW_PORT 8102
supermap-workflow SUPERMAP_WORKFLOW_PORT 8103
duckdb DUCKDB_RUNTIME_PORT 8104
document-workflow DOCUMENT_WORKFLOW_PORT 8105
raster-mosaic-runtime RASTER_MOSAIC_RUNTIME_PORT 8291
EOF
}

addp_dev_saved_port() {
  local variable="$1" state="${ROOT_DIR}/.dev-state/ports.env"
  [ -f "$state" ] || return 0
  sed -n "s/^${variable}=//p" "$state" | head -n 1
}

addp_dev_owned_listener() {
  local name="$1" port="$2" pidfile owner listener
  pidfile="${ROOT_DIR}/.dev-pids/${name}.pid"
  case "$name" in
    geopython-workflow|pointcloud-workflow|document-workflow|supermap-workflow)
      local container="${name}-engine" labels mapping
      labels=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}|{{index .Config.Labels "com.docker.compose.service"}}|{{index .Config.Labels "com.docker.compose.project.working_dir"}}' "$container" 2>/dev/null) || return 1
      [ "$labels" = "addp-app|${container}|${ROOT_DIR}" ] || return 1
      [ "$(docker inspect --format '{{.State.Running}}' "$container")" = true ] || return 1
      mapping=$(docker port "$container" 2>/dev/null | awk -v port="$port" '$0 ~ ":" port "$" { found=1 } END { exit !found }') || return 1
      return 0
      ;;
  esac
  [ -f "$pidfile" ] || return 1
  owner=$(cat "$pidfile")
  [[ "$owner" =~ ^[0-9]+$ ]] && kill -0 "$owner" 2>/dev/null || return 1
  while read -r listener; do
    [ -n "$listener" ] || continue
    if [ "$listener" = "$owner" ] || addp_process_is_descendant_of "$listener" "$owner"; then
      return 0
    fi
  done < <(lsof -nP -t -iTCP:"$port" -sTCP:LISTEN 2>/dev/null | sort -u)
  return 1
}

addp_dev_port_busy() {
  lsof -nP -iTCP:"$1" -sTCP:LISTEN >/dev/null 2>&1
}

addp_dev_remove_owned_container() {
  local container="$1" labels
  docker inspect "$container" >/dev/null 2>&1 || return 0
  labels=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}|{{index .Config.Labels "com.docker.compose.service"}}|{{index .Config.Labels "com.docker.compose.project.working_dir"}}' "$container" 2>/dev/null) || return 1
  if [ "$labels" != "addp-app|${container}|${ROOT_DIR}" ]; then
    echo "✗ 容器名 ${container} 已存在，但不属于当前工作区" >&2
    return 1
  fi
  docker rm -f "$container" >/dev/null
}

addp_dev_resolve_ports() {
  local name variable preferred desired saved selected candidate state_tmp
  local allocated=' '
  mkdir -p "${ROOT_DIR}/.dev-state"
  state_tmp=$(mktemp "${ROOT_DIR}/.dev-state/ports.env.XXXXXX") || return 1
  while read -r name variable preferred; do
    desired="${!variable:-$preferred}"
    if ! [[ "$desired" =~ ^[0-9]+$ ]] || (( 10#$desired < 1 || 10#$desired > 65535 )); then
      echo "✗ ${variable} 不是有效 TCP 端口: ${desired}" >&2
      rm -f "$state_tmp"
      return 1
    fi
    selected="$desired"
    saved=$(addp_dev_saved_port "$variable")
    if [ "${ADDP_ONLINE_HOST:-0}" != 1 ] && [[ "$saved" =~ ^[0-9]+$ ]] && addp_dev_owned_listener "$name" "$saved"; then
      selected="$saved"
    elif addp_dev_owned_listener "$name" "$desired"; then
      selected="$desired"
    elif addp_dev_port_busy "$selected" || [[ "$allocated" == *" $selected "* ]]; then
      if [ "${ADDP_ONLINE_HOST:-0}" = 1 ]; then
        echo "✗ 专用 Runner 配置的 ${variable}=${selected} 已被占用" >&2
        rm -f "$state_tmp"
        return 1
      fi
      selected=''
      for ((candidate = preferred + 10000; candidate < preferred + 10100; candidate++)); do
        if ! addp_dev_port_busy "$candidate" && [[ "$allocated" != *" $candidate "* ]]; then
          selected="$candidate"
          break
        fi
      done
      if [ -z "$selected" ]; then
        echo "✗ ${variable} 找不到空闲宿主机端口" >&2
        rm -f "$state_tmp"
        return 1
      fi
    fi
    if [[ "$allocated" == *" $selected "* ]]; then
      echo "✗ 开发端口 ${selected} 被重复分配" >&2
      rm -f "$state_tmp"
      return 1
    fi
    allocated+="${selected} "
    printf -v "$variable" '%s' "$selected"
    export "$variable"
    printf '%s=%s\n' "$variable" "$selected" >> "$state_tmp"
    [ "$selected" = "$desired" ] || echo "  ${name} ${variable}: ${desired} → ${selected}"
  done < <(addp_dev_port_specs)
  mv "$state_tmp" "${ROOT_DIR}/.dev-state/ports.env"
  export ADDP_DEV_PORTS_RESOLVED=1
  addp_dev_apply_endpoints
}

addp_dev_load_saved_ports() {
  local name variable preferred saved
  while read -r name variable preferred; do
    saved=$(addp_dev_saved_port "$variable")
    if [[ "$saved" =~ ^[0-9]+$ ]] && addp_dev_owned_listener "$name" "$saved"; then
      printf -v "$variable" '%s' "$saved"
      export "$variable"
    fi
  done < <(addp_dev_port_specs)
  addp_dev_apply_endpoints
}

addp_dev_apply_endpoints() {
  local host="${SERVICE_HOST:-localhost}" name variable preferred origin port origin_url origins="${ALLOWED_ORIGINS:-}"
  export PUBLIC_API_URL="http://${host}:${GATEWAY_PORT:-8000}"
  export CONSOLE_URL="http://${host}:${CONSOLE_FE_PORT:-5170}"
  export MONITOR_CONSOLE_BASE_URL="$CONSOLE_URL"
  export VITE_ADDP_GATEWAY_PORT="${GATEWAY_PORT:-8000}"
  export VITE_ADDP_CONSOLE_PORT="${CONSOLE_FE_PORT:-5170}"
  export VITE_ADDP_SERVICE_BACKEND_PORT="${SERVICE_BACKEND_PORT:-8086}"
  for origin in localhost 127.0.0.1 "$host"; do
    while read -r name variable preferred; do
      case "$variable" in
        *_FE_PORT)
          port="${!variable:-$preferred}"
          origin_url="http://${origin}:${port}"
          [[ ",${origins}," == *",${origin_url},"* ]] || origins="${origins:+${origins},}${origin_url}"
          ;;
      esac
    done < <(addp_dev_port_specs)
  done
  export ALLOWED_ORIGINS="$origins"
  export VITE_ADDP_FRONTEND_PORTS
  VITE_ADDP_FRONTEND_PORTS=''
  export VITE_ADDP_BACKEND_PORTS
  VITE_ADDP_BACKEND_PORTS=''
  while read -r name variable preferred; do
    case "$variable" in
      *_FE_PORT) VITE_ADDP_FRONTEND_PORTS="${VITE_ADDP_FRONTEND_PORTS}${VITE_ADDP_FRONTEND_PORTS:+,}${name%-frontend}:${!variable:-$preferred}" ;;
      *_BACKEND_PORT) VITE_ADDP_BACKEND_PORTS="${VITE_ADDP_BACKEND_PORTS}${VITE_ADDP_BACKEND_PORTS:+,}${name%-backend}:${!variable:-$preferred}" ;;
    esac
  done < <(addp_dev_port_specs)
}
