package service

import (
	"reflect"
	"testing"

	"github.com/addp/common/datatype"
)

func TestDecimalValueShapePreservesExactRequiredDigits(t *testing.T) {
	tests := []struct {
		value         interface{}
		integerDigits int
		scale         int
	}{
		{"43854577.120000", 8, 2},
		{"0.00120", 0, 4},
		{"-12.3400", 2, 2},
		{"1.2e3", 4, 0},
		{"1.2e-3", 0, 4},
		{"0", 1, 0},
	}
	for _, test := range tests {
		integerDigits, scale, err := decimalValueShape(test.value)
		if err != nil {
			t.Fatalf("decimalValueShape(%v): %v", test.value, err)
		}
		if integerDigits != test.integerDigits || scale != test.scale {
			t.Fatalf("decimalValueShape(%v) = (%d,%d), want (%d,%d)", test.value, integerDigits, scale, test.integerDigits, test.scale)
		}
	}
}

func TestDecimalRecommendationCombinesColumnWideBounds(t *testing.T) {
	accumulator := decimalRecommendationAccumulator{}
	for _, value := range []interface{}{"99999", "0.123456", nil} {
		if err := accumulator.Add(value); err != nil {
			t.Fatal(err)
		}
	}
	precision, scale := accumulator.Recommendation()
	if precision != 11 || scale != 6 || accumulator.NonNullCount != 2 {
		t.Fatalf("recommendation = decimal(%d,%d), non-null=%d", precision, scale, accumulator.NonNullCount)
	}
}

func TestRecommendationFieldsPreserveQuotedIdentifierCase(t *testing.T) {
	fields, err := normalizedRecommendationFields([]string{"Amount", "amount", "Amount"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Amount", "amount"}; !reflect.DeepEqual(fields, want) {
		t.Fatalf("normalized fields = %#v, want %#v", fields, want)
	}

	actual := []datatype.FieldInfo{
		{Name: "Amount", Type: datatype.FieldTypeDecimal},
		{Name: "amount", Type: datatype.FieldTypeString},
	}
	if err := validateDecimalRecommendationFields(actual, []string{"Amount"}); err != nil {
		t.Fatalf("exact decimal field should be accepted: %v", err)
	}
	if err := validateDecimalRecommendationFields(actual, []string{"amount"}); err == nil {
		t.Fatal("case-distinct non-decimal field should be rejected")
	}
}
