# model3d_tiles 表

`manager.model3d_tiles` 保存 Manager 拥有生命周期的分块三维模型瓦片结果。表名中的 `tiles` 指分块瓦片数据集，不特指 Cesium 3D Tiles；实际格式由 `target_format` 区分。

| 字段 | 说明 |
| --- | --- |
| `item_fingerprint` / `item_id` / `locator` | 源 data item 身份与生成时指纹 |
| `task_id` / `last_execution_id` | 生成任务和最近 execution |
| `target_format` | `3d_tiles` 或 `s3m` |
| `storage_ref` | Manager infra MinIO 目录前缀 |
| `manifest_ref` | 3D Tiles 为 `tileset.json`；S3M 为 `config/scene.scp` |
| `file_count` / `size_bytes` | 结果目录统计 |
| `status` | `building`、`ready`、`failed`、`stale`、`deleted` |

同一租户、源指纹和目标格式只保留一条当前结果。3D Tiles 与 S3M 必须分别登记，某一种格式生成中或失败不影响另一种 ready 结果预览。关联只能读取本表显式字段，不得根据 MinIO 输出目录名称推断。

已有 `building`、`ready`、`failed` 或 `stale` 结果时，重复执行关联任务必须先完成服务端强制的本次覆盖确认；确认后覆盖同一结果记录和受管 MinIO 对象前缀。结果 ID 与任务 ID 保持不变，`last_execution_id`、状态、manifest 和统计更新为最近一次 execution 事实。确认只作用于当前 `target_format`，不得把 3D Tiles 的确认用于 S3M，反之亦然。

ready 结果通过 `GET /api/v1/manager/model3d_tiles/:id/assets/*asset_path` 读取。该接口保留相对路径、Content-Type、Range 和租户鉴权，因此预览不依赖转换引擎在线。

通过 `DELETE /api/v1/manager/model3d_tiles/:id` 删除结果时，Manager 必须先删除 `storage_ref` 对应的 infra MinIO 整个瓦片前缀，再把结果状态更新为 `deleted` 并软删除记录。删除结果不删除源 item、任务定义、execution 历史或另一种 `target_format` 的结果；物理清理失败时不得删除结果记录。
