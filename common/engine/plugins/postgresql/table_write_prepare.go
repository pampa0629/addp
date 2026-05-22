package postgresql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/sqldialect"
)

func (p *PostgreSQLPlugin) PrepareTableWrite(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.TableWriteOptions) error {
	schema, table, err := tablePathParts(path)
	if err != nil {
		return err
	}
	connStr, err := p.BuildDSN(connInfo)
	if err != nil {
		return fmt.Errorf("failed to build postgresql dsn: %w", err)
	}
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Errorf("failed to open postgresql connection: %w", err)
	}
	defer db.Close()

	return createPostgresTableIfNotExists(ctx, db, schema, table, opts.Fields, opts.SpatialInfo)
}

func createPostgresTableIfNotExists(ctx context.Context, db *sql.DB, schema, table string, fields []plugin.FieldInfo, spatialInfo *datatype.SpatialInfo) error {
	if len(fields) == 0 {
		return fmt.Errorf("postgresql table write prepare requires table fields")
	}
	dialect := sqldialect.ForEngine("postgresql")
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS "+dialect.QuoteIdentifier(schema)); err != nil {
		return fmt.Errorf("create postgresql schema %s: %w", schema, err)
	}

	definitions := make([]string, 0, len(fields))
	primaryKeys := make([]string, 0)
	seen := map[string]struct{}{}
	for _, field := range fields {
		name := strings.TrimSpace(field.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		definition := dialect.QuoteIdentifier(name) + " " + postgresSQLTypeForField(field, spatialInfo)
		if !field.Nullable {
			definition += " NOT NULL"
		}
		definitions = append(definitions, definition)
		if field.PrimaryKey {
			primaryKeys = append(primaryKeys, dialect.QuoteIdentifier(name))
		}
	}
	if len(definitions) == 0 {
		return fmt.Errorf("postgresql table write prepare requires at least one named field")
	}
	if len(primaryKeys) > 0 {
		definitions = append(definitions, "PRIMARY KEY ("+strings.Join(primaryKeys, ", ")+")")
	}

	createSQL := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s)", dialect.QualifiedTable(schema, table), strings.Join(definitions, ", "))
	if _, err := db.ExecContext(ctx, createSQL); err != nil {
		return fmt.Errorf("create postgresql table %s.%s: %w", schema, table, err)
	}
	return nil
}

func (p *PostgreSQLPlugin) DeleteResource(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath) error {
	schema, table, err := tablePathParts(path)
	if err != nil {
		return err
	}
	connStr, err := p.BuildDSN(connInfo)
	if err != nil {
		return fmt.Errorf("failed to build postgresql dsn: %w", err)
	}
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Errorf("failed to open postgresql connection: %w", err)
	}
	defer db.Close()

	dropSQL := "DROP TABLE IF EXISTS " + sqldialect.ForEngine("postgresql").QualifiedTable(schema, table)
	if _, err := db.ExecContext(ctx, dropSQL); err != nil {
		return fmt.Errorf("drop postgresql table %s.%s: %w", schema, table, err)
	}
	return nil
}

func postgresSQLTypeForField(field plugin.FieldInfo, spatialInfo *datatype.SpatialInfo) string {
	if sqlType := postgresSpatialTypeForField(field, spatialInfo); sqlType != "" {
		return sqlType
	}
	if sqlType, ok := postgresSQLTypeForCommonType(field.Type); ok {
		return sqlType
	}
	return "TEXT"
}

func postgresSpatialTypeForField(field plugin.FieldInfo, spatialInfo *datatype.SpatialInfo) string {
	if !datatype.IsSpatialFieldType(field.Type) {
		return ""
	}
	column := postgresSpatialColumnForField(spatialInfo, field.Name)
	geometryType := ""
	dimension := int64(0)
	srid := int64(0)
	if column != nil {
		geometryType = strings.TrimSpace(column.GeometryType)
		if column.Dimension != nil {
			dimension = int64(*column.Dimension)
		}
		if column.SRID != nil {
			srid = int64(*column.SRID)
		}
	}
	if geometryType == "" {
		geometryType = postgresGeometryTypeForFieldType(field.Type)
	}
	if geometryType == "" {
		return ""
	}
	geometryType = postgresGeometryTypeWithDimension(geometryType, dimension)
	if srid > 0 {
		return fmt.Sprintf("GEOMETRY(%s,%d)", geometryType, srid)
	}
	return fmt.Sprintf("GEOMETRY(%s)", geometryType)
}

func postgresSpatialColumnForField(spatialInfo *datatype.SpatialInfo, fieldName string) *datatype.GeometryColumnInfo {
	if spatialInfo == nil || fieldName == "" {
		return nil
	}
	for i := range spatialInfo.GeometryColumns {
		if strings.EqualFold(spatialInfo.GeometryColumns[i].Name, fieldName) {
			return &spatialInfo.GeometryColumns[i]
		}
	}
	return nil
}

func postgresGeometryTypeForFieldType(fieldType datatype.FieldType) string {
	switch fieldType {
	case datatype.FieldTypePoint:
		return "Point"
	case datatype.FieldTypeLineString:
		return "LineString"
	case datatype.FieldTypePolygon:
		return "Polygon"
	case datatype.FieldTypeMultiPoint:
		return "MultiPoint"
	default:
		return ""
	}
}

func postgresGeometryTypeWithDimension(geometryType string, dimension int64) string {
	if dimension != 3 {
		return geometryType
	}
	normalized := strings.TrimSpace(geometryType)
	if normalized == "" {
		return geometryType
	}
	lower := strings.ToLower(normalized)
	if strings.HasSuffix(lower, "z") || strings.HasSuffix(lower, "m") {
		return normalized
	}
	return normalized + "Z"
}

func postgresSQLTypeForCommonType(fieldType datatype.FieldType) (string, bool) {
	switch datatype.ParseFieldType(string(fieldType)) {
	case datatype.FieldTypeString:
		return "TEXT", true
	case "":
		return "TEXT", true
	case datatype.FieldTypeInt:
		return "INTEGER", true
	case datatype.FieldTypeBigInt:
		return "BIGINT", true
	case datatype.FieldTypeFloat:
		return "REAL", true
	case datatype.FieldTypeDouble:
		return "DOUBLE PRECISION", true
	case datatype.FieldTypeDecimal:
		return "NUMERIC", true
	case datatype.FieldTypeBool:
		return "BOOLEAN", true
	case datatype.FieldTypeDate:
		return "DATE", true
	case datatype.FieldTypeTime:
		return "TIME", true
	case datatype.FieldTypeTimestamp:
		return "TIMESTAMP", true
	case datatype.FieldTypeBytes:
		return "BYTEA", true
	case datatype.FieldTypeGeometry:
		return "GEOMETRY", true
	case datatype.FieldTypePoint:
		return "GEOMETRY(Point)", true
	case datatype.FieldTypeLineString:
		return "GEOMETRY(LineString)", true
	case datatype.FieldTypePolygon:
		return "GEOMETRY(Polygon)", true
	case datatype.FieldTypeMultiPoint:
		return "GEOMETRY(MultiPoint)", true
	case datatype.FieldTypeJSON:
		return "JSONB", true
	case datatype.FieldTypeUUID:
		return "UUID", true
	case datatype.FieldTypeArray:
		return "TEXT[]", true
	default:
		return "", false
	}
}
