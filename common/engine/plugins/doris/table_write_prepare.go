package doris

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/sqldialect"
)

func (p *DorisPlugin) PrepareTableWrite(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.TableWriteOptions) error {
	database, table, err := dorisTablePathParts(path)
	if err != nil {
		return err
	}
	dsn, err := p.serverDSN(connInfo)
	if err != nil {
		return fmt.Errorf("failed to build doris dsn: %w", err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("failed to open doris connection: %w", err)
	}
	defer db.Close()

	return createDorisTableIfNotExists(ctx, db, database, table, opts.Fields)
}

func (p *DorisPlugin) DeleteResource(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath) error {
	database, table, err := dorisTablePathParts(path)
	if err != nil {
		return err
	}
	dsn, err := p.serverDSN(connInfo)
	if err != nil {
		return fmt.Errorf("failed to build doris dsn: %w", err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("failed to open doris connection: %w", err)
	}
	defer db.Close()

	dropSQL := "DROP TABLE IF EXISTS " + dorisDialect().QualifiedTable(database, table)
	if _, err := db.ExecContext(ctx, dropSQL); err != nil {
		return fmt.Errorf("drop doris table %s.%s: %w", database, table, err)
	}
	return nil
}

func (p *DorisPlugin) serverDSN(connInfo plugin.ConnectionInfo) (string, error) {
	parts := plugin.ParseDriverConnInfo(connInfo, p.DefaultPort(), "")
	if err := parts.Require(p.DisplayName(), "host", "user"); err != nil {
		return "", err
	}
	return plugin.MySQLStyleDSN(parts.User, parts.Password, parts.Host, parts.Port, "", map[string]string{
		"parseTime": "true",
		"timeout":   "10s",
	}), nil
}

func createDorisTableIfNotExists(ctx context.Context, db *sql.DB, database, table string, fields []datatype.FieldInfo) error {
	if len(fields) == 0 {
		return fmt.Errorf("doris table write prepare requires table fields")
	}
	dialect := dorisDialect()
	if _, err := db.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS "+dialect.QuoteIdentifier(database)); err != nil {
		return fmt.Errorf("create doris database %s: %w", database, err)
	}

	writeFields, err := dorisWriteFields(fields)
	if err != nil {
		return err
	}
	if len(writeFields) == 0 {
		return fmt.Errorf("doris table write prepare requires at least one named field")
	}
	keyField, err := dorisDuplicateKeyField(writeFields)
	if err != nil {
		return err
	}
	writeFields = dorisFieldsWithKeyFirst(writeFields, keyField.Name)

	definitions := make([]string, 0, len(writeFields))
	for _, field := range writeFields {
		definitions = append(definitions, dorisColumnDefinition(field))
	}

	createSQL := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s (%s) DUPLICATE KEY(%s) DISTRIBUTED BY HASH(%s) BUCKETS 10",
		dialect.QualifiedTable(database, table),
		strings.Join(definitions, ", "),
		dialect.QuoteIdentifier(keyField.Name),
		dialect.QuoteIdentifier(keyField.Name),
	)
	if _, err := db.ExecContext(ctx, createSQL); err != nil {
		return fmt.Errorf("create doris table %s.%s: %w", database, table, err)
	}
	return evolveDorisTableSchema(ctx, db, database, table, writeFields)
}

func evolveDorisTableSchema(ctx context.Context, db *sql.DB, database, table string, fields []datatype.FieldInfo) error {
	columns, err := dorisTableColumns(ctx, db, database, table)
	if err != nil {
		return err
	}
	statements, err := dorisSchemaEvolutionStatements(database, table, fields, columns)
	if err != nil {
		return err
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("evolve doris table %s.%s schema: %w", database, table, err)
		}
	}
	return nil
}

type dorisColumnInfo struct {
	Name       string
	DataType   string
	NativeType string
}

func dorisTableColumns(ctx context.Context, db *sql.DB, database, table string) ([]dorisColumnInfo, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT column_name, data_type, column_type
		FROM information_schema.columns
		WHERE table_schema = ? AND table_name = ?
		ORDER BY ordinal_position
	`, database, table)
	if err != nil {
		return nil, fmt.Errorf("query doris table columns: %w", err)
	}
	defer rows.Close()

	columns := make([]dorisColumnInfo, 0)
	for rows.Next() {
		var column dorisColumnInfo
		if err := rows.Scan(&column.Name, &column.DataType, &column.NativeType); err != nil {
			return nil, fmt.Errorf("scan doris table column: %w", err)
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate doris table columns: %w", err)
	}
	return columns, nil
}

func dorisSchemaEvolutionStatements(database, table string, fields []datatype.FieldInfo, existingColumns []dorisColumnInfo) ([]string, error) {
	dialect := dorisDialect()
	existingByName := make(map[string]dorisColumnInfo, len(existingColumns))
	for _, column := range existingColumns {
		existingByName[column.Name] = column
	}

	statements := make([]string, 0)
	for _, field := range fields {
		column, exists := existingByName[field.Name]
		if exists {
			if !dorisColumnCompatibleWithField(column, field) {
				return nil, fmt.Errorf("doris target column %q has type %q, expected %q", field.Name, dorisColumnNativeType(column), dorisSQLTypeForField(field))
			}
			continue
		}
		if field.PrimaryKey {
			return nil, fmt.Errorf("doris schema evolution cannot add primary key column %q to existing table", field.Name)
		}
		if !dorisMissingColumnCanBeAdded(field) {
			return nil, fmt.Errorf("doris schema evolution cannot add non-null column %q without default expression", field.Name)
		}
		statements = append(statements, "ALTER TABLE "+dialect.QualifiedTable(database, table)+" ADD COLUMN "+dorisColumnDefinition(field))
	}
	return statements, nil
}

func dorisWriteFields(fields []datatype.FieldInfo) ([]datatype.FieldInfo, error) {
	result := make([]datatype.FieldInfo, 0, len(fields))
	seen := map[string]struct{}{}
	for _, field := range fields {
		name := strings.TrimSpace(field.Name)
		if name == "" {
			continue
		}
		if datatype.IsSpatialFieldType(field.Type) {
			return nil, fmt.Errorf("doris table write does not support spatial field %q yet", name)
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

func dorisDuplicateKeyField(fields []datatype.FieldInfo) (datatype.FieldInfo, error) {
	for _, field := range fields {
		if dorisFieldCanBeDuplicateKey(field) {
			return field, nil
		}
	}
	return datatype.FieldInfo{}, fmt.Errorf("doris table write requires at least one keyable field for DUPLICATE KEY")
}

func dorisFieldCanBeDuplicateKey(field datatype.FieldInfo) bool {
	switch datatype.ParseFieldType(string(field.Type)) {
	case datatype.FieldTypeString, datatype.FieldTypeBool, datatype.FieldTypeInt, datatype.FieldTypeBigInt,
		datatype.FieldTypeFloat, datatype.FieldTypeDouble, datatype.FieldTypeDecimal,
		datatype.FieldTypeDate, datatype.FieldTypeTimestamp, datatype.FieldTypeUUID:
		return true
	default:
		return false
	}
}

func dorisFieldsWithKeyFirst(fields []datatype.FieldInfo, keyName string) []datatype.FieldInfo {
	if len(fields) == 0 || keyName == "" {
		return fields
	}
	result := make([]datatype.FieldInfo, 0, len(fields))
	for _, field := range fields {
		if field.Name == keyName {
			result = append(result, field)
			break
		}
	}
	for _, field := range fields {
		if field.Name != keyName {
			result = append(result, field)
		}
	}
	return result
}

func dorisColumnDefinition(field datatype.FieldInfo) string {
	definition := dorisDialect().QuoteIdentifier(field.Name) + " " + dorisSQLTypeForField(field)
	if strings.TrimSpace(field.DefaultExpression) != "" {
		definition += " DEFAULT " + strings.TrimSpace(field.DefaultExpression)
	}
	if !field.Nullable {
		definition += " NOT NULL"
	}
	return definition
}

func dorisMissingColumnCanBeAdded(field datatype.FieldInfo) bool {
	return field.Nullable || strings.TrimSpace(field.DefaultExpression) != ""
}

func dorisColumnCompatibleWithField(column dorisColumnInfo, field datatype.FieldInfo) bool {
	expected := datatype.ParseFieldType(string(field.Type))
	existing := dorisCommonFieldType(column)
	if expected == datatype.FieldTypeUnknown {
		return existing == datatype.FieldTypeString || existing == datatype.FieldTypeUnknown
	}
	return expected == existing
}

func dorisSQLTypeForField(field datatype.FieldInfo) string {
	switch datatype.ParseFieldType(string(field.Type)) {
	case datatype.FieldTypeString:
		return "VARCHAR(65533)"
	case datatype.FieldTypeInt:
		return "INT"
	case datatype.FieldTypeBigInt:
		return "BIGINT"
	case datatype.FieldTypeFloat:
		return "FLOAT"
	case datatype.FieldTypeDouble:
		return "DOUBLE"
	case datatype.FieldTypeDecimal:
		return "DECIMAL(38,10)"
	case datatype.FieldTypeBool:
		return "BOOLEAN"
	case datatype.FieldTypeDate:
		return "DATE"
	case datatype.FieldTypeTime:
		return "STRING"
	case datatype.FieldTypeTimestamp:
		return "DATETIME"
	case datatype.FieldTypeBytes:
		return "STRING"
	case datatype.FieldTypeJSON, datatype.FieldTypeArray, datatype.FieldTypeMixed:
		return "STRING"
	case datatype.FieldTypeUUID:
		return "VARCHAR(36)"
	default:
		return "STRING"
	}
}

func dorisColumnNativeType(column dorisColumnInfo) string {
	if nativeType := strings.TrimSpace(column.NativeType); nativeType != "" {
		return nativeType
	}
	return strings.TrimSpace(column.DataType)
}

func dorisCommonFieldType(column dorisColumnInfo) datatype.FieldType {
	nativeType := strings.ToLower(strings.TrimSpace(dorisColumnNativeType(column)))
	if idx := strings.Index(nativeType, "("); idx > 0 {
		nativeType = nativeType[:idx]
	}
	switch nativeType {
	case "varchar", "char", "string", "text":
		return datatype.FieldTypeString
	case "tinyint", "smallint", "int", "integer":
		return datatype.FieldTypeInt
	case "bigint", "largeint":
		return datatype.FieldTypeBigInt
	case "float":
		return datatype.FieldTypeFloat
	case "double":
		return datatype.FieldTypeDouble
	case "decimal", "decimalv3":
		return datatype.FieldTypeDecimal
	case "boolean", "bool":
		return datatype.FieldTypeBool
	case "date", "datev2":
		return datatype.FieldTypeDate
	case "datetime", "datetimev2":
		return datatype.FieldTypeTimestamp
	case "json", "variant":
		return datatype.FieldTypeJSON
	default:
		return datatype.FieldTypeUnknown
	}
}

func dorisTablePathParts(path plugin.CatalogPath) (string, string, error) {
	if len(path.Segments) < 2 {
		return "", "", fmt.Errorf("doris table write requires database/table catalog path")
	}
	database := strings.TrimSpace(path.Segments[len(path.Segments)-2].Name)
	table := strings.TrimSpace(path.Segments[len(path.Segments)-1].Name)
	if database == "" || table == "" {
		return "", "", fmt.Errorf("doris table write requires non-empty database and table")
	}
	return database, table, nil
}

func dorisDialect() sqldialect.Dialect {
	return sqldialect.ForEngine("doris")
}
