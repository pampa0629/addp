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

func (p *PostgreSQLPlugin) PrepareTableWrite(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableWriteOptions) error {
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

func createPostgresTableIfNotExists(ctx context.Context, db *sql.DB, schema, table string, fields []datatype.FieldInfo, spatialInfo *datatype.SpatialInfo) error {
	return createPostgresTable(ctx, db, schema, table, fields, spatialInfo, true)
}

func createPostgresTable(ctx context.Context, db *sql.DB, schema, table string, fields []datatype.FieldInfo, spatialInfo *datatype.SpatialInfo, ifNotExists bool) error {
	if len(fields) == 0 {
		return fmt.Errorf("postgresql table write prepare requires table fields")
	}
	dialect := commonquery.ForDialect("postgresql")
	if ifNotExists {
		if _, err := db.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS "+dialect.QuoteIdentifier(schema)); err != nil {
			return fmt.Errorf("create postgresql schema %s: %w", schema, err)
		}
	} else {
		var schemaExists bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.schemata WHERE schema_name=$1)`, schema).Scan(&schemaExists); err != nil {
			return fmt.Errorf("check postgresql target schema %s: %w", schema, err)
		}
		if !schemaExists {
			return fmt.Errorf("postgresql replay target schema %s does not exist", schema)
		}
	}

	writeFields := postgresWriteFields(fields)
	definitions := make([]string, 0, len(writeFields))
	primaryKeys := make([]string, 0)
	for _, field := range writeFields {
		definitions = append(definitions, postgresColumnDefinition(field, spatialInfo))
		if field.PrimaryKey {
			primaryKeys = append(primaryKeys, dialect.QuoteIdentifier(field.Name))
		}
	}
	if len(definitions) == 0 {
		return fmt.Errorf("postgresql table write prepare requires at least one named field")
	}
	if len(primaryKeys) > 0 {
		definitions = append(definitions, "PRIMARY KEY ("+strings.Join(primaryKeys, ", ")+")")
	}

	createPrefix := "CREATE TABLE "
	if ifNotExists {
		createPrefix = "CREATE TABLE IF NOT EXISTS "
	}
	createSQL := fmt.Sprintf("%s%s (%s)", createPrefix, dialect.QualifiedTable(schema, table), strings.Join(definitions, ", "))
	if _, err := db.ExecContext(ctx, createSQL); err != nil {
		return fmt.Errorf("create postgresql table %s.%s: %w", schema, table, err)
	}
	if !ifNotExists {
		return nil
	}
	if err := evolvePostgresTableSchema(ctx, db, schema, table, writeFields, spatialInfo); err != nil {
		return err
	}
	return nil
}

func evolvePostgresTableSchema(ctx context.Context, db *sql.DB, schema, table string, fields []datatype.FieldInfo, spatialInfo *datatype.SpatialInfo) error {
	columns, err := postgresTableColumns(ctx, db, schema, table)
	if err != nil {
		return err
	}
	statements, err := postgresSchemaEvolutionStatements(schema, table, fields, spatialInfo, columns)
	if err != nil {
		return err
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("evolve postgresql table %s.%s schema: %w", schema, table, err)
		}
	}
	return nil
}

func postgresSchemaEvolutionStatements(schema, table string, fields []datatype.FieldInfo, spatialInfo *datatype.SpatialInfo, existingColumns []postgresColumnInfo) ([]string, error) {
	dialect := commonquery.ForDialect("postgresql")
	existingByName := make(map[string]postgresColumnInfo, len(existingColumns))
	for _, column := range existingColumns {
		existingByName[column.Name] = column
	}

	statements := make([]string, 0)
	for _, field := range postgresWriteFields(fields) {
		column, exists := existingByName[field.Name]
		if exists {
			if !postgresColumnCompatibleWithField(column, field, spatialInfo) {
				return nil, fmt.Errorf("postgresql target column %q has type %q, expected %q", field.Name, postgresColumnNativeType(column), postgresSQLTypeForField(field, spatialInfo))
			}
			continue
		}
		if field.PrimaryKey {
			return nil, fmt.Errorf("postgresql schema evolution cannot add primary key column %q to existing table", field.Name)
		}
		if !postgresMissingColumnCanBeAdded(field) {
			return nil, fmt.Errorf("postgresql schema evolution cannot add non-null column %q without default expression", field.Name)
		}
		statements = append(statements, "ALTER TABLE "+dialect.QualifiedTable(schema, table)+" ADD COLUMN "+postgresColumnDefinition(field, spatialInfo))
	}
	return statements, nil
}

func postgresWriteFields(fields []datatype.FieldInfo) []datatype.FieldInfo {
	result := make([]datatype.FieldInfo, 0, len(fields))
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
		field.Name = name
		result = append(result, field)
	}
	return result
}

func postgresColumnDefinition(field datatype.FieldInfo, spatialInfo *datatype.SpatialInfo) string {
	definition := commonquery.ForDialect("postgresql").QuoteIdentifier(field.Name) + " " + postgresSQLTypeForField(field, spatialInfo)
	if strings.TrimSpace(field.DefaultExpression) != "" {
		definition += " DEFAULT " + strings.TrimSpace(field.DefaultExpression)
	}
	if !field.Nullable {
		definition += " NOT NULL"
	}
	return definition
}

func postgresMissingColumnCanBeAdded(field datatype.FieldInfo) bool {
	return field.Nullable || strings.TrimSpace(field.DefaultExpression) != ""
}

func postgresColumnCompatibleWithField(column postgresColumnInfo, field datatype.FieldInfo, spatialInfo *datatype.SpatialInfo) bool {
	expected := datatype.ParseFieldType(string(field.Type))
	existing := postgresCommonFieldType(column, postgresColumnNativeType(column))
	if expected == datatype.FieldTypeUnknown || expected == datatype.FieldTypeMixed {
		return existing == datatype.FieldTypeString || existing == datatype.FieldTypeUnknown
	}
	if datatype.IsSpatialFieldType(expected) {
		return postgresSpatialColumnCompatibleWithField(column, field, spatialInfo)
	}
	return expected == existing
}

func postgresSpatialColumnCompatibleWithField(column postgresColumnInfo, field datatype.FieldInfo, spatialInfo *datatype.SpatialInfo) bool {
	if !column.IsSpatial() {
		return false
	}
	expectedGeometryType, expectedSRID, expectedDimension := postgresExpectedSpatialFactsForField(field, spatialInfo)
	existingGeometryType, existingSRID, existingDimension := parsePostgresSpatialType(postgresColumnNativeType(column))
	if expectedGeometryType != "" && existingGeometryType != "" && !strings.EqualFold(expectedGeometryType, existingGeometryType) {
		return false
	}
	if expectedSRID > 0 && existingSRID > 0 && expectedSRID != existingSRID {
		return false
	}
	if expectedDimension > 0 && existingDimension > 0 && expectedDimension != existingDimension {
		return false
	}
	return true
}

func postgresExpectedSpatialFactsForField(field datatype.FieldInfo, spatialInfo *datatype.SpatialInfo) (geometryType string, srid int, dimension int) {
	if column := postgresSpatialColumnForField(spatialInfo, field.Name); column != nil {
		geometryType, dimension = normalizePostgresGeometryType(column.GeometryType)
		if geometryType == "" {
			geometryType = strings.TrimSpace(column.GeometryType)
		}
		if column.SRID != nil {
			srid = *column.SRID
		}
		if column.Dimension != nil {
			dimension = *column.Dimension
		}
	}
	if geometryType == "" {
		geometryType = string(datatype.GeometryTypeGeometry)
	}
	return geometryType, srid, dimension
}

func (p *PostgreSQLPlugin) DeleteResource(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath) error {
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

	dropSQL := "DROP TABLE IF EXISTS " + commonquery.ForDialect("postgresql").QualifiedTable(schema, table)
	if _, err := db.ExecContext(ctx, dropSQL); err != nil {
		return fmt.Errorf("drop postgresql table %s.%s: %w", schema, table, err)
	}
	return nil
}

func postgresSQLTypeForField(field datatype.FieldInfo, spatialInfo *datatype.SpatialInfo) string {
	if sqlType := postgresSpatialTypeForField(field, spatialInfo); sqlType != "" {
		return sqlType
	}
	if sqlType, ok := postgresSQLTypeForCommonType(field.Type); ok {
		return sqlType
	}
	return "TEXT"
}

func postgresSpatialTypeForField(field datatype.FieldInfo, spatialInfo *datatype.SpatialInfo) string {
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
		geometryType = string(datatype.GeometryTypeGeometry)
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
	case datatype.FieldTypeString, datatype.FieldTypeMixed, datatype.FieldTypeUnknown:
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
