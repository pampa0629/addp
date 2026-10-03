import struct
from pathlib import Path

import pytest

from exchange_model import validate_source
from .glb_fixture import glb_bytes
from operators import CommandResult, ConverterError, invoke_operator
from .test_operators import directory_plan


def dae(ref="texture.png", extra=""):
    return f'''<COLLADA xmlns="http://www.collada.org/2005/11/COLLADASchema" version="1.4.1"><asset><unit meter="0.01"/><up_axis>Z_UP</up_axis></asset><library_images><image id="image"><init_from>{ref}</init_from></image></library_images>{extra}</COLLADA>'''.encode()


def chunk(kind, body):
    return struct.pack("<HI", kind, len(body) + 6) + body


def three_ds(ref="texture.png", map_kind=0xA200, extra=b""):
    return chunk(0x4D4D, chunk(0x3D3D, chunk(0xAFFF, chunk(map_kind, chunk(0xA300, ref.encode() + b"\x00")))) + extra)


@pytest.mark.parametrize("source_format", ["dae", "3ds"])
def test_static_exchange_operator_uses_mesh_converter_and_publishes(tmp_path, source_format):
    folder = tmp_path / "source"
    folder.mkdir()
    source = folder / f"model.{source_format}"
    source.write_bytes(dae() if source_format == "dae" else three_ds())
    (folder / "texture.png").write_bytes(b"source texture is read by converter")
    target = tmp_path / "model.glb"
    captured = []
    def runner(command, timeout_seconds):
        captured.append(command)
        from .glb_fixture import triangle_doc
        doc, binary = triangle_doc("PNG")
        doc["materials"] = [{"pbrMetallicRoughness": {"baseColorTexture": {"index": 0}}}]
        Path(command[-2]).write_bytes(glb_bytes(doc, binary))
        return CommandResult(0)
    facts = invoke_operator(f"{source_format}_to_glb", {"access_plan": directory_plan(folder, target, source_format, "glb", entrypoint=source.name)}, runner=runner)
    assert facts["source_format"] == source_format
    assert captured[0][-1] == "-embtex"
    assert target.is_file()


@pytest.mark.parametrize("source_format", ["dae", "3ds"])
@pytest.mark.parametrize("ref", ["missing.png", "Texture.PNG", "../texture.png", "/texture.png", "https://example.test/a.png", "texture.tga"])
def test_references_are_exact_local_and_supported(tmp_path, source_format, ref):
    source = tmp_path / f"model.{source_format}"
    source.write_bytes(dae(ref) if source_format == "dae" else three_ds(ref))
    (tmp_path / "texture.png").write_bytes(b"png")
    (tmp_path / "texture.tga").write_bytes(b"tga")
    with pytest.raises(ValueError): validate_source(source, source_format)


@pytest.mark.parametrize("data,source_format", [
    (dae(extra="<library_animations><animation/></library_animations>"), "dae"),
    (dae(extra="<library_controllers><controller/></library_controllers>"), "dae"),
    (dae(extra='<library_effects><effect><profile_COMMON><technique><phong><specular><texture texture="a"/></specular></phong></technique></profile_COMMON></effect></library_effects>'), "dae"),
    (three_ds(map_kind=0xA204), "3ds"),
    (three_ds(extra=chunk(0xB000, chunk(0xB002, chunk(0xB020, struct.pack("<HIII", 0, 0, 0, 2))))), "3ds"),
    (chunk(0x4D4D, b"broken"), "3ds"),
])
def test_out_of_scope_source_is_rejected_before_converter(tmp_path, data, source_format):
    source = tmp_path / f"model.{source_format}"
    source.write_bytes(data)
    target = tmp_path / "model.glb"
    called = []
    with pytest.raises(ConverterError) as error:
        invoke_operator(f"{source_format}_to_glb", {"access_plan": directory_plan(tmp_path, target, source_format, "glb", entrypoint=source.name)}, runner=lambda *args: called.append(args))
    assert error.value.error_code == "UNSUPPORTED_MODEL_SOURCE"
    assert not called
    assert not target.exists()


def test_converter_cannot_silently_drop_declared_textures(tmp_path):
    source = tmp_path / "source"
    source.mkdir()
    (source / "model.dae").write_bytes(dae())
    (source / "texture.png").write_bytes(b"source texture")
    target = tmp_path / "model.glb"
    def runner(command, timeout_seconds):
        Path(command[-2]).write_bytes(glb_bytes())
        return CommandResult(0)
    with pytest.raises(ConverterError, match="dropped declared"):
        invoke_operator("dae_to_glb", {"access_plan": directory_plan(source, target, "dae", "glb", entrypoint="model.dae")}, runner=runner)
    assert not target.exists()


@pytest.mark.parametrize("encoding", ["utf-8", "utf-16", "utf-16-be"])
@pytest.mark.parametrize("declaration", [
    '<!DOCTYPE COLLADA [<!ENTITY texture "texture.png">]>',
    '<!DOCTYPE COLLADA SYSTEM "https://example.test/model.dtd">',
])
def test_dae_dtd_is_rejected_for_each_xml_encoding(tmp_path, encoding, declaration):
    xml_encoding = 'UTF-8' if encoding == 'utf-8' else 'UTF-16'
    ref = '&texture;' if 'ENTITY' in declaration else 'texture.png'
    data = f'<?xml version="1.0" encoding="{xml_encoding}"?>{declaration}' + dae(ref).decode()
    (tmp_path / 'model.dae').write_bytes(data.encode(encoding))
    (tmp_path / 'texture.png').write_bytes(b'source texture')
    target = tmp_path / 'model.glb'
    called = []
    with pytest.raises(ConverterError) as error:
        invoke_operator('dae_to_glb', {'access_plan': directory_plan(tmp_path, target, 'dae', 'glb', entrypoint='model.dae')}, runner=lambda *args: called.append(args))
    assert error.value.error_code == 'UNSUPPORTED_MODEL_SOURCE'
    assert not called and not target.exists()


def test_dae_comment_containing_dtd_text_is_not_a_declaration(tmp_path):
    source = tmp_path / 'model.dae'
    source.write_bytes(dae(extra='<!-- Documentation: <!DOCTYPE and <!ENTITY are unsupported -->'))
    (tmp_path / 'texture.png').write_bytes(b'source texture')
    assert validate_source(source, 'dae') == ['texture.png']


@pytest.mark.parametrize("source_format", ["dae", "3ds"])
@pytest.mark.parametrize("failure", ["unused_material", "missing_uv", "bad_material", "bad_uv",
                                     "uv_count", "uv_type", "uv_bounds", "uv_set", "uv_integer", "uv_transform_set"])
def test_unusable_texture_never_replaces_published_model(tmp_path, source_format, failure):
    from .glb_fixture import triangle_doc
    source = tmp_path / "source"
    source.mkdir()
    (source / f"model.{source_format}").write_bytes(dae() if source_format == "dae" else three_ds())
    (source / "texture.png").write_bytes(b"source texture")
    target = tmp_path / "published.glb"
    target.write_bytes(b"previous valid artifact")
    plan = directory_plan(source, target, source_format, "glb", entrypoint=f"model.{source_format}")
    plan["target"]["write_mode"] = "replace"
    doc, binary = triangle_doc("PNG")
    primitive = doc["meshes"][0]["primitives"][0]
    if failure == "unused_material": primitive.pop("material")
    if failure == "missing_uv": primitive["attributes"].pop("TEXCOORD_0")
    if failure == "bad_material": primitive["material"] = 99
    if failure == "bad_uv": primitive["attributes"]["TEXCOORD_0"] = 99
    if failure == "uv_count": doc["accessors"][1]["count"] = 2
    if failure == "uv_type": doc["accessors"][1]["type"] = "VEC3"
    if failure == "uv_bounds": doc["accessors"][1]["byteOffset"] = 4
    if failure == "uv_set": doc["materials"][0]["pbrMetallicRoughness"]["baseColorTexture"]["texCoord"] = 1
    if failure == "uv_integer": doc["accessors"][1]["componentType"] = 5123
    if failure == "uv_transform_set":
        doc["materials"][0]["pbrMetallicRoughness"]["baseColorTexture"]["extensions"] = {"KHR_texture_transform": {"texCoord": 1}}
    def runner(command, timeout_seconds):
        Path(command[-2]).write_bytes(glb_bytes(doc, binary))
        return CommandResult(0)
    with pytest.raises(ConverterError) as error:
        invoke_operator(f"{source_format}_to_glb", {"access_plan": plan}, runner=runner)
    assert error.value.error_code == "INVALID_GLB"
    assert target.read_bytes() == b"previous valid artifact"
