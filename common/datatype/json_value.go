package datatype

import (
	"encoding"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"

	"github.com/twpayne/go-geom/encoding/ewkb"
	"github.com/twpayne/go-geom/encoding/wkt"
)

// GeometryValue is the public, coordinate-system-preserving query projection.
// Provider values remain EWKB until the already-protected JSON boundary.
type GeometryValue struct {
	WKT  string `json:"wkt"`
	SRID int    `json:"srid"`
}

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
	return rowsForJSON(fields, rows, false)
}

// QueryRowsForJSON additionally projects canonical query EWKB after protection.
// Table previews keep their independently declared spatial wire format.
func QueryRowsForJSON(fields []FieldInfo, rows []map[string]interface{}) ([]map[string]interface{}, error) {
	return rowsForJSON(fields, rows, true)
}

func rowsForJSON(fields []FieldInfo, rows []map[string]interface{}, queryGeometry bool) ([]map[string]interface{}, error) {
	types := make(map[string]FieldInfo, len(fields))
	for _, field := range fields {
		types[field.Name] = field
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
				value, err = fieldValueForJSON(types[name], value, 0, queryGeometry)
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

func fieldValueForJSON(field FieldInfo, value interface{}, depth int, queryGeometry bool) (interface{}, error) {
	if value == nil {
		return nil, nil
	}
	if depth > 16 {
		return nil, fmt.Errorf("nested JSON field exceeds depth budget")
	}
	// Whole-field protection can replace structured values with text.
	if _, protected := value.(string); protected {
		return value, nil
	}
	if field.Temporal != nil {
		return temporalJSONText(field, value)
	}
	switch field.Type {
	case FieldTypeGeometry:
		if !queryGeometry {
			return value, nil
		}
		encoded, ok := value.([]byte)
		if !ok {
			return nil, fmt.Errorf("geometry requires provider-normalized EWKB")
		}
		geometry, err := ewkb.Unmarshal(encoded)
		if err != nil {
			return nil, fmt.Errorf("invalid query geometry EWKB")
		}
		text, err := wkt.Marshal(geometry)
		if err != nil {
			return nil, fmt.Errorf("encode query geometry WKT")
		}
		return GeometryValue{WKT: text, SRID: geometry.SRID()}, nil
	case FieldTypeBigInt:
		return BigintJSONText(value)
	case FieldTypeDecimal:
		return decimalJSONText(value)
	case FieldTypeJSON:
		if len(field.Fields) == 0 {
			return value, nil
		}
		object, ok := value.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("declared object requires object value")
		}
		result := make(map[string]interface{}, len(object))
		byName := make(map[string]FieldInfo, len(field.Fields))
		for _, child := range field.Fields {
			byName[child.Name] = child
		}
		for name, item := range object {
			converted, err := fieldValueForJSON(byName[name], item, depth+1, queryGeometry)
			if err != nil {
				return nil, err
			}
			result[name] = converted
		}
		return result, nil
	case FieldTypeArray:
		if field.Element == nil {
			return value, nil
		}
		array, ok := value.([]interface{})
		if !ok {
			return nil, fmt.Errorf("declared array requires array value")
		}
		result := make([]interface{}, len(array))
		for i, item := range array {
			var err error
			result[i], err = fieldValueForJSON(*field.Element, item, depth+1, queryGeometry)
			if err != nil {
				return nil, err
			}
		}
		return result, nil
	}
	return value, nil
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
