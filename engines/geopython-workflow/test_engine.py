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
