package shared

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

// PrepareTableUpsert prepares a non-spatial MySQL-compatible target whose only
// unique constraints exactly match the configured stable keys.
func (w MySQLCompatibleTableWriter) PrepareTableUpsert(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableUpsertOptions) error {
	return w.PrepareTableUpsertWithTargetPolicy(ctx, connInfo, path, opts, false)
}

// PrepareTableUpsertWithTargetPolicy exposes the target-absence guard needed by
// replay targets while keeping preparation and validation on the shared path.
func (w MySQLCompatibleTableWriter) PrepareTableUpsertWithTargetPolicy(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableUpsertOptions, requireTargetAbsent bool) error {
	if err := w.validate(); err != nil {
		return err
	}
	if HasSpatialTableWrite(opts.Fields, opts.SpatialInfo) {
		return fmt.Errorf("%s table upsert does not support spatial fields", w.engineType())
	}
	keys, err := w.validateTableUpsertOptions(opts)
	if err != nil {
		return err
	}
	database, table, err := w.tablePathParts(path)
	if err != nil {
		return err
	}
	dsn, err := w.serverDSN(connInfo)
	if err != nil {
		return fmt.Errorf("failed to build %s dsn: %w", w.engineType(), err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("open %s upsert target: %w", w.engineType(), err)
	}
	defer db.Close()

	exists, err := w.baseTableExists(ctx, db, database, table)
	if err != nil {
		return err
	}
	if requireTargetAbsent && exists {
		return fmt.Errorf("%s replay target %s.%s already exists", w.engineType(), database, table)
	}
	fields := append([]datatype.FieldInfo(nil), opts.Fields...)
	keySet := make(map[string]bool, len(keys))
	for _, key := range keys {
		keySet[key] = true
	}
	for index := range fields {
		name := strings.TrimSpace(fields[index].Name)
		fields[index].PrimaryKey = keySet[name]
		if keySet[name] {
			fields[index].Nullable = false
		}
	}
	if err := w.createTableIfNotExistsWithPrimaryKeys(ctx, db, database, table, fields, keys); err != nil {
		return err
	}
	return w.validateTableUpsertTarget(ctx, db, database, table, keys)
}

// UpsertBatch atomically applies one non-spatial batch. Reapplying the same
// batch yields the same target state.
func (w MySQLCompatibleTableWriter) UpsertBatch(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, batch *plugin.BatchData, opts plugin.TableUpsertOptions) error {
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}
	if err := w.validate(); err != nil {
		return err
	}
	if HasSpatialTableWrite(opts.Fields, opts.SpatialInfo) || HasSpatialTableWrite(batch.Fields, batch.Spatial) {
		return fmt.Errorf("%s table upsert does not support spatial fields", w.engineType())
	}
	keys, err := w.validateTableUpsertOptions(opts)
	if err != nil {
		return err
	}
	database, table, err := w.tablePathParts(path)
	if err != nil {
		return err
	}
	dsn, err := w.serverDSN(connInfo)
	if err != nil {
		return fmt.Errorf("failed to build %s dsn: %w", w.engineType(), err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("open %s upsert target: %w", w.engineType(), err)
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin %s upsert: %w", w.engineType(), err)
	}
	defer tx.Rollback()
	if err := w.upsertRowsTx(ctx, tx, database, table, batch, keys); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s upsert: %w", w.engineType(), err)
	}
	return nil
}

func (w MySQLCompatibleTableWriter) upsertRowsTx(ctx context.Context, tx *sql.Tx, database, table string, batch *plugin.BatchData, keys []string) error {
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}
	if tx == nil {
		return fmt.Errorf("%s upsert requires a transaction", w.engineType())
	}
	if HasSpatialTableWrite(batch.Fields, batch.Spatial) {
		return fmt.Errorf("%s table upsert does not support spatial fields", w.engineType())
	}
	columns := mysqlCompatibleBatchColumns(batch)
	if len(columns) == 0 {
		return fmt.Errorf("%s upsert requires batch columns", w.engineType())
	}
	columnSet := make(map[string]bool, len(columns))
	for _, column := range columns {
		columnSet[column] = true
	}
	for _, key := range keys {
		if !columnSet[key] {
			return fmt.Errorf("%s upsert batch is missing key field %q", w.engineType(), key)
		}
	}
	for rowIndex, row := range batch.Rows {
		for _, key := range keys {
			value, ok := row[key]
			if !ok || value == nil {
				return fmt.Errorf("%s upsert row %d requires non-null key field %q", w.engineType(), rowIndex, key)
			}
		}
	}
	chunkSize := mysqlCompatibleInsertChunkSize(len(columns), mysqlCompatibleDefaultInsertChunkSize)
	if chunkSize <= 0 {
		return fmt.Errorf("%s upsert has too many columns for insert bind parameters", w.engineType())
	}
	for start := 0; start < len(batch.Rows); start += chunkSize {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := start + chunkSize
		if end > len(batch.Rows) {
			end = len(batch.Rows)
		}
		statement, args := mysqlCompatibleInsertSQL(database, table, columns, batch.Rows[start:end])
		statement += mysqlCompatibleOnDuplicateKeyClause(columns, keys)
		if _, err := tx.ExecContext(ctx, statement, args...); err != nil {
			return fmt.Errorf("execute %s upsert rows %d-%d: %w", w.engineType(), start, end, err)
		}
	}
	return nil
}

func (w MySQLCompatibleTableWriter) validateTableUpsertOptions(opts plugin.TableUpsertOptions) ([]string, error) {
	fieldSet := make(map[string]bool, len(opts.Fields))
	for _, field := range opts.Fields {
		if name := strings.TrimSpace(field.Name); name != "" {
			fieldSet[name] = true
		}
	}
	keys := make([]string, 0, len(opts.Keys))
	seen := map[string]bool{}
	for _, key := range opts.Keys {
		key = strings.TrimSpace(key)
		if key == "" || seen[key] {
			return nil, fmt.Errorf("%s upsert keys must be non-empty and unique", w.engineType())
		}
		if !fieldSet[key] {
			return nil, fmt.Errorf("%s upsert key %q is not present in table fields", w.engineType(), key)
		}
		seen[key] = true
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("%s upsert requires keys", w.engineType())
	}
	return keys, nil
}

func (w MySQLCompatibleTableWriter) baseTableExists(ctx context.Context, db *sql.DB, database, table string) (bool, error) {
	var count int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.tables
		WHERE table_schema = ? AND table_name = ? AND table_type = 'BASE TABLE'
	`, database, table).Scan(&count); err != nil {
		return false, fmt.Errorf("check %s upsert target %s.%s: %w", w.engineType(), database, table, err)
	}
	return count > 0, nil
}

func (w MySQLCompatibleTableWriter) validateTableUpsertTarget(ctx context.Context, db *sql.DB, database, table string, keys []string) error {
	var engine sql.NullString
	if err := db.QueryRowContext(ctx, `
		SELECT engine
		FROM information_schema.tables
		WHERE table_schema = ? AND table_name = ? AND table_type = 'BASE TABLE'
	`, database, table).Scan(&engine); err != nil {
		return fmt.Errorf("read %s upsert target engine %s.%s: %w", w.engineType(), database, table, err)
	}
	if !engine.Valid || !strings.EqualFold(strings.TrimSpace(engine.String), "InnoDB") {
		return fmt.Errorf("%s upsert target %s.%s must use InnoDB, got %q", w.engineType(), database, table, engine.String)
	}

	columns, err := w.tableColumns(ctx, db, database, table)
	if err != nil {
		return err
	}
	columnsByName := make(map[string]mysqlCompatibleColumnInfo, len(columns))
	for _, column := range columns {
		columnsByName[column.Name] = column
	}
	for _, key := range keys {
		column, ok := columnsByName[key]
		if !ok {
			return fmt.Errorf("%s upsert target is missing key column %q", w.engineType(), key)
		}
		if column.Nullable {
			return fmt.Errorf("%s upsert key column %q must be NOT NULL", w.engineType(), key)
		}
	}

	indexes, err := mysqlCompatibleUniqueIndexes(ctx, db, w.engineType(), "upsert", database, table)
	if err != nil {
		return err
	}
	if !mysqlCompatibleUniqueIndexesMatchKeys(keys, indexes) {
		return fmt.Errorf("%s upsert target unique constraints must all exactly match configured keys %v, got %v", w.engineType(), keys, indexes)
	}
	return nil
}

func mysqlCompatibleBatchColumns(batch *plugin.BatchData) []string {
	if batch == nil {
		return nil
	}
	if columns := mysqlCompatibleFieldColumns(batch.Fields); len(columns) > 0 {
		return columns
	}
	seen := map[string]struct{}{}
	columns := make([]string, 0)
	for _, row := range batch.Rows {
		for name := range row {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			columns = append(columns, name)
		}
	}
	sort.Strings(columns)
	return columns
}

func mysqlCompatibleUniqueIndexesMatchKeys(keys []string, indexes [][]string) bool {
	if len(keys) == 0 || len(indexes) == 0 {
		return false
	}
	for _, columns := range indexes {
		if len(columns) != len(keys) {
			return false
		}
		for index := range keys {
			if columns[index] != keys[index] {
				return false
			}
		}
	}
	return true
}

func mysqlCompatibleOnDuplicateKeyClause(columns, keys []string) string {
	dialect := mysqlCompatibleDialect()
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
