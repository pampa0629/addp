# ADDP 工作流计算引擎接口规范

## 文档概览

**版本**: v1.0.0
**最后更新**: 2026-01-09
**适用引擎**: Math Workflow、Python Workflow、Spark Workflow

本文档定义了 ADDP 平台工作流计算引擎的统一接口规范，确保所有工作流引擎具备一致的 API 设计和行为。

---

## 目录

1. [总体架构](#1-总体架构)
2. [API 统一规范](#2-api-统一规范)
3. [算子模块规范](#3-算子模块规范)
4. [Develop 模块集成](#4-develop-模块集成)
5. [扩展新引擎指南](#5-扩展新引擎指南)

---

## 1. 总体架构

### 1.1 设计理念

ADDP 工作流计算引擎采用**插件化架构**，通过统一的 REST API 接口提供计算能力。这种设计具有以下优势：

- **技术栈隔离**: 每个引擎可使用不同的计算框架（GeoPandas、Spark、纯数学库）
- **独立扩展**: 引擎可独立部署、升级和扩容
- **统一调用**: Develop 模块通过统一接口调用不同引擎，无需关心内部实现

### 1.2 当前引擎列表

| 引擎名称 | 端口 | 技术栈 | 适用场景 | 算子数量 |
|---------|------|--------|---------|---------|
| **Math Workflow** | 8097 | Python + 基础数学库 | 数学运算、学习示例 | 5 |
| **Python Workflow** | 8099 | Python + GeoPandas + Pandas | 中小规模空间分析 | 42+ |
| **Spark Workflow** | 8098 | PySpark + Sedona | 大规模分布式计算 | 35+ |

### 1.3 引擎目录结构

采用python实现的工作流引擎可参考下面目录结构：

```
engines/<engine-name>/
├── api_server.py              # Flask API 服务（必需）
├── workflow_engine.py         # 工作流执行引擎（必需）
├── operators/                 # 算子模块（必需）
│   ├── __init__.py           # 导出算子函数和元数据
│   ├── base.py               # 算子元数据基类
│   └── <category>_operators.py  # 分类算子实现
├── requirements.txt           # Python 依赖
├── Dockerfile                 # 容器化配置
├── docker-compose.yml         # 本地开发配置
├── docs/                      # 引擎文档
│   └── api-manifest.json      # API 清单（用于 Develop 模块）
└── tests/                     # 测试用例
```

**关键文件说明**：

- **api_server.py**: 实现 5 个标准 API 接口，启动 Flask 服务
- **workflow_engine.py**: DAG 工作流解析、拓扑排序和任务调度
- **operators/**: 各类算子的具体实现和元数据定义

### 1.4 工作原理

```
┌───────────────┐
│ Develop       │
│ Backend       │
└───────┬───────┘
        │ HTTP Request (workflow_def)
        ▼
┌───────────────┐
│ Engine API    │  ← api_server.py
│ (Flask)       │
└───────┬───────┘
        │ 解析工作流定义
        ▼
┌───────────────┐
│ Workflow      │  ← workflow_engine.py
│ Engine        │
└───────┬───────┘
        │ 拓扑排序、依赖解析
        ▼
┌───────────────┐
│ Operators     │  ← operators/*.py
│ Execution     │
└───────────────┘
        │
        ▼ 返回结果（GeoJSON / 数值）
```

---

## 2. API 统一规范

### 2.1 标准接口清单

所有工作流引擎**必须**实现以下 5 个标准 API 接口：

| 接口 | 方法 | 路径 | 说明 |
|------|------|------|------|
| **健康检查** | GET | `/health` | 检查引擎状态 |
| **算子列表** | GET | `/api/operators` | 获取算子元数据 |
| **工作流执行** | POST | `/api/workflow` | 执行完整工作流（DAG） |
| **单算子执行** | POST | `/api/operators/<name>/execute` | 执行单个算子 |
| **执行状态查询** | GET | `/api/executions/<execution_id>` | 查询执行状态（可选） |

### 2.2 接口详细规范

#### 2.2.1 健康检查

**请求**：
```http
GET /health
```

**响应**：
```json
{
  "status": "healthy",
  "service": "math-workflow-engine",
  "version": "1.0.0",
  "uptime": 3600,
  "operators_count": 5,
  "dependencies": {
    "geopandas": "0.14.1"  // 可选
  }
}
```

#### 2.2.2 获取算子列表

**请求**：
```http
GET /api/operators?category=math
```

**响应**：
```json
{
  "status": "success",
  "operators": [
    {
      "name": "add",
      "display_name": "加法",
      "category": "数学运算",
      "description": "两数相加",
      "brief_description": "计算两个数的和",
      "module": "math-workflow",
      "parameters": [
        {
          "name": "a",
          "type": "float",
          "required": true,
          "description": "加数1",
          "default": 0.0
        }
      ],
      "output_ports": [
        {
          "name": "default",
          "type": "float",
          "description": "和",
          "is_default": true
        }
      ]
    }
  ],
  "count": 5
}
```

#### 2.2.3 执行工作流

**请求**：
```http
POST /api/workflow
Content-Type: application/json

{
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
  },
  "input_data": {}  // 可选，外部输入数据
}
```

**响应**：
```json
{
  "status": "success",
  "execution_id": "uuid-1234-5678",
  "final_result": 60,
  "all_results": {
    "task1": 30,
    "task2": 60
  },
  "execution_time_ms": 12.34
}
```

**错误响应**：
```json
{
  "status": "failed",
  "error": "工作流执行失败: 除数不能为零",
  "error_code": "EXECUTION_FAILED",
  "details": "任务 task3 执行失败"
}
```

#### 2.2.4 执行单个算子

**请求**：
```http
POST /api/operators/add/execute
Content-Type: application/json

{
  "params": {
    "a": 5,
    "b": 3
  }
}
```

**响应**：
```json
{
  "status": "success",
  "execution_id": "uuid-abcd-efgh",
  "result": 8,
  "execution_time_ms": 1.23
}
```

#### 2.2.5 查询执行状态

**请求**：
```http
GET /api/executions/uuid-1234-5678
```

**响应**：
```json
{
  "status": "success",
  "execution_id": "uuid-1234-5678",
  "task_status": "completed",
  "result": 60,
  "progress": 100,
  "message": "执行完成"
}
```

### 2.3 标准错误码

| 错误码 | 说明 | HTTP 状态码 |
|--------|------|------------|
| `OPERATOR_NOT_FOUND` | 算子不存在 | 404 |
| `INVALID_PARAMS` | 参数错误 | 400 |
| `EXECUTION_FAILED` | 执行失败 | 500 |
| `WORKFLOW_INVALID` | 工作流定义无效 | 400 |
| `INTERNAL_ERROR` | 内部错误 | 500 |

---

## 3. 算子模块规范

### 3.1 算子元数据结构

每个算子必须提供完整的元数据定义（参考 `operators/base.py`）：

```python
class OperatorMetadata:
    id: str                  # 唯一标识
    name: str                # 算子名称（API 调用时使用）
    display_name: str        # 显示名称（UI 显示）
    category: str            # 分类（数学运算、空间分析等）
    description: str         # 详细描述
    brief_description: str   # 简短描述
    module: str              # 所属引擎模块名
    parameters: List[ParameterMetadata]     # 输入参数
    output_ports: List[OutputPortMetadata]  # 输出端口
    use_cases: List[str]     # 应用场景（可选）
    notes: List[str]         # 使用说明（可选）
```

### 3.2 算子实现规范

以 Math Workflow 的 `add` 算子为例：

```python
# operators/math_operators.py

def add(a: float, b: float) -> float:
    """加法算子"""
    return a + b

ADD_METADATA = OperatorMetadata(
    id="add",
    name="add",
    display_name="加法",
    category="数学运算",
    description="两数相加",
    brief_description="计算两个数的和",
    module="math-workflow",
    parameters=[
        ParameterMetadata(
            name="a",
            type="float",
            required=True,
            description="加数1",
            default=0.0
        ),
        ParameterMetadata(
            name="b",
            type="float",
            required=True,
            description="加数2",
            default=0.0
        )
    ],
    output_ports=[
        OutputPortMetadata(
            name="default",
            type="float",
            description="和",
            is_default=True
        )
    ]
)

# 注册到全局算子字典
OPERATORS = {
    "add": {
        "function": add,
        "metadata": ADD_METADATA.to_dict()
    }
}
```

### 3.3 算子分类建议

| 分类 | 说明 | 示例算子 |
|------|------|---------|
| **数学运算** | 基础数学计算 | add、subtract、multiply、divide |
| **空间分析** | 空间关系判断 | buffer、intersection、union |
| **数据转换** | 格式转换 | to_geojson、to_wkt |
| **数据过滤** | 条件筛选 | filter_by_attribute、clip |
| **统计分析** | 统计计算 | count、area、length |

---

## 4. Develop 模块集成

### 4.1 集成架构

Develop 模块通过 `WorkflowEngineService` 统一调用各工作流引擎：

```
Develop Backend (Go)
├── internal/api/workflow_handler.go         # API 层：接收前端请求
├── internal/service/workflow_engine_service.go  # 服务层：引擎调用逻辑
└── internal/models/execution_config.go      # 配置模型：执行参数
```

### 4.2 调用流程

```go
// 1. 前端发送工作流定义到 Develop Backend
POST /api/develop/workflow/execute
{
  "workflow_def": {...},
  "execution_config": {
    "engine_type": "math_workflow",  // 或 python_workflow、spark_workflow
    "engine_id": 72
  }
}

// 2. Develop Backend 查询引擎信息
engine := systemClient.GetEngine(engineID)

// 3. 构造引擎 API 请求
url := fmt.Sprintf("%s://%s:%d/api/workflow",
    engine.Protocol, engine.Host, engine.Port)

// 4. 调用引擎执行工作流
resp := httpClient.Post(url, workflowRequest)

// 5. 返回结果给前端
```

### 4.3 引擎自动注册

每个引擎启动时会自动向 System Backend 注册：

```python
# api_server.py

def register_to_system():
    """向 System Backend 自注册"""
    payload = {
        "engine_type": "math_workflow",
        "name": "Math Workflow 计算引擎",
        "connection_info": {
            "protocol": "http",
            "port": 8097
        },
        "capabilities": json.dumps({
            "compute": [{
                "dev_modes": ["workflow"],
                "api_endpoints": {
                    "operators": "/api/operators",
                    "workflow": "/api/workflow"
                }
            }]
        })
    }

    requests.post(f"{SYSTEM_URL}/internal/engines/register",
                  json=payload,
                  headers={"X-Internal-API-Key": API_KEY})
```

---

## 5. 扩展新引擎指南

### 5.1 以 Math Workflow 为例

假设要创建一个新的 **统计分析引擎（Stats Workflow）**，步骤如下：

#### 步骤 1：创建目录结构

```bash
mkdir -p engines/stats-workflow/operators
cd engines/stats-workflow
```

#### 步骤 2：定义算子

```python
# operators/stats_operators.py

def mean(values: List[float]) -> float:
    """计算平均值"""
    return sum(values) / len(values)

def median(values: List[float]) -> float:
    """计算中位数"""
    sorted_values = sorted(values)
    n = len(sorted_values)
    if n % 2 == 0:
        return (sorted_values[n//2-1] + sorted_values[n//2]) / 2
    return sorted_values[n//2]

# 定义元数据...
OPERATORS = {
    "mean": {"function": mean, "metadata": MEAN_METADATA.to_dict()},
    "median": {"function": median, "metadata": MEDIAN_METADATA.to_dict()}
}
```

#### 步骤 3：实现工作流引擎

```python
# workflow_engine.py

class StatsWorkflowEngine:
    def execute(self, workflow_def, input_data):
        # 拓扑排序
        task_order = self.topological_sort()

        # 执行任务
        for task_id in task_order:
            task = self.tasks[task_id]
            operator = get_operator_function(task['operator'])
            result = operator(**task['params'])
            self.results[task_id] = result

        return self.results
```

#### 步骤 4：实现 API 服务

```python
# api_server.py

from flask import Flask, request, jsonify
from operators import list_operators, OPERATORS

app = Flask(__name__)

@app.route('/health', methods=['GET'])
def health():
    return jsonify({"status": "healthy", "service": "stats-workflow-engine"})

@app.route('/api/operators', methods=['GET'])
def get_operators():
    return jsonify({"status": "success", "operators": list_operators()})

@app.route('/api/workflow', methods=['POST'])
def execute_workflow():
    data = request.get_json()
    engine = StatsWorkflowEngine()
    engine.load_workflow(data['workflow_def'])
    results = engine.execute(data.get('input_data', {}))
    return jsonify({"status": "success", "final_result": results})

if __name__ == '__main__':
    app.run(port=8100)
```

#### 步骤 5：配置启动脚本

```bash
# 在 scripts/dev/start.sh 中添加

start_stats_workflow() {
    echo "启动 Stats Workflow 引擎..."
    cd $PROJECT_ROOT/engines/stats-workflow
    source venv/bin/activate
    PORT=8100 python api_server.py > $LOG_DIR/stats-workflow.log 2>&1 &
    echo $! > $PID_DIR/stats-workflow.pid
}
```

#### 步骤 6：测试引擎

```bash
# 启动引擎
bash scripts/dev/start.sh

# 测试健康检查
curl http://localhost:8100/health

# 测试工作流执行
curl -X POST http://localhost:8100/api/workflow \
  -H "Content-Type: application/json" \
  -d '{
    "workflow_def": {
      "tasks": [
        {"id": "t1", "operator": "mean", "params": {"values": [1, 2, 3]}}
      ]
    }
  }'
```

### 5.2 关键要点

1. **遵循标准接口**: 确保实现 5 个标准 API 接口
2. **算子元数据完整**: 提供详细的参数、输出定义
3. **错误处理**: 使用标准错误码，提供清晰的错误信息
4. **自动注册**: 启动时向 System Backend 注册引擎信息
5. **文档完善**: 编写 `docs/api-manifest.json` 和 `README.md`

### 5.3 常见问题

**Q1: 如何支持分布式计算？**
- 参考 Spark Workflow，在 `workflow_engine.py` 中集成 Spark 或 Dask

**Q2: 如何处理大文件输入？**
- 使用对象存储（MinIO）引用，参数传递 `{"$storage": "minio://bucket/path"}`

**Q3: 如何支持异步执行？**
- 实现 `/api/executions/<id>` 接口，使用 Celery 或 Asynq 任务队列

---

## 附录

### A. 参考实现

- **Math Workflow**: `engines/math-workflow/` - 简单示例，适合学习
- **Python Workflow**: `engines/python-workflow/` - 生产级实现，包含 GeoPandas
- **Spark Workflow**: `engines/spark-workflow/` - 分布式计算示例

### B. 相关文档

- [ADDP 开发原则](./addp开发原则.md)
- [ADDP API 设计规范](./addp-api设计规范.md)
- [Develop 模块文档](../develop/CLAUDE.md)
- [System 引擎管理](../system/CLAUDE.md)

### C. 技术支持

如有问题，请参考各引擎的 `README.md` 或提交 Issue。

---

**文档维护**: 本文档应随引擎接口变更及时更新。
