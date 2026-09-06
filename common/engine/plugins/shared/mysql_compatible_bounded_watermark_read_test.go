package shared

import (
	"testing"
	"time"

	"github.com/addp/common/datatype"
)

func TestStringifyMySQLCompatibleCursorUsesCanonicalTemporalValues(t *testing.T) {
	stamp := time.Date(2026, 7, 12, 8, 9, 10, 123456000, time.UTC)
	values, err := stringifyMySQLCompatibleCursor("oceanbase", []interface{}{stamp, stamp, []byte("42")}, []MySQLCompatibleWatermarkColumn{
		{Name: "event_date", NativeType: "date", Type: datatype.FieldTypeDate},
		{Name: "updated_at", NativeType: "datetime(6)", Type: datatype.FieldTypeTimestamp},
		{Name: "id", NativeType: "bigint", Type: datatype.FieldTypeBigInt},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"2026-07-12", "2026-07-12 08:09:10.123456", "42"}
	for index := range want {
		if values[index] != want[index] {
			t.Fatalf("values = %#v, want %#v", values, want)
		}
	}
}

func TestResolveMySQLCompatibleWatermarkCursorRequiresNonNullSupportedColumns(t *testing.T) {
	columns := []MySQLCompatibleWatermarkColumn{
		{Name: "updated_at", NativeType: "datetime(6)", Type: datatype.FieldTypeTimestamp},
		{Name: "id", NativeType: "bigint", Type: datatype.FieldTypeBigInt},
		{Name: "payload", NativeType: "json", Type: datatype.FieldTypeJSON},
		{Name: "optional", NativeType: "varchar(20)", Type: datatype.FieldTypeString, Nullable: true},
		{Name: "shape", NativeType: "point", Type: datatype.FieldTypeGeometry},
	}
	fields, _, err := resolveMySQLCompatibleWatermarkCursorFields("oceanbase", []string{"UPDATED_AT", "id"}, columns)
	if err != nil {
		t.Fatal(err)
	}
	if fields[0] != "updated_at" || fields[1] != "id" {
		t.Fatalf("resolved fields = %#v", fields)
	}
	for _, cursor := range [][]string{{"updated_at", "payload"}, {"updated_at", "optional"}, {"updated_at", "shape"}, {"updated_at", "missing"}} {
		if _, _, err := resolveMySQLCompatibleWatermarkCursorFields("oceanbase", cursor, columns); err == nil {
			t.Fatalf("cursor %#v was accepted", cursor)
		}
	}
}

func TestMySQLCompatibleWatermarkCursorTypeSupport(t *testing.T) {
	for _, fieldType := range []datatype.FieldType{datatype.FieldTypeBigInt, datatype.FieldTypeDecimal, datatype.FieldTypeString, datatype.FieldTypeTimestamp} {
		if !mySQLCompatibleWatermarkCursorTypeSupported(fieldType) {
			t.Fatalf("field type %q should be supported", fieldType)
		}
	}
	for _, fieldType := range []datatype.FieldType{datatype.FieldTypeJSON, datatype.FieldTypeBytes, datatype.FieldTypeGeometry} {
		if mySQLCompatibleWatermarkCursorTypeSupported(fieldType) {
			t.Fatalf("field type %q should not be supported", fieldType)
		}
	}
}
