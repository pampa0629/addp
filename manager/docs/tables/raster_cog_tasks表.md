# raster_cog_tasks 表

## 定位

`manager.raster_cog_tasks` 是栅格快显 COG 生成任务定义表。

该表只保存任务配置和最近执行摘要；真正的 COG 结果状态写入 `manager.raster_cog`，执行过程写入 `common.task_executions`。任务类型固定为 `raster_cog_generation`。

## 核心字段

| 字段 | 说明 |
| --- | --- |
| `tenant_id` | 租户。 |
| `name` / `description` | 任务名称与描述。 |
| `enabled` | 是否启用。当前不声明自身定时调度能力，但保留任务体系统一字段。 |
| `schedule` / `next_run_at` | 统一任务字段；第一阶段 `raster_cog_generation` 不支持设置。 |
| `last_execution_id` / `last_execution_status` / `last_run_at` | 最近一次执行摘要。 |
| `config` | 源 item、栅格 facts、COG 生成参数和目标 COG 结果策略。 |
| `created_by` | 创建用户。 |

## config 结构

最小结构：

```json
{
  "target": {
    "source_engine_id": 26,
    "item_id": 100,
    "locator": "addp://engine/26/path/rasters/large.tif?type=file&item_id=100"
  },
  "raster": {
    "source_profile": "geotiff",
    "source_size_bytes": 943718400,
    "width": 120000,
    "height": 80000,
    "band_count": 3,
    "nodata": -32768,
    "display_min": -49,
    "display_max": 406,
    "display_range_method": "metadata_statistics",
    "extent": [110, 20, 120, 30],
    "extent_srid": 4326
  },
  "cog": {
    "compression": "DEFLATE",
    "blocksize": 512,
    "overview_resampling": "NEAREST"
  },
  "result": {
    "target_kind": "infra_minio_object",
    "storage_ref": "{\"type\":\"object\",\"provider\":\"addp_object_storage\",\"bucket\":\"manager\",\"object\":\"tenant_1/cog/<item_fingerprint>/large.cog.tif\"}",
    "file_name": "large.cog.tif"
  }
}
```

服务端会从 ResourceLocator 归一化 `item_fingerprint`。调用方未传 `result` 时，服务端会为目标 COG 结果生成默认 infra MinIO `storage_ref`；任务配置只允许使用 `config.result` 表达目标结果，不再使用 `config.artifact`。

任务语义身份固定为 `tenant_id + target.item_fingerprint`；COG 是该源 item 唯一的受管结果变体。同一语义身份的重复创建必须复用原任务 ID，并更新名称、描述、启用状态、计划字段和规范化配置；不得新建重复任务。PostgreSQL 使用仅覆盖未软删除任务的部分唯一索引 `idx_raster_cog_tasks_source_unique` 作为并发防线；先查后插命中该索引时必须回查并复用原任务。

`raster.nodata`、`raster.display_min`、`raster.display_max` 等渲染统计来自 Meta attributes 或前端/COG 内部 metadata 兜底结果。任务执行成功后，执行器应尽量把这些 facts 写入 `manager.raster_cog.metadata.raster_facts`，供后续 Quick View capability 透传；缺失时前端仍可在打开 COG 后读取 GDAL metadata 或受控采样。

## 约束

1. 不进入 `vector_tile_cache_tasks` 或 `vector_materialized_view_tasks`。
2. 不覆盖源 TIFF，也不创建新的源 item。
3. 前端不得读取目标 `storage_ref` 拼接 URL，只能消费 `/api/v1/manager/raster_cog/{id}/content`。
4. 第一阶段任务执行器已采用单一路线：Manager 将源 ResourceLocator 和目标 COG `storage_ref` 派生为 GDAL `source_uri` / `target_uri` / `gdal_env`，通过 `WorkflowRuntimeProvider.InvokeOperator("tiff_to_cog")` direct 调用 GeoPython Workflow，并直接写入 infra MinIO。NFS / NAS 源要求 GeoPython Workflow 运行环境可访问对应挂载路径；对象存储源由 Manager 生成 presigned URL 后交给 GDAL `/vsicurl/` 读取。
5. Manager 调用 `tiff_to_cog` 时必须显式传入目标 CRS 写入参数。`source_srid=4326` 时写入 WGS84 经纬度定义；其他正数 SRID 写入对应 EPSG 定义；只有缺少 SRID 时才使用可信 `source_crs`。Python 算子只负责把该 CRS 写入目标 COG，不在第一阶段做通用重投影。
6. CRS 解析必须以数据集顶层 CRS authority 为准，不得从 WKT 子节点中提取单位、椭球或其他 authority 作为源 SRID。典型错误是把 `ANGLEUNIT ID["EPSG",9122]` 当作源 CRS，导致 COG GeoKey 写成 `User-Defined` 并在前端快显中不可见。
7. execution 必须在任务行锁保护下原子创建为 `pending`，`started_at` 为空；运行体领取后再与任务摘要同事务推进为 `running`。同一任务已有 `pending` / `running` execution 时拒绝重复执行。
8. 终态提交必须以 `manager.raster_cog.last_execution_id` fencing，并在同一 Infra PostgreSQL 事务更新结果、`common.task_executions` 和任务最近执行摘要。infra MinIO 写入成功但终态事务失败时保留未完成平台状态，由同一任务重跑恢复，不允许拆分补写或伪报成功。
9. 已有未删除当前结果时，重复执行必须先提交本次覆盖确认；未确认不得创建 execution、修改任务摘要或重置结果状态。
