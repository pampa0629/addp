import hashlib
import io
import json
import threading
import time
from datetime import datetime, timezone
from uuid import uuid4

import pytest

import private_files
from private_files import FileManifest, PrivateFileError, PrivateFiles
from test_private_execution import _difference_files


CREATOR = ("develop-service-principal", "2")


def _manifest(tmp_path, *, save=True):
    business = tmp_path / "business"
    business.mkdir()
    workflow = _difference_files(business)
    contents = [(business / name).read_bytes() for name in ["source.gpkg", "mask.gpkg"]]
    for task in workflow["tasks"]:
        task["params"].pop("connection_info", None)
        task["params"].pop("path", None)
    if not save:
        workflow["tasks"].pop()
    return {
        "schema_version": "addp.workflow.private-files/v2", "tenant_id": "2",
        "execution_id": str(uuid4()), "attempt": 1, "authorization_id": "101",
        "scope_digest": "a" * 64,
        "deadline": datetime.fromtimestamp(time.time() + 45, timezone.utc).isoformat(),
        "workflow_json": json.dumps(workflow),
        "inputs": [{"node_id": node, "format": "geopackage", "size_bytes": str(len(data)),
                    "sha256": hashlib.sha256(data).hexdigest()}
                   for node, data in zip(["source", "mask"], contents)],
        "outputs": ["save"] if save else [],
    }, contents


def test_private_files_complete_bytes_original_dag_and_cleanup(tmp_path):
    import geopandas as gpd
    from shapely.geometry import box

    raw, contents = _manifest(tmp_path)
    manifest = FileManifest.parse(raw)
    raw["inputs"][0]["sha256"] = "b" * 64
    files = PrivateFiles(manifest, CREATOR, parent=tmp_path)
    assert files.directory.stat().st_mode & 0o777 == 0o700
    try:
        for index, data in enumerate(contents):
            files.put_input(CREATOR, index, io.BytesIO(data))
            assert (files.directory / f"input-{index}.gpkg").read_bytes() == data
        files.start(CREATOR, "en")
        assert files.worker.wait(50) and files.worker.stopped()
        status = files.status(CREATOR)
        assert status["execution"]["status"] == "success", status
        assert status["inputs"] == ["ready", "ready"]
        assert status["execution_id"] == manifest.execution_id
        assert status["attempt"] == 1 and status["scope_digest"] == "a" * 64
        assert not status["cleanup_pending"]
        with files.open_output(CREATOR, 0) as stream:
            data = stream.read()
        assert status["outputs"] == [{"node_id": "save", "size_bytes": str(len(data)),
                                      "sha256": hashlib.sha256(data).hexdigest()}]
        output = gpd.read_file(files.directory / "output-0.gpkg")
        assert output["key"].tolist() == [11] and output["label"].tolist() == ["kept"]
        assert output.geometry.iloc[0].equals(box(0, 0, 2, 2))
        assert output.crs.to_epsg() == 3857
        assert [(tmp_path / "business" / name).read_bytes() for name in ["source.gpkg", "mask.gpkg"]] == contents
        status["outputs"].clear()
        assert files.status(CREATOR)["outputs"]
        with pytest.raises(PrivateFileError, match="ALREADY_DISPATCHED"):
            files.start(CREATOR, "en")
        # A transfer admitted before cleanup must actually finish first.
        with files.open_output(CREATOR, 0) as stream:
            assert files.cleanup(CREATOR) == {"cleanup": "pending"}
            assert files.status(CREATOR)["cleanup_pending"]
            assert stream.read() == data
        assert files.cleanup(CREATOR) == {"cleanup": "passed"}
        assert not files.directory.exists()
        assert files.cleanup(CREATOR) == {"cleanup": "passed"}
        assert not files.status(CREATOR)["cleanup_pending"]
        with pytest.raises(PrivateFileError, match="CLOSED"):
            with files.open_output(CREATOR, 0):
                pass
    finally:
        files.cleanup(CREATOR)


def test_private_files_read_only_rejection_has_no_target_or_output(tmp_path):
    raw, contents = _manifest(tmp_path, save=False)
    workflow = json.loads(raw["workflow_json"])
    workflow["tasks"][2]["params"]["id_field"] = "user-private-column"
    raw["workflow_json"] = json.dumps(workflow)
    files = PrivateFiles(FileManifest.parse(raw), CREATOR, parent=tmp_path)
    try:
        for index, data in enumerate(contents):
            files.put_input(CREATOR, index, io.BytesIO(data))
        files.start(CREATOR, "en")
        assert files.worker.wait(50)
        status = files.status(CREATOR)
        assert status["execution"]["error_code"] == "INVALID_PARAMS"
        assert status["execution"]["diagnostic"]["operator"]["parameter"] == "input_gdf"
        assert status["outputs"] == []
        assert "user-private-column" not in json.dumps(status)
        assert not list(files.directory.glob("output-*"))
        with pytest.raises(PrivateFileError, match="NOT_FOUND"):
            with files.open_output(CREATOR, 0):
                pass
    finally:
        assert files.cleanup(CREATOR) == {"cleanup": "passed"}


@pytest.mark.parametrize("mismatch", ["short", "extra", "digest"])
def test_private_files_reject_incomplete_or_changed_bytes_without_retry(tmp_path, mismatch):
    raw, contents = _manifest(tmp_path)
    data = contents[0]
    supplied = data[:-1] if mismatch == "short" else data + b"x" if mismatch == "extra" else data[:-1] + bytes([data[-1] ^ 1])
    files = PrivateFiles(FileManifest.parse(raw), CREATOR, parent=tmp_path)
    try:
        with pytest.raises(PrivateFileError, match="INPUT_MISMATCH"):
            files.put_input(CREATOR, 0, io.BytesIO(supplied))
        assert files.status(CREATOR)["inputs"] == ["failed", "missing"]
        assert not (files.directory / "input-0.gpkg").exists()
        with pytest.raises(PrivateFileError, match="ALREADY_CLAIMED"):
            files.put_input(CREATOR, 0, io.BytesIO(data))
        with pytest.raises(PrivateFileError, match="INPUTS_INCOMPLETE"):
            files.start(CREATOR, "en")
        assert files.worker is None
    finally:
        assert files.cleanup(CREATOR) == {"cleanup": "passed"}


def test_private_files_cleanup_waits_for_in_flight_upload(tmp_path):
    raw, contents = _manifest(tmp_path)
    files = PrivateFiles(FileManifest.parse(raw), CREATOR, parent=tmp_path)
    entered, release = threading.Event(), threading.Event()
    errors = []

    class BlockedStream(io.BytesIO):
        def read(self, size):
            entered.set()
            assert release.wait(5)
            return super().read(size)

    def upload():
        try:
            files.put_input(CREATOR, 0, BlockedStream(contents[0]))
        except PrivateFileError as exc:
            errors.append(exc.code)

    thread = threading.Thread(target=upload)
    thread.start()
    try:
        assert entered.wait(5)
        assert files.cleanup(CREATOR) == {"cleanup": "pending"}
        assert files.directory.exists() and files.status(CREATOR)["cleanup_pending"]
    finally:
        release.set()
        thread.join(5)
    assert not thread.is_alive()
    assert errors == ["PRIVATE_FILES_CLOSED"]
    assert files.cleanup(CREATOR) == {"cleanup": "passed"}


def test_private_files_deadline_after_last_chunk_does_not_start_eof_read(tmp_path, monkeypatch):
    raw, contents = _manifest(tmp_path)
    manifest = FileManifest.parse(raw)
    files = PrivateFiles(manifest, CREATOR, parent=tmp_path)
    reads = []

    class ExpiringStream(io.BytesIO):
        def read(self, size):
            reads.append(size)
            data = super().read(size)
            if self.tell() == len(contents[0]):
                monkeypatch.setattr(private_files.time, "time", lambda: manifest.deadline + 1)
            return data

    try:
        with pytest.raises(PrivateFileError, match="CLOSED"):
            files.put_input(CREATOR, 0, ExpiringStream(contents[0]))
        assert sum(reads) == len(contents[0])
        assert files.status(CREATOR)["inputs"][0] == "failed"
        assert not (files.directory / "input-0.gpkg").exists()
    finally:
        assert files.cleanup(CREATOR) == {"cleanup": "passed"}


def test_private_files_wrong_creator_expiry_and_invalid_ordinals_do_not_read(tmp_path, monkeypatch):
    raw, _ = _manifest(tmp_path)
    manifest = FileManifest.parse(raw)
    with pytest.raises(PrivateFileError, match="NOT_FOUND"):
        PrivateFiles(manifest, (CREATOR[0], "3"), parent=tmp_path)
    files = PrivateFiles(manifest, CREATOR, parent=tmp_path)

    class ForbiddenStream:
        def read(self, size):
            pytest.fail("denied request read the body")

    try:
        for actor, index in [(('other-service', '2'), 0), (CREATOR, -1), (CREATOR, 2), (CREATOR, True)]:
            with pytest.raises(PrivateFileError, match="NOT_FOUND"):
                files.put_input(actor, index, ForbiddenStream())
        with pytest.raises(PrivateFileError, match="NOT_FOUND"):
            files.cleanup(("other-service", "2"))
        monkeypatch.setattr(private_files.time, "time", lambda: manifest.deadline + 1)
        with pytest.raises(PrivateFileError, match="CLOSED"):
            files.put_input(CREATOR, 0, ForbiddenStream())
        assert files.status(CREATOR)["inputs"] == ["missing", "missing"]
    finally:
        assert files.cleanup(CREATOR) == {"cleanup": "passed"}


@pytest.mark.parametrize("key,value", [
    ("tenant_id", 2), ("tenant_id", "02"), ("authorization_id", "0"),
    ("execution_id", "AAAAAAAA-AAAA-4AAA-AAAA-AAAAAAAAAAAA"), ("execution_id", "00000000-0000-0000-0000-000000000000"),
    ("attempt", True), ("attempt", 0), ("scope_digest", "A" * 64),
    ("deadline", "2026-11-08T00:00:00"), ("inputs", []), ("outputs", ["a", "b"]),
    ("workflow_json", "{bad"), ("workflow_json", '{"tasks":[true]}'), ("workflow_json", '{"tasks":[NaN]}'),
])
def test_private_files_manifest_boundary_rejects_invalid_shapes(tmp_path, key, value):
    raw, _ = _manifest(tmp_path)
    raw[key] = value
    with pytest.raises(PrivateFileError):
        FileManifest.parse(raw)


@pytest.mark.parametrize("change", ["hidden_save", "mount", "overwrite", "wrong_node", "extra_key", "external_reader", "raster", "direct_only", "invalid_params"])
def test_private_files_manifest_rejects_undeclared_io(tmp_path, change):
    raw, _ = _manifest(tmp_path)
    workflow = json.loads(raw["workflow_json"])
    if change == "hidden_save":
        raw["outputs"] = []
    elif change == "mount":
        workflow["tasks"][0]["params"]["connection_info"] = {"mount_path": "/business"}
    elif change == "overwrite":
        workflow["tasks"][3]["params"]["mode"] = "replace"
    elif change == "wrong_node":
        raw["inputs"][0]["node_id"] = "difference"
    elif change == "extra_key":
        raw["callback_url"] = "http://not-an-owner/"
    elif change == "external_reader":
        workflow["tasks"][2]["operator"] = "load"
    elif change == "raster":
        workflow["tasks"][2]["operator"] = "raster_slope"
    elif change == "direct_only":
        workflow["tasks"][2]["operator"] = "raster_inspect"
    else:
        workflow["tasks"][2]["params"] = []
    raw["workflow_json"] = json.dumps(workflow)
    with pytest.raises(PrivateFileError):
        FileManifest.parse(raw)


def test_private_files_cleanup_failure_remains_pending(tmp_path, monkeypatch):
    raw, _ = _manifest(tmp_path)
    files = PrivateFiles(FileManifest.parse(raw), CREATOR, parent=tmp_path)
    with monkeypatch.context() as scoped:
        def unavailable(path):
            raise OSError("synthetic cleanup failure")
        scoped.setattr(private_files.shutil, "rmtree", unavailable)
        assert files.cleanup(CREATOR) == {"cleanup": "pending", "error_code": "PRIVATE_FILES_CLEANUP_FAILED"}
        assert files.status(CREATOR)["cleanup_pending"] and files.directory.exists()
    assert files.cleanup(CREATOR) == {"cleanup": "passed"}


def test_private_files_geojson_uses_same_transport_and_original_runner(tmp_path):
    data = json.dumps({'type': 'FeatureCollection', 'features': [
        {'type': 'Feature', 'properties': {'instance_id': 7},
         'geometry': {'type': 'Point', 'coordinates': [-122.27, 37.87]}}
    ]}).encode()
    workflow = {'tasks': [
        {'id': 'source', 'operator': 'load', 'params': {}, 'depends_on': []},
        {'id': 'project', 'operator': 'vector_reproject',
         'params': {'input_gdf': {'$ref': 'source'}, 'target_crs': 'EPSG:32610'},
         'depends_on': ['source']}
    ]}
    raw = {'schema_version': 'addp.workflow.private-files/v2', 'tenant_id': '2',
           'execution_id': str(uuid4()), 'attempt': 1, 'authorization_id': '101',
           'scope_digest': 'a' * 64,
           'deadline': datetime.fromtimestamp(time.time() + 45, timezone.utc).isoformat(),
           'workflow_json': json.dumps(workflow),
           'inputs': [{'node_id': 'source', 'format': 'geojson', 'size_bytes': str(len(data)),
                       'sha256': hashlib.sha256(data).hexdigest()}], 'outputs': []}
    for invalid in [None, 'gpkg', '../../private', 'GeoJSON']:
        changed = {**raw, 'inputs': [{**raw['inputs'][0], 'format': invalid}]}
        with pytest.raises(PrivateFileError):
            FileManifest.parse(changed)
    files = PrivateFiles(FileManifest.parse(raw), CREATOR, parent=tmp_path)
    try:
        files.put_input(CREATOR, 0, io.BytesIO(data))
        assert (files.directory / 'input-0.geojson').read_bytes() == data
        assert not (files.directory / 'input-0.gpkg').exists()
        files.start(CREATOR, 'en')
        assert files.worker.wait(50) and files.worker.stopped()
        status = files.status(CREATOR)
        assert status['execution']['status'] == 'success', status
        assert status['outputs'] == []
    finally:
        assert files.cleanup(CREATOR)['cleanup'] == 'passed'
