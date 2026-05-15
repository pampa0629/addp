# ADDP 数据库大小写处理规范

## 问题背景

PostgreSQL 在处理 SQL 标识符（表名、列名、schema 名等）时有特殊的大小写规则：

1. **未加引号的标识符**会被自动转换为**小写**
   - `SELECT SmGeometry FROM table` → 实际查询 `smgeometry` 列
   - 如果实际列名是 `SmGeometry`（混合大小写），会报错："column smgeometry does not exist"

2. **加引号的标识符**会**保留原始大小写**
   - `SELECT "SmGeometry" FROM table` → 查询 `SmGeometry` 列（保留大小写）

3. **ADDP 平台处理的外部数据库**（如用户上传的 Shapefile、GeoJSON，或连接的外部数据库）可能包含混合大小写的表名和列名

## 核心规范

### 1. 所有动态 SQL 必须为标识符加引号

在 ADDP 平台中，**所有涉及动态构建 SQL 的地方**，必须为标识符（schema、表名、列名）加双引号，以保留大小写。

**❌ 错误示例**：
```go
query := fmt.Sprintf("SELECT %s FROM %s.%s", column, schema, table)
// 问题：column, schema, table 会被转换为小写
```

**✅ 正确示例**：
```go
query := fmt.Sprintf(`SELECT "%s" FROM "%s"."%s"`, column, schema, table)
// 正确：保留所有标识符的原始大小写
```

### 2. 使用 pq.QuoteIdentifier 保护标识符

对于需要防止 SQL 注入的场景，推荐使用 `pq.QuoteIdentifier`：

**✅ 推荐方式**：
```go
import "github.com/lib/pq"

qSchema := pq.QuoteIdentifier(schema)
qTable := pq.QuoteIdentifier(table)
qColumn := pq.QuoteIdentifier(column)

query := fmt.Sprintf(`SELECT %s FROM %s.%s`, qColumn, qSchema, qTable)
```

**优势**：
- 自动添加双引号保留大小写
- 转义特殊字符防止 SQL 注入
- 更安全、更规范

### 3. 特殊情况：JSONB 操作符

在 PostgreSQL 的 JSONB 操作中，字符串键需要**单引号**，而不是双引号：

**✅ 正确示例**：
```go
// 从 JSONB 中排除几何列
query := fmt.Sprintf(`
    SELECT
        to_jsonb(row.*) - '%s' - 'row_id' as properties
    FROM (SELECT * FROM "%s"."%s") row
`, geomColumn, schema, table)
```

**说明**：
- `to_jsonb(row.*) - '%s'`：这里的 `'%s'` 是 JSONB 的键名，使用单引号
- `"%s"."%s"`：这里是 SQL 标识符，使用双引号

### 4. 完整 SQL 查询模板示例

#### 4.1 基本查询
```go
query := fmt.Sprintf(`
    SELECT
        "%s",          -- 列名
        "%s"           -- 另一个列名
    FROM "%s"."%s"     -- schema.table
    WHERE "%s" = $1    -- 条件列
`, col1, col2, schema, table, whereColumn)
```

#### 4.2 空间数据查询
```go
query := fmt.Sprintf(`
    SELECT
        ST_AsGeoJSON(ST_Transform("%s", 4326)) as geojson,
        ST_SRID("%s") as srid
    FROM "%s"."%s"
    WHERE "%s" = $1
`, geomColumn, geomColumn, schema, table, primaryKey)
```

#### 4.3 聚合查询
```go
query := fmt.Sprintf(`
    SELECT
        COUNT(*) as count,
        ST_Extent(ST_Transform("%s", 4326)) as extent
    FROM "%s"."%s"
`, geomColumn, schema, table)
```

#### 4.4 复杂 JSONB 构建
```go
query := fmt.Sprintf(`
    SELECT jsonb_build_object(
        'type', 'FeatureCollection',
        'features', COALESCE(
            jsonb_agg(
                jsonb_build_object(
                    'type', 'Feature',
                    'id', row.row_id,
                    'geometry', ST_AsGeoJSON(row."%s")::jsonb,
                    'properties', to_jsonb(row.*) - '%s' - 'row_id'
                )
            ),
            '[]'::jsonb
        )
    ) as geojson
    FROM (
        SELECT
            row_number() OVER () as row_id,
            *
        FROM "%s"."%s"
        ORDER BY ctid
        LIMIT %d OFFSET %d
    ) row
`, geomColumn, geomColumn, schema, table, limit, offset)
```

**关键点**：
- `row."%s"`：访问行中的列，使用双引号
- `- '%s'`：JSONB 键名，使用单引号
- `"%s"."%s"`：schema.table，使用双引号

## 需要检查的模块和文件

### 高优先级（直接涉及动态 SQL）

1. **Manager 模块**
   - `manager/backend/internal/api/geojson_handler.go` ✅ 已修复
   - `manager/backend/internal/api/feature_handler.go` ✅ 使用了 pq.QuoteIdentifier
   - `manager/backend/internal/api/mvt_handler.go`
   - `manager/backend/internal/objectcontent/`
   - `manager/backend/internal/service/data_explorer.go`

2. **Meta 模块**
   - `meta/backend/internal/service/scan_service.go`
   - `meta/backend/internal/service/metadata_query_service.go`
   - `meta/backend/internal/plugins/*/scanner.go`

3. **Transfer 模块**
   - `transfer/backend/plugins/readers/*_reader.go`
   - `transfer/backend/plugins/writers/*_writer.go`

4. **Develop 模块**
   - `develop/backend/internal/service/sql_execution_service.go`
   - `develop/backend/internal/api/sql_handler.go`

5. **Service 模块**
   - `service/backend/internal/service/data_query_service.go`
   - `service/backend/internal/api/ogc_handler.go`

### 中优先级（间接涉及）

6. **Common 模块**
   - `common/engine/plugins/*/plugin.go`
   - `common/format/*/parser.go`

### 搜索关键词

使用以下关键词搜索潜在问题：
```bash
# 查找未加引号的 SQL 标识符
grep -rn "FROM %s\.%s" --include="*.go"
grep -rn "SELECT.*%s.*FROM" --include="*.go"
grep -rn "WHERE.*%s.*=" --include="*.go"
grep -rn "ST_.*(%s" --include="*.go"

# 查找可能的动态 SQL 构建
grep -rn "fmt.Sprintf.*SELECT" --include="*.go"
grep -rn "fmt.Sprintf.*FROM" --include="*.go"
grep -rn "db.Raw\|db.Exec\|db.Query" --include="*.go"
```

## 测试验证

### 1. 测试数据集

使用包含混合大小写的测试数据：
- 表名：`TestTable`、`MyData_V2`
- 列名：`SmGeometry`、`UserName`、`CreateTime`
- Schema：`PublicData`、`UserSchema`

### 2. 验证场景

- [ ] GeoJSON 数据预览
- [ ] 要素详情查询
- [ ] MVT 瓦片生成
- [ ] 数据导入导出
- [ ] 元数据扫描
- [ ] SQL 执行
- [ ] OGC 服务查询

### 3. 错误特征

如果出现以下错误，说明存在大小写问题：
```
ERROR: column "xxx" does not exist
ERROR: relation "xxx" does not exist
ERROR: schema "xxx" does not exist
SQLSTATE 42703  -- 列不存在
SQLSTATE 42P01  -- 表/视图不存在
SQLSTATE 3F000  -- schema 不存在
```

## 修复检查清单

- [x] Manager - GeoJSON handler
- [ ] Manager - MVT handler
- [x] Manager - Object preview service (已使用双引号)
- [ ] Manager - Data explorer service
- [x] Meta - Scan service (已使用双引号)
- [x] Meta - Metadata query service (已使用双引号)
- [ ] Meta - 各类 scanner 插件
- [x] Transfer - JDBC Reader (已添加 quoteIdentifier 方法)
- [ ] Transfer - Writer 插件
- [x] Transfer - SpatiaLite Reader (已使用 quoteIdentSQLite)
- [x] Transfer - SpatiaLite Parallel Reader (已使用 quoteIdentSQLite)
- [x] Transfer - GeoPackage Reader (已使用 quoteIdentSQLite)
- [x] Transfer - PostgreSQL COPY Writer (已使用 quoteIdentifier)
- [ ] Develop - SQL execution service
- [ ] Service - Data query service
- [ ] Service - OGC handler
- [x] Common - Spatial query.go (已使用 pq.QuoteIdentifier)
- [x] Common - Spark SQL plugin (已添加 quoteSparkIdentifier 函数)
- [ ] Common - Format parsers

## 参考资料

- [PostgreSQL 标识符规则](https://www.postgresql.org/docs/current/sql-syntax-lexical.html#SQL-SYNTAX-IDENTIFIERS)
- [pq.QuoteIdentifier 文档](https://pkg.go.dev/github.com/lib/pq#QuoteIdentifier)

## 版本历史

| 版本 | 日期 | 修改内容 |
|------|------|---------|
| 1.0  | 2026-01-17 | 初始版本，定义规范和检查清单 |
| 1.1  | 2026-01-17 | 完成高优先级和中优先级修复：<br>- common/spatial/query.go<br>- transfer JDBC reader<br>- transfer SpatiaLite readers<br>- transfer GeoPackage reader<br>- common Spark SQL plugin |
