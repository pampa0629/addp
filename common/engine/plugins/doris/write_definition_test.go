package doris

import (
	"database/sql/driver"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestDorisWriteDefinitionRejectsBeforeTargetEffects(t *testing.T) {
	p := &DorisPlugin{}
	for _, fields := range [][]datatype.FieldInfo{
		{{Name: "id", Type: datatype.FieldTypeBigInt}, {Name: "payload", Type: datatype.FieldTypeArray}},
		{{Name: "amount", Type: datatype.FieldTypeDecimal, Precision: 2, Scale: 3}},
		{{Name: "payload", Type: datatype.FieldTypeJSON}},
		{{Name: "value", Type: datatype.FieldTypeDouble}},
	} {
		err := p.PrepareTableWrite(t.Context(), nil, plugin.EngineCatalogPath{}, plugin.TableWriteOptions{Fields: fields})
		if err == nil || !strings.Contains(err.Error(), "target_definition") {
			t.Fatalf("invalid definition reached connection/path: fields=%+v error=%v", fields, err)
		}
		// Private DDL entry must reject before its first SQL as well.
		if err := createDorisTableIfNotExists(t.Context(), nil, "synthetic", "target", fields); err == nil {
			t.Fatal("invalid definition reached DDL")
		}
	}
	fields := []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}, {Name: "payload", Type: datatype.FieldTypeArray}}
	if _, err := p.OpenTableWriteSession(t.Context(), nil, plugin.EngineCatalogPath{}, plugin.TableWriteSessionOptions{Fields: fields}); err == nil || !strings.Contains(err.Error(), `column "payload"`) {
		t.Fatalf("array reached session: %v", err)
	}
	if err := p.WriteBatch(t.Context(), nil, plugin.EngineCatalogPath{}, &plugin.BatchData{Fields: fields, Rows: []map[string]interface{}{{"id": 1, "payload": []int{1}}}}, plugin.BatchWriteOptions{}); err == nil || !strings.Contains(err.Error(), `column "payload"`) {
		t.Fatalf("array reached batch: %v", err)
	}
}

func TestDorisWriteDefinitionSkipsFloatingKeyCandidates(t *testing.T) {
	fields := []datatype.FieldInfo{{Name: "score", Type: datatype.FieldTypeDouble}, {Name: "id", Type: datatype.FieldTypeBigInt}}
	key, err := dorisDuplicateKeyField(fields)
	if err != nil || key.Name != "id" {
		t.Fatalf("floating value chosen as key: %+v %v", key, err)
	}
}

func TestDorisWriteDefinitionRejectsAmbiguousDeclarations(t *testing.T) {
	p := &DorisPlugin{}
	index := true
	for _, opts := range []plugin.TableWriteOptions{
		{},
		{Fields: []datatype.FieldInfo{{Name: "  ", Type: datatype.FieldTypeBigInt}}},
		{Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}, {Name: " ID ", Type: datatype.FieldTypeBigInt}}},
		{Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}}, SpatialInfo: datatype.NewSingleGeometrySpatialInfo("missing", "Point", 4326, 2)},
		{Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}}, SpatialInfo: &datatype.SpatialInfo{HasSpatialIndex: &index}},
	} {
		definition := plugin.TableWriteDefinitionFromOptions(opts)
		report := p.CheckTableWriteDefinition(definition)
		if report.UnsupportedError() == nil || !reflect.DeepEqual(report, p.CheckTableWriteDefinition(definition)) {
			t.Fatalf("invalid or unstable declaration: %+v", report)
		}
		if err := p.PrepareTableWrite(t.Context(), nil, plugin.EngineCatalogPath{}, opts); err == nil || !strings.Contains(err.Error(), "target_definition") {
			t.Fatalf("invalid declaration reached connection: %v", err)
		}
	}
}

func TestDorisWriteDefinition(t *testing.T) {
	checker, ok := interface{}(&DorisPlugin{}).(plugin.TableWriteDefinitionChecker)
	if !ok {
		t.Fatal("Doris pure checker unavailable")
	}
	constant := func(v interface{}) *plugin.TableWriteConstant { return &plugin.TableWriteConstant{Value: v} }
	observed := func(native string, precision, scale int) *datatype.TableInfo {
		return &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "value", NativeType: native, Precision: precision, Scale: scale}}}
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
		{"JSON object", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, constant(map[string]interface{}{"a": []interface{}{true, json.Number("9007199254740993")}}), nil, plugin.DefinitionSupported, "constant_encoding_supported"},
		{"encoded JSON", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, constant(json.RawMessage(`{"a":1}`)), nil, plugin.DefinitionSupported, "constant_encoding_supported"},
		{"invalid JSON", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, constant("{"), nil, plugin.DefinitionUnsupported, "constant_encoding_invalid"},
		{"JSON null", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, constant([]interface{}(nil)), nil, plugin.DefinitionSupported, "constant_encoding_supported"},
		{"SQL NULL", datatype.FieldInfo{Type: datatype.FieldTypeJSON, Nullable: true}, nil, constant(nil), nil, plugin.DefinitionSupported, "constant_encoding_supported"},
		{"nonnullable NULL", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, constant([]byte(nil)), nil, plugin.DefinitionUnsupported, "null_constant_not_nullable"},
		{"bytes", datatype.FieldInfo{Type: datatype.FieldTypeBytes}, nil, constant([]byte{0, 255}), nil, plugin.DefinitionSupported, "constant_encoding_supported"},
		{"bytes text", datatype.FieldInfo{Type: datatype.FieldTypeBytes}, nil, constant("text"), nil, plugin.DefinitionUnsupported, "constant_encoding_invalid"},
		{"text container", datatype.FieldInfo{Type: datatype.FieldTypeString}, nil, constant([]int{1}), nil, plugin.DefinitionUnsupported, "constant_encoding_invalid"},
		{"array", datatype.FieldInfo{Type: datatype.FieldTypeArray}, nil, nil, nil, plugin.DefinitionUnsupported, "field_definition_unsupported"},
		{"geometry", datatype.FieldInfo{Type: datatype.FieldTypeGeometry}, nil, nil, nil, plugin.DefinitionUnsupported, "field_definition_unsupported"},
		{"decimal invalid", datatype.FieldInfo{Type: datatype.FieldTypeDecimal, Precision: 39, Scale: 1}, nil, nil, nil, plugin.DefinitionUnsupported, "decimal_definition_invalid"},
		{"source text JSON", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, &datatype.FieldInfo{Type: datatype.FieldTypeString}, nil, nil, plugin.DefinitionUnknown, "json_representation_not_proven"},
		{"source array JSON", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, &datatype.FieldInfo{Type: datatype.FieldTypeArray}, nil, nil, plugin.DefinitionUnknown, "json_representation_not_proven"},
		{"source scalar JSON", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, &datatype.FieldInfo{Type: datatype.FieldTypeBool}, nil, nil, plugin.DefinitionSupported, "json_encoding_supported"},
		{"source unknown", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, &datatype.FieldInfo{Type: datatype.FieldTypeMixed}, nil, nil, plugin.DefinitionUnknown, "source_type_unknown"},
		{"existing STRING JSON", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, nil, observed("string", 0, 0), plugin.DefinitionSupported, "existing_column_compatible"},
		{"existing incompatible", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, nil, observed("int", 0, 0), plugin.DefinitionUnsupported, "existing_column_incompatible"},
		{"existing missing native", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, nil, observed("", 0, 0), plugin.DefinitionUnknown, "existing_native_type_missing"},
		{"existing decimal missing facts", datatype.FieldInfo{Type: datatype.FieldTypeDecimal, Precision: 18, Scale: 2}, nil, nil, observed("decimal(18,2)", 0, 0), plugin.DefinitionUnknown, "existing_numeric_facts_missing"},
		{"existing decimal compatible", datatype.FieldInfo{Type: datatype.FieldTypeDecimal, Precision: 18, Scale: 2}, nil, nil, observed("decimal(18,2)", 18, 2), plugin.DefinitionSupported, "existing_column_compatible"},
		{"existing decimal incompatible", datatype.FieldInfo{Type: datatype.FieldTypeDecimal, Precision: 18, Scale: 2}, nil, nil, observed("decimal(20,2)", 20, 2), plugin.DefinitionUnsupported, "existing_column_incompatible"},
		{"nullable addition", datatype.FieldInfo{Type: datatype.FieldTypeJSON, Nullable: true}, nil, nil, &datatype.TableInfo{}, plugin.DefinitionSupported, "nullable_column_addition_supported"},
		{"nonnullable addition", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, nil, &datatype.TableInfo{}, plugin.DefinitionUnsupported, "column_addition_unsupported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.field.Name = "value"
			definition := plugin.TableWriteDefinition{Fields: []plugin.TableWriteFieldDefinition{{Target: datatype.FieldInfo{Name: "id", Type: datatype.FieldTypeBigInt, Nullable: true}}, {Target: tc.field, Source: tc.source, Constant: tc.value}}, Existing: tc.existing}
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
}

type definitionJSONHook struct{ calls *int }

func (h definitionJSONHook) MarshalJSON() ([]byte, error) { *h.calls++; return []byte(`{}`), nil }

type definitionDriverHook string

func (definitionDriverHook) Value() (driver.Value, error) { panic("pure check called Valuer") }

func TestDorisWriteDefinitionNeverCallsHooks(t *testing.T) {
	checker, ok := interface{}(&DorisPlugin{}).(plugin.TableWriteDefinitionChecker)
	if !ok {
		t.Fatal("Doris pure checker unavailable")
	}
	calls := 0
	for _, value := range []interface{}{map[string]interface{}{"nested": []interface{}{definitionJSONHook{&calls}}}, definitionDriverHook("value")} {
		report := checker.CheckTableWriteDefinition(plugin.TableWriteDefinition{Fields: []plugin.TableWriteFieldDefinition{
			{Target: datatype.FieldInfo{Name: "id", Type: datatype.FieldTypeBigInt}},
			{Target: datatype.FieldInfo{Name: "payload", Type: datatype.FieldTypeJSON}, Constant: &plugin.TableWriteConstant{Value: value}, Source: &datatype.FieldInfo{Type: datatype.FieldTypeJSON}},
			{Target: datatype.FieldInfo{Name: "other", Type: datatype.FieldTypeArray}},
		}, Existing: &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "payload", NativeType: "int"}}}})
		found := map[string]bool{}
		for _, c := range report.Checks {
			if c.Field == "payload" {
				found[c.Code] = true
			}
		}
		if calls != 0 || !found["constant_encoding_not_checked"] || !found["source_assignment_supported"] || !found["existing_column_incompatible"] || report.UnsupportedError() == nil {
			t.Fatalf("hook or suppressed constraints: %+v calls=%d", report, calls)
		}
	}
}
