package mysql

import (
	"context"
	"database/sql"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/shared/analytical"
)

func (p *MySQLPlugin) ValidateAnalyticalExecution(ctx context.Context, tx *sql.Tx, sources []plugin.SourceBinding) error {
	return analytical.ValidateMySQLCompatibleExecution(ctx, tx, sources, analytical.MySQLCompatibleExecutionOptions{
		CatalogModel:   p.EngineCatalogModel(),
		IsSystemSchema: p.isSystemSchema,
		Certify:        certifyAnalyticalInstance,
		LoadFields: func(ctx context.Context, tx *sql.Tx, database, table string) ([]datatype.FieldInfo, error) {
			columns, err := mysqlTableColumns(ctx, tx, database, table)
			if err != nil {
				return nil, err
			}
			return mysqlFieldsFromColumns(columns), nil
		},
	})
}
