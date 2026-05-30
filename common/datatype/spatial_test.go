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
	objectSRID := 3857
	info.SRID = &objectSRID
	info.CRS = "EPSG:3857"
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
	*cloned.SRID = 4326
	cloned.CRS = "EPSG:4326"
	*cloned.Extent = NewBoundingBox(5, 6, 7, 8)
	*cloned.HasSpatialIndex = false

	primary := info.PrimaryGeometry()
	if primary.Name != "geom" || *primary.SRID != 4326 {
		t.Fatalf("original primary changed: %#v", primary)
	}
	if info.SRID == nil || *info.SRID != 3857 || info.CRS != "EPSG:3857" {
		t.Fatalf("original spatial reference changed: %#v / %q", info.SRID, info.CRS)
	}
	if *info.Extent != (BoundingBox{1, 2, 3, 4}) {
		t.Fatalf("original extent changed: %#v", info.Extent)
	}
	if !*info.HasSpatialIndex {
		t.Fatalf("original has_spatial_index changed")
	}
}

func TestSpatialInfoFromPayload(t *testing.T) {
	payload := map[string]interface{}{
		"primary_geometry_column": "shape",
		"geometry_columns": []interface{}{
			map[string]interface{}{
				"name":          "shape",
				"geometry_type": "Polygon",
				"srid":          int64(4326),
				"dimension":     int64(2),
				"nullable":      false,
			},
		},
		"has_spatial_index": true,
		"index_name":        "idx_shape",
		"extent":            []interface{}{120.0, 30.0, 121.0, 31.0},
	}

	info := SpatialInfoFromPayload(payload)
	if info == nil {
		t.Fatal("SpatialInfoFromPayload() = nil")
	}
	if info.PrimaryGeometryName() != "shape" || info.PrimaryGeometryType() != "Polygon" {
		t.Fatalf("primary geometry = %q/%q", info.PrimaryGeometryName(), info.PrimaryGeometryType())
	}
	if info.PrimarySRIDValue() != 4326 || info.PrimaryDimensionValue() != 2 {
		t.Fatalf("primary srid/dimension = %d/%d", info.PrimarySRIDValue(), info.PrimaryDimensionValue())
	}
	if info.HasSpatialIndex == nil || !*info.HasSpatialIndex || info.IndexName != "idx_shape" {
		t.Fatalf("spatial index = %#v %q", info.HasSpatialIndex, info.IndexName)
	}
	if info.Extent == nil || *info.Extent != (BoundingBox{120, 30, 121, 31}) {
		t.Fatalf("extent = %#v", info.Extent)
	}
	if primary := info.PrimaryGeometry(); primary == nil || primary.Nullable == nil || *primary.Nullable {
		t.Fatalf("primary nullable = %#v", primary)
	}
}

func TestSpatialInfoFromPayloadUsesSingleColumnAsPrimary(t *testing.T) {
	payload := map[string]interface{}{
		"geometry_columns": []interface{}{
			map[string]interface{}{"name": "geom"},
		},
	}

	info := SpatialInfoFromPayload(payload)
	if info == nil || info.PrimaryGeometryName() != "geom" {
		t.Fatalf("SpatialInfoFromPayload() = %#v, want geom primary", info)
	}
}

func TestSpatialInfoFromPayloadRestoresObjectSpatialReference(t *testing.T) {
	payload := map[string]interface{}{
		"srid":              int64(4326),
		"extent":            []interface{}{120.0, 30.0, 121.0, 31.0},
		"has_spatial_index": false,
		"geometry_columns":  []interface{}{},
	}

	info := SpatialInfoFromPayload(payload)
	if info == nil {
		t.Fatal("SpatialInfoFromPayload() = nil, want object spatial reference")
	}
	if info.PrimaryGeometry() != nil || len(info.GeometryColumns) != 0 {
		t.Fatalf("object spatial reference should not invent geometry columns: %#v", info.GeometryColumns)
	}
	if info.SRID == nil || *info.SRID != 4326 {
		t.Fatalf("srid = %#v, want 4326", info.SRID)
	}
	if info.Extent == nil || *info.Extent != (BoundingBox{120, 30, 121, 31}) {
		t.Fatalf("extent = %#v", info.Extent)
	}
	if info.HasSpatialIndex == nil || *info.HasSpatialIndex {
		t.Fatalf("has_spatial_index = %#v, want false", info.HasSpatialIndex)
	}
}

func TestSpatialInfoPayloadWritesObjectSpatialReferenceWithoutGeometryColumns(t *testing.T) {
	t.Parallel()

	srid := 4326
	hasSpatialIndex := false
	extent := NewBoundingBox(100, 180, 120, 200)
	values := SpatialInfoPayload(&SpatialInfo{
		SRID:            &srid,
		Extent:          &extent,
		HasSpatialIndex: &hasSpatialIndex,
	})

	if values["srid"] != 4326 {
		t.Fatalf("srid = %#v, want 4326", values["srid"])
	}
	if _, ok := values["geometry_columns"]; ok {
		t.Fatalf("non-table spatial should not write geometry_columns: %#v", values)
	}
	if values["has_spatial_index"] != false {
		t.Fatalf("has_spatial_index = %#v, want false", values["has_spatial_index"])
	}
	extentValues := values["extent"].([]float64)
	if len(extentValues) != 4 || extentValues[0] != 100 || extentValues[3] != 200 {
		t.Fatalf("extent = %#v", extentValues)
	}
}

func TestSpatialInfoPayloadPromotesUnnamedColumnReference(t *testing.T) {
	t.Parallel()

	srid := 3857
	values := SpatialInfoPayload(&SpatialInfo{
		GeometryColumns: []GeometryColumnInfo{{SRID: &srid}},
	})

	if values["srid"] != 3857 {
		t.Fatalf("srid = %#v, want 3857", values["srid"])
	}
	if _, ok := values["geometry_columns"]; ok {
		t.Fatalf("unnamed geometry reference should not write geometry_columns: %#v", values)
	}
}

func TestSpatialInfoPayloadWritesGeometryColumnsForTableSpatial(t *testing.T) {
	t.Parallel()

	srid := 4326
	dimension := 2
	nullable := false
	info := &SpatialInfo{
		GeometryColumns: []GeometryColumnInfo{{
			Name:         "shape",
			GeometryType: "MultiPolygon",
			SRID:         &srid,
			Dimension:    &dimension,
			Nullable:     &nullable,
		}},
		PrimaryGeometryColumn: "shape",
	}
	values := SpatialInfoPayload(info)

	if values["primary_geometry_column"] != "shape" {
		t.Fatalf("primary_geometry_column = %#v", values["primary_geometry_column"])
	}
	columns := values["geometry_columns"].([]map[string]interface{})
	if len(columns) != 1 {
		t.Fatalf("geometry_columns = %#v", columns)
	}
	if columns[0]["name"] != "shape" || columns[0]["geometry_type"] != "MultiPolygon" || columns[0]["srid"] != 4326 {
		t.Fatalf("geometry column = %#v", columns[0])
	}
	if columns[0]["nullable"] != false || columns[0]["dimension"] != 2 {
		t.Fatalf("geometry column facts = %#v", columns[0])
	}
}
