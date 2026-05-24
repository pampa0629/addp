package datatype

import (
	"strings"

	commonJSON "github.com/addp/common/jsonmap"
)

// TableInfoFromAttributes restores common table facts from attributes.type_info.table.
func TableInfoFromAttributes(attrs map[string]interface{}, fallbackName string) *TableInfo {
	tableAttrs := commonJSON.Section(attrs, "type_info.table")
	return TableInfoFromTableAttributes(tableAttrs, fallbackName)
}

// TableInfoFromTableAttributes restores common table facts from a table attribute map.
func TableInfoFromTableAttributes(tableAttrs map[string]interface{}, fallbackName string) *TableInfo {
	if len(tableAttrs) == 0 {
		return nil
	}
	var info TableInfo
	if err := commonJSON.DecodeStruct(tableAttrs, &info); err != nil {
		return nil
	}
	for i := range info.Fields {
		info.Fields[i] = normalizeFieldInfo(info.Fields[i])
	}
	if len(info.Fields) == 0 {
		return nil
	}
	info.Name = strings.TrimSpace(info.Name)
	if info.Name == "" {
		info.Name = fallbackName
	}
	info.Kind = strings.TrimSpace(info.Kind)
	info.Comment = strings.TrimSpace(info.Comment)
	info.Native = cloneInterfaceMap(info.Native)
	if info.RowCount != nil && *info.RowCount <= 0 {
		info.RowCount = nil
	}
	if info.SizeBytes != nil && *info.SizeBytes <= 0 {
		info.SizeBytes = nil
	}
	return &info
}

// TableInfoAttributes converts common table facts to attributes.type_info.table.
func TableInfoAttributes(info *TableInfo) map[string]interface{} {
	return commonJSON.MapFromStruct(info)
}

// FieldInfosFromAttributes restores common field facts from attributes arrays.
func FieldInfosFromAttributes(value interface{}) []FieldInfo {
	items := commonJSON.InterfaceSlice(value)
	fields := make([]FieldInfo, 0, len(items))
	for _, item := range items {
		var field FieldInfo
		if err := commonJSON.DecodeStruct(commonJSON.InterfaceMap(item), &field); err != nil {
			continue
		}
		field = normalizeFieldInfo(field)
		if field.Name == "" {
			continue
		}
		fields = append(fields, field)
	}
	return fields
}

// FieldInfoAttributes converts common field facts to attributes arrays.
func FieldInfoAttributes(fields []FieldInfo) []map[string]interface{} {
	fieldsData := make([]map[string]interface{}, 0, len(fields))
	for _, f := range fields {
		if attrs := commonJSON.MapFromStruct(f); len(attrs) > 0 {
			fieldsData = append(fieldsData, attrs)
		}
	}
	return fieldsData
}

func normalizeFieldInfo(field FieldInfo) FieldInfo {
	field.Name = strings.TrimSpace(field.Name)
	field.NativeType = strings.TrimSpace(field.NativeType)
	field.Comment = strings.TrimSpace(field.Comment)
	field.DefaultExpression = strings.TrimSpace(field.DefaultExpression)
	field.GenerationExpression = strings.TrimSpace(field.GenerationExpression)
	field.Type = ParseFieldType(string(field.Type))
	if IsSpatialFieldType(field.Type) {
		field.Type = FieldTypeGeometry
	}
	return field
}
