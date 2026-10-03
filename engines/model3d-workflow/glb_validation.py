"""Validate the self-contained GLB artifact before it can be published."""
from __future__ import annotations

import io
import json
import struct
import warnings
from pathlib import Path
from typing import Any

from PIL import Image


def read_glb(path: Path) -> tuple[dict[str, Any], list[tuple[bytes, bytes]]]:
    data = path.read_bytes()
    if len(data) < 20:
        raise ValueError("GLB header is truncated")
    magic, version, length = struct.unpack_from("<4sII", data)
    if magic != b"glTF" or version != 2 or length != len(data) or length % 4:
        raise ValueError("invalid GLB 2.0 header")
    chunks = []
    offset = 12
    while offset < length:
        if length - offset < 8:
            raise ValueError("truncated GLB chunk header")
        size, kind = struct.unpack_from("<I4s", data, offset)
        offset += 8
        if size % 4 or size > length - offset:
            raise ValueError("invalid GLB chunk length")
        chunks.append((kind, data[offset:offset + size]))
        offset += size
    if not chunks or chunks[0][0] != b"JSON" or [k for k, _ in chunks].count(b"JSON") != 1:
        raise ValueError("GLB requires exactly one leading JSON chunk")
    if len(chunks) > 2 or (len(chunks) == 2 and chunks[1][0] != b"BIN\x00"):
        raise ValueError("GLB supports one optional BIN chunk")
    try:
        doc = json.loads(chunks[0][1].decode("utf-8"))
    except (UnicodeError, ValueError) as error:
        raise ValueError("invalid GLB JSON") from error
    if not isinstance(doc, dict) or not isinstance(doc.get("asset"), dict) or doc["asset"].get("version") != "2.0":
        raise ValueError("GLB asset.version must be 2.0")
    return doc, chunks


def _index(value: Any, entries: list, label: str) -> dict:
    if type(value) is not int or value < 0 or value >= len(entries) or not isinstance(entries[value], dict):
        raise ValueError(f"invalid {label} index: {value}")
    return entries[value]


def _integer(value: Any, label: str, minimum: int = 0) -> int:
    if type(value) is not int or value < minimum:
        raise ValueError(f"invalid {label}")
    return value


def _entries(doc: dict, key: str) -> list:
    value = doc.get(key, [])
    if not isinstance(value, list) or any(not isinstance(item, dict) for item in value):
        raise ValueError(f"invalid {key} array")
    return value


def _validate_texture_coordinates(primitive: dict, texture: dict, accessors: list, views: list, vertex_count: int) -> None:
    extensions = texture.get("extensions", {})
    if not isinstance(extensions, dict):
        raise ValueError("invalid texture extensions")
    transform = extensions.get("KHR_texture_transform", {})
    if not isinstance(transform, dict):
        raise ValueError("invalid texture transform")
    uv_set = _integer(transform.get("texCoord", texture.get("texCoord", 0)), "texture.texCoord")
    uv = _index(primitive["attributes"].get(f"TEXCOORD_{uv_set}"), accessors, "texture coordinate accessor")
    component_size = {5121: 1, 5123: 2, 5126: 4}.get(uv.get("componentType"))
    if uv.get("type") != "VEC2" or component_size is None:
        raise ValueError("texture coordinates must be float or normalized unsigned VEC2")
    if uv["componentType"] != 5126 and uv.get("normalized") is not True:
        raise ValueError("integer texture coordinates must be normalized")
    if _integer(uv.get("count"), "texture coordinate count", 1) != vertex_count:
        raise ValueError("texture coordinate count must match POSITION count")
    view = _index(uv.get("bufferView"), views, "texture coordinate bufferView")
    offset = _integer(uv.get("byteOffset", 0), "texture coordinate byteOffset")
    element_size = 2 * component_size
    stride = _integer(view.get("byteStride", element_size), "texture coordinate stride", element_size)
    if offset + (vertex_count - 1) * stride + element_size > view["byteLength"]:
        raise ValueError("texture coordinate accessor exceeds bufferView")


def validate_glb(path: Path, *, basic_static: bool = False) -> dict[str, Any]:
    doc, chunks = read_glb(path)
    binary = chunks[1][1] if len(chunks) == 2 else b""
    buffers = _entries(doc, "buffers")
    if len(buffers) != 1 or "uri" in buffers[0]:
        raise ValueError("GLB must have one embedded buffer without URI")
    size = _integer(buffers[0].get("byteLength"), "buffer.byteLength", 1)
    if not size <= len(binary) <= size + 3 or any(binary[size:]):
        raise ValueError("GLB BIN length or padding does not match buffer")
    views = _entries(doc, "bufferViews")
    for view in views:
        if view.get("buffer") != 0:
            raise ValueError("bufferView must reference embedded buffer 0")
        offset = _integer(view.get("byteOffset", 0), "bufferView.byteOffset")
        length = _integer(view.get("byteLength"), "bufferView.byteLength", 1)
        if offset + length > size:
            raise ValueError("bufferView exceeds embedded buffer")
    images = _entries(doc, "images")
    for image in images:
        if "uri" in image:
            raise ValueError("GLB images must be embedded without URI")
        view = _index(image.get("bufferView"), views, "image.bufferView")
        mime = image.get("mimeType")
        expected = {"image/png": "PNG", "image/jpeg": "JPEG"}.get(mime)
        if expected is None:
            raise ValueError(f"unsupported image MIME: {mime}")
        start = view.get("byteOffset", 0)
        payload = binary[start:start + view["byteLength"]]
        try:
            with warnings.catch_warnings():
                warnings.simplefilter("error", Image.DecompressionBombWarning)
                with Image.open(io.BytesIO(payload)) as decoded:
                    if decoded.format != expected:
                        raise ValueError("image MIME does not match encoded image")
                    decoded.verify()
                with Image.open(io.BytesIO(payload)) as decoded:
                    decoded.load()
        except (OSError, ValueError, SyntaxError, Image.DecompressionBombWarning, Image.DecompressionBombError) as error:
            raise ValueError(f"invalid embedded {expected} image: {error}") from error
    textures = _entries(doc, "textures")
    for texture in textures:
        _index(texture.get("source"), images, "texture.source")
    materials = _entries(doc, "materials")
    for material in materials:
        pbr = material.get("pbrMetallicRoughness", {})
        if not isinstance(pbr, dict):
            raise ValueError("invalid pbrMetallicRoughness")
        texture_infos = [material.get(key) for key in ("normalTexture", "occlusionTexture", "emissiveTexture")]
        texture_infos += [pbr.get(key) for key in ("baseColorTexture", "metallicRoughnessTexture")]
        for texture_info in texture_infos:
            if texture_info is not None:
                if not isinstance(texture_info, dict):
                    raise ValueError("invalid material texture info")
                _index(texture_info.get("index"), textures, "material texture")
    accessors = _entries(doc, "accessors")
    meshes = _entries(doc, "meshes")
    if not meshes:
        raise ValueError("GLB contains no mesh")
    for mesh in meshes:
        primitives = mesh.get("primitives")
        if not isinstance(primitives, list) or not primitives:
            raise ValueError("mesh contains no primitives")
        for primitive in primitives:
            if not isinstance(primitive, dict) or not isinstance(primitive.get("attributes"), dict):
                raise ValueError("invalid mesh primitive")
            accessor = _index(primitive["attributes"].get("POSITION"), accessors, "POSITION accessor")
            _integer(accessor.get("count"), "POSITION count", 1)
            if accessor.get("type") != "VEC3" or accessor.get("componentType") != 5126:
                raise ValueError("POSITION must be float VEC3")
            view = _index(accessor.get("bufferView"), views, "POSITION bufferView")
            offset = _integer(accessor.get("byteOffset", 0), "POSITION byteOffset")
            stride = _integer(view.get("byteStride", 12), "POSITION stride", 12)
            if offset + (accessor["count"] - 1) * stride + 12 > view["byteLength"]:
                raise ValueError("POSITION accessor exceeds bufferView")
            if "material" in primitive:
                material = _index(primitive["material"], materials, "primitive.material")
                if basic_static:
                    color_texture = material.get("pbrMetallicRoughness", {}).get("baseColorTexture")
                    if color_texture is not None:
                        _validate_texture_coordinates(primitive, color_texture, accessors, views, accessor["count"])
    if basic_static:
        if doc.get("animations") or doc.get("skins"):
            raise ValueError("animated or skinned models are outside initial support")
        for material in materials:
            if any(key in material for key in ("normalTexture", "occlusionTexture", "emissiveTexture")):
                raise ValueError("only base color textures are supported")
            pbr = material.get("pbrMetallicRoughness", {})
            if "metallicRoughnessTexture" in pbr:
                raise ValueError("only base color textures are supported")
            # Assimp 5.2 duplicates a supported diffuse map in its legacy extension.
            # Accept that exact mirror; reject texture channels the renderer ignores.
            for name, extension in material.get("extensions", {}).items():
                if not isinstance(extension, dict):
                    raise ValueError("invalid material extension")
                for key, value in extension.items():
                    if "texture" not in key.lower():
                        continue
                    if name == "KHR_materials_pbrSpecularGlossiness" and key == "diffuseTexture" and value == pbr.get("baseColorTexture"):
                        continue
                    raise ValueError("material extension textures are outside initial support")
    return doc
