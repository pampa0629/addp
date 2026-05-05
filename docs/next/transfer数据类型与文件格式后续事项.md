# Transfer 数据类型与文件格式后续事项

更新时间：2026-05-05

本文从 `addp数据类型与文件格式改进清单.md` 中拆出 Transfer 模块相关工作。当前阶段先不修改 Transfer 代码，只记录后续接力事项，避免和 meta / manager / service / develop 的 attributes 治理推进混在一起。

## 背景

平台 attributes 正在从平铺字段迁移到标准分区结构：

```json
{
  "schema_version": 1,
  "storage": {},
  "item": {},
  "schema": {},
  "extensions": {}
}
```

Transfer 后续需要按同一口径消费 meta item：

- `storage`：读取物理位置、大小、内容类型、修改时间等。
- `item`：读取组合形态、数据家族、格式、入口路径、组件文件等。
- `schema`：读取字段、索引、主键、行数等结构信息。
- `extensions`：读取空间、媒体、文档、统计等标准扩展。

## 后续任务

1. 扫描 Transfer 中直接读取平铺 attributes 的路径，迁移为标准分区优先读取。
2. 文件读取、格式转换、导入导出任务优先依赖 `attributes.item.format`、`attributes.item.entry_path` 和 `attributes.item.component_files`。
3. 空间能力判断优先依赖 `attributes.extensions.spatial`，不得按固定字段名或单一格式推断空间能力。
4. 表结构和字段映射优先依赖 `attributes.schema.fields`，兼容读取旧平铺 `fields`。
5. 对湖表、Shapefile、GeoJSON、Parquet 等组合/文件格式补充真实传输用例，验证 Transfer 不绕过 meta item 归并结果。
6. 清理 Transfer 内部重复格式判断逻辑，必要时复用 `common/dataitem` 和 `common/attributes`。
7. 明确 Transfer 插件私有扩展写入规范：第三方扩展只能写入合规 `extensions.<namespace>`，不得覆盖 `storage`、`item`、`schema` 核心字段。

## 建议验证

- 针对单文件对象、Shapefile 多文件 item、湖表目录/单文件 item 分别覆盖导入导出。
- 针对带 `extensions.spatial` 的表格型数据验证空间字段和 SRID 读取。
- 针对只有旧平铺字段的存量数据验证兼容读取。
- 针对同时存在分区字段和平铺字段冲突的数据，确认 Transfer 以分区字段为准。
