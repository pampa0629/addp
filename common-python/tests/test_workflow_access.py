from pathlib import Path

import pytest

from addp_common.workflow_access import WorkflowAccessError, publish_target_directory, publish_target_file, require_access_plan, stage_source_file


def _plan(source: Path, target: Path, *, kind="file", write_mode="create"):
    return {
        "schema_version": "addp.workflow.access-plan/v1",
        "source": {
            "kind": kind,
            "format": "las" if kind == "file" else "osgb_scene",
            "access": {"method": "mounted_path", "path": str(source)},
        },
        "target": {
            "kind": kind,
            "format": "copc" if kind == "file" else "3dtiles",
            "name": target.name,
            "write_mode": write_mode,
            "access": {"method": "mounted_path", "path": str(target)},
        },
    }


def test_local_file_create_and_replace(tmp_path):
    source = tmp_path / "source.las"
    source.write_bytes(b"first")
    target = tmp_path / "out" / "result.copc.laz"
    plan = _plan(source, target)
    require_access_plan({"access_plan": plan})
    assert stage_source_file(plan, tmp_path) == source
    publish_target_file(source, plan)
    assert target.read_bytes() == b"first"
    with pytest.raises(WorkflowAccessError, match="already exists"):
        publish_target_file(source, plan)
    source.write_bytes(b"second")
    plan["target"]["write_mode"] = "replace"
    publish_target_file(source, plan)
    assert target.read_bytes() == b"second"


def test_local_directory_completion_marker_and_replace(tmp_path):
    source = tmp_path / "source"
    source.mkdir()
    target = tmp_path / "target"
    plan = _plan(source, target, kind="directory")
    with pytest.raises(WorkflowAccessError, match="completion marker"):
        publish_target_directory(source, plan, completion_marker="tileset.json")
    (source / "tileset.json").write_text("{}", encoding="utf-8")
    publish_target_directory(source, plan, completion_marker="tileset.json")
    assert (target / "tileset.json").is_file()
