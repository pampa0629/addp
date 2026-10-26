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
docker run -i --rm --network none --platform "${PLATFORM}" \
  -v "${ROOT_DIR}/business/fixtures/manager/addp_online_model_fixture.osgb:/fixture/model.osgb:ro" \
  -v "${ENGINE_DIR}/tests/fixtures/mesh:/fixture/mesh:ro" \
  -v "${ROOT_DIR}/business/fixtures/manager/addp_online_multi_material.obj:/fixture/obj/model.obj:ro" \
  -v "${ROOT_DIR}/business/fixtures/manager/addp_online_multi_material_red.mtl:/fixture/obj/paint red.mtl:ro" \
  -v "${ROOT_DIR}/business/fixtures/manager/addp_online_multi_material_blue.mtl:/fixture/obj/paint blue.mtl:ro" \
  -v "${ROOT_DIR}/business/fixtures/manager/addp_online_obj_red.png:/fixture/obj/red/color.png:ro" \
  -v "${ROOT_DIR}/business/fixtures/manager/addp_online_obj_blue.png:/fixture/obj/blue/color.png:ro" \
  --entrypoint python "${RUNTIME_IMAGE}" - <<'PY'
from pathlib import Path
import operators
import struct
import subprocess
import tempfile
import hashlib
import io
import base64
import json
from PIL import Image
from openskp import create
from skp_converter import convert
from glb_validation import validate_glb
import trimesh

dependencies = operators.converter_dependencies()
missing = [name for name, fact in dependencies.items() if fact["required"] and not fact["available"]]
if missing:
    raise SystemExit(f"model3d workflow converters are unavailable: {missing}")

with tempfile.TemporaryDirectory() as tmp:
    # Object-store staging yields longer paths than a direct mounted fixture.
    # Exercise allocation boundaries at the Rust/C converter interface.
    for padding in (0, 1, 7, 8, 15, 16, 31, 32, 63):
        osgb_source = Path(tmp) / ('staged-' + 'x' * padding) / 'source' / 'model.osgb'
        osgb_source.parent.mkdir(parents=True)
        osgb_source.write_bytes(Path('/fixture/model.osgb').read_bytes())
        osgb_target = Path(tmp) / f'osgb-{padding}.glb'
        osgb_plan = {'schema_version': 'addp.workflow.access-plan/v1',
                     'source': {'kind': 'file', 'format': 'osgb',
                                'access': {'method': 'mounted_path', 'path': str(osgb_source)}},
                     'target': {'kind': 'file', 'format': 'glb', 'name': osgb_target.name,
                                'write_mode': 'create',
                                'access': {'method': 'mounted_path', 'path': str(osgb_target)}}}
        try:
            operators.invoke_operator('osgb_to_glb', {'access_plan': osgb_plan}, timeout_seconds=60)
        except (operators.ConverterError, UnicodeDecodeError):
            diagnostic = subprocess.run([operators._converter_bin(None), '-f', 'gltf', '-i', str(osgb_source),
                                         '-o', str(Path(tmp) / 'diagnostic.glb')], capture_output=True)
            print('OSGB converter diagnostic:', diagnostic.returncode, diagnostic.stdout, diagnostic.stderr, flush=True)
            raise
        validate_glb(osgb_target)
    previous_osgb = osgb_target.read_bytes()
    invalid_osgb = Path(tmp) / 'invalid.osgb'
    invalid_osgb.write_bytes(b'not an OSGB file')
    osgb_plan['source']['access']['path'] = str(invalid_osgb)
    osgb_plan['target']['write_mode'] = 'replace'
    try:
        operators.invoke_operator('osgb_to_glb', {'access_plan': osgb_plan}, timeout_seconds=60)
    except operators.ConverterError as error:
        if error.error_code != 'EXECUTION_FAILED' or osgb_target.read_bytes() != previous_osgb:
            raise SystemExit('OSGB failed conversion did not report nonzero exit and preserve the previous artifact')
    else:
        raise SystemExit('invalid OSGB was silently accepted')
    print('Final Runtime compressed OSGB, staged paths and failure preservation smoke passed')

    mesh_source = Path(tmp) / 'mesh source'
    mesh_source.mkdir()
    fbx = mesh_source / 'static-two-triangles.fbx'
    fbx.write_bytes(Path('/fixture/mesh/static-two-triangles.fbx').read_bytes())
    if hashlib.sha256(fbx.read_bytes()).hexdigest() != '7d585849dadf512d973a0262bd1ef9bc6e1d105b15596e99b50cdb864d06ef30':
        raise SystemExit('FBX fixture differs from the self-authored Binary 7400 source')
    legacy_fbx = mesh_source / 'legacy-box.fbx'
    legacy_fbx.write_bytes(Path('/fixture/mesh/box.fbx').read_bytes())
    obj = mesh_source / 'triangle.obj'
    obj.write_text('mtllib triangle.mtl\no panel\nv 0 0 0\nv 2 0 0\nv 0 3 0\n'
                   'vt 0 0\nvt 1 0\nvt 0 1\nusemtl paint\nf 1/1 2/2 3/3\n')
    (mesh_source / 'triangle.mtl').write_text('newmtl paint\nKd 1 1 1\nd 1\nmap_Kd texture.png\n')
    Image.new('RGB', (4, 4), (32, 180, 220)).save(mesh_source / 'texture.png')
    def chunk(kind, data):
        return struct.pack('<HI', kind, len(data) + 6) + data
    material_name = b'preview\0'
    material = chunk(0xAFFF, chunk(0xA000, material_name)
                     + chunk(0xA200, chunk(0xA300, b'texture.png\0')))
    vertices = chunk(0x4110, struct.pack('<H9f', 3, 0, 0, 0, 1, 0, 0, 0, 2, 0))
    faces = chunk(0x4120, struct.pack('<5H', 1, 0, 1, 2, 0)
                  + chunk(0x4130, material_name + struct.pack('<2H', 1, 0)))
    uv = chunk(0x4140, struct.pack('<H6f', 3, 0, 0, 1, 0, 0, 1))
    mesh = chunk(0x4000, b'triangle\0' + chunk(0x4100, vertices + faces + uv))
    three_ds = mesh_source / 'model.3ds'
    three_ds.write_bytes(chunk(0x4D4D, chunk(0x3D3D, material + mesh)))
    if hashlib.sha256(three_ds.read_bytes()).hexdigest() != '856a75ee6a2de1be4e0cf19ed67dd49018d34486c762a115a45c173e30603bd6':
        raise SystemExit('3DS fixture differs from the Hosted source')
    source_hashes = {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in mesh_source.iterdir()}
    for format_name, source, triangle_count in (('fbx', fbx, 2), ('obj', obj, 1), ('3ds', three_ds, 1)):
        target = Path(tmp) / f'{format_name}.glb'
        plan = {'schema_version': 'addp.workflow.access-plan/v1',
                'source': {'kind': 'directory', 'format': format_name, 'entrypoint': source.name,
                           'access': {'method': 'mounted_path', 'path': str(mesh_source)}},
                'target': {'kind': 'file', 'format': 'glb', 'name': target.name, 'write_mode': 'create',
                           'access': {'method': 'mounted_path', 'path': str(target)}}}
        if format_name == 'fbx':
            plan['source'] = {'kind':'file','format':'fbx','access':{'method':'mounted_path','path':str(source)}}
        for attempt in range(32 if format_name == '3ds' else 1):
            operators.invoke_operator(f'{format_name}_to_glb', {'access_plan': plan}, timeout_seconds=60)
            doc = validate_glb(target, basic_static=True)
            plan['target']['write_mode'] = 'replace'
        primitives = [p for mesh in doc['meshes'] for p in mesh['primitives']]
        if (format_name != 'fbx' and len(doc['meshes']) != 1) or any(p.get('mode', 4) != 4 for p in primitives):
            raise SystemExit(f'{format_name} static mesh topology differs')
        if sum(doc['accessors'][p['indices']]['count'] for p in primitives) != triangle_count * 3:
            raise SystemExit(f'{format_name} triangle count differs')
        scene = trimesh.load(target, force='scene')
        if format_name == 'fbx':
            triangles = [sorted(tuple(round(float(v),6) for v in vertex) for vertex in triangle)
                         for mesh in scene.dump() for triangle in mesh.triangles]
            expected = [[(0.,0.,0.),(0.,3.,1.),(2.,0.,0.)],[(4.,0.,0.),(4.,3.,1.),(6.,0.,0.)]]
            if sorted(triangles) != expected or len(scene.graph.nodes_geometry) != 2 or doc.get('images'):
                raise SystemExit('FBX complete metre-space triangles, instances or untextured scope differ')
            for primitive in primitives:
                material = doc['materials'][primitive['material']]
                color = material['pbrMetallicRoughness']['baseColorFactor']
                if len(color)!=4 or any(abs(a-b)>1e-6 for a,b in zip(color,[.2,.4,.8,1])) or material.get('alphaMode','OPAQUE')!='OPAQUE':
                    raise SystemExit('FBX source material color or opacity differs')
            previous = target.read_bytes()
            plan['source']['access']['path'] = str(legacy_fbx)
            try:
                operators.invoke_operator('fbx_to_glb', {'access_plan':plan}, timeout_seconds=60)
            except operators.ConverterError as error:
                if error.error_code!='UNSUPPORTED_MODEL_SOURCE' or target.read_bytes()!=previous:
                    raise SystemExit('Legacy FBX layer indices were not rejected before conversion')
            else:
                raise SystemExit('Legacy FBX with missing layer indices was accepted')
        if format_name in {'obj', '3ds'}:
            if sum(doc['accessors'][p['attributes']['POSITION']]['count'] for p in primitives) != 3:
                raise SystemExit(f'{format_name} vertex count differs')
            expected_extents = (2, 3, 0) if format_name == 'obj' else (1, 0, 2)
            if any(abs(actual - expected) > 1e-6 for actual, expected in zip(scene.extents, expected_extents)):
                raise SystemExit(f'{format_name} source coordinates changed during conversion')
            pbr = doc['materials'][primitives[0]['material']]['pbrMetallicRoughness']
            image_index = doc['textures'][pbr['baseColorTexture']['index']]['source']
            if len(doc.get('images', [])) != 1 or pbr.get('baseColorFactor', [1, 1, 1, 1])[3] != 1:
                raise SystemExit(f'{format_name} diffuse texture or opacity was lost')
            _, chunks = operators._read_glb(target)
            view = doc['bufferViews'][doc['images'][image_index]['bufferView']]
            offset = view.get('byteOffset', 0)
            with Image.open(io.BytesIO(chunks[1][1][offset:offset + view['byteLength']])) as image:
                if image.size != (4, 4) or image.convert('RGB').tobytes() != bytes((32, 180, 220)) * 16:
                    raise SystemExit(f'{format_name} embedded texture pixels differ from the source')
        previous = target.read_bytes()
        invalid = mesh_source / f'invalid.{format_name}'
        invalid.write_text('not a model')
        if format_name == 'fbx': plan['source']['access']['path'] = str(invalid)
        else: plan['source']['entrypoint'] = invalid.name
        plan['target']['write_mode'] = 'replace'
        try:
            operators.invoke_operator(f'{format_name}_to_glb', {'access_plan': plan}, timeout_seconds=60)
        except operators.ConverterError:
            if target.read_bytes() != previous:
                raise SystemExit(f'{format_name} failure replaced the previous artifact')
        else:
            raise SystemExit(f'invalid {format_name} was silently accepted')
        invalid.unlink()
    if {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in mesh_source.iterdir()} != source_hashes:
        raise SystemExit('FBX/OBJ/3DS source files were modified')
    print('Final Runtime FBX/OBJ/3DS geometry, embedded PNG and failure preservation smoke passed')

    # Two sibling MTLs with spaces and two different textures sharing a basename
    # must survive the same official operator, without native path fallback.
    dual_source = Path(tmp) / 'obj-exact-composition'
    dual_source.mkdir()
    dual_obj = dual_source / 'model.obj'
    for name in ['model.obj', 'paint red.mtl', 'paint blue.mtl', 'red/color.png', 'blue/color.png']:
        destination = dual_source / name
        destination.parent.mkdir(parents=True, exist_ok=True)
        destination.write_bytes((Path('/fixture/obj') / name).read_bytes())
    dual_hashes = {str(p.relative_to(dual_source)): hashlib.sha256(p.read_bytes()).hexdigest()
                   for p in dual_source.rglob('*') if p.is_file()}
    dual_target = Path(tmp) / 'obj-exact-composition.glb'
    dual_plan = {'schema_version': 'addp.workflow.access-plan/v1',
                 'source': {'kind': 'directory', 'format': 'obj', 'entrypoint': dual_obj.name,
                            'access': {'method': 'mounted_path', 'path': str(dual_source)}},
                 'target': {'kind': 'file', 'format': 'glb', 'name': dual_target.name,
                            'write_mode': 'create',
                            'access': {'method': 'mounted_path', 'path': str(dual_target)}}}
    operators.invoke_operator('obj_to_glb', {'access_plan': dual_plan}, timeout_seconds=60)
    dual_doc = validate_glb(dual_target, basic_static=True)
    dual_scene = trimesh.load(dual_target, force='scene')
    if any(abs(a-b) > 1e-6 for a,b in zip(dual_scene.extents, (2, 1, 0))):
        raise SystemExit('OBJ multi-MTL world coordinates changed')
    dual_primitives = [p for m in dual_doc['meshes'] for p in m['primitives']]
    if len(dual_primitives) != 2 or len(dual_doc.get('images', [])) != 2:
        raise SystemExit('OBJ multi-MTL meshes or distinct embedded images were lost')
    _, dual_chunks = operators._read_glb(dual_target)
    observed_colors = set()
    for primitive in dual_primitives:
        position = dual_doc['accessors'][primitive['attributes']['POSITION']]
        if position['count'] != 3 or 'TEXCOORD_0' not in primitive['attributes']:
            raise SystemExit('OBJ multi-MTL vertex/UV facts differ')
        color = 'red' if abs(position['min'][0]) < 1e-6 else 'blue'
        if abs(position['min'][0] - (0 if color == 'red' else 1)) > 1e-6:
            raise SystemExit('OBJ multi-MTL primitive source position differs')
        material = dual_doc['materials'][primitive['material']]['pbrMetallicRoughness']
        image_index = dual_doc['textures'][material['baseColorTexture']['index']]['source']
        view = dual_doc['bufferViews'][dual_doc['images'][image_index]['bufferView']]
        offset = view.get('byteOffset', 0)
        embedded = dual_chunks[1][1][offset:offset + view['byteLength']]
        if embedded != (dual_source / color / 'color.png').read_bytes():
            raise SystemExit('OBJ multi-MTL source PNG bytes or material assignment differ')
        with Image.open(io.BytesIO(embedded)) as image:
            expected = (220, 30, 60) if color == 'red' else (30, 70, 220)
            if image.size != (2, 2) or image.convert('RGB').tobytes() != bytes(expected) * 4:
                raise SystemExit('OBJ multi-MTL embedded pixels differ')
        observed_colors.add(color)
    if observed_colors != {'red', 'blue'}:
        raise SystemExit('OBJ same-basename textures were confused')
    prior_dual = dual_target.read_bytes()
    dual_plan['target']['write_mode'] = 'replace'
    dual_plan['source']['entrypoint'] = 'rejected.obj'
    (dual_source / 'nested').mkdir()
    (dual_source / 'nested/paint.mtl').write_text('newmtl nested\n')
    for library, texture in [('nested/paint.mtl', None), ('rejected.mtl', 'missing.png'),
                             ('rejected.mtl', '../not-authorized.png')]:
        (dual_source / 'rejected.obj').write_text(f'mtllib {library}\nv 0 0 0\n')
        if texture:
            (dual_source / 'rejected.mtl').write_text(f'newmtl rejected\nmap_Kd {texture}\n')
        calls = []
        def forbidden_runner(command, timeout_seconds):
            calls.append(command)
            raise AssertionError('Rejected OBJ input reached the native converter')
        try:
            operators.invoke_operator('obj_to_glb', {'access_plan': dual_plan}, runner=forbidden_runner)
        except operators.ConverterError as error:
            if error.error_code != 'UNSUPPORTED_MODEL_SOURCE' or calls or dual_target.read_bytes() != prior_dual:
                raise SystemExit('OBJ rejected dependencies changed the published artifact')
        else:
            raise SystemExit('OBJ nested/missing/escaping dependency was silently accepted')
    if any(hashlib.sha256((dual_source / name).read_bytes()).hexdigest() != digest
           for name, digest in dual_hashes.items()):
        raise SystemExit('OBJ multi-MTL source bytes changed')
    print('Final Runtime OBJ exact composition: two sibling MTLs, nested same-basename PNGs, '
          'geometry/UV/material/pixel/source-byte and failure-preservation smoke passed')
    print('OBJ_EXACT_COMPOSITION_PROOF=' + json.dumps({
        'schema_version': 'addp.model3d-obj-product-probe/v1', 'result': 'passed',
        'source_sha256': dual_hashes, 'glb_size': len(prior_dual),
        'glb_sha256': hashlib.sha256(prior_dual).hexdigest(),
        'glb_base64': base64.b64encode(prior_dual).decode('ascii'),
        'operator_sha256': hashlib.sha256(Path('/app/operators.py').read_bytes()).hexdigest(),
        'validator_sha256': hashlib.sha256(Path('/app/obj_model.py').read_bytes()).hexdigest(),
        'converter_sha256': hashlib.sha256(Path(operators._mesh_converter_bin(None)).read_bytes()).hexdigest(),
    }, sort_keys=True))

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
