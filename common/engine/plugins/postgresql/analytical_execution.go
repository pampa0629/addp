package postgresql

import (
	"context"
	"database/sql"
	"errors"
	"sort"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/sqlcompile"
)

func (p *PostgreSQLPlugin) ValidateAnalyticalExecution(ctx context.Context, tx *sql.Tx, sources []plugin.SourceBinding) error {
	if tx == nil {
		return plugin.ErrAnalyticalInvalid
	}
	type boundTable struct {
		name   string
		source plugin.SourceBinding
	}
	tables := make([]boundTable, 0, len(sources))
	for _, source := range sources {
		name, err := (analyticalScanDialect{}).Table(source)
		if err != nil {
			return err
		}
		tables = append(tables, boundTable{name, source})
	}
	sort.Slice(tables, func(i, j int) bool { return tables[i].name < tables[j].name })
	for _, table := range tables {
		if _, err := tx.ExecContext(ctx, "LOCK TABLE "+table.name+" IN ACCESS SHARE MODE"); err != nil {
			var native interface{ SQLState() string }
			if errors.As(err, &native) && (native.SQLState() == "42P01" || native.SQLState() == "3F000") {
				return plugin.ErrAnalyticalPlanChanged
			}
			return err
		}
	}
	report, err := certifyAnalyticalInstance(ctx, tx)
	if err != nil {
		return err
	}
	if !report.Supported {
		return plugin.ErrAnalyticalUnsupported
	}
	for _, table := range tables {
		parts := table.source.Path.Segments
		schema, name := parts[1].Name, parts[2].Name
		var supported bool
		err := tx.QueryRowContext(ctx, `SELECT c.relkind = 'r' AND a.amname = 'heap' AND NOT c.relrowsecurity AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_inherits i WHERE i.inhparent=c.oid OR i.inhrelid=c.oid)
FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace LEFT JOIN pg_catalog.pg_am a ON a.oid=c.relam WHERE n.nspname=$1 AND c.relname=$2`, schema, name).Scan(&supported)
		if errors.Is(err, sql.ErrNoRows) {
			return plugin.ErrAnalyticalPlanChanged
		}
		if err != nil {
			return err
		}
		if !supported {
			return plugin.ErrAnalyticalUnsupported
		}
		columns, err := postgresTableColumns(ctx, tx, schema, name)
		if err != nil {
			return err
		}
		fields := make([]datatype.FieldInfo, 0, len(columns))
		for _, column := range columns {
			fields = append(fields, postgresFieldInfoFromColumn(column))
		}
		if err := sqlcompile.ValidateBoundFields(table.source, fields); err != nil {
			return err
		}
	}
	return nil
}
