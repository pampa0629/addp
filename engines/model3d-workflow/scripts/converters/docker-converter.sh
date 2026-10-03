#!/usr/bin/env bash
set -euo pipefail
entrypoint=${1:?converter entrypoint is required}
shift
IMAGE="${MODEL3D_CONVERTER_IMAGE:-localhost:5001/addp-model3d-converter:latest}"
mount_args=()
for path in /Users /Volumes /private /tmp /var/folders /home; do
  if [ -e "$path" ]; then
    mount_args+=("-v" "$path:$path")
  fi
done
exec docker run --rm ${MODEL3D_CONVERTER_PLATFORM:+--platform="$MODEL3D_CONVERTER_PLATFORM"} \
  --entrypoint "$entrypoint" "${mount_args[@]}" "$IMAGE" "$@"
