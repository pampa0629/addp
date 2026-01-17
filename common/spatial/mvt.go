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

    // 主键列处理（用于前端关联）
    // 如果 primaryKey 为空字符串,则不包含主键列
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
        // 首先添加主键列
        quoted = append(quoted, qPrimaryKey)
    }
    // 然后添加其他列
    for colName := range colsMap {
        if (!usePrimaryKey || colName != primaryKey) && !strings.EqualFold(colName, geomCol) {
            quoted = append(quoted, pq.QuoteIdentifier(colName))
        }
    }
    if len(quoted) > 0 {
        colsSQL = ", " + strings.Join(quoted, ", ")
    }

    // Optionally simplify before transform for coarse zoom levels
    simplify := "ST_Transform(t." + qGeom + ", 3857)"
    // SRID 必须直接内联到 SQL 中，不能作为参数（PostGIS 限制）
    args := []interface{}{z, x, y, opt.Layer, opt.Extent, opt.Buffer}
    if opt.Simplify {
        tol := SimplifyTolerance(z)
        simplify = fmt.Sprintf("ST_Transform(ST_SimplifyPreserveTopology(t.%s, %f), 3857)", qGeom, tol)
    }

    sql := fmt.Sprintf(`
WITH b AS (
  SELECT
    ST_TileEnvelope($1, $2, $3) AS g3857,
    ST_Transform(ST_TileEnvelope($1, $2, $3), %d) AS g_src
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
        opt.SRID, // 直接内联 SRID
        simplify,
        colsSQL,
        qSchema,
        qTable,
        qGeom,
        qGeom,
    )

    return sql, args
}

