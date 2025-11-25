package mvt

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/spatial"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// TileGenerator MVT 瓦片生成器
// 负责连接 PostgreSQL/PostGIS 并生成 MVT 瓦片
type TileGenerator struct {
	resourceService ResourceService // 用于获取数据源连接信息
}

// ResourceService 资源服务接口（避免循环依赖）
type ResourceService interface {
	GetResource(resourceID, tenantID uint) (*commonModels.Resource, error)
}

// NewTileGenerator 创建瓦片生成器
func NewTileGenerator(resourceService ResourceService) *TileGenerator {
	return &TileGenerator{
		resourceService: resourceService,
	}
}

// TileGenerationParams 瓦片生成参数
type TileGenerationParams struct {
	ResourceID   uint
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
	// 1. 获取资源连接信息
	resource, err := g.resourceService.GetResource(params.ResourceID, params.TenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource: %w", err)
	}

	// 2. 构建连接字符串
	connStr, err := commonModels.BuildConnectionString(resource)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}

	// 3. 连接数据库
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	// 4. 构建 MVT 查询（使用 common/spatial 统一实现）
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

	// 5. 详细日志：记录瓦片请求和 SQL
	logger.L().Info("📍 开始生成 MVT 瓦片",
		"resource_id", params.ResourceID,
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

	// 6. 执行查询
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
	// 快显缓存不需要 simplify（速度更重要）和列选择（保留所有列）
	opt := spatial.MVTOptions{
		Layer:    table,
		Extent:   4096,
		Buffer:   64,
		SRID:     srid,
		Simplify: false, // 缓存阶段不简化，保留完整精度
	}
	return spatial.BuildMVTQuery(schema, table, geomColumn, []string{}, z, x, y, opt, primaryKey)
}

// GetSpatialExtent 获取空间表的范围（WGS84）
func (g *TileGenerator) GetSpatialExtent(
	ctx context.Context,
	resourceID, tenantID uint,
	schema, table, geomColumn string,
) ([]float64, error) {
	// 1. 获取资源连接信息
	resource, err := g.resourceService.GetResource(resourceID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource: %w", err)
	}

	// 2. 构建连接字符串
	connStr, err := commonModels.BuildConnectionString(resource)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}

	// 3. 连接数据库
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	// 4. 查询范围（转换为 WGS84）
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
