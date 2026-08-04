package doris

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/resume"
)

var _ plugin.TableReadSessionProvider = (*DorisPlugin)(nil)

func (p *DorisPlugin) OpenTableReadSession(
	ctx context.Context,
	connInfo plugin.ConnectionInfo,
	path plugin.CatalogPath,
	opts plugin.TableReadSessionOptions,
) (plugin.TableReadSession, error) {
	if err := resume.RejectUnsupported(opts.ResumeMarker, "doris.table_read_session"); err != nil {
		return nil, err
	}
	if opts.Query != "" || len(opts.Args) != 0 {
		return nil, fmt.Errorf("Doris table read session requires a catalog table path")
	}
	database, table, err := dorisTablePathParts(path)
	if err != nil {
		return nil, err
	}
	facts, err := p.DescribeCatalogFacts(ctx, connInfo, path, plugin.CatalogFactsOptions{})
	if err != nil {
		return nil, err
	}
	if facts == nil || facts.Table == nil {
		return nil, fmt.Errorf("Doris table read requires table catalog facts")
	}
	dsn, err := p.serverDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("build Doris read connection: %w", err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open Doris read connection: %w", err)
	}
	query := dorisDialect().SelectTableSQL("*", database, table, "", "", 0, 0)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open Doris table cursor: %w", err)
	}
	return plugin.NewSQLRowsTableReadSession(db, rows, facts.Table.Fields)
}
