package spatial

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	commonClient "github.com/addp/common/client"
)

// QueryTableRowCount 查询表的记录数
//
// 参数:
//   - ctx: 上下文
//   - systemClient: System 客户端（用于获取存储引擎信息）
//   - engineID: 存储引擎 ID
//   - schema: 模式名
//   - table: 表名
//
// 返回:
//   - int64: 记录数
//   - error: 错误信息
//
// 示例:
//
//	count, err := spatial.QueryTableRowCount(ctx, systemClient, 1, "public", "cities")
func QueryTableRowCount(
	ctx context.Context,
	systemClient *commonClient.SystemClient,
	engineID uint,
	schema, table string,
) (int64, error) {
	// 1. 获取存储引擎连接信息
	engine, err := systemClient.GetEngine(engineID)
	if err != nil {
		return 0, fmt.Errorf("failed to get engine: %w", err)
	}

	// 2. 获取 PostGIS 连接池
	db, err := GetPostGISPool(engine, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to connect to database: %w", err)
	}

	// 4. 查询记录数（优先使用统计估算值，回退到 COUNT(*））
	var count int64

	// 4.1 优先使用 pg_stat_user_tables 的估算值（几乎瞬时，适用于大表）
	estimateQuery := `
		SELECT n_live_tup
		FROM pg_stat_user_tables
		WHERE schemaname = $1 AND relname = $2
	`
	err = db.Raw(estimateQuery, schema, table).Scan(&count).Error

	// 如果统计信息可用且 > 0，直接返回
	if err == nil && count > 0 {
		return count, nil
	}

	// 4.2 回退：使用 COUNT(*) 精确查询（可能很慢）
	query := BuildPostGISCountQuery(schema, table)
	err = db.Raw(query).Scan(&count).Error
	if err != nil {
		return 0, fmt.Errorf("failed to query record count: %w", err)
	}

	return count, nil
}

// QueryExtent 查询表的地理范围（WGS84 坐标系）
//
// 参数:
//   - ctx: 上下文
//   - systemClient: System 客户端
//   - engineID: 存储引擎 ID
//   - schema: 模式名
//   - table: 表名
//
// 返回:
//   - extent: [minLon, minLat, maxLon, maxLat] (WGS84)
//   - geometryColumn: 几何列名称
//   - error: 错误信息
//
// 查询策略:
//  1. 优先使用 ST_EstimatedExtent（毫秒级，基于统计信息）
//  2. 失败时回退到采样查询（TABLESAMPLE BERNOULLI 1%）
//
// 示例:
//
//	extent, geomCol, err := spatial.QueryExtent(ctx, systemClient, 1, "public", "cities")
func QueryExtent(
	ctx context.Context,
	systemClient *commonClient.SystemClient,
	engineID uint,
	schema, table string,
) (extent []float64, geometryColumn string, err error) {
	// 1. 获取存储引擎连接信息
	engine, err := systemClient.GetEngine(engineID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get engine: %w", err)
	}

	// 2. 获取 PostGIS 连接池
	db, err := GetPostGISPool(engine, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to connect to database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, "", fmt.Errorf("failed to get sql.DB: %w", err)
	}

	// 4. 从 PostGIS geometry_columns 获取几何列信息
	var geomInfo struct {
		GeomColumn string `gorm:"column:f_geometry_column"`
		SRID       int    `gorm:"column:srid"`
	}

	err = db.Raw(`
		SELECT f_geometry_column, srid
		FROM geometry_columns
		WHERE f_table_schema = ? AND f_table_name = ?
		LIMIT 1
	`, schema, table).Scan(&geomInfo).Error

	if err != nil {
		return nil, "", fmt.Errorf("geometry_columns query failed: %w", err)
	}

	if geomInfo.GeomColumn == "" {
		return nil, "", fmt.Errorf("no geometry column found for %s.%s", schema, table)
	}

	// 5. 使用 ST_EstimatedExtent 快速获取范围
	var boxStr sql.NullString
	err = sqlDB.QueryRow(
		"SELECT ST_EstimatedExtent($1, $2, $3)",
		schema, table, geomInfo.GeomColumn,
	).Scan(&boxStr)

	if err != nil || !boxStr.Valid || boxStr.String == "" {
		// 回退：使用采样方式计算 extent
		return queryExtentWithSample(sqlDB, schema, table, geomInfo.GeomColumn, geomInfo.SRID)
	}

	// 6. 解析 BOX 字符串并转换为 WGS84
	extent, err = parseAndTransformBox(sqlDB, boxStr.String, geomInfo.SRID)
	if err != nil {
		return nil, geomInfo.GeomColumn, fmt.Errorf("failed to parse extent: %w", err)
	}

	return extent, geomInfo.GeomColumn, nil
}

// queryExtentWithSample 使用采样方式计算 extent（回退方案）
func queryExtentWithSample(
	db *sql.DB,
	schema, table, geomColumn string,
	srid int,
) ([]float64, string, error) {
	query := fmt.Sprintf(`
		SELECT
			ST_XMin(extent) as min_lon,
			ST_YMin(extent) as min_lat,
			ST_XMax(extent) as max_lon,
			ST_YMax(extent) as max_lat
		FROM (
			SELECT ST_Extent(ST_Transform("%s", 4326)) as extent
			FROM "%s"."%s" TABLESAMPLE BERNOULLI (1) LIMIT 10000
		) t
	`, geomColumn, schema, table)

	var minLon, minLat, maxLon, maxLat sql.NullFloat64
	err := db.QueryRow(query).Scan(&minLon, &minLat, &maxLon, &maxLat)
	if err != nil {
		return nil, geomColumn, fmt.Errorf("failed to calculate sampled extent: %w", err)
	}

	if !minLon.Valid || !minLat.Valid || !maxLon.Valid || !maxLat.Valid {
		return nil, geomColumn, fmt.Errorf("extent calculation returned NULL")
	}

	extent := []float64{minLon.Float64, minLat.Float64, maxLon.Float64, maxLat.Float64}
	return extent, geomColumn, nil
}

// parseAndTransformBox 解析 PostGIS BOX 字符串并转换为 WGS84
func parseAndTransformBox(db *sql.DB, boxStr string, srid int) ([]float64, error) {
	// 解析 BOX 字符串: "BOX(xmin ymin, xmax ymax)"
	boxStr = strings.TrimSpace(boxStr)
	if !strings.HasPrefix(boxStr, "BOX(") || !strings.HasSuffix(boxStr, ")") {
		return nil, fmt.Errorf("invalid BOX format: %s", boxStr)
	}

	coords := strings.TrimPrefix(boxStr, "BOX(")
	coords = strings.TrimSuffix(coords, ")")

	parts := strings.Split(coords, ",")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid BOX coordinates: %s", boxStr)
	}

	min := strings.Fields(strings.TrimSpace(parts[0]))
	max := strings.Fields(strings.TrimSpace(parts[1]))

	if len(min) != 2 || len(max) != 2 {
		return nil, fmt.Errorf("invalid BOX point format")
	}

	var xmin, ymin, xmax, ymax float64
	var err error

	if xmin, err = strconv.ParseFloat(min[0], 64); err != nil {
		return nil, fmt.Errorf("invalid xmin: %s", min[0])
	}
	if ymin, err = strconv.ParseFloat(min[1], 64); err != nil {
		return nil, fmt.Errorf("invalid ymin: %s", min[1])
	}
	if xmax, err = strconv.ParseFloat(max[0], 64); err != nil {
		return nil, fmt.Errorf("invalid xmax: %s", max[0])
	}
	if ymax, err = strconv.ParseFloat(max[1], 64); err != nil {
		return nil, fmt.Errorf("invalid ymax: %s", max[1])
	}

	// 如果已经是 WGS84，直接返回
	if srid == 4326 {
		return []float64{xmin, ymin, xmax, ymax}, nil
	}

	// 使用 PostGIS ST_Transform 转换坐标系
	query := `
		SELECT
			ST_XMin(transformed) as min_lon,
			ST_YMin(transformed) as min_lat,
			ST_XMax(transformed) as max_lon,
			ST_YMax(transformed) as max_lat
		FROM (
			SELECT ST_Transform(
				ST_MakeEnvelope($1, $2, $3, $4, $5),
				4326
			) as transformed
		) t
	`

	var minLon, minLat, maxLon, maxLat float64
	err = db.QueryRow(query, xmin, ymin, xmax, ymax, srid).Scan(&minLon, &minLat, &maxLon, &maxLat)
	if err != nil {
		return nil, fmt.Errorf("failed to transform coordinates: %w", err)
	}

	return []float64{minLon, minLat, maxLon, maxLat}, nil
}

// BBox 空间范围边界框
// 用于表示地理空间的矩形范围，支持不同坐标系
type BBox struct {
	MinX float64 // 最小 X 坐标（经度）
	MinY float64 // 最小 Y 坐标（纬度）
	MaxX float64 // 最大 X 坐标（经度）
	MaxY float64 // 最大 Y 坐标（纬度）
	CRS  int     // 坐标参考系统 EPSG 代码（如 4326、3857）
}

// SupportedSRIDs 支持的坐标系列表
// 包含常用的坐标系统 EPSG 代码
var SupportedSRIDs = []int{
	4326, // WGS84 经纬度坐标系
	3857, // Web Mercator 投影坐标系
	4490, // CGCS2000 国家2000坐标系
	2000, // Anguilla 1957 / British West Indies Grid
	4214, // Beijing 1954
}

// ValidateSRID 验证坐标系是否在支持列表中
//
// 参数:
//   - srid: EPSG 坐标系代码
//
// 返回:
//   - error: 如果不支持则返回错误信息
//
// 示例:
//
//	err := spatial.ValidateSRID(4326) // nil
//	err := spatial.ValidateSRID(9999) // error: unsupported SRID
func ValidateSRID(srid int) error {
	for _, supported := range SupportedSRIDs {
		if srid == supported {
			return nil
		}
	}
	return fmt.Errorf("unsupported SRID %d, supported: %v", srid, SupportedSRIDs)
}

// ParseBBox 解析 bbox 参数字符串为 BBox 结构
//
// 格式: minLon,minLat,maxLon,maxLat[,crs]
// 默认坐标系为 EPSG:4326 (WGS84)
//
// 参数:
//   - bboxStr: bbox 字符串，格式为逗号分隔的坐标值
//
// 返回:
//   - *BBox: 解析后的边界框对象
//   - error: 解析错误信息
//
// 示例:
//
//	bbox, err := spatial.ParseBBox("120.0,30.0,121.0,31.0")       // WGS84
//	bbox, err := spatial.ParseBBox("120.0,30.0,121.0,31.0,4326")  // 显式指定 WGS84
//	bbox, err := spatial.ParseBBox("120.0,30.0,121.0,31.0,3857")  // Web Mercator
func ParseBBox(bboxStr string) (*BBox, error) {
	if bboxStr == "" {
		return nil, nil
	}

	parts := strings.Split(bboxStr, ",")
	if len(parts) != 4 && len(parts) != 5 {
		return nil, fmt.Errorf("invalid bbox format, expected minLon,minLat,maxLon,maxLat[,crs]")
	}

	bbox := &BBox{
		CRS: 4326, // 默认 WGS84
	}

	var err error
	bbox.MinX, err = strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return nil, fmt.Errorf("invalid bbox minX: %w", err)
	}

	bbox.MinY, err = strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return nil, fmt.Errorf("invalid bbox minY: %w", err)
	}

	bbox.MaxX, err = strconv.ParseFloat(parts[2], 64)
	if err != nil {
		return nil, fmt.Errorf("invalid bbox maxX: %w", err)
	}

	bbox.MaxY, err = strconv.ParseFloat(parts[3], 64)
	if err != nil {
		return nil, fmt.Errorf("invalid bbox maxY: %w", err)
	}

	// 解析 CRS（如果提供）
	if len(parts) == 5 {
		crs, err := strconv.Atoi(parts[4])
		if err != nil {
			return nil, fmt.Errorf("invalid bbox CRS: %w", err)
		}
		bbox.CRS = crs
	}

	// 验证范围合理性
	if bbox.MinX >= bbox.MaxX || bbox.MinY >= bbox.MaxY {
		return nil, fmt.Errorf("invalid bbox: min values must be less than max values")
	}

	return bbox, nil
}
