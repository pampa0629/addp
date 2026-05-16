package postgresql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/sqldialect"
)

func (p *PostgreSQLPlugin) OpenTableReadSession(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.TableReadSessionOptions) (plugin.TableReadSession, error) {
	query, err := postgresReadSessionQuery(path, opts)
	if err != nil {
		return nil, err
	}
	connStr, err := p.BuildDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to build postgresql dsn: %w", err)
	}
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open postgresql connection: %w", err)
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("begin postgresql table read session: %w", err)
	}

	cursorName := "addp_transfer_read_cursor"
	declareSQL := fmt.Sprintf("DECLARE %s NO SCROLL CURSOR FOR %s", cursorName, query)
	if _, err := tx.ExecContext(ctx, declareSQL); err != nil {
		tx.Rollback()
		db.Close()
		return nil, fmt.Errorf("declare postgresql read cursor: %w", err)
	}

	return &postgresTableReadSession{
		db:         db,
		tx:         tx,
		cursorName: cursorName,
	}, nil
}

func postgresReadSessionQuery(path plugin.CatalogPath, opts plugin.TableReadSessionOptions) (string, error) {
	query := strings.TrimSpace(opts.Query)
	if query != "" {
		return query, nil
	}
	schema, table, err := tablePathParts(path)
	if err != nil {
		return "", err
	}
	return sqldialect.ForEngine("postgresql").SelectTableSQL("*", schema, table, "", "", 0, 0), nil
}

type postgresTableReadSession struct {
	db         *sql.DB
	tx         *sql.Tx
	cursorName string
	closed     bool
	offset     int64
}

func (s *postgresTableReadSession) ReadBatch(ctx context.Context, limit int) (*plugin.BatchData, error) {
	if s.closed {
		return nil, fmt.Errorf("postgresql table read session is closed")
	}
	if limit <= 0 {
		limit = 1000
	}
	query := fmt.Sprintf("FETCH FORWARD %d FROM %s", limit, s.cursorName)
	rows, err := s.tx.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("fetch postgresql read cursor: %w", err)
	}
	defer rows.Close()

	batch, err := scanPostgresRowsToBatch(rows, s.offset)
	if err != nil {
		return nil, err
	}
	s.offset += int64(len(batch.Rows))
	return batch, nil
}

func (s *postgresTableReadSession) Close(ctx context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true

	var closeErr error
	if s.tx != nil {
		if _, err := s.tx.ExecContext(ctx, "CLOSE "+s.cursorName); err != nil {
			closeErr = fmt.Errorf("close postgresql read cursor: %w", err)
		}
		if err := s.tx.Rollback(); err != nil && err != sql.ErrTxDone && closeErr == nil {
			closeErr = fmt.Errorf("rollback postgresql table read session: %w", err)
		}
	}
	if s.db != nil {
		if err := s.db.Close(); err != nil && closeErr == nil {
			closeErr = fmt.Errorf("close postgresql table read session connection: %w", err)
		}
	}
	return closeErr
}

func scanPostgresRowsToBatch(rows *sql.Rows, offset int64) (*plugin.BatchData, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("get postgresql cursor columns: %w", err)
	}
	resultRows := make([]map[string]interface{}, 0)
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("scan postgresql cursor row: %w", err)
		}
		row := make(map[string]interface{}, len(columns))
		for i, column := range columns {
			value := values[i]
			if bytes, ok := value.([]byte); ok {
				row[column] = string(bytes)
				continue
			}
			row[column] = value
		}
		resultRows = append(resultRows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate postgresql cursor rows: %w", err)
	}

	fields := make([]plugin.FieldInfo, 0, len(columns))
	for _, column := range columns {
		fields = append(fields, plugin.FieldInfo{Name: column})
	}
	return &plugin.BatchData{
		Rows:   resultRows,
		Fields: fields,
		Offset: offset,
	}, nil
}
