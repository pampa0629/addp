package postgresql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
)

var _ plugin.QueryReadSessionProvider = (*PostgreSQLPlugin)(nil)

func (p *PostgreSQLPlugin) OpenQueryReadSession(ctx context.Context, prepared plugin.PreparedQuery) (plugin.QueryReadSession, error) {
	session, err := p.openQueryReadSession(ctx, prepared, nil)
	if err != nil {
		return nil, err
	}
	return session, nil
}

func (p *PostgreSQLPlugin) openQueryReadSession(ctx context.Context, prepared plugin.PreparedQuery, authorize plugin.QueryReadAuthorizer) (*postgresQueryReadSession, error) {
	var expected *plugin.QueryReadSet
	var err error
	if authorize != nil {
		expected, err = prepared.ReadSet(ctx)
		if err != nil {
			return nil, err
		}
		if expected == nil {
			return nil, plugin.ErrQueryReadSetUnresolved
		}
	}
	binding, err := plugin.ConsumeSQLPreparedReadSession(prepared, p)
	if err != nil {
		return nil, err
	}
	connInfo, request := binding.ConnectionInfo, binding.Request
	if err := validatePostgresQueryReadSession(request); err != nil {
		return nil, err
	}
	var db postgresTableSessionConnection
	if version := plugin.PoolEngineVersion(ctx, request.EngineID); authorize != nil && p.Type() == "postgresql" && version > 0 {
		db, err = p.borrowReadConnection(ctx, connInfo, request.EngineID, version)
	} else {
		db, err = p.openQueryDB(connInfo)
	}
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL query read database: %w", err)
	}
	isolation := sql.LevelRepeatableRead
	if authorize != nil {
		isolation = sql.LevelReadCommitted
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: isolation})
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("begin PostgreSQL query read transaction: %w", err)
	}
	var readSet *plugin.QueryReadSet
	if authorize != nil {
		catalog := &postgresDatabaseReadCatalog{db: tx, gaussDB: p.Type() == "gaussdb"}
		plan, err := prepareGuardedPostgresQuery(ctx, request, catalog)
		if err != nil {
			_ = tx.Rollback()
			_ = db.Close()
			return nil, err
		}
		actual := plan.readSet
		if actual == nil || !(len(expected.Paths) == 0 && len(actual.Paths) == 0 || reflect.DeepEqual(expected.Paths, actual.Paths)) {
			_ = tx.Rollback()
			_ = db.Close()
			return nil, plugin.ErrQueryReadSetUnresolved
		}
		for _, path := range actual.Paths {
			if _, err := p.checkOrdinaryTable(ctx, tx, path, false); err != nil {
				if ctx.Err() != nil {
					err = plugin.SQLExecutionError(errors.Join(ctx.Err(), err))
				}
				_ = tx.Rollback()
				_ = db.Close()
				return nil, err
			}
		}
		if len(plan.integerCastColumns) > 0 {
			table := actual.Paths[0].Segments
			if err := verifyGuardedIntegerCastColumns(ctx, tx, table[1].Name, table[2].Name, plan.integerCastColumns); err != nil {
				_ = tx.Rollback()
				_ = db.Close()
				return nil, err
			}
		}
		if plan.dictionarySchema != "" {
			// SQL overloads resolve the extension's own dictionary in its installation schema.
			path := "pg_catalog, " + commonquery.ForDialect("postgresql").QuoteIdentifier(plan.dictionarySchema) + ", pg_temp"
			if _, err := tx.ExecContext(ctx, "SELECT pg_catalog.set_config('search_path', $1, true)", path); err != nil {
				_ = tx.Rollback()
				_ = db.Close()
				return nil, err
			}
		}
		if err := authorize(ctx, actual.Clone()); err != nil {
			_ = tx.Rollback()
			_ = db.Close()
			return nil, err
		}
		request.Query = plan.sql
		readSet = actual
	}
	if err := binding.ValidateExecution(ctx, tx); err != nil {
		_ = tx.Rollback()
		_ = db.Close()
		return nil, err
	}
	const cursor = "addp_query_read_cursor"
	_, err = tx.ExecContext(ctx, "DECLARE "+cursor+" NO SCROLL CURSOR FOR "+request.Query, request.Options.Args...)
	if err != nil {
		_ = tx.Rollback()
		_ = db.Close()
		if ctx.Err() != nil {
			err = plugin.SQLExecutionError(errors.Join(ctx.Err(), err))
		}
		return nil, fmt.Errorf("declare PostgreSQL query read session: %w", err)
	}
	if authorize == nil {
		readSet = p.observeSessionReadSet(ctx, tx, request)
	}
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
	fields := make([]datatype.FieldInfo, len(columnTypes))
	for index, columnType := range columnTypes {
		columns[index] = columnType.Name()
		fields[index] = SQLResultField(columnType)
	}
	if err := rows.Close(); err != nil {
		_ = tx.Rollback()
		_ = db.Close()
		return nil, err
	}
	return &postgresQueryReadSession{db: db, tx: tx, cursor: cursor, readSet: readSet, columns: columns, fields: fields, binding: binding, provider: p}, nil
}

// Called after the ordinary table proof locks the same relation used by the
// cursor. Casts of extension columns cannot use this native integer path.
func verifyGuardedIntegerCastColumns(ctx context.Context, tx *sql.Tx, schema, table string, columns []string) error {
	for _, column := range columns {
		var native bool
		err := tx.QueryRowContext(ctx, `SELECT EXISTS (
         SELECT 1 FROM pg_catalog.pg_attribute a
         JOIN pg_catalog.pg_class c ON c.oid=a.attrelid
         JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
         JOIN pg_catalog.pg_type t ON t.oid=a.atttypid
         JOIN pg_catalog.pg_namespace tn ON tn.oid=t.typnamespace
         WHERE n.nspname=$1 AND c.relname=$2 AND a.attname=$3 AND a.attnum>0 AND NOT a.attisdropped
           AND tn.nspname='pg_catalog' AND t.typname IN ('int2','int4','int8'))`, schema, table, column).Scan(&native)
		if err != nil {
			return postgresQueryReadSetCatalogError("verify cursor cast source type", err)
		}
		if !native {
			return postgresQueryReadSetError("integer cast source is not a native integer column")
		}
	}
	return nil
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
	db       postgresTableSessionConnection
	tx       *sql.Tx
	cursor   string
	readSet  *plugin.QueryReadSet
	columns  []string
	fields   []datatype.FieldInfo
	binding  *plugin.SQLPreparedReadSession
	offset   int64
	provider *PostgreSQLPlugin
	closed   bool
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
		if ctx.Err() != nil {
			err = plugin.SQLExecutionError(errors.Join(ctx.Err(), err))
		}
		return nil, fmt.Errorf("fetch PostgreSQL query read rows: %w", err)
	}
	defer rows.Close()
	batch := &plugin.BatchData{Fields: append([]datatype.FieldInfo(nil), s.fields...), Offset: s.offset, Rows: make([]map[string]interface{}, 0, limit)}
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
			value, err := s.provider.DecodeSQLQueryValue(s.fields[index], plugin.SQLQueryValue(s.fields[index], values[index]))
			if err != nil {
				_ = rows.Close()
				_ = s.abort()
				return nil, err
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
	if err := s.binding.FinalizeBatch(batch); err != nil {
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
