# Math Workflow Engine

**基础数学运算工作流引擎** - ADDP 平台工作流计算引擎的最简参考实现

## 📋 概述

Math Workflow Engine 是一个符合 [ADDP 工作流计算引擎接口规范](../../docs/spec/addp工作流计算引擎接口规范.md) 的完整工作流引擎实现，提供基础数学运算的 DAG 工作流编排能力。

**特点**:
- ✅ **公共执行核心**: DAG 校验、拓扑排序、引用解析和异步状态管理复用 `common-python/addp_common/workflow_runtime`
- ✅ **完全符合规范**: 实现 OpenAPI 规范定义的所有 5 个必需接口
- ✅ **完整工作流**: 支持 DAG 执行、参数引用（$ref）、拓扑排序
- ✅ **易于理解**: 数学运算逻辑一目了然，无复杂依赖
- ✅ **可独立部署**: Docker 一键启动；ADDP 开发环境可自动启动服务，但仍需手动注册到平台
- ✅ **Develop 集成**: 注册后可被 Develop 模块发现，在工作流编辑器中使用

## 🚀 快速开始

### 方式 1: 本地运行（开发模式）

```bash
# 1. 安装依赖
cd engines/math-workflow
pip install -r requirements.txt
pip install -e ../../common-python

# 2. 启动引擎
python api_server.py

# 引擎将在 http://localhost:8089 启动
```

### 方式 2: Docker 部署（推荐）

```bash
# 从 ADDP 仓库根目录启动，统一读取根 .env
cd ../..

# 确保 addp-network 网络存在
docker network create addp-network 2>/dev/null || true

# 启动引擎
docker compose -f engines/math-workflow/docker-compose.yml up -d

# 查看日志
docker compose -f engines/math-workflow/docker-compose.yml logs -f
```

### 验证运行

```bash
# 测试健康检查
curl http://localhost:8089/health | jq

# 测试算子列表
curl http://localhost:8089/api/operators | jq

# 测试单算子 direct 调用（Math 内置算子默认只支持 workflow，会返回 403）
curl -X POST http://localhost:8089/api/operators/add/invoke \
  -H "Content-Type: application/json" \
  -d '{"params": {"a": 5, "b": 3}}' | jq

# 测试工作流执行
curl -X POST http://localhost:8089/api/workflow \
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
curl http://localhost:8089/health
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
curl http://localhost:8089/api/operators
```

### 3. 工作流执行 - `POST /api/workflow`

执行包含多个算子的 DAG 工作流。

示例：计算 `(10 + 20) × 2 = 60`

```bash
curl -X POST http://localhost:8089/api/workflow \
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

提交响应（HTTP 202）:
```json
{
  "status": "pending",
  "execution_id": "550e8400-e29b-41d4-a716-446655440000",
  "execution_time_ms": 1.23
}
```

随后查询 `GET /api/executions/{execution_id}`：

```json
{
  "status": "success",
  "result": 60,
  "all_results": {
    "task1": 30,
    "task2": 60
  },
  "task_order": ["task1", "task2"],
  "progress": 100
}
```

### 4. 单算子 direct 调用 - `POST /api/operators/{name}/invoke`

受控调用单个算子，不进入 ADDP 任务体系。Math Workflow 内置算子默认只支持 `workflow`，因此 direct 调用会被拒绝。

```bash
curl -X POST http://localhost:8089/api/operators/add/invoke \
  -H "Content-Type: application/json" \
  -d '{"params": {"a": 5, "b": 3}}'
```

响应:
```json
{
  "status": "failed",
  "error": "算子 'add' 不支持 direct 调用",
  "error_code": "DIRECT_NOT_SUPPORTED"
}
```

### 5. 执行状态查询 - `GET /api/executions/{execution_id}`

```bash
curl http://localhost:8089/api/executions/uuid-1234
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
├── api_server.py            # Flask API 与公共 Workflow Runtime 适配
├── requirements.txt         # Python 依赖
├── Dockerfile               # Docker 镜像定义
├── docker-compose.yml       # 容器编排配置
└── README.md                # 本文件
```

### 核心组件

1. **operators/math_operators.py** - 算子实现
   - 5 个算子函数（add、subtract、multiply、divide、average）
   - 完整的元数据定义（符合 OperatorMetadata 规范）
   - 算子注册表（OPERATORS 字典）

2. **addp_common.workflow_runtime** - Python Workflow Runtime 公共核心
   - 工作流定义与算子校验
   - DAG 拓扑排序和 `$ref` / 输出端口引用解析
   - 内存传递中间结果
   - 异步 execution registry 与标准失败状态

3. **api_server.py** - Flask API 服务
   - 5 个标准 HTTP 端点
   - 标准错误码（5 种）
   - 启动后由用户在 System 引擎管理中手动注册
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
from addp_common.workflow_runtime import WorkflowRunner
from operators import OPERATORS, get_operator_function

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

    runner = WorkflowRunner(set(OPERATORS), lambda operator, params: get_operator_function(operator)(**params))
    result = runner.execute(workflow_def)

    assert result.all_results['t1'] == 30
    assert result.all_results['t2'] == 60
    assert result.all_results['t3'] == 55
    assert result.all_results['t4'] == 11
```

## 🔗 集成到 ADDP 平台

### 手动注册机制

Math Workflow 是扩展引擎规范参考实现。ADDP 开发环境会随 `-all` / `-develop` 自动启动服务，但启动时不会自动注册到 System。需要在 System 引擎管理中选择“注册扩展引擎”，填入以下示例值后测试连接并保存：

```python
registration_data = {
    "engine_type": "math_workflow",
    "name": "Math Workflow 示例引擎",
    "description": "基于 Python 的数学计算工作流引擎，支持基本数学运算",
    "connection_info": {
        "protocol": "http",
        "host": "localhost",
        "port": 8089
    }
}
```

### 在 Develop 模块中使用

1. **启动引擎**
2. **登录 ADDP Console**: http://localhost:5170
3. **在 System 引擎管理中注册 Math Workflow 示例引擎**
4. **进入 Develop 模块** → 工作流编辑器
5. **查看算子面板**，应该能看到：
   - 数学运算分类：add、subtract、multiply、divide
   - 统计分析分类：average
6. **拖拽算子到画布**，配置参数，连接数据流
7. **执行工作流**，查看结果

## 📚 相关文档

- [ADDP 工作流计算引擎接口规范](../../docs/spec/addp工作流计算引擎接口规范.md) - 完整规范文档
- [OpenAPI 3.0 规范](../docs/workflow-engine-api-v1.yaml) - 机器可读的 API 定义
- [快速开始](../../docs/spec/addp工作流计算引擎接口规范.md#-快速开始) - 5 分钟实现最小引擎
- [最佳实践](../../docs/spec/addp工作流计算引擎接口规范.md#-最佳实践) - 算子设计和性能优化
- [常见问题](../../docs/spec/addp工作流计算引擎接口规范.md#-常见问题faq) - FAQ

## 🎯 作为参考实现

Math Workflow Engine 是第三方开发者的最佳实践示例：

**极简但完整**:
- 核心代码仅 500 行（含注释和日志）
- 包含所有必需功能（DAG 执行、参数引用、错误处理）
- 无复杂依赖（仅 Flask + Pydantic）

**易于扩展**:
- 添加新算子：在 `math_operators.py` 中定义函数和元数据
- 支持异步：集成 Celery 或 Asynq 任务队列
- 多输出端口：修改 `output_ports` 定义和 `resolve_params` 逻辑

**生产就绪**:
- Docker 部署
- 健康检查
- 结构化日志
- 标准错误码
- 手动注册示例

## 📄 许可证

本引擎遵循 ADDP 平台的许可证。

---

**版本**: v1.0.0
**最后更新**: 2025-12-31
**维护者**: ADDP 开发团队
