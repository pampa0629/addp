package dameng

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/shared/writetest"
)

func TestDM8WriteDefinition(t *testing.T) {
	checker, ok := interface{}(&Plugin{}).(plugin.TableWriteDefinitionChecker)
	if !ok {
		t.Fatal("DM8 pure checker unavailable")
	}
	constant := func(v interface{}) *plugin.TableWriteConstant { return &plugin.TableWriteConstant{Value: v} }
	observed := func(native string, precision, scale int) *datatype.TableInfo {
		return &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "VALUE", NativeType: native, Precision: precision, Scale: scale, Nullable: true}}}
	}
	for _, tc := range []struct {
		name     string
		field    datatype.FieldInfo
		source   *datatype.FieldInfo
		value    *plugin.TableWriteConstant
		existing *datatype.TableInfo
		status   plugin.DefinitionCheckStatus
		code     string
	}{
		{"json", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, constant(map[string]interface{}{"a": []interface{}{true, nil}}), nil, plugin.DefinitionSupported, "constant_encoding_supported"},
		{"encoded", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, constant(json.RawMessage(`{"a":1}`)), nil, plugin.DefinitionSupported, "constant_encoding_supported"},
		{"invalid", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, constant("{"), nil, plugin.DefinitionUnsupported, "constant_encoding_invalid"},
		{"json null", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, constant(json.RawMessage(nil)), nil, plugin.DefinitionUnsupported, "null_constant_not_nullable"},
		{"document null", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, constant([]interface{}(nil)), nil, plugin.DefinitionSupported, "constant_encoding_supported"},
		{"nullable", datatype.FieldInfo{Type: datatype.FieldTypeJSON, Nullable: true}, nil, constant(nil), nil, plugin.DefinitionSupported, "constant_encoding_supported"},
		{"nullable key still required", datatype.FieldInfo{Type: datatype.FieldTypeBigInt, Nullable: true, PrimaryKey: true}, nil, constant(nil), nil, plugin.DefinitionUnsupported, "null_constant_not_nullable"},
		{"binary", datatype.FieldInfo{Type: datatype.FieldTypeBytes}, nil, constant([]byte{0, 255}), nil, plugin.DefinitionSupported, "constant_encoding_supported"},
		{"empty binary", datatype.FieldInfo{Type: datatype.FieldTypeBytes}, nil, constant([]byte{}), nil, plugin.DefinitionSupported, "constant_encoding_supported"},
		{"nil binary", datatype.FieldInfo{Type: datatype.FieldTypeBytes}, nil, constant([]byte(nil)), nil, plugin.DefinitionUnsupported, "null_constant_not_nullable"},
		{"cast", datatype.FieldInfo{Type: datatype.FieldTypeInt}, nil, constant("12"), nil, plugin.DefinitionUnknown, "constant_value_not_checked"},
		{"array", datatype.FieldInfo{Type: datatype.FieldTypeArray}, nil, nil, nil, plugin.DefinitionUnsupported, "field_definition_unsupported"},
		{"mixed", datatype.FieldInfo{Type: datatype.FieldTypeMixed}, nil, nil, nil, plugin.DefinitionUnsupported, "field_definition_unsupported"},
		{"json key", datatype.FieldInfo{Type: datatype.FieldTypeJSON, PrimaryKey: true}, nil, nil, nil, plugin.DefinitionUnsupported, "field_definition_unsupported"},
		{"binary key", datatype.FieldInfo{Type: datatype.FieldTypeBytes, PrimaryKey: true}, nil, nil, nil, plugin.DefinitionUnsupported, "field_definition_unsupported"},
		{"decimal", datatype.FieldInfo{Type: datatype.FieldTypeDecimal, Precision: 39}, nil, nil, nil, plugin.DefinitionUnsupported, "decimal_definition_invalid"},
		{"source json", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, &datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, nil, plugin.DefinitionSupported, "source_assignment_supported"},
		{"source text", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, &datatype.FieldInfo{Type: datatype.FieldTypeString}, nil, nil, plugin.DefinitionUnknown, "json_representation_not_proven"},
		{"source array", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, &datatype.FieldInfo{Type: datatype.FieldTypeArray}, nil, nil, plugin.DefinitionUnknown, "json_representation_not_proven"},
		{"source bool", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, &datatype.FieldInfo{Type: datatype.FieldTypeBool}, nil, nil, plugin.DefinitionSupported, "json_encoding_supported"},
		{"existing clob", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, nil, observed("CLOB", 0, 0), plugin.DefinitionSupported, "existing_column_compatible"},
		{"existing varchar", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, nil, observed("VARCHAR", 0, 0), plugin.DefinitionUnsupported, "existing_column_incompatible"},
		{"quoted case differs", datatype.FieldInfo{Type: datatype.FieldTypeJSON, Nullable: true}, nil, nil, &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "value", NativeType: "CLOB", Nullable: true}}}, plugin.DefinitionUnsupported, "existing_column_incompatible"},
		{"existing blob", datatype.FieldInfo{Type: datatype.FieldTypeBytes}, nil, nil, observed("BLOB", 0, 0), plugin.DefinitionSupported, "existing_column_compatible"},
		{"existing not null", datatype.FieldInfo{Type: datatype.FieldTypeJSON, Nullable: true}, nil, nil, &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "VALUE", NativeType: "CLOB"}}}, plugin.DefinitionUnsupported, "existing_column_incompatible"},
		{"existing missing key", datatype.FieldInfo{Type: datatype.FieldTypeBigInt, PrimaryKey: true}, nil, nil, observed("BIGINT", 0, 0), plugin.DefinitionUnsupported, "existing_column_incompatible"},
		{"missing native", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, nil, observed("", 0, 0), plugin.DefinitionUnknown, "existing_native_type_missing"},
		{"missing decimal facts", datatype.FieldInfo{Type: datatype.FieldTypeDecimal, Precision: 18, Scale: 2}, nil, nil, observed("DECIMAL", 0, 0), plugin.DefinitionUnknown, "existing_numeric_facts_missing"},
		{"matching decimal", datatype.FieldInfo{Type: datatype.FieldTypeDecimal, Precision: 18, Scale: 2}, nil, nil, observed("DECIMAL", 18, 2), plugin.DefinitionSupported, "existing_column_compatible"},
		{"different decimal", datatype.FieldInfo{Type: datatype.FieldTypeDecimal, Precision: 18, Scale: 2}, nil, nil, observed("DECIMAL", 18, 3), plugin.DefinitionUnsupported, "existing_column_incompatible"},
		{"string width unproven", datatype.FieldInfo{Type: datatype.FieldTypeString}, nil, nil, observed("VARCHAR", 0, 0), plugin.DefinitionUnknown, "existing_column_type_not_proven"},
		{"narrow int", datatype.FieldInfo{Type: datatype.FieldTypeInt}, nil, nil, observed("SMALLINT", 0, 0), plugin.DefinitionUnsupported, "existing_column_incompatible"},
		{"nullable addition", datatype.FieldInfo{Type: datatype.FieldTypeJSON, Nullable: true}, nil, nil, &datatype.TableInfo{}, plugin.DefinitionSupported, "nullable_column_addition_supported"},
		{"required addition", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, nil, &datatype.TableInfo{}, plugin.DefinitionUnsupported, "column_addition_unsupported"},
		{"key addition", datatype.FieldInfo{Type: datatype.FieldTypeInt, Nullable: true, PrimaryKey: true}, nil, nil, &datatype.TableInfo{}, plugin.DefinitionUnsupported, "column_addition_unsupported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.field.Name = "VALUE"
			definition := plugin.TableWriteDefinition{Fields: []plugin.TableWriteFieldDefinition{{Target: tc.field, Source: tc.source, Constant: tc.value}}, Existing: tc.existing}
			before, _ := json.Marshal(definition)
			report := checker.CheckTableWriteDefinition(definition)
			last := report.Checks[len(report.Checks)-1]
			if last.Field != "VALUE" || last.Status != tc.status || last.Code != tc.code {
				t.Fatalf("check=%+v want %s %s", last, tc.status, tc.code)
			}
			after, _ := json.Marshal(definition)
			if string(before) != string(after) || !reflect.DeepEqual(report, checker.CheckTableWriteDefinition(definition)) {
				t.Fatal("facts mutated or unstable report")
			}
		})
	}
	writetest.PureDefinitionConstants(t, checker)
}

func TestDM8WriteDefinitionRejectsBeforeEffects(t *testing.T) {
	p := &Plugin{}
	for _, fields := range [][]datatype.FieldInfo{
		nil, {{Name: "VALUE", Type: datatype.FieldTypeArray}}, {{Name: "VALUE", Type: datatype.FieldTypeGeometry}},
		{{Name: "VALUE", Type: datatype.FieldTypeDecimal, Precision: 2, Scale: 3}},
		{{Name: "VALUE", Type: datatype.FieldTypeInt}, {Name: "value", Type: datatype.FieldTypeInt}},
		{{Name: " VALUE ", Type: datatype.FieldTypeInt}},
		{{Name: "DOC", Type: datatype.FieldTypeJSON, PrimaryKey: true}},
		{{Name: "PAYLOAD", Type: datatype.FieldTypeBytes, PrimaryKey: true}},
	} {
		if err := p.PrepareTableWrite(t.Context(), nil, plugin.EngineCatalogPath{}, plugin.TableWriteOptions{Fields: fields}); err == nil || !strings.Contains(err.Error(), "target_definition") {
			t.Fatalf("definition reached path/connection: %v", err)
		}
		if _, err := createTableSQL("SYNTHETIC", "TARGET", fields); err == nil {
			t.Fatal("definition reached DDL")
		}
		if _, err := p.OpenTableWriteSession(t.Context(), nil, plugin.EngineCatalogPath{}, plugin.TableWriteSessionOptions{Fields: fields, Replace: true}); err == nil || !strings.Contains(err.Error(), "target_definition") {
			t.Fatalf("definition reached DELETE: %v", err)
		}
	}
	index := true
	if err := validateWriteFields([]datatype.FieldInfo{{Name: "VALUE", Type: datatype.FieldTypeJSON}}, &datatype.SpatialInfo{HasSpatialIndex: &index}); err == nil {
		t.Fatal("spatial index silently accepted")
	}
}

func TestDM8RequiredBinaryNullBeforeBatchWrite(t *testing.T) {
	fields := []datatype.FieldInfo{{Name: "VALUE", Type: datatype.FieldTypeBytes}}
	if _, err := damengWriteRows(t.Context(), fields, []map[string]interface{}{{"VALUE": []byte{}}, {"VALUE": []byte(nil)}}); err == nil || !strings.Contains(err.Error(), `row 1 field "VALUE" is NOT NULL`) {
		t.Fatalf("nil binary passed non-null guard: %v", err)
	}
}
