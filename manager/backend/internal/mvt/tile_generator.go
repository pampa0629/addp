package mvt

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/spatial"
)

// TileGenerator MVT 瓦片生成器
// 负责连接 PostgreSQL/PostGIS 并生成 MVT 瓦片
type TileGenerator struct {
	resourceService ResourceService // 用于获取数据源连接信息
	poolConfig      *plugin.PoolConfig
}

// ResourceService 资源服务接口（避免循环依赖）
type ResourceService interface {
	GetEngine(engineID uint) (*commonModels.Engine, error)
}

// NewTileGenerator 创建瓦片生成器
// maxDBConns: 每个数据库的最大连接数，0 表示使用默认值 10
func NewTileGenerator(resourceService ResourceService, maxDBConns int) *TileGenerator {
	if maxDBConns <= 0 {
		maxDBConns = 10 // 默认值
	}
	poolConfig := plugin.DefaultPoolConfig()
	poolConfig.MaxOpenConns = maxDBConns
	poolConfig.MaxIdleConns = maxDBConns

	return &TileGenerator{
		resourceService: resourceService,
		poolConfig:      poolConfig,
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
	MaxZoom            int                              // 最大 zoom 级别（用于分层 Extent 策略）
	OptimizationConfig *commonModels.OptimizationConfig // v3.0 优化配置
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
		params.Table, // 使用从 QuickView 传入的表名（可能是 MV）
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
	originalTable := table

	opt := spatial.MVTOptions{
		Layer:  originalTable, // Layer 名称使用原始表名（前端显示）
		Extent: 1024,
		Buffer: mvtBufferForExtent(1024),
		SRID:   srid,
	}

	return spatial.BuildMVTQuery(schema, table, geomColumn, []string{}, z, x, y, opt, primaryKey)
}

// QuerySourceSRID 查询表中几何列的真实 SRID。
func (g *TileGenerator) QuerySourceSRID(
	ctx context.Context,
	engineID, tenantID uint,
	schema, table, geomColumn string,
) (int, error) {
	// ✅ 使用连接池
	db, err := g.getOrCreateDBPool(ctx, engineID, tenantID)
	if err != nil {
		return 0, fmt.Errorf("failed to get db pool: %w", err)
	}

	query := spatial.BuildPostGISSRIDQuery(schema, table, geomColumn)

	var actualSRID int
	err = db.QueryRowContext(ctx, query).Scan(&actualSRID)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("表中没有非空几何数据，无法验证 SRID")
		}
		return 0, fmt.Errorf("failed to query SRID: %w", err)
	}

	logger.L().Info("✅ 源表 SRID 查询完成",
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

	query := spatial.BuildPostGISExtentQuery(schema, table, geomColumn)

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

// getOrCreateDBPool 获取或创建数据库连接池。底层连接池由 common dbbridge/plugin 统一管理。
func (g *TileGenerator) getOrCreateDBPool(ctx context.Context, engineID, tenantID uint) (*sql.DB, error) {
	resource, err := g.resourceService.GetEngine(engineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource: %w", err)
	}

	gormDB, err := spatial.GetPostGISPool(resource, g.poolConfig)
	if err != nil {
		return nil, err
	}

	db, err := gormDB.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql db from pool: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}
	return db, nil
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
	plugin.CloseAllPools()
	logger.L().Info("✅ 所有数据库连接池已关闭")
	return nil
}

// ============================================================================
// 单阶段 Extent 优化流程（v3.0）
// 仅保留分层 Extent 策略和动态减半，移除采样和几何简化
// ============================================================================

// generateTileWithOptimization 使用单阶段 Extent 优化生成瓦片 (v3.0)
// 优化流程：属性优化 → 分层 Extent → 动态减半
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

	logger.L().Info("🔄 启用 v3.0 单阶段 Extent 优化流程",
		"z", params.Z, "x", params.X, "y", params.Y,
		"max_zoom", params.MaxZoom,
		"table", fmt.Sprintf("%s.%s", params.Schema, params.Table),
		"max_size_mb", fmt.Sprintf("%.2f MB", config.TileSizeThresholds.MaxSizeMB),
		"max_zoom_extent", config.ExtentOptimization.MaxZoomExtent,
		"base_extent", config.ExtentOptimization.BaseExtent,
		"min_extent", config.ExtentOptimization.MinExtent)

	// Step A: 应用属性优化（基于 zoom）
	stepAStart := time.Now()
	var columns []string
	if config.AttributePruning.Enabled && params.Z <= config.AttributePruning.ZoomThreshold {
		// z0-zN: 仅主键
		if params.PrimaryKey != "" {
			columns = []string{params.PrimaryKey}
		}
		logger.L().Debug("🔧 属性优化：仅返回主键", "z", params.Z, "primary_key", params.PrimaryKey)
	} else {
		// z(N+1)+: 全部属性
		columns = []string{}
		logger.L().Debug("🔧 属性优化：返回全部属性", "z", params.Z)
	}
	logger.L().Debug("📝 属性优化耗时", "duration_ms", time.Since(stepAStart).Milliseconds())

	// Step B: 获取初始 Extent（分层策略）
	initialExtent := config.ExtentOptimization.GetExtentForZoom(params.Z, params.MaxZoom)
	logger.L().Info("📊 使用分层 Extent 策略",
		"z", params.Z,
		"max_zoom", params.MaxZoom,
		"initial_extent", initialExtent,
		"reason", fmt.Sprintf("z==%d (max_zoom): %d, z<%d: %d",
			params.MaxZoom, config.ExtentOptimization.MaxZoomExtent,
			params.MaxZoom, config.ExtentOptimization.BaseExtent))

	// Step C: 生成瓦片并动态减半直到符合大小要求
	mvtData, finalExtent, err := g.generateTileWithDynamicExtentReduction(
		ctx, params, columns, initialExtent,
		config.ExtentOptimization.MinExtent,
		config.TileSizeThresholds.MaxSizeMB)
	if err != nil {
		return nil, err
	}

	tileSizeMB := float64(len(mvtData)) / (1024 * 1024)
	totalDuration := time.Since(tileStartTime)

	logger.L().Info("✅ v3.0 优化流程完成",
		"z", params.Z, "x", params.X, "y", params.Y,
		"table", fmt.Sprintf("%s.%s", params.Schema, params.Table),
		"initial_extent", initialExtent,
		"final_extent", finalExtent,
		"final_size_mb", fmt.Sprintf("%.2f", tileSizeMB),
		"max_size_mb", fmt.Sprintf("%.2f", config.TileSizeThresholds.MaxSizeMB),
		"total_duration_ms", totalDuration.Milliseconds())

	return mvtData, nil
}

// generateTileWithDynamicExtentReduction 生成瓦片，自动减半 Extent 直到符合大小要求
func (g *TileGenerator) generateTileWithDynamicExtentReduction(
	ctx context.Context,
	params TileGenerationParams,
	columns []string,
	initialExtent int,
	minExtent int,
	maxSizeMB float64,
) (mvtData []byte, finalExtent int, err error) {
	currentExtent := initialExtent
	attempt := 0

	for currentExtent >= minExtent {
		attempt++
		logger.L().Info("🔄 生成瓦片尝试",
			"attempt", attempt,
			"extent", currentExtent,
			"z", params.Z, "x", params.X, "y", params.Y)

		// 生成瓦片
		mvtData, err = g.generateBaseTile(ctx, params, columns, currentExtent)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to generate tile with extent %d: %w", currentExtent, err)
		}

		tileSizeMB := float64(len(mvtData)) / (1024 * 1024)
		logger.L().Info("📊 瓦片大小检查",
			"attempt", attempt,
			"extent", currentExtent,
			"size_mb", fmt.Sprintf("%.2f", tileSizeMB),
			"max_mb", fmt.Sprintf("%.2f", maxSizeMB),
			"is_ok", tileSizeMB <= maxSizeMB)

		if tileSizeMB <= maxSizeMB {
			logger.L().Info("✅ 瓦片大小符合要求",
				"attempt", attempt,
				"extent", currentExtent,
				"size_mb", fmt.Sprintf("%.2f", tileSizeMB),
				"z", params.Z, "x", params.X, "y", params.Y)
			return mvtData, currentExtent, nil
		}

		// 减半 Extent 继续尝试
		currentExtent = currentExtent / 2
	}

	// 最小 extent 仍超限时跳过该瓦片，避免缓存和前端继续承载超大 MVT。
	logger.L().Warn("⚠️ 瓦片大小仍超过限制，跳过超大瓦片",
		"min_extent", minExtent,
		"final_size_mb", fmt.Sprintf("%.2f", float64(len(mvtData))/(1024*1024)),
		"max_mb", fmt.Sprintf("%.2f", maxSizeMB),
		"z", params.Z, "x", params.X, "y", params.Y)

	return []byte{}, minExtent, nil
}

func mvtBufferForExtent(extent int) int {
	if extent <= 0 {
		return 32
	}
	buffer := extent / 32
	if buffer < 8 {
		return 8
	}
	if buffer > 64 {
		return 64
	}
	return buffer
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
	// 🔑 关键：params.Table 和 params.GeomColumn 已从 QuickView 准备结果中获取
	// - 如果准备阶段创建了物化视图（e.g., dltb_mv3857），则使用物化视图
	// - 如果源表已是 3857，则直接使用源表
	// - extent 减半时，继续使用同一个表（不做降级，始终保持高性能）
	// - 避免在这里重复检查物化视图存在性
	opt := spatial.MVTOptions{
		Layer:  params.Table, // Layer 名称使用原始表名（前端显示）
		Extent: extent,       // extent 由调用者控制（初始值或减半后的值）
		Buffer: mvtBufferForExtent(extent),
		SRID:   params.SRID,
	}

	sqlStr, args := spatial.BuildMVTQuery(
		params.Schema,
		params.Table,      // 使用准备阶段确定的表（物化视图或源表）
		params.GeomColumn, // 使用准备阶段确定的几何列
		columns,
		params.Z, params.X, params.Y,
		opt,
		params.PrimaryKey,
	)

	logger.L().Info("🔍 生成基础瓦片 SQL",
		"extent", extent,
		"columns_count", len(columns),
		"sql_length", len(sqlStr),
		"args_count", len(args),
		"args", fmt.Sprintf("%v", args))

	// 打印完整的SQL和参数（用于排查Extent优化卡住的问题）
	logger.L().Info("📋 完整的SQL语句（首1000字符）",
		"sql_preview", func() string {
			if len(sqlStr) > 1000 {
				return sqlStr[:1000] + "..."
			}
			return sqlStr
		}())

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
