# raster_cog 表

## 定位

`manager.raster_cog` 是 Manager 拥有生命周期的栅格快显 COG 生成结果表。

源数据仍是原始 TIFF/GeoTIFF/COG item，可能位于 NFS、业务 MinIO 或其他存储引擎。`raster_cog` 只登记前端可通过 Manager 受控接口消费的 infra MinIO COG 副本，不把 COG 变成新的基础 `format`，也不进入 `vector_tile_cache` 或 `vector_quick_view_target_generation`。

raster COG 的生成任务定义写入 `manager.raster_cog_tasks`，TaskProvider `task_type=raster_cog_generation`。本表只表达结果状态。

## 核心字段

| 字段 | 说明 |
| --- | --- |
| `tenant_id` | 租户。 |
| `item_fingerprint` | 源 item 当前身份指纹。当前唯一结果按 `tenant_id + item_fingerprint` 收敛。 |
| `locator` | 源 item ResourceLocator，用于回跳和判断 stale。 |
| `source_engine_id` | 生成 COG 结果时的源引擎事实，用于判断 stale。 |
| `task_id` | 产生或最近刷新该 COG 结果的 `manager.raster_cog_tasks.id`。 |
| `source_profile` | 源 TIFF profile，如 `geotiff` 或 `cog`。 |
| `target_kind` | 当前固定为 `infra_minio_object`。 |
| `storage_ref` | Manager infra MinIO 对象引用，结构化 JSON，不暴露给前端拼接 URL。 |
| `width` / `height` / `band_count` | 栅格基础事实。 |
| `source_srid` / `source_crs` | 源坐标参考事实。 |
| `extent` / `extent_srid` | 可渲染范围事实。 |
| `status` | COG 结果状态，取值见下文。 |
| `metadata` | 转换过程的补充事实、渲染统计和 direct 调用审计信息。 |
| `last_execution_id` | 最近一次生成执行。 |

## metadata 审计结构

`raster_cog` 通过 Python Workflow 的 `tiff_to_cog` direct 算子生成。Direct 调用不进入 Orchestrator，也不由 Monitor 统一监控，因此 Manager 必须在 COG 结果状态中保存最小审计信息：

```json
{
  "source": {
    "access": {
      "engine_type": "nfs",
      "access_method": "mounted_path"
    }
  },
  "python_workflow": {
    "engine_id": 99,
    "engine_name": "Python Workflow",
    "execution_id": "py-1",
    "operator": "tiff_to_cog",
    "mode": "direct",
    "execution_time_ms": 45.5
  },
  "raster_facts": {
    "profile": "cog",
    "width": 256,
    "height": 128,
    "band_count": 3,
    "nodata": -32768,
    "display_min": -49,
    "display_max": 406,
    "display_range_method": "metadata_statistics",
    "extent": [110, 20, 120, 30],
    "extent_srid": 4326
  }
}
```

`storage_ref`、`locator`、`item_fingerprint`、`last_execution_id` 和 `status` 是一等字段；`metadata` 只保存补充事实和运行时审计，不作为 COG 结果主身份。

## 状态

| 状态 | 语义 |
| --- | --- |
| `building` | 正在生成。 |
| `ready` | 可被 `/api/v1/manager/raster_cog/{id}/content` 读取。 |
| `failed` | 生成失败。 |
| `stale` | COG 结果与当前 item facts 不匹配，不能用于渲染。 |
| `deleted` | 已删除。 |

这些状态是 COG 结果状态，不是统一 execution status。执行过程记录在 `common.task_executions`。

## 消费规则

Quick View Capability 查询时：

1. 先根据 `tenant_id + item_fingerprint` 查最新 ready raster COG。
2. 校验 `source_engine_id`、`locator`、宽高、SRID 等关键 facts。
3. 校验通过返回 `render_source=client_cog_render`，`quick_view.preview_url=/api/v1/manager/raster_cog/{id}/content`。
4. 校验失败将 COG 结果标记为 `stale`，继续按当前源 TIFF facts 返回生成建议。
5. 若 COG 结果 metadata 或源 item attributes 中存在 `nodata`、`display_min`、`display_max` 等渲染统计，Quick View capability 应透传到 `raster` 字段；前端仍可在缺失时读取 COG 内部 GDAL metadata 作为运行时兜底。
6. ready COG 必须具备标准 GeoTIFF CRS 标识。对于 WGS84 经纬度 COG，应能被常见 GeoTIFF 工具识别为 `GCS_WGS_84` / `EPSG:4326`，不能只写成 `User-Defined` GeoKey；否则必须视为不可可靠渲染并重新生成。

前端只消费 `preview_url`，不得读取或解析 `storage_ref`。

## 删除语义

通过 `DELETE /api/v1/manager/raster_cog/{id}` 删除 COG 生成结果时，Manager 必须先按 `storage_ref` 删除 infra MinIO 对象，再将结果状态标记为 `deleted` 并软删除记录。缺少物理清理器时不得静默删除状态，避免形成孤儿对象。
