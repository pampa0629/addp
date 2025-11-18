#!/usr/bin/env python3
"""
综合示例：展示工作流引擎的完整功能
演示从简单到复杂的各种用法
"""

import sys
from pathlib import Path

# 添加 backend 目录到 Python 路径
backend_dir = Path(__file__).parent.parent
sys.path.insert(0, str(backend_dir))

from spatial.workflow_engine import SpatialWorkflowEngine
from spatial.task_ref import TaskRef
from spatial.operators_extended import (
    get_area, get_length, create_point, difference,
    simplify, batch_buffer, batch_centroid
)
import json


def register_extended_operators(engine):
    """注册扩展算子"""
    engine.register_operator("get_area", get_area)
    engine.register_operator("get_length", get_length)
    engine.register_operator("create_point", create_point)
    engine.register_operator("difference", difference)
    engine.register_operator("simplify", simplify)
    engine.register_operator("batch_buffer", batch_buffer)
    engine.register_operator("batch_centroid", batch_centroid)


def example_1_simple_workflow():
    """示例 1: 简单工作流 - 缓冲区交集"""
    print("\n" + "=" * 80)
    print("示例 1: 简单工作流 - 缓冲区交集分析")
    print("=" * 80)

    engine = SpatialWorkflowEngine()

    # 添加任务
    engine.add_task(
        "buffer1",
        "buffer",
        description="天安门 100m 缓冲区",
        input_geom={"type": "Point", "coordinates": [116.404, 39.915]},
        distance=0.001,
        segments=16
    )

    engine.add_task(
        "buffer2",
        "buffer",
        description="附近点 50m 缓冲区",
        input_geom={"type": "Point", "coordinates": [116.405, 39.916]},
        distance=0.0005,
        segments=16
    )

    engine.add_task(
        "intersection",
        "intersection",
        description="计算两个缓冲区的交集",
        geom_a=TaskRef("buffer1"),
        geom_b=TaskRef("buffer2")
    )

    # 执行
    results = engine.run()

    print(f"\n✅ 最终结果: {results['intersection']['type']} 几何对象")
    print(f"\n🔗 数据血缘:\n{engine.export_lineage()}")


def example_2_parallel_execution():
    """示例 2: 并行执行 - 多点缓冲区合并"""
    print("\n" + "=" * 80)
    print("示例 2: 并行执行 - 北京景点缓冲区合并")
    print("=" * 80)

    engine = SpatialWorkflowEngine()
    register_extended_operators(engine)

    # 北京著名景点坐标
    landmarks = [
        ("天安门", [116.39754, 39.90750]),
        ("故宫", [116.39723, 39.91649]),
        ("景山公园", [116.39535, 39.92595]),
        ("北海公园", [116.38783, 39.92565]),
        ("天坛", [116.41131, 39.88167])
    ]

    # 并行创建 5 个缓冲区
    buffer_tasks = []
    for name, coords in landmarks:
        task_id = f"buffer_{name}"
        engine.add_task(
            task_id,
            "buffer",
            description=f"{name} 500m 缓冲区",
            input_geom={"type": "Point", "coordinates": coords},
            distance=0.0045,  # ~500m
            segments=16
        )
        buffer_tasks.append(task_id)

    # 合并所有缓冲区
    engine.add_task(
        "union_all",
        "union",
        description="合并所有景点缓冲区",
        geometries=[TaskRef(tid) for tid in buffer_tasks]
    )

    # 计算合并后的质心
    engine.add_task(
        "centroid",
        "centroid",
        description="计算合并区域的中心点",
        input_geom=TaskRef("union_all")
    )

    # 计算面积
    engine.add_task(
        "area",
        "get_area",
        description="计算总面积",
        input_geom=TaskRef("union_all")
    )

    # 执行
    results = engine.run()

    # 结果分析
    center = results["centroid"]["coordinates"]
    area_sq_degrees = results["area"]
    area_sq_meters = area_sq_degrees * 111000 * 111000

    print(f"\n📊 分析结果:")
    print(f"   合并区域中心: ({center[0]:.6f}, {center[1]:.6f})")
    print(f"   覆盖面积: {area_sq_meters / 1e6:.2f} 平方公里")

    # 导出血缘图
    lineage_file = backend_dir / "examples" / "output" / "parallel_lineage.mmd"
    lineage_file.parent.mkdir(parents=True, exist_ok=True)
    lineage_file.write_text(engine.export_lineage())
    print(f"\n🔗 血缘图已保存: {lineage_file}")


def example_3_complex_workflow():
    """示例 3: 复杂工作流 - 环形缓冲区分析"""
    print("\n" + "=" * 80)
    print("示例 3: 复杂工作流 - 环形缓冲区分析")
    print("=" * 80)
    print("目标: 计算距离天安门 500m-1000m 的环形区域")

    engine = SpatialWorkflowEngine()
    register_extended_operators(engine)

    # 创建中心点
    engine.add_task(
        "center",
        "create_point",
        description="创建中心点（天安门）",
        lon=116.39754,
        lat=39.90750
    )

    # 外环缓冲区（1000m）
    engine.add_task(
        "outer_buffer",
        "buffer",
        description="外环 1000m 缓冲区",
        input_geom=TaskRef("center"),
        distance=0.009,  # ~1000m
        segments=32
    )

    # 内环缓冲区（500m）
    engine.add_task(
        "inner_buffer",
        "buffer",
        description="内环 500m 缓冲区",
        input_geom=TaskRef("center"),
        distance=0.0045,  # ~500m
        segments=32
    )

    # 计算环形区域（外环 - 内环）
    engine.add_task(
        "ring",
        "difference",
        description="环形区域（外环减内环）",
        geom_a=TaskRef("outer_buffer"),
        geom_b=TaskRef("inner_buffer")
    )

    # 计算环形区域的面积
    engine.add_task(
        "ring_area",
        "get_area",
        description="计算环形区域面积",
        input_geom=TaskRef("ring")
    )

    # 计算环形区域的周长
    engine.add_task(
        "ring_length",
        "get_length",
        description="计算环形区域周长",
        input_geom=TaskRef("ring")
    )

    # 简化几何（减少节点数）
    engine.add_task(
        "simplified",
        "simplify",
        description="简化环形几何",
        input_geom=TaskRef("ring"),
        tolerance=0.0001
    )

    # 执行
    results = engine.run()

    # 结果分析
    area_sq_meters = results["ring_area"] * 111000 * 111000
    length_meters = results["ring_length"] * 111000

    print(f"\n📊 环形区域分析结果:")
    print(f"   面积: {area_sq_meters / 1e6:.2f} 平方公里")
    print(f"   周长: {length_meters / 1000:.2f} 公里")
    print(f"   简化后几何类型: {results['simplified']['type']}")

    # 统计信息
    stats = engine.get_execution_stats()
    print(f"\n⏱️  性能统计:")
    print(f"   总耗时: {stats['total_duration_ms']:.2f}ms")
    print(f"   平均耗时: {stats['avg_duration_ms']:.2f}ms/任务")


def example_4_batch_operations():
    """示例 4: 批量操作 - 批量缓冲区"""
    print("\n" + "=" * 80)
    print("示例 4: 批量操作 - POI 批量缓冲区")
    print("=" * 80)

    engine = SpatialWorkflowEngine()
    register_extended_operators(engine)

    # 模拟 POI 数据（餐厅位置）
    restaurants = [
        {"type": "Point", "coordinates": [116.40 + i * 0.01, 39.91 + i * 0.005]}
        for i in range(10)
    ]

    # 批量创建缓冲区
    engine.add_task(
        "batch_buffers",
        "batch_buffer",
        description="批量创建 10 个餐厅的缓冲区",
        geometries=restaurants,
        distance=0.0005,  # ~50m
        segments=8
    )

    # 批量计算质心
    engine.add_task(
        "batch_centroids",
        "batch_centroid",
        description="批量计算缓冲区质心",
        geometries=TaskRef("batch_buffers")
    )

    # 执行
    results = engine.run()

    print(f"\n📊 批量操作结果:")
    print(f"   生成缓冲区数量: {len(results['batch_buffers'])}")
    print(f"   生成质心数量: {len(results['batch_centroids'])}")

    # 统计信息
    stats = engine.get_execution_stats()
    print(f"\n⏱️  性能统计:")
    print(f"   总耗时: {stats['total_duration_ms']:.2f}ms")
    print(f"   单个 POI 平均耗时: {stats['total_duration_ms'] / 10:.3f}ms")


def example_5_dolphin_integration():
    """示例 5: DolphinScheduler 集成示例"""
    print("\n" + "=" * 80)
    print("示例 5: DolphinScheduler 集成 - 使用 JSON 定义工作流")
    print("=" * 80)

    from spatial.dolphin_integration import execute_spatial_workflow

    # JSON 格式的工作流定义（可从文件加载或 DolphinScheduler 传入）
    workflow_def = {
        "name": "beijing_analysis",
        "tasks": [
            {
                "id": "create_center",
                "operator": "create_point",
                "description": "创建分析中心点",
                "params": {
                    "lon": 116.404,
                    "lat": 39.915
                }
            },
            {
                "id": "buffer_1km",
                "operator": "buffer",
                "description": "1km 缓冲区",
                "params": {
                    "input_geom": {"$ref": "create_center"},
                    "distance": 0.009,
                    "segments": 32
                }
            },
            {
                "id": "hull",
                "operator": "convex_hull",
                "description": "计算凸包",
                "params": {
                    "input_geom": {"$ref": "buffer_1km"}
                }
            },
            {
                "id": "export_wkt",
                "operator": "export_to_wkt",
                "description": "导出为 WKT 格式",
                "params": {
                    "input_geom": {"$ref": "hull"}
                }
            }
        ]
    }

    # 执行工作流
    result = execute_spatial_workflow(workflow_def)

    # 输出结果
    print(f"\n📊 执行结果:")
    print(f"   状态: {result['status']}")
    print(f"   工作流名称: {result['workflow_name']}")
    print(f"   总耗时: {result['stats']['total_duration_ms']:.2f}ms")

    if result['status'] == 'success':
        wkt_output = result['results']['export_wkt']
        print(f"\n   WKT 输出: {wkt_output if isinstance(wkt_output, str) else wkt_output['preview']}")

    # 保存工作流定义
    workflow_file = backend_dir / "examples" / "workflow_beijing_analysis.json"
    workflow_file.write_text(json.dumps(workflow_def, indent=2, ensure_ascii=False))
    print(f"\n💾 工作流定义已保存: {workflow_file}")


def main():
    """运行所有示例"""
    print("\n" + "🎓" * 40)
    print("空间算子工作流引擎 - 综合示例演示")
    print("🎓" * 40)

    examples = [
        ("简单工作流", example_1_simple_workflow),
        ("并行执行", example_2_parallel_execution),
        ("复杂工作流", example_3_complex_workflow),
        ("批量操作", example_4_batch_operations),
        ("DolphinScheduler 集成", example_5_dolphin_integration),
    ]

    for i, (name, func) in enumerate(examples, 1):
        print(f"\n\n{'🚀' * 40}")
        print(f"运行示例 {i}/{len(examples)}: {name}")
        print('🚀' * 40)
        func()

    print("\n\n" + "=" * 80)
    print("🎉 所有示例执行完成！")
    print("=" * 80)
    print("\n📚 更多信息请参考:")
    print("   - WORKFLOW_ENGINE_GUIDE.md - 完整使用指南")
    print("   - HYBRID_ARCHITECTURE.md - 架构设计详解")
    print("   - examples/performance_test.py - 性能对比测试")
    print("\n💡 提示: 查看 examples/output/ 目录获取输出文件")


if __name__ == "__main__":
    main()
