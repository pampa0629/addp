package mysql

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/shared/writetest"
)

func TestMySQLWriteDefinitionNonSpatial(t *testing.T) {
	checker, ok := interface{}(&MySQLPlugin{}).(plugin.TableWriteDefinitionChecker)
	if !ok {
		t.Fatal("MySQL pure checker unavailable")
	}
	// The same hook isolation must hold at the public spatial-capable plugin.
	writetest.PureDefinitionConstants(t, checker)
}

func TestMySQLMixedDefinitionRejectedBeforeConnection(t *testing.T) {
	p := &MySQLPlugin{}
	spatial := datatype.NewSingleGeometrySpatialInfo("shape", "Point", 4326, 2)
	for _, field := range []datatype.FieldInfo{
		{Name: "payload", Type: datatype.FieldTypeArray},
		{Name: "amount", Type: datatype.FieldTypeDecimal, Precision: 2, Scale: 3},
	} {
		t.Run(field.Name, func(t *testing.T) {
			fields := []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}, {Name: "shape", Type: datatype.FieldTypeGeometry}, field}
			opts := plugin.TableUpsertOptions{Fields: fields, SpatialInfo: spatial, Keys: []string{"id"}}
			ctx, path := context.Background(), plugin.EngineCatalogPath{}
			errors := []error{p.PrepareTableWrite(ctx, nil, path, plugin.TableWriteOptions{Fields: fields, SpatialInfo: spatial})}
			_, err := p.OpenTableWriteSession(ctx, nil, path, plugin.TableWriteSessionOptions{Fields: fields, SpatialInfo: spatial})
			errors = append(errors, err, p.PrepareTableUpsert(ctx, nil, path, opts), p.UpsertBatch(ctx, nil, path, &plugin.BatchData{Rows: []map[string]interface{}{{"id": 1}}}, opts))
			for _, err := range errors {
				if err == nil || !strings.Contains(err.Error(), `column "`+field.Name+`"`) {
					t.Fatalf("invalid definition reached path/connection: %v", err)
				}
			}
		})
	}
}

func TestMySQLSpatialNativeTypesAreNotInterchangeable(t *testing.T) {
	field := datatype.FieldInfo{Name: "shape", Type: datatype.FieldTypeGeometry}
	spatial := datatype.NewSingleGeometrySpatialInfo("shape", "Point", 0, 2)
	if mysqlSpatialColumnCompatibleWithField(mysqlColumnInfo{Name: "shape", NativeType: "polygon", DataType: "polygon"}, field, spatial) {
		t.Fatal("POLYGON target accepted a POINT definition")
	}
}

func TestMySQLSpatialWriteDefinition(t *testing.T) {
	p := &MySQLPlugin{}
	for _, tc := range []struct {
		name           string
		field          datatype.FieldInfo
		spatial        *datatype.SpatialInfo
		constant       *plugin.TableWriteConstant
		existing       *datatype.TableInfo
		check          string
		status         plugin.DefinitionCheckStatus
		code, physical string
	}{
		{"POINT definition", datatype.FieldInfo{Type: datatype.FieldTypeGeometry}, datatype.NewSingleGeometrySpatialInfo("value", "Point", 4326, 2), nil, nil, "target_definition", plugin.DefinitionSupported, "field_definition_supported", "POINT SRID 4326"},
		{"three dimensions", datatype.FieldInfo{Type: datatype.FieldTypeGeometry}, datatype.NewSingleGeometrySpatialInfo("value", "Point", 4326, 3), nil, nil, "target_definition", plugin.DefinitionUnsupported, "field_definition_unsupported", ""},
		{"JSON object", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, &plugin.TableWriteConstant{Value: map[string]interface{}{"items": []interface{}{true, json.Number("9007199254740993")}}}, nil, "constant_encoding", plugin.DefinitionSupported, "constant_encoding_supported", "JSON"},
		{"invalid JSON", datatype.FieldInfo{Type: datatype.FieldTypeJSON}, nil, &plugin.TableWriteConstant{Value: "{"}, nil, "constant_encoding", plugin.DefinitionUnsupported, "constant_encoding_invalid", "JSON"},
		{"native array", datatype.FieldInfo{Type: datatype.FieldTypeArray}, nil, nil, nil, "target_definition", plugin.DefinitionUnsupported, "field_definition_unsupported", ""},
		{"invalid EWKB", datatype.FieldInfo{Type: datatype.FieldTypeGeometry}, nil, &plugin.TableWriteConstant{Value: []byte{1, 2}}, nil, "constant_encoding", plugin.DefinitionUnsupported, "constant_encoding_invalid", "GEOMETRY"},
		{"geometry text is not EWKB", datatype.FieldInfo{Type: datatype.FieldTypeGeometry}, nil, &plugin.TableWriteConstant{Value: "POINT(1 2)"}, nil, "constant_encoding", plugin.DefinitionUnsupported, "constant_encoding_invalid", "GEOMETRY"},
		{"geometry nullable NULL", datatype.FieldInfo{Type: datatype.FieldTypeGeometry, Nullable: true}, nil, &plugin.TableWriteConstant{Value: nil}, nil, "constant_encoding", plugin.DefinitionSupported, "constant_encoding_supported", "GEOMETRY"},
		{"geometry nonnullable NULL", datatype.FieldInfo{Type: datatype.FieldTypeGeometry}, nil, &plugin.TableWriteConstant{Value: nil}, nil, "constant_encoding", plugin.DefinitionUnsupported, "null_constant_not_nullable", "GEOMETRY"},
		{"existing wrong subtype", datatype.FieldInfo{Type: datatype.FieldTypeGeometry}, datatype.NewSingleGeometrySpatialInfo("value", "Point", 4326, 2), nil, &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "value", NativeType: "polygon"}}}, "existing_target", plugin.DefinitionUnsupported, "existing_column_incompatible", "POINT SRID 4326"},
		{"existing SRID unavailable", datatype.FieldInfo{Type: datatype.FieldTypeGeometry}, datatype.NewSingleGeometrySpatialInfo("value", "Point", 4326, 2), nil, &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "value", NativeType: "point"}}}, "existing_target", plugin.DefinitionUnknown, "existing_column_type_not_proven", "POINT SRID 4326"},
		{"existing native missing", datatype.FieldInfo{Type: datatype.FieldTypeGeometry}, nil, nil, &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "value", Type: datatype.FieldTypeGeometry}}}, "existing_target", plugin.DefinitionUnknown, "existing_native_type_missing", "GEOMETRY"},
		{"nullable addition", datatype.FieldInfo{Type: datatype.FieldTypeGeometry, Nullable: true}, nil, nil, &datatype.TableInfo{}, "existing_target", plugin.DefinitionSupported, "nullable_column_addition_supported", "GEOMETRY"},
		{"nonnullable addition", datatype.FieldInfo{Type: datatype.FieldTypeGeometry}, nil, nil, &datatype.TableInfo{}, "existing_target", plugin.DefinitionUnsupported, "column_addition_unsupported", "GEOMETRY"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.field.Name = "value"
			definition := plugin.TableWriteDefinition{Fields: []plugin.TableWriteFieldDefinition{{Target: tc.field, Constant: tc.constant}}, SpatialInfo: tc.spatial, Existing: tc.existing}
			before, _ := json.Marshal(definition)
			report := p.CheckTableWriteDefinition(definition)
			last := report.Checks[len(report.Checks)-1]
			if last.Check != tc.check || last.Status != tc.status || last.Code != tc.code || last.PhysicalType != tc.physical {
				t.Fatalf("check=%+v expected %+v", last, tc)
			}
			after, _ := json.Marshal(definition)
			if string(before) != string(after) {
				t.Fatal("saved facts mutated")
			}
		})
	}
	index := true
	for _, spatial := range []*datatype.SpatialInfo{
		datatype.NewSingleGeometrySpatialInfo("missing", "Point", 4326, 2),
		datatype.NewSingleGeometrySpatialInfo("label", "Point", 4326, 2),
		{GeometryColumns: []datatype.GeometryColumnInfo{{}}},
		{HasSpatialIndex: &index},
		{HasSpatialIndex: &index, GeometryColumns: []datatype.GeometryColumnInfo{{Name: "shape"}}},
		{GeometryColumns: []datatype.GeometryColumnInfo{{Name: "shape"}, {Name: "SHAPE"}}},
	} {
		definition := plugin.TableWriteDefinition{Fields: []plugin.TableWriteFieldDefinition{{Target: datatype.FieldInfo{Name: "shape", Type: datatype.FieldTypeGeometry, Nullable: true}}, {Target: datatype.FieldInfo{Name: "label", Type: datatype.FieldTypeString}}}, SpatialInfo: spatial}
		first := p.CheckTableWriteDefinition(definition)
		if first.UnsupportedError() == nil {
			t.Fatalf("invalid spatial declaration accepted: %+v", spatial)
		}
		for i := 0; i < 5; i++ {
			if !reflect.DeepEqual(first, p.CheckTableWriteDefinition(definition)) {
				t.Fatal("unstable report")
			}
		}
	}
}
