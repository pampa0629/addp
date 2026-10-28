"""Strict basic VFF materials for the fixed OpenSKP conversion dependency.

This is a conversion guard, not a complete SKP composition proof.
"""
from __future__ import annotations

import io
import math
from collections import Counter
from itertools import chain

from PIL import Image
from pathlib import PurePosixPath

NS = "{http://sketchup.google.com/schemas/sketchup/1.0/material}"


def validate_vff_material_bindings(source, model):
    """Refuse ambiguous identities before export, not prove full composition.

    Read original registrations rather than the SDK's already-overwritten ID
    dictionary. Use the same fixed SDK's tree framing, one subtree at a time;
    unknown resource semantics remain outside this conversion guard.
    """
    import zipfile
    from defusedxml import ElementTree as ET
    from openskp import _core
    from openskp.legacy import is_legacy

    with source.open('rb') as stream:
        if is_legacy(stream.read(512)):
            return
    folders, names, member_names, registry = {}, {}, {}, {}
    with zipfile.ZipFile(source) as archive:
        members = archive.namelist()
        counts = Counter(members)
        for member in members:
            if not (member.startswith('materials/') and member.endswith('material.xml')):
                continue
            parts = member.split('/')
            if len(parts) != 3 or parts[2] != 'material.xml' or parts[1] in ('', '.', '..') or counts[member] != 1:
                raise ValueError('Ambiguous SKP material XML identity')
            _core._validate_zip_entry_size(archive, member)
            root = ET.fromstring(archive.read(member), forbid_dtd=True, forbid_entities=True, forbid_external=True)
            name = material_element(root).get('name')
            if parts[1] in folders or name in names:
                raise ValueError('Duplicate SKP material XML name or directory')
            folders[parts[1]] = member
            names[name] = member
            member_names[member] = name
        if counts['model.dat'] != 1:
            raise ValueError('SKP material bindings require one model.dat')
        _core._validate_zip_entry_size(archive, 'model.dat')
        data = archive.read('model.dat')
    for _, _, node in _core.iter_top_level_lazy(data, 0, len(data), _core.CONTAINER_TAGS):
        pending = [node]
        while pending:
            current = pending.pop()
            pending.extend(current['children'])
            if current['tag'] != 'C832':
                continue
            ids = [child for child in current['children'] if child['tag'] == 'DC05']
            tokens = [child for child in current['children'] if child['tag'] == 'CC32']
            if len(ids) != 1 or len(tokens) != 1:
                raise ValueError('Ambiguous SKP material registration')
            payload = ids[0]['payload']
            if len(payload) < 7 or payload[:2] != bytes.fromhex('DE05'):
                raise ValueError('Unsupported SKP material ID encoding')
            size = int.from_bytes(payload[2:6], 'little')
            if not 1 <= size <= 8 or len(payload) != 6 + size:
                raise ValueError('Invalid SKP material ID encoding')
            ident = int.from_bytes(payload[6:], 'little')
            token = tokens[0]['payload'].decode('utf-8', errors='strict')
            if ident in registry:
                raise ValueError('Duplicate SKP material ID')
            candidates = {mapping[token] for mapping in (folders, names) if token in mapping}
            if not token or len(candidates) != 1:
                raise ValueError('Missing or conflicting SKP material identity')
            member = candidates.pop()
            registry[ident] = member_names[member]
    if set(model.materials_by_id) != set(registry) or any(
        model.materials_by_id[ident].name != name for ident, name in registry.items()
    ):
        raise ValueError('SDK SKP material binding differs from source registration')
    for definition in chain((model.root,), model.definitions.values()):
        references = (ident for face in definition.faces.values()
                      for ident in (face.material_id, face.back_material_id))
        for ident in chain(references, (instance.material_id for instance in definition.instances)):
            if ident is not None and ident not in registry:
                raise ValueError('Dangling SKP material reference')


def _shape(element, attributes, children=()):
    if set(element.attrib) - set(attributes):
        raise ValueError(f"Unsupported SKP material attributes in {element.tag}")
    if (element.text or "").strip() or any((child.tail or "").strip() for child in element):
        raise ValueError("Unsupported SKP material mixed content")
    if any(child.tag not in {NS + name for name in children} for child in element):
        raise ValueError(f"Unsupported SKP material node in {element.tag}")
    tags = [child.tag for child in element]
    if len(tags) != len(set(tags)):
        raise ValueError("Duplicate SKP material node")


def _number(value):
    result = float(value)
    if not math.isfinite(result):
        raise ValueError("Non-finite SKP material value")
    return result


def material_element(root):
    if root.tag != NS + "materialDocument":
        raise ValueError("Unsupported SKP material document")
    _shape(root, (), ("material",))
    if len(root) != 1:
        raise ValueError("SKP material document must contain one material")
    material = root[0]
    _shape(material, ("name", "type", "workflow", "colorRed", "colorGreen", "colorBlue",
                      "colorizeType", "trans", "useTrans", "pbrPromoState", "hasTexture"),
           ("texture", "pbrMR"))
    if not material.get("name"):
        raise ValueError("Missing SKP material name")
    if material.get("type", "0") not in ("0", "1"):
        raise ValueError("Unsupported SKP colorized material")
    for key in ("workflow", "colorizeType", "pbrPromoState"):
        if material.get(key, "0") != "0":
            raise ValueError(f"Unsupported SKP material {key}")
    for key in ("colorRed", "colorGreen", "colorBlue"):
        if not 0 <= int(material.get(key, "128")) <= 255:
            raise ValueError("Invalid SKP material color")
    if material.get("useTrans", "0") not in ("0", "1") or not 0 <= _number(material.get("trans", "0")) <= 1:
        raise ValueError("Invalid SKP material transparency")
    texture = material.find(NS + "texture")
    textured = texture is not None
    if (material.get("type", "0") == "1") != textured:
        raise ValueError("Inconsistent SKP material texture type")
    if material.get("hasTexture", str(int(textured))) != str(int(textured)):
        raise ValueError("Inconsistent SKP material texture flag")
    pbr = material.find(NS + "pbrMR")
    if pbr is not None:
        flags = {"enable_metalness", "enable_roughness", "enable_normal", "enable_occlusion"}
        known = flags | {"roughness_texture_invert", "metallicFactor", "roughnessFactor",
                         "normalMapStyle", "normalScale", "occlusionStrength", "baseColorFactor"}
        _shape(pbr, ("xScale", "yScale"), known)
        for value in pbr.attrib.values():
            _number(value)
        if not flags.issubset({item.tag[len(NS):] for item in pbr}):
            raise ValueError("Unproven SKP PBR activation flags")
        defaults = {"roughness_texture_invert": 0, "metallicFactor": 1, "roughnessFactor": 1,
                    "normalMapStyle": 1, "normalScale": 1, "occlusionStrength": 1,
                    "baseColorFactor": 4294967295}
        for item in pbr:
            if item.attrib or len(item):
                raise ValueError("Unsupported SKP PBR structure")
            name = item.tag[len(NS):]
            value = (item.text or "").strip()
            if name in flags and value != "0":
                raise ValueError(f"Unsupported enabled SKP PBR slot: {name}")
            if name in defaults and _number(value) != defaults[name]:
                raise ValueError(f"Unsupported non-default SKP PBR value: {name}")
    return material


def extract_texture(zf, xml_name, material, ns):
    texture = material.find("mat:texture", ns)
    if texture is None:
        return None
    _shape(texture, ("textureFilename", "xScale", "yScale", "avgColor"), ("images",))
    x_scale = _number(texture.get("xScale", "0"))
    y_scale = _number(texture.get("yScale", "0"))
    if x_scale < 0 or y_scale < 0:
        raise ValueError("Invalid SKP texture scale")
    images = texture.find(NS + "images")
    if images is not None:
        _shape(images, (), ("image",))
        if len(images) != 1:
            raise ValueError("SKP texture requires one exact image declaration")
        image = images[0]
        _shape(image, ("id", "path", "file_name"))
        reference = image.get("path", "")
    else:
        reference = texture.get("textureFilename", "")
    # textureFilename may be a historical host path. It is never a fallback
    # when an actual image path exists, nor a reason to read host/network data.
    if reference.startswith("./"):
        reference = reference[2:]
    parts = reference.split("/")
    if not reference or any(part in ("", ".", "..") for part in parts) or any(
        char in reference for char in ("\\", ":", "\x00")
    ):
        raise ValueError("Invalid SKP embedded image path")
    folder = str(PurePosixPath(xml_name).parent)
    candidate = reference if parts[0] == "materials" else folder + "/" + reference
    matches = [entry for entry in zf.infolist() if entry.filename == candidate]
    if len(matches) != 1 or matches[0].is_dir():
        raise ValueError("Missing or ambiguous declared SKP embedded image")
    from openskp._core import _validate_zip_entry_size
    _validate_zip_entry_size(zf, candidate)
    data = zf.read(matches[0])
    if not data.startswith(b"\x89PNG\r\n\x1a\n") and not data.startswith(b"\xff\xd8\xff"):
        raise ValueError("Unsupported SKP embedded image; expected PNG or JPEG")
    with Image.open(io.BytesIO(data)) as image:
        if image.format not in ("PNG", "JPEG") or getattr(image, "n_frames", 1) != 1:
            raise ValueError("Unsupported SKP image encoding or animation")
        image.load()
    return {"filename": candidate.rsplit("/", 1)[-1], "x_scale": x_scale,
            "y_scale": y_scale, "data": data}
