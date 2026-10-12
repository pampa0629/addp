package postgresql

import (
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestPostgresWriteDefinition(t *testing.T) {
	pg := &PostgreSQLPlugin{}
	for _, tc := range []struct {
		name     string
		target   datatype.FieldInfo
		source   *datatype.FieldInfo
		constant *plugin.TableWriteConstant
		existing *datatype.TableInfo
		status   plugin.DefinitionCheckStatus
		code     string
		physical string
	}{
		{name: "json object array constant", target: datatype.FieldInfo{Type: datatype.FieldTypeJSON}, constant: &plugin.TableWriteConstant{Value: []interface{}{map[string]interface{}{"a": 1}}}, status: plugin.DefinitionSupported, code: "constant_encoding_supported", physical: "JSONB"},
		{name: "invalid JSON", target: datatype.FieldInfo{Type: datatype.FieldTypeJSON}, constant: &plugin.TableWriteConstant{Value: "{"}, status: plugin.DefinitionUnsupported, code: "constant_encoding_invalid", physical: "JSONB"},
		{name: "container needs explicit json", target: datatype.FieldInfo{Type: datatype.FieldTypeString}, constant: &plugin.TableWriteConstant{Value: map[string]interface{}{"a": 1}}, status: plugin.DefinitionUnsupported, code: "constant_encoding_invalid", physical: "TEXT"},
		{name: "integer cast not certified", target: datatype.FieldInfo{Type: datatype.FieldTypeInt}, constant: &plugin.TableWriteConstant{Value: "invalid-integer"}, status: plugin.DefinitionUnknown, code: "constant_value_not_checked", physical: "INTEGER"},
		{name: "text NUL rejected", target: datatype.FieldInfo{Type: datatype.FieldTypeString}, constant: &plugin.TableWriteConstant{Value: "invalid\x00text"}, status: plugin.DefinitionUnsupported, code: "constant_encoding_invalid", physical: "TEXT"},
		{name: "NULL JSON", target: datatype.FieldInfo{Type: datatype.FieldTypeJSON, Nullable: true}, constant: &plugin.TableWriteConstant{Value: nil}, status: plugin.DefinitionSupported, code: "constant_encoding_supported", physical: "JSONB"},
		{name: "nonnullable NULL", target: datatype.FieldInfo{Type: datatype.FieldTypeJSON}, constant: &plugin.TableWriteConstant{Value: nil}, status: plugin.DefinitionUnsupported, code: "null_constant_not_nullable", physical: "JSONB"},
		{name: "nonnullable typed NULL", target: datatype.FieldInfo{Type: datatype.FieldTypeArray}, constant: &plugin.TableWriteConstant{Value: []string(nil)}, status: plugin.DefinitionUnsupported, code: "null_constant_not_nullable", physical: "TEXT[]"},
		{name: "unknown target", target: datatype.FieldInfo{Type: "unrecognized"}, status: plugin.DefinitionUnknown, code: "target_type_unknown", physical: "TEXT"},
		{name: "unknown target with existing column", target: datatype.FieldInfo{Type: datatype.FieldTypeUnknown}, existing: &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "value", Type: datatype.FieldTypeInt, NativeType: "integer"}}}, status: plugin.DefinitionUnknown, code: "existing_column_type_not_proven", physical: "TEXT"},
		{name: "object array", target: datatype.FieldInfo{Type: datatype.FieldTypeArray, ElementType: datatype.FieldTypeJSON}, status: plugin.DefinitionUnsupported, code: "array_element_type_unsupported"},
		{name: "string array", target: datatype.FieldInfo{Type: datatype.FieldTypeArray, ElementType: datatype.FieldTypeString}, source: &datatype.FieldInfo{Type: datatype.FieldTypeArray, ElementType: datatype.FieldTypeString}, status: plugin.DefinitionSupported, code: "source_assignment_supported", physical: "TEXT[]"},
		{name: "unknown array elements", target: datatype.FieldInfo{Type: datatype.FieldTypeArray}, source: &datatype.FieldInfo{Type: datatype.FieldTypeArray}, status: plugin.DefinitionUnknown, code: "array_element_type_unknown", physical: "TEXT[]"},
		{name: "native array to json", target: datatype.FieldInfo{Type: datatype.FieldTypeJSON}, source: &datatype.FieldInfo{Type: datatype.FieldTypeArray, NativeType: "text[]"}, status: plugin.DefinitionUnknown, code: "json_representation_not_proven", physical: "JSONB"},
		{name: "text to json needs valid document", target: datatype.FieldInfo{Type: datatype.FieldTypeJSON}, source: &datatype.FieldInfo{Type: datatype.FieldTypeString}, status: plugin.DefinitionUnknown, code: "json_representation_not_proven", physical: "JSONB"},
		{name: "bytes", target: datatype.FieldInfo{Type: datatype.FieldTypeBytes}, source: &datatype.FieldInfo{Type: datatype.FieldTypeBytes}, status: plugin.DefinitionSupported, code: "source_assignment_supported", physical: "BYTEA"},
		{name: "geometry", target: datatype.FieldInfo{Type: datatype.FieldTypeGeometry}, source: &datatype.FieldInfo{Type: datatype.FieldTypeGeometry}, status: plugin.DefinitionSupported, code: "source_assignment_supported", physical: "GEOMETRY(Geometry)"},
		{name: "decimal invalid", target: datatype.FieldInfo{Type: datatype.FieldTypeDecimal, Precision: 2, Scale: 3}, status: plugin.DefinitionUnsupported, code: "decimal_definition_invalid"},
		{name: "existing integer array", target: datatype.FieldInfo{Type: datatype.FieldTypeArray}, existing: &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "value", Type: datatype.FieldTypeArray, NativeType: "integer[]"}}}, status: plugin.DefinitionUnsupported, code: "existing_column_incompatible", physical: "TEXT[]"},
		{name: "existing text array", target: datatype.FieldInfo{Type: datatype.FieldTypeArray}, existing: &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "value", Type: datatype.FieldTypeArray, NativeType: "text[]"}}}, status: plugin.DefinitionSupported, code: "existing_column_compatible", physical: "TEXT[]"},
		{name: "missing native facts", target: datatype.FieldInfo{Type: datatype.FieldTypeArray}, existing: &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "value", Type: datatype.FieldTypeArray}}}, status: plugin.DefinitionUnknown, code: "existing_native_type_missing", physical: "TEXT[]"},
		{name: "missing numeric modifiers", target: datatype.FieldInfo{Type: datatype.FieldTypeDecimal}, existing: &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "value", Type: datatype.FieldTypeDecimal, NativeType: "numeric(8,2)"}}}, status: plugin.DefinitionUnknown, code: "existing_numeric_facts_missing", physical: "NUMERIC"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.target.Name = "value"
			report := pg.CheckTableWriteDefinition(plugin.TableWriteDefinition{Fields: []plugin.TableWriteFieldDefinition{{Target: tc.target, Source: tc.source, Constant: tc.constant}}, Existing: tc.existing})
			last := report.Checks[len(report.Checks)-1]
			if last.Status != tc.status || last.Code != tc.code || last.PhysicalType != tc.physical {
				t.Fatalf("check = %#v; want %s %s %s", last, tc.status, tc.code, tc.physical)
			}
			if (report.UnsupportedError() != nil) != (tc.status == plugin.DefinitionUnsupported) {
				t.Fatalf("unexpected error: %v", report.UnsupportedError())
			}
		})
	}
}
