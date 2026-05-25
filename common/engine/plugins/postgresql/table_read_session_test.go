package postgresql

import (
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func TestPostgresFieldInfoFromColumnKeepsSpatialNativeType(t *testing.T) {
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
	spatialInfo := postgresSpatialInfoFromFields([]datatype.FieldInfo{field})
	if spatialInfo.PrimaryGeometryType() != "MultiPolygon" || spatialInfo.PrimarySRIDValue() != 4326 {
		t.Fatalf("spatial info = %#v, want standard spatial facts", spatialInfo)
	}
}

func TestPostgresReadBatchFieldsKeepsTableFieldMetadataInColumnOrder(t *testing.T) {
	fields := postgresReadBatchFields([]string{"id", "SmGeometry"}, []datatype.FieldInfo{
		{Name: "SmGeometry", Type: "geometry", NativeType: "geometry(MultiPolygon,4326)"},
		{Name: "id", Type: "bigint"},
	})

	if len(fields) != 2 {
		t.Fatalf("fields length = %d, want 2", len(fields))
	}
	if fields[0].Name != "id" || fields[0].Type != "bigint" {
		t.Fatalf("first field = %#v, want id bigint", fields[0])
	}
	if fields[1].Name != "SmGeometry" || fields[1].Type != "geometry" || fields[1].NativeType != "geometry(MultiPolygon,4326)" {
		t.Fatalf("second field = %#v, want spatial field metadata", fields[1])
	}
}

func TestPostgresSelectedFieldsFollowsFieldSelectionOrder(t *testing.T) {
	fields := []datatype.FieldInfo{
		{Name: "id", Type: "bigint"},
		{Name: "name", Type: "string"},
		{Name: "geom", Type: "geometry"},
	}

	selected, err := postgresSelectedFields(fields, map[string]interface{}{
		format.FieldSelectionOptionKey: &format.FieldSelectionOptions{
			Include: []string{"name", "id", "name"},
		},
	})
	if err != nil {
		t.Fatalf("postgresSelectedFields failed: %v", err)
	}
	if len(selected) != 2 {
		t.Fatalf("selected fields = %#v, want 2 fields", selected)
	}
	if selected[0].Name != "name" || selected[1].Name != "id" {
		t.Fatalf("selected fields = %#v, want name,id", selected)
	}
}

func TestPostgresSelectedFieldsErrorsOnMissingFieldByDefault(t *testing.T) {
	_, err := postgresSelectedFields([]datatype.FieldInfo{{Name: "id"}}, map[string]interface{}{
		format.FieldSelectionOptionKey: &format.FieldSelectionOptions{
			Include: []string{"id", "missing"},
		},
	})
	if err == nil {
		t.Fatal("postgresSelectedFields succeeded, want missing field error")
	}
}

func TestPostgresSelectedFieldsIgnoresMissingFieldWhenConfigured(t *testing.T) {
	selected, err := postgresSelectedFields([]datatype.FieldInfo{{Name: "id"}}, map[string]interface{}{
		format.FieldSelectionOptionKey: format.FieldSelectionOptions{
			Include:            []string{"missing", "id"},
			MissingFieldPolicy: format.MissingFieldIgnore,
		},
	})
	if err != nil {
		t.Fatalf("postgresSelectedFields failed: %v", err)
	}
	if len(selected) != 1 || selected[0].Name != "id" {
		t.Fatalf("selected fields = %#v, want id", selected)
	}
}

func TestPostgresSelectExprForFieldsQuotesSelectedColumns(t *testing.T) {
	expr := postgresSelectExprForFields([]datatype.FieldInfo{
		{Name: "id"},
		{Name: "Road Name"},
	})
	if expr != `"id", "Road Name"` {
		t.Fatalf("select expr = %q, want quoted selected fields", expr)
	}
}
