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

addp_install_gdal_binding() {
  local python_bin="$1" label="$2" version force="${3:-0}" lock_root="${4:-}"
  local install=("$python_bin" -m pip)
  if [ -n "$lock_root" ]; then install=(addp_with_python_dependency_lock "$lock_root" "${install[@]}"); fi
  version=$(gdal-config --version) || return 1
  echo "${label} 原生 GDAL: $version ($(command -v gdal-config))"
  # Installed package metadata cannot prove that its native library still loads.
  if [ "$force" = 1 ] || ! "$python_bin" -c 'from osgeo import gdal, gdal_array, ogr, osr; import sys; assert gdal.VersionInfo("RELEASE_NAME") == sys.argv[1], "GDAL Python/原生版本不一致"' "$version"; then
    echo "${label} GDAL 绑定不可用，正在从源码重建 $version..."
    # Build against the runtime's NumPy headers so array support cannot silently
    # disappear in an isolated build environment or a previously cached wheel.
    "${install[@]}" install --no-deps 'setuptools>=67' wheel || return 1
    # Never reuse a wheel linked against the previous native library, or upgrade
    # the already synchronized Python dependencies while repairing the binding.
    "${install[@]}" install --force-reinstall --no-cache-dir --no-binary=GDAL --no-build-isolation --no-deps "GDAL==$version" || return 1
  fi
  "$python_bin" -m pip check || return 1
  "$python_bin" -c 'from osgeo import gdal, gdal_array, ogr, osr; import sys; assert gdal.VersionInfo("RELEASE_NAME") == sys.argv[1], "GDAL Python/原生版本不一致"' "$version"
}

# Raster Mosaic retains the same binding-only preparation. GeoPython calls the
# installer inside its complete native transaction, never recursively flock.
addp_prepare_gdal_binding() {
  addp_install_gdal_binding "$1" "$2" 0 "$ROOT_DIR"
}

# Caller has already resolved the native toolchain. Only this runtime consumes
# PyArrow; Raster Mosaic keeps its own minimal requirements and binding entry.
addp_prepare_gdal_arrow_dependencies() {
  addp_with_python_dependency_lock "$ROOT_DIR" bash -c '
    set -e
    source "$1/scripts/dev/lifecycle-lock.sh"
    source "$1/scripts/dev/gdal-env.sh"
    addp_install_gdal_arrow_dependencies "$2" "$3" "$4"
  ' _ "$ROOT_DIR" "$1" "$2" "$3"
}

addp_install_gdal_arrow_dependencies() (
  local runtime_dir="$1" label="$2" common_dir="$3"
  local python_bin="$runtime_dir/venv/bin/python" helper="$ROOT_DIR/scripts/dev/gdal-arrow.py"
  local manifest key cache arrow_version arrow_prefix wheel constraint fingerprint temporary
  manifest=$(mktemp "${TMPDIR:-/tmp}/addp-arrow-manifest.XXXXXX") || return 1
  # Any failed preparation invalidates both records; a prior pip install is not
  # proof of same-process native interoperability.
  trap 'exit_status=$?; rm -f "$manifest"; rm -f "$runtime_dir/venv/.addp-dependency-fingerprint" "$runtime_dir/venv/.addp-arrow-native-fingerprint"; exit "$exit_status"' EXIT
  "$python_bin" "$helper" manifest > "$manifest" || return 1
  key=$("$python_bin" "$helper" fingerprint --manifest "$manifest") || return 1
  cache="$ROOT_DIR/.dev-state/gdal-arrow/$key"
  mkdir -p "$cache" || return 1
  constraint="$cache/constraints.txt"
  arrow_version=$("$python_bin" -c 'import json,sys; a=json.load(open(sys.argv[1]))["arrow"]; print(a["version"] if a else "")' "$manifest") || return 1
  if [ -f "$runtime_dir/venv/.addp-arrow-native-fingerprint" ] &&
     [ "$(cat "$runtime_dir/venv/.addp-arrow-native-fingerprint")" = "$key" ]; then
    fingerprint=$(addp_python_dependency_fingerprint "$ROOT_DIR" "$runtime_dir" "$common_dir") || return 1
    if addp_python_dependencies_current "$runtime_dir" "$fingerprint" "$label" &&
       "$python_bin" "$helper" admit --manifest "$manifest" --order gdal-first &&
       "$python_bin" "$helper" admit --manifest "$manifest" --order arrow-first; then
      rm -f "$manifest"
      trap - EXIT
      return 0
    fi
  fi
  rm -f "$runtime_dir/venv/.addp-dependency-fingerprint"
  if [ -n "$arrow_version" ]; then
    arrow_prefix=$("$python_bin" -c 'import json,sys; print(json.load(open(sys.argv[1]))["arrow"]["prefix"])' "$manifest") || return 1
    if [ ! -f "$cache/wheel.sha256" ] || ! (cd "$cache" && shasum -a 256 -c wheel.sha256 >/dev/null 2>&1); then
      rm -f "$cache"/*.whl "$cache/wheel.sha256"
      echo "$label: 从源码构建 PyArrow ${arrow_version}，共用原生 Arrow $arrow_prefix"
      "$python_bin" "$helper" build --manifest "$manifest" --cache "$cache" || return 1
      (cd "$cache" && shasum -a 256 ./*.whl > wheel.sha256) || return 1
    fi
    wheel=$("$python_bin" -c 'from pathlib import Path; import sys; p=list(Path(sys.argv[1]).glob("*.whl")); assert len(p)==1; print(p[0].resolve())' "$cache") || return 1
    "$python_bin" -c 'from pathlib import Path; import sys; Path(sys.argv[1]).write_text("pyarrow @ "+Path(sys.argv[2]).as_uri()+"\n")' "$constraint" "$wheel" || return 1
    # Installed version alone cannot distinguish a bundled wheel or a binding
    # built against a previous Homebrew bottle with the same version number.
    if [ ! -f "$runtime_dir/venv/.addp-arrow-native-fingerprint" ] ||
       [ "$(cat "$runtime_dir/venv/.addp-arrow-native-fingerprint")" != "$key" ] ||
       ! "$python_bin" "$helper" admit --manifest "$manifest" >/dev/null 2>&1; then
      "$python_bin" -m pip install --force-reinstall --no-deps "$wheel" || return 1
    fi
  else
    : > "$constraint"
  fi
  # One lock/transaction: native binding, full declared dependencies, GDAL, and
  # admission. Constraints keep the resolver from replacing the native wheel.
  PIP_CONSTRAINT="$constraint" addp_install_python_dependencies "$ROOT_DIR" "$runtime_dir" "$label" "$common_dir" || return 1
  addp_install_gdal_binding "$python_bin" "$label" 1 || return 1
  "$python_bin" "$helper" admit --manifest "$manifest" --order gdal-first || return 1
  "$python_bin" "$helper" admit --manifest "$manifest" --order arrow-first || return 1
  # Reject a native upgrade during the transaction as well as between starts.
  temporary=$(mktemp "${TMPDIR:-/tmp}/addp-arrow-final.XXXXXX") || return 1
  "$python_bin" "$helper" manifest > "$temporary" || { rm -f "$temporary"; return 1; }
  fingerprint=$("$python_bin" "$helper" fingerprint --manifest "$temporary") || return 1
  rm -f "$temporary"
  [ "$fingerprint" = "$key" ] || { echo '✗ 原生依赖在准备期间变化，请重新准备' >&2; return 1; }
  addp_python_dependency_fingerprint "$ROOT_DIR" "$runtime_dir" "$common_dir" > "$runtime_dir/venv/.addp-dependency-fingerprint" || return 1
  printf '%s\n' "$key" > "$runtime_dir/venv/.addp-arrow-native-fingerprint" || return 1
  cp "$manifest" "$cache/native.json" || return 1
  rm -f "$manifest"
  trap - EXIT
)
