# scan_tasks 表结构和 API 说明

## 表结构概览

`metadata.scan_tasks` 表是扫描任务配置表，定义定时或手动元数据扫描任务。

### 核心字段

| 字段名 | 类型 | 说明 |
|--------|------|------|
| `id` | SERIAL | PRIMARY KEY |
| `tenant_id` | INTEGER | 租户 ID |
| `engine_id` | INTEGER | 引擎 ID |
| `name` | VARCHAR | 任务名称 |
| `schedule_type` | VARCHAR | 调度类型：manual/cron/interval |
| `schedule` | VARCHAR | Cron 表达式 |
| `enabled` | BOOLEAN | 是否启用 |
| `parameters` | JSONB | 扫描参数 |
| `last_run_at` | TIMESTAMP | 最后运行时间 |
| `next_run_at` | TIMESTAMP | 下次运行时间 |

## 相关文档

- [scan_task_runs表](./scan_task_runs表.md) - 任务运行记录表
- [数据库架构](../数据库架构.md) - Meta 模块架构
