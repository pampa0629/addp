package shared

import (
	"database/sql/driver"
	"encoding"
	"encoding/json"
	"reflect"
)

var tableWriteConstantHookTypes = [...]reflect.Type{
	reflect.TypeOf((*json.Marshaler)(nil)).Elem(),
	reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem(),
	reflect.TypeOf((*driver.Valuer)(nil)).Elem(),
}

// IsPureTableWriteConstant checks the input before any encoder or driver hook.
// It is limited to inspectable configuration values and performs no callbacks.
func IsPureTableWriteConstant(value interface{}) bool {
	budget := 4096
	return pureTableWriteConstant(value, 0, &budget)
}

// Configuration constants are JSON values. Refuse custom conversion hooks and
// opaque objects even when nested; they are not part of this pure contract.
func pureTableWriteConstant(value interface{}, depth int, budget *int) bool {
	*budget--
	if depth > 64 || *budget < 0 {
		return false
	}
	if value == nil {
		return true
	}
	switch value.(type) {
	case json.RawMessage, json.Number, []byte:
		return true
	}
	v := reflect.ValueOf(value)
	// JSON can take the address of a slice element and invoke a pointer-only
	// method even when the element value itself does not implement the hook.
	for _, hook := range tableWriteConstantHookTypes {
		if v.Type().Implements(hook) || reflect.PointerTo(v.Type()).Implements(hook) {
			return false
		}
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Struct:
		return false
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if !pureTableWriteConstant(v.Index(i).Interface(), depth+1, budget) {
				return false
			}
		}
	case reflect.Map:
		iterator := v.MapRange()
		for iterator.Next() {
			if !pureTableWriteConstant(iterator.Key().Interface(), depth+1, budget) || !pureTableWriteConstant(iterator.Value().Interface(), depth+1, budget) {
				return false
			}
		}
	}
	return true
}
