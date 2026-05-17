package postgresql

import (
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestPostgresFieldInfoFromColumnPreservesSpatialNativeType(t *testing.T) {
	field := postgresFieldInfoFromColumn(postgresColumnInfo{
		Name:       "SmGeometry",
		DataType:   "USER-DEFINED",
		UDTName:    "geometry",
		NativeType: "geometry(MultiPolygon,4326)",
	})

	if field.Name != "SmGeometry" {
		t.Fatalf("field name = %q, want SmGeometry", field.Name)
	}
	if field.Type != "geometry" {
		t.Fatalf("field type = %q, want geometry", field.Type)
	}
	if field.NativeType != "geometry(MultiPolygon,4326)" {
		t.Fatalf("native type = %q, want geometry(MultiPolygon,4326)", field.NativeType)
	}
}

func TestPostgresReadBatchFieldsKeepsSchemaMetadataInColumnOrder(t *testing.T) {
	fields := postgresReadBatchFields([]string{"id", "SmGeometry"}, []plugin.FieldInfo{
		{Name: "SmGeometry", Type: "geometry", NativeType: "geometry(MultiPolygon,4326)"},
		{Name: "id", Type: "bigint", NativeType: "bigint"},
	})

	if len(fields) != 2 {
		t.Fatalf("fields length = %d, want 2", len(fields))
	}
	if fields[0].Name != "id" || fields[0].Type != "bigint" || fields[0].NativeType != "bigint" {
		t.Fatalf("first field = %#v, want id bigint", fields[0])
	}
	if fields[1].Name != "SmGeometry" || fields[1].Type != "geometry" || fields[1].NativeType != "geometry(MultiPolygon,4326)" {
		t.Fatalf("second field = %#v, want spatial field metadata", fields[1])
	}
}
