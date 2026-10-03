package dataprotection

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
)

func TestStructuredAlgorithmsVectorsTypesAndInvalidValues(t *testing.T) {
	for _, test := range []struct {
		name, algorithm, fieldType string
		input                      any
		parameters                 map[string]any
		want                       any
	}{
		{"sm3 standard vector", AlgorithmSM3V1, "string", "abc", map[string]any{}, "66c7f0f462eeedd9d1f2d46bdc10e4e24167c4875cf2f7a2297da02b8f4ba8e0"},
		{"sm3 multi block", AlgorithmSM3V1, "string", strings.Repeat("abcd", 16), nil, "debe9ff92275b8a138604889c18e5a4d6fdb70e5387e5765293dcba39c0c5732"},
		{"unicode mask", AlgorithmKeepPrefixSuffixV2, "string", "张三李四王五", map[string]any{"prefix_runes": 1, "suffix_runes": 1, "mask_rune": "●"}, "张●●●●五"},
		{"constant text", AlgorithmConstantV1, "string", "original", map[string]any{"value": "已隐藏"}, "已隐藏"},
		{"constant integer", AlgorithmConstantV1, "int", int32(28), map[string]any{"value": 0}, 0},
		{"constant big integer", AlgorithmConstantV1, "bigint", int64(9223372036854775807), map[string]any{"value": 0}, 0},
		{"constant boolean", AlgorithmConstantV1, "bool", true, map[string]any{"value": false}, false},
		{"constant number", AlgorithmConstantV1, "double", 1.25, map[string]any{"value": 0.5}, 0.5},
		{"null stays null", AlgorithmSM3V1, "string", nil, map[string]any{}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			decision := Decision{Effect: EffectMask, Algorithm: test.algorithm, Parameters: test.parameters, InvalidValueEffect: EffectDeny}
			result, err := protectValue(test.input, decision, test.fieldType)
			if err != nil || !reflect.DeepEqual(result.value, test.want) || result.suppress {
				t.Fatalf("unexpected protected result or error: %v", err)
			}
		})
	}
	decision := Decision{Effect: EffectMask, Algorithm: AlgorithmSM3V1, Parameters: map[string]any{}, InvalidValueEffect: EffectSuppress}
	result, err := protectValue(42, decision, "string")
	if err != nil || !result.suppress {
		t.Fatal("wrong input type must suppress")
	}
	if _, err := protectValue("abc", decision, "geometry"); err == nil {
		t.Fatal("geometry must reject text algorithms")
	}
	decision.Parameters = map[string]any{"salt": "not supported"}
	if decision.Validate() == nil {
		t.Fatal("SM3 must reject unknown parameters")
	}
	decision = Decision{Effect: EffectMask, Algorithm: AlgorithmConstantV1, Parameters: map[string]any{"value": map[string]any{"nested": "value"}}}
	if decision.Validate() == nil {
		t.Fatal("constant must reject non-scalar configuration")
	}
}

func TestAlgorithmsProtectAliasedQueryResultsForEachOwnerAction(t *testing.T) {
	for _, action := range []string{"preview", "query", "service_execute", "export"} {
		t.Run(action, func(t *testing.T) {
			rules := []Rule{
				{Action: action, Component: Component{Key: "phone", Path: []PathSegment{{Name: "phone", Container: "scalar"}}, ValueType: "string"}, Decision: Decision{Effect: EffectMask, Algorithm: AlgorithmSM3V1}},
				{Action: action, Component: Component{Key: "email", Path: []PathSegment{{Name: "email", Container: "scalar"}}, ValueType: "string"}, Decision: Decision{Effect: EffectMask, Algorithm: AlgorithmConstantV1, Parameters: map[string]any{"value": "hidden"}}},
			}
			source := plugin.QueryOutputSource{Bindings: []plugin.QueryOutputBinding{
				{SourcePath: []string{"phone"}, OutputPath: []string{"contact"}, Transformation: plugin.QueryOutputTransformationDirect},
				{SourcePath: []string{"email"}, OutputPath: []string{"mail"}, Transformation: plugin.QueryOutputTransformationDirect},
			}}
			derived := QueryOutputDerivedFields(source, action, rules, SubjectReference{}, time.Now().UTC())
			if !reflect.DeepEqual(derived, []string{"contact", "mail"}) {
				t.Fatalf("aliased transformation metadata: %v", derived)
			}
			result := &plugin.QueryResult{Columns: []string{"contact", "mail", "shape"}, Rows: []map[string]any{{"contact": "abc", "mail": "original", "shape": "geometry"}}}
			if err := ProtectQueryResultSource(result, source, action, rules, SubjectReference{}); err != nil {
				t.Fatal(err)
			}
			if result.Rows[0]["contact"] != "66c7f0f462eeedd9d1f2d46bdc10e4e24167c4875cf2f7a2297da02b8f4ba8e0" || result.Rows[0]["mail"] != "hidden" || result.Rows[0]["shape"] != "geometry" {
				t.Fatal("aliased owner output was not protected independently")
			}
		})
	}
}

func TestIndependentAlgorithmsAcrossNestedFields(t *testing.T) {
	rules := []Rule{
		{Action: "preview", Component: Component{Key: "person.phone", Path: []PathSegment{{Name: "person", Container: "object"}, {Name: "phone", Container: "scalar"}}, ValueType: "string"}, Decision: Decision{Effect: EffectMask, Algorithm: AlgorithmConstantV1, Parameters: map[string]any{"value": "hidden"}}},
		{Action: "preview", Component: Component{Key: "person.email", Path: []PathSegment{{Name: "person", Container: "object"}, {Name: "email", Container: "scalar"}}, ValueType: "string"}, Decision: Decision{Effect: EffectMask, Algorithm: AlgorithmSM3V1}},
	}
	document := map[string]any{"person": map[string]any{"phone": "123", "email": "abc"}, "shape": []byte{1, 2, 3}}
	if err := ProtectDocument(document, "preview", rules, SubjectReference{}); err != nil {
		t.Fatal(err)
	}
	person := document["person"].(map[string]any)
	if person["phone"] != "hidden" || person["email"] != "66c7f0f462eeedd9d1f2d46bdc10e4e24167c4875cf2f7a2297da02b8f4ba8e0" || !reflect.DeepEqual(document["shape"], []byte{1, 2, 3}) {
		t.Fatal("field independent protection changed unrelated geometry")
	}
}
