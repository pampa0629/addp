# vector_tile_cache 表结构说明

> 状态：当前实现说明。`manager.vector_tile_cache` 表达 Manager infra PMTiles 快显缓存结果，是原始 data item 与当前受管 artifact 之间的事实源。

## 一、表定位

`manager.vector_tile_cache` 回答：

> 某个瓦片缓存结果是否存在、在哪里、由哪个 item 和哪次任务生成、当前是否可用。

它不替代：

1. `manager.preview_state` 的用户预览模式偏好。
2. `manager.task_definitions` 中 `task_type=vector_tile_cache_generation` 的任务定义。
3. `common.task_executions` 的执行历史。

## 二、目标核心字段

| 字段名 | 当前类型 / 语义 | 说明 |
| --- | --- | --- |
| `id` | bigint | 产物 ID |
| `tenant_id` | integer | 租户 ID |
| `item_fingerprint` | varchar(64) | 标准 data item 指纹，用于源数据去重和幂等 |
| `item_id` | integer / nullable | 当前 Meta item 行引用，仅用于回查，不作为去重主键 |
| `locator` | text | 资源树或数据项回跳定位 |
| `task_id` | bigint | 产生或最近刷新该产物的 `manager.task_definitions.id` |
| `tile_format` | varchar | PMTiles 内部瓦片编码，当前固定为 `mvt` |
| `status` | varchar | 产物状态 |
| `storage_ref` | text | 指向单个 infra `.pmtiles` 对象的存储引用，不允许目录前缀或 manifest |
| `source_version` | varchar | 执行时冻结的源内容版本，用于判断缓存是否 stale |
| `profile_hash` | varchar | 规范化生成 profile 的哈希，用于判断缓存能否被业务任务复用 |
| `extent` | jsonb | 产物覆盖范围；用于 Manager 快显渲染时应保存或转换为 WGS84 经纬度范围 |
| `extent_srid` | integer | 产物范围所使用的 SRID；作为 capability 渲染范围返回时必须为 4326 |
| `min_zoom` / `max_zoom` | integer | 覆盖层级 |
| `last_execution_id` | varchar | 最近一次生成 execution |
| `error_message` | text | 最近错误摘要 |
| `created_by` | integer | 创建人 |
| `created_at` / `updated_at` / `deleted_at` | timestamp | 生命周期字段 |

当前只保留核心字段。PMTiles header 摘要、归档大小、转换引擎和每层统计进入 execution metadata；不创建 ADDP 私有 manifest。

## 三、状态语义

| 状态 | 含义 |
| --- | --- |
| `generating` | 产物记录已创建，等待或正在生成 |
| `ready` | 产物可用 |
| `failed` | 最近一次生成失败，当前产物不可用或不完整 |
| `cancelled` | 生成被取消 |
| `deleted` | 产物已清理，仅保留审计或摘要 |

这些状态属于 artifact state，不属于统一 execution status。

<a id="四索引建议"></a>

## 四、索引声明与核验边界

[TileCache 模型](../../backend/internal/models/tile_cache.go)声明查询索引；当前结果的部分唯一索引由 [InitDatabase／ensureTileCacheStateSchema](../../backend/internal/repository/database.go) 在 `AutoMigrate` 后显式创建：

| 索引名 | 字段 | 声明来源与语义 |
| --- | --- | --- |
| `idx_vector_tile_cache_tenant_item_fingerprint` | `tenant_id, item_fingerprint` | 模型普通索引，查询某 item 的瓦片缓存结果。 |
| `idx_vector_tile_cache_tenant_fingerprint_profile_unique` | `tenant_id, item_fingerprint, profile_hash` | 初始化 SQL 的部分唯一索引，仅覆盖 `deleted_at IS NULL` 的记录。 |
| `idx_vector_tile_cache_tenant_item` | `tenant_id, item_id` | 模型普通索引，按当前 Meta item 行引用辅助回查。 |
| `idx_vector_tile_cache_status` | `status` | 模型普通索引，按产物状态过滤。 |
| `idx_vector_tile_cache_task` | `task_id` | 模型普通索引，查询某任务产生的产物。 |
| `idx_vector_tile_cache_execution` | `last_execution_id` | 模型普通索引，从 execution 回溯产物。 |
| 模型未显式命名 | `deleted_at` | 模型声明普通索引；实际名称与落库状态需核验。 |

部分唯一约束针对未软删除的当前结果，不按 `status` 过滤；不能仅因 `status=deleted` 就认定该记录已退出唯一约束。实际数据库须核对所属表、字段组合、唯一性、有效状态及 `deleted_at IS NULL` 谓词，不能只核对索引名称。

[历史 014 SQL](../../backend/migrations/014_recreate_vector_tile_cache_state.sql)使用 `tile_format` 参与唯一键；[历史 039 SQL](../../backend/migrations/039_recreate_vector_tile_cache_as_pmtiles.sql)和当前 Go 初始化使用 `profile_hash`，并包含表重建或记录清理。历史文件存在不表示目标环境已执行，不以重启初始化或重放历史 SQL 代替只读约束核验。

现有 [ManagedTaskSemanticIdentityIndexes 用例](../../backend/internal/repository/tile_cache_repository_postgres_integration_test.go)会调用初始化，但直接反例断言的是任务定义的 `uq_manager_task_definition_semantic`，不能把它计为本结果表部分唯一索引的重复记录拒绝、软删除后复用或当前运行库约束已生效的独立证据。该用例由 `make test-manager-postgres` 的现有命名筛选命中；标准门禁与分版本证据统一见[Manager 开发与验证](../../CLAUDE.md#开发与验证)，源码与用例存在不计为数据库或运行验收通过。

## 五、相关文档

- [快显实现规范](../快显实现规范.md)
- [preview_state 表结构和 API 说明](./preview_state表.md)
- [数据库架构](../数据库架构.md)
