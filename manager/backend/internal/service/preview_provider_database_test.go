package service

import "testing"

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
