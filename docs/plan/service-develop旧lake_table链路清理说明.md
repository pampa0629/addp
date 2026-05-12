# Service / Develop 旧 `lake_table` 链路清理说明

更新时间：2026-05-09

本文记录 `service` 与 `develop` 模块中仍然依赖旧 `lake_table` 概念的链路。当前不立即改代码，后续单独开工。

## 背景

ADDP 的数据类型与格式模型已经收口为：

- `item_type=table`
- `item.data_type=table`
- `item.format=parquet/orc/avro/...`
- `item.organization=single/multi/whole`

因此，`lake_table` 不再应该作为基础 item type 暴露给上层模块。

但 `service` 与 `develop` 仍有真实业务逻辑依赖旧 `lake_table`：

- 查询服务发布时用它判断是否走 DuckDB + Parquet 路径。
- Develop 的 DuckDB 联邦查询用它构造可查询数据源列表。
- Swagger、前端选择项和部分 data_config 字段也延续了旧命名。

这不是简单改名，涉及 API、前端、服务配置数据、Meta 查询条件和 DuckDB SQL rewrite，需要单独处理。

## 当前残留点

### 1. `service` 查询服务

主要文件：

- `service/backend/internal/models/query_service.go`
- `service/backend/internal/service/query_service_service.go`
- `service/backend/internal/service/query_executor_service.go`
- `service/frontend/src/views/QueryServiceForm.vue`

当前表现：

- `QueryService.IsLakeTable()` 通过 `data_config["lake_mode"]` 判断是否为湖表。
- `QueryService.GetLakeMode()` 返回 `directory/file`。
- `QueryServiceService.detectLakeMode()` 从 Meta tree 中查找 `item.ItemType == "lake_table"`。
- `QueryExecutorService.executeLakeTableQuery()` 通过 `duckdb.BuildLakeTableS3Path()` 构造 `read_parquet` 路径。
- 前端 `QueryServiceForm.vue` 允许选择 `['table', 'lake_table']`。

问题：

- 新扫描结果不会再产出 `item_type=lake_table`。
- `lake_mode` 与当前 `organization=single/whole` 重叠。
- `detectLakeMode()` 继续查旧 item type 会导致新元数据无法识别为目录型/文件型表格资源。

### 2. `develop` DuckDB 联邦查询

主要文件：

- `develop/backend/internal/service/duckdb_service.go`
- `develop/backend/internal/service/sql_rewriter.go`
- `develop/backend/internal/api/duckdb_handler.go`
- `develop/backend/internal/api/engine_handler.go`
- `develop/backend/docs/*`

当前表现：

- `TableRef.ItemType` 注释仍是 `"table" 或 "lake_table"`。
- `GenerateSampleQuery()` 优先选择 `ItemType == "lake_table"`。
- `getLakeTables()` 从 Meta tree 中筛选 `item.ItemType == "lake_table"`。
- `getRelationalTables()` 排除 `item.ItemType == "lake_table"`。
- API 描述仍写“湖表 + 关系型表”。

问题：

- Develop 的数据源枚举会漏掉新的 `item_type=table + format=parquet + organization=single/whole`。
- `TableRef.ItemType` 继续承载旧概念，无法区分“原生关系表”和“文件/目录型表格资源”。
- Swagger 文档仍向外暴露旧枚举。

### 3. `common/duckdb` SQL rewrite

主要文件：

- `common/duckdb/engine.go`
- `common/duckdb/federated.go`
- `common/duckdb/rewriter.go`

当前表现：

- `IsLakeTableEngine()` 判断 MinIO/S3 等对象存储引擎。
- `BuildLakeTableMap()` 构建 `engineName -> tableName -> physicalPath`。
- `isLakeTableItem()` 已经接近新模型：它判断 `item.ItemType == "table"`，并通过 attributes 中的 `item.data_type` 与 `item.format` 判断 Parquet/ORC/Avro。
- `BuildLakeTableS3Path()` 仍以 `lake_mode=directory/file` 构造路径。
- `RewriteWithEngines()` 参数仍叫 `engineLakeTables`。

问题：

- 这里有一半已经按新模型走，一半仍是旧命名。
- `BuildLakeTableS3Path()` 是通过 schema/table/lake_mode 推导路径；新模型更应该直接使用 `storage.physical_path`、`item.organization`、`meta_item.full_name` 和 ResourceReader / FormatPlugin / content reader。

## 目标模型

后续清理目标：

- 不再对外暴露 `lake_table` item type。
- 不再使用 `lake_mode` 作为核心判断字段。
- 用标准 attributes 判断文件/目录型表格资源：
  - `item.data_type == "table"`
  - `item.format in ["parquet", "orc", "avro"]`
  - `item.organization == "single" | "whole"`
  - `storage.physical_path`
  - `item.component_files`
- DuckDB 查询路径不再基于旧 `schema/table/lake_mode` 猜路径，而是基于 Meta 已确认的物理路径和组织形态生成。

建议的新语义命名：

| 旧概念 | 新概念 |
| --- | --- |
| `lake_table` item type | `table` item type |
| `lake_mode=directory` | `organization=whole` |
| `lake_mode=file` | `organization=single` |
| `getLakeTables` | `getFileTables` / `getObjectTableItems` |
| `BuildLakeTableMap` | `BuildObjectTableMap` / `BuildFileTableMap` |
| `engineLakeTables` | `engineObjectTables` / `engineFileTables` |

## 建议改造步骤

### 第一步：定义 DuckDB 可查询表引用模型

先在 `common/duckdb` 或 develop 内部定义中间结构，避免继续让 `ItemType` 承载来源差异：

```go
type DuckDBTableRef struct {
    EngineName   string
    EngineID     uint
    TableName    string
    SchemaName   string
    DataType     string
    Format       string
    Organization string
    PhysicalPath string
    EntryPath    string
    SourceKind   string // native_table / object_table / file_table
}
```

`SourceKind` 是 DuckDB 编排内部概念，不应写回 Meta，也不应替代 `data_type/format/organization`。

### 第二步：替换 Meta 查询条件

把这些判断：

```go
item.ItemType == "lake_table"
```

替换为：

```go
item.ItemType == "table"
item.attributes.item.data_type == "table"
item.attributes.item.format in ("parquet", "orc", "avro")
item.attributes.item.organization in ("single", "whole")
```

如果 attributes 暂时不完整，应优先修 Meta 扫描与重扫，而不是保留旧兼容分支。

### 第三步：替换路径构造

废弃以 `schema/table/lake_mode` 猜 S3 路径的方式。

后续路径来源应为：

- `organization=single`：使用 `storage.physical_path` 或 `entry_path` 指向单文件。
- `organization=whole`：使用 `storage.physical_path` 作为 scope，再由 format/resource 规则决定读取表达。
- 多文件组件：使用 `item.component_files`，由 DuckDB 适配层决定是否展开成 `read_parquet([...])` 或 glob。

### 第四步：调整 `service` 查询服务配置

`QueryService.DataConfig` 中建议替换：

```json
{
  "lake_mode": "directory"
}
```

为：

```json
{
  "source": {
    "kind": "object_table",
    "format": "parquet",
    "organization": "whole",
    "physical_path": "bucket/path"
  }
}
```

这里的 `source.kind` 是 service 自身配置语义，不是 Meta item type。

### 第五步：调整前端和 Swagger

- 前端选择项删除 `lake_table`。
- 选择逻辑改为“可查询 table item”，再用 format / organization 展示来源差异。
- Swagger 中 `"table" 或 "lake_table"` 改为 `table`，并在字段说明中描述 `format` 与 `organization`。
- API 描述中的“湖表 + 关系型表”改为“对象/文件表格资源 + 原生关系表”。

### 第六步：删除旧命名

最后统一删除或改名：

- `IsLakeTable`
- `GetLakeMode`
- `detectLakeMode`
- `executeLakeTableQuery`
- `BuildLakeTableS3Path`
- `BuildLakeTableMap`
- `IsLakeTableEngine`
- `engineLakeTables`
- `getLakeTables`

不要保留旧别名或兼容分支。

## 风险点

- 已发布的查询服务可能已有 `data_config.lake_mode`。
- 前端可能仍按 `lake_table` 节点类型过滤可选资源。
- Develop 的 SQL 自动补全与样例 SQL 依赖 `TableRef.ItemType`。
- DuckDB rewrite 目前对 Parquet 路径有内置假设，只支持 `*.parquet` 风格。
- 如果 Meta 未重扫，旧数据仍可能没有 `item.format`、`item.organization`、`storage.physical_path` 等标准属性。

## 暂定验收标准

- 全仓不再出现业务判断 `item_type == "lake_table"`。
- `service` 前端不再暴露 `lake_table` 可选节点类型。
- `develop` Swagger 不再出现 `"table" 或 "lake_table"`。
- 新扫描出的 Parquet 单文件表和目录型表格 scope 都能被 Develop DuckDB 列出并生成样例 SQL。
- Service 查询服务可以基于新 attributes 执行单文件表和目录型表格查询。
- `go test ./common/duckdb ./develop/backend/internal/service ./service/backend/internal/service` 通过。

## 本轮不处理

- Transfer 模块中的空间编码 `geojson` 等概念。
- Manager preview 的 `builtin:scope-table` provider 名称。
- 旧数据库数据的迁移脚本。ADDP 当前不保留旧兼容，后续更推荐清空旧数据并重扫。
