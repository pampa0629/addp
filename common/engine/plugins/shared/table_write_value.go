package shared

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

// TableWriteFieldTypes snapshots declared field types for a write operation.
func TableWriteFieldTypes(fields []datatype.FieldInfo) map[string]datatype.FieldType {
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

// EncodeTableWriteValue encodes only declared JSON fields. Engine-specific
// geometry, binary and native array handling belongs to the caller.
func EncodeTableWriteValue(value interface{}, fieldType datatype.FieldType) (interface{}, error) {
	if value == nil || fieldType != datatype.FieldTypeJSON {
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
			return nil, plugin.NewTableWriteError(plugin.TableWriteJSONInvalid, fmt.Errorf("encode JSON value: %w", err))
		}
	}
	// json.Valid checks JSON syntax but accepts invalid UTF-8 inside strings.
	// Reject those raw bytes before an engine can replace or reject them.
	if !utf8.Valid(encoded) || !json.Valid(encoded) {
		return nil, plugin.NewTableWriteError(plugin.TableWriteJSONInvalid, fmt.Errorf("invalid encoded JSON value (%T)", value))
	}
	return string(encoded), nil
}
