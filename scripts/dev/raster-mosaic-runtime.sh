#!/usr/bin/env bash
# Manager-owned native runtime, sourced after actual ports are resolved.
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/gdal-env.sh"

addp_prepare_raster_mosaic_runtime() {
  local runtime_dir="$ROOT_DIR/manager/raster-mosaic-runtime" python_bin python_source
  # Preparation may replace an invalid environment; never mutate a live runtime.
  if addp_dev_port_busy "${RASTER_MOSAIC_RUNTIME_PORT:-8291}"; then
    echo '✗ Raster Mosaic 端口仍被占用，不能改写环境；请先停止所属 Runtime' >&2
    return 1
  fi
  local pidfile="$ROOT_DIR/.dev-pids/raster-mosaic-runtime.pid" pid
  if [ -f "$pidfile" ]; then
    pid=$(cat "$pidfile")
    if [[ "$pid" =~ ^[0-9]+$ ]] && kill -0 "$pid" 2>/dev/null; then
      echo '✗ Raster Mosaic 进程仍在运行，不能改写环境' >&2
      return 1
    fi
  fi
  addp_gdal_native_environment || return 1
  python_bin="$runtime_dir/venv/bin/python"
  if [ ! -x "$python_bin" ] || ! "$python_bin" -c 'import sys; from pathlib import Path; assert sys.version_info[:2] == (3, 12); assert "include-system-site-packages = false" in Path(sys.prefix, "pyvenv.cfg").read_text()' >/dev/null 2>&1; then
    if [ "$(uname -s)" = Darwin ]; then
      python_source="$(brew --prefix python@3.12)/bin/python3.12" || return 1
    else
      python_source=$(command -v python3.12) || return 1
    fi
    [ -x "$python_source" ] || { echo '✗ Raster Mosaic 原生开发需要 Python 3.12' >&2; return 1; }
    echo 'Raster Mosaic: 重建独立 Python 3.12 环境...'
    "$python_source" -m venv --clear "$runtime_dir/venv" || return 1
  fi
  addp_with_python_dependency_lock "$ROOT_DIR" "$python_bin" -m pip install -r "$runtime_dir/requirements.txt" || return 1
  addp_prepare_gdal_binding "$python_bin" 'Raster Mosaic' || return 1
  "$python_bin" - <<'PY' || return 1
from osgeo import gdal, gdal_array, osr
import numpy as np
gdal.UseExceptions()
osr.UseExceptions()
for name in ('GTiff', 'COG'):
    assert gdal.GetDriverByName(name), '缺少 ' + name
s = osr.SpatialReference()
assert s.ImportFromEPSG(4326) == 0, 'PROJ 坐标系解析失败'
ds = gdal.GetDriverByName('MEM').Create('', 2, 2, 1, gdal.GDT_Byte)
ds.GetRasterBand(1).WriteArray(np.ones((2, 2), dtype=np.uint8))
assert np.array_equal(ds.ReadAsArray(), np.ones((2, 2), dtype=np.uint8)), 'GDAL NumPy 数组绑定不可用'
PY
  (cd "$runtime_dir" && "$python_bin" -c 'import app') || return 1
}

addp_wait_raster_mosaic_runtime() {
  local pid="$1" port="$2" attempt
  for attempt in $(seq 1 "${MAX_WAIT:-60}"); do
    kill -0 "$pid" 2>/dev/null || break
    if addp_dev_owned_listener raster-mosaic-runtime "$port" && curl --noproxy '*' --max-time 2 -fsS "http://127.0.0.1:$port/health" >/dev/null 2>&1 && kill -0 "$pid" 2>/dev/null; then
      echo "✓ Raster Mosaic 原生服务就绪: http://127.0.0.1:$port (PID $pid)"
      return 0
    fi
    sleep 1
  done
  echo '✗ Raster Mosaic 原生服务未就绪' >&2
  tail -n 100 "$ROOT_DIR/logs/raster-mosaic-runtime.log" >&2
  return 1
}

addp_launch_raster_mosaic_runtime() {
  local runtime_dir="$ROOT_DIR/manager/raster-mosaic-runtime" port="${RASTER_MOSAIC_RUNTIME_PORT:-8291}"
  local pidfile="$ROOT_DIR/.dev-pids/raster-mosaic-runtime.pid" pid attempt
  addp_gdal_native_environment || return 1
  ! addp_dev_port_busy "$port" || { echo '✗ Raster Mosaic 端口尚未释放' >&2; return 1; }
  mkdir -p "$ROOT_DIR/.dev-pids" "$ROOT_DIR/logs"
  (
    cd "$runtime_dir" || exit 1
    export PORT="$port" SYSTEM_URL="${SYSTEM_URL:-http://localhost:${SYSTEM_BACKEND_PORT:-8180}}"
    exec "$runtime_dir/venv/bin/python" app.py
  ) > "$ROOT_DIR/logs/raster-mosaic-runtime.log" 2>&1 &
  pid=$!
  printf '%s\n' "$pid" > "$pidfile"
  if addp_wait_raster_mosaic_runtime "$pid" "$port"; then
    return 0
  fi
  kill -TERM "$pid" 2>/dev/null || true
  for attempt in $(seq 1 5); do kill -0 "$pid" 2>/dev/null || break; sleep 1; done
  kill -KILL "$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
  rm -f "$pidfile"
  return 1
}
