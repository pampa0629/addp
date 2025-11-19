# Temporal + HTTP API + Asynq 统一任务编排架构设计

**版本**: v1.0
**日期**: 2025-01-20
**状态**: 设计阶段
**适用范围**: ADDP 全平台（Transfer、Meta、Manager 等模块）

---

## 目录

- [1. 背景和动机](#1-背景和动机)
- [2. 设计目标](#2-设计目标)
- [3. 架构概览](#3-架构概览)
- [4. 核心设计原则](#4-核心设计原则)
- [5. 详细设计](#5-详细设计)
  - [5.1 Temporal 编排层](#51-temporal-编排层)
  - [5.2 模块 API 层](#52-模块-api-层)
  - [5.3 Asynq 队列层](#53-asynq-队列层)
  - [5.4 Worker 执行层](#54-worker-执行层)
- [6. 完整代码示例](#6-完整代码示例)
- [7. 部署配置](#7-部署配置)
- [8. 迁移路线图](#8-迁移路线图)
- [9. 监控和可观测性](#9-监控和可观测性)
- [10. 优势总结](#10-优势总结)

---

## 1. 背景和动机

### 1.1 现有架构的局限

**Transfer 模块**：
- 使用 Asynq（Redis-based）处理异步任务
- 仅支持单步任务，复杂编排需要手动实现
- 任务间依赖关系通过数据库状态轮询

**Meta 模块**：
- 使用 goroutine + channel 实现内存队列
- 使用 Cron 触发定时扫描
- 缺少分布式任务调度能力

**痛点**：
1. ❌ **DolphinScheduler 内存占用高**（2GB+）且不易扩展
2. ❌ **缺少复杂工作流编排能力**（多步 ETL、条件分支、循环）
3. ❌ **任务间数据传递依赖外部存储**（性能开销大）
4. ❌ **跨模块协作困难**（需要手动协调）
5. ❌ **缺少统一的血缘追踪能力**

### 1.2 技术选型考量

**为什么选择 Temporal**：
- ✅ Go 原生实现，内存占用低（~200-300MB）
- ✅ 强大的工作流编排能力（代码定义 DAG）
- ✅ 支持长时间运行的持久化工作流（数月/数年）
- ✅ 内置重试、超时、补偿机制
- ✅ 易于扩展（只需实现 Go 接口）

**为什么保留 Asynq**：
- ✅ 简单异步任务无需 Temporal 的复杂性
- ✅ 现有代码已经成熟稳定
- ✅ Redis-based 架构轻量且易于监控
- ✅ 与 ADDP 现有基础设施无缝集成

---

## 2. 设计目标

### 2.1 核心目标

1. **职责分离**：Temporal 负责编排，各模块负责执行
2. **渐进式迁移**：新功能用 Temporal，旧功能保留 Asynq
3. **完全解耦**：Temporal 通过 HTTP API 与模块通信，不依赖任何业务代码
4. **易于测试**：每个步骤都是独立的 HTTP 请求，可单独测试
5. **统一监控**：所有任务状态可通过 HTTP API 查询

### 2.2 非功能目标

- **性能**：Temporal + Asynq 总内存占用 < 500MB（vs DolphinScheduler 2GB+）
- **可靠性**：支持任务重试、超时、补偿逻辑
- **可扩展性**：支持水平扩展（Worker 多副本）
- **可观测性**：统一日志格式，支持 Prometheus 指标

---

## 3. 架构概览

### 3.1 完整架构图

```
┌────────────────────────────────────────────────────────────────────────┐
│                      ADDP Platform - 用户请求入口                      │
│                                                                        │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐               │
│  │   Gateway    │  │   Portal     │  │  用户点击    │               │
│  │   (8000)     │  │   (5170)     │  │  "执行工作流" │               │
│  └──────────────┘  └──────────────┘  └──────┬───────┘               │
│                                              │                        │
│                                              ↓                        │
│  ┌───────────────────────────────────────────────────────────────┐  │
│  │  Transfer Module API Server (8083)                            │  │
│  │                                                                │  │
│  │  POST /api/workflows/execute                                  │  │
│  │    ↓                                                           │  │
│  │  [决策] 简单任务 or 复杂编排？                                │  │
│  │    ↓                        ↓                                 │  │
│  │  简单任务                 复杂编排                            │  │
│  │    ↓                        ↓                                 │  │
│  │  直接 Enqueue          启动 Temporal Workflow                 │  │
│  └───────────────────────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────────────────────────┘

                              ↓ gRPC (复杂编排场景)

┌────────────────────────────────────────────────────────────────────────┐
│                  Temporal Server (编排引擎)                            │
│                                                                        │
│  工作流示例：ETL 数据管道                                              │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │ ETLWorkflow (Go 代码定义)                                    │   │
│  │                                                               │   │
│  │ 1. HTTP POST → Transfer API: /api/tasks/extract              │   │
│  │    等待返回 task_id                                          │   │
│  │    ↓                                                          │   │
│  │ 2. HTTP GET → Transfer API: /api/tasks/{task_id}/status      │   │
│  │    轮询直到状态变为 "completed"                              │   │
│  │    ↓                                                          │   │
│  │ 3. HTTP POST → Transfer API: /api/tasks/transform            │   │
│  │    传递上一步结果                                            │   │
│  │    ↓                                                          │   │
│  │ 4. HTTP POST → Transfer API: /api/tasks/load                 │   │
│  │    ↓                                                          │   │
│  │ 5. HTTP POST → Meta API: /api/lineage/record                 │   │
│  │    记录血缘关系                                              │   │
│  └──────────────────────────────────────────────────────────────┘   │
│                                                                        │
│  特点：                                                                │
│  - Temporal 只负责编排（调用 HTTP API）                              │
│  - 不直接执行业务逻辑                                                 │
│  - 每个步骤都是 HTTP Activity                                        │
└────────────────────────────────────────────────────────────────────────┘

                    ↓ HTTP Request (各步骤)

┌────────────────────────────────────────────────────────────────────────┐
│                      各模块 API Server (接口层)                        │
│                                                                        │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │  Transfer API Server (8083)                                 │    │
│  │                                                              │    │
│  │  POST /api/tasks/extract                                    │    │
│  │    ↓                                                         │    │
│  │  1. 创建 TaskExecution 记录 (status: pending)               │    │
│  │  2. Enqueue 到 Asynq: transfer:default                      │    │
│  │  3. 立即返回: {"task_id": 123, "status": "pending"}         │    │
│  │                                                              │    │
│  │  GET /api/tasks/{task_id}/status                            │    │
│  │    ↓                                                         │    │
│  │  查询 TaskExecution 状态                                    │    │
│  │  返回: {"status": "running|completed|failed"}               │    │
│  └─────────────────────────────────────────────────────────────┘    │
│                                                                        │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │  Meta API Server (8082)                                     │    │
│  │                                                              │    │
│  │  POST /api/scan/database                                    │    │
│  │    ↓                                                         │    │
│  │  1. 创建 ScanTaskRun 记录                                   │    │
│  │  2. Enqueue 到 Asynq: meta:default                          │    │
│  │  3. 返回 task_id                                            │    │
│  │                                                              │    │
│  │  POST /api/lineage/record                                   │    │
│  │    ↓                                                         │    │
│  │  1. 存储血缘关系到 metadata schema                          │    │
│  │  2. 异步触发血缘计算 (Enqueue 到 Asynq)                     │    │
│  └─────────────────────────────────────────────────────────────┘    │
└────────────────────────────────────────────────────────────────────────┘

                    ↓ Enqueue Task (Redis)

┌────────────────────────────────────────────────────────────────────────┐
│                         Redis (Asynq Backend)                          │
│                                                                        │
│  Queues:                                                               │
│  ├── transfer:critical  (优先级 6)                                    │
│  ├── transfer:default   (优先级 3)                                    │
│  ├── transfer:low       (优先级 1)                                    │
│  ├── meta:critical      (优先级 6)                                    │
│  ├── meta:default       (优先级 3)                                    │
│  └── meta:low           (优先级 1)                                    │
└────────────────────────────────────────────────────────────────────────┘

                    ↓ Consume Task

┌────────────────────────────────────────────────────────────────────────┐
│                      Worker Layer (执行层)                             │
│                                                                        │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │  Transfer Worker (Asynq Server)                             │    │
│  │                                                              │    │
│  │  监听队列: transfer:*                                        │    │
│  │                                                              │    │
│  │  处理任务类型:                                               │    │
│  │  - transfer:extract   → 执行数据提取逻辑                    │    │
│  │  - transfer:transform → 执行数据转换逻辑                    │    │
│  │  - transfer:load      → 执行数据加载逻辑                    │    │
│  │                                                              │    │
│  │  执行过程:                                                   │    │
│  │  1. 更新 TaskExecution.status = "running"                   │    │
│  │  2. 执行业务逻辑 (连接数据库、处理数据等)                   │    │
│  │  3. 更新 TaskExecution.status = "completed"                 │    │
│  │  4. 存储执行结果到 TaskExecution.result                     │    │
│  └─────────────────────────────────────────────────────────────┘    │
│                                                                        │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │  Meta Worker (Asynq Server)                                 │    │
│  │                                                              │    │
│  │  监听队列: meta:*                                            │    │
│  │                                                              │    │
│  │  处理任务类型:                                               │    │
│  │  - meta:scan_table    → 扫描单表元数据                      │    │
│  │  - meta:scan_schema   → 扫描整个 schema                     │    │
│  │  - meta:calc_lineage  → 计算血缘关系                        │    │
│  │                                                              │    │
│  │  执行过程:                                                   │    │
│  │  1. 更新 ScanTaskRun.status = "running"                     │    │
│  │  2. 执行扫描逻辑 (连接数据库、解析 schema)                  │    │
│  │  3. 存储元数据到 metadata schema                            │    │
│  │  4. 更新 ScanTaskRun.status = "completed"                   │    │
│  └─────────────────────────────────────────────────────────────┘    │
└────────────────────────────────────────────────────────────────────────┘

                    ↓ 状态更新 (PostgreSQL)

┌────────────────────────────────────────────────────────────────────────┐
│                          PostgreSQL Database                           │
│                                                                        │
│  transfer schema:                                                      │
│  ├── task_executions (id, status, result, created_at, updated_at)    │
│  │   - Temporal 通过 HTTP API 查询状态                               │
│  │   - Worker 更新状态和结果                                         │
│                                                                        │
│  metadata schema:                                                      │
│  ├── scan_task_runs (id, status, progress, result)                   │
│  └── meta_lineage (source_table, target_table, ...)                  │
└────────────────────────────────────────────────────────────────────────┘
```

### 3.2 数据流示例：ETL 工作流

```
用户触发 ETL 工作流
    ↓
Transfer API: POST /api/workflows/execute
    ↓
启动 Temporal Workflow: ETLWorkflow
    ↓
[步骤 1] HTTP POST → Transfer API: /api/tasks/extract
    ↓
Transfer API 返回 task_id: 123
    ↓
Transfer API Enqueue 到 Asynq: transfer:default
    ↓
Transfer Worker 拉取任务，执行数据提取
    ↓
Worker 更新 TaskExecution.status = "completed"
    ↓
Temporal 轮询: HTTP GET /api/tasks/123/status
    ↓
获取状态: "completed"
    ↓
[步骤 2] HTTP POST → Transfer API: /api/tasks/transform
    ↓
... 重复上述流程 ...
    ↓
[步骤 3] HTTP POST → Transfer API: /api/tasks/load
    ↓
[步骤 4] HTTP POST → Meta API: /api/lineage/record
    ↓
工作流完成
```

---

## 4. 核心设计原则

### 4.1 Temporal 只负责编排，不负责执行

**理由**：
- ✅ 解耦：Temporal 不依赖任何模块的业务代码
- ✅ 易测试：可以 mock HTTP 响应进行单元测试
- ✅ 灵活性：各模块可以独立升级，不影响工作流定义

**实现**：
- Temporal Workflow 只调用 HTTP Activity
- 所有业务逻辑在各模块的 Worker 中执行

### 4.2 各模块 API 提供任务触发接口

**接口规范**：

1. **创建任务接口**（POST）：
   - 返回 `task_id`
   - 立即返回（202 Accepted），异步执行
   - 将任务 Enqueue 到 Asynq

2. **查询状态接口**（GET）：
   - 根据 `task_id` 查询执行状态
   - 返回状态：`pending/running/completed/failed`
   - 支持轮询（Temporal 使用）

3. **获取结果接口**（GET）：
   - 获取任务执行结果（仅当 status = "completed"）
   - 返回结构化数据（JSON）

### 4.3 Asynq 队列保持现有架构

**保持不变**：
- Transfer 模块的 Asynq Worker（已实现）
- 队列命名规范：`{module}:{priority}`
- 任务处理器逻辑（Handler）

**新增**：
- Meta 模块的 Asynq Worker（替代现有 goroutine-based 实现）
- 新的任务类型（extract、transform、load 等）

### 4.4 统一监控和日志

**监控指标**：
- Temporal 工作流执行数量、成功率、失败率
- Asynq 队列深度、处理速度、重试次数
- 各模块 API 响应时间、错误率

**日志格式**：
```json
{
  "timestamp": "2024-01-20T10:30:00Z",
  "level": "info",
  "module": "transfer-worker",
  "orchestrator": "asynq",
  "task_type": "extract",
  "task_id": 123,
  "execution_id": 456,
  "workflow_id": "etl-20240120-001",
  "message": "Task completed successfully",
  "duration_ms": 5000
}
```

---

## 5. 详细设计

### 5.1 Temporal 编排层

#### 5.1.1 目录结构

```
temporal/                        # 独立 Temporal 项目
├── go.mod                       # 独立 Go module
├── go.sum
├── cmd/
│   └── worker/
│       └── main.go              # Temporal Worker 启动入口
├── workflows/
│   ├── etl_workflow.go          # ETL 工作流
│   ├── scan_workflow.go         # 元数据扫描工作流
│   └── lineage_workflow.go      # 血缘分析工作流
├── activities/
│   ├── http.go                  # 通用 HTTP Activity
│   └── poll.go                  # 轮询 Activity
└── config/
    └── config.go                # 配置加载
```

#### 5.1.2 通用 HTTP Activity

**职责**：封装 HTTP 请求，供所有工作流使用

**实现**：见 [6.1 节](#61-temporal-workflow-实现)

**特点**：
- 支持所有 HTTP 方法（GET、POST、PUT、DELETE）
- 自动序列化请求体（JSON）
- 自动解析响应体（JSON）
- 支持自定义 Headers（如 Authorization）
- 内置超时控制

#### 5.1.3 轮询 Activity

**职责**：轮询任务状态直到完成或超时

**参数**：
- `URL`: 状态查询接口（如 `/api/tasks/{id}/status`）
- `Timeout`: 最大等待时间（如 10 分钟）
- `Interval`: 轮询间隔（如 5 秒）
- `SuccessStatuses`: 成功状态列表（如 `["completed"]`）
- `FailureStatuses`: 失败状态列表（如 `["failed", "cancelled"]`）

**实现**：见 [6.1 节](#61-temporal-workflow-实现)

#### 5.1.4 工作流注册

```go
// cmd/worker/main.go
func main() {
    // 连接到 Temporal Server
    c, err := client.Dial(client.Options{
        HostPort: "localhost:7233",
    })
    if err != nil {
        log.Fatal(err)
    }
    defer c.Close()

    // 创建 Worker
    w := worker.New(c, "addp-workflows", worker.Options{})

    // 注册所有工作流
    w.RegisterWorkflow(workflows.ETLWorkflow)
    w.RegisterWorkflow(workflows.ScanDatabaseWorkflow)
    w.RegisterWorkflow(workflows.LineageAnalysisWorkflow)

    // 注册所有 Activities
    w.RegisterActivity(activities.HTTPActivity)
    w.RegisterActivity(activities.PollTaskStatusActivity)

    // 启动 Worker
    err = w.Run(worker.InterruptCh())
    if err != nil {
        log.Fatal(err)
    }
}
```

---

### 5.2 模块 API 层

#### 5.2.1 Transfer 模块 API 设计

**新增接口**：

| 方法 | 路径 | 描述 | 请求体 | 响应体 |
|------|------|------|--------|--------|
| POST | `/api/tasks/extract` | 创建数据提取任务 | `ExtractRequest` | `TaskResponse` |
| POST | `/api/tasks/transform` | 创建数据转换任务 | `TransformRequest` | `TaskResponse` |
| POST | `/api/tasks/load` | 创建数据加载任务 | `LoadRequest` | `TaskResponse` |
| GET | `/api/tasks/{id}/status` | 查询任务状态 | - | `StatusResponse` |
| GET | `/api/tasks/{id}/result` | 获取任务结果 | - | `ResultResponse` |

**数据结构**：

```go
// ExtractRequest - 数据提取请求
type ExtractRequest struct {
    SourceResourceID int                    `json:"source_resource_id"`
    Query            string                 `json:"query"`
    Params           map[string]interface{} `json:"params"`
}

// TransformRequest - 数据转换请求
type TransformRequest struct {
    ExtractTaskID int                    `json:"extract_task_id"`
    Rules         []TransformRule        `json:"rules"`
}

// LoadRequest - 数据加载请求
type LoadRequest struct {
    TransformTaskID  int    `json:"transform_task_id"`
    TargetResourceID int    `json:"target_resource_id"`
    TargetTable      string `json:"target_table"`
}

// TaskResponse - 任务创建响应（统一格式）
type TaskResponse struct {
    TaskID    int    `json:"task_id"`
    Status    string `json:"status"`     // "pending"
    Message   string `json:"message"`
    CreatedAt string `json:"created_at"`
}

// StatusResponse - 任务状态响应
type StatusResponse struct {
    TaskID     int                    `json:"task_id"`
    Status     string                 `json:"status"`     // pending/running/completed/failed
    Progress   int                    `json:"progress"`   // 0-100
    Message    string                 `json:"message"`
    Error      string                 `json:"error,omitempty"`
    StartedAt  string                 `json:"started_at,omitempty"`
    CompletedAt string                `json:"completed_at,omitempty"`
}

// ResultResponse - 任务结果响应
type ResultResponse struct {
    TaskID      int                    `json:"task_id"`
    Status      string                 `json:"status"`
    Result      map[string]interface{} `json:"result"`
    RowsAffected int64                 `json:"rows_affected,omitempty"`
    Duration    int64                  `json:"duration_ms"`
}
```

**Handler 实现**：见 [6.2 节](#62-transfer-模块-api-实现)

#### 5.2.2 Meta 模块 API 设计

**新增接口**：

| 方法 | 路径 | 描述 | 请求体 | 响应体 |
|------|------|------|--------|--------|
| POST | `/api/scan/table` | 扫描单表元数据 | `ScanTableRequest` | `TaskResponse` |
| POST | `/api/scan/schema` | 扫描整个 schema | `ScanSchemaRequest` | `TaskResponse` |
| POST | `/api/scan/database` | 扫描整个数据库 | `ScanDatabaseRequest` | `TaskResponse` |
| POST | `/api/lineage/record` | 记录血缘关系 | `LineageRecord` | `SuccessResponse` |
| GET | `/api/scan/{id}/status` | 查询扫描状态 | - | `StatusResponse` |

**数据结构**：

```go
// ScanTableRequest - 单表扫描请求
type ScanTableRequest struct {
    ResourceID int    `json:"resource_id"`
    SchemaName string `json:"schema_name"`
    TableName  string `json:"table_name"`
}

// ScanSchemaRequest - Schema 扫描请求
type ScanSchemaRequest struct {
    ResourceID int    `json:"resource_id"`
    SchemaName string `json:"schema_name"`
    ScanDepth  string `json:"scan_depth"` // "table_only", "full"
}

// ScanDatabaseRequest - 全库扫描请求
type ScanDatabaseRequest struct {
    ResourceID int    `json:"resource_id"`
    ScanDepth  string `json:"scan_depth"` // "schema_only", "table_only", "full"
}

// LineageRecord - 血缘记录
type LineageRecord struct {
    SourceResourceID int                    `json:"source_resource_id"`
    SourceTable      string                 `json:"source_table"`
    TargetResourceID int                    `json:"target_resource_id"`
    TargetTable      string                 `json:"target_table"`
    TransformSQL     string                 `json:"transform_sql,omitempty"`
    WorkflowID       string                 `json:"workflow_id,omitempty"`
    Metadata         map[string]interface{} `json:"metadata,omitempty"`
}
```

---

### 5.3 Asynq 队列层

#### 5.3.1 队列命名规范

**格式**：`{module}:{priority}`

**示例**：
- `transfer:critical` - Transfer 模块高优先级任务
- `transfer:default` - Transfer 模块普通任务
- `transfer:low` - Transfer 模块低优先级任务
- `meta:critical` - Meta 模块高优先级任务
- `meta:default` - Meta 模块普通任务
- `meta:low` - Meta 模块低优先级任务

#### 5.3.2 任务类型定义

**Transfer 模块**：

```go
const (
    TypeExtract   = "transfer:extract"   // 数据提取任务
    TypeTransform = "transfer:transform" // 数据转换任务
    TypeLoad      = "transfer:load"      // 数据加载任务
)
```

**Meta 模块**：

```go
const (
    TypeScanTable    = "meta:scan_table"    // 扫描单表
    TypeScanSchema   = "meta:scan_schema"   // 扫描 Schema
    TypeScanDatabase = "meta:scan_database" // 扫描全库
    TypeCalcLineage  = "meta:calc_lineage"  // 计算血缘
)
```

#### 5.3.3 队列配置

**Transfer Worker**：

```go
asynq.Config{
    Concurrency: 10,
    Queues: map[string]int{
        "transfer:critical": 6, // 权重最高
        "transfer:default":  3,
        "transfer:low":      1,
    },
}
```

**Meta Worker**：

```go
asynq.Config{
    Concurrency: 5,
    Queues: map[string]int{
        "meta:critical": 6,
        "meta:default":  3,
        "meta:low":      1,
    },
}
```

---

### 5.4 Worker 执行层

#### 5.4.1 Transfer Worker（保持现有架构）

**文件**：`transfer/backend/cmd/worker/main.go`

**改动**：**无需改动**（已支持多种任务类型）

**扩展**：新增任务处理器（Handler）

```go
// internal/worker/handler.go

// HandleExtractTask - 数据提取任务处理器
func (h *TaskHandler) HandleExtractTask(ctx context.Context, t *asynq.Task) error {
    var payload ExtractTaskPayload
    json.Unmarshal(t.Payload(), &payload)

    // 1. 更新状态为 "running"
    h.executionService.UpdateStatus(payload.ExecutionID, "running")

    // 2. 执行数据提取逻辑
    result, err := h.extractService.Extract(ctx, payload)
    if err != nil {
        h.executionService.UpdateStatus(payload.ExecutionID, "failed", err.Error())
        return err
    }

    // 3. 更新状态为 "completed"
    h.executionService.UpdateStatus(payload.ExecutionID, "completed")
    h.executionService.SaveResult(payload.ExecutionID, result)

    return nil
}

// 注册处理器
func (h *TaskHandler) RegisterHandlers(mux *asynq.ServeMux) {
    mux.HandleFunc(TypeExtract, h.HandleExtractTask)
    mux.HandleFunc(TypeTransform, h.HandleTransformTask)
    mux.HandleFunc(TypeLoad, h.HandleLoadTask)
}
```

#### 5.4.2 Meta Worker（新建）

**文件**：`meta/backend/cmd/worker/main.go`（新建）

**实现**：

```go
package main

import (
    "log"
    "github.com/hibiken/asynq"
    "github.com/addp/meta/backend/internal/worker"
)

func main() {
    cfg := loadConfig()

    // 创建 Asynq Server
    srv := asynq.NewServer(
        asynq.RedisClientOpt{
            Addr:     cfg.RedisAddr,
            Password: cfg.RedisPassword,
        },
        asynq.Config{
            Concurrency: cfg.Concurrency,
            Queues: map[string]int{
                "meta:critical": 6,
                "meta:default":  3,
                "meta:low":      1,
            },
        },
    )

    // 注册任务处理器
    mux := asynq.NewServeMux()
    handler := worker.NewScanHandler(/* dependencies */)
    handler.RegisterHandlers(mux)

    // 启动 Server
    if err := srv.Run(mux); err != nil {
        log.Fatal(err)
    }
}
```

**任务处理器**：

```go
// meta/backend/internal/worker/handler.go

type ScanHandler struct {
    scanService *service.ScanService
    runService  *service.ScanTaskRunService
}

// HandleScanTable - 扫描单表处理器
func (h *ScanHandler) HandleScanTable(ctx context.Context, t *asynq.Task) error {
    var payload ScanTablePayload
    json.Unmarshal(t.Payload(), &payload)

    // 1. 更新状态
    h.runService.UpdateStatus(payload.RunID, "running")

    // 2. 执行扫描
    metadata, err := h.scanService.ScanTable(ctx, payload.ResourceID, payload.SchemaName, payload.TableName)
    if err != nil {
        h.runService.UpdateStatus(payload.RunID, "failed", err.Error())
        return err
    }

    // 3. 存储元数据
    h.scanService.SaveMetadata(metadata)

    // 4. 更新状态
    h.runService.UpdateStatus(payload.RunID, "completed")

    return nil
}

// 注册处理器
func (h *ScanHandler) RegisterHandlers(mux *asynq.ServeMux) {
    mux.HandleFunc(TypeScanTable, h.HandleScanTable)
    mux.HandleFunc(TypeScanSchema, h.HandleScanSchema)
    mux.HandleFunc(TypeScanDatabase, h.HandleScanDatabase)
    mux.HandleFunc(TypeCalcLineage, h.HandleCalcLineage)
}
```

---

## 6. 完整代码示例

### 6.1 Temporal Workflow 实现

#### 6.1.1 通用 HTTP Activity

```go
// temporal/activities/http.go
package activities

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "time"
)

type HTTPRequest struct {
    Method  string                 `json:"method"`  // GET, POST, PUT, DELETE
    URL     string                 `json:"url"`
    Headers map[string]string      `json:"headers"`
    Body    map[string]interface{} `json:"body"`
    Timeout int                    `json:"timeout"` // 秒
}

type HTTPResponse struct {
    StatusCode int                    `json:"status_code"`
    Body       map[string]interface{} `json:"body"`
    Headers    map[string]string      `json:"headers"`
}

// HTTPActivity - 通用 HTTP 请求 Activity
func HTTPActivity(ctx context.Context, req HTTPRequest) (HTTPResponse, error) {
    // 1. 构建请求体
    var bodyReader io.Reader
    if req.Body != nil {
        bodyJSON, err := json.Marshal(req.Body)
        if err != nil {
            return HTTPResponse{}, fmt.Errorf("failed to marshal body: %w", err)
        }
        bodyReader = bytes.NewBuffer(bodyJSON)
    }

    // 2. 创建 HTTP 请求
    httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL, bodyReader)
    if err != nil {
        return HTTPResponse{}, fmt.Errorf("failed to create request: %w", err)
    }

    // 3. 设置 Headers
    httpReq.Header.Set("Content-Type", "application/json")
    for k, v := range req.Headers {
        httpReq.Header.Set(k, v)
    }

    // 4. 发送请求
    timeout := 30 * time.Second
    if req.Timeout > 0 {
        timeout = time.Duration(req.Timeout) * time.Second
    }

    client := &http.Client{Timeout: timeout}
    resp, err := client.Do(httpReq)
    if err != nil {
        return HTTPResponse{}, fmt.Errorf("request failed: %w", err)
    }
    defer resp.Body.Close()

    // 5. 读取响应
    respBody, err := io.ReadAll(resp.Body)
    if err != nil {
        return HTTPResponse{}, fmt.Errorf("failed to read response: %w", err)
    }

    // 6. 解析 JSON
    var respJSON map[string]interface{}
    if err := json.Unmarshal(respBody, &respJSON); err != nil {
        // 如果不是 JSON，返回原始文本
        respJSON = map[string]interface{}{"raw": string(respBody)}
    }

    // 7. 提取响应 Headers
    respHeaders := make(map[string]string)
    for k, v := range resp.Header {
        if len(v) > 0 {
            respHeaders[k] = v[0]
        }
    }

    return HTTPResponse{
        StatusCode: resp.StatusCode,
        Body:       respJSON,
        Headers:    respHeaders,
    }, nil
}
```

#### 6.1.2 轮询任务状态 Activity

```go
// temporal/activities/poll.go
package activities

import (
    "context"
    "fmt"
    "time"
)

type PollRequest struct {
    URL              string        `json:"url"`
    Timeout          time.Duration `json:"timeout"`           // 最大等待时间
    Interval         time.Duration `json:"interval"`          // 轮询间隔
    SuccessStatuses  []string      `json:"success_statuses"`  // 成功状态列表
    FailureStatuses  []string      `json:"failure_statuses"`  // 失败状态列表
}

// PollTaskStatusActivity - 轮询任务状态直到完成
func PollTaskStatusActivity(ctx context.Context, req PollRequest) error {
    // 默认值
    if req.Timeout == 0 {
        req.Timeout = 10 * time.Minute
    }
    if req.Interval == 0 {
        req.Interval = 5 * time.Second
    }
    if len(req.SuccessStatuses) == 0 {
        req.SuccessStatuses = []string{"completed"}
    }
    if len(req.FailureStatuses) == 0 {
        req.FailureStatuses = []string{"failed", "cancelled"}
    }

    ticker := time.NewTicker(req.Interval)
    defer ticker.Stop()

    timeout := time.After(req.Timeout)

    for {
        select {
        case <-timeout:
            return fmt.Errorf("task timeout after %v", req.Timeout)

        case <-ctx.Done():
            return ctx.Err()

        case <-ticker.C:
            // 查询任务状态
            resp, err := HTTPActivity(ctx, HTTPRequest{
                Method: "GET",
                URL:    req.URL,
            })
            if err != nil {
                // 查询失败，继续重试
                continue
            }

            // 提取状态字段
            status, ok := resp.Body["status"].(string)
            if !ok {
                continue
            }

            // 检查是否成功
            for _, s := range req.SuccessStatuses {
                if status == s {
                    return nil // 成功完成
                }
            }

            // 检查是否失败
            for _, s := range req.FailureStatuses {
                if status == s {
                    errorMsg := "task failed"
                    if errField, ok := resp.Body["error"].(string); ok {
                        errorMsg = errField
                    }
                    return fmt.Errorf("task failed: %s", errorMsg)
                }
            }

            // 其他状态（pending/running），继续等待
        }
    }
}
```

#### 6.1.3 ETL 工作流实现

```go
// temporal/workflows/etl_workflow.go
package workflows

import (
    "fmt"
    "time"
    "go.temporal.io/sdk/workflow"
    "github.com/addp/temporal/activities"
)

type ETLWorkflowInput struct {
    SourceResourceID int                    `json:"source_resource_id"`
    TargetResourceID int                    `json:"target_resource_id"`
    Query            string                 `json:"query"`
    TransformRules   []map[string]interface{} `json:"transform_rules"`
    TargetTable      string                 `json:"target_table"`
}

func ETLWorkflow(ctx workflow.Context, input ETLWorkflowInput) error {
    logger := workflow.GetLogger(ctx)
    workflowInfo := workflow.GetInfo(ctx)

    logger.Info("Starting ETL workflow",
        "workflow_id", workflowInfo.WorkflowExecution.ID,
        "source", input.SourceResourceID,
        "target", input.TargetResourceID)

    // 配置 Activity 选项
    activityOptions := workflow.ActivityOptions{
        StartToCloseTimeout: 30 * time.Second,
        RetryPolicy: &workflow.RetryPolicy{
            MaximumAttempts: 3,
        },
    }
    ctx = workflow.WithActivityOptions(ctx, activityOptions)

    // ==================== 步骤 1: 数据提取 ====================
    logger.Info("Step 1: Extracting data")

    var extractResp activities.HTTPResponse
    err := workflow.ExecuteActivity(ctx, activities.HTTPActivity, activities.HTTPRequest{
        Method: "POST",
        URL:    "http://transfer:8083/api/tasks/extract",
        Body: map[string]interface{}{
            "source_resource_id": input.SourceResourceID,
            "query":              input.Query,
        },
    }).Get(ctx, &extractResp)

    if err != nil {
        return fmt.Errorf("extract request failed: %w", err)
    }

    if extractResp.StatusCode != 202 {
        return fmt.Errorf("extract request returned status %d", extractResp.StatusCode)
    }

    extractTaskIDFloat, ok := extractResp.Body["task_id"].(float64)
    if !ok {
        return fmt.Errorf("invalid task_id in response")
    }
    extractTaskID := int(extractTaskIDFloat)

    logger.Info("Extract task created", "task_id", extractTaskID)

    // 等待提取任务完成
    pollOptions := workflow.ActivityOptions{
        StartToCloseTimeout: 10 * time.Minute,
    }
    ctx = workflow.WithActivityOptions(ctx, pollOptions)

    err = workflow.ExecuteActivity(ctx, activities.PollTaskStatusActivity, activities.PollRequest{
        URL:      fmt.Sprintf("http://transfer:8083/api/tasks/%d/status", extractTaskID),
        Timeout:  10 * time.Minute,
        Interval: 5 * time.Second,
    }).Get(ctx, nil)

    if err != nil {
        return fmt.Errorf("extract task failed: %w", err)
    }

    logger.Info("Extract completed", "task_id", extractTaskID)

    // ==================== 步骤 2: 数据转换 ====================
    logger.Info("Step 2: Transforming data")

    ctx = workflow.WithActivityOptions(ctx, activityOptions)

    var transformResp activities.HTTPResponse
    err = workflow.ExecuteActivity(ctx, activities.HTTPActivity, activities.HTTPRequest{
        Method: "POST",
        URL:    "http://transfer:8083/api/tasks/transform",
        Body: map[string]interface{}{
            "extract_task_id": extractTaskID,
            "rules":           input.TransformRules,
        },
    }).Get(ctx, &transformResp)

    if err != nil {
        return fmt.Errorf("transform request failed: %w", err)
    }

    transformTaskIDFloat := transformResp.Body["task_id"].(float64)
    transformTaskID := int(transformTaskIDFloat)

    logger.Info("Transform task created", "task_id", transformTaskID)

    // 等待转换任务完成
    ctx = workflow.WithActivityOptions(ctx, pollOptions)
    err = workflow.ExecuteActivity(ctx, activities.PollTaskStatusActivity, activities.PollRequest{
        URL:      fmt.Sprintf("http://transfer:8083/api/tasks/%d/status", transformTaskID),
        Timeout:  10 * time.Minute,
        Interval: 5 * time.Second,
    }).Get(ctx, nil)

    if err != nil {
        return fmt.Errorf("transform task failed: %w", err)
    }

    logger.Info("Transform completed", "task_id", transformTaskID)

    // ==================== 步骤 3: 数据加载 ====================
    logger.Info("Step 3: Loading data")

    ctx = workflow.WithActivityOptions(ctx, activityOptions)

    var loadResp activities.HTTPResponse
    err = workflow.ExecuteActivity(ctx, activities.HTTPActivity, activities.HTTPRequest{
        Method: "POST",
        URL:    "http://transfer:8083/api/tasks/load",
        Body: map[string]interface{}{
            "transform_task_id":  transformTaskID,
            "target_resource_id": input.TargetResourceID,
            "target_table":       input.TargetTable,
        },
    }).Get(ctx, &loadResp)

    if err != nil {
        return fmt.Errorf("load request failed: %w", err)
    }

    loadTaskIDFloat := loadResp.Body["task_id"].(float64)
    loadTaskID := int(loadTaskIDFloat)

    logger.Info("Load task created", "task_id", loadTaskID)

    // 等待加载任务完成
    ctx = workflow.WithActivityOptions(ctx, pollOptions)
    err = workflow.ExecuteActivity(ctx, activities.PollTaskStatusActivity, activities.PollRequest{
        URL:      fmt.Sprintf("http://transfer:8083/api/tasks/%d/status", loadTaskID),
        Timeout:  10 * time.Minute,
        Interval: 5 * time.Second,
    }).Get(ctx, nil)

    if err != nil {
        return fmt.Errorf("load task failed: %w", err)
    }

    logger.Info("Load completed", "task_id", loadTaskID)

    // ==================== 步骤 4: 记录血缘 ====================
    logger.Info("Step 4: Recording lineage")

    ctx = workflow.WithActivityOptions(ctx, activityOptions)

    workflow.ExecuteActivity(ctx, activities.HTTPActivity, activities.HTTPRequest{
        Method: "POST",
        URL:    "http://meta:8082/api/lineage/record",
        Body: map[string]interface{}{
            "source_resource_id": input.SourceResourceID,
            "target_resource_id": input.TargetResourceID,
            "workflow_id":        workflowInfo.WorkflowExecution.ID,
            "extract_task_id":    extractTaskID,
            "transform_task_id":  transformTaskID,
            "load_task_id":       loadTaskID,
        },
    }).Get(ctx, nil)

    logger.Info("ETL workflow completed successfully",
        "workflow_id", workflowInfo.WorkflowExecution.ID,
        "extract_task", extractTaskID,
        "transform_task", transformTaskID,
        "load_task", loadTaskID)

    return nil
}
```

#### 6.1.4 元数据扫描工作流

```go
// temporal/workflows/scan_workflow.go
package workflows

import (
    "fmt"
    "time"
    "go.temporal.io/sdk/workflow"
    "github.com/addp/temporal/activities"
)

type ScanDatabaseWorkflowInput struct {
    ResourceID int    `json:"resource_id"`
    ScanDepth  string `json:"scan_depth"` // "schema_only", "table_only", "full"
}

func ScanDatabaseWorkflow(ctx workflow.Context, input ScanDatabaseWorkflowInput) error {
    logger := workflow.GetLogger(ctx)

    logger.Info("Starting database scan workflow",
        "resource_id", input.ResourceID,
        "depth", input.ScanDepth)

    activityOptions := workflow.ActivityOptions{
        StartToCloseTimeout: 30 * time.Second,
    }
    ctx = workflow.WithActivityOptions(ctx, activityOptions)

    // 步骤 1: 触发扫描任务
    var scanResp activities.HTTPResponse
    err := workflow.ExecuteActivity(ctx, activities.HTTPActivity, activities.HTTPRequest{
        Method: "POST",
        URL:    "http://meta:8082/api/scan/database",
        Body: map[string]interface{}{
            "resource_id": input.ResourceID,
            "scan_depth":  input.ScanDepth,
        },
    }).Get(ctx, &scanResp)

    if err != nil {
        return fmt.Errorf("scan request failed: %w", err)
    }

    scanTaskIDFloat := scanResp.Body["task_id"].(float64)
    scanTaskID := int(scanTaskIDFloat)

    logger.Info("Scan task created", "task_id", scanTaskID)

    // 步骤 2: 等待扫描完成（可能很长时间）
    pollOptions := workflow.ActivityOptions{
        StartToCloseTimeout: 60 * time.Minute, // 扫描可能需要 1 小时
    }
    ctx = workflow.WithActivityOptions(ctx, pollOptions)

    err = workflow.ExecuteActivity(ctx, activities.PollTaskStatusActivity, activities.PollRequest{
        URL:      fmt.Sprintf("http://meta:8082/api/scan/%d/status", scanTaskID),
        Timeout:  60 * time.Minute,
        Interval: 10 * time.Second,
    }).Get(ctx, nil)

    if err != nil {
        return fmt.Errorf("scan task failed: %w", err)
    }

    logger.Info("Database scan completed", "task_id", scanTaskID)
    return nil
}
```

---

### 6.2 Transfer 模块 API 实现

#### 6.2.1 任务触发 Handler

```go
// transfer/backend/internal/api/task_handler.go
package api

import (
    "github.com/gin-gonic/gin"
    "github.com/addp/transfer/backend/internal/models"
    "github.com/addp/transfer/backend/internal/service"
    "github.com/addp/transfer/backend/internal/worker"
)

type TaskHandler struct {
    taskQueue        worker.TaskQueue
    executionService *service.ExecutionService
}

func NewTaskHandler(queue worker.TaskQueue, execService *service.ExecutionService) *TaskHandler {
    return &TaskHandler{
        taskQueue:        queue,
        executionService: execService,
    }
}

// POST /api/tasks/extract
func (h *TaskHandler) CreateExtractTask(c *gin.Context) {
    var req models.ExtractRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, gin.H{"error": err.Error()})
        return
    }

    // 1. 创建执行记录
    execution := &models.TaskExecution{
        TaskType: "extract",
        Status:   "pending",
        Input:    req,
        TenantID: getTenantID(c),
    }

    err := h.executionService.Create(execution)
    if err != nil {
        c.JSON(500, gin.H{"error": "Failed to create execution record"})
        return
    }

    // 2. Enqueue 到 Asynq
    payload := worker.ExtractTaskPayload{
        ExecutionID:      execution.ID,
        SourceResourceID: req.SourceResourceID,
        Query:            req.Query,
        Params:           req.Params,
        TenantID:         execution.TenantID,
    }

    err = h.taskQueue.EnqueueExtractTask(payload)
    if err != nil {
        c.JSON(500, gin.H{"error": "Failed to enqueue task"})
        return
    }

    // 3. 返回任务 ID
    c.JSON(202, gin.H{
        "task_id":    execution.ID,
        "status":     "pending",
        "message":    "Task enqueued successfully",
        "created_at": execution.CreatedAt,
    })
}

// POST /api/tasks/transform
func (h *TaskHandler) CreateTransformTask(c *gin.Context) {
    var req models.TransformRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, gin.H{"error": err.Error()})
        return
    }

    // 验证上游任务是否完成
    extractExecution, err := h.executionService.GetByID(req.ExtractTaskID)
    if err != nil {
        c.JSON(404, gin.H{"error": "Extract task not found"})
        return
    }

    if extractExecution.Status != "completed" {
        c.JSON(400, gin.H{"error": "Extract task not completed yet"})
        return
    }

    // 创建转换任务
    execution := &models.TaskExecution{
        TaskType: "transform",
        Status:   "pending",
        Input:    req,
        TenantID: extractExecution.TenantID,
    }

    h.executionService.Create(execution)

    // Enqueue
    payload := worker.TransformTaskPayload{
        ExecutionID:   execution.ID,
        ExtractTaskID: req.ExtractTaskID,
        Rules:         req.Rules,
        TenantID:      execution.TenantID,
    }

    h.taskQueue.EnqueueTransformTask(payload)

    c.JSON(202, gin.H{
        "task_id":    execution.ID,
        "status":     "pending",
        "message":    "Task enqueued successfully",
        "created_at": execution.CreatedAt,
    })
}

// POST /api/tasks/load
func (h *TaskHandler) CreateLoadTask(c *gin.Context) {
    var req models.LoadRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, gin.H{"error": err.Error()})
        return
    }

    // 验证上游任务
    transformExecution, err := h.executionService.GetByID(req.TransformTaskID)
    if err != nil {
        c.JSON(404, gin.H{"error": "Transform task not found"})
        return
    }

    if transformExecution.Status != "completed" {
        c.JSON(400, gin.H{"error": "Transform task not completed yet"})
        return
    }

    // 创建加载任务
    execution := &models.TaskExecution{
        TaskType: "load",
        Status:   "pending",
        Input:    req,
        TenantID: transformExecution.TenantID,
    }

    h.executionService.Create(execution)

    // Enqueue
    payload := worker.LoadTaskPayload{
        ExecutionID:      execution.ID,
        TransformTaskID:  req.TransformTaskID,
        TargetResourceID: req.TargetResourceID,
        TargetTable:      req.TargetTable,
        TenantID:         execution.TenantID,
    }

    h.taskQueue.EnqueueLoadTask(payload)

    c.JSON(202, gin.H{
        "task_id":    execution.ID,
        "status":     "pending",
        "message":    "Task enqueued successfully",
        "created_at": execution.CreatedAt,
    })
}

// GET /api/tasks/:id/status
func (h *TaskHandler) GetTaskStatus(c *gin.Context) {
    taskID := c.Param("id")

    execution, err := h.executionService.GetByID(taskID)
    if err != nil {
        c.JSON(404, gin.H{"error": "Task not found"})
        return
    }

    c.JSON(200, gin.H{
        "task_id":      execution.ID,
        "status":       execution.Status,
        "progress":     execution.Progress,
        "message":      execution.Message,
        "error":        execution.Error,
        "started_at":   execution.StartedAt,
        "completed_at": execution.CompletedAt,
    })
}

// GET /api/tasks/:id/result
func (h *TaskHandler) GetTaskResult(c *gin.Context) {
    taskID := c.Param("id")

    execution, err := h.executionService.GetByID(taskID)
    if err != nil {
        c.JSON(404, gin.H{"error": "Task not found"})
        return
    }

    if execution.Status != "completed" {
        c.JSON(400, gin.H{"error": "Task not completed yet", "status": execution.Status})
        return
    }

    c.JSON(200, gin.H{
        "task_id":       execution.ID,
        "status":        execution.Status,
        "result":        execution.Result,
        "rows_affected": execution.RowsAffected,
        "duration_ms":   execution.DurationMs,
    })
}

// 注册路由
func (h *TaskHandler) RegisterRoutes(r *gin.RouterGroup) {
    r.POST("/tasks/extract", h.CreateExtractTask)
    r.POST("/tasks/transform", h.CreateTransformTask)
    r.POST("/tasks/load", h.CreateLoadTask)
    r.GET("/tasks/:id/status", h.GetTaskStatus)
    r.GET("/tasks/:id/result", h.GetTaskResult)
}
```

#### 6.2.2 Asynq 队列扩展

```go
// transfer/backend/internal/worker/queue.go
package worker

const (
    TypeExtract   = "transfer:extract"
    TypeTransform = "transfer:transform"
    TypeLoad      = "transfer:load"
)

type ExtractTaskPayload struct {
    ExecutionID      uint                   `json:"execution_id"`
    SourceResourceID int                    `json:"source_resource_id"`
    Query            string                 `json:"query"`
    Params           map[string]interface{} `json:"params"`
    TenantID         uint                   `json:"tenant_id"`
}

type TransformTaskPayload struct {
    ExecutionID   uint                     `json:"execution_id"`
    ExtractTaskID int                      `json:"extract_task_id"`
    Rules         []map[string]interface{} `json:"rules"`
    TenantID      uint                     `json:"tenant_id"`
}

type LoadTaskPayload struct {
    ExecutionID      uint   `json:"execution_id"`
    TransformTaskID  int    `json:"transform_task_id"`
    TargetResourceID int    `json:"target_resource_id"`
    TargetTable      string `json:"target_table"`
    TenantID         uint   `json:"tenant_id"`
}

// EnqueueExtractTask - 入队提取任务
func (q *TaskQueue) EnqueueExtractTask(payload ExtractTaskPayload) error {
    payloadBytes, err := json.Marshal(payload)
    if err != nil {
        return err
    }

    task := asynq.NewTask(TypeExtract, payloadBytes)
    _, err = q.client.Enqueue(task, asynq.Queue("transfer:default"))
    return err
}

// EnqueueTransformTask - 入队转换任务
func (q *TaskQueue) EnqueueTransformTask(payload TransformTaskPayload) error {
    payloadBytes, err := json.Marshal(payload)
    if err != nil {
        return err
    }

    task := asynq.NewTask(TypeTransform, payloadBytes)
    _, err = q.client.Enqueue(task, asynq.Queue("transfer:default"))
    return err
}

// EnqueueLoadTask - 入队加载任务
func (q *TaskQueue) EnqueueLoadTask(payload LoadTaskPayload) error {
    payloadBytes, err := json.Marshal(payload)
    if err != nil {
        return err
    }

    task := asynq.NewTask(TypeLoad, payloadBytes)
    _, err = q.client.Enqueue(task, asynq.Queue("transfer:default"))
    return err
}
```

---

### 6.3 Meta 模块 API 实现

#### 6.3.1 扫描任务 Handler

```go
// meta/backend/internal/api/scan_handler.go
package api

import (
    "github.com/gin-gonic/gin"
    "github.com/addp/meta/backend/internal/models"
    "github.com/addp/meta/backend/internal/service"
    "github.com/addp/meta/backend/internal/worker"
)

type ScanHandler struct {
    scanQueue   *worker.ScanQueue
    runService  *service.ScanTaskRunService
}

// POST /api/scan/table
func (h *ScanHandler) ScanTable(c *gin.Context) {
    var req models.ScanTableRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, gin.H{"error": err.Error()})
        return
    }

    // 创建任务运行记录
    taskRun := &models.ScanTaskRun{
        ResourceID: req.ResourceID,
        ScanType:   "table",
        ScanTarget: fmt.Sprintf("%s.%s", req.SchemaName, req.TableName),
        Status:     "pending",
    }

    h.runService.Create(taskRun)

    // Enqueue
    payload := worker.ScanTablePayload{
        RunID:      taskRun.ID,
        ResourceID: req.ResourceID,
        SchemaName: req.SchemaName,
        TableName:  req.TableName,
    }

    h.scanQueue.EnqueueScanTable(payload)

    c.JSON(202, gin.H{
        "task_id": taskRun.ID,
        "status":  "pending",
    })
}

// POST /api/scan/schema
func (h *ScanHandler) ScanSchema(c *gin.Context) {
    var req models.ScanSchemaRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, gin.H{"error": err.Error()})
        return
    }

    taskRun := &models.ScanTaskRun{
        ResourceID: req.ResourceID,
        ScanType:   "schema",
        ScanTarget: req.SchemaName,
        Status:     "pending",
    }

    h.runService.Create(taskRun)

    payload := worker.ScanSchemaPayload{
        RunID:      taskRun.ID,
        ResourceID: req.ResourceID,
        SchemaName: req.SchemaName,
        ScanDepth:  req.ScanDepth,
    }

    h.scanQueue.EnqueueScanSchema(payload)

    c.JSON(202, gin.H{
        "task_id": taskRun.ID,
        "status":  "pending",
    })
}

// POST /api/scan/database
func (h *ScanHandler) ScanDatabase(c *gin.Context) {
    var req models.ScanDatabaseRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, gin.H{"error": err.Error()})
        return
    }

    taskRun := &models.ScanTaskRun{
        ResourceID: req.ResourceID,
        ScanType:   "database",
        Status:     "pending",
    }

    h.runService.Create(taskRun)

    payload := worker.ScanDatabasePayload{
        RunID:      taskRun.ID,
        ResourceID: req.ResourceID,
        ScanDepth:  req.ScanDepth,
    }

    h.scanQueue.EnqueueScanDatabase(payload)

    c.JSON(202, gin.H{
        "task_id": taskRun.ID,
        "status":  "pending",
    })
}

// POST /api/lineage/record
func (h *ScanHandler) RecordLineage(c *gin.Context) {
    var req models.LineageRecord
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, gin.H{"error": err.Error()})
        return
    }

    // 直接存储血缘记录（同步操作）
    err := h.lineageService.Save(&req)
    if err != nil {
        c.JSON(500, gin.H{"error": "Failed to save lineage record"})
        return
    }

    // 可选：异步触发血缘计算
    h.scanQueue.EnqueueCalculateLineage(worker.LineagePayload{
        SourceTable: req.SourceTable,
        TargetTable: req.TargetTable,
    })

    c.JSON(200, gin.H{"message": "Lineage recorded successfully"})
}

// GET /api/scan/:id/status
func (h *ScanHandler) GetScanStatus(c *gin.Context) {
    runID := c.Param("id")

    taskRun, err := h.runService.GetByID(runID)
    if err != nil {
        c.JSON(404, gin.H{"error": "Scan task not found"})
        return
    }

    c.JSON(200, gin.H{
        "task_id":      taskRun.ID,
        "status":       taskRun.Status,
        "progress":     taskRun.Progress,
        "message":      taskRun.Message,
        "error":        taskRun.Error,
        "started_at":   taskRun.StartedAt,
        "completed_at": taskRun.CompletedAt,
    })
}

// 注册路由
func (h *ScanHandler) RegisterRoutes(r *gin.RouterGroup) {
    r.POST("/scan/table", h.ScanTable)
    r.POST("/scan/schema", h.ScanSchema)
    r.POST("/scan/database", h.ScanDatabase)
    r.POST("/lineage/record", h.RecordLineage)
    r.GET("/scan/:id/status", h.GetScanStatus)
}
```

---

## 7. 部署配置

### 7.1 Docker Compose 配置

```yaml
# docker-compose.yml
version: '3.8'

services:
  # ==================== 现有服务 ====================
  postgres:
    image: postgres:15
    environment:
      POSTGRES_USER: addp
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
      POSTGRES_DB: addp
    ports:
      - "5432:5432"
    volumes:
      - postgres_data:/var/lib/postgresql/data
    networks:
      - addp-network

  redis:
    image: redis:7
    command: redis-server --requirepass ${REDIS_PASSWORD}
    ports:
      - "6379:6379"
    volumes:
      - redis_data:/data
    networks:
      - addp-network

  # ==================== Temporal 服务 (新增) ====================
  temporal:
    image: temporalio/auto-setup:latest
    container_name: addp-temporal
    ports:
      - "7233:7233"   # gRPC API
      - "8233:8233"   # Web UI
    environment:
      - DB=postgresql
      - POSTGRES_SEEDS=postgres
      - POSTGRES_USER=addp
      - POSTGRES_PWD=${POSTGRES_PASSWORD}
      - DYNAMIC_CONFIG_FILE_PATH=config/dynamicconfig/development-sql.yaml
    depends_on:
      - postgres
    networks:
      - addp-network
    profiles:
      - full

  # ==================== Temporal Worker (新增) ====================
  temporal-worker:
    build:
      context: ./temporal
      dockerfile: Dockerfile
    environment:
      - TEMPORAL_HOST_PORT=temporal:7233
      - TRANSFER_API_URL=http://transfer:8083
      - META_API_URL=http://meta:8082
    depends_on:
      - temporal
    networks:
      - addp-network
    profiles:
      - full

  # ==================== Transfer 模块 ====================
  transfer:
    build:
      context: ./transfer/backend
      dockerfile: Dockerfile
    ports:
      - "8083:8083"
    environment:
      - PORT=8083
      - REDIS_ADDR=redis:6379
      - REDIS_PASSWORD=${REDIS_PASSWORD}
      - POSTGRES_HOST=postgres
      - ENABLE_TEMPORAL=true
    depends_on:
      - postgres
      - redis
    networks:
      - addp-network
    profiles:
      - full

  transfer-worker:
    build:
      context: ./transfer/backend
      dockerfile: Dockerfile.worker
    environment:
      - REDIS_ADDR=redis:6379
      - REDIS_PASSWORD=${REDIS_PASSWORD}
      - POSTGRES_HOST=postgres
      - ASYNQ_CONCURRENCY=10
    depends_on:
      - redis
      - postgres
    networks:
      - addp-network
    profiles:
      - full

  # ==================== Meta 模块 ====================
  meta:
    build:
      context: ./meta/backend
      dockerfile: Dockerfile
    ports:
      - "8082:8082"
    environment:
      - PORT=8082
      - REDIS_ADDR=redis:6379
      - REDIS_PASSWORD=${REDIS_PASSWORD}
      - POSTGRES_HOST=postgres
    depends_on:
      - postgres
      - redis
    networks:
      - addp-network
    profiles:
      - full

  meta-worker:
    build:
      context: ./meta/backend
      dockerfile: Dockerfile.worker
    environment:
      - REDIS_ADDR=redis:6379
      - REDIS_PASSWORD=${REDIS_PASSWORD}
      - POSTGRES_HOST=postgres
      - ASYNQ_CONCURRENCY=5
    depends_on:
      - redis
      - postgres
    networks:
      - addp-network
    profiles:
      - full

networks:
  addp-network:
    driver: bridge

volumes:
  postgres_data:
  redis_data:
```

### 7.2 环境变量配置

```bash
# .env
# Security
JWT_SECRET=your-super-secret-jwt-key

# PostgreSQL
POSTGRES_PASSWORD=addp_password

# Redis
REDIS_PASSWORD=addp_redis

# Temporal
TEMPORAL_HOST_PORT=localhost:7233
ENABLE_TEMPORAL=true

# Transfer Module
TRANSFER_ASYNQ_CONCURRENCY=10

# Meta Module
META_ASYNQ_CONCURRENCY=5
```

---

## 8. 迁移路线图

### 阶段 1：基础设施准备（1 周）

- [ ] 部署 Temporal Server（Docker Compose）
- [ ] 配置 Temporal 连接到现有 PostgreSQL
- [ ] 验证 Temporal Web UI 可访问（http://localhost:8233）
- [ ] 更新 `.env` 添加 Temporal 配置

### 阶段 2：Temporal Worker 开发（2 周）

- [ ] 创建 `temporal/` 独立项目
- [ ] 实现通用 HTTP Activity
- [ ] 实现轮询任务状态 Activity
- [ ] 实现第一个简单工作流（单步 HTTP 调用）
- [ ] 测试 Temporal Worker 与 Server 通信

### 阶段 3：Transfer 模块集成（3 周）

- [ ] 扩展 Transfer API（新增任务触发接口）
- [ ] 扩展 Asynq 队列（新增任务类型）
- [ ] 扩展 Worker Handler（新增任务处理器）
- [ ] 实现 ETL 工作流（Temporal 编排）
- [ ] 端到端测试（Portal → Temporal → Transfer API → Asynq → Worker）

### 阶段 4：Meta 模块集成（3 周）

- [ ] 创建 `meta/backend/cmd/worker/main.go`
- [ ] 实现 Meta Asynq 队列和 Handler
- [ ] 扩展 Meta API（扫描和血缘接口）
- [ ] 实现扫描工作流（Temporal 编排）
- [ ] 迁移现有 goroutine-based worker 到 Asynq

### 阶段 5：监控和可观测性（2 周）

- [ ] 统一日志格式
- [ ] Prometheus 指标导出
- [ ] 开发统一任务监控界面（Portal）
- [ ] 集成 Temporal Web UI（iframe 嵌入）
- [ ] 告警规则配置

### 阶段 6：生产部署（1 周）

- [ ] Temporal 高可用配置（集群模式）
- [ ] Worker 自动扩容配置
- [ ] 负载测试和性能调优
- [ ] 文档完善和团队培训

---

## 9. 监控和可观测性

### 9.1 统一监控界面

```
┌──────────────────────────────────────────────────────────┐
│  ADDP Portal - 任务监控中心                              │
│                                                          │
│  ┌────────────────┐  ┌────────────────┐                │
│  │  Asynq 队列    │  │  Temporal 工作流│                │
│  │  - transfer:*  │  │  - 运行中: 15   │                │
│  │    pending: 5  │  │  - 已完成: 120  │                │
│  │    active: 3   │  │  - 失败: 2      │                │
│  │  - meta:*      │  │                 │                │
│  │    pending: 2  │  │  最近执行:      │                │
│  │    active: 1   │  │  - ETL-001 ✅   │                │
│  └────────────────┘  │  - Scan-002 🔄  │                │
│                      │  - Lineage-003❌│                │
│                      └────────────────┘                │
│                                                          │
│  外部链接:                                               │
│  - Temporal Web UI: http://localhost:8233               │
└──────────────────────────────────────────────────────────┘
```

### 9.2 Prometheus 指标

```go
// 工作流执行指标
temporal_workflow_executions_total{workflow_type="ETLWorkflow", status="completed"}
temporal_workflow_executions_total{workflow_type="ETLWorkflow", status="failed"}
temporal_workflow_execution_duration_seconds{workflow_type="ETLWorkflow"}

// Asynq 队列指标
asynq_queue_size{queue="transfer:default"}
asynq_queue_latency_seconds{queue="transfer:default"}
asynq_tasks_processed_total{queue="transfer:default", status="success"}
asynq_tasks_processed_total{queue="transfer:default", status="failed"}

// API 指标
http_requests_total{method="POST", endpoint="/api/tasks/extract", status="202"}
http_request_duration_seconds{method="POST", endpoint="/api/tasks/extract"}
```

### 9.3 日志聚合示例

```json
{
  "timestamp": "2024-01-20T10:30:00Z",
  "level": "info",
  "module": "temporal-worker",
  "workflow_id": "etl-20240120-001",
  "workflow_type": "ETLWorkflow",
  "run_id": "abc123",
  "message": "Workflow started",
  "input": {
    "source_resource_id": 1,
    "target_resource_id": 2
  }
}

{
  "timestamp": "2024-01-20T10:30:05Z",
  "level": "info",
  "module": "transfer-api",
  "endpoint": "/api/tasks/extract",
  "method": "POST",
  "task_id": 123,
  "workflow_id": "etl-20240120-001",
  "message": "Extract task created",
  "duration_ms": 50
}

{
  "timestamp": "2024-01-20T10:30:10Z",
  "level": "info",
  "module": "transfer-worker",
  "orchestrator": "asynq",
  "task_type": "extract",
  "task_id": 123,
  "execution_id": 456,
  "message": "Task processing started"
}

{
  "timestamp": "2024-01-20T10:35:00Z",
  "level": "info",
  "module": "transfer-worker",
  "orchestrator": "asynq",
  "task_type": "extract",
  "task_id": 123,
  "execution_id": 456,
  "message": "Task completed successfully",
  "rows_extracted": 10000,
  "duration_ms": 290000
}
```

---

## 10. 优势总结

### 10.1 与 DolphinScheduler 对比

| 特性 | DolphinScheduler | Temporal + HTTP + Asynq |
|------|------------------|-------------------------|
| **内存占用** | 2-4GB | ~500MB (Temporal 300MB + Workers 200MB) |
| **启动时间** | 30-60秒 | 5-10秒 |
| **编排方式** | 可视化拖拽 | 代码定义（Go） |
| **扩展性** | 需修改源码 | 只需实现 HTTP API |
| **内存数据传递** | ❌ 不支持（依赖外部存储） | ✅ 支持（Activity 内存传递） |
| **血缘追踪** | ❌ 不支持 | ✅ 可集成（Meta 模块） |
| **跨模块协作** | ⚠️ 需手动配置 | ✅ 原生支持（HTTP API） |
| **学习曲线** | 低（业务人员可用） | 中（需编程知识） |

### 10.2 架构优势

1. **完全解耦**
   - ✅ Temporal 不依赖任何模块代码
   - ✅ 各模块可独立升级
   - ✅ 易于测试和调试

2. **渐进式迁移**
   - ✅ 保留现有 Asynq 架构
   - ✅ 新功能使用 Temporal
   - ✅ 平滑过渡，无风险

3. **统一编排**
   - ✅ 复杂工作流统一管理
   - ✅ 跨模块协作简化
   - ✅ 统一监控和日志

4. **高性能**
   - ✅ 内存数据传递（Activity 内部）
   - ✅ 低资源占用
   - ✅ 支持并行执行

5. **易于扩展**
   - ✅ 新增模块只需提供 HTTP API
   - ✅ 新增工作流只需编写 Go 代码
   - ✅ 无需修改现有代码

---

## 11. 附录

### 11.1 参考文档

- Temporal 官方文档：https://docs.temporal.io/
- Temporal Go SDK：https://github.com/temporalio/sdk-go
- Asynq 官方文档：https://github.com/hibiken/asynq
- ADDP 平台架构：`/Users/pampa/code/addp/CLAUDE.md`

### 11.2 相关文件

- 本文档：`/Users/pampa/code/addp/labs/dolphin/docs/TEMPORAL_ARCHITECTURE.md`
- Transfer Worker：`/Users/pampa/code/addp/transfer/backend/internal/worker/`
- Meta 扫描服务：`/Users/pampa/code/addp/meta/backend/internal/service/scan_service.go`

### 11.3 贡献者

- 架构设计：Claude (Anthropic)
- 需求提出：ADDP 团队
- 验证环境：DolphinScheduler 学习实验室

---

**最后更新**: 2025-01-20
**版本**: v1.0
**状态**: ✅ 设计完成，待实施
