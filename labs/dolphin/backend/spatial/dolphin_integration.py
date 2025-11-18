"""
DolphinScheduler 工作流集成模板
用于在 DolphinScheduler Python 任务中执行空间分析工作流
"""

import sys
import json
from pathlib import Path

# 添加 backend 目录到 Python 路径
backend_dir = Path(__file__).parent.parent
sys.path.insert(0, str(backend_dir))

from spatial.workflow_engine import SpatialWorkflowEngine
from spatial.task_ref import TaskRef


def execute_spatial_workflow(workflow_definition: dict, register_extended: bool = True) -> dict:
    """
    执行空间分析工作流

    Args:
        workflow_definition: 工作流定义字典
            {
                "name": "workflow_name",
                "tasks": [
                    {
                        "id": "task1",
                        "operator": "buffer",
                        "params": {...}
                    },
                    {
                        "id": "task2",
                        "operator": "intersection",
                        "params": {
                            "geom_a": {"$ref": "task1"},
                            "geom_b": {...}
                        }
                    }
                ]
            }
        register_extended: 是否注册扩展算子（默认 True）

    Returns:
        执行结果字典
            {
                "status": "success",
                "results": {...},
                "stats": {...}
            }
    """
    try:
        # 创建工作流引擎
        engine = SpatialWorkflowEngine(verbose=True)

        # 注册扩展算子
        if register_extended:
            from spatial.operators_extended import (
                load_from_geojson_string, load_from_wkt,
                export_to_geojson_string, export_to_wkt,
                create_point, create_polygon, create_linestring,
                get_bounds, get_area, get_length,
                simplify, convex_hull, envelope,
                difference, symmetric_difference,
                batch_buffer, batch_centroid
            )

            engine.register_operator("load_from_geojson_string", load_from_geojson_string)
            engine.register_operator("load_from_wkt", load_from_wkt)
            engine.register_operator("export_to_geojson_string", export_to_geojson_string)
            engine.register_operator("export_to_wkt", export_to_wkt)
            engine.register_operator("create_point", create_point)
            engine.register_operator("create_polygon", create_polygon)
            engine.register_operator("create_linestring", create_linestring)
            engine.register_operator("get_bounds", get_bounds)
            engine.register_operator("get_area", get_area)
            engine.register_operator("get_length", get_length)
            engine.register_operator("simplify", simplify)
            engine.register_operator("convex_hull", convex_hull)
            engine.register_operator("envelope", envelope)
            engine.register_operator("difference", difference)
            engine.register_operator("symmetric_difference", symmetric_difference)
            engine.register_operator("batch_buffer", batch_buffer)
            engine.register_operator("batch_centroid", batch_centroid)

        # 解析任务定义
        for task_def in workflow_definition["tasks"]:
            task_id = task_def["id"]
            operator = task_def["operator"]
            params = task_def.get("params", {})
            description = task_def.get("description", "")

            # 解析参数中的引用（{"$ref": "task_id"} → TaskRef("task_id")）
            resolved_params = _resolve_task_refs(params)

            # 添加任务
            engine.add_task(
                task_id,
                operator,
                description=description,
                **resolved_params
            )

        # 执行工作流
        results = engine.run()

        # 获取统计信息
        stats = engine.get_execution_stats()

        return {
            "status": "success",
            "workflow_name": workflow_definition.get("name", "unnamed"),
            "results": _serialize_results(results),
            "stats": {
                "total_tasks": stats["total_tasks"],
                "success_count": stats["success_count"],
                "failed_count": stats["failed_count"],
                "total_duration_ms": stats["total_duration_ms"]
            },
            "lineage": engine.export_lineage()
        }

    except Exception as e:
        return {
            "status": "error",
            "error": str(e),
            "error_type": type(e).__name__
        }


def _resolve_task_refs(params: dict) -> dict:
    """递归解析参数中的任务引用"""
    resolved = {}

    for key, value in params.items():
        if isinstance(value, dict):
            if "$ref" in value:
                # 将 {"$ref": "task_id"} 转换为 TaskRef("task_id")
                resolved[key] = TaskRef(value["$ref"])
            else:
                # 递归处理嵌套字典
                resolved[key] = _resolve_task_refs(value)
        elif isinstance(value, list):
            # 递归处理列表
            resolved[key] = [
                _resolve_task_refs({"item": item})["item"]
                if isinstance(item, dict) else item
                for item in value
            ]
        else:
            resolved[key] = value

    return resolved


def _serialize_results(results: dict) -> dict:
    """序列化结果（转换为 JSON 可序列化格式）"""
    serialized = {}

    for task_id, result in results.items():
        # 简化输出（避免 JSON 过大）
        if isinstance(result, dict) and "type" in result:
            serialized[task_id] = {
                "type": result["type"],
                "preview": f"<{result['type']} geometry>"
            }
        else:
            serialized[task_id] = result

    return serialized


# ========================================
# DolphinScheduler 任务模板
# ========================================

DOLPHIN_TASK_TEMPLATE = """
# DolphinScheduler Python 任务脚本
# 用于执行空间分析工作流

import sys
import json
from pathlib import Path

# 添加 backend 目录到 Python 路径
backend_dir = Path('/path/to/backend')  # 修改为实际路径
sys.path.insert(0, str(backend_dir))

from spatial.dolphin_integration import execute_spatial_workflow

# 工作流定义（可从上游任务传入）
workflow_definition = {
    "name": "${workflow_name}",
    "tasks": [
        {
            "id": "task1",
            "operator": "buffer",
            "description": "创建缓冲区",
            "params": {
                "input_geom": ${input_geom},
                "distance": ${distance},
                "segments": ${segments}
            }
        },
        {
            "id": "task2",
            "operator": "centroid",
            "description": "计算质心",
            "params": {
                "input_geom": {"$ref": "task1"}
            }
        }
    ]
}

# 执行工作流
result = execute_spatial_workflow(workflow_definition)

# 输出结果（DolphinScheduler 会捕获 stdout）
print(json.dumps(result, indent=2, ensure_ascii=False))

# 设置输出变量（供下游任务使用）
if result["status"] == "success":
    final_result = result["results"]["task2"]  # 最后一个任务的输出
    print(f"##[set-output name=result]{json.dumps(final_result)}")
else:
    raise Exception(f"Workflow failed: {result['error']}")
"""


# ========================================
# 示例：北京缓冲区分析工作流
# ========================================

BEIJING_BUFFER_WORKFLOW = {
    "name": "beijing_buffer_analysis",
    "tasks": [
        {
            "id": "buffer_tiananmen",
            "operator": "buffer",
            "description": "天安门 1000m 缓冲区",
            "params": {
                "input_geom": {
                    "type": "Point",
                    "coordinates": [116.39754, 39.90750]
                },
                "distance": 0.009,  # 约 1000m（度）
                "segments": 32
            }
        },
        {
            "id": "buffer_gugong",
            "operator": "buffer",
            "description": "故宫 800m 缓冲区",
            "params": {
                "input_geom": {
                    "type": "Point",
                    "coordinates": [116.39723, 39.91649]
                },
                "distance": 0.007,  # 约 800m（度）
                "segments": 32
            }
        },
        {
            "id": "intersection",
            "operator": "intersection",
            "description": "计算两个缓冲区的交集",
            "params": {
                "geom_a": {"$ref": "buffer_tiananmen"},
                "geom_b": {"$ref": "buffer_gugong"}
            }
        },
        {
            "id": "area_calculation",
            "operator": "get_area",
            "description": "计算交集面积",
            "params": {
                "input_geom": {"$ref": "intersection"}
            }
        }
    ]
}


# ========================================
# 测试执行
# ========================================

if __name__ == "__main__":
    print("测试 DolphinScheduler 集成")
    print("=" * 60)

    # 测试工作流执行
    print("\n【示例】北京缓冲区分析")
    print("-" * 60)

    result = execute_spatial_workflow(BEIJING_BUFFER_WORKFLOW)

    # 输出结果
    print(json.dumps(result, indent=2, ensure_ascii=False))

    if result["status"] == "success":
        print("\n✅ 工作流执行成功！")
        print(f"   总任务数: {result['stats']['total_tasks']}")
        print(f"   执行时间: {result['stats']['total_duration_ms']:.2f}ms")

        # 查看交集面积
        area_result = result["results"].get("area_calculation")
        if area_result:
            print(f"   交集面积: {area_result:.6f} 平方度")
            print(f"   约等于: {area_result * 111000 * 111000:.2f} 平方米")

    else:
        print(f"\n❌ 工作流执行失败: {result['error']}")

    # 导出血缘图
    if "lineage" in result:
        print("\n🔗 数据血缘关系:")
        print(result["lineage"])

    print("\n🎉 测试完成！")
