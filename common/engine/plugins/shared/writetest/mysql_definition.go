package writetest

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

// MySQLNonSpatialDefinition verifies the shared contract at each real Provider.
func MySQLNonSpatialDefinition(t *testing.T, checker plugin.TableWriteDefinitionChecker) {
	t.Helper()
	constant := func(v interface{}) *plugin.TableWriteConstant { return &plugin.TableWriteConstant{Value: v} }
	existing := func(native string, p, s int) *datatype.TableInfo {
		return &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "value", NativeType: native, Precision: p, Scale: s}}}
	}
	for _, tc := range []struct {
		name           string
		target         datatype.FieldInfo
		source         *datatype.FieldInfo
		constant       *plugin.TableWriteConstant
		existing       *datatype.TableInfo
		status         plugin.DefinitionCheckStatus
		code, physical string
	}{
		{"json object", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, constant(map[string]interface{}{"a": []interface{}{true, json.Number("9007199254740993")}}), nil, plugin.DefinitionSupported, "constant_encoding_supported", "JSON"},
		{"json encoded", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, constant(`{"a":1}`), nil, plugin.DefinitionSupported, "constant_encoding_supported", "JSON"},
		{"json invalid", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, constant(`{`), nil, plugin.DefinitionUnsupported, "constant_encoding_invalid", "JSON"},
		{"JSON null", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, constant([]interface{}(nil)), nil, plugin.DefinitionSupported, "constant_encoding_supported", "JSON"},
		{"SQL null nonnullable", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, constant(json.RawMessage(nil)), nil, plugin.DefinitionUnsupported, "null_constant_not_nullable", "JSON"},
		{"nullable SQL null", datatype.FieldInfo{Type: datatype.FieldTypeJSON, Nullable: true}, nil, constant(nil), nil, plugin.DefinitionSupported, "constant_encoding_supported", "JSON"},
		{"bytes nil nonnullable", datatype.FieldInfo{Type: datatype.FieldTypeBytes}, nil, constant([]byte(nil)), nil, plugin.DefinitionUnsupported, "null_constant_not_nullable", "BLOB"},
		{"bytes preserved", datatype.FieldInfo{Type: datatype.FieldTypeBytes}, nil, constant([]byte{0, 255}), nil, plugin.DefinitionSupported, "constant_encoding_supported", "BLOB"},
		{"text container", datatype.FieldInfo{Type: datatype.FieldTypeString}, nil, constant([]interface{}{1}), nil, plugin.DefinitionUnsupported, "constant_encoding_invalid", "TEXT"},
		{"text NUL", datatype.FieldInfo{Type: datatype.FieldTypeString}, nil, constant("a\x00b"), nil, plugin.DefinitionSupported, "constant_encoding_supported", "TEXT"},
		{"unknown source", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, &datatype.FieldInfo{Type: datatype.FieldTypeUnknown}, nil, nil, plugin.DefinitionUnknown, "source_type_unknown", "JSON"},
		{"text not JSON proof", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, &datatype.FieldInfo{Type: datatype.FieldTypeString}, nil, nil, plugin.DefinitionUnknown, "json_representation_not_proven", "JSON"},
		{"array not JSON proof", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, &datatype.FieldInfo{Type: datatype.FieldTypeArray}, nil, nil, plugin.DefinitionUnknown, "json_representation_not_proven", "JSON"},
		{"scalar JSON encoding", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, &datatype.FieldInfo{Type: datatype.FieldTypeBool}, nil, nil, plugin.DefinitionSupported, "json_encoding_supported", "JSON"},
		{"native array unsupported", datatype.FieldInfo{Type: datatype.FieldTypeArray}, nil, nil, nil, plugin.DefinitionUnsupported, "field_definition_unsupported", ""},
		{"geometry unsupported", datatype.FieldInfo{Type: datatype.FieldTypeGeometry}, nil, nil, nil, plugin.DefinitionUnsupported, "field_definition_unsupported", ""},
		{"decimal invalid", datatype.FieldInfo{Type: datatype.FieldTypeDecimal, Precision: 66, Scale: 0}, nil, nil, nil, plugin.DefinitionUnsupported, "decimal_definition_invalid", ""},
		{"decimal scale invalid", datatype.FieldInfo{Type: datatype.FieldTypeDecimal, Precision: 65, Scale: 31}, nil, nil, nil, plugin.DefinitionUnsupported, "decimal_definition_invalid", ""},
		{"decimal supported", datatype.FieldInfo{Type: datatype.FieldTypeDecimal, Precision: 65, Scale: 30}, nil, nil, nil, plugin.DefinitionSupported, "field_definition_supported", "DECIMAL(65,30)"},
		{"existing JSON", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, nil, existing("json", 0, 0), plugin.DefinitionSupported, "existing_column_compatible", "JSON"},
		{"existing text", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, nil, existing("text", 0, 0), plugin.DefinitionUnsupported, "existing_column_incompatible", "JSON"},
		{"existing missing native", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, nil, existing("", 0, 0), plugin.DefinitionUnknown, "existing_native_type_missing", "JSON"},
		{"existing decimal", datatype.FieldInfo{Type: datatype.FieldTypeDecimal, Precision: 18, Scale: 2}, nil, nil, existing("decimal(18,2)", 18, 2), plugin.DefinitionSupported, "existing_column_compatible", "DECIMAL(18,2)"},
		{"existing numeric missing", datatype.FieldInfo{Type: datatype.FieldTypeDecimal, Precision: 18, Scale: 2}, nil, nil, existing("decimal(18,2)", 0, 0), plugin.DefinitionUnknown, "existing_numeric_facts_missing", "DECIMAL(18,2)"},
		{"timestamp incompatible without precision", datatype.FieldInfo{Type: datatype.FieldTypeTimestamp}, nil, nil, existing("bigint", 0, 0), plugin.DefinitionUnsupported, "existing_column_incompatible", "DATETIME(6)"},
		{"decimal incompatible without precision", datatype.FieldInfo{Type: datatype.FieldTypeDecimal, Precision: 18, Scale: 2}, nil, nil, existing("json", 0, 0), plugin.DefinitionUnsupported, "existing_column_incompatible", "DECIMAL(18,2)"},
		{"existing timestamp", datatype.FieldInfo{Type: datatype.FieldTypeTimestamp}, nil, nil, existing("datetime(6)", 0, 0), plugin.DefinitionSupported, "existing_column_compatible", "DATETIME(6)"},
		{"existing timestamp lossy", datatype.FieldInfo{Type: datatype.FieldTypeTimestamp}, nil, nil, existing("datetime(3)", 0, 0), plugin.DefinitionUnsupported, "existing_column_incompatible", "DATETIME(6)"},
		{"existing timestamp unproven", datatype.FieldInfo{Type: datatype.FieldTypeTimestamp}, nil, nil, existing("datetime", 0, 0), plugin.DefinitionUnknown, "existing_column_type_not_proven", "DATETIME(6)"},
		{"nullable addition", datatype.FieldInfo{Type: datatype.FieldTypeJSON, Nullable: true}, nil, nil, &datatype.TableInfo{}, plugin.DefinitionSupported, "nullable_column_addition_supported", "JSON"},
		{"nonnullable addition", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, nil, &datatype.TableInfo{}, plugin.DefinitionUnsupported, "column_addition_unsupported", "JSON"},
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
				t.Fatal("unsupported report mismatch")
			}
			after, _ := json.Marshal(definition)
			if string(before) != string(after) {
				t.Fatal("supplied facts mutated")
			}
		})
	}
	for _, definition := range []plugin.TableWriteDefinition{
		{},
		{Fields: []plugin.TableWriteFieldDefinition{{Target: datatype.FieldInfo{Name: "  "}}}},
		{Fields: []plugin.TableWriteFieldDefinition{{Target: datatype.FieldInfo{Name: "value"}}, {Target: datatype.FieldInfo{Name: " value "}}}},
		{Fields: []plugin.TableWriteFieldDefinition{{Target: datatype.FieldInfo{Name: "value", Type: datatype.FieldTypeString}}}, SpatialInfo: datatype.NewSingleGeometrySpatialInfo("value", "Point", 4326, 0)},
		{Fields: []plugin.TableWriteFieldDefinition{{Target: datatype.FieldInfo{Name: "value", Type: datatype.FieldTypeString}}}, SpatialInfo: &datatype.SpatialInfo{GeometryColumns: []datatype.GeometryColumnInfo{{Name: "other"}, {Name: "another"}, {}}}},
	} {
		first := checker.CheckTableWriteDefinition(definition)
		if first.UnsupportedError() == nil {
			t.Fatalf("invalid definition accepted: %+v", definition)
		}
		for i := 0; i < 10; i++ {
			if !reflect.DeepEqual(first, checker.CheckTableWriteDefinition(definition)) {
				t.Fatal("report order is unstable")
			}
		}
	}
}
