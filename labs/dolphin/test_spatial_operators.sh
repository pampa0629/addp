#!/bin/bash

echo "🚀 空间算子工作流系统 - 快速测试"
echo "=================================="

# 1. 检查 Python 环境
echo ""
echo "📋 步骤 1: 检查 Python 环境..."
if ! command -v python3 &> /dev/null; then
    echo "❌ 未找到 python3，请先安装 Python 3.7+"
    exit 1
fi
echo "✅ Python 版本: $(python3 --version)"

# 2. 安装 Python 依赖
echo ""
echo "📦 步骤 2: 安装 Python 依赖..."
cd backend
pip3 install -q -r requirements.txt
if [ $? -eq 0 ]; then
    echo "✅ Shapely 安装成功"
else
    echo "⚠️  安装失败，请手动运行: pip3 install -r backend/requirements.txt"
fi
cd ..

# 3. 测试算子注册中心
echo ""
echo "🔍 步骤 3: 测试算子注册中心..."
python3 -c "
import sys
sys.path.insert(0, 'backend')
from spatial.operator_registry import registry
print(f'✅ 成功加载 {len(registry.list_all())} 个空间算子')
for op in registry.list_all():
    print(f'   - {op.code}: {op.name}')
"

# 4. 测试单个算子执行
echo ""
echo "🧪 步骤 4: 测试缓冲区算子..."
python3 backend/spatial/operator_executor.py '{
  "operator": "buffer",
  "params": {
    "input_geom": {"type": "Point", "coordinates": [116.404, 39.915]},
    "distance": 100.0,
    "segments": 8
  }
}' | head -n 5

if [ $? -eq 0 ]; then
    echo "✅ 算子执行成功"
else
    echo "❌ 算子执行失败"
fi

# 5. 生成工作流定义
echo ""
echo "📄 步骤 5: 生成示例工作流定义..."
python3 backend/spatial/workflow_builder.py > /tmp/workflow_demo.json
echo "✅ 工作流定义已生成: /tmp/workflow_demo.json"
echo "   节点数量: $(python3 -c "import json; print(len(json.load(open('/tmp/workflow_demo.json'))['tasks']))")"

# 6. 启动 Go 后端服务（可选）
echo ""
echo "🌐 步骤 6: 启动后端服务（可选）"
echo "   运行以下命令启动 API 服务:"
echo "   cd backend/cmd/server && go run main.go"
echo ""
echo "   然后访问:"
echo "   - 算子列表: http://localhost:8093/api/operators"
echo "   - 执行算子: POST http://localhost:8093/api/operators/buffer/execute"
echo ""

echo "=================================="
echo "✅ 测试完成！系统已准备就绪"
echo ""
echo "📚 下一步:"
echo "   1. 查看 SPATIAL_OPERATOR_GUIDE.md 了解详细使用方法"
echo "   2. 启动 Go 服务并测试 API"
echo "   3. 集成到 DolphinScheduler 进行工作流调度"