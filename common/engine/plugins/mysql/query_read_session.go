package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/shared"
)

var _ plugin.GuardedQueryReadSessionProvider = (*MySQLPlugin)(nil)

func (p *MySQLPlugin) OpenGuardedQueryReadSession(ctx context.Context, prepared plugin.PreparedQuery, authorize plugin.QueryReadAuthorizer) (plugin.GuardedQueryReadSession, error) {
	return shared.OpenGuardedMySQLCompatibleQueryReadSession(ctx, p, p.BuildDSN, mysqlCatalogFieldType, p.DecodeSQLQueryValue, prepared, authorize, p.bindQuerySources)
}

// OpenQueryReadSession consumes the already bound, one-shot plan. The driver
// streams a single result set; batch size never changes the user's SQL.
func (p *MySQLPlugin) OpenQueryReadSession(ctx context.Context, prepared plugin.PreparedQuery) (plugin.QueryReadSession, error) {
	return shared.OpenMySQLCompatibleQueryReadSession(ctx, p, p.BuildDSN, mysqlCatalogFieldType, p.DecodeSQLQueryValue, prepared)
}

func (p *MySQLPlugin) bindQuerySources(ctx context.Context, tx *sql.Tx, request plugin.QueryRequest, expected *plugin.QueryReadSet) (*plugin.QueryReadSet, error) {
	actual, err := p.queryProvenance().ResolveTransactionReadSet(ctx, tx, request)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(expected.Paths, actual.Paths) || len(actual.Paths) > 200 {
		return nil, plugin.ErrQueryReadSetUnresolved
	}
	for _, path := range actual.Paths {
		database, table, err := mysqlTablePathParts(path)
		if err != nil {
			return nil, err
		}
		bound, err := lockMySQLInnoDBBaseTable(ctx, tx, database, table)
		if err != nil {
			return nil, fmt.Errorf("%w: bind native MySQL table: %w", plugin.ErrQueryReadSetUnresolved, err)
		}
		if !bound {
			return nil, plugin.ErrQueryReadSetUnresolved
		}
	}
	return actual, nil
}
