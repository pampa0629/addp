package preview

import (
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/sqldialect"
)

func TestBuildDatabaseRenderGeometryColumns(t *testing.T) {
	t.Parallel()

	rows := []map[string]interface{}{
		{
			"geom":                  "POINT(1 2)",
			"__render_geojson_geom": `{"type":"Point","coordinates":[1,2]}`,
		},
		{
			"geom":                  nil,
			"__render_geojson_geom": nil,
		},
	}

	got := buildDatabaseRenderGeometryColumns([]string{"geom"}, rows)
	if len(got) != 1 {
		t.Fatalf("expected 1 render geometry mapping, got %d", len(got))
	}
	if got["geom"] != "__render_geojson_geom" {
		t.Fatalf("unexpected render geometry column mapping: %+v", got)
	}
}

func TestBuildDatabaseRenderGeometryColumnsIgnoreInvalidPayload(t *testing.T) {
	t.Parallel()

	rows := []map[string]interface{}{
		{
			"geom":                  "POINT(1 2)",
			"__render_geojson_geom": "not-json",
		},
	}

	got := buildDatabaseRenderGeometryColumns([]string{"geom"}, rows)
	if len(got) != 0 {
		t.Fatalf("expected invalid render payload to be ignored, got %+v", got)
	}
}

func TestDatabasePreviewPostgreSQLPrimaryKeyPageQueryUsesKeyCTEForDeepOffset(t *testing.T) {
	t.Parallel()

	dialect := sqldialect.ForEngine("postgresql")
	columns := []plugin.ColumnInfo{
		{ColumnName: "SmID", DataType: "bigint", IsPrimaryKey: true},
		{ColumnName: "SmGeometry", DataType: "geometry(MultiPolygon,2360)"},
		{ColumnName: "DLMC", DataType: "text"},
	}
	selectExpr := databasePreviewSelectExpr(dialect, columns, databasePreviewSourceAlias)
	query := databasePreviewPostgreSQLPrimaryKeyPageQuery(dialect, selectExpr, "public", "dltb", databasePrimaryKeyColumns(columns), 20, 10000000)

	mustContain := []string{
		`WITH "__addp_page_keys" AS (SELECT "SmID" FROM "public"."dltb" ORDER BY "SmID" LIMIT 20 OFFSET 10000000)`,
		`FROM "public"."dltb" AS "__addp_src" JOIN "__addp_page_keys" AS "__addp_keys" ON "__addp_src"."SmID" = "__addp_keys"."SmID"`,
		`ST_AsText("__addp_src"."SmGeometry") AS "SmGeometry"`,
		`AS "__render_geojson_SmGeometry"`,
		`ORDER BY "__addp_src"."SmID"`,
	}
	for _, want := range mustContain {
		if !strings.Contains(query, want) {
			t.Fatalf("query does not contain %q:\n%s", want, query)
		}
	}
}

func TestDatabasePreviewPostgreSQLPrimaryKeyPageQueryOrdersFirstPage(t *testing.T) {
	t.Parallel()

	dialect := sqldialect.ForEngine("postgresql")
	columns := []plugin.ColumnInfo{
		{ColumnName: "id", DataType: "bigint", IsPrimaryKey: true},
		{ColumnName: "name", DataType: "text"},
	}
	selectExpr := databasePreviewSelectExpr(dialect, columns, databasePreviewSourceAlias)
	query := databasePreviewPostgreSQLPrimaryKeyPageQuery(dialect, selectExpr, "public", "cities", databasePrimaryKeyColumns(columns), 50, 0)

	want := `SELECT "__addp_src"."id" AS "id", "__addp_src"."name" AS "name" FROM "public"."cities" AS "__addp_src" ORDER BY "__addp_src"."id" LIMIT 50`
	if query != want {
		t.Fatalf("unexpected query:\n%s\nwant:\n%s", query, want)
	}
}
