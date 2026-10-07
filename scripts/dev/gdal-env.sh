#!/usr/bin/env bash
# Native GDAL toolchain shared by the development runtimes.

addp_gdal_native_environment() {
  local prefix pkg_config
  unset GDAL_DRIVER_PATH GDAL_DATA PROJ_DATA PROJ_LIB
  if [ "$(uname -s)" = Darwin ]; then
    command -v brew >/dev/null 2>&1 || return 1
    prefix=$(brew --prefix gdal) || return 1
    [ -x "$prefix/bin/gdal-config" ] || { echo '✗ GDAL Runtime 缺少 Homebrew GDAL，请执行 brew install gdal' >&2; return 1; }
    pkg_config="$(brew --prefix pkgconf)/bin/pkg-config" || return 1
    [ -x "$pkg_config" ] || { echo '✗ GDAL Runtime 缺少 Homebrew pkg-config' >&2; return 1; }
    # GDAL's Python build also resolves gdal-config through PATH.
    export PATH="$prefix/bin:$(dirname "$pkg_config"):$PATH"
    prefix=$(brew --prefix proj) || return 1
    PROJ_DATA=$("$pkg_config" --variable=datadir "$prefix/lib/pkgconfig/proj.pc") || return 1
  else
    command -v pkg-config >/dev/null 2>&1 || { echo '✗ GDAL Runtime 需要 pkg-config 解析原生 PROJ 资源目录' >&2; return 1; }
    PROJ_DATA=$(pkg-config --variable=datadir proj) || return 1
  fi
  command -v gdal-config >/dev/null 2>&1 || { echo '✗ GDAL Runtime 需要原生 GDAL（macOS: brew install gdal）' >&2; return 1; }
  GDAL_DATA=$(gdal-config --datadir) || return 1
  export GDAL_DATA PROJ_DATA PROJ_NETWORK=OFF
  [ -f "$PROJ_DATA/proj.db" ] && [ -d "$GDAL_DATA" ] || { echo '✗ 原生 GDAL/PROJ 资源目录不完整' >&2; return 1; }
}

addp_prepare_gdal_binding() {
  local python_bin="$1" label="$2" version
  version=$(gdal-config --version) || return 1
  echo "${label} 原生 GDAL: $version ($(command -v gdal-config))"
  # Installed package metadata cannot prove that its native library still loads.
  if ! "$python_bin" -c 'from osgeo import gdal, gdal_array, ogr, osr; import sys; assert gdal.VersionInfo("RELEASE_NAME") == sys.argv[1], "GDAL Python/原生版本不一致"' "$version"; then
    echo "${label} GDAL 绑定不可用，正在从源码重建 $version..."
    # Never reuse a wheel linked against the previous native library, or upgrade
    # the already synchronized Python dependencies while repairing the binding.
    addp_with_python_dependency_lock "$ROOT_DIR" "$python_bin" -m pip install --force-reinstall --no-cache-dir --no-binary=GDAL --no-deps "GDAL==$version" || return 1
  fi
  "$python_bin" -m pip check || return 1
  "$python_bin" -c 'from osgeo import gdal, gdal_array, ogr, osr; import sys; assert gdal.VersionInfo("RELEASE_NAME") == sys.argv[1], "GDAL Python/原生版本不一致"' "$version"
}
