package spatial

import (
    "fmt"
    "strings"

    pq "github.com/lib/pq"
)

// MVTOptions encapsulates rendering options for building an MVT SQL query.
type MVTOptions struct {
    Layer   string // layer name in the MVT
    Extent  int    // tile extent (e.g., 4096)
    Buffer  int    // buffer pixels (e.g., 64)
    SRID    int    // source data SRID (e.g., 4326)
    Simplify bool  // whether to apply simplification based on zoom
}

// SimplifyTolerance returns a degree-based tolerance heuristic by zoom.
// Lower zoom => larger tolerance. This is a simple heuristic suitable for preview.
func SimplifyTolerance(z int) float64 {
    if z < 0 {
        z = 0
    }
    if z > 22 {
        z = 22
    }
    base := 0.0001 // ~11m at equator in degrees; preview-oriented
    // As zoom decreases, increase tolerance exponentially.
    pow := 10 - z
    tol := base
    if pow > 0 {
        for i := 0; i < pow; i++ {
            tol *= 2
        }
    }
    return tol
}

// BuildMVTQuery builds a safe SQL for ST_AsMVT with placeholders for z,x,y and options.
// Identifiers (schema/table/columns/geom) are quoted to avoid injection.
// Returns the SQL string and args in order: z, x, y, layer, extent, buffer, src_srid, [optional simplify tol].
// primaryKey: 主键列名（用于前端关联表格行和地图要素），如果为空则尝试使用 "id"
func BuildMVTQuery(schema, table, geomCol string, cols []string, z, x, y int, opt MVTOptions, primaryKey string) (string, []interface{}) {
    if opt.Extent == 0 {
        opt.Extent = 4096
    }
    if opt.Buffer == 0 {
        opt.Buffer = 64
    }
    if opt.SRID == 0 {
        opt.SRID = 4326
    }
    if opt.Layer == "" {
        opt.Layer = table
    }

    // Quote identifiers safely
    qSchema := pq.QuoteIdentifier(schema)
    qTable := pq.QuoteIdentifier(table)
    qGeom := pq.QuoteIdentifier(geomCol)

    // 确保主键列被包含（用于前端关联）
    if primaryKey == "" {
        primaryKey = "id" // 默认使用 "id" 作为主键
    }
    qPrimaryKey := pq.QuoteIdentifier(primaryKey)

    // Build column list, quoted
    // 确保主键列在列表中
    colsMap := make(map[string]bool)
    colsMap[primaryKey] = true // 主键列必须包含

    var colsSQL string
    if len(cols) > 0 {
        for _, c := range cols {
            c = strings.TrimSpace(c)
            if c != "" && !strings.EqualFold(c, geomCol) {
                colsMap[c] = true
            }
        }
    }

    // 构建列列表：主键列 + 其他列
    quoted := make([]string, 0, len(colsMap))
    // 首先添加主键列
    quoted = append(quoted, qPrimaryKey)
    // 然后添加其他列
    for colName := range colsMap {
        if colName != primaryKey && !strings.EqualFold(colName, geomCol) {
            quoted = append(quoted, pq.QuoteIdentifier(colName))
        }
    }
    if len(quoted) > 0 {
        colsSQL = ", " + strings.Join(quoted, ", ")
    }

    // Optionally simplify before transform for coarse zoom levels
    simplify := "ST_Transform(t." + geomCol + ", 3857)"
    args := []interface{}{z, x, y, opt.Layer, opt.Extent, opt.Buffer, opt.SRID}
    if opt.Simplify {
        tol := SimplifyTolerance(z)
        simplify = fmt.Sprintf("ST_Transform(ST_SimplifyPreserveTopology(t.%s, %f), 3857)", geomCol, tol)
    }

    sql := fmt.Sprintf(`
WITH b AS (
  SELECT
    ST_TileEnvelope($1, $2, $3) AS g3857,
    ST_Transform(ST_TileEnvelope($1, $2, $3), $7) AS g_src
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
  WHERE t.%s && b.g_src
    AND ST_Intersects(t.%s, b.g_src)
) AS m`,
        simplify,
        colsSQL,
        qSchema,
        qTable,
        qGeom,
        qGeom,
    )

    return sql, args
}

