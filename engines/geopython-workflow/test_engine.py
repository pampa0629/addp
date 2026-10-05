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
