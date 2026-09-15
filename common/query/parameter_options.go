package query

import (
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"strings"
)

// ParameterOption is a published scalar value and its complete display names.
type ParameterOption struct {
	// Value 与参数声明类型一致的标量。| Scalar matching the declared parameter type.
	Value  any               `json:"value"`
	Labels map[string]string `json:"labels"`
}

// ParameterValueKey preserves scalar types and compares numeric values exactly.
func ParameterValueKey(value any) string {
	raw, err := json.Marshal(value)
	if err != nil || value == nil {
		return ""
	}
	switch value.(type) {
	case string, bool:
		return string(raw)
	}
	if n, ok := new(big.Rat).SetString(string(raw)); ok {
		return "number:" + n.RatString()
	}
	return ""
}

func ValidateParameterOptions(options []ParameterOption, validate func(any) error) error {
	if len(options) > 100 {
		return fmt.Errorf("at most 100 parameter options are allowed")
	}
	seen := map[string]bool{}
	for _, option := range options {
		key := ParameterValueKey(option.Value)
		if key == "" || key == `""` || seen[key] || validate(option.Value) != nil {
			return fmt.Errorf("invalid or duplicate parameter option")
		}
		seen[key] = true
		if len(option.Labels) != 2 {
			return fmt.Errorf("parameter option requires zh-cn and en labels")
		}
		for _, lang := range []string{"zh-cn", "en"} {
			label := option.Labels[lang]
			if strings.TrimSpace(label) == "" || len([]rune(label)) > 100 {
				return fmt.Errorf("invalid parameter option label")
			}
		}
	}
	return nil
}

func ParameterOptionAllows(options []ParameterOption, value any) bool {
	if len(options) == 0 {
		return true
	}
	key := ParameterValueKey(value)
	for _, option := range options {
		if key != "" && ParameterValueKey(option.Value) == key {
			return true
		}
	}
	return false
}

// IntersectParameterOptions treats an absent domain as unrestricted.
func IntersectParameterOptions(left, right []ParameterOption) ([]ParameterOption, error) {
	if len(left) == 0 {
		return right, nil
	}
	if len(right) == 0 {
		return left, nil
	}
	result := []ParameterOption{}
	for _, a := range left {
		for _, b := range right {
			if ParameterValueKey(a.Value) != ParameterValueKey(b.Value) {
				continue
			}
			if !reflect.DeepEqual(a.Labels, b.Labels) {
				return nil, fmt.Errorf("parameter option labels conflict")
			}
			result = append(result, a)
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("parameter options have no common value")
	}
	return result, nil
}
