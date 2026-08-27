package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func (p *MySQLPlugin) PrepareTableUpsert(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableUpsertOptions) error {
	return p.prepareTableUpsert(ctx, connInfo, path, opts, false)
}

func (p *MySQLPlugin) prepareTableUpsert(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableUpsertOptions, requireTargetAbsent bool) error {
	keys, err := validateMySQLUpsertOptions(opts)
	if err != nil {
		return err
	}
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
		return fmt.Errorf("open mysql upsert target: %w", err)
	}
	defer db.Close()

	exists, err := mysqlBaseTableExists(ctx, db, database, table)
	if err != nil {
		return err
	}
	if requireTargetAbsent && exists {
		return fmt.Errorf("mysql replay target %s.%s already exists", database, table)
	}
	fields := append([]datatype.FieldInfo(nil), opts.Fields...)
	if !exists {
		keySet := make(map[string]bool, len(keys))
		for _, key := range keys {
			keySet[key] = true
		}
		for i := range fields {
			if keySet[strings.TrimSpace(fields[i].Name)] {
				fields[i].PrimaryKey = true
				fields[i].Nullable = false
			}
		}
	}
	if err := createMySQLTableIfNotExists(ctx, db, database, table, fields, opts.SpatialInfo); err != nil {
		return err
	}
	if err := validateMySQLUpsertTarget(ctx, db, database, table, keys); err != nil {
		return err
	}
	return nil
}

func (p *MySQLPlugin) UpsertBatch(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, batch *plugin.BatchData, opts plugin.TableUpsertOptions) error {
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}
	keys, err := validateMySQLUpsertOptions(opts)
	if err != nil {
		return err
	}
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
		return fmt.Errorf("open mysql upsert target: %w", err)
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin mysql upsert: %w", err)
	}
	defer tx.Rollback()
	if err := upsertMySQLRowsTx(ctx, tx, database, table, batch, opts, keys); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit mysql upsert: %w", err)
	}
	return nil
}

func upsertMySQLRowsTx(ctx context.Context, tx *sql.Tx, database, table string, batch *plugin.BatchData, opts plugin.TableUpsertOptions, keys []string) error {
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}
	columns := mysqlBatchColumns(batch)
	if len(columns) == 0 {
		return fmt.Errorf("mysql upsert requires batch columns")
	}
	columnSet := make(map[string]bool, len(columns))
	for _, column := range columns {
		columnSet[column] = true
	}
	for _, key := range keys {
		if !columnSet[key] {
			return fmt.Errorf("mysql upsert batch is missing key field %q", key)
		}
	}
	chunkSize := effectiveMySQLInsertChunkSize(len(columns), mysqlDefaultInsertChunkSize)
	if chunkSize <= 0 {
		return fmt.Errorf("mysql upsert has too many columns for insert bind parameters")
	}
	geometrySRIDs := mysqlGeometrySRIDs(opts.Fields, opts.SpatialInfo)
	for start := 0; start < len(batch.Rows); start += chunkSize {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := start + chunkSize
		if end > len(batch.Rows) {
			end = len(batch.Rows)
		}
		statement, args, err := buildMySQLInsertSQL(database, table, columns, batch.Rows[start:end], geometrySRIDs)
		if err != nil {
			return fmt.Errorf("build mysql upsert rows %d-%d: %w", start, end, err)
		}
		statement += mysqlOnDuplicateKeyClause(columns, keys)
		if _, err := tx.ExecContext(ctx, statement, args...); err != nil {
			return fmt.Errorf("execute mysql upsert rows %d-%d: %w", start, end, err)
		}
	}
	return nil
}

func validateMySQLUpsertOptions(opts plugin.TableUpsertOptions) ([]string, error) {
	fieldSet := make(map[string]bool, len(opts.Fields))
	for _, field := range opts.Fields {
		name := strings.TrimSpace(field.Name)
		if name != "" {
			fieldSet[name] = true
		}
	}
	keys := make([]string, 0, len(opts.Keys))
	seen := map[string]bool{}
	for _, key := range opts.Keys {
		key = strings.TrimSpace(key)
		if key == "" || seen[key] {
			return nil, fmt.Errorf("mysql upsert keys must be non-empty and unique")
		}
		if !fieldSet[key] {
			return nil, fmt.Errorf("mysql upsert key %q is not present in table fields", key)
		}
		seen[key] = true
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("mysql upsert requires keys")
	}
	return keys, nil
}

func mysqlBaseTableExists(ctx context.Context, db *sql.DB, database, table string) (bool, error) {
	var count int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.tables
		WHERE table_schema = ? AND table_name = ? AND table_type = 'BASE TABLE'
	`, database, table).Scan(&count); err != nil {
		return false, fmt.Errorf("check mysql upsert target %s.%s: %w", database, table, err)
	}
	return count > 0, nil
}

func validateMySQLUpsertTarget(ctx context.Context, db *sql.DB, database, table string, keys []string) error {
	var engine sql.NullString
	if err := db.QueryRowContext(ctx, `
		SELECT engine
		FROM information_schema.tables
		WHERE table_schema = ? AND table_name = ? AND table_type = 'BASE TABLE'
	`, database, table).Scan(&engine); err != nil {
		return fmt.Errorf("read mysql upsert target engine %s.%s: %w", database, table, err)
	}
	if !engine.Valid || !strings.EqualFold(strings.TrimSpace(engine.String), "InnoDB") {
		return fmt.Errorf("mysql upsert target %s.%s must use InnoDB, got %q", database, table, engine.String)
	}

	columns, err := mysqlTableColumns(ctx, db, database, table)
	if err != nil {
		return err
	}
	columnsByName := make(map[string]mysqlColumnInfo, len(columns))
	for _, column := range columns {
		columnsByName[column.Name] = column
	}
	for _, key := range keys {
		column, ok := columnsByName[key]
		if !ok {
			return fmt.Errorf("mysql upsert target is missing key column %q", key)
		}
		if column.Nullable {
			return fmt.Errorf("mysql upsert key column %q must be NOT NULL", key)
		}
	}

	indexes, err := mysqlUniqueIndexes(ctx, db, database, table)
	if err != nil {
		return err
	}
	if !mysqlUniqueIndexesCompatible(keys, indexes) {
		return fmt.Errorf("mysql upsert target unique constraints must all exactly match configured keys %v, got %v", keys, indexes)
	}
	return nil
}

func mysqlUniqueIndexes(ctx context.Context, db *sql.DB, database, table string) ([][]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT index_name, seq_in_index, column_name, sub_part
		FROM information_schema.statistics
		WHERE table_schema = ? AND table_name = ? AND non_unique = 0
		ORDER BY index_name, seq_in_index
	`, database, table)
	if err != nil {
		return nil, fmt.Errorf("query mysql upsert unique constraints: %w", err)
	}
	defer rows.Close()

	indexOrder := make([]string, 0)
	indexColumns := map[string][]string{}
	for rows.Next() {
		var indexName string
		var sequence int
		var column sql.NullString
		var prefixLength sql.NullInt64
		if err := rows.Scan(&indexName, &sequence, &column, &prefixLength); err != nil {
			return nil, fmt.Errorf("scan mysql upsert unique constraint: %w", err)
		}
		if _, exists := indexColumns[indexName]; !exists {
			indexOrder = append(indexOrder, indexName)
		}
		name := ""
		if column.Valid && !prefixLength.Valid {
			name = column.String
		}
		indexColumns[indexName] = append(indexColumns[indexName], name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mysql upsert unique constraints: %w", err)
	}
	result := make([][]string, 0, len(indexOrder))
	for _, indexName := range indexOrder {
		result = append(result, indexColumns[indexName])
	}
	return result, nil
}

func mysqlUniqueIndexesCompatible(keys []string, indexes [][]string) bool {
	if len(keys) == 0 || len(indexes) == 0 {
		return false
	}
	for _, columns := range indexes {
		if len(columns) != len(keys) {
			return false
		}
		for i := range keys {
			if columns[i] != keys[i] {
				return false
			}
		}
	}
	return true
}

func mysqlOnDuplicateKeyClause(columns, keys []string) string {
	dialect := mysqlDialect()
	keySet := make(map[string]bool, len(keys))
	for _, key := range keys {
		keySet[key] = true
	}
	updates := make([]string, 0, len(columns))
	for _, column := range columns {
		if keySet[column] {
			continue
		}
		quoted := dialect.QuoteIdentifier(column)
		updates = append(updates, quoted+" = new_values."+quoted)
	}
	if len(updates) == 0 && len(keys) > 0 {
		quoted := dialect.QuoteIdentifier(keys[0])
		updates = append(updates, quoted+" = new_values."+quoted)
	}
	return " AS new_values ON DUPLICATE KEY UPDATE " + strings.Join(updates, ", ")
}
