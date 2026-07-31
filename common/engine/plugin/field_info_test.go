package plugin

import (
	"testing"

	"github.com/addp/common/datatype"
)

func TestNormalizeFieldInfosValidatesCanonicalTypeWithoutParsingNativeType(t *testing.T) {
	fields := NormalizeFieldInfos([]datatype.FieldInfo{
		{Name: " id ", Type: datatype.FieldTypeInt, Nullable: false},
		{Name: "", NativeType: "text"},
		{Name: "name", NativeType: "string", Nullable: true},
		{Name: "amount", Type: "numeric", NativeType: "numeric(18,2)"},
	})

	if len(fields) != 3 {
		t.Fatalf("fields = %#v, want three fields", fields)
	}
	if fields[0].Name != "id" || fields[0].NativeType != "int" || fields[0].Type != datatype.FieldTypeInt {
		t.Fatalf("first field = %#v", fields[0])
	}
	if fields[1].Name != "name" || fields[1].NativeType != "string" || fields[1].Type != datatype.FieldTypeUnknown {
		t.Fatalf("second field = %#v", fields[1])
	}
	if fields[2].NativeType != "numeric(18,2)" || fields[2].Type != datatype.FieldTypeUnknown {
		t.Fatalf("third field = %#v", fields[2])
	}
}
