import json
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


def mounted_plan(source: Path, target: Path, source_format: str):
    return {
        "schema_version": "addp.workflow.access-plan/v1",
        "source": {
            "kind": "file",
            "format": source_format,
            "access": {"method": "mounted_path", "path": str(source)},
            "metadata": {"source_size_bytes": source.stat().st_size},
        },
        "target": {
            "kind": "file",
            "format": "copc",
            "name": target.name,
            "write_mode": "create",
            "content_type": "application/vnd.laszip+copc",
            "access": {"method": "mounted_path", "path": str(target)},
        },
    }


def test_operator_metadata_contract_and_modes():
    ops = list_operators()
    assert [operator["name"] for operator in ops] == [
        "las_to_copc", "laz_to_copc", "e57_to_copc", "pcd_to_copc", "xyz_to_copc",
    ]
    assert_operator_metadata_contract(ops, expected_engine_type="pointcloud_workflow")
    assert all(operator["execution_modes"] == ["workflow", "direct"] for operator in ops)


def test_converter_status_defaults_to_engine_bound_binary():
    status = converter_status(env={})
    assert status["binding"] == "pointcloud_workflow"
    assert status["env"] == "POINTCLOUD_PDAL_BIN"
    assert status["path"].endswith("engines/pointcloud-workflow/bin/pdal")


def test_default_copc_threads_is_bounded():
    assert operators._default_copc_threads(env={}) == 4
    assert operators._default_copc_threads(env={"POINTCLOUD_COPC_THREADS": "12"}) == 8
    assert operators._default_copc_threads(env={"POINTCLOUD_COPC_THREADS": "0"}) == 1


@pytest.mark.parametrize(
    ("operator_name", "suffix", "source_format"),
    [
        ("las_to_copc", ".las", "las"),
        ("laz_to_copc", ".laz", "laz"),
        ("e57_to_copc", ".e57", "e57"),
        ("pcd_to_copc", ".pcd", "pcd"),
        ("xyz_to_copc", ".xyz", "xyz"),
    ],
)
def test_conversion_uses_v1_plan_and_publishes_to_mounted_target(tmp_path, operator_name, suffix, source_format):
    source = tmp_path / f"source{suffix}"
    source.write_bytes(b"pointcloud")
    target = tmp_path / "output" / "source.copc.laz"
    pdal = tmp_path / "pdal"
    pdal.write_text("#!/bin/sh\n", encoding="utf-8")
    pdal.chmod(0o755)
    captured = {}

    def fake_runner(command, timeout_seconds):
        captured["command"] = command
        Path(command[3]).write_bytes(b"copc")
        return CommandResult(returncode=0, stdout="translated")

    facts = invoke_operator(
        operator_name,
        {
            "access_plan": mounted_plan(source, target, source_format),
            "options": {"scale_x": 0.01, "threads": 2},
        },
        runner=fake_runner,
        env={"POINTCLOUD_PDAL_BIN": str(pdal)},
    )

    assert target.read_bytes() == b"copc"
    assert facts["copc_uri"] == str(target)
    assert facts["source_format"] == source_format
    assert "--writers.copc.threads=2" in captured["command"]
    if source_format == "xyz":
        assert "readers.text" in captured["command"]


def test_progress_callback_is_separate_from_access_plan(tmp_path, monkeypatch):
    source = tmp_path / "source.las"
    source.write_bytes(b"pointcloud" * 100)
    target = tmp_path / "source.copc.laz"
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
        posted.append(json.loads(req.data.decode("utf-8")))
        return FakeHTTPResponse()

    def fake_runner(command, timeout_seconds):
        Path(command[3]).write_bytes(b"copc")
        return CommandResult(returncode=0, stdout="translated")

    monkeypatch.setattr(operators.urlrequest, "urlopen", fake_urlopen)
    invoke_operator(
        "las_to_copc",
        {
            "access_plan": mounted_plan(source, target, "las"),
            "progress_callback": {
                "endpoint": "http://manager/api/v1/manager/internal/executions/exec-1/events",
                "tenant_id": 7,
                "execution_id": "exec-1",
                "internal_api_key": "secret",
            },
        },
        runner=fake_runner,
        env={"POINTCLOUD_PDAL_BIN": str(pdal)},
    )
    assert [(item["phase"], item["event"]) for item in posted] == [
        ("prepare", "started"), ("convert", "started"), ("publish", "started"), ("publish", "completed"),
    ]


def test_legacy_pcd_header_is_normalized_without_mutating_source(tmp_path):
    source = tmp_path / "legacy.pcd"
    source.write_text("VERSION .5\nFIELDS x y z\nDATA ascii\n0 0 0\n", encoding="utf-8")
    target = tmp_path / "legacy.copc.laz"
    pdal = tmp_path / "pdal"
    pdal.write_text("#!/bin/sh\n", encoding="utf-8")
    pdal.chmod(0o755)
    captured = {}

    def fake_runner(command, timeout_seconds):
        captured["source"] = Path(command[2])
        captured["text"] = captured["source"].read_text(encoding="utf-8")
        Path(command[3]).write_bytes(b"copc")
        return CommandResult(returncode=0)

    invoke_operator(
        "pcd_to_copc",
        {"access_plan": mounted_plan(source, target, "pcd")},
        runner=fake_runner,
        env={"POINTCLOUD_PDAL_BIN": str(pdal)},
    )
    assert captured["source"] != source
    assert "VERSION 0.7" in captured["text"]
    assert source.read_text(encoding="utf-8").startswith("VERSION .5")


def test_source_format_mismatch_is_rejected(tmp_path):
    source = tmp_path / "source.las"
    source.write_bytes(b"pointcloud")
    with pytest.raises(ConverterError, match="must be laz"):
        invoke_operator("laz_to_copc", {"access_plan": mounted_plan(source, tmp_path / "source.copc.laz", "las")})
