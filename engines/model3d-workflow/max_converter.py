"""Static MAX conversion inside the engine-owned Blender process."""
from __future__ import annotations

import contextlib
import hashlib
import io
import json
import math
from pathlib import Path, PurePosixPath
import sys

UNIT_SCALES = {"mm": 0.001, "cm": 0.01, "m": 1.0, "km": 1000.0,
               "in": 0.0254, "ft": 0.3048, "mi": 1609.344}
MAX_SOURCE_BYTES = 64 * 1024 * 1024


def conversion_options(options):
    if not isinstance(options, dict) or options.keys() - {"source_unit", "texture_files"}:
        raise ValueError("MAX options only support source_unit and texture_files")
    unit = options.get("source_unit", "")
    if not isinstance(unit, str) or (unit and unit not in UNIT_SCALES):
        raise ValueError("MAX source_unit must be mm, cm, m, km, in, ft or mi")
    textures = options.get("texture_files", {})
    if not isinstance(textures, dict) or any(not isinstance(k, str) or not k or not isinstance(v, str) or not v for k, v in textures.items()):
        raise ValueError("MAX texture_files must map bitmap references to relative input paths")
    return {"source_unit": unit or "m", "unit_source": "user" if unit else "default",
            "scale_to_meters": UNIT_SCALES[unit or "m"]}, textures


def declared_texture(root: Path, mapping, reference):
    if reference not in mapping:
        raise ValueError("MAX required bitmap has no declared input: " + reference)
    raw = mapping[reference]
    relative = PurePosixPath(raw)
    if relative.is_absolute() or ".." in relative.parts or "\\" in raw or ":" in raw or "\x00" in raw:
        raise ValueError("MAX bitmap declaration escapes the input directory")
    target = (root / raw).resolve()
    if not target.is_relative_to(root.resolve()) or not target.is_file():
        raise ValueError("MAX declared bitmap is unavailable: " + reference)
    return target


def convert(source: Path, target: Path, options, addon: Path):
    import bpy
    if source.suffix.lower() != ".max" or not 0 < source.stat().st_size <= MAX_SOURCE_BYTES:
        raise ValueError("MAX source must be a nonempty file within the 64 MiB parsing budget")
    facts, textures = conversion_options(options)
    source_hash = hashlib.sha256(source.read_bytes()).hexdigest()
    sys.path.insert(0, str(addon.parent))
    import source as max_addon
    from source import import_max as parser
    max_addon.register()
    bpy.ops.object.select_all(action="SELECT")
    bpy.ops.object.delete(use_global=False)
    loaded = []

    def load_declared_image(imagepath, dirname, **kwargs):
        reference = str(imagepath)
        path = declared_texture(source.parent, textures, reference)
        image = bpy.data.images.load(str(path), check_existing=True)
        if not all(image.size):
            raise ValueError("MAX declared bitmap cannot be decoded: " + reference)
        loaded.append(reference)
        return image

    # Never let the add-on search the filesystem or follow original absolute paths.
    parser.load_image = load_declared_image
    original_material = parser.adjust_material

    def static_material(filename, search, obj, material):
        if material is not None and parser.get_guid(material) not in {0x0002, 0x0006, 0x0200, parser.PHYS_MTL}:
            raise ValueError("MAX material is outside the static base-color scope")
        return original_material(filename, search, obj, material)

    parser.adjust_material = static_material
    for name in ("get_standard_material", "get_legacy_material", "get_physical_material"):
        original = getattr(parser, name)
        def base_color_material(*args, _original=original, **kwargs):
            material = _original(*args, **kwargs)
            if material and any(material.get(key) for key in ("shinmap", "glossmap", "transmap", "normalmap")):
                raise ValueError("MAX non-base-color texture is outside supported conversion scope")
            return material
        setattr(parser, name, base_color_material)
    captured = io.StringIO()
    with contextlib.redirect_stdout(captured), contextlib.redirect_stderr(captured):
        bpy.ops.import_scene.max(filepath=str(source), use_image_search=False,
                                 use_apply_matrix=True, scale_objects=1.0)
    diagnostics = captured.getvalue()
    print(diagnostics)
    if any(marker in diagnostics for marker in ("ImportError:", "TypeError:", "ValueError:", "RuntimeError:", "ArrayLengthMismatchError:")):
        raise ValueError("MAX importer reported an object error; partial output is rejected")
    meshes = [obj for obj in bpy.context.scene.objects if obj.type == "MESH"]
    if not meshes or not any(len(obj.data.polygons) for obj in meshes):
        raise ValueError("MAX contains no successfully imported mesh")
    if any(not math.isfinite(value) for obj in meshes for vertex in obj.data.vertices for value in vertex.co):
        raise ValueError("MAX mesh contains non-finite positions")
    roots = [obj for obj in bpy.context.scene.objects if obj.parent is None]
    unit_root = bpy.data.objects.new("ADDP source unit to meters", None)
    bpy.context.scene.collection.objects.link(unit_root)
    for obj in roots:
        obj.parent = unit_root
    unit_root.scale = (facts["scale_to_meters"],) * 3
    bpy.context.view_layer.update()
    if hashlib.sha256(source.read_bytes()).hexdigest() != source_hash:
        raise ValueError("MAX source changed during conversion")
    bpy.ops.export_scene.gltf(filepath=str(target), export_format="GLB", export_yup=True,
                              export_animations=False, export_skins=False)
    facts.update(blender_version=bpy.app.version_string, importer_version="1.9.2",
                 texture_refs=sorted(set(loaded)), source_sha256=source_hash,
                 meshes=len(meshes), scope="static")
    target.with_suffix(".max-facts.json").write_text(json.dumps(facts))


if __name__ == "__main__":
    args = sys.argv[sys.argv.index("--") + 1:]
    source_path, target_path, options_path, addon_path = map(Path, args)
    convert(source_path, target_path, json.loads(options_path.read_text()), addon_path)
