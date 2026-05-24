package jsonmap

import (
	"encoding/json"
	"reflect"
	"strings"
	"time"
)

// MapFromStruct converts a struct with json tags to a map.
func MapFromStruct(input interface{}) map[string]interface{} {
	value := reflect.ValueOf(input)
	if !value.IsValid() {
		return nil
	}
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return nil
	}
	result := taggedStructMap(value)
	if len(result) == 0 {
		return nil
	}
	return result
}

// DecodeStruct decodes a map into a struct using json tags.
func DecodeStruct(attrs map[string]interface{}, output interface{}) error {
	if len(attrs) == 0 || output == nil {
		return nil
	}
	data, err := json.Marshal(attrs)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, output)
}

func taggedStructMap(value reflect.Value) map[string]interface{} {
	result := map[string]interface{}{}
	valueType := value.Type()
	for i := 0; i < value.NumField(); i++ {
		fieldValue := value.Field(i)
		fieldType := valueType.Field(i)
		if fieldType.PkgPath != "" {
			continue
		}
		name, omitEmpty, ok := jsonTag(fieldType)
		if !ok {
			continue
		}
		converted, empty := taggedValue(fieldValue)
		if omitEmpty && empty {
			continue
		}
		result[name] = converted
	}
	return result
}

func jsonTag(field reflect.StructField) (string, bool, bool) {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return "", false, false
	}
	name, options, _ := strings.Cut(tag, ",")
	if name == "" {
		name = field.Name
	}
	return name, strings.Contains(","+options+",", ",omitempty,"), true
}

func taggedValue(value reflect.Value) (interface{}, bool) {
	if !value.IsValid() {
		return nil, true
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil, true
		}
		converted, _ := taggedValue(value.Elem())
		return converted, false
	}
	if value.Type() == reflect.TypeOf(time.Time{}) {
		t := value.Interface().(time.Time)
		return t, t.IsZero()
	}
	switch value.Kind() {
	case reflect.Struct:
		attrs := taggedStructMap(value)
		return attrs, len(attrs) == 0
	case reflect.Slice, reflect.Array:
		if value.Len() == 0 {
			return nil, true
		}
		items := make([]interface{}, 0, value.Len())
		for i := 0; i < value.Len(); i++ {
			converted, empty := taggedValue(value.Index(i))
			if empty {
				continue
			}
			items = append(items, converted)
		}
		return items, len(items) == 0
	case reflect.Map:
		if value.Len() == 0 {
			return nil, true
		}
		return cloneMapValue(value), false
	case reflect.String:
		text := value.String()
		return text, text == ""
	case reflect.Bool:
		b := value.Bool()
		return b, !b
	case reflect.Int:
		i := int(value.Int())
		return i, i == 0
	case reflect.Int8:
		i := int8(value.Int())
		return i, i == 0
	case reflect.Int16:
		i := int16(value.Int())
		return i, i == 0
	case reflect.Int32:
		i := int32(value.Int())
		return i, i == 0
	case reflect.Int64:
		i := value.Int()
		return i, i == 0
	case reflect.Uint:
		u := uint(value.Uint())
		return u, u == 0
	case reflect.Uint8:
		u := uint8(value.Uint())
		return u, u == 0
	case reflect.Uint16:
		u := uint16(value.Uint())
		return u, u == 0
	case reflect.Uint32:
		u := uint32(value.Uint())
		return u, u == 0
	case reflect.Uint64:
		u := value.Uint()
		return u, u == 0
	case reflect.Uintptr:
		u := uintptr(value.Uint())
		return u, u == 0
	case reflect.Float32:
		f := float32(value.Float())
		return f, f == 0
	case reflect.Float64:
		f := value.Float()
		return f, f == 0
	case reflect.Interface:
		if value.IsNil() {
			return nil, true
		}
		return taggedValue(value.Elem())
	default:
		if value.IsZero() {
			return nil, true
		}
		return value.Interface(), false
	}
}

func cloneMapValue(value reflect.Value) map[string]interface{} {
	result := make(map[string]interface{}, value.Len())
	iter := value.MapRange()
	for iter.Next() {
		key := InterfaceString(iter.Key().Interface())
		if key == "" {
			continue
		}
		converted := rawMapValue(iter.Value())
		result[key] = converted
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func rawMapValue(value reflect.Value) interface{} {
	if !value.IsValid() {
		return nil
	}
	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}
	if value.Type() == reflect.TypeOf(time.Time{}) {
		return value.Interface()
	}
	switch value.Kind() {
	case reflect.Map:
		return cloneMapValue(value)
	case reflect.Slice, reflect.Array:
		items := make([]interface{}, 0, value.Len())
		for i := 0; i < value.Len(); i++ {
			items = append(items, rawMapValue(value.Index(i)))
		}
		return items
	case reflect.Struct:
		return taggedStructMap(value)
	default:
		return value.Interface()
	}
}
