# Lineage-Demo - Temporal 集成示例

本示例展示如何将 **纯粹的 lineage 库**与 **Temporal Workflow** 集成，实现数据血缘追踪。

## 核心设计思想

### 关键点：lineage 库完全独立

- ✅ `lineage/` 目录：**零 Temporal 依赖**，纯业务逻辑
- ✅ `lineage-demo/` 目录：Temporal 集成示例，**仅在这里**出现 Temporal 代码
- ✅ Activity 作为**薄封装**：仅负责注入 Temporal 元数据到 lineage 库

### 集成模式：依赖注入

```
┌─────────────────────────────────────────────────────┐
│  Temporal Workflow                                  │
│  ┌───────────────────────────────────────────────┐ │
│  │  DataTransformWorkflow                        │ │
│  │  - 获取 Temporal 元数据（WorkflowID, RunID）  │ │
│  │  - 构造 LineageInput                          │ │
│  │  - 调用 Activity                              │ │
│  └─────────────────┬───────────────────────────────┘ │
│                    │                                 │
│  ┌─────────────────▼───────────────────────────────┐ │
│  │  RecordLineageActivity (薄封装)               │ │
│  │  - 从 context 获取 LineageRecorder           │ │
│  │  - 注入 Temporal 元数据到 LineageInput       │ │
│  │  - 调用 recorder.Record()                    │ │
│  └─────────────────┬───────────────────────────────┘ │
└────────────────────┼───────────────────────────────┘
                     │
        ┌────────────▼────────────┐
        │   Lineage Library       │
        │  (无 Temporal 依赖)     │
        │                         │
        │  LineageService.Record()│
        │  ↓                      │
        │  Repository.Create()    │
        │  ↓                      │
        │  Database (SQLite)      │
        └─────────────────────────┘
```

## 目录结构

```
lineage-demo/
├── workflow/               # Temporal Workflow 定义
│   └── lineage_workflow.go # DataTransformWorkflow + RecordLineageActivity
├── worker/                 # Temporal Worker（依赖注入）
│   └── main.go
├── starter/                # Workflow 启动器
│   └── main.go
├── api/                    # API Server（查询血缘）
│   └── server.go
├── web/                    # Web UI（可视化）
│   └── index.html
└── go.mod                  # 依赖 lineage 库 + Temporal SDK
```

## 快速开始

### 前置条件

1. **Temporal Server 已启动**（使用 worker-example 的 docker-compose）

```bash
cd ../worker-example
docker-compose up -d
```

2. **lineage 库已完成**（位于 `../lineage/`）

### 运行步骤

#### 1. 初始化依赖

```bash
cd lineage-demo
go mod tidy
```

#### 2. 启动 Worker（监听任务）

```bash
cd worker
go run main.go
```

输出：
```
✅ Database connected
✅ Lineage service initialized
✅ Connected to Temporal Server
✅ Workflow registered: DataTransformWorkflow
✅ Activity registered: RecordLineageActivity (with dependency injection)
🚀 Worker started, listening on queue: lineage-demo-queue
   Press Ctrl+C to stop
```

#### 3. 启动 Starter（触发 Workflow）

在另一个终端：

```bash
cd starter
go run main.go
```

输出：
```
✅ Connected to Temporal Server
🚀 Workflow started successfully
   Workflow ID: lineage-demo-workflow-1
   Run ID: 550e8400-e29b-41d4-a716-446655440000
   Source Item: 1
   Target Item: 2
⏳ Waiting for workflow to complete...
✅ Workflow completed successfully!
   Check lineage.db for recorded lineage data
```

#### 4. 启动 API Server（查询血缘）

在第三个终端：

```bash
cd api
go run server.go
```

输出：
```
✅ Database connected
✅ Lineage service initialized
🚀 API Server started at http://localhost:9090
   Endpoints:
   - GET /api/lineage/all
   - GET /api/lineage/upstream/:item_id
   - GET /api/lineage/downstream/:item_id
   - GET /api/lineage/graph/:item_id?max_depth=3
   - GET /api/lineage/workflow/:workflow_id
```

#### 5. 查询血缘数据

```bash
# 查询所有血缘
curl http://localhost:9090/api/lineage/all | jq

# 查询 item 2 的上游血缘
curl http://localhost:9090/api/lineage/upstream/2 | jq

# 查询 item 1 的下游血缘
curl http://localhost:9090/api/lineage/downstream/1 | jq

# 构建血缘图
curl http://localhost:9090/api/lineage/graph/2?max_depth=3 | jq
```

#### 6. 打开 Web UI

在浏览器中打开 `web/index.html`，输入 API URL：`http://localhost:9090`

## 代码解析

### 1. Workflow 定义（workflow/lineage_workflow.go）

```go
func DataTransformWorkflow(ctx workflow.Context, sourceItemID, targetItemID uint) error {
    // 获取 Temporal 元数据
    workflowInfo := workflow.GetInfo(ctx)

    // 构造 LineageInput（注入 Temporal 信息）
    lineageInput := &models.LineageInput{
        ExternalWorkflowID:  workflowInfo.WorkflowExecution.ID,     // ← Temporal 特有
        ExternalExecutionID: workflowInfo.WorkflowExecution.RunID,  // ← Temporal 特有
        WorkflowEngine:      "temporal",                            // ← 标识引擎

        SourceItemID: sourceItemID,
        TargetItemID: targetItemID,
        // ... 业务数据
    }

    // 调用 Activity 记录血缘
    err := workflow.ExecuteActivity(ctx, RecordLineageActivity, lineageInput).Get(ctx, nil)
    return err
}
```

### 2. Activity 薄封装（workflow/lineage_workflow.go）

```go
func RecordLineageActivity(ctx context.Context, input *models.LineageInput) error {
    // 从 context 获取注入的 LineageRecorder（依赖注入）
    recorder := ctx.Value("lineage_recorder").(service.LineageRecorder)

    // 更新结束时间
    input.EndTime = time.Now()

    // 调用 lineage 库（纯业务逻辑，无 Temporal 依赖）
    lineage, err := recorder.Record(ctx, input)
    return err
}
```

### 3. Worker 依赖注入（worker/main.go）

```go
func main() {
    // 1. 初始化 lineage 库
    db, _ := storage.NewSQLiteDB("./lineage.db")
    repo := repository.NewLineageRepository(db)
    lineageService := service.NewLineageService(repo)

    // 2. 创建 Temporal Worker
    c, _ := client.Dial(client.Options{HostPort: "localhost:7233"})
    w := worker.New(c, "lineage-demo-queue", worker.Options{})

    // 3. 注册 Workflow
    w.RegisterWorkflow(workflow.DataTransformWorkflow)

    // 4. 注册 Activity（依赖注入 wrapper）
    activityWrapper := func(ctx context.Context, input interface{}) error {
        // 注入 lineageService 到 context
        ctx = context.WithValue(ctx, "lineage_recorder", lineageService)
        return workflow.RecordLineageActivity(ctx, input.(*models.LineageInput))
    }
    w.RegisterActivity(activityWrapper)

    // 5. 启动 Worker
    w.Run(worker.InterruptCh())
}
```

## 关键设计要点

### ✅ lineage 库零 Temporal 依赖

查看 `lineage/go.mod`：

```go
module lineage

require (
    github.com/gin-gonic/gin v1.10.0
    gorm.io/driver/postgres v1.5.9
    gorm.io/driver/sqlite v1.5.6
    gorm.io/gorm v1.25.12
)
// 没有 go.temporal.io/sdk！
```

### ✅ ExternalWorkflowID 设计为引擎无关

```go
type DataLineage struct {
    ExternalWorkflowID  string  // 不叫 "TemporalWorkflowID"
    ExternalExecutionID string  // 不叫 "TemporalRunID"
    WorkflowEngine      string  // temporal/airflow/manual/""
}
```

**可支持任何工作流引擎**：

- Temporal: `ExternalWorkflowID = workflowInfo.WorkflowExecution.ID`
- Airflow: `ExternalWorkflowID = dag_run.dag_id`
- Manual: `ExternalWorkflowID = ""`（手动记录血缘）

### ✅ Activity 仅作薄封装

Activity 的职责：
1. 从 Temporal Context 提取元数据（WorkflowID, RunID）
2. 注入到 `LineageInput`
3. 调用 lineage 库的 `Record()` 方法

**不包含任何业务逻辑**，所有逻辑在 lineage 库中。

### ✅ 依赖注入模式

通过 `context.Context` 注入 `LineageRecorder`，而不是全局变量：

```go
// ❌ 不要这样（全局变量）
var globalLineageService *service.LineageService

func RecordLineageActivity(ctx context.Context, input *models.LineageInput) error {
    return globalLineageService.Record(ctx, input)
}

// ✅ 应该这样（依赖注入）
func RecordLineageActivity(ctx context.Context, input *models.LineageInput) error {
    recorder := ctx.Value("lineage_recorder").(service.LineageRecorder)
    return recorder.Record(ctx, input)
}
```

## 扩展到其他工作流引擎

### Airflow 集成示例

```python
from airflow import DAG
from airflow.operators.python import PythonOperator
from lineage_client import LineageClient  # 假设有 Python SDK

def my_task(**context):
    dag_run = context['dag_run']

    # 记录血缘（通过 HTTP API）
    lineage_client = LineageClient("http://lineage-api:9090")
    lineage_client.record({
        "external_workflow_id": dag_run.dag_id,
        "external_execution_id": dag_run.run_id,
        "workflow_engine": "airflow",
        "source_item_id": 1,
        "target_item_id": 2,
        "lineage_type": "transform",
        "status": "success",
        # ...
    })
```

### 无工作流引擎（直接调用）

```go
recorder.Record(ctx, &models.LineageInput{
    WorkflowEngine: "",  // 或 "manual"
    SourceItemID:   1,
    TargetItemID:   2,
    // 不需要 ExternalWorkflowID
})
```

## 迁移到 ADDP Meta 模块

该 demo 展示的集成模式可直接应用到 ADDP：

1. **复制 lineage 库**到 `meta/backend/internal/lineage/`
2. **复用 Activity 模式**：在 Transfer 模块的 Temporal Activities 中注入血缘记录
3. **统一 API**：Meta 模块提供血缘查询接口
4. **共享数据库**：使用 Meta 的 PostgreSQL

## 常见问题

**Q: 为什么不直接在 lineage 库中导入 Temporal SDK？**

A: 为了保持库的独立性和可移植性。如果 lineage 依赖 Temporal，就无法用于 Airflow、手动记录等场景。

**Q: 如何支持更多工作流引擎？**

A: 只需创建类似的 Activity 薄封装，注入对应引擎的元数据到 `ExternalWorkflowID` 和 `WorkflowEngine` 字段即可。

**Q: 可以不用 Temporal 吗？**

A: 当然可以！lineage 库完全独立，可以在任何 Go 程序中直接调用 `recorder.Record()`。

## 测试验证

```bash
# 1. 启动 Temporal Server
cd ../worker-example && docker-compose up -d

# 2. 启动 Worker
cd lineage-demo/worker && go run main.go &

# 3. 运行 Starter
cd ../starter && go run main.go

# 4. 查询血缘
cd ../api && go run server.go &
curl http://localhost:9090/api/lineage/all | jq

# 5. 检查数据库
sqlite3 worker/lineage.db "SELECT * FROM data_lineages;"
```

## 许可证

Internal use only - ADDP Project
