#!/usr/bin/env python3
"""
多输出端口功能测试
测试 split_by_area 算子和端口引用机制
"""

import json
from workflow_engine import execute_workflow

# 测试场景：使用 split_by_area 将数据分为两组，分别处理

workflow_def = {
    "tasks": [
        {
            "id": "load_data",
            "operator": "buffer",
            "params": {
                "input_gdf": {
                    "type": "FeatureCollection",
                    "features": [
                        {
                            "type": "Feature",
                            "geometry": {
                                "type": "Point",
                                "coordinates": [116.404, 39.915]
                            },
                            "properties": {"name": "Point1"}
                        },
                        {
                            "type": "Feature",
                            "geometry": {
                                "type": "Point",
                                "coordinates": [116.414, 39.925]
                            },
                            "properties": {"name": "Point2"}
                        }
                    ]
                },
                "distance": 0.01
            },
            "depends_on": []
        },
        {
            "id": "split_task",
            "operator": "split_by_area",
            "params": {
                "input_gdf": {"$ref": "load_data"},
                "threshold": 0.0003
            },
            "depends_on": ["load_data"]
        },
        {
            "id": "process_large",
            "operator": "get_area",
            "params": {
                "input_gdf": {"$ref": "split_task", "port": "large"}
            },
            "depends_on": ["split_task"]
        },
        {
            "id": "process_small",
            "operator": "get_area",
            "params": {
                "input_gdf": {"$ref": "split_task", "port": "small"}
            },
            "depends_on": ["split_task"]
        }
    ]
}

print("=" * 60)
print("多输出端口功能测试")
print("=" * 60)
print("\n工作流结构:")
print("  load_data (buffer)")
print("       ↓")
print("  split_task (split_by_area) → large / small")
print("       ↓                ↓")
print("  process_large    process_small")
print("=" * 60)

print("\n执行工作流...")
result = execute_workflow(workflow_def)

if result["status"] == "success":
    print("\n✅ 工作流执行成功!")
    print(f"\n最终结果 (process_small 的输出):")
    final_result = json.loads(result["final_result"])
    print(f"  - 要素数量: {len(final_result['features'])}")
    if final_result['features']:
        print(f"  - 第一个要素的面积: {final_result['features'][0]['properties'].get('area', 'N/A')}")

    print(f"\n所有中间结果:")
    for task_id, ports in result["all_results"].items():
        print(f"\n  📦 {task_id}:")
        for port_name, geojson_str in ports.items():
            geojson_data = json.loads(geojson_str)
            feat_count = len(geojson_data['features']) if 'features' in geojson_data else 0
            print(f"      端口 '{port_name}': {feat_count} 个要素")
else:
    print(f"\n❌ 工作流执行失败: {result.get('error')}")

print("\n" + "=" * 60)
print("测试完成")
print("=" * 60)
