# Meta 模块开发进度报告

**最后更新**: 2025-10-04
**当前阶段**: Phase 1 - Meta 后端核心功能

---

## ✅ 已完成的文件

### 1. 设计和规划文档
- ✅ `DESIGN.md` - 完整设计文档（数据库表、API、前端页面）
- ✅ `IMPLEMENTATION_STATUS.md` - 实施计划和状态
- ✅ `PROGRESS.md` - 本文档

### 2. 项目基础结构
- ✅ `backend/go.mod` - Go 模块定义
- ✅ `backend/internal/config/config.go` - 配置管理

### 3. 数据库模型（5个模型）
- ✅ `backend/internal/models/datasource.go` - 数据源元数据
- ✅ `backend/internal/models/database.go` - 数据库级元数据（Level 1）
- ✅ `backend/internal/models/table.go` - 表级元数据（Level 2）
- ✅ `backend/internal/models/field.go` - 字段级元数据（Level 2）
- ✅ `backend/internal/models/sync_log.go` - 同步日志

### 4. 数据库连接
- ✅ `backend/internal/repository/database.go` - 数据库连接、迁移

---

## 🔄 正在进行的工作

### 下一步需要创建的核心文件：

#### A. 扫描器（Scanner）- 核心逻辑
```
backend/internal/scanner/
├── scanner.go           ← 扫描器接口定义
├── factory.go           ← 扫描器工厂
├── postgres_scanner.go  ← PostgreSQL 扫描器实现
└── mysql_scanner.go     ← MySQL 扫描器实现
```

**接口设计**:
```go
type Scanner interface {
    // Level 1: 轻量级同步 - 获取数据库列表
    ScanDatabases() ([]DatabaseInfo, error)

    // Level 2: 深度扫描 - 获取表和字段
    ScanTables(database string) ([]TableInfo, error)
    ScanFields(database, table string) ([]FieldInfo, error)
}
```

#### B. 服务层（Service）
```
backend/internal/service/
├── sync_service.go      ← Level 1 轻量级同步服务
├── scan_service.go      ← Level 2 深度扫描服务
└── metadata_service.go  ← 元数据查询服务
```

#### C. API 层（Handler + Router）
```
backend/internal/api/
├── router.go            ← 路由配置
├── sync_handler.go      ← 同步 API Handler
├── scan_handler.go      ← 扫描 API Handler
└── metadata_handler.go  ← 查询 API Handler
```

#### D. 认证中间件
```
backend/internal/middleware/
└── auth.go              ← JWT 认证中间件（调用 System 模块）
```

#### E. 应用入口
```
backend/cmd/server/
└── main.go              ← 主程序入口
```

---

## 📊 代码量预估

### 已完成
- 文档: ~15 KB (3个文件)
- 配置: ~2 KB (1个文件)
- 模型: ~3 KB (5个文件)
- Repository: ~2 KB (1个文件)

**小计**: ~22 KB, 10个文件

### 待完成
- 扫描器: ~15 KB (4个文件) - **核心复杂度**
- 服务层: ~12 KB (3个文件)
- API层: ~10 KB (4个文件)
- 中间件: ~2 KB (1个文件)
- 主程序: ~2 KB (1个文件)

**小计**: ~41 KB, 13个文件

### Phase 2-4（Manager 集成 + 前端）
- Manager 后端: ~10 KB (5个文件)
- Manager 前端: ~20 KB (10个文件)
- Meta 前端: ~15 KB (8个文件)

**小计**: ~45 KB, 23个文件

---

## 💡 核心实现思路

### 1. PostgreSQL 扫描器示例

```go
type PostgresScanner struct {
    db *sql.DB
}

// Level 1: 获取数据库列表
func (s *PostgresScanner) ScanDatabases() ([]DatabaseInfo, error) {
    query := `
        SELECT
            datname AS database_name,
            pg_encoding_to_char(encoding) AS charset,
            datcollate AS collation,
            (SELECT COUNT(*) FROM information_schema.tables
             WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
             AND table_catalog = datname) AS table_count,
            pg_database_size(datname) AS total_size_bytes
        FROM pg_database
        WHERE datistemplate = false
    `
    // ... 执行查询并返回
}

// Level 2: 获取表列表
func (s *PostgresScanner) ScanTables(database string) ([]TableInfo, error) {
    query := `
        SELECT
            table_schema,
            table_name,
            table_type,
            (SELECT obj_description((table_schema||'.'||table_name)::regclass)) AS table_comment,
            (SELECT reltuples::bigint FROM pg_class WHERE relname = table_name) AS row_count,
            pg_total_relation_size((table_schema||'.'||table_name)::regclass) AS data_size_bytes
        FROM information_schema.tables
        WHERE table_catalog = $1
          AND table_schema NOT IN ('pg_catalog', 'information_schema')
        ORDER BY table_schema, table_name
    `
    // ... 执行查询并返回
}

// Level 2: 获取字段列表
func (s *PostgresScanner) ScanFields(database, table string) ([]FieldInfo, error) {
    query := `
        SELECT
            column_name,
            ordinal_position,
            data_type,
            udt_name AS column_type,
            is_nullable = 'YES' AS is_nullable,
            column_default,
            '' AS column_key,  -- 需要额外查询约束
            '' AS extra,
            col_description((table_schema||'.'||table_name)::regclass, ordinal_position) AS field_comment
        FROM information_schema.columns
        WHERE table_catalog = $1
          AND table_schema = $2
          AND table_name = $3
        ORDER BY ordinal_position
    `
    // ... 执行查询并返回
}
```

### 2. MySQL 扫描器示例

```go
type MySQLScanner struct {
    db *sql.DB
}

// Level 1: 获取数据库列表
func (s *MySQLScanner) ScanDatabases() ([]DatabaseInfo, error) {
    query := `
        SELECT
            SCHEMA_NAME AS database_name,
            DEFAULT_CHARACTER_SET_NAME AS charset,
            DEFAULT_COLLATION_NAME AS collation,
            (SELECT COUNT(*) FROM information_schema.TABLES
             WHERE TABLE_SCHEMA = SCHEMA_NAME) AS table_count,
            COALESCE(SUM(DATA_LENGTH + INDEX_LENGTH), 0) AS total_size_bytes
        FROM information_schema.SCHEMATA
        LEFT JOIN (
            SELECT TABLE_SCHEMA, SUM(DATA_LENGTH + INDEX_LENGTH) AS size
            FROM information_schema.TABLES
            GROUP BY TABLE_SCHEMA
        ) sizes ON SCHEMA_NAME = sizes.TABLE_SCHEMA
        WHERE SCHEMA_NAME NOT IN ('information_schema', 'mysql', 'performance_schema', 'sys')
        GROUP BY SCHEMA_NAME
    `
    // ... 执行查询并返回
}

// Level 2: 获取表列表
func (s *MySQLScanner) ScanTables(database string) ([]TableInfo, error) {
    query := `
        SELECT
            TABLE_SCHEMA AS table_schema,
            TABLE_NAME AS table_name,
            TABLE_TYPE AS table_type,
            ENGINE AS engine,
            TABLE_ROWS AS row_count,
            DATA_LENGTH AS data_size_bytes,
            INDEX_LENGTH AS index_size_bytes,
            TABLE_COMMENT AS table_comment
        FROM information_schema.TABLES
        WHERE TABLE_SCHEMA = ?
        ORDER BY TABLE_NAME
    `
    // ... 执行查询并返回
}

// Level 2: 获取字段列表
func (s *MySQLScanner) ScanFields(database, table string) ([]FieldInfo, error) {
    query := `
        SELECT
            COLUMN_NAME AS field_name,
            ORDINAL_POSITION AS field_position,
            DATA_TYPE AS data_type,
            COLUMN_TYPE AS column_type,
            IS_NULLABLE = 'YES' AS is_nullable,
            COLUMN_KEY AS column_key,
            COLUMN_DEFAULT AS column_default,
            EXTRA AS extra,
            COLUMN_COMMENT AS field_comment
        FROM information_schema.COLUMNS
        WHERE TABLE_SCHEMA = ?
          AND TABLE_NAME = ?
        ORDER BY ORDINAL_POSITION
    `
    // ... 执行查询并返回
}
```

### 3. 同步服务流程

```go
type SyncService struct {
    db *gorm.DB
    scannerFactory *scanner.Factory
}

// Level 1: 轻量级自动同步
func (s *SyncService) AutoSync(resourceID uint, tenantID uint) (*models.MetadataSyncLog, error) {
    // 1. 创建同步日志
    syncLog := &models.MetadataSyncLog{
        DatasourceID: resourceID,
        TenantID:     tenantID,
        SyncType:     "auto",
        SyncLevel:    "database",
        Status:       "running",
        StartedAt:    time.Now(),
    }
    s.db.Create(syncLog)

    // 2. 获取资源连接信息（调用 System API）
    connInfo, err := s.getResourceConnection(resourceID)

    // 3. 创建扫描器
    scanner, err := s.scannerFactory.CreateScanner(connInfo)

    // 4. 扫描数据库列表
    databases, err := scanner.ScanDatabases()

    // 5. 保存到 metadata.databases 表
    for _, dbInfo := range databases {
        db := &models.MetadataDatabase{
            DatasourceID:   resourceID,
            TenantID:       tenantID,
            DatabaseName:   dbInfo.Name,
            Charset:        dbInfo.Charset,
            Collation:      dbInfo.Collation,
            TableCount:     dbInfo.TableCount,
            TotalSizeBytes: dbInfo.TotalSize,
        }
        s.db.Create(db)
    }

    // 6. 更新同步日志
    syncLog.Status = "success"
    syncLog.CompletedAt = time.Now()
    syncLog.DatabasesScanned = len(databases)
    s.db.Save(syncLog)

    return syncLog, nil
}
```

### 4. 深度扫描服务流程

```go
// Level 2: 深度扫描
func (s *ScanService) DeepScan(resourceID uint, database string, tenantID uint) (*models.MetadataSyncLog, error) {
    // 1. 创建扫描日志
    syncLog := &models.MetadataSyncLog{
        DatasourceID:   resourceID,
        TenantID:       tenantID,
        SyncType:       "deep",
        SyncLevel:      "field",
        TargetDatabase: database,
        Status:         "running",
        StartedAt:      time.Now(),
    }
    s.db.Create(syncLog)

    // 2. 获取资源连接和扫描器
    connInfo, _ := s.getResourceConnection(resourceID)
    scanner, _ := s.scannerFactory.CreateScanner(connInfo)

    // 3. 扫描表列表
    tables, err := scanner.ScanTables(database)

    // 4. 保存表信息
    for _, tableInfo := range tables {
        table := &models.MetadataTable{
            DatabaseID:     databaseID,  // 从 databases 表查询
            TenantID:       tenantID,
            TableName:      tableInfo.Name,
            TableType:      tableInfo.Type,
            // ... 其他字段
        }
        s.db.Create(table)

        // 5. 扫描字段列表
        fields, _ := scanner.ScanFields(database, tableInfo.Name)
        for _, fieldInfo := range fields {
            field := &models.MetadataField{
                TableID:   table.ID,
                TenantID:  tenantID,
                FieldName: fieldInfo.Name,
                // ... 其他字段
            }
            s.db.Create(field)
        }
    }

    // 6. 更新扫描日志
    syncLog.Status = "success"
    syncLog.CompletedAt = time.Now()
    syncLog.TablesScanned = len(tables)
    s.db.Save(syncLog)

    return syncLog, nil
}
```

---

## 🎯 下一步行动计划

### 选项 1: 我继续完成所有代码（推荐）
我会继续创建所有剩余文件，直到 Meta 模块后端完全可运行。预计再需要 2-3 小时。

### 选项 2: 你先测试当前代码
你可以先测试数据库连接和模型是否正常工作：
```bash
cd meta/backend
go mod download
go run cmd/server/main.go  # 会失败，因为 main.go 还未创建
```

### 选项 3: 分步实施
我先完成扫描器，你测试后再继续服务层和 API 层。

---

## ❓ 需要确认

1. **是否继续完成所有 Meta 后端代码？** 还是先暂停，让你查看当前进度？
2. **定时任务实现方式**：
   - 使用 `robfig/cron` 库（推荐）
   - 使用系统 crontab
   - 暂不实现，先做手动触发

3. **获取 System 资源连接信息的方式**：
   - HTTP 调用 System API（需要实现 HTTP 客户端）
   - 直接查询 System 数据库（需要跨 schema 查询）

请告知是否继续！我已经准备好快速完成剩余代码。
