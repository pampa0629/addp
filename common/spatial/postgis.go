package spatial

import (
	"fmt"
	"strings"

	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
	pq "github.com/lib/pq"
	"gorm.io/gorm"
)

const PostGISEngineType = "postgresql"

// IsPostGISEngine reports whether the engine is supported by ADDP's current
// PostGIS-specific spatial preview and MVT adapter.
func IsPostGISEngine(engineType string) bool {
	return strings.EqualFold(strings.TrimSpace(engineType), PostGISEngineType)
}

func RequirePostGISEngine(engine *models.Engine) error {
	if engine == nil {
		return fmt.Errorf("engine is required")
	}
	if !IsPostGISEngine(engine.EngineType) {
		return fmt.Errorf("postgis spatial adapter only supports postgresql engines, got %s", engine.EngineType)
	}
	return nil
}

func GetPostGISPool(engine *models.Engine, config *plugin.PoolConfig) (*gorm.DB, error) {
	if err := RequirePostGISEngine(engine); err != nil {
		return nil, err
	}
	if config == nil {
		config = dbbridge.DefaultPoolConfig()
	}
	db, err := dbbridge.GetOrCreatePool(engine, config)
	if err != nil {
		return nil, fmt.Errorf("failed to get postgis connection pool: %w", err)
	}
	return db, nil
}

func QuotePostGISIdentifier(identifier string) string {
	return pq.QuoteIdentifier(identifier)
}

func QualifiedPostGISTable(schema, table string) string {
	return QuotePostGISIdentifier(schema) + "." + QuotePostGISIdentifier(table)
}

func PostGISGeoJSONExpression(geomColumn string, transformToWGS84 bool) string {
	qGeom := QuotePostGISIdentifier(geomColumn)
	if transformToWGS84 {
		return fmt.Sprintf("ST_AsGeoJSON(ST_Transform(%s, 4326))", qGeom)
	}
	return fmt.Sprintf("ST_AsGeoJSON(%s)", qGeom)
}

func IsPostGISSpatialType(dataType string) bool {
	dataTypeLower := strings.ToLower(strings.TrimSpace(dataType))
	return dataTypeLower == "geometry" ||
		dataTypeLower == "geography" ||
		strings.HasPrefix(dataTypeLower, "geometry(") ||
		strings.HasPrefix(dataTypeLower, "geography(") ||
		dataTypeLower == "user-defined"
}

func IsPostGISGeographyType(dataType string) bool {
	dataTypeLower := strings.ToLower(strings.TrimSpace(dataType))
	return dataTypeLower == "geography" || strings.HasPrefix(dataTypeLower, "geography(")
}

func PostGISWKTExpression(columnName, dataType string) string {
	quotedColumn := QuotePostGISIdentifier(columnName)
	if IsPostGISGeographyType(dataType) {
		return fmt.Sprintf("ST_AsText(%s::geometry)", quotedColumn)
	}
	return fmt.Sprintf("ST_AsText(%s)", quotedColumn)
}

func PostGISRenderGeoJSONExpression(columnName, dataType string) string {
	quotedColumn := QuotePostGISIdentifier(columnName)
	if IsPostGISGeographyType(dataType) {
		return fmt.Sprintf("CASE WHEN %s IS NULL THEN NULL ELSE ST_AsGeoJSON(%s::geometry) END", quotedColumn, quotedColumn)
	}
	return fmt.Sprintf("CASE WHEN %s IS NULL THEN NULL WHEN ST_SRID(%s) IN (0, 4326) THEN ST_AsGeoJSON(%s) ELSE ST_AsGeoJSON(ST_Transform(%s, 4326)) END", quotedColumn, quotedColumn, quotedColumn, quotedColumn)
}

func PostGISGeoJSONSelectExpression(columnName string) string {
	quotedColumn := QuotePostGISIdentifier(columnName)
	return fmt.Sprintf("ST_AsGeoJSON(%s) AS %s", quotedColumn, quotedColumn)
}

func BuildPostGISFeatureCentroidQuery(schema, table, geomColumn, primaryKey string) string {
	qGeom := QuotePostGISIdentifier(geomColumn)
	return fmt.Sprintf(`
		SELECT
			ST_X(ST_Centroid(ST_Transform(%s, 4326))) AS lon,
			ST_Y(ST_Centroid(ST_Transform(%s, 4326))) AS lat
		FROM %s
		WHERE %s = $1
	`, qGeom, qGeom, QualifiedPostGISTable(schema, table), QuotePostGISIdentifier(primaryKey))
}

func BuildPostGISFeatureGeometryQuery(schema, table, geomColumn, primaryKey string) string {
	qGeom := QuotePostGISIdentifier(geomColumn)
	return fmt.Sprintf(`
		SELECT
			ST_AsGeoJSON(ST_Transform(%s, 4326)) AS geojson,
			ST_X(ST_Centroid(ST_Transform(%s, 4326))) AS lon,
			ST_Y(ST_Centroid(ST_Transform(%s, 4326))) AS lat,
			ST_XMin(ST_Transform(%s, 4326)) AS min_lon,
			ST_YMin(ST_Transform(%s, 4326)) AS min_lat,
			ST_XMax(ST_Transform(%s, 4326)) AS max_lon,
			ST_YMax(ST_Transform(%s, 4326)) AS max_lat
		FROM %s
		WHERE %s = $1
	`, qGeom, qGeom, qGeom, qGeom, qGeom, qGeom, qGeom, QualifiedPostGISTable(schema, table), QuotePostGISIdentifier(primaryKey))
}

func BuildPostGISGeoJSONPageQuery(schema, table, geomColumn string, limit, offset int) string {
	qGeom := QuotePostGISIdentifier(geomColumn)
	return fmt.Sprintf(`
		SELECT jsonb_build_object(
			'type', 'FeatureCollection',
			'features', COALESCE(
				jsonb_agg(
					jsonb_build_object(
						'type', 'Feature',
						'id', row.row_id,
						'geometry', ST_AsGeoJSON(row.%s)::jsonb,
						'properties', to_jsonb(row.*) - %s - 'row_id'
					)
				),
				'[]'::jsonb
			)
		) as geojson
		FROM (
			SELECT
				row_number() OVER () as row_id,
				*
			FROM %s
			ORDER BY ctid
			LIMIT %d OFFSET %d
		) row
	`, qGeom, quoteLiteral(geomColumn), QualifiedPostGISTable(schema, table), limit, offset)
}

func BuildPostGISCountQuery(schema, table string) string {
	return fmt.Sprintf("SELECT COUNT(*) FROM %s", QualifiedPostGISTable(schema, table))
}

func BuildPostGISExtentQuery(schema, table, geomColumn string) string {
	return fmt.Sprintf(`
		SELECT
			ST_XMin(extent) as min_lng,
			ST_YMin(extent) as min_lat,
			ST_XMax(extent) as max_lng,
			ST_YMax(extent) as max_lat
		FROM (
			SELECT ST_Extent(ST_Transform(%s, 4326)) as extent
			FROM %s
		) subquery
	`, QuotePostGISIdentifier(geomColumn), QualifiedPostGISTable(schema, table))
}

func BuildPostGISSRIDQuery(schema, table, geomColumn string) string {
	return fmt.Sprintf(
		"SELECT ST_SRID(%s) FROM %s WHERE %s IS NOT NULL LIMIT 1",
		QuotePostGISIdentifier(geomColumn),
		QualifiedPostGISTable(schema, table),
		QuotePostGISIdentifier(geomColumn),
	)
}

func PostGISMaterializedViewName(table string) string {
	return table + "_mv3857"
}

func PostGISGISTIndexName(table, geomColumn string) string {
	return "idx_" + table + "_" + geomColumn + "_gist"
}

func BuildPostGISRefreshMaterializedViewSQL(schema, view string) string {
	return "REFRESH MATERIALIZED VIEW CONCURRENTLY " + QualifiedPostGISTable(schema, view)
}

func BuildPostGISAnalyzeSQL(schema, table string) string {
	return "ANALYZE " + QualifiedPostGISTable(schema, table)
}

func BuildPostGISDropIndexSQL(schema, indexName string) string {
	return "DROP INDEX IF EXISTS " + QualifiedPostGISTable(schema, indexName)
}

func BuildPostGISCreateGISTIndexSQL(schema, table, indexName, geomColumn string, concurrently bool) string {
	concurrentlySQL := ""
	if concurrently {
		concurrentlySQL = " CONCURRENTLY"
	}
	return fmt.Sprintf(
		"CREATE INDEX%s %s ON %s USING GIST (%s)",
		concurrentlySQL,
		QuotePostGISIdentifier(indexName),
		QualifiedPostGISTable(schema, table),
		QuotePostGISIdentifier(geomColumn),
	)
}

func BuildPostGISCreate3857MaterializedViewSQL(schema, sourceTable, viewTable, sourceGeomColumn, selectClause string) string {
	if strings.TrimSpace(selectClause) == "" {
		selectClause = "row_number() OVER () AS id"
	}
	return fmt.Sprintf(`
		CREATE MATERIALIZED VIEW %s AS
		SELECT
			%s,
			ST_Transform(%s, 3857) AS geom_3857
		FROM %s
		WHERE %s IS NOT NULL
	`,
		QualifiedPostGISTable(schema, viewTable),
		selectClause,
		QuotePostGISIdentifier(sourceGeomColumn),
		QualifiedPostGISTable(schema, sourceTable),
		QuotePostGISIdentifier(sourceGeomColumn),
	)
}

func quoteLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
