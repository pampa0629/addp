# SQL Builder - PostgreSQL 安全 SQL 构建工具

## 📖 概述

`sqlbuilder` 是 ADDP 平台的统一 SQL 构建工具库，专门用于解决 PostgreSQL 标识符大小写敏感性问题。

### 核心问题

PostgreSQL 中不带引号的标识符会自动转换为小写：

```sql
-- ❌ 错误：会被转换为小写
SELECT SmID, SmGeometry FROM public.dltb

-- ✅ 正确：使用双引号保留大小写
SELECT "SmID", "SmGeometry" FROM "public"."dltb"
```

### 常见场景

- **SuperMap 数据**：`SmID`、`SmGeometry`
- **ArcGIS 数据**：`OBJECTID`、`SHAPE`
- **PostGIS 标准**：`id`、`geom`
- **QGIS 导入**：`fid`、`geometry`

## 🚀 快速开始

### 安装

```go
import "github.com/addp/common/sqlbuilder"
```

### 基础用法

```go
// 1. 引用单个标识符
column := sqlbuilder.QuoteIdentifier("SmID")
// 结果: "SmID"

// 2. 引用多个标识符
columns := sqlbuilder.QuoteIdentifiers([]string{"SmID", "Name", "SmGeometry"})
// 结果: ["SmID", "Name", "SmGeometry"]

// 3. 生成完整表名
tableName := sqlbuilder.QualifiedTableName("public", "MyTable")
// 结果: "public"."MyTable"

// 4. 几何转换
geomTransform := sqlbuilder.GeometryTransform("SmGeometry", 3857)
// 结果: ST_Transform("SmGeometry", 3857)
```

## 📚 API 文档

### 标识符引用

#### QuoteIdentifier

使用双引号包裹标识符以保留大小写。

```go
func QuoteIdentifier(identifier string) string
```

**示例**：
```go
QuoteIdentifier("SmID")           // "SmID"
QuoteIdentifier("my\"column")     // "my""column" (转义内部双引号)
```

#### QuoteIdentifiers

批量引用多个标识符。

```go
func QuoteIdentifiers(identifiers []string) []string
```

**示例**：
```go
QuoteIdentifiers([]string{"SmID", "Name"})  // ["SmID", "Name"]
```

#### QualifiedTableName

生成安全的 `"schema"."table"` 格式。

```go
func QualifiedTableName(schema, table string) string
```

**示例**：
```go
QualifiedTableName("public", "MyTable")  // "public"."MyTable"
QualifiedTableName("", "MyTable")        // "MyTable"
```

### 空间函数

#### GeometryTransform

生成安全的几何转换 SQL。

```go
func GeometryTransform(geomColumn string, targetSRID int) string
```

**示例**：
```go
GeometryTransform("SmGeometry", 3857)  // ST_Transform("SmGeometry", 3857)
```

### SQL 语句构建

#### CreateTableSQL

生成 CREATE TABLE 语句。

```go
func CreateTableSQL(schema, table string, columnDefs []string, ifNotExists bool) string
```

**示例**：
```go
sql := sqlbuilder.CreateTableSQL(
    "public",
    "MyTable",
    []string{
        `"SmID" SERIAL PRIMARY KEY`,
        `"Name" VARCHAR(100)`,
        `"SmGeometry" geometry(Point, 4326)`,
    },
    true,
)
// 结果:
// CREATE TABLE IF NOT EXISTS "public"."MyTable" (
//     "SmID" SERIAL PRIMARY KEY,
//     "Name" VARCHAR(100),
//     "SmGeometry" geometry(Point, 4326)
// )
```

#### CreateIndexSQL

生成 CREATE INDEX 语句。

```go
func CreateIndexSQL(indexName, schema, table string, columns []string, indexType string, concurrently bool) string
```

**示例**：
```go
sql := sqlbuilder.CreateIndexSQL(
    "idx_geom",
    "public",
    "MyTable",
    []string{"SmGeometry"},
    "GIST",
    true,
)
// 结果:
// CREATE INDEX CONCURRENTLY "idx_geom" ON "public"."MyTable" USING GIST ("SmGeometry")
```

#### SelectSQL

生成 SELECT 语句。

```go
func SelectSQL(columns []string, schema, table string, whereConditions []string, orderBy string, limit, offset int) string
```

**示例**：
```go
sql := sqlbuilder.SelectSQL(
    []string{"SmID", "Name"},
    "public",
    "dltb",
    []string{`"SmID" > 100`},
    `"SmID" ASC`,
    10,
    0,
)
// 结果:
// SELECT "SmID", "Name" FROM "public"."dltb" WHERE "SmID" > 100 ORDER BY "SmID" ASC LIMIT 10
```

#### InsertSQL

生成 INSERT 语句。

```go
func InsertSQL(schema, table string, columns []string, placeholders int, useNumberedPlaceholders bool) string
```

**示例**：
```go
sql := sqlbuilder.InsertSQL(
    "public",
    "MyTable",
    []string{"SmID", "Name"},
    2,
    true,  // PostgreSQL 使用 $1, $2
)
// 结果:
// INSERT INTO "public"."MyTable" ("SmID", "Name") VALUES ($1, $2)
```

#### AnalyzeTableSQL

生成 ANALYZE 语句。

```go
func AnalyzeTableSQL(schema, table string) string
```

**示例**：
```go
sql := sqlbuilder.AnalyzeTableSQL("public", "MyTable")
// 结果: ANALYZE "public"."MyTable"
```

#### DropTableSQL

生成 DROP TABLE 语句。

```go
func DropTableSQL(schema, table string, ifExists, cascade bool) string
```

**示例**：
```go
sql := sqlbuilder.DropTableSQL("public", "MyTable", true, true)
// 结果: DROP TABLE IF EXISTS "public"."MyTable" CASCADE
```

## 🎯 实际应用场景

### 场景 1：Manager MVT 物化视图创建

```go
import "github.com/addp/common/sqlbuilder"

// 构建 MVT 物化视图
schema := "public"
table := "dltb"
mvName := "dltb_mv3857"
primaryKey := "SmID"
geomColumn := "SmGeometry"

createMVSQL := fmt.Sprintf(`
    CREATE MATERIALIZED VIEW %s AS
    SELECT
        %s,
        %s AS geom_3857
    FROM %s
    WHERE %s IS NOT NULL
`,
    sqlbuilder.QualifiedTableName(schema, mvName),
    sqlbuilder.QuoteIdentifier(primaryKey),
    sqlbuilder.GeometryTransform(geomColumn, 3857),
    sqlbuilder.QualifiedTableName(schema, table),
    sqlbuilder.QuoteIdentifier(geomColumn),
)
```

### 场景 2：Service OGC 要素查询

```go
// 构建 WFS GetFeature 查询
columns := []string{"id", "name", "SmGeometry"}
schema := "public"
table := "features"
geomColumn := "SmGeometry"

selectCols := fmt.Sprintf(`id, ST_AsGeoJSON(%s) as geometry, %s`,
    sqlbuilder.GeometryTransform(geomColumn, 4326),
    sqlbuilder.SelectColumns([]string{"name"}),
)

query := fmt.Sprintf(`SELECT %s FROM %s`,
    selectCols,
    sqlbuilder.QualifiedTableName(schema, table),
)
```

### 场景 3：Transfer 数据导入

```go
// 创建目标表
columnDefs := []string{
    fmt.Sprintf(`%s SERIAL PRIMARY KEY`, sqlbuilder.QuoteIdentifier("SmID")),
    fmt.Sprintf(`%s VARCHAR(100)`, sqlbuilder.QuoteIdentifier("Name")),
    fmt.Sprintf(`%s geometry(Point, 4326)`, sqlbuilder.QuoteIdentifier("SmGeometry")),
}

createSQL := sqlbuilder.CreateTableSQL("public", "imported_data", columnDefs, true)

// 创建空间索引
indexSQL := sqlbuilder.CreateIndexSQL(
    "idx_imported_geom",
    "public",
    "imported_data",
    []string{"SmGeometry"},
    "GIST",
    true,
)
```

## ⚠️ 注意事项

### 1. 何时使用双引号

**必须使用**：
- 混合大小写的列名（`SmID`、`SmGeometry`）
- 全大写的列名（`OBJECTID`、`SHAPE`）
- 包含特殊字符的列名

**可以不用**：
- 全小写的列名（`id`、`name`）
- 但为了一致性，建议始终使用

### 2. 双引号转义

如果标识符本身包含双引号，会自动转义：

```go
QuoteIdentifier(`my"column`)  // "my""column"
```

### 3. WHERE 条件中的列名

WHERE 条件中的列名也需要引用：

```go
// ❌ 错误
whereConditions := []string{"SmID > 100"}

// ✅ 正确
whereConditions := []string{
    fmt.Sprintf(`%s > 100`, sqlbuilder.QuoteIdentifier("SmID")),
}
```

## 🧪 测试

运行单元测试：

```bash
cd common
go test ./sqlbuilder/... -v
```

测试覆盖率：

```bash
go test ./sqlbuilder/... -cover
```

## 📖 相关文档

- [ADDP 常见故障排查](../../docs/addp常见故障排查.md) - MVT 物化视图创建失败问题
- [字段名大小写梳理](../../docs/字段名的大小写梳理.md) - 完整的排查和修复计划
- [PostgreSQL 标识符文档](https://www.postgresql.org/docs/current/sql-syntax-lexical.html#SQL-SYNTAX-IDENTIFIERS)

## 🤝 贡献

在使用过程中发现问题或有改进建议，请：

1. 在对应模块中使用 `sqlbuilder` 替代手动拼接 SQL
2. 添加单元测试覆盖新场景
3. 更新本文档的示例

## 📝 更新日志

### v1.0.0 (2026-01-30)

- ✨ 初始版本
- ✅ 支持所有常用 SQL 语句构建
- ✅ 完整的单元测试覆盖
- ✅ 真实场景集成测试
