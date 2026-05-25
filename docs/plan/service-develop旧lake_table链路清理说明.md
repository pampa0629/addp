# Service / Develop 旧 `lake_table` 链路清理记录

更新时间：2026-05-25

本文记录 Service、Develop 与 `common/duckdb` 中旧 `lake_table` / `lake_mode` 链路的清理结果。当前结论是：这些概念不再作为代码实现和对外 API 的事实来源。

## 背景

ADDP 的数据类型与格式模型已经收口为：

- `item_type` 表达 Meta item 的基础形态，例如 `table`、`object`、`file`。
- `attributes.item.data_type` 表达统一数据类型，例如 `table`。
- `attributes.item.format` 表达格式，例如 `parquet`、`orc`、`avro`。
- `attributes.item.layout` 表达多个 content 的组织方式，例如 `single`、`multi`、`whole`。
- `attributes.storage.physical_path` 表达可执行读取路径。

因此，Parquet/ORC/Avro 等对象存储表不应再通过 `lake_table` item type 或 `lake_mode` 字段识别。

## 已完成的清理

### Service 查询服务

- `QueryService.IsLakeTable()` 已替换为 `QueryService.IsObjectTable()`。
- `QueryService.GetLakeMode()` 已替换为 `QueryService.GetObjectTablePhysicalPath()`。
- 创建查询服务时不再从 Meta tree 中查找 `item_type == "lake_table"`。
- 对象存储表执行事实写入 `data_config.object_table`：

```json
{
  "object_table": {
    "physical_path": "bucket/path/orders",
    "format": "parquet",
    "layout": "whole"
  }
}
```

- DuckDB 执行路径直接使用 `storage.physical_path` 构造 `read_parquet(...)`，不再通过 `schema/table/lake_mode` 推导路径。
- 前端选择项不再包含 `lake_table`。

### Develop DuckDB 联邦查询

- `TableRef.ItemType` 不再暴露 `"lake_table"`，对象存储表也返回 `table`。
- 对象存储表通过 `duckdb.IsObjectTableItem` 判断。
- 样例 SQL 优先选择带 `physical_path` 的对象存储表。
- API 描述与 Swagger 已改为“对象存储表 + 关系型表”。

### common/duckdb

- `IsLakeTableEngine` 已替换为 `IsObjectTableEngine`。
- `BuildLakeTableMap` 已替换为 `BuildObjectTableMap`。
- `isLakeTableItem` 已替换为导出的 `IsObjectTableItem`。
- `engineLakeTables` 命名已替换为 `engineObjectTables`。
- `BuildLakeTableS3Path` 已删除。

## 当前目标模型

对象存储表是标准 facts 的组合，而不是独立类型：

| 事实 | 要求 |
| --- | --- |
| `meta_item.item_type` | `object` 或 `file` |
| `attributes.item.data_type` | `table` |
| `attributes.item.format` | `parquet`、`orc`、`avro` |
| `attributes.item.layout` | `single` 或 `whole` |
| `attributes.storage.physical_path` | 必须存在 |

如果旧数据缺失上述 attributes，应清空旧 Meta 数据后重新扫描，不增加兼容层。

## 验证结果

已完成：

```bash
go test ./service/backend/internal/...
npm run build --prefix service/frontend
go test ./common/duckdb ./develop/backend/internal/...
npm run build --prefix develop/frontend
bash scripts/swagger/gen-swagger.sh develop
```

前端构建存在 Vite chunk size warning，属于既有打包体积提示。

## 后续注意

- `湖仓` / `湖表` 作为领域语境可以出现在历史计划或业务说明中，但不能作为 Meta item type、attributes 字段或 API 枚举继续传播。
- Manager 内部的 `builtin:scope-table` 是 preview provider 名称，不代表恢复 `lake_table` item type。
- 不为旧 `data_config.lake_mode` 提供迁移脚本；旧数据删除后重建。
