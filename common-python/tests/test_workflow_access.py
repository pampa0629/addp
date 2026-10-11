from pathlib import Path

import pytest

from addp_common.workflow_access import (
    WorkflowAccessError,
    publish_target_directory,
    publish_target_file,
    require_access_plan,
    require_source_plan,
    require_target_plan,
    stage_source_file,
    target_child_file_plan,
    require_empty_target_directory,
)


@pytest.mark.parametrize('name', ['../escape.tif', '/escape.tif', 'a/b.tif', 'a\\b.tif', '.', '..', '', 'a\x00b'])
def test_batch_child_rejects_scope_escape(tmp_path, name):
    plan = _plan(tmp_path/'source', tmp_path/'tiles', kind='directory')
    del plan['source']
    with pytest.raises(WorkflowAccessError):
        target_child_file_plan(plan, name)


def test_batch_child_local_preflight_and_immutable_plan(tmp_path):
    import copy
    plan = _plan(tmp_path/'source', tmp_path/'tiles', kind='directory')
    del plan['source']
    before = copy.deepcopy(plan)
    require_empty_target_directory(plan)
    child = target_child_file_plan(plan, 'r0_c0.tif')
    assert child['target']['kind'] == 'file'
    assert child['target']['access']['path'] == str(tmp_path/'tiles/r0_c0.tif')
    assert plan == before
    (tmp_path/'tiles').mkdir()
    require_empty_target_directory(plan)
    (tmp_path/'tiles/unrelated.txt').write_text('keep')
    with pytest.raises(WorkflowAccessError): require_empty_target_directory(plan)


def test_batch_child_object_scope_preflight(monkeypatch):
    import addp_common.workflow_access as access
    plan = {'schema_version': access.SCHEMA_VERSION, 'target': {
        'kind': 'directory', 'format': 'tiff', 'name': 'tiles', 'write_mode': 'create',
        'access': {'method': 'object_store', 'endpoint': 'minio:9000', 'access_key': 'a',
                   'secret_key': 'b', 'bucket': 'business', 'prefix': 'out/tiles'}}}
    calls = []
    class Client:
        existing = []
        def list_objects(self, bucket, **kwargs):
            calls.append((bucket, kwargs)); return iter(self.existing)
    client = Client()
    monkeypatch.setattr(access, '_object_store_client', lambda _: client)
    access.require_empty_target_directory(plan)
    assert calls == [('business', {'prefix': 'out/tiles/', 'recursive': True})]
    child = access.target_child_file_plan(plan, 'r3_c4.tif')
    assert child['target']['access']['object'] == 'out/tiles/r3_c4.tif'
    assert 'prefix' not in child['target']['access']
    assert plan['target']['access']['prefix'] == 'out/tiles'
    client.existing = [object()]
    with pytest.raises(WorkflowAccessError): access.require_empty_target_directory(plan)


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


def test_source_and_target_only_access_plans(tmp_path):
    plan = _plan(tmp_path / "source.las", tmp_path / "target.copc.laz")
    require_source_plan({"access_plan": {"schema_version": plan["schema_version"], "source": plan["source"]}})
    require_target_plan({"access_plan": {"schema_version": plan["schema_version"], "target": plan["target"]}})

    with pytest.raises(WorkflowAccessError, match="must not contain target"):
        require_source_plan({"access_plan": plan})
    with pytest.raises(WorkflowAccessError, match="must not contain source"):
        require_target_plan({"access_plan": plan})
