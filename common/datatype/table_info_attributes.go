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
	fields := FieldInfosFromAttributes(tableAttrs["fields"])
	if len(fields) == 0 {
		return nil
	}
	name := strings.TrimSpace(commonJSON.InterfaceString(tableAttrs["name"]))
	if name == "" {
		name = fallbackName
	}
	info := &TableInfo{
		Name:       name,
		Kind:       strings.TrimSpace(commonJSON.InterfaceString(tableAttrs["kind"])),
		Comment:    strings.TrimSpace(commonJSON.InterfaceString(tableAttrs["comment"])),
		Fields:     fields,
		PrimaryKey: stringSliceFromAttribute(tableAttrs["primary_key"]),
		Native:     cloneInterfaceMap(commonJSON.InterfaceMap(tableAttrs["native"])),
		CreatedAt:  commonJSON.InterfaceTimePtr(tableAttrs["created_at"]),
		UpdatedAt:  commonJSON.InterfaceTimePtr(tableAttrs["updated_at"]),
	}
	if rowCount := commonJSON.InterfaceInt64(tableAttrs["row_count"]); rowCount > 0 {
		info.RowCount = &rowCount
	}
	if sizeBytes := commonJSON.InterfaceInt64(tableAttrs["size_bytes"]); sizeBytes > 0 {
		info.SizeBytes = &sizeBytes
	}
	return info
}

// TableInfoAttributes converts common table facts to attributes.type_info.table.
func TableInfoAttributes(info *TableInfo) map[string]interface{} {
	if info == nil {
		return nil
	}
	attrs := map[string]interface{}{}
	if info.Name != "" {
		attrs["name"] = info.Name
	}
	if info.Kind != "" {
		attrs["kind"] = info.Kind
	}
	if info.Comment != "" {
		attrs["comment"] = info.Comment
	}
	if info.RowCount != nil {
		attrs["row_count"] = *info.RowCount
	}
	if info.SizeBytes != nil {
		attrs["size_bytes"] = *info.SizeBytes
	}
	if info.CreatedAt != nil {
		attrs["created_at"] = info.CreatedAt
	}
	if info.UpdatedAt != nil {
		attrs["updated_at"] = info.UpdatedAt
	}
	if len(info.Fields) > 0 {
		attrs["fields"] = FieldInfoAttributes(info.Fields)
	}
	if len(info.PrimaryKey) > 0 {
		attrs["primary_key"] = append([]string(nil), info.PrimaryKey...)
	}
	if len(info.Native) > 0 {
		attrs["native"] = cloneInterfaceMap(info.Native)
	}
	if len(attrs) == 0 {
		return nil
	}
	return attrs
}

// FieldInfosFromAttributes restores common field facts from attributes arrays.
func FieldInfosFromAttributes(value interface{}) []FieldInfo {
	items := commonJSON.InterfaceSlice(value)
	fields := make([]FieldInfo, 0, len(items))
	for _, item := range items {
		attrs := commonJSON.InterfaceMap(item)
		name := strings.TrimSpace(commonJSON.InterfaceString(attrs["name"]))
		if name == "" {
			continue
		}
		fieldType := ParseFieldType(commonJSON.InterfaceString(attrs["type"]))
		if IsSpatialFieldType(fieldType) {
			fieldType = FieldTypeGeometry
		}
		fields = append(fields, FieldInfo{
			Name:                 name,
			Type:                 fieldType,
			NativeType:           strings.TrimSpace(commonJSON.InterfaceString(attrs["native_type"])),
			Nullable:             commonJSON.InterfaceBool(attrs["nullable"]),
			PrimaryKey:           commonJSON.InterfaceBool(attrs["primary_key"]),
			Comment:              strings.TrimSpace(commonJSON.InterfaceString(attrs["comment"])),
			Size:                 int(commonJSON.InterfaceInt64(attrs["size"])),
			Precision:            int(commonJSON.InterfaceInt64(attrs["precision"])),
			Scale:                int(commonJSON.InterfaceInt64(attrs["scale"])),
			OrdinalPosition:      int(commonJSON.InterfaceInt64(attrs["ordinal_position"])),
			DefaultExpression:    strings.TrimSpace(commonJSON.InterfaceString(attrs["default_expression"])),
			Generated:            commonJSON.InterfaceBool(attrs["generated"]),
			GenerationExpression: strings.TrimSpace(commonJSON.InterfaceString(attrs["generation_expression"])),
		})
	}
	return fields
}

// FieldInfoAttributes converts common field facts to attributes arrays.
func FieldInfoAttributes(fields []FieldInfo) []map[string]interface{} {
	fieldsData := make([]map[string]interface{}, 0, len(fields))
	for _, f := range fields {
		field := map[string]interface{}{
			"name":     f.Name,
			"type":     string(f.Type),
			"nullable": f.Nullable,
		}
		if f.NativeType != "" {
			field["native_type"] = f.NativeType
		}
		if f.PrimaryKey {
			field["primary_key"] = true
		}
		if f.Comment != "" {
			field["comment"] = f.Comment
		}
		if f.Size > 0 {
			field["size"] = f.Size
		}
		if f.Precision > 0 {
			field["precision"] = f.Precision
		}
		if f.Scale > 0 {
			field["scale"] = f.Scale
		}
		if f.OrdinalPosition > 0 {
			field["ordinal_position"] = f.OrdinalPosition
		}
		if f.DefaultExpression != "" {
			field["default_expression"] = f.DefaultExpression
		}
		if f.Generated {
			field["generated"] = true
		}
		if f.GenerationExpression != "" {
			field["generation_expression"] = f.GenerationExpression
		}
		fieldsData = append(fieldsData, field)
	}
	return fieldsData
}

func stringSliceFromAttribute(value interface{}) []string {
	items := commonJSON.InterfaceSlice(value)
	values := make([]string, 0, len(items))
	for _, item := range items {
		if value := strings.TrimSpace(commonJSON.InterfaceString(item)); value != "" {
			values = append(values, value)
		}
	}
	return values
}
