#!/usr/bin/env bash
set -euo pipefail
entrypoint=${1:?converter entrypoint is required}
shift
IMAGE="${MODEL3D_CONVERTER_IMAGE:-localhost:5001/addp-model3d-converter:latest}"
mount_args=()
mount_paths=()
for argument in "$@"; do
  [[ "$argument" == /* ]] || continue
  if [ -d "$argument" ]; then
    path="$argument"
  else
    path=$(dirname "$argument")
  fi
  duplicate=false
  for mounted in ${mount_paths[@]+"${mount_paths[@]}"}; do
    if [ "$mounted" = "$path" ]; then duplicate=true; break; fi
  done
  if [ "$duplicate" = false ]; then
    mount_paths+=("$path")
    mount_args+=("-v" "$path:$path")
  fi
done
exec docker run --rm ${MODEL3D_CONVERTER_PLATFORM:+--platform="$MODEL3D_CONVERTER_PLATFORM"} \
  --entrypoint "$entrypoint" ${mount_args[@]+"${mount_args[@]}"} "$IMAGE" "$@"
