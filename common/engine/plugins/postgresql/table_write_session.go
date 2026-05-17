package postgresql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/lib/pq"
)

func (p *PostgreSQLPlugin) OpenTableWriteSession(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.TableWriteSessionOptions) (plugin.TableWriteSession, error) {
	if !shouldUseCopyWriteMethod(opts.Method) {
		return nil, fmt.Errorf("postgresql table write session only supports copy method")
	}

	schema, table, err := tablePathParts(path)
	if err != nil {
		return nil, err
	}
	columns := fieldColumns(opts.Fields)
	if len(columns) == 0 {
		return nil, fmt.Errorf("postgresql table write session requires fields")
	}

	connStr, err := p.BuildDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to build postgresql dsn: %w", err)
	}
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open postgresql connection: %w", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("begin postgresql table write session: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx, pq.CopyInSchema(schema, table, columns...))
	if err != nil {
		tx.Rollback()
		db.Close()
		return nil, fmt.Errorf("prepare postgresql copy session: %w", err)
	}

	return &postgresTableWriteSession{
		db:      db,
		tx:      tx,
		stmt:    stmt,
		columns: columns,
	}, nil
}

func shouldUseCopyWriteMethod(method string) bool {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "copy", "postgres_copy":
		return true
	default:
		return false
	}
}

func fieldColumns(fields []plugin.FieldInfo) []string {
	seen := map[string]struct{}{}
	columns := make([]string, 0, len(fields))
	for _, field := range fields {
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
	return columns
}

type postgresTableWriteSession struct {
	db      *sql.DB
	tx      *sql.Tx
	stmt    *sql.Stmt
	columns []string
	closed  bool
}

func (s *postgresTableWriteSession) WriteBatch(ctx context.Context, batch *plugin.BatchData) error {
	if s.closed {
		return fmt.Errorf("postgresql table write session is closed")
	}
	if batch == nil || len(batch.Rows) == 0 {
		return nil
	}
	values := make([]interface{}, len(s.columns))
	for _, row := range batch.Rows {
		if err := ctx.Err(); err != nil {
			return err
		}
		for i, column := range s.columns {
			values[i] = row[column]
		}
		if _, err := s.stmt.ExecContext(ctx, values...); err != nil {
			return fmt.Errorf("copy postgresql row: %w", err)
		}
	}
	return nil
}

func (s *postgresTableWriteSession) Close(ctx context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true

	if _, err := s.stmt.ExecContext(ctx); err != nil {
		_ = s.abort()
		return fmt.Errorf("finalize postgresql copy session: %w", err)
	}
	if err := s.stmt.Close(); err != nil {
		_ = s.abort()
		return fmt.Errorf("close postgresql copy session statement: %w", err)
	}
	if err := s.tx.Commit(); err != nil {
		_ = s.db.Close()
		return fmt.Errorf("commit postgresql copy session: %w", err)
	}
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("close postgresql copy session connection: %w", err)
	}
	return nil
}

func (s *postgresTableWriteSession) Abort(ctx context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.abort()
}

func (s *postgresTableWriteSession) abort() error {
	var abortErr error
	if s.stmt != nil {
		if err := s.stmt.Close(); err != nil && abortErr == nil {
			abortErr = fmt.Errorf("close postgresql copy session statement: %w", err)
		}
	}
	if s.tx != nil {
		if err := s.tx.Rollback(); err != nil && err != sql.ErrTxDone && abortErr == nil {
			abortErr = fmt.Errorf("rollback postgresql copy session: %w", err)
		}
	}
	if s.db != nil {
		if err := s.db.Close(); err != nil && abortErr == nil {
			abortErr = fmt.Errorf("close postgresql copy session connection: %w", err)
		}
	}
	return abortErr
}
