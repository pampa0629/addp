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


def test_operator_metadata_contract():
    ops = list_operators()
    assert [operator["name"] for operator in ops] == ["las_to_copc", "laz_to_copc", "e57_to_copc"]
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


@pytest.mark.parametrize(
    ("operator_name", "source_suffix", "source_format"),
    [
        ("las_to_copc", ".las", "las"),
        ("laz_to_copc", ".laz", "laz"),
        ("e57_to_copc", ".e57", "e57"),
    ],
)
def test_point_cloud_to_copc_invokes_pdal_and_publishes(tmp_path, monkeypatch, operator_name, source_suffix, source_format):
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
                "source": {"local_path": str(source), "format": source_format},
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
            "options": {"compression": "laszip", "chunk_size": 50000},
        },
        runner=fake_runner,
        env={"POINTCLOUD_PDAL_BIN": str(pdal)},
    )

    assert captured["command"] == [
        str(pdal),
        "translate",
        str(source),
        str(captured["publish_path"]),
        "--writers.copc.forward=all",
        "--writers.copc.compression=laszip",
        "--writers.copc.chunk_size=50000",
    ]
    assert facts["copc_ref"] == "tenant_1/point-cloud-copc/source.copc.laz"
    assert facts["copc_uri"] == "s3://manager/tenant_1/point-cloud-copc/source.copc.laz"
    assert facts["size_bytes"] == 4
    assert facts["source_format"] == source_format
    assert facts["target_format"] == "copc"
    assert facts["converter"] == str(pdal)
    assert "secret_key" not in str(facts)


def test_point_cloud_to_copc_rejects_source_format_mismatch(tmp_path):
    source = tmp_path / "source.las"
    source.write_bytes(b"pointcloud")

    with pytest.raises(ConverterError) as exc:
        invoke_operator(
            "laz_to_copc",
            {
                "access_plan": {
                    "source": {"local_path": str(source), "format": "las"},
                    "target": {
                        "file_name": "source.copc.laz",
                        "publish": {"method": "object_store"},
                    },
                }
            },
        )

    assert exc.value.error_code == "INVALID_PARAMS"
    assert "must be laz" in exc.value.message
