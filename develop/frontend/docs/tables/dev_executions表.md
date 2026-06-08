# dev_executions 表废止说明

`develop.dev_executions` 不再作为 Develop 执行记录主表。

当前 Develop 的 query、workflow、script 执行记录统一写入 `common.task_executions`：

| 字段 | 取值 |
| --- | --- |
| `module` | `develop` |
| `task_type` | `query` / `workflow` / `script` |
| `source_task_id` | 对应 `develop.dev_tasks.id`，临时执行为空 |
| `source_task_name` | 对应开发任务名称 |
| `status` | `pending` / `running` / `success` / `failed` / `timeout` / `cancelled` |

Develop 任务定义仍由 `develop.dev_tasks` 保存，最近一次执行摘要回写到：

- `last_execution_id`
- `last_execution_status`
- `last_run_at`
- `next_run_at`

API 入口保持在 Develop 模块：

| API | 说明 |
| --- | --- |
| `GET /api/v1/develop/executions` | 查询 `common.task_executions` 中 `module=develop` 的执行记录 |
| `GET /api/v1/develop/executions/{id}` | 按 `execution_id` 查询执行详情 |
| `POST /api/v1/develop/task-definitions/{id}/execute` | 执行已有开发任务 |
| `POST /api/v1/develop/tasks/{task_type}/{id}/execute` | TaskProvider 标准执行入口 |

旧的 `develop.dev_executions` 表结构、索引和归档策略不再作为实现依据。
