#!/usr/bin/env bash
# Unique native development path; sourced after ports and dependency-lock helpers.

addp_document_native_environment() {
  local paths python_bin="$ROOT_DIR/engines/document-workflow/venv/bin/python"
  unset PYTHONHOME PYTHONPATH CONDA_PREFIX LD_LIBRARY_PATH DYLD_LIBRARY_PATH DYLD_FALLBACK_LIBRARY_PATH
  unset FONTCONFIG_PATH FONTCONFIG_FILE SAL_FONTPATH SAL_USE_VCLPLUGIN
  unset DOCUMENT_OBJECT_STORE_LOOPBACK_HOST RUNTIME_PUBLIC_PORT
  if [ ! -x "$python_bin" ]; then
    python_bin=$(command -v python3.12) || { echo '✗ Document 原生开发需要 Python 3.12' >&2; return 1; }
  fi
  paths=$("$python_bin" "$ROOT_DIR/engines/document-workflow/native_setup.py" environment "$ROOT_DIR/.dev-state/document-native") || return 1
  export DOCUMENT_LIBREOFFICE_BIN="${paths%%$'\n'*}" FONTCONFIG_FILE="${paths#*$'\n'}"
  export PYTHONNOUSERSITE=1 SAL_USE_VCLPLUGIN=svp
}

addp_document_python_current() {
  "$1" -c 'import sys; from pathlib import Path; sys.exit(0 if sys.version_info[:2] == (3,12) and "include-system-site-packages = false" in Path(sys.prefix, "pyvenv.cfg").read_text() else "Document requires an isolated Python 3.12 venv")'
}

addp_prepare_document_workflow() (
  local runtime_dir="$ROOT_DIR/engines/document-workflow" port="${DOCUMENT_WORKFLOW_PORT:-8105}"
  local python_bin="$runtime_dir/venv/bin/python" active=0 fingerprint pid
  pid=$(cat "$ROOT_DIR/.dev-pids/document-workflow-engine.pid" 2>/dev/null || true)
  if [[ "$pid" =~ ^[0-9]+$ ]] && kill -0 "$pid" 2>/dev/null; then active=1; fi
  if addp_dev_port_busy "$port"; then
    addp_dev_owned_listener document-workflow-engine "$port" || {
      echo "✗ Document 端口 $port 被外部监听者占用，请先停止、删除旧开发容器或释放端口" >&2; return 1;
    }
    active=1
  fi
  # Read-only migration rejection: never start a second development route on a spare port.
  if command -v docker >/dev/null 2>&1 && docker container inspect document-workflow-engine >/dev/null 2>&1; then
    echo '✗ 请先在自己的终端停止、删除旧 Document 开发容器，再启动原生 Runtime' >&2
    return 1
  fi
  [ "$(id -u)" -ne 0 ] || { echo '✗ Document 原生开发不能以 root 运行' >&2; return 1; }
  addp_document_native_environment || return 1
  if [ "$active" = 1 ]; then
    "$python_bin" "$runtime_dir/native_setup.py" current "$ROOT_DIR/.dev-state/document-native" || {
      echo '✗ Document 原生依赖需要更新，请先停止该 Runtime 再启动' >&2; return 1;
    }
    fingerprint=$(addp_python_dependency_fingerprint "$ROOT_DIR" "$runtime_dir") || return 1
    addp_python_dependencies_current "$runtime_dir" "$fingerprint" 'Document Workflow' || {
      echo '✗ Document Python 依赖需要更新，请先停止该 Runtime 再启动' >&2; return 1;
    }
  else
    addp_with_python_dependency_lock "$ROOT_DIR" bash -c '
      set -e
      runtime_dir="$1/engines/document-workflow"
      if [ ! -x "$runtime_dir/venv/bin/python" ]; then
        command -v python3.12 >/dev/null || { echo "✗ Document 原生开发需要 Python 3.12" >&2; exit 1; }
        python3.12 -m venv "$runtime_dir/venv"
      fi
      ROOT_DIR="$1"
      source "$ROOT_DIR/scripts/dev/document-workflow.sh"
      addp_document_python_current "$runtime_dir/venv/bin/python"
      "$runtime_dir/venv/bin/python" "$runtime_dir/native_setup.py" prepare "$1/.dev-state/document-native"
    ' _ "$ROOT_DIR" || return 1
    addp_sync_python_dependencies "$ROOT_DIR" "$runtime_dir" 'Document Workflow' || return 1
  fi
  addp_document_python_current "$python_bin" || return 1
  "$python_bin" "$runtime_dir/native_setup.py" verify "$ROOT_DIR/.dev-state/document-native" || return 1
  (cd "$runtime_dir" && "$python_bin" -c 'import api_server') || return 1
)

addp_launch_document_workflow() {
  local runtime_dir="$ROOT_DIR/engines/document-workflow" port="${DOCUMENT_WORKFLOW_PORT:-8105}"
  local pidfile="$ROOT_DIR/.dev-pids/document-workflow-engine.pid" pid attempt
  ! addp_dev_port_busy "$port" || { echo '✗ Document 端口尚未释放' >&2; return 1; }
  mkdir -p "$ROOT_DIR/.dev-pids" "$ROOT_DIR/logs"
  (
    cd "$runtime_dir" || exit 1
    addp_document_native_environment || exit 1
    export PORT="$port" WORKFLOW_BIND_HOST=127.0.0.1 RUNTIME_HOST=localhost
    export DOCUMENT_WORK_DIR="${DOCUMENT_WORK_HOST_PATH:-$ROOT_DIR/data/document-work}"
    [[ "$DOCUMENT_WORK_DIR" = /* ]] || DOCUMENT_WORK_DIR="$ROOT_DIR/$DOCUMENT_WORK_DIR"
    mkdir -p "$DOCUMENT_WORK_DIR" || exit 1
    exec "$runtime_dir/venv/bin/python" api_server.py
  ) > "$ROOT_DIR/logs/document-workflow-engine.log" 2>&1 &
  pid=$!
  printf '%s\n' "$pid" > "$pidfile"
  for attempt in $(seq 1 60); do
    kill -0 "$pid" 2>/dev/null || break
    if addp_dev_owned_listener document-workflow-engine "$port" && \
       curl --noproxy '*' --max-time 2 -fsS "http://127.0.0.1:$port/health" 2>/dev/null | \
       python3 -c 'import json,sys; sys.exit(0 if json.load(sys.stdin).get("status") == "healthy" else 1)' 2>/dev/null && kill -0 "$pid" 2>/dev/null; then
      echo "✓ Document 原生服务已就绪: http://127.0.0.1:$port (PID $pid)"
      return 0
    fi
    sleep 1
  done
  echo '✗ Document 原生服务未就绪' >&2
  tail -n 100 "$ROOT_DIR/logs/document-workflow-engine.log" >&2
  kill -TERM "$pid" 2>/dev/null || true
  for attempt in $(seq 1 5); do kill -0 "$pid" 2>/dev/null || break; sleep 1; done
  kill -KILL "$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
  rm -f "$pidfile"
  return 1
}
