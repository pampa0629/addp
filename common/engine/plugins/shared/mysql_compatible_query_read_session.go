package shared

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

type mysqlCompatibleSourceBinder func(context.Context, *sql.Tx, plugin.QueryRequest, *plugin.QueryReadSet) (*plugin.QueryReadSet, error)

func OpenGuardedMySQLCompatibleQueryReadSession(
	ctx context.Context,
	provider plugin.ControlledReadOnlySQLProvider,
	buildDSN func(plugin.ConnectionInfo) (string, error),
	mapType func(string) datatype.FieldType,
	decode func(datatype.FieldInfo, any) (any, error),
	prepared plugin.PreparedQuery,
	authorize plugin.QueryReadAuthorizer,
	bindSources mysqlCompatibleSourceBinder,
) (plugin.GuardedQueryReadSession, error) {
	if authorize == nil || bindSources == nil {
		return nil, plugin.ErrQueryReadSetUnresolved
	}
	session, err := openMySQLCompatibleQueryReadSession(ctx, provider, buildDSN, mapType, decode, prepared, authorize, bindSources)
	if err != nil {
		return nil, err
	}
	return &guardedMySQLCompatibleQueryReadSession{mysqlCompatibleQueryReadSession: session}, nil
}

// OpenGuardedMySQLCompatibleStatementReadSession uses a private connection for
// both catalog verification and the validated statement, without a transaction.
func OpenGuardedMySQLCompatibleStatementReadSession(
	ctx context.Context,
	provider plugin.ControlledReadOnlySQLProvider,
	buildDSN func(plugin.ConnectionInfo) (string, error),
	mapField func(*sql.ColumnType) datatype.FieldInfo,
	prepared plugin.PreparedQuery,
	authorize plugin.QueryReadAuthorizer,
	bindSources func(context.Context, *sql.Conn, plugin.QueryRequest) (*plugin.QueryReadSet, error),
) (plugin.GuardedQueryReadSession, error) {
	if authorize == nil || provider.ControlledReadOnlySQLBoundary() != plugin.ControlledReadOnlySQLBoundaryValidatedStatement {
		return nil, plugin.ErrQueryReadSetUnresolved
	}
	expected, err := prepared.ReadSet(ctx)
	if err != nil {
		return nil, err
	}
	binding, err := plugin.ConsumeSQLPreparedReadSession(prepared, provider)
	if err != nil {
		return nil, err
	}
	request := binding.Request
	if expected == nil || !request.Options.ReadOnly || request.Options.Limit != 0 || request.Options.Offset != 0 {
		return nil, plugin.ErrQueryReadSetUnresolved
	}
	if err := commonquery.RequireReadOnly(request.Query); err != nil {
		return nil, err
	}
	if err := binding.ValidateSingleStatementExecution(); err != nil {
		return nil, err
	}
	dsn, err := buildDSN(binding.ConnectionInfo)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	s := &mysqlCompatibleQueryReadSession{db: db, binding: binding, cancel: cancel}
	opened := false
	defer func() {
		if !opened {
			_ = s.Close(ctx)
		}
	}()
	s.conn, err = db.Conn(sessionCtx)
	if err != nil {
		return nil, err
	}
	actual, err := bindSources(sessionCtx, s.conn, request)
	if err != nil {
		return nil, err
	}
	if actual == nil || !reflect.DeepEqual(expected.Paths, actual.Paths) {
		return nil, plugin.ErrQueryReadSetUnresolved
	}
	if err := authorize(sessionCtx, actual.Clone()); err != nil {
		return nil, err
	}
	s.readSet = actual
	if err := s.openCursor(sessionCtx, s.conn, mapField); err != nil {
		return nil, err
	}
	opened = true
	return &guardedMySQLCompatibleQueryReadSession{mysqlCompatibleQueryReadSession: s}, nil
}

func OpenMySQLCompatibleQueryReadSession(
	ctx context.Context,
	provider plugin.ControlledReadOnlySQLProvider,
	buildDSN func(plugin.ConnectionInfo) (string, error),
	mapType func(string) datatype.FieldType,
	decode func(datatype.FieldInfo, any) (any, error),
	prepared plugin.PreparedQuery,
) (plugin.QueryReadSession, error) {
	session, err := openMySQLCompatibleQueryReadSession(ctx, provider, buildDSN, mapType, decode, prepared, nil, nil)
	if err != nil {
		return nil, err
	}
	return session, nil
}

// OpenMySQLCompatibleQueryReadSession streams one prepared query in one
// transaction. A guarded caller supplies its Provider's native binding and
// receives the exact bound sources before any business result cursor opens.
func openMySQLCompatibleQueryReadSession(
	ctx context.Context,
	provider plugin.ControlledReadOnlySQLProvider,
	buildDSN func(plugin.ConnectionInfo) (string, error),
	mapType func(string) datatype.FieldType,
	decode func(datatype.FieldInfo, any) (any, error),
	prepared plugin.PreparedQuery,
	authorize plugin.QueryReadAuthorizer,
	bindSources mysqlCompatibleSourceBinder,
) (*mysqlCompatibleQueryReadSession, error) {
	var expected *plugin.QueryReadSet
	if authorize != nil {
		var err error
		expected, err = prepared.ReadSet(ctx)
		if err != nil {
			return nil, err
		}
		if expected == nil {
			return nil, plugin.ErrQueryReadSetUnresolved
		}
	}
	binding, err := plugin.ConsumeSQLPreparedReadSession(prepared, provider)
	if err != nil {
		return nil, err
	}
	request := binding.Request
	if !request.Options.ReadOnly || request.Options.Limit != 0 || request.Options.Offset != 0 {
		return nil, fmt.Errorf("query read session requires read_only and no preview limit or offset")
	}
	var db interface {
		BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
		Close() error
	}
	var borrowed *plugin.SQLReadConnection
	if version := plugin.PoolEngineVersion(ctx, request.EngineID); authorize != nil && provider.Type() == "mysql" && version > 0 {
		pool, err := plugin.GetOrCreatePoolFromFactory(&plugin.Engine{ID: request.EngineID, Version: version, EngineType: provider.Type(), ConnectionInfo: binding.ConnectionInfo}, nil)
		if err != nil {
			return nil, err
		}
		native, err := pool.DB()
		if err != nil {
			return nil, err
		}
		borrowed, err = plugin.AcquireSQLReadConnection(ctx, native, provider)
		if err != nil {
			return nil, err
		}
		db = borrowed
	} else {
		dsn, err := buildDSN(binding.ConnectionInfo)
		if err != nil {
			return nil, err
		}
		db, err = sql.Open("mysql", dsn)
		if err != nil {
			return nil, err
		}
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	s := &mysqlCompatibleQueryReadSession{db: db, binding: binding, cancel: cancel, decode: decode}
	closeAfterSQLError := func(err error) error {
		_ = s.Close(ctx)
		if borrowed != nil {
			return plugin.SQLExecutionError(err)
		}
		return err
	}
	s.tx, err = plugin.BeginControlledReadOnlySQLTransactionWith(sessionCtx, db.BeginTx, provider.SQLDialect(), provider.ControlledReadOnlySQLBoundary(), sql.LevelRepeatableRead)
	if err != nil {
		return nil, closeAfterSQLError(err)
	}
	if authorize != nil {
		actual, err := bindSources(sessionCtx, s.tx, request, expected)
		if err != nil {
			return nil, closeAfterSQLError(err)
		}
		if err := authorize(sessionCtx, actual.Clone()); err != nil {
			_ = s.Close(ctx)
			return nil, err
		}
		s.readSet = actual
	}
	if err = binding.ValidateExecution(sessionCtx, s.tx); err != nil {
		return nil, closeAfterSQLError(err)
	}
	if borrowed != nil {
		if err := borrowed.BeforeQuery(sessionCtx, s.tx, request.Query); err != nil {
			return nil, closeAfterSQLError(err)
		}
	}
	if err := s.openCursor(sessionCtx, s.tx, plugin.SQLResultFieldMapper(mapType)); err != nil {
		return nil, closeAfterSQLError(err)
	}
	return s, nil
}

func (s *mysqlCompatibleQueryReadSession) openCursor(ctx context.Context, conn mysqlCompatibleQueryConnection, mapField func(*sql.ColumnType) datatype.FieldInfo) error {
	request := s.binding.Request
	var err error
	s.rows, err = conn.QueryContext(ctx, request.Query, request.Options.Args...)
	if err != nil {
		return err
	}
	types, err := s.rows.ColumnTypes()
	if err != nil {
		return err
	}
	for _, column := range types {
		s.fields = append(s.fields, mapField(column))
	}
	return nil
}

type mysqlCompatibleQueryReadSession struct {
	db        interface{ Close() error }
	tx        *sql.Tx
	conn      *sql.Conn
	readSet   *plugin.QueryReadSet
	rows      *sql.Rows
	binding   *plugin.SQLPreparedReadSession
	fields    []datatype.FieldInfo
	offset    int64
	cancel    context.CancelFunc
	exhausted bool
	closed    bool
	decode    func(datatype.FieldInfo, any) (any, error)
}

type guardedMySQLCompatibleQueryReadSession struct {
	*mysqlCompatibleQueryReadSession
}

func (s *guardedMySQLCompatibleQueryReadSession) ReadSet(context.Context) (*plugin.QueryReadSet, error) {
	return s.readSet.Clone(), nil
}

func (s *mysqlCompatibleQueryReadSession) ReadBatch(ctx context.Context, limit int) (*plugin.BatchData, error) {
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
			value := plugin.SQLQueryValue(field, values[i])
			if s.decode != nil {
				var err error
				value, err = s.decode(field, value)
				if err != nil {
					_ = s.Close(ctx)
					return nil, err
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
	s.exhausted = len(batch.Rows) < limit
	s.offset += int64(len(batch.Rows))
	return batch, nil
}

// Rollback also covers early close and cancellation; the session is read-only.
func (s *mysqlCompatibleQueryReadSession) Close(context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true
	// Cancel before Close so the driver does not drain an arbitrarily large
	// unread result set when protection fails or the consumer stops early.
	defer s.cancel()
	if s.rows != nil && !s.exhausted {
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
	if s.conn != nil {
		result = errors.Join(result, s.conn.Close())
	}
	if s.db != nil {
		result = errors.Join(result, s.db.Close())
	}
	return result
}
