import hashlib
import json
import os
import multiprocessing
import signal
import time
from pathlib import Path

import pytest

import private_execution
from private_execution import PrivateWorkflowProcess


def _blocked_worker(directory, workflow, language, deadline):
    (Path(directory) / "entered").write_text("entered")
    while True:
        time.sleep(1)


def _crashed_worker(directory, workflow, language, deadline):
    os._exit(7)


def _unsupervised_native_worker(directory, deadline):
    import workflow_engine

    def blocked(*args, **kwargs):
        (Path(directory) / "entered").touch()
        while True:
            time.sleep(1)

    workflow_engine.GeoPythonWorkflowRunner.execute = blocked
    private_execution._run_private_dag(directory, {}, "en", deadline)


def _wait_for_entry(directory, worker):
    end = time.monotonic() + 10
    while not (directory / "entered").exists() and time.monotonic() < end:
        if worker.wait(0.01):
            pytest.fail(f"worker exited before entry: {worker.snapshot()}")
    assert (directory / "entered").exists()


def _difference_files(directory):
    import geopandas as gpd
    from shapely.geometry import box

    source = gpd.GeoDataFrame({"key": [11], "label": ["kept"]},
                             geometry=[box(0, 0, 4, 2)], crs="EPSG:3857")
    mask = gpd.GeoDataFrame(geometry=[box(2, 0, 4, 2)], crs=source.crs)
    source.to_file(directory / "source.gpkg", driver="GPKG", engine="pyogrio")
    mask.to_file(directory / "mask.gpkg", driver="GPKG", engine="pyogrio")
    connection = {"mount_path": str(directory)}
    return {"tasks": [
        {"id": "source", "operator": "load", "params": {
            "connection_info": connection, "path": "source.gpkg"}, "depends_on": []},
        {"id": "mask", "operator": "load", "params": {
            "connection_info": connection, "path": "mask.gpkg"}, "depends_on": []},
        {"id": "difference", "operator": "difference", "params": {
            "input_gdf": {"$ref": "source"}, "gdf_b": {"$ref": "mask"}, "id_field": "key"},
         "depends_on": ["source", "mask"]},
        {"id": "save", "operator": "save", "params": {
            "input_df": {"$ref": "difference"}, "connection_info": connection,
            "path": "output.gpkg", "mode": "fail"}, "depends_on": ["difference"]},
    ]}


def test_private_workflow_process_runs_original_dag_and_real_geopackage(tmp_path):
    import geopandas as gpd
    from shapely.geometry import box

    workflow = _difference_files(tmp_path)
    initial = {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in tmp_path.glob("*.gpkg")}
    worker = PrivateWorkflowProcess(tmp_path, workflow, "en", time.time() + 45)
    worker.start()
    assert worker.wait(50)
    assert worker.stopped()
    report = worker.snapshot()
    assert report["status"] == "success", report
    assert report["task_order"] == ["source", "mask", "difference", "save"]
    output = gpd.read_file(tmp_path / "output.gpkg")
    assert output["key"].tolist() == [11]
    assert output["label"].tolist() == ["kept"]
    assert output.crs.to_epsg() == 3857
    assert output.geometry.iloc[0].equals(box(0, 0, 2, 2))
    assert all(hashlib.sha256((tmp_path / name).read_bytes()).hexdigest() == h
               for name, h in initial.items())
    report["all_results"].clear()
    assert worker.snapshot()["all_results"]
    with pytest.raises(RuntimeError, match="already been dispatched"):
        worker.start()


def test_private_workflow_process_keeps_controlled_diagnostic_and_blocks_save(tmp_path):
    workflow = _difference_files(tmp_path)
    workflow["tasks"][2]["params"]["id_field"] = "private-missing-column"
    worker = PrivateWorkflowProcess(tmp_path, workflow, "en", time.time() + 45)
    worker.start()
    assert worker.wait(50) and worker.stopped()
    report = worker.snapshot()
    assert report["status"] == "failed"
    assert report["error_code"] == "INVALID_PARAMS"
    assert report["diagnostic"]["operator"] == {
        "operator_id": "difference", "node_index": 3, "parameter": "input_gdf"}
    assert report["diagnostic"]["reason"] == "operator_input_invalid"
    assert not (tmp_path / "output.gpkg").exists()
    encoded = json.dumps(report)
    assert "private-missing-column" not in encoded and str(tmp_path) not in encoded


def test_private_workflow_process_complete_file_preserves_non_json_business_value(tmp_path):
    import geopandas as gpd

    workflow = _difference_files(tmp_path)
    source_path = tmp_path / "source.gpkg"
    source = gpd.read_file(source_path)
    source["metric"] = float("inf")
    source_path.unlink()
    source.to_file(source_path, driver="GPKG", engine="pyogrio")
    worker = PrivateWorkflowProcess(tmp_path, workflow, "en", time.time() + 45)
    worker.start()
    assert worker.wait(50) and worker.stopped()
    report = worker.snapshot()
    assert report["status"] == "success", report
    assert gpd.read_file(tmp_path / "output.gpkg")["metric"].iloc[0] == float("inf")
    # No full row data or invalid JSON numbers belong in private status.
    json.dumps(report, allow_nan=False)
    assert "metric" not in json.dumps(report)


def test_private_workflow_process_deadline_stops_actual_child(tmp_path, monkeypatch):
    monkeypatch.setattr(private_execution, "_run_private_dag", _blocked_worker)
    worker = PrivateWorkflowProcess(tmp_path, {}, "en", time.time() + 3)
    worker.start()
    assert worker._process.pid is not None
    assert worker.wait(6) and worker.stopped()
    assert worker.snapshot() == {"status": "failed", "error_code": "EXECUTION_EXPIRED"}
    assert not (tmp_path / "result.json").exists()


def test_private_workflow_process_child_deadline_without_supervisor(tmp_path):
    process = multiprocessing.get_context("spawn").Process(
        target=_unsupervised_native_worker, args=(str(tmp_path), time.time() + 4))
    process.start()
    try:
        process.join(8)
        assert not process.is_alive()
        assert process.exitcode == -signal.SIGALRM
        assert (tmp_path / "entered").exists()
        assert not (tmp_path / "result.json").exists()
    finally:
        if process.is_alive():
            process.kill()
            process.join(2)


def test_private_workflow_process_cancel_stops_actual_child(tmp_path, monkeypatch):
    monkeypatch.setattr(private_execution, "_run_private_dag", _blocked_worker)
    worker = PrivateWorkflowProcess(tmp_path, {}, "en", time.time() + 30)
    worker.start()
    _wait_for_entry(tmp_path, worker)
    worker.cancel()
    assert worker.wait(5) and worker.stopped()
    assert worker.snapshot() == {"status": "failed", "error_code": "EXECUTION_CANCELLED"}


def test_private_workflow_process_native_exit_is_failure(tmp_path, monkeypatch):
    monkeypatch.setattr(private_execution, "_run_private_dag", _crashed_worker)
    worker = PrivateWorkflowProcess(tmp_path, {}, "en", time.time() + 15)
    worker.start()
    assert worker.wait(10) and worker.stopped()
    assert worker.snapshot() == {"status": "failed", "error_code": "EXECUTION_FAILED"}
    assert not (tmp_path / "result.json").exists()


def test_private_workflow_process_failed_spawn_is_not_redispatched(tmp_path, monkeypatch):
    process_type = private_execution.multiprocessing.get_context("spawn").Process

    def unavailable(self):
        raise OSError("synthetic spawn failure")

    monkeypatch.setattr(process_type, "start", unavailable)
    worker = PrivateWorkflowProcess(tmp_path, {}, "en", time.time() + 15)
    with pytest.raises(OSError, match="synthetic spawn failure"):
        worker.start()
    assert worker.wait(0) and worker.stopped()
    assert worker.snapshot() == {"status": "failed", "error_code": "EXECUTION_FAILED"}
    with pytest.raises(RuntimeError, match="already been dispatched"):
        worker.start()


def test_private_workflow_process_failed_observer_stops_started_child(tmp_path, monkeypatch):
    class UnavailableObserver:
        def __init__(self, **kwargs):
            pass

        def start(self):
            raise RuntimeError("synthetic observer start failure")

    monkeypatch.setattr(private_execution, "_run_private_dag", _blocked_worker)
    monkeypatch.setattr(private_execution.threading, "Thread", UnavailableObserver)
    worker = PrivateWorkflowProcess(tmp_path, {}, "en", time.time() + 30)
    with pytest.raises(RuntimeError, match="synthetic observer start failure"):
        worker.start()
    assert worker.wait(0) and worker.stopped()
    assert worker.snapshot() == {"status": "failed", "error_code": "EXECUTION_FAILED", "cleanup_pending": False}


@pytest.mark.parametrize("cancel", [False, True])
def test_private_workflow_process_cannot_dispatch_after_terminal(tmp_path, cancel):
    worker = PrivateWorkflowProcess(tmp_path, {}, "en", time.time() - 1)
    if cancel:
        worker.cancel()
    else:
        worker.start()
    assert worker.wait(0) and worker.stopped()
    assert worker.snapshot()["error_code"] == ("EXECUTION_CANCELLED" if cancel else "EXECUTION_EXPIRED")
    assert worker._process is None
    with pytest.raises(RuntimeError, match="already been dispatched"):
        worker.start()
