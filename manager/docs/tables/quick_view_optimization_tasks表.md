# quick_view_optimization_tasks 表结构说明

> 状态：当前实现说明。`manager.quick_view_optimization_tasks` 表达快显性能优化任务定义，TaskProvider `task_type=quick_view_optimization`。

## 一、表定位

`manager.quick_view_optimization_tasks` 回答：

> 以后按什么配置为哪个 PG 空间表创建或刷新 3857 快显优化目标。

它不替代：

1. `manager.quick_view_optimization` 的结果状态。
2. `common.task_executions` 的执行历史。
3. `manager.tile_cache_tasks` 的瓦片缓存生成任务定义。

普通预览、capability 查询、动态 MVT 请求和瓦片缓存生成任务都不得隐式创建或刷新该任务负责的派生产物。

## 二、目标核心字段

公共字段遵守 `docs/spec/addp任务体系规范.md`。

| 字段名 | 当前类型 / 语义 | 说明 |
| --- | --- | --- |
| `id` | bigint | Manager 内部任务定义 ID |
| `tenant_id` | integer | 租户 ID |
| `name` | varchar | 任务名称 |
| `description` | text | 任务描述 |
| `enabled` | boolean | 是否启用 |
| `schedule` | varchar | 调度表达式；当前不声明任务自身调度能力 |
| `next_run_at` | timestamp | 下一次计划运行时间；当前只作为公共字段保留 |
| `last_run_at` | timestamp | 最近运行时间 |
| `last_execution_id` | varchar | 最近一次 `common.task_executions.execution_id` |
| `last_execution_status` | varchar | 最近一次执行状态，使用统一 execution status |
| `config` | jsonb | 快显性能优化任务私有配置 |
| `created_by` | integer | 创建人 |
| `created_at` / `updated_at` / `deleted_at` | timestamp | 生命周期字段 |

## 三、config 语义

```json
{
  "target": {
    "item_fingerprint": "string",
    "locator": "addp://engine/8/path/public/dltb?type=table&item_id=54",
    "source_engine_id": 8,
    "schema": "public",
    "table": "dltb",
    "item_id": 54
  },
  "geometry": {
    "geometry_column": "SmGeometry",
    "source_srid": 2360,
    "target_srid": 3857
  },
  "optimization": {
    "target_kind": "source_schema_materialized_view",
    "include_source_key": true,
    "attributes": [],
    "analyze_after_build": true
  },
  "storage": {
    "target_schema": "public"
  }
}
```

当前 `config.optimization.target_kind` 固定为 `source_schema_materialized_view`，`config.geometry.target_srid` 固定为 `3857`。任务执行时在源 PG 引擎和源 schema 下创建 ADDP 命名的 3857 物化视图、`geom_3857` GiST 索引，并执行 `ANALYZE`。

## 四、TaskProvider 入口

标准任务入口：

```text
GET  /api/v1/manager/tasks?task_type=quick_view_optimization
GET  /api/v1/manager/tasks/quick_view_optimization/{id}
POST /api/v1/manager/tasks/quick_view_optimization/{id}/execute
GET  /api/v1/manager/executions/{execution_id}
```

模块私有 CRUD：

```text
GET    /api/v1/manager/quick_view_optimization_tasks
POST   /api/v1/manager/quick_view_optimization_tasks
GET    /api/v1/manager/quick_view_optimization_tasks/{id}
PUT    /api/v1/manager/quick_view_optimization_tasks/{id}
DELETE /api/v1/manager/quick_view_optimization_tasks/{id}
```

当前 `supports_schedule=false`、`supports_cancel=false`。如果需要周期性刷新，应由 Orchestrator 定时编排间接触发已保存任务定义。

## 五、相关文档

- [快显概念说明](../快显概念说明.md)
- [快显实现规范](../快显实现规范.md)
- [quick_view_optimization 表结构说明](./quick_view_optimization表.md)
- [数据库架构](../数据库架构.md)
