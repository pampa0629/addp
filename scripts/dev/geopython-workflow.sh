#!/usr/bin/env bash
# Shared native development entry, sourced after actual ports are resolved.

addp_geopython_native_environment() {
  local prefix driver candidate pkg_config
  unset GDAL_DRIVER_PATH GDAL_DATA PROJ_DATA PROJ_LIB GEOPYTHON_WORKFLOW_LOOPBACK_HOST
  if [ "$(uname -s)" = Darwin ]; then
    command -v brew >/dev/null 2>&1 || return 1
    prefix=$(brew --prefix gdal) || return 1
    [ -x "$prefix/bin/gdal-config" ] || { echo '✗ GeoPython 缺少 Homebrew GDAL，请执行 brew install gdal' >&2; return 1; }
    pkg_config="$(brew --prefix pkgconf)/bin/pkg-config" || return 1
    [ -x "$pkg_config" ] || { echo '✗ GeoPython 缺少 Homebrew pkg-config' >&2; return 1; }
    # GDAL's Python build also resolves gdal-config through PATH.
    export PATH="$prefix/bin:$(dirname "$pkg_config"):$PATH"
    prefix=$(brew --prefix proj) || return 1
    PROJ_DATA=$("$pkg_config" --variable=datadir "$prefix/lib/pkgconfig/proj.pc") || return 1
  else
    command -v pkg-config >/dev/null 2>&1 || { echo '✗ GeoPython 需要 pkg-config 解析原生 PROJ 资源目录' >&2; return 1; }
    PROJ_DATA=$(pkg-config --variable=datadir proj) || return 1
  fi
  command -v gdal-config >/dev/null 2>&1 || { echo '✗ GeoPython 需要原生 GDAL（macOS: brew install gdal mdbtools）' >&2; return 1; }
  GDAL_DATA=$(gdal-config --datadir) || return 1
  export GDAL_DATA PROJ_DATA PROJ_NETWORK=OFF
  [ -f "$PROJ_DATA/proj.db" ] && [ -d "$GDAL_DATA" ] || { echo '✗ 原生 GDAL/PROJ 资源目录不完整' >&2; return 1; }
  driver=''
  if [ "$(uname -s)" = Darwin ]; then
    command -v brew >/dev/null 2>&1 || return 1
    prefix=$(brew --prefix mdbtools) || return 1
    candidate="$prefix/lib/odbc/libmdbodbc.dylib"
    [ ! -f "$candidate" ] || driver="$candidate"
  else
    for candidate in /usr/lib/*/odbc/libmdbodbc.so /usr/lib/odbc/libmdbodbc.so; do
      [ ! -f "$candidate" ] || { driver="$candidate"; break; }
    done
  fi
  [ -n "$driver" ] || { echo '✗ GeoPython 缺少 MDBTools ODBC 驱动，不能保留 PGeo 能力' >&2; return 1; }
  export ODBCSYSINI="$ROOT_DIR/.dev-state/geopython-odbc" ODBCINSTINI=odbcinst.ini
  mkdir -p "$ODBCSYSINI"
  for candidate in 'Microsoft Access Driver (*.mdb, *.accdb)' 'Microsoft Access Driver (*.mdb)'; do
    printf '[%s]\nDescription=MDBTools for GeoPython PGeo\nDriver=%s\nFileUsage=1\n\n' "$candidate" "$driver"
  done > "$ODBCSYSINI/odbcinst.ini"
}

addp_prepare_geopython_workflow() {
  local runtime_dir="$ROOT_DIR/engines/geopython-workflow" python_bin version
  addp_geopython_native_environment || return 1
  python_bin="$runtime_dir/venv/bin/python"
  if [ ! -x "$python_bin" ]; then
    command -v python3.12 >/dev/null 2>&1 || { echo '✗ GeoPython 原生开发需要 Python 3.12' >&2; return 1; }
    python3.12 -m venv "$runtime_dir/venv" || return 1
  fi
  "$python_bin" -c 'import sys; assert sys.version_info[:2] == (3,12), "GeoPython 要求 Python 3.12"; from pathlib import Path; assert "include-system-site-packages = false" in Path(sys.prefix, "pyvenv.cfg").read_text(), "GeoPython 不允许继承系统 site-packages，请重建独立 venv"' || return 1
  addp_sync_python_dependencies "$ROOT_DIR" "$runtime_dir" 'GeoPython Workflow' || return 1
  version=$(gdal-config --version) || return 1
  echo "GeoPython 原生 GDAL: $version ($(command -v gdal-config))"
  # Installed package metadata cannot prove that its native library still loads.
  if ! "$python_bin" -c 'from osgeo import gdal, ogr, osr; import sys; assert gdal.VersionInfo("RELEASE_NAME") == sys.argv[1], "GDAL Python/原生版本不一致"' "$version"; then
    echo "GeoPython GDAL 绑定不可用，正在从源码重建 $version..."
    # Never reuse a wheel linked against the previous native library, or upgrade
    # the already synchronized Python dependencies while repairing the binding.
    addp_with_python_dependency_lock "$ROOT_DIR" "$python_bin" -m pip install --force-reinstall --no-cache-dir --no-binary=GDAL --no-deps "GDAL==$version" || return 1
  fi
  "$python_bin" -m pip check || return 1
  "$python_bin" - "$version" <<'PY' || return 1
from osgeo import gdal, ogr, osr
import sys
assert gdal.VersionInfo('RELEASE_NAME') == sys.argv[1], 'GDAL Python/原生版本不一致'
for name in ('PGeo', 'OpenFileGDB'):
    assert ogr.GetDriverByName(name), '缺少 ' + name
assert ogr.GetDriverByName('OpenFileGDB').GetMetadataItem('DCAP_CREATE') == 'YES'
for name in ('GTiff', 'COG'):
    assert gdal.GetDriverByName(name), '缺少 ' + name
s = osr.SpatialReference()
assert s.ImportFromEPSG(4326) == 0, 'PROJ 坐标系解析失败'
PY
  (cd "$runtime_dir" && "$python_bin" -c 'import api_server') || return 1
  if addp_dev_port_busy "${GEOPYTHON_WORKFLOW_PORT:-8099}" && ! addp_dev_owned_listener geopython-workflow-engine "${GEOPYTHON_WORKFLOW_PORT:-8099}"; then
    echo '✗ GeoPython 端口由外部监听者占用，请先在终端停止旧容器或释放端口' >&2
    return 1
  fi
}

addp_launch_geopython_workflow() {
  local runtime_dir="$ROOT_DIR/engines/geopython-workflow" port="${GEOPYTHON_WORKFLOW_PORT:-8099}"
  local pidfile="$ROOT_DIR/.dev-pids/geopython-workflow-engine.pid" pid attempt
  addp_geopython_native_environment || return 1
  ! addp_dev_port_busy "$port" || { echo '✗ GeoPython 端口尚未释放' >&2; return 1; }
  mkdir -p "$ROOT_DIR/.dev-pids" "$ROOT_DIR/logs"
  (
    cd "$runtime_dir" || exit 1
    export PORT="$port" WORKFLOW_BIND_HOST=127.0.0.1 RUNTIME_HOST=localhost
    unset RUNTIME_PUBLIC_PORT
    exec "$runtime_dir/venv/bin/python" api_server.py
  ) > "$ROOT_DIR/logs/geopython-workflow-engine.log" 2>&1 &
  pid=$!
  printf '%s\n' "$pid" > "$pidfile"
  for attempt in $(seq 1 60); do
    kill -0 "$pid" 2>/dev/null || break
    if addp_dev_owned_listener geopython-workflow-engine "$port" && curl --noproxy '*' --max-time 2 -fsS "http://127.0.0.1:$port/health" >/dev/null 2>&1 && kill -0 "$pid" 2>/dev/null; then
      echo "✓ GeoPython 原生服务就绪: http://127.0.0.1:$port (PID $pid)"
      return 0
    fi
    sleep 1
  done
  echo '✗ GeoPython 原生服务未就绪' >&2
  tail -n 100 "$ROOT_DIR/logs/geopython-workflow-engine.log" >&2
  kill -TERM "$pid" 2>/dev/null || true
  for attempt in $(seq 1 5); do kill -0 "$pid" 2>/dev/null || break; sleep 1; done
  kill -KILL "$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
  rm -f "$pidfile"
  return 1
}
