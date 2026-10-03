package execution

import (
	"testing"

	"github.com/addp/common/datatype"
)

func TestLineageSchemaSnapshotResolvesUniqueNestedFieldNames(t *testing.T) {
	fields := []datatype.FieldInfo{{Name: "userInfo.nickName", Path: []string{"userInfo", "nickName"}, Type: datatype.FieldTypeString}}
	snapshot, err := NewLineageSchemaSnapshot(fields)
	if err != nil || !snapshot.HasField("userInfo.nickName") || snapshot.HasField("userInfo") {
		t.Fatalf("nested field identity: %+v, %v", snapshot, err)
	}
	fields = append(fields, datatype.FieldInfo{Name: "userInfo.nickName", Path: []string{"userInfo.nickName"}, Type: datatype.FieldTypeString})
	snapshot, err = NewLineageSchemaSnapshot(fields)
	if err != nil || snapshot.HasField("userInfo.nickName") {
		t.Fatalf("ambiguous name accepted: %+v, %v", snapshot, err)
	}
}

func TestLineageSchemaSnapshotValidatesStructureAndExactNames(t *testing.T) {
	fields := []datatype.FieldInfo{{Name: "a.b", Type: datatype.FieldTypeString, Nullable: true}}
	snapshot, err := NewLineageSchemaSnapshot(fields)
	if err != nil {
		t.Fatal(err)
	}
	fields[0].Name = "changed"
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
	if !snapshot.HasField("a.b") || snapshot.HasField("a") {
		t.Fatal("field name was interpreted as a nested path")
	}
	snapshot.Fields[0].Nullable = false
	if err := snapshot.Validate(); err == nil {
		t.Fatal("altered schema accepted with old hash")
	}
}
