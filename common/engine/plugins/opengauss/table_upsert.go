package opengauss

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	commonquery "github.com/addp/common/query"
)

func (p *Plugin) PrepareTableUpsert(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableUpsertOptions) error {
	if err := validateNonSpatialWrite(opts.Fields, opts.SpatialInfo); err != nil {
		return err
	}
	if _, err := validateOpenGaussUpsertOptions(opts); err != nil {
		return err
	}
	// Target creation and unique-key inspection are certified PostgreSQL-wire
	// protocol operations. Row application is openGauss-native MERGE below.
	return p.protocol().PrepareTableUpsert(ctx, connInfo, path, opts)
}

// UpsertBatch applies the entire batch in one transaction. openGauss uses its
// native MERGE statement rather than PostgreSQL's ON CONFLICT syntax.
func (p *Plugin) UpsertBatch(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, batch *plugin.BatchData, opts plugin.TableUpsertOptions) error {
	if err := validateNonSpatialWrite(opts.Fields, opts.SpatialInfo); err != nil {
		return err
	}
	if batch != nil {
		if err := validateNonSpatialWrite(batch.Fields, batch.Spatial); err != nil {
			return err
		}
	}
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}
	keys, err := validateOpenGaussUpsertOptions(opts)
	if err != nil {
		return err
	}
	schema, table, err := openGaussTablePathParts(path)
	if err != nil {
		return err
	}
	columns := openGaussBatchColumns(batch)
	if len(columns) == 0 {
		return fmt.Errorf("openGauss upsert requires batch columns")
	}
	fields, err := openGaussUpsertFields(columns, batch.Fields, opts.Fields)
	if err != nil {
		return err
	}
	statement, err := buildOpenGaussMergeSQL(schema, table, fields, keys)
	if err != nil {
		return err
	}
	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		return err
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("open openGauss upsert connection: %w", err)
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin openGauss upsert transaction: %w", err)
	}
	defer tx.Rollback()
	for index, row := range batch.Rows {
		args, err := openGaussUpsertRowArgs(row, columns)
		if err != nil {
			return fmt.Errorf("openGauss upsert row %d: %w", index, err)
		}
		if _, err := tx.ExecContext(ctx, statement, args...); err != nil {
			return fmt.Errorf("execute openGauss MERGE row %d: %w", index, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit openGauss upsert: %w", err)
	}
	return nil
}

func validateOpenGaussUpsertOptions(opts plugin.TableUpsertOptions) ([]string, error) {
	fieldSet := make(map[string]bool, len(opts.Fields))
	for _, field := range opts.Fields {
		if name := strings.TrimSpace(field.Name); name != "" {
			fieldSet[name] = true
		}
	}
	keys := make([]string, 0, len(opts.Keys))
	seen := make(map[string]bool, len(opts.Keys))
	for _, rawKey := range opts.Keys {
		key := strings.TrimSpace(rawKey)
		if key == "" || seen[key] {
			return nil, fmt.Errorf("openGauss upsert keys must be non-empty and unique")
		}
		if !fieldSet[key] {
			return nil, fmt.Errorf("openGauss upsert key %q is not present in table fields", key)
		}
		seen[key] = true
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("openGauss upsert requires keys")
	}
	return keys, nil
}

func openGaussTablePathParts(path plugin.EngineCatalogPath) (string, string, error) {
	if len(path.Segments) < 2 {
		return "", "", fmt.Errorf("openGauss upsert requires schema/table catalog path")
	}
	schema := strings.TrimSpace(path.Segments[len(path.Segments)-2].Name)
	table := strings.TrimSpace(path.Segments[len(path.Segments)-1].Name)
	if schema == "" || table == "" {
		return "", "", fmt.Errorf("openGauss upsert requires non-empty schema and table")
	}
	return schema, table, nil
}

func openGaussBatchColumns(batch *plugin.BatchData) []string {
	if batch == nil {
		return nil
	}
	seen := map[string]bool{}
	columns := make([]string, 0, len(batch.Fields))
	for _, field := range batch.Fields {
		name := strings.TrimSpace(field.Name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		columns = append(columns, name)
	}
	if len(columns) > 0 {
		return columns
	}
	for _, row := range batch.Rows {
		for rawName := range row {
			name := strings.TrimSpace(rawName)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			columns = append(columns, name)
		}
	}
	sort.Strings(columns)
	return columns
}

func openGaussUpsertFields(columns []string, fieldSources ...[]datatype.FieldInfo) ([]datatype.FieldInfo, error) {
	byName := map[string]datatype.FieldInfo{}
	for _, fields := range fieldSources {
		for _, field := range fields {
			name := strings.TrimSpace(field.Name)
			if name != "" {
				byName[name] = field
			}
		}
	}
	result := make([]datatype.FieldInfo, 0, len(columns))
	for _, column := range columns {
		field, ok := byName[column]
		if !ok {
			return nil, fmt.Errorf("openGauss upsert batch column %q is not present in table fields", column)
		}
		field.Name = column
		result = append(result, field)
	}
	return result, nil
}

func buildOpenGaussMergeSQL(schema, table string, fields []datatype.FieldInfo, keys []string) (string, error) {
	if len(fields) == 0 {
		return "", fmt.Errorf("openGauss MERGE requires fields")
	}
	dialect := commonquery.ForDialect("postgresql")
	columnSet := make(map[string]bool, len(fields))
	selects := make([]string, 0, len(fields))
	quotedColumns := make([]string, 0, len(fields))
	insertValues := make([]string, 0, len(fields))
	for index, field := range fields {
		column := strings.TrimSpace(field.Name)
		if column == "" || columnSet[column] {
			return "", fmt.Errorf("openGauss MERGE fields must be non-empty and unique")
		}
		columnSet[column] = true
		quoted := dialect.QuoteIdentifier(column)
		sqlType, ok := postgresql.ProtocolSQLTypeForCommonFieldType(field.Type)
		if !ok || datatype.IsSpatialFieldType(field.Type) {
			return "", fmt.Errorf("openGauss MERGE field %q has unsupported type %q", column, field.Type)
		}
		selects = append(selects, fmt.Sprintf("CAST($%d AS %s) AS %s", index+1, sqlType, quoted))
		quotedColumns = append(quotedColumns, quoted)
		insertValues = append(insertValues, "source."+quoted)
	}
	keySet := make(map[string]bool, len(keys))
	on := make([]string, 0, len(keys))
	for _, key := range keys {
		if !columnSet[key] {
			return "", fmt.Errorf("openGauss upsert batch is missing key field %q", key)
		}
		keySet[key] = true
		quoted := dialect.QuoteIdentifier(key)
		on = append(on, "target."+quoted+" = source."+quoted)
	}
	if len(on) == 0 {
		return "", fmt.Errorf("openGauss MERGE requires keys")
	}
	updates := make([]string, 0, len(fields)-len(keys))
	for _, field := range fields {
		if keySet[field.Name] {
			continue
		}
		quoted := dialect.QuoteIdentifier(field.Name)
		updates = append(updates, quoted+" = source."+quoted)
	}
	statement := "MERGE INTO " + dialect.QualifiedTable(schema, table) +
		" target USING (SELECT " + strings.Join(selects, ", ") + ") source ON (" + strings.Join(on, " AND ") + ") "
	if len(updates) > 0 {
		statement += "WHEN MATCHED THEN UPDATE SET " + strings.Join(updates, ", ") + " "
	}
	statement += "WHEN NOT MATCHED THEN INSERT (" + strings.Join(quotedColumns, ", ") + ") VALUES (" + strings.Join(insertValues, ", ") + ")"
	return statement, nil
}

func openGaussUpsertRowArgs(row map[string]interface{}, columns []string) ([]interface{}, error) {
	args := make([]interface{}, 0, len(columns))
	for _, column := range columns {
		value, ok := row[column]
		if !ok {
			return nil, fmt.Errorf("missing field %q", column)
		}
		args = append(args, value)
	}
	return args, nil
}
