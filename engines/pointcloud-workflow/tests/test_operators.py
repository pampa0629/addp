import sys
import json
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


def test_operator_metadata_contract():
    ops = list_operators()
    assert [operator["name"] for operator in ops] == [
        "las_to_copc",
        "laz_to_copc",
        "e57_to_copc",
        "pcd_to_copc",
        "xyz_to_copc",
    ]
    assert_operator_metadata_contract(ops, expected_engine_type="pointcloud_workflow")


def test_converter_status_defaults_to_engine_bound_binary():
    status = converter_status(env={})

    assert status["binding"] == "pointcloud_workflow"
    assert status["env"] == "POINTCLOUD_PDAL_BIN"
    assert status["path"].endswith("engines/pointcloud-workflow/bin/pdal")


def test_converter_status_rejects_path_command_name():
    status = converter_status(env={"POINTCLOUD_PDAL_BIN": "pdal", "PATH": "/usr/bin"})

    assert status["available"] is False
    assert "not a PATH command name" in status["details"]


def test_publish_endpoint_rewrites_localhost_only_when_configured():
    assert operators._publish_endpoint(
        "localhost:19000",
        env={"POINTCLOUD_OBJECT_STORE_LOCALHOST_ENDPOINT": "host.docker.internal:19000"},
    ) == "host.docker.internal:19000"
    assert operators._publish_endpoint(
        "http://127.0.0.1:19000",
        env={"POINTCLOUD_OBJECT_STORE_LOCALHOST_ENDPOINT": "http://host.docker.internal:19000"},
    ) == "host.docker.internal:19000"
    assert operators._publish_endpoint(
        "minio:9000",
        env={"POINTCLOUD_OBJECT_STORE_LOCALHOST_ENDPOINT": "host.docker.internal:19000"},
    ) == "minio:9000"


def test_runtime_env_sets_temp_dir(tmp_path):
    runtime_env = operators._runtime_env(
        {
            "source": {"root_uri": "/data/source.las", "env": {}},
            "target": {"file_name": "source.copc.laz", "publish": {"method": "object_store"}},
        },
        tmp_path,
        {"POINTCLOUD_OBJECT_STORE_LOCALHOST_ENDPOINT": "host.docker.internal:19000"},
    )

    assert runtime_env["CPL_TMPDIR"] == str(tmp_path)
    assert runtime_env["TMPDIR"] == str(tmp_path)


def test_default_copc_threads_is_bounded():
    assert operators._default_copc_threads(env={}) == 4
    assert operators._default_copc_threads(env={"POINTCLOUD_COPC_THREADS": "12"}) == 8
    assert operators._default_copc_threads(env={"POINTCLOUD_COPC_THREADS": "0"}) == 1
    assert operators._default_copc_threads(env={"POINTCLOUD_COPC_THREADS": "bad"}) == 4


def test_publish_object_store_file_uses_rewritten_localhost_endpoint(tmp_path, monkeypatch):
    path = tmp_path / "sample.copc.laz"
    path.write_bytes(b"copc")
    captured = {}

    class FakeMinio:
        def __init__(self, endpoint, *, access_key, secret_key, secure):
            captured["endpoint"] = endpoint
            captured["access_key"] = access_key
            captured["secret_key"] = secret_key
            captured["secure"] = secure

        def bucket_exists(self, bucket):
            captured["bucket_exists"] = bucket
            return True

        def make_bucket(self, bucket):
            captured["make_bucket"] = bucket

        def fput_object(self, bucket, object_name, file_path, *, content_type):
            captured["put"] = (bucket, object_name, file_path, content_type)

    monkeypatch.setenv("POINTCLOUD_OBJECT_STORE_LOCALHOST_ENDPOINT", "host.docker.internal:19000")
    monkeypatch.setattr(operators, "Minio", FakeMinio)

    result = operators.publish_object_store_file(
        path,
        {
            "endpoint": "localhost:19000",
            "bucket": "manager",
            "object": "tenant_1/point-cloud-copc/sample.copc.laz",
            "access_key": "ak",
            "secret_key": "sk",
            "use_ssl": False,
        },
    )

    assert captured["endpoint"] == "host.docker.internal:19000"
    assert captured["secure"] is False
    assert captured["put"] == (
        "manager",
        "tenant_1/point-cloud-copc/sample.copc.laz",
        str(path),
        operators.COPC_CONTENT_TYPE,
    )
    assert result["uploaded_bytes"] == 4


def test_redact_command_hides_vsicurl_query():
    redacted = operators._redact_command([
        "/opt/conda/bin/pdal",
        "translate",
        "/vsicurl/http://minio:9000/bucket/source.las?X-Amz-Credential=secret&X-Amz-Signature=abc",
        "/vsis3/manager/source.copc.laz",
    ])

    assert redacted[2] == "/vsicurl/http://minio:9000/bucket/source.las?<redacted>"
    assert "Signature" not in redacted[2]


@pytest.mark.parametrize(
    ("operator_name", "source_suffix", "source_format"),
    [
        ("las_to_copc", ".las", "las"),
        ("laz_to_copc", ".laz", "laz"),
        ("e57_to_copc", ".e57", "e57"),
        ("pcd_to_copc", ".pcd", "pcd"),
        ("xyz_to_copc", ".xyz", "xyz"),
    ],
)
def test_point_cloud_to_copc_invokes_pdal_with_work_dir_and_publishes(tmp_path, monkeypatch, operator_name, source_suffix, source_format):
    source = tmp_path / f"source{source_suffix}"
    source.write_bytes(b"pointcloud")
    pdal = tmp_path / "pdal"
    pdal.write_text("#!/bin/sh\n", encoding="utf-8")
    pdal.chmod(0o755)
    captured = {}

    def fake_runner(command, timeout_seconds):
        captured["command"] = command
        Path(command[3]).write_bytes(b"copc")
        return CommandResult(returncode=0, stdout="translated")

    def fake_publish(path, publish):
        captured["publish_path"] = path
        captured["publish"] = publish
        return {
            "object_uri": publish["locator"],
            "object_name": publish["object"],
            "uploaded_files": 1,
            "uploaded_bytes": path.stat().st_size,
            "content_type": publish.get("content_type") or operators.COPC_CONTENT_TYPE,
        }

    monkeypatch.setattr(operators, "publish_object_store_file", fake_publish)

    facts = invoke_operator(
        operator_name,
        {
            "access_plan": {
                "source": {"root_uri": str(source), "format": source_format},
                "target": {
                    "file_name": "source.copc.laz",
                    "publish": {
                        "method": "object_store",
                        "endpoint": "minio:9000",
                        "access_key": "ak",
                        "secret_key": "sk",
                        "bucket": "manager",
                        "object": "tenant_1/point-cloud-copc/source.copc.laz",
                        "locator": "s3://manager/tenant_1/point-cloud-copc/source.copc.laz",
                        "content_type": operators.COPC_CONTENT_TYPE,
                    },
                },
            },
            "options": {"scale_x": 0.01, "scale_y": 0.01, "threads": 2},
        },
        runner=fake_runner,
        env={"POINTCLOUD_PDAL_BIN": str(pdal)},
    )

    assert captured["command"] == [
        str(pdal),
        "translate",
        str(captured.get("expected_source_path", source)),
        str(captured["publish_path"]),
        *(
            ["--reader", "readers.text", "--readers.text.header=X Y Z", "--readers.text.skip=0"]
            if source_format == "xyz"
            else []
        ),
        "--writers.copc.forward=all",
        "--writers.copc.scale_x=0.01",
        "--writers.copc.scale_y=0.01",
        "--writers.copc.threads=2",
    ]
    assert facts["copc_ref"] == "tenant_1/point-cloud-copc/source.copc.laz"
    assert facts["copc_uri"] == "s3://manager/tenant_1/point-cloud-copc/source.copc.laz"
    assert facts["size_bytes"] == 4
    assert facts["source_format"] == source_format
    assert facts["target_format"] == "copc"
    assert facts["converter"] == str(pdal)
    assert "secret_key" not in str(facts)


def test_point_cloud_to_copc_posts_progress_events(tmp_path, monkeypatch):
    source = tmp_path / "source.las"
    source.write_bytes(b"pointcloud" * 100)
    pdal = tmp_path / "pdal"
    pdal.write_text("#!/bin/sh\n", encoding="utf-8")
    pdal.chmod(0o755)
    posted = []

    class FakeHTTPResponse:
        def __enter__(self):
            return self

        def __exit__(self, exc_type, exc, tb):
            return False

        def read(self):
            return b""

    def fake_urlopen(req, timeout):
        posted.append(
            {
                "url": req.full_url,
                "headers": dict(req.header_items()),
                "body": json.loads(req.data.decode("utf-8")),
                "timeout": timeout,
            }
        )
        return FakeHTTPResponse()

    def fake_runner(command, timeout_seconds):
        Path(command[3]).write_bytes(b"copc")
        return CommandResult(returncode=0, stdout="translated")

    def fake_publish(path, publish):
        return {
            "object_uri": publish["locator"],
            "object_name": publish["object"],
            "uploaded_files": 1,
            "uploaded_bytes": path.stat().st_size,
            "content_type": operators.COPC_CONTENT_TYPE,
        }

    monkeypatch.setattr(operators.urlrequest, "urlopen", fake_urlopen)
    monkeypatch.setattr(operators, "publish_object_store_file", fake_publish)

    invoke_operator(
        "las_to_copc",
        {
            "access_plan": {
                "source": {
                    "root_uri": str(source),
                    "format": "las",
                    "metadata": {"source_size_bytes": source.stat().st_size},
                },
                "target": {
                    "file_name": "source.copc.laz",
                    "publish": {
                        "method": "object_store",
                        "endpoint": "minio:9000",
                        "access_key": "ak",
                        "secret_key": "sk",
                        "bucket": "manager",
                        "object": "tenant_1/point-cloud-copc/source.copc.laz",
                        "locator": "s3://manager/tenant_1/point-cloud-copc/source.copc.laz",
                    },
                },
                "progress_callback": {
                    "endpoint": "http://manager/api/v1/manager/internal/executions/exec-1/events",
                    "tenant_id": 7,
                    "execution_id": "exec-1",
                    "internal_api_key": "secret",
                },
            },
        },
        runner=fake_runner,
        env={"POINTCLOUD_PDAL_BIN": str(pdal), "POINTCLOUD_COPC_THREADS": "2"},
    )

    events = [(item["body"]["phase"], item["body"]["event"]) for item in posted]
    assert events == [
        ("prepare", "started"),
        ("convert", "started"),
        ("publish", "started"),
        ("publish", "completed"),
    ]
    assert posted[0]["headers"]["X-internal-api-key"] == "secret"
    assert posted[0]["headers"]["X-tenant-id"] == "7"
    assert posted[-1]["body"]["overall_progress"] == 95


def test_point_cloud_to_copc_normalizes_legacy_pcd_header_for_pdal(tmp_path, monkeypatch):
    source = tmp_path / "legacy.pcd"
    source.write_text(
        "\n".join(
            [
                "# .PCD v.5 - Point Cloud Data file format",
                "VERSION .5",
                "FIELDS x y z",
                "SIZE 4 4 4",
                "TYPE F F F",
                "COUNT 1 1 1",
                "WIDTH 1",
                "HEIGHT 1",
                "POINTS 1",
                "DATA ascii",
                "0 0 0",
            ]
        ),
        encoding="utf-8",
    )
    pdal = tmp_path / "pdal"
    pdal.write_text("#!/bin/sh\n", encoding="utf-8")
    pdal.chmod(0o755)
    captured = {}

    def fake_runner(command, timeout_seconds):
        captured["source_path"] = Path(command[2])
        captured["source_text"] = captured["source_path"].read_text(encoding="utf-8")
        Path(command[3]).write_bytes(b"copc")
        return CommandResult(returncode=0, stdout="translated")

    def fake_publish(path, publish):
        return {
            "object_uri": publish["locator"],
            "object_name": publish["object"],
            "uploaded_files": 1,
            "uploaded_bytes": path.stat().st_size,
            "content_type": operators.COPC_CONTENT_TYPE,
        }

    monkeypatch.setattr(operators, "publish_object_store_file", fake_publish)

    facts = invoke_operator(
        "pcd_to_copc",
        {
            "access_plan": {
                "source": {"root_uri": str(source), "format": "pcd"},
                "target": {
                    "file_name": "legacy.copc.laz",
                    "publish": {
                        "method": "object_store",
                        "endpoint": "minio:9000",
                        "access_key": "ak",
                        "secret_key": "sk",
                        "bucket": "manager",
                        "object": "tenant_1/point-cloud-copc/legacy.copc.laz",
                        "locator": "s3://manager/tenant_1/point-cloud-copc/legacy.copc.laz",
                    },
                },
            },
        },
        runner=fake_runner,
        env={"POINTCLOUD_PDAL_BIN": str(pdal)},
    )

    assert captured["source_path"] != source
    assert "VERSION 0.7" in captured["source_text"]
    assert "VERSION .5" not in captured["source_text"]
    assert source.read_text(encoding="utf-8").splitlines()[1] == "VERSION .5"
    assert facts["source_format"] == "pcd"


def test_point_cloud_to_copc_rejects_source_format_mismatch(tmp_path):
    source = tmp_path / "source.las"
    source.write_bytes(b"pointcloud")

    with pytest.raises(ConverterError) as exc:
        invoke_operator(
            "laz_to_copc",
	            {
	                "access_plan": {
	                    "source": {"root_uri": str(source), "format": "las"},
	                    "target": {
	                        "file_name": "source.copc.laz",
	                        "publish": {"method": "object_store"},
	                    },
	                }
	            },
        )

    assert exc.value.error_code == "INVALID_PARAMS"
    assert "must be laz" in exc.value.message
