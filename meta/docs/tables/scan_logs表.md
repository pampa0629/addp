# scan_logs 表结构和 API 说明

## 表结构概览

`metadata.scan_logs` 表记录扫描任务的日志信息，用于调试和审计。

### 核心字段

| 字段名 | 类型 | 说明 |
|--------|------|------|
| `id` | SERIAL | PRIMARY KEY |
| `task_run_id` | INTEGER | 任务运行 ID |
| `log_level` | VARCHAR | 日志级别：INFO/WARN/ERROR |
| `message` | TEXT | 日志消息 |
| `context` | JSONB | 上下文信息 |
| `created_at` | TIMESTAMP | 创建时间 |

## 相关文档

- [scan_task_runs表](./scan_task_runs表.md) - 任务运行记录表
- [数据库架构](../数据库架构.md) - Meta 模块架构
