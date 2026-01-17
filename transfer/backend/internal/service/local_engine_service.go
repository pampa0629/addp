package service

import (
    "context"
    "database/sql"
    "fmt"
    "log/slog"
    "os"
    "strconv"
    "sync"
    "time"

    "errors"
    "strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/events"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/mappers/mysql"
	_ "github.com/addp/common/format/mappers/postgresql"
	_ "github.com/addp/common/format/mappers/spatialite"
	commonLogger "github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	commonUtils "github.com/addp/common/utils"
	"github.com/addp/transfer/internal/config"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/repository"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
    "github.com/redis/go-redis/v9"
    "gorm.io/gorm"

    _ "github.com/lib/pq" // PostgreSQL driver for connection tests
    _ "github.com/mattn/go-sqlite3" // SQLite/SpatiaLite for local scans
    _ "github.com/go-sql-driver/mysql" // MySQL for local scans
)

var (
	ErrSystemIntegrationDisabled = errors.New("system integration not available")
	ErrEngineAccessDenied        = errors.New("engine not accessible")
)

// engineCacheEntry 缓存条目，包含引擎和过期时间
type engineCacheEntry struct {
	engine    *commonModels.Engine
	expiresAt time.Time
}

// LocalEngineService 提供 Transfer 模块的本地引擎管理能力
type LocalEngineService struct {
	repo            *repository.LocalEngineRepository
	systemClient    *commonClient.SystemClient
	logger          *slog.Logger
	cfg             *config.Config
	cacheMu         sync.RWMutex
	engineCache     map[uint]*engineCacheEntry
	cacheTTL        time.Duration // 缓存生存时间，默认 2 分钟（Transfer 使用较短TTL）
	eventSubscriber *events.EngineEventSubscriber
}

// NewLocalEngineService 创建 Service
func NewLocalEngineService(db *gorm.DB, cfg *config.Config, redisClient *redis.Client) *LocalEngineService {
	var systemClient *commonClient.SystemClient
	if cfg.EnableIntegration && cfg.SystemServiceURL != "" && cfg.InternalAPIKey != "" {
		systemClient = commonClient.NewSystemClientWithInternalKey(cfg.SystemServiceURL, cfg.InternalAPIKey)
	}

	service := &LocalEngineService{
		repo:          repository.NewLocalEngineRepository(db),
		systemClient:  systemClient,
		logger:        commonLogger.With("component", "local_engine_service"),
		cfg:           cfg,
		engineCache:   make(map[uint]*engineCacheEntry),
		cacheTTL:      2 * time.Minute, // Transfer 使用 2 分钟 TTL
	}

	// 初始化 Redis 事件订阅器
	if redisClient != nil {
		service.eventSubscriber = events.NewEngineEventSubscriber(
			redisClient,
			service.handleResourceChangeEvent,
			service.logger,
		)
		// 启动订阅（在后台 goroutine 中）
		go func() {
			if err := service.eventSubscriber.Start(); err != nil {
				service.logger.Error("资源事件订阅器启动失败", "error", err)
			}
		}()
		service.logger.Info("资源事件订阅器已启动")
	} else {
		service.logger.Warn("Redis 未配置，资源变更事件同步功能将被禁用")
	}

	return service
}

// List 返回指定租户下的本地引擎列表
func (s *LocalEngineService) List(tenantID uint, engineType string) ([]models.LocalEngine, error) {
	engines, err := s.repo.List(tenantID, engineType)
	if err != nil {
		return nil, err
	}
	// 解密并脱敏敏感信息
	for i := range engines {
		connInfo := engines[i].ConnectionInfo
		if err := s.decryptConnectionInfo(connInfo); err != nil {
			s.logger.Warn("failed to decrypt connection info", "engine_id", engines[i].ID, "error", err)
			// 解密失败不影响其他资源
		}
		s.sanitizeConnectionInfo(connInfo)
	}
	return engines, nil
}

// ListSystemEngines 获取 System 模块的存储引擎（需启用集成）
func (s *LocalEngineService) ListSystemEngines(engineType string, tenantID uint) ([]commonModels.Engine, error) {
	if s.systemClient == nil {
		s.logger.Warn("system client unavailable when listing system engines")
		return nil, ErrSystemIntegrationDisabled
	}

	engines, err := s.systemClient.ListEngines(engineType, tenantID)
	if err != nil {
		return nil, err
	}

	for i := range engines {
		if engines[i].ConnectionInfo != nil {
			sanitizeMap(engines[i].ConnectionInfo)
		}
	}

	filtered := make([]commonModels.Engine, 0, len(engines))
	for _, res := range engines {
		if res.IsActive {
			filtered = append(filtered, res)
		}
	}

	return filtered, nil
}

// GetSystemResource 获取 System 模块的资源详情（包含解密信息，带缓存）
func (s *LocalEngineService) GetSystemResource(engineID, tenantID uint) (*commonModels.Engine, error) {
	if s.systemClient == nil {
		s.logger.Warn("system client unavailable when fetching system resource", "engine_id", engineID)
		return nil, ErrSystemIntegrationDisabled
	}

	// 检查缓存
	s.cacheMu.RLock()
	entry, ok := s.engineCache[engineID]
	s.cacheMu.RUnlock()

	// 如果缓存命中且未过期，返回缓存数据
	if ok && entry != nil && entry.engine != nil && time.Now().Before(entry.expiresAt) {
		if tenantID == 0 || (entry.engine.TenantID != nil && *entry.engine.TenantID == tenantID) {
			resourceCopy := *entry.engine
			s.logger.Debug("引擎连接信息命中缓存",
				"engine_id", engineID,
				"expires_in_seconds", int(time.Until(entry.expiresAt).Seconds()),
			)
			return &resourceCopy, nil
		}
	}

	// 缓存未命中或已过期，从 System API 获取
	resource, err := s.systemClient.GetEngine(engineID)
	if err != nil {
		return nil, err
	}

	if resource.TenantID != nil && *resource.TenantID != 0 && *resource.TenantID != tenantID {
		return nil, ErrEngineAccessDenied
	}

	// 更新缓存
	s.cacheResource(resource)
	s.logger.Info("通过内部 API 获取资源连接信息成功",
		"engine_id", engineID,
		"engine_type", resource.EngineType,
	)

	return resource, nil
}

// Get 获取指定资源
func (s *LocalEngineService) Get(id, tenantID uint) (*models.LocalEngine, error) {
	resource, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}
	// 解密敏感信息
	if err := s.decryptConnectionInfo(resource.ConnectionInfo); err != nil {
		s.logger.Warn("failed to decrypt connection info", "engine_id", id, "error", err)
		// 解密失败不返回错误，但记录日志
	}
	return resource, nil
}

// Create 创建资源
func (s *LocalEngineService) Create(resource *models.LocalEngine) error {
	// 加密敏感信息
	if err := s.encryptConnectionInfo(resource.ConnectionInfo); err != nil {
		return fmt.Errorf("failed to encrypt connection info: %w", err)
	}
	if err := s.repo.Create(resource); err != nil {
		return err
	}
	// 数据库已保存后，对返回对象进行脱敏，避免泄露密文
	s.sanitizeConnectionInfo(resource.ConnectionInfo)
	return nil
}

// Update 更新资源
func (s *LocalEngineService) Update(resource *models.LocalEngine) error {
	// 加密敏感信息
	if err := s.encryptConnectionInfo(resource.ConnectionInfo); err != nil {
		return fmt.Errorf("failed to encrypt connection info: %w", err)
	}
	if err := s.repo.Update(resource); err != nil {
		return err
	}
	// 更新成功后脱敏返回
	s.sanitizeConnectionInfo(resource.ConnectionInfo)
	return nil
}

// Delete 删除资源
func (s *LocalEngineService) Delete(id, tenantID uint) error {
	return s.repo.Delete(id, tenantID)
}

// TestConnectionBeforeCreate 测试尚未入库的配置（未加密的明文配置）
func (s *LocalEngineService) TestConnectionBeforeCreate(engineType string, connInfo models.JSONMap) error {
	// 这里传入的是前端的明文配置，直接测试即可
	return s.testConnection(engineType, connInfo)
}

// TestConnection 测试已存在资源的连接
func (s *LocalEngineService) TestConnection(id, tenantID uint) error {
	resource, err := s.Get(id, tenantID)
	if err != nil {
		return err
	}
	// Get 方法已经解密了 connection_info，可以直接使用
	return s.testConnection(resource.EngineType, resource.ConnectionInfo)
}

// SyncToSystem 将本地配置推送到 System 模块，返回在 System 中创建的资源
func (s *LocalEngineService) SyncToSystem(resource *models.LocalEngine) (*commonModels.Engine, error) {
	if s.systemClient == nil {
		return nil, fmt.Errorf("system integration not available")
	}

	payload := map[string]interface{}{
		"name":            resource.Name,
		"engine_type":   resource.EngineType,
		"description":     resource.Description,
		"connection_info": resource.ConnectionInfo,
	}

	if resource.TenantID != 0 {
		payload["tenant_id"] = resource.TenantID
	}
	if resource.CreatedBy != nil {
		payload["created_by"] = *resource.CreatedBy
	}

	resp, err := s.systemClient.CreateEngine(payload)
	if err != nil {
		return nil, err
	}

	if resp != nil && resp.ConnectionInfo != nil {
		sanitizeMap(resp.ConnectionInfo)
	}

	return resp, nil
}

func (s *LocalEngineService) testConnection(engineType string, connInfo models.JSONMap) error {
    switch engineType {
    case "postgresql":
        return s.testPostgreSQL(connInfo)
    case "mysql":
        return s.testMySQL(connInfo)
    case "minio", "s3":
        return s.testObjectStorage(connInfo)
    case "spatialite", "sqlite":
        return s.testSpatiaLite(connInfo)
    default:
        return fmt.Errorf("unsupported resource type: %s", engineType)
    }
}

func (s *LocalEngineService) normalizeHost(host string) string {
	if host == "localhost" || host == "127.0.0.1" {
		if alias := os.Getenv("RESOURCE_LOCALHOST_ALIAS"); alias != "" {
			return alias
		}
		return "127.0.0.1"
	}
	return host
}

func (s *LocalEngineService) testPostgreSQL(connInfo models.JSONMap) error {
	host, _ := connInfo["host"].(string)
	host = s.normalizeHost(host)

	var port int
	switch v := connInfo["port"].(type) {
	case float64:
		port = int(v)
	case int:
		port = v
	case string:
		if parsed, err := strconv.Atoi(v); err == nil {
			port = parsed
		}
	}
	if port == 0 {
		port = 5432
	}

	database, _ := connInfo["database"].(string)
	user, _ := connInfo["user"].(string)
	password, _ := connInfo["password"].(string)
	sslMode, _ := connInfo["sslmode"].(string)
	if sslMode == "" {
		sslMode = "disable"
	}

	if host == "" || user == "" || database == "" {
		return fmt.Errorf("missing required fields: host, user, database")
	}

	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		host, port, user, password, database, sslMode,
	)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("failed to open connection: %w", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	return nil
}

func (s *LocalEngineService) testMySQL(connInfo models.JSONMap) error {
    host, _ := connInfo["host"].(string)
    host = s.normalizeHost(host)
    var port int
    switch v := connInfo["port"].(type) {
    case float64:
        port = int(v)
    case int:
        port = v
    case string:
        if parsed, err := strconv.Atoi(v); err == nil { port = parsed }
    }
    if port == 0 { port = 3306 }
    database, _ := connInfo["database"].(string)
    user, _ := connInfo["user"].(string)
    password, _ := connInfo["password"].(string)
    if host == "" || user == "" || database == "" {
        return fmt.Errorf("missing required fields: host, user, database")
    }
    dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true", user, password, host, port, database)
    db, err := sql.Open("mysql", dsn)
    if err != nil { return fmt.Errorf("failed to open connection: %w", err) }
    defer db.Close()
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    if err := db.PingContext(ctx); err != nil { return fmt.Errorf("failed to ping database: %w", err) }
    return nil
}

func (s *LocalEngineService) testObjectStorage(connInfo models.JSONMap) error {
	endpoint, _ := connInfo["endpoint"].(string)
	if endpoint == "" {
		return fmt.Errorf("missing required field: endpoint")
	}
	accessKey, _ := connInfo["access_key"].(string)
	secretKey, _ := connInfo["secret_key"].(string)
	if accessKey == "" || secretKey == "" {
		return fmt.Errorf("missing required fields: access_key, secret_key")
	}

	useSSL := false
	if val, ok := connInfo["use_ssl"].(bool); ok {
		useSSL = val
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := client.ListBuckets(ctx); err != nil {
		return fmt.Errorf("failed to list buckets: %w", err)
	}

	return nil
}

// ----- Metadata scan for local engines -----

// ListTables 列出本地资源的表名（当前支持 spatialite/sqlite）
func (s *LocalEngineService) ListTables(id, tenantID uint) ([]string, error) {
    res, err := s.Get(id, tenantID)
    if err != nil { return nil, err }
    switch res.EngineType {
    case "spatialite", "sqlite":
        return s.listSQLiteTables(res.ConnectionInfo)
    case "postgresql":
        return s.listPostgresTables(res.ConnectionInfo)
    case "mysql":
        return s.listMySQLTables(res.ConnectionInfo)
    default:
        return nil, fmt.Errorf("list tables not supported for resource type: %s", res.EngineType)
    }
}

// ListFields 列出指定表的字段信息
func (s *LocalEngineService) ListFields(id, tenantID uint, table string) ([]map[string]interface{}, error) {
    res, err := s.Get(id, tenantID)
    if err != nil { return nil, err }
    switch res.EngineType {
    case "spatialite", "sqlite":
        return s.listSQLiteFields(res.ConnectionInfo, table)
    case "postgresql":
        return s.listPostgresFields(res.ConnectionInfo, table)
    case "mysql":
        return s.listMySQLFields(res.ConnectionInfo, table)
    default:
        return nil, fmt.Errorf("list fields not supported for resource type: %s", res.EngineType)
    }
}

func (s *LocalEngineService) testSpatiaLite(connInfo models.JSONMap) error {
    fullName, _ := connInfo["full_name"].(string)
    if fullName == "" { return fmt.Errorf("missing required field: full_name") }

    db, err := sql.Open("sqlite3", fullName)
    if err != nil { return fmt.Errorf("failed to open sqlite: %w", err) }
    defer db.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    if err := db.PingContext(ctx); err != nil {
        return fmt.Errorf("failed to ping sqlite: %w", err)
    }

    // Try to load SpatiaLite extension (ignore failure)
    _, _ = db.ExecContext(ctx, "SELECT load_extension('mod_spatialite')")
    return nil
}

func (s *LocalEngineService) listSQLiteTables(connInfo models.JSONMap) ([]string, error) {
    fullName, _ := connInfo["full_name"].(string)
    if fullName == "" { return nil, fmt.Errorf("missing required field: full_name") }

    db, err := sql.Open("sqlite3", fullName)
    if err != nil { return nil, fmt.Errorf("failed to open sqlite: %w", err) }
    defer db.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    // Load extension if available
    _, _ = db.ExecContext(ctx, "SELECT load_extension('mod_spatialite')")

    // Exclude internal tables
    rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='table'`)
    if err != nil { return nil, err }
    defer rows.Close()

    exclude := map[string]bool{
        "sqlite_sequence": true,
        "geometry_columns": true,
        "spatial_ref_sys": true,
        "views_geometry_columns": true,
        "virts_geometry_columns": true,
        "views_geometry_columns_auth": true,
        "views_geometry_columns_statistics": true,
        "views_geometry_columns_field_infos": true,
    }
    // Also exclude common GPKG tables if present
    exclude["gpkg_contents"] = true
    exclude["gpkg_geometry_columns"] = true

    var tables []string
    for rows.Next() {
        var name string
        if err := rows.Scan(&name); err != nil { return nil, err }
        lower := name
        if exclude[strings.ToLower(lower)] { continue }
        tables = append(tables, name)
    }
    return tables, nil
}

func (s *LocalEngineService) listSQLiteFields(connInfo models.JSONMap, table string) ([]map[string]interface{}, error) {
    fullName, _ := connInfo["full_name"].(string)
    if fullName == "" { return nil, fmt.Errorf("missing required field: full_name") }

    db, err := sql.Open("sqlite3", fullName)
    if err != nil { return nil, fmt.Errorf("failed to open sqlite: %w", err) }
    defer db.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    _, _ = db.ExecContext(ctx, "SELECT load_extension('mod_spatialite')")

    pragma := fmt.Sprintf("PRAGMA table_info(%s)", table)
    rows, err := db.QueryContext(ctx, pragma)
    if err != nil { return nil, err }
    defer rows.Close()

    // Optionally detect geometry columns
    geomCols := map[string]bool{}
    grows, _ := db.QueryContext(ctx, `SELECT f_geometry_column FROM geometry_columns WHERE LOWER(f_table_name)=LOWER(?)`, table)
    for grows != nil && grows.Next() {
        var col string
        if err := grows.Scan(&col); err == nil { geomCols[col] = true }
    }
    if grows != nil { grows.Close() }

    // 获取 SpatiaLite TypeMapper
    spatialiteMapper := format.GetTypeMapper("spatialite")

    type fieldRow struct{ cid int; name, ctype string; notnull int; dflt interface{}; pk int }
    var out []map[string]interface{}
    for rows.Next() {
        var cid, notnull, pk int
        var name, ctype string
        var dflt interface{}
        if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil { return nil, err }

        // 计算标准化类型
        var standardType string
        if geomCols[name] {
            // 几何字段
            standardType = "geometry"
        } else if spatialiteMapper != nil {
            // 使用 TypeMapper 转换
            ft := spatialiteMapper.ToCommon(ctype)
            standardType = string(ft)
        } else {
            standardType = "string"  // 默认
        }

        f := map[string]interface{}{
            "name":         name,
            "data_type":    ctype,
            "standard_type": standardType,  // 添加标准化类型
            "nullable":     notnull == 0,
            "primary_key":  pk == 1,
        }
        if geomCols[name] { f["is_geometry"] = true }
        out = append(out, f)
    }
    return out, nil
}

// ------- PostgreSQL metadata -------

func (s *LocalEngineService) pgConn(connInfo models.JSONMap) (*sql.DB, error) {
    host, _ := connInfo["host"].(string)
    host = s.normalizeHost(host)
    var port int
    switch v := connInfo["port"].(type) {
    case float64:
        port = int(v)
    case int:
        port = v
    case string:
        if parsed, err := strconv.Atoi(v); err == nil { port = parsed }
    }
    if port == 0 { port = 5432 }
    database, _ := connInfo["database"].(string)
    user, _ := connInfo["user"].(string)
    password, _ := connInfo["password"].(string)
    sslMode, _ := connInfo["sslmode"].(string)
    if sslMode == "" { sslMode = "disable" }
    dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s", host, port, user, password, database, sslMode)
    return sql.Open("postgres", dsn)
}

func (s *LocalEngineService) listPostgresTables(connInfo models.JSONMap) ([]string, error) {
    db, err := s.pgConn(connInfo)
    if err != nil { return nil, err }
    defer db.Close()
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    rows, err := db.QueryContext(ctx, `
      SELECT table_schema, table_name
      FROM information_schema.tables
      WHERE table_type='BASE TABLE'
        AND table_schema NOT IN ('pg_catalog','information_schema')
      ORDER BY table_schema, table_name`)
    if err != nil { return nil, err }
    defer rows.Close()
    var out []string
    for rows.Next() {
        var schema, name string
        if err := rows.Scan(&schema, &name); err != nil { return nil, err }
        out = append(out, fmt.Sprintf("%s.%s", schema, name))
    }
    return out, nil
}

func splitSchemaTableInput(input string) (string, string) {
    trimmed := strings.TrimSpace(input)
    if trimmed == "" { return "", "" }
    parts := strings.Split(trimmed, ".")
    if len(parts) == 1 { return "public", strings.Trim(parts[0], `"`) }
    return strings.Trim(parts[0], `"`), strings.Trim(parts[1], `"`)
}

func (s *LocalEngineService) listPostgresFields(connInfo models.JSONMap, table string) ([]map[string]interface{}, error) {
    db, err := s.pgConn(connInfo)
    if err != nil { return nil, err }
    defer db.Close()
    schema, name := splitSchemaTableInput(table)
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    rows, err := db.QueryContext(ctx, `
      SELECT column_name,
             CASE WHEN data_type='USER-DEFINED' THEN udt_name ELSE data_type END AS data_type,
             udt_name,
             is_nullable='YES' AS nullable,
             (SELECT COUNT(*)>0 FROM information_schema.table_constraints tc
              JOIN information_schema.key_column_usage kcu
                ON tc.constraint_name=kcu.constraint_name AND tc.table_schema=kcu.table_schema
             WHERE tc.table_schema=$1 AND tc.table_name=$2 AND tc.constraint_type='PRIMARY KEY'
               AND kcu.column_name=columns.column_name) AS pk
      FROM information_schema.columns columns
      WHERE table_schema=$1 AND table_name=$2
      ORDER BY ordinal_position`, schema, name)
    if err != nil { return nil, err }
    defer rows.Close()

    // 获取 PostgreSQL TypeMapper
    pgMapper := format.GetTypeMapper("postgresql")

    var out []map[string]interface{}
    for rows.Next() {
        var col, dataType, udt string
        var nullable, pk bool
        if err := rows.Scan(&col, &dataType, &udt, &nullable, &pk); err != nil { return nil, err }

        // 计算标准化类型
        var standardType string
        if pgMapper != nil {
            ft := pgMapper.ToCommon(dataType)
            standardType = string(ft)
        } else {
            standardType = "string"  // 默认
        }

        field := map[string]interface{}{
            "name":         col,
            "data_type":    dataType,
            "column_type":  udt,
            "standard_type": standardType,  // 添加标准化类型
            "nullable":     nullable,
            "primary_key":  pk,
        }
        // 检测 PostGIS 空间类型
        if strings.Contains(strings.ToLower(standardType), "geometry") ||
           strings.Contains(strings.ToLower(standardType), "point") ||
           strings.Contains(strings.ToLower(standardType), "polygon") ||
           strings.Contains(strings.ToLower(standardType), "linestring") {
            field["is_geometry"] = true
        }
        out = append(out, field)
    }
    return out, nil
}

// ------- MySQL metadata -------

func (s *LocalEngineService) mySQLConn(connInfo models.JSONMap) (*sql.DB, error) {
    host, _ := connInfo["host"].(string)
    host = s.normalizeHost(host)
    var port int
    switch v := connInfo["port"].(type) {
    case float64:
        port = int(v)
    case int:
        port = v
    case string:
        if parsed, err := strconv.Atoi(v); err == nil { port = parsed }
    }
    if port == 0 { port = 3306 }
    database, _ := connInfo["database"].(string)
    user, _ := connInfo["user"].(string)
    password, _ := connInfo["password"].(string)
    dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true", user, password, host, port, database)
    return sql.Open("mysql", dsn)
}

func (s *LocalEngineService) listMySQLTables(connInfo models.JSONMap) ([]string, error) {
    db, err := s.mySQLConn(connInfo)
    if err != nil { return nil, err }
    defer db.Close()
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    rows, err := db.QueryContext(ctx, `
      SELECT table_name FROM information_schema.tables
      WHERE table_schema = DATABASE() AND table_type='BASE TABLE'
      ORDER BY table_name`)
    if err != nil { return nil, err }
    defer rows.Close()
    var out []string
    for rows.Next() {
        var name string
        if err := rows.Scan(&name); err != nil { return nil, err }
        out = append(out, name)
    }
    return out, nil
}

func (s *LocalEngineService) listMySQLFields(connInfo models.JSONMap, table string) ([]map[string]interface{}, error) {
    db, err := s.mySQLConn(connInfo)
    if err != nil { return nil, err }
    defer db.Close()
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    rows, err := db.QueryContext(ctx, `
      SELECT column_name, data_type, column_type,
             is_nullable='YES' AS nullable,
             column_key='PRI' AS pk
      FROM information_schema.columns
      WHERE table_schema = DATABASE() AND table_name = ?
      ORDER BY ordinal_position`, table)
    if err != nil { return nil, err }
    defer rows.Close()

    // 获取 MySQL TypeMapper
    mysqlMapper := format.GetTypeMapper("mysql")

    var out []map[string]interface{}
    for rows.Next() {
        var col, dataType, colType string
        var nullable, pk bool
        if err := rows.Scan(&col, &dataType, &colType, &nullable, &pk); err != nil { return nil, err }

        // 计算标准化类型
        var standardType string
        if mysqlMapper != nil {
            ft := mysqlMapper.ToCommon(dataType)
            standardType = string(ft)
        } else {
            standardType = "string"  // 默认
        }

        out = append(out, map[string]interface{}{
            "name":         col,
            "data_type":    dataType,
            "column_type":  colType,
            "standard_type": standardType,  // 添加标准化类型
            "nullable":     nullable,
            "primary_key":  pk,
        })
    }
    return out, nil
}

// encryptConnectionInfo 加密连接信息中的敏感字段
func (s *LocalEngineService) encryptConnectionInfo(connInfo models.JSONMap) error {
	if len(s.cfg.EncryptionKey) != 32 {
		return fmt.Errorf("encryption key must be 32 bytes, got %d", len(s.cfg.EncryptionKey))
	}

	removeMetaFields(connInfo)

	// 加密 password 字段（PostgreSQL/MySQL）
	if password, ok := connInfo["password"].(string); ok && password != "" {
		encrypted, err := commonUtils.Encrypt(password, s.cfg.EncryptionKey)
		if err != nil {
			return fmt.Errorf("failed to encrypt password: %w", err)
		}
		connInfo["password"] = encrypted
	}

	// 加密 secret_key 字段（MinIO/S3）
	if secretKey, ok := connInfo["secret_key"].(string); ok && secretKey != "" {
		encrypted, err := commonUtils.Encrypt(secretKey, s.cfg.EncryptionKey)
		if err != nil {
			return fmt.Errorf("failed to encrypt secret_key: %w", err)
		}
		connInfo["secret_key"] = encrypted
	}

	return nil
}

// decryptConnectionInfo 解密连接信息中的敏感字段
func (s *LocalEngineService) decryptConnectionInfo(connInfo models.JSONMap) error {
	if len(s.cfg.EncryptionKey) != 32 {
		return fmt.Errorf("decryption key must be 32 bytes, got %d", len(s.cfg.EncryptionKey))
	}

	// 解密 password 字段（PostgreSQL/MySQL）
	if encryptedPassword, ok := connInfo["password"].(string); ok && encryptedPassword != "" {
		decrypted, err := commonUtils.Decrypt(encryptedPassword, s.cfg.EncryptionKey)
		if err != nil {
			// 如果解密失败，可能是明文密码（向后兼容）
			s.logger.Warn("failed to decrypt password, might be plaintext", "error", err)
			return err
		}
		connInfo["password"] = decrypted
	}

	// 解密 secret_key 字段（MinIO/S3）
	if encryptedSecretKey, ok := connInfo["secret_key"].(string); ok && encryptedSecretKey != "" {
		decrypted, err := commonUtils.Decrypt(encryptedSecretKey, s.cfg.EncryptionKey)
		if err != nil {
			// 如果解密失败，可能是明文密钥（向后兼容）
			s.logger.Warn("failed to decrypt secret_key, might be plaintext", "error", err)
			return err
		}
		connInfo["secret_key"] = decrypted
	}

	return nil
}

// cacheResource 缓存资源
func (s *LocalEngineService) cacheResource(resource *commonModels.Engine) {
	if resource == nil {
		return
	}
	resourceCopy := *resource
	s.cacheMu.Lock()
	s.engineCache[resourceCopy.ID] = &engineCacheEntry{
		engine:    &resourceCopy,
		expiresAt: time.Now().Add(s.cacheTTL),
	}
	s.cacheMu.Unlock()
}

// sanitizeConnectionInfo 在返回前对敏感字段进行脱敏处理
func (s *LocalEngineService) sanitizeConnectionInfo(connInfo models.JSONMap) {
	if connInfo == nil {
		return
	}
	sanitizeMap(connInfo)
}

// sanitizeMap 对任意 map[string]interface{} 进行敏感字段脱敏
func sanitizeMap(m map[string]interface{}) {
	if m == nil {
		return
	}

	if rawPassword, ok := m["password"]; ok {
		if passwordStr, ok := rawPassword.(string); ok && passwordStr != "" {
			m["_has_password"] = true
		}
		m["password"] = ""
	}

	if rawSecret, ok := m["secret_key"]; ok {
		if secretStr, ok := rawSecret.(string); ok && secretStr != "" {
			m["_has_secret_key"] = true
		}
		m["secret_key"] = ""
	}
}

// removeMetaFields 清理前端使用的临时标记字段
func removeMetaFields(connInfo models.JSONMap) {
	if connInfo == nil {
		return
	}
	delete(connInfo, "_has_password")
	delete(connInfo, "_has_secret_key")
}

// ClearCache 清除所有引擎缓存
func (s *LocalEngineService) ClearCache() {
	s.cacheMu.Lock()
	s.engineCache = make(map[uint]*engineCacheEntry)
	s.cacheMu.Unlock()
	s.logger.Info("引擎缓存已清除")
}

// ClearEngineCache 清除指定引擎的缓存
func (s *LocalEngineService) ClearEngineCache(engineID uint) {
	s.cacheMu.Lock()
	delete(s.engineCache, engineID)
	s.cacheMu.Unlock()
	s.logger.Info("引擎缓存已清除", "engine_id", engineID)
}

// handleEngineChangeEvent 处理引擎变更事件（Redis 订阅回调）
func (s *LocalEngineService) handleResourceChangeEvent(event events.EngineChangeEvent) error {
	s.logger.Info("收到引擎变更事件",
		"engine_id", event.EngineID,
		"action", event.Action,
		"timestamp", event.Timestamp)

	switch event.Action {
	case events.ActionCreate:
		// 引擎创建：不需要特殊处理，等待下次访问时自动加载
		s.logger.Debug("引擎已创建，等待首次访问时加载", "engine_id", event.EngineID)

	case events.ActionUpdate:
		// 引擎更新：清除缓存，强制下次访问时重新获取
		s.ClearEngineCache(event.EngineID)
		s.logger.Info("引擎已更新，缓存已清除", "engine_id", event.EngineID)

	case events.ActionDelete:
		// 引擎删除：清除缓存
		s.ClearEngineCache(event.EngineID)
		s.logger.Info("资源已删除，缓存已清除", "engine_id", event.EngineID)

	default:
		s.logger.Warn("未知的资源变更动作", "action", event.Action, "engine_id", event.EngineID)
	}

	return nil
}

// Stop 停止资源服务（清理资源）
func (s *LocalEngineService) Stop() {
	if s.eventSubscriber != nil {
		s.eventSubscriber.Stop()
		s.logger.Info("资源事件订阅器已停止")
	}
}
