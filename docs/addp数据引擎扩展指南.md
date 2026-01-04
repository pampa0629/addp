# ADDP 数据引擎扩展指南

本文档提供 ADDP 平台数据引擎扩展的完整指南,包括插件系统架构、实施步骤和各模块集成要点。

---

## 📋 快速概览

添加新数据引擎支持需要以下步骤:

1. **创建数据库插件**（150-400 行代码）
2. **注册插件到 DBBridge**（1 行代码）
3. **添加扫描插件**（100-300 行代码）
4. **添加预览插件**（50-150 行代码）
5. **前端适配**（可选,根据特殊需求）
6. **集成测试**（6-10 行代码）

**总工作量：4-10 小时**

---

## 🏗️ 架构概览

ADDP 数据引擎支持基于三层插件化架构:

```
┌──────────────────────────────────────────────────────┐
│   应用层 (System/Manager/Meta/Transfer/Develop)      │
│   - 调用 DBBridge/Meta/Manager 统一接口             │
└────────────┬─────────────────────────────────────────┘
             │
┌────────────▼─────────────────────────────────────────┐
│   DBBridge Facade (common/dbbridge/)                 │
│   - 数据库连接测试                                    │
│   - 元数据查询入口                                    │
│   - 自动导入所有数据库插件                            │
└────────────┬─────────────────────────────────────────┘
             │
┌────────────▼─────────────────────────────────────────┐
│   Database Plugin Registry (common/database/plugin/) │
│   - 插件注册表（线程安全）                            │
│   - 连接池管理                                        │
│   - 类型映射工具                                      │
└────────────┬─────────────────────────────────────────┘
             │
┌────────────▼─────────────────────────────────────────┐
│   具体数据库插件 (common/database/plugins/*)          │
│   - PostgreSQL、MySQL、Doris、ClickHouse、MongoDB... │
│   - 每个插件独立实现接口                              │
└─────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────┐
│   Meta 扫描系统 (meta/plugins/scanners/)             │
│   - Scanner 插件（数据库元数据扫描）                  │
│   - Extractor 插件（对象存储文件内容提取）            │
└──────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────┐
│   Manager 预览系统 (manager/service/preview_*)        │
│   - PreviewProvider 插件（数据预览）                  │
│   - 基于引擎类型动态选择预览实现                      │
└──────────────────────────────────────────────────────┘
```

---

## 🗂️ 当前支持的数据引擎

截至 **v0.0.18** (2025-12-25)，ADDP 平台支持 **8 种**数据引擎:

### 关系型数据库 (OLTP/OLAP)

| 数据库 | 类型 | 默认端口 | 驱动 | 用途 |
|--------|------|----------|------|------|
| **PostgreSQL** | OLTP | 5432 | github.com/lib/pq | 系统主数据库，存储用户、引擎、元数据等 |
| **MySQL** | OLTP | 3306 | github.com/go-sql-driver/mysql | 通用关系型数据库 |
| **Apache Doris** | HTAP | 9030 | go-sql-driver (MySQL兼容) | 实时分析数据库，支持OLAP查询 |
| **ClickHouse** | OLAP | 9000 | github.com/ClickHouse/clickhouse-go/v2 | 高性能列式存储，适合大数据量分析 |

### NoSQL 数据库

| 数据库 | 类型 | 默认端口 | 驱动 | 用途 |
|--------|------|----------|------|------|
| **MongoDB** | 文档型 | 27017 | go.mongodb.org/mongo-driver | 文档型NoSQL数据库，支持灵活Schema |

### 计算引擎

| 引擎 | 类型 | 默认端口 | 驱动 | 用途 |
|------|------|----------|------|------|
| **Apache Spark** | 分布式计算 | 10000 | github.com/beltran/gohive | 大数据分布式查询引擎 |

### 对象存储

| 存储 | 类型 | 默认端口 | 驱动 | 用途 |
|------|------|----------|------|------|
| **MinIO** | 对象存储 | 9000 | github.com/minio/minio-go | S3兼容的对象存储，存储文件和二进制数据 |
| **Amazon S3** | 对象存储 | 443 | github.com/minio/minio-go | AWS云对象存储 |

---

## 🚀 实施步骤

### 第一步：创建数据库插件 (Common 模块)

#### 1.1 创建插件目录

```bash
mkdir -p common/database/plugins/<dbtype>/
cd common/database/plugins/<dbtype>/
```

#### 1.2 实现插件接口

创建 `plugin.go`，实现 `DatabasePlugin` 接口:

```go
package <dbtype>

import (
    "context"
    "database/sql"
    "fmt"
    "time"

    "github.com/addp/common/database/plugin"
    _ "<driver_import>" // 导入数据库驱动
    "<gorm_driver_import>" // 导入 GORM 驱动（如果支持）
    "gorm.io/gorm"
)

type <DBType>Plugin struct{}

// init 函数自动注册插件
func init() {
    plugin.Register(&<DBType>Plugin{})
}

// ========== 实现 DatabasePlugin 接口（10个必须方法） ==========

func (p *<DBType>Plugin) Type() string {
    return "<dbtype>"
}

func (p *<DBType>Plugin) DisplayName() string {
    return "<Display Name>"
}

func (p *<DBType>Plugin) EngineCategory() string {
    // 选择：standard (标准引擎，如 PostgreSQL/MySQL) 或 extension (扩展引擎，如工作流引擎)
    return "standard"
}

func (p *<DBType>Plugin) DefaultPort() int {
    return <默认端口>
}

func (p *<DBType>Plugin) RequiredFields() []string {
    return []string{"host", "user", "database"}
}

func (p *<DBType>Plugin) SensitiveFields() []string {
    return []string{"password"}
}

func (p *<DBType>Plugin) GenerateCapabilities() string {
    // compute 中不需要 "type" 字段，使用 dev_modes 标识开发方式
    return `{"storage":[{"type":"relational_db","engine":"<dbtype>","supports_query":true}],"compute":[{"dev_modes":["sql"],"description":"SQL查询"}]}`
}

func (p *<DBType>Plugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
    return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

func (p *<DBType>Plugin) BuildConnectionString(connInfo plugin.ConnectionInfo) (string, error) {
    host := plugin.NormalizeHost(plugin.GetString(connInfo, "host"))
    port := plugin.GetInt(connInfo, "port")
    if port == 0 {
        port = p.DefaultPort()
    }

    user := plugin.GetString(connInfo, "user")
    password := plugin.GetString(connInfo, "password")
    database := plugin.GetString(connInfo, "database")

    if host == "" || user == "" {
        return "", fmt.Errorf("missing required connection info")
    }

    // 使用工具函数构建 DSN
    return plugin.MySQLStyleDSN(user, password, host, port, database, map[string]string{
        "parseTime": "true",
        "timeout":   "10s",
    }), nil
}

func (p *<DBType>Plugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
    connStr, err := p.BuildConnectionString(connInfo)
    if err != nil {
        return fmt.Errorf("failed to build connection string: %w", err)
    }

    db, err := sql.Open("<driver_name>", connStr)
    if err != nil {
        return fmt.Errorf("failed to open connection: %w", err)
    }
    defer db.Close()

    testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
    defer cancel()

    if err := db.PingContext(testCtx); err != nil {
        return fmt.Errorf("failed to ping database: %w", err)
    }

    return nil
}

// ========== 实现可选接口（根据需要） ==========

// ConnectionPoolPlugin 接口（SQL数据库推荐实现）
func (p *<DBType>Plugin) CreateConnectionPool(connInfo plugin.ConnectionInfo, poolConfig *plugin.PoolConfig) (*gorm.DB, error) {
    connStr, err := p.BuildConnectionString(connInfo)
    if err != nil {
        return nil, fmt.Errorf("failed to build connection string: %w", err)
    }

    db, err := gorm.Open(<gorm_driver>.Open(connStr), &gorm.Config{
        DisableAutomaticPing: false,
    })
    if err != nil {
        return nil, fmt.Errorf("failed to create gorm connection: %w", err)
    }

    sqlDB, err := db.DB()
    if err != nil {
        return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
    }

    sqlDB.SetMaxOpenConns(poolConfig.MaxOpenConns)
    sqlDB.SetMaxIdleConns(poolConfig.MaxIdleConns)
    sqlDB.SetConnMaxLifetime(poolConfig.ConnMaxLifetime)

    return db, nil
}

func (p *<DBType>Plugin) GetDialect() string {
    return "<dialect_name>"
}

// MetadataPlugin 接口（元数据查询支持）
// 参考 postgresql/plugin.go 实现
func (p *<DBType>Plugin) ListSchemas(ctx context.Context, db *gorm.DB) ([]plugin.SchemaInfo, error) {
    // 实现 schema 列表查询
    var schemas []plugin.SchemaInfo
    // ... 查询逻辑 ...
    return schemas, nil
}

func (p *<DBType>Plugin) ListTables(ctx context.Context, db *gorm.DB, schema string) ([]plugin.TableInfo, error) {
    // 实现表列表查询
    var tables []plugin.TableInfo
    // ... 查询逻辑 ...
    return tables, nil
}

func (p *<DBType>Plugin) ListColumns(ctx context.Context, db *gorm.DB, schema, table string) ([]plugin.ColumnInfo, error) {
    var columns []plugin.ColumnInfo
    // ... 查询列信息 ...

    // 使用通用类型映射工具
    for i := range columns {
        columns[i].StdType = plugin.MapToStandardType(columns[i].DataType)
    }

    return columns, nil
}

func (p *<DBType>Plugin) GetTableRowCount(ctx context.Context, db *gorm.DB, schema, table string) (int64, error) {
    // 实现行数统计查询
    var count int64
    // ... 查询逻辑 ...
    return count, nil
}
```

#### 1.3 注册插件到 DBBridge

编辑 `common/dbbridge/bridge.go`，添加导入:

```go
import (
    _ "github.com/addp/common/database/plugins/postgresql"
    _ "github.com/addp/common/database/plugins/mysql"
    _ "github.com/addp/common/database/plugins/doris"
    _ "github.com/addp/common/database/plugins/clickhouse"
    _ "github.com/addp/common/database/plugins/<dbtype>" // 👈 添加这一行
)
```

---

### 第二步：创建扫描插件 (Meta 模块)

#### 2.1 创建扫描器目录

```bash
mkdir -p meta/plugins/scanners/<dbtype>/
cd meta/plugins/scanners/<dbtype>/
```

#### 2.2 实现 Scanner 接口

创建 `scanner.go`:

```go
package <dbtype>

import (
    "context"
    "database/sql"
    "fmt"

    "github.com/addp/common/models"
    "github.com/addp/meta/plugins"
    _ "<driver_import>" // 导入数据库驱动
)

type Scanner struct {
    db     *sql.DB
    engine *models.Engine
}

// init 函数自动注册扫描器
func init() {
    plugins.RegisterScanner("<dbtype>", NewScanner)
}

func NewScanner(engine *models.Engine) (plugins.Scanner, error) {
    // 使用 common/database/plugin 构建连接字符串
    dbPlugin := plugin.Get("<dbtype>")
    if dbPlugin == nil {
        return nil, fmt.Errorf("database plugin not found for type: <dbtype>")
    }

    connStr, err := dbPlugin.BuildConnectionString(engine.ConnectionInfo)
    if err != nil {
        return nil, err
    }

    db, err := sql.Open("<driver_name>", connStr)
    if err != nil {
        return nil, err
    }

    // 测试连接
    if err := db.Ping(); err != nil {
        db.Close()
        return nil, err
    }

    return &Scanner{
        db:     db,
        engine: engine,
    }, nil
}

func (s *Scanner) Close() error {
    if s.db != nil {
        return s.db.Close()
    }
    return nil
}

func (s *Scanner) ListSchemas() ([]plugins.SchemaInfo, error) {
    query := `SELECT schema_name FROM information_schema.schemata`
    rows, err := s.db.Query(query)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var schemas []plugins.SchemaInfo
    for rows.Next() {
        var name string
        if err := rows.Scan(&name); err != nil {
            return nil, err
        }
        schemas = append(schemas, plugins.SchemaInfo{Name: name})
    }
    return schemas, rows.Err()
}

func (s *Scanner) ScanTables(schemaName string) ([]plugins.TableInfo, error) {
    query := `
        SELECT
            table_name,
            table_type,
            table_comment,
            table_rows,
            data_length
        FROM information_schema.tables
        WHERE table_schema = ?
    `

    rows, err := s.db.Query(query, schemaName)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var tables []plugins.TableInfo
    for rows.Next() {
        var table plugins.TableInfo
        var rowCount, sizeBytes sql.NullInt64
        var tableType, comment sql.NullString

        err := rows.Scan(
            &table.Name,
            &tableType,
            &comment,
            &rowCount,
            &sizeBytes,
        )
        if err != nil {
            return nil, err
        }

        if tableType.Valid {
            table.Type = tableType.String
        }
        if comment.Valid {
            table.Comment = comment.String
        }
        if rowCount.Valid {
            table.RowCount = rowCount.Int64
        }
        if sizeBytes.Valid {
            table.SizeBytes = sizeBytes.Int64
        }

        tables = append(tables, table)
    }
    return tables, rows.Err()
}

func (s *Scanner) ScanFields(schemaName, tableName string) ([]plugins.FieldInfo, error) {
    query := `
        SELECT
            column_name,
            ordinal_position,
            data_type,
            column_type,
            is_nullable,
            column_default,
            column_comment,
            column_key
        FROM information_schema.columns
        WHERE table_schema = ? AND table_name = ?
        ORDER BY ordinal_position
    `

    rows, err := s.db.Query(query, schemaName, tableName)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var fields []plugins.FieldInfo
    for rows.Next() {
        var field plugins.FieldInfo
        var nullable, defaultValue, comment, columnKey sql.NullString

        err := rows.Scan(
            &field.Name,
            &field.OrdinalPosition,
            &field.DataType,
            &field.ColumnType,
            &nullable,
            &defaultValue,
            &comment,
            &columnKey,
        )
        if err != nil {
            return nil, err
        }

        field.IsNullable = nullable.Valid && nullable.String == "YES"
        if defaultValue.Valid {
            field.DefaultValue = defaultValue.String
        }
        if comment.Valid {
            field.Comment = comment.String
        }
        if columnKey.Valid {
            field.IsPrimaryKey = columnKey.String == "PRI"
            field.IsUniqueKey = columnKey.String == "UNI"
        }

        fields = append(fields, field)
    }
    return fields, rows.Err()
}
```

#### 2.3 注册扫描器

编辑 `meta/plugins/scanners/register.go`:

```go
import (
    _ "github.com/addp/meta/plugins/scanners/postgresql"
    _ "github.com/addp/meta/plugins/scanners/mysql"
    _ "github.com/addp/meta/plugins/scanners/<dbtype>" // 👈 添加这一行
)
```

---

### 第三步：创建预览插件 (Manager 模块)

#### 3.1 创建预览提供者文件

创建 `manager/backend/internal/service/preview_provider_<dbtype>.go`:

```go
package service

import (
    "context"
    "fmt"

    "github.com/addp/manager/internal/models"
    "github.com/addp/manager/internal/repository"
)

type <dbtype>PreviewProvider struct {
    metadataRepo *repository.MetadataRepository
    priority     int
}

// init 函数自动注册预览插件
func init() {
    RegisterPreviewProvider("<dbtype>", func(
        metadataRepo *repository.MetadataRepository,
        metaClient *commonClient.MetaClient,
        metaServiceURL string,
        contentRegistry *ObjectContentRegistry,
    ) (PreviewProvider, error) {
        return New<DBType>PreviewProvider(metadataRepo), nil
    })
}

func New<DBType>PreviewProvider(metadataRepo *repository.MetadataRepository) PreviewProvider {
    return &<dbtype>PreviewProvider{
        metadataRepo: metadataRepo,
        priority:     100,
    }
}

func (p *<dbtype>PreviewProvider) Name() string {
    return "builtin:<dbtype>-table"
}

func (p *<dbtype>PreviewProvider) Priority() int {
    return p.priority
}

func (p *<dbtype>PreviewProvider) Supports(req *PreviewRequest) bool {
    if req == nil || req.Engine == nil {
        return false
    }
    if req.Schema == "" || req.Table == "" {
        return false
    }

    resourceType := sanitizeResourceType(req.Engine.EngineType)
    return resourceType == "<dbtype>"
}

func (p *<dbtype>PreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
    const maxRows = 50

    // 查询数据预览
    columns, rows, total, geometryColumns, err := p.metadataRepo.Query<DBType>TablePreview(
        req.Engine,
        req.Schema,
        req.Table,
        req.Page,
        req.PageSize,
        maxRows,
    )
    if err != nil {
        return nil, fmt.Errorf("<dbtype> preview query failed: %w", err)
    }

    return &models.TablePreview{
        Mode:            PreviewModeTable,
        Columns:         columns,
        Rows:            rows,
        Total:           total,
        Page:            req.Page,
        PageSize:        req.PageSize,
        GeometryColumns: geometryColumns,
        EngineID:        req.Engine.ID,
        Schema:          req.Schema,
        Table:           req.Table,
        EngineType:      req.Engine.EngineType,
    }, nil
}
```

#### 3.2 实现数据查询方法

在 `manager/backend/internal/repository/metadata_repository.go` 添加查询方法:

```go
func (r *MetadataRepository) Query<DBType>TablePreview(
    engine *models.Engine,
    schemaName, tableName string,
    page, pageSize, maxRows int,
) ([]string, []map[string]interface{}, int64, []string, error) {
    // 使用 common/database/plugin 创建连接
    dbPlugin := plugin.Get("<dbtype>")
    if dbPlugin == nil {
        return nil, nil, 0, nil, fmt.Errorf("database plugin not found")
    }

    connStr, err := dbPlugin.BuildConnectionString(engine.ConnectionInfo)
    if err != nil {
        return nil, nil, 0, nil, err
    }

    db, err := sql.Open("<driver_name>", connStr)
    if err != nil {
        return nil, nil, 0, nil, err
    }
    defer db.Close()

    // 构建查询
    fullTableName := fmt.Sprintf("%s.%s", schemaName, tableName)
    offset := (page - 1) * pageSize
    query := fmt.Sprintf("SELECT * FROM %s LIMIT %d OFFSET %d", fullTableName, pageSize, offset)

    rows, err := db.Query(query)
    if err != nil {
        return nil, nil, 0, nil, err
    }
    defer rows.Close()

    // 获取列名
    columns, err := rows.Columns()
    if err != nil {
        return nil, nil, 0, nil, err
    }

    // 读取数据行
    var data []map[string]interface{}
    for rows.Next() {
        values := make([]interface{}, len(columns))
        valuePtrs := make([]interface{}, len(columns))
        for i := range values {
            valuePtrs[i] = &values[i]
        }

        if err := rows.Scan(valuePtrs...); err != nil {
            return nil, nil, 0, nil, err
        }

        row := make(map[string]interface{})
        for i, col := range columns {
            row[col] = values[i]
        }
        data = append(data, row)
    }

    // 查询总数
    var total int64
    countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s", fullTableName)
    db.QueryRow(countQuery).Scan(&total)

    return columns, data, total, []string{}, nil
}
```

---

### 第四步：前端适配（可选）

大多数情况下，前端无需修改即可支持新数据引擎。如有特殊需求：

#### 4.1 添加引擎类型图标

编辑 `manager/frontend/src/views/DataExplorer.vue`:

```vue
<script setup>
const getEngineIcon = (type) => {
  const iconMap = {
    postgresql: 'database',
    mysql: 'database',
    clickhouse: 'chart-column',
    mongodb: 'leaf',
    '<dbtype>': '<icon-name>', // 👈 添加图标映射
    minio: 'box',
    s3: 'cloud'
  }
  return iconMap[type?.toLowerCase()] || 'database'
}
</script>
```

#### 4.2 添加特殊预览逻辑（如需要）

如果需要特殊的预览处理（如空间数据、二进制数据等），参考:
- [manager/frontend/src/views/DataExplorer.vue](/manager/frontend/src/views/DataExplorer.vue) - 预览组件
- [common-frontend/map/src/components/TablePreview.vue](/common-frontend/map/src/components/TablePreview.vue) - 通用表格预览组件

---

### 第五步：集成测试

#### 5.1 添加数据库插件测试

编辑 `common/database/plugins/integration_test.go`:

```go
func Test<DBType>PluginRegistration(t *testing.T) {
    p := plugin.Get("<dbtype>")
    assert.NotNil(t, p, "<DBType> plugin should be registered")
    assert.Equal(t, "<Display Name>", p.DisplayName())
    assert.Equal(t, <默认端口>, p.DefaultPort())
}
```

#### 5.2 运行测试

```bash
# 构建 common 模块
cd common
go build ./...

# 运行数据库插件测试
go test ./database/plugins/integration_test.go -v

# 构建 meta 模块（扫描器）
cd ../meta/backend
go build ./...

# 构建 manager 模块（预览）
cd ../../manager/backend
go build ./...

# 重启开发环境验证
cd ../..
bash scripts/dev/restart.sh -all
```

#### 5.3 功能验证

1. **连接测试**:
   - 在 System 模块添加新引擎
   - 测试连接是否成功

2. **元数据扫描**:
   - 在 Manager 模块触发扫描
   - 验证 Schema/Table/Field 是否正确提取

3. **数据预览**:
   - 在 Data Explorer 点击表
   - 验证数据预览是否正常显示

4. **跨模块集成**:
   - Transfer 模块: 验证数据导入/导出
   - Develop 模块: 验证 SQL 执行
   - Service 模块: 验证数据服务发布

---

## 🛠️ 常用工具函数

### 1. 连接信息提取

```go
host := plugin.NormalizeHost(plugin.GetString(connInfo, "host"))
port := plugin.GetInt(connInfo, "port")
user := plugin.GetString(connInfo, "user")
password := plugin.GetString(connInfo, "password")
database := plugin.GetString(connInfo, "database")
```

### 2. 字段验证

```go
err := plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
```

### 3. DSN 构建

```go
// MySQL 风格（MySQL、Doris、TiDB）
dsn := plugin.MySQLStyleDSN(user, password, host, port, database, map[string]string{
    "parseTime": "true",
    "timeout":   "10s",
})

// PostgreSQL 风格
dsn := plugin.PostgreSQLStyleDSN(user, password, host, port, database, "disable")

// ClickHouse 风格
dsn := plugin.ClickHouseStyleDSN(user, password, host, port, database, map[string]string{
    "dial_timeout": "10s",
})

// MongoDB 风格
dsn := plugin.MongoDBStyleDSN(user, password, host, port, database)
```

### 4. 类型映射

```go
// 使用通用映射表（推荐）
stdType := plugin.MapToStandardType("varchar")  // 返回 "string"
stdType := plugin.MapToStandardType("bigint")   // 返回 "integer"

// 使用自定义映射表
customMappings := []plugin.TypeMapping{
    {DBTypes: []string{"hll", "bitmap"}, StdType: plugin.StdTypeBinary},
}
stdType := plugin.MapToStandardTypeWithCustom("hll", customMappings)
```

---

## 📊 模块集成说明

### System 模块

**作用**: 引擎注册、连接测试、租户管理

**集成点**:
- `common/dbbridge/bridge.go`: 自动调用数据库插件的 `TestConnection()`
- 前端无需修改，表单自动根据 `RequiredFields()` 生成

### Meta 模块

**作用**: 元数据扫描、索引、向量化

**集成点**:
- Scanner 插件实现 `ListSchemas()`, `ScanTables()`, `ScanFields()`
- 自动触发 Meilisearch 索引
- 支持向量化（对象存储）

**扫描深度**:
- `basic`: 只列文件名/表名（不提取字段）
- `deep`: 完整元数据提取（字段、类型、注释等）

### Manager 模块

**作用**: 数据预览、文件预览、对象浏览

**集成点**:
- PreviewProvider 插件实现 `Preview()`
- 前端 `DataExplorer.vue` 自动适配

**预览模式**:
- `PreviewModeTable`: 表格预览（关系数据库）
- `PreviewModeObject`: 文件预览（对象存储）
- `PreviewModeNode`: 目录/Schema 统计信息

### Transfer 模块

**作用**: 数据导入、导出、同步

**集成点**:
- 自动从 Meta 获取表字段信息（`GetTableFieldDetails()`）
- 使用 `common/database/plugin` 创建连接池

### Develop 模块

**作用**: SQL 执行、GIS 工作流

**集成点**:
- 使用 `common/database/plugin` 创建连接
- 支持所有实现 `ConnectionPoolPlugin` 的数据库

### Service 模块

**作用**: 数据服务发布、OGC 标准

**集成点**:
- MVT 服务需要空间元数据（PostgreSQL/PostGIS）
- 通用数据查询服务支持所有 SQL 数据库

---

## 📚 参考实现

### 简单示例：MySQL 插件

**特点**: 标准 SQL 数据库，使用 MySQL 协议

**文件**: [common/database/plugins/mysql/plugin.go](/common/database/plugins/mysql/plugin.go)

**实现接口**:
- DatabasePlugin（基础）
- SQLDatabasePlugin（标记）
- ConnectionPoolPlugin（连接池）
- MetadataPlugin（元数据）

**代码量**: ~290 行

**扫描器**: [meta/plugins/scanners/mysql/scanner.go](/meta/plugins/scanners/mysql/scanner.go)

**预览**: [manager/backend/internal/service/preview_provider_mysql.go](/manager/backend/internal/service/preview_provider_mysql.go)

---

### 复杂示例：ClickHouse 插件

**特点**: 列式存储，特殊的 Native 协议

**文件**: [common/database/plugins/clickhouse/plugin.go](/common/database/plugins/clickhouse/plugin.go)

**实现接口**:
- DatabasePlugin（基础）
- ConnectionPoolPlugin（连接池）
- MetadataPlugin（元数据）

**代码量**: ~320 行

**扫描器**: [meta/plugins/scanners/clickhouse/scanner.go](/meta/plugins/scanners/clickhouse/scanner.go)

**预览**: [manager/backend/internal/service/preview_provider_clickhouse.go](/manager/backend/internal/service/preview_provider_clickhouse.go)

---

### 对象存储示例：S3 插件

**特点**: 对象存储，支持文件元数据提取

**文件**: [common/database/plugins/s3/plugin.go](/common/database/plugins/s3/plugin.go)

**实现接口**:
- DatabasePlugin（基础）
- ObjectStoragePlugin（对象存储）

**扫描器**: [meta/plugins/scanners/s3/scanner.go](/meta/plugins/scanners/s3/scanner.go) (实现 `ObjectStorageScanner` 接口)

**提取器**: [meta/plugins/extractors/](/meta/plugins/extractors/) (根据文件类型自动选择)

---

## ⚠️ 注意事项

### 1. 安全性

- **不要打印敏感信息**: 避免在日志中打印密码等敏感字段
- **使用 SensitiveFields()**: 标记敏感字段供上层处理
- **SQL 注入防护**: 使用参数化查询而非字符串拼接

### 2. 错误处理

- **使用 fmt.Errorf() 包装错误**: 保留错误链
- **提供清晰的错误消息**: 帮助用户排查问题
- **设置超时**: 避免长时间阻塞（推荐 10-30 秒）

### 3. 性能优化

- **使用统计信息而非实际查询**: 获取行数时优先使用 INFORMATION_SCHEMA
- **避免 SELECT \***: 只查询需要的列
- **合理设置连接池**: 参考 poolConfig 配置

### 4. 兼容性

- **支持多种字段名**: 如同时支持 `user` 和 `username`
- **处理空密码**: 某些数据库（如 Doris）默认无密码
- **处理默认值**: 如数据库名默认为 `default`

---

## 🎯 常见数据库类型实现参考

### TiDB

```go
// 驱动：github.com/go-sql-driver/mysql（与 MySQL 相同）
// GORM 驱动：gorm.io/driver/mysql
// 默认端口：4000
// DSN 格式：与 MySQL 相同

// TiDB 可直接复用 MySQL 插件代码，仅修改默认端口和名称
func (p *TiDBPlugin) DefaultPort() int {
    return 4000
}
```

### SQLite

```go
// 驱动：github.com/mattn/go-sqlite3
// GORM 驱动：gorm.io/driver/sqlite
// 默认端口：无（本地文件）
// DSN 格式：文件路径

func (p *SQLitePlugin) BuildConnectionString(connInfo plugin.ConnectionInfo) (string, error) {
    filePath := plugin.GetString(connInfo, "file_path")
    if filePath == "" {
        return "", fmt.Errorf("missing file_path")
    }
    return filePath, nil
}
```

### Redis（NoSQL）

```go
// 驱动：github.com/go-redis/redis/v9
// 默认端口：6379
// 不支持 GORM，不实现 ConnectionPoolPlugin

func (p *RedisPlugin) EngineCategory() string {
    return "standard"  // 标准引擎
}

// 注意：Redis 不支持标准 SQL，不实现 MetadataPlugin
// 仅实现 DatabasePlugin 基础接口
```

---

## 📊 实施检查清单

- [ ] 创建数据库插件目录 `common/database/plugins/<dbtype>/`
- [ ] 实现 `DatabasePlugin` 基础接口（10 个方法）
- [ ] 实现可选接口（ConnectionPoolPlugin / MetadataPlugin / ObjectStoragePlugin）
- [ ] 在 `init()` 中注册插件：`plugin.Register(&<DBType>Plugin{})`
- [ ] 在 `common/dbbridge/bridge.go` 添加导入
- [ ] 创建 Scanner 插件 `meta/plugins/scanners/<dbtype>/scanner.go`
- [ ] 在 `meta/plugins/scanners/register.go` 添加导入
- [ ] 创建 PreviewProvider `manager/backend/internal/service/preview_provider_<dbtype>.go`
- [ ] 实现查询方法 `Query<DBType>TablePreview()`（在 MetadataRepository）
- [ ] 添加数据库插件测试
- [ ] 使用 `plugin.MapToStandardType()` 处理类型映射
- [ ] 清理 DEBUG 代码和敏感信息打印
- [ ] 设置合理的连接超时（10-30 秒）
- [ ] 更新 `docs/addp数据引擎扩展指南.md` 文档（数据库列表）
- [ ] 本地测试：`go build ./...` 和 `go test ./...`
- [ ] 集成测试：重启服务并测试连接/扫描/预览

---

## 🆘 故障排查

### 问题 1：插件未注册

**症状**: `plugin.Get("<dbtype>")` 返回 nil

**解决方案**:
1. 确认 `init()` 函数调用了 `plugin.Register()`
2. 确认在 `dbbridge/bridge.go` 中添加了 `import _ "..."`
3. 重新编译 `go build ./...`

### 问题 2：连接测试失败

**症状**: TestConnection 返回超时或连接拒绝

**解决方案**:
1. 检查 DSN 格式是否正确（打印到日志查看）
2. 确认数据库服务正在运行
3. 检查防火墙和网络配置
4. 增加连接超时时间

### 问题 3：类型映射错误

**症状**: 列类型显示为 "string" 而不是预期类型

**解决方案**:
1. 检查数据库返回的原生类型名称（打印 `dataType` 调试）
2. 确认使用了 `plugin.MapToStandardType()`
3. 如需自定义映射，使用 `MapToStandardTypeWithCustom()`

### 问题 4：扫描器未找到

**症状**: Meta 扫描时报错 "scanner not found"

**解决方案**:
1. 确认在 `meta/plugins/scanners/register.go` 添加了导入
2. 检查 `init()` 函数是否正确注册
3. 重新编译 meta 模块

### 问题 5：预览不显示数据

**症状**: 数据预览页面空白或报错

**解决方案**:
1. 检查 PreviewProvider 是否正确注册（`RegisterPreviewProvider()`）
2. 验证 `Supports()` 方法是否返回 true
3. 检查查询方法是否正确返回数据
4. 查看浏览器控制台错误

---

## 🎓 学习资源

- **Plugin 接口定义**: [common/database/plugin/interfaces.go](/common/database/plugin/interfaces.go)
- **类型映射工具**: [common/database/plugin/type_mapper.go](/common/database/plugin/type_mapper.go)
- **DSN 构建工具**: [common/database/plugin/dsn_builder.go](/common/database/plugin/dsn_builder.go)
- **Scanner 接口**: [meta/plugins/interfaces.go](/meta/plugins/interfaces.go)
- **PreviewProvider 接口**: [manager/backend/internal/service/preview_registry.go](/manager/backend/internal/service/preview_registry.go)
- **完整示例**: [common/database/plugins/postgresql/plugin.go](/common/database/plugins/postgresql/plugin.go)

---

## 📧 需要帮助？

如果遇到问题，请：
1. 查看已有插件实现作为参考
2. 阅读 [docs/addp常见故障排查.md](/docs/addp常见故障排查.md)
3. 在项目仓库提交 Issue

---

**祝你成功添加新的数据引擎支持！** 🎉

**最后更新**: 2025-12-29
**维护者**: ADDP 开发团队
