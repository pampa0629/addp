package executor

import (
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func isCopyWriteMethod(method string) bool {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "", "copy", "postgres_copy":
		return true
	default:
		return false
	}
}

func tableInfoFields(info *format.TableInfo) []datatype.FieldInfo {
	if info == nil {
		return nil
	}
	fields := make([]datatype.FieldInfo, 0, len(info.Fields))
	for _, field := range info.Fields {
		if field.Name == "" {
			continue
		}
		fields = append(fields, datatype.FieldInfo{
			Name:       field.Name,
			Type:       field.Type,
			Nullable:   field.Nullable,
			PrimaryKey: field.IsPrimaryKey,
			Comment:    field.Comment,
		})
	}
	return fields
}

func tableInfoSpatialInfo(info *format.TableInfo) *datatype.SpatialInfo {
	if info == nil {
		return nil
	}
	return info.SpatialInfo.Clone()
}
