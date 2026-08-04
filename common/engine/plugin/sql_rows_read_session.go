package plugin

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
)

// NewSQLRowsTableReadSession adapts one already-running database/sql query to
// the table cursor contract. The query is executed exactly once; callers own
// the engine-specific snapshot/read-only setup before constructing the session.
func NewSQLRowsTableReadSession(db *sql.DB, rows *sql.Rows, fields []datatype.FieldInfo) (TableReadSession, error) {
	if db == nil || rows == nil {
		return nil, fmt.Errorf("SQL table read session requires database and rows")
	}
	columns, err := rows.Columns()
	if err != nil {
		_ = rows.Close()
		_ = db.Close()
		return nil, fmt.Errorf("read SQL cursor columns: %w", err)
	}
	return &sqlRowsTableReadSession{
		db: db, rows: rows, columns: columns,
		fields: alignSQLReadFields(columns, fields),
	}, nil
}

type sqlRowsTableReadSession struct {
	db        *sql.DB
	rows      *sql.Rows
	columns   []string
	fields    []datatype.FieldInfo
	offset    int64
	exhausted bool
	closed    bool
}

func (s *sqlRowsTableReadSession) ReadBatch(_ context.Context, limit int) (*BatchData, error) {
	if s.closed {
		return nil, fmt.Errorf("SQL table read session is closed")
	}
	if limit <= 0 {
		limit = 1000
	}
	batch := &BatchData{Fields: append([]datatype.FieldInfo(nil), s.fields...), Offset: s.offset}
	if s.exhausted {
		return batch, nil
	}
	batch.Rows = make([]map[string]interface{}, 0, limit)
	for len(batch.Rows) < limit {
		if !s.rows.Next() {
			s.exhausted = true
			break
		}
		values := make([]interface{}, len(s.columns))
		pointers := make([]interface{}, len(s.columns))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := s.rows.Scan(pointers...); err != nil {
			return nil, fmt.Errorf("scan SQL cursor row: %w", err)
		}
		row := make(map[string]interface{}, len(s.columns))
		for index, column := range s.columns {
			value := values[index]
			if raw, ok := value.([]byte); ok && s.fields[index].Type != datatype.FieldTypeBytes {
				value = string(raw)
			}
			row[column] = value
		}
		batch.Rows = append(batch.Rows, row)
	}
	if err := s.rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate SQL cursor rows: %w", err)
	}
	s.offset += int64(len(batch.Rows))
	return batch, nil
}

func (s *sqlRowsTableReadSession) Close(context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true
	var closeErr error
	if s.rows != nil {
		closeErr = s.rows.Close()
	}
	if s.db != nil {
		if err := s.db.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	return closeErr
}

func alignSQLReadFields(columns []string, fields []datatype.FieldInfo) []datatype.FieldInfo {
	byName := make(map[string]datatype.FieldInfo, len(fields))
	for _, field := range fields {
		if name := strings.TrimSpace(field.Name); name != "" {
			byName[strings.ToLower(name)] = field
		}
	}
	result := make([]datatype.FieldInfo, 0, len(columns))
	for _, column := range columns {
		field, ok := byName[strings.ToLower(column)]
		if !ok {
			field = datatype.FieldInfo{Type: datatype.FieldTypeUnknown, Nullable: true}
		}
		field.Name = column
		result = append(result, field)
	}
	return result
}
