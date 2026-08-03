package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/addp/common/datatype"
	"gorm.io/gorm"
)

type mysqlSpatialColumnRow struct {
	Name     string        `gorm:"column:name"`
	DataType string        `gorm:"column:data_type"`
	SRSID    sql.NullInt64 `gorm:"column:srs_id"`
	Nullable bool          `gorm:"column:nullable"`
}

type mysqlSpatialIndexRow struct {
	Name       string `gorm:"column:name"`
	ColumnName string `gorm:"column:column_name"`
}

type mysqlCRSRow struct {
	SRSID                    int            `gorm:"column:srs_id"`
	Organization             sql.NullString `gorm:"column:organization"`
	OrganizationCoordinateID sql.NullInt64  `gorm:"column:organization_coordinate_id"`
	Definition               string         `gorm:"column:definition"`
}

func (p *MySQLPlugin) describeSpatialFacts(ctx context.Context, db *gorm.DB, database, table string, _ []datatype.FieldInfo) (*datatype.SpatialInfo, error) {
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get mysql spatial facts connection: %w", err)
	}
	columnRows, err := sqlDB.QueryContext(ctx, `
		SELECT column_name AS name, data_type, srs_id, (is_nullable = 'YES') AS nullable
		FROM information_schema.columns
		WHERE table_schema = ? AND table_name = ?
		  AND data_type IN ('geometry', 'point', 'linestring', 'polygon', 'multipoint', 'multilinestring', 'multipolygon', 'geomcollection')
		ORDER BY ordinal_position
	`, database, table)
	if err != nil {
		return nil, fmt.Errorf("query mysql spatial columns: %w", err)
	}
	defer columnRows.Close()
	columns := make([]mysqlSpatialColumnRow, 0)
	for columnRows.Next() {
		var column mysqlSpatialColumnRow
		if err := columnRows.Scan(&column.Name, &column.DataType, &column.SRSID, &column.Nullable); err != nil {
			return nil, fmt.Errorf("scan mysql spatial column: %w", err)
		}
		columns = append(columns, column)
	}
	if err := columnRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mysql spatial columns: %w", err)
	}
	if len(columns) == 0 {
		return nil, nil
	}

	indexRows, err := sqlDB.QueryContext(ctx, `
		SELECT index_name AS name, column_name
		FROM information_schema.statistics
		WHERE table_schema = ? AND table_name = ? AND index_type = 'SPATIAL'
		ORDER BY index_name, seq_in_index
	`, database, table)
	if err != nil {
		return nil, fmt.Errorf("query mysql spatial indexes: %w", err)
	}
	defer indexRows.Close()
	indexes := make([]mysqlSpatialIndexRow, 0)
	for indexRows.Next() {
		var index mysqlSpatialIndexRow
		if err := indexRows.Scan(&index.Name, &index.ColumnName); err != nil {
			return nil, fmt.Errorf("scan mysql spatial index: %w", err)
		}
		indexes = append(indexes, index)
	}
	if err := indexRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mysql spatial indexes: %w", err)
	}

	definitions := make(map[int]datatype.CRSDefinition)
	for _, column := range columns {
		if !column.SRSID.Valid || column.SRSID.Int64 <= 0 {
			continue
		}
		srid := int(column.SRSID.Int64)
		if _, exists := definitions[srid]; exists {
			continue
		}
		definition, err := queryMySQLCRSDefinition(ctx, sqlDB, srid)
		if err != nil {
			return nil, err
		}
		if definition != nil {
			definitions[srid] = *definition
		}
	}
	return buildMySQLSpatialInfo(columns, indexes, definitions), nil
}

func queryMySQLCRSDefinition(ctx context.Context, db *sql.DB, srid int) (*datatype.CRSDefinition, error) {
	var row mysqlCRSRow
	err := db.QueryRowContext(ctx, `
		SELECT srs_id, organization, organization_coordsys_id AS organization_coordinate_id, definition
		FROM information_schema.st_spatial_reference_systems
		WHERE srs_id = ?
	`, srid).Scan(&row.SRSID, &row.Organization, &row.OrganizationCoordinateID, &row.Definition)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query mysql CRS definition %d: %w", srid, err)
	}
	if strings.TrimSpace(row.Definition) == "" {
		return nil, nil
	}
	organizationID := 0
	if row.OrganizationCoordinateID.Valid {
		organizationID = int(row.OrganizationCoordinateID.Int64)
	}
	id := datatype.CRSRefFromAuthority(row.Organization.String, organizationID, row.Definition)
	if id == "" {
		return nil, nil
	}
	return &datatype.CRSDefinition{
		ID:                 id,
		DefinitionEncoding: datatype.CRSDefinitionEncodingWKT,
		Definition:         row.Definition,
		Source:             datatype.CRSDefinitionSourceMySQLSpatialRefSys,
	}, nil
}

func buildMySQLSpatialInfo(columns []mysqlSpatialColumnRow, indexes []mysqlSpatialIndexRow, definitions map[int]datatype.CRSDefinition) *datatype.SpatialInfo {
	if len(columns) == 0 {
		return nil
	}
	indexedColumns := make(map[string]string, len(indexes))
	for _, index := range indexes {
		name := strings.TrimSpace(index.Name)
		column := strings.TrimSpace(index.ColumnName)
		if name != "" && column != "" {
			indexedColumns[strings.ToLower(column)] = name
		}
	}
	dimension := 2
	spatial := &datatype.SpatialInfo{
		GeometryColumns:       make([]datatype.GeometryColumnInfo, 0, len(columns)),
		PrimaryGeometryColumn: strings.TrimSpace(columns[0].Name),
	}
	hasSpatialIndex := len(indexedColumns) > 0
	spatial.HasSpatialIndex = &hasSpatialIndex
	for _, column := range columns {
		nullable := column.Nullable
		geometry := datatype.GeometryColumnInfo{
			Name:         strings.TrimSpace(column.Name),
			GeometryType: datatype.StandardGeometryType(column.DataType),
			Dimension:    &dimension,
			Nullable:     &nullable,
		}
		if geometry.GeometryType == "" {
			geometry.GeometryType = string(datatype.GeometryTypeGeometry)
		}
		if column.SRSID.Valid && column.SRSID.Int64 > 0 {
			srid := int(column.SRSID.Int64)
			geometry.SRID = &srid
			if definition, ok := definitions[srid]; ok {
				geometry.CRSRef = definition.ID
			} else {
				geometry.CRSRef = datatype.EPSGCRSRef(srid)
			}
		}
		spatial.GeometryColumns = append(spatial.GeometryColumns, geometry)
	}
	if indexName, ok := indexedColumns[strings.ToLower(spatial.PrimaryGeometryColumn)]; ok {
		spatial.IndexName = indexName
	}
	srids := make([]int, 0, len(definitions))
	for srid := range definitions {
		srids = append(srids, srid)
	}
	sort.Ints(srids)
	for _, srid := range srids {
		spatial.CRSDefinitions = append(spatial.CRSDefinitions, definitions[srid])
	}
	return spatial
}
