# ADDP 工作流计算引擎接口规范

## 文档概览

**版本**: v1.0.0
**最后更新**: 2026-07-12
**适用引擎**: Math Workflow、GeoPython Workflow、Spark Workflow、Model3D Workflow、PointCloud Workflow、SuperMap Workflow

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
| **GeoPython Workflow** | 8099 | Python + Pandas + GeoPandas | 中小规模空间和数据处理 | 42+ |
| **Spark Workflow** | 8098 | PySpark + Sedona | 大规模分布式计算 | 35+ |
| **Model3D Workflow** | 8101 | Python wrapper + 三维转换 CLI | 三维模型、BIM、高斯泼溅与 3D Tiles 持久化转换 | workflow + direct 转换算子 |
| **PointCloud Workflow** | 8102 | Python wrapper + PDAL | LAS / LAZ / E57 / PCD / XYZ 转持久化 COPC | workflow + direct 转换算子 |
| **SuperMap Workflow** | 8103 | Java + SuperMap iObjects Java / SPS | 超图数据格式、空间分析与 SPS DAG 内存对象传递 | 21 个真实算子（19 个 workflow + 2 个 direct） |

### 1.3 引擎目录结构

采用python实现的工作流引擎可参考下面目录结构：

```
engines/<engine-name>/
├── api_server.py              # Flask API 服务（必需）
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
- **common-python/addp_common/workflow_runtime**: Python Runtime 共用的工作流定义校验、DAG 拓扑排序、引用解析和异步执行状态管理；Python 工作流运行时不得分别复制这些逻辑
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
│ Workflow      │  ← addp_common.workflow_runtime
│ Runtime Core  │
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

### 2.0 算子契约分层

工作流算子契约必须拆分为以下三层，三层职责不得混入同一个 `parameters[]`：

1. **Public Operator Spec**：面向用户、前端、AI 和 Develop 任务定义，声明公开参数、资源选择方式和校验规则。读取已有资源使用 `locator`，创建目标使用 `target_parent_locator + target_name`。
2. **Develop Adapter Spec**：由 Develop Backend 按当前 workflow engine type 和 operator id 显式选择，声明公开资源参数如何转换为运行时参数，并负责权限校验、System Engine Instance 查询和连接信息派生。
3. **Runtime Operator Spec**：由 Workflow Runtime 暴露和消费，只声明运行时真实需要的参数、输入输出端口和执行行为。

`ResourceLocator` 只属于 Public Operator Spec。GeoPython Workflow、Spark Workflow、SuperMap Workflow 等运行时不得解析 `addp://` locator。

`connection_info`、`engine_id` 以及由资源身份派生的 `schema/table/path` 属于 Develop Adapter Spec 到 Runtime Operator Spec 的内部参数。用户、前端和 AI 不得直接提交这些内部参数；只有在对应 Public Operator Spec 明确将同名字段定义为公开业务参数时才可例外。

需要同时读取源存储并写入目标存储的持久化转换算子，Runtime Operator Spec 使用 `addp.workflow.access-plan/v1` 工作流访问计划，不使用单个共享 `engine_id/connection_info` 表达两端资源。源与目标必须分别由调用方解析为执行期访问参数；运行时仍不得解析 ADDP `ResourceLocator`。

Develop Backend 不得仅因任意算子的参数中出现 `locator`、`target_parent_locator` 等固定名称就触发派生。每个需要资源适配的算子都必须存在显式 Develop Adapter Spec；未声明的算子携带公开资源参数时必须拒绝执行，不得回退到隐式派生路径。

Develop 的算子发现 API 必须为前端提供独立的 `public_parameters`。前端工作流编辑器、参数面板、输入连线和任务保存只允许消费 `public_parameters`，不得读取 Workflow Runtime 返回的 `parameters`。`parameters` 保留为 Runtime Operator Spec；在 Runtime 元数据完成净化前，即使其中仍存在历史 UI 或公开资源字段，也不得再作为前端契约使用。

Public Operator Spec 由 Develop Adapter Spec registry 统一维护。Workflow Runtime 的 `parameters[]` 禁止出现 `ui_type=resource_tree_picker`，禁止出现 `locator`、`target_parent_locator`、`target_name` 等 ADDP 公开资源身份参数；runtime 必须直接声明自身真实消费的 `connection_info/schema/table/path` 等执行参数。

Develop 聚合算子时必须校验 Runtime Operator Spec：发现 UI 参数或公开资源身份参数时拒绝该运行时算子目录；存在 Develop Adapter Spec 时，runtime `parameters[]` 必须完整声明 adapter spec 所需的运行时参数。不得在参数缺失时继续展示算子并等待执行阶段失败。

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
    engine_type: str         # 所属扩展引擎类型，如 math_workflow、geopython_workflow
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
    type: str                 # string、integer、float、boolean、array、object、geodataframe、dataframe，或引擎私有点分类型
    param_type: str           # 可选，input、output、param、ui；input 表示由工作流连线自动传入
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

工作流画布通过 `parameters[]` 和上游 `output_ports[]` 生成标准参数引用。运行时应优先用 `param_type="input"` 标识由连线传入的参数；上游 `output_ports[].type` 与下游 `parameters[].type` 应尽量使用精确类型匹配。对于运行时内部内存对象句柄，可以使用引擎私有点分类型，例如 `supermap.datasource`、`supermap.dataset`，避免多个 `object` 输入只能按连线顺序猜测。`param_type` 缺失时，Develop 仅可按下述历史命名约定识别输入参数：

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

Develop 对前端暴露的算子发现主路径必须是 `GET /api/v1/develop/workflow-engines/{workflow_engine_id}/operators`。`workflow_engine_id` 是 System 中已注册的具体工作流运行时实例 ID；API 路径不得包含 `geopython_workflow`、`spark_workflow`、`math_workflow` 等具体 `engine_type`，也不得使用 `module` 表达算子来源或分组。这样用户动态注册的新扩展工作流引擎只要具备 `compute.workflow` 能力，就能通过同一条路径被 Develop 发现和消费。

Develop 不提供按 `engine_type`、`module` 或全局汇总查询算子的公开 API；所有上层调用方必须先选择具体工作流引擎实例，再通过实例 ID 路径获取算子。这样可以避免同一 `engine_type` 多实例、用户扩展引擎或同名算子场景下发生混淆。

Develop 的算子发现 API 是工作流编排视角，只返回 `execution_modes` 显式包含 `workflow` 的算子。只支持 `direct` 的算子不得出现在工作流画布或 Copilot 工作流生成链路中；业务模块若需要调用 direct 算子，必须通过受控的单算子调用路径，并在调用前校验目标算子显式支持 `direct`。

Copilot 等上层智能生成链路也必须沿用同一实例 ID 契约：生成工作流时只接收 `workflow_engine_id`，不得要求前端额外提交 `engine_type` 作为生成上下文或算子发现条件。工作流引擎类型只能由 Develop/System 后端按实例 ID 查询得到，不能成为 API 输入侧的第二事实源。

### 4.3 引擎注册

工作流运行时注册后才成为 ADDP 可用运行时。生产内置运行时可以在启动时向 System Backend 自注册；参考示例运行时可以只作为扩展规范样例存在，由用户在 System 引擎管理中按扩展引擎手动注册。

GeoPython Workflow / Spark Workflow 等生产内置运行时自注册示例；需要外部 SDK 或许可绑定的运行时（例如 SuperMap Workflow）也可以启动后由 System 扩展引擎表单手动注册：

```python
# api_server.py

def register_to_system():
    """向 System Backend 自注册"""
    payload = {
        "engine_type": "geopython_workflow",
        "name": "GeoPython 工作流引擎",
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

内置扩展引擎和用户自研扩展引擎在 System 中待遇一致：业务模块不得假设 `geopython_workflow`、`spark_workflow`、`math_workflow`、`model3d_workflow`、`pointcloud_workflow` 等内置工作流引擎一定存在；只有已注册、启用且声明 `compute.workflow.supported=true` 的 Engine Instance 才能被发现和调用。生产内置工作流引擎自注册 payload 可以不提交 `capabilities`，由 System 按内置声明生成 `engine.capabilities/v1`；用户自研扩展引擎和参考示例引擎必须提交或由注册表单生成符合 `engine.capabilities/v1` 的 workflow 能力声明。算子列表、参数、分类、执行模式和输出端口通过 `GET /api/operators` 动态获取，不写入能力声明。

System 在保存声明 `compute.workflow.supported=true` 且 `compute.workflow.runtime_api="addp.workflow/v1"` 的手动注册扩展运行时前，必须执行一次只读协议探测：

- 校验 `engine.capabilities/v1` 的 `engine_type` 与注册请求中的 `engine_type` 完全一致。
- 调用该运行时实例的 `GET /health`，HTTP 状态必须为 `200`。
- 调用同一运行时实例的 `GET /api/operators`，响应必须是 `addp.workflow/v1` 算子列表结构。
- 若 `operators` 非空，必须对每个算子执行完整元数据契约校验；其中算子 `engine_type` 必须等于注册请求中的 `engine_type`，不得返回 `geopython_workflow`、`spark_workflow` 等其他运行时类型作为兼容值。
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

若某类工作流引擎需要绑定外部运行时资源，例如 `spark_workflow` 需要实际 Spark 资源 ID，应作为执行期运行时参数传入标准请求顶层字段（当前为 `engine_id`），而不是写入 `capabilities`，也不是由 Develop 等业务模块直接拼接引擎私有 HTTP 契约。对 `spark_workflow`，Develop 执行配置必须提供 `engine_specific.spark_cluster_id`；后端在调用 Provider 前必须校验该 ID 指向已启用的 `engine_type=spark` 通用引擎资源，并将其映射为标准请求顶层 `engine_id`。GeoPython Workflow、Math Workflow、Model3D Workflow、PointCloud Workflow、SuperMap Workflow 不需要也不得携带该 Spark 绑定。

Model3D Workflow 是三维模型转换专用运行时，`engine_type=model3d_workflow`，默认端口 `8101`。`osgb_to_glb`、`gltf_to_glb`、`fbx_to_glb`、`obj_to_glb`、`stl_to_glb`、`ifc_to_glb`、`osgb_scene_to_3dtiles` 和 `gaussian_splat_to_ksplat` 同时支持 workflow 与 direct。算子只表达持久化格式转换，不把 Manager 快显归属写入 Runtime Operator Spec；Manager direct 调用选择 infra 目标并登记私有 artifact，Develop workflow 调用要求用户选择业务存储并触发 Meta scan。`_3dtile`、`assimp`、`IfcConvert` 和 KSplat 脚本仍是运行时内部依赖。PLY / SPLAT 高斯泼溅可以转 KSplat；源已经是 KSplat 时直接作为业务数据项或 Manager 基础预览读取，不进入转换算子。

SuperMap Workflow 的 `osgb_scene_to_s3m` 同时支持 workflow 与 direct，使用 `addp.workflow.access-plan/v1`。源当前使用 NFS `mounted_path` whole scope；目标支持 NFS `mounted_path` 和 MinIO/S3 `object_store`。运行时必须调用 iObjects Java `OSGBCacheBuilder.generateConfigFile` 与 `CacheBuilderOSGBTool.osgb2s3m`，保留引擎生成的结果内相对引用；当前产物为 `config/scene.scp + config/scene/Data/**/*.s3m`，manifest 中的 `./scene/Data/...` 相对 `config/scene.scp` 解析，纹理压缩固定为 WebP，避免预览依赖 WebGL S3TC 压缩纹理上传能力。Develop workflow 模式写入用户选择的业务存储，结果经 Meta 扫描形成 `format=s3m + layout=whole` item；Manager direct 模式写入 Manager infra MinIO，登记为 `manager.model3d_tiles` 中 `target_format=s3m` 的受管快显结果，不形成 Meta item。

OSGB Scene 的 Manager 分块瓦片任务统一使用 `model3d_tiles_generation`。任务定义保存在 `manager.model3d_tiles_tasks`，`source.item_fingerprint + target_format` 标识当前目标结果；`target_format` 只允许 `3d_tiles`、`s3m`。`3d_tiles` 调用 `osgb_scene_to_3dtiles`，`s3m` 调用 `osgb_scene_to_s3m`。Manager 必须根据当前租户可用工作流引擎实例及其 direct 算子声明决定格式能否生成；已生成结果的读取不依赖工作流引擎在线。

PointCloud Workflow 是点云处理专用运行时，`engine_type=pointcloud_workflow`，默认端口 `8102`。`las_to_copc`、`laz_to_copc`、`e57_to_copc`、`pcd_to_copc` 和 `xyz_to_copc` 同时支持 workflow 与 direct。PDAL 是运行时内部依赖；运行时未绑定 PDAL 时 `/health` 返回 `degraded` 且不自注册。源和目标由调用方按 `addp.workflow.access-plan/v1` 派生；PointCloud Runtime 不解析 ADDP locator。对象存储源由运行时按 `object_store` 访问参数生成受控读取方式，COPC 始终先写入 `POINTCLOUD_WORK_DIR` / `CPL_TMPDIR`，再按目标访问参数发布。Manager direct 结果仍是 infra 快显 artifact；Develop workflow 结果是用户选择的业务 COPC 数据项。源已经是 COPC 时直接读取，不进入转换算子。

SuperMap Workflow 是超图数据格式和空间算法专用运行时，`engine_type=supermap_workflow`，默认端口 `8103`。第一版运行时由 Java 实现，对外提供 `addp.workflow/v1`，对内绑定 SuperMap iObjects Java `Bin`、GPA/SPS libs 和许可文件，并把 ADDP `workflow_def` 编译为 SPS workflow 后一次性执行 DAG。同一 JVM / SPS DAG 内优先通过 `IDataItem` 或等价内存对象传递中间结果，只有用户显式保存数据集或跨 HTTP 边界返回结果摘要时才落盘。当前真实算子覆盖 datasource open/open_postgis/create、dataset select/info/project/save、vector filter/spatial_filter/buffer/dissolve/merge/feature_envelope/inner_point/query、overlay intersect/clip/erase/union；`datasource.enable_postgis` 和 `datasource.upgrade_udbx` 是 direct-only 高危算子，不进入 Develop 工作流画布。`datasource.enable_postgis` 只允许由 System 引擎管理入口显式触发，用于初始化 SuperMap SDX+ 空间工作区。`datasource.upgrade_udbx` 对已有 UDBX 执行原位 schema 升级：运行时先以 SQLite 只读检查 SuperMap 系统表和 `SmRegister` 关键字段，再以可写方式打开数据源，由当前 iObjects Java SDK 完成官方 schema 迁移，关闭后重新检查并返回 `changed`、`schema_current` 和 `dataset_count`。该算子必须保持幂等；调用方负责权限确认、升级前备份和审计，不得在普通读取链路中隐式触发。`datasource.open_postgis` 只打开已有 PostGIS 空间表所在数据源，不得调用 SuperMap `create` 或默认创建 SuperMap `sm*` 系统表；`datasource.enable_postgis` 则可能在目标 PostgreSQL 数据库中创建 SuperMap `sm*` 系统表，必须经过 System 显式确认。ADDP `locator` 只属于 Develop/UI 的资源选择契约，Develop Backend 在调用 runtime 前必须把它派生为 `connection_info`、`schema` 和 `table` 并移除，SuperMap runtime 不解析 ADDP locator。SuperMap SDK、native `.so`、GPA libs 和许可文件属于 engine runtime 部署依赖，不进入 ADDP 代码仓库；运行时未绑定 objectsjava 或 GPA/SPS libs 时 `/health` 应返回不可用依赖，System 连接测试必须失败并提示具体缺失项。

单算子调用由 Common Engine 的 `WorkflowRuntimeProvider.InvokeOperator()` 统一调用，只允许调用 `execution_modes` 包含 `direct` 的算子。`InvokeOperator()` 是模块受控能力调用，不是任务执行入口；它不创建 Develop 任务，不进入 Orchestrator，也不进入 Monitor 通用执行监控。调用方模块必须持有明确业务目的并管理自身领域状态，例如 Manager 触发 `tiff_to_cog` 后负责记录 COG 生成结果状态、源 item fingerprint、目标 `storage_ref` 和失败原因。凡是需要任务编排、调度、重试、跨模块依赖或统一监控的执行，必须建模为工作流任务并走 `ExecuteWorkflow()`。

业务模块需要 direct 算子能力时，不得按内置 `engine_type` 硬编码查找运行时，例如不得要求 `geopython_workflow` 必须存在。调用方应声明自己需要的算子名和调用模式，例如 `operator=tiff_to_cog`、`execution_mode=direct`；Common Engine 或调用方模块应在当前租户可见的已启用 workflow 引擎中，通过 `ListOperators()` 查找实际提供该 direct 算子的运行时实例。有可用运行时则调用；没有任何运行时提供该算子时，该业务功能应进入“能力暂不可用”状态或产生明确失败原因，而不是回退到私有 HTTP、单节点 workflow 或固定内置引擎假设。

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

Develop Adapter Spec registry 为 Public Operator Spec 提供资源选择器 `ui_config`。过滤资源来源必须使用 `engine_families` / 能力族，例如表格资源使用 `tabular`、`dynamic_schema`，文件或对象资源使用 `file`、`object`。不得使用 `engine_types`，也不得把 `postgresql`、`mysql`、`nfs`、`minio` 等具体 `engine_type` 写成白名单，否则用户动态注册的同能力引擎无法被工作流画布发现和选择。Workflow Runtime 的 `parameters[]` 不得包含 `ui_config` 或任何资源树 UI 参数。

#### 持久化转换访问计划

文件、对象和目录型持久化转换统一使用以下内部契约：

```json
{
  "schema_version": "addp.workflow.access-plan/v1",
  "source": {
    "kind": "file",
    "format": "las",
    "entrypoint": "sample.las",
    "access": {
      "method": "mounted_path",
      "path": "/mnt/business/sample.las"
    }
  },
  "target": {
    "kind": "file",
    "format": "copc",
    "name": "sample.copc.laz",
    "write_mode": "create",
    "access": {
      "method": "object_store",
      "endpoint": "minio:9000",
      "bucket": "research",
      "object": "pointcloud/sample.copc.laz",
      "use_ssl": false
    }
  }
}
```

稳定规则：

1. `schema_version` 固定为 `addp.workflow.access-plan/v1`。
2. `source.kind` / `target.kind` 只表达 `file` 或 `directory`；`format` 使用平台规范格式标识。
3. `access.method` 第一版只允许 `mounted_path` / `object_store`。对象存储的下载、staging、presigned URL 或本地工作目录属于运行时实现，不扩展为 Public Operator 参数。
4. `target.write_mode` 只允许 `create` / `replace`，默认 `create`；不得在 `create` 失败后隐式回退为覆盖。
5. 访问计划只在执行期存在，可以携带临时连接信息；用户任务定义只保存 `locator`、`target_parent_locator + target_name` 和公开业务参数。
6. 调用方必须同时生成不含密钥的 audit plan，用于领域结果或统一执行记录；不得把运行时访问计划原文长期保存。
7. Manager infra 目标和 Develop 业务目标分别在调用方领域边界内解析，再进入同一个访问计划构造器。Develop 的源和目标存储必须是当前租户的业务 Engine Instance，不得选择 `tenant_id=nil` 的平台 infra 存储；Manager 可按私有 artifact 生命周期显式构造 infra 目标。访问计划不表达 owner module、artifact/data item 归属或 Meta 扫描策略。
8. Runtime 完成写入后只返回转换结果事实。Manager 负责登记私有 artifact；Develop 负责记录 `produced_targets` 并触发 Meta scan。

Python 实现的 Workflow Runtime 应共享 `common-python` 中的协议执行核心，包括 workflow definition 校验、DAG 拓扑排序、引用解析、异步 execution 状态和标准错误；GeoDataFrame、Spark DataFrame、PDAL、三维转换器等领域执行仍保留在各自运行时。该公共核心是库，不是新的 System Engine Instance 或独立服务。

资源选择器必须通过 `ui_config.resource_binding` 显式声明参数绑定，前端不得按算子 ID 或固定参数名猜测。读取已有资源使用 `mode=existing + locator_param`；创建目标使用 `mode=target + parent_locator_param + name_param`。需要同步公开访问形态时声明 `type_param + type_values`，需要选择后填充默认公开参数时声明 `default_params`。

资源选择后需要绑定标准元数据事实时，也必须由 `resource_binding` 显式声明目标参数。例如 `geometry_column_param` 表示从所选 item 的 `capabilities.spatial.geometry_columns[]` 读取可用几何列：单列自动选择；多列默认第一列并允许用户改选；无几何列时不得显示手工文本输入框。前端不得猜测 `geom`、`geometry` 等固定字段名。

同一算子能够读取多类资源时，应使用一个资源选择参数并由 locator 对应的资源事实决定运行时访问方式，不得要求用户预先选择 `source_type`。文件格式应优先由所选资源的格式事实或路径扩展名确定；支持格式列表属于资源选择器过滤条件，不应作为必选算子参数。内存对象输入应通过算子输入端口传递，不得在资源加载算子中另设 `geojson` 等旁路参数。

同一算子能够写入表格或文件目标时，也应使用一个目标父资源选择参数，由父 locator 类型决定 Adapter 派生 `schema/table` 或 `path`，不得要求用户预先选择 `target_type`。文件输出格式由目标名称扩展名确定，不应再公开独立 `format` 参数。

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

#### 步骤 3：接入 Python Workflow Runtime 公共核心

```python
from addp_common.workflow_runtime import ExecutionRegistry, WorkflowRunner

operator_ids = set(OPERATORS)
runner = WorkflowRunner(
    operator_ids,
    lambda operator, params: get_operator_function(operator)(**params),
)
executions = ExecutionRegistry()

# POST /api/workflow 校验请求后提交异步执行；GET /api/executions/<id> 读取状态。
snapshot = executions.submit(runner, workflow_def, input_data)
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
- DAG 校验、依赖解析与状态管理继续复用 `common-python/addp_common/workflow_runtime`；Spark 或 Dask 只作为算子执行器或 Runtime 私有调度后端接入，不复制公共 DAG 核心。

**Q2: 如何处理大文件输入？**
- 使用标准对象存储 `ResourceLocator`，例如 `locator=addp://engine/4/path/bucket/path/data.parquet?type=object&item_id=46`；Develop Backend 会在执行前派生运行时所需的 `connection_info` 和 Spark 可读 `s3a://bucket/path/data.parquet`。

**Q3: 如何支持异步执行？**
- `/api/executions/<id>` 是必需接口。单进程 Python Runtime 使用公共 `ExecutionRegistry`；需要跨进程或分布式执行时，可以替换其状态持久化和调度后端，但对外状态契约保持一致。未知 ID 必须返回 `EXECUTION_NOT_FOUND`。

---

## 附录

### A. 参考实现

- **Math Workflow**: `engines/math-workflow/` - 简单示例，适合学习
- **GeoPython Workflow**: `engines/python-workflow/` - 生产级 Python 实现，包含 Pandas / GeoPandas 等运行库
- **Spark Workflow**: `engines/spark-workflow/` - 分布式计算示例
- **Model3D Workflow**: `engines/model3d-workflow/` - 三维模型和 BIM 快显转换运行时
- **PointCloud Workflow**: `engines/pointcloud-workflow/` - 点云 COPC 快显转换运行时
- **SuperMap Workflow**: `engines/supermap-workflow/` - 超图 iObjects Java / SPS 工作流运行时

### B. 相关文档

- [ADDP 开发原则](./addp开发原则.md)
- [ADDP API 设计规范](./addp-api设计规范.md)
- [Develop 模块文档](../develop/CLAUDE.md)
- [System 引擎管理](../system/CLAUDE.md)

### C. 技术支持

如有问题，请参考各引擎的 `README.md` 或提交 Issue。

---

**文档维护**: 本文档应随引擎接口变更及时更新。
