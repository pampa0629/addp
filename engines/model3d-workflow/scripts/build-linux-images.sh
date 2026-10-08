#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENGINE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
ROOT_DIR="$(cd "${ENGINE_DIR}/../.." && pwd)"

case "$(uname -m)" in
  arm64|aarch64) native_arch=arm64 ;;
  x86_64) native_arch=amd64 ;;
  *) echo "unsupported Model3D host architecture" >&2; exit 1 ;;
esac
PLATFORM="${MODEL3D_DOCKER_PLATFORM:-linux/$native_arch}"
CONVERTER_IMAGE="${MODEL3D_CONVERTER_IMAGE:-addp/model3d-converter:linux-${PLATFORM#linux/}}"
RUNTIME_IMAGE="${MODEL3D_RUNTIME_IMAGE:-addp/model3d-workflow:linux-${PLATFORM#linux/}}"
THREE_DTILES_REF="${THREE_DTILES_REF:-acbcf603f33fdfe3c34b704a8b019c4fd32a8376}"

if [[ "${PLATFORM}" == *,* ]]; then
  echo "model3d converter build currently supports one Linux platform per run, got: ${PLATFORM}" >&2
  exit 1
fi

if [[ "${PLATFORM}" != linux/* ]]; then
  echo "model3d converter build requires a Linux Docker platform, got: ${PLATFORM}" >&2
  exit 1
fi

case "$PLATFORM" in
  linux/amd64|linux/arm64) ;;
  *) echo "model3d converter requires linux/amd64 or linux/arm64, got: $PLATFORM" >&2; exit 1 ;;
esac

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
  "${ROOT_DIR}"

echo "Smoke checking converter image"
docker run --rm --platform "${PLATFORM}" "${CONVERTER_IMAGE}" --help >/dev/null
docker run --rm --platform "${PLATFORM}" --entrypoint /opt/addp/model3d-workflow/bin/IfcConvert "${CONVERTER_IMAGE}" --version >/dev/null

echo "Building Model3D workflow runtime image"
echo "  platform: ${PLATFORM}"
echo "  image:    ${RUNTIME_IMAGE}"
docker build \
  --platform "${PLATFORM}" \
  --build-arg CONVERTER_IMAGE="${CONVERTER_IMAGE}" \
  -f "${ENGINE_DIR}/docker/runtime/Dockerfile" \
  -t "${RUNTIME_IMAGE}" \
  "${ROOT_DIR}"

echo "Smoke checking runtime image"
docker run --rm --platform "${PLATFORM}" --entrypoint /opt/addp/model3d-workflow/bin/_3dtile "${RUNTIME_IMAGE}" --help >/dev/null
docker run --rm --platform "${PLATFORM}" --entrypoint /opt/addp/model3d-workflow/bin/IfcConvert "${RUNTIME_IMAGE}" --version >/dev/null
docker run -i --rm --platform "${PLATFORM}" --entrypoint python "${RUNTIME_IMAGE}" - <<'PY'
from pathlib import Path
import operators
import struct
import subprocess
import tempfile
from openskp import create
from skp_converter import convert
from glb_validation import validate_glb
import trimesh

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
        raise SystemExit(completed.stderr or completed.stdout or "KSplat smoke conversion failed")
    if not target.is_file() or target.stat().st_size == 0:
        raise SystemExit("KSplat smoke conversion produced no output")

    skp = Path(tmp) / "panel.skp"
    glb = Path(tmp) / "panel.glb"
    builder = create()
    builder.add_face([(0, 0, 0), (1, 0, 0), (1, 1, 0)])
    builder.save(str(skp))
    convert(skp, glb)
    validate_glb(glb, basic_static=True)
    scene = trimesh.load(glb, force="scene")
    if abs(scene.extents[0] - 0.0254) > 1e-6 or abs(scene.extents[2] - 0.0254) > 1e-6:
        raise SystemExit("SKP smoke conversion has incorrect metre scale")
    print("SKP parser, GLB publication validation and metre scale smoke passed")

    import json
    import hashlib
    import io
    from PIL import Image
    from operators import ConverterError
    fixture = Path('/app/max-fixture')
    max_source = fixture / 'horsewalk02.max'
    source_hash = hashlib.sha256(max_source.read_bytes()).hexdigest()
    textures = json.loads((fixture / 'textures.json').read_text())
    extents = []
    for unit in ('', 'mm'):
        target = Path(tmp) / ('max-default.glb' if not unit else 'max-mm.glb')
        plan = {'schema_version': 'addp.workflow.access-plan/v1',
                'source': {'kind': 'directory', 'format': 'max', 'entrypoint': max_source.name,
                           'access': {'method': 'mounted_path', 'path': str(fixture)}},
                'target': {'kind': 'file', 'format': 'glb', 'name': target.name, 'write_mode': 'create',
                           'access': {'method': 'mounted_path', 'path': str(target)}}}
        options = {'texture_files': textures}
        if unit: options['source_unit'] = unit
        result = operators.invoke_operator('max_to_glb', {'access_plan': plan, 'options': options}, timeout_seconds=60)
        facts = result['conversion']
        if facts['unit_source'] != ('user' if unit else 'default') or facts['source_unit'] != (unit or 'm'):
            raise SystemExit('MAX conversion did not audit source unit provenance')
        doc = validate_glb(target, basic_static=True)
        if len(doc.get('images', [])) != 1:
            raise SystemExit('MAX base-color texture was lost')
        doc, chunks = operators._read_glb(target)
        view = doc['bufferViews'][doc['images'][0]['bufferView']]
        image = Image.open(io.BytesIO(chunks[1][1][view.get('byteOffset', 0):view.get('byteOffset', 0)+view['byteLength']]))
        if image.size != (4, 4) or image.convert('RGB').getpixel((0, 0)) != (220, 30, 60):
            raise SystemExit('MAX embedded texture differs from declared input')
        extents.append(trimesh.load(target, force='scene').extents)
    if any(abs(a / b - 1000) > .01 for a, b in zip(*extents)):
        raise SystemExit('MAX default metre and selected millimetre scale differ incorrectly')
    if hashlib.sha256(max_source.read_bytes()).hexdigest() != source_hash:
        raise SystemExit('MAX source was modified')
    plan['target']['write_mode'] = 'replace'
    previous = target.read_bytes()
    try:
        operators.invoke_operator('max_to_glb', {'access_plan': plan}, timeout_seconds=60)
    except ConverterError:
        if target.read_bytes() != previous:
            raise SystemExit('MAX missing texture replaced the previous artifact')
    else:
        raise SystemExit('MAX missing texture was silently accepted')
    print('MAX static GLB, declared texture, default/user units and old-artifact preservation smoke passed')
PY

echo "Built images:"
echo "  ${CONVERTER_IMAGE}"
echo "  ${RUNTIME_IMAGE}"
