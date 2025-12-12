#!/usr/bin/env python3
"""
GeoPandas Engine Test Script
测试工作流引擎的核心功能
"""

import json
import sys

def test_imports():
    """测试依赖导入"""
    print("=== 测试 1: 依赖导入 ===")
    try:
        import geopandas as gpd
        import shapely
        from flask import Flask
        print("✅ 所有依赖导入成功")
        print(f"  - geopandas: {gpd.__version__}")
        print(f"  - shapely: {shapely.__version__}")
        return True
    except ImportError as e:
        print(f"❌ 导入失败: {e}")
        return False

def test_operators():
    """测试算子函数"""
    print("\n=== 测试 2: 算子库 ===")
    try:
        from operators import list_operators, get_operator
        operators = list_operators()
        print(f"✅ 成功加载 {len(operators)} 个空间算子")

        # 测试几个关键算子
        categories = {}
        for name, meta in operators.items():
            cat = meta['category']
            categories[cat] = categories.get(cat, 0) + 1

        print("算子分类统计:")
        for cat, count in categories.items():
            print(f"  - {cat}: {count} 个")

        return True
    except Exception as e:
        print(f"❌ 算子测试失败: {e}")
        return False

def test_workflow_engine():
    """测试工作流引擎"""
    print("\n=== 测试 3: 工作流引擎 ===")
    try:
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

        if result['status'] == 'success':
            print("✅ 工作流执行成功")
            print(f"  - 状态: {result['status']}")
            print(f"  - 结果类型: GeoJSON")
            print(f"  - 结果长度: {len(result['final_result'])} 字符")
            return True
        else:
            print(f"❌ 工作流执行失败: {result.get('error')}")
            return False

    except Exception as e:
        print(f"❌ 工作流引擎测试失败: {e}")
        import traceback
        traceback.print_exc()
        return False

def test_dag_sorting():
    """测试 DAG 拓扑排序"""
    print("\n=== 测试 4: DAG 拓扑排序 ===")
    try:
        from workflow_engine import execute_workflow

        # 测试复杂依赖链: t1 → t2 → t3
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

        if result['status'] == 'success':
            print("✅ DAG 拓扑排序成功")
            print(f"  - 执行了 3 个任务")
            print(f"  - 依赖链: t1 → t2 → t3")
            return True
        else:
            print(f"❌ DAG 测试失败: {result.get('error')}")
            return False

    except Exception as e:
        print(f"❌ DAG 测试失败: {e}")
        import traceback
        traceback.print_exc()
        return False

def main():
    """运行所有测试"""
    print("GeoPandas Engine 功能测试\n")

    tests = [
        test_imports,
        test_operators,
        test_workflow_engine,
        test_dag_sorting
    ]

    results = []
    for test_func in tests:
        try:
            results.append(test_func())
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
        print("✅ 所有测试通过！GeoPandas Engine 已就绪。")
        return 0
    else:
        print(f"❌ {total - passed} 个测试失败")
        return 1

if __name__ == "__main__":
    sys.exit(main())
