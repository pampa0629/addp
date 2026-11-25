package tidb

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/shared"
	commonquery "github.com/addp/common/query"
)

var _ plugin.GuardedQueryReadSessionProvider = (*Plugin)(nil)

func (p *Plugin) OpenGuardedQueryReadSession(ctx context.Context, prepared plugin.PreparedQuery, authorize plugin.QueryReadAuthorizer) (plugin.GuardedQueryReadSession, error) {
	return shared.OpenGuardedMySQLCompatibleQueryReadSession(ctx, p, p.BuildDSN, tidbCatalogFieldType, nil, prepared, authorize, p.bindQuerySources)
}

func (p *Plugin) bindQuerySources(ctx context.Context, tx *sql.Tx, request plugin.QueryRequest, expected *plugin.QueryReadSet) (*plugin.QueryReadSet, error) {
	var metadataLock, staleness int
	var snapshot string
	if err := tx.QueryRowContext(ctx, "SELECT @@GLOBAL.tidb_enable_metadata_lock, @@SESSION.tidb_read_staleness, @@SESSION.tidb_snapshot").Scan(&metadataLock, &staleness, &snapshot); err != nil {
		return nil, fmt.Errorf("%w: read native TiDB metadata lock state: %w", plugin.ErrQueryReadSetUnresolved, err)
	}
	if metadataLock != 1 || staleness != 0 || snapshot != "" {
		return nil, fmt.Errorf("%w: TiDB requires metadata locking and a current snapshot", plugin.ErrQueryReadSetUnresolved)
	}
	actual, err := p.queryProvenance().ResolveTransactionReadSet(ctx, tx, request)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(expected.Paths, actual.Paths) || len(actual.Paths) > 200 {
		return nil, plugin.ErrQueryReadSetUnresolved
	}
	dialect := commonquery.ForDialect(p.SQLDialect())
	for _, path := range actual.Paths {
		database, table := path.Segments[1].Name, path.Segments[2].Name
		qualified := dialect.QualifiedTable(database, table)
		rows, err := tx.QueryContext(ctx, "SELECT 1 FROM "+qualified+" LIMIT 0")
		if err != nil {
			return nil, fmt.Errorf("%w: bind native TiDB table: %w", plugin.ErrQueryReadSetUnresolved, err)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
		var name, definition string
		if err := tx.QueryRowContext(ctx, "SHOW CREATE TABLE "+qualified).Scan(&name, &definition); err != nil {
			return nil, fmt.Errorf("%w: verify native TiDB table: %w", plugin.ErrQueryReadSetUnresolved, err)
		}
		if name != table || !strings.HasPrefix(definition, "CREATE TABLE ") {
			return nil, plugin.ErrQueryReadSetUnresolved
		}
	}
	return actual, nil
}
