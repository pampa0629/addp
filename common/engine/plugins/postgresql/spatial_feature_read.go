package postgresql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
)

func (p *PostgreSQLPlugin) ReadSpatialFeature(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.SpatialFeatureReadOptions) (*plugin.SpatialFeatureData, error) {
	schema, table, err := tablePathParts(path)
	if err != nil {
		return nil, err
	}
	connStr, err := p.BuildDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("build postgresql spatial feature dsn: %w", err)
	}
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("open postgresql spatial feature connection: %w", err)
	}
	defer db.Close()

	columns, err := postgresTableColumns(ctx, db, schema, table)
	if err != nil {
		return nil, err
	}
	geometryColumn, identityColumn, err := resolvePostgresSpatialFeatureColumns(columns, opts)
	if err != nil {
		return nil, err
	}
	dialect := commonquery.ForEngine("postgresql")
	quotedGeometry := dialect.QuoteIdentifier(geometryColumn.Name)
	geometryExpression := quotedGeometry
	if strings.EqualFold(strings.TrimSpace(geometryColumn.UDTName), "geography") {
		geometryExpression += "::geometry"
	}
	query := fmt.Sprintf(
		"SELECT ST_AsEWKB(%s), ST_AsEWKB(ST_Centroid(%s)), ST_SRID(%s) FROM %s WHERE %s = $1 LIMIT 1",
		geometryExpression,
		geometryExpression,
		geometryExpression,
		dialect.QualifiedTable(schema, table),
		dialect.QuoteIdentifier(identityColumn.Name),
	)
	var geometryEWKB, centroidEWKB []byte
	var srid int
	if err := db.QueryRowContext(ctx, query, opts.IdentityValue).Scan(&geometryEWKB, &centroidEWKB, &srid); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("read postgresql spatial feature: %w", err)
	}
	field := postgresFieldInfoFromColumn(geometryColumn)
	return &plugin.SpatialFeatureData{
		GeometryEWKB: geometryEWKB,
		CentroidEWKB: centroidEWKB,
		SRID:         srid,
		Spatial:      postgresSpatialInfoFromFields([]datatype.FieldInfo{field}),
	}, nil
}

func resolvePostgresSpatialFeatureColumns(columns []postgresColumnInfo, opts plugin.SpatialFeatureReadOptions) (postgresColumnInfo, postgresColumnInfo, error) {
	geometryName := strings.TrimSpace(opts.GeometryField)
	identityName := strings.TrimSpace(opts.IdentityField)
	if geometryName == "" || identityName == "" {
		return postgresColumnInfo{}, postgresColumnInfo{}, fmt.Errorf("postgresql spatial feature requires geometry_field and identity_field")
	}
	var geometryColumn, identityColumn postgresColumnInfo
	for _, column := range columns {
		if strings.EqualFold(column.Name, geometryName) {
			geometryColumn = column
		}
		if strings.EqualFold(column.Name, identityName) {
			identityColumn = column
		}
	}
	if geometryColumn.Name == "" || !geometryColumn.IsSpatial() {
		return postgresColumnInfo{}, postgresColumnInfo{}, fmt.Errorf("postgresql spatial feature geometry field %q does not exist or is not spatial", geometryName)
	}
	if identityColumn.Name == "" {
		return postgresColumnInfo{}, postgresColumnInfo{}, fmt.Errorf("postgresql spatial feature identity field %q does not exist", identityName)
	}
	return geometryColumn, identityColumn, nil
}
