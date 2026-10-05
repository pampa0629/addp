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

addp_spark_configure_java() {
  local candidate version
  local candidates=("${JAVA_HOME:-}" /opt/homebrew/opt/openjdk@11/libexec/openjdk.jdk/Contents/Home /usr/local/opt/openjdk@11/libexec/openjdk.jdk/Contents/Home /usr/lib/jvm/java-11-openjdk-amd64 /usr/lib/jvm/java-11-openjdk-arm64)
  if [ "$(uname -s)" = Darwin ]; then
    candidate=$(/usr/libexec/java_home -v 11 2>/dev/null) && candidates+=("$candidate")
  elif command -v java >/dev/null 2>&1; then
    candidate=$(readlink -f "$(command -v java)") && candidates+=("${candidate%/bin/java}")
  fi
  for candidate in "${candidates[@]}"; do
    [ -n "$candidate" ] && [ -x "$candidate/bin/java" ] || continue
    version=$("$candidate/bin/java" -version 2>&1) || continue
    if printf '%s\n' "$version" | head -n 1 | awk '$3 ~ /^"11[.\"]/ { found=1 } END { exit !found }'; then
      export JAVA_HOME="$candidate" PATH="$candidate/bin:$PATH"
      return 0
    fi
  done
  echo '✗ Spark Workflow 需要原生 OpenJDK 11，请安装或通过 JAVA_HOME 指向 Java 11' >&2
  return 1
}

addp_prepare_spark_workflow() {
  local runtime_dir="$ROOT_DIR/engines/spark-workflow" python_bin candidate shared_host
  [ -z "${SPARK_MODE:-}" ] || { echo '✗ 开发生命周期不接受 SPARK_MODE，计算必须使用已登记集群' >&2; return 1; }
  addp_spark_configure_java || return 1
  python_bin="$runtime_dir/venv/bin/python"
  if [ ! -x "$python_bin" ]; then
    for candidate in python3.12 python3.11; do
      if command -v "$candidate" >/dev/null 2>&1; then
        "$candidate" -m venv "$runtime_dir/venv" || return 1
        break
      fi
    done
  fi
  [ -x "$python_bin" ] && "$python_bin" -c 'import sys; assert sys.version_info[:2] in ((3,11),(3,12)), "Spark 开发环境要求 Python 3.11/3.12"' || {
    echo '✗ Spark Workflow 需要 Python 3.11/3.12 虚拟环境' >&2
    return 1
  }
  shared_host=$(addp_spark_shared_host) || return 1
  [ -n "$shared_host" ] || { echo '✗ 无法确定 SPARK_WORKFLOW_SHARED_HOST' >&2; return 1; }
  "$python_bin" - "$shared_host" <<'PY_DNS' || return 1
import socket, sys
try:
    socket.getaddrinfo(sys.argv[1], None, type=socket.SOCK_STREAM)
except OSError as error:
    print('✗ Spark 共用地址无法在本机解析: ' + sys.argv[1] + ': ' + str(error), file=sys.stderr)
    print('  macOS 使用默认地址时，请在 /etc/hosts 配置 127.0.0.1 host.docker.internal', file=sys.stderr)
    sys.exit(1)
PY_DNS
  addp_sync_python_dependencies "$ROOT_DIR" "$runtime_dir" 'Spark Workflow' || return 1
  (cd "$runtime_dir" && "$python_bin" -c 'import api_server, runtime_server') || return 1
  if addp_dev_port_busy "${SPARK_WORKFLOW_PORT:-8098}" && ! addp_dev_owned_listener spark-workflow-engine "${SPARK_WORKFLOW_PORT:-8098}"; then
    echo "✗ Spark Workflow 端口 ${SPARK_WORKFLOW_PORT:-8098} 被外部监听者占用" >&2
    return 1
  fi
}

addp_launch_spark_workflow() {
  local runtime_dir="$ROOT_DIR/engines/spark-workflow" port="${SPARK_WORKFLOW_PORT:-8098}"
  local pidfile="$ROOT_DIR/.dev-pids/spark-workflow-engine.pid" pid attempt shared_host
  addp_spark_configure_java || return 1
  shared_host=$(addp_spark_shared_host) || return 1
  if addp_dev_port_busy "$port"; then
    echo "✗ Spark Workflow 端口 ${port} 仍被占用，拒绝启动" >&2
    return 1
  fi
  mkdir -p "$ROOT_DIR/.dev-pids" "$ROOT_DIR/logs"
  (
    cd "$runtime_dir" || exit 1
    export PORT="$port" WORKFLOW_BIND_HOST=127.0.0.1 RUNTIME_HOST="${RUNTIME_HOST:-localhost}" SPARK_WORKFLOW_SHARED_HOST="$shared_host"
    exec "$runtime_dir/venv/bin/python" api_server.py
  ) > "$ROOT_DIR/logs/spark-workflow-engine.log" 2>&1 &
  pid=$!
  printf '%s\n' "$pid" > "$pidfile"
  for attempt in $(seq 1 60); do
    kill -0 "$pid" 2>/dev/null || break
    if addp_dev_owned_listener spark-workflow-engine "$port" && curl --max-time 2 -fsS "http://127.0.0.1:${port}/health" >/dev/null 2>&1 && kill -0 "$pid" 2>/dev/null; then
      echo "✓ Spark Workflow 原生服务已就绪: http://127.0.0.1:${port} (PID $pid)"
      return 0
    fi
    sleep 1
  done
  echo '✗ Spark Workflow 原生服务未就绪' >&2
  tail -n 100 "$ROOT_DIR/logs/spark-workflow-engine.log" >&2
  kill -TERM "$pid" 2>/dev/null || true
  for attempt in $(seq 1 5); do kill -0 "$pid" 2>/dev/null || break; sleep 1; done
  kill -KILL "$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
  rm -f "$pidfile"
  return 1
}

addp_start_spark_workflow() {
  addp_prepare_spark_workflow && addp_launch_spark_workflow
}
