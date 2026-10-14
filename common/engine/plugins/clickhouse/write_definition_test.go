package clickhouse

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/shared/writetest"
)

func TestClickHouseWriteDefinitionRejectsBeforeTargetEffects(t *testing.T) {
	p := &ClickHousePlugin{}
	for _, fields := range [][]datatype.FieldInfo{
		{{Name: "payload", Type: datatype.FieldTypeArray}},
		{{Name: "amount", Type: datatype.FieldTypeDecimal, Precision: 2, Scale: 3}},
		{{Name: "shape", Type: datatype.FieldTypeGeometry}},
		{{Name: "  ", Type: datatype.FieldTypeString}},
		{{Name: "id", Type: datatype.FieldTypeInt}, {Name: " id ", Type: datatype.FieldTypeInt}},
		{{Name: "computed", Type: datatype.FieldTypeInt, Generated: true}},
	} {
		err := p.PrepareTableWrite(t.Context(), nil, plugin.EngineCatalogPath{}, plugin.TableWriteOptions{Fields: fields})
		if err == nil || !strings.Contains(err.Error(), "target_definition") {
			t.Errorf("invalid definition reached connection/path: fields=%+v error=%v", fields, err)
			continue
		}
		if err := createClickHouseTableIfNotExists(t.Context(), nil, "synthetic", "target", fields); err == nil {
			t.Fatal("invalid definition reached DDL")
		}
		if _, err := p.OpenTableWriteSession(t.Context(), nil, plugin.EngineCatalogPath{}, plugin.TableWriteSessionOptions{Fields: fields}); err == nil || !strings.Contains(err.Error(), "target_definition") {
			t.Fatalf("invalid definition reached session: %v", err)
		}
		if err := p.WriteBatch(t.Context(), nil, plugin.EngineCatalogPath{}, &plugin.BatchData{Fields: fields, Rows: []map[string]interface{}{{"payload": []int{1}}}}, plugin.BatchWriteOptions{}); err == nil || !strings.Contains(err.Error(), "target_definition") {
			t.Fatalf("invalid definition reached batch: %v", err)
		}
	}
}

func TestClickHouseWriteDefinition(t *testing.T) {
	checker, ok := interface{}(&ClickHousePlugin{}).(plugin.TableWriteDefinitionChecker)
	if !ok {
		t.Fatal("ClickHouse pure checker unavailable")
	}
	constant := func(v interface{}) *plugin.TableWriteConstant { return &plugin.TableWriteConstant{Value: v} }
	observed := func(native string) *datatype.TableInfo {
		return &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "value", NativeType: native}}}
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
		{"encoded json", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, constant(json.RawMessage(`{"a":1}`)), nil, plugin.DefinitionSupported, "constant_encoding_supported"},
		{"invalid json", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, constant("{"), nil, plugin.DefinitionUnsupported, "constant_encoding_invalid"},
		{"nullable null", datatype.FieldInfo{Type: datatype.FieldTypeJSON, Nullable: true}, nil, constant(nil), nil, plugin.DefinitionSupported, "constant_encoding_supported"},
		{"nonnullable null", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, constant([]byte(nil)), nil, plugin.DefinitionUnsupported, "null_constant_not_nullable"},
		{"bytes", datatype.FieldInfo{Type: datatype.FieldTypeBytes}, nil, constant([]byte{0, 255}), nil, plugin.DefinitionSupported, "constant_encoding_supported"},
		{"bytes nil is null", datatype.FieldInfo{Type: datatype.FieldTypeBytes}, nil, constant([]byte(nil)), nil, plugin.DefinitionUnsupported, "null_constant_not_nullable"},
		{"string nil bytes cast unknown", datatype.FieldInfo{Type: datatype.FieldTypeString}, nil, constant([]byte(nil)), nil, plugin.DefinitionUnknown, "constant_value_not_checked"},
		{"string nil raw cast unknown", datatype.FieldInfo{Type: datatype.FieldTypeString}, nil, constant(json.RawMessage(nil)), nil, plugin.DefinitionUnknown, "constant_value_not_checked"},
		{"bytes nil raw cast unknown", datatype.FieldInfo{Type: datatype.FieldTypeBytes}, nil, constant(json.RawMessage(nil)), nil, plugin.DefinitionUnknown, "constant_value_not_checked"},
		{"bytes slice cast unknown", datatype.FieldInfo{Type: datatype.FieldTypeBytes}, nil, constant([]int{1}), nil, plugin.DefinitionUnknown, "constant_value_not_checked"},
		{"unsigned native value unknown", datatype.FieldInfo{Type: datatype.FieldTypeBigInt}, nil, constant(uint64(math.MaxUint64)), nil, plugin.DefinitionUnknown, "constant_value_not_checked"},
		{"numeric cast unknown", datatype.FieldInfo{Type: datatype.FieldTypeInt}, nil, constant("12"), nil, plugin.DefinitionUnknown, "constant_value_not_checked"},
		{"array", datatype.FieldInfo{Type: datatype.FieldTypeArray}, nil, nil, nil, plugin.DefinitionUnsupported, "field_definition_unsupported"},
		{"decimal invalid", datatype.FieldInfo{Type: datatype.FieldTypeDecimal, Precision: 39}, nil, nil, nil, plugin.DefinitionUnsupported, "decimal_definition_invalid"},
		{"mixed", datatype.FieldInfo{Type: datatype.FieldTypeMixed}, nil, nil, nil, plugin.DefinitionUnknown, "target_type_unknown"},
		{"source json", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, &datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, nil, plugin.DefinitionSupported, "source_assignment_supported"},
		{"source text", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, &datatype.FieldInfo{Type: datatype.FieldTypeString}, nil, nil, plugin.DefinitionUnknown, "json_representation_not_proven"},
		{"source array", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, &datatype.FieldInfo{Type: datatype.FieldTypeArray}, nil, nil, plugin.DefinitionUnknown, "json_representation_not_proven"},
		{"source decimal", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, &datatype.FieldInfo{Type: datatype.FieldTypeDecimal}, nil, nil, plugin.DefinitionUnknown, "json_representation_not_proven"},
		{"source bool", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, &datatype.FieldInfo{Type: datatype.FieldTypeBool}, nil, nil, plugin.DefinitionSupported, "json_encoding_supported"},
		{"existing json string", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, nil, observed("Nullable(String)"), plugin.DefinitionSupported, "existing_column_compatible"},
		{"existing fixed string", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, nil, observed("FixedString(20)"), plugin.DefinitionUnsupported, "existing_column_incompatible"},
		{"existing array", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, nil, observed("Array(String)"), plugin.DefinitionUnsupported, "existing_column_incompatible"},
		{"existing native json", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, nil, observed("JSON"), plugin.DefinitionUnsupported, "existing_column_incompatible"},
		{"existing facts missing", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, nil, observed(""), plugin.DefinitionUnknown, "existing_native_type_missing"},
		{"existing decimal facts missing", datatype.FieldInfo{Type: datatype.FieldTypeDecimal, Precision: 18, Scale: 2}, nil, nil, observed("Decimal"), plugin.DefinitionUnknown, "existing_numeric_facts_missing"},
		{"existing decimal", datatype.FieldInfo{Type: datatype.FieldTypeDecimal, Precision: 18, Scale: 2}, nil, nil, observed("Nullable(Decimal64(2))"), plugin.DefinitionSupported, "existing_column_compatible"},
		{"existing decimal incompatible", datatype.FieldInfo{Type: datatype.FieldTypeDecimal, Precision: 18, Scale: 2}, nil, nil, observed("Decimal(20,2)"), plugin.DefinitionUnsupported, "existing_column_incompatible"},
		{"nullable addition", datatype.FieldInfo{Type: datatype.FieldTypeJSON, Nullable: true}, nil, nil, &datatype.TableInfo{}, plugin.DefinitionSupported, "nullable_column_addition_supported"},
		{"nonnullable addition", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, nil, &datatype.TableInfo{}, plugin.DefinitionUnsupported, "column_addition_unsupported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.field.Name = "value"
			definition := plugin.TableWriteDefinition{Fields: []plugin.TableWriteFieldDefinition{{Target: tc.field, Source: tc.source, Constant: tc.value}}, Existing: tc.existing}
			before, _ := json.Marshal(definition)
			report := checker.CheckTableWriteDefinition(definition)
			last := report.Checks[len(report.Checks)-1]
			if last.Field != "value" || last.Status != tc.status || last.Code != tc.code {
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

func TestClickHouseWriteDefinitionProjectionAndSpatial(t *testing.T) {
	checker, ok := interface{}(&ClickHousePlugin{}).(plugin.TableWriteDefinitionChecker)
	if !ok {
		t.Fatal("ClickHouse pure checker unavailable")
	}
	fields := []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeInt}, {Name: "ID", Type: datatype.FieldTypeInt}, {Name: "computed", Type: datatype.FieldTypeArray, Generated: true}}
	definition := plugin.TableWriteDefinitionFromOptions(plugin.TableWriteOptions{Fields: fields})
	report := checker.CheckTableWriteDefinition(definition)
	if report.UnsupportedError() != nil || len(report.Checks) != 2 {
		t.Fatalf("case-sensitive names/generated projection changed: %+v", report)
	}
	index := true
	for _, spatial := range []*datatype.SpatialInfo{datatype.NewSingleGeometrySpatialInfo("missing", "Point", 4326, 2), {HasSpatialIndex: &index}} {
		opts := plugin.TableWriteOptions{Fields: fields, SpatialInfo: spatial}
		if checker.CheckTableWriteDefinition(plugin.TableWriteDefinitionFromOptions(opts)).UnsupportedError() == nil {
			t.Fatal("spatial metadata accepted")
		}
		if _, err := (&ClickHousePlugin{}).OpenTableWriteSession(t.Context(), nil, plugin.EngineCatalogPath{}, plugin.TableWriteSessionOptions{Fields: fields, SpatialInfo: spatial}); err == nil || !strings.Contains(err.Error(), "target_definition") {
			t.Fatalf("spatial metadata reached session: %v", err)
		}
	}
}
