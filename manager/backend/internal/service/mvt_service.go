package service

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/addp/common/logger"
	"github.com/addp/common/spatial"
	"github.com/addp/manager/internal/repository"
	_ "github.com/lib/pq" // PostgreSQL driver
)

// MVTService generates Mapbox Vector Tiles (MVT) from PostGIS tables for preview.
type MVTService struct {
	metadataRepo *repository.MetadataRepository
	resourceRepo *repository.ResourceRepository

	// ✅ 连接池管理 (按 engineID 缓存)
	dbPools   map[uint]*sql.DB
	poolMutex sync.RWMutex
}

func NewMVTService(meta *repository.MetadataRepository, res *repository.ResourceRepository) *MVTService {
	return &MVTService{
		metadataRepo: meta,
		resourceRepo: res,
		dbPools:      make(map[uint]*sql.DB),
	}
}

// getOrCreateDBPool 获取或创建数据库连接池 (线程安全)
func (s *MVTService) getOrCreateDBPool(ctx context.Context, engineID uint) (*sql.DB, error) {
	// 1. 先尝试读锁获取已有连接池
	s.poolMutex.RLock()
	if pool, exists := s.dbPools[resourceID]; exists {
		s.poolMutex.RUnlock()
		// 验证连接是否有效
		if err := pool.PingContext(ctx); err == nil {
			return pool, nil
		} else {
			logger.L().Warn("数据库连接池失效，准备重建", "engine_id", engineID, "error", err)
		}
	} else {
		s.poolMutex.RUnlock()
	}

	// 2. 使用写锁创建新连接池
	s.poolMutex.Lock()
	defer s.poolMutex.Unlock()

	// 双重检查 (可能其他 goroutine 已创建)
	if pool, exists := s.dbPools[resourceID]; exists {
		if err := pool.PingContext(ctx); err == nil {
			return pool, nil
		}
		// 关闭失效连接池
		pool.Close()
		delete(s.dbPools, engineID)
	}

	// 3. 获取资源配置
	res, err := s.resourceRepo.GetByID(engineID)
	if err != nil {
		return nil, fmt.Errorf("get resource failed: %w", err)
	}

	// 4. 解密连接信息
	connInfo, err := s.metadataRepo.DecryptConnectionInfo(res.ConnectionInfo)
	if err != nil {
		return nil, fmt.Errorf("decrypt connection info failed: %w", err)
	}

	// 5. 构建 DSN
	dsn, err := s.buildDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("build DSN failed: %w", err)
	}

	// 6. 创建连接池
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database failed: %w", err)
	}

	// 7. 配置连接池参数
	db.SetMaxOpenConns(25)                 // 最大打开连接数
	db.SetMaxIdleConns(5)                  // 最大空闲连接数
	db.SetConnMaxLifetime(5 * time.Minute) // 连接最大存活时间
	db.SetConnMaxIdleTime(1 * time.Minute) // 连接最大空闲时间

	// 8. 验证连接
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database failed: %w", err)
	}

	// 9. 缓存连接池
	s.dbPools[resourceID] = db
	logger.L().Info("✅ 创建数据库连接池",
		"engine_id", engineID,
		"max_open_conns", 25,
		"max_idle_conns", 5)

	return db, nil
}

// buildDSN 构建 PostgreSQL 连接字符串
func (s *MVTService) buildDSN(connInfo map[string]interface{}) (string, error) {
	host, _ := connInfo["host"].(string)
	if host == "" {
		return "", fmt.Errorf("missing host in connection info")
	}

	// Docker 环境特殊处理
	if host == "localhost" || host == "127.0.0.1" {
		if alias := os.Getenv("RESOURCE_LOCALHOST_ALIAS"); alias != "" {
			host = alias
		}
	}

	database, _ := connInfo["database"].(string)
	if database == "" {
		return "", fmt.Errorf("missing database in connection info")
	}

	password, _ := connInfo["password"].(string)

	username, _ := connInfo["username"].(string)
	if username == "" {
		username, _ = connInfo["user"].(string)
	}
	if username == "" {
		return "", fmt.Errorf("missing username in connection info")
	}

	var port string
	switch v := connInfo["port"].(type) {
	case float64:
		port = fmt.Sprintf("%.0f", v)
	case int:
		port = fmt.Sprintf("%d", v)
	case string:
		port = v
	default:
		port = "5432" // 默认端口
	}

	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		host, port, username, password, database), nil
}

// GetTile produces a single MVT tile for given z/x/y and table.
// Validates tenant access against the resource.
func (s *MVTService) GetTile(
	ctx context.Context,
	tenantID *uint,
	resourceID uint,
	schema, table, geomCol string,
	cols []string,
	z, x, y int,
	srid int,
) ([]byte, error) {
	// 1. 验证租户权限
	res, err := s.resourceRepo.GetByID(engineID)
	if err != nil {
		return nil, err
	}
	if !resourceAccessible(res, tenantID) {
		return nil, ErrResourceAccessDenied
	}

	// 2. ✅ 获取连接池 (复用已有连接)
	db, err := s.getOrCreateDBPool(ctx, engineID)
	if err != nil {
		return nil, fmt.Errorf("get db pool failed: %w", err)
	}

	// 3. 查询主键列名
	primaryKey, err := s.getPrimaryKeyColumn(ctx, db, schema, table)
	if err != nil {
		logger.L().Warn("Failed to get primary key, using 'id' as fallback",
			"error", err,
			"schema", schema,
			"table", table)
		primaryKey = "id"
	}

	// 4. 如果未指定列,查询所有列
	if len(cols) == 0 {
		allCols, err := s.getAllColumns(ctx, db, schema, table, geomCol)
		if err != nil {
			logger.L().Warn("Failed to get all columns", "error", err)
		} else {
			cols = allCols
		}
	}

	// 5. 构建 MVT SQL
	opt := spatial.MVTOptions{
		Layer:    table,
		Extent:   4096,
		Buffer:   64,
		SRID:     srid,
		Simplify: true,
	}
	sqlStr, args := spatial.BuildMVTQuery(schema, table, geomCol, cols, z, x, y, opt, primaryKey)

	// 6. ✅ 使用连接池执行查询 (不再每次创建新连接)
	var mvt []byte
	scanErr := db.QueryRowContext(ctx, sqlStr, args...).Scan(&mvt)
	if scanErr != nil {
		logger.L().Error("MVT query failed",
			"error", scanErr,
			"engine_id", engineID,
			"schema", schema,
			"table", table,
			"z", z, "x", x, "y", y)
		return nil, scanErr
	}

	if mvt == nil {
		return []byte{}, nil
	}

	return mvt, nil
}

// getPrimaryKeyColumn 查询表的主键列名
func (s *MVTService) getPrimaryKeyColumn(ctx context.Context, db *sql.DB, schema, table string) (string, error) {
	query := `
		SELECT a.attname
		FROM pg_index i
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
		WHERE i.indrelid = ($1 || '.' || $2)::regclass
		  AND i.indisprimary
		LIMIT 1
	`
	var pkColumn string
	err := db.QueryRowContext(ctx, query, schema, table).Scan(&pkColumn)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("query primary key failed: %w", err)
	}
	return pkColumn, nil
}

// getAllColumns 查询表的所有列名（排除几何列）
func (s *MVTService) getAllColumns(ctx context.Context, db *sql.DB, schema, table, geomCol string) ([]string, error) {
	query := `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = $1
		  AND table_name = $2
		  AND column_name != $3
		ORDER BY ordinal_position
	`
	rows, err := db.QueryContext(ctx, query, schema, table, geomCol)
	if err != nil {
		return nil, fmt.Errorf("query columns failed: %w", err)
	}
	defer rows.Close()

	var cols []string
	for rows.Next() {
		var colName string
		if err := rows.Scan(&colName); err != nil {
			return nil, err
		}
		cols = append(cols, colName)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return cols, nil
}

// Close 关闭所有连接池 (服务关闭时调用)
func (s *MVTService) Close() error {
	s.poolMutex.Lock()
	defer s.poolMutex.Unlock()

	var errs []error
	for engineID, pool := range s.dbPools {
		if err := pool.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close pool for resource %d: %w", engineID, err))
		}
	}

	s.dbPools = make(map[uint]*sql.DB)

	if len(errs) > 0 {
		return fmt.Errorf("close pools failed: %v", errs)
	}

	logger.L().Info("✅ 所有数据库连接池已关闭")
	return nil
}

// Note: access policy and ErrResourceAccessDenied are defined in metadata_service.go
