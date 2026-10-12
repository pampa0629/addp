package executor

import (
	"reflect"
	"testing"

	"github.com/addp/common/datatype"
)

func TestTransformDefinitionKeepsProvenanceAndRuntimeMetadata(t *testing.T) {
	source := &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "payload", Type: datatype.FieldTypeArray, ElementType: datatype.FieldTypeJSON, Nullable: true}, {Name: "other", Type: datatype.FieldTypeBytes, Nullable: true}}}
	before := source.Clone()
	plans := []TableTransformPlan{
		{Type: "field_mapping", FieldMapping: &FieldMappingTransformPlan{Mode: FieldMappingModeProject, Fields: []FieldMappingFieldPlan{{Source: "payload", Target: "renamed", TargetType: "json", Nullable: true}, {Target: "constant", TargetType: "json", Default: map[string]interface{}{"x": true}, Nullable: true}}}},
		{Type: "field_mapping", FieldMapping: &FieldMappingTransformPlan{Mode: FieldMappingModePassthrough, Fields: []FieldMappingFieldPlan{{Source: "renamed", Target: "again", TargetType: "json", Nullable: true}}}},
	}
	definition, err := TransformTableDefinition(source, nil, plans, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(source, before) {
		t.Fatal("source facts mutated")
	}
	if len(definition.Fields) != 3 {
		t.Fatalf("definition=%+v", definition)
	}
	for _, field := range definition.Fields {
		if field.Target.Name == "again" {
			if field.Source == nil || field.Source.Type != datatype.FieldTypeArray || field.Source.ElementType != datatype.FieldTypeJSON || field.Target.Type != datatype.FieldTypeJSON {
				t.Fatalf("provenance lost: %+v", field)
			}
		}
		if field.Target.Name == "constant" && (field.Constant == nil || field.Source != nil) {
			t.Fatalf("constant lost: %+v", field)
		}
	}
	runtimeTransforms, err := buildTableTransforms(plans, nil)
	if err != nil {
		t.Fatal(err)
	}
	expected, _, err := applyTableInfoTransforms(source, nil, runtimeTransforms)
	if err != nil {
		t.Fatal(err)
	}
	var fields []datatype.FieldInfo
	for _, binding := range definition.Fields {
		fields = append(fields, binding.Target)
	}
	if !reflect.DeepEqual(fields, expected.Fields) {
		t.Fatal("precheck and runtime metadata diverged")
	}
}
func TestTransformDefinitionUsesNullFallbackForAbsentRowKey(t *testing.T) {
	info := &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "Known", Type: datatype.FieldTypeString}}}
	definition, err := TransformTableDefinition(info, nil, []TableTransformPlan{{Type: "field_mapping", FieldMapping: &FieldMappingTransformPlan{Mode: FieldMappingModeProject, Fields: []FieldMappingFieldPlan{{Source: "known", Target: "value", TargetType: "string"}}}}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(definition.Fields) != 1 || definition.Fields[0].Source != nil || definition.Fields[0].Constant == nil || definition.Fields[0].Constant.Value != nil {
		t.Fatalf("absent case-sensitive key must use runtime NULL fallback: %+v", definition)
	}
}

func TestTransformDefinitionPreservesEarlierNonNullConstant(t *testing.T) {
	definition, err := TransformTableDefinition(nil, nil, []TableTransformPlan{
		{Type: "field_mapping", FieldMapping: &FieldMappingTransformPlan{Mode: FieldMappingModeProject, Fields: []FieldMappingFieldPlan{{Target: "first", TargetType: "json", Default: map[string]interface{}{"value": true}, Nullable: true}}}},
		{Type: "field_mapping", FieldMapping: &FieldMappingTransformPlan{Mode: FieldMappingModeProject, Fields: []FieldMappingFieldPlan{{Source: "first", Target: "second", TargetType: "json", Default: "invalid-json", Nullable: true}}}},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(definition.Fields) != 1 || definition.Fields[0].Constant == nil {
		t.Fatalf("definition=%+v", definition)
	}
	if _, ok := definition.Fields[0].Constant.Value.(map[string]interface{}); !ok {
		t.Fatal("unreachable fallback replaced actual constant")
	}
}

func TestTransformDefinitionIncompleteSourceDoesNotInventNullOrInheritedConstraints(t *testing.T) {
	info := &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "sample", Type: datatype.FieldTypeArray, ElementType: datatype.FieldTypeJSON, Precision: 999, NativeType: "unproven"}}}
	plans := []TableTransformPlan{{Type: "field_mapping", FieldMapping: &FieldMappingTransformPlan{Mode: FieldMappingModeProject, Fields: []FieldMappingFieldPlan{
		{Source: "unobserved", Target: "missing", TargetType: "json", Nullable: false},
		{Source: "sample", Target: "inferred", Nullable: false},
		{Source: "unobserved", Target: "fallback", TargetType: "json", Default: "invalid-json", Nullable: true},
		{Target: "known_null", TargetType: "json", Nullable: false},
	}}}}
	definition, err := TransformTableDefinition(info, nil, plans, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, binding := range definition.Fields {
		switch binding.Target.Name {
		case "missing":
			if binding.Source == nil || binding.Source.Type != datatype.FieldTypeUnknown || binding.Constant != nil {
				t.Fatalf("unobserved became NULL: %+v", binding)
			}
		case "inferred":
			if binding.Target.Type != datatype.FieldTypeUnknown || binding.Target.ElementType != "" || binding.Target.NativeType != "" || binding.Target.Precision != 0 {
				t.Fatalf("sample became constraint: %+v", binding)
			}
		case "fallback":
			if binding.Source == nil || binding.Constant == nil || binding.Constant.Value != "invalid-json" {
				t.Fatalf("independent fallback lost: %+v", binding)
			}
		case "known_null":
			if binding.Source != nil || binding.Constant == nil || binding.Constant.Value != nil {
				t.Fatalf("explicit constant lost: %+v", binding)
			}
		}
	}
}
