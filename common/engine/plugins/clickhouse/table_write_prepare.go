package clickhouse

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/sqldialect"
)

func (p *ClickHousePlugin) PrepareTableWrite(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.TableWriteOptions) error {
	database, table, err := clickhouseTablePathParts(path)
	if err != nil {
		return err
	}
	dsn, err := p.serverDSN(connInfo)
	if err != nil {
		return fmt.Errorf("failed to build clickhouse dsn: %w", err)
	}
	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		return fmt.Errorf("failed to open clickhouse connection: %w", err)
	}
	defer db.Close()

	return createClickHouseTableIfNotExists(ctx, db, database, table, opts.Fields)
}

func (p *ClickHousePlugin) DeleteResource(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath) error {
	database, table, err := clickhouseTablePathParts(path)
	if err != nil {
		return err
	}
	dsn, err := p.serverDSN(connInfo)
	if err != nil {
		return fmt.Errorf("failed to build clickhouse dsn: %w", err)
	}
	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		return fmt.Errorf("failed to open clickhouse connection: %w", err)
	}
	defer db.Close()

	dropSQL := "DROP TABLE IF EXISTS " + clickhouseDialect().QualifiedTable(database, table)
	if _, err := db.ExecContext(ctx, dropSQL); err != nil {
		return fmt.Errorf("drop clickhouse table %s.%s: %w", database, table, err)
	}
	return nil
}

func (p *ClickHousePlugin) serverDSN(connInfo plugin.ConnectionInfo) (string, error) {
	parts := plugin.ParseDriverConnInfo(connInfo, p.DefaultPort(), "")
	if err := parts.Require(p.DisplayName(), "host", "user"); err != nil {
		return "", err
	}
	return plugin.ClickHouseStyleDSN(parts.User, parts.Password, parts.Host, parts.Port, "", map[string]string{
		"dial_timeout":       "10s",
		"max_execution_time": "60",
	}), nil
}

func createClickHouseTableIfNotExists(ctx context.Context, db *sql.DB, database, table string, fields []datatype.FieldInfo) error {
	if len(fields) == 0 {
		return fmt.Errorf("clickhouse table write prepare requires table fields")
	}
	dialect := clickhouseDialect()
	if _, err := db.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS "+dialect.QuoteIdentifier(database)); err != nil {
		return fmt.Errorf("create clickhouse database %s: %w", database, err)
	}

	writeFields, err := clickhouseWriteFields(fields)
	if err != nil {
		return err
	}
	if len(writeFields) == 0 {
		return fmt.Errorf("clickhouse table write prepare requires at least one named field")
	}

	definitions := make([]string, 0, len(writeFields))
	for _, field := range writeFields {
		definitions = append(definitions, clickhouseColumnDefinition(field))
	}

	createSQL := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s) ENGINE = MergeTree ORDER BY tuple()", dialect.QualifiedTable(database, table), strings.Join(definitions, ", "))
	if _, err := db.ExecContext(ctx, createSQL); err != nil {
		return fmt.Errorf("create clickhouse table %s.%s: %w", database, table, err)
	}
	return evolveClickHouseTableSchema(ctx, db, database, table, writeFields)
}

func evolveClickHouseTableSchema(ctx context.Context, db *sql.DB, database, table string, fields []datatype.FieldInfo) error {
	columns, err := clickhouseTableColumns(ctx, db, database, table)
	if err != nil {
		return err
	}
	statements, err := clickhouseSchemaEvolutionStatements(database, table, fields, columns)
	if err != nil {
		return err
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("evolve clickhouse table %s.%s schema: %w", database, table, err)
		}
	}
	return nil
}

type clickhouseWriteColumnInfo struct {
	Name       string
	NativeType string
}

func clickhouseTableColumns(ctx context.Context, db *sql.DB, database, table string) ([]clickhouseWriteColumnInfo, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT name, type
		FROM system.columns
		WHERE database = ? AND table = ?
		ORDER BY position
	`, database, table)
	if err != nil {
		return nil, fmt.Errorf("query clickhouse table columns: %w", err)
	}
	defer rows.Close()

	columns := make([]clickhouseWriteColumnInfo, 0)
	for rows.Next() {
		var column clickhouseWriteColumnInfo
		if err := rows.Scan(&column.Name, &column.NativeType); err != nil {
			return nil, fmt.Errorf("scan clickhouse table column: %w", err)
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate clickhouse table columns: %w", err)
	}
	return columns, nil
}

func clickhouseSchemaEvolutionStatements(database, table string, fields []datatype.FieldInfo, existingColumns []clickhouseWriteColumnInfo) ([]string, error) {
	dialect := clickhouseDialect()
	existingByName := make(map[string]clickhouseWriteColumnInfo, len(existingColumns))
	for _, column := range existingColumns {
		existingByName[column.Name] = column
	}

	statements := make([]string, 0)
	for _, field := range fields {
		column, exists := existingByName[field.Name]
		if exists {
			if !clickhouseColumnCompatibleWithField(column, field) {
				return nil, fmt.Errorf("clickhouse target column %q has type %q, expected %q", field.Name, column.NativeType, clickhouseSQLTypeForField(field))
			}
			continue
		}
		if field.PrimaryKey {
			return nil, fmt.Errorf("clickhouse schema evolution cannot add primary key column %q to existing table", field.Name)
		}
		if !clickhouseMissingColumnCanBeAdded(field) {
			return nil, fmt.Errorf("clickhouse schema evolution cannot add non-null column %q without default expression", field.Name)
		}
		statements = append(statements, "ALTER TABLE "+dialect.QualifiedTable(database, table)+" ADD COLUMN "+clickhouseColumnDefinition(field))
	}
	return statements, nil
}

func clickhouseWriteFields(fields []datatype.FieldInfo) ([]datatype.FieldInfo, error) {
	result := make([]datatype.FieldInfo, 0, len(fields))
	seen := map[string]struct{}{}
	for _, field := range fields {
		name := strings.TrimSpace(field.Name)
		if name == "" || field.Generated {
			continue
		}
		if datatype.IsSpatialFieldType(field.Type) {
			return nil, fmt.Errorf("clickhouse table write does not support spatial field %q yet", name)
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		field.Name = name
		result = append(result, field)
	}
	return result, nil
}

func clickhouseColumnDefinition(field datatype.FieldInfo) string {
	definition := clickhouseDialect().QuoteIdentifier(field.Name) + " " + clickhouseSQLTypeForField(field)
	if strings.TrimSpace(field.DefaultExpression) != "" {
		definition += " DEFAULT " + strings.TrimSpace(field.DefaultExpression)
	}
	return definition
}

func clickhouseMissingColumnCanBeAdded(field datatype.FieldInfo) bool {
	return field.Nullable || strings.TrimSpace(field.DefaultExpression) != ""
}

func clickhouseColumnCompatibleWithField(column clickhouseWriteColumnInfo, field datatype.FieldInfo) bool {
	expected := datatype.ParseFieldType(string(field.Type))
	existing := clickhouseCommonFieldType(column.NativeType)
	if expected == datatype.FieldTypeUnknown {
		return existing == datatype.FieldTypeString || existing == datatype.FieldTypeUnknown
	}
	return expected == existing
}

func clickhouseSQLTypeForField(field datatype.FieldInfo) string {
	baseType := clickhouseBaseSQLTypeForField(field)
	if field.Nullable {
		return "Nullable(" + baseType + ")"
	}
	return baseType
}

func clickhouseBaseSQLTypeForField(field datatype.FieldInfo) string {
	switch datatype.ParseFieldType(string(field.Type)) {
	case datatype.FieldTypeString:
		return "String"
	case datatype.FieldTypeInt:
		return "Int32"
	case datatype.FieldTypeBigInt:
		return "Int64"
	case datatype.FieldTypeFloat:
		return "Float32"
	case datatype.FieldTypeDouble:
		return "Float64"
	case datatype.FieldTypeDecimal:
		return "Decimal(38,10)"
	case datatype.FieldTypeBool:
		return "Bool"
	case datatype.FieldTypeDate:
		return "Date"
	case datatype.FieldTypeTime:
		return "String"
	case datatype.FieldTypeTimestamp:
		return "DateTime"
	case datatype.FieldTypeBytes:
		return "String"
	case datatype.FieldTypeJSON, datatype.FieldTypeArray, datatype.FieldTypeMixed:
		return "String"
	case datatype.FieldTypeUUID:
		return "UUID"
	default:
		return "String"
	}
}

func clickhouseCommonFieldType(nativeType string) datatype.FieldType {
	value := clickhouseNormalizeTypeName(nativeType)
	switch value {
	case "String", "FixedString":
		return datatype.FieldTypeString
	case "Int8", "Int16", "Int32", "UInt8", "UInt16", "UInt32":
		return datatype.FieldTypeInt
	case "Int64", "UInt64", "Int128", "UInt128", "Int256", "UInt256":
		return datatype.FieldTypeBigInt
	case "Float32":
		return datatype.FieldTypeFloat
	case "Float64":
		return datatype.FieldTypeDouble
	case "Decimal", "Decimal32", "Decimal64", "Decimal128", "Decimal256":
		return datatype.FieldTypeDecimal
	case "Bool":
		return datatype.FieldTypeBool
	case "Date", "Date32":
		return datatype.FieldTypeDate
	case "DateTime", "DateTime64":
		return datatype.FieldTypeTimestamp
	case "UUID":
		return datatype.FieldTypeUUID
	case "JSON":
		return datatype.FieldTypeJSON
	default:
		return datatype.FieldTypeUnknown
	}
}

func clickhouseNormalizeTypeName(nativeType string) string {
	value := strings.TrimSpace(nativeType)
	for {
		switch {
		case strings.HasPrefix(value, "Nullable(") && strings.HasSuffix(value, ")"):
			value = strings.TrimSuffix(strings.TrimPrefix(value, "Nullable("), ")")
		case strings.HasPrefix(value, "LowCardinality(") && strings.HasSuffix(value, ")"):
			value = strings.TrimSuffix(strings.TrimPrefix(value, "LowCardinality("), ")")
		default:
			if idx := strings.Index(value, "("); idx > 0 {
				return value[:idx]
			}
			return value
		}
	}
}

func clickhouseTablePathParts(path plugin.CatalogPath) (string, string, error) {
	if len(path.Segments) < 2 {
		return "", "", fmt.Errorf("clickhouse table write requires database/table catalog path")
	}
	database := strings.TrimSpace(path.Segments[len(path.Segments)-2].Name)
	table := strings.TrimSpace(path.Segments[len(path.Segments)-1].Name)
	if database == "" || table == "" {
		return "", "", fmt.Errorf("clickhouse table write requires non-empty database and table")
	}
	return database, table, nil
}

func clickhouseDialect() sqldialect.Dialect {
	return sqldialect.ForEngine("clickhouse")
}
