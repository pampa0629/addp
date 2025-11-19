"""
空间算子 HTTP API 服务
提供 RESTful API，让 DolphinScheduler 可以通过 HTTP 任务调用空间算子
"""

from flask import Flask, request, jsonify
import sys
from pathlib import Path

# 添加 spatial 模块到路径
backend_dir = Path(__file__).parent.parent
sys.path.insert(0, str(backend_dir))

from spatial.workflow_engine import SpatialWorkflowEngine
from spatial.task_ref import TaskRef
from spatial.operators_extended import *

app = Flask(__name__)

# 注册所有算子
OPERATORS = {
    # 基础算子
    "buffer": "缓冲区分析",
    "intersection": "几何相交",
    "union": "几何合并",
    "centroid": "计算质心",
    "convex_hull": "凸包",
    "envelope": "最小外接矩形",

    # 扩展算子
    "difference": "几何差集",
    "simplify": "简化几何",
    "get_area": "计算面积",
    "get_length": "计算长度",
    "get_bounds": "获取边界框",
    "create_point": "创建点",
    "load_from_wkt": "从 WKT 加载",
    "export_to_wkt": "导出为 WKT",
    "export_to_geojson": "导出为 GeoJSON",
    "batch_buffer": "批量缓冲区",
    "batch_centroid": "批量质心"
}


@app.route('/health', methods=['GET'])
def health():
    """健康检查"""
    return jsonify({"status": "ok", "service": "spatial-operator-api"})


@app.route('/operators', methods=['GET'])
def list_operators():
    """列出所有可用的空间算子"""
    return jsonify({
        "operators": [
            {"code": code, "name": name}
            for code, name in OPERATORS.items()
        ]
    })


@app.route('/operator/<operator_code>', methods=['POST'])
def execute_operator(operator_code):
    """
    执行单个空间算子

    请求体示例:
    {
        "input_geom": {"type": "Point", "coordinates": [116.404, 39.915]},
        "distance": 0.001,
        "segments": 16
    }
    """
    if operator_code not in OPERATORS:
        return jsonify({
            "status": "error",
            "message": f"未知算子: {operator_code}",
            "available_operators": list(OPERATORS.keys())
        }), 400

    try:
        params = request.json or {}

        # 创建临时工作流引擎执行单个算子
        engine = SpatialWorkflowEngine(verbose=False)

        # 注册扩展算子
        engine.register_operator("get_area", get_area)
        engine.register_operator("get_length", get_length)
        engine.register_operator("get_bounds", get_bounds)
        engine.register_operator("create_point", create_point)
        engine.register_operator("difference", difference)
        engine.register_operator("simplify", simplify)
        engine.register_operator("load_from_wkt", load_from_wkt)
        engine.register_operator("export_to_wkt", export_to_wkt)
        engine.register_operator("export_to_geojson", export_to_geojson_string)  # 使用正确的函数名
        engine.register_operator("batch_buffer", batch_buffer)
        engine.register_operator("batch_centroid", batch_centroid)
        engine.register_operator("convex_hull", convex_hull)
        engine.register_operator("envelope", envelope)

        # 添加并执行任务
        engine.add_task("task", operator_code, **params)
        results = engine.run()

        return jsonify({
            "status": "success",
            "operator": operator_code,
            "result": results["task"],
            "stats": engine.get_execution_stats()
        })

    except Exception as e:
        return jsonify({
            "status": "error",
            "operator": operator_code,
            "message": str(e),
            "type": type(e).__name__
        }), 500


@app.route('/workflow', methods=['POST'])
def execute_workflow():
    """
    执行完整的工作流

    请求体示例:
    {
        "tasks": [
            {
                "id": "buffer1",
                "operator": "buffer",
                "params": {
                    "input_geom": {"type": "Point", "coordinates": [116.404, 39.915]},
                    "distance": 0.001,
                    "segments": 16
                }
            },
            {
                "id": "centroid",
                "operator": "centroid",
                "params": {
                    "input_geom": {"$ref": "buffer1"}
                }
            }
        ]
    }
    """
    try:
        workflow_def = request.json

        if not workflow_def or "tasks" not in workflow_def:
            return jsonify({
                "status": "error",
                "message": "缺少 tasks 字段"
            }), 400

        # 使用集成层执行工作流
        from spatial.dolphin_integration import execute_spatial_workflow

        result = execute_spatial_workflow(workflow_def)
        return jsonify(result)

    except Exception as e:
        return jsonify({
            "status": "error",
            "message": str(e),
            "type": type(e).__name__
        }), 500


@app.route('/operator/<operator_code>/schema', methods=['GET'])
def get_operator_schema(operator_code):
    """
    获取算子的参数模式（用于 UI 表单生成）
    """
    schemas = {
        "buffer": {
            "name": "缓冲区分析",
            "description": "创建几何对象的缓冲区",
            "params": [
                {"name": "input_geom", "type": "geometry", "required": True, "description": "输入几何对象"},
                {"name": "distance", "type": "number", "required": True, "description": "缓冲距离"},
                {"name": "segments", "type": "integer", "default": 16, "description": "圆角分段数"}
            ]
        },
        "intersection": {
            "name": "几何相交",
            "description": "计算两个几何对象的交集",
            "params": [
                {"name": "geom_a", "type": "geometry", "required": True, "description": "几何对象 A"},
                {"name": "geom_b", "type": "geometry", "required": True, "description": "几何对象 B"}
            ]
        },
        "centroid": {
            "name": "计算质心",
            "description": "计算几何对象的质心",
            "params": [
                {"name": "input_geom", "type": "geometry", "required": True, "description": "输入几何对象"}
            ]
        },
        "get_area": {
            "name": "计算面积",
            "description": "计算几何对象的面积",
            "params": [
                {"name": "input_geom", "type": "geometry", "required": True, "description": "输入几何对象"}
            ]
        },
        "create_point": {
            "name": "创建点",
            "description": "从经纬度创建点",
            "params": [
                {"name": "lon", "type": "number", "required": True, "description": "经度"},
                {"name": "lat", "type": "number", "required": True, "description": "纬度"}
            ]
        }
        # 可以继续添加其他算子的 schema
    }

    if operator_code not in schemas:
        return jsonify({
            "status": "error",
            "message": f"算子 {operator_code} 的 schema 未定义"
        }), 404

    return jsonify(schemas[operator_code])


if __name__ == '__main__':
    import os
    port = int(os.environ.get('API_PORT', 5001))  # 默认使用 5001，避免 macOS AirPlay 占用 5000

    print("=" * 60)
    print("空间算子 API 服务启动")
    print("=" * 60)
    print(f"可用算子: {len(OPERATORS)} 个")
    print(f"访问地址: http://localhost:{port}")
    print(f"健康检查: http://localhost:{port}/health")
    print(f"算子列表: http://localhost:{port}/operators")
    print("=" * 60)

    app.run(host='0.0.0.0', port=port, debug=True)
