package mysql

import (
	"context"
	"database/sql"
	"errors"
	"sort"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/sqlcompile"
	mysqlDriver "github.com/go-sql-driver/mysql"
)

func (p *MySQLPlugin) ValidateAnalyticalExecution(ctx context.Context, tx *sql.Tx, sources []plugin.SourceBinding) error {
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
		// A zero-row read acquires MDL through transaction end without reading
		// business data or relying on a separate metadata connection.
		rows, err := tx.QueryContext(ctx, "SELECT 1 FROM "+table.name+" LIMIT 0")
		if err != nil {
			var native *mysqlDriver.MySQLError
			if errors.As(err, &native) && (native.Number == 1146 || native.Number == 1049) {
				return plugin.ErrAnalyticalPlanChanged
			}
			return err
		}
		if err = rows.Close(); err != nil {
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
		database, name := parts[1].Name, parts[2].Name
		var kind, engine string
		err := tx.QueryRowContext(ctx, `SELECT TABLE_TYPE, COALESCE(ENGINE,'') FROM information_schema.tables WHERE TABLE_SCHEMA=? AND TABLE_NAME=?`, database, name).Scan(&kind, &engine)
		if errors.Is(err, sql.ErrNoRows) {
			return plugin.ErrAnalyticalPlanChanged
		}
		if err != nil {
			return err
		}
		if kind != "BASE TABLE" || engine != "InnoDB" {
			return plugin.ErrAnalyticalUnsupported
		}
		columns, err := mysqlTableColumns(ctx, tx, database, name)
		if err != nil {
			return err
		}
		if err := sqlcompile.ValidateBoundFields(table.source, mysqlFieldsFromColumns(columns)); err != nil {
			return err
		}
	}
	return nil
}
