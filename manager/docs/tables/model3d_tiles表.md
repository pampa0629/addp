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

ready 结果通过 `GET /api/v1/manager/model3d_tiles/:id/assets/*asset_path` 读取。该接口保留相对路径、Content-Type、Range 和租户鉴权，因此预览不依赖转换引擎在线。
