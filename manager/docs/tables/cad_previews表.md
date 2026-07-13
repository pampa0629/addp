# cad_previews 表结构说明

> 状态：当前实现说明。`manager.cad_previews` 表达 Manager 拥有生命周期的二维 DWG / DXF 栅格瓦片预览结果。

## 一、表定位

该表回答某个 CAD item 当前是否存在可复用的预览 artifact、产物存放位置、瓦片参数、范围和最近执行。它不是源业务 data item，也不是 CAD entity 表或 CAD→GIS 导入结果。

## 二、核心字段

| 字段名 | 类型 / 语义 | 说明 |
| --- | --- | --- |
| `id` | bigint | 结果 ID |
| `tenant_id` / `item_fingerprint` | bigint / varchar(64) | 源 item 稳定身份 |
| `item_id` / `locator` | bigint / text | 当前 Meta 行引用与 ResourceLocator |
| `task_id` / `last_execution_id` | bigint / varchar | 最近任务和 execution |
| `source_engine_id` / `source_format` | bigint / varchar | 源引擎与 `dwg` / `dxf` 格式 |
| `source_size_bytes` | bigint | 源 CAD 文件大小 |
| `storage_ref` | text | Manager infra MinIO 目录引用，前端不得消费 |
| `manifest_ref` / `thumbnail_ref` | varchar | 目录内 manifest 和缩略图相对路径 |
| `tile_count` / `tile_size` | bigint / integer | 瓦片数量与像素尺寸 |
| `min_zoom` / `max_zoom` | integer | 本地二维瓦片层级范围 |
| `bounds` | jsonb | CAD 本地二维坐标范围 |
| `status` | varchar | `building`、`ready`、`failed`、`deleted` |
| `metadata` / `error_message` | jsonb / text | access plan 审计、runtime facts 与错误摘要 |
| `created_by` / `created_at` / `updated_at` / `deleted_at` | 生命周期字段 | 创建与软删除信息 |

同租户同 item fingerprint 只保留一个未删除的当前结果。

## 三、产物与访问

第一阶段受管目录为：

```text
manifest.json
thumbnail.webp
model-space/{z}/{x}/{y}.webp
```

前端只使用：

```text
GET /api/v1/manager/cad-previews/{id}/manifest
GET /api/v1/manager/cad-previews/{id}/tiles/{z}/{x}/{y}
```

接口必须校验当前租户和 `status=ready`。manifest API 注入稳定的 `tile_url_template`；前端使用 OpenLayers 自定义本地二维 projection，不假定 EPSG:3857。

## 四、删除语义

删除结果时，Manager 先删除 `storage_ref` 下的 infra artifact，再将记录标记为 `deleted` 并软删除。不得删除源 CAD 文件、任务定义、execution 历史或 `preview_state`。

## 五、相关文档

- [CAD 数据支持设计](../../../docs/next/addp-CAD数据支持设计.md)
- [数据预览语义协议](../数据预览语义协议.md)
- [cad_preview_tasks 表结构说明](./cad_preview_tasks表.md)
- [数据库架构](../数据库架构.md)
