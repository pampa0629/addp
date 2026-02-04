package spatial

import (
	"fmt"
	"strings"

	pq "github.com/lib/pq"
	"gorm.io/gorm"
)

// MVTOptions encapsulates rendering options for building an MVT SQL query.
type MVTOptions struct {
	Layer  string // layer name in the MVT
	Extent int    // tile extent (e.g., 2048)
	Buffer int    // buffer pixels (e.g., 64)
	SRID   int    // source data SRID (e.g., 4326 or 3857)
}

// BuildMVTQuery builds a safe SQL for ST_AsMVT with placeholders for z,x,y and options.
// Identifiers (schema/table/columns/geom) are quoted to avoid injection.
// Returns the SQL string and args in order: z, x, y, layer, extent, buffer.
// primaryKey: 主键列名（用于前端关联表格行和地图要素），如果为空则尝试使用 "id"
func BuildMVTQuery(schema, table, geomCol string, cols []string, z, x, y int, opt MVTOptions, primaryKey string) (string, []interface{}) {
	if opt.Extent == 0 {
		opt.Extent = 2048
	}
	if opt.Buffer == 0 {
		opt.Buffer = 64
	}
	if opt.SRID == 0 {
		opt.SRID = 3857
	}
	if opt.Layer == "" {
		opt.Layer = table
	}

	// Quote identifiers safely
	qSchema := pq.QuoteIdentifier(schema)
	qTable := pq.QuoteIdentifier(table)
	qGeom := pq.QuoteIdentifier(geomCol)

	// 主键列处理（用于前端关联）
	var qPrimaryKey string
	usePrimaryKey := false
	if primaryKey != "" {
		usePrimaryKey = true
		qPrimaryKey = pq.QuoteIdentifier(primaryKey)
	}

	// Build column list, quoted
	colsMap := make(map[string]bool)
	if usePrimaryKey {
		colsMap[primaryKey] = true // 主键列必须包含
	}

	var colsSQL string
	if len(cols) > 0 {
		for _, c := range cols {
			c = strings.TrimSpace(c)
			if c != "" && !strings.EqualFold(c, geomCol) {
				colsMap[c] = true
			}
		}
	}

	// 构建列列表
	quoted := make([]string, 0, len(colsMap))
	if usePrimaryKey {
		quoted = append(quoted, qPrimaryKey)
	}
	for colName := range colsMap {
		if (!usePrimaryKey || colName != primaryKey) && !strings.EqualFold(colName, geomCol) {
			quoted = append(quoted, pq.QuoteIdentifier(colName))
		}
	}
	if len(quoted) > 0 {
		colsSQL = ", " + strings.Join(quoted, ", ")
	}

	// 基础参数
	args := []interface{}{z, x, y, opt.Layer, opt.Extent, opt.Buffer}

	// 简单模式：直接使用源表的几何字段（不做 ST_Transform）
	// 假设源表已经在 3857 或已通过物化视图转换
	geomExpr := "t." + qGeom

	sql := fmt.Sprintf(`
WITH b AS (
  SELECT
    ST_TileEnvelope($1, $2, $3) AS g3857
)
SELECT ST_AsMVT(m, $4, $5, 'geom')
FROM (
  SELECT
    ST_AsMVTGeom(
      %s,
      b.g3857,
      $5,
      $6,
      true
    ) AS geom%s
  FROM %s.%s AS t, b
  WHERE t.%s && b.g3857
    AND ST_Intersects(t.%s, b.g3857)
) AS m`,
		geomExpr,
		colsSQL,
		qSchema,
		qTable,
		qGeom,
		qGeom,
	)

	return sql, args
}

// GenerateMVTTileWithConfig 生成 MVT 瓦片（高层封装）
// 使用图层配置参数直接生成 MVT 二进制数据
//
// 参数:
//   - db: GORM 数据库连接
//   - schema: 数据库 schema 名称
//   - table: 数据表名称
//   - geomCol: 几何列名称
//   - srid: 源数据坐标系 EPSG 代码
//   - z, x, y: MVT 瓦片坐标
//   - extent: 瓦片范围（通常为 4096）
//   - buffer: 缓冲区像素（通常为 256）
//   - columns: 要包含的属性列列表
//   - layerName: MVT 图层名称
//
// 返回:
//   - []byte: MVT 二进制数据
//   - error: 错误信息
//
// 示例:
//
//	mvtData, err := spatial.GenerateMVTTileWithConfig(
//	    db, "public", "cities", "geom", 4326,
//	    10, 512, 384, 4096, 256,
//	    []string{"name", "population"}, "cities"
//	)
func GenerateMVTTileWithConfig(
	db *gorm.DB,
	schema, table, geomCol string,
	srid int,
	z, x, y int,
	extent, buffer int,
	columns []string,
	layerName string,
) ([]byte, error) {
	// 验证参数
	if extent == 0 {
		extent = 4096
	}
	if buffer == 0 {
		buffer = 256
	}
	if layerName == "" {
		layerName = table
	}

	// 构建 MVT 选项
	opts := MVTOptions{
		Layer:  layerName,
		Extent: extent,
		Buffer: buffer,
		SRID:   srid,
	}

	// 构建 SQL 查询和参数
	sql, args := BuildMVTQuery(schema, table, geomCol, columns, z, x, y, opts, "")

	// 执行查询
	var mvtData []byte
	err := db.Raw(sql, args...).Scan(&mvtData).Error
	if err != nil {
		return nil, fmt.Errorf("failed to generate MVT tile: %w", err)
	}

	// 空瓦片返回 nil（无数据）
	if len(mvtData) == 0 {
		return nil, nil
	}

	return mvtData, nil
}
