package mysql

import (
	"testing"
	"time"

	"github.com/addp/common/datatype"
)

func TestMySQLCapabilitiesDeclareBoundedWatermarkRead(t *testing.T) {
	capabilities := (&MySQLPlugin{}).Capabilities()
	if capabilities.Storage == nil || capabilities.Storage.Store == nil || !capabilities.Storage.Store.BoundedWatermarkRead {
		t.Fatalf("MySQL bounded watermark read capability = %#v, want true", capabilities.Storage)
	}
}

func TestStringifyMySQLCursorUsesCanonicalTemporalValues(t *testing.T) {
	stamp := time.Date(2026, 7, 12, 8, 9, 10, 123456000, time.UTC)
	values, err := stringifyMySQLCursor([]interface{}{stamp, stamp, []byte("42")}, []mysqlColumnInfo{
		{Name: "event_date", NativeType: "date"},
		{Name: "updated_at", NativeType: "datetime(6)"},
		{Name: "id", NativeType: "bigint"},
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

func TestResolveMySQLWatermarkCursorRequiresNonNullSupportedColumns(t *testing.T) {
	columns := map[string]mysqlColumnInfo{
		"updated_at": {Name: "updated_at", NativeType: "datetime(6)"},
		"id":         {Name: "id", NativeType: "bigint"},
		"payload":    {Name: "payload", NativeType: "json"},
		"optional":   {Name: "optional", NativeType: "varchar(20)", Nullable: true},
	}
	fields, _, err := resolveMySQLWatermarkCursorFields([]string{"UPDATED_AT", "id"}, columns)
	if err != nil {
		t.Fatal(err)
	}
	if fields[0] != "updated_at" || fields[1] != "id" {
		t.Fatalf("resolved fields = %#v", fields)
	}
	for _, cursor := range [][]string{{"updated_at", "payload"}, {"updated_at", "optional"}, {"updated_at", "missing"}} {
		if _, _, err := resolveMySQLWatermarkCursorFields(cursor, columns); err == nil {
			t.Fatalf("cursor %#v was accepted", cursor)
		}
	}
}

func TestMySQLWatermarkCursorTypeSupport(t *testing.T) {
	for _, fieldType := range []datatype.FieldType{
		datatype.FieldTypeBigInt, datatype.FieldTypeDecimal, datatype.FieldTypeString, datatype.FieldTypeTimestamp,
	} {
		if !mysqlWatermarkCursorTypeSupported(fieldType) {
			t.Fatalf("field type %q should be supported", fieldType)
		}
	}
	for _, fieldType := range []datatype.FieldType{datatype.FieldTypeJSON, datatype.FieldTypeBytes, datatype.FieldTypeGeometry} {
		if mysqlWatermarkCursorTypeSupported(fieldType) {
			t.Fatalf("field type %q should not be supported", fieldType)
		}
	}
}

func TestMySQLWatermarkFieldsRejectSpatialColumns(t *testing.T) {
	_, _, err := mysqlWatermarkFields([]mysqlColumnInfo{
		{Name: "id", NativeType: "bigint"},
		{Name: "shape", NativeType: "geometry"},
	})
	if err == nil {
		t.Fatal("mysqlWatermarkFields accepted a spatial source column")
	}
}
