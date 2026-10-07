#!/usr/bin/env bash
# Source after actual development ports and dependency-lock helpers are resolved.

addp_pointcloud_native_environment() {
  local prefix="${1:-$ROOT_DIR/engines/pointcloud-workflow/venv}"
  unset PYTHONHOME PYTHONPATH GDAL_DRIVER_PATH GDAL_DATA PROJ_LIB PROJ_DATA PDAL_DRIVER_PATH
  unset LD_LIBRARY_PATH DYLD_LIBRARY_PATH DYLD_FALLBACK_LIBRARY_PATH
  export CONDA_PREFIX="$prefix" PYTHONNOUSERSITE=1 GDAL_DATA="$prefix/share/gdal" PROJ_DATA="$prefix/share/proj"
  export PDAL_DRIVER_PATH="$prefix/lib" POINTCLOUD_PDAL_BIN="$prefix/bin/pdal"
  export PATH="$prefix/bin:$PATH"
}

addp_pointcloud_packages_current() {
  python3 - "$ROOT_DIR/engines/pointcloud-workflow/venv" "$ROOT_DIR/engines/pointcloud-workflow/native-packages.txt" <<'PY'
import json, sys
from pathlib import Path
prefix = Path(sys.argv[1])
try:
    packages = {p['name']: p['version'] for f in (prefix / 'conda-meta').glob('*.json') for p in [json.loads(f.read_text())]}
    for specification in Path(sys.argv[2]).read_text().splitlines():
        name, _, expected = specification.partition('=')
        actual = packages[name]
        if expected and not (actual.startswith(expected + '.') if name == 'python' else actual == expected):
            sys.exit(1)
except (KeyError, AssertionError, ValueError, OSError):
    sys.exit(1)
PY
}

addp_install_pointcloud_native_packages() {
  local prefix="$ROOT_DIR/engines/pointcloud-workflow/venv" conda_bin mode
  local create_options=()
  addp_pointcloud_packages_current && return 0
  # Reject an unrelated environment rather than deleting or converting it implicitly.
  if [ -e "$prefix" ] && [ ! -d "$prefix/conda-meta" ]; then
    echo "✗ $prefix 不是独立 Conda 前缀，请先将旧 Python venv 移出该目录" >&2
    return 1
  fi
  conda_bin="${CONDA_EXE:-}"
  if [ -z "$conda_bin" ]; then conda_bin=$(command -v conda) || { echo '✗ PointCloud 原生开发需要 Conda（推荐 Miniforge）' >&2; return 1; }; fi
  [ -x "$conda_bin" ] || { echo '✗ Conda 可执行文件不可用' >&2; return 1; }
  mode=create
  if [ -d "$prefix/conda-meta" ]; then mode=install; else create_options=(--no-default-packages); fi
  "$conda_bin" "$mode" --yes "${create_options[@]}" --override-channels --strict-channel-priority \
    --channel conda-forge --prefix "$prefix" --file "$ROOT_DIR/engines/pointcloud-workflow/native-packages.txt" || return 1
  addp_pointcloud_packages_current
}

addp_prepare_pointcloud_workflow() (
  local runtime_dir="$ROOT_DIR/engines/pointcloud-workflow" port="${POINTCLOUD_WORKFLOW_PORT:-8102}"
  local python_bin="$ROOT_DIR/engines/pointcloud-workflow/venv/bin/python" active=0 fingerprint pid
  pid=$(cat "$ROOT_DIR/.dev-pids/pointcloud-workflow-engine.pid" 2>/dev/null || true)
  if [[ "$pid" =~ ^[0-9]+$ ]] && kill -0 "$pid" 2>/dev/null; then active=1; fi
  if addp_dev_port_busy "$port"; then
    addp_dev_owned_listener pointcloud-workflow-engine "$port" || {
      echo "✗ PointCloud 端口 $port 被外部监听者占用，请先在终端停止旧开发容器或释放端口" >&2; return 1;
    }
    active=1
  fi
  if [ "$active" = 1 ]; then
    addp_pointcloud_packages_current || { echo '✗ PointCloud 原生包需要更新，请先停止该 Runtime 再启动' >&2; return 1; }
  else
    addp_with_python_dependency_lock "$ROOT_DIR" bash -c '
      set -e
      ROOT_DIR="$1"
      source "$ROOT_DIR/scripts/dev/pointcloud-workflow.sh"
      addp_install_pointcloud_native_packages
    ' _ "$ROOT_DIR" || return 1
  fi
  addp_pointcloud_native_environment
  if [ "$active" = 1 ]; then
    fingerprint=$(addp_python_dependency_fingerprint "$ROOT_DIR" "$runtime_dir") || return 1
    addp_python_dependencies_current "$runtime_dir" "$fingerprint" 'PointCloud Workflow' || {
      echo '✗ PointCloud Python 依赖需要更新，请先停止该 Runtime 再启动' >&2; return 1;
    }
  else
    addp_sync_python_dependencies "$ROOT_DIR" "$runtime_dir" 'PointCloud Workflow' || return 1
  fi
  "$python_bin" "$runtime_dir/native_check.py" "$runtime_dir/venv" || return 1
  (cd "$runtime_dir" && "$python_bin" -c 'import api_server') || return 1
)

addp_launch_pointcloud_workflow() {
  local runtime_dir="$ROOT_DIR/engines/pointcloud-workflow" port="${POINTCLOUD_WORKFLOW_PORT:-8102}"
  local pidfile="$ROOT_DIR/.dev-pids/pointcloud-workflow-engine.pid" pid attempt
  ! addp_dev_port_busy "$port" || { echo '✗ PointCloud 端口尚未释放' >&2; return 1; }
  mkdir -p "$ROOT_DIR/.dev-pids" "$ROOT_DIR/logs"
  (
    cd "$runtime_dir" || exit 1
    addp_pointcloud_native_environment
    unset RUNTIME_PUBLIC_PORT POINTCLOUD_OBJECT_STORE_LOOPBACK_HOST
    export PORT="$port" WORKFLOW_BIND_HOST=127.0.0.1 RUNTIME_HOST=localhost
    export POINTCLOUD_WORK_DIR="${POINTCLOUD_WORK_HOST_PATH:-$ROOT_DIR/data/pointcloud-work}"
    [[ "$POINTCLOUD_WORK_DIR" = /* ]] || POINTCLOUD_WORK_DIR="$ROOT_DIR/$POINTCLOUD_WORK_DIR"
    export CPL_TMPDIR="$POINTCLOUD_WORK_DIR"
    mkdir -p "$POINTCLOUD_WORK_DIR" || exit 1
    exec "$runtime_dir/venv/bin/python" api_server.py
  ) > "$ROOT_DIR/logs/pointcloud-workflow-engine.log" 2>&1 &
  pid=$!
  printf '%s\n' "$pid" > "$pidfile"
  for attempt in $(seq 1 60); do
    kill -0 "$pid" 2>/dev/null || break
    if addp_dev_owned_listener pointcloud-workflow-engine "$port" && \
       curl --max-time 2 -fsS "http://127.0.0.1:$port/health" 2>/dev/null | \
       python3 -c 'import json,sys; sys.exit(0 if json.load(sys.stdin).get("status") == "healthy" else 1)' 2>/dev/null && kill -0 "$pid" 2>/dev/null; then
      echo "✓ PointCloud 原生服务已就绪: http://127.0.0.1:$port (PID $pid)"
      return 0
    fi
    sleep 1
  done
  echo '✗ PointCloud 原生服务未就绪' >&2
  tail -n 100 "$ROOT_DIR/logs/pointcloud-workflow-engine.log" >&2
  kill -TERM "$pid" 2>/dev/null || true
  for attempt in $(seq 1 5); do kill -0 "$pid" 2>/dev/null || break; sleep 1; done
  kill -KILL "$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
  rm -f "$pidfile"
  return 1
}
