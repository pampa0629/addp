package postgresql

import (
	"encoding/hex"
	"strings"

	"github.com/addp/common/datatype"
)

func postgresGeometryColumns(fields []datatype.FieldInfo) map[string]struct{} {
	columns := map[string]struct{}{}
	for _, field := range fields {
		name := strings.TrimSpace(field.Name)
		if name == "" || !isPostgresGeometryField(field.Type) {
			continue
		}
		columns[name] = struct{}{}
	}
	return columns
}

func isPostgresGeometryField(fieldType datatype.FieldType) bool {
	return datatype.IsSpatialFieldType(fieldType)
}

func postgresWriteValue(value interface{}, isGeometry bool) interface{} {
	if !isGeometry {
		return value
	}
	switch v := value.(type) {
	case []byte:
		if len(v) == 0 {
			return nil
		}
		return hex.EncodeToString(v)
	default:
		return value
	}
}
