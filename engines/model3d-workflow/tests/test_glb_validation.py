import struct

import pytest

from glb_validation import validate_glb
from .glb_fixture import glb_bytes, triangle_doc
from operators import ConverterError, CommandResult, invoke_operator
from .test_operators import file_plan


def scene_fixture(failure):
    doc, binary = triangle_doc()
    if failure == "missing_scene": doc.pop("scenes")
    if failure == "empty_scene": doc["scenes"][0]["nodes"] = []
    if failure == "unreferenced_mesh": doc["nodes"][0].pop("mesh")
    if failure == "bad_scene": doc["scene"] = 1
    if failure == "bad_root": doc["scenes"][0]["nodes"] = [9]
    if failure == "bool_root": doc["scenes"][0]["nodes"] = [False]
    if failure == "duplicate_roots": doc["scenes"][0]["nodes"] = [0, 0]
    if failure == "bad_mesh": doc["nodes"][0]["mesh"] = 9
    if failure == "bad_child": doc["nodes"][0]["children"] = [9]
    if failure == "bad_children_type": doc["nodes"][0]["children"] = "0"
    if failure == "duplicate_children":
        doc["nodes"] += [{}]
        doc["nodes"][0]["children"] = [1, 1]
    if failure == "cycle":
        doc["nodes"] += [{"children": [0]}]
        doc["nodes"][0]["children"] = [1]
    if failure == "unreachable_cycle":
        doc["nodes"] += [{"children": [2]}, {"children": [1]}]
    if failure == "multiple_parents":
        doc["nodes"] += [{"children": [0]}, {"children": [0]}]
        doc["scenes"][0]["nodes"] = [1, 2]
    if failure == "root_is_child":
        doc["nodes"] += [{"children": [0]}]
    if failure == "nondefault_bad_root": doc["scenes"] += [{"nodes": [9]}]
    if failure == "nondefault_bad_transform":
        doc["nodes"] += [{"translation": [float("nan"), 0, 0]}]
        doc["scenes"] += [{"nodes": [1]}]
    return doc, binary


@pytest.mark.parametrize("failure", [
    "missing_scene", "empty_scene", "unreferenced_mesh", "bad_scene", "bad_root", "bool_root",
    "duplicate_roots", "bad_mesh", "bad_child", "bad_children_type", "duplicate_children",
    "cycle", "unreachable_cycle", "multiple_parents", "root_is_child", "nondefault_bad_root",
    "nondefault_bad_transform",
])
def test_invalid_scene_is_rejected(tmp_path, failure):
    path = tmp_path / "invalid-scene.glb"
    path.write_bytes(glb_bytes(*scene_fixture(failure)))
    with pytest.raises(ValueError):
        validate_glb(path)


@pytest.mark.parametrize("source_format", ["dae", "3ds", "stl"])
def test_blank_scene_never_replaces_published_model(tmp_path, source_format):
    from .test_exchange_model import dae, three_ds
    from .test_operators import directory_plan
    folder = tmp_path / "source"
    folder.mkdir()
    source = folder / f"model.{source_format}"
    source.write_bytes({"dae": dae(), "3ds": three_ds(), "stl": b"mesh"}[source_format])
    (folder / "texture.png").write_bytes(b"source texture")
    target = tmp_path / "published.glb"
    target.write_bytes(b"previous valid artifact")
    plan = (file_plan(source, target, "stl", "glb") if source_format == "stl"
            else directory_plan(folder, target, source_format, "glb", entrypoint=source.name))
    plan["target"]["write_mode"] = "replace"
    doc, binary = triangle_doc("PNG")
    doc["scenes"][0]["nodes"] = []

    def runner(command, timeout_seconds):
        from pathlib import Path
        Path(command[-2]).write_bytes(glb_bytes(doc, binary))
        return CommandResult(0)

    with pytest.raises(ConverterError) as error:
        invoke_operator(f"{source_format}_to_glb", {"access_plan": plan}, runner=runner)
    assert error.value.error_code == "INVALID_GLB"
    assert target.read_bytes() == b"previous valid artifact"


@pytest.mark.parametrize("transform", [
    {"translation": [0, 1]}, {"translation": [True, 0, 0]}, {"scale": [1, float("inf"), 1]},
    {"rotation": [0, 0, 0, 0]}, {"rotation": [0, 0, 0, 2]},
    {"matrix": [1] * 15}, {"matrix": [float("nan")] * 16},
    {"matrix": [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1], "scale": [1, 1, 1]},
])
def test_invalid_node_transform_is_rejected(tmp_path, transform):
    doc, binary = triangle_doc()
    doc["nodes"][0].update(transform)
    path = tmp_path / "invalid-transform.glb"
    path.write_bytes(glb_bytes(doc, binary))
    with pytest.raises(ValueError):
        validate_glb(path)


@pytest.mark.parametrize("default_scene", [None, 0, 1])
def test_scene_selection_and_unit_axis_transforms_are_preserved(tmp_path, default_scene):
    doc, binary = triangle_doc()
    # Collada centimeter/Z_UP conversion, followed by translation and mirrored scale.
    matrix = [0.01, 0, 0, 0, 0, 0, -0.01, 0, 0, 0.01, 0, 0, 0, 0, 0, 1]
    doc["nodes"] = [{"matrix": matrix, "children": [1]},
                    {"mesh": 0, "translation": [10, 20, 30], "scale": [-1, 2, 3],
                     "rotation": [0, 0, 0.70710678, 0.70710678]}]
    doc["scenes"] = [{"nodes": [0]}, {"nodes": [0]}]
    if default_scene is None:
        doc.pop("scene")
    else:
        doc["scene"] = default_scene
        if default_scene == 1: doc["scenes"][0]["nodes"] = []
    path = tmp_path / "units-and-axis.glb"
    path.write_bytes(glb_bytes(doc, binary))
    assert validate_glb(path) == doc


def test_deep_scene_is_checked_without_python_recursion_limit(tmp_path):
    doc, binary = triangle_doc()
    doc["nodes"] = [{"children": [index + 1]} for index in range(2000)] + [{"mesh": 0}]
    path = tmp_path / "deep.glb"
    path.write_bytes(glb_bytes(doc, binary))
    validate_glb(path)


def vertex_storage_fixture(attribute, failure):
    doc, binary = triangle_doc("PNG")
    accessor = doc["accessors"][0 if attribute == "POSITION" else 1]
    view = doc["bufferViews"][accessor["bufferView"]]
    element_size = 12 if attribute == "POSITION" else 8
    # Keep the bytes in bounds so these cases specifically exercise alignment/stride.
    view["byteLength"] = 1024
    binary += b"\0" * 1024
    doc["buffers"][0]["byteLength"] = len(binary)
    if failure == "accessor_alignment": accessor["byteOffset"] = 1
    if failure == "view_alignment": view["byteOffset"] = view.get("byteOffset", 0) + 1
    if failure == "stride_alignment": view["byteStride"] = element_size + 1
    if failure == "stride_limit": view["byteStride"] = 256
    if failure == "stride_short": view["byteStride"] = element_size - 4
    if failure == "stride_exceeds_view":
        accessor["count"] = 1
        view.update(byteLength=element_size, byteStride=element_size + 4)
        doc["accessors"][0]["count"] = doc["accessors"][1]["count"] = 1
        doc["meshes"][0]["primitives"][0]["mode"] = 0
    if failure == "integer_without_stride":
        accessor.update(componentType=5123, normalized=True)
    return doc, binary


@pytest.mark.parametrize("attribute", ["POSITION", "TEXCOORD_0"])
@pytest.mark.parametrize("failure", ["accessor_alignment", "view_alignment", "stride_alignment", "stride_limit",
                                     "stride_short", "stride_exceeds_view"])
def test_invalid_vertex_storage_is_rejected(tmp_path, attribute, failure):
    doc, binary = vertex_storage_fixture(attribute, failure)
    path = tmp_path / "bad-vertices.glb"
    path.write_bytes(glb_bytes(doc, binary))
    with pytest.raises(ValueError):
        validate_glb(path, basic_static=True)


def test_integer_uv_requires_explicit_stride(tmp_path):
    doc, binary = vertex_storage_fixture("TEXCOORD_0", "integer_without_stride")
    path = tmp_path / "bad-uv.glb"
    path.write_bytes(glb_bytes(doc, binary))
    with pytest.raises(ValueError):
        validate_glb(path, basic_static=True)


@pytest.mark.parametrize("component_type", [5121, 5123, 5126])
@pytest.mark.parametrize("stride", [24, 252])
def test_interleaved_position_and_uv_storage_is_accepted(tmp_path, component_type, stride):
    doc, binary = triangle_doc("PNG")
    binary += b"\0" * (-len(binary) % 4)
    start = len(binary)
    interleaved = b"\0" * 4
    encoding, maximum = {5121: ("B", 255), 5123: ("H", 65535), 5126: ("f", 1)}[component_type]
    for position, uv in zip(((0, 0, 0), (1, 0, 0), (0, 1, 0)), ((0, 0), (maximum, 0), (0, maximum))):
        record = struct.pack("<3f", *position) + struct.pack("<2" + encoding, *uv)
        interleaved += record + b"\0" * (stride - len(record))
    view_index = len(doc["bufferViews"])
    doc["bufferViews"].append({"buffer": 0, "byteOffset": start, "byteLength": len(interleaved),
                               "byteStride": stride, "target": 34962})
    binary += interleaved
    doc["buffers"][0]["byteLength"] = len(binary)
    doc["accessors"][0].update(bufferView=view_index, byteOffset=4)
    doc["accessors"][1].update(bufferView=view_index, byteOffset=16, componentType=component_type,
                               normalized=component_type != 5126)
    path = tmp_path / "interleaved.glb"
    path.write_bytes(glb_bytes(doc, binary))
    validate_glb(path, basic_static=True)


@pytest.mark.parametrize("source_format", ["stl", "dae", "3ds"])
def test_misaligned_vertices_never_replace_published_target(tmp_path, source_format):
    from .test_exchange_model import dae, three_ds
    from .test_operators import directory_plan
    doc, binary = vertex_storage_fixture("POSITION", "accessor_alignment")
    folder = tmp_path / "source"
    folder.mkdir()
    source = folder / f"model.{source_format}"
    source.write_bytes({"stl": b"mesh", "dae": dae(), "3ds": three_ds()}[source_format])
    (folder / "texture.png").write_bytes(b"source texture")
    target = tmp_path / "published.glb"
    target.write_bytes(b"previous valid artifact")
    plan = (file_plan(source, target, "stl", "glb") if source_format == "stl"
            else directory_plan(folder, target, source_format, "glb", entrypoint=source.name))
    plan["target"]["write_mode"] = "replace"

    def runner(command, timeout_seconds):
        from pathlib import Path
        Path(command[-2]).write_bytes(glb_bytes(doc, binary))
        return CommandResult(0)

    with pytest.raises(ConverterError) as error:
        invoke_operator(f"{source_format}_to_glb", {"access_plan": plan}, runner=runner)
    assert error.value.error_code == "INVALID_GLB"
    assert target.read_bytes() == b"previous valid artifact"


def indexed_triangle(component_type=5123, values=(0, 1, 2), texture_format=None):
    doc, binary = triangle_doc(texture_format)
    binary += b"\x00" * (-len(binary) % 4)
    payload = struct.pack("<" + {5121: "B", 5123: "H", 5125: "I"}[component_type] * len(values), *values)
    view_index = len(doc["bufferViews"])
    doc["bufferViews"].append({"buffer": 0, "byteOffset": len(binary), "byteLength": len(payload)})
    binary += payload
    doc["buffers"][0]["byteLength"] = len(binary)
    accessor_index = len(doc["accessors"])
    doc["accessors"].append({"bufferView": view_index, "componentType": component_type,
                             "count": len(values), "type": "SCALAR"})
    doc["meshes"][0]["primitives"][0]["indices"] = accessor_index
    return doc, binary


@pytest.mark.parametrize("source_format", ["stl", "dae", "3ds"])
def test_out_of_bounds_index_never_replaces_published_target(tmp_path, source_format):
    from .test_exchange_model import dae, three_ds
    from .test_operators import directory_plan
    doc, binary = indexed_triangle(values=(0, 1, 3), texture_format="PNG")
    folder = tmp_path / "source"
    folder.mkdir()
    source = folder / f"model.{source_format}"
    source.write_bytes({"stl": b"mesh", "dae": dae(), "3ds": three_ds()}[source_format])
    (folder / "texture.png").write_bytes(b"source texture")
    target = tmp_path / "published.glb"
    target.write_bytes(b"previous valid artifact")
    plan = (file_plan(source, target, "stl", "glb") if source_format == "stl"
            else directory_plan(folder, target, source_format, "glb", entrypoint=source.name))
    plan["target"]["write_mode"] = "replace"

    def runner(command, timeout_seconds):
        from pathlib import Path
        Path(command[-2]).write_bytes(glb_bytes(doc, binary))
        return CommandResult(0)

    with pytest.raises(ConverterError) as error:
        invoke_operator(f"{source_format}_to_glb", {"access_plan": plan}, runner=runner)
    assert error.value.error_code == "INVALID_GLB"
    assert target.read_bytes() == b"previous valid artifact"


@pytest.mark.parametrize("component_type", [5121, 5123, 5125])
def test_unsigned_index_encodings_with_accessor_offset_are_accepted(tmp_path, component_type):
    doc, binary = indexed_triangle(component_type, (99, 0, 1, 2))
    doc["accessors"][1].update(byteOffset={5121: 1, 5123: 2, 5125: 4}[component_type], count=3)
    doc["bufferViews"][1]["target"] = 34963
    path = tmp_path / "indexed.glb"
    path.write_bytes(glb_bytes(doc, binary))
    validate_glb(path)


@pytest.mark.parametrize("failure", ["reference", "boolean_reference", "type", "signed", "float", "normalized",
                                     "zero_count", "short_view", "offset", "view_alignment", "stride", "target"])
def test_invalid_index_storage_is_rejected(tmp_path, failure):
    doc, binary = indexed_triangle()
    accessor, view = doc["accessors"][1], doc["bufferViews"][1]
    if failure == "reference": doc["meshes"][0]["primitives"][0]["indices"] = 9
    if failure == "boolean_reference": doc["meshes"][0]["primitives"][0]["indices"] = True
    if failure == "type": accessor["type"] = "VEC2"
    if failure == "signed": accessor["componentType"] = 5122
    if failure == "float": accessor["componentType"] = 5126
    if failure == "normalized": accessor["normalized"] = True
    if failure == "zero_count": accessor["count"] = 0
    if failure == "short_view": view["byteLength"] -= 1
    if failure == "offset": accessor["byteOffset"] = 1
    if failure == "view_alignment": view["byteOffset"] -= 1
    if failure == "stride": view["byteStride"] = 4
    if failure == "target": view["target"] = 34962
    path = tmp_path / "bad-indices.glb"
    path.write_bytes(glb_bytes(doc, binary))
    with pytest.raises(ValueError):
        validate_glb(path)


def test_restart_sentinel_is_rejected_even_when_below_vertex_count(tmp_path):
    doc, binary = indexed_triangle(5121, (0, 1, 255))
    positions = binary[:36] + bytes(253 * 12)
    doc["accessors"][0]["count"] = 256
    doc["bufferViews"][0]["byteLength"] = len(positions)
    doc["bufferViews"][1]["byteOffset"] = len(positions)
    binary = positions + binary[36:]
    doc["buffers"][0]["byteLength"] = len(binary)
    path = tmp_path / "restart.glb"
    path.write_bytes(glb_bytes(doc, binary))
    with pytest.raises(ValueError, match="restart"):
        validate_glb(path)


def sparse_triangle(*, dense=False, positions=(1, 2), values=(1, 2), position_type=5121):
    doc, binary = indexed_triangle(values=(0, 99, 99))
    accessor = doc["accessors"][1]
    if not dense:
        del accessor["bufferView"]
    sparse = {"count": len(positions)}
    for key, component_type, items in (("indices", position_type, positions), ("values", 5123, values)):
        binary += b"\x00" * (-len(binary) % 4)
        payload = struct.pack("<" + {5121: "B", 5123: "H", 5125: "I"}[component_type] * len(items), *items)
        sparse[key] = {"bufferView": len(doc["bufferViews"])}
        if key == "indices":
            sparse[key]["componentType"] = component_type
        doc["bufferViews"].append({"buffer": 0, "byteOffset": len(binary), "byteLength": len(payload)})
        binary += payload
    accessor["sparse"] = sparse
    doc["buffers"][0]["byteLength"] = len(binary)
    return doc, binary


@pytest.mark.parametrize("dense", [False, True])
@pytest.mark.parametrize("position_type", [5121, 5123, 5125])
def test_sparse_replacements_validate_effective_indices(tmp_path, dense, position_type):
    doc, binary = sparse_triangle(dense=dense, position_type=position_type)
    path = tmp_path / "sparse.glb"
    path.write_bytes(glb_bytes(doc, binary))
    validate_glb(path)


def test_index_accessor_without_storage_uses_implicit_zeros(tmp_path):
    doc, binary = indexed_triangle()
    del doc["accessors"][1]["bufferView"]
    path = tmp_path / "implicit.glb"
    path.write_bytes(glb_bytes(doc, binary))
    validate_glb(path)


@pytest.mark.parametrize("dense", [False, True])
@pytest.mark.parametrize("positions,values", [((2, 1), (2, 1)), ((1, 1), (1, 2)), ((1, 3), (1, 2)), ((1, 2), (1, 3))])
def test_invalid_sparse_replacements_are_rejected(tmp_path, dense, positions, values):
    doc, binary = sparse_triangle(dense=dense, positions=positions, values=values)
    path = tmp_path / "sparse.glb"
    path.write_bytes(glb_bytes(doc, binary))
    with pytest.raises(ValueError):
        validate_glb(path)


@pytest.mark.parametrize("failure", ["count", "zero_count", "missing_values", "position_type", "short_values",
                                     "values_alignment", "positions_stride", "values_target", "offset_without_base"])
def test_invalid_sparse_storage_is_rejected(tmp_path, failure):
    doc, binary = sparse_triangle()
    accessor = doc["accessors"][1]
    sparse = accessor["sparse"]
    positions_view = doc["bufferViews"][sparse["indices"]["bufferView"]]
    values_view = doc["bufferViews"][sparse["values"]["bufferView"]]
    if failure == "count": sparse["count"] = 4
    if failure == "zero_count": sparse["count"] = 0
    if failure == "missing_values": del sparse["values"]
    if failure == "position_type": sparse["indices"]["componentType"] = 5126
    if failure == "short_values": values_view["byteLength"] -= 1
    if failure == "values_alignment": values_view["byteOffset"] -= 1
    if failure == "positions_stride": positions_view["byteStride"] = 4
    if failure == "values_target": values_view["target"] = 34963
    if failure == "offset_without_base": accessor["byteOffset"] = 0
    path = tmp_path / "sparse.glb"
    path.write_bytes(glb_bytes(doc, binary))
    with pytest.raises(ValueError):
        validate_glb(path)


@pytest.mark.parametrize("mode,count", [(0, 1), (1, 2), (2, 2), (3, 2), (4, 3), (5, 3), (6, 3)])
def test_supported_draw_modes_are_accepted(tmp_path, mode, count):
    doc, binary = indexed_triangle(values=(0, 1, 2)[:count])
    doc["meshes"][0]["primitives"][0]["mode"] = mode
    path = tmp_path / "mode.glb"
    path.write_bytes(glb_bytes(doc, binary))
    validate_glb(path)


@pytest.mark.parametrize("mode,count", [(1, 3), (2, 1), (3, 1), (4, 2), (5, 2), (6, 2), (7, 3), (True, 3)])
def test_invalid_draw_counts_and_modes_are_rejected(tmp_path, mode, count):
    doc, binary = indexed_triangle(values=(0, 1, 2)[:count])
    doc["meshes"][0]["primitives"][0]["mode"] = mode
    path = tmp_path / "mode.glb"
    path.write_bytes(glb_bytes(doc, binary))
    with pytest.raises(ValueError, match="draw"):
        validate_glb(path)


def test_nonindexed_draw_count_is_checked(tmp_path):
    doc, binary = triangle_doc()
    doc["accessors"][0]["count"] = 2
    path = tmp_path / "nonindexed.glb"
    path.write_bytes(glb_bytes(doc, binary))
    with pytest.raises(ValueError, match="draw count"):
        validate_glb(path)


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
