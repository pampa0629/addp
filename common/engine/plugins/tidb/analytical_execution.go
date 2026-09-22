package tidb

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/shared/analytical"
)

var _ plugin.AnalyticalSQLExecutionValidator = (*Plugin)(nil)

func (p *Plugin) ValidateAnalyticalExecution(ctx context.Context, tx *sql.Tx, sources []plugin.SourceBinding) error {
	return analytical.ValidateMySQLCompatibleExecution(ctx, tx, sources, analytical.MySQLCompatibleExecutionOptions{
		CatalogModel:   p.EngineCatalogModel(),
		IsSystemSchema: p.isSystemSchema,
		Certify:        certifyTiDBAnalyticalInstance,
		LoadFields:     tidbAnalyticalFields,
	})
}

func tidbAnalyticalFields(ctx context.Context, tx *sql.Tx, database, table string) ([]datatype.FieldInfo, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT column_name, data_type, column_type, numeric_precision, numeric_scale,
		       (is_nullable = 'YES') AS is_nullable, (column_key = 'PRI') AS primary_key
		FROM information_schema.columns
		WHERE table_schema = ? AND table_name = ?
		ORDER BY ordinal_position
	`, database, table)
	if err != nil {
		return nil, fmt.Errorf("query tidb analytical table columns: %w", err)
	}
	defer rows.Close()
	fields := make([]datatype.FieldInfo, 0)
	for rows.Next() {
		var name, dataType, nativeType string
		var precision, scale sql.NullInt64
		var nullable, primaryKey bool
		if err := rows.Scan(&name, &dataType, &nativeType, &precision, &scale, &nullable, &primaryKey); err != nil {
			return nil, fmt.Errorf("scan tidb analytical table column: %w", err)
		}
		field := datatype.FieldInfo{Name: name, Type: tidbCatalogFieldType(nativeType), NativeType: nativeType, Nullable: nullable, PrimaryKey: primaryKey}
		if field.Type == datatype.FieldTypeDecimal && precision.Valid {
			field.Precision = int(precision.Int64)
			if scale.Valid {
				field.Scale = int(scale.Int64)
			}
		}
		if field.Type == datatype.FieldTypeUnknown {
			field.Type = tidbCatalogFieldType(dataType)
		}
		fields = append(fields, field)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tidb analytical table columns: %w", err)
	}
	return plugin.NormalizeFieldInfos(fields), nil
}
