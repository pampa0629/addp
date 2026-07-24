# vector_tile_set_tasks 表结构说明

> `manager.vector_tile_set_tasks` 表达业务矢量瓦片集生成任务定义，TaskProvider `task_type=vector_tile_set_generation`。

## 一、表定位

该表只保存可重复执行的派生任务定义。结果是用户选择的 Business 存储中的 PMTiles v3 文件和 Meta item；Manager 不建立 `vector_tile_set` 结果表，也不拥有业务产物生命周期。

## 二、核心字段

| 字段 | 说明 |
| --- | --- |
| `id` / `tenant_id` / `name` / `description` | 任务定义基础字段。 |
| `enabled` / `schedule` / `next_run_at` / `last_run_at` | 统一任务字段；当前不启动 Manager owner scheduler。 |
| `last_execution_id` / `last_execution_status` | 最近执行摘要。 |
| `config` | 源 item、目标 Business 存储、生成 profile 和完整语义身份。 |
| `created_by` / `created_at` / `updated_at` / `deleted_at` | 生命周期字段。 |

## 三、config 语义

```json
{
  "source": {
    "locator": "addp://engine/8/path/public/roads?type=table&item_id=54",
    "source_engine_id": 8,
    "item_id": 54,
    "item_fingerprint": "string"
  },
  "target": {
    "engine_id": 26,
    "storage_locator": "addp://engine/26/path/vector-tiles?type=directory&node_id=4",
    "name": "roads.pmtiles"
  },
  "tile": {
    "archive_format": "pmtiles",
    "tile_type": "mvt",
    "tile_matrix_set": "WebMercatorQuad",
    "min_zoom": 0,
    "max_zoom": 12
  },
  "options": {
    "geometry_column": "geometry",
    "layer_name": "roads"
  },
  "profile_hash": "sha256",
  "semantic_hash": "sha256"
}
```

`semantic_hash` 由源 item fingerprint、目标 Business 引擎与完整对象路径、placement 和 `profile_hash` 规范化计算。数据库通过 `tenant_id + semantic_hash` 的部分唯一索引防止并发重复；同一源可以生成多个业务瓦片集，目标完整语义相同的重复创建更新并返回原任务定义。

`source_version` 在每次 execution 开始时从 Meta 当前事实解析并写入 execution metadata，不作为任务定义输入持久化。

## 四、执行与复用

执行器默认调用 GeoPython Workflow `vector_to_pmtiles`。如果存在同源、同 `source_version`、同 `profile_hash` 的 ready `manager.vector_tile_cache`，可将其作为执行复用候选；复用不改变任务身份，也不能让业务 item 或 Service 依赖 infra。目标写入采用临时对象、PMTiles 校验、原子提交、Meta scan 的固定顺序。

标准入口：

```text
GET  /api/v1/manager/tasks?task_type=vector_tile_set_generation
GET  /api/v1/manager/tasks/vector_tile_set_generation/{id}
POST /api/v1/manager/tasks/vector_tile_set_generation/{id}/execute
GET  /api/v1/manager/executions/{execution_id}
```

模块私有 CRUD：

```text
GET    /api/v1/manager/vector_tile_set_tasks
POST   /api/v1/manager/vector_tile_set_tasks
GET    /api/v1/manager/vector_tile_set_tasks/{id}
PUT    /api/v1/manager/vector_tile_set_tasks/{id}
DELETE /api/v1/manager/vector_tile_set_tasks/{id}
```
