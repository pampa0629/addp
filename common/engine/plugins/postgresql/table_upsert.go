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

func (p *PostgreSQLPlugin) PrepareTableUpsert(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableUpsertOptions) error {
	return p.prepareTableUpsert(ctx, connInfo, path, opts, false)
}

func (p *PostgreSQLPlugin) prepareTableUpsert(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableUpsertOptions, requireTargetAbsent bool) error {
	keys, err := validatePostgresUpsertOptions(opts)
	if err != nil {
		return err
	}
	schema, table, err := tablePathParts(path)
	if err != nil {
		return err
	}
	connStr, err := p.BuildDSN(connInfo)
	if err != nil {
		return err
	}
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return err
	}
	defer db.Close()
	var exists bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, schema+"."+table).Scan(&exists); err != nil {
		return fmt.Errorf("check postgresql upsert target: %w", err)
	}
	if requireTargetAbsent && exists {
		return fmt.Errorf("postgresql replay target %s.%s already exists", schema, table)
	}
	writeFields := append([]datatype.FieldInfo(nil), opts.Fields...)
	if !exists {
		keySet := make(map[string]bool, len(keys))
		for _, key := range keys {
			keySet[key] = true
		}
		for i := range writeFields {
			writeFields[i].PrimaryKey = keySet[writeFields[i].Name]
			if writeFields[i].PrimaryKey {
				writeFields[i].Nullable = false
			}
		}
	}
	if err := createPostgresTable(ctx, db, schema, table, writeFields, opts.SpatialInfo, !requireTargetAbsent); err != nil {
		return err
	}
	if err := validatePostgresUniqueTieBreakers(ctx, db, schema, table, keys); err != nil {
		return fmt.Errorf("postgresql upsert keys are not backed by a unique constraint: %w", err)
	}
	return nil
}

func (p *PostgreSQLPlugin) UpsertBatch(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, batch *plugin.BatchData, opts plugin.TableUpsertOptions) error {
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}
	keys, err := validatePostgresUpsertOptions(opts)
	if err != nil {
		return err
	}
	schema, table, err := tablePathParts(path)
	if err != nil {
		return err
	}
	columns := batchColumns(batch)
	if len(columns) == 0 {
		return fmt.Errorf("postgresql upsert requires batch columns")
	}
	columnSet := make(map[string]bool, len(columns))
	for _, column := range columns {
		columnSet[column] = true
	}
	for _, key := range keys {
		if !columnSet[key] {
			return fmt.Errorf("postgresql upsert batch is missing key field %q", key)
		}
	}
	connStr, err := p.BuildDSN(connInfo)
	if err != nil {
		return err
	}
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := upsertPostgresRowsTx(ctx, tx, schema, table, batch, keys); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit postgresql upsert: %w", err)
	}
	return nil
}

func upsertPostgresRowsTx(ctx context.Context, tx *sql.Tx, schema, table string, batch *plugin.BatchData, keys []string) error {
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}
	columns := batchColumns(batch)
	if len(columns) == 0 {
		return fmt.Errorf("postgresql upsert requires batch columns")
	}
	columnSet := make(map[string]bool, len(columns))
	for _, column := range columns {
		columnSet[column] = true
	}
	for _, key := range keys {
		if !columnSet[key] {
			return fmt.Errorf("postgresql upsert batch is missing key field %q", key)
		}
	}
	chunkSize := effectivePostgresInsertChunkSize(len(columns), postgresDefaultInsertChunkSize)
	geometryColumns := postgresGeometryColumns(batch.Fields)
	for start := 0; start < len(batch.Rows); start += chunkSize {
		end := start + chunkSize
		if end > len(batch.Rows) {
			end = len(batch.Rows)
		}
		statement, args := buildPostgresInsertSQL(schema, table, columns, batch.Rows[start:end], geometryColumns)
		statement += postgresOnConflictClause(columns, keys)
		if _, err := tx.ExecContext(ctx, statement, args...); err != nil {
			return fmt.Errorf("execute postgresql upsert rows %d-%d: %w", start, end, err)
		}
	}
	return nil
}

func validatePostgresUpsertOptions(opts plugin.TableUpsertOptions) ([]string, error) {
	keys := make([]string, 0, len(opts.Keys))
	seen := map[string]bool{}
	fieldSet := map[string]bool{}
	for _, field := range opts.Fields {
		fieldSet[strings.TrimSpace(field.Name)] = true
	}
	for _, key := range opts.Keys {
		key = strings.TrimSpace(key)
		if key == "" || seen[key] {
			return nil, fmt.Errorf("postgresql upsert keys must be non-empty and unique")
		}
		if !fieldSet[key] {
			return nil, fmt.Errorf("postgresql upsert key %q is not present in table fields", key)
		}
		seen[key] = true
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("postgresql upsert requires keys")
	}
	return keys, nil
}

func postgresOnConflictClause(columns, keys []string) string {
	dialect := commonquery.ForEngine("postgresql")
	quotedKeys := make([]string, 0, len(keys))
	keySet := map[string]bool{}
	for _, key := range keys {
		quotedKeys = append(quotedKeys, dialect.QuoteIdentifier(key))
		keySet[key] = true
	}
	updates := make([]string, 0, len(columns))
	for _, column := range columns {
		if keySet[column] {
			continue
		}
		quoted := dialect.QuoteIdentifier(column)
		updates = append(updates, quoted+" = EXCLUDED."+quoted)
	}
	clause := " ON CONFLICT (" + strings.Join(quotedKeys, ", ") + ") "
	if len(updates) == 0 {
		return clause + "DO NOTHING"
	}
	return clause + "DO UPDATE SET " + strings.Join(updates, ", ")
}
