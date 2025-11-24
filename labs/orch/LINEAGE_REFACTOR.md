# Lineage 重构完成报告

## 重构目标

将数据血缘追踪功能从 Temporal 依赖中解耦，实现：
1. **lineage 库**：零工作流引擎依赖的纯血缘追踪库
2. **lineage-demo**：展示如何将 lineage 库与 Temporal 集成

## 完成情况

### ✅ 1. lineage 库（纯库，无 Temporal 依赖）

```
lineage/
├── models/
│   ├── lineage.go          ✅ 引擎无关的数据模型
│   └── types.go            ✅ LineageInput, LineageQuery
├── repository/
│   └── lineage_repository.go ✅ 数据访问层（GORM）
├── service/
│   ├── recorder.go         ✅ LineageRecorder 接口
│   └── lineage_service.go  ✅ 业务逻辑 + 血缘图构建
├── storage/
│   ├── sqlite.go           ✅ SQLite 支持
│   └── postgres.go         ✅ PostgreSQL 支持
├── go.mod                  ✅ 零 Temporal 依赖
└── README.md               ✅ 完整文档
```

**关键设计点**：
- `ExternalWorkflowID` 代替 `TemporalWorkflowID`（引擎无关）
- `WorkflowEngine` 字段支持 temporal/airflow/manual 等
- `LineageRecorder` 接口（策略模式）
- 完整的 Repository + Service 分层

### ✅ 2. lineage-demo（Temporal 集成示例）

```
lineage-demo/
├── workflow/
│   └── lineage_workflow.go    ✅ Workflow + Activity（薄封装）
├── worker/
│   └── main.go                ✅ 依赖注入模式
├── starter/
│   └── main.go                ✅ Workflow 触发器
├── api/
│   └── server.go              ✅ API Server（查询血缘）
├── web/
│   └── index.html             ✅ Web UI（可视化）
├── go.mod                     ✅ 依赖 lineage + Temporal
└── README.md                  ✅ 集成指南
```

**集成模式**：
- Activity 仅作薄封装，注入 Temporal 元数据
- Worker 通过 context 注入 LineageRecorder
- 所有业务逻辑在 lineage 库中

## 核心设计模式

### 1. 依赖注入（Dependency Injection）

**Worker 注册 Activity 时注入依赖**：

```go
// worker/main.go
lineageService := service.NewLineageService(repo)

activityWrapper := func(ctx context.Context, input interface{}) error {
    ctx = context.WithValue(ctx, "lineage_recorder", lineageService)
    return workflow.RecordLineageActivity(ctx, input.(*models.LineageInput))
}
w.RegisterActivity(activityWrapper)
```

**Activity 从 context 获取依赖**：

```go
// workflow/lineage_workflow.go
func RecordLineageActivity(ctx context.Context, input *models.LineageInput) error {
    recorder := ctx.Value("lineage_recorder").(service.LineageRecorder)
    return recorder.Record(ctx, input)
}
```

### 2. 策略模式（Strategy Pattern）

`LineageRecorder` 接口允许不同实现：

```go
type LineageRecorder interface {
    Record(ctx context.Context, input *models.LineageInput) (*models.DataLineage, error)
    RecordBatch(ctx context.Context, inputs []*models.LineageInput) error
    QueryUpstream(ctx context.Context, itemID uint) ([]*models.DataLineage, error)
    QueryDownstream(ctx context.Context, itemID uint) ([]*models.DataLineage, error)
    Query(ctx context.Context, query *models.LineageQuery) ([]*models.DataLineage, error)
}
```

### 3. Repository 模式（Repository Pattern）

数据访问抽象：

```go
type LineageRepository struct {
    db *gorm.DB
}

func (r *LineageRepository) Create(ctx context.Context, lineage *models.DataLineage) (*models.DataLineage, error)
func (r *LineageRepository) FindByTargetItemID(ctx context.Context, itemID uint) ([]*models.DataLineage, error)
func (r *LineageRepository) FindBySourceItemID(ctx context.Context, itemID uint) ([]*models.DataLineage, error)
func (r *LineageRepository) Query(ctx context.Context, query *models.LineageQuery) ([]*models.DataLineage, error)
```

## 关键改进

### 原设计问题

1. **Temporal 耦合**：`WorkflowID` 和 `RunID` 字段与 Temporal 强绑定
2. **全局变量**：使用全局变量传递依赖
3. **混合职责**：业务逻辑和框架代码混在一起

### 新设计改进

1. **引擎无关**：`ExternalWorkflowID` + `WorkflowEngine` 字段支持任何引擎
2. **依赖注入**：通过 context 注入依赖，避免全局变量
3. **关注点分离**：lineage 库纯业务逻辑，demo 展示集成方式

## 数据模型对比

### 旧模型（Temporal 耦合）

```go
type DataLineage struct {
    WorkflowID   string  // ← Temporal 特定
    RunID        string  // ← Temporal 特定
    // ...
}
```

### 新模型（引擎无关）

```go
type DataLineage struct {
    ExternalWorkflowID  string  // 可以是任何引擎的 Workflow ID
    ExternalExecutionID string  // 可以是任何引擎的 Execution ID
    WorkflowEngine      string  // temporal | airflow | manual | ""
    // ...
}
```

## 使用场景

### 1. Temporal 集成（当前 demo）

```go
lineageInput := &models.LineageInput{
    ExternalWorkflowID:  workflowInfo.WorkflowExecution.ID,
    ExternalExecutionID: workflowInfo.WorkflowExecution.RunID,
    WorkflowEngine:      "temporal",
    // ... 业务数据
}
```

### 2. Airflow 集成（未来）

```python
lineage_data = {
    "external_workflow_id": dag_run.dag_id,
    "external_execution_id": dag_run.run_id,
    "workflow_engine": "airflow",
    # ... 业务数据
}
requests.post("http://lineage-api:9090/api/lineage", json=lineage_data)
```

### 3. 手动记录（无工作流引擎）

```go
lineage, err := recorder.Record(ctx, &models.LineageInput{
    WorkflowEngine: "",  // 或 "manual"
    SourceItemID:   1,
    TargetItemID:   2,
    // 不需要 ExternalWorkflowID
})
```

## API 端点

- `GET /api/lineage/all` - 查询所有血缘
- `GET /api/lineage/upstream/:item_id` - 查询上游血缘
- `GET /api/lineage/downstream/:item_id` - 查询下游血缘
- `GET /api/lineage/graph/:item_id?max_depth=3` - 构建血缘图
- `GET /api/lineage/workflow/:workflow_id` - 根据 Workflow ID 查询

## 下一步：迁移到 ADDP Meta 模块

### 迁移步骤

1. **复制 lineage 库**到 `meta/backend/internal/lineage/`
2. **集成到 Meta 服务**：
   ```go
   import "meta/internal/lineage/service"

   func (s *MetaService) RecordLineage(input *lineage.LineageInput) error {
       return s.lineageRecorder.Record(ctx, input)
   }
   ```
3. **修改数据库**：从 SQLite 切换到 PostgreSQL `metadata` schema
4. **添加 API 路由**：
   ```go
   api.GET("/lineage/upstream/:item_id", metaHandler.GetUpstreamLineage)
   api.GET("/lineage/downstream/:item_id", metaHandler.GetDownstreamLineage)
   ```
5. **Transfer 模块集成**：在 Transfer Activity 中调用 Meta 的 lineage API

### 无需修改的部分

- ✅ 数据模型（`models/lineage.go`, `models/types.go`）
- ✅ Repository 层（`repository/lineage_repository.go`）
- ✅ Service 层（`service/lineage_service.go`, `service/recorder.go`）
- ✅ 仅需修改 Storage 配置（SQLite → PostgreSQL）

## 测试计划

### 单元测试（待完成）

```bash
cd lineage
go test ./models
go test ./repository
go test ./service
```

### 集成测试（当前 demo）

```bash
# 1. 启动 Temporal Server
cd worker-example && docker-compose up -d

# 2. 启动 Worker
cd lineage-demo/worker && go run main.go &

# 3. 触发 Workflow
cd ../starter && go run main.go

# 4. 查询血缘
cd ../api && go run server.go &
curl http://localhost:9090/api/lineage/all | jq
```

## 总结

### 重构成果

1. **✅ 模块化**：lineage 库完全独立，可单独使用
2. **✅ 可扩展**：支持任意工作流引擎（Temporal, Airflow, etc.）
3. **✅ 可移植**：可直接集成到 ADDP Meta 模块
4. **✅ 可测试**：业务逻辑与框架解耦，便于单元测试

### 关键文件

- [`lineage/README.md`](lineage/README.md) - lineage 库使用文档
- [`lineage-demo/README.md`](lineage-demo/README.md) - Temporal 集成示例
- [`lineage/models/lineage.go`](lineage/models/lineage.go) - 核心数据模型
- [`lineage/service/recorder.go`](lineage/service/recorder.go) - LineageRecorder 接口
- [`lineage-demo/workflow/lineage_workflow.go`](lineage-demo/workflow/lineage_workflow.go) - Temporal 集成示例

### 设计理念

> **lineage 库不应该有 Temporal 的影子。它就是一个被调用的库。在 example 中来实现两者的集成。**

✅ 达成目标！
