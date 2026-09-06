package oceanbase

import (
	"context"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/shared"
)

func (p *Plugin) tableWriter() shared.MySQLCompatibleTableWriter {
	return shared.MySQLCompatibleTableWriter{
		EngineType:  p.Type(),
		EngineName:  p.DisplayName(),
		DefaultPort: p.DefaultPort(),
	}
}

func (p *Plugin) PrepareTableWrite(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableWriteOptions) error {
	return p.tableWriter().PrepareTableWrite(ctx, connInfo, path, opts)
}

func (p *Plugin) DeleteResource(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath) error {
	return p.tableWriter().DeleteResource(ctx, connInfo, path)
}

func (p *Plugin) OpenTableWriteSession(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableWriteSessionOptions) (plugin.TableWriteSession, error) {
	return p.tableWriter().OpenTableWriteSession(ctx, connInfo, path, opts)
}

func (p *Plugin) PrepareTableUpsert(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.TableUpsertOptions) error {
	return p.tableWriter().PrepareTableUpsert(ctx, connInfo, path, opts)
}

func (p *Plugin) UpsertBatch(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, batch *plugin.BatchData, opts plugin.TableUpsertOptions) error {
	return p.tableWriter().UpsertBatch(ctx, connInfo, path, batch, opts)
}
