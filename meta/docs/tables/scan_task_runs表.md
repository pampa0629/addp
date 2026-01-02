# scan_task_runs 表结构和 API 说明

## 表结构概览

`metadata.scan_task_runs` 表记录扫描任务的每次运行历史，包括状态、进度、结果摘要。

### 核心字段

| 字段名 | 类型 | 说明 |
|--------|------|------|
| `id` | SERIAL | PRIMARY KEY |
| `task_id` | INTEGER | 任务 ID |
| `tenant_id` | INTEGER | 租户 ID |
| `engine_id` | INTEGER | 引擎 ID |
| `trigger_type` | VARCHAR | 触发方式：manual/schedule |
| `status` | VARCHAR | 状态：pending/running/completed/failed |
| `progress_percent` | FLOAT | 进度（0-100） |
| `result_summary` | JSONB | 结果摘要 |
| `started_at` | TIMESTAMP | 开始时间 |
| `completed_at` | TIMESTAMP | 完成时间 |

## 相关文档

- [scan_tasks表](./scan_tasks表.md) - 扫描任务表
- [数据库架构](../数据库架构.md) - Meta 模块架构
