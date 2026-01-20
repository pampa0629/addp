package mvt

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/spatial"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// TileGenerator MVT 瓦片生成器
// 负责连接 PostgreSQL/PostGIS 并生成 MVT 瓦片
type TileGenerator struct {
	resourceService ResourceService // 用于获取数据源连接信息

	// ✅ 连接池管理 (按 engineID 缓存)
	dbPools   map[uint]*sql.DB
	poolMutex sync.RWMutex
}

// ResourceService 资源服务接口（避免循环依赖）
type ResourceService interface {
	GetEngine(engineID, tenantID uint) (*commonModels.Engine, error)
}

// NewTileGenerator 创建瓦片生成器
func NewTileGenerator(resourceService ResourceService) *TileGenerator {
	return &TileGenerator{
		resourceService: resourceService,
		dbPools:         make(map[uint]*sql.DB),
	}
}

// TileGenerationParams 瓦片生成参数
type TileGenerationParams struct {
	EngineID     uint
	TenantID     uint
	Schema       string
	Table        string
	GeomColumn   string
	SRID         int
	PrimaryKey   string
	Z, X, Y      int
}

// GenerateTile 生成 MVT 瓦片
// 返回: MVT 二进制数据（未压缩）
func (g *TileGenerator) GenerateTile(
	ctx context.Context,
	params TileGenerationParams,
) ([]byte, error) {
	// ✅ 使用连接池（不再每次创建新连接）
	db, err := g.getOrCreateDBPool(ctx, params.EngineID, params.TenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get db pool: %w", err)
	}

	// 注意：不再 defer db.Close()，连接池会自动管理

	// 构建 MVT 查询（使用 common/spatial 统一实现）
	sqlStr, args := g.buildMVTQuery(
		params.Schema,
		params.Table,
		params.GeomColumn,
		params.SRID,
		params.Z,
		params.X,
		params.Y,
		params.PrimaryKey,
	)

	// 详细日志：记录瓦片请求和 SQL
	logger.L().Info("📍 开始生成 MVT 瓦片",
		"engine_id", params.EngineID,
		"tenant_id", params.TenantID,
		"z", params.Z, "x", params.X, "y", params.Y,
		"table", fmt.Sprintf("%s.%s", params.Schema, params.Table),
		"geom_col", params.GeomColumn,
		"srid", params.SRID,
		"primary_key", params.PrimaryKey)

	// 记录 SQL 查询（用于调试）
	logger.L().Debug("🔍 MVT SQL 查询",
		"sql", sqlStr,
		"args", args)

	// 执行查询
	var mvtData []byte
	err = db.QueryRowContext(ctx, sqlStr, args...).Scan(&mvtData)
	if err != nil {
		if err == sql.ErrNoRows {
			logger.L().Info("📭 MVT 查询无结果（空瓦片）",
				"z", params.Z, "x", params.X, "y", params.Y,
				"table", fmt.Sprintf("%s.%s", params.Schema, params.Table))
			return []byte{}, nil // 空瓦片
		}
		logger.L().Error("❌ MVT 查询失败",
			"error", err,
			"z", params.Z, "x", params.X, "y", params.Y,
			"schema", params.Schema,
			"table", params.Table,
			"sql", sqlStr)
		return nil, fmt.Errorf("failed to execute MVT query: %w", err)
	}

	logger.L().Info("✅ MVT 瓦片生成完成",
		"z", params.Z, "x", params.X, "y", params.Y,
		"table", fmt.Sprintf("%s.%s", params.Schema, params.Table),
		"data_size", fmt.Sprintf("%d bytes", len(mvtData)))

	return mvtData, nil
}

// buildMVTQuery 构建 MVT 查询 SQL（使用 common/spatial 统一实现）
func (g *TileGenerator) buildMVTQuery(
	schema, table, geomColumn string,
	srid, z, x, y int,
	primaryKey string,
) (string, []interface{}) {
	// 使用 common/spatial.BuildMVTQuery 统一实现
	// ✅ 根据 zoom 级别动态启用简化：
	//   - z < 10: 简化几何，减少数据量，防止浏览器崩溃
	//   - z >= 10: 保留完整精度，提供详细展示
	simplify := z < 10

	opt := spatial.MVTOptions{
		Layer:    table,
		Extent:   4096,
		Buffer:   64,
		SRID:     srid,
		Simplify: simplify,
	}

	if simplify {
		logger.L().Debug("🔧 启用几何简化",
			"z", z,
			"tolerance", spatial.SimplifyTolerance(z))
	}

	return spatial.BuildMVTQuery(schema, table, geomColumn, []string{}, z, x, y, opt, primaryKey)
}

// VerifySRID 验证表中几何列的 SRID 是否与期望的 SRID 一致
// 返回: (actualSRID, error)
func (g *TileGenerator) VerifySRID(
	ctx context.Context,
	engineID, tenantID uint,
	schema, table, geomColumn string,
	expectedSRID int,
) (int, error) {
	// ✅ 使用连接池
	db, err := g.getOrCreateDBPool(ctx, engineID, tenantID)
	if err != nil {
		return 0, fmt.Errorf("failed to get db pool: %w", err)
	}

	// 查询几何列的 SRID
	query := fmt.Sprintf(`
		SELECT ST_SRID("%s")
		FROM "%s"."%s"
		WHERE "%s" IS NOT NULL
		LIMIT 1
	`, geomColumn, schema, table, geomColumn)

	var actualSRID int
	err = db.QueryRowContext(ctx, query).Scan(&actualSRID)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("表中没有非空几何数据，无法验证 SRID")
		}
		return 0, fmt.Errorf("failed to query SRID: %w", err)
	}

	// 验证 SRID 是否匹配
	if actualSRID != expectedSRID {
		return actualSRID, fmt.Errorf("SRID 不匹配: 元数据记录为 %d，但表中实际为 %d", expectedSRID, actualSRID)
	}

	logger.L().Info("✅ SRID 验证通过",
		"table", fmt.Sprintf("%s.%s", schema, table),
		"geom_column", geomColumn,
		"srid", actualSRID)

	return actualSRID, nil
}

// GetSpatialExtent 获取空间表的范围（WGS84）
func (g *TileGenerator) GetSpatialExtent(
	ctx context.Context,
	engineID, tenantID uint,
	schema, table, geomColumn string,
) ([]float64, error) {
	// ✅ 使用连接池
	db, err := g.getOrCreateDBPool(ctx, engineID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get db pool: %w", err)
	}

	// 查询范围（转换为 WGS84）
	query := fmt.Sprintf(`
		SELECT
			ST_XMin(extent) as min_lng,
			ST_YMin(extent) as min_lat,
			ST_XMax(extent) as max_lng,
			ST_YMax(extent) as max_lat
		FROM (
			SELECT ST_Extent(ST_Transform("%s", 4326)) as extent
			FROM "%s"."%s"
		) t
	`, geomColumn, schema, table)

	var minLng, minLat, maxLng, maxLat sql.NullFloat64
	err = db.QueryRowContext(ctx, query).Scan(&minLng, &minLat, &maxLng, &maxLat)
	if err != nil {
		return nil, fmt.Errorf("failed to query extent: %w", err)
	}

	if !minLng.Valid || !minLat.Valid || !maxLng.Valid || !maxLat.Valid {
		return nil, fmt.Errorf("no valid extent found")
	}

	return []float64{minLng.Float64, minLat.Float64, maxLng.Float64, maxLat.Float64}, nil
}

// GetOrCreateDBPool 获取或创建数据库连接池 (线程安全) - 导出给 service 层使用
func (g *TileGenerator) GetOrCreateDBPool(ctx context.Context, engineID, tenantID uint) (*sql.DB, error) {
	return g.getOrCreateDBPool(ctx, engineID, tenantID)
}

// GetPrimaryKeyColumn 查询表的主键列名 - 导出给 service 层使用
func (g *TileGenerator) GetPrimaryKeyColumn(ctx context.Context, db *sql.DB, schema, table string) (string, error) {
	return g.getPrimaryKeyColumn(ctx, db, schema, table)
}

// GetAllColumns 查询表的所有列名（排除几何列）- 导出给 service 层使用
func (g *TileGenerator) GetAllColumns(ctx context.Context, db *sql.DB, schema, table, geomCol string) ([]string, error) {
	return g.getAllColumns(ctx, db, schema, table, geomCol)
}

// ============================================================================
// 连接池管理（从 service/mvt_service.go 迁移）
// ============================================================================

// getOrCreateDBPool 获取或创建数据库连接池 (线程安全)
func (g *TileGenerator) getOrCreateDBPool(ctx context.Context, engineID, tenantID uint) (*sql.DB, error) {
	// 1. 先尝试读锁获取已有连接池
	g.poolMutex.RLock()
	if pool, exists := g.dbPools[engineID]; exists {
		g.poolMutex.RUnlock()
		// 验证连接是否有效
		if err := pool.PingContext(ctx); err == nil {
			return pool, nil
		} else {
			logger.L().Warn("数据库连接池失效，准备重建", "engine_id", engineID, "error", err)
		}
	} else {
		g.poolMutex.RUnlock()
	}

	// 2. 使用写锁创建新连接池
	g.poolMutex.Lock()
	defer g.poolMutex.Unlock()

	// 双重检查 (可能其他 goroutine 已创建)
	if pool, exists := g.dbPools[engineID]; exists {
		if err := pool.PingContext(ctx); err == nil {
			return pool, nil
		}
		// 关闭失效连接池
		pool.Close()
		delete(g.dbPools, engineID)
	}

	// 3. 获取资源配置
	resource, err := g.resourceService.GetEngine(engineID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource: %w", err)
	}

	// 4. 构建连接字符串
	connStr, err := g.buildDSN(resource)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}

	// 5. 创建连接池
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// 6. 配置连接池参数
	db.SetMaxOpenConns(25)                 // 最大打开连接数
	db.SetMaxIdleConns(5)                  // 最大空闲连接数
	db.SetConnMaxLifetime(5 * time.Minute) // 连接最大存活时间
	db.SetConnMaxIdleTime(1 * time.Minute) // 连接最大空闲时间

	// 7. 验证连接
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// 8. 缓存连接池
	g.dbPools[engineID] = db
	logger.L().Info("✅ 创建数据库连接池",
		"engine_id", engineID,
		"max_open_conns", 25,
		"max_idle_conns", 5)

	return db, nil
}

// buildDSN 构建 PostgreSQL 连接字符串
func (g *TileGenerator) buildDSN(engine *commonModels.Engine) (string, error) {
	connStr, err := commonModels.BuildConnectionString(engine)
	if err != nil {
		return "", fmt.Errorf("failed to build connection string: %w", err)
	}

	// Docker 环境特殊处理 (如果连接字符串包含 localhost)
	if alias := os.Getenv("RESOURCE_LOCALHOST_ALIAS"); alias != "" {
		// 简单替换 (更健壮的实现需要解析 DSN)
		// 这里假设 BuildConnectionString 已经处理了 localhost 替换
		// 如果需要额外处理，可以在这里添加
	}

	return connStr, nil
}

// getPrimaryKeyColumn 查询表的主键列名
func (g *TileGenerator) getPrimaryKeyColumn(ctx context.Context, db *sql.DB, schema, table string) (string, error) {
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
func (g *TileGenerator) getAllColumns(ctx context.Context, db *sql.DB, schema, table, geomCol string) ([]string, error) {
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
func (g *TileGenerator) Close() error {
	g.poolMutex.Lock()
	defer g.poolMutex.Unlock()

	var errs []error
	for engineID, pool := range g.dbPools {
		if err := pool.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close pool for engine %d: %w", engineID, err))
		}
	}

	g.dbPools = make(map[uint]*sql.DB)

	if len(errs) > 0 {
		return fmt.Errorf("close pools failed: %v", errs)
	}

	logger.L().Info("✅ 所有数据库连接池已关闭")
	return nil
}
