# Math Workflow Engine

**基础数学运算工作流引擎** - ADDP 平台工作流计算引擎的最简参考实现

## 📋 概述

Math Workflow Engine 是一个符合 [ADDP 工作流计算引擎接口规范](../docs/addp工作流计算引擎接口规范.md) 的完整工作流引擎实现，提供基础数学运算的 DAG 工作流编排能力。

**特点**:
- ✅ **极简实现**: 核心代码约 350 行（api_server.py 310 行 + workflow_engine.py 170 行）
- ✅ **完全符合规范**: 实现 OpenAPI 规范定义的所有 5 个必需接口
- ✅ **完整工作流**: 支持 DAG 执行、参数引用（$ref）、拓扑排序
- ✅ **易于理解**: 数学运算逻辑一目了然，无复杂依赖
- ✅ **可独立部署**: Docker 一键启动，自动注册到 ADDP 平台
- ✅ **Develop 集成**: 自动被 Develop 模块发现，可在工作流编辑器中使用

## 🚀 快速开始

### 方式 1: 本地运行（开发模式）

```bash
# 1. 安装依赖
cd engines/math-workflow
pip install -r requirements.txt

# 2. 启动引擎
python api_server.py

# 引擎将在 http://localhost:8097 启动
```

### 方式 2: Docker 部署（推荐）

```bash
# 1. 创建 .env 文件
cp .env.example .env
# 编辑 .env 文件，设置 INTERNAL_API_KEY

# 2. 确保 addp-network 网络存在
docker network create addp-network 2>/dev/null || true

# 3. 启动引擎
docker-compose up -d

# 4. 查看日志
docker-compose logs -f
```

### 验证运行

```bash
# 测试健康检查
curl http://localhost:8097/health | jq

# 测试算子列表
curl http://localhost:8097/api/operators | jq

# 测试单算子执行
curl -X POST http://localhost:8097/api/spatial/operators/add/execute \
  -H "Content-Type: application/json" \
  -d '{"params": {"a": 5, "b": 3}}' | jq

# 测试工作流执行
curl -X POST http://localhost:8097/api/spatial/workflow \
  -H "Content-Type: application/json" \
  -d '{
    "workflow_def": {
      "tasks": [
        {"id": "t1", "operator": "add", "params": {"a": 10, "b": 20}, "depends_on": []},
        {"id": "t2", "operator": "multiply", "params": {"a": {"$ref": "t1"}, "b": 2}, "depends_on": ["t1"]}
      ]
    }
  }' | jq
```

## 📦 算子列表

| 算子 | 类型 | 参数 | 描述 |
|-----|------|------|------|
| **add** | 数学运算 | a, b | 两数相加 |
| **subtract** | 数学运算 | a, b | 两数相减 |
| **multiply** | 数学运算 | a, b | 两数相乘 |
| **divide** | 数学运算 | a, b | 两数相除（自动处理除零错误） |
| **average** | 统计分析 | values (array) | 计算平均值 |

## 🔌 API 接口

### 1. 健康检查 - `GET /health`

```bash
curl http://localhost:8097/health
```

响应:
```json
{
  "status": "healthy",
  "service": "math-workflow-engine",
  "version": "1.0.0",
  "uptime": 3600,
  "operators_count": 5
}
```

### 2. 算子列表 - `GET /api/operators`

```bash
curl http://localhost:8097/api/operators
```

### 3. 工作流执行 - `POST /api/spatial/workflow`

执行包含多个算子的 DAG 工作流。

示例：计算 `(10 + 20) × 2 = 60`

```bash
curl -X POST http://localhost:8097/api/spatial/workflow \
  -H "Content-Type: application/json" \
  -d '{
    "workflow_def": {
      "tasks": [
        {
          "id": "task1",
          "operator": "add",
          "params": {"a": 10, "b": 20},
          "depends_on": []
        },
        {
          "id": "task2",
          "operator": "multiply",
          "params": {
            "a": {"$ref": "task1"},
            "b": 2
          },
          "depends_on": ["task1"]
        }
      ]
    }
  }'
```

响应:
```json
{
  "status": "success",
  "execution_id": "550e8400-e29b-41d4-a716-446655440000",
  "final_result": 60,
  "all_results": {
    "task1": 30,
    "task2": 60
  },
  "execution_time_ms": 1.23
}
```

### 4. 单算子执行 - `POST /api/spatial/operators/{name}/execute`

快速执行单个算子（用于测试或简单计算）。

```bash
curl -X POST http://localhost:8097/api/spatial/operators/add/execute \
  -H "Content-Type: application/json" \
  -d '{"params": {"a": 5, "b": 3}}'
```

响应:
```json
{
  "status": "success",
  "execution_id": "uuid-5678",
  "result": 8,
  "execution_time_ms": 0.12
}
```

### 5. 执行状态查询 - `GET /api/spatial/executions/{execution_id}`

```bash
curl http://localhost:8097/api/spatial/executions/uuid-1234
```

## 🏗️ 架构设计

### 目录结构

```
engines/math-workflow/
├── operators/
│   ├── __init__.py          # 算子模块导出
│   ├── base.py              # Pydantic 模型定义
│   └── math_operators.py    # 5 个数学算子实现
├── tests/
│   ├── test_operators.py    # 算子单元测试
│   ├── test_workflow.py     # 工作流执行测试
│   └── test_api.py          # API 集成测试
├── docs/
│   └── OPERATORS.md         # 算子详细文档
├── api_server.py            # Flask API 服务（约 310 行）
├── workflow_engine.py       # 简化版 DAG 工作流引擎（约 170 行）
├── requirements.txt         # Python 依赖
├── Dockerfile               # Docker 镜像定义
├── docker-compose.yml       # 容器编排配置
├── .env.example             # 环境变量模板
└── README.md                # 本文件
```

### 核心组件

1. **operators/math_operators.py** - 算子实现
   - 5 个算子函数（add、subtract、multiply、divide、average）
   - 完整的元数据定义（符合 OperatorMetadata 规范）
   - 算子注册表（OPERATORS 字典）

2. **workflow_engine.py** - DAG 工作流引擎
   - Kahn 算法拓扑排序
   - 参数引用解析（$ref 机制）
   - 内存传递中间结果
   - 循环依赖检测

3. **api_server.py** - Flask API 服务
   - 5 个标准 HTTP 端点
   - 标准错误码（5 种）
   - 自动注册到 System Backend
   - 性能监控（execution_time_ms）

## 🧪 测试

### 单元测试

```bash
# 安装 pytest
pip install pytest pytest-cov

# 运行所有测试
pytest tests/ -v

# 生成覆盖率报告
pytest tests/ --cov=. --cov-report=html
```

### 集成测试

```bash
# 启动引擎
python api_server.py

# 运行 API 集成测试
pytest tests/test_api.py -v
```

### 工作流测试示例

```python
from workflow_engine import MathWorkflowEngine

def test_complex_workflow():
    """测试复杂工作流：((10 + 20) × 2 - 5) / 5 = 11"""
    workflow_def = {
        "tasks": [
            {"id": "t1", "operator": "add", "params": {"a": 10, "b": 20}, "depends_on": []},
            {"id": "t2", "operator": "multiply", "params": {"a": {"$ref": "t1"}, "b": 2}, "depends_on": ["t1"]},
            {"id": "t3", "operator": "subtract", "params": {"a": {"$ref": "t2"}, "b": 5}, "depends_on": ["t2"]},
            {"id": "t4", "operator": "divide", "params": {"a": {"$ref": "t3"}, "b": 5}, "depends_on": ["t3"]}
        ]
    }

    engine = MathWorkflowEngine()
    engine.load_workflow(workflow_def)
    results = engine.execute()

    assert results['t1'] == 30
    assert results['t2'] == 60
    assert results['t3'] == 55
    assert results['t4'] == 11
```

## 🔗 集成到 ADDP 平台

### 自动注册机制

引擎启动时会自动注册到 System Backend：

```python
# api_server.py 中的自动注册逻辑
registration_data = {
    "unique_identifier": "api.math-workflow",
    "engine_type": "api.math-workflow",
    "display_name": "Math Workflow 计算引擎",
    "dev_modes": ["workflow"],  # 关键：声明支持工作流
    "capabilities": {
        "compute": [{
            "type": "math",
            "dev_modes": ["workflow"],
            "api_endpoints": {
                "operators": "/api/operators",
                "execute": "/api/spatial/operators/:name/execute",
                "workflow": "/api/spatial/workflow"
            }
        }]
    },
    "connection_info": {
        "api_url": "http://math-workflow-engine:8097"
    }
}
```

### 在 Develop 模块中使用

1. **启动引擎**（自动注册）
2. **登录 ADDP Portal**: http://localhost:5170
3. **进入 Develop 模块** → GIS 工作流编辑器
4. **查看算子面板**，应该能看到：
   - 数学运算分类：add、subtract、multiply、divide
   - 统计分析分类：average
5. **拖拽算子到画布**，配置参数，连接数据流
6. **执行工作流**，查看结果

## 📚 相关文档

- [ADDP 工作流计算引擎接口规范](../docs/addp工作流计算引擎接口规范.md) - 完整规范文档
- [OpenAPI 3.0 规范](../docs/workflow-engine-api-v1.yaml) - 机器可读的 API 定义
- [快速开始](../docs/addp工作流计算引擎接口规范.md#-快速开始) - 5 分钟实现最小引擎
- [最佳实践](../docs/addp工作流计算引擎接口规范.md#-最佳实践) - 算子设计和性能优化
- [常见问题](../docs/addp工作流计算引擎接口规范.md#-常见问题faq) - FAQ

## 🎯 作为参考实现

Math Workflow Engine 是第三方开发者的最佳实践示例：

**极简但完整**:
- 核心代码仅 500 行（含注释和日志）
- 包含所有必需功能（DAG 执行、参数引用、错误处理）
- 无复杂依赖（仅 Flask + Pydantic + requests）

**易于扩展**:
- 添加新算子：在 `math_operators.py` 中定义函数和元数据
- 支持异步：集成 Celery 或 Asynq 任务队列
- 多输出端口：修改 `output_ports` 定义和 `resolve_params` 逻辑

**生产就绪**:
- Docker 部署
- 健康检查
- 结构化日志
- 标准错误码
- 自动注册

## 📄 许可证

本引擎遵循 ADDP 平台的许可证。

---

**版本**: v1.0.0
**最后更新**: 2025-12-31
**维护者**: ADDP 开发团队
