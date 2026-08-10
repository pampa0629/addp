package oracle

import (
	"database/sql"
	"reflect"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func TestOracleSelectedFieldsAllowsExcludingUnsupportedColumns(t *testing.T) {
	fields := []datatype.FieldInfo{
		{Name: "ID", Type: datatype.FieldTypeBigInt},
		{Name: "SHAPE", Type: datatype.FieldTypeUnknown, NativeType: "MDSYS.SDO_GEOMETRY"},
	}
	selected, err := oracleSelectedFields(fields, map[string]interface{}{
		format.FieldSelectionOptionKey: format.FieldSelectionOptions{Include: []string{"ID"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 1 || selected[0].Name != "ID" {
		t.Fatalf("selected fields = %#v", selected)
	}
}

func TestOracleScanValuesPreservesDecimalTextAndBytes(t *testing.T) {
	fields := []datatype.FieldInfo{
		{Name: "ID", Type: datatype.FieldTypeBigInt},
		{Name: "AMOUNT", Type: datatype.FieldTypeDecimal},
		{Name: "PAYLOAD", Type: datatype.FieldTypeBytes},
	}
	values, destinations := oracleScanValues(fields, []string{"ID", "AMOUNT", "PAYLOAD"})

	*destinations[0].(*sql.NullInt64) = sql.NullInt64{Int64: 42, Valid: true}
	*destinations[1].(*sql.RawBytes) = sql.RawBytes("12345678901234567890.25")
	*destinations[2].(*[]byte) = []byte{1, 2, 3}

	got := []interface{}{values[0](), values[1](), values[2]()}
	want := []interface{}{int64(42), "12345678901234567890.25", []byte{1, 2, 3}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scan values = %#v, want %#v", got, want)
	}
}
