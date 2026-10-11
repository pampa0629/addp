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
	definition, err := TransformTableDefinition(source, nil, plans)
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
	definition, err := TransformTableDefinition(info, nil, []TableTransformPlan{{Type: "field_mapping", FieldMapping: &FieldMappingTransformPlan{Mode: FieldMappingModeProject, Fields: []FieldMappingFieldPlan{{Source: "known", Target: "value", TargetType: "string"}}}}})
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
	})
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
