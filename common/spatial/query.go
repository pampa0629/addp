package spatial

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/lib/pq"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
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

	// 2. 构建连接字符串
	connStr, err := commonModels.BuildConnectionString(engine)
	if err != nil {
		return 0, fmt.Errorf("failed to build connection string: %w", err)
	}

	// 3. 连接数据库
	db, err := gorm.Open(postgres.Open(connStr), &gorm.Config{})
	if err != nil {
		return 0, fmt.Errorf("failed to connect to database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return 0, fmt.Errorf("failed to get sql.DB: %w", err)
	}
	defer sqlDB.Close()

	// 4. 查询记录数（使用 COUNT(*) 精确查询）
	// 重要：使用 pq.QuoteIdentifier 保留标识符大小写
	var count int64
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s.%s", pq.QuoteIdentifier(schema), pq.QuoteIdentifier(table))
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

	// 2. 构建连接字符串
	connStr, err := commonModels.BuildConnectionString(engine)
	if err != nil {
		return nil, "", fmt.Errorf("failed to build connection string: %w", err)
	}

	// 3. 连接数据库
	db, err := gorm.Open(postgres.Open(connStr), &gorm.Config{})
	if err != nil {
		return nil, "", fmt.Errorf("failed to connect to database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, "", fmt.Errorf("failed to get sql.DB: %w", err)
	}
	defer sqlDB.Close()

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
