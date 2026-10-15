package oracle

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/shared/writetest"
)

func TestOracleWriteDefinition(t *testing.T) {
	checker, ok := interface{}(&OraclePlugin{}).(plugin.TableWriteDefinitionChecker)
	if !ok {
		t.Fatal("Oracle pure definition checker unavailable")
	}
	constant := func(v interface{}) *plugin.TableWriteConstant { return &plugin.TableWriteConstant{Value: v} }
	observed := func(native string) *datatype.TableInfo {
		return &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "VALUE", NativeType: native, Nullable: true}}}
	}
	for _, tc := range []struct {
		name     string
		field    datatype.FieldInfo
		value    *plugin.TableWriteConstant
		source   *datatype.FieldInfo
		existing *datatype.TableInfo
		status   plugin.DefinitionCheckStatus
		code     string
	}{
		{"JSON container", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, constant(map[string]interface{}{"a": []interface{}{true, nil}}), nil, nil, plugin.DefinitionSupported, "constant_encoding_supported"},
		{"encoded JSON", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, constant(json.RawMessage(`{"n":9007199254740993}`)), nil, nil, plugin.DefinitionSupported, "constant_encoding_supported"},
		{"invalid JSON", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, constant("{"), nil, nil, plugin.DefinitionUnsupported, "constant_encoding_invalid"},
		{"SQL NULL", datatype.FieldInfo{Type: datatype.FieldTypeJSON, Nullable: true}, constant(nil), nil, nil, plugin.DefinitionSupported, "constant_encoding_supported"},
		{"typed JSON NULL", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, constant(json.RawMessage(nil)), nil, nil, plugin.DefinitionUnsupported, "null_constant_not_nullable"},
		{"document null", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, constant([]interface{}(nil)), nil, nil, plugin.DefinitionSupported, "constant_encoding_supported"},
		{"primary NULL", datatype.FieldInfo{Type: datatype.FieldTypeBigInt, PrimaryKey: true, Nullable: true}, constant(nil), nil, nil, plugin.DefinitionUnsupported, "null_constant_not_nullable"},
		{"binary", datatype.FieldInfo{Type: datatype.FieldTypeBytes}, constant([]byte{0, 255}), nil, nil, plugin.DefinitionSupported, "constant_encoding_supported"},
		{"empty binary not certified", datatype.FieldInfo{Type: datatype.FieldTypeBytes}, constant([]byte{}), nil, nil, plugin.DefinitionUnknown, "constant_value_not_checked"},
		{"nil binary not certified", datatype.FieldInfo{Type: datatype.FieldTypeBytes}, constant([]byte(nil)), nil, nil, plugin.DefinitionUnknown, "constant_value_not_checked"},
		{"array", datatype.FieldInfo{Type: datatype.FieldTypeArray}, nil, nil, nil, plugin.DefinitionUnsupported, "field_definition_unsupported"},
		{"time", datatype.FieldInfo{Type: datatype.FieldTypeTime}, nil, nil, nil, plugin.DefinitionUnsupported, "field_definition_unsupported"},
		{"decimal", datatype.FieldInfo{Type: datatype.FieldTypeDecimal, Precision: 39}, nil, nil, nil, plugin.DefinitionUnsupported, "decimal_definition_invalid"},
		{"binary key", datatype.FieldInfo{Type: datatype.FieldTypeBytes, PrimaryKey: true}, nil, nil, nil, plugin.DefinitionUnsupported, "field_definition_unsupported"},
		{"JSON key unproven", datatype.FieldInfo{Type: datatype.FieldTypeJSON, PrimaryKey: true}, nil, nil, nil, plugin.DefinitionUnknown, "primary_key_type_not_proven"},
		{"JSON source", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, &datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, plugin.DefinitionSupported, "source_assignment_supported"},
		{"text source", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, &datatype.FieldInfo{Type: datatype.FieldTypeString}, nil, plugin.DefinitionUnknown, "json_representation_not_proven"},
		{"array source", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, &datatype.FieldInfo{Type: datatype.FieldTypeArray}, nil, plugin.DefinitionUnknown, "json_representation_not_proven"},
		{"native JSON", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, nil, observed("JSON"), plugin.DefinitionSupported, "existing_column_compatible"},
		{"text is not native JSON", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, nil, observed("CLOB"), plugin.DefinitionUnsupported, "existing_column_incompatible"},
		{"native BLOB", datatype.FieldInfo{Type: datatype.FieldTypeBytes}, nil, nil, observed("BLOB"), plugin.DefinitionSupported, "existing_column_compatible"},
		{"RAW is not BLOB", datatype.FieldInfo{Type: datatype.FieldTypeBytes}, nil, nil, observed("RAW(20)"), plugin.DefinitionUnsupported, "existing_column_incompatible"},
		{"missing native", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, nil, observed(""), plugin.DefinitionUnknown, "existing_native_type_missing"},
		{"unknown string length semantics", datatype.FieldInfo{Type: datatype.FieldTypeString}, nil, nil, observed("VARCHAR2(4000)"), plugin.DefinitionUnknown, "existing_column_type_not_proven"},
		{"missing decimal facts", datatype.FieldInfo{Type: datatype.FieldTypeDecimal, Precision: 20, Scale: 2}, nil, nil, observed("NUMBER"), plugin.DefinitionUnknown, "existing_numeric_facts_missing"},
		{"nullable addition", datatype.FieldInfo{Type: datatype.FieldTypeJSON, Nullable: true}, nil, nil, &datatype.TableInfo{}, plugin.DefinitionSupported, "nullable_column_addition_supported"},
		{"required addition", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, nil, &datatype.TableInfo{}, plugin.DefinitionUnsupported, "column_addition_unsupported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.field.Name = "VALUE"
			definition := plugin.TableWriteDefinition{Fields: []plugin.TableWriteFieldDefinition{{Target: tc.field, Constant: tc.value, Source: tc.source}}, Existing: tc.existing}
			before, _ := json.Marshal(definition)
			report := checker.CheckTableWriteDefinition(definition)
			last := report.Checks[len(report.Checks)-1]
			if last.Status != tc.status || last.Code != tc.code || (report.UnsupportedError() != nil) != (tc.status == plugin.DefinitionUnsupported) {
				t.Fatalf("report=%+v want=%s %s", report, tc.status, tc.code)
			}
			after, _ := json.Marshal(definition)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("checker mutated facts")
			}
		})
	}
	writetest.PureDefinitionConstants(t, checker)
	for _, fields := range [][]datatype.FieldInfo{
		nil, {{Name: "VALUE", Type: datatype.FieldTypeJSON}, {Name: "VALUE", Type: datatype.FieldTypeJSON}},
		{{Name: "", Type: datatype.FieldTypeJSON}}, {{Name: "nul\x00name", Type: datatype.FieldTypeJSON}},
	} {
		definition := plugin.TableWriteDefinition{}
		for _, field := range fields {
			definition.Fields = append(definition.Fields, plugin.TableWriteFieldDefinition{Target: field})
		}
		if checker.CheckTableWriteDefinition(definition).UnsupportedError() == nil {
			t.Fatalf("invalid field names accepted: %+v", fields)
		}
	}
	spatial := datatype.NewSingleGeometrySpatialInfo("SHAPE", "Point", 4326, 2)
	extent := datatype.NewBoundingBox(0, 0, 1, 1)
	spatial.Extent = &extent
	definition := plugin.TableWriteDefinition{Fields: []plugin.TableWriteFieldDefinition{{Target: datatype.FieldInfo{Name: "SHAPE", Type: datatype.FieldTypeGeometry, Nullable: true}}}, SpatialInfo: spatial}
	if err := checker.CheckTableWriteDefinition(definition).UnsupportedError(); err != nil {
		t.Fatal(err)
	}
	spatial.Extent = nil
	if checker.CheckTableWriteDefinition(definition).UnsupportedError() == nil {
		t.Fatal("missing spatial extent accepted")
	}
}
