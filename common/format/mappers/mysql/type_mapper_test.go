package mysql

import (
	"testing"

	"github.com/addp/common/datatype"
)

func TestNativeUnsignedResultTypes(t *testing.T) {
	for native, want := range map[string]datatype.FieldType{
		"UNSIGNED BIGINT":     datatype.FieldTypeBigInt,
		"bigint unsigned":     datatype.FieldTypeBigInt,
		"BIGINT(20) UNSIGNED": datatype.FieldTypeBigInt,
		"UNSIGNED INT":        datatype.FieldTypeInt,
		"DECIMAL(38,18)":      datatype.FieldTypeDecimal,
	} {
		if got := (&TypeMapper{}).ToCommon(native); got != want {
			t.Fatalf("%s: got %s, want %s", native, got, want)
		}
	}
}
