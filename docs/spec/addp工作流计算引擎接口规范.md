# ADDP 工作流计算引擎接口规范

## 文档概览

**版本**: v1.0.0
**最后更新**: 2026-01-09
**适用引擎**: Math Workflow、Python Workflow、Spark Workflow

本文档定义 ADDP 工作流运行时的 `addp.workflow/v1` HTTP 协议。该协议由 Common Engine 的 `WorkflowRuntimeProvider` 消费；工作流引擎仍必须先通过 `EnginePlugin` 和 `engine.capabilities/v1` 纳入 System 统一引擎体系。

---

## 目录

1. [总体架构](#1-总体架构)
2. [API 统一规范](#2-api-统一规范)
3. [算子元数据规范](#3-算子元数据规范)
4. [Develop 模块集成](#4-develop-模块集成)
5. [扩展新引擎指南](#5-扩展新引擎指南)

---

## 1. 总体架构

### 1.1 设计理念

ADDP 工作流计算引擎采用 `EnginePlugin + WorkflowRuntimeProvider + HTTP runtime` 的插件化架构，通过统一 REST 协议提供计算能力。这种设计具有以下优势：

- **技术栈隔离**: 每个引擎可使用不同的计算框架或运行库（Pandas / GeoPandas、Spark、纯数学库）
- **独立扩展**: 引擎可独立部署、升级和扩容
- **统一调用**: Develop 模块通过 `WorkflowRuntimeProvider` 调用不同引擎，无需直接拼接引擎私有 HTTP 契约

### 1.2 当前引擎列表

| 引擎名称 | 端口 | 技术栈 | 适用场景 | 算子数量 |
|---------|------|--------|---------|---------|
| **Math Workflow** | 8089 | Python + 基础数学库 | 数学运算、学习示例 | 5 |
| **Python Workflow** | 8099 | Python + Pandas + GeoPandas | 中小规模空间和数据处理 | 42+ |
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
├── README.md                  # 引擎说明与运行示例
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
| **单算子调用** | POST | `/api/operators/<name>/invoke` | 受控调用单个算子，不进入 ADDP 任务体系 |
| **执行状态查询** | GET | `/api/executions/<execution_id>` | 查询执行状态 |

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
      "id": "add",
      "name": "add",
      "display_name": "加法",
      "engine_type": "math_workflow",
      "category": "数学运算",
      "category_path": ["数学运算"],
      "description": "两数相加",
      "brief_description": "计算两个数的和",
      "execution_modes": ["workflow"],
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
  "input_data": {},  // 可选，外部输入数据
  "engine_id": 34    // 条件必填：spark_workflow 必须提供，指向实际 Spark 通用引擎资源
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

`final_result` 和 `all_results` 必须可 JSON 序列化。对于 Spark DataFrame 等大规模运行时对象，工作流引擎不得把完整数据集塞入响应；应返回轻量结果摘要，例如类型、schema、少量预览行和预览上限。需要持久化或跨模块消费完整数据时，必须通过 `save` 等算子写入目标资源，并在结果中返回目标资源引用或保存摘要。

**错误响应**：
```json
{
  "status": "failed",
  "error": "工作流执行失败: 除数不能为零",
  "error_code": "EXECUTION_FAILED",
  "details": "任务 task3 执行失败"
}
```

#### 2.2.4 调用单个算子

单算子调用用于业务模块受控消费扩展引擎能力，例如 Manager 触发 `tiff_to_cog` 生成 COG 生成结果。它不是任务执行入口，不创建 Develop 工作流任务，不进入 Orchestrator 编排，也不进入 Monitor 通用任务监控；调用方模块必须负责自己的领域状态、审计和失败处理。

只有算子元数据 `execution_modes` 显式包含 `direct` 时，才允许通过该接口调用。未声明 `direct` 的算子只能作为工作流节点执行。

**请求**：
```http
POST /api/operators/add/invoke
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
  "result": 8,
  "execution_time_ms": 1.23
}
```

`execution_id` 对 direct 调用响应是可选字段。同步执行且调用方模块自行管理领域状态时可以不返回；如果引擎内部为 direct 调用生成了运行时执行记录，也可以返回该 ID 供诊断使用。

direct 调用除了 JSON 参数外，还可以携带一个二进制载荷，用于几何批、Arrow 记录批或其他需要避免 JSON 退化的场景。该载荷由 `binary_payload` 表达，和 `params` 并列：

```json
{
  "params": {
    "source_crs": "EPSG:3857",
    "target_crs": "EPSG:4326"
  },
  "binary_payload": {
    "content_type": "application/vnd.apache.arrow.stream",
    "encoding": "arrow",
    "name": "geometry_batch",
    "data": "base64-encoded-arrow-ipc"
  }
}
```

`binary_payload.data` 在 JSON 传输中按标准 base64 编码；运行时必须按二进制语义处理，不得再把几何数组展开成 JSON records。对于几何坐标转换等批处理算子，`binary_payload` 承载的内容必须只包含 geometry 列及其批元数据，不得混入其他属性字段。

对 `spark_workflow`，direct 调用请求仍必须携带顶层 `engine_id`，该 ID 指向真实 `engine_type=spark` 的通用引擎资源；Develop/Manager 等调用方必须由 `execution_config.engine_specific.spark_cluster_id` 或等价领域配置映射得到，不能和工作流运行时实例 ID 或数据源 locator 中的存储引擎 ID 混用。

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
  "result": 60,
  "all_results": {
    "task1": 30,
    "task2": 60
  },
  "progress": 100,
  "execution_time_ms": 12.34,
  "message": "执行完成"
}
```

状态查询响应只使用一个 `status` 表达运行时本地执行状态，不得再同时返回 `task_status`。同步执行的引擎也必须只对已知的本地 `execution_id` 返回状态；未知执行 ID 必须返回 404 和 `EXECUTION_NOT_FOUND`。

`POST /api/workflow` 可以同步完成并返回 `status="success"` / `status="failed"`，也可以仅完成受理并返回 `status="running"` / `status="pending"`。只要返回了 `running` 或 `pending`，运行时必须已经创建可查询的本地 `execution_id`，并保证 `GET /api/executions/{execution_id}` 能持续返回该执行的最新状态，直到进入 `success`、`failed` 或 `cancelled` 等终态。进入 ADDP 任务体系的调用方不得把 `running` / `pending` 当作成功；Develop 后端必须通过 `WorkflowRuntimeProvider.GetExecutionStatus()` 轮询到终态后，才更新 `common.task_executions` 的最终状态。

### 2.3 标准错误码

扩展工作流引擎返回失败时应使用对应的 HTTP 4xx/5xx 状态码，并在响应体中提供 `status="failed"`、`error_code` 和 `error`。调用方仍必须把标准响应体中的 `status="failed"` 或 `status="error"` 视为失败，即使运行时错误地返回了 HTTP 2xx；不得仅凭 HTTP 2xx 将工作流或 direct 调用标记为成功。

| 错误码 | 说明 | HTTP 状态码 |
|--------|------|------------|
| `OPERATOR_NOT_FOUND` | 算子不存在 | 404 |
| `DIRECT_NOT_SUPPORTED` | 算子未声明 `execution_modes: ["direct"]`，不允许单算子调用 | 403 |
| `EXECUTION_NOT_FOUND` | 执行记录不存在 | 404 |
| `INVALID_PARAMS` | 参数错误 | 400 |
| `EXECUTION_FAILED` | 执行失败 | 500 |
| `WORKFLOW_INVALID` | 工作流定义无效 | 400 |
| `INTERNAL_ERROR` | 内部错误 | 500 |

---

## 3. 算子元数据规范

### 3.1 算子描述结构

每个算子必须提供完整的描述定义（参考 `operators/base.py`）：

```python
class OperatorDescriptor:
    id: str                  # 唯一标识
    name: str                # 算子名称（API 调用时使用）
    display_name: str        # 显示名称（UI 显示）
    engine_type: str         # 所属扩展引擎类型，如 math_workflow、python_workflow
    type: str                # 可选，算子类型，如 spatial、non_spatial、general；不得用作 ADDP 业务模块
    category: str            # 算子分类/分组（数学运算、空间分析等）
    category_path: List[str] # 必填，多级分组目录；不需要多级目录时显式提供 [category]
    description: str         # 详细描述
    brief_description: str   # 简短描述
    execution_modes: List[str] # 必填，workflow 或 direct；只支持编排时也必须显式提供 ["workflow"]
    parameters: List[ParameterDescriptor]     # 输入参数
    output_ports: List[OutputPortDescriptor]  # 输出端口
    use_cases: List[str]     # 应用场景（可选）
    notes: List[str]         # 使用说明（可选）
    detailed_description: Dict[str, Any] # 可选，供 AI 生成、文档和高级 UI 使用
    inputs: List[Any]        # 可选，输入端口或输入类型摘要
    attributes: Dict[str, Any] # 可选，引擎自定义扩展属性
```

`module` 是 ADDP 业务模块概念，只用于 Meta、Manager、Transfer、Develop 等模块边界，不得用于表达算子来源或算子分组。工作流算子的来源必须使用 `engine_type`，算子面板分组必须使用 `category` / `category_path`。

Develop/Common 不为算子元数据合成默认值。扩展工作流引擎通过 `GET /api/operators` 返回的每个算子都必须显式提供 `id`、`name`、`display_name`、`engine_type`、`category`、`category_path`、`description`、`execution_modes`、`parameters`、`output_ports`；缺失任一字段都视为引擎实现不符合 `addp.workflow/v1`，调用方应拒绝使用该算子列表，而不是自动补齐。

参数元数据必须使用结构化 `parameters[]`，不得再使用历史 `paramDefinitions`、`paramDefs` 或自由文本参数描述：

```python
class ParameterDescriptor:
    name: str
    type: str                 # string、integer、float、boolean、array、object、geodataframe、dataframe
    required: bool
    description: str
    default: Any              # 可选
    enum: List[str]           # 可选
    min: float                # 可选
    max: float                # 可选
    pattern: str              # 可选
    item_type: str            # 可选，数组元素类型
    properties: Dict[str, ParameterDescriptor] # 可选，object 参数属性
    depends_on: str           # 可选，依赖参数名
    show_when: Dict[str, Any] # 可选，条件显示规则
    notes: str                # 可选
    ui_type: str              # 可选，例如 resource_tree_picker
    ui_config: Dict[str, Any] # 可选，UI 组件配置；资源过滤必须使用 engine_families
```

`execution_modes` 取值：

| 值 | 含义 |
| --- | --- |
| `workflow` | 算子可作为工作流节点执行。默认必备。 |
| `direct` | 算子允许被业务模块通过 `InvokeOperator` / `/api/operators/{name}/invoke` 直接调用；该调用不进入任务体系。 |

未显式声明 `direct` 的算子不得被单算子调用。`direct` 适用于 `tiff_to_cog`、`raster_info`、`validate_cog` 这类模块受控能力；需要调度、重试、跨模块编排或统一监控的场景必须走工作流任务。

对于需要二进制批处理的 direct 算子，建议在 `attributes` 中声明最小调用契约，例如：

```json
{
  "direct_binary": {
    "content_type": "application/vnd.apache.arrow.stream",
    "encoding": "arrow",
    "input_name": "geometry_batch",
    "output_name": "geometry_batch"
  }
}
```

调用方不得依赖 `attributes` 作为唯一能力事实源；它只用于描述 direct 二进制载荷的调用约定，真正是否可调用仍以 `execution_modes` 和运行时响应为准。

### 3.2 算子参数命名约定

所有算子的参数命名必须遵循以下约定，确保工作流配置能够正确映射：

#### 输入参数命名规则

工作流画布通过 `parameters[]` 和上游 `output_ports[]` 生成标准参数引用：

```json
{
  "input_df": {"$ref": "load_roads", "port": "default"}
}
```

Develop 前端不得硬编码只向 `input_gdf` 或 `input_df` 写入引用；必须按目标算子的参数定义选择与上游输出端口类型兼容的输入参数。多输出端口引用使用 `{ "$ref": "...", "port": "..." }`；默认端口可省略 `port`。

允许作为工作流连线自动填充的输入参数名如下：

| 场景 | 空间数据（GeoDataFrame） | 非空间数据（DataFrame） |
| --- | --- | --- |
| 单输入主数据 | `input_gdf` | `input_df` |
| 二元关系/叠加分析第二输入 | `gdf_b`、`mask_gdf` | `df_b` |
| 左右表连接 | `left_df`、`right_df` | `left_df`、`right_df` |
| 列表输入 | `gdf_list` | `df_list` |

**禁止**使用 `gdf_a`、`df_a` 等非 `input_` 前缀的主输入参数名。

示例：

```python
# ✅ 正确
def intersection(input_gdf: gpd.GeoDataFrame, gdf_b: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    ...

# ❌ 错误（工作流配置无法正确映射）
def intersection(gdf_a: gpd.GeoDataFrame, gdf_b: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    ...
```

#### 几何列访问规则

**禁止**通过硬编码列名字符串访问几何列，必须使用 GeoDataFrame 的 `geometry` 属性或 `geometry.name`，以兼容任意几何列名（`geom`、`geometry`、`shape` 等）：

```python
# ✅ 正确：通过 active geometry 属性访问，兼容任意列名
result = input_gdf.copy()
geom_col = result.geometry.name        # 获取实际列名（可能是 'geom'、'geometry' 等）
result[geom_col] = result.geometry.buffer(distance)

# ❌ 错误：硬编码列名，若数据库几何列名为 'geom' 则报 KeyError
result['geometry'] = result['geometry'].buffer(distance)
```

> **背景**：PostGIS 数据库中几何列名通常为 `geom`，而非 `geometry`。GeoDataFrame 的 `.geometry` 属性始终指向 active geometry 列，与实际列名无关。

### 3.4 `vector_reproject` 算子契约

`vector_reproject` 是空间坐标参考转换算子，只负责 geometry 批转换，不负责外部数据源接入、字段保留或写出。

```python
def vector_reproject(
    input_gdf: gpd.GeoDataFrame,
    source_crs: str = "",
    target_crs: str = "EPSG:4326",
) -> gpd.GeoDataFrame:
    ...
```

| 参数 | 类型 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| `input_gdf` | `GeoDataFrame` | 是 | 无 | 输入几何批。direct 调用时由 `binary_payload` 解码得到，workflow 调用时由上游节点传入。 |
| `source_crs` | `string` | 条件必填 | 空 | 源 CRS。若输入批本身没有 CRS，则必须显式提供。 |
| `target_crs` | `string` | 否 | `EPSG:4326` | 目标 CRS。GeoJSON 标准导出默认使用该值。 |

| 输出 | 类型 | 说明 |
| --- | --- | --- |
| `default` | `GeoDataFrame` | 转换后的几何批，只保留 geometry 和原有属性列；direct 二进制调用时由运行时重新编码为 `binary_payload`。 |

`vector_reproject` 的 direct 调用约定如下：

1. `binary_payload.content_type` 必须为 `application/vnd.apache.arrow.stream`。
2. `binary_payload.encoding` 必须为 `arrow`，且 Arrow 批只能包含一个 geometry 列。
3. geometry 列元素必须为 EWKB 二进制值，Arrow schema metadata 必须声明 `addp.geometry.encoding=ewkb`；HTTP `binary_payload.metadata` 中也必须声明 `geometry_encoding=ewkb`。
4. 算子只转换 geometry，不得接管 Transfer 的 load/save、checkpoint、属性字段重排或 GPKG 中间产物。
5. 失败时必须返回明确错误，不得静默退回原坐标。
6. 输出 `binary_payload` 仍为 Arrow + EWKB，只包含转换后的 geometry 列；输出 metadata 中 `source_crs` 和 `target_crs` 都应写入输出几何当前 CRS，不能继续保留输入 CRS。

---

### 3.3 算子实现规范

以 Math Workflow 的 `add` 算子为例：

```python
# operators/math_operators.py

def add(a: float, b: float) -> float:
    """加法算子"""
    return a + b

ADD_DESCRIPTOR = OperatorDescriptor(
    id="add",
    name="add",
    display_name="加法",
    engine_type="math_workflow",
    category="数学运算",
    category_path=["数学运算"],
    description="两数相加",
    brief_description="计算两个数的和",
    execution_modes=["workflow"],
    parameters=[
        ParameterDescriptor(
            name="a",
            type="float",
            required=True,
            description="加数1",
            default=0.0
        ),
        ParameterDescriptor(
            name="b",
            type="float",
            required=True,
            description="加数2",
            default=0.0
        )
    ],
    output_ports=[
        OutputPortDescriptor(
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
        "descriptor": ADD_DESCRIPTOR.to_dict()
    }
}
```

### 3.4 算子分类建议

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

Develop 模块通过 Common Engine 的 `WorkflowRuntimeProvider` 统一调用各工作流引擎：

```
Develop Backend (Go)
├── internal/api/workflow_handler.go         # API 层：接收前端请求
├── internal/service/workflow_engine_service.go  # 服务层：通过 common/engine provider 调用运行时
└── internal/models/execution_config.go      # 配置模型：执行参数
```

### 4.2 调用流程

```go
// 1. 前端先向 Develop Backend 获取可用工作流引擎实例
GET /api/v1/develop/workflow-engines

// 2. 用户选择具体工作流引擎实例后，前端按实例 ID 获取该实例的算子目录
GET /api/v1/develop/workflow-engines/{workflow_engine_id}/operators

// 3. Develop Backend 查询该引擎实例，确认它已启用且具备 compute.workflow 能力
engine := systemClient.GetEngineByID(workflowEngineID)
assert engine.IsActive && SupportsComputeEntrypoint(engine, "workflow")

// 4. Common Engine 按该实例的 engine_type 取得 WorkflowRuntimeProvider
provider := commonEngine.GetWorkflowRuntimeProvider(engine.EngineType)

// 5. Provider 按 addp.workflow/v1 HTTP 协议调用该运行时实例的 GET /api/operators
operators := provider.ListOperators(ctx, engine.ConnectionInfo)

// 6. 执行工作流时，Develop 仍使用工作流引擎实例 ID 作为 execution_config.engine_id
resp := provider.ExecuteWorkflow(ctx, engine.ConnectionInfo, workflowRequest)
```

Develop 对前端暴露的算子发现主路径必须是 `GET /api/v1/develop/workflow-engines/{workflow_engine_id}/operators`。`workflow_engine_id` 是 System 中已注册的具体工作流运行时实例 ID；API 路径不得包含 `python_workflow`、`spark_workflow`、`math_workflow` 等具体 `engine_type`，也不得使用 `module` 表达算子来源或分组。这样用户动态注册的新扩展工作流引擎只要具备 `compute.workflow` 能力，就能通过同一条路径被 Develop 发现和消费。

Develop 不提供按 `engine_type`、`module` 或全局汇总查询算子的公开 API；所有上层调用方必须先选择具体工作流引擎实例，再通过实例 ID 路径获取算子。这样可以避免同一 `engine_type` 多实例、用户扩展引擎或同名算子场景下发生混淆。

Develop 的算子发现 API 是工作流编排视角，只返回 `execution_modes` 显式包含 `workflow` 的算子。只支持 `direct` 的算子不得出现在工作流画布或 Copilot 工作流生成链路中；业务模块若需要调用 direct 算子，必须通过受控的单算子调用路径，并在调用前校验目标算子显式支持 `direct`。

Copilot 等上层智能生成链路也必须沿用同一实例 ID 契约：生成工作流时只接收 `workflow_engine_id`，不得要求前端额外提交 `engine_type` 作为生成上下文或算子发现条件。工作流引擎类型只能由 Develop/System 后端按实例 ID 查询得到，不能成为 API 输入侧的第二事实源。

### 4.3 引擎注册

工作流运行时注册后才成为 ADDP 可用运行时。生产内置运行时可以在启动时向 System Backend 自注册；参考示例运行时可以只作为扩展规范样例存在，由用户在 System 引擎管理中按扩展引擎手动注册。

Python Workflow / Spark Workflow 等生产内置运行时自注册示例：

```python
# api_server.py

def register_to_system():
    """向 System Backend 自注册"""
    payload = {
        "engine_type": "python_workflow",
        "name": "Python Workflow 计算引擎",
        "connection_info": {
            "protocol": "http",
            "port": 8099
        },
        "is_builtin": True
    }

    requests.post(f"{SYSTEM_URL}/api/v1/internal/engines/register",
                  json=payload,
                  headers={"X-Internal-API-Key": API_KEY})
```

Math Workflow 是 `addp.workflow/v1` 参考实现，可以随 ADDP 开发环境启动服务，但不随启动自动注册。需要使用时，在 System 引擎管理中选择“注册扩展引擎”，填入 `engine_type=math_workflow`、默认端口 `8089`，可先通过表单“检查服务”确认 `/health` 与 `/api/operators` 可达，再测试连接并保存。

内置扩展引擎和用户自研扩展引擎在 System 中待遇一致：业务模块不得假设 `python_workflow`、`spark_workflow`、`math_workflow` 等内置工作流引擎一定存在；只有已注册、启用且声明 `compute.workflow.supported=true` 的 Engine Instance 才能被发现和调用。生产内置工作流引擎自注册 payload 可以不提交 `capabilities`，由 System 按内置声明生成 `engine.capabilities/v1`；用户自研扩展引擎和参考示例引擎必须提交或由注册表单生成符合 `engine.capabilities/v1` 的 workflow 能力声明。算子列表、参数、分类、执行模式和输出端口通过 `GET /api/operators` 动态获取，不写入能力声明。

System 在保存声明 `compute.workflow.supported=true` 且 `compute.workflow.runtime_api="addp.workflow/v1"` 的手动注册扩展运行时前，必须执行一次只读协议探测：

- 校验 `engine.capabilities/v1` 的 `engine_type` 与注册请求中的 `engine_type` 完全一致。
- 调用该运行时实例的 `GET /health`，HTTP 状态必须为 `200`。
- 调用同一运行时实例的 `GET /api/operators`，响应必须是 `addp.workflow/v1` 算子列表结构。
- 若 `operators` 非空，必须对每个算子执行完整元数据契约校验；其中算子 `engine_type` 必须等于注册请求中的 `engine_type`，不得返回 `python_workflow`、`spark_workflow` 等其他运行时类型作为兼容值。
- 探测阶段只验证协议面、必填字段、分类、执行模式和输出端口结构；不做算子参数枚举、范围、正则、业务语义等深度校验。

System 管理界面可以在用户保存前提供手动“测试连接/协议探测”入口，便于用户先确认运行时可达和算子契约有效。该入口只能复用创建前测试 API，不能在前端或 System 后端另写一套按具体 `engine_type` 判断的探测逻辑。

探测失败时 System 不得保存该引擎，也不得降级为“已保存但不可用”。创建前测试 API 和最终保存入口必须使用同一条 Common Engine / `WorkflowRuntimeProvider` 主路径；最终保存入口仍必须再次探测，不能因为前端已经手动测试通过就跳过后端校验。普通通用引擎仍按对应 `EnginePlugin.TestConnection()` 做只读连接测试。

工作流执行由 Common Engine 的 `WorkflowRuntimeProvider.ExecuteWorkflow()` 统一调用。Common Engine 在调用运行时前必须通过同一工作流运行时实例的 `ListOperators()` 获取算子列表，严格校验算子元数据，并确认工作流定义中每个 `tasks[].operator` 存在且 `execution_modes` 显式包含 `workflow`；不满足时应拒绝执行，不能把 direct-only 算子交给工作流运行时编排。

Common Engine 同时必须校验工作流定义本身：

- `tasks` 必须是非空数组。
- 每个任务必须显式提供非空 `id`、非空 `operator`、对象类型 `params`、字符串数组 `depends_on`。
- `tasks[].id` 不得重复。
- `depends_on` 不得为空字符串、不得引用自身、不得重复、不得引用不存在的任务。
- 任务依赖图不得包含环。
- `params` 中出现 `{ "$ref": "<task_id>" }` 时，`<task_id>` 必须存在且必须同时出现在该任务的 `depends_on` 中。
- `{ "$ref": "<task_id>", "port": "<port_name>" }` 引用非默认端口时，`<port_name>` 必须存在于被引用任务算子的 `output_ports[]` 中。

工作流运行时仍必须保留同等或更细的校验防线，但不能把 Common Engine 的前置校验缺失作为运行时私有行为处理。

若某类工作流引擎需要绑定外部运行时资源，例如 `spark_workflow` 需要实际 Spark 资源 ID，应作为执行期运行时参数传入标准请求顶层字段（当前为 `engine_id`），而不是写入 `capabilities`，也不是由 Develop 等业务模块直接拼接引擎私有 HTTP 契约。对 `spark_workflow`，Develop 执行配置必须提供 `engine_specific.spark_cluster_id`；后端在调用 Provider 前必须校验该 ID 指向已启用的 `engine_type=spark` 通用引擎资源，并将其映射为标准请求顶层 `engine_id`。Python Workflow、Math Workflow 不需要也不得携带该 Spark 绑定。

单算子调用由 Common Engine 的 `WorkflowRuntimeProvider.InvokeOperator()` 统一调用，只允许调用 `execution_modes` 包含 `direct` 的算子。`InvokeOperator()` 是模块受控能力调用，不是任务执行入口；它不创建 Develop 任务，不进入 Orchestrator，也不进入 Monitor 通用执行监控。调用方模块必须持有明确业务目的并管理自身领域状态，例如 Manager 触发 `tiff_to_cog` 后负责记录 COG 生成结果状态、源 item fingerprint、目标 `storage_ref` 和失败原因。凡是需要任务编排、调度、重试、跨模块依赖或统一监控的执行，必须建模为工作流任务并走 `ExecuteWorkflow()`。

业务模块需要 direct 算子能力时，不得按内置 `engine_type` 硬编码查找运行时，例如不得要求 `python_workflow` 必须存在。调用方应声明自己需要的算子名和调用模式，例如 `operator=tiff_to_cog`、`execution_mode=direct`；Common Engine 或调用方模块应在当前租户可见的已启用 workflow 引擎中，通过 `ListOperators()` 查找实际提供该 direct 算子的运行时实例。有可用运行时则调用；没有任何运行时提供该算子时，该业务功能应进入“能力暂不可用”状态或产生明确失败原因，而不是回退到私有 HTTP、单节点 workflow 或固定内置引擎假设。

调用方模块在 direct 调用前必须通过同一个工作流运行时实例的 `ListOperators()` / `GET /api/operators` 获取算子元数据，并确认目标算子的 `execution_modes` 显式包含 `direct`；不得只依赖运行时 `/api/operators/{name}/invoke` 返回 403 作为权限判断。运行时仍必须保留 `DIRECT_NOT_SUPPORTED` 防线，调用方前置校验和运行时校验共同构成单一路径的能力边界。

运行时本地执行状态查询由 Common Engine 的 `WorkflowRuntimeProvider.GetExecutionStatus()` 统一调用，对应 `GET /api/executions/{execution_id}`。Develop、Orchestrator 或其他模块如果需要轮询工作流运行时本地状态，必须通过 Provider / dbbridge 主路径，不得自行拼接运行时私有 HTTP URL。这里的 `execution_id` 是工作流运行时返回的本地执行 ID，不是 `common.task_executions.execution_id`；进入 ADDP 任务体系后的统一执行状态仍以 `common.task_executions` 为准。

Develop 执行工作流时必须把工作流运行时的本地状态收敛为 ADDP 统一执行状态：`success` 才能写入统一成功态；`failed` / `error` / `cancelled` 必须写入统一失败或取消语义；`running` / `pending` 只能作为中间态轮询，不得直接结束统一 execution。运行时状态摘要可以保存到 execution metadata 的 `runtime_status`，但该字段只用于诊断，不作为 Monitor 或 Orchestrator 的主状态源。

业务模块使用 direct 调用时必须在本模块领域表或执行记录中保存最小审计信息，而不是把运行时响应作为唯一事实源。至少包括：

- **调用目的**：模块名、领域动作或任务类型，例如 Manager 的 `raster_cog_generation`。
- **运行时身份**：工作流运行时实例 ID、实例名称、`engine_type`，以及运行时返回的 `execution_id`（若有）。
- **算子身份**：算子 `id/name`、调用模式 `direct`、实际使用的参数策略或关键配置摘要。
- **资源身份**：源资源 locator / item fingerprint / source engine，目标 `storage_ref` 或等价领域引用；不得只记录运行时临时 URI。
- **领域状态**：调用前后的领域对象状态、产物状态、大小/空间事实等可供前端和后续任务消费的结果摘要。
- **失败信息**：错误码或错误消息、失败阶段、是否可重试；失败不能只留在运行时日志中。

以 Manager COG 生成结果为例，Manager 通过 `InvokeOperator("tiff_to_cog")` 调用实际提供该 direct 算子的工作流运行时，但 COG 是否可用以 `manager.raster_cog.status` 为准；`raster_cog` 记录中应保留源 item fingerprint、源 locator、目标 `storage_ref`、`workflow_runtime.engine_id/name/engine_type/execution_id/operator/mode`、栅格 facts 与失败原因。工作流运行时只负责执行 GDAL 转换，不负责登记 Manager 的 COG 结果生命周期。

### 4.4 工作流算子资源参数

用户、AI 和前端工作流画布配置数据源/目标时，资源身份必须使用标准 `ResourceLocator`：

- 读取已有资源：使用 `locator`，例如表/集合 `addp://engine/12/path/public/roads?type=table&item_id=99`，NFS 文件 `addp://engine/3/path/data/roads.csv?type=file&item_id=45`，对象存储对象 `addp://engine/4/path/addp/lake/roads.parquet?type=object&item_id=46`。
- 创建新目标：使用 `target_parent_locator + target_name`。父 locator 必须指向真实父节点，例如 schema/database、NFS root/directory、对象存储 bucket/prefix；不得提前构造尚不存在的虚拟目标 locator。

Develop Backend 在调用 `WorkflowRuntimeProvider.ExecuteWorkflow()` 前负责把上述公开参数派生为运行时参数：

- table/collection：派生 `connection_info + schema + table`。
- NFS/file：派生 `connection_info + path`，其中 `path` 为挂载根内相对路径。
- MinIO/S3 object：派生 `connection_info + path`，其中 Spark Workflow 的运行时 `path` 使用 `s3a://bucket/key`。

算子 `source_type` / `target_type` 表达访问形态，统一使用 `table`、`file`、`geojson` 等值；不得使用 `nfs`、`minio`、`s3` 等存储引擎类型作为算子数据源类型。具体存储引擎类型只来自 locator 对应的 System engine 及派生后的 `connection_info.engine_type`。

若算子元数据为前端资源选择器提供 `ui_config`，过滤资源来源必须使用 `engine_families` / 能力族，例如表格资源使用 `tabular`、`dynamic_schema`，文件或对象资源使用 `file`、`object`。不得使用 `engine_types`，也不得把 `postgresql`、`mysql`、`nfs`、`minio` 等具体 `engine_type` 写成白名单，否则用户动态注册的同能力引擎无法被工作流画布发现和选择。

`connection_info`、`schema`、`table`、`path` 和用于派生连接的存储引擎 `engine_id` 都是 Develop 到运行时之间的内部参数，不应作为算子公开填写项；Develop 不接受工作流任务参数直接提交存储引擎 `engine_id` 作为旧式资源身份。Spark Workflow 顶层 `engine_id` 只绑定实际 `spark` 通用引擎资源，与数据源 locator 中的存储引擎 ID 不是同一概念。

Copilot 在生成工作流前做数据源理解时也必须遵守同一资源契约：低置信度或未验证的数据源只能返回 `DataSourceCandidate[]` 给调用方澄清，候选项中的位置字段使用 `namespace`、`table`、`bucket`、`path` 作为解释性事实，并提供标准 `locator` 或 `target_parent_locator`；不得把元数据搜索结果中的 `schema` 字段透传为 Copilot 对外模型字段，也不得把存储引擎 `engine_id` 写入工作流任务 params。

---

## 5. 扩展新引擎指南

### 5.1 以 Stats Workflow 为例

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

# 定义算子描述...
OPERATORS = {
    "mean": {"function": mean, "descriptor": MEAN_DESCRIPTOR.to_dict()},
    "median": {"function": median, "descriptor": MEDIAN_DESCRIPTOR.to_dict()}
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
import time
import uuid
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
    start = time.time()
    data = request.get_json()
    engine = StatsWorkflowEngine()
    engine.load_workflow(data['workflow_def'])
    results = engine.execute(data.get('input_data', {}))
    final_task_id = next(reversed(results)) if results else None
    return jsonify({
        "status": "success",
        "execution_id": str(uuid.uuid4()),
        "final_result": results.get(final_task_id),
        "all_results": results,
        "execution_time_ms": (time.time() - start) * 1000
    })

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

# 保存算子列表并校验 addp.workflow/v1 算子元数据契约
curl http://localhost:8100/api/operators > /tmp/stats-workflow-operators.json
python engines/docs/workflow_operator_contract.py /tmp/stats-workflow-operators.json --engine-type stats_workflow

# 测试工作流执行
curl -X POST http://localhost:8100/api/workflow \
  -H "Content-Type: application/json" \
  -d '{
    "workflow_def": {
      "tasks": [
        {"id": "t1", "operator": "mean", "params": {"values": [1, 2, 3]}, "depends_on": []}
      ]
    }
  }'
```

### 5.2 关键要点

1. **遵循标准接口**: 确保实现 5 个标准 API 接口
2. **算子元数据完整**: 提供完整的必填字段、参数、执行模式和输出端口，并通过 `engines/docs/workflow_operator_contract.py` 校验
3. **错误处理**: 使用标准错误码，提供清晰的错误信息
4. **注册方式明确**: 生产内置运行时可启动自注册；参考实现和用户自研运行时可在 System 引擎管理中手动注册
5. **文档完善**: 编写 `README.md`；API 文档以 OpenAPI/Swagger 或本规范为准，不维护手写 API 清单

### 5.3 常见问题

**Q1: 如何支持分布式计算？**
- 参考 Spark Workflow，在 `workflow_engine.py` 中集成 Spark 或 Dask

**Q2: 如何处理大文件输入？**
- 使用标准对象存储 `ResourceLocator`，例如 `locator=addp://engine/4/path/bucket/path/data.parquet?type=object&item_id=46`；Develop Backend 会在执行前派生运行时所需的 `connection_info` 和 Spark 可读 `s3a://bucket/path/data.parquet`。

**Q3: 如何支持异步执行？**
- `/api/executions/<id>` 是必需接口。同步执行的引擎可以对已知本地 `execution_id` 返回已完成状态；未知 ID 必须返回 `EXECUTION_NOT_FOUND`。需要异步执行时可使用 Celery 或 Asynq 任务队列维护执行状态。

---

## 附录

### A. 参考实现

- **Math Workflow**: `engines/math-workflow/` - 简单示例，适合学习
- **Python Workflow**: `engines/python-workflow/` - 生产级 Python 实现，包含 Pandas / GeoPandas 等运行库
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
