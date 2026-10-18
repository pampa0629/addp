# Raster mosaic 快显

[返回快显实现规范](../快显实现规范.md)

本页定义 raster_mosaic 业务数据集的生成配置、瓦片读取、执行生命周期与当前能力边界。共用任务规则见[共用契约](共用契约.md)。

阅读导航：[Raster mosaic 栅格快显](#raster-mosaic-栅格快显)、[能力判断与响应](#能力判断与响应)、[瓦片接口与读取策略](#瓦片接口与读取策略)、[任务接口与生成配置](#任务接口与生成配置)、[执行与租约](#执行与租约)、[运行事件与进度](#运行事件与进度)、[当前支持边界](#当前支持边界)。

## Raster mosaic 栅格快显

### 能力判断与响应

Raster mosaic 的能力判断同样通过：

```http
GET /api/v1/manager/quick-view/capability?locator={resource_locator}
```

当 item 是 `data_type=media`、`format=raster_mosaic`、`layout=whole`，且 manifest、index、overview COG 可读时，能力响应使用专用渲染源：

```json
{
  "can_use_quick_view": true,
  "status": "available",
  "render_source": "raster_mosaic_tile",
  "quick_view": {
    "render_source": "raster_mosaic_tile",
    "tile_url": "/api/v1/manager/raster_mosaic/tiles/{z}/{x}/{y}.png?locator=...",
    "extent": [110.0, 20.0, 120.0, 30.0],
    "extent_srid": 4326,
    "source_srid": 4326
  },
  "raster_mosaic": {
    "format": "raster_mosaic",
    "manifest_ref": "mosaic.addp.json",
    "index_ref": "index/source-index.json",
    "overview_ref": "overviews/overview.cog.tif",
    "leaf_count": 2360,
    "leaf_storage": "referenced_and_generated_cog"
  }
}
```

### 瓦片接口与读取策略

mosaic 瓦片接口：

```http
GET /api/v1/manager/raster_mosaic/tiles/{z}/{x}/{y}.png?locator={resource_locator}
```

读取策略：

1. 低层级优先读取 mosaic 数据集内的低层级 tile cache；没有 tile cache 时读取全局 overview COG 动态切片。
2. 中高层级按 mosaic index 查询相交 leaf COG，读取对应 window，按 NoData、优先级、重采样和样式合成图片瓦片。
3. 全幅显示不得临时读取几千个 leaf COG 的 overview，应使用 global overview COG 或低层级 tile cache。
4. 常规在线预览不得调用 GeoPython Workflow；GeoPython Workflow 只参与 `raster_mosaic_generation`、重建 overview、重建 leaf COG、重算 stats 或重建 tile cache 等离线任务。

### 任务接口与生成配置

mosaic 任务管理接口：

```http
GET    /api/v1/manager/tasks?task_type=raster_mosaic_generation
POST   /api/v1/manager/tasks/raster_mosaic_generation
GET    /api/v1/manager/tasks/raster_mosaic_generation/{id}
PUT    /api/v1/manager/tasks/raster_mosaic_generation/{id}
DELETE /api/v1/manager/tasks/raster_mosaic_generation/{id}
```

`manager.task_definitions.config` 在 `task_type=raster_mosaic_generation` 时建议使用以下最小结构：

```json
{
  "source": {
    "node_locator": "addp://engine/26/path/rasters/srtm?type=node",
    "source_engine_id": 26,
    "recursive": true,
    "include_patterns": ["*.tif", "*.tiff"],
    "exclude_patterns": []
  },
  "placement": {
    "mode": "detached"
  },
  "target": {
    "storage_locator": "addp://engine/31/path/mosaics/srtm?type=node",
    "target_engine_id": 31,
    "dataset_name": "srtm"
  },
  "cog": {
    "compression": "DEFLATE",
    "blocksize": 512,
    "overview_resampling": "NEAREST",
    "validate_source_cog": true,
    "leaf_concurrency": 4,
    "num_threads": 2,
    "leaf_retry_attempts": 2
  },
  "overview": {
    "enabled": true,
    "max_pixels": 64000000,
    "resampling": "AVERAGE"
  },
  "tiles": {
    "enabled": false,
    "min_zoom": 0,
    "max_zoom": 6,
    "format": "webp"
  }
}
```

配置规则：

1. `source.node_locator` 必须指向 node，不接受临时文件列表作为任务长期身份。
2. `placement.mode` 必须是 `in_place` 或 `detached`。
3. `in_place` 表示在原 node 创建 mosaic；`target` 可以省略，服务端默认使用 `source.node_locator`。该模式会将原 node 内非 COG TIFF 原地规范化为 COG，文件名保持不变；具体实现必须先生成临时新文件，校验成功后再替换旧文件。已经是 COG 的文件不处理。
4. `detached` 表示创建到新 node；`target.storage_locator` 必填，且不得等于 `source.node_locator`。该模式将所有 leaf COG 写入目标 mosaic 数据集，不修改原 node。
5. `target.storage_locator` 必须指向用户选择的业务存储位置，不得指向 Manager infra MinIO。
6. `include_patterns` / `exclude_patterns` 只用于候选发现；COG 判定必须走内容级校验。
7. `overview.enabled` 必须为 `true`，因为全幅显示必须依赖 global overview COG。
8. `tiles.enabled=false` 时不生成低层级瓦片，但仍保留后续任务配置入口。
9. `cog.leaf_concurrency` 只控制 `detached` 模式下 leaf COG 生成并发，默认按运行机器 CPU 预算归一化：逻辑 CPU 小于 8 时为 1，8 到 15 时为 2，16 到 31 时为 4，32 及以上时为 6，上限 8。`in_place` 模式涉及原文件替换，必须保持串行。
10. `cog.num_threads` 控制单个 leaf COG 的 GDAL `NUM_THREADS`，默认按 `逻辑 CPU / (leaf_concurrency * 2)` 归一化并限制在 1 到 4，避免 leaf 并发与 GDAL 内部线程叠加导致过量抢占。
11. `cog.leaf_retry_attempts` 控制单个 leaf COG 生成和内容级校验失败后的重试次数，默认 2，上限 5。`detached` 模式重跑时，目标数据集中已存在且内容级校验通过的 leaf COG 必须复用，不能从头覆盖全部 leaf；超时或中断后的恢复方式是再次执行同一任务继续完成未生成 leaf。

### 执行与租约

标准 TaskProvider 使用 `task_type=raster_mosaic_generation` 触发执行。执行器主路径为：Manager 校验源 node locator、目标业务存储 locator 和任务配置，选择可用 GeoPython Workflow runtime，并把 ADDP 业务语义解析为 `access_plan` 后调用 GeoPython Workflow 的 `build_raster_mosaic` direct 算子。`access_plan` 包含 GDAL 可读写 root URI、GDAL 环境变量、候选发现规则、目标数据集 root、进度回调地址和诊断 metadata；原始 locator 只作为 metadata 保留，不由 Python 解析。GeoPython Workflow 内部使用 GDAL Python API 组合落地：`gdal.Translate` 负责 TIFF 到 COG，`gdal.BuildVRT` 负责 virtual mosaic，`gdal.Translate` 负责生成全局 overview COG，并通过 GDAL callback 向 Manager 上报进度。源和目标都是 `/vsis3/` 且 endpoint/凭据不同时，GeoPython Workflow 使用 GDAL path-specific option 将 source root 和 target root 分别绑定到各自的访问参数，避免把业务库凭证写入任务结果或 manifest。任务完成后由 Manager 或任务体系触发 Meta 刷新目标业务存储，形成 raster mosaic 业务 item。

`raster_mosaic_generation` execution 必须先在 Infra PostgreSQL 的任务定义行锁内原子创建为 `pending`，`started_at` 为空；Manager 运行体领取时将 execution 和任务摘要同事务推进为 `running`。同一任务存在 `pending` / `running` execution 时重复执行返回 `409 Conflict`。Mosaic 没有 Manager 私有结果表，终态事务只原子提交 `common.task_executions` 和对应 `manager.task_definitions` 最近执行摘要。业务存储中的 mosaic 数据集写入、Meta scan 提交与 Infra PostgreSQL 不能组成跨系统事务；外部产物已经生成但终态事务失败时不得拆分补写或伪报成功，应保留未完成 execution 并通过同一任务的可恢复重跑继续处理。

### 运行事件与进度

GeoPython Workflow 不直接写 `common.task_executions`。mosaic 生成过程中的阶段进度统一通过 Manager 执行事件入口汇总，Runtime 以 `addp-geopython` Tenant Service Access Token 调用，Tenant 只从 AuthContext 获取：

```http
POST /api/v1/manager/executions/{execution_id}/events
Authorization: Bearer <tenant_service_access_token>
Content-Type: application/json
```

事件体只保存当前阶段摘要和计数，不保存全量文件明细：

```json
{
  "attempt": 1,
  "lease_token": "attempt-scoped-lease-token",
  "phase": "leaf_cog",
  "event": "file_progress",
  "message": "生成 leaf COG",
  "total_files": 3200,
  "processed_files": 36,
  "failed_files": 0,
  "current_file": "srtm_40_01.tif",
  "file_progress": 62,
  "overall_progress": 18,
  "metadata": {
    "gdal_operation": "Translate"
  }
}
```

Manager 收到事件后必须校验租户、`execution_id`、`module=manager`、`task_type=raster_mosaic_generation`、`status=running` 以及当前 `attempt + lease_token`，并只更新 `progress`、`current_step`、`execution_time_ms`、`metadata.progress_event` 和 `updated_at`。`pending`、已完成 execution 或旧 attempt 的迟到事件都不接受。最终成功或失败状态仍由 mosaic 任务执行主流程写入，失败时应保留最近一次 `progress_event` 作为诊断摘要。

### 当前支持边界

当前支持边界：

1. mosaic 结果不写入 `manager.raster_cog`。
2. mosaic 长期产物不进入 Manager infra MinIO。
3. 源文件已是 COG 时，必须通过内容级 COG 校验后才能进入 leaf 计划；不能根据后缀或文件名判断。
4. `placement.mode` 只影响生成过程。生成完成后，结果统一是一个 `format=raster_mosaic` 的业务 item，不再按 `in_place` / `detached` 区分 item 语义。
5. `in_place` 模式下，源文件不是 COG 或校验不确定时，任务必须先生成临时 COG，校验成功后再替换为原文件名。
6. `detached` 模式下，所有 leaf COG 都写入目标 mosaic 数据集，不修改原 node。
7. leaf COG 默认作为 mosaic item 内部浏览对象，不自动升格为同级 Meta item。
8. leaf COG 必须写入可被 GeoTIFF / OpenLayers 识别的 CRS GeoKey。源 TIFF 缺 Projection 但 GeoTransform/extent 明确落在经纬度范围内时，生成器按 `EPSG:4326` 写入目标 COG，并在 source index 中记录 `source_crs=EPSG:4326`；历史 leaf index 缺 `source_crs` 时，预览层可基于经纬度 extent 推断 `extent_srid=4326`。
