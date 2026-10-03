"""A valid embedded triangle, optionally with a real encoded texture."""
import io
import json
import struct

from PIL import Image


def triangle_doc(texture_format=None):
    binary = struct.pack("<9f", 0, 0, 0, 1, 0, 0, 0, 1, 0)
    doc = {"asset": {"version": "2.0"}, "buffers": [{"byteLength": len(binary)}],
           "bufferViews": [{"buffer": 0, "byteLength": len(binary)}],
           "accessors": [{"bufferView": 0, "componentType": 5126, "count": 3, "type": "VEC3",
                          "min": [0, 0, 0], "max": [1, 1, 0]}],
           "meshes": [{"primitives": [{"attributes": {"POSITION": 0}}]}],
           "nodes": [{"mesh": 0}], "scenes": [{"nodes": [0]}], "scene": 0}
    if texture_format:
        output = io.BytesIO()
        Image.new("RGB", (2, 2), (245, 180, 20)).save(output, format=texture_format)
        image = output.getvalue()
        doc["bufferViews"].append({"buffer": 0, "byteOffset": len(binary), "byteLength": len(image)})
        binary += image
        doc["buffers"][0]["byteLength"] = len(binary)
        doc["images"] = [{"bufferView": 1, "mimeType": {"PNG": "image/png", "JPEG": "image/jpeg"}[texture_format]}]
        doc["textures"] = [{"source": 0}]
        binary += b"\x00" * (-len(binary) % 4)
        uv = struct.pack("<6f", 0, 0, 1, 0, 0, 1)
        doc["bufferViews"].append({"buffer": 0, "byteOffset": len(binary), "byteLength": len(uv)})
        binary += uv
        doc["buffers"][0]["byteLength"] = len(binary)
        doc["accessors"].append({"bufferView": 2, "componentType": 5126, "count": 3, "type": "VEC2"})
        doc["materials"] = [{"pbrMetallicRoughness": {"baseColorTexture": {"index": 0}}}]
        doc["meshes"][0]["primitives"][0].update(material=0)
        doc["meshes"][0]["primitives"][0]["attributes"]["TEXCOORD_0"] = 1
    return doc, binary


def glb_bytes(doc=None, binary=None):
    if doc is None:
        doc, binary = triangle_doc()
    binary = binary or b""
    payload = json.dumps(doc, separators=(",", ":")).encode()
    payload += b" " * (-len(payload) % 4)
    chunks = [(b"JSON", payload)]
    if binary:
        chunks.append((b"BIN\x00", binary + b"\x00" * (-len(binary) % 4)))
    result = bytearray(struct.pack("<4sII", b"glTF", 2, 12 + sum(8 + len(data) for _, data in chunks)))
    for kind, data in chunks:
        result += struct.pack("<I4s", len(data), kind) + data
    return bytes(result)
