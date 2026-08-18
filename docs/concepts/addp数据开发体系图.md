# ADDP 数据开发体系图

本文档展示 ADDP 平台的三种数据开发方式及其与引擎能力的关系。

---

## 目录

1. [三种开发方式概述](#三种开发方式概述)
2. [查询开发 (Query)](#查询开发-query)
3. [算子工作流 (Workflow)](#算子工作流-workflow)
4. [Notebook 开发](#notebook-开发)
5. [能力声明与开发界面映射](#能力声明与开发界面映射)

---

## 三种开发方式概述

ADDP 的 Develop 模块提供三种数据开发方式，由引擎的 `capabilities.compute` 能力决定：

```mermaid
graph TB
    Develop[Develop 模块<br/>数据开发]

    Develop --> Query[查询开发<br/>Query Development]
    Develop --> Workflow[算子工作流<br/>Operator Workflow]
    Develop --> Notebook[Notebook 开发<br/>Notebook Development]

    Query --> QueryUI[查询工作台界面<br/>Monaco 编辑器]
    Workflow --> WorkflowUI[工作流编辑器<br/>算子拖拽/DAG 可视化]
    Notebook --> NotebookUI[Notebook 编辑器<br/>Jupyter 界面]

    QueryUI --> QueryEngine[支持引擎:<br/>由 compute.query<br/>能力声明决定]

    WorkflowUI --> WorkflowEngine[支持引擎:<br/>GeoPython Workflow<br/>Spark Workflow]

    NotebookUI --> NotebookEngine[支持引擎:<br/>Jupyter]

    classDef module fill:#fff9c4,stroke:#f57f17
    classDef mode fill:#e1f5ff,stroke:#01579b
    classDef ui fill:#e8f5e9,stroke:#1b5e20
    classDef engine fill:#f3e5f5,stroke:#4a148c

    class Develop module
    class Query,Workflow,Notebook mode
    class QueryUI,WorkflowUI,NotebookUI ui
    class QueryEngine,WorkflowEngine,NotebookEngine engine
```

### 三种开发方式对比

| 维度 | 查询开发 (Query) | 算子工作流 (Workflow) | Notebook 开发 |
|------|-----------------|---------------------|--------------|
| **能力字段** | `compute.query.supported` | `compute.workflow.supported` | `compute.script.supported` |
| **界面** | 查询工作台 | 工作流编辑器 | Notebook 编辑器 |
| **编辑器** | Monaco（语言由 capability 声明） | 算子拖拽 + DAG 可视化 | Jupyter Notebook |
| **执行方式** | ad-hoc 或已保存 query execution | DAG 工作流执行 | Cell 逐个执行 |
| **适用场景** | 数据查询、统计分析 | 数据处理、空间分析 | 交互式探索、变量传递 |
| **支持引擎** | 可查询通用引擎 | 工作流运行时 | Jupyter 脚本运行时 |
| **结果展示** | capability 驱动的表格/图视图、导出 CSV | GeoDataFrame、可视化 | 文本、图表、地图 |
| **保存形式** | 可选 `query` 任务定义 + 统一 execution | DAG JSON + 执行记录 | .ipynb 文件 |

---

## 查询开发 (Query)

### 查询开发概述

**查询开发** 面向声明 `capabilities.compute.query.supported=true` 的 Engine Instance，查询语言、默认语言和结果形态分别来自 `languages`、`default_language` 和 `result_kinds`，Develop 不按 `engine_type` 维护私有映射。

```mermaid
graph LR
    User[用户] --> Catalog[Meta 资源树<br/>Native Engine 或联邦 Source Engines]
    Catalog --> Editor[查询工作台<br/>Monaco Editor]
    Editor --> CreateExecution[POST /develop/executions<br/>task_type=query]
    CreateExecution --> Unified[common.task_executions<br/>source_task_id 为空]
    Unified --> Runtime[Execution Authorization<br/>Query Runtime Provider]
    Runtime --> Result[受限结果预览<br/>表格或图视图]
    Result --> Monitor[统一执行详情]

    classDef user fill:#69db7c,stroke:#2f9e44
    classDef editor fill:#e1f5ff,stroke:#01579b
    classDef query fill:#fff9c4,stroke:#f57f17
    classDef engine fill:#e8f5e9,stroke:#1b5e20
    classDef result fill:#f3e5f5,stroke:#4a148c

    class User user
    class Catalog,Editor editor
    class CreateExecution,Unified query
    class Runtime engine
    class Result,Monitor result
```

### 查询开发特性

- **Catalog 浏览**：左侧直接消费 Meta resource-tree，不新增 Develop 私有 Catalog API。native query engine 展示自身原生路径；federated query runtime 不拥有 Catalog，工作台按 `federation.source_engine_types` 展示当前租户可参与查询的 Source Engine。
- **能力驱动编辑**：语言、高亮、格式化可用性和图结果入口来自 Engine query capability。
- **选区执行**：优先执行 Monaco 当前选区，没有选区时执行全文。
- **统一执行**：即时查询和已保存任务都创建 `common.task_executions`；即时查询使用 `task_type=query`、`source_task_id=null`。
- **结果预览**：服务端统一限制返回规模，并记录 `result_limit`、`truncated`、`result_kind` 和 execution ID。
- **防丢失**：未保存内容在离开页面或加载其他任务前必须确认。
- **任务收敛**：查询任务在 `/develop/tasks` 统一管理，不保留独立查询任务列表。

---

## 算子工作流 (Workflow)

### 算子工作流概述

**算子工作流** 是基于数据处理算子的可视化 DAG 工作流,用于空间和非空间数据分析。

```mermaid
graph TB
    subgraph "1. 引擎选择"
        EngineSelect[选择执行引擎]

        EngineSelect --> PyWF[GeoPython Workflow<br/>< 100万行<br/>内存计算]
        EngineSelect --> SparkWF[Spark Workflow<br/>> 100万行<br/>分布式计算]
        EngineSelect --> MathWF[Math Workflow<br/>数学计算（示范用）]
    end

    subgraph "2. 算子获取"
        GetOps[调用引擎API<br/>/api/operators]

        PyWF -.获取算子列表.-> GetOps
        SparkWF -.获取算子列表.-> GetOps
        MathWF -.获取算子列表.-> GetOps

        GetOps --> OpList[引擎支持的算子库<br/>空间和非空间算子]
    end

    subgraph "3. 工作流编辑"
        UI[工作流画布<br/>拖拽绘制]
        OpList --> UI

        UI --> DAG[生成 DAG 定义<br/>JSON 格式]

        DAG --> Task1[节点1: buffer]
        DAG --> Task2[节点2: intersection]
        DAG --> Task3[节点3: centroid]

        Task1 --> Task2
        Task2 --> Task3
    end

    subgraph "4. 执行提交"
        Submit[提交到引擎<br/>/api/workflow]
        DAG -.发送JSON.-> Submit

        Submit --> Parse[引擎解析DAG]
        Parse --> Execute[按拓扑序执行]
    end

    classDef engine fill:#e8f5e9,stroke:#1b5e20
    classDef operator fill:#e1f5ff,stroke:#01579b
    classDef workflow fill:#fff9c4,stroke:#f57f17
    classDef execution fill:#fce4ec,stroke:#c2185b

    class EngineSelect,PyWF,SparkWF,MathWF engine
    class GetOps,OpList operator
    class UI,DAG,Task1,Task2,Task3 workflow
    class Submit,Parse,Execute execution
```

### 重要说明

### 查询任务与 DuckDB 联邦查询

Develop 的 `query` 任务只有一条引擎绑定路径：`content.query_type` 保存真实查询语言，`execution_config.engine_id` 指向 System 中具备 `compute.query.supported=true` 的 Runtime Engine Instance。

- PostgreSQL、MySQL 等 native query engine 同时是 Runtime Engine 和唯一 Source Engine。
- DuckDB 是 System 中注册的共享联邦查询 Runtime Engine，不属于租户注册存储引擎，也不进入 Meta resource-tree。工作台选择 DuckDB 后，执行目标仍绑定 DuckDB Runtime，左侧 Catalog 则从当前租户 Meta Engine 列表中按 `federation.source_engine_types` 过滤 PostgreSQL、MySQL、MinIO/S3 等 Source Engine。
- Catalog selection 的 ResourceLocator 保留真实 Source Engine ID；插入联邦 SQL 时以 Source Engine 名称的规范标识符作为首段。SQL 引用的 Source Engine 逐个进入本次只读 Execution Authorization，不能用 DuckDB Runtime ID 替换。
- `/develop/engines` 按 capability 返回包括 DuckDB 在内的全部真实 Query Runtime；Develop 不再提供 `/develop/query-modes`、`query_mode=duckdb` 或 DuckDB 专用执行路由。
- DuckDB 样例由 Develop 从当前租户真实 Catalog/Meta 构造候选，逐个签发只读授权并调用 DuckDB Runtime 验证；没有返回数据的候选不能作为样例，也不允许退回 `SELECT 1`。

### Notebook 脚本任务

Notebook 使用 `dev_type="script"`，并在 `execution_config.engine_id` 中绑定 System 注册的 Notebook 引擎实例。Develop 只列出 `active`、`compute.script.modes` 包含 `notebook` 且 `compute.script.interactive=true` 的 Runtime Descriptor。新建空白与上传导入都创建同一种 script 任务和 MinIO Notebook 对象；编辑和执行沿用保存的引擎与 Kernel，不接受临时改绑。

已保存的 Notebook 可以由用户显式更换绑定引擎和 Kernel。重绑定直接更新原任务，不复制任务或 Notebook 文件，也不自动匹配替代引擎；目标引擎与 Kernel 校验通过后，`execution_config.engine_id` 和 `content.kernel` 必须在同一次数据库更新中生效。任务的新绑定只供后续执行使用，既有 `common.task_executions.execution_config` 快照保持不变。绑定引擎失效时，任务仍可查看、下载、删除和重绑定，但不可执行。

Develop 后端通过 `common/dbbridge.OpenScriptSession()` 消费 `ScriptRuntimeProvider`，并使用租户 Service Access Token 调用返回的受控运行端点。交互编辑由 Bearer `POST /notebooks/{id}/sessions` 创建，浏览器只得到同源 `/notebook-sessions/{session_id}/...` URL 和单会话 Path 限定 HttpOnly Cookie；Develop 校验后代理 JupyterLab HTTP/WebSocket 并注入内部 Runtime Token。关闭或过期时 Runtime 保存回原 MinIO 对象并清理 process。部署配置不提供 `JUPYTER_URL` 或共享 Lab 入口，Develop 也不代理引擎健康检查。

⚠️ **Spark Workflow 运行时特殊要求**:

当用户选择 **Spark Workflow** 运行时时，除了该运行时自注册记录本身，还需要：

1. **注册 Spark 通用引擎资源**作为运行时（`engine_type: "spark"`）
2. 在执行配置中指定 `spark_cluster_id`（指向实际的 Spark 通用引擎资源）
3. Develop 后端校验 `spark_cluster_id` 指向已启用的 `engine_type=spark` 通用引擎资源，再调用 `WorkflowRuntimeProvider.ExecuteWorkflow()` 并映射为标准请求顶层 `engine_id`
4. Spark Workflow 根据顶层 `engine_id` 读取 Spark 资源配置并创建 SparkSession

工作流算子中的数据源/目标资源不复用这个顶层 `engine_id`。用户和 AI 侧应使用 `locator` 或 `target_parent_locator + target_name` 选择表、文件、对象存储资源；Develop 后端在执行前派生数据源的 `connection_info` 以及 `schema/table` 或 `path`，再传给运行时。

**架构关系**：
```
Develop 执行配置 spark_cluster_id
    ↓ 映射为标准请求顶层 engine_id
Spark Workflow 运行时 (WorkflowRuntimeProvider)
    ↓ 读取 System 中的 spark 通用引擎资源
Spark 通用引擎资源 (运行时)
    ↓ 创建 SparkSession 并分布式执行
结果返回
```

相比之下，**GeoPython Workflow** 和手动注册后的 **Math Workflow** 自身就是可执行运行时，无需额外绑定 Spark 通用引擎资源。

### 算子工作流特点

**节点粒度**: 细粒度算子 (buffer、intersection、centroid)
**DAG 层级**: 算子级别的有向无环图
**数据传递**: GeoDataFrame 在内存中传递
**执行运行时**: GeoPython Workflow (单节点内存计算) 或 Spark Workflow (分布式计算)
**适用场景**: 数据分析、地理计算

### 典型算子

**空间算子** (21 个):
- **几何操作**: buffer、centroid、convex_hull、envelope
- **空间关系**: intersection、union、difference、symmetric_difference
- **空间查询**: within、contains、intersects、touches
- **坐标转换**: to_crs、reproject
- **其他**: dissolve、clip、overlay

**非空间算子**:
- **数据转换**: filter、select、aggregate
- **数学计算**: sum、mean、max、min
- **连接操作**: join、merge

---

## Notebook 开发

### Notebook 概述

**Jupyter Notebook** 交互式开发环境,支持 Python 和 Shell 代码执行。

```mermaid
graph TB
    subgraph "Notebook 编辑器"
        Notebook[Jupyter Notebook<br/>.ipynb 文件]

        Notebook --> Cell1[Cell 1: Markdown<br/>文档说明]
        Notebook --> Cell2[Cell 2: Code<br/>Python代码]
        Notebook --> Cell3[Cell 3: Code<br/>Shell命令]
    end

    subgraph "执行引擎"
        Jupyter[Jupyter 引擎<br/>jupyter]

        Jupyter --> Kernel[Python Kernel<br/>Shell Kernel]
    end

    subgraph "输出结果"
        Output[输出]

        Output --> Text[文本输出<br/>print结果]
        Output --> Chart[图表输出<br/>matplotlib/plotly]
        Output --> GeoDF[GeoDataFrame<br/>空间数据可视化]
    end

    Cell2 & Cell3 --> Jupyter
    Jupyter --> Output

    classDef editor fill:#e1f5ff,stroke:#01579b
    classDef engine fill:#fff9c4,stroke:#f57f17
    classDef output fill:#e8f5e9,stroke:#1b5e20

    class Notebook,Cell1,Cell2,Cell3 editor
    class Jupyter,Kernel engine
    class Output,Text,Chart,GeoDF output
```

### Notebook 特性

- **交互式开发**: Cell 逐个执行,实时查看结果
- **变量传递**: Cell 间共享变量,工作流间传递数据
- **多种输出**: 文本、图表、GeoDataFrame 可视化
- **Python Notebook**: 通过 Jupyter 运行 Python Notebook
- **保存回放**: .ipynb 文件保存,可重新执行

---

## 能力声明与开发界面映射

用户先选择开发界面，系统根据界面需求筛选支持对应 compute 能力的引擎:

```mermaid
sequenceDiagram
    participant User as 用户
    participant Develop as Develop 模块
    participant System as System 模块

    User->>Develop: 1. 选择开发界面<br/>(查询工作台/工作流编辑器/Notebook)
    Develop->>Develop: 2. 确定需要的 compute 能力<br/>(query/workflow/script)
    Develop->>System: 3. GET /api/v1/system/engines
    System-->>Develop: 4. 返回所有引擎<br/>(含 capabilities)
    Develop->>Develop: 5. 筛选支持对应能力的引擎<br/>读取 capabilities.compute

    alt 选择了查询工作台
        Develop-->>User: 6. 显示支持 "query" 的引擎列表<br/>(PostgreSQL, MySQL, MongoDB等)
    end

    alt 选择了工作流编辑器
        Develop-->>User: 7. 显示支持 "workflow" 的引擎列表<br/>(GeoPython Workflow, Spark Workflow等)
    end

    alt 选择了 Notebook 编辑器
        Develop-->>User: 8. 显示支持 "notebook" 的引擎列表<br/>(Jupyter等)
    end

    User->>Develop: 9. 选择具体引擎
    Develop->>User: 10. 渲染对应的开发界面
```

### 开发界面与引擎筛选规则

| 用户选择的界面 | 需要的能力 | 筛选后的引擎示例 | 编辑器组件 |
|--------------|----------------|----------------|-----------|
| 查询工作台 | `compute.query.supported=true` | PostgreSQL, MySQL, MongoDB, Neo4j, ClickHouse, Doris, Spark SQL | Monaco Editor |
| 工作流编辑器 | `compute.workflow.supported=true` | GeoPython Workflow, Spark Workflow, Model3D Workflow, PointCloud Workflow, SuperMap Workflow, Math Workflow（自动启动服务、手动注册示例） | 算子拖拽 + DAG Canvas |
| Notebook 编辑器 | `compute.script.supported=true` | Jupyter | Jupyter Notebook |

### 引擎能力声明示例

**PostgreSQL 引擎**:
```json
{
  "schema_version": "engine.capabilities/v1",
  "compute": {
    "query": {
      "supported": true,
      "languages": ["sql"],
      "default_language": "sql"
    }
  }
}
```
→ 当用户选择**查询工作台**时，PostgreSQL 会出现在可用引擎列表中

**GeoPython Workflow**:
```json
{
  "schema_version": "engine.capabilities/v1",
  "compute": {
    "workflow": {
      "supported": true,
      "runtime_api": "addp.workflow/v1",
      "dynamic_operators": true
    }
  }
}
```
→ 当用户选择**工作流编辑器**时，GeoPython Workflow 会出现在可用引擎列表中

**Jupyter 引擎**:
```json
{
  "schema_version": "engine.capabilities/v1",
  "compute": {
    "script": {
      "supported": true,
      "modes": ["notebook"],
      "languages": ["python"],
      "interactive": true
    }
  }
}
```
→ 当用户选择**Notebook 编辑器**时，Jupyter 会出现在可用引擎列表中

**组合能力引擎** (理论示例):
```json
{
  "schema_version": "engine.capabilities/v1",
  "compute": {
    "query": {"supported": true, "languages": ["sql"]},
    "workflow": {"supported": true, "runtime_api": "addp.workflow/v1", "dynamic_operators": true}
  }
}
```
→ 该引擎会同时出现在**查询工作台**和**工作流编辑器**的引擎列表中

---

## 相关文档

- [返回核心概念关系图](addp核心概念关系图.md)
- [ADDP 引擎体系图](addp引擎体系图.md)
- [ADDP 任务编排体系图](addp任务编排体系图.md)
- [Develop 模块详情](../../develop/CLAUDE.md)

---

**文档版本**: v1.0
**创建日期**: 2026-02-16
**作者**: ADDP 开发团队
