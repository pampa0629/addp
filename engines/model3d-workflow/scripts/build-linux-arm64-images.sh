#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENGINE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

PLATFORM="${MODEL3D_DOCKER_PLATFORM:-linux/arm64}"
CONVERTER_IMAGE="${MODEL3D_CONVERTER_IMAGE:-addp/model3d-converter:linux-arm64}"
RUNTIME_IMAGE="${MODEL3D_RUNTIME_IMAGE:-addp/model3d-workflow:linux-arm64}"
THREE_DTILES_REF="${THREE_DTILES_REF:-acbcf603f33fdfe3c34b704a8b019c4fd32a8376}"

if [[ "${PLATFORM}" == *,* ]]; then
  echo "model3d converter build currently supports one Linux platform per run, got: ${PLATFORM}" >&2
  exit 1
fi

if [[ "${PLATFORM}" != linux/* ]]; then
  echo "model3d converter build requires a Linux Docker platform, got: ${PLATFORM}" >&2
  exit 1
fi

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is required" >&2
  exit 1
fi

echo "Building Model3D converter image"
echo "  platform: ${PLATFORM}"
echo "  image:    ${CONVERTER_IMAGE}"
echo "  ref:      ${THREE_DTILES_REF}"
docker build \
  --platform "${PLATFORM}" \
  --build-arg THREE_DTILES_REF="${THREE_DTILES_REF}" \
  -f "${ENGINE_DIR}/docker/converter/Dockerfile" \
  -t "${CONVERTER_IMAGE}" \
  "${ENGINE_DIR}/docker/converter"

echo "Smoke checking converter image"
docker run --rm --platform "${PLATFORM}" "${CONVERTER_IMAGE}" --help >/dev/null

echo "Building Model3D workflow runtime image"
echo "  platform: ${PLATFORM}"
echo "  image:    ${RUNTIME_IMAGE}"
docker build \
  --platform "${PLATFORM}" \
  --build-arg CONVERTER_IMAGE="${CONVERTER_IMAGE}" \
  -f "${ENGINE_DIR}/docker/runtime/Dockerfile" \
  -t "${RUNTIME_IMAGE}" \
  "${ENGINE_DIR}"

echo "Smoke checking runtime image"
docker run --rm --platform "${PLATFORM}" --entrypoint /opt/addp/model3d-workflow/bin/_3dtile "${RUNTIME_IMAGE}" --help >/dev/null
docker run -i --rm --platform "${PLATFORM}" --entrypoint python "${RUNTIME_IMAGE}" - <<'PY'
from pathlib import Path
import operators
import struct
import subprocess
import tempfile

status = operators.converter_status()
if not status.get("available"):
    raise SystemExit(f"model3d workflow converters are unavailable: {status.get('details')}")

with tempfile.TemporaryDirectory() as tmp:
    source = Path(tmp) / "tiny.splat"
    target = Path(tmp) / "tiny.ksplat"
    record = bytearray(32)
    for index, value in enumerate([0.0, 0.0, 0.0, 1.0, 1.0, 1.0]):
        struct.pack_into("<f", record, index * 4, value)
    record[24:32] = bytes([255, 64, 32, 255, 255, 0, 0, 0])
    source.write_bytes(record)
    completed = subprocess.run(
        ["/usr/bin/node", "/app/create_ksplat.mjs", str(source), str(target), "splat"],
        check=False,
        capture_output=True,
        text=True,
    )
    if completed.returncode != 0:
        raise SystemExit(completed.stderr or completed.stdout or "KPlat smoke conversion failed")
    if not target.is_file() or target.stat().st_size == 0:
        raise SystemExit("KPlat smoke conversion produced no output")
PY

echo "Built images:"
echo "  ${CONVERTER_IMAGE}"
echo "  ${RUNTIME_IMAGE}"
