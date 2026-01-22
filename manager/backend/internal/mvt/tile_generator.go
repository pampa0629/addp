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
	EngineID           uint
	TenantID           uint
	Schema             string
	Table              string
	GeomColumn         string
	SRID               int
	PrimaryKey         string
	Z, X, Y            int
	OptimizationConfig *commonModels.OptimizationConfig // v2.0 优化配置
}

// GenerateTile 生成 MVT 瓦片
// 返回: MVT 二进制数据（未压缩）
func (g *TileGenerator) GenerateTile(
	ctx context.Context,
	params TileGenerationParams,
) ([]byte, error) {
	// ✅ 如果有优化配置，使用三阶段优化流程
	if params.OptimizationConfig != nil {
		return g.generateTileWithOptimization(ctx, params)
	}

	tileStartTime := time.Now()

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
		nil, // 不使用优化配置
	)

	// 详细日志：记录瓦片请求和 SQL
	logger.L().Info("📍 开始生成 MVT 瓦片（无优化）",
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
	queryStart := time.Now()
	var mvtData []byte
	err = db.QueryRowContext(ctx, sqlStr, args...).Scan(&mvtData)
	queryDuration := time.Since(queryStart)

	if err != nil {
		if err == sql.ErrNoRows {
			logger.L().Info("📭 MVT 查询无结果（空瓦片）",
				"z", params.Z, "x", params.X, "y", params.Y,
				"table", fmt.Sprintf("%s.%s", params.Schema, params.Table),
				"query_duration_ms", queryDuration.Milliseconds())
			return []byte{}, nil // 空瓦片
		}
		logger.L().Error("❌ MVT 查询失败",
			"error", err,
			"z", params.Z, "x", params.X, "y", params.Y,
			"schema", params.Schema,
			"table", params.Table,
			"query_duration_ms", queryDuration.Milliseconds(),
			"sql", sqlStr)
		return nil, fmt.Errorf("failed to execute MVT query: %w", err)
	}

	totalDuration := time.Since(tileStartTime)
	tileSizeMB := float64(len(mvtData)) / (1024 * 1024)

	logger.L().Info("✅ MVT 瓦片生成完成（无优化）",
		"z", params.Z, "x", params.X, "y", params.Y,
		"table", fmt.Sprintf("%s.%s", params.Schema, params.Table),
		"data_size_mb", fmt.Sprintf("%.2f", tileSizeMB),
		"query_duration_ms", queryDuration.Milliseconds(),
		"total_duration_ms", totalDuration.Milliseconds())

	return mvtData, nil
}

// buildMVTQuery 构建 MVT 查询 SQL（使用 common/spatial 统一实现）
func (g *TileGenerator) buildMVTQuery(
	schema, table, geomColumn string,
	srid, z, x, y int,
	primaryKey string,
	config *commonModels.OptimizationConfig,
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

// ============================================================================
// 三阶段优化流程（v2.0）
// ============================================================================

// generateTileWithOptimization 使用三阶段优化流程生成瓦片
// 优化顺序：Extent 优化 → 对象采样 → 几何简化
func (g *TileGenerator) generateTileWithOptimization(
	ctx context.Context,
	params TileGenerationParams,
) ([]byte, error) {
	tileStartTime := time.Now()
	config := params.OptimizationConfig
	if config == nil {
		config = &commonModels.OptimizationConfig{}
		*config = commonModels.DefaultOptimizationConfig()
	}

	logger.L().Info("🔄 启用三阶段优化流程",
		"z", params.Z, "x", params.X, "y", params.Y,
		"table", fmt.Sprintf("%s.%s", params.Schema, params.Table),
		"extent_blur_level", config.ExtentOptimization.BlurLevel,
		"sampling_polygon_ratio", fmt.Sprintf("%.2f", config.Sampling.PolygonLine.CumulativeSizeRatio),
		"sampling_point_ratio", fmt.Sprintf("%.2f", config.Sampling.Point.SampleRatio),
		"simplification_algo", config.Simplification.Algorithm,
		"simplification_tolerance_mult", fmt.Sprintf("%.2f", config.Simplification.ToleranceMultiplier),
		"size_thresholds_no_opt", fmt.Sprintf("%.2f MB", config.TileSizeThresholds.NoOptimizationMB),
		"size_thresholds_stop", fmt.Sprintf("%.2f MB", config.TileSizeThresholds.StopOptimizationMB))

	// Step A: 应用属性优化（基于 zoom）
	stepAStart := time.Now()
	var columns []string
	if config.AttributePruning.Enabled && params.Z <= config.AttributePruning.ZoomThreshold {
		// z0-z8: 仅主键
		if params.PrimaryKey != "" {
			columns = []string{params.PrimaryKey}
		}
		logger.L().Debug("🔧 属性优化：仅返回主键", "z", params.Z)
	} else {
		// z9+: 全部属性（传空数组让 BuildMVTQuery 返回所有列）
		columns = []string{}
		logger.L().Debug("🔧 属性优化：返回全部属性", "z", params.Z)
	}
	logger.L().Debug("📝 属性优化耗时", "duration_ms", time.Since(stepAStart).Milliseconds())

	// Step B: 生成基础瓦片（使用默认 Extent 4096）
	stepBStart := time.Now()
	mvtData, err := g.generateBaseTile(ctx, params, columns, 4096)
	if err != nil {
		return nil, err
	}
	stepBDuration := time.Since(stepBStart)

	tileSizeMB := float64(len(mvtData)) / (1024 * 1024)
	noOptMB := config.TileSizeThresholds.NoOptimizationMB
	stopOptMB := config.TileSizeThresholds.StopOptimizationMB

	logger.L().Info("📊 基础瓦片大小",
		"size_mb", fmt.Sprintf("%.2f", tileSizeMB),
		"threshold", fmt.Sprintf("%.2f", noOptMB),
		"duration_ms", stepBDuration.Milliseconds())

	// Step C: 检查大小，决定是否优化
	if tileSizeMB < noOptMB {
		totalDuration := time.Since(tileStartTime)
		logger.L().Info("✅ 瓦片大小符合要求，无需优化",
			"size_mb", fmt.Sprintf("%.2f", tileSizeMB),
			"z", params.Z, "x", params.X, "y", params.Y,
			"total_duration_ms", totalDuration.Milliseconds())
		return mvtData, nil
	}

	// ════════════════════════════════════════════════════════
	// 进入优化流程（新顺序）
	// ════════════════════════════════════════════════════════

	// Step 1: Extent 优化
	logger.L().Info("🔧 Step 1: Extent 优化（模糊度）",
		"blur_level", config.ExtentOptimization.BlurLevel)
	step1Start := time.Now()
	targetExtent := config.GetExtent() // 基于 blur_level 计算
	mvtData, err = g.generateBaseTile(ctx, params, columns, targetExtent)
	if err != nil {
		return nil, err
	}
	step1Duration := time.Since(step1Start)
	tileSizeMB = float64(len(mvtData)) / (1024 * 1024)

	logger.L().Info("📊 Step 1 完成: Extent 优化后大小",
		"size_mb", fmt.Sprintf("%.2f", tileSizeMB),
		"extent", targetExtent,
		"duration_ms", step1Duration.Milliseconds())

	if tileSizeMB < noOptMB {
		totalDuration := time.Since(tileStartTime)
		logger.L().Info("✅ Step 1 后符合要求，停止优化",
			"size_mb", fmt.Sprintf("%.2f", tileSizeMB),
			"z", params.Z, "x", params.X, "y", params.Y,
			"total_duration_ms", totalDuration.Milliseconds())
		return mvtData, nil
	}

	// Step 2: 对象采样
	logger.L().Info("🔧 Step 2: 对象采样",
		"polygon_ratio", fmt.Sprintf("%.2f", config.Sampling.PolygonLine.CumulativeSizeRatio),
		"point_ratio", fmt.Sprintf("%.2f", config.Sampling.Point.SampleRatio))
	step2Start := time.Now()
	mvtData, err = g.generateTileWithSampling(ctx, params, columns, targetExtent, config)
	if err != nil {
		return nil, err
	}
	step2Duration := time.Since(step2Start)
	tileSizeMB = float64(len(mvtData)) / (1024 * 1024)

	logger.L().Info("📊 Step 2 完成: 对象采样后大小",
		"size_mb", fmt.Sprintf("%.2f", tileSizeMB),
		"duration_ms", step2Duration.Milliseconds())

	if tileSizeMB < noOptMB {
		totalDuration := time.Since(tileStartTime)
		logger.L().Info("✅ Step 2 后符合要求，停止优化",
			"size_mb", fmt.Sprintf("%.2f", tileSizeMB),
			"z", params.Z, "x", params.X, "y", params.Y,
			"total_duration_ms", totalDuration.Milliseconds())
		return mvtData, nil
	}

	if tileSizeMB < stopOptMB {
		totalDuration := time.Since(tileStartTime)
		logger.L().Info("✅ 大小在停止优化阈值内，跳过几何简化",
			"size_mb", fmt.Sprintf("%.2f", tileSizeMB),
			"stop_threshold", fmt.Sprintf("%.2f", stopOptMB),
			"z", params.Z, "x", params.X, "y", params.Y,
			"total_duration_ms", totalDuration.Milliseconds())
		return mvtData, nil
	}

	// Step 3: 几何简化
	logger.L().Info("🔧 Step 3: 几何简化",
		"algorithm", config.Simplification.Algorithm,
		"tolerance_mult", fmt.Sprintf("%.2f", config.Simplification.ToleranceMultiplier))
	step3Start := time.Now()
	mvtData, err = g.generateTileWithSimplification(ctx, params, columns, targetExtent, config)
	if err != nil {
		return nil, err
	}
	step3Duration := time.Since(step3Start)
	tileSizeMB = float64(len(mvtData)) / (1024 * 1024)

	totalDuration := time.Since(tileStartTime)
	logger.L().Info("✅ 优化流程完成（全部3步）",
		"z", params.Z, "x", params.X, "y", params.Y,
		"table", fmt.Sprintf("%s.%s", params.Schema, params.Table),
		"final_size_mb", fmt.Sprintf("%.2f", tileSizeMB),
		"step1_extent_ms", step1Duration.Milliseconds(),
		"step2_sampling_ms", step2Duration.Milliseconds(),
		"step3_simplification_ms", step3Duration.Milliseconds(),
		"total_duration_ms", totalDuration.Milliseconds(),
		"algo", config.Simplification.Algorithm)

	return mvtData, nil
}

// generateBaseTile 生成基础瓦片（指定 Extent）
func (g *TileGenerator) generateBaseTile(
	ctx context.Context,
	params TileGenerationParams,
	columns []string,
	extent int,
) ([]byte, error) {
	queryStart := time.Now()

	db, err := g.getOrCreateDBPool(ctx, params.EngineID, params.TenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get db pool: %w", err)
	}

	// 构建简单的 MVT 查询（不使用采样和简化）
	opt := spatial.MVTOptions{
		Layer:  params.Table,
		Extent: extent,
		Buffer: 64,
		SRID:   params.SRID,
	}

	sqlStr, args := spatial.BuildMVTQuery(
		params.Schema,
		params.Table,
		params.GeomColumn,
		columns,
		params.Z, params.X, params.Y,
		opt,
		params.PrimaryKey,
	)

	logger.L().Debug("🔍 生成基础瓦片 SQL",
		"extent", extent,
		"columns_count", len(columns),
		"sql_length", len(sqlStr))

	var mvtData []byte
	err = db.QueryRowContext(ctx, sqlStr, args...).Scan(&mvtData)
	queryDuration := time.Since(queryStart)

	if err != nil {
		if err == sql.ErrNoRows {
			logger.L().Debug("📭 基础瓦片查询无结果",
				"extent", extent,
				"query_duration_ms", queryDuration.Milliseconds())
			return []byte{}, nil
		}
		logger.L().Error("❌ 基础瓦片查询失败",
			"error", err,
			"extent", extent,
			"query_duration_ms", queryDuration.Milliseconds())
		return nil, fmt.Errorf("failed to execute MVT query: %w", err)
	}

	logger.L().Debug("✅ 基础瓦片查询完成",
		"extent", extent,
		"data_size_bytes", len(mvtData),
		"query_duration_ms", queryDuration.Milliseconds())

	return mvtData, nil
}

// generateTileWithSampling 生成带对象采样的瓦片
func (g *TileGenerator) generateTileWithSampling(
	ctx context.Context,
	params TileGenerationParams,
	columns []string,
	extent int,
	config *commonModels.OptimizationConfig,
) ([]byte, error) {
	queryStart := time.Now()

	db, err := g.getOrCreateDBPool(ctx, params.EngineID, params.TenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get db pool: %w", err)
	}

	// 构建带采样的 MVT 查询
	samplingParams := &spatial.SamplingParams{
		Enabled: true,
		PolygonLine: spatial.PolygonLineSampling{
			CumulativeSizeRatio:  config.Sampling.PolygonLine.CumulativeSizeRatio,
			MaxFeatureCountRatio: config.Sampling.PolygonLine.MaxFeatureCountRatio,
		},
		Point: spatial.PointSampling{
			SampleRatio: config.Sampling.Point.SampleRatio,
		},
	}

	opt := spatial.MVTOptions{
		Layer:          params.Table,
		Extent:         extent,
		Buffer:         64,
		SRID:           params.SRID,
		SamplingConfig: samplingParams,
	}

	sqlStr, args := spatial.BuildMVTQuery(
		params.Schema,
		params.Table,
		params.GeomColumn,
		columns,
		params.Z, params.X, params.Y,
		opt,
		params.PrimaryKey,
	)

	logger.L().Debug("🔍 生成采样瓦片 SQL",
		"extent", extent,
		"polygon_ratio", fmt.Sprintf("%.2f", config.Sampling.PolygonLine.CumulativeSizeRatio),
		"point_ratio", fmt.Sprintf("%.2f", config.Sampling.Point.SampleRatio),
		"sql_length", len(sqlStr))

	var mvtData []byte
	err = db.QueryRowContext(ctx, sqlStr, args...).Scan(&mvtData)
	queryDuration := time.Since(queryStart)

	if err != nil {
		if err == sql.ErrNoRows {
			logger.L().Debug("📭 采样瓦片查询无结果",
				"query_duration_ms", queryDuration.Milliseconds())
			return []byte{}, nil
		}
		logger.L().Error("❌ 采样瓦片查询失败",
			"error", err,
			"query_duration_ms", queryDuration.Milliseconds())
		return nil, fmt.Errorf("failed to execute MVT query with sampling: %w", err)
	}

	logger.L().Debug("✅ 采样瓦片查询完成",
		"data_size_bytes", len(mvtData),
		"query_duration_ms", queryDuration.Milliseconds())

	return mvtData, nil
}

// generateTileWithSimplification 生成带几何简化的瓦片（累积优化）
func (g *TileGenerator) generateTileWithSimplification(
	ctx context.Context,
	params TileGenerationParams,
	columns []string,
	extent int,
	config *commonModels.OptimizationConfig,
) ([]byte, error) {
	queryStart := time.Now()

	db, err := g.getOrCreateDBPool(ctx, params.EngineID, params.TenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get db pool: %w", err)
	}

	// 继续使用采样参数（累积优化）
	samplingParams := &spatial.SamplingParams{
		Enabled: true,
		PolygonLine: spatial.PolygonLineSampling{
			CumulativeSizeRatio:  config.Sampling.PolygonLine.CumulativeSizeRatio,
			MaxFeatureCountRatio: config.Sampling.PolygonLine.MaxFeatureCountRatio,
		},
		Point: spatial.PointSampling{
			SampleRatio: config.Sampling.Point.SampleRatio,
		},
	}

	// 启用几何简化
	simplificationParams := &spatial.SimplificationParams{
		Enabled:             true,
		ToleranceMultiplier: config.Simplification.ToleranceMultiplier,
		Algorithm:           config.Simplification.Algorithm,
	}

	opt := spatial.MVTOptions{
		Layer:                params.Table,
		Extent:               extent,
		Buffer:               64,
		SRID:                 params.SRID,
		SamplingConfig:       samplingParams,
		SimplificationConfig: simplificationParams,
	}

	sqlStr, args := spatial.BuildMVTQuery(
		params.Schema,
		params.Table,
		params.GeomColumn,
		columns,
		params.Z, params.X, params.Y,
		opt,
		params.PrimaryKey,
	)

	logger.L().Debug("🔍 生成简化瓦片 SQL",
		"extent", extent,
		"algorithm", config.Simplification.Algorithm,
		"tolerance_mult", fmt.Sprintf("%.2f", config.Simplification.ToleranceMultiplier),
		"sql_length", len(sqlStr))

	var mvtData []byte
	err = db.QueryRowContext(ctx, sqlStr, args...).Scan(&mvtData)
	queryDuration := time.Since(queryStart)

	if err != nil {
		if err == sql.ErrNoRows {
			logger.L().Debug("📭 简化瓦片查询无结果",
				"query_duration_ms", queryDuration.Milliseconds())
			return []byte{}, nil
		}
		logger.L().Error("❌ 简化瓦片查询失败",
			"error", err,
			"algorithm", config.Simplification.Algorithm,
			"query_duration_ms", queryDuration.Milliseconds())
		return nil, fmt.Errorf("failed to execute MVT query with simplification: %w", err)
	}

	logger.L().Debug("✅ 简化瓦片查询完成",
		"data_size_bytes", len(mvtData),
		"query_duration_ms", queryDuration.Milliseconds(),
		"algorithm", config.Simplification.Algorithm)

	return mvtData, nil
}
