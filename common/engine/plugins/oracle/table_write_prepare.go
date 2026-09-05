package oracle

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"math"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
)

const oracleSpatialMinimumTolerance = 1e-9

func (p *OraclePlugin) PrepareTableWrite(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableWriteOptions) error {
	schema, table, err := oracleTablePathParts(path)
	if err != nil {
		return err
	}
	fields, err := validateOracleWriteFields(opts.Fields, opts.SpatialInfo)
	if err != nil {
		return err
	}
	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		return fmt.Errorf("build Oracle table write DSN: %w", err)
	}
	db, err := sql.Open("oracle", dsn)
	if err != nil {
		return fmt.Errorf("open Oracle table write connection: %w", err)
	}
	defer db.Close()

	if err := validateOracleTargetSchema(ctx, db, schema); err != nil {
		return err
	}
	exists, err := oracleBaseTableExists(ctx, db, schema, table)
	if err != nil {
		return err
	}
	if !exists {
		if err := createOracleTable(ctx, db, schema, table, fields); err != nil {
			return err
		}
	} else if err := evolveOracleTable(ctx, db, schema, table, fields, opts.SpatialInfo); err != nil {
		return err
	}
	return ensureOracleSpatialMetadataAndIndexes(ctx, db, schema, table, fields, opts.SpatialInfo)
}

func (p *OraclePlugin) DeleteResource(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath) error {
	schema, table, err := oracleTablePathParts(path)
	if err != nil {
		return err
	}
	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		return fmt.Errorf("build Oracle delete resource DSN: %w", err)
	}
	db, err := sql.Open("oracle", dsn)
	if err != nil {
		return fmt.Errorf("open Oracle delete resource connection: %w", err)
	}
	defer db.Close()
	if err := validateOracleTargetSchema(ctx, db, schema); err != nil {
		return err
	}
	exists, err := oracleBaseTableExists(ctx, db, schema, table)
	if err != nil {
		return err
	}
	if exists {
		qualified := commonquery.ForDialect("oracle").QualifiedTable(schema, table)
		if _, err := db.ExecContext(ctx, "DROP TABLE "+qualified+" PURGE"); err != nil {
			return fmt.Errorf("drop Oracle table %s.%s: %w", schema, table, err)
		}
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM USER_SDO_GEOM_METADATA WHERE TABLE_NAME = :1", table); err != nil {
		return fmt.Errorf("delete Oracle spatial metadata for %s.%s: %w", schema, table, err)
	}
	return nil
}

func ensureOracleSpatialMetadataAndIndexes(ctx context.Context, db *sql.DB, schema, table string, fields []datatype.FieldInfo, spatialInfo *datatype.SpatialInfo) error {
	for _, field := range fields {
		if !datatype.IsSpatialFieldType(field.Type) {
			continue
		}
		column := oracleSpatialColumnForField(spatialInfo, field.Name)
		if column == nil {
			return fmt.Errorf("oracle geometry field %q requires frozen spatial facts", field.Name)
		}
		if err := ensureOracleSpatialMetadata(ctx, db, table, *column, spatialInfo); err != nil {
			return err
		}
		if err := ensureOracleSpatialIndex(ctx, db, schema, table, field.Name); err != nil {
			return err
		}
	}
	return nil
}

func ensureOracleSpatialMetadata(ctx context.Context, db *sql.DB, table string, column datatype.GeometryColumnInfo, spatialInfo *datatype.SpatialInfo) error {
	metadataColumnName := oracleSpatialMetadataColumnName(column.Name)
	dimension := 0
	if column.Dimension != nil {
		dimension = *column.Dimension
	}
	if dimension != 2 {
		return fmt.Errorf("oracle geometry field %q requires XY dimension 2", column.Name)
	}
	expectedSRID := 0
	if column.SRID != nil {
		expectedSRID = *column.SRID
	}

	var currentSRID sql.NullInt64
	var currentDimension int
	err := db.QueryRowContext(ctx, `
		SELECT metadata.srid, (SELECT COUNT(*) FROM TABLE(metadata.diminfo))
		  FROM user_sdo_geom_metadata metadata
		 WHERE metadata.table_name = :1 AND metadata.column_name = :2
	`, table, metadataColumnName).Scan(&currentSRID, &currentDimension)
	if err == nil {
		if currentDimension != dimension {
			return fmt.Errorf("oracle spatial metadata for %s.%s has dimension %d, expected %d", table, column.Name, currentDimension, dimension)
		}
		if expectedSRID > 0 && (!currentSRID.Valid || int(currentSRID.Int64) != expectedSRID) {
			return fmt.Errorf("oracle spatial metadata for %s.%s has SRID %v, expected %d", table, column.Name, currentSRID, expectedSRID)
		}
		return nil
	}
	if err != sql.ErrNoRows {
		return fmt.Errorf("query Oracle spatial metadata for %s.%s: %w", table, column.Name, err)
	}

	minX, minY, maxX, maxY, tolerance, err := oracleSpatialBounds(spatialInfo)
	if err != nil {
		return fmt.Errorf("prepare Oracle spatial metadata for %s.%s: %w", table, column.Name, err)
	}
	var srid interface{}
	if expectedSRID > 0 {
		srid = expectedSRID
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO user_sdo_geom_metadata (table_name, column_name, diminfo, srid)
		VALUES (:1, :2,
		        MDSYS.SDO_DIM_ARRAY(
		          MDSYS.SDO_DIM_ELEMENT('X', :3, :4, :5),
		          MDSYS.SDO_DIM_ELEMENT('Y', :6, :7, :8)),
		        :9)
	`, table, metadataColumnName, minX, maxX, tolerance, minY, maxY, tolerance, srid)
	if err != nil {
		return fmt.Errorf("insert Oracle spatial metadata for %s.%s: %w", table, column.Name, err)
	}
	return nil
}

func oracleSpatialMetadataColumnName(name string) string {
	name = strings.TrimSpace(name)
	if isOracleUnquotedIdentifier(name) && name == strings.ToUpper(name) {
		return name
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func isOracleUnquotedIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for index, value := range []byte(name) {
		if index == 0 {
			if !((value >= 'A' && value <= 'Z') || value == '_') {
				return false
			}
			continue
		}
		if !((value >= 'A' && value <= 'Z') || (value >= '0' && value <= '9') || value == '_' || value == '$' || value == '#') {
			return false
		}
	}
	return true
}

func oracleSpatialBounds(spatialInfo *datatype.SpatialInfo) (float64, float64, float64, float64, float64, error) {
	if spatialInfo == nil || spatialInfo.Extent == nil {
		return 0, 0, 0, 0, 0, fmt.Errorf("frozen spatial extent is required")
	}
	extent := *spatialInfo.Extent
	for _, value := range extent {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return 0, 0, 0, 0, 0, fmt.Errorf("spatial extent must contain finite coordinates")
		}
	}
	minX, minY, maxX, maxY := extent[0], extent[1], extent[2], extent[3]
	if maxX < minX || maxY < minY {
		return 0, 0, 0, 0, 0, fmt.Errorf("spatial extent bounds are reversed")
	}
	span := math.Max(maxX-minX, maxY-minY)
	tolerance := math.Max(span*1e-9, oracleSpatialMinimumTolerance)
	if minX == maxX {
		minX -= tolerance
		maxX += tolerance
	}
	if minY == maxY {
		minY -= tolerance
		maxY += tolerance
	}
	return minX, minY, maxX, maxY, tolerance, nil
}

func ensureOracleSpatialIndex(ctx context.Context, db *sql.DB, schema, table, column string) error {
	var count int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		  FROM all_indexes i
		  JOIN all_ind_columns c
		    ON c.index_owner = i.owner
		   AND c.index_name = i.index_name
		   AND c.table_owner = i.table_owner
		   AND c.table_name = i.table_name
		 WHERE i.table_owner = :1
		   AND i.table_name = :2
		   AND c.column_name = :3
		   AND i.index_type = 'DOMAIN'
		   AND UPPER(i.ityp_owner) = 'MDSYS'
		   AND UPPER(i.ityp_name) IN ('SPATIAL_INDEX', 'SPATIAL_INDEX_V2')
	`, schema, table, column).Scan(&count)
	if err != nil {
		return fmt.Errorf("query Oracle spatial index for %s.%s.%s: %w", schema, table, column, err)
	}
	if count > 0 {
		return nil
	}
	dialect := commonquery.ForDialect("oracle")
	indexName := oracleSpatialIndexName(schema, table, column)
	statement := "CREATE INDEX " + dialect.QuoteIdentifier(indexName) + " ON " +
		dialect.QualifiedTable(schema, table) + " (" + dialect.QuoteIdentifier(column) + ") " +
		"INDEXTYPE IS MDSYS.SPATIAL_INDEX_V2"
	if _, err := db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("create Oracle spatial index %s on %s.%s.%s: %w", indexName, schema, table, column, err)
	}
	return nil
}

func oracleSpatialIndexName(schema, table, column string) string {
	sum := sha256.Sum256([]byte(strings.ToUpper(schema + "." + table + "." + column)))
	return fmt.Sprintf("ADDP_SIDX_%X", sum[:8])
}
