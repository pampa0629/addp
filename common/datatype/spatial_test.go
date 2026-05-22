package datatype

import (
	"reflect"
	"testing"
)

func TestSpatialInfoHelpersDoNotAssumeGeometryName(t *testing.T) {
	srid := 4326
	dimension := 2
	hasIndex := false
	info := &SpatialInfo{
		GeometryColumns: []GeometryColumnInfo{
			{Name: "shape", GeometryType: string(FieldTypePolygon), SRID: &srid, Dimension: &dimension},
		},
		HasSpatialIndex: &hasIndex,
	}

	primary := info.PrimaryGeometry()
	if primary == nil || primary.Name != "shape" {
		t.Fatalf("PrimaryGeometry() = %#v, want shape", primary)
	}
	if !info.IsPrimaryWGS84() {
		t.Fatalf("IsPrimaryWGS84() = false, want true")
	}
	if got := info.GeometryColumnNames(); !reflect.DeepEqual(got, []string{"shape"}) {
		t.Fatalf("GeometryColumnNames() = %#v", got)
	}
}
