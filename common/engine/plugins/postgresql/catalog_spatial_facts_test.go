package postgresql

import (
	"strings"
	"testing"
)

func TestPostgresSpatialExtentQueryUsesNativeGeometry(t *testing.T) {
	t.Parallel()

	query := postgresSpatialExtentQuery("public", "land_use", "SmGeometry")

	if strings.Contains(query, "ST_Transform") {
		t.Fatalf("extent query must not transform source geometry:\n%s", query)
	}
	if !strings.Contains(query, `ST_Extent("SmGeometry")`) {
		t.Fatalf("extent query should use native geometry column:\n%s", query)
	}
	if !strings.Contains(query, `FROM "public"."land_use"`) {
		t.Fatalf("extent query should quote qualified table:\n%s", query)
	}
}
