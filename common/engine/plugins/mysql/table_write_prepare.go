package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/mappers/mysql"
	"github.com/addp/common/sqldialect"
)

func (p *MySQLPlugin) PrepareTableWrite(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.TableWriteOptions) error {
	database, table, err := mysqlTablePathParts(path)
	if err != nil {
		return err
	}
	dsn, err := p.serverDSN(connInfo)
	if err != nil {
		return fmt.Errorf("failed to build mysql dsn: %w", err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("failed to open mysql connection: %w", err)
	}
	defer db.Close()

	return createMySQLTableIfNotExists(ctx, db, database, table, opts.Fields, opts.SpatialInfo)
}

func (p *MySQLPlugin) DeleteResource(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath) error {
	database, table, err := mysqlTablePathParts(path)
	if err != nil {
		return err
	}
	dsn, err := p.serverDSN(connInfo)
	if err != nil {
		return fmt.Errorf("failed to build mysql dsn: %w", err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("failed to open mysql connection: %w", err)
	}
	defer db.Close()

	dropSQL := "DROP TABLE IF EXISTS " + mysqlDialect().QualifiedTable(database, table)
	if _, err := db.ExecContext(ctx, dropSQL); err != nil {
		return fmt.Errorf("drop mysql table %s.%s: %w", database, table, err)
	}
	return nil
}

func (p *MySQLPlugin) serverDSN(connInfo plugin.ConnectionInfo) (string, error) {
	parts := plugin.ParseDriverConnInfo(connInfo, p.DefaultPort(), "")
	if err := parts.Require(p.DisplayName(), "host", "user"); err != nil {
		return "", err
	}
	return plugin.MySQLStyleDSN(parts.User, parts.Password, parts.Host, parts.Port, "", map[string]string{
		"parseTime": "true",
		"timeout":   "10s",
		"charset":   "utf8mb4",
		"collation": "utf8mb4_unicode_ci",
	}), nil
}

func createMySQLTableIfNotExists(ctx context.Context, db *sql.DB, database, table string, fields []datatype.FieldInfo, spatialInfo *datatype.SpatialInfo) error {
	if len(fields) == 0 {
		return fmt.Errorf("mysql table write prepare requires table fields")
	}
	dialect := mysqlDialect()
	if _, err := db.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS "+dialect.QuoteIdentifier(database)+" CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		return fmt.Errorf("create mysql database %s: %w", database, err)
	}

	writeFields := mysqlWriteFields(fields)
	definitions := make([]string, 0, len(writeFields)+1)
	primaryKeys := make([]string, 0)
	for _, field := range writeFields {
		definition, err := mysqlColumnDefinition(field, spatialInfo)
		if err != nil {
			return err
		}
		definitions = append(definitions, definition)
		if field.PrimaryKey {
			primaryKeys = append(primaryKeys, dialect.QuoteIdentifier(field.Name))
		}
	}
	if len(definitions) == 0 {
		return fmt.Errorf("mysql table write prepare requires at least one named field")
	}
	if len(primaryKeys) > 0 {
		definitions = append(definitions, "PRIMARY KEY ("+strings.Join(primaryKeys, ", ")+")")
	}

	createSQL := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci", dialect.QualifiedTable(database, table), strings.Join(definitions, ", "))
	if _, err := db.ExecContext(ctx, createSQL); err != nil {
		return fmt.Errorf("create mysql table %s.%s: %w", database, table, err)
	}
	return evolveMySQLTableSchema(ctx, db, database, table, writeFields, spatialInfo)
}

func evolveMySQLTableSchema(ctx context.Context, db *sql.DB, database, table string, fields []datatype.FieldInfo, spatialInfo *datatype.SpatialInfo) error {
	columns, err := mysqlTableColumns(ctx, db, database, table)
	if err != nil {
		return err
	}
	statements, err := mysqlSchemaEvolutionStatements(database, table, fields, spatialInfo, columns)
	if err != nil {
		return err
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("evolve mysql table %s.%s schema: %w", database, table, err)
		}
	}
	return nil
}

type mysqlColumnInfo struct {
	Name              string
	DataType          string
	NativeType        string
	NumericPrecision  sql.NullInt64
	NumericScale      sql.NullInt64
	TemporalPrecision sql.NullInt64
	Nullable          bool
}

func mysqlTableColumns(ctx context.Context, db *sql.DB, database, table string) ([]mysqlColumnInfo, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT column_name, data_type, column_type, numeric_precision, numeric_scale, datetime_precision,
		       (is_nullable = 'YES') AS is_nullable
		FROM information_schema.columns
		WHERE table_schema = ? AND table_name = ?
		ORDER BY ordinal_position
	`, database, table)
	if err != nil {
		return nil, fmt.Errorf("query mysql table columns: %w", err)
	}
	defer rows.Close()

	columns := make([]mysqlColumnInfo, 0)
	for rows.Next() {
		var column mysqlColumnInfo
		if err := rows.Scan(
			&column.Name, &column.DataType, &column.NativeType, &column.NumericPrecision, &column.NumericScale,
			&column.TemporalPrecision, &column.Nullable,
		); err != nil {
			return nil, fmt.Errorf("scan mysql table column: %w", err)
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mysql table columns: %w", err)
	}
	return columns, nil
}

func mysqlSchemaEvolutionStatements(database, table string, fields []datatype.FieldInfo, spatialInfo *datatype.SpatialInfo, existingColumns []mysqlColumnInfo) ([]string, error) {
	dialect := mysqlDialect()
	existingByName := make(map[string]mysqlColumnInfo, len(existingColumns))
	for _, column := range existingColumns {
		existingByName[column.Name] = column
	}

	statements := make([]string, 0)
	for _, field := range mysqlWriteFields(fields) {
		expectedType, err := mysqlSQLTypeForField(field, spatialInfo)
		if err != nil {
			return nil, err
		}
		column, exists := existingByName[field.Name]
		if exists {
			if !mysqlColumnCompatibleWithField(column, field, spatialInfo) {
				return nil, fmt.Errorf("mysql target column %q has type %q, expected %q", field.Name, mysqlColumnNativeType(column), expectedType)
			}
			continue
		}
		if field.PrimaryKey {
			return nil, fmt.Errorf("mysql schema evolution cannot add primary key column %q to existing table", field.Name)
		}
		if !mysqlMissingColumnCanBeAdded(field) {
			return nil, fmt.Errorf("mysql schema evolution cannot add non-null column %q without default expression", field.Name)
		}
		definition, err := mysqlColumnDefinition(field, spatialInfo)
		if err != nil {
			return nil, err
		}
		statements = append(statements, "ALTER TABLE "+dialect.QualifiedTable(database, table)+" ADD COLUMN "+definition)
	}
	return statements, nil
}

func mysqlWriteFields(fields []datatype.FieldInfo) []datatype.FieldInfo {
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

func mysqlColumnDefinition(field datatype.FieldInfo, spatialInfo *datatype.SpatialInfo) (string, error) {
	sqlType, err := mysqlSQLTypeForField(field, spatialInfo)
	if err != nil {
		return "", err
	}
	definition := mysqlDialect().QuoteIdentifier(field.Name) + " " + sqlType
	if strings.TrimSpace(field.DefaultExpression) != "" {
		definition += " DEFAULT " + strings.TrimSpace(field.DefaultExpression)
	}
	if !field.Nullable {
		definition += " NOT NULL"
	}
	return definition, nil
}

func mysqlMissingColumnCanBeAdded(field datatype.FieldInfo) bool {
	return field.Nullable || strings.TrimSpace(field.DefaultExpression) != ""
}

func mysqlColumnCompatibleWithField(column mysqlColumnInfo, field datatype.FieldInfo, spatialInfo *datatype.SpatialInfo) bool {
	expected := datatype.ParseFieldType(string(field.Type))
	existing := mysqlCommonFieldType(column)
	if expected == datatype.FieldTypeUnknown {
		return existing == datatype.FieldTypeString || existing == datatype.FieldTypeUnknown
	}
	if datatype.IsSpatialFieldType(expected) {
		return mysqlSpatialColumnCompatibleWithField(column, field, spatialInfo)
	}
	if expected == datatype.FieldTypeDecimal && existing == datatype.FieldTypeDecimal {
		return column.NumericPrecision.Valid &&
			column.NumericScale.Valid &&
			int(column.NumericPrecision.Int64) == field.Precision &&
			int(column.NumericScale.Int64) == field.Scale
	}
	if (expected == datatype.FieldTypeTime || expected == datatype.FieldTypeTimestamp) && existing == expected {
		return column.TemporalPrecision.Valid && column.TemporalPrecision.Int64 == 6
	}
	if expected == datatype.FieldTypeUUID {
		return strings.EqualFold(strings.TrimSpace(column.NativeType), "varchar(36)")
	}
	return expected == existing
}

func mysqlSpatialColumnCompatibleWithField(column mysqlColumnInfo, field datatype.FieldInfo, spatialInfo *datatype.SpatialInfo) bool {
	existing := mysqlCommonFieldType(column)
	if !datatype.IsSpatialFieldType(existing) {
		return false
	}
	expectedType := datatype.ParseFieldType(string(field.Type))
	if column := mysqlSpatialColumnForField(spatialInfo, field.Name); column != nil {
		expectedType = mysqlFieldTypeForGeometryType(column.GeometryType)
	}
	if expectedType == datatype.FieldTypeGeometry || existing == datatype.FieldTypeGeometry {
		return true
	}
	return expectedType == existing
}

func mysqlSQLTypeForField(field datatype.FieldInfo, spatialInfo *datatype.SpatialInfo) (string, error) {
	if sqlType := mysqlSpatialTypeForField(field, spatialInfo); sqlType != "" {
		return sqlType, nil
	}
	fieldType := datatype.ParseFieldType(string(field.Type))
	if fieldType == datatype.FieldTypeTime {
		return "TIME(6)", nil
	}
	if fieldType == datatype.FieldTypeTimestamp {
		return "DATETIME(6)", nil
	}
	if mapper := format.GetTypeMapper("mysql"); mapper != nil {
		nativeType, size, precision := mapper.FromCommon(fieldType)
		if fieldType == datatype.FieldTypeDecimal {
			if err := validateMySQLDecimalField(field); err != nil {
				return "", err
			}
			size = field.Precision
			precision = field.Scale
		}
		return mysqlNativeTypeWithSize(nativeType, size, precision), nil
	}
	return "TEXT", nil
}

func validateMySQLDecimalField(field datatype.FieldInfo) error {
	if field.Precision <= 0 {
		return fmt.Errorf("mysql decimal field %q requires explicit precision and scale", field.Name)
	}
	if field.Precision > 65 {
		return fmt.Errorf("mysql decimal field %q precision %d exceeds maximum 65", field.Name, field.Precision)
	}
	if field.Scale < 0 || field.Scale > 30 {
		return fmt.Errorf("mysql decimal field %q scale %d must be between 0 and 30", field.Name, field.Scale)
	}
	if field.Scale > field.Precision {
		return fmt.Errorf("mysql decimal field %q scale %d exceeds precision %d", field.Name, field.Scale, field.Precision)
	}
	return nil
}

func mysqlSpatialTypeForField(field datatype.FieldInfo, spatialInfo *datatype.SpatialInfo) string {
	if !datatype.IsSpatialFieldType(field.Type) {
		return ""
	}
	if column := mysqlSpatialColumnForField(spatialInfo, field.Name); column != nil {
		if sqlType := mysqlSQLTypeForGeometryType(column.GeometryType); sqlType != "" {
			return sqlType
		}
	}
	return "GEOMETRY"
}

func mysqlSpatialColumnForField(spatialInfo *datatype.SpatialInfo, fieldName string) *datatype.GeometryColumnInfo {
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

func mysqlSQLTypeForGeometryType(geometryType string) string {
	switch datatype.ParseGeometryType(geometryType) {
	case datatype.GeometryTypePoint:
		return "POINT"
	case datatype.GeometryTypeLineString:
		return "LINESTRING"
	case datatype.GeometryTypePolygon:
		return "POLYGON"
	case datatype.GeometryTypeMultiPoint:
		return "MULTIPOINT"
	case datatype.GeometryTypeMultiLineString:
		return "MULTILINESTRING"
	case datatype.GeometryTypeMultiPolygon:
		return "MULTIPOLYGON"
	case datatype.GeometryTypeGeometryCollection:
		return "GEOMETRYCOLLECTION"
	case datatype.GeometryTypeGeometry:
		return "GEOMETRY"
	default:
		return "GEOMETRY"
	}
}

func mysqlFieldTypeForGeometryType(geometryType string) datatype.FieldType {
	return datatype.FieldTypeGeometry
}

func mysqlNativeTypeWithSize(nativeType string, size, precision int) string {
	nativeType = strings.ToUpper(strings.TrimSpace(nativeType))
	if nativeType == "" {
		return "TEXT"
	}
	switch nativeType {
	case "VARCHAR", "CHAR", "VARBINARY", "BINARY":
		if size > 0 {
			return fmt.Sprintf("%s(%d)", nativeType, size)
		}
	case "DECIMAL", "NUMERIC":
		if size > 0 && precision > 0 {
			return fmt.Sprintf("%s(%d,%d)", nativeType, size, precision)
		}
		if size > 0 {
			return fmt.Sprintf("%s(%d)", nativeType, size)
		}
	case "TINYINT":
		if size > 0 {
			return fmt.Sprintf("%s(%d)", nativeType, size)
		}
	}
	return nativeType
}

func mysqlColumnNativeType(column mysqlColumnInfo) string {
	if nativeType := strings.TrimSpace(column.NativeType); nativeType != "" {
		return nativeType
	}
	return strings.TrimSpace(column.DataType)
}

func mysqlCommonFieldType(column mysqlColumnInfo) datatype.FieldType {
	if strings.EqualFold(strings.TrimSpace(column.NativeType), "tinyint(1)") {
		return datatype.FieldTypeBool
	}
	if mapper := format.GetTypeMapper("mysql"); mapper != nil {
		if fieldType := mapper.ToCommon(mysqlColumnNativeType(column)); fieldType != "" && fieldType != datatype.FieldTypeUnknown {
			return fieldType
		}
		if fieldType := mapper.ToCommon(column.DataType); fieldType != "" && fieldType != datatype.FieldTypeUnknown {
			return fieldType
		}
	}
	return datatype.FieldTypeUnknown
}

func mysqlTablePathParts(path plugin.CatalogPath) (string, string, error) {
	if len(path.Segments) < 2 {
		return "", "", fmt.Errorf("mysql table path requires database/table catalog path")
	}
	database := strings.TrimSpace(path.Segments[len(path.Segments)-2].Name)
	table := strings.TrimSpace(path.Segments[len(path.Segments)-1].Name)
	if database == "" || table == "" {
		return "", "", fmt.Errorf("mysql table path requires non-empty database and table")
	}
	return database, table, nil
}

func mysqlDialect() sqldialect.Dialect {
	return sqldialect.ForEngine("mysql")
}
