package datatype

import (
	"strings"

	commonJSON "github.com/addp/common/jsonmap"
)

// TableInfoFromPayload restores common table facts from a table JSON payload.
func TableInfoFromPayload(payload map[string]interface{}, fallbackName string) *TableInfo {
	if len(payload) == 0 {
		return nil
	}
	var info TableInfo
	if err := commonJSON.DecodeStruct(payload, &info); err != nil {
		return nil
	}
	for i := range info.Fields {
		info.Fields[i] = normalizeFieldInfo(info.Fields[i])
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
	if !hasTableInfoFacts(info) {
		return nil
	}
	return &info
}

// TableInfoPayload converts common table facts to a JSON payload.
func TableInfoPayload(info *TableInfo) map[string]interface{} {
	return commonJSON.MapFromStruct(info)
}

// FieldInfosFromPayload restores common field facts from a JSON payload array.
func FieldInfosFromPayload(value interface{}) []FieldInfo {
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

// FieldInfoPayload converts common field facts to JSON payload arrays.
func FieldInfoPayload(fields []FieldInfo) []map[string]interface{} {
	fieldsData := make([]map[string]interface{}, 0, len(fields))
	for _, f := range fields {
		if payload := commonJSON.MapFromStruct(f); len(payload) > 0 {
			fieldsData = append(fieldsData, payload)
		}
	}
	return fieldsData
}

func hasTableInfoFacts(info TableInfo) bool {
	return info.Name != "" ||
		info.Kind != "" ||
		info.Comment != "" ||
		info.RowCount != nil ||
		info.SizeBytes != nil ||
		info.CreatedAt != nil ||
		info.UpdatedAt != nil ||
		len(info.Fields) > 0 ||
		len(info.PrimaryKey) > 0 ||
		len(info.Native) > 0
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
