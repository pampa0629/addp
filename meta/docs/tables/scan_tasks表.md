# scan_tasks 表结构和 API 说明

本篇维护任务定义字段及其与执行事实的关系。逻辑关系见[数据库架构](../数据库架构.md)，API 与用户／TaskProvider 的职责见[模块入口](../../CLAUDE.md#核心-api)，任务、执行与来源语义沿[扫描机制规范](../../../docs/spec/addp元数据扫描机制规范.md#scantask-与-execution-边界)。

## 表结构概览

`meta.scan_tasks` 表是 Meta 扫描任务定义表，只保存可复用任务的定义态信息，例如范围、参数、调度和归属。每次实际执行的运行态记录统一写入 `common.task_executions`，不再使用 Meta 私有运行历史表。

### 核心字段

以下为[模型声明](../../backend/internal/models/scan_task.go)与字段语义摘要；实际数据库类型、索引和约束须核对已执行迁移及目标数据库，不能由说明表推定。

| 字段名 | 类型 | 说明 |
|--------|------|------|
| `id` | SERIAL | PRIMARY KEY |
| `tenant_id` | INTEGER | 租户 ID |
| `engine_id` | INTEGER | 引擎 ID |
| `name` | VARCHAR | 任务名称 |
| `description` | VARCHAR | 任务说明 |
| `schedule` | VARCHAR | Cron 表达式，空值表示无定时计划 |
| `enabled` | BOOLEAN | 定时启用意图；调度器同时要求非空 schedule 和到期 next_run_at |
| `scope` | JSONB | 结构化扫描范围，例如 engine / catalog path / ref group |
| `parameters` | JSONB | scan_depth、force 等扫描参数，不承载范围；两者不另建独立物理列 |
| `owner_module` | VARCHAR | 任务绑定对象所属模块，例如 `meta`、`system` |
| `owner_ref` | VARCHAR | 绑定对象在所属模块内的幂等引用，例如 `engine:{engine_id}` |
| `last_run_at` | TIMESTAMP | 任务运行摘要时间，具体写入边界见下文 |
| `next_run_at` | TIMESTAMP | 下一次计划触发时间，不是执行完成时间 |
| `last_execution_id` | VARCHAR | 最近回写的 `common.task_executions.execution_id` 摘要 |
| `last_execution_status` | VARCHAR | 最近回写的执行状态摘要，不替代完整历史 |
| `created_by` | INTEGER | 创建用户 |
| `updated_by` | INTEGER | 更新用户 |
| `created_at` / `updated_at` / `deleted_at` | TIMESTAMP | 定义创建、修改与 GORM 软删除字段 |

`owner_module`／`owner_ref` 表达定义绑定的外部对象，不是 execution 的触发来源 `source`，也不表示把任务所有权移交该模块；二者沿上述正式规范区分。

当前定时入队会将触发时间写入 `last_run_at`，终态回写又使用完成时间，详见[到期调度](../../backend/internal/service/scan_task_scheduler_due.go)与[摘要字段生成](../../backend/internal/scantask/task_execution.go)。因此它不是统一的开始／完成时间或耗时统计基准；应读取对应 execution 的 `created_at`、`started_at`、`completed_at`。本文如实记录现行写入，不修改实现或重算历史。

## 执行记录

扫描执行记录统一存储在 `common.task_executions`：

- `module = meta`
- `task_type = scan`
- `source_task_id` 以十进制文本软引用 `scan_tasks.id`（定时任务或任务定义触发时）；一次性执行可以没有该引用
- `trigger_type = manual` 或 `scheduled`
- `execution_config` 保存本次执行的 engine、scope、depth、force、planned_run_at 等私有运行参数，不直接返回给用户或 Monitor

具体执行与源读取仍须通过[采集资格和范围核验](../../../docs/spec/addp元数据扫描机制规范.md#采集资格与两种范围语义)；保存定义、存在 execution 或启用调度都不代表已批准、已读取源或已执行成功。专业历史与安全 DTO 沿[Meta 读取契约](../../../docs/spec/tasks/执行监控.md#meta-专业执行读取与响应)，定义删除不抹去 Common 已有执行事实。

验证入口与分层范围见[模块开发与验证](../../CLAUDE.md#开发与验证)，固定版本、失败与未覆盖项见[历史证据](../../../docs/spec/testing/evidence/资源读取与预览历史证据.md#meta-专业扫描读取--t4)；源码字段或迁移文件存在不等于当前数据库已执行、真实 Worker 链路已验收。

## 相关文档

- [数据库架构](../数据库架构.md) - Meta 模块架构
