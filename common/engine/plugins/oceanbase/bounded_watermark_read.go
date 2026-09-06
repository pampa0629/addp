package oceanbase

import (
	"context"
	"database/sql"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/shared"
)

func (p *Plugin) OpenBoundedWatermarkRead(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.BoundedWatermarkReadOptions) (plugin.BoundedWatermarkReadSession, error) {
	reader := shared.MySQLCompatibleBoundedWatermarkReader{
		EngineType: p.Type(),
		BuildDSN:   p.BuildDSN,
		DescribeTable: func(ctx context.Context, db *sql.DB, database, table string) (*shared.MySQLCompatibleWatermarkTable, error) {
			return shared.DescribeNonSpatialMySQLCompatibleWatermarkTable(ctx, db, p.Type(), database, table, oceanBaseCatalogFieldType)
		},
	}
	return reader.Open(ctx, connInfo, path, opts)
}
