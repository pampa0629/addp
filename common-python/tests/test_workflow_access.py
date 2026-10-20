from pathlib import Path

import pytest

from addp_common.workflow_access import (
    WorkflowAccessError,
    WorkflowTargetExistsError,
    publish_target_directory,
    publish_target_file,
    require_access_plan,
    require_source_plan,
    require_target_plan,
    stage_source_file,
    stage_source_directory,
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
    with pytest.raises(WorkflowTargetExistsError, match="already exists"):
        publish_target_file(source, plan)
    source.write_bytes(b"second")
    plan["target"]["write_mode"] = "replace"
    publish_target_file(source, plan)
    assert target.read_bytes() == b"second"


def _object_source(kind, key, *, entrypoint=None):
    source = {"kind": kind, "format": "ifc" if kind == "file" else "3ds",
              "access": {"method": "object_store", "endpoint": "minio:9000", "access_key": "a",
                         "secret_key": "b", "bucket": "manager", "object" if kind == "file" else "prefix": key}}
    if entrypoint is not None:
        source["entrypoint"] = entrypoint
    return {"schema_version": "addp.workflow.access-plan/v1", "source": source}


def test_source_file_preserves_exact_object_and_entrypoint_names(tmp_path, monkeypatch):
    import addp_common.workflow_access as access
    key, name = "temp/source/ 模型.ifc ", " 模型.ifc "
    calls = []
    class Client:
        def fget_object(self, bucket, object_name, target):
            calls.append((bucket, object_name))
            assert object_name == key
            Path(target).write_bytes(b"exact source")
    monkeypatch.setattr(access, "_object_store_client", lambda _: Client())
    result = stage_source_file(_object_source("file", key, entrypoint=name), tmp_path)
    assert result.name == name
    assert result.read_bytes() == b"exact source"
    assert calls == [("manager", key)]


@pytest.mark.parametrize("prefix", ["temp/source/目录 ", "/temp/source/目录 ", "temp/source/目录 /"])
def test_directory_source_preserves_prefix_and_distinct_trailing_space_members(tmp_path, monkeypatch, prefix):
    import addp_common.workflow_access as access
    from types import SimpleNamespace
    data = {prefix + "/Textures/A.PNG": b"first", prefix + "/Textures/A.PNG ": b"second"}
    calls = []
    class Client:
        def list_objects(self, bucket, **options):
            assert options == {"prefix": prefix + "/", "recursive": True}
            return [SimpleNamespace(object_name=key, is_dir=False) for key in data]
        def fget_object(self, bucket, key, target):
            calls.append(key)
            Path(target).write_bytes(data[key])
    monkeypatch.setattr(access, "_object_store_client", lambda _: Client())
    result = stage_source_directory(_object_source("directory", prefix), tmp_path)
    assert (result / "Textures/A.PNG").read_bytes() == b"first"
    assert (result / "Textures/A.PNG ").read_bytes() == b"second"
    assert calls == list(data)


@pytest.mark.parametrize("key", ["outside/texture.png", "prefix", "prefix/", "prefix/../escape.png",
                                  "prefix/nested/../../escape.png", "prefix//escape.png", "prefix/./escape.png",
                                  "prefix/a\\escape.png", "prefix/a\x00b", None, 7])
def test_directory_source_rejects_invalid_members_before_any_download(tmp_path, monkeypatch, key):
    import addp_common.workflow_access as access
    from types import SimpleNamespace
    calls = []
    class Client:
        def list_objects(self, *args, **kwargs):
            return [SimpleNamespace(object_name="prefix/valid.png", is_dir=False),
                    SimpleNamespace(object_name=key, is_dir=False)]
        def fget_object(self, *args):
            calls.append(args)
    monkeypatch.setattr(access, "_object_store_client", lambda _: Client())
    with pytest.raises(WorkflowAccessError):
        stage_source_directory(_object_source("directory", "prefix"), tmp_path)
    assert calls == []
    assert not (tmp_path / "escape.png").exists()


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


@pytest.mark.parametrize('kind', ['file', 'directory'])
@pytest.mark.parametrize('method', ['mounted_path', 'object_store'])
def test_create_conflict_is_typed_and_never_writes(tmp_path, monkeypatch, kind, method):
    import addp_common.workflow_access as access
    from types import SimpleNamespace
    source, target = tmp_path / 'source', tmp_path / 'target'
    if kind == 'file':
        source.write_bytes(b'new')
        target.write_bytes(b'original')
    else:
        source.mkdir(); target.mkdir()
        (source / 'tileset.json').write_text('{}')
        (target / 'original').write_bytes(b'original')
    plan = _plan(source, target, kind=kind)
    writes = []
    if method == 'object_store':
        plan['target']['access'] = {'method': method, 'bucket': 'test', 'object': 'target', 'prefix': 'target'}
        class Client:
            def bucket_exists(self, bucket): return True
            def stat_object(self, *args): return object()
            def list_objects(self, *args, **kwargs): return iter([SimpleNamespace(object_name='target/original', is_dir=False)])
            def fput_object(self, *args, **kwargs): writes.append(args)
            def remove_object(self, *args): writes.append(args)
        monkeypatch.setattr(access, '_object_store_client', lambda _: Client())
    with pytest.raises(WorkflowTargetExistsError):
        if kind == 'file': publish_target_file(source, plan)
        else: publish_target_directory(source, plan, completion_marker='tileset.json')
    assert writes == []
    assert (target if kind == 'file' else target / 'original').read_bytes() == b'original'
