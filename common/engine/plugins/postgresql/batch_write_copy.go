package postgresql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/lib/pq"
)

func (p *PostgreSQLPlugin) writeBatchWithCopy(ctx context.Context, db *sql.DB, schema, table string, columns []string, rows []map[string]interface{}, chunkSize int, geometryColumns map[string]struct{}) error {
	if chunkSize <= 0 {
		chunkSize = postgresDefaultInsertChunkSize
	}
	for start := 0; start < len(rows); start += chunkSize {
		end := start + chunkSize
		if end > len(rows) {
			end = len(rows)
		}
		if err := writePostgresCopyChunk(ctx, db, schema, table, columns, rows[start:end], geometryColumns); err != nil {
			return fmt.Errorf("execute postgresql copy rows %d-%d: %w", start, end, err)
		}
	}
	return nil
}

func writePostgresCopyChunk(ctx context.Context, db *sql.DB, schema, table string, columns []string, rows []map[string]interface{}, geometryColumns map[string]struct{}) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin postgresql copy transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, pq.CopyInSchema(schema, table, columns...))
	if err != nil {
		return fmt.Errorf("prepare postgresql copy statement: %w", err)
	}
	defer stmt.Close()

	values := make([]interface{}, len(columns))
	for _, row := range rows {
		for i, column := range columns {
			_, isGeometry := geometryColumns[column]
			values[i] = postgresWriteValue(row[column], isGeometry)
		}
		if _, err := stmt.ExecContext(ctx, values...); err != nil {
			return fmt.Errorf("copy postgresql row: %w", err)
		}
	}
	if _, err := stmt.ExecContext(ctx); err != nil {
		return fmt.Errorf("finalize postgresql copy: %w", err)
	}
	if err := stmt.Close(); err != nil {
		return fmt.Errorf("close postgresql copy statement: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit postgresql copy: %w", err)
	}
	return nil
}
