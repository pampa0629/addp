#!/usr/bin/env bash
# Shared native development entry, sourced after actual ports are resolved.
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/gdal-env.sh"

addp_geopython_build_native() {
  local source="$1" kind="$2" flags="$3" versions="$4" key directory temporary
  local -a native_flags build_flags
  key=$({ cat "$source"; printf '%s\n' "$flags" "$versions"; } | shasum -a 256) || return 1
  key=${key%% *}
  directory="$ROOT_DIR/.dev-state/geopython-$kind/$key"
  local target="$directory/$kind-preflight"
  if [ ! -x "$target" ]; then
    mkdir -p "$directory" || return 1
    temporary=$(mktemp "$directory/.compile.XXXXXX") || return 1
    read -r -a native_flags <<< "$flags"
    build_flags=(-std=c11 -D_POSIX_C_SOURCE=200809L -Wall -Wextra -Werror)
    if [ "$kind" = grib-ccsds ]; then build_flags+=(-shared -fPIC); fi
    if ! cc "${build_flags[@]}" "$source" -o "$temporary" "${native_flags[@]}"; then
      rm -f "$temporary"
      return 1
    fi
    chmod 755 "$temporary" && mv "$temporary" "$target" || { rm -f "$temporary"; return 1; }
  fi
  printf '%s\n' "$target"
}

addp_geopython_netcdf_checker() {
  local flags versions
  flags=$(unset PKG_CONFIG_PATH PKG_CONFIG_LIBDIR; pkg-config --cflags --libs hdf5 netcdf) || return 1
  versions=$(unset PKG_CONFIG_PATH PKG_CONFIG_LIBDIR; pkg-config --modversion hdf5 netcdf) || return 1
  GEOPYTHON_NETCDF_PREFLIGHT_BIN=$(addp_geopython_build_native "$ROOT_DIR/engines/geopython-workflow/native/netcdf_preflight.c" netcdf "$flags" "$versions") || return 1
  export GEOPYTHON_NETCDF_PREFLIGHT_BIN
}

addp_geopython_grib_checker() {
  local prefix flags=-laec version
  if [ "$(uname -s)" = Darwin ]; then
    prefix=$(brew --prefix libaec) || return 1
    flags="-I$prefix/include -L$prefix/lib -laec"
    version=$(brew list --versions libaec) || return 1
  else
    version=$(dpkg-query -W libaec-dev) || return 1
  fi
  GEOPYTHON_GRIB_CCSDS_PREFLIGHT_LIB=$(addp_geopython_build_native "$ROOT_DIR/engines/geopython-workflow/native/grib_ccsds_preflight.c" grib-ccsds "$flags" "$version") || return 1
  export GEOPYTHON_GRIB_CCSDS_PREFLIGHT_LIB
}

addp_geopython_native_environment() {
  local prefix driver candidate
  unset GEOPYTHON_WORKFLOW_LOOPBACK_HOST
  addp_gdal_native_environment || return 1
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
  addp_geopython_netcdf_checker && addp_geopython_grib_checker
}

addp_prepare_geopython_workflow() {
  local runtime_dir="$ROOT_DIR/engines/geopython-workflow" python_bin version
  # Preparation must not replace libraries in a running worker.
  if addp_dev_port_busy "${GEOPYTHON_WORKFLOW_PORT:-8099}"; then
    echo '✗ GeoPython 端口仍被占用（所属 Runtime 或外部监听者），不能改写环境；请先停止所属 Runtime' >&2
    return 1
  fi
  local active_pid
  if [ -f "$ROOT_DIR/.dev-pids/geopython-workflow-engine.pid" ]; then
    active_pid=$(cat "$ROOT_DIR/.dev-pids/geopython-workflow-engine.pid")
    if [[ "$active_pid" =~ ^[0-9]+$ ]] && kill -0 "$active_pid" 2>/dev/null; then
      echo '✗ GeoPython 进程仍在运行，不能改写环境' >&2
      return 1
    fi
  fi
  addp_geopython_native_environment || return 1
  python_bin="$runtime_dir/venv/bin/python"
  if [ ! -x "$python_bin" ]; then
    command -v python3.12 >/dev/null 2>&1 || { echo '✗ GeoPython 原生开发需要 Python 3.12' >&2; return 1; }
    python3.12 -m venv "$runtime_dir/venv" || return 1
  fi
  "$python_bin" -c 'import sys; assert sys.version_info[:2] == (3,12), "GeoPython 要求 Python 3.12"; from pathlib import Path; assert "include-system-site-packages = false" in Path(sys.prefix, "pyvenv.cfg").read_text(), "GeoPython 不允许继承系统 site-packages，请重建独立 venv"' || return 1
  addp_prepare_gdal_arrow_dependencies "$runtime_dir" 'GeoPython Workflow' "$ROOT_DIR/common-python" || return 1
  version=$(gdal-config --version) || return 1
  "$python_bin" - "$version" <<'PY' || return 1
from osgeo import gdal, ogr, osr
import pyorc
assert pyorc.__version__ == "0.12.0", "ORC dependency mismatch"
import sys
assert gdal.VersionInfo('RELEASE_NAME') == sys.argv[1], 'GDAL Python/原生版本不一致'
for name in ('PGeo', 'OpenFileGDB'):
    assert ogr.GetDriverByName(name), '缺少 ' + name
assert ogr.GetDriverByName('OpenFileGDB').GetMetadataItem('DCAP_CREATE') == 'YES'
for name in ('GTiff', 'COG', 'HFA', 'JP2OpenJPEG', 'GS7BG', 'GSBG', 'GSAG'):
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
