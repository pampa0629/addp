# vector_tile_cache 表结构说明

> 状态：当前实现说明。`manager.vector_tile_cache` 表达瓦片缓存结果状态，是原始 data item 与瓦片缓存结果之间的事实源。

## 一、表定位

`manager.vector_tile_cache` 回答：

> 某个瓦片缓存结果是否存在、在哪里、由哪个 item 和哪次任务生成、当前是否可用。

它不替代：

1. `manager.preview_state` 的用户预览模式偏好。
2. `manager.vector_tile_cache_tasks` 的任务定义。
3. `common.task_executions` 的执行历史。

## 二、目标核心字段

| 字段名 | 当前类型 / 语义 | 说明 |
| --- | --- | --- |
| `id` | bigint | 产物 ID |
| `tenant_id` | integer | 租户 ID |
| `item_fingerprint` | varchar(64) | 标准 data item 指纹，用于源数据去重和幂等 |
| `item_id` | integer / nullable | 当前 Meta item 行引用，仅用于回查，不作为去重主键 |
| `locator` | text | 资源树或数据项回跳定位 |
| `task_id` | bigint | 产生或最近刷新该产物的 `manager.vector_tile_cache_tasks.id` |
| `tile_format` | varchar | 瓦片格式，例如 `mvt` |
| `status` | varchar | 产物状态 |
| `storage_ref` | text | 存储引用，指向当前瓦片缓存所在位置或 manifest；对象前缀按 `item_fingerprint` 组织 |
| `extent` | jsonb | 产物覆盖范围；用于 Manager 快显渲染时应保存或转换为 WGS84 经纬度范围 |
| `extent_srid` | integer | 产物范围所使用的 SRID；作为 capability 渲染范围返回时必须为 4326 |
| `min_zoom` / `max_zoom` | integer | 覆盖层级 |
| `last_execution_id` | varchar | 最近一次生成 execution |
| `error_message` | text | 最近错误摘要 |
| `created_by` | integer | 创建人 |
| `created_at` / `updated_at` / `deleted_at` | timestamp | 生命周期字段 |

当前只保留核心字段。瓦片数量、产物大小、访问策略、清理策略、矢量物化视图目标状态、转换引擎等信息分别放在 manifest、execution metadata、`manager.vector_materialized_view` 或后续专题中。

## 三、状态语义

| 状态 | 含义 |
| --- | --- |
| `generating` | 产物记录已创建，等待或正在生成 |
| `ready` | 产物可用 |
| `failed` | 最近一次生成失败，当前产物不可用或不完整 |
| `cancelled` | 生成被取消 |
| `deleted` | 产物已清理，仅保留审计或摘要 |

这些状态属于 artifact state，不属于统一 execution status。

## 四、索引建议

| 索引名 | 字段 | 说明 |
| --- | --- | --- |
| `idx_vector_tile_cache_tenant_item_fingerprint` | `tenant_id, item_fingerprint` | 查询某 item 的瓦片缓存结果 |
| `idx_vector_tile_cache_tenant_fingerprint_format_unique` | `tenant_id, item_fingerprint, tile_format` | 同一 item 和格式只保留一条当前结果 |
| `idx_vector_tile_cache_status` | `status` | 按产物状态过滤 |
| `idx_vector_tile_cache_task` | `task_id` | 查询某任务产生的产物 |
| `idx_vector_tile_cache_execution` | `last_execution_id` | 从 execution 回溯产物 |

## 五、相关文档

- [快显实现规范](../快显实现规范.md)
- [preview_state 表结构和 API 说明](./preview_state表.md)
- [数据库架构](../数据库架构.md)
