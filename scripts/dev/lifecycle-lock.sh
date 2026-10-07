#!/bin/bash
# lifecycle-lock.sh - ADDP 开发服务工作区生命周期互斥锁

# Python editable 安装会改写共享源码目录的元数据；仅安装命令需要串行。
# exec 保持同一 PID，文件描述符随安装进程退出而关闭，失败也不会遗留占用。
addp_with_python_dependency_lock() {
  local project_root="$1"
  shift
  mkdir -p "${project_root}/.dev-state"
  python3 - "${project_root}/.dev-state/python-dependencies.lock" "$@" <<'PY'
import fcntl
import os
import sys

with open(sys.argv[1], "a") as lock:
    fcntl.flock(lock, fcntl.LOCK_EX)
    os.set_inheritable(lock.fileno(), True)
    os.execvp(sys.argv[2], sys.argv[2:])
PY
}

# 输入和实际安装环境共同决定是否可复用；不依赖源码时间戳。
addp_python_dependency_fingerprint() {
  local project_root="$1" runtime_dir="$2"
  "$runtime_dir/venv/bin/python" - "$project_root" "$runtime_dir" <<'PY'
import hashlib
import importlib.metadata
import json
from pathlib import Path
import shlex
import sys

root, runtime = map(Path, sys.argv[1:])
files = set()

def collect_requirements(path):
    path = path.resolve()
    if path in files:
        return
    files.add(path)
    for line in path.read_text().replace('\\\n', '').splitlines():
        parts = shlex.split(line, comments=True)
        if not parts:
            continue
        option = parts[0]
        if option in ('-r', '-c', '--requirement', '--constraint'):
            collect_requirements(path.parent / parts[1])
        elif option.startswith(('--requirement=', '--constraint=')):
            collect_requirements(path.parent / option.split('=', 1)[1])
        elif option.startswith(('-r', '-c')):
            collect_requirements(path.parent / option[2:])

collect_requirements(runtime / 'requirements.txt')
common = root / 'common-python'
files.update((common / 'pyproject.toml', common / 'README.md'))
files.update(path for path in (common / 'addp_common').rglob('*')
             if path.is_file() and '__pycache__' not in path.parts
             and path.suffix not in ('.pyc', '.pyo'))
inputs = [('python-dependencies-v1',)]
inputs.extend((str(path), hashlib.sha256(path.read_bytes()).hexdigest())
              for path in sorted(files))
packages = sorted((dist.metadata['Name'] or '', dist.version,
                   str(dist.locate_file('')), dist.requires or [],
                   dist.read_text('direct_url.json') or '')
                  for dist in importlib.metadata.distributions())
environment = (sys.executable, sys.prefix, sys.base_prefix, sys.version, packages)

def digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True).encode()).hexdigest()

print(digest(inputs) + ':' + digest(environment))
PY
}

addp_python_dependencies_current() {
  local runtime_dir="$1" fingerprint="$2" label="$3"
  local fingerprint_file="$runtime_dir/venv/.addp-dependency-fingerprint"
  if [ -f "$fingerprint_file" ] &&
     [ "$(cat "$fingerprint_file")" = "$fingerprint" ] &&
     "$runtime_dir/venv/bin/python" -m pip check >/dev/null 2>&1; then
    echo "✓ $label Python 依赖未变化，跳过安装"
    return 0
  fi
  return 1
}

# 仅失效环境进入安装锁；锁内重查，避免等待者重复安装。
addp_install_python_dependencies() {
  local project_root="$1"
  local runtime_dir="$2"
  local label="$3"
  local python_bin="$runtime_dir/venv/bin/python"
  local fingerprint_file="$runtime_dir/venv/.addp-dependency-fingerprint"
  local fingerprint installed_fingerprint temporary
  local pip_args=(-m pip install -r "$runtime_dir/requirements.txt" -e "$project_root/common-python")

  fingerprint=$(addp_python_dependency_fingerprint "$project_root" "$runtime_dir") || return 1
  addp_python_dependencies_current "$runtime_dir" "$fingerprint" "$label" && return 0
  rm -f "$fingerprint_file" || return 1

  if [ -n "${PIP_INDEX_URL:-}" ]; then
    pip_args+=(-i "$PIP_INDEX_URL")
    if [ -n "${PIP_TRUSTED_HOST:-}" ]; then
      pip_args+=(--trusted-host "$PIP_TRUSTED_HOST")
    fi
  fi

  echo "同步 $label Python 依赖..."
  if ! "$python_bin" "${pip_args[@]}" ||
     ! "$python_bin" -m pip check; then
    echo "✗ $label Python 依赖同步失败" >&2
    return 1
  fi
  installed_fingerprint=$(addp_python_dependency_fingerprint "$project_root" "$runtime_dir") || return 1
  if [ "${fingerprint%%:*}" != "${installed_fingerprint%%:*}" ]; then
    echo "✗ $label 依赖输入在安装期间变化，请重新执行" >&2
    return 1
  fi
  temporary=$(mktemp "$fingerprint_file.XXXXXX") || return 1
  if ! printf '%s\n' "$installed_fingerprint" > "$temporary" ||
     ! mv -f "$temporary" "$fingerprint_file"; then
    rm -f "$temporary"
    return 1
  fi
  echo "✓ $label Python 依赖同步完成"
}

# 宿主机 Python Runtime 按完整声明检查与同步；启动和重启共用。
addp_sync_python_dependencies() {
  local project_root="$1" runtime_dir="$2" label="$3" fingerprint
  if [ ! -x "$runtime_dir/venv/bin/python" ]; then
    echo "✗ $label 虚拟环境不存在或不可执行，请先使用 start.sh 创建环境" >&2
    return 1
  fi
  fingerprint=$(addp_python_dependency_fingerprint "$project_root" "$runtime_dir") || return 1
  addp_python_dependencies_current "$runtime_dir" "$fingerprint" "$label" && return 0
  addp_with_python_dependency_lock "$project_root" bash -c '
    source "$1"
    shift
    addp_install_python_dependencies "$@"
  ' _ "${BASH_SOURCE[0]}" "$project_root" "$runtime_dir" "$label"
}

addp_process_is_descendant_of() {
  local process_pid="$1"
  local ancestor_pid="$2"
  local current_pid="$process_pid"

  while [ -n "$current_pid" ] && [ "$current_pid" -gt 1 ] 2>/dev/null; do
    if [ "$current_pid" = "$ancestor_pid" ]; then
      return 0
    fi
    current_pid=$(ps -p "$current_pid" -o ppid= 2>/dev/null | tr -d ' ' || true)
  done
  return 1
}

addp_read_lifecycle_lock_field() {
  local field="$1"
  local metadata_file="$2"
  sed -n "s/^${field}=//p" "$metadata_file" 2>/dev/null | head -n 1
}

addp_acquire_lifecycle_lock() {
  local operation="$1"
  shift || true

  ADDP_LIFECYCLE_STATE_DIR="${ROOT_DIR}/.dev-state"
  ADDP_LIFECYCLE_LOCK_DIR="${ADDP_LIFECYCLE_STATE_DIR}/lifecycle.lock"
  ADDP_LIFECYCLE_LOCK_METADATA="${ADDP_LIFECYCLE_LOCK_DIR}/owner"
  mkdir -p "$ADDP_LIFECYCLE_STATE_DIR"

  if [ -n "${ADDP_LIFECYCLE_OWNER_PID:-}" ] && [ -d "$ADDP_LIFECYCLE_LOCK_DIR" ]; then
    local recorded_owner
    recorded_owner=$(addp_read_lifecycle_lock_field pid "$ADDP_LIFECYCLE_LOCK_METADATA")
    if [ "$recorded_owner" = "$ADDP_LIFECYCLE_OWNER_PID" ] &&
      addp_process_is_descendant_of "$$" "$ADDP_LIFECYCLE_OWNER_PID"; then
      if [ "$$" = "$ADDP_LIFECYCLE_OWNER_PID" ]; then
        ADDP_LIFECYCLE_LOCK_INHERITED=0
        trap addp_release_lifecycle_lock EXIT
        trap 'exit 130' INT
        trap 'exit 143' TERM
      else
        ADDP_LIFECYCLE_LOCK_INHERITED=1
      fi
      export ADDP_LIFECYCLE_OWNER_PID ADDP_LIFECYCLE_LOCK_DIR ADDP_LIFECYCLE_LOCK_INHERITED
      return 0
    fi
    echo "❌ 无效的开发环境生命周期锁继承，已拒绝执行 ${operation}" >&2
    return 1
  fi

  if ! mkdir "$ADDP_LIFECYCLE_LOCK_DIR" 2>/dev/null; then
    local holder_pid holder_operation holder_args holder_started
    holder_pid=$(addp_read_lifecycle_lock_field pid "$ADDP_LIFECYCLE_LOCK_METADATA")
    holder_operation=$(addp_read_lifecycle_lock_field operation "$ADDP_LIFECYCLE_LOCK_METADATA")
    holder_args=$(addp_read_lifecycle_lock_field args "$ADDP_LIFECYCLE_LOCK_METADATA")
    holder_started=$(addp_read_lifecycle_lock_field started_at "$ADDP_LIFECYCLE_LOCK_METADATA")

    if [ -n "$holder_pid" ] && ! ps -p "$holder_pid" >/dev/null 2>&1; then
      echo "❌ 检测到失效的开发环境生命周期锁：${ADDP_LIFECYCLE_LOCK_DIR}" >&2
      echo "   原持有进程 PID ${holder_pid} 已不存在，请确认没有生命周期操作后删除该锁目录。" >&2
      return 1
    fi

    echo "❌ ADDP 开发环境正在执行生命周期操作，拒绝并行执行 ${operation}" >&2
    echo "   当前操作: ${holder_operation:-unknown} ${holder_args:-}" >&2
    echo "   持有进程: ${holder_pid:-unknown}" >&2
    echo "   开始时间: ${holder_started:-unknown}" >&2
    return 1
  fi

  ADDP_LIFECYCLE_OWNER_PID="$$"
  ADDP_LIFECYCLE_LOCK_INHERITED=0
  export ADDP_LIFECYCLE_OWNER_PID ADDP_LIFECYCLE_LOCK_DIR ADDP_LIFECYCLE_LOCK_INHERITED
  {
    printf 'pid=%s\n' "$$"
    printf 'operation=%s\n' "$operation"
    printf 'args=%s\n' "$*"
    printf 'started_at=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
    printf 'workspace=%s\n' "$ROOT_DIR"
  } > "$ADDP_LIFECYCLE_LOCK_METADATA"

  trap addp_release_lifecycle_lock EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
}

addp_release_lifecycle_lock() {
  local exit_code=$?
  trap - EXIT INT TERM
  if [ "${ADDP_LIFECYCLE_LOCK_INHERITED:-1}" = "0" ] &&
    [ "${ADDP_LIFECYCLE_OWNER_PID:-}" = "$$" ] &&
    [ -d "${ADDP_LIFECYCLE_LOCK_DIR:-}" ]; then
    rm -rf "$ADDP_LIFECYCLE_LOCK_DIR"
  fi
  return "$exit_code"
}
