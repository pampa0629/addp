import sys
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
    assert [operator["name"] for operator in operators] == ["osgb_to_glb", "osgb_scene_to_3dtiles"]
    assert_operator_metadata_contract(operators, expected_engine_type="model3d_workflow")


def test_converter_status_defaults_to_engine_bound_binary():
    status = converter_status(env={})

    assert status["binding"] == "model3d_workflow"
    assert status["env"] == "MODEL3D_CONVERTER_BIN"
    assert status["path"].endswith("engines/model3d-workflow/bin/_3dtile")


def test_converter_status_rejects_path_command_name():
    status = converter_status(env={"MODEL3D_CONVERTER_BIN": "_3dtile", "PATH": "/usr/bin"})

    assert status["available"] is False
    assert "not a PATH command name" in status["details"]


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
