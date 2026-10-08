#!/usr/bin/env bash
# Unique native development path; source after port and dependency helpers.

addp_model3d_native_environment() {
  local prefix python_bin="$ROOT_DIR/engines/model3d-workflow/venv/bin/python"
  unset PYTHONHOME PYTHONPATH CONDA_PREFIX CONDA_DEFAULT_ENV
  unset GDAL_DRIVER_PATH GDAL_DATA PROJ_DATA PROJ_LIB OSG_LIBRARY_PATH
  unset LD_LIBRARY_PATH DYLD_LIBRARY_PATH DYLD_FALLBACK_LIBRARY_PATH CMAKE_PREFIX_PATH PKG_CONFIG_PATH
  unset MODEL3D_CONVERTER_IMAGE MODEL3D_CONVERTER_PLATFORM RUNTIME_PUBLIC_PORT
  if [ ! -x "$python_bin" ]; then
    python_bin=$(command -v python3.12) || { echo '✗ Model3D 原生开发需要 Python 3.12' >&2; return 1; }
  fi
  prefix=$("$python_bin" "$ROOT_DIR/engines/model3d-workflow/native_setup.py" environment "$ROOT_DIR/.dev-state/model3d-native") || return 1
  export MODEL3D_CONVERTER_BIN="$prefix/bin/_3dtile"
  export MODEL3D_MESH_CONVERTER_BIN="$prefix/bin/assimp" MODEL3D_IFC_CONVERTER_BIN="$prefix/bin/IfcConvert"
  export GDAL_DATA="$prefix/bin/gdal" PROJ_DATA="$prefix/bin/proj" OSG_LIBRARY_PATH="$prefix/bin/osgPlugins-3.6.5"
  export MODEL3D_GAUSSIAN_SPLAT_NODE_BIN
  MODEL3D_GAUSSIAN_SPLAT_NODE_BIN=$(command -v node) || { echo '✗ Model3D 需要 Node.js' >&2; return 1; }
  export PYTHONNOUSERSITE=1
}

addp_model3d_python_current() {
  "$1" -c 'import sys; from pathlib import Path; sys.exit(0 if sys.version_info[:2] == (3,12) and "include-system-site-packages = false" in Path(sys.prefix, "pyvenv.cfg").read_text() else "Model3D requires an isolated Python 3.12 venv")'
}

addp_prepare_model3d_workflow() (
  local runtime_dir="$ROOT_DIR/engines/model3d-workflow" port="${MODEL3D_WORKFLOW_PORT:-8101}"
  local python_bin="$runtime_dir/venv/bin/python" active=0 fingerprint pid
  pid=$(cat "$ROOT_DIR/.dev-pids/model3d-workflow-engine.pid" 2>/dev/null || true)
  if [[ "$pid" =~ ^[0-9]+$ ]] && kill -0 "$pid" 2>/dev/null; then active=1; fi
  if addp_dev_port_busy "$port"; then
    addp_dev_owned_listener model3d-workflow-engine "$port" || {
      echo "✗ Model3D 端口 $port 被外部监听者占用，请先释放端口" >&2; return 1;
    }
    active=1
  fi
  if command -v docker >/dev/null 2>&1 && docker container inspect model3d-workflow-engine >/dev/null 2>&1; then
    echo '✗ 请先在自己的终端停止、删除旧 Model3D 开发容器，再启动原生 Runtime' >&2
    return 1
  fi
  addp_model3d_native_environment || return 1
  if [ "$active" = 1 ]; then
    "$python_bin" "$runtime_dir/native_setup.py" current "$ROOT_DIR/.dev-state/model3d-native" || {
      echo '✗ Model3D 原生工具需要准备或更新，请先执行 README 的独立工具准备命令；不得修改活动 Python 环境' >&2; return 1;
    }
    fingerprint=$(addp_python_dependency_fingerprint "$ROOT_DIR" "$runtime_dir") || return 1
    addp_python_dependencies_current "$runtime_dir" "$fingerprint" 'Model3D' || {
      echo '✗ Model3D Python 依赖需要更新，请先停止该 Runtime 再启动' >&2; return 1;
    }
  else
    addp_with_python_dependency_lock "$ROOT_DIR" bash -c '
      set -e
      runtime_dir="$1/engines/model3d-workflow"
      if [ ! -x "$runtime_dir/venv/bin/python" ]; then
        command -v python3.12 >/dev/null || { echo "✗ Model3D 原生开发需要 Python 3.12" >&2; exit 1; }
        python3.12 -m venv "$runtime_dir/venv"
      fi
      ROOT_DIR="$1"
      source "$ROOT_DIR/scripts/dev/model3d-workflow.sh"
      addp_model3d_python_current "$runtime_dir/venv/bin/python"
    ' _ "$ROOT_DIR" || return 1
    addp_sync_python_dependencies "$ROOT_DIR" "$runtime_dir" 'Model3D' || return 1
    "$python_bin" "$runtime_dir/native_setup.py" prepare "$ROOT_DIR/.dev-state/model3d-native" || return 1
  fi
  addp_model3d_python_current "$python_bin" || return 1
  if [ ! -d "$runtime_dir/node_modules/@mkkellogg/gaussian-splats-3d" ]; then
    [ "$active" = 0 ] || { echo '✗ Model3D Node 依赖缺失，请先停止该 Runtime 再启动' >&2; return 1; }
    addp_install_node_dependencies "$runtime_dir" --omit=dev || return 1
  fi
  "$python_bin" "$runtime_dir/native_setup.py" verify "$ROOT_DIR/.dev-state/model3d-native" || return 1
  (cd "$runtime_dir" && "$python_bin" -c 'import api_server') || return 1
)

addp_launch_model3d_workflow() {
  local runtime_dir="$ROOT_DIR/engines/model3d-workflow" port="${MODEL3D_WORKFLOW_PORT:-8101}"
  local pidfile="$ROOT_DIR/.dev-pids/model3d-workflow-engine.pid" pid attempt
  ! addp_dev_port_busy "$port" || { echo '✗ Model3D 端口尚未释放' >&2; return 1; }
  mkdir -p "$ROOT_DIR/.dev-pids" "$ROOT_DIR/logs"
  (
    cd "$runtime_dir" || exit 1
    addp_model3d_native_environment || exit 1
    export PORT="$port" WORKFLOW_BIND_HOST=127.0.0.1 RUNTIME_HOST=localhost
    exec "$runtime_dir/venv/bin/python" api_server.py
  ) > "$ROOT_DIR/logs/model3d-workflow-engine.log" 2>&1 &
  pid=$!
  printf '%s\n' "$pid" > "$pidfile"
  for attempt in $(seq 1 60); do
    kill -0 "$pid" 2>/dev/null || break
    if addp_dev_owned_listener model3d-workflow-engine "$port" && \
       curl --noproxy '*' --max-time 2 -fsS "http://127.0.0.1:$port/health" 2>/dev/null | \
       python3 -c 'import json,sys; sys.exit(0 if json.load(sys.stdin).get("status") == "healthy" else 1)' 2>/dev/null && kill -0 "$pid" 2>/dev/null; then
      echo "✓ Model3D 原生服务已就绪: http://127.0.0.1:$port (PID $pid)"
      return 0
    fi
    sleep 1
  done
  echo '✗ Model3D 原生服务未就绪' >&2
  tail -n 100 "$ROOT_DIR/logs/model3d-workflow-engine.log" >&2
  kill -TERM "$pid" 2>/dev/null || true
  for attempt in $(seq 1 5); do kill -0 "$pid" 2>/dev/null || break; sleep 1; done
  kill -KILL "$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
  rm -f "$pidfile"
  return 1
}
