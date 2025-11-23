package mvt

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/spatial"
	metaModels "github.com/addp/meta/internal/models"
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

// GenerateTile 生成 MVT 瓦片
// 参数:
//   - ctx: 上下文
//   - item: meta_item 对象（包含表信息）
//   - tenantID: 租户 ID
//   - z, x, y: 瓦片坐标
// 返回: MVT 二进制数据（未压缩）
func (g *TileGenerator) GenerateTile(
	ctx context.Context,
	item *metaModels.MetaItem,
	tenantID uint,
	z, x, y int,
) ([]byte, error) {
	// 1. 获取资源连接信息
	resource, err := g.resourceService.GetResource(item.ResID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource: %w", err)
	}

	// 2. 提取空间元数据
	spatialMeta, ok := item.Attributes["spatial_metadata"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("item %d is not a spatial table", item.ID)
	}

	geomColumn, _ := spatialMeta["geometry_column"].(string)
	srid, _ := spatialMeta["srid"].(float64) // JSON 数字默认为 float64
	if geomColumn == "" || srid == 0 {
		return nil, fmt.Errorf("invalid spatial metadata")
	}

	// 3. 提取表信息
	schema, _ := item.Attributes["schema"].(string)
	tableName := item.Name

	// 4. 构建连接字符串
	connStr, err := commonModels.BuildConnectionString(resource)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}

	// 5. 连接数据库
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	// 6. 构建 MVT 查询（使用 common/spatial 统一实现）
	sqlStr, args := g.buildMVTQuery(schema, tableName, geomColumn, int(srid), z, x, y)

	// 7. 执行查询
	var mvtData []byte
	err = db.QueryRowContext(ctx, sqlStr, args...).Scan(&mvtData)
	if err != nil {
		if err == sql.ErrNoRows {
			return []byte{}, nil // 空瓦片
		}
		logger.L().Error("MVT query failed",
			"error", err,
			"item_id", item.ID,
			"z", z, "x", x, "y", y,
			"schema", schema,
			"table", tableName)
		return nil, fmt.Errorf("failed to execute MVT query: %w", err)
	}

	return mvtData, nil
}

// buildMVTQuery 构建 MVT 查询 SQL（使用 common/spatial 统一实现）
func (g *TileGenerator) buildMVTQuery(
	schema, table, geomColumn string,
	srid, z, x, y int,
) (string, []interface{}) {
	// 使用 common/spatial.BuildMVTQuery 统一实现
	// Meta Worker 预处理不需要 simplify（速度更重要）和列选择（保留所有列）
	opt := spatial.MVTOptions{
		Layer:    table,
		Extent:   4096,
		Buffer:   64,
		SRID:     srid,
		Simplify: false, // 预处理阶段不简化，保留完整精度
	}
	return spatial.BuildMVTQuery(schema, table, geomColumn, []string{}, z, x, y, opt, "id")
}

// GetSpatialExtent 获取空间表的范围（WGS84）
func (g *TileGenerator) GetSpatialExtent(
	ctx context.Context,
	item *metaModels.MetaItem,
	tenantID uint,
) ([]float64, error) {
	// 1. 获取资源连接信息
	resource, err := g.resourceService.GetResource(item.ResID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource: %w", err)
	}

	// 2. 提取空间元数据
	spatialMeta, ok := item.Attributes["spatial_metadata"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("item %d is not a spatial table", item.ID)
	}

	geomColumn, _ := spatialMeta["geometry_column"].(string)
	// Note: SRID not needed here as we transform to WGS84 (4326) in the query

	// 3. 提取表信息
	schema, _ := item.Attributes["schema"].(string)
	tableName := item.Name

	// 4. 构建连接字符串
	connStr, err := commonModels.BuildConnectionString(resource)
	if err != nil {
		return nil, fmt.Errorf("failed to build connection string: %w", err)
	}

	// 5. 连接数据库
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	// 6. 查询范围（转换为 WGS84）
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
	`, geomColumn, schema, tableName)

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
