package clickhouse

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
	"github.com/addp/common/resume"
)

var _ plugin.TableReadSessionProvider = (*ClickHousePlugin)(nil)

func (p *ClickHousePlugin) OpenTableReadSession(
	ctx context.Context,
	connInfo plugin.ConnectionInfo,
	path plugin.EngineCatalogPath,
	opts plugin.TableReadSessionOptions,
) (plugin.TableReadSession, error) {
	if err := resume.RejectUnsupported(opts.ResumeMarker, "clickhouse.table_read_session"); err != nil {
		return nil, err
	}
	if opts.Query != "" || len(opts.Args) != 0 {
		return nil, fmt.Errorf("ClickHouse table read session requires a catalog table path")
	}
	segments := plugin.EngineCatalogPathWithoutRoot(path).Segments
	if len(segments) != 2 || segments[0].Name == "" || segments[1].Name == "" {
		return nil, fmt.Errorf("ClickHouse table read requires database/table catalog path")
	}
	facts, err := p.DescribeEngineCatalogFacts(ctx, connInfo, path, plugin.EngineCatalogFactsOptions{})
	if err != nil {
		return nil, err
	}
	if facts == nil || facts.Table == nil {
		return nil, fmt.Errorf("ClickHouse table read requires table catalog facts")
	}
	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("build ClickHouse read connection: %w", err)
	}
	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		return nil, fmt.Errorf("open ClickHouse read connection: %w", err)
	}
	query := commonquery.ForDialect(p.SQLDialect()).SelectTableSQL("*", segments[0].Name, segments[1].Name, "", "", 0, 0)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open ClickHouse table cursor: %w", err)
	}
	return plugin.NewSQLRowsTableReadSession(db, rows, facts.Table.Fields)
}
