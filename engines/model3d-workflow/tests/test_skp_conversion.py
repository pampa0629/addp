"""Actual parser/exporter coverage; no SDK or downloaded model is needed."""
import io
from collections import Counter
from pathlib import Path

import numpy as np
import pytest
import trimesh
from openskp import create
from PIL import Image

from glb_validation import read_glb, validate_glb
from operators import ConverterError, invoke_operator
from .test_operators import directory_plan, file_plan


def textured_skp(directory: Path) -> Path:
    image = directory / "texture.png"
    Image.new("RGB", (4, 4), (220, 30, 60)).save(image)
    builder = create()
    texture = builder.add_texture_material("Texture", str(image))
    with builder.add_component_definition("Panel") as panel:
        panel.add_face([(0, 0, 0), (1, 0, 0), (1, 2, 3)], material=texture)
    builder.add_instance(panel)
    builder.add_instance(panel, translation=(2, 0, 0))
    source = directory / "model.SKP"
    builder.save(str(source))
    image.unlink()  # Conversion must consume the embedded image only.
    return source


def test_real_skp_preserves_metre_scale_instances_and_embedded_pixels(tmp_path):
    source = textured_skp(tmp_path)
    original = source.read_bytes()
    target = tmp_path / "result" / "model.glb"
    result = invoke_operator("skp_to_glb", {"access_plan": file_plan(source, target, "skp", "glb")}, timeout_seconds=30)
    assert result["source_format"] == "skp"
    assert source.read_bytes() == original
    assert list(target.parent.iterdir()) == [target]
    doc = validate_glb(target, basic_static=True)
    scene = trimesh.load(target, force="scene")
    # 3 inches in X, 3 inches in Z mapped to Y, 2 inches in Y mapped to -Z.
    np.testing.assert_allclose(scene.bounds, [[0, 0, -0.0508], [0.0762, 0.0762, 0]], atol=1e-6)
    mesh_nodes = [node for node in doc["nodes"] if "mesh" in node]
    # Front/back materials may split the panel into multiple resources;
    # every resource must still be shared by the two placed components.
    assert Counter(node["mesh"] for node in mesh_nodes) == {index: 2 for index in range(len(doc["meshes"]))}
    assert len(doc["images"]) == 1
    _, chunks = read_glb(target)
    blob = next(data for kind, data in chunks if kind == b"BIN\x00")
    view = doc["bufferViews"][doc["images"][0]["bufferView"]]
    image = Image.open(io.BytesIO(blob[view.get("byteOffset", 0):view.get("byteOffset", 0) + view["byteLength"]]))
    assert image.size == (4, 4)
    assert image.convert("RGB").getpixel((0, 0)) == (220, 30, 60)


@pytest.mark.parametrize("visibility", ["instance", "face", "layer"])
def test_skp_visibility_not_applied_by_exporter_is_rejected(tmp_path, visibility):
    builder = create()
    layer = builder.add_layer("Hidden", hidden=True) if visibility == "layer" else None
    with builder.add_component_definition("Panel") as panel:
        panel.add_face([(0, 0, 0), (1, 0, 0), (1, 1, 0)], hidden=visibility == "face")
    builder.add_instance(panel, hidden=visibility == "instance", layer=layer)
    source = tmp_path / "hidden.skp"
    builder.save(str(source))
    target = tmp_path / "model.glb"
    target.write_bytes(b"previous ready artifact")
    plan = file_plan(source, target, "skp", "glb")
    plan["target"]["write_mode"] = "replace"
    with pytest.raises(ConverterError) as error:
        invoke_operator("skp_to_glb", {"access_plan": plan}, timeout_seconds=30)
    assert "Hidden components, faces or layers" in error.value.details
    assert target.read_bytes() == b"previous ready artifact"


@pytest.mark.parametrize("source_bytes", [b"not a SketchUp file", b"\xff\xfe\xff"])
def test_skp_parse_failure_does_not_replace_existing_artifact(tmp_path, source_bytes):
    source = tmp_path / "broken.skp"
    source.write_bytes(source_bytes)
    target = tmp_path / "model.glb"
    target.write_bytes(b"previous ready artifact")
    plan = file_plan(source, target, "skp", "glb")
    plan["target"]["write_mode"] = "replace"
    with pytest.raises(ConverterError, match="converter failed"):
        invoke_operator("skp_to_glb", {"access_plan": plan}, timeout_seconds=30)
    assert target.read_bytes() == b"previous ready artifact"


@pytest.mark.parametrize("source_kind", ["directory", "other_format"])
def test_skp_refuses_non_file_skp_plan(tmp_path, source_kind):
    source = tmp_path / "source"
    source.mkdir()
    target = tmp_path / "model.glb"
    plan = directory_plan(source, target, "skp", "glb") if source_kind == "directory" else file_plan(source, target, "obj", "glb")
    with pytest.raises(ConverterError) as error:
        invoke_operator("skp_to_glb", {"access_plan": plan})
    assert error.value.error_code == "UNSUPPORTED_MODEL_SOURCE"
    assert not target.exists()


def modern_material_skp(path, mutation=None):
    """ADDP-authored minimal VFF triangle; a regression input, not a profile proof."""
    import struct
    import zipfile
    from xml.etree import ElementTree as ET

    def record(tag, data):
        return bytes.fromhex(tag) + struct.pack('<I', len(data)) + data

    def ident(value):
        return record('DC05', record('DE05', bytes([value])))

    vertices = b''.join(record('C409', ident(i + 1) + record('C509', struct.pack('<3d', *point)))
                        for i, point in enumerate([(0, 0, 0), (2, 0, 0), (0, 2, 0)]))
    edges = b''.join(record('B80B', ident(10 + i) + record('B90B', bytes([i + 1]))
                           + record('BA0B', bytes([(i + 1) % 3 + 1]))) for i in range(3))
    loop = b''.join(record('A00F', record('A10F', bytes([10 + i])) + record('A20F', b'\x00')) for i in range(3))
    face = record('AC0D', record('D007', ident(20) + record('D107', b'\x64'))
                  + record('AF0D', b'\x65') + record('AD0D', struct.pack('<4d', 0, 0, 1, 0))
                  + record('AE0D', record('9411', record('9511', loop))))
    materials = b''.join(record('C832', ident(100 + i) + record('CC32', name.encode()))
                         for i, name in enumerate(('Front', 'Back')))
    content = {'model.dat': record('F401', record('F701', record('D430', record('D530', materials)))
                                 + record('F601', vertices + edges + face))}
    for name, color in [('Front', (220, 30, 60, 128)), ('Back', (20, 40, 230, 255))]:
        image = io.BytesIO()
        Image.new('RGBA', (4, 4), color).save(image, format='PNG', compress_level=0)
        content[f'materials/{name}/image.png'] = image.getvalue()
        content[f'materials/{name}/material.xml'] = (
            '<materialDocument xmlns="http://sketchup.google.com/schemas/sketchup/1.0/material">'
            f'<material name="{name}" type="1" hasTexture="1" workflow="0" colorRed="255" colorGreen="255" colorBlue="255">'
            '<texture textureFilename="/historical/unavailable/image.png" xScale="2" yScale="2">'
            '<images><image id="1" path="./image.png" file_name="image.png"/></images></texture>'
            '<pbrMR><enable_metalness>0</enable_metalness><enable_roughness>0</enable_roughness>'
            '<enable_normal>0</enable_normal><enable_occlusion>0</enable_occlusion></pbrMR>'
            '</material></materialDocument>').encode()
    if mutation:
        mutation(content, ET)
    with path.open('wb') as stream:
        stream.write(b'\xff\xfe\xff\x0e' + b'\x00' * 28)
        with zipfile.ZipFile(stream, 'w', zipfile.ZIP_DEFLATED) as archive:
            for name, data in content.items():
                archive.writestr(name, data)
    return path


def test_vff_same_size_prefix_images_preserve_pixels_alpha_uv_and_front_back(tmp_path):
    import zipfile
    source = modern_material_skp(tmp_path / 'pair.skp')
    original = source.read_bytes()
    with zipfile.ZipFile(source) as archive:
        red, blue = [archive.read(f'materials/{name}/image.png') for name in ('Front', 'Back')]
        assert len(red) == len(blue) and red[:16] == blue[:16] and red != blue
    target = tmp_path / 'pair.glb'
    invoke_operator('skp_to_glb', {'access_plan': file_plan(source, target, 'skp', 'glb')}, timeout_seconds=30)
    doc, chunks = read_glb(target)
    binary = next(data for kind, data in chunks if kind == b'BIN\x00')
    pixels = []
    for image in doc['images']:
        view = doc['bufferViews'][image['bufferView']]
        offset = view.get('byteOffset', 0)
        decoded = Image.open(io.BytesIO(binary[offset:offset + view['byteLength']])).convert('RGBA')
        pixels.append(list(decoded.get_flattened_data()))
    expected = [[color] * 16 for color in [(220, 30, 60, 128), (20, 40, 230, 255)]]
    assert len(pixels) == 2 and sorted(pixels) == sorted(expected)
    assert all(material.get('alphaMode') == 'MASK' for material in doc['materials'])
    scene = trimesh.load(target, force='scene')
    material_by_normal = {}
    for mesh in scene.geometry.values():
        assert mesh.visual.uv.shape == (len(mesh.vertices), 2)
        assert np.isfinite(mesh.visual.uv).all()
        np.testing.assert_allclose(sorted(map(tuple, mesh.visual.uv)),
                                   [(0., 0.), (0., 1.), (1., 0.)], atol=1e-6)
        # Front and back are separately exported, with opposite winding.
        normal = tuple(np.round(mesh.face_normals[0], 5))
        material_by_normal[normal] = mesh.visual.material.baseColorTexture.convert('RGBA').getpixel((0, 0))
    assert source.read_bytes() == original
    assert len(material_by_normal) == 2
    assert material_by_normal[(0., 1., 0.)] == (220, 30, 60, 128)
    assert material_by_normal[(0., -1., 0.)] == (20, 40, 230, 255)


@pytest.mark.parametrize('case', ['missing-decoy', 'normal', 'roughness', 'metalness', 'occlusion',
                                  'workflow', 'colorized', 'unknown-node', 'unknown-attribute',
                                  'malformed', 'texture-flag', 'image-traversal', 'mime', 'corrupt-png',
                                  'pbr-incomplete', 'pbr-factor'])
def test_vff_bad_material_fails_and_preserves_old_artifact(tmp_path, case):
    key = 'materials/Front/material.xml'
    def mutate(content, ET):
        if case == 'missing-decoy':
            content['materials/Front/decoy.png'] = content.pop('materials/Front/image.png')
            return
        if case == 'malformed':
            content[key] = b'<broken'
            return
        if case == 'corrupt-png':
            content['materials/Front/image.png'] = b'\x89PNG\r\n\x1a\ncorrupt'
            return
        if case == 'mime':
            content['materials/Front/image.png'] = b'unsupported image'
            return
        root = ET.fromstring(content[key])
        ns = '{http://sketchup.google.com/schemas/sketchup/1.0/material}'
        material = root[0]
        if case in ('normal', 'roughness', 'metalness', 'occlusion'):
            material.find(ns + 'pbrMR/' + ns + 'enable_' + case).text = '1'
        elif case == 'pbr-incomplete':
            pbr = material.find(ns + 'pbrMR')
            pbr.remove(pbr.find(ns + 'enable_normal'))
        elif case == 'pbr-factor':
            ET.SubElement(material.find(ns + 'pbrMR'), ns + 'baseColorFactor').text = '0'
        elif case == 'workflow':
            material.set('workflow', '1')
        elif case == 'colorized':
            material.set('type', '2')
        elif case == 'unknown-node':
            ET.SubElement(material, ns + 'unexplainedResource', {'path': 'image.png'})
        elif case == 'unknown-attribute':
            material.set('unexplainedResource', 'image.png')
        elif case == 'texture-flag':
            material.set('hasTexture', '0')
        elif case == 'image-traversal':
            material.find(ns + 'texture/' + ns + 'images/' + ns + 'image').set('path', './../Front/image.png')
        content[key] = ET.tostring(root)
    source = modern_material_skp(tmp_path / 'bad.skp', mutate)
    original = source.read_bytes()
    target = tmp_path / 'model.glb'
    target.write_bytes(b'previous ready artifact')
    plan = file_plan(source, target, 'skp', 'glb')
    plan['target']['write_mode'] = 'replace'
    with pytest.raises(ConverterError) as error:
        invoke_operator('skp_to_glb', {'access_plan': plan}, timeout_seconds=30)
    assert 'material' in error.value.details.lower()
    assert target.read_bytes() == b'previous ready artifact'
    assert source.read_bytes() == original


@pytest.mark.parametrize('reference', ['/image.png', '../image.png', '././image.png',
                                     './Front/../image.png', 'https://example/image.png',
                                     r'Front\image.png', './missing.png'])
def test_vff_image_paths_are_not_repaired_or_guessed(tmp_path, reference):
    def mutate(content, ET):
        key = 'materials/Front/material.xml'
        root = ET.fromstring(content[key])
        ns = '{http://sketchup.google.com/schemas/sketchup/1.0/material}'
        texture = root[0].find(ns + 'texture')
        texture.set('textureFilename', 'image.png')  # Existing alternative must not mask a failed declaration.
        texture.find(ns + 'images/' + ns + 'image').set('path', reference)
        content[key] = ET.tostring(root)
    source = modern_material_skp(tmp_path / 'bad-path.skp', mutate)
    target = tmp_path / 'model.glb'
    with pytest.raises(ConverterError):
        invoke_operator('skp_to_glb', {'access_plan': file_plan(source, target, 'skp', 'glb')}, timeout_seconds=30)
    assert not target.exists()


@pytest.mark.parametrize('member', ['materials/Front/material.xml', 'materials/Front/image.png'])
def test_vff_duplicate_material_or_declared_image_is_rejected(tmp_path, member):
    import warnings
    import zipfile
    source = modern_material_skp(tmp_path / 'duplicate.skp')
    with zipfile.ZipFile(source) as archive:
        data = archive.read(member)
    with warnings.catch_warnings():
        warnings.simplefilter('ignore', UserWarning)
        with zipfile.ZipFile(source, 'a') as archive:
            archive.writestr(member, data)
    target = tmp_path / 'model.glb'
    with pytest.raises(ConverterError):
        invoke_operator('skp_to_glb', {'access_plan': file_plan(source, target, 'skp', 'glb')}, timeout_seconds=30)
    assert not target.exists()


@pytest.mark.parametrize('declaration', ['archive-root', 'relative', 'filename-only', 'same-bytes'])
def test_vff_exact_image_forms_and_identical_content_reuse(tmp_path, declaration):
    def mutate(content, ET):
        ns = '{http://sketchup.google.com/schemas/sketchup/1.0/material}'
        if declaration == 'same-bytes':
            content['materials/Back/image.png'] = content['materials/Front/image.png']
        for name in ('Front', 'Back'):
            key = f'materials/{name}/material.xml'
            root = ET.fromstring(content[key])
            texture = root[0].find(ns + 'texture')
            image = texture.find(ns + 'images/' + ns + 'image')
            if declaration == 'archive-root':
                image.set('path', f'materials/{name}/image.png')
            elif declaration == 'relative':
                image.set('path', 'image.png')
            elif declaration == 'filename-only':
                texture.remove(texture.find(ns + 'images'))
                texture.set('textureFilename', 'image.png')
            content[key] = ET.tostring(root)
    source = modern_material_skp(tmp_path / 'exact.skp', mutate)
    target = tmp_path / 'model.glb'
    invoke_operator('skp_to_glb', {'access_plan': file_plan(source, target, 'skp', 'glb')}, timeout_seconds=30)
    doc = validate_glb(target, basic_static=True)
    assert len(doc['images']) == (1 if declaration == 'same-bytes' else 2)
