import pytest
#!/usr/bin/env python3
"""
GeoPython Workflow Test Script
测试工作流引擎的核心功能
"""

import sys

def test_imports():
    """测试依赖导入"""
    print("=== 测试 1: 依赖导入 ===")
    import geopandas as gpd
    import shapely
    from flask import Flask

    assert Flask is not None
    assert gpd.__version__
    assert shapely.__version__

def test_operators():
    """测试算子函数"""
    print("\n=== 测试 2: 算子库 ===")
    from operators import list_operators, get_operator

    operators = list_operators()
    operator_names = {operator["name"] for operator in operators}
    categories = {}
    for meta in operators:
        cat = meta["category"]
        categories[cat] = categories.get(cat, 0) + 1

    assert len(operators) >= 40
    assert {"buffer", "load", "save", "get_area"}.issubset(operator_names)
    assert len(categories) >= 4
    assert get_operator("buffer") is not None

def test_workflow_engine():
    """测试工作流引擎"""
    print("\n=== 测试 3: 工作流引擎 ===")
    from workflow_engine import execute_workflow

    # 测试简单的缓冲区工作流
    workflow_def = {
        "tasks": [{
            "id": "t1",
            "operator": "buffer",
            "params": {
                "input_gdf": {
                    "type": "FeatureCollection",
                    "features": [{
                        "type": "Feature",
                        "geometry": {
                            "type": "Point",
                            "coordinates": [116.404, 39.915]
                        },
                        "properties": {"name": "Beijing"}
                    }]
                },
                "distance": 0.01
            },
            "depends_on": []
        }]
    }

    result = execute_workflow(workflow_def)

    assert result["status"] == "success", result.get("error")
    assert result["final_result"]

def test_dag_sorting():
    """测试 DAG 拓扑排序"""
    print("\n=== 测试 4: DAG 拓扑排序 ===")
    from workflow_engine import execute_workflow

    # 测试复杂依赖链: t1 -> t2 -> t3
    workflow_def = {
        "tasks": [
            {
                "id": "t1",
                "operator": "buffer",
                "params": {
                    "input_gdf": {
                        "type": "FeatureCollection",
                        "features": [{
                            "type": "Feature",
                            "geometry": {"type": "Point", "coordinates": [116.404, 39.915]},
                            "properties": {"name": "Beijing"}
                        }]
                    },
                    "distance": 0.01
                },
                "depends_on": []
            },
            {
                "id": "t2",
                "operator": "centroid",
                "params": {
                    "input_gdf": {"$ref": "t1"}
                },
                "depends_on": ["t1"]
            },
            {
                "id": "t3",
                "operator": "get_area",
                "params": {
                    "input_gdf": {"$ref": "t2"}
                },
                "depends_on": ["t2"]
            }
        ]
    }

    result = execute_workflow(workflow_def)

    assert result["status"] == "success", result.get("error")
    assert len(result["logs"]) >= 3

def test_workflow_engine_json_result():
    """测试非 GeoDataFrame 的结构化 JSON 输出"""
    print("\n=== 测试 5: JSON 结果输出 ===")
    import json
    import operators
    from workflow_engine import execute_workflow

    operators.OPERATORS["json_facts"] = {
        "function": lambda **_: {"status": "success", "profile": "cog", "width": 256}, "execution_modes": ["workflow"]
    }
    try:
        result = execute_workflow({
            "tasks": [{
                "id": "facts",
                "operator": "json_facts",
                "params": {},
                "depends_on": []
            }]
        })
    finally:
        operators.OPERATORS.pop("json_facts", None)

    assert result["status"] == "success", result.get("error")
    facts = json.loads(result["final_result"])
    assert facts["profile"] == "cog"
    assert facts["width"] == 256

def test_workflow_definition_requires_params_object_and_string_dependencies():
    from addp_common.workflow_runtime import WorkflowValidationError
    from workflow_engine import GeoPythonWorkflowRunner
    import pytest
    for params, dependencies in [([],[]),({},[1])]:
        with pytest.raises(WorkflowValidationError):
            GeoPythonWorkflowRunner().execute({'tasks':[{'id':'invalid','operator':'buffer','params':params,'depends_on':dependencies}]})


def test_direct_operator_json_result():
    """测试 direct 算子支持非 GeoDataFrame 的结构化 JSON 输出"""
    print("\n=== 测试 6: direct JSON 结果输出 ===")
    import operators
    from workflow_engine import execute_single_operator

    operators.OPERATORS["direct_json_facts"] = {
        "function": lambda **_: {"status": "success", "profile": "cog", "width": 256}, "execution_modes": ["workflow"]
    }
    try:
        result = execute_single_operator("direct_json_facts", {})
    finally:
        operators.OPERATORS.pop("direct_json_facts", None)

    assert result["status"] == "success", result.get("error")
    assert result["result"]["profile"] == "cog"
    assert result["result"]["width"] == 256


def test_direct_operator_preserves_object_params_containing_features_text():
    """object 参数由元数据约束，不得按字符串内容误判成 GeoJSON。"""
    import operators
    from workflow_engine import execute_single_operator

    captured = {}

    def direct_object_options(options):
        captured["options"] = options
        return {"received": options}

    operators.OPERATORS["direct_object_options"] = {
        "function": direct_object_options,
        "param_schema": [{
            "name": "options",
            "type": "object",
            "param_type": "param",
            "required": True,
            "description": "structured options",
        }],
    }
    options = {"max_features": 1000000, "layer_name": "farmland"}
    try:
        result = execute_single_operator("direct_object_options", {"options": options})
    finally:
        operators.OPERATORS.pop("direct_object_options", None)

    assert result["status"] == "success", result.get("error")
    assert captured["options"] == options
    assert result["result"]["received"] == options


def test_direct_operator_converts_only_declared_geodataframe_param():
    """GeoDataFrame 参数仍按元数据从 FeatureCollection 转换。"""
    import geopandas as gpd
    import operators
    from workflow_engine import execute_single_operator

    def direct_geodataframe(input_gdf):
        assert isinstance(input_gdf, gpd.GeoDataFrame)
        return {"rows": len(input_gdf)}

    operators.OPERATORS["direct_geodataframe"] = {
        "function": direct_geodataframe,
        "param_schema": [{
            "name": "input_gdf",
            "type": "GeoDataFrame",
            "param_type": "input",
            "required": True,
            "description": "geometry input",
        }],
    }
    try:
        result = execute_single_operator("direct_geodataframe", {
            "input_gdf": {
                "type": "FeatureCollection",
                "features": [{
                    "type": "Feature",
                    "geometry": {"type": "Point", "coordinates": [120, 30]},
                    "properties": {"name": "point"},
                }],
            },
        })
    finally:
        operators.OPERATORS.pop("direct_geodataframe", None)

    assert result["status"] == "success", result.get("error")
    assert result["result"]["rows"] == 1

def main():
    """运行所有测试"""
    print("GeoPython Workflow 功能测试\n")

    tests = [
        test_imports,
        test_operators,
        test_workflow_engine,
        test_dag_sorting,
        test_workflow_engine_json_result,
        test_workflow_definition_requires_params_object_and_string_dependencies,
        test_direct_operator_json_result,
        test_direct_operator_preserves_object_params_containing_features_text,
        test_direct_operator_converts_only_declared_geodataframe_param
    ]

    results = []
    for test_func in tests:
        try:
            test_func()
            results.append(True)
        except Exception as e:
            print(f"❌ 测试异常: {e}")
            results.append(False)

    # 统计结果
    passed = sum(results)
    total = len(results)

    print(f"\n{'='*50}")
    print(f"测试完成: {passed}/{total} 通过")
    print(f"{'='*50}")

    if passed == total:
        print("✅ 所有测试通过！GeoPython Workflow 已就绪。")
        return 0
    else:
        print(f"❌ {total - passed} 个测试失败")
        return 1

if __name__ == "__main__":
    sys.exit(main())


def test_raster_resource_policy_hot_admission_and_restart_cache(monkeypatch):
    import pytest
    import raster_resources as module
    from osgeo import gdal
    payload = {'policy': {'engine_id': 1, 'version': 1, 'running': 2, 'waiting': 2,
                         'cache_mib': 256, 'default_tenant_running': 2, 'default_tenant_waiting': 2}, 'quotas': []}
    class Client:
        def fetch(self):
            return payload
    applied = []
    monkeypatch.setattr(gdal, 'SetCacheMax', applied.append)
    resources = module.RasterResources(Client, poll=False)
    assert not resources.snapshot()['enabled']
    assert resources.snapshot()['running_limit'] is None and resources.snapshot()['waiting_limit'] is None
    resources.refresh()
    assert applied == [256 * 1048576]
    payload['policy'].update(version=2, running=1, waiting=0, default_tenant_running=1, default_tenant_waiting=0, cache_mib=64)
    resources.refresh()
    facts = resources.snapshot()
    assert facts['applied_version'] == 2 and facts['running_limit'] == 1 and facts['pending_restart']
    assert applied == [256 * 1048576]
    payload['policy']['running'] = True
    with pytest.raises(module.RasterPolicyUnavailable):
        resources.refresh()
    assert not resources.snapshot()['enabled']
    payload['policy']['running'] = 1
    resources.refresh()
    assert resources.snapshot()['enabled']
    payload['policy']['version'] = 1
    with pytest.raises(module.RasterPolicyUnavailable):
        resources.refresh()


def test_raster_resource_observation_uses_strictest_container_ancestor(tmp_path, monkeypatch):
    import raster_resources as module
    root = tmp_path / 'cgroups'
    leaf = root / 'parent' / 'worker'
    leaf.mkdir(parents=True)
    (root / 'cpu.max').write_text('max 100000')
    (leaf.parent / 'cpu.max').write_text('150000 100000')
    (leaf / 'cpu.max').write_text('200000 100000')
    (root / 'memory.max').write_text('max')
    (leaf.parent / 'memory.max').write_text(str(1024 * 1048576))
    (leaf / 'memory.max').write_text(str(2048 * 1048576))
    proc = tmp_path / 'proc'
    proc.write_text('malformed\n0::/parent/worker\n')
    monkeypatch.setattr(module.os, 'cpu_count', lambda: 8)
    monkeypatch.setattr(module.os, 'sched_getaffinity', lambda _: set(range(4)), raising=False)
    monkeypatch.setattr(module.os, 'sysconf', lambda key: 4096 if key == 'SC_PAGE_SIZE' else 1048576)
    facts = module.resource_observation(cgroup_root=root, proc_cgroup=proc)
    assert facts['effective_cpu'] == 1.5 and facts['memory_limit_bytes'] == 1024 * 1048576
    assert facts['advice'] == {'running': 1, 'waiting': 1, 'cache_mib': 32, 'basis': 'cpu-and-memory-budget-heuristic'}
    (leaf.parent / 'memory.max').write_text(str(256 * 1048576))
    assert module.resource_observation(cgroup_root=root, proc_cgroup=proc)['advice'] is None


def test_raster_resource_observation_unknown_cpu_is_not_fabricated(tmp_path, monkeypatch):
    import raster_resources as module
    monkeypatch.setattr(module.os, 'cpu_count', lambda: None)
    def unknown(_):
        raise OSError('unknown')
    monkeypatch.setattr(module.os, 'sched_getaffinity', unknown, raising=False)
    monkeypatch.setattr(module.os, 'sysconf', lambda _: -1)
    # Unknown physical memory must stay unknown even if sysconf returned a negative sentinel.
    facts = module.resource_observation(cgroup_root=tmp_path, proc_cgroup=tmp_path / 'missing')
    assert facts['effective_cpu'] is None and facts['memory_limit_bytes'] is None and facts['advice'] is None


def test_raster_policy_rejects_foreign_engine_quota_and_clamps_saved_quota(monkeypatch):
    import pytest
    import raster_resources as module
    from osgeo import gdal
    monkeypatch.setattr(gdal, 'SetCacheMax', lambda _: None)
    payload = {'policy': {'engine_id': 1, 'version': 1, 'running': 1, 'waiting': 0,
        'cache_mib': 256, 'default_tenant_running': 1, 'default_tenant_waiting': 0},
        'quotas': [{'engine_id': 1, 'tenant_id': 7, 'version': 1, 'running': 2, 'waiting': 2}]}
    class Client:
        def fetch(self): return payload
    resources = module.RasterResources(Client, poll=False)
    resources.refresh()
    with resources.admission.direct(7):
        with pytest.raises(module.RuntimeBusy):
            with resources.admission.direct(7): pass
    payload['quotas'][0]['engine_id'] = 2
    with pytest.raises(module.RasterPolicyUnavailable): resources.refresh()
    assert not resources.snapshot()['enabled']


def test_geopython_workflow_preserves_publisher_conflict_code(tmp_path, monkeypatch):
    import operators
    from addp_common.workflow_access import publish_target_file
    from workflow_engine import execute_workflow
    source, target = tmp_path / 'source', tmp_path / 'target'
    source.write_bytes(b'new'); target.write_bytes(b'original')
    plan = {'target': {'kind': 'file', 'write_mode': 'create',
                       'access': {'method': 'mounted_path', 'path': str(target)}}}
    monkeypatch.setitem(operators.OPERATORS, 'publish_conflict', {
        'function': lambda: publish_target_file(source, plan), 'execution_modes': ['workflow']})
    response = execute_workflow({'tasks': [
        {'id': 'publish', 'operator': 'publish_conflict', 'params': {}, 'depends_on': []}]})
    assert response['status'] == 'failed'
    assert response['error_code'] == 'TARGET_ALREADY_EXISTS'
    assert response['all_results'] == {}
    assert target.read_bytes() == b'original'


def test_plain_dataframe_projection_preserves_int64_null_and_bounds_preview():
    import json
    import pandas as pd
    from workflow_engine import _project
    frame=pd.DataFrame({'zone_value':pd.Series([2**53+1]*51,dtype='int64'),
                        'mean':pd.Series([None]*51,dtype='Float64')})
    projected=json.loads(json.dumps(_project(frame),allow_nan=False))
    assert projected['row_count']==51 and projected['preview_limit']==50 and projected['truncated'] is True
    assert len(projected['preview_rows'])==50
    assert projected['preview_rows'][0]=={'zone_value':2**53+1,'mean':None}
    empty=_project(frame.head(0))
    assert empty['row_count']==0 and empty['preview_rows']==[] and empty['truncated'] is False
    assert empty['schema']==projected['schema']


@pytest.mark.parametrize('memory', ['physical', 'oserror', 'valueerror', 'unknown', 'cgroup'])
def test_resource_memory_observation_never_starts_process(tmp_path, monkeypatch, memory):
    import os
    import subprocess
    import raster_resources as module
    attempts = []
    def forbidden(*args, **kwargs):
        attempts.append(True)
        raise AssertionError('health observation must stay in process')
    monkeypatch.setattr(subprocess, 'Popen', forbidden)
    def sysconf(key):
        if memory in ('oserror', 'cgroup'): raise OSError('unknown physical memory')
        if memory == 'valueerror': raise ValueError('unknown physical memory')
        return -1 if memory == 'unknown' else 4096 if key == 'SC_PAGE_SIZE' else 1048576
    monkeypatch.setattr(os, 'sysconf', sysconf)
    root = tmp_path / 'cgroup'; root.mkdir()
    proc = tmp_path / 'proc'
    if memory == 'cgroup':
        proc.write_text('0::/\n'); (root / 'memory.max').write_text(str(1024**3))
    fact = module.resource_observation(cgroup_root=root, proc_cgroup=proc)
    assert not attempts
    assert fact['memory_limit_bytes'] == (4096 * 1048576 if memory == 'physical' else 1024**3 if memory == 'cgroup' else None)
    if memory in ('oserror', 'valueerror', 'unknown'): assert fact['advice'] is None


@pytest.mark.parametrize('route', ['extent', 'facts', 'progress', 'vector'])
def test_native_runtime_spawn_route_never_forks_loaded_gis(tmp_path, monkeypatch, route):
    import os
    import subprocess
    import sys
    import threading
    from pyproj import Transformer
    from operators import get_operator
    from operators import vector_tile_operators as tiles
    Transformer.from_crs(4326, 3857, always_xy=True).transform(112, 28)
    stop = threading.Event(); active = threading.Thread(target=stop.wait); active.start()
    calls = []
    native_spawn = os.posix_spawn
    def spawn(*args, **kwargs):
        calls.append(args[0]); return native_spawn(*args, **kwargs)
    def forbidden(*args, **kwargs): raise AssertionError('unsafe fork attempted after GIS initialization')
    monkeypatch.setattr(os, 'posix_spawn', spawn)
    monkeypatch.setattr(os, 'fork', forbidden)
    monkeypatch.setattr(subprocess, '_fork_exec', forbidden)
    source = tmp_path / 'points.geojson'
    source.write_text('{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"name":"长沙"},"geometry":{"type":"Point","coordinates":[112,28]}}]}')
    try:
        if route == 'extent':
            result = tiles._extent_to_wgs84([0, 0, 1000, 1000], 3857)
            assert result[0] == result[1] == 0 and 0 < result[2] < 1
        elif route == 'facts':
            layer, fields, geometry = tiles._vector_source_facts(str(source), {}, {}, 'EPSG:4326')
            assert layer == 'points' and fields == {'name': 'String'} and geometry
        elif route == 'progress':
            events = []
            tiles._run_command_with_gdal_progress([sys.executable, '-c', "print('0...50...100 - done.')"], {}, events.append, 0, 1)
            assert [e['progress_percent'] for e in events] == [0, 50, 100]
        else:
            exchange = tmp_path / 'sites.kml'
            exchange.write_text('<kml xmlns="http://www.opengis.net/kml/2.2"><Document><Placemark><name>长沙</name><Point><coordinates>112,28</coordinates></Point></Placemark></Document></kml>')
            result = get_operator('vector_dataset.inspect')(access_plan={'schema_version':'addp.workflow.access-plan/v1','source':{'kind':'file','format':'kml','access':{'method':'mounted_path','path':str(exchange)}}})
            assert result['container']['children']
        assert len(calls) == 1
    finally:
        stop.set(); active.join(5)


@pytest.mark.parametrize('text', [False, True])
@pytest.mark.parametrize('short_command', [False, True])
def test_native_process_pipes_environment_cwd_and_descriptor_isolation(tmp_path, monkeypatch, text, short_command):
    import fcntl
    import hashlib
    import json
    import os
    import subprocess
    import sys
    from pathlib import Path
    from operators import _native_process as native
    sentinel = tmp_path / 'private-parent-only'; sentinel.write_text('private descriptor')
    with sentinel.open() as stream:
        leaked = fcntl.fcntl(stream.fileno(), fcntl.F_DUPFD, 100)
    os.set_inheritable(leaked, True)
    request = 'stdin-private-secret:' + '数' * 600000
    payload = request if text else request.encode()
    env = {'PATH':str(Path(sys.executable).parent), 'ONLY_CHILD':'isolated'}
    monkeypatch.setenv('PARENT_ONLY_SECRET', 'must-not-leak')
    calls = []
    spawn = os.posix_spawn
    def observed(file, args, values, **kwargs):
        assert 'stdin-private-secret' not in repr(args)
        calls.append(args); return spawn(file, args, values, **kwargs)
    monkeypatch.setattr(os, 'posix_spawn', observed)
    script = '''
import hashlib,json,os,sys
try: os.fstat(int(sys.argv[1])); closed=False
except OSError: closed=True
value=sys.stdin.buffer.read()
print(json.dumps({'cwd':os.getcwd(),'closed':closed,'only':os.environ.get('ONLY_CHILD'),'private':os.environ.get('PARENT_ONLY_SECRET'),'sha256':hashlib.sha256(value).hexdigest()}))
sys.stdout.write('o'*2000000)
sys.stderr.write('e'*1000000)
'''
    try:
        result = native.run([Path(sys.executable).name if short_command else sys.executable, '-c', script, str(leaked)],
            input=payload, capture_output=True, text=text, cwd=tmp_path, env=env, timeout=10, check=True)
        stdout = result.stdout if text else result.stdout.decode()
        facts, large = stdout.split('\n', 1)
        facts = json.loads(facts)
        assert facts == {'cwd':str(tmp_path.resolve()), 'closed':True, 'only':'isolated', 'private':None, 'sha256':hashlib.sha256(request.encode()).hexdigest()}
        assert large == 'o' * 2000000
        assert result.stderr == ('e' * 1000000 if text else b'e' * 1000000)
        assert len(calls) == 1 and env == {'PATH':str(Path(sys.executable).parent),'ONLY_CHILD':'isolated'}
    finally: os.close(leaked)


@pytest.mark.parametrize('problem', ['exit', 'timeout', 'missing', 'cwd', 'spawn'])
def test_native_process_failures_reap_children_and_preserve_errors(tmp_path, monkeypatch, problem):
    import os
    import signal
    import subprocess
    import sys
    from operators import _native_process as native
    before = set(os.listdir('/dev/fd'))
    processes = []
    start = native.start
    def observed(*args, **kwargs):
        process = start(*args, **kwargs); processes.append(process); return process
    monkeypatch.setattr(native, 'start', observed)
    if problem == 'exit':
        with pytest.raises(subprocess.CalledProcessError) as caught:
            native.run([sys.executable, '-c', "import sys;print('output');print('error',file=sys.stderr);sys.exit(23)"], capture_output=True, text=True, check=True)
        assert caught.value.returncode == 23 and caught.value.stdout == 'output\n' and caught.value.stderr == 'error\n'
    elif problem == 'timeout':
        with pytest.raises(subprocess.TimeoutExpired) as caught:
            native.run([sys.executable, '-c', "import time; print('ready',flush=True); time.sleep(30)"], capture_output=True, text=True, timeout=.5)
        assert caught.value.output == b'ready\n' and processes[0].returncode == -signal.SIGKILL
    elif problem == 'missing':
        with pytest.raises(FileNotFoundError): native.run(['definitely-no-addp-executable'], capture_output=True)
    elif problem == 'cwd':
        with pytest.raises(FileNotFoundError): native.run([sys.executable, '-c', 'pass'], cwd=tmp_path/'missing')
    else:
        def unavailable(*args, **kwargs): raise OSError('spawn unavailable; no fork fallback')
        monkeypatch.setattr(os, 'posix_spawn', unavailable)
        with pytest.raises(OSError, match='spawn unavailable'): native.run([sys.executable, '-c', 'pass'], capture_output=True)
    for process in processes:
        assert process.poll() is not None
        with pytest.raises(ChildProcessError): os.waitpid(process.pid, os.WNOHANG)
    assert set(os.listdir('/dev/fd')) == before


def test_native_process_streams_progress_before_exit_and_terminates(tmp_path):
    import subprocess
    import sys
    from operators import _native_process as native
    from operators.vector_tile_operators import _run_command_with_gdal_progress
    marker = tmp_path/'done'
    events = []
    def emit(event):
        events.append(event)
        if event['progress_percent'] < 100: assert not marker.exists()
    _run_command_with_gdal_progress([sys.executable, '-c',
        "import sys,time,pathlib;print('0...',end='',flush=True);time.sleep(.1);print('50...',end='',flush=True);time.sleep(.1);pathlib.Path(sys.argv[1]).touch();print('100 - done.',flush=True)",str(marker)], {}, emit, 0, 1)
    assert [e['progress_percent'] for e in events] == [0,50,100]
    with native.start([sys.executable,'-c','import time;time.sleep(30)'], stdout=subprocess.PIPE) as process:
        process.terminate(); assert process.wait(5) < 0


def test_health_unknown_memory_observation_is_healthy_without_subprocess(tmp_path, monkeypatch):
    import os
    import subprocess
    import api_server
    import raster_resources as module
    observe = module.resource_observation
    def unknown(_): raise OSError('physical memory unavailable')
    def forbidden(*args, **kwargs): raise AssertionError('health must not create a process')
    monkeypatch.setattr(os, 'sysconf', unknown)
    monkeypatch.setattr(subprocess, 'Popen', forbidden)
    monkeypatch.setattr(module, 'resource_observation', lambda: observe(cgroup_root=tmp_path, proc_cgroup=tmp_path/'missing'))
    with api_server.app.test_client() as client: response = client.get('/health')
    assert response.status_code == 200 and response.get_json()['status'] == 'healthy'
    assert response.get_json()['raster_resources']['memory_limit_bytes'] is None
    assert response.get_json()['raster_resources']['advice'] is None


def test_native_process_progress_error_terminates_and_reaps(tmp_path, monkeypatch):
    import os
    import subprocess
    import sys
    from operators import _native_process as native
    from operators.vector_tile_operators import _run_command_with_gdal_progress
    processes = []
    start = native.start
    def observed(*args, **kwargs):
        process = start(*args, **kwargs); processes.append(process); return process
    def fail(_): raise RuntimeError('progress consumer failed')
    monkeypatch.setattr(native, 'start', observed)
    try:
        with pytest.raises(RuntimeError, match='progress consumer failed'):
            _run_command_with_gdal_progress([sys.executable,'-c',"import time;print('0...',end='',flush=True);time.sleep(30)"],{},fail,0,1)
        assert len(processes) == 1 and processes[0].poll() is not None
        assert processes[0].stdout.closed
        with pytest.raises(ChildProcessError): os.waitpid(processes[0].pid, os.WNOHANG)
    finally:
        for process in processes:
            if process.poll() is None: process.kill()
            process.wait(5)
            if process.stdout is not None: process.stdout.close()


def _distance_to_frames(source_name='geometry', target_name='geometry', crs='EPSG:3857'):
    import geopandas as gpd
    import pandas as pd
    from shapely.geometry import Point
    source=gpd.GeoDataFrame({'source_key':['second','first'],
        'grade':pd.Series([None,2**53+1],dtype='Int64'),
        'enabled':pd.Series([True,None],dtype='boolean')},
        geometry=[Point(0,0),Point(9,12)],crs=crs)
    source.index=pd.Index([109,103],name='original_row')
    target=gpd.GeoDataFrame({'target_key':['near','far']},geometry=[Point(3,4),Point(30,40)],crs=crs)
    if source_name!='geometry':source=source.rename_geometry(source_name)
    if target_name!='geometry':target=target.rename_geometry(target_name)
    return source,target


@pytest.mark.parametrize('source_name,target_name',[
    ('geometry','geometry'),('route_shape','geometry'),('geometry','reference_shape'),('route_shape','reference_shape')])
def test_distance_to_active_geometry_columns_keep_values_schema_and_both_sources(source_name,target_name):
    from geopandas.testing import assert_geodataframe_equal
    from operators import get_operator
    source,target=_distance_to_frames(source_name,target_name)
    source_before,target_before=source.copy(deep=True),target.copy(deep=True)
    result=get_operator('distance_to')(source,target)
    assert result['distance'].tolist()==[5.,10.]
    assert result.geometry.name==source_name and result.crs==source.crs
    assert result.index.equals(source.index) and result.index.name==source.index.name
    assert_geodataframe_equal(result.drop(columns='distance'),source_before)
    assert_geodataframe_equal(source,source_before);assert_geodataframe_equal(target,target_before)


def test_distance_to_uses_active_geometry_not_decoy_named_geometry_columns():
    import geopandas as gpd
    from shapely.geometry import Point
    from geopandas.testing import assert_geodataframe_equal
    from operators import get_operator
    source,target=_distance_to_frames('route_shape','reference_shape')
    source['geometry']=gpd.GeoSeries([Point(100,100),Point(101,101)],index=source.index,crs=source.crs)
    target['geometry']=gpd.GeoSeries([Point(100,100),Point(200,200)],index=target.index,crs=target.crs)
    source_before,target_before=source.copy(deep=True),target.copy(deep=True)
    result=get_operator('distance_to')(source,target)
    assert result['distance'].tolist()==[5.,10.]
    assert result.geometry.name=='route_shape'
    assert_geodataframe_equal(result.drop(columns='distance'),source_before)
    assert_geodataframe_equal(source,source_before);assert_geodataframe_equal(target,target_before)


@pytest.mark.parametrize('crs', ['EPSG:3857', 'EPSG:2263'])
def test_distance_to_preserves_projected_native_units_without_reprojection(crs):
    from operators import get_operator
    source,target=_distance_to_frames('route_shape','reference_shape',crs)
    result=get_operator('distance_to')(source,target)
    assert result['distance'].tolist()==[5.,10.]
    assert result.crs==source.crs and source.crs==target.crs


def test_distance_to_active_geometry_empty_batch_keeps_source_schema():
    from geopandas.testing import assert_geodataframe_equal
    from operators import get_operator
    source,target=_distance_to_frames('route_shape','reference_shape');source=source.iloc[:0].copy()
    result=get_operator('distance_to')(source,target)
    assert result.empty and 'distance' in result.columns and result.geometry.name=='route_shape'
    assert_geodataframe_equal(result.drop(columns='distance'),source)


def test_distance_to_active_geometry_overwrites_distance_on_copy_only():
    from geopandas.testing import assert_geodataframe_equal
    from operators import get_operator
    source,target=_distance_to_frames('route_shape','reference_shape');source['distance']=[-999.,-888.]
    before=source.copy(deep=True);result=get_operator('distance_to')(source,target)
    assert result['distance'].tolist()==[5.,10.] and source['distance'].tolist()==[-999.,-888.]
    assert result.columns.equals(source.columns) and result.geometry.name=='route_shape'
    assert_geodataframe_equal(result.drop(columns='distance'),source.drop(columns='distance'))
    assert_geodataframe_equal(source,before)


@pytest.mark.parametrize('empty', [False, True])
def test_distance_to_active_geometry_distance_conflict_preserves_inputs_and_can_be_renamed(empty):
    from geopandas.testing import assert_geodataframe_equal
    from operators import get_operator
    from operators.spatial_relations import DistanceInputError

    source, target = _distance_to_frames('distance', 'reference_shape')
    if empty:
        source = source.iloc[:0].copy()
    source_before, target_before = source.copy(deep=True), target.copy(deep=True)
    with pytest.raises(DistanceInputError, match='请先手动重命名源几何列'):
        get_operator('distance_to')(source, target)
    assert_geodataframe_equal(source, source_before)
    assert_geodataframe_equal(target, target_before)

    renamed = source.rename_geometry('source_shape')
    result = get_operator('distance_to')(renamed, target)
    assert result.geometry.name == 'source_shape'
    assert result['distance'].tolist() == ([] if empty else [5., 10.])
    assert_geodataframe_equal(result.drop(columns='distance'), renamed)


def test_distance_to_active_geometry_target_named_distance_is_allowed():
    from geopandas.testing import assert_geodataframe_equal
    from operators import get_operator

    source, target = _distance_to_frames('source_shape', 'distance')
    before = target.copy(deep=True)
    result = get_operator('distance_to')(source, target)
    assert result['distance'].tolist() == [5., 10.]
    assert_geodataframe_equal(result.drop(columns='distance'), source)
    assert_geodataframe_equal(target, before)


@pytest.mark.parametrize('language,expected', [
    ('zh-cn', '源图层的活动几何列名 distance 与距离输出列冲突；请先手动重命名源几何列，再执行距离计算。'),
    ('en', 'Source active geometry column distance conflicts with the distance output column; '
           'rename the source geometry column before running distance calculation.'),
])
def test_distance_to_active_geometry_conflict_uses_submitted_workflow_language(language, expected):
    from addp_common.workflow_runtime import WorkflowExecutionError
    from workflow_engine import GeoPythonWorkflowRunner

    source, target = _distance_to_frames('distance', 'target_shape')
    workflow = {'tasks': [{'id': 'distance', 'operator': 'distance_to', 'params': {
        'input_gdf': {'$input': 'source'}, 'target_geom': {'$input': 'target'},
    }, 'depends_on': []}]}
    runner = GeoPythonWorkflowRunner(language=language)
    with pytest.raises(WorkflowExecutionError) as failure:
        runner.execute(workflow, {'source': source, 'target': target})
    assert str(failure.value).endswith(expected)
    assert failure.value.error_code == 'INVALID_PARAMS'
    assert failure.value.all_results == {}
    assert failure.value.task_order == ['distance']

    # A failed execution does not poison a later renamed input on the same runner.
    result = runner.execute(workflow, {'source': source.rename_geometry('shape'), 'target': target})
    import json
    assert [feature['properties']['distance'] for feature in json.loads(result.final_result)['features']] == [5., 10.]


@pytest.mark.parametrize('empty', [False, True])
@pytest.mark.parametrize('source_crs,target_crs,code', [
    (None, 'EPSG:3857', 'projected_crs'),
    ('EPSG:3857', None, 'projected_crs'),
    (None, None, 'projected_crs'),
    ('EPSG:4326', 'EPSG:4326', 'projected_crs'),
    ('EPSG:4326', 'EPSG:3857', 'projected_crs'),
    ('EPSG:3857', 'EPSG:4326', 'projected_crs'),
    ('OGC:CRS84', 'OGC:CRS84', 'projected_crs'),
    ('EPSG:4978', 'EPSG:4978', 'projected_crs'),
    ('EPSG:3857', 'EPSG:4978', 'projected_crs'),
    ('EPSG:3857', 'EPSG:32650', 'crs_mismatch'),
    ('EPSG:3857', 'EPSG:2263', 'crs_mismatch'),
])
def test_distance_to_rejects_unknown_nonprojected_and_mismatched_crs_without_mutation(
    source_crs, target_crs, code, empty,
):
    from geopandas.testing import assert_geodataframe_equal
    from operators import get_operator
    from operators.spatial_relations import DistanceInputError

    source, _ = _distance_to_frames('route_shape', 'reference_shape', source_crs)
    _, target = _distance_to_frames('route_shape', 'reference_shape', target_crs)
    if empty:
        source = source.iloc[:0].copy()
    before_source, before_target = source.copy(deep=True), target.copy(deep=True)
    with pytest.raises(DistanceInputError) as error:
        get_operator('distance_to')(source, target)
    assert error.value.code == code
    assert '请先显式重投影' in str(error.value)
    assert_geodataframe_equal(source, before_source)
    assert_geodataframe_equal(target, before_target)


@pytest.mark.parametrize('side', ['source', 'target'])
def test_distance_to_rejects_projected_crs_with_mixed_horizontal_units(side):
    from pyproj import CRS
    from operators import get_operator
    from operators.spatial_relations import DistanceInputError

    definition = CRS.from_epsg(3857).to_json_dict()
    definition['coordinate_system']['axis'][1]['unit'] = {
        'type': 'LinearUnit', 'name': 'foot', 'conversion_factor': 0.3048,
    }
    mixed = CRS.from_json_dict(definition)
    assert mixed.is_projected
    assert mixed.axis_info[0].unit_conversion_factor != mixed.axis_info[1].unit_conversion_factor
    source, target = _distance_to_frames()
    if side == 'source':
        source = source.set_crs(mixed, allow_override=True)
    else:
        target = target.set_crs(mixed, allow_override=True)
    with pytest.raises(DistanceInputError) as error:
        get_operator('distance_to')(source, target)
    assert error.value.code == 'projected_crs'


def test_distance_to_accepts_semantically_equal_crs_expressions_without_changing_them():
    from geopandas.testing import assert_geodataframe_equal
    from operators import get_operator

    source, target = _distance_to_frames('source_shape', 'target_shape')
    target = target.set_crs(source.crs.to_wkt(version='WKT1_GDAL'), allow_override=True)
    before_source, before_target = source.copy(deep=True), target.copy(deep=True)
    assert source.crs.equals(target.crs)
    result = get_operator('distance_to')(source, target)
    assert result['distance'].tolist() == [5., 10.]
    assert_geodataframe_equal(result.drop(columns='distance'), before_source)
    assert_geodataframe_equal(source, before_source)
    assert_geodataframe_equal(target, before_target)


@pytest.mark.parametrize('language,projected_message,mismatch_message', [
    ('zh-cn', '距离计算要求两个输入均具有已知的投影坐标系', '距离计算要求两个输入使用相同的投影坐标系'),
    ('en', 'Distance calculation requires both inputs to have a known projected CRS',
           'Distance calculation requires the same projected CRS for both inputs'),
])
@pytest.mark.parametrize('target_crs,code', [('EPSG:4326', 'projected_crs'), ('EPSG:32650', 'crs_mismatch')])
def test_distance_to_crs_error_uses_submitted_workflow_language(
    language, projected_message, mismatch_message, target_crs, code,
):
    import json
    from addp_common.workflow_runtime import WorkflowExecutionError
    from workflow_engine import GeoPythonWorkflowRunner

    source, good_target = _distance_to_frames('source_shape', 'target_shape')
    _, bad_target = _distance_to_frames('source_shape', 'target_shape', target_crs)
    workflow = {'tasks': [{'id': 'distance', 'operator': 'distance_to', 'params': {
        'input_gdf': {'$input': 'source'}, 'target_geom': {'$input': 'target'},
    }, 'depends_on': []}]}
    runner = GeoPythonWorkflowRunner(language=language)
    with pytest.raises(WorkflowExecutionError) as failure:
        runner.execute(workflow, {'source': source, 'target': bad_target})
    assert (projected_message if code == 'projected_crs' else mismatch_message) in str(failure.value)
    assert failure.value.error_code == 'INVALID_PARAMS'
    assert failure.value.all_results == {} and failure.value.task_order == ['distance']
    result = runner.execute(workflow, {'source': source, 'target': good_target})
    assert [feature['properties']['distance'] for feature in json.loads(result.final_result)['features']] == [5., 10.]


def _multi_distance_source():
    import geopandas as gpd
    import pandas as pd
    from shapely.geometry import Point
    source = pd.DataFrame({
        'source_id': pd.array([9007199254740993, None], dtype='Int64'),
        'enabled': pd.array([True, None], dtype='boolean'),
        'label': pd.array(['甲', None], dtype='string'),
        'geometry': ['ordinary attribute', 'not active geometry'],
    })
    source['shape'] = gpd.GeoSeries([Point(0, 0), Point(10, 0)], crs=3857)
    source = gpd.GeoDataFrame(source, geometry='shape', crs=3857)
    source.index = [8, 8]
    return source


def test_multi_distance_buffer_single_layer_order_types_and_source_unchanged():
    import pandas as pd
    from geopandas.testing import assert_geodataframe_equal
    from operators.data_operations import batch_buffer
    from operators.geometric_operators import buffer
    source = _multi_distance_source()
    before = source.copy(deep=True)
    result = batch_buffer(source, [2, 1, 2], resolution=1)
    assert result.geometry.name == 'shape' and result.crs == source.crs
    assert result.index.equals(pd.RangeIndex(6))
    assert result.columns.tolist() == [*source.columns, 'distance']
    assert str(result['distance'].dtype) == 'float64'
    assert result['distance'].tolist() == [2., 2., 1., 1., 2., 2.]
    for offset, distance in enumerate([2, 1, 2]):
        expected = buffer(source, distance, resolution=1).reset_index(drop=True)
        actual = result.iloc[offset * 2:(offset + 1) * 2].drop(columns='distance').reset_index(drop=True)
        assert_geodataframe_equal(actual, expected)
        assert actual.geometry.to_wkb().tolist() == expected.geometry.to_wkb().tolist()
    assert result.source_id.dtype == source.source_id.dtype
    assert result.enabled.dtype == source.enabled.dtype
    assert result.label.dtype == source.label.dtype
    assert_geodataframe_equal(source, before)


@pytest.mark.parametrize('empty_source,distances', [(False, []), (True, [1, 2]), (True, [])])
def test_multi_distance_buffer_empty_schema(empty_source, distances):
    import pandas as pd
    from geopandas.testing import assert_geodataframe_equal
    from operators.data_operations import batch_buffer
    source = _multi_distance_source()
    if empty_source:
        source = source.iloc[:0].copy()
    before = source.copy(deep=True)
    result = batch_buffer(source, distances)
    assert len(result) == 0 and result.crs == source.crs and result.geometry.name == 'shape'
    assert result.columns.tolist() == [*source.columns, 'distance']
    assert str(result['distance'].dtype) == 'float64' and result.index.equals(pd.RangeIndex(0))
    assert result.drop(columns='distance').dtypes.equals(source.dtypes)
    assert_geodataframe_equal(source, before)


def test_multi_distance_buffer_zero_negative_and_full_buffers_not_rings():
    import geopandas as gpd
    from shapely.geometry import box
    from operators.data_operations import batch_buffer
    from operators.geometric_operators import buffer
    source = gpd.GeoDataFrame({'id': [1]}, geometry=[box(0, 0, 4, 4)], crs=3857)
    result = batch_buffer(source, [0, -1, -10, 1, 2], resolution=1)
    assert len(result) == 5 and result['distance'].tolist() == [0., -1., -10., 1., 2.]
    assert result.geometry.iloc[0].equals(source.geometry.iloc[0])
    assert result.geometry.iloc[2].is_empty
    for index, distance in enumerate(result['distance']):
        assert result.geometry.iloc[index].equals_exact(buffer(source, distance, 1).geometry.iloc[0], 0)
    assert result.geometry.iloc[4].contains(result.geometry.iloc[3])


@pytest.mark.parametrize('distances', [None, '1,2', '[1,2]', 1, [True], [False], [None], ['1'], [float('nan')], [float('inf')], [-float('inf')], [10 ** 400], [1, {'value': 2}]])
def test_multi_distance_buffer_rejects_invalid_distances_even_empty_source(distances):
    from operators.data_operations import batch_buffer, MultiBufferInputError
    source = _multi_distance_source().iloc[:0]
    with pytest.raises(MultiBufferInputError) as failure:
        batch_buffer(source, distances)
    assert failure.value.code == 'distances'


@pytest.mark.parametrize('resolution', [None, True, False, 0, -1, 1.5, '16', float('inf')])
def test_multi_distance_buffer_rejects_invalid_resolution(resolution):
    from operators.data_operations import batch_buffer, MultiBufferInputError
    with pytest.raises(MultiBufferInputError) as failure:
        batch_buffer(_multi_distance_source(), [1], resolution)
    assert failure.value.code == 'resolution'


@pytest.mark.parametrize('geometry_conflict', [True, False])
@pytest.mark.parametrize('empty', [True, False])
def test_multi_distance_buffer_conflicting_column_rejected_before_computation(geometry_conflict, empty):
    from geopandas.testing import assert_geodataframe_equal
    from operators.data_operations import batch_buffer, MultiBufferInputError
    source = _multi_distance_source()
    if geometry_conflict:
        source = source.rename_geometry('distance')
    else:
        source['distance'] = [8., 9.]
    if empty:
        source = source.iloc[:0].copy()
    before = source.copy(deep=True)
    with pytest.raises(MultiBufferInputError) as failure:
        batch_buffer(source, [])
    assert failure.value.code == 'distance_conflict'
    assert_geodataframe_equal(source, before)


def test_multi_distance_buffer_requires_active_geometry_and_unique_columns():
    import geopandas as gpd
    import pandas as pd
    from operators.data_operations import batch_buffer, MultiBufferInputError
    duplicate = _multi_distance_source()
    duplicate.columns = ['x', 'x', 'label', 'geometry', 'shape']
    for source in (pd.DataFrame({'x': [1]}), gpd.GeoDataFrame({'x': [1]}), duplicate):
        with pytest.raises(MultiBufferInputError) as failure:
            batch_buffer(source, [1])
        assert failure.value.code == 'source'


@pytest.mark.parametrize('crs', [None, 'EPSG:2263'])
def test_multi_distance_buffer_keeps_native_crs_policy_and_units(crs):
    from operators.data_operations import batch_buffer
    source = _multi_distance_source().set_crs(crs, allow_override=True)
    result = batch_buffer(source, [2], resolution=1)
    assert result.crs == source.crs
    assert result.geometry.iloc[0].bounds == (-2., -2., 2., 2.)


def _assert_multi_distance_saved_geometry(actual, computed_wkb, centre, distance):
    from math import hypot
    from osgeo import ogr

    # Persistence must retain every coordinate bit, including signed zero.
    assert bytes(actual.ExportToWkb()) == computed_wkb, 'buffer save changed computed WKB'
    assert actual.GetGeometryType() == ogr.wkbPolygon
    assert actual.GetCoordinateDimension() == 2 and actual.IsValid()
    assert actual.GetGeometryCount() == 1
    coordinates = actual.GetGeometryRef(0).GetPoints()
    assert len(coordinates) == 5 and coordinates[0] == coordinates[-1]
    x, y = centre
    # Independent closed-form checks for this resolution=1 point fixture.
    assert actual.GetEnvelope() == (x - distance, x + distance, y - distance, y + distance)
    assert actual.GetArea() == 2 * distance * distance
    assert all(hypot(px - x, py - y) == distance for px, py in coordinates)


@pytest.mark.parametrize('change', ['one_ulp', 'signed_zero'])
def test_multi_distance_buffer_physical_oracle_rejects_coordinate_bit_changes(change):
    from math import inf, nextafter
    from osgeo import ogr

    expected = ogr.CreateGeometryFromWkt('POLYGON ((2 0,0 -2,-2 0,0 2,2 0))')
    computed_wkb = bytes(expected.ExportToWkb())
    _assert_multi_distance_saved_geometry(expected, computed_wkb, (0., 0.), 2.)
    altered = expected.Clone()
    altered.GetGeometryRef(0).SetPoint_2D(1, nextafter(0., inf) if change == 'one_ulp' else -0., -2.)
    assert bytes(altered.ExportToWkb()) != computed_wkb
    assert altered.GetEnvelope() == expected.GetEnvelope()
    assert altered.GetArea() == expected.GetArea()
    assert altered.IsValid()
    with pytest.raises(AssertionError, match='buffer save changed computed WKB'):
        _assert_multi_distance_saved_geometry(altered, computed_wkb, (0., 0.), 2.)


@pytest.mark.parametrize('port', [None, 'default'])
@pytest.mark.parametrize('empty', [False, True])
def test_multi_distance_buffer_workflow_save_and_independent_ogr_reload(tmp_path, port, empty):
    import hashlib
    import json
    import geopandas as gpd
    from osgeo import ogr
    from geopandas.testing import assert_geodataframe_equal
    from workflow_engine import GeoPythonWorkflowRunner

    class RecordingRunner(GeoPythonWorkflowRunner):
        def _execute_operator(self, operator, params):
            outputs = super()._execute_operator(operator, params)
            if operator == 'batch_buffer':
                self.computed_wkb = outputs['__ports__']['default'].geometry.to_wkb().tolist()
            return outputs

    source = _multi_distance_source().drop(columns='geometry')
    # Use nullable scalar columns in a real GeoPackage, without a list-saving adapter.
    before = source.copy(deep=True)
    ref = {'$ref': 'multi'}
    if port:
        ref['port'] = port
    workflow = {'tasks': [
        {'id': 'multi', 'operator': 'batch_buffer', 'params': {'input_gdf': {'$input': 'source'},
         'distances': [] if empty else [2, 1, 2], 'resolution': 1}, 'depends_on': []},
        {'id': 'saved', 'operator': 'save', 'params': {'input_df': ref,
         'connection_info': {'engine_type': 'nfs', 'mount_path': str(tmp_path)},
         'path': 'buffers.gpkg', 'mode': 'fail'}, 'depends_on': ['multi']},
    ]}
    runner = RecordingRunner()
    outcome = runner.execute(workflow, {'source': source})
    assert len(runner.computed_wkb) == (0 if empty else 6)
    projected = json.loads(outcome.final_result)
    assert projected['type'] == 'FeatureCollection'
    path = tmp_path / 'buffers.gpkg'
    digest = hashlib.sha256(path.read_bytes()).hexdigest()
    dataset = ogr.Open(str(path))
    assert dataset and dataset.GetLayerCount() == 1
    layer = dataset.GetLayer(0)
    assert layer.GetSpatialRef().GetAuthorityCode(None) == '3857'
    assert layer.GetFeatureCount() == (0 if empty else 6)
    definition = layer.GetLayerDefn()
    assert definition.GetFieldDefn(definition.GetFieldIndex('source_id')).GetType() == ogr.OFTInteger64
    assert definition.GetFieldDefn(definition.GetFieldIndex('distance')).GetType() == ogr.OFTReal
    for index, feature in enumerate(layer):
        source_row = index % 2
        distance = [2., 1., 2.][index // 2]
        assert feature.GetField('source_id') == (9007199254740993 if source_row == 0 else None)
        assert feature.GetField('enabled') == (1 if source_row == 0 else None)
        assert feature.GetField('label') == ('甲' if source_row == 0 else None)
        assert feature.GetField('distance') == distance
        _assert_multi_distance_saved_geometry(
            feature.GetGeometryRef(), runner.computed_wkb[index], (source_row * 10., 0.), distance,
        )
    dataset = None
    assert hashlib.sha256(path.read_bytes()).hexdigest() == digest
    assert_geodataframe_equal(source, before)


@pytest.mark.parametrize('language,fragment', [('zh-cn', 'distance 与'), ('en', 'distance conflicts')])
def test_multi_distance_buffer_localized_error_does_not_publish_and_can_recover(tmp_path, language, fragment):
    from addp_common.workflow_runtime import WorkflowExecutionError
    from workflow_engine import GeoPythonWorkflowRunner
    source = _multi_distance_source()
    source['distance'] = [4., 5.]
    workflow = {'tasks': [
        {'id': 'multi', 'operator': 'batch_buffer', 'params': {'input_gdf': {'$input': 'source'}, 'distances': [1]}, 'depends_on': []},
        {'id': 'save', 'operator': 'save', 'params': {'input_df': {'$ref': 'multi'},
         'connection_info': {'engine_type': 'nfs', 'mount_path': str(tmp_path)}, 'path': 'conflict.gpkg', 'mode': 'fail'}, 'depends_on': ['multi']},
    ]}
    runner = GeoPythonWorkflowRunner(language=language)
    with pytest.raises(WorkflowExecutionError) as failure:
        runner.execute(workflow, {'source': source})
    assert fragment in str(failure.value)
    assert failure.value.all_results == {} and failure.value.error_code == 'INVALID_PARAMS'
    assert not (tmp_path / 'conflict.gpkg').exists()
    result = runner.execute(workflow, {'source': source.rename(columns={'distance': 'original_distance'})})
    assert 'original_distance' in __import__('json').loads(result.final_result)['features'][0]['properties']


def test_multi_distance_buffer_removes_multi_centroid_without_redirect():
    import operators.data_operations as data
    from operators import OPERATORS, get_operator, list_operators
    from addp_common.workflow_runtime import WorkflowValidationError
    from workflow_engine import GeoPythonWorkflowRunner
    assert 'batch_centroid' not in OPERATORS
    assert 'batch_centroid' not in __import__('operators').__all__
    assert not hasattr(data, 'batch_centroid') and not hasattr(data, 'BATCH_CENTROID_METADATA')
    assert all(item['id'] != 'batch_centroid' for item in list_operators())
    with pytest.raises(ValueError, match='Unknown operator: batch_centroid'):
        get_operator('batch_centroid')
    workflow = {'tasks': [{'id': 'removed', 'operator': 'batch_centroid', 'params': {}, 'depends_on': []}]}
    with pytest.raises(WorkflowValidationError):
        GeoPythonWorkflowRunner().execute(workflow)


@pytest.mark.parametrize('language,fragment', [('zh-cn', '多距缓冲的距离列表'), ('en', 'Multi-distance buffer distances')])
def test_multi_distance_buffer_public_workflow_language_metadata_and_context(monkeypatch, language, fragment):
    import json
    import time
    import api_server
    from addp_common.workflow_runtime import ExecutionRegistry
    monkeypatch.setattr(api_server, 'executions', ExecutionRegistry())
    source = _multi_distance_source().drop(columns='geometry')
    params = {'input_gdf': json.loads(source.to_json()), 'distances': [True]}
    with api_server.app.test_client() as client:
        operators = client.get('/api/operators').get_json()['operators']
        spec = next(item for item in operators if item['id'] == 'batch_buffer')
        distances = next(item for item in spec['parameters'] if item['name'] == 'distances')
        assert distances['type'] == 'array' and distances['item_type'] == 'float'
        assert not any(item['id'] == 'batch_centroid' for item in operators)
        assert client.post('/api/operators/batch_buffer/invoke', json={'params': params}).status_code == 403
        workflow = {'tasks': [{'id': 'multi', 'operator': 'batch_buffer', 'params': params, 'depends_on': []}]}
        assert client.post('/api/workflow', json={'workflow_def': workflow}).status_code == 400
        response = client.post('/api/workflow', headers={'Accept-Language': language}, json={
            'workflow_def': workflow,
            'runtime': {'tenant_id': 7, 'execution_authorization': {'id': 1, 'effects': ['read']}},
        })
        assert response.status_code == 202
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            state = client.get('/api/executions/' + response.get_json()['execution_id']).get_json()
            if state['status'] in {'success', 'failed'}:
                break
            time.sleep(.01)
        assert state['status'] == 'failed' and state['error_code'] == 'INVALID_PARAMS'
        assert fragment in state['error']


@pytest.mark.parametrize('language', ['zh-cn', 'en'])
@pytest.mark.parametrize('operator,code,reason,parameter,field', [
    ('batch_buffer','source','operator_input_invalid','input_gdf',''),
    ('batch_buffer','distance_conflict','operator_field_conflict','input_gdf','distance'),
    ('batch_buffer','distances','operator_number_array_invalid','distances',''),
    ('batch_buffer','resolution','operator_positive_integer_required','resolution',''),
    ('distance_to','geometry_column_conflict','operator_geometry_field_conflict','input_gdf','distance'),
    ('distance_to','projected_crs','operator_projected_crs_required','',''),
    ('distance_to','crs_mismatch','operator_crs_mismatch','',''),
    ('check_line_proximity','tolerance','operator_positive_number_required','tolerance',''),
    ('check_line_proximity','source','operator_input_invalid','input_gdf',''),
    ('check_line_proximity','reference','operator_reference_invalid','reference_gdf',''),
    ('check_line_proximity','crs','operator_crs_invalid','',''),
])
def test_operator_input_diagnostic_survives_async_http_snapshot(monkeypatch, language, operator, code, reason, parameter, field):
    import json
    import time
    import geopandas as gpd
    from shapely.geometry import Point, LineString
    import api_server
    from addp_common.workflow_runtime import ExecutionRegistry
    from workflow_engine import GeoPythonWorkflowRunner
    monkeypatch.setattr(api_server, 'executions', ExecutionRegistry())
    source = gpd.GeoDataFrame({'key':['source']}, geometry=[Point(0,0)],crs=3857)
    target = gpd.GeoDataFrame({'key':['target']}, geometry=[Point(1,0)],crs=3857)
    if operator == 'batch_buffer':
        params = {'input_gdf':source, 'distances':[1], 'resolution':1}
        if code == 'source': source.insert(0,'key',['duplicate'],allow_duplicates=True)
        if code == 'distance_conflict': source['distance'] = [7]
        if code == 'distances': params['distances'] = [True]
        if code == 'resolution': params['resolution'] = 0
    elif operator == 'distance_to':
        params = {'input_gdf':source,'target_geom':target}
        if code == 'geometry_column_conflict': params['input_gdf'] = source.rename_geometry('distance')
        if code == 'projected_crs': params['input_gdf'] = source.set_crs(4326,allow_override=True)
        if code == 'crs_mismatch': params['target_geom'] = target.set_crs(3395,allow_override=True)
    else:
        source = gpd.GeoDataFrame({'key':['s']},geometry=[LineString([(0,0),(1,0)])],crs=3857)
        target = gpd.GeoDataFrame({'key':['r']},geometry=[LineString([(0,1),(1,1)])],crs=3857)
        params = {'input_gdf':source,'reference_gdf':target,'id_field':'key','reference_id_field':'key','tolerance':.5}
        if code == 'tolerance':params['tolerance'] = 0
        if code == 'source':params['id_field'] = 'private_missing'
        if code == 'reference':params['reference_id_field'] = 'private_missing'
        if code == 'crs':params['reference_gdf'] = target.set_crs(3395,allow_override=True)
    # The HTTP route still performs admission/auth/context and runs the actual operator.
    # Inject only the transport's in-memory input conversion, which cannot carry a GDF in JSON.
    original = GeoPythonWorkflowRunner._execute_operator
    calls = []
    def execute(self, actual, submitted):
        calls.append(actual)
        return original(self,actual,params)
    monkeypatch.setattr(GeoPythonWorkflowRunner,'_execute_operator',execute)
    definition = {'tasks':[{'id':'private-node-password','operator':operator,'params':{},'depends_on':[]}]}
    with api_server.app.test_client() as client:
        response = client.post('/api/workflow',headers={'Accept-Language':language},json={
            'workflow_def':definition,'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read']}}})
        assert response.status_code == 202
        deadline = time.monotonic()+5
        while time.monotonic()<deadline:
            snapshot_response = client.get('/api/executions/'+response.get_json()['execution_id'])
            state = snapshot_response.get_json()
            if state['status']=='failed':break
            time.sleep(.01)
        assert snapshot_response.status_code == 200 and calls == [operator]
        assert state['status']=='failed' and state['error_code']=='INVALID_PARAMS'
        expected={'node_index':1,'operator_id':operator}
        if parameter: expected['parameter']=parameter
        if field: expected['field']=field
        assert state['diagnostic']=={'schema_version':'addp.execution-failure/v3','stage':'execution','reason':reason,'operator':expected}
        assert 'private' not in json.dumps(state['diagnostic'])
        assert not state.get('results')


def test_operator_numerical_failure_remains_execution_failure(monkeypatch):
    import workflow_engine
    from operators.geometric_operators import NearLineInputError
    from addp_common.workflow_runtime import WorkflowExecutionError
    def numerical_failure(*args): raise NearLineInputError('calculation')
    monkeypatch.setattr(workflow_engine,'_execute_workflow_operator',numerical_failure)
    with pytest.raises(WorkflowExecutionError) as caught:
        workflow_engine.GeoPythonWorkflowRunner().execute({'tasks':[{'id':'check','operator':'check_line_proximity','params':{},'depends_on':[]}]})
    assert caught.value.error_code=='EXECUTION_FAILED' and caught.value.diagnostic is None
