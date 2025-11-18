#!/bin/bash

# 在 DolphinScheduler 容器中运行的测试脚本
# 验证空间算子工作流引擎是否正常工作

set -e

echo "=========================================="
echo "测试空间算子工作流引擎"
echo "=========================================="
echo ""

# 测试 1: 验证 Python 环境
echo "✅ 测试 1: Python 环境"
python3 --version
echo ""

# 测试 2: 验证 Shapely 库
echo "✅ 测试 2: Shapely 库"
python3 -c "import shapely; print(f'Shapely 版本: {shapely.__version__}')"
echo ""

# 测试 3: 导入空间算子模块
echo "✅ 测试 3: 空间算子模块"
cd /opt/dolphinscheduler
python3 -c "
import sys
sys.path.insert(0, '/opt/dolphinscheduler')
from spatial.workflow_engine import SpatialWorkflowEngine
from spatial.task_ref import TaskRef
print('✅ 模块导入成功')
"
echo ""

# 测试 4: 运行简单工作流
echo "✅ 测试 4: 运行简单工作流"
python3 << 'EOF'
import sys
sys.path.insert(0, '/opt/dolphinscheduler')

from spatial.workflow_engine import SpatialWorkflowEngine
from spatial.task_ref import TaskRef

# 创建工作流
engine = SpatialWorkflowEngine(verbose=False)

# 添加任务
engine.add_task(
    "buffer1",
    "buffer",
    description="天安门缓冲区",
    input_geom={"type": "Point", "coordinates": [116.404, 39.915]},
    distance=0.001,
    segments=16
)

engine.add_task(
    "centroid",
    "centroid",
    description="计算质心",
    input_geom=TaskRef("buffer1")
)

# 执行
results = engine.run()

print(f"✅ 工作流执行成功")
print(f"   质心坐标: {results['centroid']['coordinates']}")

# 统计信息
stats = engine.get_execution_stats()
print(f"   总耗时: {stats['total_duration_ms']:.2f}ms")
EOF
echo ""

echo "=========================================="
echo "✅ 所有测试通过！"
echo "=========================================="
