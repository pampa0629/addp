# Orchestrator 模块数据结构和 API 文档

## 目录

- [1. 模块概述](#1-模块概述)
- [2. 数据库结构](#2-数据库结构)
- [3. API 端点清单](#3-api-端点清单)
- [4. 执行引擎架构](#4-执行引擎架构)
- [5. 服务层架构](#5-服务层架构)
- [6. 配置参数](#6-配置参数)

---

## 1. 模块概述

Orchestrator 模块负责任务编排和工作流调度，提供以下功能：

- **DAG 编排**：支持有向无环图（DAG）的任务编排
- **动态引擎调用**：通过能力注册中心动态发现和调用执行引擎
- **拓扑排序**：自动解析任务依赖关系并按正确顺序执行
- **定时调度**：基于 Cron 表达式的定时执行
- **API 配置驱动**：通过 JSON 配置驱动任务 API 调用
- **向后兼容**：同时支持新的动态模式和旧的硬编码模块模式
- **异步执行**：Go 协程 + 轮询实现非阻塞式执行

### 端口配置

- **开发端口**: 8084
- **生产端口**: 8084
- **数据库 Schema**: `orchestrator`
- **依赖**: PostgreSQL, System 模块（能力注册中心）

### 模块依赖关系

```
System（能力注册中心）
  ↓
Orchestrator（任务编排）
  ↓
Meta、Transfer、Manager 等（任务执行）
```

---

## 2. 数据库结构

### 2.1 PostgreSQL Schema: orchestrator

Orchestrator 模块使用 `orchestrator` schema，包含 2 张核心表。

#### 表 1: orchestrations - 编排定义表

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | SERIAL | PRIMARY KEY | 编排唯一标识 |
| `tenant_id` | INTEGER | NOT NULL, INDEXED | 租户 ID |
| `name` | VARCHAR(128) | NOT NULL | 编排名称 |
| `description` | VARCHAR(512) | | 编排描述 |
| `steps` | JSONB | NOT NULL | 步骤定义（JSON 格式） |
| `enabled` | BOOLEAN | DEFAULT false | 是否启用 |
| `cron_expr` | VARCHAR(128) | | Cron 表达式（可选） |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | 创建时间 |
| `updated_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | 更新时间 |
| `deleted_at` | TIMESTAMP | | 软删除时间戳 |

**索引**:
- `idx_orchestrations_tenant` - 租户索引
- `idx_orchestrations_enabled` - 启用状态索引

**Steps 字段结构**:

```json
[
  {
    "id": "step1",
    "name": "扫描元数据",
    "engine_identifier": "meta.scanner.default",
    "parameters": {
      "engine_id": 1,
      "schema_names": ["public"]
    },
    "depends_on": [],
    "timeout": 300
  },
  {
    "id": "step2",
    "name": "生成 MVT 瓦片",
    "engine_identifier": "manager.mvt.default",
    "parameters": {
      "engine_id": 1,
      "schema": "public",
      "table": "cities"
    },
    "depends_on": ["step1"],
    "timeout": 600
  }
]
```

**Go 模型** (`internal/models/orchestration.go`):

```go
type Orchestration struct {
    ID          uint           `gorm:"primaryKey" json:"id"`
    TenantID    uint           `gorm:"not null;index" json:"tenant_id"`
    Name        string         `gorm:"not null;size:128" json:"name"`
    Description string         `gorm:"size:512" json:"description"`
    Steps       Steps          `gorm:"type:jsonb" json:"steps"`
    Enabled     bool           `gorm:"default:false" json:"enabled"`
    CronExpr    string         `gorm:"size:128" json:"cron_expr,omitempty"`
    CreatedAt   time.Time      `json:"created_at"`
    UpdatedAt   time.Time      `json:"updated_at"`
    DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

type Step struct {
    ID               string                 `json:"id"`
    Name             string                 `json:"name"`
    EngineIdentifier string                 `json:"engine_identifier,omitempty"`
    Module           string                 `json:"module,omitempty"`
    Action           string                 `json:"action,omitempty"`
    Endpoint         string                 `json:"endpoint,omitempty"`
    Method           string                 `json:"method,omitempty"`
    Parameters       map[string]interface{} `json:"parameters"`
    DependsOn        []string               `json:"depends_on"`
    Timeout          int                    `json:"timeout"`
}
```

---

#### 表 2: executions - 执行实例表

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | SERIAL | PRIMARY KEY | 执行实例唯一标识 |
| `orchestration_id` | INTEGER | NOT NULL, INDEXED | 编排 ID（关联 orchestrations） |
| `tenant_id` | INTEGER | NOT NULL, INDEXED | 租户 ID |
| `status` | VARCHAR(32) | NOT NULL | 状态：running/completed/failed/pending |
| `current_step` | VARCHAR(64) | | 当前执行步骤 |
| `step_results` | JSONB | | 步骤结果集合（JSON 格式） |
| `error_message` | TEXT | | 错误信息 |
| `started_at` | TIMESTAMP | | 开始时间 |
| `completed_at` | TIMESTAMP | | 完成时间 |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | 创建时间 |

**索引**:
- `idx_executions_orchestration` - 编排索引
- `idx_executions_tenant` - 租户索引
- `idx_executions_status` - 状态索引

**StepResults 字段结构**:

```json
{
  "step1": {
    "status": "success",
    "result": {
      "schemas_scanned": 2,
      "tables_scanned": 50
    },
    "error": "",
    "started_at": "2025-12-11T10:00:00Z",
    "ended_at": "2025-12-11T10:05:00Z",
    "duration": 300000
  },
  "step2": {
    "status": "success",
    "result": {
      "tiles_generated": 5000
    },
    "error": "",
    "started_at": "2025-12-11T10:05:00Z",
    "ended_at": "2025-12-11T10:15:00Z",
    "duration": 600000
  }
}
```

**Go 模型** (`internal/models/orchestration.go`):

```go
type Execution struct {
    ID              uint           `gorm:"primaryKey" json:"id"`
    OrchestrationID uint           `gorm:"not null;index" json:"orchestration_id"`
    TenantID        uint           `gorm:"not null;index" json:"tenant_id"`
    Status          string         `gorm:"not null;size:32" json:"status"`
    CurrentStep     string         `gorm:"size:64" json:"current_step"`
    StepResults     StepResults    `gorm:"type:jsonb" json:"step_results"`
    ErrorMessage    string         `gorm:"type:text" json:"error_message,omitempty"`
    StartedAt       *time.Time     `json:"started_at,omitempty"`
    CompletedAt     *time.Time     `json:"completed_at,omitempty"`
    CreatedAt       time.Time      `json:"created_at"`
}

type StepResult struct {
    Status    string                 `json:"status"`
    Result    map[string]interface{} `json:"result"`
    Error     string                 `json:"error"`
    StartedAt time.Time              `json:"started_at"`
    EndedAt   time.Time              `json:"ended_at"`
    Duration  int64                  `json:"duration"`
}
```

---

### 2.2 数据表关系图

```
orchestrator.orchestrations (编排定义)
    ↓ 1:N
orchestrator.executions (执行实例)

system.engines (能力注册)
    ↓ (动态引用)
orchestrator.orchestrations.steps[].engine_identifier
```

---

## 3. API 端点清单

### 3.1 编排管理 API

#### POST /api/orchestrations - 创建编排

**请求体**:

```json
{
  "name": "数据处理流水线",
  "description": "自动扫描元数据并生成 MVT 瓦片",
  "steps": [
    {
      "id": "step1",
      "name": "扫描元数据",
      "engine_identifier": "meta.scanner.default",
      "parameters": {
        "engine_id": 1,
        "schema_names": ["public"]
      },
      "depends_on": [],
      "timeout": 300
    }
  ],
  "enabled": true,
  "cron_expr": "0 2 * * *"
}
```

**响应** (201 Created): 返回 Orchestration 对象

---

#### GET /api/orchestrations - 列出编排

**响应** (200 OK):

```json
{
  "orchestrations": [
    {
      "id": 1,
      "name": "数据处理流水线",
      "description": "自动扫描元数据并生成 MVT 瓦片",
      "enabled": true,
      "cron_expr": "0 2 * * *",
      "created_at": "2025-12-11T10:00:00Z"
    }
  ]
}
```

---

#### GET /api/orchestrations/:id - 获取编排详情

**响应** (200 OK): 返回 Orchestration 对象（包含完整 steps）

---

#### PUT /api/orchestrations/:id - 更新编排

**请求体**: 同创建编排

**响应** (200 OK): 返回更新后的 Orchestration 对象

**说明**: 如果 enabled 或 cron_expr 变化，自动重新调度

---

#### DELETE /api/orchestrations/:id - 删除编排

**响应** (200 OK):

```json
{
  "message": "删除成功"
}
```

**说明**: 软删除，停止定时调度

---

### 3.2 执行管理 API

#### POST /api/orchestrations/:id/execute - 手动触发执行

**响应** (202 Accepted):

```json
{
  "execution_id": 100
}
```

**说明**: 创建执行实例并异步执行（Go 协程）

---

#### GET /api/orchestrations/:id/executions - 列出执行记录

**查询参数**:
- `limit`: 记录数（默认 20）
- `offset`: 偏移量（默认 0）

**响应** (200 OK):

```json
{
  "items": [
    {
      "id": 100,
      "orchestration_id": 1,
      "status": "completed",
      "started_at": "2025-12-11T10:00:00Z",
      "completed_at": "2025-12-11T10:15:00Z"
    }
  ],
  "total": 1
}
```

---

#### GET /api/orch-executions/:id - 获取执行详情

**响应** (200 OK):

```json
{
  "id": 100,
  "orchestration_id": 1,
  "status": "completed",
  "current_step": "step2",
  "step_results": {
    "step1": {
      "status": "success",
      "result": {"schemas_scanned": 2},
      "duration": 300000
    },
    "step2": {
      "status": "success",
      "result": {"tiles_generated": 5000},
      "duration": 600000
    }
  },
  "started_at": "2025-12-11T10:00:00Z",
  "completed_at": "2025-12-11T10:15:00Z"
}
```

---

### 3.3 计算引擎 API

#### GET /api/compute-engines - 列出计算引擎

**响应** (200 OK):

```json
{
  "engines": [
    {
      "id": 10,
      "unique_identifier": "meta.scanner.default",
      "name": "Meta 元数据扫描器",
      "engine_type": "compute_engine",
      "is_builtin": true,
      "capabilities": {
        "scan": true,
        "schedule": true
      }
    }
  ]
}
```

**说明**: 从 System 模块的能力注册中心动态获取

---

#### GET /api/tasks/list - 列出模块任务

**查询参数**:
- `unique_identifier`: 引擎标识符（必填）
- `page`: 页码（默认 1）
- `page_size`: 每页条数（默认 100）

**响应** (200 OK):

```json
{
  "items": [
    {
      "id": 10,
      "name": "每日扫描",
      "status": "scheduled",
      "next_run_at": "2025-12-12T02:00:00Z"
    }
  ],
  "total": 1
}
```

**说明**: 根据引擎的 TaskAPIConfig 动态调用

---

### 3.4 基础设施 API

#### GET /health - 健康检查

**响应** (200 OK):

```json
{
  "status": "ok"
}
```

---

## 4. 执行引擎架构

### 4.1 DAG 构建和拓扑排序

**Executor** (`internal/service/executor.go`):

```go
type Executor struct {
    orchRepo       *OrchestrationRepository
    execRepo       *ExecutionRepository
    engineRegistry *EngineRegistry
    taskClient     *TaskClient
    moduleClient   *ModuleClient
}

// ExecuteAsync 异步执行编排
func (e *Executor) ExecuteAsync(executionID uint) {
    go e.executeSync(context.Background(), executionID)
}

// executeSync 同步执行编排的核心逻辑
func (e *Executor) executeSync(ctx context.Context, executionID uint) error {
    // 1. 获取编排和执行实例
    // 2. 构建 DAG
    graph := e.buildDAG(orchestration.Steps)
    // 3. 拓扑排序
    sortedSteps, err := e.topologicalSort(graph)
    // 4. 逐步执行
    for _, stepID := range sortedSteps {
        result, err := e.executeStep(ctx, step)
        // 存储结果
    }
    // 5. 标记完成
}
```

**DAG 处理**:

```go
// buildDAG 从步骤列表构建 DAG 邻接表
func (e *Executor) buildDAG(steps []Step) DAG

// topologicalSort 使用 Kahn 算法执行拓扑排序
func (e *Executor) topologicalSort(graph DAG) ([]string, error)
```

---

### 4.2 步骤执行流程

**executeStep** 支持两种模式：

**新模式**（动态引擎）:

```go
// 1. 从 EngineRegistry 获取引擎配置
engine, err := e.engineRegistry.GetEngine(ctx, step.EngineIdentifier)

// 2. 使用 TaskClient 创建任务
taskID, err := e.taskClient.CreateTask(ctx, engine, step.Parameters)

// 3. 执行任务
runID, err := e.taskClient.ExecuteTask(ctx, engine, taskID, step.Parameters)

// 4. 轮询任务状态直到完成或超时
for {
    status, err := e.taskClient.GetTaskStatus(ctx, engine, runID)
    if status.Status == "completed" || status.Status == "failed" {
        break
    }
    time.Sleep(5 * time.Second)
}
```

**旧模式**（硬编码模块）:

```go
// 使用 ModuleClient 调用
result, err := e.moduleClient.Call(ctx, step.Module, step.Endpoint, step.Method, step.Parameters)
```

---

### 4.3 执行流程图

```
POST /api/orchestrations/:id/execute
  ↓
Handler.Execute()
  ↓
创建 Execution 记录 (status="pending")
  ↓
executor.ExecuteAsync() (Go 协程)
  ↓
标记 status="running"
  ↓
buildDAG() → topologicalSort()
  ↓
for each step (按拓扑顺序):
  ↓
  executeStep()
    ↓
    engineRegistry.GetEngine() → System API
    ↓
    taskClient.CreateTask() → HTTP 请求
    ↓
    taskClient.ExecuteTask() → HTTP 请求
    ↓
    轮询 taskClient.GetTaskStatus() 直到完成
    ↓
    存储 StepResult
  ↓
标记 status="completed" 或 "failed"
  ↓
返回
```

---

## 5. 服务层架构

### 5.1 核心服务类

#### EngineRegistry - 引擎注册中心

**文件**: `internal/service/engine_registry.go`

```go
type EngineRegistry struct {
    systemClient *commonClient.SystemClient
    engines      map[string]*commonModels.Engine
    mu           sync.RWMutex
    cacheTTL     time.Duration  // 5 分钟
    lastRefresh  time.Time
}

// GetEngine 根据 unique_identifier 获取引擎配置
func (r *EngineRegistry) GetEngine(ctx context.Context, identifier string) (*Engine, error)

// RefreshCache 从 System 服务刷新所有引擎缓存
func (r *EngineRegistry) RefreshCache(ctx context.Context) error

// ListAllEngines 列出所有已注册引擎
func (r *EngineRegistry) ListAllEngines(ctx context.Context) ([]*Engine, error)
```

**缓存策略**:
- TTL: 5 分钟
- 自动刷新：缓存过期时自动调用 System API

---

#### TaskClient - 通用任务客户端

**文件**: `internal/service/task_client.go`

```go
type TaskClient struct {
    httpClient *http.Client
    timeout    time.Duration  // 30 秒
}

// CreateTask 创建任务
func (c *TaskClient) CreateTask(ctx context.Context, engine *Engine, params map[string]interface{}) (string, error)

// ExecuteTask 执行任务
func (c *TaskClient) ExecuteTask(ctx context.Context, engine *Engine, taskID string, params map[string]interface{}) (string, error)

// GetTaskStatus 获取任务状态
func (c *TaskClient) GetTaskStatus(ctx context.Context, engine *Engine, taskID string) (*TaskStatus, error)
```

**API 配置驱动**:

所有请求根据 `engine.TaskAPIConfig` 构建：

```json
{
  "base_url": "http://localhost:8082",
  "endpoints": {
    "create": {
      "method": "POST",
      "path": "/api/scan/tasks",
      "body_template": {
        "tenant_id": "{{ .TenantID }}",
        "engine_id": "{{ .ResourceID }}"
      }
    },
    "execute": {
      "method": "POST",
      "path": "/api/scan/tasks/{{.TaskID}}/execute"
    },
    "status": {
      "method": "GET",
      "path": "/api/scan/runs/{{.RunID}}",
      "response_mapping": {
        "status_field": "status",
        "message_field": "error_message",
        "progress_field": "progress"
      }
    }
  },
  "timeout": {
    "create": 30,
    "execute": 300,
    "status": 10
  }
}
```

---

#### Scheduler - 定时调度器

**文件**: `internal/service/scheduler.go`

```go
type Scheduler struct {
    cron          *cron.Cron
    orchRepo      *OrchestrationRepository
    execRepo      *ExecutionRepository
    executor      *Executor
    entryIDs      map[uint]cron.EntryID
}

// Start 启动调度器
func (s *Scheduler) Start() error

// Schedule 调度单个编排任务
func (s *Scheduler) Schedule(orchID uint, cronExpr string) error

// Unschedule 取消调度任务
func (s *Scheduler) Unschedule(orchID uint)
```

**定时机制**:
- 使用 `robfig/cron/v3` 库
- 维护 `entryIDs` 映射管理 cron 任务
- 支持热更新：更新编排时自动重新调度

---

#### ModuleClient - 模块客户端（向后兼容）

**文件**: `internal/service/module_client.go`

```go
type ModuleClient struct {
    httpClient *http.Client
    baseURLs   map[string]string
}

// Call 调用模块 API
func (c *ModuleClient) Call(ctx interface{}, module, endpoint, method string, params map[string]interface{}) (interface{}, error)

// GetTaskStatus 获取任务状态
func (c *ModuleClient) GetTaskStatus(ctx context.Context, module string, taskID interface{}) (string, map[string]interface{}, error)
```

**配置**:

```go
moduleClient := NewModuleClient(map[string]string{
    "transfer": "http://localhost:8083",
    "meta":     "http://localhost:8082",
    "manager":  "http://localhost:8081",
})
```

---

## 6. 配置参数

### 6.1 环境变量清单

| 变量名 | 默认值 | 说明 |
|--------|--------|------|
| `SERVER_PORT` | 8084 | 服务端口 |
| `DB_HOST` | localhost | PostgreSQL 主机 |
| `DB_PORT` | 5432 | PostgreSQL 端口 |
| `DB_USER` | addp | 数据库用户 |
| `DB_PASSWORD` | addp_password | 数据库密码 |
| `DB_NAME` | addp | 数据库名 |
| `DB_SCHEMA` | orchestrator | Orchestrator schema 名 |
| `SYSTEM_SERVICE_URL` | http://localhost:8080 | System 服务 URL |
| `INTERNAL_API_KEY` | - | 内部 API 认证密钥 |
| `TRANSFER_SERVICE_URL` | http://localhost:8083 | Transfer 服务 URL（向后兼容） |
| `META_SERVICE_URL` | http://localhost:8082 | Meta 服务 URL（向后兼容） |
| `MANAGER_SERVICE_URL` | http://localhost:8081 | Manager 服务 URL（向后兼容） |

---

### 6.2 启动流程

**文件**: `orchestrator/backend/cmd/server/main.go`

1. 加载配置
2. 连接 PostgreSQL (search_path=orchestrator)
3. 执行 AutoMigrate 创建表
4. 初始化 EngineRegistry (缓存 5 分钟)
5. 初始化 TaskClient (30 秒超时)
6. 初始化 ModuleClient (向后兼容)
7. 初始化 Executor (支持新旧两种模式)
8. 初始化 Scheduler 并启动
9. 设置路由
10. 启动 HTTP 服务

---

## 7. 关键特性

### 7.1 向后兼容模式

Orchestrator 支持两种步骤定义模式：

**旧模式**（硬编码）:

```json
{
  "module": "meta",
  "action": "scan",
  "endpoint": "/api/scan/tasks",
  "method": "POST",
  "parameters": {}
}
```

→ 使用 `ModuleClient` 调用

**新模式**（动态引擎）:

```json
{
  "engine_identifier": "meta.scanner.default",
  "parameters": {}
}
```

→ 使用 `EngineRegistry` + `TaskClient` 调用

---

### 7.2 关键特性总结

| 特性 | 实现方式 |
|------|---------|
| **DAG 编排** | 邻接表 + Kahn 拓扑排序 |
| **定时调度** | robfig/cron + 内存 EntryID 映射 |
| **动态引擎** | EngineRegistry (从 System 拉取，5min 缓存) |
| **通用任务 API** | TaskClient (基于 TaskAPIConfig) |
| **向后兼容** | ModuleClient (硬编码模块 URL) |
| **异步执行** | Go 协程 + 轮询 |
| **多租户** | TenantID 字段 + where 条件过滤 |
| **软删除** | GORM DeletedAt |

---

## 8. 关键文件路径

| 文件 | 路径 | 说明 |
|------|------|------|
| 路由配置 | `orchestrator/backend/internal/api/router.go` | API 端点定义 |
| 编排模型 | `orchestrator/backend/internal/models/orchestration.go` | 数据模型 |
| 执行器 | `orchestrator/backend/internal/service/executor.go` | DAG + 拓扑排序 |
| 调度器 | `orchestrator/backend/internal/service/scheduler.go` | 定时调度 |
| 引擎注册中心 | `orchestrator/backend/internal/service/engine_registry.go` | 能力发现 |
| 任务客户端 | `orchestrator/backend/internal/service/task_client.go` | 通用任务 API |
| 模块客户端 | `orchestrator/backend/internal/service/module_client.go` | 向后兼容 |
| 应用入口 | `orchestrator/backend/cmd/server/main.go` | 服务启动 |

---

## 9. 前端实现

### 9.1 核心组件

**DAGEditor.vue** (核心编辑器):
- 动态渲染计算引擎按钮（从 API 动态获取）
- 拖拽添加节点到画布
- 连线模式：建立步骤依赖关系
- 节点配置抽屉：编辑步骤参数
- 参数编辑支持 JSON 格式

**OrchestrationList.vue**:
- 列表展示所有编排
- CRUD 操作

**ExecutionList.vue**:
- 显示编排的执行历史
- 查看执行详情和步骤结果

**TaskPanel.vue**:
- 展示单个任务的配置和参数

---

## 10. 相关文档

- [ADDP 平台架构文档](../CLAUDE.md)
- [Orchestrator 模块详细文档](README.md)
- [System 模块数据结构文档](../system/DATA_STRUCTURES.md)
- [Meta 模块数据结构文档](../meta/DATA_STRUCTURES.md)
- [Transfer 模块数据结构文档](../transfer/DATA_STRUCTURES.md)
