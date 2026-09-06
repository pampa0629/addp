package mysql

import (
	"context"
	"database/sql"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/shared"
	"github.com/addp/common/format"
)

func (p *MySQLPlugin) OpenBoundedWatermarkRead(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.BoundedWatermarkReadOptions) (plugin.BoundedWatermarkReadSession, error) {
	reader := shared.MySQLCompatibleBoundedWatermarkReader{
		EngineType: p.Type(),
		BuildDSN:   p.serverDSN,
		DescribeTable: func(ctx context.Context, db *sql.DB, database, table string) (*shared.MySQLCompatibleWatermarkTable, error) {
			columns, err := mysqlTableColumns(ctx, db, database, table)
			if err != nil {
				return nil, err
			}
			fields, spatialInfo, err := mysqlWatermarkFields(columns)
			if err != nil {
				return nil, err
			}
			projection, err := mysqlSelectExpr(columns, fields, nil, format.GeometryEncodingEWKB)
			if err != nil {
				return nil, err
			}
			cursorColumns := make([]shared.MySQLCompatibleWatermarkColumn, 0, len(columns))
			for _, column := range columns {
				cursorColumns = append(cursorColumns, shared.MySQLCompatibleWatermarkColumn{
					Name: column.Name, NativeType: mysqlColumnNativeType(column), Type: mysqlCommonFieldType(column), Nullable: column.Nullable,
				})
			}
			return &shared.MySQLCompatibleWatermarkTable{Fields: fields, SpatialInfo: spatialInfo, Columns: cursorColumns, Projection: projection}, nil
		},
		DecodeValue: func(column string, value interface{}, fields []datatype.FieldInfo, spatialInfo *datatype.SpatialInfo) (interface{}, error) {
			return mysqlReadValue(column, value, fields, spatialInfo, format.GeometryEncodingEWKB)
		},
	}
	return reader.Open(ctx, connInfo, path, opts)
}

func mysqlWatermarkFields(columns []mysqlColumnInfo) ([]datatype.FieldInfo, *datatype.SpatialInfo, error) {
	fields := make([]datatype.FieldInfo, 0, len(columns))
	spatialRows := make([]mysqlSpatialColumnRow, 0)
	for index, column := range columns {
		field := mysqlFieldInfoFromColumn(column)
		field.OrdinalPosition = index + 1
		if datatype.IsSpatialFieldType(field.Type) {
			spatialRows = append(spatialRows, mysqlSpatialColumnRow{Name: column.Name, DataType: column.DataType, SRSID: column.SRSID, Nullable: column.Nullable})
		}
		fields = append(fields, field)
	}
	return fields, buildMySQLSpatialInfo(spatialRows, nil, nil), nil
}

func mysqlFieldInfoFromColumn(column mysqlColumnInfo) datatype.FieldInfo {
	field := datatype.FieldInfo{Name: column.Name, Type: mysqlCommonFieldType(column), NativeType: mysqlColumnNativeType(column), Nullable: column.Nullable}
	if column.NumericPrecision.Valid {
		field.Precision = int(column.NumericPrecision.Int64)
	}
	if column.NumericScale.Valid {
		field.Scale = int(column.NumericScale.Int64)
	}
	return field
}
