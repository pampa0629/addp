package clickhouse

import (
	"context"
	"database/sql"
	"errors"
	"reflect"

	chdriver "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
)

var _ plugin.GuardedQueryReadSessionProvider = (*ClickHousePlugin)(nil)

func (*ClickHousePlugin) ControlledReadOnlySQLBoundary() plugin.ControlledReadOnlySQLBoundary {
	return plugin.ControlledReadOnlySQLBoundaryValidatedStatement
}

func (p *ClickHousePlugin) OpenGuardedQueryReadSession(ctx context.Context, prepared plugin.PreparedQuery, authorize plugin.QueryReadAuthorizer) (plugin.GuardedQueryReadSession, error) {
	if authorize == nil {
		return nil, plugin.ErrQueryReadSetUnresolved
	}
	expected, err := prepared.ReadSet(ctx)
	if err != nil {
		return nil, err
	}
	binding, err := plugin.ConsumeSQLPreparedReadSession(prepared, p)
	if err != nil {
		return nil, err
	}
	request := binding.Request
	if request.Options.Limit != 0 || request.Options.Offset != 0 {
		return nil, plugin.ErrQueryReadSetUnresolved
	}
	if err := binding.ValidateSingleStatementExecution(); err != nil {
		return nil, err
	}
	sources, err := p.querySources(binding.ConnectionInfo, request)
	if err != nil {
		return nil, err
	}
	if expected == nil || !reflect.DeepEqual(expected.Paths, sources.readSet.Paths) {
		return nil, plugin.ErrQueryReadSetUnresolved
	}
	dsn, err := plugin.BuildClickHouseDSN(binding.ConnectionInfo, p.DefaultPort(), map[string]string{
		"dial_timeout": "10s", "readonly": "1",
	})
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		return nil, err
	}
	// The driver turns a visible deadline into a native settings change,
	// which a readonly=1 profile rejects. Forward cancellation instead.
	queryCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	stop := context.AfterFunc(ctx, cancel)
	finish := func() {
		stop()
		cancel()
	}
	var conn *sql.Conn
	var rows *sql.Rows
	opened := false
	defer func() {
		if !opened {
			finish()
			if rows != nil {
				_ = rows.Close()
			}
			if conn != nil {
				_ = conn.Close()
			}
			_ = db.Close()
		}
	}()
	conn, err = db.Conn(queryCtx)
	if err != nil {
		return nil, err
	}
	if err := sources.verify(queryCtx, conn, plugin.GetString(binding.ConnectionInfo, "database")); err != nil {
		return nil, err
	}
	if err := authorize(ctx, sources.readSet.Clone()); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if lease, ok := execution.LeaseFromContext(ctx); ok {
		queryCtx = chdriver.Context(queryCtx, chdriver.WithQueryID(lease.ExecutionID))
	}
	rows, err = conn.QueryContext(queryCtx, request.Query, request.Options.Args...)
	if err != nil {
		return nil, err
	}
	types, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}
	mapField := plugin.SQLResultFieldMapper(clickhouseCommonFieldType)
	fields := make([]datatype.FieldInfo, len(types))
	for i, column := range types {
		fields[i] = mapField(column)
	}
	cursor, err := plugin.NewSQLRowsTableReadSession(db, rows, fields)
	if err != nil {
		return nil, err
	}
	opened = true
	return &queryReadSession{cursor: cursor, conn: conn, readSet: sources.readSet, binding: binding, finish: finish}, nil
}

type queryReadSession struct {
	cursor  plugin.TableReadSession
	conn    *sql.Conn
	readSet *plugin.QueryReadSet
	binding *plugin.SQLPreparedReadSession
	finish  func()
}

func (s *queryReadSession) ReadSet(context.Context) (*plugin.QueryReadSet, error) {
	return s.readSet.Clone(), nil
}

func (s *queryReadSession) ReadBatch(ctx context.Context, limit int) (*plugin.BatchData, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, s.finish)
	defer stop()
	batch, err := s.cursor.ReadBatch(ctx, limit)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return batch, s.binding.FinalizeBatch(batch)
}

func (s *queryReadSession) Close(ctx context.Context) error {
	s.finish()
	cursorErr := s.cursor.Close(ctx)
	if errors.Is(cursorErr, context.Canceled) {
		cursorErr = nil
	}
	connErr := s.conn.Close()
	if errors.Is(connErr, sql.ErrConnDone) {
		connErr = nil
	}
	return errors.Join(cursorErr, connErr)
}
