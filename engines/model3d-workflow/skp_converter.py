"""SketchUp conversion worker; the parent owns access, timeout and publication."""
from __future__ import annotations

import sys
from pathlib import Path

from openskp import SkpFile
from openskp.export import instanced_glb

from glb_validation import read_glb, validate_glb
from operators import _write_glb


def convert(source: Path, target: Path) -> None:
    skp = SkpFile.open(str(source))
    model = skp.parse()
    # This release records visibility but its GLB exporter does not apply it.
    # Reject it rather than publish a scene containing hidden geometry.
    if any(layer.hidden for layer in model.layers) or any(
        any(instance.hidden for instance in definition.instances)
        or any(face.hidden for face in definition.faces.values())
        for definition in [model.root, *model.definitions.values()]
    ):
        raise ValueError("Hidden components, faces or layers are outside supported SKP conversion scope")
    for material in model.materials:
        if material.texture is not None and not material.texture.data:
            raise ValueError(f"Missing embedded texture for material {material.name!r}")
    instanced_glb.export(skp, str(target), textures=True)
    doc, chunks = read_glb(target)
    # OpenSKP 1.3.0 exports millimetres. Scale the entire graph (including
    # translations), preserving shared meshes and local instance transforms.
    roots = doc["scenes"][doc.get("scene", 0)]["nodes"]
    root = len(doc["nodes"])
    doc["nodes"].append({"name": "SketchUp millimetres to metres", "scale": [0.001] * 3, "children": roots})
    doc["scenes"][doc.get("scene", 0)]["nodes"] = [root]
    _write_glb(target, doc, chunks)
    validate_glb(target, basic_static=True)


if __name__ == "__main__":
    try:
        convert(Path(sys.argv[1]), Path(sys.argv[2]))
    except Exception as error:
        print(f"{type(error).__name__}: {error}", file=sys.stderr)
        raise SystemExit(1)
