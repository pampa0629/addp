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
	if got := info.PrimaryGeometryName(); got != "shape" {
		t.Fatalf("PrimaryGeometryName() = %q, want shape", got)
	}
	if got := info.PrimaryGeometryType(); got != string(FieldTypePolygon) {
		t.Fatalf("PrimaryGeometryType() = %q, want polygon", got)
	}
	if got := info.PrimarySRIDValue(); got != 4326 {
		t.Fatalf("PrimarySRIDValue() = %d, want 4326", got)
	}
	if got := info.PrimaryDimensionValue(); got != 2 {
		t.Fatalf("PrimaryDimensionValue() = %d, want 2", got)
	}
	if !info.IsPrimaryWGS84() {
		t.Fatalf("IsPrimaryWGS84() = false, want true")
	}
	if got := info.GeometryColumnNames(); !reflect.DeepEqual(got, []string{"shape"}) {
		t.Fatalf("GeometryColumnNames() = %#v", got)
	}
}

func TestNewSingleGeometrySpatialInfo(t *testing.T) {
	info := NewSingleGeometrySpatialInfo("geom", "Point", 4326, 3)
	primary := info.PrimaryGeometry()
	if primary == nil || primary.Name != "geom" || primary.GeometryType != "Point" {
		t.Fatalf("PrimaryGeometry() = %#v", primary)
	}
	if primary.SRID == nil || *primary.SRID != 4326 {
		t.Fatalf("SRID = %#v, want 4326", primary.SRID)
	}
	if primary.Dimension == nil || *primary.Dimension != 3 {
		t.Fatalf("Dimension = %#v, want 3", primary.Dimension)
	}
}

func TestSpatialInfoCloneDeepCopiesPointers(t *testing.T) {
	info := NewSingleGeometrySpatialInfo("geom", "Point", 4326, 2)
	hasIndex := true
	extent := NewBoundingBox(1, 2, 3, 4)
	info.HasSpatialIndex = &hasIndex
	info.Extent = &extent

	cloned := info.Clone()
	if cloned == nil || cloned == info {
		t.Fatalf("Clone() = %#v", cloned)
	}
	cloned.GeometryColumns[0].Name = "shape"
	*cloned.GeometryColumns[0].SRID = 3857
	*cloned.Extent = NewBoundingBox(5, 6, 7, 8)
	*cloned.HasSpatialIndex = false

	primary := info.PrimaryGeometry()
	if primary.Name != "geom" || *primary.SRID != 4326 {
		t.Fatalf("original primary changed: %#v", primary)
	}
	if *info.Extent != (BoundingBox{1, 2, 3, 4}) {
		t.Fatalf("original extent changed: %#v", info.Extent)
	}
	if !*info.HasSpatialIndex {
		t.Fatalf("original has_spatial_index changed")
	}
}
