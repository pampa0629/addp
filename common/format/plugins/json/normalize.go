package jsonformat

import (
	"encoding/json"
	"strings"
)

func normalizeValue(value interface{}) interface{} {
	switch v := value.(type) {
	case json.Number:
		if strings.Contains(v.String(), ".") {
			if f, err := v.Float64(); err == nil {
				return f
			}
		}
		if i, err := v.Int64(); err == nil {
			return i
		}
		if f, err := v.Float64(); err == nil {
			return f
		}
		return v.String()
	case map[string]interface{}:
		m := make(map[string]interface{}, len(v))
		for key, val := range v {
			m[key] = normalizeValue(val)
		}
		return m
	case []interface{}:
		arr := make([]interface{}, len(v))
		for i, val := range v {
			arr[i] = normalizeValue(val)
		}
		return arr
	default:
		return value
	}
}
