package query

import (
	"encoding/json"
	"testing"
)

func TestParameterOptionTypesAndIntersection(t *testing.T) {
	labels := map[string]string{"zh-cn": "一", "en": "One"}
	options := []ParameterOption{{Value: 1, Labels: labels}}
	if !ParameterOptionAllows(options, json.Number("1.0")) || ParameterOptionAllows(options, "1") || ParameterOptionAllows(options, true) {
		t.Fatal("scalar types or numeric equality lost")
	}
	if err := ValidateParameterOptions(append(options, ParameterOption{Value: float64(1), Labels: labels}), func(any) error { return nil }); err == nil {
		t.Fatal("duplicate numeric value accepted")
	}
	if _, err := IntersectParameterOptions(options, []ParameterOption{{Value: 2, Labels: labels}}); err == nil {
		t.Fatal("empty intersection accepted")
	}
	if _, err := IntersectParameterOptions(options, []ParameterOption{{Value: 1, Labels: map[string]string{"zh-cn": "不同", "en": "Different"}}}); err == nil {
		t.Fatal("conflicting names accepted")
	}
	for _, invalid := range []ParameterOption{{Value: nil, Labels: labels}, {Value: []int{1}, Labels: labels}, {Value: "", Labels: labels}, {Value: 1, Labels: map[string]string{"en": "One"}}} {
		if ValidateParameterOptions([]ParameterOption{invalid}, func(any) error { return nil }) == nil {
			t.Fatalf("invalid option accepted: %#v", invalid)
		}
	}
}
