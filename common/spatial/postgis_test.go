package spatial

import "testing"

func TestPostGISExpressions(t *testing.T) {
	t.Parallel()

	wkt := PostGISWKTExpression(`geo"col`, "geography")
	if wkt != `ST_AsText("geo""col"::geometry)` {
		t.Fatalf("PostGISWKTExpression() = %q", wkt)
	}
}

func TestIsPostGISSpatialType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		dataType string
		want     bool
	}{
		{name: "geometry", dataType: "geometry", want: true},
		{name: "geometry with srid", dataType: "geometry(Point,4326)", want: true},
		{name: "geography", dataType: "geography", want: true},
		{name: "postgres user defined", dataType: "USER-DEFINED", want: true},
		{name: "plain text", dataType: "text", want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := IsPostGISSpatialType(tt.dataType); got != tt.want {
				t.Fatalf("IsPostGISSpatialType() = %v, want %v", got, tt.want)
			}
		})
	}
}
