package postgresql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/addp/common/datatype"
	commonquery "github.com/addp/common/query"
	"gorm.io/gorm"
)

func (p *PostgreSQLPlugin) describeSpatialFacts(ctx context.Context, db *gorm.DB, schema, table string, fields []datatype.FieldInfo) (*datatype.SpatialInfo, error) {
	spatialInfo := postgresSpatialInfoFromFields(fields)
	if spatialInfo == nil || spatialInfo.PrimaryGeometryColumn == "" {
		return nil, nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	primary := spatialInfo.PrimaryGeometry()
	if primary == nil || primary.Name == "" {
		return spatialInfo, nil
	}
	if primary.SRID == nil {
		if srid, err := queryPostgresSpatialSRID(ctx, sqlDB, schema, table, primary.Name); err == nil && srid > 0 {
			primary.SRID = &srid
		}
	}
	if primary.SRID != nil {
		if definition, err := queryPostgresCRSDefinition(ctx, sqlDB, *primary.SRID); err == nil && definition != nil {
			primary.CRSRef = definition.ID
			spatialInfo.CRSDefinitions = []datatype.CRSDefinition{*definition}
		}
	}
	if primary.GeometryType == "" {
		if geometryType, err := queryPostgresGeometryType(ctx, sqlDB, schema, table, primary.Name); err == nil {
			primary.GeometryType = geometryType
		}
	}
	if extent, err := queryPostgresSpatialExtent(ctx, sqlDB, schema, table, primary.Name); err == nil {
		spatialInfo.Extent = extent
	}
	hasIndex, indexName, err := queryPostgresSpatialIndex(ctx, sqlDB, schema, table, primary.Name)
	if err == nil {
		spatialInfo.HasSpatialIndex = &hasIndex
		spatialInfo.IndexName = indexName
	}
	return spatialInfo, nil
}

func queryPostgresSpatialSRID(ctx context.Context, db *sql.DB, schema, table, column string) (int, error) {
	var srid int
	err := db.QueryRowContext(ctx, `SELECT Find_SRID($1, $2, $3)`, schema, table, column).Scan(&srid)
	if err != nil {
		return 0, fmt.Errorf("query postgresql spatial srid: %w", err)
	}
	return srid, nil
}

func queryPostgresCRSDefinition(ctx context.Context, db *sql.DB, srid int) (*datatype.CRSDefinition, error) {
	if srid <= 0 {
		return nil, nil
	}
	var authName sql.NullString
	var authSRID sql.NullInt64
	var srtext sql.NullString
	var proj4text sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT auth_name, auth_srid, srtext, proj4text
		FROM spatial_ref_sys
		WHERE srid = $1
		LIMIT 1
	`, srid).Scan(&authName, &authSRID, &srtext, &proj4text)
	if err != nil {
		return nil, fmt.Errorf("query postgresql crs definition: %w", err)
	}

	encoding := datatype.CRSDefinitionEncodingWKT
	definition := strings.TrimSpace(srtext.String)
	if definition == "" {
		encoding = datatype.CRSDefinitionEncodingProj4
		definition = strings.TrimSpace(proj4text.String)
	}
	if definition == "" {
		return nil, nil
	}

	code := 0
	if authSRID.Valid {
		code = int(authSRID.Int64)
	}
	id := datatype.CRSRefFromAuthority(authName.String, code, definition)
	if id == "" {
		return nil, nil
	}
	return &datatype.CRSDefinition{
		ID:                 id,
		DefinitionEncoding: encoding,
		Definition:         definition,
		Source:             datatype.CRSDefinitionSourcePostGISSpatialRefSys,
	}, nil
}

func queryPostgresGeometryType(ctx context.Context, db *sql.DB, schema, table, column string) (string, error) {
	dialect := commonquery.ForDialect("postgresql")
	query := fmt.Sprintf(`
		SELECT DISTINCT ST_GeometryType(%s) AS geometry_type
		FROM %s
		WHERE %s IS NOT NULL
		LIMIT 1
	`, dialect.QuoteIdentifier(column), dialect.QualifiedTable(schema, table), dialect.QuoteIdentifier(column))
	var geometryType string
	err := db.QueryRowContext(ctx, query).Scan(&geometryType)
	if err != nil {
		return "", fmt.Errorf("query postgresql geometry type: %w", err)
	}
	return strings.TrimPrefix(strings.TrimSpace(geometryType), "ST_"), nil
}

func queryPostgresSpatialExtent(ctx context.Context, db *sql.DB, schema, table, column string) (*datatype.BoundingBox, error) {
	query := postgresSpatialExtentQuery(schema, table, column)

	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	var minX, minY, maxX, maxY sql.NullFloat64
	if err := db.QueryRowContext(queryCtx, query).Scan(&minX, &minY, &maxX, &maxY); err != nil {
		return nil, fmt.Errorf("query postgresql spatial extent: %w", err)
	}
	if !minX.Valid || !minY.Valid || !maxX.Valid || !maxY.Valid {
		return nil, nil
	}
	extent := datatype.NewBoundingBox(minX.Float64, minY.Float64, maxX.Float64, maxY.Float64)
	return &extent, nil
}

func postgresSpatialExtentQuery(schema, table, column string) string {
	dialect := commonquery.ForDialect("postgresql")
	quotedColumn := dialect.QuoteIdentifier(column)
	return fmt.Sprintf(`
		SELECT
			round(ST_XMin(extent)::numeric, 6)::float8,
			round(ST_YMin(extent)::numeric, 6)::float8,
			round(ST_XMax(extent)::numeric, 6)::float8,
			round(ST_YMax(extent)::numeric, 6)::float8
		FROM (
			SELECT ST_Extent(%s) AS extent
			FROM %s
			WHERE %s IS NOT NULL
		) t
		WHERE extent IS NOT NULL
	`, quotedColumn, dialect.QualifiedTable(schema, table), quotedColumn)
}

func queryPostgresSpatialIndex(ctx context.Context, db *sql.DB, schema, table, column string) (bool, string, error) {
	var indexName string
	err := db.QueryRowContext(ctx, `
		SELECT indexname
		FROM pg_indexes
		WHERE schemaname = $1 AND tablename = $2
		  AND lower(indexdef) LIKE '%using gist%'
		  AND indexdef LIKE '%' || $3 || '%'
		LIMIT 1
	`, schema, table, column).Scan(&indexName)
	if err == sql.ErrNoRows {
		return false, "", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("query postgresql spatial index: %w", err)
	}
	return true, indexName, nil
}
