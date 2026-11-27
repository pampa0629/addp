package tidb

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/shared"
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
	return p.queryProvenance().CheckTransactionTables(ctx, tx, request, expected)
}
