import base64
import json
import importlib
import os
import struct
import sys
from pathlib import Path

import pytest

from gltf_model import parse_composition, validate_source, prepare_converter_source
from operators import CommandResult, ConverterError, invoke_operator
from glb_validation import read_glb
from PIL import Image
import io
from .glb_fixture import glb_bytes, triangle_doc
from .test_operators import directory_plan

CORPUS = Path(__file__).resolve().parents[3] / 'common/format/plugins/gltf/testdata/composition-cases.json'


def test_runtime_image_packages_gltf_validator_with_operator():
    root = Path(__file__).resolve().parents[1]
    dockerfile = (root / 'docker/runtime/Dockerfile').read_text()
    copied = {Path(source).name for line in dockerfile.splitlines() if line.startswith('COPY ') and line.endswith(' /app/') for source in line.split()[1:-1]}
    assert {'gltf_model.py', 'operators.py', 'glb_validation.py'} <= copied


@pytest.mark.parametrize('case', json.loads(CORPUS.read_text()), ids=lambda case: case['name'])
def test_shared_format_profile(case):
    if not case['valid']:
        with pytest.raises(ValueError):
            parse_composition(case['raw'].encode())
        return
    result = parse_composition(case['raw'].encode())
    assert result.paths() == case['paths']
    assert result.resources == case['resources']


def source_fixture(root):
    doc, binary = triangle_doc('PNG')
    # The existing output fixture's BIN also contains the PNG image; the source
    # uses that same core bufferView form and declares one exact external buffer.
    doc['buffers'][0]['uri'] = 'buffers/%20geometry%2520.mesh'
    binary += b'\0' * (-len(binary) % 2)
    doc['bufferViews'].append({'buffer': 0, 'byteOffset': len(binary), 'byteLength': 6})
    doc['accessors'].append({'bufferView': len(doc['bufferViews']) - 1, 'componentType': 5123, 'count': 3, 'type': 'SCALAR'})
    doc['meshes'][0]['primitives'][0]['indices'] = len(doc['accessors']) - 1
    binary += struct.pack('<3H', 0, 1, 2)
    doc['buffers'][0]['byteLength'] = len(binary)
    source = root / 'model.gltf'
    source.parent.mkdir(parents=True, exist_ok=True)
    source.write_text(json.dumps(doc))
    buffer = root / 'buffers/ geometry%20.mesh'
    buffer.parent.mkdir()
    buffer.write_bytes(binary)
    return source, buffer, doc, binary


def test_real_bufferView_image_and_literal_percent_name(tmp_path):
    source, buffer, doc, binary = source_fixture(tmp_path / 'source')
    result = validate_source(source)
    assert result.paths() == ['buffers/ geometry%20.mesh']
    assert result.resources[0]['uri'] == 'buffers/%20geometry%2520.mesh'
    assert buffer.read_bytes() == binary
    assert json.loads(source.read_text()) == doc


@pytest.mark.parametrize('failure', ['missing', 'short', 'symlink', 'outside', 'wrong_image', 'wrong_indices', 'nonfinite'])
def test_source_failures_never_invoke_or_replace(tmp_path, failure):
    root = tmp_path / 'source'
    source, buffer, doc, binary = source_fixture(root)
    if failure == 'missing':
        buffer.unlink()
    elif failure == 'short':
        buffer.write_bytes(binary[:-1])
    elif failure == 'symlink':
        saved = tmp_path / 'outside.mesh'
        saved.write_bytes(binary)
        buffer.unlink()
        buffer.symlink_to(saved)
    elif failure == 'outside':
        doc['buffers'][0]['uri'] = '../outside.mesh'
        (tmp_path / 'outside.mesh').write_bytes(binary)
        source.write_text(json.dumps(doc))
    elif failure == 'wrong_image':
        picture = doc['images'][0]
        view = doc['bufferViews'][picture['bufferView']]
        payload = bytearray(binary)
        payload[view.get('byteOffset', 0)] = 0
        buffer.write_bytes(payload)
    elif failure == 'nonfinite':
        payload = bytearray(binary)
        struct.pack_into('<f', payload, 0, float('nan'))
        buffer.write_bytes(payload)
    else:
        accessor = doc['accessors'][doc['meshes'][0]['primitives'][0]['indices']]
        view = doc['bufferViews'][accessor['bufferView']]
        payload = bytearray(binary)
        payload[view.get('byteOffset', 0) + accessor.get('byteOffset', 0)] = 99
        buffer.write_bytes(payload)
    target = tmp_path / 'previous.glb'
    target.write_bytes(b'previous artifact')
    plan = directory_plan(root, target, 'gltf', 'glb', entrypoint=source.name)
    plan['target']['write_mode'] = 'replace'
    called = []
    with pytest.raises(ConverterError) as error:
        invoke_operator('gltf_to_glb', {'access_plan': plan}, runner=lambda *args: called.append(args))
    assert error.value.error_code == 'UNSUPPORTED_MODEL_SOURCE'
    assert called == []
    assert target.read_bytes() == b'previous artifact'


@pytest.mark.parametrize('inline_kind', ['buffer', 'percent'])
def test_embedded_resources_are_checked_without_external_file_items(tmp_path, inline_kind):
    source, buffer, doc, binary = source_fixture(tmp_path / 'source')
    if inline_kind in ('buffer', 'all'):
        doc['buffers'][0]['uri'] = 'data:application/gltf-buffer;base64,' + base64.b64encode(binary).decode()
    else:
        doc['buffers'][0]['uri'] = 'data:application/octet-stream,' + ''.join(f'%{value:02X}' for value in binary)
    source.write_text(json.dumps(doc))
    buffer.unlink()
    assert validate_source(source).paths() == []


def test_valid_source_uses_bound_converter_and_artifact_validator(tmp_path):
    source, _, _, _ = source_fixture(tmp_path / 'source')
    target = tmp_path / 'out.glb'
    commands = []
    def runner(command, timeout_seconds):
        commands.append(command)
        Path(command[-2]).write_bytes(glb_bytes(*triangle_doc('PNG')))
        return CommandResult(0)
    facts = invoke_operator('gltf_to_glb', {'access_plan': directory_plan(source.parent, target, 'gltf', 'glb', entrypoint=source.name)}, runner=runner,
                            env={'MODEL3D_MESH_CONVERTER_BIN': '/bound/native/assimp'})
    assert commands[0][0] == '/bound/native/assimp'
    assert commands[0][-1] == '-embtex'
    assert facts['source_format'] == 'gltf' and target.is_file()


@pytest.mark.skipif(not os.environ.get('MODEL3D_MESH_CONVERTER_BIN'), reason='real native converter is exercised separately when bound')
@pytest.mark.parametrize('resource_form', ['external', 'data-buffer', 'data-all', 'bufferView-images', 'percent-data', 'buffer-trailing-data'])
def test_bound_native_gltf_retains_two_triangles_uv_and_exact_red_blue_pixels(tmp_path, resource_form, record_property):
    repository = Path(__file__).resolve().parents[3]
    fixtures = repository / 'business/fixtures/manager'
    doc = json.loads((fixtures / 'addp_online_model_fixture.gltf').read_text())
    binary = (fixtures / 'addp_online_gltf_geometry.mesh').read_bytes()
    root = tmp_path / 'source'
    root.mkdir()
    members = {'buffers/ geometry%20.mesh': binary}
    for index, color in enumerate(('red', 'blue')):
        image = (fixtures / f'addp_online_obj_{color}.png').read_bytes()
        if resource_form in ('data-all', 'percent-data'):
            payload = base64.b64encode(image).decode() if resource_form == 'data-all' else ''.join(f'%{b:02X}' for b in image)
            doc['images'][index] = {'uri': 'data:image/png' + (';base64' if resource_form == 'data-all' else '') + ',' + payload}
        elif resource_form == 'bufferView-images':
            binary += b'\0' * (-len(binary) % 4)
            doc['bufferViews'].append({'buffer': 0, 'byteOffset': len(binary), 'byteLength': len(image)})
            binary += image
            doc['images'][index] = {'bufferView': len(doc['bufferViews']) - 1, 'mimeType': 'image/png'}
        else:
            members[f'{color}/color.png'] = image
    if resource_form in ('data-buffer', 'data-all', 'percent-data'):
        payload = ''.join(f'%{b:02X}' for b in binary) if resource_form == 'percent-data' else base64.b64encode(binary).decode()
        doc['buffers'][0]['uri'] = 'data:application/octet-stream' + ('' if resource_form == 'percent-data' else ';base64') + ',' + payload
        members.pop('buffers/ geometry%20.mesh')
    else:
        members['buffers/ geometry%20.mesh'] = binary
    doc['buffers'][0]['byteLength'] = len(binary)
    if resource_form == 'buffer-trailing-data':
        members['buffers/ geometry%20.mesh'] += b'valid unreferenced trailing bytes'
    members['model.gltf'] = json.dumps(doc).encode()
    for name, payload in members.items():
        member = root / name
        member.parent.mkdir(parents=True, exist_ok=True)
        member.write_bytes(payload)
    target = tmp_path / 'result.glb'
    facts = invoke_operator('gltf_to_glb', {'access_plan': directory_plan(root, target, 'gltf', 'glb', entrypoint='model.gltf')})
    sys.path.insert(0, str(repository))
    try:
        oracle = importlib.import_module('scripts.test.ftp-consumer-flow-online')
        proof = oracle.validate_obj_multi_material_glb(target.read_bytes())
        record_property('independent_oracle', json.dumps(proof, sort_keys=True))
        record_property('artifact_path', str(target))
    finally:
        sys.path.remove(str(repository))
    assert facts['source_format'] == 'gltf'
    assert all((root / name).read_bytes() == payload for name, payload in members.items())
    assert {str(path.relative_to(root)) for path in root.rglob('*') if path.is_file()} == set(members)
    record_property('source_bytes_and_names_unchanged', True)


def test_converter_adapter_uses_only_previously_verified_bytes(tmp_path):
    root = tmp_path / 'source'
    source, buffer, doc, binary = source_fixture(root)
    raw = source.read_bytes()
    verified = validate_source(source)
    renamed = tmp_path / 'moved-original'
    root.rename(renamed)
    scratch = tmp_path / 'scratch'
    scratch.mkdir()
    prepared = prepare_converter_source(verified, scratch)
    adapted = json.loads(prepared.read_text())
    assert list(prepared.parent.iterdir()) == [prepared]
    assert all(value['uri'].startswith('data:application/octet-stream;base64,') for value in adapted['buffers'])
    assert all(value['uri'].startswith('data:image/png;base64,') for value in adapted['images'])
    assert (renamed / source.name).read_bytes() == raw
    assert (renamed / 'buffers/ geometry%20.mesh').read_bytes() == binary


@pytest.mark.skipif(not os.environ.get('MODEL3D_MESH_CONVERTER_BIN'), reason='real native converter is exercised separately when bound')
def test_bound_native_jpeg_preserves_decoded_pixels(tmp_path, record_property):
    root = tmp_path / 'source'
    root.mkdir()
    doc, binary = triangle_doc('JPEG')
    view = doc['bufferViews'][doc['images'][0]['bufferView']]
    original = binary[view['byteOffset']:view['byteOffset'] + view['byteLength']]
    doc['buffers'][0]['uri'] = 'data:application/octet-stream;base64,' + base64.b64encode(binary).decode()
    source = root / 'model.gltf'
    raw = json.dumps(doc).encode()
    source.write_bytes(raw)
    target = tmp_path / 'result.glb'
    invoke_operator('gltf_to_glb', {'access_plan': directory_plan(root, target, 'gltf', 'glb', entrypoint=source.name)})
    actual, chunks = read_glb(target)
    assert len(actual['images']) == 1
    view = actual['bufferViews'][actual['images'][0]['bufferView']]
    image = chunks[1][1][view.get('byteOffset', 0):view.get('byteOffset', 0) + view['byteLength']]
    with Image.open(io.BytesIO(original)) as before, Image.open(io.BytesIO(image)) as after:
        assert after.size == before.size and after.convert('RGB').tobytes() == before.convert('RGB').tobytes()
    assert source.read_bytes() == raw
    record_property('jpeg_pixels_and_source_unchanged', True)
