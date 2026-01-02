# tasks 表结构和 API 说明

## 一、表结构概览

`transfer.tasks` 表是 Transfer 模块的传输任务定义表，支持导入、导出、同步三种任务类型。提供批处理、流式、微批次三种执行模式，支持定时调度和断点续传。

### 核心功能

- **多种任务类型**：import（导入）、export（导出）、sync（同步）
- **多种执行模式**：batch（批处理）、stream（流式）、micro-batch（微批次）
- **定时调度**：支持 Cron 表达式定时执行
- **断点续传**：支持任务中断后从断点恢复
- **并行控制**：支持配置最大并行度和批大小

---

## 二、表结构定义

### 2.1 核心字段

| 字段名 | 类型 | 约束 | 说明 |
|--------|------|------|------|
| `id` | SERIAL | PRIMARY KEY | 任务唯一标识 |
| `name` | VARCHAR(255) | NOT NULL | 任务名称 |
| `description` | TEXT | | 任务描述 |
| `type` | VARCHAR(50) | NOT NULL | 任务类型：import/export/sync |
| `mode` | VARCHAR(20) | DEFAULT 'batch' | 执行模式：batch/stream/micro-batch |
| `source_id` | INTEGER | INDEXED | 源数据源 ID（system.engines） |
| `target_id` | INTEGER | INDEXED | 目标数据源 ID |
| `config` | JSONB | NOT NULL | 任务配置 |
| `schedule` | VARCHAR(100) | | Cron 表达式 |
| `batch_size` | INTEGER | DEFAULT 1000 | 批大小 |
| `max_parallelism` | INTEGER | DEFAULT 1 | 最大并行度 |
| `retry_policy` | JSONB | | 重试策略 |
| `status` | VARCHAR(20) | DEFAULT 'pending', INDEXED | 任务状态 |
| `progress` | NUMERIC(5,2) | DEFAULT 0 | 进度（0-100） |
| `last_execution_id` | INTEGER | | 最后执行记录 ID |
| `last_execution_status` | VARCHAR(20) | | 最后执行状态 |
| `tenant_id` | INTEGER | NOT NULL, INDEXED | 租户 ID |
| `created_at` | TIMESTAMP | DEFAULT NOW() | 创建时间 |
| `updated_at` | TIMESTAMP | DEFAULT NOW() | 更新时间 |

### 2.2 JSON 字段结构

**config 字段（任务配置）**：
```json
{
  "source": {
    "type": "table",
    "schema": "public",
    "table": "source_cities"
  },
  "target": {
    "type": "table",
    "schema": "public",
    "table": "target_cities"
  },
  "filter": "population > 1000000",
  "incremental": true,
  "incremental_column": "updated_at"
}
```

**retry_policy 字段**：
```json
{
  "max_retries": 3,
  "retry_interval": 60,
  "backoff_factor": 2
}
```

---

## 三、API 端点说明

### 3.1 POST /api/tasks - 创建任务

**请求体**：
```json
{
  "name": "导入城市数据",
  "type": "import",
  "mode": "batch",
  "source_id": 1,
  "target_id": 2,
  "config": {...},
  "schedule": "0 2 * * *",
  "batch_size": 5000
}
```

### 3.2 GET /api/tasks - 查询任务列表

### 3.3 POST /api/tasks/:id/execute - 执行任务

### 3.4 POST /api/tasks/:id/pause - 暂停任务

### 3.5 POST /api/tasks/:id/resume - 恢复任务

---

## 四、相关文档

- [task_executions表](./task_executions表.md) - 执行记录表
- [data_mappings表](./data_mappings表.md) - 字段映射表
- [数据库架构](../数据库架构.md) - Transfer 模块架构
