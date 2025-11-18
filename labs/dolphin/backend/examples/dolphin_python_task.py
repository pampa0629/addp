#!/usr/bin/env python3
"""
DolphinScheduler Python 任务示例
直接在 DolphinScheduler 中作为 Python 任务运行
"""

# ============================================
# 方式 1: 最简单 - 直接在 Python 任务中使用
# ============================================

import sys
import json

# 添加 backend 目录到 Python 路径
sys.path.insert(0, '/path/to/addp/labs/dolphin/backend')

from spatial.workflow_engine import SpatialWorkflowEngine
from spatial.task_ref import TaskRef

# 创建工作流
engine = SpatialWorkflowEngine()

# 添加任务
engine.add_task(
    "buffer1",
    "buffer",
    input_geom={"type": "Point", "coordinates": [116.404, 39.915]},
    distance=0.001
)

engine.add_task(
    "buffer2",
    "buffer",
    input_geom={"type": "Point", "coordinates": [116.405, 39.916]},
    distance=0.0005
)

engine.add_task(
    "intersection",
    "intersection",
    geom_a=TaskRef("buffer1"),
    geom_b=TaskRef("buffer2")
)

# 执行工作流
results = engine.run()

# 输出结果（DolphinScheduler 会捕获 stdout）
print(json.dumps({
    "status": "success",
    "result": results["intersection"]
}, ensure_ascii=False))

# 设置输出变量供下游任务使用
final_result = results["intersection"]
print(f"##[set-output name=result_geom]{json.dumps(final_result)}")

# ============================================
# 方式 2: 使用 JSON 定义工作流（更灵活）
# ============================================

from spatial.dolphin_integration import execute_spatial_workflow

# 工作流定义（可以从文件读取或上游任务传入）
workflow_def = {
    "name": "spatial_analysis",
    "tasks": [
        {
            "id": "task1",
            "operator": "buffer",
            "description": "创建缓冲区",
            "params": {
                "input_geom": {"type": "Point", "coordinates": [116.404, 39.915]},
                "distance": 0.001
            }
        },
        {
            "id": "task2",
            "operator": "centroid",
            "description": "计算质心",
            "params": {
                "input_geom": {"$ref": "task1"}  # 引用上游任务
            }
        }
    ]
}

# 执行工作流
result = execute_spatial_workflow(workflow_def)

# 输出结果
print(json.dumps(result, indent=2, ensure_ascii=False))

if result["status"] == "success":
    # 设置输出变量
    final_output = result["results"]["task2"]
    print(f"##[set-output name=result]{json.dumps(final_output)}")
else:
    raise Exception(f"工作流执行失败: {result['error']}")


# ============================================
# 方式 3: 从上游任务接收参数
# ============================================

# 从 DolphinScheduler 参数获取输入
input_data = ${input_geom}  # DolphinScheduler 变量替换
distance_value = ${distance}

# 解析参数
import json
input_geom = json.loads(input_data) if isinstance(input_data, str) else input_data

# 创建工作流
engine = SpatialWorkflowEngine()
engine.add_task("buffer", "buffer",
               input_geom=input_geom,
               distance=float(distance_value))
results = engine.run()

print(json.dumps(results["buffer"], ensure_ascii=False))
