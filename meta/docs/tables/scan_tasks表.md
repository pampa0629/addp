# scan_tasks 表结构和 API 说明

## 表结构概览

`meta.scan_tasks` 表是 Meta 扫描任务定义表，只保存可复用任务的定义态信息，例如范围、参数、调度和归属。每次实际执行的运行态记录统一写入 `common.task_executions`，不再使用 Meta 私有运行历史表。

### 核心字段

| 字段名 | 类型 | 说明 |
|--------|------|------|
| `id` | SERIAL | PRIMARY KEY |
| `tenant_id` | INTEGER | 租户 ID |
| `engine_id` | INTEGER | 引擎 ID |
| `name` | VARCHAR | 任务名称 |
| `schedule` | VARCHAR | Cron 表达式 |
| `enabled` | BOOLEAN | 是否启用 |
| `scope` | JSONB | 结构化扫描范围，例如 engine / catalog path / ref group |
| `parameters` | JSONB | 扫描参数，不承载范围 |
| `owner_module` | VARCHAR | 任务绑定对象所属模块，例如 `meta`、`system` |
| `owner_ref` | VARCHAR | 绑定对象在所属模块内的幂等引用，例如 `engine:{engine_id}` |
| `last_run_at` | TIMESTAMP | 最后运行时间 |
| `next_run_at` | TIMESTAMP | 下次运行时间 |
| `last_execution_id` | VARCHAR | 最近一次执行的 `common.task_executions.execution_id` |
| `last_execution_status` | VARCHAR | 最近一次执行状态 |
| `created_by` | INTEGER | 创建用户 |
| `updated_by` | INTEGER | 更新用户 |

## 执行记录

扫描执行记录统一存储在 `common.task_executions`：

- `module = meta`
- `task_type = scan`
- `source_task_id = scan_tasks.id`（定时任务或任务定义触发时）
- `trigger_type = manual` 或 `scheduled`
- `execution_config` 保存本次执行的 engine、scope、depth、force、planned_run_at 等运行参数

## 相关文档

- [数据库架构](../数据库架构.md) - Meta 模块架构
