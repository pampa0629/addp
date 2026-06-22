package datatype

import "testing"

func TestParseGeometryType(t *testing.T) {
	tests := []struct {
		input string
		want  GeometryType
	}{
		{"Point", GeometryTypePoint},
		{"pointz", GeometryTypePoint},
		{"LINESTRING", GeometryTypeLineString},
		{"MultiLineString", GeometryTypeMultiLineString},
		{"polygonm", GeometryTypePolygon},
		{"ST_MultiPolygon", GeometryTypeMultiPolygon},
		{"st_multipolygon", GeometryTypeMultiPolygon},
		{"geometry-collection", GeometryTypeGeometryCollection},
		{"geometrycollection", GeometryTypeGeometryCollection},
		{"", GeometryTypeUnknown},
	}

	for _, tt := range tests {
		if got := ParseGeometryType(tt.input); got != tt.want {
			t.Fatalf("ParseGeometryType(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestStandardGeometryType(t *testing.T) {
	if got := StandardGeometryType("multipolygonz"); got != "MultiPolygon" {
		t.Fatalf("StandardGeometryType() = %q, want MultiPolygon", got)
	}
}
