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
