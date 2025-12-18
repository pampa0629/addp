# Transfer 模块实现总结

## 📦 已完成的核心功能

### 1. 核心管道层（pkg/pipeline/）

#### ✅ 接口与数据结构 (`interfaces.go`)
- `Reader` - 统一数据读取接口
  - 支持三种模式：Batch（批处理）、Stream（流式）、MicroBatch（微批次）
  - `SeekTo()` 支持断点续传
- `Writer` - 统一数据写入接口
  - 批量写入 + 缓冲区管理
- `Transform` - 数据转换接口
- `DataBatch` - 批次数据结构
  - 包含行数据、schema、offset、timestamp、分区信息
- `ConnectorConfig` - 连接器配置

#### ✅ 执行引擎 (`engine.go`)
```go
ExecutionEngine
├── 微批处理循环
├── 自动 Checkpoint 保存（每 N 批次）
├── 流式/批处理统一处理
├── 上下文取消支持
└── 实时指标收集
```

**核心流程**：
```
1. 创建 Reader/Writer
2. 加载 Checkpoint（如有）
3. 进入循环：
   - Read() 读取批次
   - Transform() 数据转换
   - Write() 写入目标
   - 每 N 批次保存 Checkpoint
   - 收集指标
4. Flush() 刷新缓冲区
```

#### ✅ 状态管理器 (`state_manager.go`)
- Checkpoint 保存/加载
- 支持断点续传
- 自动清理旧 Checkpoint
- 数据库存储：`transfer.checkpoints` 表

#### ✅ 指标收集器 (`metrics.go`)
实时收集：
- RecordsRead/Written - 读取/写入记录数
- BytesRead/Written - 读取/写入字节数
- BatchCount - 批次数
- QPS - 每秒处理记录数
- Duration/AvgLatency - 时间指标

#### ✅ 连接器注册表 (`registry.go`)
- 工厂模式创建 Reader/Writer
- 支持动态注册连接器
- 线程安全实现

### 2. 数据转换层（pkg/pipeline/transform.go）

#### ✅ 内置转换器

**FieldMappingTransform** - 字段映射和类型转换
```go
mappings := []FieldMapping{
    {Source: "user_id", Target: "id", Type: "int"},
    {Source: "created_time", Target: "created_at", Type: "datetime", Format: "2006-01-02"},
}
```

**FilterTransform** - 行过滤
```go
conditions := []FilterCondition{
    {Field: "status", Operator: "eq", Value: "active"},
    {Field: "age", Operator: "gte", Value: 18},
}
transform := NewFilterTransform(conditions, "and") // 支持 and/or
```

**其他转换器**：
- `RenameFieldsTransform` - 字段重命名
- `SelectFieldsTransform` - 字段投影（只保留指定字段）

**支持的操作符**：
- 比较：`eq`, `ne`, `gt`, `lt`, `gte`, `lte`
- 字符串：`contains`

**支持的类型**：
- `string`, `int`, `float`, `bool`, `datetime`（支持自定义格式）

### 3. JDBC 连接器（internal/connector/）

#### ✅ JDBC Reader (`jdbc_reader.go`)

**支持数据库**：PostgreSQL, MySQL

**核心功能**：
- 自动 schema 推断
- 分页查询（LIMIT/OFFSET）
- 类型映射（数据库类型 → 统一类型）
- SeekTo 支持断点续传
- 支持自定义 SQL 或表名查询

**配置示例**：
```json
{
  "driver": "postgres",
  "host": "localhost",
  "port": 5432,
  "database": "testdb",
  "username": "user",
  "password": "pass",
  "table": "users",
  "where_clause": "created_at > '2024-01-01'",
  "order_by": "id"
}
```

#### ✅ JDBC Writer (`jdbc_writer.go`)

**支持数据库**：PostgreSQL, MySQL

**核心功能**：
- 三种写入模式：
  - `insert` - 普通插入
  - `upsert` - PostgreSQL ON CONFLICT 更新
  - `replace` - MySQL REPLACE 语法
- 批量插入（事务保护）
- 缓冲区自动刷新
- 动态 schema 检测

**配置示例**：
```json
{
  "driver": "postgres",
  "host": "localhost",
  "port": 5432,
  "database": "targetdb",
  "username": "user",
  "password": "pass",
  "table": "users",
  "write_mode": "upsert",
  "conflict_key": "id"
}
```

### 4. 数据模型（internal/models/task.go）

#### ✅ 核心模型

**Task** - 任务模型
```go
type Task struct {
    ID          uint
    Name        string
    Type        TaskType    // import, export, sync
    Mode        TaskMode    // batch, stream, micro-batch
    SourceID    *uint       // 关联 system.resources
    TargetID    *uint
    Config      JSONMap     // 任务配置
    Schedule    string      // Cron 表达式
    BatchSize   int
    Status      TaskStatus  // pending, running, success, failed, paused
    Progress    float64
    TenantID    uint
}
```

**TaskExecution** - 执行记录
```go
type TaskExecution struct {
    ID              uint
    TaskID          uint
    Status          ExecutionStatus
    StartTime       time.Time
    EndTime         *time.Time
    RecordsRead     int64
    RecordsWritten  int64
    BytesRead       int64
    BytesWritten    int64
    ErrorMsg        string
    CheckpointOffset int64
    TriggerType     string  // manual, schedule, api
}
```

**DataMapping** - 字段映射
```go
type DataMapping struct {
    ID           uint
    TaskID       uint
    SourceField  string
    TargetField  string
    FieldType    string
    Format       string
    DefaultValue string
}
```

### 5. 数据访问层（internal/repository/）

#### ✅ TaskRepository
- 任务 CRUD 操作
- 按状态查询任务
- 分页查询
- 任务统计（各状态任务数、总执行数、总记录数）

#### ✅ ExecutionRepository
- 执行记录 CRUD
- 按任务ID查询执行记录
- 更新执行指标
- 完成执行（设置结束时间和状态）

#### ✅ MappingRepository
- 批量创建/更新字段映射
- 按任务ID查询映射

### 6. 业务服务层（internal/service/）

#### ✅ TaskService (`task_service.go`)

**核心方法**：
```go
// 任务管理
CreateTask()       // 创建任务
GetTask()          // 获取任务
UpdateTask()       // 更新任务
DeleteTask()       // 删除任务
ListTasks()        // 列出任务（分页 + 过滤）
GetTaskStatistics() // 获取统计信息

// 任务控制
StartTask()        // 启动任务
StopTask()         // 停止任务
PauseTask()        // 暂停任务
ResumeTask()       // 恢复任务

// 任务执行（由 Worker 调用）
ExecuteTask()      // 执行任务（调用 ExecutionEngine）
```

**关键功能**：
- 构建 ExecutionTask（解析配置、创建转换器）
- 推断连接器类型（jdbc, s3, file, kafka）
- 执行任务并收集指标
- 自动更新任务状态和进度

#### ✅ ExecutionService (`execution_service.go`)

**核心方法**：
```go
GetExecution()           // 获取执行记录
ListExecutions()         // 列出任务的执行记录
GetLatestExecution()     // 获取最新执行
GetRunningExecutions()   // 获取运行中的执行
RetryExecution()         // 重试失败的执行
CancelExecution()        // 取消运行中的执行
GetExecutionProgress()   // 获取执行进度（实时）
GetExecutionLogs()       // 获取执行日志
```

## 📊 数据库 Schema

### transfer.tasks
```sql
CREATE TABLE transfer.tasks (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    type VARCHAR(50) NOT NULL,           -- import, export, sync
    mode VARCHAR(20) DEFAULT 'batch',    -- batch, stream, micro-batch
    source_id INTEGER,
    target_id INTEGER,
    config JSONB NOT NULL,
    schedule VARCHAR(100),               -- Cron 表达式
    batch_size INT DEFAULT 1000,
    max_parallelism INT DEFAULT 1,
    retry_policy JSONB,
    status VARCHAR(20) DEFAULT 'pending',
    progress NUMERIC(5,2) DEFAULT 0,
    last_execution_id INTEGER,
    created_by INTEGER,
    tenant_id BIGINT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

### transfer.task_executions
```sql
CREATE TABLE transfer.task_executions (
    id SERIAL PRIMARY KEY,
    task_id INTEGER NOT NULL,
    status VARCHAR(20) NOT NULL,
    start_time TIMESTAMP NOT NULL,
    end_time TIMESTAMP,
    records_read BIGINT DEFAULT 0,
    records_written BIGINT DEFAULT 0,
    bytes_read BIGINT DEFAULT 0,
    bytes_written BIGINT DEFAULT 0,
    error_msg TEXT,
    logs TEXT,
    checkpoint_offset BIGINT DEFAULT 0,
    checkpoint_state JSONB,
    trigger_type VARCHAR(50),
    trigger_by INTEGER
);
```

### transfer.checkpoints
```sql
CREATE TABLE transfer.checkpoints (
    id SERIAL PRIMARY KEY,
    task_id INTEGER NOT NULL,
    execution_id INTEGER NOT NULL,
    offset_value BIGINT NOT NULL,
    partition_id VARCHAR(255),
    state JSONB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_checkpoint_task_exec (task_id, execution_id)
);
```

## 🎯 使用示例

### 示例1：数据库到数据库传输

```go
// 1. 创建任务
task, _ := taskService.CreateTask(ctx, &models.CreateTaskRequest{
    Name: "Sync users from MySQL to PostgreSQL",
    Type: models.TaskTypeSync,
    Mode: models.TaskModeBatch,
    BatchSize: 1000,
    Config: map[string]interface{}{
        "source": map[string]interface{}{
            "driver":   "mysql",
            "host":     "mysql-host",
            "port":     3306,
            "database": "source_db",
            "username": "user",
            "password": "pass",
            "table":    "users",
            "order_by": "id",
        },
        "target": map[string]interface{}{
            "driver":      "postgres",
            "host":        "pg-host",
            "port":        5432,
            "database":    "target_db",
            "username":    "user",
            "password":    "pass",
            "table":       "users",
            "write_mode":  "upsert",
            "conflict_key": "id",
        },
    },
    Mappings: []models.DataMapping{
        {SourceField: "user_id", TargetField: "id", FieldType: "int"},
        {SourceField: "user_name", TargetField: "name", FieldType: "string"},
    },
}, tenantID, userID)

// 2. 启动任务
execution, _ := taskService.StartTask(ctx, task.ID, tenantID, userID)

// 3. 查询执行进度
progress, _ := executionService.GetExecutionProgress(ctx, execution.ID, tenantID)
fmt.Printf("Records written: %d, QPS: %.2f\n",
    progress["records_written"], progress["qps"])
```

### 示例2：带数据转换和过滤

```go
Config: map[string]interface{}{
    "source": {...},
    "target": {...},
    "transforms": []interface{}{
        map[string]interface{}{
            "type": "filter",
            "conditions": []interface{}{
                map[string]interface{}{
                    "field":    "status",
                    "operator": "eq",
                    "value":    "active",
                },
                map[string]interface{}{
                    "field":    "age",
                    "operator": "gte",
                    "value":    18,
                },
            },
            "mode": "and",
        },
    },
}
```

### 示例3：断点续传

```go
// 任务中断后重新执行，自动从 Checkpoint 恢复
execution, _ := executionService.RetryExecution(ctx, failedExecutionID, tenantID, userID)

// ExecutionEngine 内部逻辑：
// 1. 加载最新 Checkpoint
// 2. 调用 reader.SeekTo(checkpoint.Offset)
// 3. 从中断位置继续处理
```

## 📁 目录结构

```
transfer/backend/
├── pkg/pipeline/              # ✅ 核心管道层
│   ├── interfaces.go          # Reader/Writer/Transform 接口
│   ├── engine.go              # 执行引擎
│   ├── state_manager.go       # Checkpoint 管理
│   ├── metrics.go             # 指标收集
│   ├── registry.go            # 连接器注册
│   └── transform.go           # 数据转换
│
├── internal/
│   ├── models/                # ✅ 数据模型
│   │   └── task.go
│   │
│   ├── connector/             # ✅ 连接器实现
│   │   ├── jdbc_reader.go     # JDBC 读取器
│   │   ├── jdbc_writer.go     # JDBC 写入器
│   │   ├── file_reader.go     # 文件读取器（CSV/JSON/JSONL）
│   │   ├── file_writer.go     # 文件写入器（CSV/JSON/JSONL）
│   │   ├── s3_reader.go       # S3/MinIO 读取器
│   │   ├── s3_writer.go       # S3/MinIO 写入器
│   │   ├── registry.go        # 连接器注册器
│   │   └── utils.go
│   │
│   ├── repository/            # ✅ 数据访问层
│   │   └── task_repository.go
│   │
│   ├── service/               # ✅ 业务服务层
│   │   ├── task_service.go
│   │   └── execution_service.go
│   │
│   ├── api/                   # ✅ HTTP API
│   │   ├── task_handler.go     # 任务管理 Handler
│   │   ├── execution_handler.go # 执行记录 Handler
│   │   └── router.go           # 路由配置
│   │
│   ├── middleware/            # ✅ 中间件
│   │   ├── auth.go             # JWT 认证
│   │   ├── cors.go             # 跨域处理
│   │   ├── logger.go           # 日志记录
│   │   └── recovery.go         # 错误恢复
│   │
│   ├── config/                # ✅ 配置管理
│   │   └── config.go           # 配置加载
│   │
│   └── worker/                # ✅ Worker 进程
│       ├── queue.go            # 任务队列管理器
│       ├── handler.go          # 任务处理器
│       └── scheduler.go        # 定时调度器
│
├── cmd/
│   ├── server/                # ✅ API 服务
│   │   └── main.go             # 服务入口
│   └── worker/                # ✅ Worker 进程
│       └── main.go             # Worker 入口
│
├── frontend/                  # ✅ 前端项目
│   ├── src/
│   │   ├── views/             # 页面视图
│   │   ├── api/               # API 接口
│   │   ├── router/            # 路由配置
│   │   └── main.js
│   ├── Dockerfile             # Docker 配置
│   ├── nginx.conf             # Nginx 配置
│   └── README.md              # 前端文档
│
├── DESIGN.md                  # ✅ 详细设计文档
├── WORKER.md                  # ✅ Worker 使用文档
├── 快速开始.md                 # ✅ 快速上手指南
├── 连接器使用指南.md           # ✅ 连接器使用文档
└── README_IMPLEMENTATION.md   # ✅ 本文档
```

## 🚀 下一步工作

### Phase 4: API 层（✅ 已完成）
- [x] HTTP Handler（Gin）
  - [x] TaskHandler - 任务管理（创建、查询、更新、删除、启动、停止、暂停、恢复）
  - [x] ExecutionHandler - 执行记录管理（查询、取消、重试、进度、日志）
- [x] 路由配置（router.go）
- [x] 请求验证（Gin 绑定）
- [x] JWT 认证中间件（Auth middleware）
- [x] CORS 中间件
- [x] Logger 中间件
- [x] Recovery 中间件
- [x] 服务入口（cmd/server/main.go）

**API 端点**：
```
公开接口：
  GET  /health                              # 健康检查
  GET  /api/ping                            # Ping

受保护接口（需要 JWT）：
  # 任务管理
  POST   /api/tasks                         # 创建任务
  GET    /api/tasks                         # 获取任务列表
  GET    /api/tasks/statistics              # 获取任务统计
  GET    /api/tasks/:id                     # 获取任务详情
  PUT    /api/tasks/:id                     # 更新任务
  DELETE /api/tasks/:id                     # 删除任务
  POST   /api/tasks/:id/start               # 启动任务
  POST   /api/tasks/:id/stop                # 停止任务
  POST   /api/tasks/:id/pause               # 暂停任务
  POST   /api/tasks/:id/resume              # 恢复任务
  GET    /api/tasks/:id/executions          # 获取任务的执行记录
  POST   /api/tasks/:id/mappings            # 创建字段映射
  GET    /api/tasks/:id/mappings            # 获取任务的字段映射

  # 字段映射
  DELETE /api/mappings/:id                  # 删除字段映射

  # 执行记录
  GET    /api/executions                    # 获取执行记录列表
  GET    /api/executions/statistics         # 获取执行统计
  GET    /api/executions/:id                # 获取执行详情
  POST   /api/executions/:id/cancel         # 取消执行
  POST   /api/executions/:id/retry          # 重试执行
  GET    /api/executions/:id/progress       # 获取执行进度
  GET    /api/executions/:id/logs           # 获取执行日志
```

### Phase 5: Worker & 任务队列（✅ 已完成）
- [x] Asynq 集成
  - [x] TaskQueue - 任务队列管理器（入队、查询、取消）
  - [x] 支持三级优先级队列（critical, default, low）
  - [x] 自动重试机制（可配置次数和延迟）
- [x] Worker 进程实现
  - [x] TaskHandler - 任务处理器（解析载荷、执行任务、记录日志）
  - [x] 并发任务执行（可配置并发数）
  - [x] 优雅关闭机制（SIGINT/SIGTERM）
- [x] 任务分发与执行
  - [x] Redis 队列分发
  - [x] 任务载荷序列化/反序列化
  - [x] 执行状态实时更新
- [x] 定时调度（Cron）
  - [x] Scheduler - 定时调度器（支持秒级精度）
  - [x] 启动时自动加载所有定时任务
  - [x] 动态添加/移除定时任务
  - [x] 支持标准 Cron 表达式
- [x] Worker 主程序（cmd/worker/main.go）

**Worker 架构**:
```
┌─────────────┐         ┌─────────────┐
│ API Server  │────────▶│   Redis     │
│ (创建任务)   │         │  (队列)      │
└─────────────┘         └──────┬──────┘
                               │
                               ▼
                        ┌─────────────┐
                        │   Worker    │
                        │  ┌────────┐ │
      ┌─────────────────┤  │Handler │ │
      │                 │  └────┬───┘ │
      │                 └───────┼─────┘
      │                         │
      ▼                         ▼
┌─────────────┐         ┌─────────────┐
│ Scheduler   │         │   Engine    │
│ (Cron定时)   │────────▶│  (Pipeline) │
└─────────────┘         └─────────────┘
```

**使用示例**:
```bash
# 启动 Worker
cd transfer/backend
go run cmd/worker/main.go

# 配置环境变量
export CONCURRENT_TASKS=10    # 并发任务数
export MAX_RETRIES=3          # 最大重试次数
export RETRY_DELAY=30s        # 重试延迟
```

详细文档见 [WORKER.md](./WORKER.md)

### Phase 6: 更多连接器（✅ 已完成）
- [x] File Reader/Writer（CSV, JSON, JSONL）
  - [x] FileReader - 读取本地文件（支持 CSV、JSON、JSONL 格式）
  - [x] FileWriter - 写入本地文件（支持 CSV、JSON、JSONL 格式）
  - [x] 自动类型推断（CSV）
  - [x] Schema 推断
  - [x] 自定义分隔符（CSV）
- [x] S3 Reader/Writer（MinIO 兼容）
  - [x] S3Reader - 读取 S3/MinIO 对象存储
  - [x] S3Writer - 写入 S3/MinIO 对象存储
  - [x] 批量对象处理
  - [x] 文件模式匹配（`*.json`, `*.csv`）
  - [x] 支持多种文件格式（JSON、JSONL、CSV）
- [x] 连接器注册系统
  - [x] RegisterAllConnectors() - 统一注册所有连接器
  - [x] 支持连接器类型别名（`mysql`, `postgresql`, `postgres`, `jdbc` 等）
- [ ] Kafka Reader/Writer（流式）- 待实现

**已支持的连接器**:
- **数据库**: `mysql`, `postgresql`, `postgres`, `jdbc`
- **文件**: `csv`, `json`, `jsonl`, `file`
- **对象存储**: `s3`, `minio`

**使用示例**:
```json
{
  "source": {
    "connector_type": "csv",
    "file_path": "/data/users.csv",
    "delimiter": ","
  },
  "target": {
    "connector_type": "s3",
    "endpoint": "http://minio:9000",
    "bucket": "data-lake",
    "prefix": "export/",
    "file_name": "users.json",
    "file_type": "json"
  }
}
```

详细文档见 [连接器使用指南.md](./连接器使用指南.md)

### Phase 7: 前端（✅ 已完成）
- [x] Vue 3 + Element Plus 项目框架
  - [x] Vite 构建配置
  - [x] Vue Router 路由
  - [x] Axios API 客户端
  - [x] Element Plus UI 组件库
- [x] 任务管理界面
  - [x] TaskList.vue - 任务列表（搜索、过滤、统计）
  - [x] TaskForm.vue - 任务创建/编辑表单
  - [x] TaskDetail.vue - 任务详情和执行历史
- [x] 执行监控界面
  - [x] ExecutionList.vue - 执行记录列表
  - [x] ExecutionDetail.vue - 执行详情和日志查看
  - [x] Dashboard.vue - 监控面板和统计
- [x] Docker 部署配置
  - [x] Dockerfile（多阶段构建）
  - [x] nginx.conf（反向代理配置）
  - [x] 生产环境优化

**前端功能**:
- 实时数据刷新（每5秒）
- 任务启动/停止/删除操作
- 执行进度可视化
- 执行日志查看
- 统计数据展示
- 响应式布局

**访问地址**:
- 开发环境: http://localhost:5176
- 生产环境: http://localhost:8093/transfer/
- Portal 集成: 通过 iframe 加载

详细文档见 [frontend/README.md](./frontend/README.md)

## 🎨 核心设计亮点

### 1. 流批一体化
- 统一接口处理批处理和流式数据
- 微批处理模式平衡延迟和吞吐量
- Reader.Mode() 区分不同处理模式

### 2. 断点续传
- Checkpoint 机制记录偏移量
- Reader.SeekTo() 跳转到中断位置
- 支持任务中断后恢复

### 3. 插件化架构
- ConnectorRegistry 工厂模式
- 易于扩展新的连接器类型
- Transform Pipeline 支持自定义转换

### 4. 可观测性
- 实时指标收集（QPS、延迟、吞吐量）
- 执行记录完整追踪
- Checkpoint 历史查询

### 5. 多租户隔离
- TenantID 字段隔离数据
- 权限检查在 Service 层
- 统计信息按租户聚合

## 📖 参考资料

### 相关文档
- [DESIGN.md](./DESIGN.md) - 完整架构设计文档
- [WORKER.md](./WORKER.md) - Worker 使用文档（Asynq + Cron）
- [快速开始.md](./快速开始.md) - 快速上手指南
- [连接器使用指南.md](./连接器使用指南.md) - 连接器配置和使用
- [frontend/README.md](./frontend/README.md) - 前端开发文档
- [scripts/init-db.sql](../../scripts/init-db.sql) - 数据库 Schema

### 相关模块
- **System 模块**：资源管理（`system.resources`）、用户认证
- **Meta 模块**：元数据扫描器（可复用为 Reader）
- **Manager 模块**：S3 客户端（可复用）

### 设计模式
- **管道模式**（Pipeline）：Reader → Transform → Writer
- **策略模式**（Strategy）：不同连接器实现相同接口
- **工厂模式**（Factory）：ConnectorRegistry
- **观察者模式**（Observer）：MetricsCollector

---

**版本**：v1.0.0
**最后更新**：2025-10-21
**状态**：✅ 全部完成 - 核心框架、业务层、API、Worker、连接器、前端全部实现
