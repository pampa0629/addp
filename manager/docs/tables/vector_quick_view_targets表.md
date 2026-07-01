# vector_quick_view_target_generation 表结构说明

> 状态：当前实现说明。`manager.vector_quick_view_targets` 表达 Manager 创建并拥有生命周期的 3857 快显性能优化结果状态。

## 一、表定位

`manager.vector_quick_view_targets` 回答：

> 当前 spatial item 是否存在可复用的 3857 快显优化目标，该目标在哪里、是否可用、由哪次任务执行生成或刷新。

它不替代：

1. `manager.preview_state` 的用户预览模式偏好。
2. `manager.vector_quick_view_target_tasks` 的任务定义。
3. `manager.vector_tile_cache` 的瓦片缓存结果。
4. `common.task_executions` 的执行历史。

自动识别的外部 3857 物化视图不写入该表，不进入结果列表，也不获得 Manager 的删除、刷新或 stale 生命周期。

## 二、目标核心字段

| 字段名 | 当前类型 / 语义 | 说明 |
| --- | --- | --- |
| `id` | bigint | 结果 ID |
| `tenant_id` | integer | 租户 ID |
| `item_fingerprint` | varchar(64) | 源 item 指纹 |
| `item_id` | integer | 当前 Meta item 行引用，仅用于回查 |
| `locator` | text | ResourceLocator，用于回跳和定位 |
| `task_id` | bigint | 产生或最近刷新该结果的 `manager.vector_quick_view_target_tasks.id` |
| `last_execution_id` | varchar | 最近一次优化 execution |
| `source_engine_id` | integer | 源 PG 引擎 ID |
| `source_schema` / `source_table` / `source_geometry_column` | varchar | 源空间表和几何列 |
| `source_srid` / `target_srid` | integer | 源 SRID 和目标 SRID；当前目标 SRID 固定为 3857 |
| `target_kind` | varchar | 目标形态；当前为 `source_schema_materialized_view` |
| `target_schema` / `target_table` / `target_geometry_column` | varchar | 优化目标位置 |
| `status` | varchar | 快显性能优化结果 artifact state |
| `render_extent` / `render_extent_srid` | jsonb / integer | 可渲染范围；当前保存 WGS84 范围 |
| `row_count_estimate` | bigint | 优化目标估算行数 |
| `source_fingerprint_snapshot` | jsonb | 源事实快照，用于失效判断 |
| `metadata` | jsonb | 属性列、索引名、诊断摘要和构建选项 |
| `error_message` | text | 最近错误摘要 |
| `created_by` / `created_at` / `updated_at` / `deleted_at` | timestamp | 生命周期字段 |

## 三、状态语义

| 状态 | 含义 |
| --- | --- |
| `building` | 优化结果正在生成或刷新 |
| `ready` | 优化结果可用 |
| `stale` | 源事实变化或 Manager 自建目标校验失败，结果需要刷新 |
| `failed` | 最近生成失败，当前结果不可用或不完整 |
| `deleted` | 结果已清理，仅保留审计或摘要 |

这些状态属于 artifact state，不属于统一 execution status。

## 四、索引建议

| 索引名 | 字段 | 说明 |
| --- | --- | --- |
| `idx_qvo_tenant_item_fingerprint` | `tenant_id, item_fingerprint` | 查询某 item 的优化结果 |
| `idx_qvo_tenant_item` | `tenant_id, item_id` | 按当前 Meta item 行引用辅助回查 |
| `idx_qvo_task` | `task_id` | 查询某任务产生的优化结果 |
| `idx_qvo_execution` | `last_execution_id` | execution 回溯 |
| `idx_qvo_status` | `status` | 按结果状态过滤 |
| `idx_qvo_current_target_unique` | `tenant_id, item_fingerprint, source_geometry_column, target_srid` | 同一 item、几何列和目标 SRID 只允许一个当前目标 |

## 五、删除语义

删除快显性能优化结果时，只删除 Manager 创建并登记的 3857 目标及其索引、结果记录和相关运行时缓存。

不得删除：

1. 源业务表。
2. 源表原有索引。
3. 用户或 DBA 创建的外部物化视图。
4. 已生成的 MVT 瓦片缓存。
5. Meta 源 facts。

如果物理清理失败，不得把结果伪装为已清理；应保留记录并写入错误摘要。

## 六、相关文档

- [快显概念说明](../快显概念说明.md)
- [快显实现规范](../快显实现规范.md)
- [vector_quick_view_target_tasks 表结构说明](./vector_quick_view_target_tasks表.md)
- [数据库架构](../数据库架构.md)
