# Lineage - 数据血缘追踪库

**Lineage** 是一个纯粹的数据血缘追踪库，**不依赖任何工作流引擎**（如 Temporal、Airflow 等）。它提供了简洁的 API 来记录和查询数据之间的依赖关系。

## 设计原则

1. **框架无关**：不包含任何工作流引擎特定的代码或依赖
2. **纯业务逻辑**：专注于血缘数据的记录、查询和图构建
3. **依赖注入**：通过接口和依赖注入实现灵活集成
4. **数据库抽象**：支持 SQLite（开发）和 PostgreSQL（生产）

## 架构

```
lineage/
├── models/              # 数据模型
│   ├── lineage.go      # DataLineage 核心模型
│   └── types.go        # LineageInput, LineageQuery
├── repository/          # 数据访问层
│   └── lineage_repository.go
├── service/             # 业务逻辑层
│   ├── recorder.go     # LineageRecorder 接口
│   └── lineage_service.go
└── storage/             # 数据库连接
    ├── sqlite.go       # SQLite 支持
    └── postgres.go     # PostgreSQL 支持
```

## 核心概念

### 1. DataLineage 模型

```go
type DataLineage struct {
    // 外部工作流引擎信息（可选）
    ExternalWorkflowID  string  // 可以是 Temporal WorkflowID、Airflow DAG ID 等
    ExternalExecutionID string  // 可以是 Temporal RunID、Airflow RunID 等
    WorkflowEngine      string  // "temporal" | "airflow" | "manual" | ""

    // 源数据
    SourceItemID      uint
    SourceFingerprint string
    SourceType        string  // table, file, api, etc.
    SourcePath        string

    // 目标数据
    TargetItemID      uint
    TargetFingerprint string
    TargetType        string
    TargetPath        string

    // 转换信息
    LineageType     string  // transform, copy, merge, aggregate
    TransformConfig string  // JSON

    // 执行信息
    Status       string    // success, failed, partial
    StartTime    time.Time
    EndTime      *time.Time
    DurationMs   int64

    // 指标
    RecordsProcessed int64
    BytesWritten     int64
    Metrics          string  // JSON
}
```

### 2. LineageRecorder 接口

```go
type LineageRecorder interface {
    Record(ctx context.Context, input *models.LineageInput) (*models.DataLineage, error)
    RecordBatch(ctx context.Context, inputs []*models.LineageInput) error
    QueryUpstream(ctx context.Context, itemID uint) ([]*models.DataLineage, error)
    QueryDownstream(ctx context.Context, itemID uint) ([]*models.DataLineage, error)
    Query(ctx context.Context, query *models.LineageQuery) ([]*models.DataLineage, error)
}
```

## 使用示例

### 基本使用（无工作流引擎）

```go
package main

import (
    "context"
    "lineage/models"
    "lineage/repository"
    "lineage/service"
    "lineage/storage"
    "time"
)

func main() {
    // 1. 初始化数据库
    db, _ := storage.NewSQLiteDB("./lineage.db")

    // 2. 创建服务
    repo := repository.NewLineageRepository(db)
    recorder := service.NewLineageService(repo)

    // 3. 记录血缘
    input := &models.LineageInput{
        SourceItemID:      1,
        SourceFingerprint: "source-fingerprint",
        SourceType:        "table",
        SourcePath:        "db.schema.source_table",

        TargetItemID:      2,
        TargetFingerprint: "target-fingerprint",
        TargetType:        "table",
        TargetPath:        "db.schema.target_table",

        LineageType: "transform",
        Status:      "success",
        StartTime:   time.Now().Add(-5 * time.Minute),
        EndTime:     time.Now(),

        RecordsProcessed: 1000,
        BytesWritten:     50000,
    }

    lineage, _ := recorder.Record(context.Background(), input)
    println("Lineage recorded:", lineage.ID)

    // 4. 查询上游血缘
    upstream, _ := recorder.QueryUpstream(context.Background(), 2)
    println("Found upstream lineages:", len(upstream))

    // 5. 查询下游血缘
    downstream, _ := recorder.QueryDownstream(context.Background(), 1)
    println("Found downstream lineages:", len(downstream))

    // 6. 构建血缘图
    graph, _ := recorder.(*service.LineageService).BuildLineageGraph(context.Background(), 2, 3)
    println("Graph nodes:", len(graph.Nodes))
    println("Graph edges:", len(graph.Edges))
}
```

### 与 Temporal 集成

参见 `../lineage-demo/` 示例，展示如何在 Temporal Activity 中注入工作流元数据。

### 与 Airflow 集成

```python
from airflow import DAG
from airflow.operators.python import PythonOperator
import requests

def record_lineage_to_api(**context):
    dag_run = context['dag_run']

    lineage_data = {
        "external_workflow_id": dag_run.dag_id,
        "external_execution_id": dag_run.run_id,
        "workflow_engine": "airflow",
        "source_item_id": 1,
        "target_item_id": 2,
        "lineage_type": "transform",
        "status": "success",
        # ... other fields
    }

    requests.post("http://lineage-api:9090/api/lineage", json=lineage_data)

with DAG('my_dag', ...) as dag:
    record_task = PythonOperator(
        task_id='record_lineage',
        python_callable=record_lineage_to_api
    )
```

## API 查询示例

### 查询所有血缘

```bash
curl http://localhost:9090/api/lineage/all
```

### 查询上游血缘

```bash
curl http://localhost:9090/api/lineage/upstream/2
```

### 查询下游血缘

```bash
curl http://localhost:9090/api/lineage/downstream/1
```

### 构建血缘图

```bash
curl http://localhost:9090/api/lineage/graph/2?max_depth=3
```

### 根据 Workflow ID 查询

```bash
curl http://localhost:9090/api/lineage/workflow/my-workflow-id
```

## 数据库支持

### SQLite（开发环境）

```go
db, err := storage.NewSQLiteDB("./lineage.db")
```

### PostgreSQL（生产环境）

```go
db, err := storage.NewPostgresDB(storage.PostgresConfig{
    Host:     "localhost",
    Port:     5432,
    User:     "postgres",
    Password: "password",
    DBName:   "lineage",
    SSLMode:  "disable",
})
```

## 高级功能

### 血缘图构建

`BuildLineageGraph` 方法递归查询上下游血缘，构建完整的血缘图谱：

```go
graph, err := lineageService.BuildLineageGraph(ctx, itemID, maxDepth)
// graph.Nodes: map[itemID]*LineageNode
// graph.Edges: []*LineageEdge
```

### 批量记录

```go
inputs := []*models.LineageInput{input1, input2, input3}
err := recorder.RecordBatch(ctx, inputs)
```

### 灵活查询

```go
query := &models.LineageQuery{
    SourceItemID: &sourceID,
    TargetItemID: &targetID,
    Status:       "success",
    LineageType:  "transform",
    Limit:        100,
    Offset:       0,
}
lineages, err := recorder.Query(ctx, query)
```

## 集成到其他项目

### 作为 Go Module

```go
// go.mod
require lineage v0.0.0
replace lineage => /path/to/lineage
```

### 作为独立服务（API）

1. 启动 API Server: `cd api && go run server.go`
2. 通过 HTTP 调用: `POST /api/lineage` 记录血缘
3. 语言无关：支持 Python、Java、Node.js 等任何语言

## 迁移到 ADDP Meta 模块

该库设计时已考虑到迁移到 ADDP 的 `meta` 模块：

1. **无依赖冲突**：不引入任何工作流引擎依赖
2. **数据库兼容**：使用 GORM，与 ADDP 一致
3. **模型独立**：`DataLineage` 可直接集成到 `meta/backend/internal/models/`
4. **接口清晰**：`LineageRecorder` 可作为 Meta 服务的一部分

## 开发与测试

```bash
# 运行测试
cd lineage
go test ./...

# 查看数据库内容
sqlite3 lineage.db
> SELECT * FROM data_lineages;
```

## 许可证

Internal use only - ADDP Project
