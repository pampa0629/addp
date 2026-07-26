# ADDP 任务编排体系图

本文档展示 ADDP 平台的任务库机制、任务编排流与算子工作流的区别、以及跨模块编排能力。任务定义、执行记录、TaskProvider、Orchestrator 和 Monitor 的正式约束以 [ADDP 任务体系规范](../spec/addp任务体系规范.md) 为准。

---

## 目录

1. [任务编排概述](#任务编排概述)
2. [任务库机制](#任务库机制)
3. [任务编排流 vs 算子工作流](#任务编排流-vs-算子工作流)
4. [编排 DAG 示例](#编排-dag-示例)
5. [依赖管理与参数模板化](#依赖管理与参数模板化)
6. [调度方式](#调度方式)

---

## 任务编排概述

**任务编排流** 是基于业务任务的跨模块 DAG 工作流,用于复杂的数据流水线和 ETL 作业。

**核心特点**:
- **节点粒度**: 粗粒度业务任务 (扫描元数据、Transfer 任务、生成瓦片)
- **DAG 层级**: 任务级别的有向无环图
- **数据传递**: 参数模板 `{{step_id.field.path}}` 或 `{{step_id}}` 引用显式依赖步骤的结果
- **执行方式**: 通过 TaskProvider 调用各模块已存在的任务定义 (Meta、Transfer、Develop、Manager、Quality、Graph、Orchestrator 等)
- **适用场景**: 跨模块数据流水线、定时 ETL 作业

---

## 任务库机制

**任务库** 是由各模块提供的可复用任务集合,通过 TaskProvider capabilities 注册机制实现跨模块调用。

```mermaid
graph TB
    subgraph "任务提供者 (各模块)"
        Meta[Meta 模块]
        Transfer[Transfer 模块]
        Develop[Develop 模块]
        Manager[Manager 模块]
        Quality[Quality 模块]
        Graph[Graph 模块]
        OrchestratorProvider[Orchestrator 模块]
    end

    subgraph "TaskProvider 注册中心 (System 模块)"
        Registry[task_providers 表]

        Meta --> |注册 capabilities| MetaCap[Meta task_capabilities<br/>scan]
        Transfer --> |注册 capabilities| TransferCap[Transfer task_capabilities<br/>sync]
        Develop --> |注册 capabilities| DevelopCap[Develop task_capabilities<br/>query<br/>workflow<br/>script]
        Manager --> |注册 capabilities| ManagerCap[Manager task_capabilities<br/>vector_tile_cache_generation<br/>vector_materialized_view_generation<br/>embedding]
        Quality --> |注册 capabilities| QualityCap[Quality task_capabilities<br/>check]
        Graph --> |注册 capabilities| GraphCap[Graph task_capabilities<br/>kg_build]
        OrchestratorProvider --> |注册 capabilities| OrchestratorCap[Orchestrator task_capabilities<br/>orchestration]

        MetaCap --> Registry
        TransferCap --> Registry
        DevelopCap --> Registry
        ManagerCap --> Registry
        QualityCap --> Registry
        GraphCap --> Registry
        OrchestratorCap --> Registry
    end

    subgraph "任务编排 (Orchestrator 模块)"
        Orchestrator[Orchestrator 模块]

        Orchestrator --> |发现| Registry
        Orchestrator --> |选择任务| Tasks[任务列表]
        Orchestrator --> |配置参数| Params[参数配置]
        Orchestrator --> |设置依赖| DAG[DAG 依赖关系]
        Orchestrator --> |执行| Call[TaskProvider API 调用]
    end

    Call --> Meta
    Call --> Transfer
    Call --> Develop
    Call --> Manager
    Call --> Quality
    Call --> Graph
    Call --> OrchestratorProvider

    classDef provider fill:#e1f5ff,stroke:#01579b
    classDef registry fill:#fff9c4,stroke:#f57f17
    classDef orchestrator fill:#e8f5e9,stroke:#1b5e20

    class Meta,Transfer,Develop,Manager,Quality,Graph,OrchestratorProvider provider
    class Registry,MetaCap,TransferCap,DevelopCap,ManagerCap,QualityCap,GraphCap,OrchestratorCap registry
    class Orchestrator,Tasks,Params,DAG,Call orchestrator
```

### 任务库工作原理

**步骤 1: 模块注册任务能力**
- 各模块在启动时向 System 模块注册自己的 TaskProvider 能力
- 注册信息包括:模块名、标准 API endpoint，以及 `task_capabilities[]` 中声明的任务类型、定义 schema、执行 schema 和调度/取消能力

**步骤 2: Orchestrator 发现任务**
- Orchestrator 通过 System 中的 TaskProvider 注册中心发现可用任务能力
- 前端展示任务列表供用户选择

**步骤 3: 配置编排**
- 用户选择任务、配置参数、设置依赖关系
- 保存为编排定义 JSON

**步骤 4: 执行调用**
- Orchestrator 通过 TaskProvider 标准接口调用对应模块的任务
- 编排步骤只能引用 owner 模块已保存的任务定义；参数模板只作为本次执行参数覆盖传入,不创建或改写 owner 任务定义

### 任务示例

| 模块 | 任务名称 | 任务 API | 参数示例 |
|------|---------|---------|---------|
| **Meta** | 扫描元数据 | `POST /api/v1/meta/tasks/{task_type}/{id}/execute` | `task_type=scan` |
| **Transfer** | Transfer 任务 | `POST /api/v1/transfer/tasks/{task_type}/{id}/execute` | `task_type=sync` |
| **Develop** | 执行查询 | `POST /api/v1/develop/internal/tasks/{task_type}/{id}/execute` | `task_type=query` |
| **Develop** | 执行工作流 | `POST /api/v1/develop/internal/tasks/{task_type}/{id}/execute` | `task_type=workflow` |
| **Develop** | 执行脚本 | `POST /api/v1/develop/internal/tasks/{task_type}/{id}/execute` | `task_type=script` |
| **Manager** | 生成瓦片缓存 | `POST /api/v1/manager/tasks/{task_type}/{id}/execute` | `task_type=vector_tile_cache_generation` |
| **Manager** | 矢量物化视图 | `POST /api/v1/manager/tasks/{task_type}/{id}/execute` | `task_type=vector_materialized_view_generation` |
| **Manager** | 向量化 | `POST /api/v1/manager/tasks/{task_type}/{id}/execute` | `task_type=embedding` |
| **Quality** | 质量检查 | `POST /api/v1/quality/tasks/{task_type}/{id}/execute` | `task_type=check` |
| **Graph** | 图谱构建 | `POST /api/v1/graph/tasks/{task_type}/{id}/execute` | `task_type=kg_build` |
| **Orchestrator** | 已保存编排 | `POST /api/v1/orchestrator/tasks/{task_type}/{id}/execute` | `task_type=orchestration` |

---

## 任务编排流 vs 算子工作流

ADDP 提供两种工作流:任务编排流(Orchestrator)和算子工作流(Develop)。

```mermaid
graph TB
    subgraph "算子工作流 (Develop 模块)"
        OpWF[算子工作流<br/>Operator Workflow]

        OpWF --> OpNode1[算子节点<br/>buffer<br/>distance: 1000]
        OpWF --> OpNode2[算子节点<br/>intersection<br/>input: buffer结果]
        OpWF --> OpNode3[算子节点<br/>centroid]

        OpNode1 --> OpNode2
        OpNode2 --> OpNode3

        OpWF --> OpEngine[执行引擎:<br/>已注册 Workflow Runtime]
        OpWF --> OpData[数据传递:<br/>运行时内部对象 / 参数引用]
    end

    subgraph "任务编排流 (Orchestrator 模块)"
        TaskWF[任务编排流<br/>Task Orchestration Flow]

        TaskWF --> TaskNode1[业务任务<br/>扫描元数据<br/>Meta.scan]
        TaskWF --> TaskNode2[业务任务<br/>Transfer任务<br/>Transfer.sync]
        TaskWF --> TaskNode3[业务任务<br/>生成瓦片<br/>Manager.vector_tile_cache_generation]

        TaskNode1 --> TaskNode2
        TaskNode2 --> TaskNode3

        TaskWF --> TaskEngine[执行方式:<br/>TaskProvider API调用]
        TaskWF --> TaskData["数据传递:<br/>参数模板<br/>{{step_id.field.path}}"]
    end

    classDef operator fill:#e1f5ff,stroke:#01579b
    classDef task fill:#e8f5e9,stroke:#1b5e20

    class OpWF,OpNode1,OpNode2,OpNode3,OpEngine,OpData operator
    class TaskWF,TaskNode1,TaskNode2,TaskNode3,TaskEngine,TaskData task
```

### 对比表格

| 维度 | 算子工作流 (Develop) | 任务编排流 (Orchestrator) |
|------|---------------------|-------------------------|
| **节点粒度** | 细粒度算子 (buffer, centroid) | 粗粒度业务任务 (扫描元数据, Transfer 任务) |
| **DAG 层级** | 算子级别 DAG | 任务级别 DAG |
| **执行引擎/方式** | 已注册 Workflow Runtime，例如 GeoPython Workflow、Spark Workflow、SuperMap Workflow | 跨模块 TaskProvider API 调用 |
| **数据传递** | 运行时内部对象或参数引用；例如 Python GeoDataFrame、SuperMap SPS `IDataItem` | 已保存任务执行时可使用参数模板 `{{step_id.field.path}}` 或 `{{step_id}}` 传递本次执行参数 |
| **适用场景** | 空间数据分析、地理计算 | 跨模块数据流水线、ETL 作业 |
| **存储表** | `develop.dev_tasks` | `orchestrator.orchestrations` |
| **执行记录** | `common.task_executions`（`module=develop`） | `common.task_executions`（`module=orchestrator`） |
| **前端界面** | 工作流画布 (算子拖拽) | 编排表单 (步骤配置) |

### 嵌套调用模式

Orchestrator 可以调用 Develop 模块已经创建好的工作流任务作为一个步骤。算子工作流必须先在 Develop 中编排并保存为 `workflow` 任务，再以 `provider=develop, task_type=workflow, task_id=...` 的形式进入 Orchestrator。Orchestrator 不创建 Develop 内部算子工作流，也不把算子节点直接拖入任务编排 DAG:

```json
{
  "steps": [
    {
      "id": "extract_data",
      "name": "提取数据",
      "provider": "develop",
      "task_type": "query",
      "task_id": 101,
      "parameters": {"engine_id": 1, "sql": "SELECT * FROM cities"}
    },
    {
      "id": "spatial_analysis",
      "name": "空间分析工作流",
      "provider": "develop",
      "task_type": "workflow",
      "task_id": 102,
      "parameters": {
        "workflow_name": "city_buffer_analysis",
        "input_table": "{{extract_data.result_table}}"
      }
    },
    {
      "id": "run_transfer_task",
      "name": "执行 Transfer 任务",
      "provider": "transfer",
      "task_type": "sync",
      "task_id": 201,
      "parameters": {}
    }
  ]
}
```

---

## 编排 DAG 示例

### 数据处理流水线示例

```mermaid
flowchart TD
    Start([开始]) --> ScanMeta[扫描元数据<br/>Meta.scan<br/>task_id=11]
    ScanMeta --> ImportData[Transfer任务<br/>Transfer.sync<br/>task_id=21]
    ImportData --> ExecuteWorkflow[执行工作流<br/>Develop.workflow<br/>参数: input={{ImportData.target_table}}]
    ExecuteWorkflow --> GenerateTileCache[生成瓦片缓存<br/>Manager.vector_tile_cache_generation<br/>task_id=31]
    GenerateTileCache --> End([结束])

    classDef meta fill:#e1f5ff,stroke:#01579b
    classDef transfer fill:#fff9c4,stroke:#f57f17
    classDef develop fill:#e8f5e9,stroke:#1b5e20
    classDef manager fill:#f3e5f5,stroke:#4a148c

    class ScanMeta meta
    class ImportData transfer
    class ExecuteWorkflow develop
    class GenerateTileCache manager
```

### 编排定义 JSON

```json
{
  "name": "数据处理流水线",
  "description": "从数据扫描到瓦片生成的完整流程",
  "steps": [
    {
      "id": "scan_metadata",
      "name": "扫描元数据",
      "provider": "meta",
      "task_type": "scan",
      "task_id": 11,
      "parameters": {},
      "depends_on": [],
      "timeout": 300
    },
    {
      "id": "import_data",
      "name": "Transfer任务",
      "provider": "transfer",
      "task_type": "sync",
      "task_id": 21,
      "parameters": {},
      "depends_on": ["scan_metadata"],
      "timeout": 600
    },
    {
      "id": "execute_workflow",
      "name": "执行空间分析工作流",
      "provider": "develop",
      "task_type": "workflow",
      "task_id": 22,
      "parameters": {
        "workflow_name": "buffer_analysis",
        "input_table": "{{import_data.target_table}}"
      },
      "depends_on": ["import_data"],
      "timeout": 900
    },
    {
      "id": "generate_tile_cache",
      "name": "生成瓦片缓存",
      "provider": "manager",
      "task_type": "vector_tile_cache_generation",
      "task_id": 31,
      "parameters": {},
      "depends_on": ["execute_workflow"],
      "timeout": 1200
    }
  ],
  "schedule": "0 2 * * *"
}
```

---

## 依赖管理与参数模板化

### 依赖管理 (DAG 拓扑排序)

Orchestrator 使用拓扑排序解析任务依赖。`depends_on` 表示当前步骤依赖的前置步骤，执行器必须先执行依赖步骤，再执行当前步骤:

```mermaid
graph LR
    A[Task A<br/>depends_on: []] --> C[Task C<br/>depends_on: [A,B]]
    B[Task B<br/>depends_on: []] --> C
    C --> D[Task D<br/>depends_on: [C]]

    subgraph "拓扑排序结果"
        Order["执行顺序:<br/>1. A<br/>2. B<br/>3. C<br/>4. D"]
    end

    classDef task fill:#e1f5ff,stroke:#01579b
    classDef result fill:#fff9c4,stroke:#f57f17

    class A,B,C,D task
    class Order result
```

**依赖检测**:
- **循环依赖检测**: 防止死锁(如 A → B → C → A)
- **缺失依赖检测**: `depends_on` 引用不存在的步骤时直接失败
- **拓扑排序**: 按依赖顺序执行；当前主干不承诺并行执行

### 参数模板化

参数模板只支持完整字符串引用，格式为 `{{step_id.field.path}}` 或 `{{step_id}}`。模板引用的 `step_id` 必须存在，并且必须在当前 Step 的 `depends_on` 中显式声明；不支持在普通字符串中做局部插值，也不支持数组索引语法:

```mermaid
sequenceDiagram
    participant Orchestrator
    participant Step1 as Step 1: scan_metadata
    participant Step2 as Step 2: import_data

    Orchestrator->>Step1: 1. 执行扫描元数据
    Step1-->>Orchestrator: 2. 返回结果<br/>{table_name: "cities", row_count: 1000}
    Orchestrator->>Orchestrator: 3. 解析参数模板<br/>table: "{{scan_metadata.table_name}}"<br/>→ table: "cities"
    Orchestrator->>Step2: 4. 执行导入数据<br/>参数: {table: "cities"}
    Step2-->>Orchestrator: 5. 返回结果<br/>{target_table: "public.cities_imported"}
```

**模板语法**:
- 简单字段引用: `{{step1.result}}`
- 嵌套字段引用: `{{step1.result.nested.field}}`
- 整个步骤结果引用: `{{step1}}`
- 模板解析返回被引用输出的原始值，不做隐式类型转换。运行时如果引用步骤没有结果、字段路径不存在，或路径试图进入非对象值，当前 Step 必须失败。

---

## 调度方式

### 定时调度 (Cron)

编排调度只决定某个 orchestration 何时启动一次编排 run。被 Step 引用的 owner 任务如果自身也配置了定时计划，该计划仍然是独立入口；编排不会继承、覆盖或关闭子任务自身调度。

```mermaid
sequenceDiagram
    participant Cron as 编排调度器
    participant Orchestrator as Orchestrator Backend
    participant DB as PostgreSQL
    participant Worker as Orchestrator Worker

    Cron->>Orchestrator: 1. 触发调度<br/>(根据 cron 表达式)
    Orchestrator->>DB: 2. 创建执行记录<br/>(common.task_executions)
    Orchestrator->>Worker: 3. 异步执行编排
    Worker->>Worker: 4. 解析 DAG 依赖
    Worker->>Worker: 5. 依次执行任务步骤
    Worker->>DB: 6. 更新执行状态
    Worker-->>Orchestrator: 7. 执行完成
```

约束：

- Orchestrator 调度的是 orchestration run，不是把 Step 对应 owner 任务的调度“搬到编排里”。
- Step 执行时消费的是 owner 任务定义和执行接口，不读取其 `schedule` / `next_run_at` 作为编排依赖。
- 如果同一个 owner 任务既启用了自身调度，又被某个已调度的 orchestration 引用，两者会形成两个独立执行入口。

**Cron 表达式示例**:
- `0 2 * * *`: 每天凌晨 2 点执行
- `*/15 * * * *`: 每 15 分钟执行
- `0 0 * * 0`: 每周日凌晨执行

### 手动触发

用户通过 API 或前端手动触发执行。Orchestrator 会把 `parameters` 作为本次执行参数传给 owner provider；如果对应 provider 不支持参数覆盖，必须明确拒绝，不能静默忽略。

---

## 典型使用场景

**任务编排流场景**:
- 每日凌晨扫描数据库元数据 → 生成瓦片缓存 → 刷新热点区域瓦片缓存产物
- 从 CSV 导入数据 → 执行空间分析 → 导出结果到 S3
- 多数据源同步: PostgreSQL → MySQL → MongoDB
- 跨模块工作流: Meta 扫描 → Transfer 传输 → Manager 预览

---

## 相关文档

- [返回核心概念关系图](addp核心概念关系图.md)
- [ADDP 数据开发体系图](addp数据开发体系图.md)
- [ADDP 监控与执行体系图](addp监控与执行体系图.md)
- [ADDP 任务体系规范](../spec/addp任务体系规范.md)
- [Orchestrator 模块详情](../../orchestrator/CLAUDE.md)

---

**文档版本**: v1.1
**创建日期**: 2026-02-16
**更新日期**: 2026-06-09
**作者**: ADDP 开发团队
