package datatype

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestRowsForJSONUsesDeclaredTypesAndLeavesNativeValuesUnchanged(t *testing.T) {
	fields := []FieldInfo{{Name: "small", Type: FieldTypeInt}, {Name: "big", Type: FieldTypeBigInt}, {Name: "amount", Type: FieldTypeDecimal}, {Name: "masked", Type: FieldTypeBigInt}, {Name: "empty", Type: FieldTypeBigInt}}
	row := map[string]interface{}{"small": int64(2), "big": int64(9007199254740993), "amount": "12345678901234567890.123456789012345678", "masked": "***", "empty": nil}
	before := map[string]interface{}{}
	for key, value := range row {
		before[key] = value
	}
	result, err := RowsForJSON(fields, []map[string]interface{}{row})
	if err != nil {
		t.Fatal(err)
	}
	if result[0]["small"] != int64(2) || result[0]["big"] != "9007199254740993" || result[0]["amount"] != row["amount"] || result[0]["masked"] != "***" || result[0]["empty"] != nil {
		t.Fatalf("wire values: %#v", result)
	}
	if !reflect.DeepEqual(row, before) {
		t.Fatal("native result mutated")
	}
	encoded, _ := json.Marshal(result)
	var decoded []map[string]interface{}
	if json.Unmarshal(encoded, &decoded) != nil || decoded[0]["big"] != "9007199254740993" || decoded[0]["small"] != float64(2) {
		t.Fatal("JSON round trip lost precision")
	}
}

func TestRowsForJSONPreservesReauthorizedNumbersAndRejectsFloatingPoint(t *testing.T) {
	for _, value := range []interface{}{json.Number("9007199254740993"), json.Number("18446744073709551615"), int64(-9223372036854775808)} {
		if _, err := RowsForJSON([]FieldInfo{{Name: "value", Type: FieldTypeBigInt}}, []map[string]interface{}{{"value": value}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, fieldType := range []FieldType{FieldTypeBigInt, FieldTypeDecimal} {
		if _, err := RowsForJSON([]FieldInfo{{Name: "value", Type: fieldType}}, []map[string]interface{}{{"value": float64(9007199254740993)}}); err == nil {
			t.Fatal("floating point accepted as an exact value")
		}
	}
	if _, err := BigintJSONText(json.Number("1.0")); err == nil {
		t.Fatal("fractional integer accepted")
	}
	if _, err := BigintJSONText(json.Number("18446744073709551616")); err == nil {
		t.Fatal("out of range integer accepted")
	}
}

type exactDecimalText string

func (value exactDecimalText) MarshalText() ([]byte, error) { return []byte(value), nil }

func TestRowsForJSONPreservesNativeExactDecimalRepresentations(t *testing.T) {
	for _, value := range []interface{}{exactDecimalText("12345678901234567890.123456789012345678"), json.Number("12345678901234567890.123456789012345678")} {
		rows, err := RowsForJSON([]FieldInfo{{Name: "value", Type: FieldTypeDecimal}}, []map[string]interface{}{{"value": value}})
		if err != nil || rows[0]["value"] != "12345678901234567890.123456789012345678" {
			t.Fatal("native exact Decimal changed")
		}
	}
	rows, err := RowsForJSON([]FieldInfo{{Name: "value", Type: FieldTypeDecimal}}, []map[string]interface{}{{"value": int64(9007199254740993)}})
	if err != nil || rows[0]["value"] != "9007199254740993" {
		t.Fatal("integral Decimal changed")
	}
}
