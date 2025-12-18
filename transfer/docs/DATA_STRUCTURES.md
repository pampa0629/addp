# Transfer 模块数据结构和 API 文档

## 目录

- [1. 模块概述](#1-模块概述)
- [2. 数据库结构](#2-数据库结构)
- [3. API 端点清单](#3-api-端点清单)
- [4. 基础设施使用](#4-基础设施使用)
- [5. Worker 架构](#5-worker-架构)
- [6. 配置参数](#6-配置参数)

---

## 1. 模块概述

Transfer 模块负责数据导入、导出和同步，提供以下功能：

- **数据传输任务**：支持 import/export/sync 三种任务类型
- **字段映射**：灵活的源字段到目标字段映射
- **批处理**：支持 batch/stream/micro-batch 执行模式
- **断点续传**：checkpoint 机制支持任务中断恢复
- **重试机制**：可配置的重试策略
- **本地资源**：Transfer 模块私有的存储引擎配置
- **异步队列**：基于 Asynq 的任务队列（三级优先级）
- **高可用性**：Docker Swarm 模式支持多副本 Worker

### 端口配置

- **开发端口**: 8083
- **生产端口**: 8083
- **数据库 Schema**: `transfer`
- **依赖**: PostgreSQL, Redis (Asynq), MinIO, System 模块, Manager 模块, Meta 模块

### 模块依赖关系

```
System（资源配置）
  ↓
Manager、Meta（元数据、连接信息）
  ↓
Transfer（数据传输）
```

---

## 2. 数据库结构

### 2.1 PostgreSQL Schema: transfer

Transfer 模块使用 `transfer` schema，包含 5 张核心表。

#### 表 1: tasks - 传输任务表

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | BIGSERIAL | PRIMARY KEY | 任务唯一标识 |
| `name` | VARCHAR(255) | NOT NULL | 任务名称 |
| `description` | TEXT | | 任务描述 |
| `type` | VARCHAR(50) | NOT NULL | 任务类型：import/export/sync |
| `mode` | VARCHAR(20) | DEFAULT 'batch' | 执行模式：batch/stream/micro-batch |
| `source_id` | BIGINT | | 源数据源 ID (关联 system.resources) |
| `target_id` | BIGINT | | 目标数据源 ID (关联 system.resources) |
| `config` | JSONB | NOT NULL | 任务配置（JSON 格式） |
| `schedule` | VARCHAR(100) | | Cron 表达式（定时调度） |
| `batch_size` | INTEGER | DEFAULT 1000 | 批处理大小 |
| `max_parallelism` | INTEGER | DEFAULT 1 | 最大并行度 |
| `retry_policy` | JSONB | | 重试策略配置 |
| `status` | VARCHAR(20) | DEFAULT 'pending' | 任务状态 |
| `progress` | NUMERIC(5,2) | DEFAULT 0 | 进度百分比 (0-100) |
| `last_execution_id` | BIGINT | | 最后执行 ID |
| `last_execution_status` | VARCHAR(20) | | 最后执行状态 |
| `last_execution_started_at` | TIMESTAMP | | 最后执行开始时间 |
| `last_execution_finished_at` | TIMESTAMP | | 最后执行完成时间 |
| `created_by` | BIGINT | | 创建者 ID |
| `tenant_id` | BIGINT | NOT NULL | 租户 ID |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | 创建时间 |
| `updated_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | 更新时间 |

**索引**:
- `idx_tasks_status` - 状态索引
- `idx_tasks_type` - 类型索引
- `idx_tasks_tenant` - 租户索引
- `idx_tasks_source` - 源数据源索引
- `idx_tasks_target` - 目标数据源索引
- `idx_tasks_last_execution` - 最后执行索引

**任务状态枚举**:
- `pending` - 未执行/未启动
- `running` - 执行中
- `paused` - 暂停
- `stopped` - 已停止
- `scheduled` - 已启动（定时任务）
- `completed` - 已完成（手动任务）

**Go 模型** (`internal/models/task.go`):

```go
type Task struct {
    ID                       uint
    Name                     string
    Description              string
    Type                     string
    Mode                     string
    SourceID                 *uint
    TargetID                 *uint
    Config                   JSONMap
    Schedule                 string
    BatchSize                int
    MaxParallelism           int
    RetryPolicy              JSONMap
    Status                   string
    Progress                 float64
    LastExecutionID          *uint
    LastExecutionStatus      string
    LastExecutionStartedAt   *time.Time
    LastExecutionFinishedAt  *time.Time
    CreatedBy                uint
    TenantID                 uint
    CreatedAt                time.Time
    UpdatedAt                time.Time
}
```

---

#### 表 2: task_executions - 任务执行记录表

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | BIGSERIAL | PRIMARY KEY | 执行记录唯一标识 |
| `task_id` | BIGINT | NOT NULL, INDEXED | 关联任务 ID |
| `status` | TEXT | NOT NULL | 执行状态：pending/running/success/failed/cancelled |
| `start_time` | TIMESTAMP | NOT NULL | 开始时间 |
| `end_time` | TIMESTAMP | | 结束时间 |
| `records_read` | BIGINT | DEFAULT 0 | 读取记录数 |
| `records_written` | BIGINT | DEFAULT 0 | 写入记录数 |
| `bytes_read` | BIGINT | DEFAULT 0 | 读取字节数 |
| `bytes_written` | BIGINT | DEFAULT 0 | 写入字节数 |
| `error_msg` | TEXT | | 错误信息 |
| `logs` | TEXT | | 执行日志 |
| `checkpoint_offset` | BIGINT | DEFAULT 0 | 断点续传偏移量 |
| `checkpoint_state` | JSONB | | 断点状态 |
| `trigger_type` | VARCHAR(50) | | 触发类型：manual/schedule/api |
| `trigger_by` | BIGINT | | 触发者 ID |

**索引**:
- `idx_executions_task` - 任务索引
- `idx_executions_status` - 状态索引
- `idx_executions_start_time` - 开始时间索引

**执行状态枚举**:
- `pending` - 待执行
- `running` - 运行中
- `success` - 成功
- `failed` - 失败
- `cancelled` - 已取消

**Go 模型** (`internal/models/task.go`):

```go
type TaskExecution struct {
    ID               uint
    TaskID           uint
    Status           string
    StartTime        time.Time
    EndTime          *time.Time
    RecordsRead      int64
    RecordsWritten   int64
    BytesRead        int64
    BytesWritten     int64
    ErrorMsg         string
    Logs             string
    CheckpointOffset int64
    CheckpointState  JSONMap
    TriggerType      string
    TriggerBy        *uint
}
```

---

#### 表 3: data_mappings - 字段映射表

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | BIGSERIAL | PRIMARY KEY | 映射 ID |
| `task_id` | BIGINT | NOT NULL, INDEXED | 关联任务 ID |
| `source_field` | VARCHAR(255) | NOT NULL | 源字段名 |
| `target_field` | VARCHAR(255) | NOT NULL | 目标字段名 |
| `transform` | VARCHAR(500) | | 转换函数表达式 |
| `default_value` | TEXT | | 默认值 |
| `field_type` | VARCHAR(50) | | 字段类型 |
| `format` | VARCHAR(100) | | 格式（日期等） |
| `nullable` | BOOLEAN | DEFAULT true | 是否允许 NULL |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | 创建时间 |

**索引**:
- `idx_mappings_task` - 任务索引

**Go 模型** (`internal/models/mapping.go`):

```go
type DataMapping struct {
    ID           uint
    TaskID       uint
    SourceField  string
    TargetField  string
    Transform    string
    DefaultValue string
    FieldType    string
    Format       string
    Nullable     bool
    CreatedAt    time.Time
}
```

---

#### 表 4: local_resources - 本地存储引擎配置表

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | BIGSERIAL | PRIMARY KEY | 本地资源 ID |
| `tenant_id` | BIGINT | NOT NULL, INDEXED | 租户 ID |
| `name` | VARCHAR(255) | NOT NULL | 名称 |
| `resource_type` | VARCHAR(50) | NOT NULL | 资源类型 |
| `description` | TEXT | | 描述 |
| `is_active` | BOOLEAN | DEFAULT true | 是否激活 |
| `connection_info` | JSONB | NOT NULL | 连接信息（加密存储） |
| `created_by` | BIGINT | | 创建者 ID |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | 创建时间 |
| `updated_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | 更新时间 |

**索引**:
- `idx_local_resources_tenant` - 租户索引

**用途**: Transfer 模块私有的存储引擎配置，不共享到 System 模块

---

#### 表 5: checkpoints - 断点续传表

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | BIGSERIAL | PRIMARY KEY | 检查点 ID |
| `task_id` | BIGINT | NOT NULL | 任务 ID |
| `execution_id` | BIGINT | NOT NULL | 执行 ID |
| `offset` | BIGINT | NOT NULL | 偏移量 |
| `partition_id` | VARCHAR(255) | | 分区 ID |
| `state` | JSONB | | 状态信息 |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | 创建时间 |

**索引**:
- `idx_checkpoint_task_exec` - (task_id, execution_id) 组合索引
- `idx_checkpoint_partition` - 分区索引

---

### 2.2 数据表关系图

```
system.resources (来自 System 模块)
    ↓
transfer.tasks (传输任务)
    ↓ 1:N
transfer.task_executions (执行记录)
    ↓ 1:N
transfer.checkpoints (断点记录)

transfer.tasks
    ↓ 1:N
transfer.data_mappings (字段映射)

transfer.local_resources (本地资源)
    ↓ (独立表)
```

---

## 3. API 端点清单

### 3.1 任务管理 API

#### POST /api/tasks - 创建任务

**请求体**:

```json
{
  "name": "PostgreSQL 到 MinIO 导出",
  "description": "导出用户表到对象存储",
  "type": "export",
  "mode": "batch",
  "source_id": 1,
  "target_id": 2,
  "config": {
    "source_table": "public.users",
    "target_path": "/exports/users.csv"
  },
  "batch_size": 1000,
  "schedule": "0 2 * * *"
}
```

**响应** (201 Created): 返回 Task 对象

---

#### GET /api/tasks - 获取任务列表

**查询参数**:
- `page`: 页码（默认 1）
- `page_size`: 每页条数（默认 10）
- `status`: 按状态过滤
- `type`: 按类型过滤

**响应** (200 OK):

```json
{
  "tasks": [
    {
      "id": 1,
      "name": "PostgreSQL 到 MinIO 导出",
      "type": "export",
      "status": "scheduled",
      "progress": 0,
      "created_at": "2025-12-11T10:00:00Z"
    }
  ],
  "total": 1,
  "page": 1,
  "page_size": 10
}
```

---

#### GET /api/tasks/statistics - 获取任务统计信息

**响应** (200 OK):

```json
{
  "total": 10,
  "pending": 2,
  "running": 1,
  "scheduled": 5,
  "completed": 2,
  "success_rate": 0.95
}
```

---

#### GET /api/tasks/:id - 获取任务详情

**响应** (200 OK): 返回 Task 对象（包含完整配置）

---

#### PUT /api/tasks/:id - 更新任务

**请求体**: 同创建任务（可选字段）

**响应** (200 OK): 返回更新后的 Task 对象

---

#### DELETE /api/tasks/:id - 删除任务

**响应** (200 OK):

```json
{
  "message": "任务删除成功"
}
```

---

#### POST /api/tasks/:id/start - 启动任务

**响应** (202 Accepted):

```json
{
  "message": "任务已启动",
  "execution_id": 100
}
```

**说明**: 手动任务同步执行或入队，定时任务标记为 scheduled

---

#### POST /api/tasks/:id/stop - 停止任务

**响应** (200 OK):

```json
{
  "message": "任务已停止"
}
```

---

#### POST /api/tasks/:id/pause - 暂停任务

**响应** (200 OK):

```json
{
  "message": "任务已暂停"
}
```

---

#### POST /api/tasks/:id/resume - 恢复任务

**响应** (200 OK):

```json
{
  "message": "任务已恢复"
}
```

---

#### GET /api/tasks/:id/executions - 获取任务的执行记录列表

**查询参数**:
- `page`: 页码（默认 1）
- `page_size`: 每页条数（默认 10）

**响应** (200 OK): 返回执行记录列表

---

#### POST /api/tasks/:id/mappings - 创建字段映射

**请求体**:

```json
{
  "source_field": "id",
  "target_field": "user_id",
  "transform": "CAST(? AS INTEGER)",
  "field_type": "integer",
  "nullable": false
}
```

**响应** (201 Created): 返回 DataMapping 对象

---

#### GET /api/tasks/:id/mappings - 获取任务的字段映射列表

**响应** (200 OK):

```json
{
  "mappings": [
    {
      "id": 1,
      "source_field": "id",
      "target_field": "user_id",
      "transform": "CAST(? AS INTEGER)",
      "field_type": "integer"
    }
  ]
}
```

---

### 3.2 字段映射 API

#### DELETE /api/mappings/:id - 删除字段映射

**响应** (200 OK):

```json
{
  "message": "映射删除成功"
}
```

---

### 3.3 执行记录 API

#### GET /api/executions - 获取执行记录列表

**查询参数**:
- `task_id`: 按任务过滤
- `status`: 按状态过滤
- `page`: 页码（默认 1）
- `page_size`: 每页条数（默认 10）

**响应** (200 OK): 返回执行记录列表

---

#### GET /api/executions/statistics - 获取执行统计信息

**查询参数**:
- `task_id`: 按任务过滤
- `start_date`: 开始日期
- `end_date`: 结束日期

**响应** (200 OK):

```json
{
  "total_executions": 100,
  "success": 95,
  "failed": 5,
  "success_rate": 0.95,
  "avg_duration_ms": 5000,
  "total_records_processed": 1000000
}
```

---

#### GET /api/executions/:id - 获取执行记录详情

**响应** (200 OK): 返回 TaskExecution 对象

---

#### POST /api/executions/:id/cancel - 取消执行

**响应** (200 OK):

```json
{
  "message": "执行已取消"
}
```

---

#### POST /api/executions/:id/retry - 重试失败的执行

**响应** (202 Accepted):

```json
{
  "message": "重试已启动",
  "execution_id": 101
}
```

---

#### GET /api/executions/:id/progress - 获取执行进度

**响应** (200 OK):

```json
{
  "status": "running",
  "progress": 50.0,
  "records_read": 500,
  "records_written": 480,
  "estimated_remaining_time_ms": 5000
}
```

---

#### GET /api/executions/:id/logs - 获取执行日志

**查询参数**:
- `limit`: 日志行数（默认 100）

**响应** (200 OK):

```json
{
  "logs": "2025-12-11 10:00:00 INFO Task started\n2025-12-11 10:01:00 INFO Reading data..."
}
```

---

### 3.4 本地资源 API

#### GET /api/local-resources - 列出本地资源

**响应** (200 OK): 返回本地资源列表

---

#### POST /api/local-resources - 创建本地资源

**请求体**:

```json
{
  "name": "本地 PostgreSQL",
  "resource_type": "postgresql",
  "connection_info": {
    "host": "localhost",
    "port": "5432",
    "user": "postgres",
    "password": "secret",
    "database": "mydb"
  }
}
```

**响应** (201 Created): 返回本地资源对象

---

#### PUT /api/local-resources/:id - 更新本地资源

**请求体**: 同创建本地资源

**响应** (200 OK): 返回更新后的对象

---

#### DELETE /api/local-resources/:id - 删除本地资源

**响应** (200 OK):

```json
{
  "message": "本地资源删除成功"
}
```

---

#### POST /api/local-resources/test-connection - 创建前测试连接

**请求体**: 同创建本地资源

**响应** (200 OK):

```json
{
  "success": true,
  "message": "连接测试成功"
}
```

---

#### POST /api/local-resources/:id/test - 测试现有资源连接

**响应** (200 OK): 同上

---

#### POST /api/local-resources/:id/sync - 同步资源元数据

**响应** (202 Accepted):

```json
{
  "message": "元数据同步已启动"
}
```

---

#### GET /api/local-resources/:id/tables - 列出本地资源的表

**响应** (200 OK):

```json
{
  "tables": [
    {"schema": "public", "table": "users"},
    {"schema": "public", "table": "orders"}
  ]
}
```

---

#### GET /api/local-resources/:id/fields - 列出表的字段

**查询参数**:
- `schema`: Schema 名称（必填）
- `table`: 表名（必填）

**响应** (200 OK):

```json
{
  "fields": [
    {"name": "id", "type": "integer", "nullable": false},
    {"name": "name", "type": "varchar(255)", "nullable": true}
  ]
}
```

---

### 3.5 系统资源 API

#### GET /api/system-resources - 列出系统中注册的资源

**响应** (200 OK): 返回 System 模块的资源列表

---

### 3.6 对象存储 API

#### POST /api/object-storage/browse - 浏览对象存储目录

**请求体**:

```json
{
  "resource_id": 2,
  "scope": "system",
  "prefix": "/data/"
}
```

**响应** (200 OK):

```json
{
  "objects": [
    {
      "name": "file1.csv",
      "size": 1024,
      "last_modified": "2025-12-11T10:00:00Z"
    }
  ],
  "prefixes": ["/data/subfolder/"]
}
```

---

### 3.7 转换器 API

#### GET /api/transforms - 列出所有可用的转换器

**响应** (200 OK):

```json
{
  "transforms": [
    {"name": "UPPER", "description": "转换为大写"},
    {"name": "LOWER", "description": "转换为小写"},
    {"name": "TRIM", "description": "去除空格"}
  ]
}
```

---

#### GET /api/transforms/stats - 获取转换器统计信息

**响应** (200 OK):

```json
{
  "total_transforms": 10,
  "most_used": ["UPPER", "TRIM"]
}
```

---

#### GET /api/transforms/:name - 获取转换器能力描述

**响应** (200 OK):

```json
{
  "name": "UPPER",
  "description": "转换为大写",
  "parameters": [],
  "examples": ["UPPER('hello') -> 'HELLO'"]
}
```

---

#### POST /api/transforms/:name/validate - 验证转换器配置

**请求体**:

```json
{
  "config": {"input": "hello"}
}
```

**响应** (200 OK):

```json
{
  "valid": true
}
```

---

#### POST /api/transforms/:name/test - 测试转换器

**请求体**:

```json
{
  "sample_data": ["hello", "world"]
}
```

**响应** (200 OK):

```json
{
  "results": ["HELLO", "WORLD"]
}
```

---

## 4. 基础设施使用

### 4.1 Redis Asynq 队列

**队列命名规范**:

```
transfer:critical  高优先级任务队列
transfer:default   默认优先级任务队列
transfer:low       低优先级任务队列
```

**任务类型**:

```
TypeExecuteTask = "transfer:execute"
```

**任务载荷**:

```json
{
  "task_id": 1,
  "execution_id": 100,
  "tenant_id": 1
}
```

**Worker 并发配置**:

```go
Concurrency: 10  // 总并发数
Queues: {
    "transfer:critical": 6,  // 高优先级分配 6 个 worker
    "transfer:default":  3,  // 默认分配 3 个 worker
    "transfer:low":      1,  // 低优先级分配 1 个 worker
}
```

---

### 4.2 MinIO 存储

**Bucket**: `transfer` (系统 MinIO，端口 9000-9001)

**目录结构**:

```
transfer/
├── exports/     # 导出临时文件
├── imports/     # 导入临时文件
└── staging/     # 分段上传暂存
```

---

## 5. Worker 架构

### 5.1 Worker 启动流程

位置：`transfer/backend/cmd/worker/main.go`

1. 加载环境变量和配置
2. 连接 PostgreSQL 数据库
3. 初始化 Pipeline 连接器注册表
4. 创建日志记录器和执行引擎
5. 创建任务队列（连接 Redis）
6. 初始化 Service 层
7. 创建 Asynq Server 并配置并发参数
8. 注册任务处理器
9. 创建并启动定时调度器
10. 启动 Worker 并监听关闭信号

### 5.2 任务处理器

**HandleExecuteTask** (`internal/worker/handler.go`):

```go
func (h *TaskHandler) HandleExecuteTask(ctx context.Context, t *asynq.Task) error {
    // 1. 解析任务载荷
    var payload ExecuteTaskPayload
    json.Unmarshal(t.Payload(), &payload)

    // 2. 执行任务
    err := h.taskService.ExecuteTask(ctx, payload.TaskID, payload.ExecutionID)

    // 3. 返回结果
    return err
}
```

### 5.3 高可用性配置

**Docker Swarm 模式**:

```yaml
transfer-worker:
  deploy:
    replicas: 2              # 2 个副本
    restart_policy:
      condition: on-failure
      delay: 5s
      max_attempts: 3
    resources:
      limits:
        cpus: '2'
        memory: 2G
```

**特性**:
- ✅ 自动恢复：容器崩溃自动替换
- ✅ 负载均衡：任务分布到多个 worker
- ✅ 零停机更新：滚动更新

---

## 6. 配置参数

### 6.1 环境变量清单

| 变量名 | 默认值 | 说明 |
|--------|--------|------|
| `PORT` | 8083 | 服务端口 |
| `DB_HOST` | localhost | PostgreSQL 主机 |
| `DB_PORT` | 5432 | PostgreSQL 端口 |
| `DB_USER` | addp | 数据库用户 |
| `DB_PASSWORD` | addp_password | 数据库密码 |
| `DB_NAME` | addp | 数据库名 |
| `DB_SCHEMA` | transfer | Transfer schema 名 |
| `REDIS_HOST` | localhost | Redis 主机 |
| `REDIS_PORT` | 6379 | Redis 端口 |
| `REDIS_PASSWORD` | - | Redis 密码（可选） |
| `SYSTEM_SERVICE_URL` | http://localhost:8080 | System 服务 URL |
| `WORKER_COUNT` | 10 | Worker 数量 |
| `MAX_RETRIES` | 3 | 最大重试次数 |
| `RETRY_DELAY` | 5s | 重试延迟 |
| `CONCURRENT_TASKS` | 10 | 并发任务数 |

---

## 7. 关键文件路径

| 文件 | 路径 | 说明 |
|------|------|------|
| 路由配置 | `transfer/backend/internal/api/router.go` | 所有 API 端点定义 |
| 任务模型 | `transfer/backend/internal/models/task.go` | 任务和执行模型 |
| 任务服务 | `transfer/backend/internal/service/task_service.go` | 任务业务逻辑 |
| 执行服务 | `transfer/backend/internal/service/execution_service.go` | 执行管理 |
| Worker 主程序 | `transfer/backend/cmd/worker/main.go` | Worker 入口 |
| 任务处理器 | `transfer/backend/internal/worker/handler.go` | 任务处理 |
| 调度器 | `transfer/backend/internal/worker/scheduler.go` | 定时调度 |

---

## 8. 相关文档

- [ADDP 平台架构文档](../CLAUDE.md)
- [Transfer 模块详细文档](README.md)
- [System 模块数据结构文档](../system/DATA_STRUCTURES.md)
- [Manager 模块数据结构文档](../manager/DATA_STRUCTURES.md)
- [Meta 模块数据结构文档](../meta/DATA_STRUCTURES.md)
