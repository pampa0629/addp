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

# Probe the real Desktop forwarding, not a settings file or a VM-only listener.
addp_spark_check_host_network() {
  python3 - "$1" "$ROOT_DIR" <<'PY_NETWORK'
import json
import selectors
import socket
import subprocess
import sys
import threading
import time
import uuid

image, workspace = sys.argv[1:]
nonce = uuid.uuid4().hex
name = 'addp-spark-network-' + nonce
container_code = '''
import json, socket, sys
port, nonce = int(sys.argv[1]), sys.argv[2].encode()
host_reachable = False
try:
    with socket.create_connection(('127.0.0.1', port), timeout=2) as peer:
        host_reachable = peer.recv(64) == nonce
except OSError:
    pass
with socket.socket() as listener:
    listener.bind(('127.0.0.1', 0))
    listener.listen(1)
    listener.settimeout(6)
    print(json.dumps({'host_reachable': host_reachable, 'port': listener.getsockname()[1]}), flush=True)
    try:
        with listener.accept()[0] as peer:
            peer.sendall(nonce)
    except OSError:
        pass
'''
process = None
failed = False
try:
    with socket.socket() as listener:
        listener.bind(('127.0.0.1', 0))
        listener.listen(1)
        listener.settimeout(8)
        def reply():
            try:
                with listener.accept()[0] as peer:
                    peer.sendall(nonce.encode())
            except OSError:
                pass
        worker = threading.Thread(target=reply, daemon=True)
        worker.start()
        process = subprocess.Popen(['docker', 'run', '--rm', '--name', name,
            '--network', 'host', '--label', 'com.addp.network-probe=spark-workflow',
            '--label', 'com.addp.workspace=' + workspace, '--entrypoint', 'python',
            image, '-u', '-c', container_code, str(listener.getsockname()[1]), nonce],
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        with selectors.DefaultSelector() as selector:
            selector.register(process.stdout, selectors.EVENT_READ)
            if not selector.select(8):
                raise RuntimeError('网络探针未在 8 秒内返回监听状态')
            line = process.stdout.readline()
        if not line:
            raise RuntimeError('网络探针容器未启动，请检查产品镜像和 Docker 状态')
        result = json.loads(line)
        container_reachable = False
        deadline = time.monotonic() + 3
        while not container_reachable and time.monotonic() < deadline:
            try:
                with socket.create_connection(('127.0.0.1', result['port']), timeout=0.5) as peer:
                    container_reachable = peer.recv(64) == nonce.encode()
            except OSError:
                pass
            if not container_reachable:
                time.sleep(0.1)
        if not result['host_reachable'] or not container_reachable:
            raise RuntimeError('容器 → 宿主回环: ' + ('可达' if result['host_reachable'] else '不可达') +
                '；宿主 → 容器回环: ' + ('可达' if container_reachable else '不可达'))
        process.wait(timeout=3)
        if process.returncode:
            raise RuntimeError('网络探针容器异常退出')
except (OSError, ValueError, KeyError, TypeError, RuntimeError, subprocess.TimeoutExpired) as error:
    failed = True
    print('✗ Spark Workflow 宿主网络预检失败: ' + str(error), file=sys.stderr)
    print('  macOS 请在 Docker Desktop Settings → Resources → Network 启用 Enable host networking，并执行 Apply and restart。', file=sys.stderr)
finally:
    if process is not None:
        # Auto-remove handles normal exit; force removal also covers timeout.
        try:
            removed = subprocess.run(['docker', 'rm', '-f', name], capture_output=True, text=True, timeout=10)
            if removed.returncode and subprocess.run(['docker', 'container', 'inspect', name],
                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=5).returncode == 0:
                raise RuntimeError('容器仍存在')
        except (OSError, RuntimeError, subprocess.TimeoutExpired):
            failed = True
            print('✗ 无法清理 Spark 网络探针容器: ' + name, file=sys.stderr)
        if process.poll() is None:
            process.terminate()
        try:
            process.communicate(timeout=3)
        except subprocess.TimeoutExpired:
            process.kill()
            process.communicate()
            failed = True
sys.exit(1 if failed else 0)
PY_NETWORK
}

addp_prepare_spark_workflow_container() {
  local container=spark-workflow-engine port="${SPARK_WORKFLOW_PORT:-8098}"
  local image="${REGISTRY:-localhost:5001}/addp-spark-workflow-engine:${IMAGE_TAG:-latest}"
  local labels shared_host
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
  addp_spark_check_host_network "$image" || return 1
}

addp_start_spark_workflow_container() {
  local container=spark-workflow-engine port="${SPARK_WORKFLOW_PORT:-8098}"
  local image="${REGISTRY:-localhost:5001}/addp-spark-workflow-engine:${IMAGE_TAG:-latest}"
  local shared_host attempt
  addp_prepare_spark_workflow_container || return 1
  shared_host=$(addp_spark_shared_host) || return 1
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
  echo "✗ Spark Workflow 宿主网络预检已通过，但 Runtime 在 90 秒内未就绪: http://127.0.0.1:${port}/health" >&2
  docker logs --tail 100 "$container" >&2 || true
  return 1
}
