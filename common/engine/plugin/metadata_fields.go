package plugin

import commonJSON "github.com/addp/common/jsonmap"

func FieldInfosFromColumns(columns []ColumnInfo) []FieldInfo {
	if len(columns) == 0 {
		return nil
	}
	fields := make([]FieldInfo, 0, len(columns))
	for _, col := range columns {
		fields = append(fields, FieldInfo{
			Name:       col.ColumnName,
			Type:       col.DataType,
			Nullable:   col.IsNullable,
			PrimaryKey: col.IsPrimaryKey,
			Comment:    col.Comment,
			Attributes: map[string]interface{}{"native_type": col.DataType},
		})
	}
	return fields
}

func ColumnInfosFromFields(fields []FieldInfo) []ColumnInfo {
	if len(fields) == 0 {
		return nil
	}
	columns := make([]ColumnInfo, 0, len(fields))
	for _, field := range fields {
		dataType := commonJSON.InterfaceString(field.Attributes["native_type"])
		if dataType == "" {
			dataType = field.Type
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
