# vector_tile_cache_tasks 表结构说明

> 状态：当前实现说明。`manager.vector_tile_cache_tasks` 表达瓦片缓存生成任务定义，TaskProvider `task_type=vector_tile_cache_generation`。

## 一、表定位

`manager.vector_tile_cache_tasks` 回答：

> 以后按什么配置生成或刷新瓦片缓存。

瓦片缓存生成必然先创建任务定义，再执行。即使用户从空间预览页点击“生成瓦片缓存”，也应跳转瓦片缓存页面的“任务”tab 创建 `manager.vector_tile_cache_tasks`。如果当前 item 处于源表转换慢路径、`optimization.status=stale` 或瓦片响应提示 `vector_materialized_view_generation`，UI 应先引导用户执行矢量物化视图任务；结果 ready 后再生成瓦片缓存。

瓦片缓存任务创建页的矢量物化视图提示分三类：

1. Manager `ready` 结果：展示可复用提示，不展示“执行矢量物化视图”入口。
2. 外部 3857 只读目标：展示外部目标可复用提示，不展示“执行矢量物化视图”入口，不暗示 Manager 可删除或刷新该目标。
3. Manager `stale` 结果或源表转换慢路径：展示警告和“执行矢量物化视图”入口；瓦片缓存任务仍可创建，不因缺少可索引 3857 目标而禁用。

如果源表本身已是 3857 但缺少 GiST 索引，矢量物化视图任务不创建冗余 3857 目标；动态 MVT 超时闭环应提示生成瓦片缓存，并按 TTL 抑制重复慢请求。

## 二、目标核心字段

公共字段遵守 `docs/spec/addp任务体系规范.md`，并与 `manager.embedding_tasks` 保持一致。

| 字段名 | 当前类型 / 语义 | 说明 |
| --- | --- | --- |
| `id` | bigint | Manager 内部任务定义 ID |
| `tenant_id` | integer | 租户 ID |
| `name` | varchar | 任务名称 |
| `description` | text | 任务描述 |
| `enabled` | boolean | 是否启用定时或自动触发 |
| `schedule` | varchar | Cron 表达式，空表示只手动执行 |
| `next_run_at` | timestamp | 下一次计划运行时间 |
| `last_run_at` | timestamp | 最近运行时间 |
| `last_execution_id` | varchar | 最近一次 `common.task_executions.execution_id` |
| `last_execution_status` | varchar | 最近一次执行状态，使用统一 execution status |
| `config` | jsonb | 瓦片缓存生成私有配置 |
| `created_by` | integer | 创建人 |
| `created_at` / `updated_at` / `deleted_at` | timestamp | 生命周期字段 |

除公共字段、最近执行摘要和审计字段外，瓦片缓存生成私有策略应尽量合并到 `config`。只有存在明确查询、排序、唯一约束、权限过滤或生命周期管理需要时，才考虑拆成独立列。

执行生命周期遵守平台统一任务规范：创建 execution 和任务摘要 `pending` 同事务；运行体领取时 execution 与任务摘要同事务推进为 `running` 并写真实 `started_at`；完成时 `manager.vector_tile_cache` 结果、execution 终态和任务摘要同事务提交。`last_execution_id` 同时用于结果终态更新的 fencing，旧 execution 不得覆盖新 execution 的结果。

## 三、config 语义

```json
{
  "target": {
    "item_fingerprint": "string",
    "locator": "string",
    "source_engine_id": 1,
    "source_kind": "table",
    "full_name": "public.roads",
    "schema": "public",
    "table": "roads",
    "item_id": 123
  },
  "tile": {
    "format": "mvt",
    "tile_matrix_set": "WebMercatorQuad",
    "min_zoom": 0,
    "max_zoom": 12,
    "extent": null,
    "extent_srid": null,
    "target_srid": 3857
  },
  "storage": {
    "storage_ref": "string"
  },
  "options": {
    "geometry_column": "string",
    "attributes": [],
    "simplification": 0,
    "simplification_max_zoom": 0,
    "mvt_extent": 8192,
    "mvt_buffer": 160,
    "max_tile_size_bytes": 5000000,
    "max_features": 1000000,
    "num_threads": "ALL_CPUS",
    "publish_concurrency": 8
  }
}
```

`target` 的主身份是 `source_engine_id + locator + item_fingerprint`。`schema/table` 只表达数据库表型 item 的 PG 执行事实；文件、对象等非表型空间 item 不应为了生成瓦片缓存伪造 `schema/table`。`source_kind` 来自 ResourceLocator 的 `type`，`full_name` 来自 ResourceLocator 解析后的稳定路径，并用于统一计算 `item_fingerprint`。

任务语义身份固定为 `tenant_id + target.item_fingerprint + tile.format`。同一语义身份的重复创建必须复用原任务 ID，并更新名称、描述、启用状态、计划字段和规范化配置；不得返回“任务已存在”，也不得新建重复任务。PostgreSQL 使用仅覆盖未软删除任务的部分唯一索引 `idx_vector_tile_cache_tasks_source_format_unique` 作为并发防线；先查后插命中该索引时必须回查并复用原任务。

非表型空间 item 的 MVT 生成由 GeoPython Workflow 的 `vector_to_mvt_tiles` 算子执行。`options.simplification`、`options.simplification_max_zoom`、`options.mvt_extent`、`options.mvt_buffer`、`options.max_tile_size_bytes`、`options.max_features` 直接控制 GDAL MVT 创建参数；`options.num_threads` 控制 GDAL_NUM_THREADS；`options.publish_concurrency` 控制生成后向对象存储发布瓦片的并发度。默认值面向空间快显质量，避免 GDAL 默认单瓦片 500KB 限制过早导致降精度或丢要素。

以上是语义示例，不是最终 API schema。

## 四、TaskProvider 入口

标准任务入口：

```text
GET  /api/v1/manager/tasks?task_type=vector_tile_cache_generation
GET  /api/v1/manager/tasks/vector_tile_cache_generation/{id}
POST /api/v1/manager/tasks/vector_tile_cache_generation/{id}/execute
GET  /api/v1/manager/executions/{execution_id}
```

模块私有 CRUD 可使用：

```text
GET    /api/v1/manager/vector_tile_cache_tasks
POST   /api/v1/manager/vector_tile_cache_tasks
GET    /api/v1/manager/vector_tile_cache_tasks/{id}
PUT    /api/v1/manager/vector_tile_cache_tasks/{id}
DELETE /api/v1/manager/vector_tile_cache_tasks/{id}
```

私有 CRUD 不替代 TaskProvider 标准入口。

## 五、相关文档

- [快显实现规范](../快显实现规范.md)
- [vector_tile_cache 表结构说明](./vector_tile_cache表.md)
- [数据库架构](../数据库架构.md)
