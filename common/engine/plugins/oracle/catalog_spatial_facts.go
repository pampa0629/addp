package oracle

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
	commonquery "github.com/addp/common/query"
	"gorm.io/gorm"
)

type oracleSpatialMetadataRow struct {
	ColumnName string
	SRID       sql.NullInt64
	Dimension  sql.NullInt64
}

type oracleSpatialIndexRow struct {
	Name       string
	ColumnName string
}

func (p *OraclePlugin) describeSpatialFacts(ctx context.Context, db *gorm.DB, schema, table string, fields []datatype.FieldInfo) (*datatype.SpatialInfo, error) {
	spatialInfo := oracleSpatialInfoFromFields(fields)
	if spatialInfo == nil {
		return nil, nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get Oracle spatial facts connection: %w", err)
	}
	return enrichOracleSpatialInfo(ctx, sqlDB, schema, table, spatialInfo)
}

func enrichOracleSpatialInfo(ctx context.Context, db *sql.DB, schema, table string, spatialInfo *datatype.SpatialInfo) (*datatype.SpatialInfo, error) {
	if spatialInfo == nil {
		return nil, nil
	}
	metadata, err := queryOracleSpatialMetadata(ctx, db, schema, table)
	if err != nil {
		return nil, err
	}
	for index := range spatialInfo.GeometryColumns {
		column := &spatialInfo.GeometryColumns[index]
		row, ok := metadata[column.Name]
		if !ok {
			continue
		}
		if row.SRID.Valid && row.SRID.Int64 > 0 {
			srid := int(row.SRID.Int64)
			column.SRID = &srid
			column.CRSRef = datatype.EPSGCRSRef(srid)
		}
		if row.Dimension.Valid && row.Dimension.Int64 > 0 {
			dimension := int(row.Dimension.Int64)
			column.Dimension = &dimension
		}
		if definition, err := queryOracleCRSDefinition(ctx, db, column.SRID); err != nil {
			return nil, err
		} else if definition != nil {
			column.CRSRef = definition.ID
			spatialInfo.CRSDefinitions = append(spatialInfo.CRSDefinitions, *definition)
		}
		geometryType, err := queryOracleGeometryType(ctx, db, schema, table, column.Name)
		if err != nil {
			return nil, fmt.Errorf("query Oracle geometry type for %s.%s.%s: %w", schema, table, column.Name, err)
		}
		if geometryType != "" {
			column.GeometryType = geometryType
		}
	}
	indexes, err := queryOracleSpatialIndexes(ctx, db, schema, table)
	if err != nil {
		return nil, err
	}
	if primary := spatialInfo.PrimaryGeometry(); primary != nil {
		for _, index := range indexes {
			if strings.EqualFold(index.ColumnName, primary.Name) {
				hasIndex := true
				spatialInfo.HasSpatialIndex = &hasIndex
				spatialInfo.IndexName = index.Name
				break
			}
		}
		if spatialInfo.HasSpatialIndex == nil {
			hasIndex := false
			spatialInfo.HasSpatialIndex = &hasIndex
		}
	}
	return spatialInfo, nil
}

func oracleSpatialInfoFromFields(fields []datatype.FieldInfo) *datatype.SpatialInfo {
	columns := make([]datatype.GeometryColumnInfo, 0)
	for _, field := range fields {
		if !datatype.IsSpatialFieldType(field.Type) {
			continue
		}
		nullable := field.Nullable
		columns = append(columns, datatype.GeometryColumnInfo{
			Name:         field.Name,
			GeometryType: string(datatype.GeometryTypeGeometry),
			Nullable:     &nullable,
		})
	}
	if len(columns) == 0 {
		return nil
	}
	return &datatype.SpatialInfo{GeometryColumns: columns, PrimaryGeometryColumn: columns[0].Name}
}

func queryOracleSpatialMetadata(ctx context.Context, db *sql.DB, schema, table string) (map[string]oracleSpatialMetadataRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT metadata.column_name,
		       metadata.srid,
		       (SELECT COUNT(*) FROM TABLE(metadata.diminfo))
		  FROM all_sdo_geom_metadata metadata
		 WHERE metadata.owner = :1 AND metadata.table_name = :2
		 ORDER BY column_name
	`, schema, table)
	if err != nil {
		return nil, fmt.Errorf("query Oracle spatial metadata: %w", err)
	}
	defer rows.Close()
	result := make(map[string]oracleSpatialMetadataRow)
	for rows.Next() {
		var row oracleSpatialMetadataRow
		if err := rows.Scan(&row.ColumnName, &row.SRID, &row.Dimension); err != nil {
			return nil, fmt.Errorf("scan Oracle spatial metadata: %w", err)
		}
		result[oracleSpatialMetadataFieldName(row.ColumnName)] = row
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Oracle spatial metadata: %w", err)
	}
	return result, nil
}

func oracleSpatialMetadataFieldName(name string) string {
	name = strings.TrimSpace(name)
	if len(name) >= 2 && name[0] == '"' && name[len(name)-1] == '"' {
		return strings.ReplaceAll(name[1:len(name)-1], `""`, `"`)
	}
	return name
}

func queryOracleCRSDefinition(ctx context.Context, db *sql.DB, srid *int) (*datatype.CRSDefinition, error) {
	if srid == nil || *srid <= 0 {
		return nil, nil
	}
	var definition sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT DBMS_LOB.SUBSTR(wktext, 4000, 1)
		  FROM mdsys.cs_srs
		 WHERE srid = :1
	`, *srid).Scan(&definition)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query Oracle CRS definition %d: %w", *srid, err)
	}
	text := strings.TrimSpace(definition.String)
	if text == "" {
		return nil, nil
	}
	return &datatype.CRSDefinition{
		ID:                 datatype.EPSGCRSRef(*srid),
		DefinitionEncoding: datatype.CRSDefinitionEncodingWKT,
		Definition:         text,
		Source:             datatype.CRSDefinitionSourceOracleCSRS,
	}, nil
}

func queryOracleGeometryType(ctx context.Context, db *sql.DB, schema, table, column string) (string, error) {
	dialect := commonquery.ForEngine("oracle")
	qualified := dialect.QualifiedTable(schema, table)
	quoted := dialect.QuoteIdentifier(column)
	var minType, maxType sql.NullInt64
	query := fmt.Sprintf(`
		SELECT MIN(gtype), MAX(gtype)
		  FROM (SELECT MOD(source_row.%s.SDO_GTYPE, 1000) AS gtype
		          FROM %s source_row
		         WHERE source_row.%s IS NOT NULL AND ROWNUM <= 100)
	`, quoted, qualified, quoted)
	if err := db.QueryRowContext(ctx, query).Scan(&minType, &maxType); err != nil {
		return "", err
	}
	if !minType.Valid || !maxType.Valid || minType.Int64 != maxType.Int64 {
		return string(datatype.GeometryTypeGeometry), nil
	}
	switch minType.Int64 {
	case 1:
		return string(datatype.GeometryTypePoint), nil
	case 2:
		return string(datatype.GeometryTypeLineString), nil
	case 3:
		return string(datatype.GeometryTypePolygon), nil
	case 4:
		return string(datatype.GeometryTypeGeometryCollection), nil
	case 5:
		return string(datatype.GeometryTypeMultiPoint), nil
	case 6:
		return string(datatype.GeometryTypeMultiLineString), nil
	case 7:
		return string(datatype.GeometryTypeMultiPolygon), nil
	default:
		return string(datatype.GeometryTypeGeometry), nil
	}
}

func queryOracleSpatialIndexes(ctx context.Context, db *sql.DB, schema, table string) ([]oracleSpatialIndexRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT i.index_name, ic.column_name
		  FROM all_indexes i
		  JOIN all_ind_columns ic
		    ON ic.index_owner = i.owner
		   AND ic.index_name = i.index_name
		   AND ic.table_owner = i.table_owner
		   AND ic.table_name = i.table_name
		 WHERE i.table_owner = :1
		   AND i.table_name = :2
		   AND i.index_type = 'DOMAIN'
		   AND UPPER(i.ityp_owner) = 'MDSYS'
		   AND UPPER(i.ityp_name) IN ('SPATIAL_INDEX', 'SPATIAL_INDEX_V2')
		 ORDER BY i.index_name, ic.column_position
	`, schema, table)
	if err != nil {
		return nil, fmt.Errorf("query Oracle spatial indexes: %w", err)
	}
	defer rows.Close()
	result := make([]oracleSpatialIndexRow, 0)
	for rows.Next() {
		var row oracleSpatialIndexRow
		if err := rows.Scan(&row.Name, &row.ColumnName); err != nil {
			return nil, fmt.Errorf("scan Oracle spatial index: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Oracle spatial indexes: %w", err)
	}
	return result, nil
}
