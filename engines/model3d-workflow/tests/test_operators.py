import sys
import json
import struct
from pathlib import Path

import pytest


for parent in Path(__file__).resolve().parents:
    contract_path = parent / "docs" / "workflow_operator_contract.py"
    if contract_path.exists():
        sys.path.insert(0, str(contract_path.parent))
        break

import operators
from workflow_operator_contract import assert_operator_metadata_contract
from operators import CommandResult, ConverterError, converter_status, invoke_operator, list_operators


def test_operator_metadata_contract():
    operators = list_operators()
    assert [operator["name"] for operator in operators] == [
        "osgb_to_glb",
        "gltf_to_glb",
        "fbx_to_glb",
        "obj_to_glb",
        "stl_to_glb",
        "ifc_to_glb",
        "osgb_scene_to_3dtiles",
        "gaussian_splat_to_ksplat",
    ]
    assert_operator_metadata_contract(operators, expected_engine_type="model3d_workflow")


def test_converter_status_defaults_to_engine_bound_binary():
    status = converter_status(env={})

    assert status["binding"] == "model3d_workflow"
    assert status["env"] == "MODEL3D_CONVERTER_BIN"
    assert status["path"].endswith("engines/model3d-workflow/bin/_3dtile")
    assert status["mesh_converter"]["env"] == "MODEL3D_MESH_CONVERTER_BIN"
    assert status["mesh_converter"]["path"].endswith("engines/model3d-workflow/bin/assimp")
    assert status["ifc_converter"]["env"] == "MODEL3D_IFC_CONVERTER_BIN"
    assert status["ifc_converter"]["path"].endswith("engines/model3d-workflow/bin/IfcConvert")
    assert status["gaussian_splat_converter"]["env"] == "MODEL3D_GAUSSIAN_SPLAT_NODE_BIN"
    assert status["gaussian_splat_converter"]["script"].endswith("engines/model3d-workflow/create_ksplat.mjs")


def test_converter_status_rejects_path_command_name():
    status = converter_status(env={"MODEL3D_CONVERTER_BIN": "_3dtile", "PATH": "/usr/bin"})

    assert status["available"] is False
    assert "not a PATH command name" in status["details"]


def test_gaussian_splat_to_ksplat_publishes_existing_ksplat(tmp_path, monkeypatch):
    source = tmp_path / "model.ksplat"
    source.write_bytes(b"ksplat")
    captured = {}

    def fake_publish(path, publish):
        captured["path"] = path
        captured["publish"] = publish
        return {
            "object_uri": publish["locator"],
            "object_name": publish["object"],
            "uploaded_files": 1,
            "uploaded_bytes": path.stat().st_size,
            "content_type": publish.get("content_type") or "application/vnd.gaussian-ksplat",
        }

    monkeypatch.setattr(operators, "publish_object_store_file", fake_publish)

    facts = invoke_operator(
        "gaussian_splat_to_ksplat",
        {
            "access_plan": {
                "source": {"local_path": str(source), "format": "ksplat"},
                "target": {
                    "file_name": "model.ksplat",
                    "publish": {
                        "method": "object_store",
                        "endpoint": "minio:9000",
                        "access_key": "ak",
                        "secret_key": "sk",
                        "use_ssl": False,
                        "bucket": "manager",
                        "object": "tenant_1/gaussian-splat/model.ksplat",
                        "locator": "s3://manager/tenant_1/gaussian-splat/model.ksplat",
                    },
                },
            }
        },
    )

    assert captured["path"] == source
    assert facts["ksplat_ref"] == "tenant_1/gaussian-splat/model.ksplat"
    assert facts["ksplat_uri"] == "s3://manager/tenant_1/gaussian-splat/model.ksplat"
    assert facts["size_bytes"] == 6
    assert facts["converter"] == "copy"
    assert facts["source_format"] == "ksplat"
    assert facts["target_format"] == "ksplat"
    assert "secret_key" not in str(facts)


def test_gaussian_splat_to_ksplat_converts_ply_source(tmp_path, monkeypatch):
    source = tmp_path / "model.ply"
    source.write_bytes(b"ply")
    node = tmp_path / "node"
    node.write_text("#!/bin/sh\n", encoding="utf-8")
    node.chmod(0o755)
    captured = {}

    def fake_runner(command, timeout_seconds):
        captured["command"] = command
        target = Path(command[3])
        target.write_bytes(b"ksplat")
        return CommandResult(returncode=0, stdout="converted")

    def fake_publish(path, publish):
        captured["publish_path"] = path
        return {
            "object_uri": publish["locator"],
            "object_name": publish["object"],
            "uploaded_files": 1,
            "uploaded_bytes": path.stat().st_size,
            "content_type": publish.get("content_type") or "application/vnd.gaussian-ksplat",
        }

    monkeypatch.setattr(operators, "publish_object_store_file", fake_publish)

    facts = invoke_operator(
        "gaussian_splat_to_ksplat",
        {
            "access_plan": {
                "source": {"local_path": str(source), "format": "ply"},
                "target": {
                    "file_name": "model.ksplat",
                    "publish": {
                        "method": "object_store",
                        "endpoint": "minio:9000",
                        "access_key": "ak",
                        "secret_key": "sk",
                        "bucket": "manager",
                        "object": "model.ksplat",
                        "locator": "s3://manager/model.ksplat",
                    },
                },
            },
            "options": {
                "compression_level": 2,
                "alpha_threshold": 8,
                "spherical_harmonics_degree": 1,
                "bounds_3d": {
                    "min_x": 1,
                    "min_y": 2,
                    "min_z": 3,
                    "max_x": 4,
                    "max_y": 6,
                    "max_z": 8,
                },
            },
        },
        runner=fake_runner,
        env={"MODEL3D_GAUSSIAN_SPLAT_NODE_BIN": str(node)},
    )

    assert captured["command"] == [
        str(node),
        operators.GAUSSIAN_SPLAT_CONVERTER_SCRIPT,
        str(source),
        str(captured["publish_path"]),
        "ply",
        "2",
        "8",
        "1",
        "262144",
        "2.5,4,5.5",
        "5",
        "256",
    ]
    assert facts["ksplat_ref"] == "model.ksplat"
    assert facts["size_bytes"] == 6
    assert facts["source_format"] == "ply"
    assert facts["target_format"] == "ksplat"
    assert facts["converter"] == operators.GAUSSIAN_SPLAT_CONVERTER_SCRIPT
    assert facts["scene_center"] == [2.5, 4.0, 5.5]
    assert facts["scene_center_source"] == "bounds_3d"
    assert facts["section_size"] == 262144
    assert facts["block_size"] == 5.0
    assert facts["bucket_size"] == 256
    assert "secret_key" not in str(facts)


def test_gaussian_splat_to_ksplat_converts_splat_source(tmp_path, monkeypatch):
    source = tmp_path / "model.splat"
    source.write_bytes(b"splat")
    node = tmp_path / "node"
    node.write_text("#!/bin/sh\n", encoding="utf-8")
    node.chmod(0o755)
    captured = {}

    def fake_runner(command, timeout_seconds):
        captured["command"] = command
        Path(command[3]).write_bytes(b"ksplat")
        return CommandResult(returncode=0)

    def fake_publish(path, publish):
        return {
            "object_uri": "s3://manager/model.ksplat",
            "object_name": "model.ksplat",
            "uploaded_files": 1,
            "uploaded_bytes": path.stat().st_size,
        }

    monkeypatch.setattr(operators, "publish_object_store_file", fake_publish)

    facts = invoke_operator(
        "gaussian_splat_to_ksplat",
        {
            "access_plan": {
                "source": {"local_path": str(source), "format": "splat"},
                "target": {
                    "file_name": "model.ksplat",
                    "publish": {
                        "method": "object_store",
                        "endpoint": "minio:9000",
                        "access_key": "ak",
                        "secret_key": "sk",
                        "bucket": "manager",
                        "object": "model.ksplat",
                    },
                },
            },
            "options": {
                "sampled_bounds_3d": {
                    "min_x": -10,
                    "min_y": -20,
                    "min_z": -30,
                    "max_x": 10,
                    "max_y": 20,
                    "max_z": 30,
                },
                "block_size": 7.5,
                "bucket_size": 512,
            },
        },
        runner=fake_runner,
        env={"MODEL3D_GAUSSIAN_SPLAT_NODE_BIN": str(node)},
    )

    assert captured["command"][4] == "splat"
    assert captured["command"][8:] == ["262144", "0,0,0", "7.5", "512"]
    assert facts["source_format"] == "splat"
    assert facts["target_format"] == "ksplat"
    assert facts["scene_center_source"] == "sampled_bounds_3d"


def test_gaussian_splat_to_ksplat_runs_real_converter_when_node_modules_exist(tmp_path, monkeypatch):
    node_modules = Path(operators.__file__).resolve().parent / "node_modules" / "@mkkellogg" / "gaussian-splats-3d"
    if not node_modules.exists():
        pytest.skip("model3d workflow Node dependencies are not installed")
    node_bin = Path(sys.executable).parent / "node"
    if not node_bin.exists():
        node_path = operators.shutil.which("node")
        if not node_path:
            pytest.skip("node executable is not available")
        node_bin = Path(node_path)
    if not node_bin.is_file():
        pytest.skip("node executable is not available")

    source = tmp_path / "tiny.splat"
    source.write_bytes(_minimal_splat_bytes())
    captured = {}

    def fake_publish(path, publish):
        captured["path"] = path
        assert path.is_file()
        return {
            "object_uri": "s3://manager/tiny.ksplat",
            "object_name": "tiny.ksplat",
            "uploaded_files": 1,
            "uploaded_bytes": path.stat().st_size,
        }

    monkeypatch.setattr(operators, "publish_object_store_file", fake_publish)
    facts = invoke_operator(
        "gaussian_splat_to_ksplat",
        {
            "access_plan": {
                "source": {"local_path": str(source), "format": "splat"},
                "target": {
                    "file_name": "tiny.ksplat",
                    "publish": {
                        "method": "object_store",
                        "endpoint": "minio:9000",
                        "access_key": "ak",
                        "secret_key": "sk",
                        "bucket": "manager",
                        "object": "tiny.ksplat",
                    },
                },
            },
        },
        env={"MODEL3D_GAUSSIAN_SPLAT_NODE_BIN": str(node_bin)},
    )

    assert facts["source_format"] == "splat"
    assert facts["target_format"] == "ksplat"
    assert facts["size_bytes"] > 0
    assert captured["path"].exists() is False


def test_gaussian_splat_to_ksplat_rejects_unsupported_source(tmp_path):
    source = tmp_path / "model.xyz"
    source.write_bytes(b"xyz")

    with pytest.raises(ConverterError) as exc:
        invoke_operator(
            "gaussian_splat_to_ksplat",
            {
                "access_plan": {
                    "source": {"local_path": str(source), "format": "xyz"},
                    "target": {
                        "file_name": "model.ksplat",
                        "publish": {
                            "method": "object_store",
                            "endpoint": "minio:9000",
                            "access_key": "ak",
                            "secret_key": "sk",
                            "bucket": "manager",
                            "object": "model.ksplat",
                        },
                    },
                }
            },
        )

    assert exc.value.error_code == "UNSUPPORTED_SOURCE_FORMAT"
    assert "xyz" in (exc.value.details or "")


def _minimal_splat_bytes() -> bytes:
    record = bytearray(32)
    for offset, value in enumerate([0.0, 0.0, 0.0, 1.0, 1.0, 1.0]):
        struct.pack_into("<f", record, offset * 4, value)
    record[24:32] = bytes([255, 64, 32, 255, 255, 0, 0, 0])
    return bytes(record)


def test_osgb_scene_to_3dtiles_invokes_converter_and_returns_facts(tmp_path):
    source = tmp_path / "osgb_scene"
    target = tmp_path / "tiles"
    converter = tmp_path / "engine" / "bin" / "_3dtile"
    source.mkdir()
    (source / "metadata.xml").write_text("<ModelMetadata />", encoding="utf-8")

    seen_commands = []

    def fake_runner(command, timeout_seconds):
        seen_commands.append(command)
        target.mkdir(exist_ok=True)
        (target / "tileset.json").write_text("{}", encoding="utf-8")
        (target / "0.b3dm").write_bytes(b"tile")
        return CommandResult(returncode=0, stdout="ok")

    facts = invoke_operator(
        "osgb_scene_to_3dtiles",
        {
            "access_plan": {
                "source": {"root_uri": str(source)},
                "target": {"dataset_root_uri": str(target)},
            }
        },
        runner=fake_runner,
        env={"MODEL3D_CONVERTER_BIN": str(converter)},
    )

    assert seen_commands == [[str(converter), "-f", "osgb", "-i", str(source), "-o", str(target)]]
    assert facts["tileset_locator"] == str(target)
    assert facts["tileset_ref"] == "tileset.json"
    assert facts["tile_count"] == 1
    assert facts["converter"] == str(converter)


def test_osgb_scene_to_3dtiles_publishes_object_store_target_from_temp_workspace(tmp_path, monkeypatch):
    source = tmp_path / "osgb_scene"
    source.mkdir()
    converter = tmp_path / "engine" / "bin" / "_3dtile"
    captured = {}

    def fake_runner(command, timeout_seconds):
        target = Path(command[-1])
        captured["target"] = target
        target.mkdir(parents=True, exist_ok=True)
        (target / "tileset.json").write_text("{}", encoding="utf-8")
        (target / "Data").mkdir()
        (target / "Data" / "0.b3dm").write_bytes(b"tile")
        return CommandResult(returncode=0, stdout="converted")

    def fake_publish(root, publish):
        captured["publish_root"] = root
        captured["publish"] = publish
        assert (root / "tileset.json").exists()
        return {
            "tileset_locator": publish["locator"],
            "tileset_ref": "tileset.json",
            "target_root_uri": "s3://target-bucket/out/site",
            "uploaded_files": 2,
            "uploaded_bytes": 6,
        }

    monkeypatch.setattr(operators, "publish_object_store_directory", fake_publish)

    facts = invoke_operator(
        "osgb_scene_to_3dtiles",
        {
            "access_plan": {
                "source": {"root_uri": str(source)},
                "target": {
                    "publish": {
                        "method": "object_store",
                        "endpoint": "minio:9000",
                        "access_key": "ak",
                        "secret_key": "sk",
                        "use_ssl": False,
                        "bucket": "target-bucket",
                        "prefix": "out/site",
                        "locator": "addp://engine/31/path/target-bucket/out/site?type=directory",
                    }
                },
            }
        },
        runner=fake_runner,
        env={"MODEL3D_CONVERTER_BIN": str(converter)},
    )

    assert facts["tileset_locator"] == "addp://engine/31/path/target-bucket/out/site?type=directory"
    assert facts["target_root_uri"] == "s3://target-bucket/out/site"
    assert facts["publish"]["uploaded_files"] == 2
    assert captured["target"] == captured["publish_root"]
    assert not captured["target"].exists()
    assert "secret_key" not in str(facts)
    assert "'sk'" not in str(facts)


def test_osgb_scene_to_3dtiles_stages_object_store_source_before_conversion(tmp_path, monkeypatch):
    target = tmp_path / "tiles"
    converter = tmp_path / "engine" / "bin" / "_3dtile"
    captured = {}

    def fake_stage(root, stage):
        captured["stage_root"] = root
        captured["stage"] = stage
        root.mkdir(parents=True, exist_ok=True)
        (root / "metadata.xml").write_text("<ModelMetadata />", encoding="utf-8")
        (root / "Data").mkdir()
        (root / "Data" / "tile.osgb").write_bytes(b"osgb")
        return {"method": "object_store", "bucket": stage["bucket"], "prefix": stage["prefix"], "downloaded_files": 2, "downloaded_bytes": 16}

    def fake_runner(command, timeout_seconds):
        captured["command"] = command
        assert Path(command[4]) == captured["stage_root"]
        target.mkdir(parents=True, exist_ok=True)
        (target / "tileset.json").write_text("{}", encoding="utf-8")
        return CommandResult(returncode=0)

    monkeypatch.setattr(operators, "stage_object_store_directory", fake_stage)

    facts = invoke_operator(
        "osgb_scene_to_3dtiles",
        {
            "access_plan": {
                "source": {
                    "stage": {
                        "method": "object_store",
                        "endpoint": "minio:9000",
                        "access_key": "ak",
                        "secret_key": "sk",
                        "use_ssl": False,
                        "bucket": "source-bucket",
                        "prefix": "models/site",
                    }
                },
                "target": {"dataset_root_uri": str(target)},
            }
        },
        runner=fake_runner,
        env={"MODEL3D_CONVERTER_BIN": str(converter)},
    )

    assert facts["stage"]["downloaded_files"] == 2
    assert facts["tileset_ref"] == "tileset.json"
    assert not captured["stage_root"].exists()
    assert "secret_key" not in str(facts)
    assert "'sk'" not in str(facts)


def test_stage_object_store_directory_downloads_prefix(tmp_path, monkeypatch):
    calls = []

    class FakeObject:
        def __init__(self, object_name, size=0, is_dir=False):
            self.object_name = object_name
            self.size = size
            self.is_dir = is_dir

    class FakeMinio:
        def __init__(self, endpoint, access_key, secret_key, secure):
            calls.append(("init", endpoint, access_key, secret_key, secure))

        def list_objects(self, bucket, prefix, recursive):
            calls.append(("list", bucket, prefix, recursive))
            return [
                FakeObject("models/site/metadata.xml", 8),
                FakeObject("models/site/Data/tile.osgb", 4),
            ]

        def fget_object(self, bucket, object_name, file_path):
            calls.append(("get", bucket, object_name, Path(file_path).relative_to(tmp_path).as_posix()))
            Path(file_path).parent.mkdir(parents=True, exist_ok=True)
            Path(file_path).write_bytes(b"x")

    monkeypatch.setattr(operators, "Minio", FakeMinio)

    result = operators.stage_object_store_directory(
        tmp_path / "stage",
        {
            "endpoint": "https://minio.example.com",
            "access_key": "ak",
            "secret_key": "sk",
            "use_ssl": True,
            "bucket": "source-bucket",
            "prefix": "models/site",
        },
    )

    assert (tmp_path / "stage" / "metadata.xml").exists()
    assert (tmp_path / "stage" / "Data" / "tile.osgb").exists()
    assert result["downloaded_files"] == 2
    assert result["downloaded_bytes"] == 12
    assert ("list", "source-bucket", "models/site", True) in calls


def test_publish_object_store_directory_uploads_tileset_last(tmp_path, monkeypatch):
    root = tmp_path / "tiles"
    (root / "Data").mkdir(parents=True)
    (root / "Data" / "0.b3dm").write_bytes(b"tile")
    (root / "tileset.json").write_text("{}", encoding="utf-8")
    calls = []

    class FakeMinio:
        def __init__(self, endpoint, access_key, secret_key, secure):
            calls.append(("init", endpoint, access_key, secret_key, secure))

        def bucket_exists(self, bucket):
            calls.append(("bucket_exists", bucket))
            return False

        def make_bucket(self, bucket):
            calls.append(("make_bucket", bucket))

        def fput_object(self, bucket, object_name, file_path, content_type):
            calls.append(("put", bucket, object_name, Path(file_path).name, content_type))

    monkeypatch.setattr(operators, "Minio", FakeMinio)

    result = operators.publish_object_store_directory(
        root,
        {
            "endpoint": "http://minio:9000",
            "access_key": "ak",
            "secret_key": "sk",
            "use_ssl": False,
            "bucket": "target-bucket",
            "prefix": "out/site",
            "locator": "addp://engine/31/path/target-bucket/out/site?type=directory",
        },
    )

    puts = [call for call in calls if call[0] == "put"]
    assert puts[-1][2] == "out/site/tileset.json"
    assert puts[-1][4] == "application/vnd.ogc.3dtiles+json"
    assert result["uploaded_files"] == 2
    assert result["uploaded_bytes"] == 6
    assert result["tileset_object"] == "out/site/tileset.json"


def test_osgb_to_glb_invokes_converter_and_returns_facts(tmp_path, monkeypatch):
    source = tmp_path / "tile.osgb"
    converter = tmp_path / "engine" / "bin" / "_3dtile"
    source.write_bytes(b"osgb")
    captured = {}

    def fake_runner(command, timeout_seconds):
        target = Path(command[-1])
        captured["target"] = target
        target.write_bytes(b"glb")
        return CommandResult(returncode=0, stderr="")

    def fake_publish(path, publish):
        captured["publish_path"] = path
        captured["publish"] = publish
        assert path.name == "preview.glb"
        assert path.read_bytes() == b"glb"
        return {
            "object_uri": "s3://manager/model3d/preview.glb",
            "object_name": "model3d/preview.glb",
            "uploaded_bytes": path.stat().st_size,
        }

    monkeypatch.setattr(operators, "publish_object_store_file", fake_publish)
    facts = invoke_operator(
        "osgb_to_glb",
        {
            "access_plan": {
                "source": {"local_path": str(source)},
                "target": {
                    "file_name": "preview.glb",
                    "publish": {
                        "method": "object_store",
                        "endpoint": "minio:9000",
                        "access_key": "ak",
                        "secret_key": "sk",
                        "use_ssl": False,
                        "bucket": "manager",
                        "object": "model3d/preview.glb",
                        "locator": "s3://manager/model3d/preview.glb",
                    },
                },
            }
        },
        runner=fake_runner,
        env={"MODEL3D_CONVERTER_BIN": str(converter)},
    )

    assert facts["glb_uri"] == "s3://manager/model3d/preview.glb"
    assert facts["glb_ref"] == "model3d/preview.glb"
    assert facts["size_bytes"] == 3
    assert facts["command"] == [str(converter), "-f", "gltf", "-i", str(source), "-o", str(captured["target"])]
    assert captured["publish"]["bucket"] == "manager"
    assert captured["publish_path"].exists() is False


def test_gltf_to_glb_invokes_converter_and_returns_facts(tmp_path, monkeypatch):
    source = tmp_path / "scene.gltf"
    converter = tmp_path / "engine" / "bin" / "assimp"
    source.write_text('{"asset":{"version":"2.0"}}', encoding="utf-8")
    captured = {}

    def fake_runner(command, timeout_seconds):
        target = Path(command[-2])
        captured["target"] = target
        target.write_bytes(b"glb")
        return CommandResult(returncode=0, stderr="")

    def fake_publish(path, publish):
        captured["publish_path"] = path
        assert path.name == "scene.glb"
        return {
            "object_uri": "s3://manager/model3d/scene.glb",
            "object_name": "model3d/scene.glb",
            "uploaded_bytes": path.stat().st_size,
        }

    monkeypatch.setattr(operators, "publish_object_store_file", fake_publish)
    facts = invoke_operator(
        "gltf_to_glb",
        {
            "access_plan": {
                "source": {"local_path": str(source)},
                "target": {
                    "file_name": "scene.glb",
                    "publish": {
                        "method": "object_store",
                        "endpoint": "minio:9000",
                        "access_key": "ak",
                        "secret_key": "sk",
                        "use_ssl": False,
                        "bucket": "manager",
                        "object": "model3d/scene.glb",
                    },
                },
            }
        },
        runner=fake_runner,
        env={"MODEL3D_MESH_CONVERTER_BIN": str(converter)},
    )

    assert facts["glb_uri"] == "s3://manager/model3d/scene.glb"
    assert facts["glb_ref"] == "model3d/scene.glb"
    assert facts["source_format"] == "gltf"
    assert facts["command"] == [str(converter), "export", str(source), str(captured["target"]), "-embtex"]
    assert captured["publish_path"].exists() is False


def test_fbx_to_glb_invokes_converter_and_returns_facts(tmp_path, monkeypatch):
    source = tmp_path / "mesh.fbx"
    converter = tmp_path / "engine" / "bin" / "assimp"
    source.write_text("mesh", encoding="utf-8")
    captured = {}

    def fake_runner(command, timeout_seconds):
        target = Path(command[-2])
        captured["target"] = target
        target.write_bytes(_glb_bytes({"asset": {"version": "2.0"}}))
        return CommandResult(returncode=0)

    def fake_publish(path, publish):
        captured["publish_path"] = path
        return {
            "object_uri": "s3://manager/model3d/fbx.glb",
            "object_name": "model3d/fbx.glb",
            "uploaded_bytes": path.stat().st_size,
        }

    monkeypatch.setattr(operators, "publish_object_store_file", fake_publish)
    facts = invoke_operator(
        "fbx_to_glb",
        {
            "access_plan": {
                "source": {"local_path": str(source)},
                "target": {
                    "file_name": "fbx.glb",
                    "publish": {
                        "method": "object_store",
                        "endpoint": "minio:9000",
                        "access_key": "ak",
                        "secret_key": "sk",
                        "bucket": "manager",
                        "object": "model3d/fbx.glb",
                    },
                },
            }
        },
        runner=fake_runner,
        env={"MODEL3D_MESH_CONVERTER_BIN": str(converter)},
    )

    assert facts["source_format"] == "fbx"
    assert facts["glb_ref"] == "model3d/fbx.glb"
    assert facts["command"] == [str(converter), "export", str(source), str(captured["target"]), "-embtex"]
    assert captured["publish_path"].exists() is False


def test_obj_to_glb_invokes_converter_and_returns_facts(tmp_path, monkeypatch):
    source = tmp_path / "mesh.obj"
    converter = tmp_path / "engine" / "bin" / "assimp"
    source.write_text("v 0 0 0\n", encoding="utf-8")
    captured = {}

    def fake_runner(command, timeout_seconds):
        target = Path(command[-2])
        captured["target"] = target
        target.write_bytes(_glb_bytes({"asset": {"version": "2.0"}}))
        return CommandResult(returncode=0)

    def fake_publish(path, publish):
        captured["publish_path"] = path
        return {
            "object_uri": "s3://manager/model3d/obj.glb",
            "object_name": "model3d/obj.glb",
            "uploaded_bytes": path.stat().st_size,
        }

    monkeypatch.setattr(operators, "publish_object_store_file", fake_publish)
    facts = invoke_operator(
        "obj_to_glb",
        {
            "access_plan": {
                "source": {"local_path": str(source)},
                "target": {
                    "file_name": "obj.glb",
                    "publish": {
                        "method": "object_store",
                        "endpoint": "minio:9000",
                        "access_key": "ak",
                        "secret_key": "sk",
                        "bucket": "manager",
                        "object": "model3d/obj.glb",
                    },
                },
            }
        },
        runner=fake_runner,
        env={"MODEL3D_MESH_CONVERTER_BIN": str(converter)},
    )

    assert facts["source_format"] == "obj"
    assert facts["glb_ref"] == "model3d/obj.glb"
    assert facts["command"] == [str(converter), "export", str(source), str(captured["target"]), "-embtex"]
    assert captured["publish_path"].exists() is False


def test_stl_to_glb_invokes_converter_and_returns_facts(tmp_path, monkeypatch):
    source = tmp_path / "mesh.stl"
    converter = tmp_path / "engine" / "bin" / "assimp"
    source.write_text("solid mesh\nendsolid mesh\n", encoding="utf-8")
    captured = {}

    def fake_runner(command, timeout_seconds):
        target = Path(command[-2])
        captured["target"] = target
        target.write_bytes(_glb_bytes({"asset": {"version": "2.0"}}))
        return CommandResult(returncode=0)

    def fake_publish(path, publish):
        captured["publish_path"] = path
        return {
            "object_uri": "s3://manager/model3d/stl.glb",
            "object_name": "model3d/stl.glb",
            "uploaded_bytes": path.stat().st_size,
        }

    monkeypatch.setattr(operators, "publish_object_store_file", fake_publish)
    facts = invoke_operator(
        "stl_to_glb",
        {
            "access_plan": {
                "source": {"local_path": str(source)},
                "target": {
                    "file_name": "stl.glb",
                    "publish": {
                        "method": "object_store",
                        "endpoint": "minio:9000",
                        "access_key": "ak",
                        "secret_key": "sk",
                        "bucket": "manager",
                        "object": "model3d/stl.glb",
                    },
                },
            }
        },
        runner=fake_runner,
        env={"MODEL3D_MESH_CONVERTER_BIN": str(converter)},
    )

    assert facts["source_format"] == "stl"
    assert facts["glb_ref"] == "model3d/stl.glb"
    assert facts["command"] == [str(converter), "export", str(source), str(captured["target"]), "-embtex"]
    assert captured["publish_path"].exists() is False


def test_ifc_to_glb_invokes_ifcconvert_and_returns_facts(tmp_path, monkeypatch):
    source = tmp_path / "building.ifc"
    converter = tmp_path / "engine" / "bin" / "IfcConvert"
    source.write_text("ISO-10303-21;\nHEADER;\nFILE_SCHEMA(('IFC4'));\nENDSEC;\nDATA;\nENDSEC;\nEND-ISO-10303-21;\n", encoding="utf-8")
    converter.parent.mkdir(parents=True)
    converter.write_text("#!/bin/sh\n", encoding="utf-8")
    converter.chmod(0o755)
    captured = {}

    def fake_runner(command, timeout_seconds):
        target = Path(command[-1])
        captured["target"] = target
        target.write_bytes(_glb_bytes({"asset": {"version": "2.0"}}))
        return CommandResult(returncode=0, stdout="converted", stderr="material warnings")

    def fake_publish(path, publish):
        captured["publish_path"] = path
        return {
            "object_uri": "s3://manager/model3d/ifc.glb",
            "object_name": "model3d/ifc.glb",
            "uploaded_bytes": path.stat().st_size,
        }

    monkeypatch.setattr(operators, "publish_object_store_file", fake_publish)
    facts = invoke_operator(
        "ifc_to_glb",
        {
            "access_plan": {
                "source": {"local_path": str(source)},
                "target": {
                    "file_name": "ifc.glb",
                    "publish": {
                        "method": "object_store",
                        "endpoint": "minio:9000",
                        "access_key": "ak",
                        "secret_key": "sk",
                        "bucket": "manager",
                        "object": "model3d/ifc.glb",
                    },
                },
            }
        },
        runner=fake_runner,
        env={"MODEL3D_IFC_CONVERTER_BIN": str(converter)},
    )

    assert facts["source_format"] == "ifc"
    assert facts["glb_ref"] == "model3d/ifc.glb"
    assert facts["command"] == [str(converter), "--center-model", str(source), str(captured["target"])]
    assert facts["stdout"] == "converted"
    assert facts["stderr"] == "material warnings"
    assert captured["publish_path"].exists() is False


def test_obj_to_glb_repairs_assimp_transparent_textured_materials(tmp_path, monkeypatch):
    source = tmp_path / "mesh.obj"
    converter = tmp_path / "engine" / "bin" / "assimp"
    source.write_text("mtllib mesh.mtl\nv 0 0 0\n", encoding="utf-8")
    (tmp_path / "mesh.mtl").write_text("newmtl material_0\nTr 1.000000\nmap_Kd texture.jpg\n", encoding="utf-8")
    captured = {}

    def fake_runner(command, timeout_seconds):
        target = Path(command[-2])
        captured["target"] = target
        target.write_bytes(
            _glb_bytes(
                {
                    "asset": {"version": "2.0"},
                    "materials": [
                        {
                            "name": "material_0_material",
                            "pbrMetallicRoughness": {
                                "baseColorTexture": {"index": 0},
                                "baseColorFactor": [1.0, 1.0, 1.0, 0.0],
                            },
                            "alphaMode": "BLEND",
                            "extensions": {
                                "KHR_materials_pbrSpecularGlossiness": {
                                    "diffuseTexture": {"index": 0},
                                    "diffuseFactor": [1.0, 1.0, 1.0, 0.0],
                                }
                            },
                        }
                    ],
                    "textures": [{"source": 0}],
                    "images": [{"bufferView": 0, "mimeType": "image/jpeg"}],
                    "bufferViews": [{"buffer": 0, "byteOffset": 0, "byteLength": 3}],
                    "buffers": [{"byteLength": 3}],
                },
                b"jpg",
            )
        )
        return CommandResult(returncode=0)

    def fake_publish(path, publish):
        captured["published_doc"] = _glb_json(path.read_bytes())
        return {
            "object_uri": "s3://manager/model3d/obj.glb",
            "object_name": "model3d/obj.glb",
            "uploaded_bytes": path.stat().st_size,
        }

    monkeypatch.setattr(operators, "publish_object_store_file", fake_publish)
    facts = invoke_operator(
        "obj_to_glb",
        {
            "access_plan": {
                "source": {"local_path": str(source)},
                "target": {
                    "file_name": "obj.glb",
                    "publish": {
                        "method": "object_store",
                        "endpoint": "minio:9000",
                        "access_key": "ak",
                        "secret_key": "sk",
                        "bucket": "manager",
                        "object": "model3d/obj.glb",
                    },
                },
            }
        },
        runner=fake_runner,
        env={"MODEL3D_MESH_CONVERTER_BIN": str(converter)},
    )

    material = captured["published_doc"]["materials"][0]
    assert material["pbrMetallicRoughness"]["baseColorFactor"][3] == 1.0
    assert material["extensions"]["KHR_materials_pbrSpecularGlossiness"]["diffuseFactor"][3] == 1.0
    assert "alphaMode" not in material
    assert facts["postprocess"] == {
        "obj_textured_material_alpha": "normalized_to_opaque",
        "material_count": 1,
    }


def test_obj_to_glb_rejects_missing_material_library(tmp_path, monkeypatch):
    source = tmp_path / "mesh.obj"
    converter = tmp_path / "engine" / "bin" / "assimp"
    source.write_text("mtllib missing.mtl\nv 0 0 0\n", encoding="utf-8")

    def fake_runner(command, timeout_seconds):
        raise AssertionError("converter must not run when OBJ material library is missing")

    with pytest.raises(ConverterError) as exc_info:
        invoke_operator(
            "obj_to_glb",
            {
                "access_plan": {
                    "source": {"local_path": str(source)},
                    "target": {
                        "file_name": "obj.glb",
                        "publish": {
                            "method": "object_store",
                            "endpoint": "minio:9000",
                            "access_key": "ak",
                            "secret_key": "sk",
                            "bucket": "manager",
                            "object": "model3d/obj.glb",
                        },
                    },
                }
            },
            runner=fake_runner,
            env={"MODEL3D_MESH_CONVERTER_BIN": str(converter)},
        )

    assert exc_info.value.error_code == "MISSING_OBJ_MATERIAL_LIBRARY"
    assert exc_info.value.details == "missing.mtl"


def test_fbx_to_glb_rejects_directory_output(tmp_path, monkeypatch):
    source = tmp_path / "mesh.fbx"
    converter = tmp_path / "engine" / "bin" / "assimp"
    source.write_text("mesh", encoding="utf-8")

    def fake_runner(command, timeout_seconds):
        Path(command[-2]).mkdir()
        return CommandResult(returncode=0)

    def fake_publish(path, publish):
        raise AssertionError("directory output must not be published as GLB")

    monkeypatch.setattr(operators, "publish_object_store_file", fake_publish)

    with pytest.raises(ConverterError) as exc_info:
        invoke_operator(
            "fbx_to_glb",
            {
                "access_plan": {
                    "source": {"local_path": str(source)},
                    "target": {
                        "file_name": "fbx.glb",
                        "publish": {
                            "method": "object_store",
                            "endpoint": "minio:9000",
                            "access_key": "ak",
                            "secret_key": "sk",
                            "bucket": "manager",
                            "object": "model3d/fbx.glb",
                        },
                    },
                }
            },
            runner=fake_runner,
            env={"MODEL3D_MESH_CONVERTER_BIN": str(converter)},
        )

    assert exc_info.value.error_code == "OUTPUT_NOT_FOUND"


def test_publish_object_store_file_uploads_glb_last_fact(tmp_path, monkeypatch):
    glb = tmp_path / "preview.glb"
    converter = tmp_path / "engine" / "bin" / "_3dtile"
    glb.write_bytes(b"glb")
    calls = []

    class FakeMinio:
        def __init__(self, endpoint, access_key, secret_key, secure):
            calls.append(("init", endpoint, access_key, secret_key, secure))

        def bucket_exists(self, bucket):
            calls.append(("bucket_exists", bucket))
            return False

        def make_bucket(self, bucket):
            calls.append(("make_bucket", bucket))

        def fput_object(self, bucket, object_name, file_path, content_type=None):
            calls.append(("put", bucket, object_name, Path(file_path).read_bytes(), content_type))

    monkeypatch.setattr(operators, "Minio", FakeMinio)
    facts = invoke_operator(
        "osgb_to_glb",
        {
            "access_plan": {
                "source": {"local_path": str(glb)},
                "target": {
                    "file_name": "preview.glb",
                    "publish": {
                        "method": "object_store",
                        "endpoint": "http://minio:9000",
                        "access_key": "ak",
                        "secret_key": "sk",
                        "use_ssl": False,
                        "bucket": "manager",
                        "object": "model3d/preview.glb",
                    },
                },
            }
        },
        runner=lambda command, timeout: (Path(command[-1]).write_bytes(b"glb"), CommandResult(0))[1],
        env={"MODEL3D_CONVERTER_BIN": str(converter)},
    )

    assert calls[0] == ("init", "minio:9000", "ak", "sk", False)
    assert ("make_bucket", "manager") in calls
    assert calls[-1] == ("put", "manager", "model3d/preview.glb", b"glb", "model/gltf-binary")
    assert facts["glb_uri"] == "s3://manager/model3d/preview.glb"
    assert facts["glb_ref"] == "model3d/preview.glb"
    assert facts["size_bytes"] == 3


def test_converter_unavailable_is_explicit(tmp_path):
    with pytest.raises(ConverterError) as exc_info:
        invoke_operator(
            "osgb_scene_to_3dtiles",
            {
                "access_plan": {
                    "source": {"root_uri": str(tmp_path / "source")},
                    "target": {"dataset_root_uri": str(tmp_path / "target")},
                }
            },
            env={"MODEL3D_CONVERTER_BIN": str(tmp_path / "missing_converter")},
        )

    assert exc_info.value.error_code == "CONVERTER_UNAVAILABLE"
    assert exc_info.value.http_status == 503


def test_missing_access_plan_is_invalid():
    with pytest.raises(ConverterError) as exc_info:
        invoke_operator("osgb_scene_to_3dtiles", {}, runner=lambda command, timeout: CommandResult(0))

    assert exc_info.value.error_code == "INVALID_PARAMS"


def _glb_bytes(doc, bin_chunk=b""):
    json_bytes = json.dumps(doc, separators=(",", ":")).encode("utf-8")
    json_bytes += b" " * ((4 - len(json_bytes) % 4) % 4)
    chunks = [(b"JSON", json_bytes)]
    if bin_chunk:
        bin_chunk += b"\x00" * ((4 - len(bin_chunk) % 4) % 4)
        chunks.append((b"BIN\x00", bin_chunk))
    total_length = 12 + sum(8 + len(chunk) for _, chunk in chunks)
    output = bytearray(struct.pack("<4sII", b"glTF", 2, total_length))
    for chunk_type, chunk in chunks:
        output += struct.pack("<I4s", len(chunk), chunk_type)
        output += chunk
    return bytes(output)


def _glb_json(data):
    chunk_length, chunk_type = struct.unpack_from("<I4s", data, 12)
    assert chunk_type == b"JSON"
    return json.loads(data[20 : 20 + chunk_length].decode("utf-8").strip())
