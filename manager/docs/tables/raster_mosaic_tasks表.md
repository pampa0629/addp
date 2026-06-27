# raster_mosaic_tasks 表

## 定位

`manager.raster_mosaic_tasks` 是栅格 mosaic 生成任务定义表。

该表只保存任务配置和最近执行摘要；mosaic 生成结果不是 Manager artifact，不写入 Manager 私有结果表。任务成功后，目标业务存储中应形成 `raster_mosaic` 数据集，并由 Meta 探测或登记为业务 item。执行过程写入 `common.task_executions`。任务类型固定为 `raster_mosaic_generation`。

## 核心字段

| 字段 | 说明 |
| --- | --- |
| `tenant_id` | 租户。 |
| `name` / `description` | 任务名称与描述。 |
| `enabled` | 是否启用。当前不声明自身定时调度能力。 |
| `schedule` / `next_run_at` | 统一任务字段；第一阶段 `raster_mosaic_generation` 不支持设置。 |
| `last_execution_id` / `last_execution_status` / `last_run_at` | 最近一次执行摘要。 |
| `config` | 源 node、目标业务存储、leaf COG、全局 overview 和可选 tiles 配置。 |
| `created_by` | 创建用户。 |

## config 结构

最小结构：

```json
{
  "source": {
    "node_locator": "addp://engine/26/path/rasters?type=node",
    "source_engine_id": 26,
    "recursive": true,
    "include_patterns": ["*.tif", "*.tiff"],
    "exclude_patterns": []
  },
  "placement": {
    "mode": "detached"
  },
  "target": {
    "storage_locator": "addp://engine/27/path/mosaics/srtm?type=node",
    "target_engine_id": 27,
    "dataset_name": "srtm_mosaic"
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
    "max_zoom": 0,
    "format": "webp"
  }
}
```

`placement.mode` 必须是：

| 模式 | 说明 |
| --- | --- |
| `in_place` | 在原 node 创建 mosaic。`target` 可以省略，服务端默认使用 `source.node_locator`；非 COG TIFF 原地规范化为 COG，文件名保持不变，具体实现必须先生成临时新文件，校验成功后再替换旧文件。已经是 COG 的文件不处理。 |
| `detached` | 创建到新 node。`target.storage_locator` 必填且不得等于 `source.node_locator`；所有 leaf COG 都写入目标 mosaic 数据集，不修改原 node。 |

服务端会校验 `source.node_locator` 与 `source.source_engine_id` 一致、`target.storage_locator` 与 `target.target_engine_id` 一致，并归一化默认参数。`placement.mode` 只影响生成过程；生成完成后，结果统一是一个 `format=raster_mosaic` 的业务 item。

## 约束

1. 不复用 `manager.raster_cog_tasks` 或 `manager.raster_cog` 表表达 mosaic。
2. 不把 mosaic 长期产物写入 Manager infra MinIO。
3. `in_place` 模式会把原 node 内非 COG TIFF 原地规范化为 COG，文件名保持不变；必须先写临时新文件，校验成功后再替换旧文件。
4. `detached` 模式必须把所有 leaf COG 写入目标 mosaic 数据集，不修改原 node。
5. 全局 overview 是低分辨率 COG，不是全分辨率单文件 mosaic COG。
6. 任务执行必须纳入 `common.task_executions`，未接入真实执行器时不得标记成功。
7. `cog.leaf_concurrency` 只对 `detached` 模式生效，默认按运行机器 CPU 预算归一化：逻辑 CPU 小于 8 时为 1，8 到 15 时为 2，16 到 31 时为 4，32 及以上时为 6，上限 8；当前 18 逻辑 CPU 开发机默认值为 4。`in_place` 模式保持串行。
8. `cog.num_threads` 控制单个 leaf COG 的 GDAL `NUM_THREADS`，默认按 `逻辑 CPU / (leaf_concurrency * 2)` 归一化并限制在 1 到 4；当前 18 逻辑 CPU、`leaf_concurrency=4` 时默认值为 2。
9. `cog.leaf_retry_attempts` 控制单个 leaf COG 生成和内容级校验失败后的重试次数，默认 2，上限 5。`detached` 模式再次执行同一任务时会复用目标数据集中已存在且内容级校验通过的 leaf COG，用于超时或中断后的恢复。
