package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

// OpenQueryReadSession consumes the already bound, one-shot plan. The driver
// streams a single result set; batch size never changes the user's SQL.
func (p *MySQLPlugin) OpenQueryReadSession(ctx context.Context, prepared plugin.PreparedQuery) (plugin.QueryReadSession, error) {
	binding, err := plugin.ConsumeSQLPreparedReadSession(prepared, p)
	if err != nil {
		return nil, err
	}
	request := binding.Request
	if !request.Options.ReadOnly || request.Options.Limit != 0 || request.Options.Offset != 0 {
		return nil, fmt.Errorf("MySQL query read session requires read_only and no preview limit or offset")
	}
	dsn, err := p.BuildDSN(binding.ConnectionInfo)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	s := &mysqlQueryReadSession{db: db, binding: binding, cancel: cancel}
	s.tx, err = db.BeginTx(sessionCtx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		_ = s.Close(ctx)
		return nil, err
	}
	if err = binding.ValidateExecution(sessionCtx, s.tx); err != nil {
		_ = s.Close(ctx)
		return nil, err
	}
	s.rows, err = s.tx.QueryContext(sessionCtx, request.Query, request.Options.Args...)
	if err != nil {
		_ = s.Close(ctx)
		return nil, err
	}
	types, err := s.rows.ColumnTypes()
	if err != nil {
		_ = s.Close(ctx)
		return nil, err
	}
	mapField := plugin.SQLResultFieldMapper(mysqlCatalogFieldType)
	for _, column := range types {
		s.fields = append(s.fields, mapField(column))
	}
	return s, nil
}

type mysqlQueryReadSession struct {
	db      *sql.DB
	tx      *sql.Tx
	rows    *sql.Rows
	binding *plugin.SQLPreparedReadSession
	fields  []datatype.FieldInfo
	offset  int64
	cancel  context.CancelFunc
	closed  bool
}

func (s *mysqlQueryReadSession) ReadBatch(ctx context.Context, limit int) (*plugin.BatchData, error) {
	if s.closed || limit <= 0 {
		return nil, fmt.Errorf("MySQL query read session is closed or batch limit is invalid")
	}
	if err := ctx.Err(); err != nil {
		_ = s.Close(ctx)
		return nil, err
	}
	batch := &plugin.BatchData{Fields: append([]datatype.FieldInfo(nil), s.fields...), Offset: s.offset}
	for len(batch.Rows) < limit && s.rows.Next() {
		if err := ctx.Err(); err != nil {
			_ = s.Close(ctx)
			return nil, err
		}
		values := make([]any, len(s.fields))
		pointers := make([]any, len(values))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := s.rows.Scan(pointers...); err != nil {
			_ = s.Close(ctx)
			return nil, err
		}
		row := make(map[string]any, len(values))
		for i, field := range s.fields {
			value := values[i]
			if bytes, ok := value.([]byte); ok {
				if field.Type == datatype.FieldTypeBytes {
					value = append([]byte{}, bytes...)
				} else {
					value = string(bytes)
				}
			}
			row[field.Name] = value
		}
		batch.Rows = append(batch.Rows, row)
	}
	if err := s.rows.Err(); err != nil {
		_ = s.Close(ctx)
		return nil, err
	}
	if err := s.binding.FinalizeBatch(batch); err != nil {
		_ = s.Close(ctx)
		return nil, err
	}
	s.offset += int64(len(batch.Rows))
	return batch, nil
}

// Rollback also covers early close and cancellation; the session is read-only.
func (s *mysqlQueryReadSession) Close(context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true
	// Cancel before Close so the driver does not drain an arbitrarily large
	// unread result set when protection fails or the consumer stops early.
	if s.cancel != nil {
		s.cancel()
	}
	var result error
	if s.rows != nil {
		if err := s.rows.Close(); !errors.Is(err, context.Canceled) {
			result = err
		}
	}
	if s.tx != nil {
		if err := s.tx.Rollback(); !errors.Is(err, sql.ErrTxDone) {
			result = errors.Join(result, err)
		}
	}
	if s.db != nil {
		result = errors.Join(result, s.db.Close())
	}
	return result
}
