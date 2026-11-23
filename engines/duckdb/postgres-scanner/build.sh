#!/usr/bin/env bash
# Build the ADDP scanner from the exact sources used by DuckDB 1.4.5.
set -euo pipefail
if [[ $# != 2 ]]; then
  echo "usage: $0 BUILD_DIRECTORY OUTPUT_DIRECTORY" >&2
  exit 2
fi
script_dir=$(cd "$(dirname "$0")" && pwd)
mkdir -p "$1" "$2"
build_dir=$(cd "$1" && pwd)
output_dir=$(cd "$2" && pwd)
work=$(mktemp -d "$build_dir/scanner.XXXXXXXX")
trap 'rm -rf "$work"' EXIT

# The local build cache stores native compiler outputs separately from the
# runtime extension directory. Only the build process supplies the executable pin.
target=$(uname -s)-$(uname -m)
recipe=$(cat "$script_dir/build.sh" "$script_dir/only-named-table.patch" | shasum -a 256 | cut -d' ' -f1)
cached="$build_dir/$target-$recipe"
binary=postgres_scanner.duckdb_extension
publish() {
  local input=$1 digest=$2
  temporary=$(mktemp "$output_dir/.$binary.XXXXXXXX")
  trap 'rm -f "$temporary"; rm -rf "$work"' EXIT
  install -m 0644 "$input" "$temporary"
  if [[ "$(shasum -a 256 -b "$temporary" | cut -d' ' -f1)" != "$digest" ]]; then
    echo "native build cache does not match build digest" >&2
    exit 1
  fi
  mv -f "$temporary" "$output_dir/$binary"
  printf '%s\n' "$digest"
}
if [[ -f "$cached/sha256" ]]; then
  publish "$cached/$binary" "$(cat "$cached/sha256")"
  exit
fi

# Cache only archives. Revalidate bytes before every extraction so an older
# source tree or a partially completed build cannot supply the release input.
unpack() {
  local name=$1 url=$2 digest=$3 archive="$build_dir/$1.tar.gz"
  local input="$archive"
  if [[ ! -f "$archive" ]]; then
    curl --fail --location --silent --show-error "$url" -o "$work/download"
    input="$work/download"
  fi
  if [[ "$(shasum -a 256 -b "$input" | cut -d' ' -f1)" != "$digest" ]]; then
    echo "source checksum mismatch: $name" >&2
    exit 1
  fi
  if [[ "$input" != "$archive" ]]; then
    mv "$input" "$archive"
  fi
  mkdir "$work/$name"
  tar -xzf "$archive" -C "$work/$name" --strip-components=1
}

unpack duckdb https://codeload.github.com/duckdb/duckdb/tar.gz/refs/tags/v1.4.5 \
  29931ac91cf9077292773099a900c67be6d13b933978e176249b6e5b75b0b958
unpack scanner https://codeload.github.com/duckdb/duckdb-postgres/tar.gz/b9fce43bc5d36bc6db70844f28b7b146e756eb22 \
  6a454405b93b6c9e1472c751f72585be7276197614495a8a3989cb8e60ffdbbe
unpack postgres https://codeload.github.com/postgres/postgres/tar.gz/refs/tags/REL_15_13 \
  e6d02d9da913ae9a4c528a5cfcbb2f29a5666b65ef4c06c533ce7ee77c5dd5ac

(cd "$work/scanner" && patch -p1 --fuzz=0 < "$script_dir/only-named-table.patch") >&2
mv "$work/postgres" "$work/scanner/postgres"
(cd "$work/scanner/postgres" && ./configure --without-readline --without-zlib) >&2
printf 'duckdb_extension_load(postgres_scanner SOURCE_DIR "%s/scanner" DONT_LINK)\n' "$work" >"$work/extensions.cmake"
case "$(uname -s)" in
  Darwin)
    static=OFF
    if [[ -z "${OPENSSL_ROOT_DIR:-}" ]] && command -v brew >/dev/null; then
      OPENSSL_ROOT_DIR=$(brew --prefix openssl@3)
      export OPENSSL_ROOT_DIR
    fi
    ;;
  Linux) static=ON ;;
  *) echo "unsupported build platform" >&2; exit 1 ;;
esac
cmake -S "$work/duckdb" -B "$work/build" -G Ninja \
  -DDUCKDB_EXTENSION_CONFIGS="$work/extensions.cmake" -DOVERRIDE_GIT_DESCRIBE=v1.4.5 \
  -DCMAKE_BUILD_TYPE=Release -DCMAKE_POLICY_VERSION_MINIMUM=3.5 \
  -DBUILD_UNITTESTS=OFF -DBUILD_SHELL=OFF -DEXTENSION_STATIC_BUILD="$static" >&2
cmake --build "$work/build" --target postgres_scanner_loadable_extension --parallel "${CMAKE_BUILD_PARALLEL_LEVEL:-4}" >&2
mkdir "$work/product"
install -m 0644 "$work/build/extension/postgres_scanner/$binary" "$work/product/$binary"
digest=$(shasum -a 256 -b "$work/product/$binary" | cut -d' ' -f1)
printf '%s\n' "$digest" > "$work/product/sha256"
# Concurrent builders may complete the same recipe. Use the first complete
# cache entry, including its own digest, for both publication and linking.
python3 - "$work/product" "$cached" <<'PYINSTALL'
import errno, os, sys
try:
    os.rename(sys.argv[1], sys.argv[2])
except OSError as error:
    if error.errno not in (errno.ENOTEMPTY, errno.EEXIST):
        raise
PYINSTALL
publish "$cached/$binary" "$(cat "$cached/sha256")"
