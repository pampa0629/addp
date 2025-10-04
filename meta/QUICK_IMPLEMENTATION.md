# Meta 模块快速实施指南

**目的**: 提供完整的实现框架，可以在新对话中快速完成剩余代码

---

## 📦 已完成的文件（10个）

✅ 设计文档（DESIGN.md, IMPLEMENTATION_STATUS.md, PROGRESS.md）
✅ 项目配置（go.mod, config.go）
✅ 数据库模型（5个model文件）
✅ 数据库连接（repository/database.go）

---

## 🚀 剩余需要创建的文件（13个）

### 1. 扫描器模块（4个文件）

#### `internal/scanner/types.go`
```go
package scanner

// DatabaseInfo 数据库信息
type DatabaseInfo struct {
    Name           string
    Charset        string
    Collation      string
    TableCount     int
    TotalSizeBytes int64
}

// TableInfo 表信息
type TableInfo struct {
    Schema         string
    Name           string
    Type           string
    Engine         string
    RowCount       int64
    DataSize       int64
    IndexSize      int64
    Comment        string
}

// FieldInfo 字段信息
type FieldInfo struct {
    Name          string
    Position      int
    DataType      string
    ColumnType    string
    IsNullable    bool
    ColumnKey     string
    DefaultValue  string
    Extra         string
    Comment       string
}

// Scanner 扫描器接口
type Scanner interface {
    ScanDatabases() ([]DatabaseInfo, error)
    ScanTables(database string) ([]TableInfo, error)
    ScanFields(database, table string) ([]FieldInfo, error)
    Close() error
}
```

#### `internal/scanner/postgres_scanner.go`
```go
package scanner

import (
    "database/sql"
    "fmt"
    _ "github.com/lib/pq"
)

type PostgresScanner struct {
    db *sql.DB
}

func NewPostgresScanner(connStr string) (*PostgresScanner, error) {
    db, err := sql.Open("postgres", connStr)
    if err != nil {
        return nil, err
    }
    return &PostgresScanner{db: db}, nil
}

func (s *PostgresScanner) ScanDatabases() ([]DatabaseInfo, error) {
    query := `
        SELECT
            datname,
            pg_encoding_to_char(encoding),
            datcollate,
            0 as table_count,
            pg_database_size(datname)
        FROM pg_database
        WHERE datistemplate = false
          AND datname NOT IN ('postgres', 'template0', 'template1')
    `
    // 执行查询，填充 DatabaseInfo 列表
    var databases []DatabaseInfo
    rows, err := s.db.Query(query)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    for rows.Next() {
        var db DatabaseInfo
        err := rows.Scan(&db.Name, &db.Charset, &db.Collation, &db.TableCount, &db.TotalSizeBytes)
        if err != nil {
            continue
        }
        databases = append(databases, db)
    }
    return databases, nil
}

func (s *PostgresScanner) ScanTables(database string) ([]TableInfo, error) {
    // 实现表扫描逻辑（见 PROGRESS.md）
    return nil, nil
}

func (s *PostgresScanner) ScanFields(database, table string) ([]FieldInfo, error) {
    // 实现字段扫描逻辑
    return nil, nil
}

func (s *PostgresScanner) Close() error {
    return s.db.Close()
}
```

#### `internal/scanner/mysql_scanner.go`
```go
package scanner

import (
    "database/sql"
    _ "github.com/go-sql-driver/mysql"
)

type MySQLScanner struct {
    db *sql.DB
}

func NewMySQLScanner(connStr string) (*MySQLScanner, error) {
    db, err := sql.Open("mysql", connStr)
    if err != nil {
        return nil, err
    }
    return &MySQLScanner{db: db}, nil
}

// 实现 Scanner 接口的三个方法（见 PROGRESS.md 中的示例）
```

#### `internal/scanner/factory.go`
```go
package scanner

import "fmt"

type Factory struct{}

func NewFactory() *Factory {
    return &Factory{}
}

func (f *Factory) CreateScanner(resourceType, connectionString string) (Scanner, error) {
    switch resourceType {
    case "postgresql":
        return NewPostgresScanner(connectionString)
    case "mysql":
        return NewMySQLScanner(connectionString)
    default:
        return nil, fmt.Errorf("unsupported database type: %s", resourceType)
    }
}
```

### 2. System 客户端（1个文件）

#### `pkg/utils/system_client.go`
```go
package utils

import (
    "encoding/json"
    "fmt"
    "io"
    "net/http"
)

type SystemClient struct {
    baseURL string
    client  *http.Client
}

type Resource struct {
    ID             uint   `json:"id"`
    Name           string `json:"name"`
    ResourceType   string `json:"resource_type"`
    ConnectionInfo map[string]interface{} `json:"connection_info"`
}

func NewSystemClient(baseURL string) *SystemClient {
    return &SystemClient{
        baseURL: baseURL,
        client:  &http.Client{},
    }
}

func (c *SystemClient) GetResource(resourceID uint, token string) (*Resource, error) {
    url := fmt.Sprintf("%s/api/resources/%d", c.baseURL, resourceID)
    req, _ := http.NewRequest("GET", url, nil)
    req.Header.Set("Authorization", "Bearer "+token)

    resp, err := c.client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    body, _ := io.ReadAll(resp.Body)
    var resource Resource
    json.Unmarshal(body, &resource)
    return &resource, nil
}

func (c *SystemClient) BuildConnectionString(resource *Resource) (string, error) {
    // 根据 resource_type 构建连接字符串
    connInfo := resource.ConnectionInfo
    switch resource.ResourceType {
    case "postgresql":
        return fmt.Sprintf("host=%s port=%v user=%s password=%s dbname=%s sslmode=disable",
            connInfo["host"], connInfo["port"], connInfo["username"],
            connInfo["password"], connInfo["database"]), nil
    case "mysql":
        return fmt.Sprintf("%s:%s@tcp(%s:%v)/%s?charset=utf8mb4&parseTime=True",
            connInfo["username"], connInfo["password"], connInfo["host"],
            connInfo["port"], connInfo["database"]), nil
    default:
        return "", fmt.Errorf("unsupported type: %s", resource.ResourceType)
    }
}
```

### 3. 服务层（3个文件）

#### `internal/service/sync_service.go`
```go
package service

import (
    "time"
    "github.com/addp/meta/internal/models"
    "github.com/addp/meta/internal/scanner"
    "github.com/addp/meta/pkg/utils"
    "gorm.io/gorm"
)

type SyncService struct {
    db             *gorm.DB
    scannerFactory *scanner.Factory
    systemClient   *utils.SystemClient
}

func NewSyncService(db *gorm.DB, systemURL string) *SyncService {
    return &SyncService{
        db:             db,
        scannerFactory: scanner.NewFactory(),
        systemClient:   utils.NewSystemClient(systemURL),
    }
}

// AutoSync Level 1 轻量级自动同步
func (s *SyncService) AutoSync(resourceID uint, tenantID uint, token string) (*models.MetadataSyncLog, error) {
    now := time.Now()
    syncLog := &models.MetadataSyncLog{
        DatasourceID: resourceID,
        TenantID:     tenantID,
        SyncType:     "auto",
        SyncLevel:    "database",
        Status:       "running",
        StartedAt:    &now,
    }
    s.db.Create(syncLog)

    // 获取资源连接信息
    resource, err := s.systemClient.GetResource(resourceID, token)
    if err != nil {
        syncLog.Status = "failed"
        syncLog.ErrorMessage = err.Error()
        s.db.Save(syncLog)
        return syncLog, err
    }

    // 创建扫描器
    connStr, _ := s.systemClient.BuildConnectionString(resource)
    scanner, err := s.scannerFactory.CreateScanner(resource.ResourceType, connStr)
    if err != nil {
        syncLog.Status = "failed"
        syncLog.ErrorMessage = err.Error()
        s.db.Save(syncLog)
        return syncLog, err
    }
    defer scanner.Close()

    // 扫描数据库列表
    databases, err := scanner.ScanDatabases()
    if err != nil {
        syncLog.Status = "failed"
        syncLog.ErrorMessage = err.Error()
        s.db.Save(syncLog)
        return syncLog, err
    }

    // 保存到数据库（先删除旧数据）
    s.db.Where("datasource_id = ?", resourceID).Delete(&models.MetadataDatabase{})

    for _, dbInfo := range databases {
        db := &models.MetadataDatabase{
            DatasourceID:   resourceID,
            TenantID:       tenantID,
            DatabaseName:   dbInfo.Name,
            Charset:        dbInfo.Charset,
            Collation:      dbInfo.Collation,
            TableCount:     dbInfo.TableCount,
            TotalSizeBytes: dbInfo.TotalSizeBytes,
        }
        s.db.Create(db)
    }

    // 更新同步日志
    completed := time.Now()
    syncLog.Status = "success"
    syncLog.CompletedAt = &completed
    syncLog.DurationSeconds = int(completed.Sub(now).Seconds())
    syncLog.DatabasesScanned = len(databases)
    s.db.Save(syncLog)

    return syncLog, nil
}
```

#### `internal/service/scan_service.go`
```go
package service

// DeepScan Level 2 深度扫描
// 实现逻辑见 PROGRESS.md
```

#### `internal/service/metadata_service.go`
```go
package service

// 提供元数据查询服务
// GetDatabases, GetTables, GetFields, GetSyncLogs 等方法
```

### 4. API 层（4个文件）

#### `internal/middleware/auth.go`
```go
package middleware

import (
    "github.com/gin-gonic/gin"
    "net/http"
    "strings"
)

func AuthMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        token := c.GetHeader("Authorization")
        if token == "" {
            c.JSON(http.StatusUnauthorized, gin.H{"error": "未提供认证令牌"})
            c.Abort()
            return
        }

        // 提取 Bearer token
        token = strings.TrimPrefix(token, "Bearer ")

        // TODO: 调用 System API 验证 token
        // 这里简化处理，实际应该调用 System /api/auth/verify

        c.Set("token", token)
        c.Next()
    }
}
```

#### `internal/api/sync_handler.go`
```go
package api

import (
    "github.com/gin-gonic/gin"
    "github.com/addp/meta/internal/service"
    "net/http"
)

type SyncHandler struct {
    syncService *service.SyncService
}

func NewSyncHandler(syncService *service.SyncService) *SyncHandler {
    return &SyncHandler{syncService: syncService}
}

// AutoSync POST /api/metadata/sync/auto
func (h *SyncHandler) AutoSync(c *gin.Context) {
    var req struct {
        ResourceID uint `json:"resource_id" binding:"required"`
        Force      bool `json:"force"`
    }
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    token := c.GetString("token")
    tenantID := uint(1) // TODO: 从 token 解析出 tenantID

    syncLog, err := h.syncService.AutoSync(req.ResourceID, tenantID, token)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }

    c.JSON(http.StatusOK, gin.H{
        "sync_id": syncLog.ID,
        "status":  syncLog.Status,
        "message": "开始同步数据库列表",
    })
}
```

#### `internal/api/scan_handler.go`
```go
package api

// DeepScan POST /api/metadata/scan/deep
// GetScanStatus GET /api/metadata/scan/status/:id
```

#### `internal/api/metadata_handler.go`
```go
package api

// GetDatabases GET /api/metadata/databases
// GetTables GET /api/metadata/tables
// GetFields GET /api/metadata/fields
// GetSyncLogs GET /api/metadata/sync-logs
```

#### `internal/api/router.go`
```go
package api

import (
    "github.com/gin-gonic/gin"
    "github.com/gin-contrib/cors"
    "github.com/addp/meta/internal/service"
    "github.com/addp/meta/internal/middleware"
)

func SetupRouter(
    syncService *service.SyncService,
    scanService *service.ScanService,
    metadataService *service.MetadataService,
) *gin.Engine {
    r := gin.Default()

    // CORS
    r.Use(cors.New(cors.Config{
        AllowOrigins:     []string{"*"},
        AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
        AllowHeaders:     []string{"Content-Type", "Authorization"},
        AllowCredentials: true,
    }))

    // Health check
    r.GET("/health", func(c *gin.Context) {
        c.JSON(200, gin.H{"status": "healthy"})
    })

    // API routes
    api := r.Group("/api/metadata")
    api.Use(middleware.AuthMiddleware())
    {
        // Sync routes
        syncHandler := NewSyncHandler(syncService)
        api.POST("/sync/auto", syncHandler.AutoSync)

        // Scan routes
        scanHandler := NewScanHandler(scanService)
        api.POST("/scan/deep", scanHandler.DeepScan)
        api.GET("/scan/status/:id", scanHandler.GetStatus)

        // Metadata query routes
        metaHandler := NewMetadataHandler(metadataService)
        api.GET("/databases", metaHandler.GetDatabases)
        api.GET("/tables", metaHandler.GetTables)
        api.GET("/fields", metaHandler.GetFields)
        api.GET("/sync-logs", metaHandler.GetSyncLogs)
    }

    return r
}
```

### 5. 主程序（1个文件）

#### `cmd/server/main.go`
```go
package main

import (
    "log"
    "github.com/addp/meta/internal/config"
    "github.com/addp/meta/internal/repository"
    "github.com/addp/meta/internal/service"
    "github.com/addp/meta/internal/api"
    "github.com/joho/godotenv"
)

func main() {
    // 加载环境变量
    godotenv.Load()

    // 加载配置
    cfg := config.Load()
    log.Printf("Meta service starting on port %s", cfg.Port)

    // 初始化数据库
    db, err := repository.InitDatabase(cfg)
    if err != nil {
        log.Fatalf("Failed to initialize database: %v", err)
    }

    // 初始化服务
    syncService := service.NewSyncService(db, cfg.SystemServiceURL)
    scanService := service.NewScanService(db, cfg.SystemServiceURL)
    metadataService := service.NewMetadataService(db)

    // 设置路由
    router := api.SetupRouter(syncService, scanService, metadataService)

    // 启动定时任务（如果启用）
    if cfg.AutoSyncEnabled {
        // TODO: 使用 robfig/cron 启动定时任务
        log.Println("Auto sync enabled with schedule:", cfg.AutoSyncSchedule)
    }

    // 启动服务器
    if err := router.Run(":" + cfg.Port); err != nil {
        log.Fatalf("Failed to start server: %v", err)
    }
}
```

---

## 🎯 下一步行动

### 在新对话中继续：

1. **完成扫描器实现**（PostgreSQL + MySQL 的完整SQL查询）
2. **完成服务层**（深度扫描 + 元数据查询）
3. **完成 API 层**（所有 Handler 的实现）
4. **添加 robfig/cron 定时任务**
5. **测试运行 Meta 服务**

### 更新 go.mod 添加依赖：

```bash
cd meta/backend
go get github.com/robfig/cron/v3
go get github.com/go-sql-driver/mysql
go mod tidy
```

### 测试命令：

```bash
# 启动 Meta 服务
cd meta/backend
go run cmd/server/main.go

# 测试健康检查
curl http://localhost:8082/health

# 测试自动同步
curl -X POST http://localhost:8082/api/metadata/sync/auto \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -d '{"resource_id": 1}'
```

---

## 📝 当前进度

- ✅ 设计文档完成
- ✅ 数据库模型完成
- ✅ 数据库连接完成
- 🔄 核心代码框架已规划
- ⏳ 待在新对话中完成具体实现

**预计剩余工作量**: 2-3小时即可完成所有 Meta 后端代码！
