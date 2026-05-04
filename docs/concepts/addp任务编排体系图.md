# ADDP 任务编排体系图

本文档展示 ADDP 平台的任务库机制、任务编排流与算子工作流的区别、以及跨模块编排能力。

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
- **节点粒度**: 粗粒度业务任务 (扫描元数据、导入数据、生成瓦片)
- **DAG 层级**: 任务级别的有向无环图
- **数据传递**: 参数模板 `{{stepID.field}}` 引用前序任务结果
- **执行引擎**: 跨模块动态引擎调用 (Meta、Transfer、Manager、Develop 等)
- **适用场景**: 跨模块数据流水线、定时 ETL 作业

---

## 任务库机制

**任务库** 是由各模块提供的可复用任务集合,通过能力注册机制实现跨模块调用。

```mermaid
graph TB
    subgraph "任务提供者 (各模块)"
        Meta[Meta 模块]
        Transfer[Transfer 模块]
        Manager[Manager 模块]
        Develop[Develop 模块]
    end

    subgraph "能力注册中心 (System 模块)"
        Registry[engine_capabilities 表]

        Meta --> |注册| MetaCap[Meta 任务能力<br/>scan_metadata<br/>deep_scan]
        Transfer --> |注册| TransferCap[Transfer 任务能力<br/>import_data<br/>export_data<br/>sync_data]
        Manager --> |注册| ManagerCap[Manager 任务能力<br/>generate_mvt<br/>preview_data]
        Develop --> |注册| DevelopCap[Develop 任务能力<br/>execute_query<br/>execute_workflow<br/>execute_notebook]

        MetaCap & TransferCap & ManagerCap & DevelopCap --> Registry
    end

    subgraph "任务编排 (Orchestrator 模块)"
        Orchestrator[Orchestrator 模块]

        Orchestrator --> |发现| Registry
        Orchestrator --> |选择任务| Tasks[任务列表]
        Orchestrator --> |配置参数| Params[参数配置]
        Orchestrator --> |设置依赖| DAG[DAG 依赖关系]
        Orchestrator --> |执行| Call[动态引擎调用]
    end

    Call --> Meta
    Call --> Transfer
    Call --> Manager
    Call --> Develop

    classDef provider fill:#e1f5ff,stroke:#01579b
    classDef registry fill:#fff9c4,stroke:#f57f17
    classDef orchestrator fill:#e8f5e9,stroke:#1b5e20

    class Meta,Transfer,Manager,Develop provider
    class Registry,MetaCap,TransferCap,ManagerCap,DevelopCap registry
    class Orchestrator,Tasks,Params,DAG,Call orchestrator
```

### 任务库工作原理

**步骤 1: 模块注册任务能力**
- 各模块在启动时向 System 模块注册自己的任务 API
- 注册信息包括:任务名称、API 端点、参数定义、超时配置

**步骤 2: Orchestrator 发现任务**
- Orchestrator 通过能力注册中心发现可用任务
- 前端展示任务列表供用户选择

**步骤 3: 配置编排**
- 用户选择任务、配置参数、设置依赖关系
- 保存为编排定义 JSON

**步骤 4: 执行调用**
- Orchestrator 通过动态引擎调用机制调用对应模块的任务 API
- 支持参数模板化,引用前序任务结果

### 任务示例

| 模块 | 任务名称 | 任务 API | 参数示例 |
|------|---------|---------|---------|
| **Meta** | 扫描元数据 | `POST /api/meta/scan` | `{engine_id, scan_type}` |
| **Transfer** | 导入数据 | `POST /api/transfer/import` | `{source_engine, target_engine, table}` |
| **Manager** | 生成 MVT 瓦片 | `POST /api/manager/mvt/generate` | `{engine_id, schema, table}` |
| **Develop** | 执行查询 | `POST /api/develop/query/execute` | `{engine_id, sql}` |
| **Develop** | 执行工作流 | `POST /api/develop/workflow/execute` | `{workflow_id, params}` |

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

        OpWF --> OpEngine[执行引擎:<br/>GeoPandas/Spark]
        OpWF --> OpData[数据传递:<br/>GeoDataFrame 内存]
    end

    subgraph "任务编排流 (Orchestrator 模块)"
        TaskWF[任务编排流<br/>Task Orchestration Flow]

        TaskWF --> TaskNode1[业务任务<br/>扫描元数据<br/>Meta.scan]
        TaskWF --> TaskNode2[业务任务<br/>导入数据<br/>Transfer.import]
        TaskWF --> TaskNode3[业务任务<br/>生成瓦片<br/>Manager.mvt]

        TaskNode1 --> TaskNode2
        TaskNode2 --> TaskNode3

        TaskWF --> TaskEngine[执行引擎:<br/>跨模块API调用]
        TaskWF --> TaskData["数据传递:<br/>参数模板<br/>{{stepID.field}}"]
    end

    classDef operator fill:#e1f5ff,stroke:#01579b
    classDef task fill:#e8f5e9,stroke:#1b5e20

    class OpWF,OpNode1,OpNode2,OpNode3,OpEngine,OpData operator
    class TaskWF,TaskNode1,TaskNode2,TaskNode3,TaskEngine,TaskData task
```

### 对比表格

| 维度 | 算子工作流 (Develop) | 任务编排流 (Orchestrator) |
|------|---------------------|-------------------------|
| **节点粒度** | 细粒度算子 (buffer, centroid) | 粗粒度业务任务 (扫描元数据, 导入数据) |
| **DAG 层级** | 算子级别 DAG | 任务级别 DAG |
| **执行引擎** | GeoPandas/Spark 工作流引擎 | 跨模块动态引擎调用 |
| **数据传递** | GeoDataFrame 内存传递 | 参数模板 `{{stepID.field}}` |
| **适用场景** | 空间数据分析、地理计算 | 跨模块数据流水线、ETL 作业 |
| **存储表** | `develop.dev_items` | `orchestrator.orchestrations` |
| **执行记录** | `develop.dev_executions` | `orchestrator.executions` |
| **前端界面** | 工作流画布 (算子拖拽) | 编排表单 (步骤配置) |

### 嵌套调用模式

Orchestrator 可以调用 Develop 模块的工作流任务作为一个步骤:

```json
{
  "steps": [
    {
      "id": "extract_data",
      "name": "提取数据",
      "engine_identifier": "develop.query.default",
      "parameters": {"engine_id": 1, "sql": "SELECT * FROM cities"}
    },
    {
      "id": "spatial_analysis",
      "name": "空间分析工作流",
      "engine_identifier": "develop.workflow.default",
      "parameters": {
        "workflow_name": "city_buffer_analysis",
        "input_table": "{{extract_data.result_table}}"
      }
    },
    {
      "id": "export_result",
      "name": "导出结果",
      "engine_identifier": "transfer.export.default",
      "parameters": {
        "source_table": "{{spatial_analysis.output_table}}",
        "target_format": "geojson"
      }
    }
  ]
}
```

---

## 编排 DAG 示例

### 数据处理流水线示例

```mermaid
flowchart TD
    Start([开始]) --> ScanMeta[扫描元数据<br/>Meta.scan<br/>参数: engine_id=1]
    ScanMeta --> ImportData[导入数据<br/>Transfer.import<br/>参数: table={{ScanMeta.table_name}}]
    ImportData --> ExecuteWorkflow[执行工作流<br/>Develop.workflow<br/>参数: input={{ImportData.target_table}}]
    ExecuteWorkflow --> GenerateMVT[生成MVT瓦片<br/>Manager.mvt<br/>参数: table={{ExecuteWorkflow.output_table}}]
    GenerateMVT --> End([结束])

    classDef meta fill:#e1f5ff,stroke:#01579b
    classDef transfer fill:#fff9c4,stroke:#f57f17
    classDef develop fill:#e8f5e9,stroke:#1b5e20
    classDef manager fill:#f3e5f5,stroke:#4a148c

    class ScanMeta meta
    class ImportData transfer
    class ExecuteWorkflow develop
    class GenerateMVT manager
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
      "engine_identifier": "meta.scanner.default",
      "parameters": {
        "engine_id": 1,
        "scan_type": "full"
      },
      "depends_on": [],
      "timeout": 300
    },
    {
      "id": "import_data",
      "name": "导入数据",
      "engine_identifier": "transfer.import.default",
      "parameters": {
        "source_engine_id": 1,
        "target_engine_id": 2,
        "table": "{{scan_metadata.table_name}}"
      },
      "depends_on": ["scan_metadata"],
      "timeout": 600
    },
    {
      "id": "execute_workflow",
      "name": "执行空间分析工作流",
      "engine_identifier": "develop.workflow.default",
      "parameters": {
        "workflow_name": "buffer_analysis",
        "input_table": "{{import_data.target_table}}"
      },
      "depends_on": ["import_data"],
      "timeout": 900
    },
    {
      "id": "generate_mvt",
      "name": "生成MVT瓦片",
      "engine_identifier": "manager.mvt.default",
      "parameters": {
        "engine_id": 2,
        "schema": "public",
        "table": "{{execute_workflow.output_table}}"
      },
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

Orchestrator 使用 **Kahn 算法**自动解析任务依赖:

```mermaid
graph LR
    A[Task A<br/>depends_on: []] --> C[Task C<br/>depends_on: [A,B]]
    B[Task B<br/>depends_on: []] --> C
    C --> D[Task D<br/>depends_on: [C]]

    subgraph "拓扑排序结果"
        Order["执行顺序:<br/>1. A, B (并行)<br/>2. C<br/>3. D"]
    end

    classDef task fill:#e1f5ff,stroke:#01579b
    classDef result fill:#fff9c4,stroke:#f57f17

    class A,B,C,D task
    class Order result
```

**依赖检测**:
- **循环依赖检测**: 防止死锁(如 A → B → C → A)
- **拓扑排序**: 按依赖顺序执行
- **并行执行**: 无依赖关系的任务并行执行

### 参数模板化

支持 `{{stepID.field}}` 语法引用前序任务结果:

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
- 数组索引引用: `{{step1.result.items[0]}}`
- 自动类型转换: 字符串、数字、对象

---

## 调度方式

### 定时调度 (Cron)

```mermaid
sequenceDiagram
    participant Cron as Cron 调度器
    participant Orchestrator as Orchestrator Backend
    participant DB as PostgreSQL
    participant Worker as Orchestrator Worker

    Cron->>Orchestrator: 1. 触发调度<br/>(根据 cron 表达式)
    Orchestrator->>DB: 2. 创建执行记录<br/>(executions 表)
    Orchestrator->>Worker: 3. 异步执行编排
    Worker->>Worker: 4. 解析 DAG 依赖
    Worker->>Worker: 5. 依次执行任务步骤
    Worker->>DB: 6. 更新执行状态
    Worker-->>Orchestrator: 7. 执行完成
```

**Cron 表达式示例**:
- `0 2 * * *`: 每天凌晨 2 点执行
- `*/15 * * * *`: 每 15 分钟执行
- `0 0 * * 0`: 每周日凌晨执行

### 手动触发

用户通过 API 或前端手动触发执行,支持传入参数覆盖默认配置。

---

## 典型使用场景

**任务编排流场景**:
- 每日凌晨扫描数据库元数据 → 生成 MVT 瓦片 → 预缓存热点区域
- 从 CSV 导入数据 → 执行空间分析 → 导出结果到 S3
- 多数据源同步: PostgreSQL → MySQL → MongoDB
- 跨模块工作流: Meta 扫描 → Transfer 传输 → Manager 预览

---

## 相关文档

- [返回核心概念关系图](addp核心概念关系图.md)
- [ADDP 数据开发体系图](addp数据开发体系图.md)
- [ADDP 监控与执行体系图](addp监控与执行体系图.md)
- [Orchestrator 模块详情](../../orchestrator/CLAUDE.md)

---

**文档版本**: v1.0
**创建日期**: 2026-02-16
**作者**: ADDP 开发团队
