package plugin

import (
	"strings"

	"github.com/addp/common/datatype"
)

// NormalizeFieldInfos validates canonical field facts without interpreting NativeType.
func NormalizeFieldInfos(fields []datatype.FieldInfo) []datatype.FieldInfo {
	if len(fields) == 0 {
		return nil
	}
	normalized := make([]datatype.FieldInfo, 0, len(fields))
	for _, field := range fields {
		field.Name = strings.TrimSpace(field.Name)
		field.NativeType = strings.TrimSpace(field.NativeType)
		if field.Name == "" {
			continue
		}
		field.Type = datatype.ParseFieldType(string(field.Type))
		if field.NativeType == "" && field.Type != datatype.FieldTypeUnknown {
			field.NativeType = string(field.Type)
		}
		normalized = append(normalized, field)
	}
	return normalized
}
