package spatial

import (
	"fmt"
	"strings"

	pq "github.com/lib/pq"
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
