# Transfer 数据类型与文件格式后续事项

更新时间：2026-05-05

本文记录 Transfer 模块与数据类型、文件格式、组织方式、标准 attributes 相关的后续工作。当前阶段先不修改 Transfer 代码，只记录后续接力事项，避免和 meta / manager / service / develop 的 attributes 治理推进混在一起。

正式规范见：

- [ADDP 数据类型与格式体系图](addp数据类型与格式体系图.md)
- [ADDP 数据格式扩展指南](addp数据格式扩展指南.md)
- [ADDP 元数据 attributes 规范](addp元数据attributes规范.md)

## 背景

平台 attributes 使用标准分区结构：

```json
{
  "schema_version": 1,
  "storage": {},
  "item": {},
  "type_info": {},
  "format_info": {},
  "capabilities": {}
}
```

Transfer 后续需要按同一口径消费 meta item：

- `storage`：读取物理位置、大小、内容类型、修改时间等。
- `item`：读取组织方式、数据类型、格式、入口路径、组件文件等。
- `type_info`：读取字段、索引、主键、行数、媒体信息、文档信息、容器内部对象等类型信息。
- `format_info`：读取具体格式私有信息。
- `capabilities`：读取空间、时间、统计、提取、分区、索引等横切能力。

## 后续任务

1. 扫描 Transfer 中直接读取旧 attributes 的路径，改为只读取标准分区。
2. 文件读取、格式转换、导入导出任务优先依赖 `attributes.item.format`、`attributes.item.entry_path` 和 `attributes.item.component_files`。
3. 空间能力判断只依赖 `attributes.capabilities.spatial`，不得按固定字段名或单一格式推断空间能力。
4. 表结构和字段映射只依赖 `attributes.type_info.table.fields`。
5. 对 Shapefile、GeoJSON、Parquet、Iceberg 等组织方式/文件格式补充真实传输用例，验证 Transfer 不绕过 meta item 归并结果。
6. 清理 Transfer 内部重复格式判断逻辑，必要时复用 `common/dataitem` 和 `common/attributes`。
7. 明确 Transfer 插件私有扩展写入规范：第三方格式信息只能写入合规 `format_info.<namespace>`，横切能力只能写入合规 `capabilities.<namespace>`，不得覆盖 `storage`、`item`、`type_info` 核心字段。

## 建议验证

- 针对 single item、Shapefile multi item、Iceberg whole item 分别覆盖导入导出。
- 针对带 `capabilities.spatial` 的表格型数据验证空间字段和 SRID 读取。
- 针对旧字段数据验证失败暴露是否清晰，确认重新 meta 扫描后恢复。
