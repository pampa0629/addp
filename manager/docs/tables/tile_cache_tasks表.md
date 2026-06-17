# tile_cache_tasks 表结构说明

> 状态：当前实现说明。`manager.tile_cache_tasks` 表达瓦片缓存生成任务定义，TaskProvider `task_type=tile_cache_generation`。

## 一、表定位

`manager.tile_cache_tasks` 回答：

> 以后按什么配置生成或刷新瓦片缓存。

瓦片缓存生成必然先创建任务定义，再执行。即使用户从空间预览页点击“生成瓦片缓存”，也应跳转瓦片缓存页面的“任务”tab 创建 `manager.tile_cache_tasks`。如果当前 item 处于源表转换慢路径、`optimization.status=stale` 或瓦片响应提示 `quick_view_optimization`，UI 应先引导用户执行快显性能优化；优化结果 ready 后再生成瓦片缓存。

瓦片缓存任务创建页的快显性能优化提示分三类：

1. Manager `ready` 优化结果：展示可复用提示，不展示“执行快显优化”入口。
2. 外部 3857 只读目标：展示外部目标可复用提示，不展示“执行快显优化”入口，不暗示 Manager 可删除或刷新该目标。
3. Manager `stale` 结果或源表转换慢路径：展示警告和“执行快显优化”入口；瓦片缓存任务仍可创建，不因缺少可索引 3857 目标而禁用。

如果源表本身已是 3857 但缺少 GiST 索引，快显性能优化任务不创建冗余 3857 目标；动态 MVT 超时闭环应提示生成瓦片缓存，并按 TTL 抑制重复慢请求。

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

## 三、config 语义

```json
{
  "target": {
    "item_fingerprint": "string",
    "locator": "string",
    "source_engine_id": 1,
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
    "simplification": null
  }
}
```

以上是语义示例，不是最终 API schema。

## 四、TaskProvider 入口

标准任务入口：

```text
GET  /api/v1/manager/tasks?task_type=tile_cache_generation
GET  /api/v1/manager/tasks/tile_cache_generation/{id}
POST /api/v1/manager/tasks/tile_cache_generation/{id}/execute
GET  /api/v1/manager/executions/{execution_id}
```

模块私有 CRUD 可使用：

```text
GET    /api/v1/manager/tile_cache_tasks
POST   /api/v1/manager/tile_cache_tasks
GET    /api/v1/manager/tile_cache_tasks/{id}
PUT    /api/v1/manager/tile_cache_tasks/{id}
DELETE /api/v1/manager/tile_cache_tasks/{id}
```

私有 CRUD 不替代 TaskProvider 标准入口。

## 五、相关文档

- [快显实现规范](../快显实现规范.md)
- [tile_cache 表结构说明](./tile_cache表.md)
- [数据库架构](../数据库架构.md)
