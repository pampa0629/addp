package postgresql

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
)

func postgresWriteFieldTypes(fields []datatype.FieldInfo) map[string]datatype.FieldType {
	types := make(map[string]datatype.FieldType, len(fields))
	for _, field := range fields {
		name := strings.TrimSpace(field.Name)
		if name != "" {
			if _, exists := types[name]; !exists {
				types[name] = field.Type
			}
		}
	}
	return types
}

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
	if fieldType != datatype.FieldTypeJSON {
		return value, nil
	}

	// Text and byte values already carry JSON from readers/drivers. Validate
	// them without decoding numbers or quoting the document a second time.
	var encoded []byte
	switch v := value.(type) {
	case string:
		encoded = []byte(v)
	case []byte:
		if v == nil {
			return nil, nil
		}
		encoded = v
	case json.RawMessage:
		if v == nil {
			return nil, nil
		}
		encoded = v
	default:
		var err error
		encoded, err = json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("encode JSON value: %w", err)
		}
	}
	if !json.Valid(encoded) {
		return nil, fmt.Errorf("invalid encoded JSON value (%T)", value)
	}
	return string(encoded), nil
}
