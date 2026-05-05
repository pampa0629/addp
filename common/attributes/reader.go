package attributes

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Section returns a nested attributes section such as "storage" or "extensions.spatial".
func Section(attrs map[string]interface{}, section string) map[string]interface{} {
	if attrs == nil || section == "" {
		return nil
	}
	current := attrs
	for _, part := range strings.Split(section, ".") {
		raw, ok := current[part]
		if !ok {
			return nil
		}
		next := interfaceMap(raw)
		if next == nil {
			return nil
		}
		current = next
	}
	return current
}

// Value reads a key from a standard attributes section first, then falls back to flat compatibility.
func Value(attrs map[string]interface{}, section, key string) interface{} {
	return ValueFromSections(attrs, key, section)
}

// ValueFromSections reads a key from the first matching standard section, then falls back to flat compatibility.
func ValueFromSections(attrs map[string]interface{}, key string, sections ...string) interface{} {
	for _, section := range sections {
		if sectionAttrs := Section(attrs, section); sectionAttrs != nil {
			if value, ok := sectionAttrs[key]; ok {
				return value
			}
		}
	}
	if attrs == nil {
		return nil
	}
	return attrs[key]
}

func StringFromSections(attrs map[string]interface{}, key string, sections ...string) string {
	return InterfaceString(ValueFromSections(attrs, key, sections...))
}

func BoolFromSections(attrs map[string]interface{}, key string, sections ...string) bool {
	return InterfaceBool(ValueFromSections(attrs, key, sections...))
}

func Int64FromSections(attrs map[string]interface{}, key string, sections ...string) int64 {
	return InterfaceInt64(ValueFromSections(attrs, key, sections...))
}

func Float64SliceFromSections(attrs map[string]interface{}, key string, sections ...string) []float64 {
	return InterfaceFloat64Slice(ValueFromSections(attrs, key, sections...))
}

func String(attrs map[string]interface{}, section, key string) string {
	return InterfaceString(Value(attrs, section, key))
}

func Bool(attrs map[string]interface{}, section, key string) bool {
	return InterfaceBool(Value(attrs, section, key))
}

func Int64(attrs map[string]interface{}, section, key string) int64 {
	return InterfaceInt64(Value(attrs, section, key))
}

func Float64Slice(attrs map[string]interface{}, section, key string) []float64 {
	return InterfaceFloat64Slice(Value(attrs, section, key))
}

func InterfaceBool(value interface{}) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, _ := strconv.ParseBool(strings.TrimSpace(typed))
		return parsed
	default:
		return false
	}
}

func InterfaceFloat64Slice(value interface{}) []float64 {
	switch values := value.(type) {
	case []float64:
		return values
	case []interface{}:
		result := make([]float64, 0, len(values))
		for _, raw := range values {
			switch value := raw.(type) {
			case float64:
				result = append(result, value)
			case float32:
				result = append(result, float64(value))
			case int:
				result = append(result, float64(value))
			case int64:
				result = append(result, float64(value))
			case string:
				if parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
					result = append(result, parsed)
				}
			}
		}
		return result
	default:
		return nil
	}
}

func InterfaceString(value interface{}) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case []byte:
		return string(typed)
	default:
		text := fmt.Sprintf("%v", typed)
		if strings.TrimSpace(text) == "" {
			return ""
		}
		return text
	}
}

func InterfaceInt64(value interface{}) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int8:
		return int64(typed)
	case int16:
		return int64(typed)
	case int32:
		return int64(typed)
	case int64:
		return typed
	case uint:
		return int64(typed)
	case uint8:
		return int64(typed)
	case uint16:
		return int64(typed)
	case uint32:
		return int64(typed)
	case uint64:
		if typed > uint64(^uint64(0)>>1) {
			return 0
		}
		return int64(typed)
	case float32:
		return int64(typed)
	case float64:
		return int64(typed)
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed
	default:
		return 0
	}
}

func TimePtr(attrs map[string]interface{}, section, key string) *time.Time {
	return InterfaceTimePtr(Value(attrs, section, key))
}

func InterfaceTimePtr(value interface{}) *time.Time {
	switch typed := value.(type) {
	case time.Time:
		if typed.IsZero() {
			return nil
		}
		return &typed
	case *time.Time:
		if typed == nil || typed.IsZero() {
			return nil
		}
		return typed
	default:
		return nil
	}
}

func interfaceMap(value interface{}) map[string]interface{} {
	if value == nil {
		return nil
	}
	if typed, ok := value.(map[string]interface{}); ok {
		return typed
	}
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Map || rv.Type().Key().Kind() != reflect.String {
		return nil
	}
	result := make(map[string]interface{}, rv.Len())
	iter := rv.MapRange()
	for iter.Next() {
		result[iter.Key().String()] = iter.Value().Interface()
	}
	return result
}
