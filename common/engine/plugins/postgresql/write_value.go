package postgresql

import (
	"encoding/hex"
	"fmt"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugins/shared"
)

func isPostgresGeometryField(fieldType datatype.FieldType) bool {
	return datatype.IsSpatialFieldType(fieldType)
}

func postgresWriteRow(row map[string]interface{}, columns []string, fieldTypes map[string]datatype.FieldType) ([]interface{}, error) {
	values := make([]interface{}, len(columns))
	for i, column := range columns {
		value, err := postgresWriteValue(row[column], fieldTypes[column])
		if err != nil {
			return nil, fmt.Errorf("postgresql write column %q: %w", column, err)
		}
		values[i] = value
	}
	return values, nil
}

func postgresWriteValue(value interface{}, fieldType datatype.FieldType) (interface{}, error) {
	if value == nil {
		return nil, nil
	}
	if isPostgresGeometryField(fieldType) {
		if v, ok := value.([]byte); ok {
			if len(v) == 0 {
				return nil, nil
			}
			return hex.EncodeToString(v), nil
		}
		return value, nil
	}
	return shared.EncodeTableWriteValue(value, fieldType)
}
