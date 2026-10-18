package datatype

import (
	"encoding"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
)

// BigintJSONText preserves exact native integers and already protected text.
func BigintJSONText(value interface{}) (string, error) {
	switch value := value.(type) {
	case string:
		return value, nil
	case json.Number:
		if _, err := value.Int64(); err != nil {
			if _, unsignedErr := strconv.ParseUint(value.String(), 10, 64); unsignedErr != nil {
				return "", fmt.Errorf("bigint JSON number must be an exact integer")
			}
		}
		return value.String(), nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprint(value), nil
	default:
		return "", fmt.Errorf("bigint requires an integer or exact text, got %T", value)
	}
}

// RowsForJSON copies an already protected result into its public representation.
// It never changes Provider values or infers a column type from a Go value.
func RowsForJSON(fields []FieldInfo, rows []map[string]interface{}) ([]map[string]interface{}, error) {
	types := make(map[string]FieldType, len(fields))
	for _, field := range fields {
		types[field.Name] = field.Type
	}
	result := make([]map[string]interface{}, len(rows))
	for i, row := range rows {
		if row == nil {
			continue
		}
		copyRow := make(map[string]interface{}, len(row))
		for name, value := range row {
			if value != nil {
				var err error
				switch types[name] {
				case FieldTypeBigInt:
					value, err = BigintJSONText(value)
				case FieldTypeDecimal:
					value, err = decimalJSONText(value)
				}
				if err != nil {
					return nil, fmt.Errorf("serialize field %q: %w", name, err)
				}
			}
			copyRow[name] = value
		}
		result[i] = copyRow
	}
	return result, nil
}

func decimalJSONText(value interface{}) (string, error) {
	switch value := value.(type) {
	case string:
		// Includes a masking result; never reinterpret protected text.
		return value, nil
	case json.Number:
		if _, ok := new(big.Rat).SetString(value.String()); ok && json.Valid([]byte(value.String())) {
			return value.String(), nil
		}
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprint(value), nil
	case encoding.TextMarshaler:
		text, err := value.MarshalText()
		if err == nil && json.Valid(text) {
			if _, ok := new(big.Rat).SetString(string(text)); ok {
				return string(text), nil
			}
		}
	}
	return "", fmt.Errorf("decimal requires exact decimal text, got %T", value)
}
