import json
import struct
import sys
from pathlib import Path

import pytest


for parent in Path(__file__).resolve().parents:
    contract_path = parent / "docs" / "workflow_operator_contract.py"
    if contract_path.exists():
        sys.path.insert(0, str(contract_path.parent))
        break

import operators
from operators import CommandResult, ConverterError, converter_status, invoke_operator, list_operators
from workflow_operator_contract import assert_operator_metadata_contract


def file_plan(source: Path, target: Path, source_format: str, target_format: str):
    return {
        "schema_version": "addp.workflow.access-plan/v1",
        "source": {
            "kind": "file",
            "format": source_format,
            "access": {"method": "mounted_path", "path": str(source)},
        },
        "target": {
            "kind": "file",
            "format": target_format,
            "name": target.name,
            "write_mode": "create",
            "access": {"method": "mounted_path", "path": str(target)},
        },
    }


def directory_plan(source: Path, target: Path, source_format: str, target_format: str, *, entrypoint=""):
    source_part = {
        "kind": "directory",
        "format": source_format,
        "access": {"method": "mounted_path", "path": str(source)},
    }
    if entrypoint:
        source_part["entrypoint"] = entrypoint
    return {
        "schema_version": "addp.workflow.access-plan/v1",
        "source": source_part,
        "target": {
            "kind": "directory" if target_format == "3dtiles" else "file",
            "format": target_format,
            "name": target.name,
            "write_mode": "create",
            "access": {"method": "mounted_path", "path": str(target)},
        },
    }


def _glb_bytes(doc: dict, binary: bytes = b"") -> bytes:
    json_bytes = json.dumps(doc, separators=(",", ":")).encode("utf-8")
    json_bytes += b" " * ((4 - len(json_bytes) % 4) % 4)
    chunks = [(b"JSON", json_bytes)]
    if binary:
        binary += b"\x00" * ((4 - len(binary) % 4) % 4)
        chunks.append((b"BIN\x00", binary))
    total = 12 + sum(8 + len(data) for _, data in chunks)
    result = bytearray(struct.pack("<4sII", b"glTF", 2, total))
    for kind, data in chunks:
        result += struct.pack("<I4s", len(data), kind) + data
    return bytes(result)


def _glb_json(data: bytes) -> dict:
    length, kind = struct.unpack_from("<I4s", data, 12)
    assert kind == b"JSON"
    return json.loads(data[20:20 + length].decode("utf-8").rstrip(" \x00"))


def test_operator_metadata_contract_and_modes():
    ops = list_operators()
    assert [operator["name"] for operator in ops] == [
        "osgb_to_glb", "gltf_to_glb", "fbx_to_glb", "obj_to_glb", "stl_to_glb", "ifc_to_glb",
        "osgb_scene_to_3dtiles", "gaussian_splat_to_ksplat",
    ]
    assert_operator_metadata_contract(ops, expected_engine_type="model3d_workflow")
    assert all(operator["execution_modes"] == ["workflow", "direct"] for operator in ops)
    assert all(operator["effects"] == ["read", "write"] for operator in ops)


def test_converter_status_defaults_to_bound_binaries():
    status = converter_status(env={})
    assert status["binding"] == "model3d_workflow"
    assert status["path"].endswith("engines/model3d-workflow/bin/_3dtile")
    assert status["mesh_converter"]["path"].endswith("engines/model3d-workflow/bin/assimp")
    assert status["ifc_converter"]["path"].endswith("engines/model3d-workflow/bin/IfcConvert")


def test_gaussian_splat_to_ksplat_uses_v1_plan(tmp_path):
    source = tmp_path / "model.ply"
    source.write_bytes(b"ply")
    target = tmp_path / "out" / "model.ksplat"
    node = tmp_path / "node"
    node.write_text("#!/bin/sh\n", encoding="utf-8")
    node.chmod(0o755)

    def fake_runner(command, timeout_seconds):
        Path(command[3]).write_bytes(b"ksplat")
        return CommandResult(returncode=0, stdout="converted")

    facts = invoke_operator(
        "gaussian_splat_to_ksplat",
        {
            "access_plan": file_plan(source, target, "ply", "ksplat"),
            "options": {"compression_level": 2, "alpha_threshold": 8, "spherical_harmonics_degree": 1},
        },
        runner=fake_runner,
        env={"MODEL3D_GAUSSIAN_SPLAT_NODE_BIN": str(node)},
    )
    assert target.read_bytes() == b"ksplat"
    assert facts["ksplat_uri"] == str(target)
    assert facts["source_format"] == "ply"


def test_gaussian_splat_rejects_existing_ksplat_source(tmp_path):
    source = tmp_path / "model.ksplat"
    source.write_bytes(b"ksplat")
    with pytest.raises(ConverterError, match="PLY or SPLAT"):
        invoke_operator(
            "gaussian_splat_to_ksplat",
            {"access_plan": file_plan(source, tmp_path / "copy.ksplat", "ksplat", "ksplat")},
        )


def test_osgb_to_glb_publishes_mounted_file(tmp_path):
    source = tmp_path / "model.osgb"
    source.write_bytes(b"osgb")
    target = tmp_path / "out" / "model.glb"

    def fake_runner(command, timeout_seconds):
        Path(command[-1]).write_bytes(_glb_bytes({"asset": {"version": "2.0"}}))
        return CommandResult(returncode=0)

    facts = invoke_operator(
        "osgb_to_glb",
        {"access_plan": file_plan(source, target, "osgb", "glb")},
        runner=fake_runner,
        env={"MODEL3D_CONVERTER_BIN": str(tmp_path / "_3dtile")},
    )
    assert target.is_file()
    assert facts["glb_uri"] == str(target)


@pytest.mark.parametrize(
    ("operator_name", "suffix", "source_format"),
    [
        ("gltf_to_glb", ".gltf", "gltf"),
        ("fbx_to_glb", ".fbx", "fbx"),
        ("obj_to_glb", ".obj", "obj"),
        ("stl_to_glb", ".stl", "stl"),
    ],
)
def test_mesh_converters_publish_glb(tmp_path, operator_name, suffix, source_format):
    source_dir = tmp_path / source_format
    source_dir.mkdir()
    source = source_dir / f"model{suffix}"
    source.write_text("v 0 0 0\n" if source_format == "obj" else "mesh", encoding="utf-8")
    target = tmp_path / "out" / f"{source_format}.glb"

    def fake_runner(command, timeout_seconds):
        Path(command[-2]).write_bytes(_glb_bytes({"asset": {"version": "2.0"}}))
        return CommandResult(returncode=0)

    plan = directory_plan(source_dir, target, source_format, "glb", entrypoint=source.name)
    if source_format == "stl":
        plan = file_plan(source, target, source_format, "glb")
    facts = invoke_operator(
        operator_name,
        {"access_plan": plan},
        runner=fake_runner,
        env={"MODEL3D_MESH_CONVERTER_BIN": str(tmp_path / "assimp")},
    )
    assert target.is_file()
    assert facts["source_format"] == source_format


def test_ifc_to_glb_defaults_to_center_model(tmp_path):
    source = tmp_path / "building.ifc"
    source.write_text("ISO-10303-21;", encoding="utf-8")
    target = tmp_path / "building.glb"
    converter = tmp_path / "IfcConvert"
    converter.write_text("#!/bin/sh\n", encoding="utf-8")
    converter.chmod(0o755)
    captured = {}

    def fake_runner(command, timeout_seconds):
        captured["command"] = command
        Path(command[-1]).write_bytes(_glb_bytes({"asset": {"version": "2.0"}}))
        return CommandResult(returncode=0)

    invoke_operator(
        "ifc_to_glb",
        {"access_plan": file_plan(source, target, "ifc", "glb")},
        runner=fake_runner,
        env={"MODEL3D_IFC_CONVERTER_BIN": str(converter)},
    )
    assert "--center-model" in captured["command"]


def test_obj_missing_material_library_is_rejected(tmp_path):
    source_dir = tmp_path / "obj"
    source_dir.mkdir()
    source = source_dir / "model.obj"
    source.write_text("mtllib missing.mtl\nv 0 0 0\n", encoding="utf-8")
    plan = directory_plan(source_dir, tmp_path / "model.glb", "obj", "glb", entrypoint=source.name)
    with pytest.raises(ConverterError, match="material libraries"):
        invoke_operator("obj_to_glb", {"access_plan": plan}, runner=lambda *_: CommandResult(0))


def test_obj_transparent_textured_material_is_repaired(tmp_path):
    source_dir = tmp_path / "obj"
    source_dir.mkdir()
    source = source_dir / "model.obj"
    source.write_text("v 0 0 0\n", encoding="utf-8")
    target = tmp_path / "model.glb"

    def fake_runner(command, timeout_seconds):
        Path(command[-2]).write_bytes(_glb_bytes({
            "asset": {"version": "2.0"},
            "materials": [{
                "pbrMetallicRoughness": {"baseColorTexture": {"index": 0}, "baseColorFactor": [1, 1, 1, 0]},
                "alphaMode": "BLEND",
            }],
            "textures": [{"source": 0}],
            "images": [{"bufferView": 0, "mimeType": "image/jpeg"}],
        }, b"jpg"))
        return CommandResult(returncode=0)

    plan = directory_plan(source_dir, target, "obj", "glb", entrypoint=source.name)
    facts = invoke_operator("obj_to_glb", {"access_plan": plan}, runner=fake_runner)
    material = _glb_json(target.read_bytes())["materials"][0]
    assert material["pbrMetallicRoughness"]["baseColorFactor"][3] == 1.0
    assert facts["postprocess"]["material_count"] == 1


def test_osgb_scene_to_3dtiles_publishes_directory_with_marker(tmp_path):
    source = tmp_path / "scene"
    source.mkdir()
    target = tmp_path / "tiles"

    def fake_runner(command, timeout_seconds):
        output = Path(command[-1])
        output.mkdir(parents=True, exist_ok=True)
        (output / "0.b3dm").write_bytes(b"tile")
        (output / "tileset.json").write_text("{}", encoding="utf-8")
        return CommandResult(returncode=0)

    facts = invoke_operator(
        "osgb_scene_to_3dtiles",
        {"access_plan": directory_plan(source, target, "osgb_scene", "3dtiles")},
        runner=fake_runner,
        env={"MODEL3D_CONVERTER_BIN": str(tmp_path / "_3dtile")},
    )
    assert (target / "tileset.json").is_file()
    assert facts["tile_count"] == 1
    assert facts["tileset_locator"] == str(target)
