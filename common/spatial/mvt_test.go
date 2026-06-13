package spatial

import (
	"strings"
	"testing"
)

func TestBuildMVTQueryTransformsNonWebMercatorSourceTo3857(t *testing.T) {
	sql, _ := BuildMVTQuery("public", "test", "geom", nil, 0, 0, 0, MVTOptions{
		SRID: 4549,
	}, "")

	if !strings.Contains(sql, `ST_Transform(t."geom", 3857)`) {
		t.Fatalf("MVT query should transform source SRID to 3857, sql = %s", sql)
	}
}

func TestBuildMVTQueryUsesWebMercatorSourceDirectly(t *testing.T) {
	sql, _ := BuildMVTQuery("public", "test", "geom", nil, 0, 0, 0, MVTOptions{
		SRID: 3857,
	}, "")

	if strings.Contains(sql, "ST_Transform") {
		t.Fatalf("MVT query should not transform 3857 source, sql = %s", sql)
	}
}
