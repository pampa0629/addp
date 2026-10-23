import hashlib
import os
from pathlib import Path

import pytest

import obj_model
from exchange_model import validate_source
from operators import CommandResult, ConverterError, invoke_operator
from .glb_fixture import glb_bytes, triangle_doc
from .test_operators import directory_plan


def model(tmp_path, *, primary="mtllib paint.mtl\nv 0 0 0\n", material="newmtl paint\nmap_Kd colors/red.png\n"):
    source = tmp_path / "source"
    source.mkdir()
    (source / "model.obj").write_text(primary, encoding="utf-8")
    (source / "paint.mtl").write_text(material, encoding="utf-8")
    (source / "colors").mkdir()
    (source / "colors/red.png").write_bytes(b"input texture")
    return source


def test_obj_multiple_sibling_mtllibs_preserve_spaces_and_literal_texture_names(tmp_path):
    source = model(tmp_path, primary="mtllib paint red.mtl\nmtllib paint blue.mtl\nmtllib paint red.mtl\n")
    (source / "paint.mtl").rename(source / "paint red.mtl")
    (source / "paint blue.mtl").write_text("newmtl blue\nmap_Kd -clamp on -o -1 0 1 colors/blue%20literal.JPG\n")
    (source / "colors/blue%20literal.JPG").write_bytes(b"other texture")
    (source / "colors/blue literal.JPG").write_bytes(b"must not be selected")
    before = {str(p.relative_to(source)): p.read_bytes() for p in source.rglob("*") if p.is_file()}
    assert validate_source(source / "model.obj", "obj") == ["colors/red.png", "colors/blue%20literal.JPG"]
    assert {str(p.relative_to(source)): p.read_bytes() for p in source.rglob("*") if p.is_file()} == before


@pytest.mark.parametrize("primary,material", [("v 0 0 0\n", "unused"), ("mtllib paint.mtl\n", "")])
def test_obj_known_empty_compositions_are_valid(tmp_path, primary, material):
    source = model(tmp_path, primary=primary, material=material)
    assert validate_source(source / "model.obj", "obj") == []


@pytest.mark.parametrize("ref", ["nested/paint.mtl", "../paint.mtl", "/paint.mtl", "https://host/paint.mtl",
                                 "./paint.mtl", "paint.MTL", "missing.mtl", "colors/red.png"])
def test_obj_mtl_is_exact_sibling_and_never_basename_fallback(tmp_path, ref):
    source = model(tmp_path, primary=f"mtllib {ref}\n")
    (source / "nested").mkdir()
    (source / "nested/paint.mtl").write_text("newmtl nested\n")
    with pytest.raises((ValueError, OSError)):
        validate_source(source / "model.obj", "obj")


@pytest.mark.parametrize("ref", ["../outside.png", "/outside.png", "https://host/p.png", "colors/../red.png",
                                 "colors//red.png", "colors/./red.png", "colors/Red.png", "missing.png",
                                 "colors/red.bmp", "colors/empty.png", "colors\\red.png"])
def test_obj_texture_paths_are_exact_png_jpeg_under_model_root(tmp_path, ref):
    source = model(tmp_path, material=f"newmtl paint\nmap_Kd {ref}\n")
    (source / "colors/empty.png").touch()
    with pytest.raises((ValueError, OSError)):
        validate_source(source / "model.obj", "obj")


@pytest.mark.parametrize("target", ["primary", "mtl", "texture", "directory"])
def test_obj_rejects_every_symlink_component_before_read(tmp_path, target):
    source = model(tmp_path)
    outside = tmp_path / "outside"
    outside.mkdir()
    if target == "directory":
        (source / "colors/red.png").unlink()
        (source / "colors").rmdir()
        (outside / "red.png").write_bytes(b"external")
        (source / "colors").symlink_to(outside, target_is_directory=True)
    else:
        name = {"primary": "model.obj", "mtl": "paint.mtl", "texture": "colors/red.png"}[target]
        original = source / name
        external = outside / original.name
        original.rename(external)
        original.symlink_to(external)
    with pytest.raises((ValueError, OSError)):
        validate_source(source / "model.obj", "obj")


@pytest.mark.parametrize("kind", ["mtl", "texture"])
def test_obj_never_blocks_on_fifo_dependencies(tmp_path, kind):
    source = model(tmp_path)
    item = source / ("paint.mtl" if kind == "mtl" else "colors/red.png")
    item.unlink()
    os.mkfifo(item)
    with pytest.raises((ValueError, OSError)):
        validate_source(source / "model.obj", "obj")


@pytest.mark.parametrize("material,text", [
    (False, "mtllib 'paint.mtl'"), (False, "mtllib paint.mtl # comment"),
    (False, "mtllib paint.mtl "), (False, "mtllib paint.mtl\\\nmore.mtl"),
    (False, "call external.obj"), (False, "csh command"), (False, "maplib old.map"),
    (False, "MTLLIB paint.mtl"), (False, "mtllib_more paint.mtl"),
    (True, "map_unknown image.png"), (True, "refl image.png"), (True, "decal image.png"),
    (True, "map_Kd -o 1 2 image.png"), (True, "map_Kd -s 1 image.png"),
    (True, "map_Kd -t 0 0 image.png"), (True, "map_Kd -mm 0 image.png"),
    (True, "map_Kd -clamp yes image.png"), (True, "map_Kd -boost NaN image.png"),
    (True, "map_Kd -bm Inf image.png"), (True, "map_Kd -texres 0 image.png"),
    (True, "map_Kd -imfchan invalid image.png"), (True, "map_Kd -unknown 1 image.png"),
    (True, "map_Kd -type sphere image.png"), (True, "map_Kd -clamp on"),
    (True, "map_Kd \"image.png\""), (True, "map_Kd image.png\t"),
])
def test_obj_and_mtl_ambiguous_declarations_are_rejected_atomically(material, text):
    prefix = "map_Kd valid.png\n" if material else "mtllib valid.mtl\n"
    with pytest.raises(ValueError):
        obj_model.parse_composition((prefix + text + "\n").encode(), material=material)


def test_obj_parser_requires_complete_bounded_text_and_counts_duplicates():
    data = b"mtllib paint.mtl\n"
    assert obj_model.parse_composition(data * 200) == ["paint.mtl"]
    with pytest.raises(ValueError):
        obj_model.parse_composition(data * 201)
    assert obj_model.parse_composition(b"# long enough for more than one range\n" * 40000 + data) == ["paint.mtl"]
    for raw in [b"", b"v \x00 0 0\n", b"v \xff 0 0\n", b"\xef\xbb\xbfmtllib paint.mtl\n",
                b"mtllib " + b"a" * 4097, b"#" * (obj_model.MAX_LINE_BYTES + 1)]:
        with pytest.raises((ValueError, UnicodeError)):
            obj_model.parse_composition(raw)


def test_obj_mtl_aggregate_budget_is_checked_before_content_read(tmp_path, monkeypatch):
    source = model(tmp_path, primary="mtllib a.mtl\nmtllib b.mtl\n")
    (source / "a.mtl").write_bytes(b"#" * 20)
    (source / "b.mtl").write_bytes(b"#" * 20)
    monkeypatch.setattr(obj_model, "MAX_COMPOSITION_BYTES", 30)
    # Primary is below 30, each MTL is below 30, but their total is not.
    with pytest.raises(ValueError, match="budget"):
        validate_source(source / "model.obj", "obj")


def test_obj_total_reference_budget_includes_primary_and_mtl(tmp_path, monkeypatch):
    source = model(tmp_path)
    monkeypatch.setattr(obj_model, "MAX_COMPOSITION_REFS", 2)
    with pytest.raises(ValueError, match="reference budget"):
        validate_source(source / "model.obj", "obj")


def test_obj_operator_does_not_let_another_extension_select_a_different_importer(tmp_path):
    source = tmp_path / "foreign.dae"
    source.write_bytes(b"<COLLADA/>")
    calls = []
    target = tmp_path / "previous.glb"
    target.write_bytes(b"previous valid artifact")
    plan = directory_plan(tmp_path, target, "obj", "glb", entrypoint=source.name)
    plan["target"]["write_mode"] = "replace"
    with pytest.raises(ConverterError) as failure:
        invoke_operator("obj_to_glb", {"access_plan": plan}, runner=lambda *a: calls.append(a))
    assert failure.value.error_code == "UNSUPPORTED_MODEL_SOURCE"
    assert not calls and target.read_bytes() == b"previous valid artifact"


def test_obj_missing_material_never_uses_same_basename_or_overwrites_output(tmp_path):
    source = model(tmp_path, primary="mtllib missing.mtl\n")
    (source / "model.mtl").write_bytes((source / "paint.mtl").read_bytes())
    target = tmp_path / "previous.glb"
    target.write_bytes(b"previous valid model")
    plan = directory_plan(source, target, "obj", "glb", entrypoint="model.obj")
    plan["target"]["write_mode"] = "replace"
    calls = []
    with pytest.raises(ConverterError) as failure:
        invoke_operator("obj_to_glb", {"access_plan": plan}, runner=lambda *a: calls.append(a))
    assert failure.value.error_code == "UNSUPPORTED_MODEL_SOURCE"
    assert not calls and target.read_bytes() == b"previous valid model"


def test_obj_converter_cannot_publish_gray_model_when_source_declares_texture(tmp_path):
    source = model(tmp_path)
    target = tmp_path / "previous.glb"
    target.write_bytes(b"previous valid model")
    plan = directory_plan(source, target, "obj", "glb", entrypoint="model.obj")
    plan["target"]["write_mode"] = "replace"

    def runner(command, timeout_seconds):
        Path(command[-2]).write_bytes(glb_bytes())
        return CommandResult(0)

    with pytest.raises(ConverterError) as failure:
        invoke_operator("obj_to_glb", {"access_plan": plan}, runner=runner)
    assert failure.value.error_code == "INVALID_GLB"
    assert target.read_bytes() == b"previous valid model"


def test_obj_valid_source_uses_one_converter_and_keeps_source_bytes(tmp_path):
    source = model(tmp_path, primary="mtllib paint name.mtl\nv 0 0 0\n")
    (source / "paint.mtl").rename(source / "paint name.mtl")
    before = {str(p.relative_to(source)): hashlib.sha256(p.read_bytes()).hexdigest() for p in source.rglob("*") if p.is_file()}
    target = tmp_path / "result.glb"
    calls = []

    def runner(command, timeout_seconds):
        calls.append(command)
        doc, binary = triangle_doc("PNG")
        Path(command[-2]).write_bytes(glb_bytes(doc, binary))
        return CommandResult(0)

    invoke_operator("obj_to_glb", {"access_plan": directory_plan(source, target, "obj", "glb", entrypoint="model.obj")}, runner=runner)
    assert len(calls) == 1 and calls[0][-1] == "-embtex" and target.is_file()
    assert {str(p.relative_to(source)): hashlib.sha256(p.read_bytes()).hexdigest() for p in source.rglob("*") if p.is_file()} == before


def test_obj_validator_is_packaged_in_existing_product_runtime():
    root = Path(__file__).resolve().parents[1]
    dockerfile = (root / "docker/runtime/Dockerfile").read_text()
    copies = [line for line in dockerfile.splitlines() if line.startswith("COPY ") and line.endswith(" /app/")]
    assert any("engines/model3d-workflow/obj_model.py" in line for line in copies)
    assert "CMD [\"python\", \"api_server.py\"]" in dockerfile
