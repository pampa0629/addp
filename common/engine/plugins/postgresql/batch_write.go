package postgresql

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/sqldialect"
)

const postgresDefaultInsertChunkSize = 1000
const postgresMaxBindParams = 65535

func (p *PostgreSQLPlugin) WriteBatch(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, batch *plugin.BatchData, opts plugin.BatchWriteOptions) error {
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}
	if err := validateBatchWriteMode(opts.Mode); err != nil {
		return err
	}

	schema, table, err := tablePathParts(path)
	if err != nil {
		return err
	}

	columns := batchColumns(batch)
	if len(columns) == 0 {
		return fmt.Errorf("postgresql batch write requires at least one column")
	}
	if len(columns) > postgresMaxBindParams {
		return fmt.Errorf("postgresql batch write has %d columns, exceeding max bind parameters %d", len(columns), postgresMaxBindParams)
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

	chunkSize := postgresDefaultInsertChunkSize
	if batch.Metadata != nil {
		if value, ok := batch.Metadata["chunk_size"].(int); ok && value > 0 {
			chunkSize = value
		}
	}
	if shouldUseCopyBatchWrite(opts, batch) {
		return p.writeBatchWithCopy(ctx, db, schema, table, columns, batch.Rows, chunkSize)
	}
	chunkSize = effectivePostgresInsertChunkSize(len(columns), chunkSize)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin postgresql batch write transaction: %w", err)
	}
	defer tx.Rollback()

	for start := 0; start < len(batch.Rows); start += chunkSize {
		end := start + chunkSize
		if end > len(batch.Rows) {
			end = len(batch.Rows)
		}
		insertSQL, args := buildPostgresInsertSQL(schema, table, columns, batch.Rows[start:end])
		if _, err := tx.ExecContext(ctx, insertSQL, args...); err != nil {
			return fmt.Errorf("execute postgresql batch insert rows %d-%d: %w", start, end, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit postgresql batch write: %w", err)
	}
	return nil
}

func shouldUseCopyBatchWrite(opts plugin.BatchWriteOptions, batch *plugin.BatchData) bool {
	method := strings.ToLower(strings.TrimSpace(opts.Method))
	if batch != nil && batch.Metadata != nil {
		if value, ok := batch.Metadata["write_method"].(string); ok && strings.TrimSpace(value) != "" {
			method = strings.ToLower(strings.TrimSpace(value))
		}
	}
	return method == "copy" || method == "postgres_copy"
}

func validateBatchWriteMode(mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "append", "insert":
		return nil
	default:
		return fmt.Errorf("postgresql batch write mode %q is not supported; table-level overwrite/truncate must be planned outside WriteBatch", mode)
	}
}

func tablePathParts(path plugin.CatalogPath) (string, string, error) {
	if len(path.Segments) < 2 {
		return "", "", fmt.Errorf("postgresql batch write requires schema/table catalog path")
	}
	schema := strings.TrimSpace(path.Segments[len(path.Segments)-2].Name)
	table := strings.TrimSpace(path.Segments[len(path.Segments)-1].Name)
	if schema == "" || table == "" {
		return "", "", fmt.Errorf("postgresql batch write requires non-empty schema and table")
	}
	return schema, table, nil
}

func batchColumns(batch *plugin.BatchData) []string {
	if batch == nil {
		return nil
	}
	seen := map[string]struct{}{}
	columns := make([]string, 0, len(batch.Fields))
	for _, field := range batch.Fields {
		name := strings.TrimSpace(field.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		columns = append(columns, name)
	}
	if len(columns) > 0 {
		return columns
	}
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

func effectivePostgresInsertChunkSize(columnCount, requested int) int {
	if requested <= 0 {
		requested = postgresDefaultInsertChunkSize
	}
	if columnCount <= 0 {
		return requested
	}
	maxRowsByParams := postgresMaxBindParams / columnCount
	if requested > maxRowsByParams {
		return maxRowsByParams
	}
	return requested
}

func buildPostgresInsertSQL(schema, table string, columns []string, rows []map[string]interface{}) (string, []interface{}) {
	dialect := sqldialect.ForEngine("postgresql")
	quotedColumns := make([]string, 0, len(columns))
	for _, column := range columns {
		quotedColumns = append(quotedColumns, dialect.QuoteIdentifier(column))
	}

	var sb strings.Builder
	sb.WriteString("INSERT INTO ")
	sb.WriteString(dialect.QualifiedTable(schema, table))
	sb.WriteString(" (")
	sb.WriteString(strings.Join(quotedColumns, ", "))
	sb.WriteString(") VALUES ")

	args := make([]interface{}, 0, len(rows)*len(columns))
	placeholder := 1
	valueGroups := make([]string, 0, len(rows))
	for _, row := range rows {
		group := make([]string, 0, len(columns))
		for _, column := range columns {
			group = append(group, fmt.Sprintf("$%d", placeholder))
			args = append(args, row[column])
			placeholder++
		}
		valueGroups = append(valueGroups, "("+strings.Join(group, ", ")+")")
	}
	sb.WriteString(strings.Join(valueGroups, ", "))
	return sb.String(), args
}
