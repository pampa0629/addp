package executor

import (
	"testing"

	"github.com/addp/common/datatype"
)

func TestTableFieldLineageComposesActualMappingsAndConstants(t *testing.T) {
	fields := []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeString}, {Name: "name", Type: datatype.FieldTypeString}}
	read := &datatype.TableInfo{Fields: fields}
	target := []datatype.FieldInfo{{Name: "client_id", Type: datatype.FieldTypeString}, {Name: "title", Type: datatype.FieldTypeString}, {Name: "label", Type: datatype.FieldTypeString}}
	plans := []TableTransformPlan{
		{Type: "field_mapping", FieldMapping: &FieldMappingTransformPlan{Mode: FieldMappingModeProject, Fields: []FieldMappingFieldPlan{{Source: "id", Target: "temp"}, {Source: "name", Target: "title", Default: "unknown"}, {Target: "label", Default: "customer"}}}},
		{Type: "field_mapping", FieldMapping: &FieldMappingTransformPlan{Mode: FieldMappingModePassthrough, Fields: []FieldMappingFieldPlan{{Source: "temp", Target: "client_id"}}}},
	}
	lineage := buildTableFieldLineage(fields, target, read, &datatype.TableInfo{Fields: target}, plans)
	if lineage == nil || len(lineage.Mappings) != 3 {
		t.Fatalf("lineage: %+v", lineage)
	}
	byTarget := map[string]int{}
	for index, mapping := range lineage.Mappings {
		byTarget[mapping.TargetField] = index
	}
	if mapping := lineage.Mappings[byTarget["client_id"]]; mapping.SourceField != "id" || mapping.Transformation != "direct" {
		t.Fatalf("rename: %+v", mapping)
	}
	if mapping := lineage.Mappings[byTarget["title"]]; mapping.SourceField != "name" || mapping.Transformation != "derived" {
		t.Fatalf("default: %+v", mapping)
	}
	if mapping := lineage.Mappings[byTarget["label"]]; mapping.SourceField != "" || mapping.InputPort != "" || mapping.Transformation != "generated" {
		t.Fatalf("constant: %+v", mapping)
	}
}

func TestTableFieldLineageDoesNotGuessAbsentFields(t *testing.T) {
	fields := []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeString}}
	info := &datatype.TableInfo{Fields: fields}
	if result := buildTableFieldLineage(fields, nil, info, info, nil); result != nil {
		t.Fatal("missing target schema claimed complete")
	}
	if result := buildTableFieldLineage(fields, fields, &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "absent", Type: datatype.FieldTypeString}}}, info, nil); result != nil {
		t.Fatal("missing source field claimed complete")
	}
}
