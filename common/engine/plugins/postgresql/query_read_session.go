package postgresql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
)

var _ plugin.QueryReadSessionProvider = (*PostgreSQLPlugin)(nil)

func (p *PostgreSQLPlugin) OpenQueryReadSession(ctx context.Context, prepared plugin.PreparedQuery) (plugin.QueryReadSession, error) {
	connInfo, request, err := plugin.ConsumeSQLPreparedQuery(prepared, p)
	if err != nil {
		return nil, err
	}
	if err := validatePostgresQueryReadSession(request); err != nil {
		return nil, err
	}
	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("build PostgreSQL query read DSN: %w", err)
	}
	db, err := sql.Open(p.sqlDriverName(), dsn)
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL query read database: %w", err)
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("begin PostgreSQL query read transaction: %w", err)
	}
	const cursor = "addp_query_read_cursor"
	_, err = tx.ExecContext(ctx, "DECLARE "+cursor+" NO SCROLL CURSOR FOR "+request.Query, request.Options.Args...)
	if err != nil {
		_ = tx.Rollback()
		_ = db.Close()
		return nil, fmt.Errorf("declare PostgreSQL query read session: %w", err)
	}
	readSet := p.observeSessionReadSet(ctx, tx, request)
	rows, err := tx.QueryContext(ctx, "FETCH FORWARD 0 FROM "+cursor)
	if err != nil {
		_ = tx.Rollback()
		_ = db.Close()
		return nil, fmt.Errorf("describe PostgreSQL query read session: %w", err)
	}
	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		_ = rows.Close()
		_ = tx.Rollback()
		_ = db.Close()
		return nil, fmt.Errorf("read PostgreSQL query column types: %w", err)
	}
	columns := make([]string, len(columnTypes))
	binaryColumns := make([]bool, len(columnTypes))
	for index, columnType := range columnTypes {
		columns[index] = columnType.Name()
		binaryColumns[index] = strings.EqualFold(columnType.DatabaseTypeName(), "BYTEA")
	}
	if err := rows.Close(); err != nil {
		_ = tx.Rollback()
		_ = db.Close()
		return nil, err
	}
	return &postgresQueryReadSession{db: db, tx: tx, cursor: cursor, readSet: readSet, columns: columns, binaryColumns: binaryColumns}, nil
}

func validatePostgresQueryReadSession(request plugin.QueryRequest) error {
	if !request.Options.ReadOnly {
		return fmt.Errorf("PostgreSQL query read session requires read_only=true")
	}
	if request.Options.Limit != 0 || request.Options.Offset != 0 {
		return fmt.Errorf("PostgreSQL query read session does not accept preview limit or offset")
	}
	return nil
}

type postgresQueryReadSession struct {
	db            *sql.DB
	tx            *sql.Tx
	cursor        string
	readSet       *plugin.QueryReadSet
	columns       []string
	binaryColumns []bool
	offset        int64
	closed        bool
}

func (s *postgresQueryReadSession) ReadSet(context.Context) (*plugin.QueryReadSet, error) {
	if s.readSet == nil {
		return nil, plugin.ErrQueryReadSetUnresolved
	}
	return s.readSet.Clone(), nil
}

func (s *postgresQueryReadSession) ReadBatch(ctx context.Context, limit int) (*plugin.BatchData, error) {
	if s.closed {
		return nil, fmt.Errorf("PostgreSQL query read session is closed")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("PostgreSQL query read batch limit must be positive")
	}
	rows, err := s.tx.QueryContext(ctx, fmt.Sprintf("FETCH FORWARD %d FROM %s", limit, s.cursor))
	if err != nil {
		_ = s.abort()
		return nil, fmt.Errorf("fetch PostgreSQL query read rows: %w", err)
	}
	defer rows.Close()
	batch := &plugin.BatchData{Offset: s.offset, Rows: make([]map[string]interface{}, 0, limit)}
	for rows.Next() {
		values := make([]interface{}, len(s.columns))
		pointers := make([]interface{}, len(s.columns))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := rows.Scan(pointers...); err != nil {
			_ = rows.Close()
			_ = s.abort()
			return nil, fmt.Errorf("scan PostgreSQL query read row: %w", err)
		}
		row := make(map[string]interface{}, len(s.columns))
		for index, column := range s.columns {
			value := values[index]
			if bytes, ok := value.([]byte); ok {
				if s.binaryColumns[index] {
					// SQL NULL is a nil interface; an empty BYTEA remains non-nil.
					copied := make([]byte, len(bytes))
					copy(copied, bytes)
					value = copied
				} else {
					value = string(bytes)
				}
			}
			row[column] = value
		}
		batch.Rows = append(batch.Rows, row)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		_ = s.abort()
		return nil, fmt.Errorf("iterate PostgreSQL query read rows: %w", err)
	}
	if err := rows.Close(); err != nil {
		_ = s.abort()
		return nil, err
	}
	s.offset += int64(len(batch.Rows))
	if len(batch.Rows) < limit {
		if err := s.finish(); err != nil {
			return nil, err
		}
	}
	return batch, nil
}

func (s *postgresQueryReadSession) Close(context.Context) error {
	if s.closed {
		return nil
	}
	return s.finish()
}

func (s *postgresQueryReadSession) finish() error {
	if s.closed {
		return nil
	}
	s.closed = true
	var firstErr error
	if s.tx != nil {
		if _, err := s.tx.ExecContext(context.Background(), "CLOSE "+s.cursor); err != nil {
			firstErr = err
		}
		if err := s.tx.Commit(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if s.db != nil {
		if err := s.db.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return fmt.Errorf("close PostgreSQL query read session: %w", firstErr)
	}
	return nil
}

func (s *postgresQueryReadSession) abort() error {
	if s.closed {
		return nil
	}
	s.closed = true
	var firstErr error
	if s.tx != nil {
		firstErr = s.tx.Rollback()
	}
	if s.db != nil {
		if err := s.db.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
