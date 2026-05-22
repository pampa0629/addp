# ADDP 空间计算引擎集成方案（最终版）

## 📋 项目概述

**目标**: 在 ADDP 平台中引入通用的**空间计算引擎**架构，首个实现为 **Python Workflow Engine**

**核心设计理念**（✅ 已与用户确认）:
1. **计算引擎语言无关**: 不限定 Go，支持 Python/GeoPandas、Go、Rust 等任意技术栈
2. **内存高效传递**: GeoPandas 工作流全程使用 GeoDataFrame，避免反复序列化 GeoJSON
3. **引擎外挂注册**: 与 Dolphin 解耦，作为独立计算引擎，可并存多个 GIS 引擎（如未来的 PostGIS Engine、GDAL Engine）
4. **双执行模式**:
   - **即时执行**: Develop 模块中直接运行，结果存储到 PostGIS，可设置调度周期
   - **任务保存**: 保存为 GIS 任务定义（存储到 `develop.spatial_tasks`），供 Orchestrator 发现和编排
5. **通用模式**: 为未来的统计引擎（R/Python）、机器学习引擎提供参考架构
6. **仅注册引擎本身**: System 中只注册 `python-workflow.engine.default`，不注册 21 个具体算子
7. **动态任务发现**: Orchestrator 通过 Python Workflow Engine 的标准工作流 API 动态查询和执行任务（类似 Transfer 模式）

**核心价值**:
- ✅ 空间算子在 GeoPandas 内存中链式计算（高性能）
- ✅ 可视化拖拽式工作流设计（DAG 画布）
- ✅ GIS 任务与 Transfer、Meta 任务平等编排
- ✅ 可扩展的计算引擎注册机制（SQL Engine / Spatial Engine / Stats Engine）
- ✅ 结果存储到 PostGIS 空间表（非 JSONB），支持高效空间查询

---

## 🏗️ 整体架构

### 计算引擎注册模式（基于 Transfer 模式）

```
┌─────────────────────────────────────────────────────────────────┐
│  System Backend (引擎注册中心)                                  │
│  ┌───────────────────────────────────────────────────────────┐ │
│  │ PostgreSQL (system.engines 表) ★ 任务定义存储       │ │
│  │                                                            │ │
│  │ ★ 只注册引擎本身（1 条记录）                               │ │
│  │   {                                                        │ │
│  │     engine_type: "python_workflow",                       │ │
│  │     engine_family: "workflow",                            │ │
│  │     connection_info: { protocol: "http", port: 8099 },     │ │
│  │     capabilities: engine.capabilities/v1                   │ │
│  │   }                                                        │ │
│  │                                                            │ │
│  │ ✅ Transfer: "transfer.worker.default"                    │ │
│  │ ✅ Meta: "meta.scanner.default"                           │ │
│  │ ✅ Python Workflow: "python-workflow.engine.default" (新增)           │ │
│  └───────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
                           ▲
                           │ (启动时注册)
                           │
┌──────────────────────────┴──────────────────────────────────────┐
│  Python Workflow Engine (独立微服务，HTTP Sidecar)                    │
│  容器名: python-workflow-engine, 端口: 8090                            │
│                                                                  │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │ Flask API Server                                           │ │
│  │ • GET  /api/spatial/tasks (任务列表) ★                     │ │
│  │ • POST /api/spatial/tasks (创建任务)                       │ │
│  │ • POST /api/spatial/tasks/:id/execute (执行任务) ★         │ │
│  │ • GET  /api/spatial/executions/:id (查询状态) ★            │ │
│  │ • GET  /api/spatial/operators (算子列表 - 供前端使用)      │ │
│  │ • POST /api/spatial/workflow (即时执行 - 供 Develop 使用) │ │
│  │ • GET  /health (健康检查)                                  │ │
│  └────────────────┬───────────────────────────────────────────┘ │
│                   │                                              │
│  ┌────────────────▼───────────────────────────────────────────┐ │
│  │ GeoPandas Workflow Engine                                  │ │
│  │ • DAG 拓扑排序                                              │ │
│  │ • GeoDataFrame 内存传递（关键优化）                         │ │
│  │ • 避免中间结果序列化                                        │ │
│  │ • 最终结果写入 PostGIS 空间表                               │ │
│  └────────────────┬───────────────────────────────────────────┘ │
│                   │                                              │
│  ┌────────────────▼───────────────────────────────────────────┐ │
│  │ 空间算子库 (21 个算子)                                      │ │
│  │ • GeoPandas 原生算子: buffer, centroid, intersection, ...  │ │
│  │ • Shapely 补充算子: convex_hull, simplify, ...             │ │
│  └────────────────────────────────────────────────────────────┘ │
│                                                                  │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │ PostgreSQL (develop.spatial_tasks 表) ★ 任务定义存储       │ │
│  │ • 任务名称、工作流定义、输入 schema、调度配置               │ │
│  │ • 租户隔离（tenant_id）                                     │ │
│  └────────────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────────┘
                           ▲
                           │ HTTP 调用
                           │
┌──────────────────────────┴──────────────────────────────────────┐
│  Develop Backend (数据开发工作台)                                │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │ SQL 执行服务 (已有)                                         │ │
│  │ • 连接 PostgreSQL, MySQL, ClickHouse                       │ │
│  └────────────────────────────────────────────────────────────┘ │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │ 空间工作流服务 (新增)                                       │ │
│  │ • 即时执行：转发到 Python Workflow Engine                         │ │
│  │ • 保存任务：写入 develop.spatial_tasks 表                  │ │
│  │ • 管理任务：CRUD + 调度配置                                 │ │
│  └────────────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────────┘
                           ▲
                           │
┌──────────────────────────┴──────────────────────────────────────┐
│  Develop Frontend (数据开发工作台 UI)                            │
│  ┌────────────────┐  ┌──────────────────────────────────────┐  │
│  │ SQL 编辑器     │  │ 空间工作流设计器（新增，基于 G6/Vue) │  │
│  │ (已有)         │  │ • 从 Python Workflow Engine 获取算子列表    │  │
│  │                │  │ • DAG 画布（拖拽算子节点）            │  │
│  │                │  │ • 参数配置面板                        │  │
│  │                │  │ • 即时执行 / 保存为任务               │  │
│  │                │  │ • 结果地图预览（PostGIS 读取）        │  │
│  └────────────────┘  └──────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────┐
│  Orchestrator Backend (统一编排层)                                │
│  • 查询 System: GET /internal/registry/compute-engines           │
│  • 返回引擎列表: [transfer, meta, python-workflow]                     │
│  • 查询工作流运行时能力: compute.workflow                        │
│  • 调用引擎 API: GET http://python-workflow-engine:8090/api/spatial/tasks │
│  • 动态发现 GIS 任务（类似 Transfer）                            │
│  • 混合编排：SQL → GIS 工作流 → Transfer                         │
│  • 步骤间数据传递（参数模板化 {{step1.result.result_table}}）   │
└──────────────────────────────────────────────────────────────────┘
                           ▲
                           │
┌──────────────────────────┴──────────────────────────────────────┐
│  Orchestrator Frontend                                           │
│  • 左侧任务库：点击 "Python Workflow" 展开                             │
│  • 动态加载任务列表（从 Python Workflow Engine 获取）                  │
│  • 显示：[北京 POI 缓冲区分析, 上海边界叠加, ...]                │
│  • 拖拽任务节点到 DAG 画布                                        │
│  • 配置运行时参数                                                 │
│  • 执行混合编排工作流                                             │
└──────────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────┐
│  PostGIS 空间表 (结果存储)                                        │
│  • develop.spatial_execution_results (Develop 即时执行结果)      │
│  • orchestrator.gis_results (Orchestrator 编排执行结果)          │
│  • 字段: geom GEOMETRY(GEOMETRY, 4326), properties JSONB         │
│  • 支持空间索引: GIST(geom)                                       │
└──────────────────────────────────────────────────────────────────┘
```

### 关键设计决策（已与用户确认✅）

| 决策点 | 方案 | 理由 |
|--------|------|------|
| **内存传递机制** | GeoDataFrame 全程内存传递 | 避免反复序列化 GeoJSON，性能最优 |
| **前端交互方式** | DAG 画布（拖拽节点+连线，基于 G6/Vue Flow） | 可视化清晰，支持复杂工作流 |
| **执行模式** | 1) 即时执行（Develop 内） <br> 2) 保存为 GIS 任务 | 快速验证 + 供 Orchestrator 编排 |
| **引擎部署形态** | HTTP Sidecar（独立容器） | 语言隔离，可并存多引擎（python-workflow-engine, postgis-engine） |
| **引擎命名** | Python Workflow Engine | 明确技术栈，未来可扩展 PostGIS Engine、GDAL Engine |
| **独立实现** | 完全独立的空间计算引擎 | GIS 引擎是通用架构，可复用于多个场景 |

---

## 🎯 核心功能设计

### 1. Python Workflow Engine 内部实现（关键优化）

**工作流执行伪代码**（Python）:

```python
class GeoPandasWorkflowEngine:
    """
    核心优化：全程使用 GeoDataFrame，避免中间序列化
    """
    def __init__(self):
        self.tasks = {}           # {task_id: TaskDef}
        self.results = {}         # {task_id: GeoDataFrame}  # 内存缓存

    def add_task(self, task_id, operator, params):
        """添加任务节点"""
        self.tasks[task_id] = {
            'operator': operator,
            'params': params
        }

    def run(self):
        """执行工作流（DAG 拓扑排序）"""
        sorted_tasks = self._topological_sort()

        for task_id in sorted_tasks:
            task = self.tasks[task_id]

            # 解析参数中的引用（如 {"$ref": "task1"}）
            resolved_params = self._resolve_references(task['params'])

            # 执行算子（返回 GeoDataFrame，不序列化）
            if task['operator'] == 'buffer':
                result = resolved_params['input_gdf'].buffer(
                    distance=resolved_params['distance']
                )
            elif task['operator'] == 'centroid':
                result = resolved_params['input_gdf'].centroid
            elif task['operator'] == 'intersection':
                result = gpd.overlay(
                    resolved_params['gdf_a'],
                    resolved_params['gdf_b'],
                    how='intersection'
                )
            # ... 其他算子

            # 内存缓存（GeoDataFrame 对象）
            self.results[task_id] = result

        # 最终输出：只在最后序列化一次
        final_result = self.results[sorted_tasks[-1]]
        return final_result.to_json()  # GeoJSON 字符串

    def _resolve_references(self, params):
        """解析参数引用"""
        resolved = {}
        for key, value in params.items():
            if isinstance(value, dict) and "$ref" in value:
                # 从内存缓存获取上游结果（GeoDataFrame）
                ref_task_id = value["$ref"]
                resolved[key] = self.results[ref_task_id]
            else:
                resolved[key] = value
        return resolved
```

**关键优势**:
- ✅ 中间结果全程是 GeoDataFrame 对象（Pandas 内存高效）
- ✅ 只在开始解析 GeoJSON，结束时序列化一次
- ✅ 支持复杂算子链: `gdf → buffer → intersection → centroid → simplify`

---

### 2. Develop 模块数据流

#### 2.1 即时执行流程

```
用户在 Develop Frontend 拖拽设计工作流
    ↓
配置参数（距离、容差等）
    ↓
点击"执行"
    ↓
POST /api/develop/spatial/execute
{
  "workflow": {
    "tasks": [
      {"id": "t1", "operator": "buffer", "params": {...}},
      {"id": "t2", "operator": "centroid", "params": {"input_gdf": {"$ref": "t1"}}}
    ]
  }
}
    ↓
Develop Backend → Python Workflow Engine: POST /api/spatial/workflow
    ↓
Python Workflow Engine 执行（内存中）
    ↓
返回最终 GeoJSON
    ↓
Develop Frontend 在地图上渲染结果
```

#### 2.2 保存为 GIS 任务流程

```
用户点击"保存为任务"
    ↓
POST /api/develop/spatial/tasks
{
  "name": "北京 POI 缓冲区分析",
  "description": "...",
  "engine_type": "python-workflow",
  "workflow_def": {
    "tasks": [...]
  },
  "input_schema": {  // 定义工作流输入参数
    "poi_location": {"type": "geojson"},
    "buffer_distance": {"type": "float"}
  }
}
    ↓
存储到 PostgreSQL: develop.spatial_tasks 表
    ↓
保存为 Develop 任务定义，记录所使用的 Python Workflow Engine
    ↓
Orchestrator Frontend 从 System 查询，显示在任务库
    ↓
用户在 Orchestrator 拖拽该 GIS 任务节点，与其他任务混合编排
```

---

### 3. Orchestrator 集成（参数模板化）

**关键改动**: 扩展 Orchestrator Executor，支持跨步骤引用

**示例工作流**:

```json
{
  "name": "数据预处理 + 空间分析 + 结果导出",
  "steps": [
    {
      "id": "sql_extract",
      "engine_identifier": "sql.postgresql.default",
      "parameters": {
        "query": "SELECT geom, name FROM poi WHERE city='北京'"
      }
    },
    {
      "id": "spatial_analysis",
      "engine_identifier": "spatial.beijing_poi_buffer.v1",
      "parameters": {
        "poi_location": "{{sql_extract.result.geojson}}",  // 引用上一步输出
        "buffer_distance": 0.001
      }
    },
    {
      "id": "export_result",
      "engine_identifier": "transfer.export.shapefile",
      "parameters": {
        "input_geojson": "{{spatial_analysis.result.geojson}}",
        "target_path": "s3://bucket/result.shp"
      }
    }
  ]
}
```

**Orchestrator Executor 伪代码**:

```go
// orchestrator/backend/internal/service/executor.go

func (e *Executor) executeStep(ctx context.Context, step *models.Step, stepResults models.StepResults) {
    // 1. 解析参数模板 {{sql_extract.result.geojson}}
    resolvedParams := e.resolveTemplateReferences(step.Parameters, stepResults)

    // 2. 从 EngineRegistry 获取引擎配置
    engine, _ := e.engineRegistry.GetEngine(ctx, step.EngineIdentifier)

    // 3. 调用引擎执行（可能是 Python Workflow Engine, 也可能是 Transfer Worker）
    taskID, _ := e.taskClient.CreateTask(ctx, engine, resolvedParams)
    result, _ := e.taskClient.ExecuteTask(ctx, engine, taskID)

    // 4. 存储结果到 stepResults（JSONB）
    stepResults[step.ID] = result
}

func (e *Executor) resolveTemplateReferences(params map[string]interface{}, stepResults models.StepResults) map[string]interface{} {
    resolved := make(map[string]interface{})
    for key, value := range params {
        if strVal, ok := value.(string); ok && strings.Contains(strVal, "{{") {
            // 解析模板: {{sql_extract.result.geojson}}
            refPath := extractPath(strVal)  // ["sql_extract", "result", "geojson"]
            stepID := refPath[0]

            // 从 stepResults 提取数据
            data := stepResults[stepID].Result
            for _, field := range refPath[1:] {
                data = data[field]
            }
            resolved[key] = data
        } else {
            resolved[key] = value
        }
    }
    return resolved
}
```

---

### 4. 关键数据结构

#### 4.1 System 注册（仅注册引擎本身）

```json
{
  "engine_type": "python_workflow",
  "name": "Python Workflow 空间计算引擎",
  "description": "基于 Python 的工作流执行引擎",
  "connection_info": {
    "protocol": "http",
    "port": 8099
  },
  "is_builtin": true
}
```

#### 4.2 任务定义表（develop.spatial_tasks）

```sql
-- develop.spatial_tasks (GIS 任务定义，类似 transfer.tasks)
CREATE TABLE develop.spatial_tasks (
    id SERIAL PRIMARY KEY,
    tenant_id INT NOT NULL,
    name VARCHAR(128) NOT NULL,
    description TEXT,
    workflow_def JSONB NOT NULL,       -- DAG 定义（算子链）
    input_schema JSONB,                -- 参数定义（参数化）
    output_schema JSONB,               -- 输出定义
    schedule VARCHAR(100),             -- Cron 表达式（可选）
    status VARCHAR(20) DEFAULT 'active',
    last_execution_id UUID,
    last_execution_status VARCHAR(20),
    last_execution_started_at TIMESTAMP,
    last_execution_finished_at TIMESTAMP,
    created_by INT,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_spatial_tasks_tenant ON develop.spatial_tasks(tenant_id);
CREATE INDEX idx_spatial_tasks_status ON develop.spatial_tasks(status);
```

**示例记录**:
```json
{
  "id": 1,
  "tenant_id": 1,
  "name": "北京 POI 缓冲区分析",
  "workflow_def": {
    "tasks": [
      {
        "id": "t1",
        "operator": "buffer",
        "params": {
          "input_geom": "{{input.poi_location}}",
          "distance": "{{input.buffer_distance}}"
        }
      },
      {
        "id": "t2",
        "operator": "centroid",
        "params": {
          "input_geom": {"$ref": "t1"}
        }
      }
    ]
  },
  "input_schema": {
    "poi_location": {"type": "geojson", "required": true},
    "buffer_distance": {"type": "float", "default": 1000}
  },
  "schedule": "0 2 * * *"
}
```

#### 4.3 执行结果表（develop.spatial_execution_results）

```sql
-- develop.spatial_execution_results (Develop 即时执行结果)
CREATE TABLE develop.spatial_execution_results (
    id SERIAL PRIMARY KEY,
    execution_id UUID NOT NULL,
    tenant_id INT NOT NULL,
    task_id INT,                          -- 关联 spatial_tasks（可选）
    geom GEOMETRY(GEOMETRY, 4326),        -- 空间字段
    properties JSONB,                     -- 属性数据
    created_by INT,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_spatial_exec_results_tenant ON develop.spatial_execution_results(tenant_id);
CREATE INDEX idx_spatial_exec_results_execution ON develop.spatial_execution_results(execution_id);
CREATE INDEX idx_spatial_exec_results_geom ON develop.spatial_execution_results USING GIST(geom);
```

#### 4.4 编排执行结果表（orchestrator.gis_results）

```sql
-- orchestrator.gis_results (Orchestrator 编排执行结果)
CREATE TABLE orchestrator.gis_results (
    id SERIAL PRIMARY KEY,
    execution_id INT NOT NULL,           -- 关联 orchestrator.executions
    step_id VARCHAR(64) NOT NULL,
    geom GEOMETRY(GEOMETRY, 4326),
    properties JSONB,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_gis_results_execution ON orchestrator.gis_results(execution_id);
CREATE INDEX idx_gis_results_geom ON orchestrator.gis_results USING GIST(geom);
```

---

## 📁 关键文件清单

### 新建文件（核心实现）

| 文件路径 | 说明 | 代码量 |
|----------|------|--------|
| `python-workflow/workflow_engine.py` | Python Workflow 工作流引擎（DAG 执行 + 内存传递） | ~300 行 |
| `python-workflow/api_server.py` | Flask API Server | ~200 行 |
| `python-workflow/operators.py` | 空间算子库（buffer, centroid, intersection 等） | ~500 行 |
| `python-workflow/Dockerfile` | 容器化配置 | ~30 行 |
| `develop/backend/internal/service/spatial_workflow_service.go` | 空间工作流服务（转发+任务保存） | ~400 行 |
| `develop/backend/internal/api/spatial_handler.go` | API 处理器 | ~250 行 |
| `develop/frontend/src/views/SpatialWorkflowDesigner.vue` | DAG 画布（基于 @antv/g6） | ~600 行 |
| `develop/frontend/src/components/SpatialOperatorNode.vue` | 算子节点组件 | ~150 行 |

### 修改文件

| 文件路径 | 改动说明 | 改动量 |
|----------|----------|--------|
| `docker-compose.yml` | 新增 python-workflow-engine 服务 | +40 行 |
| `orchestrator/backend/internal/service/executor.go` | 扩展参数模板解析 | +100 行 |
| `develop/backend/internal/api/router.go` | 新增空间工作流路由组 | +15 行 |

---

## 🚀 实施步骤（简化版）

### 阶段 1: Python Workflow Engine 实现（3-4 天）

**任务**:
1. 创建 `python-workflow-engine` 目录结构
2. 实现 `workflow_engine.py`（核心：GeoDataFrame 内存传递）
3. 实现 `operators.py`（GeoPandas 原生算子封装）
4. 实现 `api_server.py`（Flask REST API）
5. 容器化（Dockerfile + docker-compose.yml）

**验证**:
```bash
# 启动引擎
docker-compose up -d python-workflow-engine

# 测试单算子
curl -X POST http://localhost:8090/api/spatial/operators/buffer \
  -d '{"input_geom": {...}, "distance": 0.001}'

# 测试工作流
curl -X POST http://localhost:8090/api/spatial/workflow \
  -d '{"tasks": [...]}'
```

---

### 阶段 2: Develop 模块集成（4-5 天）

**任务**:
1. **Backend**:
   - 创建 `spatial_workflow_service.go`（转发到 Python Workflow Engine）
   - 创建 `spatial_handler.go`（API 端点）
   - 添加数据库表 `develop.spatial_tasks`
2. **Frontend**:
   - 创建 `SpatialWorkflowDesigner.vue`（基于 @antv/g6 的 DAG 画布）
   - 实现算子拖拽、参数配置、即时执行
   - 实现"保存为任务"功能

**验证**:
- [ ] 在 Develop 模块拖拽设计工作流
- [ ] 点击"执行"，查看地图上的结果
- [ ] 点击"保存为任务"，检查数据库记录

---

### 阶段 3: Orchestrator 集成（2-3 天）

**任务**:
1. 扩展 `executor.go`，实现 `resolveTemplateReferences`
2. Orchestrator Frontend 从 System 查询 GIS 任务
3. 支持拖拽 GIS 任务节点到编排画布
4. 测试混合编排（SQL → GIS → Transfer）

**验证**:
- [ ] Orchestrator 任务库显示 GIS 任务
- [ ] 创建混合编排工作流
- [ ] 执行成功，步骤间数据正确传递

---

### 阶段 4: 测试与优化（2-3 天）

**任务**:
1. 性能测试（大 GeoDataFrame 处理）
2. 错误处理完善（Python 异常映射）
3. 文档编写

---

## ⚠️ 关键风险

| 风险点 | 缓解措施 |
|--------|----------|
| **GeoDataFrame 内存占用过大** | 实现分块处理（batch_size 参数） |
| **GeoPandas 版本兼容性** | 固定版本（requirements.txt） |
| **前端 DAG 画布复杂度** | 使用成熟库（@antv/g6），参考 Orchestrator 现有实现 |

---

## 📊 工作量估算

| 阶段 | 任务 | 开发天数 |
|------|------|---------|
| 阶段 1 | Python Workflow Engine | 3-4 天 |
| 阶段 2 | Develop 模块集成 | 4-5 天 |
| 阶段 3 | Orchestrator 集成 | 2-3 天 |
| 阶段 4 | 测试与优化 | 2-3 天 |
| **总计** | - | **11-15 天** |

---

## ✅ 下一步

**确认以上简化方案是否符合你的预期？如有调整再告诉我，然后我会退出计划模式，准备开始实施。**

---

## 📝 脚本和文档补充

### 需要更新的脚本

| 脚本文件 | 改动说明 |
|---------|----------|
| `scripts/dev/start.sh` | 新增 python-workflow-engine 启动步骤（端口8090健康检查） |
| `scripts/prod/start.sh` | 新增 python-workflow-engine 容器启动和健康检查 |
| `scripts/infra/up.sh` | 确认 develop schema 存在（spatial_tasks 表依赖） |
| `scripts/build/compile.sh` | 无需改动（Python 服务不需编译） |
| `scripts/build/build-images.sh` | 新增 python-workflow-engine 镜像构建 |
| `Makefile` | 新增 `make python-workflow-engine` 和 `make python-workflow-logs` 命令 |

### 需要更新的文档

| 文档文件 | 改动说明 |
|---------|----------|
| `CLAUDE.md` | 新增 Python Workflow Engine 章节（架构、端口、技术栈） |
| `docs/PORTS.md` | 新增端口8090（python-workflow-engine） |
| `scripts/README.md` | 更新启动脚本说明（包含 python-workflow-engine） |
| `develop/CLAUDE.md` | 新增空间工作流设计器章节 |
| `orchestrator/CLAUDE.md` | 更新任务发现机制（支持 Python Workflow 任务） |

---

## 🔧 关键技术实现补充

### 1. Orchestrator 参数模板化

**目的**: 支持跨步骤引用（如 `{{step1.result.result_table}}`）

**关键改动**:
```go
// orchestrator/backend/internal/service/executor.go

func (e *Executor) resolveTemplateReferences(params map[string]interface{}, stepResults models.StepResults) map[string]interface{} {
    resolved := make(map[string]interface{})
    for key, value := range params {
        if strVal, ok := value.(string); ok && strings.Contains(strVal, "{{") {
            // 解析模板: {{step1.result.geojson}}
            refPath := extractPath(strVal)  // ["step1", "result", "geojson"]
            stepID := refPath[0]

            // 从 stepResults 提取数据
            data := stepResults[stepID].Result
            for _, field := range refPath[1:] {
                data = data[field]
            }
            resolved[key] = data
        } else {
            resolved[key] = value
        }
    }
    return resolved
}
```

### 2. PostGIS 结果存储

**目的**: 最终结果存储到 PostGIS 空间表（高效空间查询）

**关键表结构**:
```sql
-- develop.spatial_execution_results
CREATE TABLE develop.spatial_execution_results (
    id SERIAL PRIMARY KEY,
    execution_id UUID NOT NULL,
    tenant_id INT NOT NULL,
    geom GEOMETRY(GEOMETRY, 4326),  -- 空间字段（非 JSONB）
    properties JSONB,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_spatial_exec_results_geom ON develop.spatial_execution_results USING GIST(geom);
```

**Python Workflow Engine 写入逻辑**:
```python
# python-workflow-engine/workflow_engine.py
def save_to_postgis(self, gdf: gpd.GeoDataFrame, execution_id: str, tenant_id: int):
    """
    将 GeoDataFrame 写入 PostGIS 空间表
    """
    gdf.to_postgis(
        name='spatial_execution_results',
        con=self.db_engine,
        schema='develop',
        if_exists='append',
        index=False,
        dtype={'geom': Geometry('GEOMETRY', srid=4326)}
    )
```

---

## 📁 关键文件清单

### 新建文件（核心实现）

| 文件路径 | 说明 | 代码量 |
|----------|------|--------|
| `python-workflow/workflow_engine.py` | GeoPandas 工作流引擎（DAG 执行 + 内存传递） | ~300 行 |
| `python-workflow/api_server.py` | Flask API Server | ~200 行 |
| `python-workflow/operators.py` | 空间算子库（21个算子实现） | ~500 行 |
| `python-workflow/requirements.txt` | Python 依赖（geopandas, flask, gunicorn, psycopg2, sqlalchemy） | ~15 行 |
| `python-workflow/Dockerfile` | 容器化配置 | ~30 行 |
| `develop/backend/internal/service/spatial_workflow_service.go` | 空间工作流服务（转发+任务保存） | ~400 行 |
| `develop/backend/internal/api/spatial_handler.go` | API 处理器 | ~250 行 |
| `develop/frontend/src/views/SpatialWorkflowDesigner.vue` | DAG 画布（基于 @antv/g6） | ~600 行 |
| `develop/frontend/src/components/SpatialOperatorNode.vue` | 算子节点组件 | ~150 行 |

### 修改文件

| 文件路径 | 改动说明 | 改动量 |
|----------|----------|--------|
| `docker-compose.yml` | 新增 python-workflow-engine 服务 | +40 行 |
| `orchestrator/backend/internal/service/executor.go` | 扩展参数模板解析 | +100 行 |
| `develop/backend/internal/api/router.go` | 新增空间工作流路由组 | +15 行 |
| `scripts/dev/start.sh` | 新增 python-workflow-engine 启动步骤 | +20 行 |
| `scripts/prod/start.sh` | 新增 python-workflow-engine 容器启动 | +25 行 |
| `scripts/build/build-images.sh` | 新增 python-workflow-engine 镜像构建 | +15 行 |
| `Makefile` | 新增 python-workflow-engine 相关命令 | +10 行 |
| `CLAUDE.md` | 新增 Python Workflow Engine 章节 | +50 行 |
| `docs/PORTS.md` | 新增端口 8090 说明 | +5 行 |

---

## 🚀 实施步骤（简化版）

### 阶段 1: Python Workflow Engine 实现（3-4 天）

**任务**:
1. 创建 `python-workflow` 目录结构
2. 实现 `workflow_engine.py`（核心：GeoDataFrame 内存传递）
3. 实现 `operators.py`（21个空间算子封装）
4. 实现 `api_server.py`（Flask REST API）
5. 容器化（Dockerfile + docker-compose.yml）
6. 启动时自动注册到 System（POST /api/v1/internal/engines/register）

**验证**:
```bash
# 启动引擎
docker-compose up -d python-workflow-engine

# 验证健康检查
curl http://localhost:8090/health

# 测试工作流执行
curl -X POST http://localhost:8090/api/spatial/workflow \
  -H "Content-Type: application/json" \
  -d '{"tasks": [{"id": "t1", "operator": "buffer", "params": {...}}]}'

# 验证 System 注册
curl http://localhost:8180/internal/registry/compute-engines | \
  jq '.[] | select(.engine_type == "python_workflow")'
```

---

### 阶段 2: Develop 模块集成（4-5 天）

**任务**:
1. **Backend**:
   - 创建 `spatial_workflow_service.go`（转发到 Python Workflow Engine）
   - 创建 `spatial_handler.go`（API 端点）
   - 数据库迁移：添加 `develop.spatial_tasks` 表
2. **Frontend**:
   - 创建 `SpatialWorkflowDesigner.vue`（基于 @antv/g6 的 DAG 画布）
   - 实现算子拖拽、参数配置、即时执行
   - 实现"保存为任务"功能
   - 结果地图预览（从 PostGIS 读取）

**验证**:
- [ ] 在 Develop 模块拖拽设计工作流
- [ ] 点击"执行"，查看地图上的结果
- [ ] 点击"保存为任务"，检查 `develop.spatial_tasks` 表记录
- [ ] 验证 PostGIS 表 `develop.spatial_execution_results` 中有空间数据

---

### 阶段 3: Orchestrator 集成（2-3 天）

**任务**:
1. 扩展 `executor.go`，实现 `resolveTemplateReferences` 函数
2. Orchestrator Frontend 从 System 查询 GIS 任务
3. 支持拖拽 GIS 任务节点到编排画布
4. 测试混合编排（SQL → GIS → Transfer）

**验证**:
- [ ] Orchestrator 任务库显示 Python Workflow Engine
- [ ] 点击展开，显示从 `develop.spatial_tasks` 动态查询的任务列表
- [ ] 创建混合编排工作流
- [ ] 执行成功，步骤间数据通过 `{{step.result.*}}` 正确传递

---

### 阶段 4: 测试与优化（2-3 天）

**任务**:
1. 性能测试（大 GeoDataFrame 处理）
2. 错误处理完善（Python 异常映射）
3. 文档编写（CLAUDE.md, develop/CLAUDE.md, orchestrator/CLAUDE.md）
4. 更新所有相关脚本（scripts/dev/start.sh, scripts/prod/start.sh, Makefile）

---

## ⚠️ 关键风险

| 风险点 | 缓解措施 |
|--------|----------|
| **GeoDataFrame 内存占用过大** | 实现分块处理（batch_size 参数） |
| **GeoPandas 版本兼容性** | 固定版本（requirements.txt） |
| **前端 DAG 画布复杂度** | 使用成熟库（@antv/g6），参考 Orchestrator 现有实现 |
| **Python/Go 数据类型不一致** | 使用 GeoJSON 标准格式，严格接口契约 |
| **PostGIS 连接管理** | 使用 SQLAlchemy 连接池，避免连接泄漏 |

---

## 📊 工作量估算

| 阶段 | 任务 | 开发天数 |
|------|------|---------|
| 阶段 1 | Python Workflow Engine | 3-4 天 |
| 阶段 2 | Develop 模块集成 | 4-5 天 |
| 阶段 3 | Orchestrator 集成 | 2-3 天 |
| 阶段 4 | 测试与优化 | 2-3 天 |
| **总计** | - | **11-15 天** |

---

## 🎁 预期成果

### 功能成果

1. **Python Workflow Engine 独立运行**:
   - 21 个空间算子内置（几何处理、空间关系、批处理等）
   - GeoDataFrame 内存高效传递
   - 最终结果写入 PostGIS 空间表

2. **双模式访问**:
   - **Develop 模块**: 工作流设计器（DAG 画布），即时执行或保存为任务
   - **Orchestrator 模块**: 从 Python Workflow Engine 动态发现任务，混合编排

3. **步骤间数据传递**:
   - 支持 `{{step1.result.result_table}}` 模板引用
   - Orchestrator 自动解析嵌套路径

### 技术成果

1. **扩展能力**:
   - Orchestrator 支持任意计算引擎的参数模板化（不限于 GIS）
   - 为未来集成其他 Python 引擎（Pandas、Scikit-learn）提供参考架构

2. **架构价值**:
   - 验证了 Python/Go 混合架构的可行性
   - Transfer 模式的成功复用（只注册引擎，任务动态发现）

---

## 🔗 相关文档

- **ADDP 架构文档**: [CLAUDE.md](CLAUDE.md)
- **Orchestrator 现有实现**: [orchestrator/backend/internal/service/executor.go](orchestrator/backend/internal/service/executor.go)
- **Transfer 模块参考**: [transfer/backend/internal/api/router.go](transfer/backend/internal/api/router.go)
- **Labs 空间算子**: [labs/dolphin/backend/spatial/operators.py](labs/dolphin/backend/spatial/operators.py)

---

## ✅ 下一步行动

1. **立即启动**: 阶段 1 - Python Workflow Engine 实现（3-4 天）
2. **并行准备**: 设计数据库 schema（develop.spatial_tasks）
3. **中期里程碑**: 阶段 2 完成后，Develop 模块可独立使用（约 7-9 天）
4. **最终交付**: 阶段 4 完成后，完整功能上线（约 11-15 天）

**关键成功因素**:
- ✅ Python Workflow Engine 只注册自己，不注册具体算子
- ✅ 任务存储到 develop.spatial_tasks，Orchestrator 通过 API 动态查询
- ✅ 最终结果存储到 PostGIS 空间表（geom GEOMETRY），非 JSONB
- ✅ 完善错误处理和用户提示
- ✅ 更新所有相关脚本和文档

---

**计划编制时间**: 2025-12-11
**预计开始时间**: 待用户确认
**预计完成时间**: 开始后 11-15 个工作日
