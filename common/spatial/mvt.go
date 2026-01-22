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

	// 优化配置（v2.0 新增）
	SamplingConfig       *SamplingParams       // 对象采样配置
	SimplificationConfig *SimplificationParams // 几何简化配置
}

// SamplingParams 对象采样参数
type SamplingParams struct {
	Enabled     bool                   // 是否启用采样
	PolygonLine PolygonLineSampling    // 面/线要素采样
	Point       PointSampling          // 点要素采样
}

// PolygonLineSampling 面/线要素采样配置
type PolygonLineSampling struct {
	CumulativeSizeRatio  float64 // 面积/长度累计占比（0.5-1.0）
	MaxFeatureCountRatio float64 // 对象数量占比（0.3-1.0）
}

// PointSampling 点要素采样配置
type PointSampling struct {
	SampleRatio float64 // 点保留比例（0.3-1.0）
}

// SimplificationParams 几何简化参数
type SimplificationParams struct {
	Enabled             bool    // 是否启用简化
	ToleranceMultiplier float64 // 容错度倍数（2, 4, 8）
	Algorithm           string  // 算法：visvalingam | douglas_peucker
}

// SimplifyTolerance returns a degree-based tolerance heuristic by zoom.
// Lower zoom => larger tolerance. This is a simple heuristic suitable for preview.
func SimplifyTolerance(z int) float64 {
	return SimplifyToleranceWithMultiplier(z, 2.0)
}

// SimplifyToleranceWithMultiplier returns tolerance with custom multiplier.
// multiplier: 容错度倍数，推荐值 2(保守), 4(平衡), 8(激进)
func SimplifyToleranceWithMultiplier(z int, multiplier float64) float64 {
	if z < 0 {
		z = 0
	}
	if z > 22 {
		z = 22
	}
	if multiplier <= 0 {
		multiplier = 2.0
	}

	base := 0.0001 // ~11m at equator in degrees
	pow := 10 - z
	tol := base
	if pow > 0 {
		for i := 0; i < pow; i++ {
			tol *= multiplier
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

	// ✅ v2.0 优化配置
	enableSampling := opt.SamplingConfig != nil && opt.SamplingConfig.Enabled
	enableSimplification := opt.SimplificationConfig != nil && opt.SimplificationConfig.Enabled

	// 基础参数
	args := []interface{}{z, x, y, opt.Layer, opt.Extent, opt.Buffer}

	// 如果启用了高级优化，使用复杂 SQL；否则使用简单 SQL
	if enableSampling || enableSimplification {
		return buildOptimizedMVTQuery(qSchema, qTable, qGeom, qPrimaryKey, colsSQL, z, opt, args, enableSampling, enableSimplification)
	}

	// 简单模式（向后兼容）
	simplify := "ST_Transform(t." + qGeom + ", 3857)"
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
		opt.SRID,
		simplify,
		colsSQL,
		qSchema,
		qTable,
		qGeom,
		qGeom,
	)

	return sql, args
}

// buildOptimizedMVTQuery 构建带采样和简化的 MVT 查询（v2.0）
func buildOptimizedMVTQuery(
	qSchema, qTable, qGeom, qPrimaryKey, colsSQL string,
	z int,
	opt MVTOptions,
	args []interface{},
	enableSampling, enableSimplification bool,
) (string, []interface{}) {
	// 构建几何处理表达式
	geomExpr := "t." + qGeom

	// 1. 几何简化（如果启用）
	if enableSimplification {
		tol := SimplifyToleranceWithMultiplier(z, opt.SimplificationConfig.ToleranceMultiplier)
		algorithm := opt.SimplificationConfig.Algorithm

		// 先去除重复点，再简化
		geomExpr = fmt.Sprintf("ST_RemoveRepeatedPoints(%s, %f)", geomExpr, tol*0.1)

		if algorithm == "visvalingam" {
			geomExpr = fmt.Sprintf("ST_SimplifyVW(%s, %f)", geomExpr, tol)
		} else {
			// douglas_peucker (default)
			geomExpr = fmt.Sprintf("ST_SimplifyPreserveTopology(%s, %f)", geomExpr, tol)
		}
	}

	// 2. 转换为 Web Mercator
	geomExpr = fmt.Sprintf("ST_Transform(%s, 3857)", geomExpr)

	// 3. 构建 WHERE 子句（采样条件）
	whereClause := fmt.Sprintf("t.%s && b.g_src AND ST_Intersects(t.%s, b.g_src)", qGeom, qGeom)

	if enableSampling {
		samplingClause := buildSamplingClause(qPrimaryKey, qGeom, opt.SamplingConfig)
		if samplingClause != "" {
			whereClause = whereClause + " AND (" + samplingClause + ")"
		}
	}

	// 4. 是否需要排序 CTE（用于面/线采样）
	needRanking := enableSampling &&
		(opt.SamplingConfig.PolygonLine.CumulativeSizeRatio < 1.0 ||
			opt.SamplingConfig.PolygonLine.MaxFeatureCountRatio < 1.0)

	var sql string
	if needRanking {
		// 使用排序 CTE 实现面/线采样
		sql = fmt.Sprintf(`
WITH b AS (
  SELECT
    ST_TileEnvelope($1, $2, $3) AS g3857,
    ST_Transform(ST_TileEnvelope($1, $2, $3), %d) AS g_src
),
ranked_features AS (
  SELECT
    t.*,
    CASE
      WHEN ST_GeometryType(t.%s) IN ('ST_Polygon', 'ST_MultiPolygon')
        THEN ST_Area(t.%s)
      WHEN ST_GeometryType(t.%s) IN ('ST_LineString', 'ST_MultiLineString')
        THEN ST_Length(t.%s)
      ELSE 0
    END AS size_metric,
    SUM(CASE
      WHEN ST_GeometryType(t.%s) IN ('ST_Polygon', 'ST_MultiPolygon', 'ST_LineString', 'ST_MultiLineString')
        THEN CASE
          WHEN ST_GeometryType(t.%s) IN ('ST_Polygon', 'ST_MultiPolygon')
            THEN ST_Area(t.%s)
          ELSE ST_Length(t.%s)
        END
      ELSE 0
    END) OVER (ORDER BY CASE
      WHEN ST_GeometryType(t.%s) IN ('ST_Polygon', 'ST_MultiPolygon')
        THEN ST_Area(t.%s)
      WHEN ST_GeometryType(t.%s) IN ('ST_LineString', 'ST_MultiLineString')
        THEN ST_Length(t.%s)
      ELSE 0
    END DESC) AS cumulative_size,
    ROW_NUMBER() OVER (ORDER BY CASE
      WHEN ST_GeometryType(t.%s) IN ('ST_Polygon', 'ST_MultiPolygon')
        THEN ST_Area(t.%s)
      WHEN ST_GeometryType(t.%s) IN ('ST_LineString', 'ST_MultiLineString')
        THEN ST_Length(t.%s)
      ELSE 0
    END DESC) AS row_num,
    COUNT(*) OVER () AS total_count,
    SUM(CASE
      WHEN ST_GeometryType(t.%s) IN ('ST_Polygon', 'ST_MultiPolygon', 'ST_LineString', 'ST_MultiLineString')
        THEN CASE
          WHEN ST_GeometryType(t.%s) IN ('ST_Polygon', 'ST_MultiPolygon')
            THEN ST_Area(t.%s)
          ELSE ST_Length(t.%s)
        END
      ELSE 0
    END) OVER () AS total_size
  FROM %s.%s AS t, b
  WHERE t.%s && b.g_src
    AND ST_Intersects(t.%s, b.g_src)
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
  FROM ranked_features AS t, b
  WHERE (
    -- 面/线要素：按面积/长度采样
    (ST_GeometryType(t.%s) IN ('ST_Polygon', 'ST_MultiPolygon', 'ST_LineString', 'ST_MultiLineString')
     AND (t.cumulative_size <= t.total_size * %f OR t.row_num <= t.total_count * %f))
    OR
    -- 点要素：随机采样
    (ST_GeometryType(t.%s) NOT IN ('ST_Polygon', 'ST_MultiPolygon', 'ST_LineString', 'ST_MultiLineString')
     AND (hashtext(%s::text)::bigint %% 100) < %d)
  )
  AND NOT ST_IsEmpty(%s)
) AS m`,
			opt.SRID,
			// ranked_features CTE 中的字段重复（用于计算）
			qGeom, qGeom, qGeom, qGeom, qGeom, qGeom, qGeom, qGeom,
			qGeom, qGeom, qGeom, qGeom, qGeom, qGeom, qGeom, qGeom,
			qGeom, qGeom, qGeom, qGeom,
			qSchema, qTable,
			qGeom, qGeom,
			// 最终查询
			geomExpr,
			colsSQL,
			qGeom,
			opt.SamplingConfig.PolygonLine.CumulativeSizeRatio,
			opt.SamplingConfig.PolygonLine.MaxFeatureCountRatio,
			qGeom,
			qPrimaryKey,
			int(opt.SamplingConfig.Point.SampleRatio*100),
			geomExpr,
		)
	} else {
		// 简单模式（无需排序 CTE）
		sql = fmt.Sprintf(`
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
  WHERE %s
    AND NOT ST_IsEmpty(%s)
) AS m`,
			opt.SRID,
			geomExpr,
			colsSQL,
			qSchema,
			qTable,
			whereClause,
			geomExpr,
		)
	}

	return sql, args
}

// buildSamplingClause 构建采样 WHERE 子句（仅点采样）
func buildSamplingClause(qPrimaryKey, qGeom string, config *SamplingParams) string {
	if config == nil || !config.Enabled {
		return ""
	}

	// 点采样条件（基于主键哈希）
	pointSamplePercent := int(config.Point.SampleRatio * 100)
	if pointSamplePercent >= 100 {
		return "" // 100% 采样，不需要条件
	}

	// 只对点要素进行哈希采样，非点要素全部保留（或通过排序 CTE 处理）
	return fmt.Sprintf(
		"ST_GeometryType(%s) NOT IN ('ST_Point', 'ST_MultiPoint') OR (hashtext(%s::text)::bigint %% 100) < %d",
		qGeom,
		qPrimaryKey,
		pointSamplePercent,
	)
}

