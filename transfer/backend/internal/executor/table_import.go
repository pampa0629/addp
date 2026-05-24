package executor

import (
	"strings"

	"github.com/addp/common/datatype"
)

func isCopyWriteMethod(method string) bool {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "", "copy", "postgres_copy":
		return true
	default:
		return false
	}
}

func tableInfoFields(info *datatype.TableInfo) []datatype.FieldInfo {
	if info == nil {
		return nil
	}
	fields := make([]datatype.FieldInfo, 0, len(info.Fields))
	for _, field := range info.Fields {
		if field.Name == "" {
			continue
		}
		fields = append(fields, field)
	}
	return fields
}
