import struct

import pytest

from glb_validation import validate_glb
from .glb_fixture import glb_bytes, triangle_doc
from operators import ConverterError, CommandResult, invoke_operator
from .test_operators import file_plan


@pytest.mark.parametrize("encoding", ["PNG", "JPEG"])
def test_real_embedded_image_is_decoded(tmp_path, encoding):
    path = tmp_path / "model.glb"
    path.write_bytes(glb_bytes(*triangle_doc(encoding)))
    validate_glb(path)


@pytest.mark.parametrize("failure", ["external_buffer", "external_image", "mime_mismatch", "tga",
                                     "corrupt_image", "out_of_bounds_view", "empty_mesh", "bad_accessor"])
def test_invalid_artifact_never_replaces_published_target(tmp_path, failure):
    doc, binary = triangle_doc("PNG")
    if failure == "external_buffer": doc["buffers"][0]["uri"] = "mesh.bin"
    if failure == "external_image": doc["images"][0]["uri"] = "missing.png"
    if failure == "mime_mismatch": doc["images"][0]["mimeType"] = "image/jpeg"
    if failure == "tga": doc["images"][0]["mimeType"] = "image/tga"
    if failure == "corrupt_image": binary = binary[:36] + b"x" * (len(binary) - 36)
    if failure == "out_of_bounds_view": doc["bufferViews"][1]["byteLength"] = len(binary) * 2
    if failure == "empty_mesh": doc["meshes"] = []
    if failure == "bad_accessor": doc["accessors"][0]["count"] = 100
    source = tmp_path / "source.stl"
    source.write_text("mesh")
    target = tmp_path / "published.glb"
    target.write_bytes(b"previous valid artifact")
    plan = file_plan(source, target, "stl", "glb")
    plan["target"]["write_mode"] = "replace"
    def runner(command, timeout_seconds):
        from pathlib import Path
        Path(command[-2]).write_bytes(glb_bytes(doc, binary))
        return CommandResult(0)
    with pytest.raises(ConverterError) as error:
        invoke_operator("stl_to_glb", {"access_plan": plan}, runner=runner)
    assert error.value.error_code == "INVALID_GLB"
    assert target.read_bytes() == b"previous valid artifact"


@pytest.mark.parametrize("failure", ["length", "chunk", "json", "duplicate", "trailing"])
def test_broken_container_is_rejected(tmp_path, failure):
    data = bytearray(glb_bytes())
    if failure == "length": struct.pack_into("<I", data, 8, len(data) + 4)
    if failure == "chunk": struct.pack_into("<I", data, 12, len(data) * 2)
    if failure == "json": data[20] = ord("!")
    if failure == "duplicate":
        length = struct.unpack_from("<I", data, 12)[0]
        data[20 + length + 4:20 + length + 8] = b"JSON"
    if failure == "trailing":
        data += b"xxxx"
        struct.pack_into("<I", data, 8, len(data))
    path = tmp_path / "broken.glb"
    path.write_bytes(data)
    with pytest.raises(ValueError): validate_glb(path)


def test_legacy_diffuse_mirror_is_supported_but_other_channels_are_rejected(tmp_path):
    doc, binary = triangle_doc("PNG")
    doc["materials"] = [{"pbrMetallicRoughness": {"baseColorTexture": {"index": 0}},
                         "extensions": {"KHR_materials_pbrSpecularGlossiness": {"diffuseTexture": {"index": 0}}}}]
    path = tmp_path / "model.glb"
    path.write_bytes(glb_bytes(doc, binary))
    validate_glb(path, basic_static=True)
    doc["materials"][0]["extensions"]["KHR_materials_pbrSpecularGlossiness"]["specularGlossinessTexture"] = {"index": 0}
    path.write_bytes(glb_bytes(doc, binary))
    with pytest.raises(ValueError, match="extension textures"):
        validate_glb(path, basic_static=True)


@pytest.mark.parametrize("component_type,uv_bytes,stride", [
    (5126, struct.pack("<6f", 0, 0, 1, 0, 0, 1), 8),
    (5121, bytes([0, 0, 0, 0, 255, 0, 0, 0, 0, 255, 0, 0]), 4),
    (5123, struct.pack("<6H", 0, 0, 65535, 0, 0, 65535), 4),
])
@pytest.mark.parametrize("uv_selection", ["core", "transform"])
def test_supported_uv_encoding_and_selected_set_are_accepted(tmp_path, component_type, uv_bytes, stride, uv_selection):
    doc, binary = triangle_doc("PNG")
    view = doc["bufferViews"][2]
    binary = binary[:view["byteOffset"]] + uv_bytes
    view.update(byteLength=len(uv_bytes), byteStride=stride)
    doc["buffers"][0]["byteLength"] = len(binary)
    doc["accessors"][1].update(componentType=component_type, normalized=component_type != 5126)
    primitive = doc["meshes"][0]["primitives"][0]
    primitive["attributes"]["TEXCOORD_1"] = 1
    texture = doc["materials"][0]["pbrMetallicRoughness"]["baseColorTexture"]
    if uv_selection == "core":
        texture["texCoord"] = 1
    else:
        texture["extensions"] = {"KHR_texture_transform": {"texCoord": 1}}
    path = tmp_path / "model.glb"
    path.write_bytes(glb_bytes(doc, binary))
    validate_glb(path, basic_static=True)
