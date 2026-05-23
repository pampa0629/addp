package plugin

import (
	"strings"

	"github.com/addp/common/datatype"
)

func FieldInfoFromNative(name, nativeType string, nullable, primaryKey bool, comment string) datatype.FieldInfo {
	nativeType = strings.TrimSpace(nativeType)
	fieldType := datatype.ParseFieldType(nativeType)
	return datatype.FieldInfo{
		Name:       strings.TrimSpace(name),
		Type:       fieldType,
		NativeType: nativeType,
		Nullable:   nullable,
		PrimaryKey: primaryKey,
		Comment:    comment,
	}
}

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
		if field.NativeType == "" && field.Type != "" {
			field.NativeType = string(field.Type)
		}
		if !datatype.IsKnownFieldType(field.Type) || field.Type == "" {
			field.Type = datatype.ParseFieldType(field.NativeType)
		}
		normalized = append(normalized, field)
	}
	return normalized
}
