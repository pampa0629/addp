package plugin

import "github.com/addp/common/datatype"

func FieldInfosFromColumns(columns []ColumnInfo) []datatype.FieldInfo {
	if len(columns) == 0 {
		return nil
	}
	fields := make([]datatype.FieldInfo, 0, len(columns))
	for _, col := range columns {
		fields = append(fields, datatype.FieldInfo{
			Name:       col.ColumnName,
			Type:       datatype.ParseFieldType(col.DataType),
			NativeType: col.DataType,
			Nullable:   col.IsNullable,
			PrimaryKey: col.IsPrimaryKey,
			Comment:    col.Comment,
		})
	}
	return fields
}

func ColumnInfosFromFields(fields []datatype.FieldInfo) []ColumnInfo {
	if len(fields) == 0 {
		return nil
	}
	columns := make([]ColumnInfo, 0, len(fields))
	for _, field := range fields {
		dataType := field.NativeType
		if dataType == "" {
			dataType = string(field.Type)
		}
		columns = append(columns, ColumnInfo{
			ColumnName:   field.Name,
			DataType:     dataType,
			IsNullable:   field.Nullable,
			IsPrimaryKey: field.PrimaryKey,
			Comment:      field.Comment,
		})
	}
	return columns
}
