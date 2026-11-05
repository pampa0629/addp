package clickhouse

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/resume"
)

var (
	_ plugin.TableReadSessionProvider = (*ClickHousePlugin)(nil)
	_ plugin.CatalogTablePageReader   = (*ClickHousePlugin)(nil)
)

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
	return p.openCatalogTableRead(ctx, connInfo, path, plugin.CatalogTablePageOptions{})
}

func (p *ClickHousePlugin) ReadCatalogTablePage(
	ctx context.Context,
	connInfo plugin.ConnectionInfo,
	path plugin.EngineCatalogPath,
	opts plugin.CatalogTablePageOptions,
) (*plugin.BatchData, error) {
	if opts.Limit <= 0 || opts.Offset < 0 || opts.Offset > math.MaxInt {
		return nil, fmt.Errorf("invalid ClickHouse table page")
	}
	if strings.TrimSpace(opts.Predicate) == "" && len(opts.Args) != 0 {
		return nil, fmt.Errorf("ClickHouse table page parameters require a predicate")
	}
	session, err := p.openCatalogTableRead(ctx, connInfo, path, opts)
	if err != nil {
		return nil, err
	}
	defer session.Close(ctx)
	batch, err := session.ReadBatch(ctx, opts.Limit)
	if err == nil {
		batch.Offset = opts.Offset
	}
	return batch, err
}

func (p *ClickHousePlugin) openCatalogTableRead(
	ctx context.Context,
	connInfo plugin.ConnectionInfo,
	path plugin.EngineCatalogPath,
	opts plugin.CatalogTablePageOptions,
) (plugin.TableReadSession, error) {
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
	fields := facts.Table.Fields
	if opts.CheckSchema != nil {
		if err := opts.CheckSchema(fields); err != nil {
			return nil, err
		}
	}
	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		return nil, fmt.Errorf("build ClickHouse read connection: %w", err)
	}
	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		return nil, fmt.Errorf("open ClickHouse read connection: %w", err)
	}
	query := clickhouseDialect().SelectTableSQL("*", segments[0].Name, segments[1].Name, opts.Predicate, "", opts.Limit, int(opts.Offset))
	rows, err := db.QueryContext(ctx, query, opts.Args...)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open ClickHouse table cursor: %w", err)
	}
	boundFields, err := clickhouseBindTableFields(rows, fields)
	if err != nil {
		_ = rows.Close()
		_ = db.Close()
		return nil, err
	}
	return plugin.NewSQLRowsTableReadSession(db, rows, boundFields)
}

func clickhouseBindTableFields(rows *sql.Rows, fields []datatype.FieldInfo) ([]datatype.FieldInfo, error) {
	columns, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("read ClickHouse table cursor schema: %w", err)
	}
	byName := make(map[string]datatype.FieldInfo, len(fields))
	for _, field := range fields {
		byName[field.Name] = field
	}
	bound := make([]datatype.FieldInfo, 0, len(columns))
	for _, column := range columns {
		field, ok := byName[column.Name()]
		if !ok || field.NativeType != column.DatabaseTypeName() {
			return nil, fmt.Errorf("ClickHouse table cursor no longer matches the live column schema")
		}
		bound = append(bound, field)
		delete(byName, column.Name())
	}
	for _, field := range byName {
		if !field.Generated {
			return nil, fmt.Errorf("ClickHouse table cursor is missing a live column")
		}
	}
	return bound, nil
}
