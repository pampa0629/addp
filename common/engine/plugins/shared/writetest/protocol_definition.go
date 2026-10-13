package writetest

import (
	"encoding/json"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

// NonSpatialProtocolDefinition exercises the same pure acceptance contract on
// the registered non-spatial PostgreSQL protocol Providers, without connections.
func NonSpatialProtocolDefinition(t *testing.T, checker plugin.TableWriteDefinitionChecker) {
	t.Helper()
	for _, tc := range []struct {
		name           string
		target         datatype.FieldInfo
		source         *datatype.FieldInfo
		constant       *plugin.TableWriteConstant
		existing       *datatype.TableInfo
		status         plugin.DefinitionCheckStatus
		code, physical string
	}{
		{"json object array", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, &plugin.TableWriteConstant{Value: []interface{}{map[string]interface{}{"a": true}}}, nil, plugin.DefinitionSupported, "constant_encoding_supported", "JSONB"},
		{"invalid JSON", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, &plugin.TableWriteConstant{Value: "{"}, nil, plugin.DefinitionUnsupported, "constant_encoding_invalid", "JSONB"},
		{"SQL NULL", datatype.FieldInfo{Type: datatype.FieldTypeJSON, Nullable: true}, nil, &plugin.TableWriteConstant{Value: nil}, nil, plugin.DefinitionSupported, "constant_encoding_supported", "JSONB"},
		{"required NULL", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, &plugin.TableWriteConstant{Value: nil}, nil, plugin.DefinitionUnsupported, "null_constant_not_nullable", "JSONB"},
		{"object array declaration", datatype.FieldInfo{Type: datatype.FieldTypeArray, ElementType: datatype.FieldTypeJSON}, nil, nil, nil, plugin.DefinitionUnsupported, "array_element_type_unsupported", ""},
		{"string array", datatype.FieldInfo{Type: datatype.FieldTypeArray, ElementType: datatype.FieldTypeString}, &datatype.FieldInfo{Type: datatype.FieldTypeArray, ElementType: datatype.FieldTypeString}, nil, nil, plugin.DefinitionSupported, "source_assignment_supported", "TEXT[]"},
		{"unknown array elements", datatype.FieldInfo{Type: datatype.FieldTypeArray}, &datatype.FieldInfo{Type: datatype.FieldTypeArray}, nil, nil, plugin.DefinitionUnknown, "array_element_type_unknown", "TEXT[]"},
		{"native array is not JSON", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, &datatype.FieldInfo{Type: datatype.FieldTypeArray, NativeType: "text[]"}, nil, nil, plugin.DefinitionUnknown, "json_representation_not_proven", "JSONB"},
		{"ordinary text is not proven JSON", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, &datatype.FieldInfo{Type: datatype.FieldTypeString}, nil, nil, plugin.DefinitionUnknown, "json_representation_not_proven", "JSONB"},
		{"bytes", datatype.FieldInfo{Type: datatype.FieldTypeBytes}, &datatype.FieldInfo{Type: datatype.FieldTypeBytes}, nil, nil, plugin.DefinitionSupported, "source_assignment_supported", "BYTEA"},
		{"geometry unsupported", datatype.FieldInfo{Type: datatype.FieldTypeGeometry}, nil, nil, nil, plugin.DefinitionUnsupported, "field_definition_unsupported", ""},
		{"decimal invalid", datatype.FieldInfo{Type: datatype.FieldTypeDecimal, Precision: 2, Scale: 3}, nil, nil, nil, plugin.DefinitionUnsupported, "decimal_definition_invalid", ""},
		{"existing integer array", datatype.FieldInfo{Type: datatype.FieldTypeArray}, nil, nil, &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "value", Type: datatype.FieldTypeArray, NativeType: "integer[]"}}}, plugin.DefinitionUnsupported, "existing_column_incompatible", "TEXT[]"},
		{"missing native fact", datatype.FieldInfo{Type: datatype.FieldTypeArray}, nil, nil, &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "value", Type: datatype.FieldTypeArray}}}, plugin.DefinitionUnknown, "existing_native_type_missing", "TEXT[]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.target.Name = "value"
			definition := plugin.TableWriteDefinition{Fields: []plugin.TableWriteFieldDefinition{{Target: tc.target, Source: tc.source, Constant: tc.constant}}, Existing: tc.existing}
			before, _ := json.Marshal(definition)
			report := checker.CheckTableWriteDefinition(definition)
			if len(report.Checks) == 0 {
				t.Fatal("empty report")
			}
			last := report.Checks[len(report.Checks)-1]
			if last.Status != tc.status || last.Code != tc.code || last.PhysicalType != tc.physical {
				t.Fatalf("check=%+v want %s %s %s", last, tc.status, tc.code, tc.physical)
			}
			if (report.UnsupportedError() != nil) != (tc.status == plugin.DefinitionUnsupported) {
				t.Fatal("unsupported status mismatch")
			}
			after, _ := json.Marshal(definition)
			if string(before) != string(after) {
				t.Fatal("checker mutated supplied facts")
			}
		})
	}
	if checker.CheckTableWriteDefinition(plugin.TableWriteDefinition{}).UnsupportedError() == nil {
		t.Fatal("empty fields accepted")
	}
	definition := plugin.TableWriteDefinition{Fields: []plugin.TableWriteFieldDefinition{{Target: datatype.FieldInfo{Name: "value", Type: datatype.FieldTypeString}}}, SpatialInfo: datatype.NewSingleGeometrySpatialInfo("value", "Point", 4326, 0)}
	if checker.CheckTableWriteDefinition(definition).UnsupportedError() == nil {
		t.Fatal("spatial declaration accepted")
	}
	definition.SpatialInfo = &datatype.SpatialInfo{GeometryColumns: []datatype.GeometryColumnInfo{{}}}
	if checker.CheckTableWriteDefinition(definition).UnsupportedError() == nil {
		t.Fatal("unnamed spatial declaration accepted")
	}
}
