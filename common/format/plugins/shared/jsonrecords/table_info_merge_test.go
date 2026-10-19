package jsonrecords

import (
	"reflect"
	"testing"

	"github.com/addp/common/datatype"
)

func TestMergeTableInfoPreservesInputsAndObservedNulls(t *testing.T) {
	current := &datatype.TableInfo{Fields: []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeInt},
		{Name: "shape", Type: datatype.FieldTypeUnknown, Nullable: true, Path: []string{"shape"}},
	}}
	next := &datatype.TableInfo{Fields: []datatype.FieldInfo{
		{Name: "shape", Type: datatype.FieldTypeGeometry},
		{Name: "label", Type: datatype.FieldTypeString, Path: []string{"label"}},
	}}
	beforeCurrent, beforeNext := current.Clone(), next.Clone()
	merged := MergeTableInfo(current, next)
	if got := merged.FieldNames(); !reflect.DeepEqual(got, []string{"id", "shape", "label"}) {
		t.Fatalf("field order = %v", got)
	}
	for _, field := range merged.Fields {
		if !field.Nullable {
			t.Errorf("field %q has missing/null observations but is non-nullable", field.Name)
		}
	}
	if merged.GetField("shape").Type != datatype.FieldTypeGeometry {
		t.Fatal("null-only shape did not acquire its observed geometry type")
	}
	merged.GetField("shape").Path[0] = "changed"
	merged.GetField("label").Path[0] = "changed"
	if !reflect.DeepEqual(current, beforeCurrent) || !reflect.DeepEqual(next, beforeNext) {
		t.Fatal("schema merge mutated an input")
	}
}
