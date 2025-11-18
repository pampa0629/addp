# 空间算子工作流编排系统

## 系统架构

```
┌─────────────────────────────────────────────────────────────┐
│  用户界面 (Vue 前端)                                        │
│  - 拖拽式算子编排                                           │
│  - 参数配置面板                                             │
│  - DAG 可视化                                               │
└───────────────────┬─────────────────────────────────────────┘
                    │ HTTP API
┌───────────────────▼─────────────────────────────────────────┐
│  Go 后端服务 (8093)                                         │
│  - 算子注册与查询                                           │
│  - 工作流定义生成                                           │
│  - DolphinScheduler 集成                                    │
└───────────────────┬─────────────────────────────────────────┘
                    │ Python 调用
┌───────────────────▼─────────────────────────────────────────┐
│  Python 算子执行层                                          │
│  - operator_registry.py  (算子注册中心)                    │
│  - operator_executor.py  (算子执行器)                      │
│  - operators.py          (空间算子实现)                    │
└───────────────────┬─────────────────────────────────────────┘
                    │ 工作流提交
┌───────────────────▼─────────────────────────────────────────┐
│  DolphinScheduler                                           │
│  - 工作流调度                                               │
│  - 任务执行                                                 │
│  - 日志监控                                                 │
└─────────────────────────────────────────────────────────────┘
```

## 核心概念

### 1. 空间算子 (Spatial Operator)
独立的空间计算单元，每个算子有：
- **唯一标识** (code): 如 "buffer", "intersection"
- **输入参数** (params): 类型化参数列表
- **输出类型** (output_type): geometry, table, metrics
- **分类** (category): 几何处理、空间关系、数据处理

### 2. 工作流节点 (Workflow Node)
算子在工作流中的实例，包含：
- **节点 ID**: 唯一标识节点
- **算子引用**: 指向具体的算子
- **参数配置**: 节点级别的参数值
- **上游依赖**: 依赖的其他节点 ID 列表

### 3. 工作流 (Workflow)
由多个节点组成的 DAG（有向无环图），定义了：
- 节点之间的依赖关系
- 数据流向
- 执行顺序

## 使用流程

### 步骤 1: 启动服务

```bash
# 1. 安装 Python 依赖
cd backend
pip install -r requirements.txt

# 2. 启动 Go 后端
cd backend/cmd/server
go run main.go
# 服务运行在 http://localhost:8093

# 3. 启动前端（可选，用于可视化编排）
cd frontend
npm install
npm run dev
```

### 步骤 2: 查看可用算子

**API 请求**:
```bash
curl http://localhost:8093/api/operators
```

**响应示例**:
```json
{
  "operators": [
    {
      "code": "buffer",
      "name": "缓冲区分析",
      "category": "几何处理",
      "description": "对几何对象创建指定距离的缓冲区",
      "params": [
        {
          "name": "input_geom",
          "type": "geojson",
          "required": true,
          "description": "输入几何对象"
        },
        {
          "name": "distance",
          "type": "float",
          "required": true,
          "description": "缓冲距离（米）"
        }
      ],
      "output_type": "geometry"
    },
    ...
  ]
}
```

### 步骤 3: 测试单个算子

**API 请求**:
```bash
curl -X POST http://localhost:8093/api/operators/buffer/execute \
  -H "Content-Type: application/json" \
  -d '{
    "input_geom": {
      "type": "Point",
      "coordinates": [116.404, 39.915]
    },
    "distance": 100.0,
    "segments": 16
  }'
```

**响应示例**:
```json
{
  "status": "success",
  "operator": "buffer",
  "result": {
    "type": "Polygon",
    "coordinates": [[...]]
  }
}
```

### 步骤 4: 创建工作流

**示例工作流**: 计算两个点的缓冲区交集

```bash
curl -X POST http://localhost:8093/api/workflows \
  -H "Content-Type: application/json" \
  -d '{
    "project_name": "spatial_analysis",
    "workflow_name": "buffer_intersection_demo",
    "nodes": [
      {
        "node_id": "node_1",
        "operator_code": "buffer",
        "params": {
          "input_geom": {"type": "Point", "coordinates": [116.404, 39.915]},
          "distance": 100.0,
          "segments": 16
        },
        "upstream_nodes": []
      },
      {
        "node_id": "node_2",
        "operator_code": "buffer",
        "params": {
          "input_geom": {"type": "Point", "coordinates": [116.405, 39.916]},
          "distance": 50.0,
          "segments": 16
        },
        "upstream_nodes": []
      },
      {
        "node_id": "node_3",
        "operator_code": "intersection",
        "params": {
          "geom_a": "${{ task.node_1.output }}",
          "geom_b": "${{ task.node_2.output }}"
        },
        "upstream_nodes": ["node_1", "node_2"]
      }
    ]
  }'
```

**执行流程**:
1. `node_1` 和 `node_2` 并行执行（无依赖）
2. `node_3` 等待前两者完成后执行（依赖 node_1 和 node_2）

### 步骤 5: 可视化编排（可选）

访问 `http://localhost:5177` 使用拖拽界面：

1. 从左侧算子库拖拽算子到画布
2. 配置节点参数
3. 连接节点定义依赖关系
4. 保存并提交到 DolphinScheduler

## 文件结构

```
backend/
├── spatial/
│   ├── operator_registry.py     # 算子注册中心
│   ├── operator_executor.py     # 算子执行器（被 DolphinScheduler 调用）
│   ├── operators.py             # 实际算子实现（基于 Shapely）
│   └── workflow_builder.py      # 工作流定义生成器
├── cmd/server/
│   └── main.go                  # Go 后端服务
└── requirements.txt             # Python 依赖

frontend/
└── WorkflowEditor.vue           # 可视化编排界面
```

## 扩展算子

### 添加新算子的步骤

1. **在 `operator_registry.py` 中注册**:
```python
SPATIAL_OPERATORS.append(
    SpatialOperator(
        code="convex_hull",
        name="凸包计算",
        category="几何处理",
        description="计算几何对象的凸包",
        input_params=[
            OperatorParam("input_geom", ParamType.GEOJSON)
        ],
        output_type="geometry"
    )
)
```

2. **在 `operators.py` 中实现**:
```python
def convex_hull(input_geom: Dict[str, Any]) -> Dict[str, Any]:
    geom = shape(input_geom)
    hull = geom.convex_hull
    return mapping(hull)
```

3. **在 `operator_executor.py` 中添加分发逻辑**:
```python
elif self.operator_code == "convex_hull":
    result = convex_hull(self.params["input_geom"])
```

## 与 DolphinScheduler 集成

### 任务类型选择

使用 **Python 任务类型**，任务脚本示例：

```python
import subprocess
import json

task_config = {
    "operator": "buffer",
    "params": {
        "input_geom": {"type": "Point", "coordinates": [116.404, 39.915]},
        "distance": 100.0
    }
}

result = subprocess.run(
    ['python3', '/path/to/operator_executor.py', json.dumps(task_config)],
    capture_output=True,
    text=True
)

print(result.stdout)
if result.returncode != 0:
    raise Exception(result.stderr)
```

### 任务依赖配置

在 DolphinScheduler 中：
1. 创建 Python 任务节点（每个算子一个节点）
2. 配置任务参数（通过自定义参数传递）
3. 设置任务依赖关系（上游任务完成后触发下游任务）

### 参数传递

使用 DolphinScheduler 的变量替换功能：
- `${{ task.node_1.output }}` - 引用上游任务输出
- 任务输出通过 stdout 捕获并存储

## 已实现功能

✅ 算子注册中心（8 个基础算子）
✅ 算子执行器（支持 CLI 调用）
✅ Go 后端 API（算子查询、工作流创建）
✅ 基于 Shapely 的空间算子实现
✅ Vue 前端可视化编排界面（基础版）

## 待实现功能

⬜ DolphinScheduler API 集成（自动提交工作流）
⬜ 任务参数传递机制（上游输出 → 下游输入）
⬜ 工作流执行监控（实时状态查询）
⬜ 数据库数据源集成（spatial_join, aggregate）
⬜ 结果可视化（在前端地图上展示几何结果）

## 下一步开发建议

1. **完善 DolphinScheduler 集成**:
   - 实现 `DolphinClient.CreateWorkflow()` 方法
   - 自动生成 Python 任务脚本
   - 处理任务依赖关系

2. **增强参数传递**:
   - 实现任务间数据共享（通过 Redis/文件/数据库）
   - 支持复杂类型参数（如 Table 引用）

3. **前端优化**:
   - 实现节点连线功能（可视化依赖关系）
   - 添加工作流执行状态监控
   - 集成地图组件展示几何结果

4. **性能优化**:
   - 大数据量处理（分块处理、流式计算）
   - 并行执行优化（充分利用 DolphinScheduler 的并行能力）

## 参考资源

- DolphinScheduler API 文档: https://dolphinscheduler.apache.org/
- Shapely 空间算子: https://shapely.readthedocs.io/
- GeoJSON 规范: https://geojson.org/