#!/usr/bin/env bash
# Source from start.sh/restart.sh after environment and actual ports are resolved.

addp_spark_shared_host() {
  if [ -n "${SPARK_WORKFLOW_SHARED_HOST:-}" ]; then
    printf '%s\n' "$SPARK_WORKFLOW_SHARED_HOST"
  elif [ "$(uname -s)" = Darwin ]; then
    printf '%s\n' host.docker.internal
  elif [ "${ADDP_ONLINE_HOSTED:-0}" = 1 ] && [ "$(uname -s)" = Linux ]; then
    printf '%s\n' 127.0.0.1
  elif command -v ip >/dev/null 2>&1; then
    ip route get 1.1.1.1 | awk '{ for (i=1; i<=NF; i++) if ($i == "src") { print $(i+1); exit } }'
  else
    echo '✗ 请配置 Driver 与 Worker 共用的 SPARK_WORKFLOW_SHARED_HOST' >&2
    return 1
  fi
}

addp_start_spark_workflow_container() {
  local container=spark-workflow-engine port="${SPARK_WORKFLOW_PORT:-8098}"
  local image="${REGISTRY:-localhost:5001}/addp-spark-workflow-engine:${IMAGE_TAG:-latest}"
  local labels shared_host attempt
  command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1 || {
    echo '✗ Spark Workflow 需要可用的 Docker' >&2
    return 1
  }
  case "$(uname -s)" in Linux|Darwin) ;; *) echo '✗ Spark Workflow 开发容器只支持 Linux / macOS Docker Desktop' >&2; return 1 ;; esac
  [ -z "${SPARK_MODE:-}" ] || { echo '✗ 开发生命周期不接受 SPARK_MODE，计算必须使用已登记集群' >&2; return 1; }
  if docker inspect "$container" >/dev/null 2>&1; then
    labels=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}|{{index .Config.Labels "com.docker.compose.service"}}|{{index .Config.Labels "com.docker.compose.project.working_dir"}}' "$container") || return 1
    [ "$labels" = "addp-runtimes|${container}|${ROOT_DIR}" ] || {
      echo "✗ 容器 ${container} 不属于当前工作区，拒绝替换" >&2
      return 1
    }
  fi
  if addp_dev_port_busy "$port" && ! addp_dev_owned_listener "$container" "$port"; then
    echo "✗ Spark Workflow 端口 ${port} 被外部监听者占用" >&2
    return 1
  fi
  shared_host=$(addp_spark_shared_host) || return 1
  [ -n "$shared_host" ] || { echo '✗ 无法确定 SPARK_WORKFLOW_SHARED_HOST' >&2; return 1; }
  # Use the registered product builder, including its source/cache validation.
  # A failed build must not remove an existing owned container.
  (cd "$ROOT_DIR" && make build-images IMAGE_BUILD_ARGS="--services spark-workflow-engine --verify --jobs 1") || return 1
  addp_dev_remove_owned_container "$container" || return 1
  mkdir -p "${ROOT_DIR}/.dev-pids"
  docker run -d --name "$container" --network host \
    --label com.docker.compose.project=addp-runtimes \
    --label "com.docker.compose.project.config_files=${ROOT_DIR}/docker-compose.runtimes.yml" \
    --label "com.docker.compose.service=${container}" \
    --label "com.docker.compose.project.working_dir=${ROOT_DIR}" \
    -e "PORT=${port}" -e WORKFLOW_BIND_HOST=127.0.0.1 -e "RUNTIME_HOST=${RUNTIME_HOST:-localhost}" \
    -e SYSTEM_URL -e SPARK_WORKFLOW_SERVICE_CLIENT_SECRET -e HADOOP_USER_NAME \
    -e "SPARK_WORKFLOW_SHARED_HOST=${shared_host}" \
    "$image" > "${ROOT_DIR}/.dev-pids/${container}.pid" || return 1
  for attempt in $(seq 1 90); do
    if [ "$(docker inspect --format '{{.State.Running}}' "$container")" != true ]; then
      docker logs --tail 100 "$container" >&2 || true
      return 1
    fi
    if addp_dev_owned_listener "$container" "$port" && curl -fsS "http://127.0.0.1:${port}/health" >/dev/null 2>&1; then
      echo "✓ Spark Workflow 产品容器已就绪: http://127.0.0.1:${port}"
      return 0
    fi
    sleep 1
  done
  echo '✗ Spark Workflow 容器未就绪；macOS 请检查 Docker Desktop 的 Enable host networking' >&2
  docker logs --tail 100 "$container" >&2 || true
  return 1
}
