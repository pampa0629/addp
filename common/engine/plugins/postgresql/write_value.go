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
		value, err := ProtocolTableWriteValue(row[column], fieldTypes[column])
		if err != nil {
			return nil, fmt.Errorf("postgresql write column %q: %w", column, err)
		}
		values[i] = value
	}
	return values, nil
}

// ProtocolTableWriteValue is the single value encoder for certified PostgreSQL
// wire-protocol writers, including engine-native statements that bind PG types.
// JSON, TEXT[] and geometry rules stay here; bytes retain native binding.
func ProtocolTableWriteValue(value interface{}, fieldType datatype.FieldType) (interface{}, error) {
	if value == nil {
		return nil, nil
	}
	// lib/pq encodes a typed nil byte slice as empty BYTEA, including COPY.
	// Preserve SQL NULL for bytes without changing non-nil empty slices.
	if fieldType == datatype.FieldTypeBytes {
		if value, ok := value.([]byte); ok && value == nil {
			return nil, nil
		}
	}
	if fieldType == datatype.FieldTypeArray {
		return postgresArrayWriteValue(value)
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
